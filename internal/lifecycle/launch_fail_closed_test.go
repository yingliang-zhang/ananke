package lifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func newLaunchFailureEngine(t *testing.T, supervisorBin string) *Engine {
	t.Helper()
	dir := t.TempDir()
	engine, err := NewEngine(EngineConfig{
		StorePath:     filepath.Join(dir, "store.sqlite"),
		SocketPath:    filepath.Join(dir, "engine.sock"),
		SupervisorBin: supervisorBin,
		DataDir:       filepath.Join(dir, "runs"),
		Token:         "launch-failure-token",
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	ctx := context.Background()
	if err := engine.store.CreateProject(ctx, "launch-project", "project", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := engine.store.CreateWorkstream(ctx, "launch-workstream", "launch-project", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	return engine
}

type entropyFailureReader struct{}

func (entropyFailureReader) Read([]byte) (int, error) {
	return 0, errors.New("injected entropy failure")
}

func TestEngineRunTokenEntropyFailureCreatesNoRun(t *testing.T) {
	engine := newLaunchFailureEngine(t, filepath.Join(t.TempDir(), "unused-supervisor"))
	engine.tokenReader = entropyFailureReader{}
	response := engine.handleLaunchRun(context.Background(), &apiRequest{
		ID:           "entropy-failure",
		ProjectID:    "launch-project",
		WorkstreamID: "launch-workstream",
		WorkerPath:   "/bin/true",
	})
	if response.OK || !strings.Contains(response.Error, "generate run auth token") {
		t.Fatalf("response = %+v, want entropy failure", response)
	}
	if _, err := engine.store.GetRun(context.Background(), "entropy-failure"); !errors.Is(err, store.ErrRunNotFound) {
		t.Fatalf("GetRun error = %v, want ErrRunNotFound", err)
	}
}

func TestEngineSupervisorStartFailureFinalizesNoProcessRun(t *testing.T) {
	engine := newLaunchFailureEngine(t, filepath.Join(t.TempDir(), "missing-supervisor"))
	ctx := context.Background()
	response := engine.handleLaunchRun(ctx, &apiRequest{
		ID:           "supervisor-start-failure",
		ProjectID:    "launch-project",
		WorkstreamID: "launch-workstream",
		WorkerPath:   "/bin/true",
	})
	if response.OK || !strings.Contains(response.Error, "launch supervisor") {
		t.Fatalf("response = %+v, want surfaced supervisor start failure", response)
	}
	run, err := engine.store.GetRun(ctx, "supervisor-start-failure")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateFailed {
		t.Fatalf("state = %q, want failed", run.State)
	}
	outbox, err := engine.store.GetOutbox(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.TerminalState != store.StateFailed || outbox.Acknowledged != 1 {
		t.Fatalf("outbox = %+v, want atomically acknowledged failed", outbox)
	}
	progress, err := engine.store.GetTranscriptProgress(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if !progress.Required || progress.ConsumedOffset != 0 || progress.FinalSize != 0 {
		t.Fatalf("supervisor-start failure transcript progress = %+v, want durable empty seal", progress)
	}
}

func TestEngineSupervisorStartFailureSurfacesPersistenceFailure(t *testing.T) {
	engine := newLaunchFailureEngine(t, filepath.Join(t.TempDir(), "missing-supervisor"))
	ctx := context.Background()
	if _, err := engine.store.DB().ExecContext(ctx, `
		CREATE TRIGGER fail_no_process_outbox
		BEFORE INSERT ON finalization_outbox
		WHEN NEW.run_id = 'supervisor-start-persist-failure'
		BEGIN SELECT RAISE(FAIL, 'injected no-process persistence failure'); END`); err != nil {
		t.Fatalf("install outbox trigger: %v", err)
	}

	response := engine.handleLaunchRun(ctx, &apiRequest{
		ID:           "supervisor-start-persist-failure",
		ProjectID:    "launch-project",
		WorkstreamID: "launch-workstream",
		WorkerPath:   "/bin/true",
	})
	if response.OK || !strings.Contains(response.Error, "injected no-process persistence failure") {
		t.Fatalf("response = %+v, want surfaced persistence failure", response)
	}
	run, err := engine.store.GetRun(ctx, "supervisor-start-persist-failure")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateCreated {
		t.Fatalf("state = %q, want created after atomic persistence rollback", run.State)
	}
}

func TestEngineSupervisorWatcherFailureRetainsOwnershipAndRetries(t *testing.T) {
	env := newEngineEnvWithTick(t, 10*time.Millisecond)
	var mu sync.Mutex
	watchCalls := 0
	env.eng.mu.Lock()
	env.eng.newExitWatcher = func(pid int) (*processExitWatcher, error) {
		mu.Lock()
		watchCalls++
		call := watchCalls
		mu.Unlock()
		if call == 1 {
			return nil, errors.New("injected supervisor watcher failure")
		}
		return newProcessExitWatcher(pid)
	}
	env.eng.mu.Unlock()

	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "0",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_EXIT_DELAY_MS": "10000",
	})
	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	if !processAlive(run.SupervisorPID) || !processAlive(run.WorkerPID) {
		t.Fatalf("watcher failure abandoned launched processes: supervisor_alive=%v worker_alive=%v",
			processAlive(run.SupervisorPID), processAlive(run.WorkerPID))
	}

	waitUntil(t, "supervisor watcher retry", 5*time.Second, func() bool {
		mu.Lock()
		calls := watchCalls
		mu.Unlock()
		env.eng.mu.Lock()
		handle := env.eng.active[runID]
		hasWatcher := handle != nil && handle.hasWatcher()
		env.eng.mu.Unlock()
		return calls >= 2 && hasWatcher
	})

	response := engineAPI(t, env, "cancel-run", map[string]any{"id": runID})
	if response["ok"] != true {
		t.Fatalf("cancel-run: %v", response)
	}
	waitRunState(t, env.store, runID, store.StateCancelled, 30*time.Second)
	waitUntil(t, "retried-watcher supervisor reap", 5*time.Second, func() bool {
		env.eng.mu.Lock()
		_, tracked := env.eng.active[runID]
		env.eng.mu.Unlock()
		return !tracked
	})
	if processAlive(run.WorkerPID) {
		t.Fatalf("worker %d survived watcher-recovery finalization", run.WorkerPID)
	}
}
