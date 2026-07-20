package lifecycle

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestAcceptedCancellationSurvivesCloseAndRetriesAfterRestart(t *testing.T) {
	env := newEngineEnvWithTick(t, 20*time.Millisecond)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "0",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_EXIT_DELAY_MS": "30000",
	})
	waitRunRunningIdentity(t, env.store, runID, 10*time.Second)

	deliveryStarted := make(chan struct{})
	deliveryStopped := make(chan struct{})
	env.eng.mu.Lock()
	env.eng.cleanupDelivery = func(ctx context.Context, _, _ string) (supCmdResponse, error) {
		close(deliveryStarted)
		<-ctx.Done()
		close(deliveryStopped)
		return supCmdResponse{}, ctx.Err()
	}
	env.eng.mu.Unlock()

	response := engineAPI(t, env, "cancel-run", map[string]any{"id": runID})
	if response["ok"] != true || response["accepted"] != true {
		t.Fatalf("cancel-run response = %v, want accepted", response)
	}
	select {
	case <-deliveryStarted:
	case <-time.After(time.Second):
		t.Fatal("first cleanup delivery did not start")
	}
	durable, err := env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after acceptance: %v", err)
	}
	if !durable.CancelRequested || durable.State != store.StateCancelling {
		t.Fatalf("accepted run = %+v, want durable cancelling intent", durable)
	}

	if err := env.eng.Close(); err != nil {
		t.Fatalf("Close first engine: %v", err)
	}
	select {
	case <-deliveryStopped:
	case <-time.After(time.Second):
		t.Fatal("Close returned without cancelling blocked cleanup delivery")
	}

	supervisorBin, _ := ensureBinaries(t)
	second, err := NewEngine(EngineConfig{
		StorePath:     env.storePath,
		SocketPath:    env.socketPath,
		SupervisorBin: supervisorBin,
		DataDir:       env.dataDir,
		Token:         env.token,
		TickInterval:  20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine restart: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- second.Run(ctx) }()
	waitForEngineSocket(t, env.socketPath, 5*time.Second)
	t.Cleanup(func() {
		cancel()
		_ = second.Close()
	})

	cancelled := waitRunState(t, second.store, runID, store.StateCancelled, 15*time.Second)
	if !cancelled.CancelRequested {
		t.Fatal("terminal run lost durable cancellation intent")
	}
	cancel()
	if err := second.Close(); err != nil {
		t.Fatalf("Close restarted engine: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("restarted Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("restarted Run did not join Close")
	}
}

func TestDurableCancellationTickAuthenticatesAndRetriesFailedDelivery(t *testing.T) {
	fixture := newRecoveryFixture(t, store.StateRunning)
	writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
	if _, err := fixture.engine.store.RequestCancellation(context.Background(), fixture.run.ID); err != nil {
		t.Fatalf("RequestCancellation: %v", err)
	}

	var cancelAttempts atomic.Int32
	requests := startRecoverySocket(t, fixture.run.SocketPath, func(request map[string]string) []byte {
		response := recoveryStatus(fixture.run)
		if request["cmd"] == "cancel" {
			attempt := cancelAttempts.Add(1)
			if attempt == 1 {
				response = map[string]any{"ok": false, "error": "injected delivery failure"}
			} else {
				response = map[string]any{"ok": true, "state": string(store.StateCancelling)}
			}
		}
		return encodeRecoveryResponse(t, response)
	})

	assertCycle := func(wantAttempt int32) {
		t.Helper()
		fixture.engine.tick(context.Background())
		for _, command := range []string{"status", "adopt", "cancel"} {
			request := receiveRecoveryRequest(t, requests)
			if request["cmd"] != command || request["token"] != fixture.run.Token {
				t.Fatalf("request = %v, want authenticated %s", request, command)
			}
		}
		waitUntil(t, "cleanup delivery attempt", time.Second, func() bool {
			return cancelAttempts.Load() == wantAttempt
		})
	}

	assertCycle(1)
	waitUntil(t, "failed cleanup delivery retryable", time.Second, func() bool {
		fixture.engine.mu.Lock()
		defer fixture.engine.mu.Unlock()
		_, pending := fixture.engine.cleanupRequested[fixture.run.ID]
		return !pending
	})
	assertCycle(2)
	waitUntil(t, "successful cleanup delivery deduplicated", time.Second, func() bool {
		fixture.engine.mu.Lock()
		defer fixture.engine.mu.Unlock()
		_, pending := fixture.engine.cleanupRequested[fixture.run.ID]
		return pending
	})
}

func TestCreatedCancellationWaitsForRecoveryAuthorityOrExactChild(t *testing.T) {
	engine := newShutdownTestEngine(t)
	ctx := context.Background()
	if err := engine.store.CreateProject(ctx, "created-p", "created", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := engine.store.CreateWorkstream(ctx, "created-w", "created-p", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	if err := engine.store.CreateRun(ctx, "created-r", "created-p", "created-w", store.RunSpec{
		WorkerPath: "/bin/true",
		SocketPath: "/tmp/created-supervisor.sock",
		Token:      "created-token",
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := engine.store.RequestCancellation(ctx, "created-r"); err != nil {
		t.Fatalf("RequestCancellation: %v", err)
	}

	delivered := make(chan struct{}, 1)
	engine.mu.Lock()
	engine.cleanupDelivery = func(context.Context, string, string) (supCmdResponse, error) {
		delivered <- struct{}{}
		return supCmdResponse{OK: true}, nil
	}
	engine.mu.Unlock()
	if err := engine.Recover(ctx); err != nil {
		t.Fatalf("Recover created intent: %v", err)
	}
	select {
	case <-delivered:
		t.Fatal("restarted engine delivered created cancellation without authenticated identity")
	case <-time.After(50 * time.Millisecond):
	}
	run, err := engine.store.GetRun(ctx, "created-r")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateCreated || !run.CancelRequested {
		t.Fatalf("run = %+v, want created intent preserved for bootstrap", run)
	}

	engine.mu.Lock()
	engine.active[run.ID] = &runHandle{
		runID:   run.ID,
		cmd:     &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}},
		watcher: &processExitWatcher{exited: make(chan struct{})},
	}
	engine.mu.Unlock()
	engine.tick(ctx)
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("exact locally owned supervisor did not receive created cancellation")
	}
}
