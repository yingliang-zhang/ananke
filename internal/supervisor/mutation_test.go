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
)

// recordingBackend wraps a LifecycleBackend and records the order of
// ReapWorker and GroupMembers calls.
type recordingBackend struct {
	inner   lifecycle.LifecycleBackend
	mu      sync.Mutex
	callLog []string
	sigPIDs []int // PIDs passed to SignalProcess (negative = group signal)
}

func (r *recordingBackend) BecomeGroupLeader() (int, error) {
	return r.inner.BecomeGroupLeader()
}
func (r *recordingBackend) LaunchWorker(path string, args []string, env []string) (int, error) {
	return r.inner.LaunchWorker(path, args, env)
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
	r.callLog = append(r.callLog, "group_members")
	r.mu.Unlock()
	return r.inner.GroupMembers(pgid)
}
func (r *recordingBackend) SignalProcess(pid int, sig lifecycle.Signal) error {
	r.mu.Lock()
	r.callLog = append(r.callLog, "signal")
	r.sigPIDs = append(r.sigPIDs, pid)
	r.mu.Unlock()
	return r.inner.SignalProcess(pid, sig)
}
func (r *recordingBackend) ProcessAlive(pid int) bool {
	return r.inner.ProcessAlive(pid)
}

// TestMutationReapBeforeCleanupOrder verifies that cleanupGroup (GroupMembers)
// is called BEFORE ReapWorker. With the mutation_reap_before_cleanup tag,
// the order is reversed and this test fails.
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
	socketPath := filepath.Join("/tmp", "ananke-mut-test.sock")
	defer os.Remove(socketPath)

	// Use the real Darwin backend but wrap it with recording.
	real := lifecycle.NewDarwinBackend()
	rec := &recordingBackend{inner: real}

	cfg := Config{
		StorePath:    storePath,
		RunID:        "run-mut-1",
		WorkerPath:   os.Args[0],
		WorkerEnv:    []string{"ANANKE_FW_HELPER=fakeworker", "ANANKE_FW_EVENTS=1", "ANANKE_FW_EXIT=0", "ANANKE_FW_DELAY_MS=100", "ANANKE_FW_SPAWN_CHILD=1", "ANANKE_FW_CHILD_MODE=resistant"},
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

	// Verify call order: group_members must appear BEFORE reap.
	// With the mutation, reap appears first.
	rec.mu.Lock()
	log := rec.callLog
	rec.mu.Unlock()

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

	// Check that no SignalProcess call used a negative PID (group signal).
	// With the mutation, a group SIGTERM is sent after reap via
	// SignalProcess(-pgid, SIGTERM).
	rec.mu.Lock()
	sigPIDs := rec.sigPIDs
	rec.mu.Unlock()

	for _, pid := range sigPIDs {
		if pid < 0 {
			t.Errorf("SignalProcess called with negative PID %d (group signal after reap)", pid)
		}
	}
}
