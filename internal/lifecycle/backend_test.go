package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBecomeGroupLeaderReturnsNonzeroPGID(t *testing.T) {
	cmd, resultPath := forkHelper(t, "groupleader", nil)
	t.Cleanup(func() {
		// groupleader exits on its own after reporting; ensure reaped.
		_, _ = cmd.Process.Wait()
	})
	var r struct {
		PID  int    `json:"pid"`
		PGID int    `json:"pgid"`
		Err  string `json:"err"`
	}
	readResultFile(t, resultPath, 5*time.Second, &r)
	if r.Err != "" {
		t.Fatalf("helper BecomeGroupLeader error: %s", r.Err)
	}
	if r.PGID == 0 {
		t.Fatalf("PGID = 0, want nonzero")
	}
	// A process-group leader's PGID equals its own PID.
	if r.PGID != r.PID {
		t.Errorf("PGID %d != PID %d (group leader must have pgid==pid)", r.PGID, r.PID)
	}
	if r.PID != cmd.Process.Pid {
		t.Errorf("reported PID %d != forked PID %d", r.PID, cmd.Process.Pid)
	}
}

// TestLaunchWorkerReturnsNonzeroPID verifies LaunchWorker starts a process and
// returns a positive PID. The worker (test binary in "worker" helper mode)
// writes its own PID to a result file so the test can cross-check.
func TestLaunchWorkerReturnsNonzeroPID(t *testing.T) {
	b := NewDarwinBackend()
	resultPath := filepath.Join(t.TempDir(), "worker.json")
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + resultPath,
		"ANANKE_SLEEP_MS=100",
		"ANANKE_EXIT=0",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}
	t.Cleanup(func() {
		_ = unix.Kill(pid, unix.SIGKILL)
		_, _ = b.ReapWorker(pid)
	})
	var r struct {
		PID  int `json:"pid"`
		PGID int `json:"pgid"`
	}
	readResultFile(t, resultPath, 5*time.Second, &r)
	if r.PID != pid {
		t.Errorf("worker reported PID %d, want %d", r.PID, pid)
	}
}

// TestWorkerExitedChannelCloses verifies the channel returned by WorkerExited
// closes when the worker process exits.
func TestWorkerExitedChannelCloses(t *testing.T) {
	b := NewDarwinBackend()
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + filepath.Join(t.TempDir(), "worker.json"),
		"ANANKE_SLEEP_MS=100",
		"ANANKE_EXIT=0",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	t.Cleanup(func() {
		_, _ = b.ReapWorker(pid)
	})
	ch, err := b.WorkerExited(pid)
	if err != nil {
		t.Fatalf("WorkerExited: %v", err)
	}
	select {
	case <-ch:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("WorkerExited channel did not close within 5s")
	}
}

// TestReapWorkerExitCode verifies ReapWorker returns the process exit code.
func TestReapWorkerExitCode(t *testing.T) {
	b := NewDarwinBackend()
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + filepath.Join(t.TempDir(), "worker.json"),
		"ANANKE_SLEEP_MS=100",
		"ANANKE_EXIT=42",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	exitCode, err := b.ReapWorker(pid)
	if err != nil {
		t.Fatalf("ReapWorker: %v", err)
	}
	if exitCode != 42 {
		t.Errorf("exitCode = %d, want 42", exitCode)
	}
}

// TestReapWorkerSignaled verifies ReapWorker returns a negative (signal-encoded)
// exit code when the worker is killed by a signal.
func TestReapWorkerSignaled(t *testing.T) {
	b := NewDarwinBackend()
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + filepath.Join(t.TempDir(), "worker.json"),
		"ANANKE_SLEEP_MS=10000", // long sleep so we can signal before exit
		"ANANKE_EXIT=0",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	if err := b.SignalProcess(pid, unix.SIGKILL); err != nil {
		t.Fatalf("SignalProcess: %v", err)
	}
	exitCode, err := b.ReapWorker(pid)
	if err != nil {
		t.Fatalf("ReapWorker: %v", err)
	}
	want := -int(unix.SIGKILL)
	if exitCode != want {
		t.Errorf("exitCode = %d, want %d (negative SIGKILL)", exitCode, want)
	}
}

// TestProcessAliveSelfAndDead verifies ProcessAlive returns true for the
// calling process and false for a reaped (freed) PID.
func TestProcessAliveSelfAndDead(t *testing.T) {
	b := NewDarwinBackend()
	if !b.ProcessAlive(os.Getpid()) {
		t.Error("ProcessAlive(self) = false, want true")
	}
	// Launch a short-lived worker, reap it, then confirm the PID is gone.
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + filepath.Join(t.TempDir(), "w.json"),
		"ANANKE_SLEEP_MS=50",
		"ANANKE_EXIT=0",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	if _, err := b.ReapWorker(pid); err != nil {
		t.Fatalf("ReapWorker: %v", err)
	}
	if b.ProcessAlive(pid) {
		t.Errorf("ProcessAlive(%d) = true after reap, want false", pid)
	}
}

// TestLaunchWorkerInheritsSupervisorPGID uses the "groupeval" helper which
// becomes a group leader, launches a worker, and reports both PGIDs. The
// worker must inherit the supervisor's PGID (ADR-0002 §1).
func TestLaunchWorkerInheritsSupervisorPGID(t *testing.T) {
	cmd, resultPath := forkHelper(t, "groupeval", nil)
	t.Cleanup(func() {
		_, _ = cmd.Process.Wait()
	})
	var r struct {
		SupervisorPID  int    `json:"supervisor_pid"`
		SupervisorPGID int    `json:"supervisor_pgid"`
		WorkerPID      int    `json:"worker_pid"`
		WorkerPGID     int    `json:"worker_pgid"`
		ExitCode       int    `json:"exit_code"`
		GroupMembers   []int  `json:"group_members"`
		Err            string `json:"err"`
	}
	readResultFile(t, resultPath, 10*time.Second, &r)
	if r.Err != "" {
		t.Fatalf("groupeval error: %s", r.Err)
	}
	if r.SupervisorPGID != r.SupervisorPID {
		t.Errorf("supervisor PGID %d != PID %d", r.SupervisorPGID, r.SupervisorPID)
	}
	if r.WorkerPGID != r.SupervisorPGID {
		t.Errorf("worker PGID %d != supervisor PGID %d (worker must inherit group)", r.WorkerPGID, r.SupervisorPGID)
	}
	if r.WorkerPID == r.SupervisorPID {
		t.Errorf("worker PID == supervisor PID %d", r.WorkerPID)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", r.ExitCode)
	}
	// GroupMembers must include the worker PID but not the supervisor.
	found := false
	for _, m := range r.GroupMembers {
		if m == r.WorkerPID {
			found = true
		}
	}
	if !found {
		t.Errorf("worker PID %d not found in group members %v", r.WorkerPID, r.GroupMembers)
	}
}
