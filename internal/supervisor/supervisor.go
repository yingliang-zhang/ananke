// Package supervisor implements the Ananke run supervisor (ADR-0002 §1-3).
//
// The supervisor is the process-group anchor. It becomes the group leader at
// startup, launches the worker into its own group, monitors for worker exit or
// cancellation, cleans up any resistant descendants, reaps the worker, and
// commits a terminal state transition with a finalization outbox row.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

// Config configures a single supervisor instance.
type Config struct {
	StorePath      string
	RunID          string
	WorkerPath     string
	WorkerArgs     []string
	WorkerEnv      []string
	IdentityPath   string
	SocketPath     string
	TranscriptPath string
	Token          string
	GracePeriod    time.Duration // SIGTERM→SIGKILL grace; default 2s
	Backend        lifecycle.LifecycleBackend
}

// Supervisor manages one run's lifecycle as the process-group anchor.
type Supervisor struct {
	cfg     Config
	backend lifecycle.LifecycleBackend
	store   *store.Store
	id      lifecycle.Identity

	mu         sync.Mutex
	workerPID  int
	cancelled  bool
	finalState store.State
	exitCode   int
	reaped     bool

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
	backend := cfg.Backend
	if backend == nil {
		backend = lifecycle.NewDarwinBackend()
	}
	return &Supervisor{cfg: cfg, backend: backend, store: st}, nil
}

// Close releases the store handle.
func (s *Supervisor) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// Run executes the full supervisor lifecycle. It blocks until the worker has
// exited, cleanup is complete, and the terminal transition + outbox row are
// committed. Returns the committed terminal state.
func (s *Supervisor) Run(ctx context.Context) (store.State, error) {
	defer s.Close()

	// 1. Become group leader (ADR-0002 §1).
	pgid, err := s.backend.BecomeGroupLeader()
	if err != nil {
		return "", fmt.Errorf("become group leader: %w", err)
	}
	s.id = lifecycle.Identity{
		SupervisorPID:  os.Getpid(),
		SupervisorPGID: pgid,
		WorkerArgs:     s.cfg.WorkerArgs,
		SocketPath:     s.cfg.SocketPath,
		Token:          s.cfg.Token,
		TranscriptPath: s.cfg.TranscriptPath,
		LaunchTime:     time.Now().UTC(),
	}

	// 2. Write identity file BEFORE launching the worker (ADR-0002 §3).
	// worker_pid is 0 until launch; updated immediately after.
	if err := lifecycle.WriteIdentity(s.cfg.IdentityPath, s.id); err != nil {
		return "", fmt.Errorf("write identity (pre-launch): %w", err)
	}

	// 3. Transition created → running.
	if err := s.store.Transition(ctx, s.cfg.RunID, store.StateRunning, "supervisor started"); err != nil {
		return "", fmt.Errorf("transition to running: %w", err)
	}

	// 4. Launch the worker into the supervisor's process group.
	workerPID, err := s.backend.LaunchWorker(s.cfg.WorkerPath, s.cfg.WorkerArgs, s.cfg.WorkerEnv)
	if err != nil {
		_ = s.store.Transition(ctx, s.cfg.RunID, store.StateFailed, "launch failed: "+err.Error())
		return store.StateFailed, fmt.Errorf("launch worker: %w", err)
	}
	s.mu.Lock()
	s.workerPID = workerPID
	s.mu.Unlock()
	s.id.WorkerPID = workerPID

	// 5. Rewrite identity file with the worker PID.
	if err := lifecycle.WriteIdentity(s.cfg.IdentityPath, s.id); err != nil {
		return "", fmt.Errorf("write identity (post-launch): %w", err)
	}

	// 6. Record supervisor identity in the store.
	if err := s.store.SetRunSupervisor(ctx, s.cfg.RunID, s.id.SupervisorPID, s.id.SupervisorPGID, workerPID); err != nil {
		return "", fmt.Errorf("set run supervisor: %w", err)
	}

	// 7. Listen on the Unix socket for commands (cancel, status, adopt).
	if err := s.listenSocket(); err != nil {
		return "", err
	}
	defer os.Remove(s.cfg.SocketPath)

	// 8. Ignore SIGTERM so group-wide TERM does not kill the supervisor.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, unix.SIGTERM)
	go func() {
		for range sigCh {
			// swallow — supervisor must survive group-wide TERM
		}
	}()

	// 9. Monitor worker exit.
	exitCh, err := s.backend.WorkerExited(workerPID)
	if err != nil {
		return "", fmt.Errorf("worker exit monitor: %w", err)
	}

	// 10. Wait for worker exit or cancellation.
	cancelCh := make(chan struct{})
	go s.serveSocket(ctx, cancelCh)

	select {
	case <-exitCh:
		// worker exited on its own
	case <-cancelCh:
		s.mu.Lock()
		s.cancelled = true
		s.mu.Unlock()
		_ = s.store.Transition(ctx, s.cfg.RunID, store.StateCancelling, "cancel requested")
		// Signal the worker to exit.
		_ = s.backend.SignalProcess(workerPID, unix.SIGTERM)
		select {
		case <-exitCh:
		case <-time.After(s.cfg.GracePeriod):
			_ = s.backend.SignalProcess(workerPID, unix.SIGKILL)
			<-exitCh
		}
	case <-ctx.Done():
		// external shutdown — treat as cancel
		s.mu.Lock()
		s.cancelled = true
		s.mu.Unlock()
		_ = s.store.Transition(ctx, s.cfg.RunID, store.StateCancelling, "context cancelled")
		_ = s.backend.SignalProcess(workerPID, unix.SIGTERM)
		select {
		case <-exitCh:
		case <-time.After(s.cfg.GracePeriod):
			_ = s.backend.SignalProcess(workerPID, unix.SIGKILL)
			<-exitCh
		}
	}

	// 11. Cleanup: check for surviving group members, escalate if needed.
	if err := s.cleanupGroup(ctx, s.id.SupervisorPGID); err != nil {
		// cleanup error does not prevent reap; record it
		fmt.Fprintf(os.Stderr, "supervisor: cleanup group: %v\n", err)
	}

	// 12. Reap the worker (only after group cleanup).
	exitCode, reapErr := s.backend.ReapWorker(workerPID)
	s.mu.Lock()
	s.exitCode = exitCode
	s.reaped = true
	s.mu.Unlock()
	if reapErr != nil {
		fmt.Fprintf(os.Stderr, "supervisor: reap worker: %v\n", reapErr)
	}

	// 13. Commit terminal state + outbox row atomically.
	terminal, reason := s.decideTerminal(exitCode)
	outbox := store.OutboxRow{
		RunID:          s.cfg.RunID,
		TerminalState:  terminal,
		SupervisorPID:  s.id.SupervisorPID,
		SupervisorPGID: s.id.SupervisorPGID,
		SocketPath:     s.cfg.SocketPath,
		Token:          s.cfg.Token,
	}
	if err := s.store.CommitTerminal(ctx, s.cfg.RunID, terminal, reason, outbox); err != nil {
		return "", fmt.Errorf("commit terminal: %w", err)
	}
	s.mu.Lock()
	s.finalState = terminal
	s.mu.Unlock()

	// 14. Acknowledge our own outbox row (supervisor is the finalizer in the
	// single-process proof; the daemon would do this in production).
	if err := s.store.AcknowledgeOutbox(ctx, s.cfg.RunID); err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: acknowledge outbox: %v\n", err)
	}

	// Stop serving socket.
	s.listener.Close()
	return terminal, nil
}

// cleanupGroup sends SIGTERM to surviving group members, waits the grace
// period, then SIGKILLs any remaining members. SIGKILL targets individual PIDs
// (never kill(-pgid)) to avoid self-kill (ADR-0002 §2 step 2c).
func (s *Supervisor) cleanupGroup(ctx context.Context, pgid int) error {
	members, err := s.backend.GroupMembers(pgid)
	if err != nil {
		return fmt.Errorf("group members: %w", err)
	}
	if len(members) == 0 {
		return nil
	}
	// SIGTERM each survivor individually.
	for _, pid := range members {
		_ = s.backend.SignalProcess(pid, unix.SIGTERM)
	}
	// Wait for the grace period.
	select {
	case <-time.After(s.cfg.GracePeriod):
	case <-ctx.Done():
	}
	// SIGKILL any remaining survivors individually.
	remaining, err := s.backend.GroupMembers(pgid)
	if err != nil {
		return fmt.Errorf("re-enumerate group: %w", err)
	}
	for _, pid := range remaining {
		_ = s.backend.SignalProcess(pid, unix.SIGKILL)
	}
	// Wait for them to exit (short poll).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		remaining, err = s.backend.GroupMembers(pgid)
		if err != nil || len(remaining) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// decideTerminal picks the terminal state from the exit code and cancel flag.
func (s *Supervisor) decideTerminal(exitCode int) (store.State, string) {
	s.mu.Lock()
	cancelled := s.cancelled
	s.mu.Unlock()
	if cancelled {
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
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	State     string `json:"state,omitempty"`
	WorkerPID int    `json:"worker_pid,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	PGID      int    `json:"pgid,omitempty"`
}

func (s *Supervisor) listenSocket() error {
	// Remove stale socket if present.
	_ = os.Remove(s.cfg.SocketPath)
	dir := filepath.Dir(s.cfg.SocketPath)
	_ = os.MkdirAll(dir, 0o700)
	l, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.SocketPath, err)
	}
	s.listener = l
	return nil
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
	case "ping", "status":
		s.mu.Lock()
		state := s.finalState
		if state == "" {
			if s.cancelled {
				state = store.StateCancelling
			} else {
				state = store.StateRunning
			}
		}
		resp := cmdResponse{
			OK:        true,
			State:     string(state),
			WorkerPID: s.workerPID,
			ExitCode:  s.exitCode,
			PGID:      s.id.SupervisorPGID,
		}
		s.mu.Unlock()
		data, _ := json.Marshal(resp)
		_, _ = conn.Write(data)
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
		// Daemon acknowledges reconnection (no-op for proof; the supervisor
		// stays alive until finalization).
		s.mu.Lock()
		state := s.finalState
		if state == "" {
			if s.cancelled {
				state = store.StateCancelling
			} else {
				state = store.StateRunning
			}
		}
		s.mu.Unlock()
		resp := cmdResponse{OK: true, State: string(state)}
		data, _ := json.Marshal(resp)
		_, _ = conn.Write(data)
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
	if s.reaped {
		return s.finalState
	}
	if s.cancelled {
		return store.StateCancelling
	}
	return store.StateRunning
}
