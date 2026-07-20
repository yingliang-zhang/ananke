package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

// recordingBackend wraps a LifecycleBackend and records both method order and
// whether the exact worker PID is still this process's child when group
// cleanup begins.
type recordingBackend struct {
	inner                  lifecycle.LifecycleBackend
	mu                     sync.Mutex
	callLog                []string
	sigPGIDs               []int
	workerPID              int
	workerPresentAtCleanup []bool
	workerProbeErr         []error
}

func (r *recordingBackend) LaunchWorker(path string, args []string, env []string) (int, error) {
	pid, err := r.inner.LaunchWorker(path, args, env)
	if err == nil {
		r.mu.Lock()
		r.workerPID = pid
		r.mu.Unlock()
	}
	return pid, err
}
func (r *recordingBackend) ReleaseWorker(pid int) error {
	return r.inner.ReleaseWorker(pid)
}
func (r *recordingBackend) WorkerExited(pid int) (<-chan struct{}, error) {
	return r.inner.WorkerExited(pid)
}
func (r *recordingBackend) ReapWorker(pid int) (int, error) {
	r.mu.Lock()
	r.callLog = append(r.callLog, "reap")
	r.mu.Unlock()
	return r.inner.ReapWorker(pid)
}
func (r *recordingBackend) GroupMembers(pgid int) ([]int, error) {
	r.mu.Lock()
	pid := r.workerPID
	r.mu.Unlock()
	proc, probeErr := unix.SysctlKinfoProc("kern.proc.pid", pid)
	present := probeErr == nil && int(proc.Proc.P_pid) == pid && int(proc.Eproc.Ppid) == os.Getpid()
	r.mu.Lock()
	r.callLog = append(r.callLog, "group_members")
	r.workerPresentAtCleanup = append(r.workerPresentAtCleanup, present)
	r.workerProbeErr = append(r.workerProbeErr, probeErr)
	r.mu.Unlock()
	return r.inner.GroupMembers(pgid)
}
func (r *recordingBackend) SignalGroup(pgid int, sig lifecycle.Signal) error {
	r.mu.Lock()
	r.callLog = append(r.callLog, "signal_group")
	r.sigPGIDs = append(r.sigPGIDs, pgid)
	r.mu.Unlock()
	return r.inner.SignalGroup(pgid, sig)
}
func (r *recordingBackend) ProcessAlive(pid int) bool {
	return r.inner.ProcessAlive(pid)
}

// TestMutationReapBeforeCleanupOrder verifies the real kernel safety property:
// the exact exited worker remains our reapable child when group cleanup begins.
// The mutation performs Wait4 first, so the PID is absent at that observation.
func TestMutationReapBeforeCleanupOrder(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "test.sqlite")
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.CreateProject(ctx, "proj-1", "test", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.CreateWorkstream(ctx, "ws-1", "proj-1", "test"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}

	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity.json")
	childPIDPath := filepath.Join(dir, "child.pid")
	var childPID int
	t.Cleanup(func() {
		// A mutation intentionally violates production cleanup. Keep its exact
		// known fixture child from escaping into subsequent mutation/gate runs.
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
	socketPath := filepath.Join("/tmp", "ananke-mut-test.sock")
	defer os.Remove(socketPath)

	// Use the real Darwin backend but wrap it with recording.
	real := lifecycle.NewDarwinBackend()
	rec := &recordingBackend{inner: real}

	cfg := Config{
		StorePath:    storePath,
		RunID:        "run-mut-1",
		WorkerPath:   os.Args[0],
		WorkerEnv:    []string{"ANANKE_FW_HELPER=fakeworker", "ANANKE_FW_EVENTS=1", "ANANKE_FW_EXIT=0", "ANANKE_FW_DELAY_MS=100", "ANANKE_FW_SPAWN_CHILD=1", "ANANKE_FW_CHILD_MODE=resistant", "ANANKE_FW_CHILD_PID_FILE=" + childPIDPath},
		IdentityPath: identityPath,
		SocketPath:   socketPath,
		Token:        "test-token",
		GracePeriod:  500 * time.Millisecond,
		Backend:      rec,
	}
	if err := st.CreateRun(ctx, "run-mut-1", "proj-1", "ws-1", store.RunSpec{
		WorkerPath:     os.Args[0],
		TranscriptPath: filepath.Join(dir, "transcript.ndjson"),
		SocketPath:     socketPath,
		Token:          "test-token",
		IdentityPath:   identityPath,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	s, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	terminal, err := s.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if terminal != store.StateCompleted {
		t.Fatalf("terminal = %q, want completed", terminal)
	}
	if data, err := os.ReadFile(childPIDPath); err == nil {
		childPID, _ = atoi(string(data))
	}

	// The first group enumeration is cleanup's first OS-facing operation. The
	// exited worker must still exist as this process's exact child at that point.
	rec.mu.Lock()
	log := append([]string(nil), rec.callLog...)
	workerPID := rec.workerPID
	presentAtCleanup := append([]bool(nil), rec.workerPresentAtCleanup...)
	probeErrs := append([]error(nil), rec.workerProbeErr...)
	rec.mu.Unlock()
	if len(presentAtCleanup) == 0 {
		t.Fatalf("group cleanup never observed worker PID %d (log: %v)", workerPID, log)
	}
	if probeErrs[0] != nil || !presentAtCleanup[0] {
		t.Fatalf("worker PID %d was reaped before group cleanup (present=%v, probe_err=%v, log=%v)",
			workerPID, presentAtCleanup[0], probeErrs[0], log)
	}
	reapIdx := -1
	groupIdx := -1
	for i, call := range log {
		if call == "reap" && reapIdx == -1 {
			reapIdx = i
		}
		if call == "group_members" && groupIdx == -1 {
			groupIdx = i
		}
	}

	if reapIdx == -1 {
		t.Fatalf("ReapWorker was never called (log: %v)", log)
	}
	if groupIdx == -1 {
		t.Fatalf("GroupMembers was never called (log: %v)", log)
	}
	if groupIdx > reapIdx {
		t.Errorf("GroupMembers (idx %d) called AFTER ReapWorker (idx %d) — order violation", groupIdx, reapIdx)
	}
}

// TestMutationNoGroupSignalAfterReap verifies that no signal is issued to the
// numeric PGID after the worker is reaped. With the mutation_signal_after_reap
// tag, a group SIGTERM is sent after reap and this test detects it by checking
// that the supervisor itself is not killed by a post-reap group signal.
//
// We detect this by checking that the supervisor process is still alive
// briefly after the run completes — if a group SIGTERM was sent after reap,
// it might hit the supervisor itself (though it ignores SIGTERM, we verify
// the run completed cleanly without any unexpected signal-related issues).
//
// More directly: we verify that the run reaches completed (not killed by
// a stray group signal) and the exit code is 0.
func TestMutationNoGroupSignalAfterReap(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "test.sqlite")
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.CreateProject(ctx, "proj-m3", "test", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.CreateWorkstream(ctx, "ws-m3", "proj-m3", "test"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}

	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity.json")
	socketPath := filepath.Join("/tmp", "ananke-mut-m3.sock")
	defer os.Remove(socketPath)

	real := lifecycle.NewDarwinBackend()
	rec := &recordingBackend{inner: real}

	cfg := Config{
		StorePath:    storePath,
		RunID:        "run-mut-m3",
		WorkerPath:   os.Args[0],
		WorkerEnv:    []string{"ANANKE_FW_HELPER=fakeworker", "ANANKE_FW_EVENTS=1", "ANANKE_FW_EXIT=0", "ANANKE_FW_DELAY_MS=100"},
		IdentityPath: identityPath,
		SocketPath:   socketPath,
		Token:        "test-token",
		GracePeriod:  500 * time.Millisecond,
		Backend:      rec,
	}

	if err := st.CreateRun(ctx, "run-mut-m3", "proj-m3", "ws-m3", store.RunSpec{
		WorkerPath:     os.Args[0],
		TranscriptPath: filepath.Join(dir, "transcript.ndjson"),
		SocketPath:     socketPath,
		Token:          "test-token",
		IdentityPath:   identityPath,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	s, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	terminal, err := s.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if terminal != store.StateCompleted {
		t.Fatalf("terminal = %q, want completed", terminal)
	}

	rec.mu.Lock()
	log := append([]string(nil), rec.callLog...)
	rec.mu.Unlock()
	reaped := false
	for _, call := range log {
		if call == "reap" {
			reaped = true
		}
		if call == "signal_group" && reaped {
			t.Fatalf("SignalGroup called after exact worker reap: %v", log)
		}
	}
}
