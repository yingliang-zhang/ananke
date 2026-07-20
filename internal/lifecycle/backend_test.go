package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProcessExitWatcherTerminalErrorClosesReadiness(t *testing.T) {
	kq, err := unix.Kqueue()
	if err != nil {
		t.Fatalf("Kqueue: %v", err)
	}
	w := &processExitWatcher{exited: make(chan struct{})}
	if err := unix.Close(kq); err != nil {
		t.Fatalf("close kqueue: %v", err)
	}
	go w.observe(kq)
	select {
	case <-w.exited:
	case <-time.After(time.Second):
		t.Fatal("watcher readiness remained open after terminal kevent error")
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
	if err := b.ReleaseWorker(pid); err != nil {
		t.Fatalf("ReleaseWorker: %v", err)
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

func TestLaunchWorkerStartsPausedDistinctGroupLeader(t *testing.T) {
	b := NewDarwinBackend()
	resultPath := filepath.Join(t.TempDir(), "paused-worker.json")
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + resultPath,
		"ANANKE_SLEEP_MS=50",
		"ANANKE_EXIT=0",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = unix.Kill(pid, unix.SIGKILL)
			_, _ = b.ReapWorker(pid)
		}
	})
	workerPGID, err := unix.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", pid, err)
	}
	if workerPGID != pid {
		t.Fatalf("worker PGID = %d, want group-leading PID %d", workerPGID, pid)
	}
	if workerPGID == unix.Getpgrp() {
		t.Fatalf("worker PGID %d equals supervisor/test group %d", workerPGID, unix.Getpgrp())
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(resultPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worker executed before release: stat error %v", err)
	}
	releaser, ok := any(b).(interface{ ReleaseWorker(int) error })
	if !ok {
		t.Fatal("backend has no ReleaseWorker barrier")
	}
	if err := releaser.ReleaseWorker(pid); err != nil {
		t.Fatalf("ReleaseWorker: %v", err)
	}
	var result struct {
		PID  int `json:"pid"`
		PGID int `json:"pgid"`
	}
	readResultFile(t, resultPath, 5*time.Second, &result)
	if result.PID != pid || result.PGID != pid {
		t.Fatalf("released worker identity = pid %d pgid %d, want %d/%d", result.PID, result.PGID, pid, pid)
	}
	if _, err := b.ReapWorker(pid); err != nil {
		t.Fatalf("ReapWorker: %v", err)
	}
	reaped = true
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
	if err := b.ReleaseWorker(pid); err != nil {
		t.Fatalf("ReleaseWorker: %v", err)
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

// TestWorkerExitedDeferredReap proves exit observation does not consume the
// child's wait status. The exited PID must remain our child until ReapWorker
// performs the sole wait and returns the real exit code.
func TestWorkerExitedDeferredReap(t *testing.T) {
	b := NewDarwinBackend()
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + filepath.Join(t.TempDir(), "worker.json"),
		"ANANKE_SLEEP_MS=50",
		"ANANKE_EXIT=42",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	if err := b.ReleaseWorker(pid); err != nil {
		t.Fatalf("ReleaseWorker: %v", err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = unix.Kill(pid, unix.SIGKILL)
			_, _ = b.ReapWorker(pid)
		}
	})

	exited, err := b.WorkerExited(pid)
	if err != nil {
		t.Fatalf("WorkerExited: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("WorkerExited channel did not close within 5s")
	}

	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		t.Fatalf("worker PID %d disappeared before ReapWorker: %v", pid, err)
	}
	if got := int(proc.Proc.P_pid); got != pid {
		t.Fatalf("observed PID = %d, want exact worker PID %d", got, pid)
	}
	if got := int(proc.Eproc.Ppid); got != os.Getpid() {
		t.Fatalf("worker PID %d parent = %d, want test process %d", pid, got, os.Getpid())
	}

	exitCode, err := b.ReapWorker(pid)
	if err != nil {
		t.Fatalf("ReapWorker: %v", err)
	}
	reaped = true
	if exitCode != 42 {
		t.Fatalf("exitCode = %d, want 42", exitCode)
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
	if err := b.ReleaseWorker(pid); err != nil {
		t.Fatalf("ReleaseWorker: %v", err)
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
	if err := b.SignalGroup(pid, unix.SIGKILL); err != nil {
		t.Fatalf("SignalGroup: %v", err)
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
	if err := b.ReleaseWorker(pid); err != nil {
		t.Fatalf("ReleaseWorker: %v", err)
	}
	if _, err := b.ReapWorker(pid); err != nil {
		t.Fatalf("ReapWorker: %v", err)
	}
	if b.ProcessAlive(pid) {
		t.Errorf("ProcessAlive(%d) = true after reap, want false", pid)
	}
}

func TestLaunchWorkerWatcherFailureRetainsPositiveOwnedPID(t *testing.T) {
	b := NewDarwinBackend()
	b.newExitWatcher = func(int) (*processExitWatcher, error) {
		return nil, errors.New("injected watcher construction failure")
	}
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + filepath.Join(t.TempDir(), "worker.json"),
		"ANANKE_SLEEP_MS=10000",
		"ANANKE_EXIT=0",
	})
	if err == nil {
		t.Fatal("LaunchWorker error = nil, want watcher failure")
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want positive owned child despite watcher failure", pid)
	}
	if err := b.SignalGroup(pid, unix.SIGKILL); err != nil {
		t.Fatalf("SignalGroup: %v", err)
	}
	if _, err := b.ReapWorker(pid); err != nil {
		t.Fatalf("ReapWorker after watcher failure: %v", err)
	}
	if b.ProcessAlive(pid) {
		t.Fatalf("worker %d alive after explicit cleanup and reap", pid)
	}
}

func TestReapWorkerRetriesWait4EINTR(t *testing.T) {
	b := NewDarwinBackend()
	waitCalls := 0
	b.wait4 = func(pid int, status *unix.WaitStatus, options int, rusage *unix.Rusage) (int, error) {
		waitCalls++
		if waitCalls == 1 {
			return 0, unix.EINTR
		}
		return unix.Wait4(pid, status, options, rusage)
	}
	pid, err := b.LaunchWorker(os.Args[0], nil, []string{
		helperEnv + "=worker",
		resultEnv + "=" + filepath.Join(t.TempDir(), "worker.json"),
		"ANANKE_SLEEP_MS=25",
		"ANANKE_EXIT=42",
	})
	if err != nil {
		t.Fatalf("LaunchWorker: %v", err)
	}
	if err := b.ReleaseWorker(pid); err != nil {
		t.Fatalf("ReleaseWorker: %v", err)
	}
	exitCode, err := b.ReapWorker(pid)
	if err != nil {
		t.Fatalf("ReapWorker: %v", err)
	}
	if waitCalls != 2 {
		t.Fatalf("Wait4 calls = %d, want EINTR retry", waitCalls)
	}
	if exitCode != 42 {
		t.Fatalf("exitCode = %d, want 42", exitCode)
	}
}
