package lifecycle

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func newShutdownTestEngine(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	socketPath := filepath.Join("/tmp", "ananke-shutdown-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	engine, err := NewEngine(EngineConfig{
		StorePath:     filepath.Join(dir, "store.sqlite"),
		SocketPath:    socketPath,
		SupervisorBin: "/bin/true",
		DataDir:       filepath.Join(dir, "data"),
		Token:         "shutdown-token",
		TickInterval:  time.Hour,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

func requireStoreClosed(t *testing.T, engine *Engine) {
	t.Helper()
	if _, err := engine.store.SchemaVersion(context.Background()); err == nil {
		t.Fatal("store query succeeded after Engine.Close")
	}
}

func TestEngineCloseJoinsBlockedRecoveryBeforeStoreClose(t *testing.T) {
	engine := newShutdownTestEngine(t)
	engine.recoveryMu.Lock()
	recoveryLocked := true
	defer func() {
		if recoveryLocked {
			engine.recoveryMu.Unlock()
		}
	}()

	runDone := make(chan error, 1)
	go func() { runDone <- engine.Run(context.Background()) }()
	waitUntil(t, "Run entered recovery", time.Second, func() bool {
		engine.mu.Lock()
		defer engine.mu.Unlock()
		return engine.running
	})

	closeDone := make(chan error, 1)
	go func() { closeDone <- engine.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before blocked recovery joined: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := engine.store.SchemaVersion(context.Background()); err != nil {
		t.Fatalf("store closed while recovery was blocked: %v", err)
	}

	engine.recoveryMu.Unlock()
	recoveryLocked = false
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not join cancelled recovery")
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Close")
	}
	requireStoreClosed(t, engine)
}

func TestEngineCloseUnblocksAndJoinsAPIHandler(t *testing.T) {
	engine := newShutdownTestEngine(t)
	runDone := make(chan error, 1)
	go func() { runDone <- engine.Run(context.Background()) }()
	waitForEngineSocket(t, engine.cfg.SocketPath, time.Second)

	conn, err := net.Dial("unix", engine.cfg.SocketPath)
	if err != nil {
		t.Fatalf("dial engine: %v", err)
	}
	defer conn.Close()
	waitUntil(t, "API connection tracked", time.Second, func() bool {
		engine.mu.Lock()
		defer engine.mu.Unlock()
		return len(engine.connections) == 1
	})

	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not join Close")
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("blocked API connection remained open after Close")
	}
	engine.mu.Lock()
	connections := len(engine.connections)
	engine.mu.Unlock()
	if connections != 0 {
		t.Fatalf("tracked API connections after Close = %d, want 0", connections)
	}
	requireStoreClosed(t, engine)
}

func TestEngineCloseCancelsAndJoinsCleanupDelivery(t *testing.T) {
	engine := newShutdownTestEngine(t)
	ctx := context.Background()
	if err := engine.store.CreateProject(ctx, "p", "p", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := engine.store.CreateWorkstream(ctx, "w", "p", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	if err := engine.store.CreateRun(ctx, "r", "p", "w", store.RunSpec{
		WorkerPath: "/bin/true",
		SocketPath: "/tmp/supervisor.sock",
		Token:      "supervisor-token",
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := engine.store.Transition(ctx, "r", store.StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}

	started := make(chan struct{})
	stopped := make(chan struct{})
	engine.mu.Lock()
	engine.cleanupDelivery = func(ctx context.Context, _, _ string) (supCmdResponse, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return supCmdResponse{}, ctx.Err()
	}
	engine.mu.Unlock()
	response := engine.handleCancelRun(ctx, &apiRequest{ID: "r"})
	if !response.OK || !response.Accepted {
		t.Fatalf("cancel response = %+v, want accepted", response)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("cleanup delivery did not start")
	}

	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel blocked cleanup delivery")
	}
	engine.mu.Lock()
	_, requestedBefore := engine.cleanupRequested["r"]
	engine.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	engine.mu.Lock()
	_, requestedAfter := engine.cleanupRequested["r"]
	connections := len(engine.connections)
	engine.mu.Unlock()
	if requestedBefore != requestedAfter || requestedAfter || connections != 0 {
		t.Fatalf("post-close map state changed: cleanup before=%t after=%t connections=%d", requestedBefore, requestedAfter, connections)
	}
	requireStoreClosed(t, engine)
}

func TestEngineCloseClosesTranscriptTails(t *testing.T) {
	engine := newShutdownTestEngine(t)
	transcriptPath := filepath.Join(t.TempDir(), "transcript.ndjson")
	if err := os.WriteFile(transcriptPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	ctx := context.Background()
	if err := engine.store.CreateProject(ctx, "tail-project", "tail", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := engine.store.CreateWorkstream(ctx, "tail-workstream", "tail-project", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	if err := engine.store.CreateRun(ctx, "tail-run", "tail-project", "tail-workstream", store.RunSpec{
		WorkerPath:         "/bin/true",
		TranscriptPath:     transcriptPath,
		TranscriptRequired: true,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	persistTranscriptIdentity(t, engine.store, "tail-run", transcriptPath)
	if err := engine.store.Transition(ctx, "tail-run", store.StateRunning, "fixture"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	engine.startTailing(context.Background(), "tail-run", transcriptPath, 0)
	engine.mu.Lock()
	tail := engine.tails["tail-run"]
	engine.mu.Unlock()
	if tail == nil {
		t.Fatal("transcript tail was not opened")
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := tail.file.Stat(); err == nil {
		t.Fatal("transcript file remained open after Close")
	}
	requireStoreClosed(t, engine)
}

func TestEngineCloseConcurrentCallsAreIdempotent(t *testing.T) {
	engine := newShutdownTestEngine(t)
	const callers = 8
	results := make(chan error, callers)
	for range callers {
		go func() { results <- engine.Close() }()
	}
	for range callers {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("concurrent Close: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent Close did not return")
		}
	}
	requireStoreClosed(t, engine)
}
