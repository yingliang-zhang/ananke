package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// TestMutationResetOffsetNoDup verifies that the engine does not reset the
// transcript offset on reconnect. With the mutation_reset_offset tag, the
// engine re-reads from offset 0 on reconnect, causing duplicate events.
func TestMutationResetOffsetNoDup(t *testing.T) {
	env := newEngineEnv(t)
	// Use a long delay so the worker stays alive while we crash the engine.
	// 3 events are written quickly (20ms inter-event), then 10s pre-exit sleep.
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "3",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_DELAY_MS":      "0",
		"ANANKE_FW_EXIT_DELAY_MS": "10000",
	})

	// Wait for all 3 events to be ingested (events are written immediately,
	// the 10s delay is just the pre-exit sleep).
	waitRunState(t, env.store, runID, store.StateRunning, 10*time.Second)

	// Wait for events to appear in the store.
	deadline := time.Now().Add(10 * time.Second)
	var originalCount int
	for time.Now().Before(deadline) {
		events, _ := env.store.ListEvents(context.Background(), runID, 0)
		if len(events) >= 3 {
			originalCount = len(events)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if originalCount != 3 {
		t.Fatalf("expected 3 events before crash, got %d", originalCount)
	}

	// Kill the engine while the worker is still running (non-terminal).
	env.cancel()
	_ = env.eng.Close()

	// Restart the engine. The run is still running (supervisor alive).
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
	defer func() {
		cancel2()
		_ = eng2.Close()
	}()
	waitForEngineSocket(t, env.socketPath, 5*time.Second)

	// Wait a few ticks for the recovery loop to process the transcript.
	time.Sleep(1 * time.Second)

	// Cancel the run so it can complete (the worker has a 10s delay).
	resp := engineAPI(t, &engineEnv{
		eng:        eng2,
		store:      eng2.Store(),
		socketPath: env.socketPath,
		token:      env.token,
	}, "cancel-run", map[string]any{"id": runID})
	if resp["ok"] != true {
		t.Logf("cancel-run: %v (may already be cleaning up)", resp)
	}

	// Wait for the run to reach a terminal state.
	waitRunState(t, eng2.Store(), runID, store.StateCancelled, 30*time.Second)

	// Check that no duplicate events were created.
	events2, err := eng2.Store().ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents after restart: %v", err)
	}
	if len(events2) != originalCount {
		t.Errorf("event count after restart = %d, want %d (duplicate events from offset reset)", len(events2), originalCount)
	}
}
