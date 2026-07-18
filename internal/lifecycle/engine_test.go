package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

// --- Binary build helper ---

var (
	binOnce     sync.Once
	supBin      string
	fwBin       string
	binBuildErr error
)

// ensureBinaries builds the ananke-supervisor and ananke-fakeworker binaries
// once for all engine tests. Returns their paths.
func ensureBinaries(t *testing.T) (string, string) {
	t.Helper()
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ananke-bins-*")
		if err != nil {
			binBuildErr = err
			return
		}
		supBin = filepath.Join(dir, "ananke-supervisor")
		fwBin = filepath.Join(dir, "ananke-fakeworker")
		wd, _ := os.Getwd()
		root := filepath.Join(wd, "..", "..")
		for _, pair := range [][2]string{{supBin, "./cmd/ananke-supervisor"}, {fwBin, "./cmd/ananke-fakeworker"}} {
			cmd := exec.Command("go", "build", "-o", pair[0], pair[1])
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				binBuildErr = fmt.Errorf("build %s: %v\n%s", pair[1], err, out)
				return
			}
		}
	})
	if binBuildErr != nil {
		t.Fatalf("ensureBinaries: %v", binBuildErr)
	}
	return supBin, fwBin
}

// --- Engine test environment ---

type engineEnv struct {
	eng        *Engine
	store      *store.Store
	storePath  string
	socketPath string
	dataDir    string
	token      string
	cancel     context.CancelFunc
	// supPIDs tracks supervisor PIDs that need cleanup.
	supPIDs   []int
	supPIDsMu sync.Mutex
}

// newEngineEnv creates a fresh engine with a test store, starts it, and
// registers cleanup. Returns the env.
func newEngineEnv(t *testing.T) *engineEnv {
	t.Helper()
	sup, _ := ensureBinaries(t)
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	// Use a short socket path to stay within the 104-byte Unix socket limit.
	socketPath := filepath.Join("/tmp", "ananke-eng-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	dataDir := filepath.Join("/tmp", "ananke-data-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	token := "test-engine-token"
	eng, err := NewEngine(EngineConfig{
		StorePath:     storePath,
		SocketPath:    socketPath,
		SupervisorBin: sup,
		DataDir:       dataDir,
		Token:         token,
		TickInterval:  100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- eng.Run(ctx) }()
	// Wait for the API socket to be ready.
	waitForEngineSocket(t, socketPath, 5*time.Second)
	env := &engineEnv{
		eng:        eng,
		store:      eng.Store(),
		storePath:  storePath,
		socketPath: socketPath,
		dataDir:    dataDir,
		token:      token,
		cancel:     cancel,
	}
	t.Cleanup(func() {
		// Kill any tracked supervisor PIDs.
		env.supPIDsMu.Lock()
		for _, pid := range env.supPIDs {
			_ = unix.Kill(pid, unix.SIGKILL)
			var ws unix.WaitStatus
			_, _ = unix.Wait4(pid, &ws, 0, nil)
		}
		env.supPIDsMu.Unlock()
		cancel()
		_ = eng.Close()
		_ = os.Remove(socketPath)
		_ = os.RemoveAll(dataDir)
	})
	return env
}

// waitForEngineSocket polls until the daemon's API socket is connectable.
func waitForEngineSocket(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("engine socket %s not ready within %v", path, timeout)
}

// engineAPI sends a JSON command to the engine's API socket and returns the
// response.
func engineAPI(t *testing.T, env *engineEnv, cmd string, extra map[string]any) map[string]any {
	t.Helper()
	conn, err := net.Dial("unix", env.socketPath)
	if err != nil {
		t.Fatalf("dial engine: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	req := map[string]any{"cmd": cmd, "token": env.token}
	for k, v := range extra {
		req[k] = v
	}
	data, _ := json.Marshal(req)
	_, _ = conn.Write(data)
	dec := json.NewDecoder(conn)
	var resp map[string]any
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode engine response: %v", err)
	}
	return resp
}

// engineAPIRaw sends a raw JSON request to the engine and returns the response.
func engineAPIRaw(t *testing.T, env *engineEnv, req map[string]any) map[string]any {
	t.Helper()
	conn, err := net.Dial("unix", env.socketPath)
	if err != nil {
		t.Fatalf("dial engine: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	req["token"] = env.token
	data, _ := json.Marshal(req)
	_, _ = conn.Write(data)
	dec := json.NewDecoder(conn)
	var resp map[string]any
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode engine response: %v", err)
	}
	return resp
}

// waitRunState polls the store until the run reaches the target state.
func waitRunState(t *testing.T, st *store.Store, runID string, target store.State, timeout time.Duration) store.Run {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r, err := st.GetRun(context.Background(), runID)
		if err == nil && r.State == target {
			return r
		}
		time.Sleep(50 * time.Millisecond)
	}
	r, _ := st.GetRun(context.Background(), runID)
	t.Fatalf("run %s did not reach %s within %v (state=%s)", runID, target, timeout, r.State)
	return r
}

// setupProject creates a project+workstream and returns their IDs.
func setupProject(t *testing.T, env *engineEnv) (projectID, workstreamID string) {
	t.Helper()
	projectID = "proj-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	workstreamID = "ws-" + projectID
	resp := engineAPI(t, env, "create-project", map[string]any{"id": projectID, "name": "test", "root": "/tmp"})
	if resp["ok"] != true {
		t.Fatalf("create-project: %v", resp)
	}
	resp = engineAPI(t, env, "create-workstream", map[string]any{"id": workstreamID, "project_id": projectID, "name": "main"})
	if resp["ok"] != true {
		t.Fatalf("create-workstream: %v", resp)
	}
	return
}

// launchRun creates a project+workstream, launches a run, and tracks the
// supervisor PID for cleanup. Returns the run ID.
func launchRun(t *testing.T, env *engineEnv, fwEnv map[string]string) string {
	t.Helper()
	projectID, workstreamID := setupProject(t, env)
	runID := "run-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, fw := ensureBinaries(t)
	var workerEnv []string
	for k, v := range fwEnv {
		workerEnv = append(workerEnv, k+"="+v)
	}
	resp := engineAPIRaw(t, env, map[string]any{
		"cmd":           "launch-run",
		"id":            runID,
		"project_id":    projectID,
		"workstream_id": workstreamID,
		"worker_path":   fw,
		"worker_env":    workerEnv,
	})
	if resp["ok"] != true {
		t.Fatalf("launch-run: %v", resp)
	}
	// Track the supervisor PID for cleanup once it's recorded.
	go func() {
		for i := 0; i < 100; i++ {
			r, err := env.store.GetRun(context.Background(), runID)
			if err == nil && r.SupervisorPID > 0 {
				env.supPIDsMu.Lock()
				env.supPIDs = append(env.supPIDs, r.SupervisorPID)
				env.supPIDsMu.Unlock()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return runID
}

// --- Tests ---

// TestEnginePing verifies the ping command returns ok.
func TestEnginePing(t *testing.T) {
	env := newEngineEnv(t)
	resp := engineAPI(t, env, "ping", nil)
	if resp["ok"] != true {
		t.Fatalf("ping: %v", resp)
	}
}

// TestEngineCreateProjectWorkstream verifies project+workstream creation.
func TestEngineCreateProjectWorkstream(t *testing.T) {
	env := newEngineEnv(t)
	pid, wsid := setupProject(t, env)
	// Verify the project exists in the store.
	runs, err := env.store.ListRunsByProject(context.Background(), pid)
	if err != nil {
		t.Fatalf("ListRunsByProject: %v", err)
	}
	_ = wsid
	if len(runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(runs))
	}
}

// TestEngineLaunchRunEventsStream verifies launching a run starts the
// supervisor and events are streamed into the store.
func TestEngineLaunchRunEventsStream(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "3",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "50",
	})
	// Wait for the run to complete.
	r := waitRunState(t, env.store, runID, store.StateCompleted, 30*time.Second)
	if r.SupervisorPID == 0 {
		t.Errorf("supervisor pid not recorded")
	}
	if r.WorkerPID == 0 {
		t.Errorf("worker pid not recorded")
	}
	// Verify events were streamed into the store.
	events, err := env.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("events = %d, want 3", len(events))
	}
}

// TestEngineGetRunState verifies the get-run command returns the correct state.
func TestEngineGetRunState(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "1",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "2000",
	})
	// Wait for the supervisor to transition the run to running.
	waitRunState(t, env.store, runID, store.StateRunning, 10*time.Second)
	// While the worker is running, the state should be running.
	resp := engineAPI(t, env, "get-run", map[string]any{"id": runID})
	if resp["ok"] != true {
		t.Fatalf("get-run: %v", resp)
	}
	run := resp["run"].(map[string]any)
	if run["state"] != "running" {
		t.Errorf("state = %v, want running", run["state"])
	}
	// Wait for completion.
	waitRunState(t, env.store, runID, store.StateCompleted, 30*time.Second)
	// Get the final state.
	resp = engineAPI(t, env, "get-run", map[string]any{"id": runID})
	if resp["ok"] != true {
		t.Fatalf("get-run (final): %v", resp)
	}
	run = resp["run"].(map[string]any)
	if run["state"] != "completed" {
		t.Errorf("final state = %v, want completed", run["state"])
	}
}

// TestEngineListEvents verifies events can be listed by sequence.
func TestEngineListEvents(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "5",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "20",
	})
	waitRunState(t, env.store, runID, store.StateCompleted, 30*time.Second)
	// List all events.
	resp := engineAPI(t, env, "list-events", map[string]any{"id": runID, "after_seq": 0})
	if resp["ok"] != true {
		t.Fatalf("list-events: %v", resp)
	}
	events := resp["events"].([]any)
	if len(events) != 5 {
		t.Fatalf("events = %d, want 5", len(events))
	}
	// Verify sequence ordering.
	for i, ev := range events {
		m := ev.(map[string]any)
		seq := int64(m["seq"].(float64))
		if seq != int64(i+1) {
			t.Errorf("event[%d].seq = %d, want %d", i, seq, i+1)
		}
	}
	// List events after seq 2 — should get 3.
	resp = engineAPI(t, env, "list-events", map[string]any{"id": runID, "after_seq": 2})
	if resp["ok"] != true {
		t.Fatalf("list-events after: %v", resp)
	}
	events = resp["events"].([]any)
	if len(events) != 3 {
		t.Errorf("events after seq 2 = %d, want 3", len(events))
	}
}

// TestEngineCancelRun verifies a cancel command returns accepted immediately
// and the run eventually reaches cancelled.
func TestEngineCancelRun(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "0",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "5000",
	})
	// Wait for the run to be running.
	waitRunState(t, env.store, runID, store.StateRunning, 10*time.Second)
	// Cancel the run.
	start := time.Now()
	resp := engineAPI(t, env, "cancel-run", map[string]any{"id": runID})
	elapsed := time.Since(start)
	if resp["ok"] != true {
		t.Fatalf("cancel-run: %v", resp)
	}
	if resp["accepted"] != true {
		t.Errorf("accepted = %v, want true", resp["accepted"])
	}
	// The cancel should return quickly (asynchronous).
	if elapsed > 2*time.Second {
		t.Errorf("cancel took %v, should be < 2s", elapsed)
	}
	// The run should eventually reach cancelled.
	waitRunState(t, env.store, runID, store.StateCancelled, 30*time.Second)
}

// TestEngineDaemonRestartRecover verifies that after the daemon is killed and
// restarted, a running run's supervisor is detected and the run is recovered.
func TestEngineDaemonRestartRecover(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "0",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "10000",
	})
	// Wait for the run to be running.
	r := waitRunState(t, env.store, runID, store.StateRunning, 10*time.Second)
	supPID := r.SupervisorPID
	if supPID == 0 {
		t.Fatalf("supervisor pid not recorded")
	}

	// Kill the daemon (cancel context + close engine, but DON'T kill the
	// supervisor).
	env.cancel()
	_ = env.eng.Close()
	// Verify the supervisor is still alive.
	if !processAlive(supPID) {
		t.Fatalf("supervisor %d died after daemon kill", supPID)
	}

	// Restart the engine with the same store.
	sup, _ := ensureBinaries(t)
	eng2, err := NewEngine(EngineConfig{
		StorePath:     env.storePath,
		SocketPath:    env.socketPath,
		SupervisorBin: sup,
		DataDir:       env.dataDir,
		Token:         env.token,
		TickInterval:  100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine (restart): %v", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { _ = eng2.Run(ctx2) }()
	waitForEngineSocket(t, env.socketPath, 5*time.Second)
	t.Cleanup(func() {
		cancel2()
		_ = eng2.Close()
	})

	// The recovery loop should detect the supervisor is alive.
	// Verify the run is still running (or progressing).
	r2, err := eng2.Store().GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after restart: %v", err)
	}
	if store.IsTerminal(r2.State) {
		t.Errorf("run is terminal (%s) after restart, expected nonterminal", r2.State)
	}

	// Cancel the run via the restarted daemon so it can clean up.
	resp := engineAPI(t, &engineEnv{
		eng:        eng2,
		store:      eng2.Store(),
		socketPath: env.socketPath,
		token:      env.token,
	}, "cancel-run", map[string]any{"id": runID})
	if resp["ok"] != true {
		t.Errorf("cancel-run after restart: %v", resp)
	}
	waitRunState(t, eng2.Store(), runID, store.StateCancelled, 30*time.Second)
}

// TestEngineTranscriptCorruption verifies that corrupting a transcript while
// the worker is alive transitions the run to cleanup_required (not failed).
func TestEngineTranscriptCorruption(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "1",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "5000",
	})
	// Wait for the run to be running.
	waitRunState(t, env.store, runID, store.StateRunning, 10*time.Second)

	// Find the transcript file.
	r, _ := env.store.GetRun(context.Background(), runID)
	transcriptPath := r.TranscriptPath
	if transcriptPath == "" {
		t.Fatalf("transcript path not set")
	}

	// Corrupt the transcript by appending invalid JSON.
	_ = os.WriteFile(transcriptPath, []byte("THIS IS NOT JSON\n"), 0o644)

	// The engine's recovery loop should detect the corruption and transition
	// to cleanup_required (NOT failed).
	r = waitRunState(t, env.store, runID, store.StateCleanupRequired, 15*time.Second)
	if r.State == store.StateFailed {
		t.Errorf("run went to failed, should be cleanup_required while group alive")
	}
}

// TestEngineSupervisorSIGKILL verifies that SIGKILLing the supervisor
// transitions the run to recovery_unknown.
func TestEngineSupervisorSIGKILL(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "0",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "10000",
	})
	// Wait for the run to be running.
	r := waitRunState(t, env.store, runID, store.StateRunning, 10*time.Second)
	supPID := r.SupervisorPID
	if supPID == 0 {
		t.Fatalf("supervisor pid not recorded")
	}

	// SIGKILL the supervisor.
	_ = unix.Kill(supPID, unix.SIGKILL)
	var ws unix.WaitStatus
	_, _ = unix.Wait4(supPID, &ws, 0, nil)

	// The engine's recovery loop should detect the dead supervisor and
	// transition to recovery_unknown.
	r = waitRunState(t, env.store, runID, store.StateRecoveryUnknown, 15*time.Second)
	_ = r
}

// TestEngineTerminalCommitCrashBeforeFinalize verifies that a terminal commit
// with a pending (unacknowledged) outbox row is reconciled on daemon restart.
func TestEngineTerminalCommitCrashBeforeFinalize(t *testing.T) {
	env := newEngineEnv(t)
	projectID, workstreamID := setupProject(t, env)
	runID := "run-outbox-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	// Manually create a run in the store and commit it to a terminal state
	// with an outbox row, but DON'T acknowledge the outbox.
	spec := store.RunSpec{
		WorkerPath:     "/bin/true",
		TranscriptPath: filepath.Join(env.dataDir, runID, "transcript.ndjson"),
		SocketPath:     filepath.Join(env.dataDir, runID, "supervisor.sock"),
		Token:          "test",
		IdentityPath:   filepath.Join(env.dataDir, runID, "identity.json"),
	}
	if err := env.store.CreateRun(context.Background(), runID, projectID, workstreamID, spec); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := env.store.Transition(context.Background(), runID, store.StateRunning, "manual"); err != nil {
		t.Fatalf("Transition to running: %v", err)
	}
	// Commit terminal with outbox row (simulates supervisor crash after commit
	// but before acknowledge).
	if err := env.store.CommitTerminal(context.Background(), runID, store.StateCompleted, "test terminal", store.OutboxRow{
		RunID:         runID,
		TerminalState: store.StateCompleted,
		SupervisorPID: 999999, // nonexistent PID → will be detected as dead
	}); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}

	// Verify the outbox row is pending.
	outbox, err := env.store.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.Acknowledged != 0 {
		t.Fatalf("outbox already acknowledged before restart")
	}

	// Kill the current daemon and restart.
	env.cancel()
	_ = env.eng.Close()

	// Open the store directly to verify the pending state survived.
	st, err := store.Open(env.storePath)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	outbox2, err := st.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox after reopen: %v", err)
	}
	if outbox2.Acknowledged != 0 {
		t.Fatalf("outbox acknowledged before engine restart")
	}
	_ = st.Close()

	// Restart the engine — Recover() should process the pending outbox row.
	sup, _ := ensureBinaries(t)
	eng2, err := NewEngine(EngineConfig{
		StorePath:     env.storePath,
		SocketPath:    env.socketPath,
		SupervisorBin: sup,
		DataDir:       env.dataDir,
		Token:         env.token,
		TickInterval:  100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine (restart): %v", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { _ = eng2.Run(ctx2) }()
	waitForEngineSocket(t, env.socketPath, 5*time.Second)
	t.Cleanup(func() {
		cancel2()
		_ = eng2.Close()
	})

	// The recovery loop should have reconciled the pending outbox row.
	// Since the supervisor PID (999999) is dead, it should be abandoned.
	st2 := eng2.Store()
	outbox3, err := st2.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox after restart: %v", err)
	}
	if outbox3.Acknowledged == 0 {
		t.Errorf("outbox still pending after restart; expected acknowledged or abandoned")
	}
}

// processAlive reports whether a PID is alive.
func processAlive(pid int) bool {
	return unix.Kill(pid, 0) == nil
}
