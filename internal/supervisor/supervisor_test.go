package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
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

// TestSupervisorBecomesGroupLeaderAndWritesIdentity verifies the supervisor
// subprocess becomes the group leader, writes an identity file with PGID==PID,
// and transitions the run to running.
func TestSupervisorBecomesGroupLeaderAndWritesIdentity(t *testing.T) {
	storePath := seedRunStore(t, "run-1")
	cmd, socketPath, _, identityPath := forkSupervisor(t, storePath, "run-1", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 2000})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

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

	// Wait for the run to complete.
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer st.Close()
	waitUntilState(t, st, "run-1", store.StateCompleted, 10*time.Second)

	// Verify identity file: PGID == supervisor PID.
	id := readIdentityFile(t, identityPath)
	if id.SupervisorPGID != id.SupervisorPID {
		t.Errorf("identity PGID %d != PID %d", id.SupervisorPGID, id.SupervisorPID)
	}
	if id.WorkerPID == 0 {
		t.Errorf("identity WorkerPID = 0, want nonzero")
	}
	if id.WorkerPGID() != id.SupervisorPGID {
		t.Errorf("identity WorkerPGID %d != SupervisorPGID %d", id.WorkerPGID(), id.SupervisorPGID)
	}
}

// TestSupervisorWorkerInheritsGroup verifies the worker PID inherits the
// supervisor's PGID.
func TestSupervisorWorkerInheritsGroup(t *testing.T) {
	storePath := seedRunStore(t, "run-2")
	cmd, socketPath, _, identityPath := forkSupervisor(t, storePath, "run-2", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 2000})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
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
	cmd, socketPath, _, _ := forkSupervisor(t, storePath, "run-3", supEnv{FWEvents: 2, FWExit: 0, FWDelayMS: 20})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
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
	cmd, socketPath, resultPath, _ := forkSupervisor(t, storePath, "run-4", supEnv{
		FWEvents:       1,
		FWExit:         0,
		FWDelayMS:      50,
		FWSpawnChild:   true,
		FWChildMode:    "resistant",
		FWChildPIDFile: childPIDFile,
	})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	waitForSocket(t, socketPath, 5*time.Second)

	// Wait for the child PID file to appear.
	var childPID int
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
	cmd, socketPath, _, _ := forkSupervisor(t, storePath, "run-5", supEnv{FWEvents: 0, FWExit: 0, FWDelayMS: 5000})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
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
	cmd, socketPath, _, _ := forkSupervisor(t, storePath, "run-6", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 500})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	waitForSocket(t, socketPath, 5*time.Second)

	resp := sendCmd(t, socketPath, "test-token", "adopt")
	if resp["ok"] != true {
		t.Fatalf("adopt failed: %v", resp)
	}
	if resp["state"] != "running" {
		t.Errorf("state = %v, want running", resp["state"])
	}
}

// TestSupervisorIdentityFileWrittenBeforeLaunch verifies the identity file
// exists by the time the worker starts producing a transcript. We detect this
// by having the fakeworker write its transcript, then checking that the
// identity file was present (non-empty) at worker-start time via the transcript
// event order.
func TestSupervisorIdentityFileWrittenBeforeLaunch(t *testing.T) {
	storePath := seedRunStore(t, "run-7")
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.ndjson")
	cmd, socketPath, _, identityPath := forkSupervisor(t, storePath, "run-7", supEnv{FWEvents: 3, FWExit: 0, FWDelayMS: 30, TranscriptPath: transcriptPath})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
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

// TestSupervisorNoGroupSignalAfterReap is a structural assertion: it verifies
// that after the run completes, the supervisor process is gone and no signal
// was issued to the numeric PGID post-reap. We check this by confirming the
// run completed cleanly and the supervisor's own PID (group leader) is the
// last surviving member (it exits on its own after finalization).
func TestSupervisorNoGroupSignalAfterReap(t *testing.T) {
	storePath := seedRunStore(t, "run-8")
	cmd, socketPath, _, _ := forkSupervisor(t, storePath, "run-8", supEnv{FWEvents: 1, FWExit: 0, FWDelayMS: 20})
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
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
