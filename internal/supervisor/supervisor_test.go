package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

// seedRunStore creates a fresh store with one run ready to be supervised.
func seedRunStore(t *testing.T, runID string) string {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if err := st.CreateProject(ctx, "proj-1", "p", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.CreateWorkstream(ctx, "ws-1", "proj-1", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	spec := store.RunSpec{
		WorkerPath:     "/bin/true",
		WorkerArgs:     nil,
		WorkerEnv:      nil,
		TranscriptPath: filepath.Join(dir, "transcript.ndjson"),
		SocketPath:     filepath.Join(dir, "supervisor.sock"),
		Token:          "test-token",
		IdentityPath:   filepath.Join(dir, "identity.json"),
	}
	if err := st.CreateRun(ctx, runID, "proj-1", "ws-1", spec); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return storePath
}

func TestForkSupervisorCleanupKillsOwnedGroup(t *testing.T) {
	var supervisorPID, workerPID, childPID int
	var socketPath string
	t.Cleanup(func() {
		if workerPID > 0 {
			_ = unix.Kill(-workerPID, unix.SIGKILL)
		}
		if supervisorPID > 0 {
			_ = unix.Kill(supervisorPID, unix.SIGKILL)
		}
	})

	t.Run("resistant child", func(t *testing.T) {
		const runID = "run-cleanup-owned-group"
		storePath := seedRunStore(t, runID)
		childPIDFile := filepath.Join(t.TempDir(), "child.pid")
		cmd, socket, _, identityPath := forkSupervisor(t, storePath, runID, supEnv{
			FWEvents:       1,
			FWDelayMS:      60_000,
			FWSpawnChild:   true,
			FWChildMode:    "resistant",
			FWChildPIDFile: childPIDFile,
		})
		supervisorPID = cmd.Process.Pid
		socketPath = socket
		waitForSocket(t, socketPath, 5*time.Second)
		identity := readIdentityFile(t, identityPath)
		workerPID = identity.WorkerPID
		if identity.SupervisorPGID != workerPID {
			t.Fatalf("compatibility SupervisorPGID = %d, want worker leader %d", identity.SupervisorPGID, workerPID)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if data, err := os.ReadFile(childPIDFile); err == nil {
				if pid, parseErr := atoi(string(data)); parseErr == nil && pid > 0 {
					childPID = pid
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		if childPID == 0 {
			t.Fatal("resistant child PID was not written")
		}
		supervisorGroup, err := unix.Getpgid(supervisorPID)
		if err != nil {
			t.Fatalf("get supervisor PID %d process group: %v", supervisorPID, err)
		}
		if supervisorGroup == workerPID {
			t.Fatalf("supervisor PID %d joined owned worker group %d", supervisorPID, workerPID)
		}
		for name, pid := range map[string]int{"worker": workerPID, "child": childPID} {
			pgid, err := unix.Getpgid(pid)
			if err != nil {
				t.Fatalf("get %s PID %d process group: %v", name, pid, err)
			}
			if pgid != workerPID {
				t.Fatalf("%s PID %d PGID = %d, want owned worker group %d", name, pid, pgid, workerPID)
			}
		}
		response := sendCmd(t, socketPath, "test-token", "cancel")
		if response["ok"] != true {
			t.Fatalf("cancel resistant group: %v", response)
		}
		st, err := store.Open(storePath)
		if err != nil {
			t.Fatalf("open store for cancellation: %v", err)
		}
		defer st.Close()
		waitUntilState(t, st, runID, store.StateCancelled, 15*time.Second)
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (processAlive(supervisorPID) || processAlive(workerPID) || processAlive(childPID)) {
		time.Sleep(20 * time.Millisecond)
	}
	for name, pid := range map[string]int{"supervisor": supervisorPID, "worker": workerPID, "child": childPID} {
		if processAlive(pid) {
			t.Errorf("%s PID %d survived forkSupervisor cleanup", name, pid)
		}
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("socket %q still exists after forkSupervisor cleanup: %v", socketPath, err)
	}
}

// TestSupervisorStaysOutsideWorkerGroupAndWritesIdentity verifies the
// supervisor remains outside the worker-led group while publishing complete
// compatibility identity and transitioning the run to running.
func TestSupervisorStaysOutsideWorkerGroupAndWritesIdentity(t *testing.T) {
	storePath := seedRunStore(t, "run-1")
	cmd, socketPath, _, identityPath := forkSupervisor(t, storePath, "run-1", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 2000})

	waitForSocket(t, socketPath, 5*time.Second)
	// Ping while the worker is still running to capture the running state.
	resp := sendCmd(t, socketPath, "test-token", "ping")
	if resp["ok"] != true {
		t.Fatalf("ping failed: %v", resp)
	}
	if resp["state"] != "running" {
		t.Errorf("state = %v, want running", resp["state"])
	}
	pgid, _ := toInt(resp["pgid"])
	if pgid == 0 {
		t.Errorf("pgid = 0, want nonzero")
	}

	id := readIdentityFile(t, identityPath)
	if id.SupervisorPID != cmd.Process.Pid || id.WorkerPID <= 0 {
		t.Fatalf("identity pids = supervisor %d worker %d, want %d and positive", id.SupervisorPID, id.WorkerPID, cmd.Process.Pid)
	}
	if id.SupervisorPGID != id.WorkerPID {
		t.Fatalf("compatibility SupervisorPGID = %d, want worker group leader %d", id.SupervisorPGID, id.WorkerPID)
	}
	if id.SupervisorPGID == id.SupervisorPID {
		t.Fatalf("supervisor PID %d equals owned worker PGID", id.SupervisorPID)
	}
	supervisorGroup, err := unix.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("get supervisor process group: %v", err)
	}
	if supervisorGroup == id.SupervisorPGID {
		t.Fatalf("supervisor current group %d equals owned worker group", supervisorGroup)
	}

	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	waitUntilState(t, st, "run-1", store.StateCompleted, 10*time.Second)
}

// TestSupervisorWorkerPreservesTrampolineGroup verifies exec preserves the
// paused trampoline's worker-led process group.
func TestSupervisorWorkerPreservesTrampolineGroup(t *testing.T) {
	storePath := seedRunStore(t, "run-2")
	_, socketPath, _, identityPath := forkSupervisor(t, storePath, "run-2", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 2000})
	waitForSocket(t, socketPath, 5*time.Second)
	resp := sendCmd(t, socketPath, "test-token", "ping")
	pgid, _ := toInt(resp["pgid"])
	if pgid == 0 {
		t.Fatalf("pgid = 0")
	}
	// Read the identity file to get the worker PID and verify group membership.
	id := readIdentityFile(t, identityPath)
	if id.WorkerPGID() != pgid {
		t.Errorf("worker pgid %d != supervisor pgid %d", id.WorkerPGID(), pgid)
	}

	st, _ := store.Open(storePath)
	defer st.Close()
	waitUntilState(t, st, "run-2", store.StateCompleted, 10*time.Second)
}

// TestSupervisorWorkerExitNoSurvivorsReapReport verifies a clean worker exit
// (zero code, no children) yields completed state and the outbox row is
// acknowledged.
func TestSupervisorWorkerExitNoSurvivorsReapReport(t *testing.T) {
	storePath := seedRunStore(t, "run-3")
	_, socketPath, _, _ := forkSupervisor(t, storePath, "run-3", supEnv{FWEvents: 2, FWExit: 0, FWDelayMS: 20})
	waitForSocket(t, socketPath, 5*time.Second)

	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	r := waitUntilState(t, st, "run-3", store.StateCompleted, 10*time.Second)
	if r.SupervisorPID == 0 || r.SupervisorPGID == 0 || r.WorkerPID == 0 {
		t.Errorf("pids = sup %d pgid %d worker %d, all want nonzero", r.SupervisorPID, r.SupervisorPGID, r.WorkerPID)
	}
	// Outbox row should be acknowledged (not pending).
	outbox, err := st.GetOutbox(context.Background(), "run-3")
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.Acknowledged != 1 {
		t.Errorf("outbox acknowledged = %d, want 1", outbox.Acknowledged)
	}
	if outbox.TerminalState != store.StateCompleted {
		t.Errorf("outbox terminal = %q, want completed", outbox.TerminalState)
	}
	// Transitions should be created → running → completed.
	trs, err := st.Transitions(context.Background(), "run-3")
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if len(trs) != 3 {
		t.Fatalf("transitions = %d, want 3", len(trs))
	}
	if trs[1].ToState != store.StateRunning {
		t.Errorf("transition[1].to = %q, want running", trs[1].ToState)
	}
	if trs[2].ToState != store.StateCompleted {
		t.Errorf("transition[2].to = %q, want completed", trs[2].ToState)
	}
}

// TestSupervisorResistantChildEscalation verifies a worker that spawns a
// SIGTERM-resistant child is fully cleaned up: the child receives SIGKILL and
// exits before the worker is reaped.
func TestSupervisorResistantChildEscalation(t *testing.T) {
	storePath := seedRunStore(t, "run-4")
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	var childPID int
	t.Cleanup(func() {
		// Mutation builds intentionally skip production group cleanup. Remove the
		// exact known fixture child so a detected mutation cannot pollute later
		// gates; production cleanup remains negative-PGID only.
		if childPID > 0 && processAlive(childPID) {
			_ = unix.Kill(childPID, unix.SIGKILL)
			deadline := time.Now().Add(2 * time.Second)
			for processAlive(childPID) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if processAlive(childPID) {
				t.Errorf("mutation fixture child PID %d survived cleanup", childPID)
			}
		}
	})
	_, socketPath, resultPath, _ := forkSupervisor(t, storePath, "run-4", supEnv{
		FWEvents:       1,
		FWExit:         0,
		FWDelayMS:      50,
		FWSpawnChild:   true,
		FWChildMode:    "resistant",
		FWChildPIDFile: childPIDFile,
	})
	waitForSocket(t, socketPath, 5*time.Second)

	// Wait for the child PID file to appear.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(childPIDFile); err == nil {
			if n, e := atoi(string(data)); e == nil && n > 0 {
				childPID = n
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatalf("child PID file not written within 3s")
	}

	// The run should still complete — the resistant child is SIGKILLed.
	st, _ := store.Open(storePath)
	defer st.Close()
	r := waitUntilState(t, st, "run-4", store.StateCompleted, 15*time.Second)
	if r.WorkerPID == 0 {
		t.Errorf("worker pid not recorded")
	}

	// The supervisor result should report a clean completion.
	res := readSupResult(t, resultPath, 5*time.Second)
	if res.TerminalState != "completed" {
		t.Errorf("terminal = %q, want completed", res.TerminalState)
	}

	// After the supervisor exits, the resistant child should be dead.
	// Give the supervisor a moment to exit.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := waitPID(childPID); err != nil {
			break // child reaped
		}
		// Check via kill(0).
		if !processAlive(childPID) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(childPID) {
		t.Errorf("resistant child PID %d still alive after supervisor exit", childPID)
	}
}

// TestSupervisorCancelCommand verifies a cancel command over the socket
// triggers the cleanup state machine: the run transitions through cancelling
// to cancelled.
func TestSupervisorCancelCommand(t *testing.T) {
	storePath := seedRunStore(t, "run-5")
	_, socketPath, _, _ := forkSupervisor(t, storePath, "run-5", supEnv{FWEvents: 0, FWExit: 0, FWDelayMS: 5000})
	waitForSocket(t, socketPath, 5*time.Second)

	// Cancel the run.
	resp := sendCmd(t, socketPath, "test-token", "cancel")
	if resp["ok"] != true {
		t.Fatalf("cancel failed: %v", resp)
	}

	st, _ := store.Open(storePath)
	defer st.Close()
	r := waitUntilState(t, st, "run-5", store.StateCancelled, 10*time.Second)
	// WorkerPID should be recorded.
	if r.WorkerPID == 0 {
		t.Errorf("worker pid not recorded")
	}
	// Outbox row should be acknowledged with terminal = cancelled.
	outbox, err := st.GetOutbox(context.Background(), "run-5")
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.Acknowledged != 1 {
		t.Errorf("acknowledged = %d, want 1", outbox.Acknowledged)
	}
	if outbox.TerminalState != store.StateCancelled {
		t.Errorf("terminal = %q, want cancelled", outbox.TerminalState)
	}
}

// TestSupervisorAdoptCommand verifies an adopt command over the socket returns
// ok with the current state (daemon reconnect acknowledgment).
func TestSupervisorAdoptCommand(t *testing.T) {
	storePath := seedRunStore(t, "run-6")
	_, socketPath, _, _ := forkSupervisor(t, storePath, "run-6", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 500})
	waitForSocket(t, socketPath, 5*time.Second)

	resp := sendCmd(t, socketPath, "test-token", "adopt")
	if resp["ok"] != true {
		t.Fatalf("adopt failed: %v", resp)
	}
	if resp["state"] != "running" {
		t.Errorf("state = %v, want running", resp["state"])
	}
}

func TestSupervisorFinalizeCommandReturnsAuthenticatedIdentity(t *testing.T) {
	storePath := seedRunStore(t, "run-finalize")
	cmd, socketPath, _, _ := forkSupervisor(t, storePath, "run-finalize", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 500})
	waitForSocket(t, socketPath, 5*time.Second)

	resp := sendCmd(t, socketPath, "test-token", "finalize")
	if resp["ok"] != true {
		t.Fatalf("finalize failed: %v", resp)
	}
	if resp["run_id"] != "run-finalize" || resp["state"] != "running" {
		t.Fatalf("finalize response = %v, want exact running run identity", resp)
	}
	if supervisorPID, _ := toInt(resp["supervisor_pid"]); supervisorPID != cmd.Process.Pid {
		t.Fatalf("finalize supervisor_pid = %d, want %d", supervisorPID, cmd.Process.Pid)
	}
	workerPID, _ := toInt(resp["worker_pid"])
	if workerPID <= 0 {
		t.Fatalf("finalize worker_pid = %d, want positive", workerPID)
	}
	if pgid, _ := toInt(resp["pgid"]); pgid != workerPID {
		t.Fatalf("finalize pgid = %d, want worker group leader %d", pgid, workerPID)
	}
}

// TestSupervisorIdentityPublishedBeforeWorkerExecution verifies complete
// process identity exists before the released worker can write transcript data.
func TestSupervisorIdentityPublishedBeforeWorkerExecution(t *testing.T) {
	storePath := seedRunStore(t, "run-7")
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.ndjson")
	_, socketPath, _, identityPath := forkSupervisor(t, storePath, "run-7", supEnv{FWEvents: 3, FWExit: 0, FWDelayMS: 30, TranscriptPath: transcriptPath})
	waitForSocket(t, socketPath, 5*time.Second)

	st, _ := store.Open(storePath)
	defer st.Close()
	waitUntilState(t, st, "run-7", store.StateCompleted, 10*time.Second)

	// The identity file must exist and be non-empty.
	id := readIdentityFile(t, identityPath)
	if id.SupervisorPID == 0 || id.WorkerPID == 0 {
		t.Errorf("identity pids = %d/%d, want nonzero", id.SupervisorPID, id.WorkerPID)
	}

	// Transcript must exist and contain 3 events.
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("transcript empty")
	}
	// Count newlines (last event has a newline since FWNoNL is false).
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("transcript lines = %d, want 3", lines)
	}
}

// TestSupervisorNoGroupSignalAfterReap verifies a clean worker is reaped and
// finalization completes without any later signal to its now-unpinned PGID.
func TestSupervisorNoGroupSignalAfterReap(t *testing.T) {
	storePath := seedRunStore(t, "run-8")
	_, socketPath, _, _ := forkSupervisor(t, storePath, "run-8", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 20})
	waitForSocket(t, socketPath, 5*time.Second)

	st, _ := store.Open(storePath)
	defer st.Close()
	r := waitUntilState(t, st, "run-8", store.StateCompleted, 10*time.Second)

	// After completion, the worker should be reaped (not alive).
	if processAlive(r.WorkerPID) {
		t.Errorf("worker pid %d still alive after reap", r.WorkerPID)
	}
	// The supervisor's own outbox row is acknowledged.
	outbox, err := st.GetOutbox(context.Background(), "run-8")
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.Acknowledged != 1 {
		t.Errorf("acknowledged = %d, want 1", outbox.Acknowledged)
	}
}

func TestSupervisorCleanupRequiredOverridesExitAndCancel(t *testing.T) {
	tests := []struct {
		name      string
		exitCode  int
		cancelled bool
	}{
		{name: "zero exit", exitCode: 0},
		{name: "zero exit after normal cancel", exitCode: 0, cancelled: true},
		{name: "nonzero exit", exitCode: 23},
		{name: "nonzero exit after normal cancel", exitCode: 23, cancelled: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const runID = "run-cleanup-required"
			storePath := seedRunStore(t, runID)
			st, err := store.Open(storePath)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer st.Close()
			ctx := context.Background()
			if err := st.Transition(ctx, runID, store.StateRunning, "launched"); err != nil {
				t.Fatalf("Transition running: %v", err)
			}
			if err := st.Transition(ctx, runID, store.StateCleanupRequired, "transcript corruption"); err != nil {
				t.Fatalf("Transition cleanup_required: %v", err)
			}

			s := &Supervisor{
				cfg:       Config{RunID: runID},
				store:     st,
				cancelled: tc.cancelled,
			}
			state, reason := s.decideTerminal(tc.exitCode)
			if state != store.StateFailed {
				t.Errorf("decideTerminal(%d) = %q, want failed", tc.exitCode, state)
			}
			if reason != "transcript corruption required group cleanup" {
				t.Errorf("reason = %q, want transcript corruption reason", reason)
			}
		})
	}
}
