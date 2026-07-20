// Package supervisor implements the Ananke run supervisor (ADR-0002 §1-3).
//
// The supervisor remains outside the owned worker process group. A paused
// trampoline leads that group, preserving exact PID/PGID identity through
// authority publication, cleanup, deferred reap, and finalization.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

// Config configures a single supervisor instance.
type Config struct {
	StorePath           string
	RunID               string
	WorkerPath          string
	WorkerArgs          []string
	WorkerEnv           []string
	IdentityPath        string
	SocketPath          string
	TranscriptPath      string
	Token               string
	GracePeriod         time.Duration // SIGTERM→SIGKILL grace; default 2s
	CleanupTimeout      time.Duration // maximum duration of one cleanup proof attempt
	CleanupPollInterval time.Duration // group-quiescence poll interval
	CleanupRetryMin     time.Duration // initial retry delay after a failed proof
	CleanupRetryMax     time.Duration // maximum retry delay while retaining worker-group authority
	Backend             lifecycle.LifecycleBackend
}

// Supervisor manages one run while remaining outside its owned worker group.
type Supervisor struct {
	cfg                   Config
	backend               lifecycle.LifecycleBackend
	store                 *store.Store
	id                    lifecycle.Identity
	transcriptFile        *os.File
	transcriptRequired    bool
	writeIdentity         func(string, lifecycle.Identity) error
	setRunSupervisor      func(context.Context, string, int, int, int) error
	setTranscriptIdentity func(context.Context, string, store.TranscriptFileIdentity) error
	listenAuthority       func() error
	acknowledgeOutbox     func(context.Context, string) error
	socketServeOnce       sync.Once
	cancelCh              chan struct{}

	mu              sync.Mutex
	workerPID       int
	cancelled       bool
	finalState      store.State
	exitCode        int
	reaped          bool
	cleanupRequired bool

	listener net.Listener
}

// New validates the config, opens the store, loads the run, and returns a
// Supervisor ready to Run. It does NOT become group leader or launch the
// worker yet.
func New(ctx context.Context, cfg Config) (*Supervisor, error) {
	if cfg.RunID == "" {
		return nil, errors.New("supervisor: run id required")
	}
	if cfg.WorkerPath == "" {
		return nil, errors.New("supervisor: worker path required")
	}
	if cfg.SocketPath == "" {
		return nil, errors.New("supervisor: socket path required")
	}
	if cfg.Token == "" {
		return nil, errors.New("supervisor: token required")
	}
	if cfg.GracePeriod == 0 {
		cfg.GracePeriod = 2 * time.Second
	}
	if cfg.CleanupTimeout <= 0 {
		cfg.CleanupTimeout = 5 * time.Second
	}
	if cfg.CleanupPollInterval <= 0 {
		cfg.CleanupPollInterval = 100 * time.Millisecond
	}
	if cfg.CleanupRetryMin <= 0 {
		cfg.CleanupRetryMin = 50 * time.Millisecond
	}
	if cfg.CleanupRetryMax <= 0 {
		cfg.CleanupRetryMax = time.Second
	}
	if cfg.CleanupRetryMax < cfg.CleanupRetryMin {
		cfg.CleanupRetryMax = cfg.CleanupRetryMin
	}
	st, err := store.Open(cfg.StorePath)
	if err != nil {
		return nil, fmt.Errorf("supervisor: open store: %w", err)
	}
	run, err := st.GetRun(ctx, cfg.RunID)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("supervisor: load run: %w", err)
	}
	if run.State != store.StateCreated {
		st.Close()
		return nil, fmt.Errorf("supervisor: run state is %q, want %q", run.State, store.StateCreated)
	}
	// Inherit WorkerArgs and WorkerEnv from the run spec if not provided
	// directly (the supervisor binary doesn't pass these via flags).
	if len(cfg.WorkerArgs) == 0 {
		cfg.WorkerArgs = run.WorkerArgs
	}
	if len(cfg.WorkerEnv) == 0 {
		cfg.WorkerEnv = run.WorkerEnv
	}
	if cfg.TranscriptPath == "" {
		cfg.TranscriptPath = run.TranscriptPath
	}
	backend := cfg.Backend
	if backend == nil {
		backend = lifecycle.NewDarwinBackend()
	}
	supervisor := &Supervisor{
		cfg:                   cfg,
		backend:               backend,
		store:                 st,
		transcriptRequired:    run.TranscriptRequired,
		writeIdentity:         lifecycle.WriteIdentity,
		setRunSupervisor:      st.SetRunSupervisor,
		setTranscriptIdentity: st.SetTranscriptIdentity,
		acknowledgeOutbox:     st.AcknowledgeOutbox,
		cancelCh:              make(chan struct{}, 1),
	}
	supervisor.listenAuthority = supervisor.listenSocket
	return supervisor, nil
}

// Close releases the store handle.
func (s *Supervisor) Close() error {
	var result error
	if s.transcriptFile != nil {
		result = s.transcriptFile.Close()
		s.transcriptFile = nil
	}
	if s.store != nil {
		result = errors.Join(result, s.store.Close())
	}
	return result
}

func createTranscript(path string) (*os.File, store.TranscriptFileIdentity, error) {
	if path == "" {
		return nil, store.TranscriptFileIdentity{}, errors.New("transcript path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, store.TranscriptFileIdentity{}, fmt.Errorf("create transcript directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, store.TranscriptFileIdentity{}, fmt.Errorf("create transcript %q: %w", path, err)
	}
	closeFailure := func(failure error) (*os.File, store.TranscriptFileIdentity, error) {
		return nil, store.TranscriptFileIdentity{}, errors.Join(failure, f.Close())
	}
	if err := f.Chmod(0o600); err != nil {
		return closeFailure(fmt.Errorf("set transcript mode: %w", err))
	}
	identity, err := lifecycle.TranscriptIdentityFromFile(f)
	if err != nil {
		return closeFailure(fmt.Errorf("derive transcript identity: %w", err))
	}
	return f, identity, nil
}

func (s *Supervisor) finishNoProcessFailure(ctx context.Context, failure error) (store.State, error) {
	if err := s.store.CommitNoProcessFailure(ctx, s.cfg.RunID, failure.Error()); err != nil {
		return "", errors.Join(failure, fmt.Errorf("persist no-process failure: %w", err))
	}
	s.mu.Lock()
	s.finalState = store.StateFailed
	s.mu.Unlock()
	return store.StateFailed, failure
}

// Run executes the full supervisor lifecycle. It blocks until the worker has
// exited, cleanup is complete, and the terminal transition + outbox row are
// committed. Returns the committed terminal state.
func (s *Supervisor) Run(ctx context.Context) (store.State, error) {
	defer s.Close()
	lifecycleCtx := context.WithoutCancel(ctx)

	s.id = lifecycle.Identity{
		RunID:          s.cfg.RunID,
		SupervisorPID:  os.Getpid(),
		WorkerArgs:     s.cfg.WorkerArgs,
		SocketPath:     s.cfg.SocketPath,
		Token:          s.cfg.Token,
		TranscriptPath: s.cfg.TranscriptPath,
		LaunchTime:     time.Now().UTC(),
	}

	if s.transcriptRequired {
		transcriptFile, transcriptIdentity, err := createTranscript(s.cfg.TranscriptPath)
		if err != nil {
			return s.finishNoProcessFailure(lifecycleCtx, fmt.Errorf("prepare transcript: %w", err))
		}
		s.transcriptFile = transcriptFile
		s.id.TranscriptIdentity = transcriptIdentity
		if err := s.setTranscriptIdentity(lifecycleCtx, s.cfg.RunID, transcriptIdentity); err != nil {
			return s.finishNoProcessFailure(lifecycleCtx, fmt.Errorf("persist transcript identity: %w", err))
		}
	}

	workerPID, launchErr := s.backend.LaunchWorker(s.cfg.WorkerPath, s.cfg.WorkerArgs, s.cfg.WorkerEnv)
	if workerPID <= 0 {
		if launchErr == nil {
			launchErr = fmt.Errorf("invalid worker pid %d", workerPID)
		}
		return s.finishNoProcessFailure(lifecycleCtx, fmt.Errorf("launch worker: %w", launchErr))
	}

	// A positive PID transfers ownership even when watcher construction also
	// failed. The paused trampoline is the owned group leader, so the legacy
	// SupervisorPGID compatibility field intentionally stores its PID/PGID.
	s.mu.Lock()
	s.workerPID = workerPID
	s.mu.Unlock()
	s.id.WorkerPID = workerPID
	s.id.SupervisorPGID = workerPID
	defer s.closeAuthoritySocket()

	var startedErr error
	if startedErr == nil {
		if err := s.writeIdentity(s.cfg.IdentityPath, s.id); err != nil {
			startedErr = fmt.Errorf("write identity (post-launch): %w", err)
		}
	}
	if startedErr == nil {
		if err := s.setRunSupervisor(lifecycleCtx, s.cfg.RunID, s.id.SupervisorPID, s.id.SupervisorPGID, workerPID); err != nil {
			startedErr = fmt.Errorf("set run supervisor: %w", err)
		}
	}
	if startedErr == nil {
		if err := s.ensureSocketAuthority(lifecycleCtx); err != nil {
			startedErr = err
		}
	}
	if startedErr == nil {
		if err := s.store.Transition(lifecycleCtx, s.cfg.RunID, store.StateRunning, "worker authority published"); err != nil {
			startedErr = fmt.Errorf("transition to running: %w", err)
		}
	}
	// A positive PID transfers process ownership even if exact-exit watcher
	// construction failed. Publish that authority first so cleanup_required is
	// a legal durable transition, then fail closed without releasing the paused
	// trampoline.
	if startedErr == nil && launchErr != nil {
		startedErr = fmt.Errorf("launch worker watcher: %w", launchErr)
	}
	if startedErr != nil {
		return s.finishOwnedWorker(lifecycleCtx, workerPID, startedErr)
	}

	run, err := s.store.GetRun(lifecycleCtx, s.cfg.RunID)
	if err != nil {
		return s.finishOwnedWorker(lifecycleCtx, workerPID, fmt.Errorf("load cancellation intent: %w", err))
	}
	if run.CancelRequested {
		return s.finishOwnedWorker(lifecycleCtx, workerPID,
			s.cancelOwnedWorker(lifecycleCtx, "durable cancellation requested during startup"))
	}
	if err := s.backend.ReleaseWorker(workerPID); err != nil {
		return s.finishOwnedWorker(lifecycleCtx, workerPID, fmt.Errorf("release worker trampoline: %w", err))
	}
	exitCh, err := s.backend.WorkerExited(workerPID)
	if err != nil {
		return s.finishOwnedWorker(lifecycleCtx, workerPID, fmt.Errorf("worker exit monitor: %w", err))
	}

	cancelCh := s.cancelCh
	var waitErr error
	select {
	case <-exitCh:
	case <-cancelCh:
		waitErr = s.cancelOwnedWorker(lifecycleCtx, "cancel requested")
	case <-ctx.Done():
		waitErr = s.cancelOwnedWorker(lifecycleCtx, "context cancelled")
	}
	return s.finishOwnedWorker(lifecycleCtx, workerPID, waitErr)
}

func (s *Supervisor) cancelOwnedWorker(ctx context.Context, reason string) error {
	s.mu.Lock()
	s.cancelled = true
	s.mu.Unlock()
	if err := s.persistCancelling(ctx, reason); err != nil {
		return fmt.Errorf("persist cancellation: %w", err)
	}
	return nil
}

func (s *Supervisor) persistCancelling(ctx context.Context, _ string) error {
	state, err := s.store.RequestCancellation(ctx, s.cfg.RunID)
	if err != nil {
		return err
	}
	if state == store.StateCreated {
		return fmt.Errorf("cannot enter cancellation path before supervisor bootstrap")
	}
	return nil
}

func (s *Supervisor) finishOwnedWorker(ctx context.Context, workerPID int, failure error) (store.State, error) {
	var (
		exitCode int
		reapErr  error
	)
	if mutationHooks.reapBeforeCleanup {
		exitCode, reapErr = s.reapOwnedWorker(workerPID)
		failure = s.cleanupGroupUntilProven(ctx, s.id.SupervisorPGID, failure)
	} else {
		failure = s.cleanupGroupUntilProven(ctx, s.id.SupervisorPGID, failure)
		exitCode, reapErr = s.reapOwnedWorker(workerPID)
	}
	if reapErr != nil {
		failure = errors.Join(failure, fmt.Errorf("reap worker: %w", reapErr))
		s.retainCleanupObligationUntilPersisted(ctx, failure.Error())
	}
	if reapErr == nil && s.transcriptRequired {
		if err := s.awaitTranscriptDurability(ctx); err != nil {
			failure = errors.Join(failure, fmt.Errorf("final transcript durability: %w", err))
			s.retainCleanupObligationUntilPersisted(ctx, failure.Error())
			return store.StateCleanupRequired, failure
		}
	}

	s.ensureFinalizationAuthority(ctx)
	// M3 mutation: signal the numeric PGID after reap (unsafe).
	if mutationHooks.signalAfterReap {
		_ = s.backend.SignalGroup(s.id.SupervisorPGID, unix.SIGTERM)
	}

	terminal, reason := s.decideTerminal(exitCode)
	if failure != nil {
		terminal = store.StateFailed
		reason = "lifecycle failure after worker start: " + failure.Error()
	}
	outbox := store.OutboxRow{
		RunID:          s.cfg.RunID,
		TerminalState:  terminal,
		SupervisorPID:  s.id.SupervisorPID,
		SupervisorPGID: s.id.SupervisorPGID,
		SocketPath:     s.cfg.SocketPath,
		Token:          s.cfg.Token,
	}
	commitErr := s.store.CommitTerminal(ctx, s.cfg.RunID, terminal, reason, outbox)
	if errors.Is(commitErr, store.ErrCancellationRequested) && failure == nil {
		terminal = store.StateCancelled
		reason = "cancelled by durable request"
		outbox.TerminalState = terminal
		commitErr = s.store.CommitTerminal(ctx, s.cfg.RunID, terminal, reason, outbox)
	}
	if commitErr != nil {
		return "", fmt.Errorf("commit terminal: %w", commitErr)
	}
	s.mu.Lock()
	s.finalState = terminal
	s.mu.Unlock()
	s.acknowledgeOutboxUntilDurable(ctx)
	return terminal, failure
}

func (s *Supervisor) ensureFinalizationAuthority(ctx context.Context) {
	backoff := s.cfg.CleanupRetryMin
	for {
		if err := s.establishFinalizationAuthority(ctx); err == nil {
			return
		}
		timer := time.NewTimer(backoff)
		<-timer.C
		backoff = nextCleanupBackoff(backoff, s.cfg.CleanupRetryMax)
	}
}

func (s *Supervisor) establishFinalizationAuthority(ctx context.Context) error {
	if err := s.writeIdentity(s.cfg.IdentityPath, s.id); err != nil {
		return fmt.Errorf("write final identity: %w", err)
	}
	identity, err := lifecycle.ReadIdentity(s.cfg.IdentityPath)
	if err != nil {
		return fmt.Errorf("read final identity: %w", err)
	}
	if identity.RunID != s.id.RunID || identity.SupervisorPID != s.id.SupervisorPID ||
		identity.SupervisorPGID != s.id.SupervisorPGID || identity.WorkerPID != s.id.WorkerPID ||
		identity.SocketPath != s.id.SocketPath || identity.Token != s.id.Token ||
		identity.TranscriptPath != s.id.TranscriptPath ||
		identity.TranscriptIdentity != s.id.TranscriptIdentity {
		return errors.New("final identity does not match supervisor authority")
	}
	if err := s.setRunSupervisor(ctx, s.cfg.RunID, s.id.SupervisorPID, s.id.SupervisorPGID, s.id.WorkerPID); err != nil {
		return fmt.Errorf("set final run identity: %w", err)
	}
	run, err := s.store.GetRun(ctx, s.cfg.RunID)
	if err != nil {
		return fmt.Errorf("read final run identity: %w", err)
	}
	if run.ID != s.id.RunID || run.SupervisorPID != s.id.SupervisorPID ||
		run.SupervisorPGID != s.id.SupervisorPGID || run.WorkerPID != s.id.WorkerPID ||
		run.SocketPath != s.id.SocketPath || run.Token != s.id.Token ||
		run.TranscriptPath != s.id.TranscriptPath ||
		run.TranscriptIdentity != s.id.TranscriptIdentity {
		return errors.New("durable run does not match supervisor authority")
	}
	if s.transcriptRequired {
		if _, err := s.validateTranscriptAuthority(ctx); err != nil {
			return fmt.Errorf("validate final transcript authority: %w", err)
		}
	}
	return s.ensureSocketAuthority(ctx)
}

func (s *Supervisor) validateTranscriptAuthority(ctx context.Context) (os.FileInfo, error) {
	expected := s.id.TranscriptIdentity
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("supervisor transcript identity: %w", err)
	}
	if s.transcriptFile == nil {
		return nil, errors.New("supervisor transcript anchor is not open")
	}
	anchorInfo, err := s.transcriptFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat retained transcript anchor: %w", err)
	}
	if err := lifecycle.ValidateTranscriptIdentity(anchorInfo, expected); err != nil {
		return nil, fmt.Errorf("retained transcript anchor: %w", err)
	}
	pathInfo, err := os.Stat(s.cfg.TranscriptPath)
	if err != nil {
		return nil, fmt.Errorf("stat named transcript %q: %w", s.cfg.TranscriptPath, err)
	}
	if err := lifecycle.ValidateTranscriptIdentity(pathInfo, expected); err != nil {
		return nil, fmt.Errorf("named transcript %q: %w", s.cfg.TranscriptPath, err)
	}
	if pathInfo.Size() != anchorInfo.Size() {
		return nil, fmt.Errorf("named transcript size %d does not match retained anchor size %d", pathInfo.Size(), anchorInfo.Size())
	}
	run, err := s.store.GetRun(ctx, s.cfg.RunID)
	if err != nil {
		return nil, fmt.Errorf("load durable transcript authority: %w", err)
	}
	if run.TranscriptPath != s.cfg.TranscriptPath || run.TranscriptIdentity != expected {
		return nil, fmt.Errorf("durable run transcript authority does not match supervisor: path=%q identity=%+v", run.TranscriptPath, run.TranscriptIdentity)
	}
	identity, err := lifecycle.ReadIdentity(s.cfg.IdentityPath)
	if err != nil {
		return nil, fmt.Errorf("read transcript identity record: %w", err)
	}
	if identity.TranscriptPath != s.cfg.TranscriptPath || identity.TranscriptIdentity != expected {
		return nil, fmt.Errorf("identity record transcript authority does not match supervisor: path=%q identity=%+v", identity.TranscriptPath, identity.TranscriptIdentity)
	}
	return anchorInfo, nil
}

func (s *Supervisor) acknowledgeOutboxUntilDurable(ctx context.Context) {
	backoff := s.cfg.CleanupRetryMin
	for {
		if err := s.acknowledgeOutbox(ctx, s.cfg.RunID); err == nil {
			return
		}
		row, err := s.store.GetOutbox(ctx, s.cfg.RunID)
		if err == nil && row.Acknowledged == 1 {
			return
		}
		timer := time.NewTimer(backoff)
		<-timer.C
		backoff = nextCleanupBackoff(backoff, s.cfg.CleanupRetryMax)
	}
}

// awaitTranscriptDurability seals the transcript only after exact worker reap,
// then waits on the store high-water mark written by the daemon tailer. The
// named file, retained descriptor, run row, and identity file must all retain
// the prelaunch device/inode authority through handoff.
func (s *Supervisor) awaitTranscriptDurability(ctx context.Context) error {
	info, err := s.validateTranscriptAuthority(ctx)
	if err != nil {
		return err
	}
	finalSize := info.Size()

	backoff := s.cfg.CleanupRetryMin
	retry := func() {
		timer := time.NewTimer(backoff)
		<-timer.C
		backoff = nextCleanupBackoff(backoff, s.cfg.CleanupRetryMax)
	}
	sealed := false
	for {
		run, err := s.store.GetRun(ctx, s.cfg.RunID)
		if err != nil {
			if permanentTranscriptStoreError(err) {
				return fmt.Errorf("load run before transcript handoff: %w", err)
			}
			retry()
			continue
		}
		if store.IsTerminal(run.State) {
			return fmt.Errorf("terminal state %q published before transcript handoff completed", run.State)
		}
		if run.TranscriptIdentity != s.id.TranscriptIdentity {
			return fmt.Errorf("durable transcript identity changed during handoff: have %+v want %+v", run.TranscriptIdentity, s.id.TranscriptIdentity)
		}

		if !sealed {
			if err := s.store.SealTranscript(ctx, s.cfg.RunID, finalSize); err != nil {
				if permanentTranscriptStoreError(err) {
					return fmt.Errorf("seal transcript at %d bytes: %w", finalSize, err)
				}
				retry()
				continue
			}
			sealed = true
		}

		progress, err := s.store.GetTranscriptProgress(ctx, s.cfg.RunID)
		if err != nil {
			if permanentTranscriptStoreError(err) {
				return fmt.Errorf("load transcript progress: %w", err)
			}
			retry()
			continue
		}
		if progress.FinalSize != finalSize {
			return fmt.Errorf("sealed size changed from %d to %d", finalSize, progress.FinalSize)
		}
		if progress.ConsumedOffset == finalSize {
			currentInfo, err := s.validateTranscriptAuthority(ctx)
			if err != nil {
				return fmt.Errorf("revalidate transcript authority: %w", err)
			}
			if currentInfo.Size() != finalSize {
				return fmt.Errorf("transcript size changed from %d to %d during handoff", finalSize, currentInfo.Size())
			}
			return nil
		}
		if progress.ConsumedOffset > finalSize {
			return fmt.Errorf("consumed offset %d exceeds final size %d", progress.ConsumedOffset, finalSize)
		}
		retry()
	}
}

func permanentTranscriptStoreError(err error) bool {
	return errors.Is(err, store.ErrRunNotFound) ||
		errors.Is(err, store.ErrTranscriptFinalSizeChanged) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

func (s *Supervisor) reapOwnedWorker(workerPID int) (int, error) {
	exitCode, err := s.backend.ReapWorker(workerPID)
	s.mu.Lock()
	s.exitCode = exitCode
	s.reaped = err == nil
	s.mu.Unlock()
	return exitCode, err
}

// cleanupGroupUntilProven retains a durable cleanup obligation after any
// failure and retries forever with capped backoff. The supervisor therefore
// remains the group anchor until an empty-group enumeration is observed.
func (s *Supervisor) cleanupGroupUntilProven(ctx context.Context, pgid int, failure error) error {
	backoff := s.cfg.CleanupRetryMin
	obligationPersisted := failure == nil
	cleanupFailureRecorded := false
	for {
		if failure != nil && !obligationPersisted {
			obligationPersisted = s.retainCleanupObligation(ctx, failure.Error()) == nil
		}

		cleanupErr := s.cleanupGroup(ctx, pgid)
		if cleanupErr == nil && obligationPersisted {
			return failure
		}
		if cleanupErr != nil && !cleanupFailureRecorded {
			failure = errors.Join(failure, fmt.Errorf("cleanup group: %w", cleanupErr))
			cleanupFailureRecorded = true
			obligationPersisted = false
		}

		timer := time.NewTimer(backoff)
		<-timer.C
		backoff = nextCleanupBackoff(backoff, s.cfg.CleanupRetryMax)
	}
}

func (s *Supervisor) retainCleanupObligation(ctx context.Context, reason string) error {
	run, err := s.store.GetRun(ctx, s.cfg.RunID)
	if err != nil {
		return err
	}
	if run.State == store.StateCleanupRequired {
		s.mu.Lock()
		s.cleanupRequired = true
		s.mu.Unlock()
		return nil
	}
	if store.IsTerminal(run.State) || !store.CanTransition(run.State, store.StateCleanupRequired) {
		return fmt.Errorf("cannot retain cleanup obligation from %q", run.State)
	}
	if err := s.store.Transition(ctx, s.cfg.RunID, store.StateCleanupRequired, reason); err != nil {
		return err
	}
	s.mu.Lock()
	s.cleanupRequired = true
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) retainCleanupObligationUntilPersisted(ctx context.Context, reason string) {
	backoff := s.cfg.CleanupRetryMin
	for s.retainCleanupObligation(ctx, reason) != nil {
		timer := time.NewTimer(backoff)
		<-timer.C
		backoff = nextCleanupBackoff(backoff, s.cfg.CleanupRetryMax)
	}
}

func nextCleanupBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum || current > maximum/2 {
		return maximum
	}
	return current * 2
}

// cleanupGroup uses membership only as fail-closed quiescence evidence. TERM
// and KILL are each one atomic negative-PGID signal while the unreaped worker
// leader pins group identity.
func (s *Supervisor) cleanupGroup(ctx context.Context, pgid int) error {
	// M6 mutation: skip group cleanup entirely — only the parent PID was
	// signalled, leaving resistant descendants alive.
	if mutationHooks.cancelParentOnly {
		return nil
	}
	members, err := s.backend.GroupMembers(pgid)
	if err != nil {
		return fmt.Errorf("group members: %w", err)
	}
	if len(members) == 0 {
		return nil
	}
	if err := s.backend.SignalGroup(pgid, unix.SIGTERM); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("SIGTERM group %d: %w", pgid, err)
	}
	if s.cfg.GracePeriod > 0 {
		timer := time.NewTimer(s.cfg.GracePeriod)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}

	remaining, err := s.backend.GroupMembers(pgid)
	if err != nil {
		return fmt.Errorf("re-enumerate group before SIGKILL: %w", err)
	}
	if len(remaining) == 0 {
		return nil
	}
	if err := s.backend.SignalGroup(pgid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("SIGKILL group %d: %w", pgid, err)
	}

	deadline := time.Now().Add(s.cfg.CleanupTimeout)
	for {
		remaining, err = s.backend.GroupMembers(pgid)
		if err != nil {
			return fmt.Errorf("final group members: %w", err)
		}
		if len(remaining) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("group members still alive at cleanup deadline: %v", remaining)
		}
		timer := time.NewTimer(s.cfg.CleanupPollInterval)
		<-timer.C
	}
}

// decideTerminal picks the terminal state from the exit code and cancel flag.
func (s *Supervisor) decideTerminal(exitCode int) (store.State, string) {
	run, err := s.store.GetRun(context.Background(), s.cfg.RunID)
	if err != nil {
		return store.StateFailed, "durable run state unavailable: " + err.Error()
	}
	if run.State == store.StateCleanupRequired {
		return store.StateFailed, "transcript corruption required group cleanup"
	}
	s.mu.Lock()
	cancelled := s.cancelled
	s.mu.Unlock()
	if run.CancelRequested || run.State == store.StateCancelling || cancelled {
		return store.StateCancelled, "cancelled by request"
	}
	if exitCode == 0 {
		return store.StateCompleted, "worker exited 0"
	}
	return store.StateFailed, fmt.Sprintf("worker exited %d", exitCode)
}

// --- Socket command protocol ---

type cmdRequest struct {
	Cmd   string `json:"cmd"`
	Token string `json:"token"`
}

type cmdResponse struct {
	OK               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	State            string `json:"state,omitempty"`
	RunID            string `json:"run_id,omitempty"`
	SupervisorPID    int    `json:"supervisor_pid,omitempty"`
	WorkerPID        int    `json:"worker_pid,omitempty"`
	ExitCode         int    `json:"exit_code,omitempty"`
	PGID             int    `json:"pgid,omitempty"`
	TranscriptDevice int64  `json:"transcript_device,omitempty"`
	TranscriptInode  int64  `json:"transcript_inode,omitempty"`
}

func (s *Supervisor) commandResponse() cmdResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.finalState
	if state == "" {
		if s.cleanupRequired {
			state = store.StateCleanupRequired
		} else if s.cancelled {
			state = store.StateCancelling
		} else {
			state = store.StateRunning
		}
	}
	return cmdResponse{
		OK:               true,
		State:            string(state),
		RunID:            s.cfg.RunID,
		SupervisorPID:    s.id.SupervisorPID,
		WorkerPID:        s.workerPID,
		ExitCode:         s.exitCode,
		PGID:             s.id.SupervisorPGID,
		TranscriptDevice: s.id.TranscriptIdentity.Device,
		TranscriptInode:  s.id.TranscriptIdentity.Inode,
	}
}

func (s *Supervisor) listenSocket() error {
	if err := os.Remove(s.cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket %s: %w", s.cfg.SocketPath, err)
	}
	dir := filepath.Dir(s.cfg.SocketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create socket directory %s: %w", dir, err)
	}
	listener, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.SocketPath, err)
	}
	s.listener = listener
	return nil
}

func (s *Supervisor) ensureSocketAuthority(ctx context.Context) error {
	if s.listener == nil {
		if err := s.listenAuthority(); err != nil {
			return err
		}
	}
	if s.listener == nil {
		return errors.New("socket listener was not established")
	}
	s.socketServeOnce.Do(func() {
		go s.serveSocket(ctx, s.cancelCh)
	})
	return s.probeSocketAuthority(ctx)
}
func (s *Supervisor) probeSocketAuthority(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(probeCtx, "unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("dial authority socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(conn).Encode(cmdRequest{Cmd: "status", Token: s.cfg.Token}); err != nil {
		return fmt.Errorf("write authority probe: %w", err)
	}
	var response cmdResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return fmt.Errorf("read authority probe: %w", err)
	}
	if !response.OK || response.RunID != s.id.RunID ||
		response.SupervisorPID != s.id.SupervisorPID || response.WorkerPID != s.id.WorkerPID ||
		response.PGID != s.id.SupervisorPGID ||
		response.TranscriptDevice != s.id.TranscriptIdentity.Device ||
		response.TranscriptInode != s.id.TranscriptIdentity.Inode {
		return fmt.Errorf("authority probe identity mismatch: %+v", response)
	}
	return nil
}

func (s *Supervisor) closeAuthoritySocket() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.cfg.SocketPath)
}

func (s *Supervisor) serveSocket(ctx context.Context, cancelCh chan<- struct{}) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn, cancelCh)
	}
}

func (s *Supervisor) handleConn(conn net.Conn, cancelCh chan<- struct{}) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	var req cmdRequest
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(cmdResponse{OK: false, Error: "bad request"})
		return
	}
	if req.Token != s.cfg.Token {
		_ = enc.Encode(cmdResponse{OK: false, Error: "auth failed"})
		return
	}
	switch req.Cmd {
	case "ping", "status", "finalize":
		_ = enc.Encode(s.commandResponse())
	case "cancel":
		s.mu.Lock()
		already := s.cancelled
		s.mu.Unlock()
		if !already {
			select {
			case cancelCh <- struct{}{}:
			default:
			}
		}
		_ = enc.Encode(cmdResponse{OK: true, State: "cancelling"})
	case "adopt":
		// The response proves the authenticated supervisor identity while the
		// daemon acknowledges reconnection. Ownership remains with the process
		// that originally launched the supervisor.
		_ = enc.Encode(s.commandResponse())
	default:
		_ = enc.Encode(cmdResponse{OK: false, Error: "unknown cmd"})
	}
}

// currentState returns the supervisor's view of the run state. For the proof,
// this is derived from the local lifecycle phase, not a store round-trip.
func (s *Supervisor) currentState() store.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalState != "" {
		return s.finalState
	}
	if s.cleanupRequired {
		return store.StateCleanupRequired
	}
	if s.cancelled {
		return store.StateCancelling
	}
	return store.StateRunning
}
