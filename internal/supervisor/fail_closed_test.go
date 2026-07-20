package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

type groupResult struct {
	members []int
	err     error
}

type failClosedBackend struct {
	mu sync.Mutex

	pgid          int
	launchPID     int
	launchErr     error
	afterLaunch   func()
	workerExitErr error
	releaseErr    error
	afterRelease  func()
	exited        chan struct{}
	groupResults  []groupResult
	groupSignals  []unix.Signal
	groupDefault  groupResult
	signalErrors  map[unix.Signal]error
	reapCode      int
	reapErr       error

	calls                     []string
	terminalObservedInCleanup bool
	terminalProbe             func() bool
}

func (b *failClosedBackend) LaunchWorker(string, []string, []string) (int, error) {
	b.record("launch")
	if b.afterLaunch != nil {
		b.afterLaunch()
	}
	return b.launchPID, b.launchErr
}

func (b *failClosedBackend) ReleaseWorker(int) error {
	b.record("release")
	if b.afterRelease != nil {
		b.afterRelease()
	}
	return b.releaseErr
}

func (b *failClosedBackend) WorkerExited(int) (<-chan struct{}, error) {
	b.record("monitor")
	if b.workerExitErr != nil {
		return nil, b.workerExitErr
	}
	if b.exited == nil {
		b.exited = make(chan struct{})
		close(b.exited)
	}
	return b.exited, nil
}

func (b *failClosedBackend) ReapWorker(int) (int, error) {
	b.record("reap")
	return b.reapCode, b.reapErr
}

func (b *failClosedBackend) GroupMembers(int) ([]int, error) {
	b.mu.Lock()
	result := b.groupDefault
	if len(b.groupResults) != 0 {
		result = b.groupResults[0]
		b.groupResults = b.groupResults[1:]
	}
	b.mu.Unlock()
	b.record("group")
	return append([]int(nil), result.members...), result.err
}

func (b *failClosedBackend) SignalGroup(_ int, sig lifecycle.Signal) error {
	b.mu.Lock()
	b.groupSignals = append(b.groupSignals, sig)
	b.mu.Unlock()
	b.record("group-signal:" + sig.String())
	return b.signalErrors[sig]
}

func (b *failClosedBackend) ProcessAlive(int) bool { return false }

func (b *failClosedBackend) record(call string) {
	b.mu.Lock()
	b.calls = append(b.calls, call)
	probe := b.terminalProbe
	b.mu.Unlock()

	if probe == nil || (call != "group" && !strings.HasPrefix(call, "group-signal:")) {
		return
	}
	if probe() {
		b.mu.Lock()
		b.terminalObservedInCleanup = true
		b.mu.Unlock()
	}
}

func (b *failClosedBackend) callLog() ([]string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...), b.terminalObservedInCleanup
}

func newFailClosedSupervisor(t *testing.T, runID string, backend *failClosedBackend) (*Supervisor, *store.Store, Config) {
	t.Helper()
	storePath := seedRunStore(t, runID)
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("open assertion store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := t.TempDir()
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("ananke-fc-%d-%s.sock", os.Getpid(), runID))
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	cfg := Config{
		StorePath:      storePath,
		RunID:          runID,
		WorkerPath:     "/bin/true",
		IdentityPath:   filepath.Join(dir, "identity.json"),
		SocketPath:     socketPath,
		TranscriptPath: filepath.Join(dir, "transcript.ndjson"),
		Token:          "fail-closed-token",
		GracePeriod:    time.Millisecond,
		Backend:        backend,
	}
	if _, err := st.DB().ExecContext(context.Background(), `UPDATE runs
		SET identity_path = ?, socket_path = ?, transcript_path = ?, token = ? WHERE id = ?`,
		cfg.IdentityPath, cfg.SocketPath, cfg.TranscriptPath, cfg.Token, runID); err != nil {
		t.Fatalf("align test run authority: %v", err)
	}
	s, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	backend.terminalProbe = func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return store.IsTerminal(s.finalState)
	}
	return s, st, cfg
}

func TestSupervisorPublishesAuthorityBeforeWorkerRelease(t *testing.T) {
	const runID = "authority-before-release"
	backend := &failClosedBackend{
		pgid:         50001,
		launchPID:    50002,
		groupDefault: groupResult{},
		reapCode:     0,
	}
	s, st, cfg := newFailClosedSupervisor(t, runID, backend)
	var releaseCheckErr error
	backend.afterRelease = func() {
		identity, err := lifecycle.ReadIdentity(cfg.IdentityPath)
		if err != nil {
			releaseCheckErr = fmt.Errorf("read identity at release: %w", err)
			return
		}
		run, err := st.GetRun(context.Background(), runID)
		if err != nil {
			releaseCheckErr = fmt.Errorf("read run at release: %w", err)
			return
		}
		if identity.WorkerPID != backend.launchPID || identity.SupervisorPGID != backend.launchPID ||
			run.WorkerPID != backend.launchPID || run.SupervisorPGID != backend.launchPID {
			releaseCheckErr = fmt.Errorf("durable worker-group authority incomplete: identity=%+v run=%+v", identity, run)
			return
		}
		response := sendCmd(t, cfg.SocketPath, cfg.Token, "status")
		if response["ok"] != true {
			releaseCheckErr = fmt.Errorf("socket authority unavailable at release: %v", response)
		}
	}

	terminal, err := s.Run(context.Background())
	if err != nil || terminal != store.StateCompleted {
		t.Fatalf("Run = terminal %q error %v", terminal, err)
	}
	if releaseCheckErr != nil {
		t.Fatal(releaseCheckErr)
	}
	calls, _ := backend.callLog()
	releaseIndex, monitorIndex := -1, -1
	for i, call := range calls {
		if call == "release" && releaseIndex == -1 {
			releaseIndex = i
		}
		if call == "monitor" && monitorIndex == -1 {
			monitorIndex = i
		}
	}
	if releaseIndex == -1 || monitorIndex == -1 || releaseIndex > monitorIndex {
		t.Fatalf("release/monitor call order = %v, want release before monitor", calls)
	}
}

func TestSupervisorPostStartFailuresCleanupBeforeReap(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, st *store.Store, cfg *Config, backend *failClosedBackend)
	}{
		{
			name: "launch watcher creation",
			setup: func(_ *testing.T, _ *store.Store, _ *Config, backend *failClosedBackend) {
				backend.launchErr = errors.New("watcher construction failed")
			},
		},
		{
			name: "identity rewrite",
			setup: func(_ *testing.T, _ *store.Store, _ *Config, _ *failClosedBackend) {
				// Installed below as a one-shot writeIdentity hook after New.
			},
		},
		{
			name: "store identity",
			setup: func(_ *testing.T, _ *store.Store, _ *Config, _ *failClosedBackend) {
				// Installed below as a one-shot setRunSupervisor hook after New.
			},
		},
		{
			name: "socket setup",
			setup: func(_ *testing.T, _ *store.Store, _ *Config, _ *failClosedBackend) {
				// Installed below as a one-shot listenAuthority hook after New.
			},
		},
		{
			name: "worker monitor",
			setup: func(_ *testing.T, _ *store.Store, _ *Config, backend *failClosedBackend) {
				backend.workerExitErr = errors.New("monitor construction failed")
			},
		},
		{
			name: "release barrier",
			setup: func(_ *testing.T, _ *store.Store, _ *Config, backend *failClosedBackend) {
				backend.releaseErr = errors.New("release barrier failed")
			},
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runID := fmt.Sprintf("post-start-%d", i)
			backend := &failClosedBackend{
				pgid:      51001,
				launchPID: 51002,
				groupResults: []groupResult{
					{members: []int{51002}},
					{},
				},
			}
			s, st, cfg := newFailClosedSupervisor(t, runID, backend)
			tc.setup(t, st, &cfg, backend)
			s.cfg = cfg
			switch tc.name {
			case "identity rewrite":
				original := s.writeIdentity
				failed := false
				s.writeIdentity = func(path string, identity lifecycle.Identity) error {
					if identity.WorkerPID > 0 && !failed {
						failed = true
						return errors.New("injected identity rewrite failure")
					}
					return original(path, identity)
				}
			case "store identity":
				original := s.setRunSupervisor
				failed := false
				s.setRunSupervisor = func(ctx context.Context, runID string, supervisorPID, pgid, workerPID int) error {
					if !failed {
						failed = true
						return errors.New("injected supervisor identity failure")
					}
					return original(ctx, runID, supervisorPID, pgid, workerPID)
				}
			case "socket setup":
				original := s.listenAuthority
				failed := false
				s.listenAuthority = func() error {
					if !failed {
						failed = true
						return errors.New("injected socket setup failure")
					}
					return original()
				}
			}

			terminal, runErr := s.Run(context.Background())
			if runErr == nil {
				t.Fatal("Run error = nil, want originating post-start failure")
			}
			if terminal != store.StateFailed {
				t.Fatalf("terminal = %q, want failed (error: %v)", terminal, runErr)
			}

			calls, terminalDuringCleanup := backend.callLog()
			groupIndex, reapIndex := -1, -1
			for i, call := range calls {
				if call == "group" && groupIndex == -1 {
					groupIndex = i
				}
				if call == "reap" && reapIndex == -1 {
					reapIndex = i
				}
			}
			if groupIndex == -1 || reapIndex == -1 || groupIndex > reapIndex {
				t.Fatalf("cleanup/reap call order = %v, want group proof before reap", calls)
			}
			if terminalDuringCleanup {
				t.Fatalf("terminal state observed before cleanup proof; calls=%v", calls)
			}
			if !s.reaped {
				t.Fatal("supervisor did not record successful exact-worker reap")
			}

			run, err := st.GetRun(context.Background(), runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.State != store.StateFailed {
				t.Fatalf("durable state = %q, want failed", run.State)
			}
			outbox, err := st.GetOutbox(context.Background(), runID)
			if err != nil {
				t.Fatalf("GetOutbox: %v", err)
			}
			if outbox.TerminalState != store.StateFailed || outbox.Acknowledged != 1 {
				t.Fatalf("outbox = %+v, want acknowledged failed", outbox)
			}
		})
	}
}

func TestSupervisorWorkerLaunchWithoutPIDFinalizesNoProcessFailure(t *testing.T) {
	const runID = "launch-no-pid"
	backend := &failClosedBackend{
		pgid:      52001,
		launchPID: 0,
		launchErr: errors.New("exec failed before child creation"),
	}
	s, st, _ := newFailClosedSupervisor(t, runID, backend)
	if _, err := st.DB().ExecContext(context.Background(),
		`UPDATE runs SET transcript_required = 1 WHERE id = ?`, runID); err != nil {
		t.Fatalf("require transcript handoff: %v", err)
	}

	terminal, err := s.Run(context.Background())
	if err == nil {
		t.Fatal("Run error = nil, want launch failure")
	}
	if terminal != store.StateFailed {
		t.Fatalf("terminal = %q, want failed", terminal)
	}
	calls, _ := backend.callLog()
	for _, call := range calls {
		if call == "group" || call == "reap" || strings.HasPrefix(call, "signal:") {
			t.Fatalf("no-process failure performed process operation %q; calls=%v", call, calls)
		}
	}
	run, err := st.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateFailed {
		t.Fatalf("durable state = %q, want failed", run.State)
	}
	outbox, err := st.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.TerminalState != store.StateFailed || outbox.Acknowledged != 1 {
		t.Fatalf("outbox = %+v, want atomically acknowledged failed", outbox)
	}
	progress, err := st.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if !progress.Required || progress.ConsumedOffset != 0 || progress.FinalSize != 0 {
		t.Fatalf("worker-start failure transcript progress = %+v, want durable empty seal", progress)
	}
}

// TestSupervisorPublishesTranscriptBeforeTrampolineLaunch proves phase-one
// authority: the transcript file and durable device/inode identity exist before
// a paused trampoline is launched. Complete process/socket authority before
// real worker execution is covered by TestSupervisorPublishesAuthorityBeforeWorkerRelease.
func TestSupervisorPublishesTranscriptBeforeTrampolineLaunch(t *testing.T) {
	const runID = "transcript-before-launch"
	backend := &failClosedBackend{
		launchErr: errors.New("stop after launch observation"),
	}
	s, st, cfg := newFailClosedSupervisor(t, runID, backend)
	s.transcriptRequired = true
	cfg.TranscriptPath = filepath.Join(filepath.Dir(cfg.TranscriptPath), "transcripts", "transcript.ndjson")
	s.cfg = cfg
	if _, err := st.DB().ExecContext(context.Background(), `UPDATE runs
		SET transcript_path = ?, transcript_required = 1 WHERE id = ?`, cfg.TranscriptPath, runID); err != nil {
		t.Fatalf("require transcript at nested path: %v", err)
	}

	var launchCheckErr error
	backend.afterLaunch = func() {
		info, err := os.Lstat(cfg.TranscriptPath)
		if err != nil {
			launchCheckErr = fmt.Errorf("lstat transcript at launch: %w", err)
			return
		}
		if !info.Mode().IsRegular() {
			launchCheckErr = fmt.Errorf("transcript mode at launch = %v, want regular", info.Mode())
			return
		}
		if got := info.Mode().Perm(); got != 0o600 {
			launchCheckErr = fmt.Errorf("transcript permissions at launch = %04o, want 0600", got)
			return
		}
		if got := info.Size(); got != 0 {
			launchCheckErr = fmt.Errorf("transcript size at launch = %d, want 0", got)
			return
		}
		fileIdentity, err := lifecycle.TranscriptIdentityFromInfo(info)
		if err != nil {
			launchCheckErr = fmt.Errorf("derive transcript identity at launch: %w", err)
			return
		}
		run, err := st.GetRun(context.Background(), runID)
		if err != nil {
			launchCheckErr = fmt.Errorf("read run at launch: %w", err)
			return
		}
		if run.TranscriptIdentity != fileIdentity {
			launchCheckErr = fmt.Errorf("transcript identity not durable before launch: file=%+v run=%+v", fileIdentity, run.TranscriptIdentity)
		}
	}

	terminal, err := s.Run(context.Background())
	if err == nil {
		t.Fatal("Run error = nil, want injected launch failure")
	}
	if terminal != store.StateFailed {
		t.Fatalf("terminal = %q, want failed", terminal)
	}
	if launchCheckErr != nil {
		t.Fatal(launchCheckErr)
	}
}

func TestAwaitTranscriptDurabilityRejectsMissingProcessTranscript(t *testing.T) {
	const runID = "missing-process-transcript"
	s, _, cfg := newFailClosedSupervisor(t, runID, &failClosedBackend{})
	t.Cleanup(func() { _ = s.Close() })
	if _, err := os.Stat(cfg.TranscriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transcript path unexpectedly exists: %v", err)
	}

	if err := s.awaitTranscriptDurability(context.Background()); err == nil {
		t.Fatal("awaitTranscriptDurability accepted a missing process transcript")
	}
}

func TestAwaitTranscriptDurabilityRejectsReplacementBeforeReturn(t *testing.T) {
	const runID = "replaced-process-transcript"
	s, st, cfg := newFailClosedSupervisor(t, runID, &failClosedBackend{})
	t.Cleanup(func() { _ = s.Close() })
	s.transcriptRequired = true
	if _, err := st.DB().ExecContext(context.Background(), `UPDATE runs SET transcript_required = 1 WHERE id = ?`, runID); err != nil {
		t.Fatalf("require transcript: %v", err)
	}
	transcriptFile, transcriptIdentity, err := createTranscript(cfg.TranscriptPath)
	if err != nil {
		t.Fatalf("createTranscript: %v", err)
	}
	s.transcriptFile = transcriptFile
	s.id.TranscriptPath = cfg.TranscriptPath
	s.id.TranscriptIdentity = transcriptIdentity
	if err := st.SetTranscriptIdentity(context.Background(), runID, transcriptIdentity); err != nil {
		t.Fatalf("SetTranscriptIdentity: %v", err)
	}
	if err := s.writeIdentity(cfg.IdentityPath, s.id); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	original := []byte("record")
	if _, err := transcriptFile.Write(original); err != nil {
		t.Fatalf("write original transcript: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- s.awaitTranscriptDurability(ctx)
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		progress, err := st.GetTranscriptProgress(context.Background(), runID)
		if err == nil && progress.FinalSize == int64(len(original)) && progress.ConsumedOffset == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	progress, err := st.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress after seal: %v", err)
	}
	if progress.FinalSize != int64(len(original)) || progress.ConsumedOffset != 0 {
		t.Fatalf("progress before replacement = %+v, want sealed and waiting", progress)
	}
	replacement := filepath.Join(filepath.Dir(cfg.TranscriptPath), "replacement.ndjson")
	if err := os.WriteFile(replacement, []byte("change"), 0o600); err != nil {
		t.Fatalf("write replacement transcript: %v", err)
	}
	if err := os.Rename(replacement, cfg.TranscriptPath); err != nil {
		t.Fatalf("replace transcript inode: %v", err)
	}
	if err := st.AdvanceTranscriptConsumed(context.Background(), runID, int64(len(original))); err != nil {
		t.Fatalf("AdvanceTranscriptConsumed: %v", err)
	}

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
			t.Fatalf("awaitTranscriptDurability error = %v, want identity mismatch", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("awaitTranscriptDurability did not return after replacement")
	}
}

func TestSupervisorTranscriptIdentityFailureUsesNoProcessCommit(t *testing.T) {
	const runID = "prelaunch-transcript-identity-failure"
	backend := &failClosedBackend{launchPID: 52602}
	s, st, _ := newFailClosedSupervisor(t, runID, backend)
	s.transcriptRequired = true
	if _, err := st.DB().ExecContext(context.Background(), `UPDATE runs SET transcript_required = 1 WHERE id = ?`, runID); err != nil {
		t.Fatalf("require transcript: %v", err)
	}
	s.setTranscriptIdentity = func(context.Context, string, store.TranscriptFileIdentity) error {
		return errors.New("injected transcript identity store failure")
	}

	terminal, err := s.Run(context.Background())
	if err == nil || terminal != store.StateFailed {
		t.Fatalf("Run = terminal %q error %v, want failed with error", terminal, err)
	}
	calls, _ := backend.callLog()
	for _, call := range calls {
		if call == "launch" {
			t.Fatalf("worker launched after transcript identity failure; calls=%v", calls)
		}
	}
	run, err := st.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateFailed || run.TranscriptIdentity != (store.TranscriptFileIdentity{}) || run.TranscriptFinalSize != 0 {
		t.Fatalf("no-process result = state %q identity %+v final %d", run.State, run.TranscriptIdentity, run.TranscriptFinalSize)
	}
}

func TestSupervisorIdentityFileFailureRetainsOwnedCleanupObligation(t *testing.T) {
	const runID = "postlaunch-identity-file-failure"
	backend := &failClosedBackend{
		launchPID: 52602,
		groupResults: []groupResult{
			{members: []int{52602}},
			{},
		},
	}
	s, st, cfg := newFailClosedSupervisor(t, runID, backend)
	cfg.CleanupRetryMin = time.Millisecond
	cfg.CleanupRetryMax = 2 * time.Millisecond
	s.cfg = cfg
	recoverIdentity := make(chan struct{})
	originalWrite := s.writeIdentity
	s.writeIdentity = func(path string, identity lifecycle.Identity) error {
		select {
		case <-recoverIdentity:
			return originalWrite(path, identity)
		default:
			return errors.New("injected identity file failure")
		}
	}
	type runResult struct {
		terminal store.State
		err      error
	}
	done := make(chan runResult, 1)
	go func() {
		terminal, err := s.Run(context.Background())
		done <- runResult{terminal: terminal, err: err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, err := st.GetRun(context.Background(), runID)
		calls, _ := backend.callLog()
		reaped := false
		for _, call := range calls {
			if call == "reap" {
				reaped = true
			}
		}
		if err == nil && run.State == store.StateCleanupRequired && reaped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned cleanup obligation not retained: run=%+v err=%v calls=%v", run, err, calls)
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := st.GetOutbox(context.Background(), runID); !errors.Is(err, store.ErrOutboxNotFound) {
		t.Fatalf("outbox before identity recovery = %v, want not found", err)
	}
	select {
	case result := <-done:
		t.Fatalf("Run returned without identity authority: terminal=%q error=%v", result.terminal, result.err)
	default:
	}
	close(recoverIdentity)
	select {
	case result := <-done:
		if result.terminal != store.StateFailed || result.err == nil {
			t.Fatalf("resolved Run = terminal %q error %v, want failed with original error", result.terminal, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not finalize after identity authority recovered")
	}
}

func TestCleanupGroupUsesAtomicGroupSignals(t *testing.T) {
	backend := &failClosedBackend{
		groupResults: []groupResult{
			{members: []int{53002}},
			{members: []int{53002}},
			{},
		},
	}
	s := &Supervisor{
		cfg: Config{
			GracePeriod:         time.Millisecond,
			CleanupTimeout:      10 * time.Millisecond,
			CleanupPollInterval: time.Millisecond,
		},
		backend: backend,
	}
	if err := s.cleanupGroup(context.Background(), 53002); err != nil {
		t.Fatalf("cleanupGroup: %v", err)
	}
	backend.mu.Lock()
	groupSignals := append([]unix.Signal(nil), backend.groupSignals...)
	backend.mu.Unlock()
	if len(groupSignals) != 2 || groupSignals[0] != unix.SIGTERM || groupSignals[1] != unix.SIGKILL {
		t.Fatalf("group signals = %v, want [terminated killed]", groupSignals)
	}
}

func TestCleanupGroupRejectsUnprovenQuiescence(t *testing.T) {
	tests := []struct {
		name          string
		groupResults  []groupResult
		groupDefault  groupResult
		signalErrors  map[unix.Signal]error
		wantSubstring string
	}{
		{
			name:          "initial enumeration",
			groupResults:  []groupResult{{err: errors.New("enumeration unavailable")}},
			wantSubstring: "group members",
		},
		{
			name:          "TERM signal",
			groupResults:  []groupResult{{members: []int{53002}}},
			signalErrors:  map[unix.Signal]error{unix.SIGTERM: errors.New("term denied")},
			wantSubstring: "SIGTERM",
		},
		{
			name: "KILL signal",
			groupResults: []groupResult{
				{members: []int{53002}},
				{members: []int{53002}},
			},
			signalErrors:  map[unix.Signal]error{unix.SIGKILL: errors.New("kill denied")},
			wantSubstring: "SIGKILL",
		},
		{
			name: "final enumeration",
			groupResults: []groupResult{
				{members: []int{53002}},
				{members: []int{53002}},
				{err: errors.New("final poll unavailable")},
			},
			wantSubstring: "final group members",
		},
		{
			name: "surviving member timeout",
			groupResults: []groupResult{
				{members: []int{53002}},
				{members: []int{53002}},
			},
			groupDefault:  groupResult{members: []int{53002}},
			wantSubstring: "still alive",
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runID := fmt.Sprintf("cleanup-proof-%d", i)
			backend := &failClosedBackend{
				pgid:         53001,
				launchPID:    53002,
				groupResults: append([]groupResult(nil), tc.groupResults...),
				groupDefault: tc.groupDefault,
				signalErrors: tc.signalErrors,
			}
			s, st, cfg := newFailClosedSupervisor(t, runID, backend)
			defer s.Close()
			cfg.CleanupTimeout = 3 * time.Millisecond
			cfg.CleanupPollInterval = 500 * time.Microsecond
			s.cfg = cfg
			if err := st.Transition(context.Background(), runID, store.StateRunning, "test cleanup"); err != nil {
				t.Fatalf("Transition running: %v", err)
			}

			err := s.cleanupGroup(context.Background(), backend.pgid)
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Fatalf("cleanupGroup error = %v, want substring %q", err, tc.wantSubstring)
			}
			run, getErr := st.GetRun(context.Background(), runID)
			if getErr != nil {
				t.Fatalf("GetRun: %v", getErr)
			}
			if store.IsTerminal(run.State) {
				t.Fatalf("cleanup failure published terminal state %q", run.State)
			}
		})
	}
}

func TestSupervisorCleanupFailureRetriesBeforeFinalizing(t *testing.T) {
	const runID = "cleanup-retry"
	backend := &failClosedBackend{
		pgid:      54001,
		launchPID: 54002,
		groupResults: []groupResult{
			{err: errors.New("transient enumeration failure")},
			{members: []int{54002}},
			{},
		},
	}
	s, st, cfg := newFailClosedSupervisor(t, runID, backend)
	cfg.CleanupRetryMin = time.Millisecond
	cfg.CleanupRetryMax = time.Millisecond
	s.cfg = cfg

	terminal, err := s.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "transient enumeration failure") {
		t.Fatalf("Run error = %v, want surfaced transient cleanup failure", err)
	}
	if terminal != store.StateFailed {
		t.Fatalf("terminal = %q, want failed after lifecycle cleanup fault", terminal)
	}
	calls, terminalDuringCleanup := backend.callLog()
	if terminalDuringCleanup {
		t.Fatalf("terminal observed before quiescence proof; calls=%v", calls)
	}
	groupCalls := 0
	for _, call := range calls {
		if call == "group" {
			groupCalls++
		}
	}
	if groupCalls < 3 {
		t.Fatalf("group calls = %d, want retry and empty proof; calls=%v", groupCalls, calls)
	}
	transitions, err := st.Transitions(context.Background(), runID)
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	cleanupIndex, failedIndex := -1, -1
	for i, transition := range transitions {
		if transition.ToState == store.StateCleanupRequired {
			cleanupIndex = i
		}
		if transition.ToState == store.StateFailed {
			failedIndex = i
		}
	}
	if cleanupIndex == -1 || failedIndex == -1 || cleanupIndex > failedIndex {
		t.Fatalf("transitions = %+v, want cleanup_required before failed", transitions)
	}
}

func TestSupervisorPermanentReapErrorCannotCompleteOrCancel(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "completion"
		if cancelled {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			runID := "reap-error-" + name
			exited := make(chan struct{})
			close(exited)
			backend := &failClosedBackend{
				pgid:      55001,
				launchPID: 55002,
				exited:    exited,
				groupResults: []groupResult{
					{},
				},
				reapErr: errors.New("wait status unavailable"),
			}
			s, st, _ := newFailClosedSupervisor(t, runID, backend)
			s.cancelled = cancelled

			terminal, err := s.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "wait status unavailable") {
				t.Fatalf("Run error = %v, want surfaced reap failure", err)
			}
			if terminal != store.StateFailed {
				t.Fatalf("terminal = %q, want failed", terminal)
			}
			if s.reaped {
				t.Fatal("reaped = true after permanent wait failure")
			}
			transitions, err := st.Transitions(context.Background(), runID)
			if err != nil {
				t.Fatalf("Transitions: %v", err)
			}
			reason := transitions[len(transitions)-1].Reason
			if !strings.Contains(reason, "wait status unavailable") {
				t.Fatalf("terminal reason = %q, want explicit reap failure", reason)
			}
		})
	}
}

func TestSupervisorPermanentCleanupFailureKeepsNonterminalAnchor(t *testing.T) {
	const runID = "cleanup-permanent"
	backend := &failClosedBackend{
		pgid:         56001,
		launchPID:    56002,
		groupDefault: groupResult{err: errors.New("enumeration remains unavailable")},
	}
	s, st, cfg := newFailClosedSupervisor(t, runID, backend)
	cfg.CleanupRetryMin = time.Millisecond
	cfg.CleanupRetryMax = 2 * time.Millisecond
	s.cfg = cfg

	type runResult struct {
		terminal store.State
		err      error
	}
	done := make(chan runResult, 1)
	go func() {
		terminal, err := s.Run(context.Background())
		done <- runResult{terminal: terminal, err: err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, err := st.GetRun(context.Background(), runID)
		if err == nil && run.State == store.StateCleanupRequired {
			calls, _ := backend.callLog()
			groupCalls := 0
			for _, call := range calls {
				if call == "group" {
					groupCalls++
				}
			}
			if groupCalls >= 2 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup failure was not retained and retried")
		}
		time.Sleep(time.Millisecond)
	}

	select {
	case result := <-done:
		t.Fatalf("Run returned without cleanup proof: terminal=%q error=%v", result.terminal, result.err)
	default:
	}
	if s.reaped {
		t.Fatal("worker reaped while group enumeration remained unavailable")
	}
	if _, err := st.GetOutbox(context.Background(), runID); err == nil {
		t.Fatal("terminal outbox published while cleanup proof was unavailable")
	}
	calls, terminalDuringCleanup := backend.callLog()
	for _, call := range calls {
		if call == "reap" {
			t.Fatalf("reap occurred before cleanup proof; calls=%v", calls)
		}
	}
	if terminalDuringCleanup {
		t.Fatalf("terminal state observed during failed cleanup; calls=%v", calls)
	}

	// Resolve the fake enumeration failure so the goroutine is not leaked. The
	// retained first failure still forces a durable failed terminal outcome.
	backend.mu.Lock()
	backend.groupDefault = groupResult{}
	backend.mu.Unlock()
	select {
	case result := <-done:
		if result.terminal != store.StateFailed || result.err == nil ||
			!strings.Contains(result.err.Error(), "enumeration remains unavailable") {
			t.Fatalf("resolved Run = terminal %q error %v, want surfaced durable failure", result.terminal, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not finalize after empty-group proof became available")
	}
}
