package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	return newEngineEnvWithTick(t, 100*time.Millisecond)
}

func newEngineEnvWithTick(t *testing.T, tickInterval time.Duration) *engineEnv {
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
		TickInterval:  tickInterval,
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
		// Mutation builds can deliberately stop before production cleanup. The
		// test store is the authority for every owned worker PGID, so remove
		// those groups before killing their outside-group supervisors.
		rows, err := env.store.DB().QueryContext(context.Background(),
			`SELECT DISTINCT supervisor_pgid FROM runs WHERE supervisor_pgid > 0`)
		var pgids []int
		if err == nil {
			for rows.Next() {
				var pgid int
				if rows.Scan(&pgid) == nil && pgid > 0 {
					pgids = append(pgids, pgid)
				}
			}
			_ = rows.Close()
		}
		for _, pgid := range pgids {
			_ = unix.Kill(-pgid, unix.SIGKILL)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			quiescent := true
			for _, pgid := range pgids {
				members, groupErr := eng.backend.GroupMembers(pgid)
				if groupErr == nil && len(members) != 0 {
					quiescent = false
					break
				}
			}
			if quiescent {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
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

// waitRunRunningIdentity polls until running and all durable process IDs are published.
func waitRunRunningIdentity(t *testing.T, st *store.Store, runID string, timeout time.Duration) store.Run {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var r store.Run
	for time.Now().Before(deadline) {
		current, err := st.GetRun(context.Background(), runID)
		if err == nil {
			r = current
			if r.State == store.StateRunning && r.SupervisorPID > 0 && r.SupervisorPGID > 0 && r.WorkerPID > 0 &&
				(!r.TranscriptRequired || r.TranscriptIdentity.Validate() == nil) {
				return r
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach running with durable identity within %v (state=%s supervisor_pid=%d supervisor_pgid=%d worker_pid=%d)",
		runID, timeout, r.State, r.SupervisorPID, r.SupervisorPGID, r.WorkerPID)
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

func TestEngineListRunsByProject(t *testing.T) {
	env := newEngineEnv(t)
	projectID, workstreamID := setupProject(t, env)
	secondaryWorkstreamID := "secondary-" + projectID
	if response := engineAPI(t, env, "create-workstream", map[string]any{
		"id":         secondaryWorkstreamID,
		"project_id": projectID,
		"name":       "secondary",
	}); response["ok"] != true {
		t.Fatalf("create secondary workstream: %v", response)
	}
	for _, testRun := range []struct {
		id           string
		workstreamID string
	}{
		{id: "first-" + projectID, workstreamID: workstreamID},
		{id: "second-" + projectID, workstreamID: secondaryWorkstreamID},
	} {
		if err := env.store.CreateRun(context.Background(), testRun.id, projectID, testRun.workstreamID, store.RunSpec{WorkerPath: "/bin/true"}); err != nil {
			t.Fatalf("create run %s: %v", testRun.id, err)
		}
	}
	otherProjectID, otherWorkstreamID := setupProject(t, env)
	if err := env.store.CreateRun(context.Background(), "other-"+otherProjectID, otherProjectID, otherWorkstreamID, store.RunSpec{WorkerPath: "/bin/true"}); err != nil {
		t.Fatalf("create unrelated run: %v", err)
	}

	expected, err := env.store.ListRunsByProject(context.Background(), projectID)
	if err != nil {
		t.Fatalf("ListRunsByProject: %v", err)
	}
	response := engineAPI(t, env, "list-runs", map[string]any{"project_id": projectID})
	if response["ok"] != true {
		t.Fatalf("list-runs: %v", response)
	}
	runs, ok := response["runs"].([]any)
	if !ok || len(runs) != len(expected) {
		t.Fatalf("list-runs = %#v, want %d canonical runs", response["runs"], len(expected))
	}
	for index, rawRun := range runs {
		run, ok := rawRun.(map[string]any)
		if !ok {
			t.Fatalf("run %d = %#v, want object", index, rawRun)
		}
		if run["id"] != expected[index].ID || run["state"] != string(expected[index].State) {
			t.Errorf("run %d = %#v, want id=%q state=%q", index, run, expected[index].ID, expected[index].State)
		}
		if _, hasSecret := run["token"]; hasSecret {
			t.Errorf("run %d exposes token: %#v", index, run)
		}
	}

	response = engineAPI(t, env, "list-runs", map[string]any{"project_id": projectID, "workstream_id": secondaryWorkstreamID})
	if response["ok"] != true {
		t.Fatalf("list-runs workstream filter: %v", response)
	}
	filteredRuns, ok := response["runs"].([]any)
	if !ok || len(filteredRuns) != 1 || filteredRuns[0].(map[string]any)["id"] != expected[1].ID {
		t.Fatalf("workstream-filtered runs = %#v, want only %q", response["runs"], expected[1].ID)
	}

	response = engineAPI(t, env, "list-runs", nil)
	if response["ok"] != false || response["error"] != "project_id required" {
		t.Fatalf("missing project list-runs = %v, want project_id rejection", response)
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

func TestEngineListEventsPreservesFakeworkerPayload(t *testing.T) {
	env := newEngineEnvWithTick(t, 20*time.Millisecond)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS": "2",
		"ANANKE_FW_EXIT":   "0",
	})
	waitRunState(t, env.store, runID, store.StateCompleted, 15*time.Second)

	response := engineAPI(t, env, "list-events", map[string]any{"id": runID, "after_seq": 0})
	if response["ok"] != true {
		t.Fatalf("list-events: %v", response)
	}
	events, ok := response["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("events = %#v, want two canonical fakeworker events", response["events"])
	}
	for i, rawEvent := range events {
		event := rawEvent.(map[string]any)
		storeSequence := i + 1
		if event["seq"] != float64(storeSequence) {
			t.Fatalf("event %d API seq = %#v, want store sequence %d", storeSequence, event["seq"], storeSequence)
		}
		payload, ok := event["payload"].(map[string]any)
		if !ok {
			t.Fatalf("event %d payload = %#v, want non-null object", storeSequence, event["payload"])
		}
		if payload["source_seq"] != float64(storeSequence) {
			t.Errorf("event %d source_seq = %#v, want %d", storeSequence, payload["source_seq"], storeSequence)
		}
		if payload["text"] != fmt.Sprintf("event %d", storeSequence) {
			t.Errorf("event %d text = %#v, want event %d", storeSequence, payload["text"], storeSequence)
		}
		timestamp, ok := payload["timestamp"].(string)
		if !ok {
			t.Errorf("event %d timestamp = %#v, want RFC3339 string", storeSequence, payload["timestamp"])
		} else if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			t.Errorf("event %d timestamp %q: %v", storeSequence, timestamp, err)
		}
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
	r := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
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
	eng2.mu.Lock()
	_, locallyOwned := eng2.active[runID]
	eng2.mu.Unlock()
	if locallyOwned {
		t.Fatal("restarted engine pretended recovered supervisor was its child")
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

const transcriptPersistenceRunID = "transcript-persistence-run"

func persistTranscriptIdentity(t *testing.T, st *store.Store, runID, path string) store.TranscriptFileIdentity {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat transcript identity fixture: %v", err)
	}
	identity, err := TranscriptIdentityFromInfo(info)
	if err != nil {
		t.Fatalf("derive transcript identity fixture: %v", err)
	}
	if err := st.SetTranscriptIdentity(context.Background(), runID, identity); err != nil {
		t.Fatalf("SetTranscriptIdentity: %v", err)
	}
	return identity
}

func newTranscriptPersistenceEngine(t *testing.T, transcript []byte) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.ndjson")
	if err := os.WriteFile(transcriptPath, transcript, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, "store.sqlite"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	ctx := context.Background()
	if err := st.CreateProject(ctx, "transcript-project", "transcript", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.CreateWorkstream(ctx, "transcript-workstream", "transcript-project", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	if err := st.CreateRun(ctx, transcriptPersistenceRunID, "transcript-project", "transcript-workstream", store.RunSpec{
		WorkerPath:         "/bin/true",
		TranscriptPath:     transcriptPath,
		TranscriptRequired: true,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	persistTranscriptIdentity(t, st, transcriptPersistenceRunID, transcriptPath)
	if err := st.Transition(ctx, transcriptPersistenceRunID, store.StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	engine := &Engine{
		store:            st,
		active:           make(map[string]*runHandle),
		tails:            make(map[string]*transcriptTail),
		cleanupRequested: make(map[string]struct{}),
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine, transcriptPath
}

func installSQLiteFailureTrigger(t *testing.T, engine *Engine, name, definition string) func() {
	t.Helper()
	if _, err := engine.store.DB().ExecContext(context.Background(), definition); err != nil {
		t.Fatalf("create trigger %s: %v", name, err)
	}
	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		if _, err := engine.store.DB().ExecContext(context.Background(), `DROP TRIGGER IF EXISTS "`+name+`"`); err != nil {
			t.Fatalf("drop trigger %s: %v", name, err)
		}
	}
	t.Cleanup(drop)
	return drop
}

func tailOffset(t *testing.T, engine *Engine, runID string) int64 {
	t.Helper()
	engine.mu.Lock()
	defer engine.mu.Unlock()
	tail := engine.tails[runID]
	if tail == nil {
		t.Fatalf("no transcript tail for run %s", runID)
	}
	return tail.offset
}

func TestEngineTranscriptValidEOFWaitsForDurableFraming(t *testing.T) {
	prefix := []byte(`{"type":"first","payload":{"n":1}}`)
	engine, transcriptPath := newTranscriptPersistenceEngine(t, prefix)
	ctx := context.Background()

	engine.tailTranscript(ctx, transcriptPersistenceRunID, transcriptPath)
	progress, err := engine.store.GetTranscriptProgress(ctx, transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress before framing: %v", err)
	}
	events, err := engine.store.ListEvents(ctx, transcriptPersistenceRunID, 0)
	if err != nil {
		t.Fatalf("ListEvents before framing: %v", err)
	}
	if progress.ConsumedOffset != 0 || len(events) != 0 || tailOffset(t, engine, transcriptPersistenceRunID) != 0 {
		t.Fatalf("unsealed EOF was published: progress=%+v tail=%d events=%d", progress, tailOffset(t, engine, transcriptPersistenceRunID), len(events))
	}

	suffix := []byte(`{"type":"second","payload":{"n":2}}` + "\n")
	f, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open transcript for append: %v", err)
	}
	if _, err := f.Write(suffix); err != nil {
		_ = f.Close()
		t.Fatalf("append second envelope: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close appended transcript: %v", err)
	}

	engine.tailTranscript(ctx, transcriptPersistenceRunID, transcriptPath)
	run, err := engine.store.GetRun(ctx, transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetRun after malformed physical line: %v", err)
	}
	progress, err = engine.store.GetTranscriptProgress(ctx, transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress after malformed physical line: %v", err)
	}
	events, err = engine.store.ListEvents(ctx, transcriptPersistenceRunID, 0)
	if err != nil {
		t.Fatalf("ListEvents after malformed physical line: %v", err)
	}
	wantOffset := int64(len(prefix) + len(suffix))
	if run.State != store.StateCleanupRequired || progress.ConsumedOffset != wantOffset || len(events) != 0 {
		t.Fatalf("malformed physical line result: state=%s consumed=%d/%d events=%d", run.State, progress.ConsumedOffset, wantOffset, len(events))
	}
}

func TestEngineTranscriptBlankLinesAdvanceWithoutEvents(t *testing.T) {
	blankLines := []byte("\n\n")
	engine, transcriptPath := newTranscriptPersistenceEngine(t, blankLines)

	engine.tailTranscript(context.Background(), transcriptPersistenceRunID, transcriptPath)
	events, err := engine.store.ListEvents(context.Background(), transcriptPersistenceRunID, 0)
	if err != nil {
		t.Fatalf("ListEvents after blank lines: %v", err)
	}
	run, err := engine.store.GetRun(context.Background(), transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetRun after blank lines: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events after blank lines = %d, want 0", len(events))
	}
	if run.State != store.StateRunning {
		t.Errorf("state after blank lines = %q, want running", run.State)
	}
	if run.CommittedOffset != 0 {
		t.Errorf("durable offset after blank lines = %d, want 0 without an event", run.CommittedOffset)
	}
	if got := tailOffset(t, engine, transcriptPersistenceRunID); got != int64(len(blankLines)) {
		t.Errorf("tail offset after blank lines = %d, want %d", got, len(blankLines))
	}

	firstLine := []byte("{\"type\":\"first\",\"payload\":{\"n\":1}}\n")
	secondLine := []byte("{\"type\":\"second\",\"payload\":{\"n\":2}}\n")
	f, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open transcript for later events: %v", err)
	}
	if _, err := f.Write(append(firstLine, secondLine...)); err != nil {
		_ = f.Close()
		t.Fatalf("append later events: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close transcript after later events: %v", err)
	}

	engine.tailTranscript(context.Background(), transcriptPersistenceRunID, transcriptPath)
	events, err = engine.store.ListEvents(context.Background(), transcriptPersistenceRunID, 0)
	if err != nil {
		t.Fatalf("ListEvents after later events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events after later lines = %d, want 2", len(events))
	}
	wantOffsets := []int64{
		int64(len(blankLines) + len(firstLine)),
		int64(len(blankLines) + len(firstLine) + len(secondLine)),
	}
	for i, wantType := range []string{"first", "second"} {
		if events[i].Seq != int64(i+1) || events[i].Type != wantType || events[i].TranscriptOffset != wantOffsets[i] {
			t.Errorf("event %d = seq %d type %q offset %d, want seq %d type %q offset %d",
				i, events[i].Seq, events[i].Type, events[i].TranscriptOffset, i+1, wantType, wantOffsets[i])
		}
	}
	run, err = engine.store.GetRun(context.Background(), transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetRun after later events: %v", err)
	}
	if run.State != store.StateRunning {
		t.Errorf("state after later events = %q, want running", run.State)
	}
	if run.CommittedOffset != wantOffsets[1] {
		t.Errorf("committed offset after later events = %d, want %d", run.CommittedOffset, wantOffsets[1])
	}
	if got := tailOffset(t, engine, transcriptPersistenceRunID); got != wantOffsets[1] {
		t.Errorf("tail offset after later events = %d, want %d", got, wantOffsets[1])
	}
}

func TestEngineTranscriptAppendFailureRetriesWithoutOffsetSkip(t *testing.T) {
	transcript := []byte("{\"type\":\"first\",\"payload\":{\"n\":1}}\n" +
		"{\"type\":\"second\",\"payload\":{\"n\":2}}\n" +
		"{\"type\":\"third\",\"payload\":{\"n\":3}}\n")
	engine, transcriptPath := newTranscriptPersistenceEngine(t, transcript)
	dropFailure := installSQLiteFailureTrigger(t, engine, "fail_transcript_event_insert", `
		CREATE TRIGGER fail_transcript_event_insert
		BEFORE INSERT ON events
		WHEN NEW.run_id = 'transcript-persistence-run'
		BEGIN
			SELECT RAISE(ABORT, 'injected event append failure');
		END`)

	engine.tailTranscript(context.Background(), transcriptPersistenceRunID, transcriptPath)
	events, err := engine.store.ListEvents(context.Background(), transcriptPersistenceRunID, 0)
	if err != nil {
		t.Fatalf("ListEvents after failed append: %v", err)
	}
	run, err := engine.store.GetRun(context.Background(), transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetRun after failed append: %v", err)
	}
	if len(events) != 0 || run.CommittedOffset != 0 {
		t.Errorf("after failed append: events=%d committed_offset=%d, want 0 and 0", len(events), run.CommittedOffset)
	}
	if got := tailOffset(t, engine, transcriptPersistenceRunID); got != 0 {
		t.Errorf("tail offset after failed append = %d, want 0", got)
	}

	dropFailure()
	engine.tailTranscript(context.Background(), transcriptPersistenceRunID, transcriptPath)
	events, err = engine.store.ListEvents(context.Background(), transcriptPersistenceRunID, 0)
	if err != nil {
		t.Fatalf("ListEvents after retry: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events after retry = %d, want 3", len(events))
	}
	for i, wantType := range []string{"first", "second", "third"} {
		if events[i].Seq != int64(i+1) || events[i].Type != wantType {
			t.Errorf("event %d = seq %d type %q, want seq %d type %q", i, events[i].Seq, events[i].Type, i+1, wantType)
		}
		if i > 0 && events[i].TranscriptOffset <= events[i-1].TranscriptOffset {
			t.Errorf("event offsets not increasing: %d then %d", events[i-1].TranscriptOffset, events[i].TranscriptOffset)
		}
	}
	run, err = engine.store.GetRun(context.Background(), transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetRun after retry: %v", err)
	}
	if run.CommittedOffset != int64(len(transcript)) {
		t.Errorf("committed offset after retry = %d, want %d", run.CommittedOffset, len(transcript))
	}
	if got := tailOffset(t, engine, transcriptPersistenceRunID); got != int64(len(transcript)) {
		t.Errorf("tail offset after retry = %d, want %d", got, len(transcript))
	}
}

func TestEngineTranscriptInvalidLineRetriesFailedCleanupTransition(t *testing.T) {
	transcript := []byte("THIS IS NOT JSON\n")
	engine, transcriptPath := newTranscriptPersistenceEngine(t, transcript)
	dropFailure := installSQLiteFailureTrigger(t, engine, "fail_transcript_cleanup_transition", `
		CREATE TRIGGER fail_transcript_cleanup_transition
		BEFORE UPDATE OF state ON runs
		WHEN OLD.id = 'transcript-persistence-run' AND NEW.state = 'cleanup_required'
		BEGIN
			SELECT RAISE(ABORT, 'injected cleanup transition failure');
		END`)

	engine.tailTranscript(context.Background(), transcriptPersistenceRunID, transcriptPath)
	run, err := engine.store.GetRun(context.Background(), transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetRun after failed transition: %v", err)
	}
	if run.State != store.StateRunning {
		t.Errorf("state after failed transition = %q, want running", run.State)
	}
	if got := tailOffset(t, engine, transcriptPersistenceRunID); got != 0 {
		t.Errorf("tail offset after failed transition = %d, want 0", got)
	}

	dropFailure()
	engine.tailTranscript(context.Background(), transcriptPersistenceRunID, transcriptPath)
	run, err = engine.store.GetRun(context.Background(), transcriptPersistenceRunID)
	if err != nil {
		t.Fatalf("GetRun after transition retry: %v", err)
	}
	if run.State != store.StateCleanupRequired {
		t.Errorf("state after transition retry = %q, want cleanup_required", run.State)
	}
	if got := tailOffset(t, engine, transcriptPersistenceRunID); got != int64(len(transcript)) {
		t.Errorf("tail offset after transition retry = %d, want %d", got, len(transcript))
	}
}

func TestEngineMalformedTranscriptRecordAccountingRetriesAfterCleanupTransition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		transcript []byte
	}{
		{name: "final newline", transcript: []byte("THIS IS NOT JSON\n")},
		{name: "sealed final bytes without newline", transcript: []byte("THIS IS NOT JSON")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, transcriptPath := newTranscriptPersistenceEngine(t, tc.transcript)
			ctx := context.Background()
			if _, err := engine.store.DB().ExecContext(ctx,
				`UPDATE runs SET transcript_required = 1 WHERE id = ?`, transcriptPersistenceRunID); err != nil {
				t.Fatalf("require transcript handoff: %v", err)
			}
			if err := engine.store.SealTranscript(ctx, transcriptPersistenceRunID, int64(len(tc.transcript))); err != nil {
				t.Fatalf("SealTranscript: %v", err)
			}
			dropFailure := installSQLiteFailureTrigger(t, engine, "fail_corrupt_record_accounting", `
				CREATE TRIGGER fail_corrupt_record_accounting
				BEFORE UPDATE OF transcript_consumed_offset ON runs
				WHEN OLD.id = '`+transcriptPersistenceRunID+`'
					AND NEW.transcript_consumed_offset > OLD.transcript_consumed_offset
				BEGIN
					SELECT RAISE(FAIL, 'injected corrupt-record accounting failure');
				END`)

			engine.tailTranscript(ctx, transcriptPersistenceRunID, transcriptPath)
			run, err := engine.store.GetRun(ctx, transcriptPersistenceRunID)
			if err != nil {
				t.Fatalf("GetRun after accounting failure: %v", err)
			}
			if run.State != store.StateCleanupRequired {
				t.Fatalf("state = %q, want durable cleanup_required before accounting", run.State)
			}
			progress, err := engine.store.GetTranscriptProgress(ctx, transcriptPersistenceRunID)
			if err != nil {
				t.Fatalf("GetTranscriptProgress after accounting failure: %v", err)
			}
			if progress.ConsumedOffset != 0 {
				t.Fatalf("consumed offset after injected failure = %d, want 0", progress.ConsumedOffset)
			}
			if got := tailOffset(t, engine, transcriptPersistenceRunID); got != 0 {
				t.Fatalf("tail offset after injected failure = %d, want durable offset 0", got)
			}

			dropFailure()
			engine.tailTranscript(ctx, transcriptPersistenceRunID, transcriptPath)
			progress, err = engine.store.GetTranscriptProgress(ctx, transcriptPersistenceRunID)
			if err != nil {
				t.Fatalf("GetTranscriptProgress after replay: %v", err)
			}
			if progress.ConsumedOffset != int64(len(tc.transcript)) || progress.FinalSize != int64(len(tc.transcript)) {
				t.Fatalf("progress after replay = %+v, want malformed bytes fully accounted", progress)
			}
			events, err := engine.store.ListEvents(ctx, transcriptPersistenceRunID, 0)
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if len(events) != 0 {
				t.Fatalf("malformed record created %d events, want 0", len(events))
			}
		})
	}
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
	r := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	transcriptPath := r.TranscriptPath
	waitUntil(t, "initial transcript record", 5*time.Second, func() bool {
		info, err := os.Stat(transcriptPath)
		return err == nil && info.Size() > 0
	})
	f, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open transcript for corruption: %v", err)
	}
	if _, err := f.WriteString("THIS IS NOT JSON\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append transcript corruption: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		t.Fatalf("sync transcript corruption: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close transcript corruption: %v", err)
	}

	// The engine's recovery loop should detect the corruption and transition
	// to cleanup_required (NOT failed).
	r = waitRunState(t, env.store, runID, store.StateCleanupRequired, 15*time.Second)
	if r.State == store.StateFailed {
		t.Errorf("run went to failed, should be cleanup_required while group alive")
	}
}

func TestEngineTranscriptCorruptionStaysNonterminalWhileAlive(t *testing.T) {
	env := newEngineEnvWithTick(t, time.Hour)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "1",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_DELAY_MS":      "0",
		"ANANKE_FW_EXIT_DELAY_MS": "30000",
	})

	r := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, statErr := os.Stat(r.TranscriptPath)
		if statErr == nil && info.Size() > 0 && processAlive(r.WorkerPID) && processAlive(r.SupervisorPID) {
			break
		}
		time.Sleep(10 * time.Millisecond)
		r, _ = env.store.GetRun(context.Background(), runID)
	}
	if !processAlive(r.WorkerPID) || !processAlive(r.SupervisorPID) {
		t.Fatalf("run not live before corruption: worker_pid=%d alive=%v supervisor_pid=%d alive=%v",
			r.WorkerPID, processAlive(r.WorkerPID), r.SupervisorPID, processAlive(r.SupervisorPID))
	}

	env.eng.tailTranscript(context.Background(), runID, r.TranscriptPath)
	r, err := env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after initial tail: %v", err)
	}
	if r.CommittedOffset <= 0 {
		t.Fatalf("committed offset = %d, want > 0 before corruption", r.CommittedOffset)
	}

	f, err := os.OpenFile(r.TranscriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open transcript for corruption: %v", err)
	}
	if _, err := f.WriteString("THIS IS NOT JSON\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append transcript corruption: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		t.Fatalf("sync transcript corruption: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close transcript corruption: %v", err)
	}

	env.eng.tailTranscript(context.Background(), runID, r.TranscriptPath)
	r, err = env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after corruption: %v", err)
	}
	workerAlive := processAlive(r.WorkerPID)
	supervisorAlive := processAlive(r.SupervisorPID)
	if store.IsTerminal(r.State) {
		t.Fatalf("terminal state %q visible while worker/supervisor alive: worker_pid=%d alive=%v supervisor_pid=%d alive=%v",
			r.State, r.WorkerPID, workerAlive, r.SupervisorPID, supervisorAlive)
	}
	if r.State != store.StateCleanupRequired {
		t.Fatalf("state = %q, want cleanup_required while worker/supervisor alive: worker_pid=%d alive=%v supervisor_pid=%d alive=%v",
			r.State, r.WorkerPID, workerAlive, r.SupervisorPID, supervisorAlive)
	}
	if !workerAlive || !supervisorAlive {
		t.Fatalf("cleanup_required observed without live worker/supervisor: worker_pid=%d alive=%v supervisor_pid=%d alive=%v",
			r.WorkerPID, workerAlive, r.SupervisorPID, supervisorAlive)
	}

	env.eng.progressCleanupRequired(context.Background(), r)
	r = waitRunState(t, env.store, runID, store.StateFailed, 15*time.Second)

	deadline = time.Now().Add(5 * time.Second)
	var (
		members  []int
		groupErr error
	)
	for time.Now().Before(deadline) {
		members, groupErr = env.eng.backend.GroupMembers(r.SupervisorPGID)
		groupQuiescent := groupErr == nil
		for _, pid := range members {
			if pid != r.SupervisorPID {
				groupQuiescent = false
				break
			}
		}
		if groupQuiescent && !processAlive(r.WorkerPID) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if groupErr != nil {
		t.Fatalf("enumerate cleaned group: %v", groupErr)
	}
	for _, pid := range members {
		if pid != r.SupervisorPID {
			t.Fatalf("failed became visible before safe cleanup: worker_pid=%d alive=%v supervisor_pid=%d alive=%v group_members=%v",
				r.WorkerPID, processAlive(r.WorkerPID), r.SupervisorPID, processAlive(r.SupervisorPID), members)
		}
	}
	if processAlive(r.WorkerPID) {
		t.Fatalf("failed became visible before worker cleanup: worker_pid=%d alive=true supervisor_pid=%d alive=%v group_members=%v",
			r.WorkerPID, r.SupervisorPID, processAlive(r.SupervisorPID), members)
	}
}

func TestEngineTranscriptReplacementAtOffsetZeroFailsClosed(t *testing.T) {
	env := newEngineEnvWithTick(t, time.Hour)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "0",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_EXIT_DELAY_MS": "30000",
	})
	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	waitUntil(t, "empty live transcript", 5*time.Second, func() bool {
		info, err := os.Stat(run.TranscriptPath)
		return err == nil && info.Mode().IsRegular() && info.Size() == 0 &&
			processAlive(run.WorkerPID) && processAlive(run.SupervisorPID)
	})
	env.eng.startTailing(context.Background(), runID, run.TranscriptPath, 0)
	if got := tailOffset(t, env.eng, runID); got != 0 {
		t.Fatalf("initial tail offset = %d, want 0", got)
	}

	replacement := filepath.Join(filepath.Dir(run.TranscriptPath), "replacement.ndjson")
	if err := os.WriteFile(replacement, []byte("{\"type\":\"replacement\",\"payload\":{}}\n"), 0o600); err != nil {
		t.Fatalf("write replacement transcript: %v", err)
	}
	if err := os.Rename(replacement, run.TranscriptPath); err != nil {
		t.Fatalf("replace transcript inode: %v", err)
	}

	env.eng.tailTranscript(context.Background(), runID, run.TranscriptPath)
	env.eng.tailTranscript(context.Background(), runID, run.TranscriptPath)
	run, err := env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after offset-zero replacement: %v", err)
	}
	if run.State != store.StateCleanupRequired {
		t.Fatalf("state = %q, want cleanup_required after offset-zero replacement", run.State)
	}
	if store.IsTerminal(run.State) {
		t.Fatalf("terminal state %q published while replacement authority is live", run.State)
	}
	if !processAlive(run.WorkerPID) || !processAlive(run.SupervisorPID) {
		t.Fatalf("authority not live at cleanup_required: worker=%v supervisor=%v",
			processAlive(run.WorkerPID), processAlive(run.SupervisorPID))
	}
	events, err := env.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("replacement fabricated %d events, want 0", len(events))
	}
	progress, err := env.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if progress.ConsumedOffset != 0 || progress.FinalSize != -1 {
		t.Fatalf("replacement progress = %+v, want no fabricated accounting", progress)
	}
	if _, err := env.store.GetOutbox(context.Background(), runID); err == nil {
		t.Fatal("terminal outbox published while replacement authority is live")
	}
}

// TestEngineTranscriptTruncationAfterTailing verifies that replacing a live
// transcript below its committed offset is detected after real events have
// already been tailed.
func TestEngineTranscriptTruncationAfterTailing(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "3",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_DELAY_MS":      "20",
		"ANANKE_FW_EXIT_DELAY_MS": "10000",
	})

	waitRunState(t, env.store, runID, store.StateRunning, 10*time.Second)

	var (
		r             store.Run
		ingestedCount int
	)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		events, eventsErr := env.store.ListEvents(context.Background(), runID, 0)
		current, runErr := env.store.GetRun(context.Background(), runID)
		if eventsErr == nil && runErr == nil {
			r = current
			ingestedCount = len(events)
			if ingestedCount == 3 && r.CommittedOffset > 0 && r.WorkerPID > 0 && r.SupervisorPID > 0 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ingestedCount != 3 || r.CommittedOffset <= 0 {
		t.Fatalf("events durably ingested = %d, committed offset = %d; want 3 events and offset > 0", ingestedCount, r.CommittedOffset)
	}
	if !processAlive(r.WorkerPID) || !processAlive(r.SupervisorPID) {
		t.Fatalf("worker/supervisor not alive before truncation: worker=%d alive=%v supervisor=%d alive=%v",
			r.WorkerPID, processAlive(r.WorkerPID), r.SupervisorPID, processAlive(r.SupervisorPID))
	}
	t.Cleanup(func() {
		_, _ = env.eng.sendSupervisorCmd(context.Background(), r.SocketPath, r.Token, "cancel")
	})

	malformed := []byte("{broken}\n")
	if int64(len(malformed)) >= r.CommittedOffset {
		t.Fatalf("test replacement size %d is not below committed offset %d", len(malformed), r.CommittedOffset)
	}
	if err := os.WriteFile(r.TranscriptPath, malformed, 0o600); err != nil {
		t.Fatalf("replace transcript: %v", err)
	}
	info, err := os.Stat(r.TranscriptPath)
	if err != nil {
		t.Fatalf("stat replaced transcript: %v", err)
	}
	if info.Size() >= r.CommittedOffset {
		t.Fatalf("replacement size = %d, want below committed offset %d", info.Size(), r.CommittedOffset)
	}

	r = waitRunState(t, env.store, runID, store.StateCleanupRequired, 5*time.Second)
	if !processAlive(r.WorkerPID) || !processAlive(r.SupervisorPID) {
		t.Fatalf("cleanup_required published after worker/group exit: worker=%d alive=%v supervisor=%d alive=%v",
			r.WorkerPID, processAlive(r.WorkerPID), r.SupervisorPID, processAlive(r.SupervisorPID))
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
	r := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	supPID := r.SupervisorPID
	if supPID == 0 {
		t.Fatalf("supervisor pid not recorded")
	}

	// Observe the exact child with the same non-reaping Darwin primitive used
	// by the engine, then crash it.
	exited := observeProcessExitForTest(t, supPID)
	if err := unix.Kill(supPID, unix.SIGKILL); err != nil {
		t.Fatalf("SIGKILL supervisor: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor NOTE_EXIT not observed")
	}

	// Exit detection must not reap. The recovery loop should transition using
	// the exact local watcher while the PID remains our reapable child.
	waitRunState(t, env.store, runID, store.StateRecoveryUnknown, 15*time.Second)
	assertExactChild(t, supPID)
}

func TestEngineTerminalSupervisorReaped(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "0",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "750",
	})
	r := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	supPID := r.SupervisorPID
	if supPID == 0 {
		t.Fatal("supervisor pid not recorded")
	}
	env.eng.mu.Lock()
	h := env.eng.active[runID]
	env.eng.mu.Unlock()
	if h == nil {
		t.Fatal("local supervisor handle not tracked")
	}

	waitRunState(t, env.store, runID, store.StateCompleted, 15*time.Second)
	waitUntil(t, "terminal supervisor outbox finalized", 5*time.Second, func() bool {
		outbox, err := env.store.GetOutbox(context.Background(), runID)
		return err == nil && outbox.Acknowledged != 0
	})
	waitUntil(t, "finalized local supervisor reaped and handle released", 5*time.Second, func() bool {
		env.eng.mu.Lock()
		_, tracked := env.eng.active[runID]
		env.eng.mu.Unlock()
		_, procErr := unix.SysctlKinfoProc("kern.proc.pid", supPID)
		return !tracked && procErr != nil
	})
	if h.cmd.ProcessState == nil || !h.cmd.ProcessState.Exited() {
		t.Fatal("supervisor handle has no reaped process state")
	}
	reapResults := make(chan error, 2)
	go func() { reapResults <- h.reap() }()
	go func() { reapResults <- h.reap() }()
	for range 2 {
		if err := <-reapResults; err != nil {
			t.Fatalf("cached reap result: %v", err)
		}
	}
}

func TestEngineCloseLeavesLiveSupervisor(t *testing.T) {
	env := newEngineEnvWithTick(t, time.Hour)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "0",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "10000",
	})
	r := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	supPID := r.SupervisorPID
	if supPID == 0 {
		t.Fatal("supervisor pid not recorded")
	}
	env.eng.mu.Lock()
	h := env.eng.active[runID]
	env.eng.mu.Unlock()
	if h == nil {
		t.Fatal("local supervisor handle not tracked")
	}
	t.Cleanup(func() {
		// The assertion below intentionally proves Engine.Close leaves the live
		// process tree recoverable. Test cleanup still owns the exact child and
		// must remove it so package/race runs do not leak into later gates.
		if r.WorkerPID > 0 {
			_ = unix.Kill(r.WorkerPID, unix.SIGKILL)
		}
		_ = unix.Kill(supPID, unix.SIGKILL)
		_ = h.reap()
	})

	env.cancel()
	closed := make(chan error, 1)
	go func() { closed <- env.eng.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Engine.Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Engine.Close blocked on live supervisor")
	}
	assertExactChild(t, supPID)
	if err := unix.Kill(supPID, 0); err != nil {
		t.Fatalf("Engine.Close killed live supervisor %d: %v", supPID, err)
	}
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
	if err := env.store.SetRunSupervisor(context.Background(), runID, recoveryTestSupervisorPID, recoveryTestSupervisorPGID, recoveryTestWorkerPID); err != nil {
		t.Fatalf("SetRunSupervisor: %v", err)
	}
	if err := WriteIdentity(spec.IdentityPath, Identity{
		RunID:          runID,
		SupervisorPID:  recoveryTestSupervisorPID,
		SupervisorPGID: recoveryTestSupervisorPGID,
		WorkerPID:      recoveryTestWorkerPID,
		WorkerArgs:     []string{},
		SocketPath:     spec.SocketPath,
		Token:          spec.Token,
		TranscriptPath: spec.TranscriptPath,
		LaunchTime:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("WriteIdentity: %v", err)
	}
	// Commit terminal with outbox row (simulates supervisor crash after commit
	// but before acknowledge).
	if err := env.store.CommitTerminal(context.Background(), runID, store.StateCompleted, "test terminal", store.OutboxRow{
		RunID:          runID,
		TerminalState:  store.StateCompleted,
		SupervisorPID:  recoveryTestSupervisorPID,
		SupervisorPGID: recoveryTestSupervisorPGID,
		SocketPath:     spec.SocketPath,
		Token:          spec.Token,
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
	eng2.backend = &cleanupProbeBackend{
		alive: map[int]bool{
			recoveryTestSupervisorPID: false,
			recoveryTestWorkerPID:     false,
		},
		members: []int{},
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { _ = eng2.Run(ctx2) }()
	waitForEngineSocket(t, env.socketPath, 5*time.Second)
	t.Cleanup(func() {
		cancel2()
		_ = eng2.Close()
	})

	// The recovery loop should abandon the row after the fake backend reports
	// the exact supervisor and worker absent and their process group empty.
	st2 := eng2.Store()
	outbox3, err := st2.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox after restart: %v", err)
	}
	if outbox3.Acknowledged != -1 || strings.TrimSpace(outbox3.Diagnostic) == "" {
		t.Fatalf("outbox acknowledged=%d diagnostic=%q, want abandoned with durable diagnostic", outbox3.Acknowledged, outbox3.Diagnostic)
	}
}

func TestEngineCleanupRequiredCancelsSupervisorAndFails(t *testing.T) {
	env := newEngineEnv(t)
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":         "3",
		"ANANKE_FW_EXIT":           "0",
		"ANANKE_FW_DELAY_MS":       "20",
		"ANANKE_FW_EXIT_DELAY_MS":  "5000",
		"ANANKE_FW_SPAWN_CHILD":    "1",
		"ANANKE_FW_CHILD_MODE":     "resistant",
		"ANANKE_FW_CHILD_PID_FILE": childPIDFile,
	})

	var (
		r        store.Run
		childPID int
	)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		events, eventsErr := env.store.ListEvents(context.Background(), runID, 0)
		current, runErr := env.store.GetRun(context.Background(), runID)
		if data, err := os.ReadFile(childPIDFile); err == nil {
			childPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if eventsErr == nil && runErr == nil {
			r = current
			if len(events) == 3 && r.CommittedOffset > 0 && childPID > 0 {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if r.State != store.StateRunning || r.CommittedOffset <= 0 || childPID <= 0 {
		t.Fatalf("live run not ready: state=%s offset=%d child_pid=%d", r.State, r.CommittedOffset, childPID)
	}
	if !processAlive(r.WorkerPID) || !processAlive(r.SupervisorPID) || !processAlive(childPID) {
		t.Fatalf("process group not live before corruption: supervisor=%v worker=%v child=%v",
			processAlive(r.SupervisorPID), processAlive(r.WorkerPID), processAlive(childPID))
	}

	f, err := os.OpenFile(r.TranscriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open transcript for corruption: %v", err)
	}
	if _, err := f.WriteString("THIS IS NOT JSON\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append transcript corruption: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		t.Fatalf("sync transcript corruption: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close transcript corruption: %v", err)
	}

	sawCleanupRequired := false
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		current, err := env.store.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun after corruption: %v", err)
		}
		switch current.State {
		case store.StateCleanupRequired:
			sawCleanupRequired = true
		case store.StateCompleted:
			t.Fatalf("run observed as completed after transcript corruption")
		case store.StateCancelled:
			t.Fatalf("run observed as cancelled after transcript corruption")
		case store.StateFailed:
			r = current
			goto terminal
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("run did not reach failed after transcript corruption")

terminal:
	transitions, err := env.store.Transitions(context.Background(), runID)
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	var terminalReason string
	for _, tr := range transitions {
		if tr.ToState == store.StateCleanupRequired {
			sawCleanupRequired = true
		}
		if tr.ToState == store.StateCompleted {
			t.Fatalf("transition history contains completed after transcript corruption")
		}
		if tr.ToState == store.StateFailed {
			terminalReason = tr.Reason
		}
	}
	if !sawCleanupRequired {
		t.Fatalf("transition history never contained cleanup_required")
	}
	if !strings.Contains(terminalReason, "transcript corruption") {
		t.Fatalf("failed reason = %q, want transcript corruption reason", terminalReason)
	}

	outbox, err := env.store.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("failed state committed without outbox: %v", err)
	}
	if outbox.TerminalState != store.StateFailed {
		t.Fatalf("outbox terminal = %q, want failed", outbox.TerminalState)
	}

	deadline = time.Now().Add(5 * time.Second)
	var members []int
	for time.Now().Before(deadline) {
		members, err = env.eng.backend.GroupMembers(r.SupervisorPGID)
		if err == nil && len(members) == 0 && !processAlive(r.WorkerPID) && !processAlive(childPID) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("enumerate cleaned group: %v", err)
	}
	if len(members) != 0 || processAlive(r.WorkerPID) || processAlive(childPID) {
		t.Fatalf("group cleanup incomplete: members=%v worker_alive=%v child_alive=%v",
			members, processAlive(r.WorkerPID), processAlive(childPID))
	}
}

// processAlive reports whether a PID is alive.
func processAlive(pid int) bool {
	return unix.Kill(pid, 0) == nil
}

func observeProcessExitForTest(t *testing.T, pid int) <-chan struct{} {
	t.Helper()
	w, err := newProcessExitWatcher(pid)
	if err != nil {
		t.Fatalf("register NOTE_EXIT for pid %d: %v", pid, err)
	}
	return w.exited
}

func assertExactChild(t *testing.T, pid int) {
	t.Helper()
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		t.Fatalf("process %d is not present: %v", pid, err)
	}
	if got := int(proc.Proc.P_pid); got != pid {
		t.Fatalf("observed pid = %d, want %d", got, pid)
	}
	if got := int(proc.Eproc.Ppid); got != os.Getpid() {
		t.Fatalf("process %d parent = %d, want daemon/test process %d", pid, got, os.Getpid())
	}
}

const (
	cleanupTestSupervisorPID  = 41001
	cleanupTestSupervisorPGID = 41001
	cleanupTestWorkerPID      = 41002
)

type cleanupProbeBackend struct {
	mu          sync.Mutex
	alive       map[int]bool
	members     []int
	groupErr    error
	groupCalls  int
	signalCalls []int
}

func (b *cleanupProbeBackend) LaunchWorker(string, []string, []string) (int, error) {
	return 0, fmt.Errorf("unexpected LaunchWorker")
}

func (b *cleanupProbeBackend) ReleaseWorker(int) error {
	return fmt.Errorf("unexpected ReleaseWorker")
}

func (b *cleanupProbeBackend) WorkerExited(int) (<-chan struct{}, error) {
	return nil, fmt.Errorf("unexpected WorkerExited")
}

func (b *cleanupProbeBackend) ReapWorker(int) (int, error) {
	return 0, fmt.Errorf("unexpected ReapWorker")
}

func (b *cleanupProbeBackend) GroupMembers(int) ([]int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.groupCalls++
	return append([]int(nil), b.members...), b.groupErr
}

func (b *cleanupProbeBackend) SignalGroup(pgid int, _ Signal) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.signalCalls = append(b.signalCalls, pgid)
	return nil
}

func (b *cleanupProbeBackend) ProcessAlive(pid int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.alive[pid]
}

func (b *cleanupProbeBackend) snapshotCalls() (groupCalls int, signals []int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.groupCalls, append([]int(nil), b.signalCalls...)
}

func newCleanupRequiredTickEngine(t *testing.T, backend LifecycleBackend, socketPath string) (*Engine, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.CreateProject(ctx, "cleanup-project", "cleanup", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.CreateWorkstream(ctx, "cleanup-workstream", "cleanup-project", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	runID := "cleanup-run-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := st.CreateRun(ctx, runID, "cleanup-project", "cleanup-workstream", store.RunSpec{
		WorkerPath: "/bin/true",
		SocketPath: socketPath,
		Token:      "cleanup-supervisor-token",
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.Transition(ctx, runID, store.StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	if err := st.SetRunSupervisor(ctx, runID, cleanupTestSupervisorPID, cleanupTestSupervisorPGID, cleanupTestWorkerPID); err != nil {
		t.Fatalf("SetRunSupervisor: %v", err)
	}
	if err := st.Transition(ctx, runID, store.StateCleanupRequired, "transcript corruption"); err != nil {
		t.Fatalf("Transition cleanup_required: %v", err)
	}
	return &Engine{
		store:   st,
		backend: backend,
		active:  make(map[string]*runHandle),
		tails:   make(map[string]*transcriptTail),
	}, runID
}

func TestEngineCleanupRequiredTickDoesNotBlock(t *testing.T) {
	socketPath := filepath.Join("/tmp", "ananke-cleanup-block-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	backend := &cleanupProbeBackend{alive: map[int]bool{
		cleanupTestSupervisorPID: true,
		cleanupTestWorkerPID:     true,
	}}
	engine, _ := newCleanupRequiredTickEngine(t, backend, socketPath)
	tickDone := make(chan struct{})
	go func() {
		engine.tick(context.Background())
		close(tickDone)
	}()

	blocked := false
	select {
	case <-tickDone:
	case <-time.After(250 * time.Millisecond):
		blocked = true
	}

	var supervisorConn net.Conn
	requested := true
	select {
	case supervisorConn = <-accepted:
	case <-time.After(time.Second):
		requested = false
	}
	if supervisorConn != nil {
		_ = supervisorConn.Close()
	}
	if blocked {
		select {
		case <-tickDone:
		case <-time.After(time.Second):
			t.Fatal("tick remained blocked after supervisor connection closed")
		}
		t.Error("tick blocked waiting for supervisor cleanup response")
	}
	if !requested {
		t.Error("tick did not request authenticated supervisor cancellation")
	}
}

func TestEngineCleanupRequiredTickRequestsCancellationOnce(t *testing.T) {
	socketPath := filepath.Join("/tmp", "ananke-cleanup-once-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})
	requests := make(chan map[string]string, 8)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				var req map[string]string
				if err := json.NewDecoder(conn).Decode(&req); err == nil {
					requests <- req
					_ = json.NewEncoder(conn).Encode(map[string]any{"ok": true, "state": "cancelling"})
				}
			}(conn)
		}
	}()

	backend := &cleanupProbeBackend{alive: map[int]bool{
		cleanupTestSupervisorPID: true,
		cleanupTestWorkerPID:     true,
	}}
	engine, _ := newCleanupRequiredTickEngine(t, backend, socketPath)
	for range 3 {
		engine.tick(context.Background())
	}

	var first map[string]string
	select {
	case first = <-requests:
	case <-time.After(time.Second):
		t.Fatal("no cleanup cancellation request received")
	}
	time.Sleep(250 * time.Millisecond)
	count := 1
	for {
		select {
		case <-requests:
			count++
		default:
			goto counted
		}
	}

counted:
	if count != 1 {
		t.Fatalf("cleanup cancellation requests = %d, want exactly 1", count)
	}
	if first["cmd"] != "cancel" || first["token"] != "cleanup-supervisor-token" {
		t.Fatalf("cleanup request = %v, want authenticated cancel", first)
	}
}

func TestEngineCleanupRequiredDeadSupervisorSafetyGate(t *testing.T) {
	tests := []struct {
		name           string
		workerAlive    bool
		members        []int
		groupErr       error
		wantGroupCalls int
	}{
		{name: "worker still alive", workerAlive: true},
		{name: "group still occupied", members: []int{41003}, wantGroupCalls: 1},
		{name: "group membership ambiguous", groupErr: fmt.Errorf("membership unavailable"), wantGroupCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := &cleanupProbeBackend{
				alive: map[int]bool{
					cleanupTestSupervisorPID: false,
					cleanupTestWorkerPID:     tc.workerAlive,
				},
				members:  tc.members,
				groupErr: tc.groupErr,
			}
			engine, runID := newCleanupRequiredTickEngine(t, backend, "")
			engine.tick(context.Background())

			run, err := engine.store.GetRun(context.Background(), runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.State != store.StateCleanupRequired {
				t.Errorf("state = %q, want cleanup_required obligation intact", run.State)
			}
			groupCalls, signals := backend.snapshotCalls()
			if groupCalls != tc.wantGroupCalls {
				t.Errorf("GroupMembers calls = %d, want %d", groupCalls, tc.wantGroupCalls)
			}
			if len(signals) != 0 {
				t.Errorf("signals after supervisor death = %v, want none", signals)
			}
		})
	}
}

func TestEngineCleanupRequiredDeadSupervisorQuiescentFails(t *testing.T) {
	backend := &cleanupProbeBackend{alive: map[int]bool{
		cleanupTestSupervisorPID: false,
		cleanupTestWorkerPID:     false,
	}}
	engine, runID := newCleanupRequiredTickEngine(t, backend, "")
	engine.tick(context.Background())

	run, err := engine.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateFailed {
		t.Fatalf("state = %q, want failed after worker death and group quiescence", run.State)
	}
	_, signals := backend.snapshotCalls()
	if len(signals) != 0 {
		t.Fatalf("signals after supervisor death = %v, want none", signals)
	}
}

func TestEngineCleanupRequiredHandoffFailureStaysNonterminal(t *testing.T) {
	backend := &cleanupProbeBackend{alive: map[int]bool{
		cleanupTestSupervisorPID: false,
		cleanupTestWorkerPID:     false,
	}}
	engine, runID := newCleanupRequiredTickEngine(t, backend, "")
	transcriptPath := filepath.Join(t.TempDir(), "unsealed.ndjson")
	if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"message\"}\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if _, err := engine.store.DB().ExecContext(context.Background(), `UPDATE runs
		SET transcript_required = 1, transcript_path = ?, transcript_final_size = -1,
			transcript_consumed_offset = 0
		WHERE id = ?`, transcriptPath, runID); err != nil {
		t.Fatalf("mark transcript handoff incomplete: %v", err)
	}
	persistTranscriptIdentity(t, engine.store, runID, transcriptPath)
	dropFailure := installSQLiteFailureTrigger(t, engine, "fail_cleanup_transcript_seal", `
		CREATE TRIGGER fail_cleanup_transcript_seal
		BEFORE UPDATE OF transcript_final_size ON runs
		WHEN OLD.id = '`+runID+`' AND OLD.transcript_final_size < 0
		BEGIN SELECT RAISE(FAIL, 'injected cleanup transcript seal failure'); END`)

	engine.tick(context.Background())
	run, err := engine.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateCleanupRequired {
		t.Fatalf("state = %q, want cleanup_required until transcript handoff succeeds", run.State)
	}
	if _, err := engine.store.GetOutbox(context.Background(), runID); err == nil {
		t.Fatal("terminal outbox became visible before transcript handoff")
	}
	progress, err := engine.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress while seal blocked: %v", err)
	}
	if progress.FinalSize != -1 || progress.ConsumedOffset != int64(len("{\"type\":\"message\"}\n")) {
		t.Fatalf("blocked cleanup progress = %+v, want bytes accounted but seal unavailable", progress)
	}

	dropFailure()
	engine.tick(context.Background())
	run, err = engine.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after handoff retry: %v", err)
	}
	if run.State != store.StateFailed {
		t.Fatalf("state after handoff retry = %q, want failed", run.State)
	}
	progress, err = engine.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress after handoff retry: %v", err)
	}
	if progress.FinalSize != int64(len("{\"type\":\"message\"}\n")) || progress.ConsumedOffset != progress.FinalSize {
		t.Fatalf("cleanup terminal progress = %+v, want exact sealed drain", progress)
	}
}

func TestEngineCleanupRequiredDeadSupervisorTerminalOutboxAtomic(t *testing.T) {
	t.Run("failed state includes outbox", func(t *testing.T) {
		backend := &cleanupProbeBackend{alive: map[int]bool{
			cleanupTestSupervisorPID: false,
			cleanupTestWorkerPID:     false,
		}}
		engine, runID := newCleanupRequiredTickEngine(t, backend, "")
		engine.tick(context.Background())

		outbox, err := engine.store.GetOutbox(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetOutbox after terminal tick: %v", err)
		}
		if outbox.TerminalState != store.StateFailed {
			t.Fatalf("outbox terminal = %q, want failed", outbox.TerminalState)
		}
		run, err := engine.store.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if run.State != store.StateFailed {
			t.Fatalf("state = %q, want failed", run.State)
		}
	})

	t.Run("outbox failure rolls back terminal state", func(t *testing.T) {
		backend := &cleanupProbeBackend{alive: map[int]bool{
			cleanupTestSupervisorPID: false,
			cleanupTestWorkerPID:     false,
		}}
		engine, runID := newCleanupRequiredTickEngine(t, backend, "")
		if _, err := engine.store.DB().ExecContext(context.Background(), `INSERT INTO finalization_outbox
			(run_id, terminal_state, acknowledged, created_at) VALUES (?, ?, 0, ?)`,
			runID, store.StateFailed, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("seed outbox collision: %v", err)
		}

		engine.tick(context.Background())
		run, err := engine.store.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if run.State != store.StateCleanupRequired {
			t.Fatalf("state = %q, want cleanup_required after atomic rollback", run.State)
		}
	})
}

func TestEngineCleanupTerminalPersistenceFailureReportedAndRetried(t *testing.T) {
	backend := &cleanupProbeBackend{alive: map[int]bool{
		cleanupTestSupervisorPID: false,
		cleanupTestWorkerPID:     false,
	}}
	engine, runID := newCleanupRequiredTickEngine(t, backend, "")
	var reported []error
	engine.cfg.ReportError = func(err error) {
		reported = append(reported, err)
	}
	dropFailure := installSQLiteFailureTrigger(t, engine, "fail_cleanup_terminal_update", `
		CREATE TRIGGER fail_cleanup_terminal_update
		BEFORE UPDATE OF state ON runs
		WHEN OLD.id = '`+runID+`' AND NEW.state = 'failed'
		BEGIN
			SELECT RAISE(ABORT, 'injected cleanup terminal failure');
		END`)

	engine.tick(context.Background())
	run, err := engine.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after failed terminal persistence: %v", err)
	}
	if run.State != store.StateCleanupRequired {
		t.Fatalf("state after failed terminal persistence = %q, want cleanup_required", run.State)
	}
	if len(reported) != 1 || !strings.Contains(reported[0].Error(), "persist cleanup terminal") || !strings.Contains(reported[0].Error(), "injected cleanup terminal failure") {
		t.Fatalf("reported errors = %v, want one cleanup terminal persistence error", reported)
	}

	dropFailure()
	engine.tick(context.Background())
	run, err = engine.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after terminal retry: %v", err)
	}
	if run.State != store.StateFailed {
		t.Fatalf("state after terminal retry = %q, want failed", run.State)
	}
	outbox, err := engine.store.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox after terminal retry: %v", err)
	}
	if outbox.TerminalState != store.StateFailed {
		t.Fatalf("outbox terminal state after retry = %q, want failed", outbox.TerminalState)
	}
}

const (
	recoveryTestSupervisorPID  = 52001
	recoveryTestSupervisorPGID = 52001
	recoveryTestWorkerPID      = 52002
)

type recoveryFixture struct {
	storePath string
	engine    *Engine
	run       store.Run
	backend   *cleanupProbeBackend
}

func newRecoveryFixture(t *testing.T, state store.State) *recoveryFixture {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	ctx := context.Background()
	if err := st.CreateProject(ctx, "recovery-project", "recovery", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.CreateWorkstream(ctx, "recovery-workstream", "recovery-project", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	runID := "recovery-run"
	identityPath := filepath.Join(dir, "identity.json")
	transcriptPath := filepath.Join(dir, "transcript.ndjson")
	transcript := []byte("{\"type\":\"seed\",\"payload\":{\"seed\":true}}\n")
	socketPath := filepath.Join("/tmp", "ananke-m5-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	if err := os.WriteFile(transcriptPath, transcript, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := st.CreateRun(ctx, runID, "recovery-project", "recovery-workstream", store.RunSpec{
		WorkerPath:         "/bin/true",
		TranscriptPath:     transcriptPath,
		SocketPath:         socketPath,
		Token:              "recovery-token",
		IdentityPath:       identityPath,
		TranscriptRequired: true,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	persistTranscriptIdentity(t, st, runID, transcriptPath)
	if err := st.Transition(ctx, runID, store.StateRunning, "supervisor started"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	if err := st.SetRunSupervisor(ctx, runID, recoveryTestSupervisorPID, recoveryTestSupervisorPGID, recoveryTestWorkerPID); err != nil {
		t.Fatalf("SetRunSupervisor: %v", err)
	}
	if state != store.StateRunning {
		if err := st.Transition(ctx, runID, state, "seed recovery state"); err != nil {
			t.Fatalf("Transition %s: %v", state, err)
		}
	}
	if _, err := st.AppendEvent(ctx, runID, "seed", []byte(`{"seed":true}`), int64(len(transcript))); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	run, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	backend := &cleanupProbeBackend{alive: map[int]bool{
		recoveryTestSupervisorPID: true,
		recoveryTestWorkerPID:     true,
	}}
	engine := &Engine{
		store:            st,
		backend:          backend,
		active:           make(map[string]*runHandle),
		tails:            make(map[string]*transcriptTail),
		cleanupRequested: make(map[string]struct{}),
	}
	t.Cleanup(func() { _ = engine.Close() })
	return &recoveryFixture{engine: engine, run: run, backend: backend, storePath: storePath}
}

func recoveryIdentityValues(r store.Run) map[string]any {
	return map[string]any{
		"run_id":              r.ID,
		"supervisor_pid":      r.SupervisorPID,
		"supervisor_pgid":     r.SupervisorPGID,
		"worker_pid":          r.WorkerPID,
		"worker_args":         []string{},
		"socket_path":         r.SocketPath,
		"token":               r.Token,
		"transcript_path":     r.TranscriptPath,
		"transcript_identity": r.TranscriptIdentity,
		"launch_time":         time.Now().UTC(),
	}
}

func writeRecoveryIdentityValues(t *testing.T, path string, values map[string]any) {
	t.Helper()
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal identity: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
}

func recoveryStatus(r store.Run) map[string]any {
	return map[string]any{
		"ok":                true,
		"state":             "running",
		"run_id":            r.ID,
		"supervisor_pid":    r.SupervisorPID,
		"worker_pid":        r.WorkerPID,
		"pgid":              r.SupervisorPGID,
		"transcript_device": r.TranscriptIdentity.Device,
		"transcript_inode":  r.TranscriptIdentity.Inode,
	}
}

func encodeRecoveryResponse(t *testing.T, response map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return append(data, '\n')
}

func startRecoverySocket(t *testing.T, path string, respond func(map[string]string) []byte) <-chan map[string]string {
	t.Helper()
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("Listen recovery socket: %v", err)
	}
	requests := make(chan map[string]string, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			var req map[string]string
			if err := json.NewDecoder(conn).Decode(&req); err == nil {
				requests <- req
				_, _ = conn.Write(respond(req))
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("recovery socket server did not stop")
		}
		_ = os.Remove(path)
	})
	return requests
}

func recoverWithDeadline(t *testing.T, engine *Engine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := engine.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}
}

func receiveRecoveryRequest(t *testing.T, requests <-chan map[string]string) map[string]string {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovery socket request")
		return nil
	}
}

func assertRecoveryRejected(t *testing.T, fixture *recoveryFixture, diagnostic string) {
	t.Helper()
	run, err := fixture.engine.store.GetRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun after Recover: %v", err)
	}
	if run.State != store.StateRecoveryUnknown {
		t.Fatalf("state = %q, want recovery_unknown", run.State)
	}
	transitions, err := fixture.engine.store.Transitions(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	last := transitions[len(transitions)-1]
	if last.ToState != store.StateRecoveryUnknown || !strings.Contains(last.Reason, diagnostic) {
		t.Fatalf("last transition = %q (%q), want recovery_unknown diagnostic containing %q", last.ToState, last.Reason, diagnostic)
	}
	groupCalls, signals := fixture.backend.snapshotCalls()
	if groupCalls != 0 || len(signals) != 0 {
		t.Fatalf("unsafe cleanup calls after rejected recovery: GroupMembers=%d signals=%v", groupCalls, signals)
	}
	fixture.engine.mu.Lock()
	_, tailing := fixture.engine.tails[fixture.run.ID]
	_, locallyOwned := fixture.engine.active[fixture.run.ID]
	_, cleanupRequested := fixture.engine.cleanupRequested[fixture.run.ID]
	fixture.engine.mu.Unlock()
	if tailing || locallyOwned || cleanupRequested {
		t.Fatalf("rejected recovery side effects: tailing=%v locally_owned=%v cleanup_requested=%v", tailing, locallyOwned, cleanupRequested)
	}
}

func TestEngineRecoverValidAuthenticatedSupervisor(t *testing.T) {
	fixture := newRecoveryFixture(t, store.StateRunning)
	writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
	requests := startRecoverySocket(t, fixture.run.SocketPath, func(map[string]string) []byte {
		return encodeRecoveryResponse(t, recoveryStatus(fixture.run))
	})

	recoverWithDeadline(t, fixture.engine)

	for _, wantCmd := range []string{"status", "adopt"} {
		req := receiveRecoveryRequest(t, requests)
		if req["cmd"] != wantCmd || req["token"] != fixture.run.Token {
			t.Fatalf("recovery request = %v, want cmd=%q token=%q", req, wantCmd, fixture.run.Token)
		}
	}
	run, err := fixture.engine.store.GetRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun after Recover: %v", err)
	}
	if run.State != store.StateRunning {
		t.Fatalf("state = %q, want running", run.State)
	}
	fixture.engine.mu.Lock()
	tail := fixture.engine.tails[fixture.run.ID]
	_, locallyOwned := fixture.engine.active[fixture.run.ID]
	fixture.engine.mu.Unlock()
	if tail == nil || tail.offset != fixture.run.CommittedOffset {
		t.Fatalf("tail = %#v, want committed offset %d", tail, fixture.run.CommittedOffset)
	}
	if locallyOwned {
		t.Fatal("restarted engine claimed local child ownership")
	}
}

func TestEngineRecoverRejectsIdentity(t *testing.T) {
	tests := []struct {
		name       string
		state      store.State
		prepare    func(*testing.T, *recoveryFixture, map[string]any)
		diagnostic string
	}{
		{
			name:       "missing identity",
			prepare:    func(_ *testing.T, _ *recoveryFixture, _ map[string]any) {},
			diagnostic: "read identity",
		},
		{
			name: "malformed identity",
			prepare: func(t *testing.T, fixture *recoveryFixture, _ map[string]any) {
				if err := os.WriteFile(fixture.run.IdentityPath, []byte("{broken"), 0o600); err != nil {
					t.Fatalf("write malformed identity: %v", err)
				}
			},
			diagnostic: "unmarshal identity",
		},
		{
			name:  "malformed cleanup required identity",
			state: store.StateCleanupRequired,
			prepare: func(t *testing.T, fixture *recoveryFixture, _ map[string]any) {
				if err := os.WriteFile(fixture.run.IdentityPath, []byte("null"), 0o600); err != nil {
					t.Fatalf("write empty identity: %v", err)
				}
			},
			diagnostic: "run_id",
		},
		{name: "mismatched run ID", diagnostic: "run_id", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["run_id"] = "different-run" }},
		{name: "empty run ID", diagnostic: "run_id", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["run_id"] = "" }},
		{name: "mismatched supervisor PID", diagnostic: "supervisor_pid", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) {
			values["supervisor_pid"] = recoveryTestSupervisorPID + 10
		}},
		{name: "empty supervisor PID", diagnostic: "supervisor_pid", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["supervisor_pid"] = 0 }},
		{name: "mismatched supervisor PGID", diagnostic: "supervisor_pgid", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) {
			values["supervisor_pgid"] = recoveryTestSupervisorPGID + 10
		}},
		{name: "empty supervisor PGID", diagnostic: "supervisor_pgid", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["supervisor_pgid"] = 0 }},
		{name: "mismatched worker PID", diagnostic: "worker_pid", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) {
			values["worker_pid"] = recoveryTestWorkerPID + 10
		}},
		{name: "empty worker PID", diagnostic: "worker_pid", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["worker_pid"] = 0 }},
		{name: "mismatched token", diagnostic: "token", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["token"] = "different-token" }},
		{name: "empty token", diagnostic: "token", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["token"] = "" }},
		{name: "mismatched socket path", diagnostic: "socket_path", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) {
			values["socket_path"] = "/tmp/different.sock"
		}},
		{name: "empty socket path", diagnostic: "socket_path", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["socket_path"] = "" }},
		{name: "mismatched transcript path", diagnostic: "transcript_path", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) {
			values["transcript_path"] = "/tmp/different.ndjson"
		}},
		{name: "empty transcript path", diagnostic: "transcript_path", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) { values["transcript_path"] = "" }},
		{name: "missing transcript identity", diagnostic: "transcript identity", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) {
			delete(values, "transcript_identity")
		}},
		{name: "zero transcript identity", diagnostic: "transcript identity", prepare: func(_ *testing.T, _ *recoveryFixture, values map[string]any) {
			values["transcript_identity"] = map[string]any{"device": 0, "inode": 0}
		}},
		{name: "mismatched transcript identity", diagnostic: "transcript identity", prepare: func(_ *testing.T, fixture *recoveryFixture, values map[string]any) {
			values["transcript_identity"] = map[string]any{
				"device": fixture.run.TranscriptIdentity.Device,
				"inode":  fixture.run.TranscriptIdentity.Inode + 1,
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := tc.state
			if state == "" {
				state = store.StateRunning
			}
			fixture := newRecoveryFixture(t, state)
			values := recoveryIdentityValues(fixture.run)
			tc.prepare(t, fixture, values)
			if tc.name != "missing identity" && tc.name != "malformed identity" && tc.name != "malformed cleanup required identity" {
				writeRecoveryIdentityValues(t, fixture.run.IdentityPath, values)
			}

			recoverWithDeadline(t, fixture.engine)
			assertRecoveryRejected(t, fixture, tc.diagnostic)
		})
	}
}

func TestEngineRecoverRejectsSupervisorAuthentication(t *testing.T) {
	tests := []struct {
		name       string
		start      bool
		respond    func(*testing.T, *recoveryFixture, map[string]string) []byte
		diagnostic string
	}{
		{name: "PID alive but socket unreachable", diagnostic: "authentication"},
		{
			name:  "bad token",
			start: true,
			respond: func(t *testing.T, _ *recoveryFixture, _ map[string]string) []byte {
				return encodeRecoveryResponse(t, map[string]any{"ok": false, "error": "auth failed"})
			},
			diagnostic: "auth failed",
		},
		{
			name:       "malformed response",
			start:      true,
			respond:    func(_ *testing.T, _ *recoveryFixture, _ map[string]string) []byte { return []byte("{broken") },
			diagnostic: "authentication",
		},
		{
			name:  "empty response",
			start: true,
			respond: func(t *testing.T, _ *recoveryFixture, _ map[string]string) []byte {
				return encodeRecoveryResponse(t, map[string]any{})
			},
			diagnostic: "authentication",
		},
		{
			name:  "returned worker PID mismatch",
			start: true,
			respond: func(t *testing.T, fixture *recoveryFixture, _ map[string]string) []byte {
				resp := recoveryStatus(fixture.run)
				resp["worker_pid"] = fixture.run.WorkerPID + 1
				return encodeRecoveryResponse(t, resp)
			},
			diagnostic: "worker_pid",
		},
		{
			name:  "returned PGID mismatch",
			start: true,
			respond: func(t *testing.T, fixture *recoveryFixture, _ map[string]string) []byte {
				resp := recoveryStatus(fixture.run)
				resp["pgid"] = fixture.run.SupervisorPGID + 1
				return encodeRecoveryResponse(t, resp)
			},
			diagnostic: "pgid",
		},
		{
			name:  "returned run ID mismatch",
			start: true,
			respond: func(t *testing.T, fixture *recoveryFixture, _ map[string]string) []byte {
				resp := recoveryStatus(fixture.run)
				resp["run_id"] = "different-run"
				return encodeRecoveryResponse(t, resp)
			},
			diagnostic: "run_id",
		},
		{
			name:  "returned supervisor PID mismatch",
			start: true,
			respond: func(t *testing.T, fixture *recoveryFixture, _ map[string]string) []byte {
				resp := recoveryStatus(fixture.run)
				resp["supervisor_pid"] = fixture.run.SupervisorPID + 1
				return encodeRecoveryResponse(t, resp)
			},
			diagnostic: "supervisor_pid",
		},
		{
			name:  "returned transcript identity mismatch",
			start: true,
			respond: func(t *testing.T, fixture *recoveryFixture, _ map[string]string) []byte {
				resp := recoveryStatus(fixture.run)
				resp["transcript_inode"] = fixture.run.TranscriptIdentity.Inode + 1
				return encodeRecoveryResponse(t, resp)
			},
			diagnostic: "transcript identity",
		},
		{
			name:  "adopt rejected",
			start: true,
			respond: func(t *testing.T, fixture *recoveryFixture, req map[string]string) []byte {
				if req["cmd"] == "adopt" {
					return encodeRecoveryResponse(t, map[string]any{"ok": false, "error": "adopt refused"})
				}
				return encodeRecoveryResponse(t, recoveryStatus(fixture.run))
			},
			diagnostic: "adopt refused",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRecoveryFixture(t, store.StateRunning)
			writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
			if tc.start {
				startRecoverySocket(t, fixture.run.SocketPath, func(req map[string]string) []byte {
					return tc.respond(t, fixture, req)
				})
			}

			recoverWithDeadline(t, fixture.engine)
			assertRecoveryRejected(t, fixture, tc.diagnostic)
		})
	}
}

func assertRecoveryStateAndSignals(t *testing.T, fixture *recoveryFixture, want store.State, wantGroupCalls int) {
	t.Helper()
	run, err := fixture.engine.store.GetRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != want {
		t.Fatalf("state = %q, want %q", run.State, want)
	}
	groupCalls, signals := fixture.backend.snapshotCalls()
	if groupCalls != wantGroupCalls {
		t.Fatalf("GroupMembers calls = %d, want %d", groupCalls, wantGroupCalls)
	}
	if len(signals) != 0 {
		t.Fatalf("signals = %v, want none", signals)
	}
}

func TestEngineRecoveryUnknownAuthenticatedProgression(t *testing.T) {
	for _, target := range []store.State{
		store.StateRunning,
		store.StateCancelling,
		store.StateCleanupRequired,
	} {
		t.Run(string(target), func(t *testing.T) {
			fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
			writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
			requests := startRecoverySocket(t, fixture.run.SocketPath, func(map[string]string) []byte {
				resp := recoveryStatus(fixture.run)
				resp["state"] = string(target)
				return encodeRecoveryResponse(t, resp)
			})

			recoverWithDeadline(t, fixture.engine)

			for _, wantCmd := range []string{"status", "adopt"} {
				req := receiveRecoveryRequest(t, requests)
				if req["cmd"] != wantCmd || req["token"] != fixture.run.Token {
					t.Fatalf("request = %v, want cmd=%q token=%q", req, wantCmd, fixture.run.Token)
				}
			}
			assertRecoveryStateAndSignals(t, fixture, target, 0)
			fixture.engine.mu.Lock()
			_, locallyOwned := fixture.engine.active[fixture.run.ID]
			fixture.engine.mu.Unlock()
			if locallyOwned {
				t.Fatal("authenticated reconnect fabricated local child ownership")
			}
		})
	}
}

func TestEngineRecoveryUnknownRejectsIncoherentAdoption(t *testing.T) {
	tests := []struct {
		name  string
		adopt func(map[string]any)
	}{
		{
			name: "state disagreement",
			adopt: func(resp map[string]any) {
				resp["state"] = string(store.StateCancelling)
			},
		},
		{
			name: "identity disagreement",
			adopt: func(resp map[string]any) {
				resp["worker_pid"] = recoveryTestWorkerPID + 1
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
			writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
			requests := startRecoverySocket(t, fixture.run.SocketPath, func(req map[string]string) []byte {
				resp := recoveryStatus(fixture.run)
				if req["cmd"] == "adopt" {
					tc.adopt(resp)
				}
				return encodeRecoveryResponse(t, resp)
			})

			recoverWithDeadline(t, fixture.engine)

			if req := receiveRecoveryRequest(t, requests); req["cmd"] != "status" {
				t.Fatalf("first request = %v, want status", req)
			}
			if req := receiveRecoveryRequest(t, requests); req["cmd"] != "adopt" {
				t.Fatalf("second request = %v, want adopt", req)
			}
			assertRecoveryStateAndSignals(t, fixture, store.StateRecoveryUnknown, 0)
		})
	}
}

func TestEngineRecoveryUnknownLiveUnauthenticatedIsAmbiguous(t *testing.T) {
	fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
	writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))

	fixture.engine.tick(context.Background())

	assertRecoveryStateAndSignals(t, fixture, store.StateRecoveryUnknown, 0)
}

func TestEngineRecoveryUnknownDeadSupervisorQuiescence(t *testing.T) {
	tests := []struct {
		name           string
		workerAlive    bool
		members        []int
		groupErr       error
		wantState      store.State
		wantGroupCalls int
	}{
		{
			name:        "live worker",
			workerAlive: true,
			wantState:   store.StateRecoveryUnknown,
		},
		{
			name:           "nonempty group",
			members:        []int{recoveryTestWorkerPID + 10},
			wantState:      store.StateRecoveryUnknown,
			wantGroupCalls: 1,
		},
		{
			name:           "unowned supervisor PID remains occupancy",
			members:        []int{recoveryTestSupervisorPID},
			wantState:      store.StateRecoveryUnknown,
			wantGroupCalls: 1,
		},
		{
			name:           "group enumeration error",
			groupErr:       fmt.Errorf("membership unavailable"),
			wantState:      store.StateRecoveryUnknown,
			wantGroupCalls: 1,
		},
		{
			name:           "dead worker and empty group",
			wantState:      store.StateFailed,
			wantGroupCalls: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
			writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
			fixture.backend.alive[fixture.run.SupervisorPID] = false
			fixture.backend.alive[fixture.run.WorkerPID] = tc.workerAlive
			fixture.backend.members = tc.members
			fixture.backend.groupErr = tc.groupErr

			fixture.engine.tick(context.Background())

			assertRecoveryStateAndSignals(t, fixture, tc.wantState, tc.wantGroupCalls)
			if tc.wantState != store.StateFailed {
				if _, err := fixture.engine.store.GetOutbox(context.Background(), fixture.run.ID); !errors.Is(err, store.ErrOutboxNotFound) {
					t.Fatalf("GetOutbox error = %v, want ErrOutboxNotFound", err)
				}
				return
			}
			outbox, err := fixture.engine.store.GetOutbox(context.Background(), fixture.run.ID)
			if err != nil {
				t.Fatalf("GetOutbox: %v", err)
			}
			if outbox.Acknowledged != -1 || outbox.TerminalState != store.StateFailed || outbox.Diagnostic == "" {
				t.Fatalf("outbox acknowledged=%d terminal=%q diagnostic=%q, want abandoned failed with diagnostic", outbox.Acknowledged, outbox.TerminalState, outbox.Diagnostic)
			}
			if outbox.SupervisorPID != fixture.run.SupervisorPID || outbox.SupervisorPGID != fixture.run.SupervisorPGID || outbox.SocketPath != fixture.run.SocketPath || outbox.Token != fixture.run.Token {
				t.Fatalf("outbox identity = pid:%d pgid:%d socket:%q token:%q, want durable run identity", outbox.SupervisorPID, outbox.SupervisorPGID, outbox.SocketPath, outbox.Token)
			}
		})
	}
}

func TestEngineRecoveryUnknownProgressionIdempotent(t *testing.T) {
	fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
	writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
	fixture.backend.alive[fixture.run.SupervisorPID] = false
	fixture.backend.alive[fixture.run.WorkerPID] = false

	recoverWithDeadline(t, fixture.engine)
	for range 3 {
		recoverWithDeadline(t, fixture.engine)
		fixture.engine.tick(context.Background())
	}

	assertRecoveryStateAndSignals(t, fixture, store.StateFailed, 2)
	transitions, err := fixture.engine.store.Transitions(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	failedTransitions := 0
	for _, transition := range transitions {
		if transition.ToState == store.StateFailed {
			failedTransitions++
		}
	}
	if failedTransitions != 1 {
		t.Fatalf("failed transitions = %d, want exactly 1", failedTransitions)
	}
	outbox, err := fixture.engine.store.GetOutbox(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.Acknowledged != -1 || outbox.Diagnostic == "" {
		t.Fatalf("outbox acknowledged=%d diagnostic=%q, want abandoned with diagnostic", outbox.Acknowledged, outbox.Diagnostic)
	}
}

func TestEngineRecoveryUnknownLocalSupervisorZombieQuiesces(t *testing.T) {
	env := newEngineEnv(t)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":   "0",
		"ANANKE_FW_EXIT":     "0",
		"ANANKE_FW_DELAY_MS": "2000",
	})
	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	env.eng.mu.Lock()
	handle := env.eng.active[runID]
	env.eng.mu.Unlock()
	if handle == nil {
		t.Fatal("locally launched supervisor handle not tracked")
	}
	t.Cleanup(func() {
		if processAlive(run.WorkerPID) {
			_ = unix.Kill(run.WorkerPID, unix.SIGKILL)
			waitUntil(t, "orphan worker cleanup", 5*time.Second, func() bool {
				return !processAlive(run.WorkerPID)
			})
		}
	})

	exited := observeProcessExitForTest(t, run.SupervisorPID)
	if err := unix.Kill(run.SupervisorPID, unix.SIGKILL); err != nil {
		t.Fatalf("SIGKILL supervisor: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor NOTE_EXIT not observed")
	}

	waitRunState(t, env.store, runID, store.StateRecoveryUnknown, 10*time.Second)
	assertExactChild(t, run.SupervisorPID)
	failed := waitRunState(t, env.store, runID, store.StateFailed, 15*time.Second)
	if failed.WorkerPID != run.WorkerPID || failed.SupervisorPGID != run.SupervisorPGID {
		t.Fatalf("failed identity changed: worker=%d pgid=%d", failed.WorkerPID, failed.SupervisorPGID)
	}
	waitUntil(t, "M3 outbox resolution", 5*time.Second, func() bool {
		outbox, err := env.store.GetOutbox(context.Background(), runID)
		return err == nil && outbox.Acknowledged == -1 && outbox.TerminalState == store.StateFailed && outbox.Diagnostic != ""
	})
	waitUntil(t, "resolved local supervisor reaped", 5*time.Second, func() bool {
		env.eng.mu.Lock()
		_, tracked := env.eng.active[runID]
		env.eng.mu.Unlock()
		_, lookupErr := unix.SysctlKinfoProc("kern.proc.pid", run.SupervisorPID)
		return !tracked && handle.cmd.ProcessState != nil && lookupErr != nil
	})
}

func TestEngineRecoveryUnknownOutboxDurableAcrossFreshEngine(t *testing.T) {
	fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
	writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
	fixture.backend.alive[fixture.run.SupervisorPID] = false
	fixture.backend.alive[fixture.run.WorkerPID] = false
	recoverWithDeadline(t, fixture.engine)

	secondSocket := filepath.Join("/tmp", "ananke-m4-fresh-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	second, err := NewEngine(EngineConfig{
		StorePath:     fixture.storePath,
		SocketPath:    secondSocket,
		SupervisorBin: "/bin/true",
		DataDir:       t.TempDir(),
		Token:         "fresh-engine-token",
	})
	if err != nil {
		t.Fatalf("NewEngine fresh instance: %v", err)
	}
	second.backend = fixture.backend
	t.Cleanup(func() {
		_ = second.Close()
		_ = os.Remove(secondSocket)
	})

	for range 3 {
		recoverWithDeadline(t, second)
		second.tick(context.Background())
	}

	outbox, err := second.store.GetOutbox(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetOutbox from fresh engine: %v", err)
	}
	if outbox.Acknowledged != -1 || outbox.TerminalState != store.StateFailed || outbox.Diagnostic == "" {
		t.Fatalf("fresh engine outbox acknowledged=%d terminal=%q diagnostic=%q, want abandoned failed with diagnostic", outbox.Acknowledged, outbox.TerminalState, outbox.Diagnostic)
	}
	transitions, err := second.store.Transitions(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("Transitions from fresh engine: %v", err)
	}
	failedTransitions := 0
	for _, transition := range transitions {
		if transition.ToState == store.StateFailed {
			failedTransitions++
		}
	}
	if failedTransitions != 1 {
		t.Fatalf("failed transitions after fresh startup/ticks = %d, want exactly 1", failedTransitions)
	}
}
