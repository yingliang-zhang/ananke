package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestEngineCompletionIncludesFinalLineWithoutNewline(t *testing.T) {
	env := newEngineEnvWithTick(t, 20*time.Millisecond)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":      "5",
		"ANANKE_FW_EXIT":        "0",
		"ANANKE_FW_DELAY_MS":    "5",
		"ANANKE_FW_NO_FINAL_NL": "1",
	})
	waitRunState(t, env.store, runID, store.StateCompleted, 15*time.Second)
	events, err := env.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("events at completed = %d, want all 5 including final unterminated line", len(events))
	}
	progress, err := env.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if !progress.Required || progress.FinalSize <= 0 || progress.ConsumedOffset != progress.FinalSize {
		t.Fatalf("completed transcript progress = %+v, want sealed and fully consumed", progress)
	}
}

func TestEngineCompletionWaitsForTranscriptAcrossDaemonRestart(t *testing.T) {
	env := newEngineEnvWithTick(t, 20*time.Millisecond)
	projectID, workstreamID := setupProject(t, env)
	runID := "transcript-restart-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := env.store.DB().ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TRIGGER block_transcript_events
		BEFORE INSERT ON events
		WHEN NEW.run_id = %q
		BEGIN SELECT RAISE(FAIL, 'injected transcript persistence outage'); END`, runID)); err != nil {
		t.Fatalf("install transcript outage trigger: %v", err)
	}
	_, worker := ensureBinaries(t)
	response := engineAPIRaw(t, env, map[string]any{
		"cmd":           "launch-run",
		"id":            runID,
		"project_id":    projectID,
		"workstream_id": workstreamID,
		"worker_path":   worker,
		"worker_env": []string{
			"ANANKE_FW_EVENTS=5",
			"ANANKE_FW_EXIT=0",
			"ANANKE_FW_DELAY_MS=5",
		},
	})
	if response["ok"] != true {
		t.Fatalf("launch-run: %v", response)
	}
	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	env.supPIDsMu.Lock()
	env.supPIDs = append(env.supPIDs, run.SupervisorPID)
	env.supPIDsMu.Unlock()

	waitUntil(t, "sealed transcript blocked on durability", 10*time.Second, func() bool {
		current, runErr := env.store.GetRun(context.Background(), runID)
		progress, progressErr := env.store.GetTranscriptProgress(context.Background(), runID)
		if runErr != nil || progressErr != nil {
			return false
		}
		run = current
		return progress.FinalSize > 0 && progress.ConsumedOffset < progress.FinalSize
	})
	if store.IsTerminal(run.State) {
		t.Fatalf("state = %q while transcript persistence was blocked", run.State)
	}
	select {
	case <-time.After(100 * time.Millisecond):
	}
	current, err := env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun before restart: %v", err)
	}
	if store.IsTerminal(current.State) {
		t.Fatalf("terminal state %q became visible before transcript durability", current.State)
	}

	env.cancel()
	if err := env.eng.Close(); err != nil {
		t.Fatalf("close first engine: %v", err)
	}

	supervisor, _ := ensureBinaries(t)
	engine2, err := NewEngine(EngineConfig{
		StorePath:     env.storePath,
		SocketPath:    env.socketPath,
		SupervisorBin: supervisor,
		DataDir:       env.dataDir,
		Token:         env.token,
		TickInterval:  20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewEngine after outage: %v", err)
	}
	if _, err := engine2.store.DB().ExecContext(context.Background(), `DROP TRIGGER block_transcript_events`); err != nil {
		_ = engine2.Close()
		t.Fatalf("clear transcript outage: %v", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { _ = engine2.Run(ctx2) }()
	waitForEngineSocket(t, env.socketPath, 5*time.Second)
	t.Cleanup(func() {
		cancel2()
		_ = engine2.Close()
	})

	waitRunState(t, engine2.store, runID, store.StateCompleted, 15*time.Second)
	events, err := engine2.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents after restart: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("events after restart = %d, want 5", len(events))
	}
	progress, err := engine2.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress after restart: %v", err)
	}
	if progress.ConsumedOffset != progress.FinalSize {
		t.Fatalf("completed progress after restart = %+v", progress)
	}
}

func TestEngineFailedRunDrainsTranscriptBeforeTerminal(t *testing.T) {
	env := newEngineEnvWithTick(t, 20*time.Millisecond)
	unblock := blockTranscriptEventPersistence(t, env)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":      "3",
		"ANANKE_FW_EXIT":        "23",
		"ANANKE_FW_NO_FINAL_NL": "1",
	})

	waitForSealedTranscriptBeforeTerminal(t, env.store, runID, 10*time.Second)
	unblock()
	waitRunState(t, env.store, runID, store.StateFailed, 15*time.Second)
	assertTerminalTranscriptDrained(t, env.store, runID, 3)
}

func TestEngineFailedRunRetriesTranscriptSealAfterStoreFailure(t *testing.T) {
	env := newEngineEnvWithTick(t, 20*time.Millisecond)
	if _, err := env.store.DB().ExecContext(context.Background(), `
		CREATE TRIGGER block_terminal_transcript_seal
		BEFORE UPDATE OF transcript_final_size ON runs
		WHEN OLD.transcript_final_size < 0 AND NEW.transcript_final_size >= 0
		BEGIN SELECT RAISE(FAIL, 'injected transcript seal outage'); END`); err != nil {
		t.Fatalf("install transcript seal outage: %v", err)
	}
	blocked := true
	unblock := func() {
		if !blocked {
			return
		}
		blocked = false
		if _, err := env.store.DB().ExecContext(context.Background(), `DROP TRIGGER IF EXISTS block_terminal_transcript_seal`); err != nil {
			t.Errorf("clear transcript seal outage: %v", err)
		}
	}
	t.Cleanup(unblock)

	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "3",
		"ANANKE_FW_EXIT":          "23",
		"ANANKE_FW_DELAY_MS":      "10",
		"ANANKE_FW_EXIT_DELAY_MS": "100",
		"ANANKE_FW_NO_FINAL_NL":   "1",
	})
	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	supervisorPID := run.SupervisorPID

	waitUntil(t, "failed worker reaped with transcript seal blocked", 10*time.Second, func() bool {
		current, runErr := env.store.GetRun(context.Background(), runID)
		progress, progressErr := env.store.GetTranscriptProgress(context.Background(), runID)
		if runErr != nil || progressErr != nil {
			return false
		}
		run = current
		return !processAlive(current.WorkerPID) && progress.FinalSize == -1
	})
	progress, err := env.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress while seal blocked: %v", err)
	}
	if store.IsTerminal(run.State) {
		t.Fatalf("terminal state %q visible while transcript seal blocked: %+v", run.State, progress)
	}
	status, err := env.eng.sendSupervisorCmd(context.Background(), run.SocketPath, run.Token, "status")
	if err != nil {
		t.Fatalf("same supervisor unavailable while transcript seal blocked: %v", err)
	}
	if status.SupervisorPID != supervisorPID || status.ExitCode != 23 {
		t.Fatalf("supervisor status while seal blocked = %+v, want pid=%d exit_code=23", status, supervisorPID)
	}

	unblock()
	run = waitRunState(t, env.store, runID, store.StateFailed, 15*time.Second)
	if run.SupervisorPID != supervisorPID {
		t.Fatalf("terminal supervisor pid = %d, want retrying supervisor %d", run.SupervisorPID, supervisorPID)
	}
	assertTerminalTranscriptDrained(t, env.store, runID, 3)
}

func TestEngineCancelledRunDrainsTranscriptBeforeTerminal(t *testing.T) {
	env := newEngineEnvWithTick(t, 20*time.Millisecond)
	unblock := blockTranscriptEventPersistence(t, env)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "3",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_EXIT_DELAY_MS": "5000",
	})

	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	waitUntil(t, "worker transcript emission", 5*time.Second, func() bool {
		data, err := os.ReadFile(run.TranscriptPath)
		return err == nil && bytes.Count(data, []byte{'\n'}) == 3
	})
	response := engineAPI(t, env, "cancel-run", map[string]any{"id": runID})
	if response["ok"] != true || response["accepted"] != true {
		t.Fatalf("cancel-run response = %v, want accepted", response)
	}

	waitForSealedTranscriptBeforeTerminal(t, env.store, runID, 10*time.Second)
	unblock()
	waitRunState(t, env.store, runID, store.StateCancelled, 15*time.Second)
	assertTerminalTranscriptDrained(t, env.store, runID, 3)
}

func TestEngineMalformedTranscriptWaitsForSealAndDrainBeforeFailed(t *testing.T) {
	env := newEngineEnvWithTick(t, time.Hour)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "1",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_EXIT_DELAY_MS": "30000",
	})
	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	waitUntil(t, "initial transcript record", 5*time.Second, func() bool {
		info, err := os.Stat(run.TranscriptPath)
		return err == nil && info.Size() > 0
	})
	env.eng.tailTranscript(context.Background(), runID, run.TranscriptPath)
	events, err := env.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents before corruption: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events before corruption = %d, want 1", len(events))
	}

	dropFailure := installSQLiteFailureTrigger(t, env.eng, "block_malformed_record_accounting", `
		CREATE TRIGGER block_malformed_record_accounting
		BEFORE UPDATE OF transcript_consumed_offset ON runs
		WHEN OLD.id = '`+runID+`' AND NEW.transcript_consumed_offset > OLD.transcript_consumed_offset
		BEGIN SELECT RAISE(FAIL, 'injected malformed accounting outage'); END`)
	f, err := os.OpenFile(run.TranscriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open transcript: %v", err)
	}
	if _, err := f.WriteString("THIS IS NOT JSON\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append malformed transcript: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		t.Fatalf("sync malformed transcript: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close malformed transcript: %v", err)
	}

	env.eng.tailTranscript(context.Background(), runID, run.TranscriptPath)
	run, err = env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after corruption: %v", err)
	}
	if run.State != store.StateCleanupRequired {
		t.Fatalf("state after malformed record = %q, want cleanup_required", run.State)
	}
	env.eng.progressCleanupRequired(context.Background(), run)
	waitUntil(t, "sealed transcript with accounting blocked", 10*time.Second, func() bool {
		current, runErr := env.store.GetRun(context.Background(), runID)
		progress, progressErr := env.store.GetTranscriptProgress(context.Background(), runID)
		return runErr == nil && progressErr == nil &&
			(store.IsTerminal(current.State) || progress.FinalSize >= 0)
	})
	run, err = env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun while accounting blocked: %v", err)
	}
	progress, err := env.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress while accounting blocked: %v", err)
	}
	if store.IsTerminal(run.State) {
		t.Fatalf("terminal state %q published before malformed bytes were sealed and accounted: %+v", run.State, progress)
	}
	if progress.FinalSize <= 0 || progress.ConsumedOffset >= progress.FinalSize {
		t.Fatalf("blocked transcript progress = %+v, want sealed with unaccounted malformed bytes", progress)
	}
	events, err = env.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents while accounting blocked: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events after malformed record = %d, want original event only", len(events))
	}

	dropFailure()
	env.eng.tailTranscript(context.Background(), runID, run.TranscriptPath)
	waitRunState(t, env.store, runID, store.StateFailed, 15*time.Second)
	progress, err = env.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress at failed: %v", err)
	}
	if !progress.Required || progress.ConsumedOffset != progress.FinalSize {
		t.Fatalf("failed transcript progress = %+v, want sealed and fully accounted", progress)
	}
	events, err = env.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents at failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events at failed = %d, want malformed record excluded", len(events))
	}
}

func TestEngineRecoveryUnknownDeadSupervisorSealsAndDrainsTranscript(t *testing.T) {
	fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
	ctx := context.Background()
	if _, err := fixture.engine.store.DB().ExecContext(ctx,
		`UPDATE runs SET transcript_required = 1 WHERE id = ?`, fixture.run.ID); err != nil {
		t.Fatalf("require transcript handoff: %v", err)
	}
	additional := []byte("{\"type\":\"recovered\",\"payload\":{\"n\":2}}\n")
	f, err := os.OpenFile(fixture.run.TranscriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open recovery transcript: %v", err)
	}
	if _, err := f.Write(additional); err != nil {
		_ = f.Close()
		t.Fatalf("append recovery transcript: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		t.Fatalf("sync recovery transcript: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close recovery transcript: %v", err)
	}
	fixture.run, err = fixture.engine.store.GetRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun after requiring transcript: %v", err)
	}
	writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
	fixture.backend.alive[fixture.run.SupervisorPID] = false
	fixture.backend.alive[fixture.run.WorkerPID] = false
	dropFailure := installSQLiteFailureTrigger(t, fixture.engine, "block_recovery_transcript_event", `
		CREATE TRIGGER block_recovery_transcript_event
		BEFORE INSERT ON events
		WHEN NEW.run_id = '`+fixture.run.ID+`'
		BEGIN SELECT RAISE(FAIL, 'injected recovery transcript outage'); END`)

	fixture.engine.tick(ctx)
	run, err := fixture.engine.store.GetRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun while recovery drain blocked: %v", err)
	}
	if run.State != store.StateRecoveryUnknown {
		t.Fatalf("state = %q, want recovery_unknown until daemon transcript drain completes", run.State)
	}
	progress, err := fixture.engine.store.GetTranscriptProgress(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress while recovery drain blocked: %v", err)
	}
	info, err := os.Stat(fixture.run.TranscriptPath)
	if err != nil {
		t.Fatalf("stat recovery transcript: %v", err)
	}
	if progress.FinalSize != info.Size() || progress.ConsumedOffset >= progress.FinalSize {
		t.Fatalf("blocked recovery progress = %+v, file size %d", progress, info.Size())
	}
	if _, err := fixture.engine.store.GetOutbox(ctx, fixture.run.ID); err == nil {
		t.Fatal("terminal outbox published before recovery transcript drain")
	}

	dropFailure()
	fixture.engine.tick(ctx)
	run, err = fixture.engine.store.GetRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun after recovery drain: %v", err)
	}
	if run.State != store.StateFailed {
		t.Fatalf("state after recovery drain = %q, want failed", run.State)
	}
	progress, err = fixture.engine.store.GetTranscriptProgress(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress after recovery drain: %v", err)
	}
	if !progress.Required || progress.ConsumedOffset != progress.FinalSize || progress.FinalSize != info.Size() {
		t.Fatalf("terminal recovery progress = %+v, want exact sealed drain of %d bytes", progress, info.Size())
	}
	events, err := fixture.engine.store.ListEvents(ctx, fixture.run.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents after recovery drain: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events after recovery drain = %d, want 2", len(events))
	}
}

func TestEngineRecoveryUnknownMissingTranscriptStaysNonterminal(t *testing.T) {
	fixture := newRecoveryFixture(t, store.StateRecoveryUnknown)
	ctx := context.Background()
	if _, err := fixture.engine.store.DB().ExecContext(ctx, `DELETE FROM events WHERE run_id = ?`, fixture.run.ID); err != nil {
		t.Fatalf("delete seeded transcript event: %v", err)
	}
	if _, err := fixture.engine.store.DB().ExecContext(ctx, `UPDATE runs
		SET transcript_required = 1, committed_offset = 0,
			transcript_consumed_offset = 0, transcript_final_size = -1
		WHERE id = ?`, fixture.run.ID); err != nil {
		t.Fatalf("reset required transcript progress: %v", err)
	}
	if err := os.Remove(fixture.run.TranscriptPath); err != nil {
		t.Fatalf("remove process transcript: %v", err)
	}
	updatedRun, err := fixture.engine.store.GetRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	fixture.run = updatedRun
	writeRecoveryIdentityValues(t, fixture.run.IdentityPath, recoveryIdentityValues(fixture.run))
	fixture.backend.alive[fixture.run.SupervisorPID] = false
	fixture.backend.alive[fixture.run.WorkerPID] = false

	fixture.engine.tick(ctx)
	run, err := fixture.engine.store.GetRun(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun after missing transcript recovery: %v", err)
	}
	if run.State != store.StateRecoveryUnknown {
		t.Fatalf("state = %q, want recovery_unknown for missing process transcript", run.State)
	}
	progress, err := fixture.engine.store.GetTranscriptProgress(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if progress.FinalSize != -1 || progress.ConsumedOffset != 0 {
		t.Fatalf("missing process transcript progress = %+v, want unsealed and unconsumed", progress)
	}
	if _, err := fixture.engine.store.GetOutbox(ctx, fixture.run.ID); err == nil {
		t.Fatal("terminal outbox published for missing process transcript")
	}
}

func blockTranscriptEventPersistence(t *testing.T, env *engineEnv) func() {
	t.Helper()
	if _, err := env.store.DB().ExecContext(context.Background(), `
		CREATE TRIGGER block_terminal_transcript_events
		BEFORE INSERT ON events
		BEGIN SELECT RAISE(FAIL, 'injected terminal transcript persistence outage'); END`); err != nil {
		t.Fatalf("install transcript persistence outage: %v", err)
	}
	dropped := false
	unblock := func() {
		if dropped {
			return
		}
		dropped = true
		if _, err := env.store.DB().ExecContext(context.Background(), `DROP TRIGGER IF EXISTS block_terminal_transcript_events`); err != nil {
			t.Errorf("clear transcript persistence outage: %v", err)
		}
	}
	t.Cleanup(unblock)
	return unblock
}

func waitForSealedTranscriptBeforeTerminal(t *testing.T, st *store.Store, runID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		run, runErr := st.GetRun(context.Background(), runID)
		progress, progressErr := st.GetTranscriptProgress(context.Background(), runID)
		if runErr == nil && store.IsTerminal(run.State) {
			t.Fatalf("run became %q before transcript handoff: %+v", run.State, progress)
		}
		if runErr == nil && progressErr == nil && progress.FinalSize > 0 && progress.ConsumedOffset < progress.FinalSize {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, _ := st.GetRun(context.Background(), runID)
	progress, _ := st.GetTranscriptProgress(context.Background(), runID)
	t.Fatalf("run did not seal transcript before terminal within %v: state=%q progress=%+v", timeout, run.State, progress)
}

func assertTerminalTranscriptDrained(t *testing.T, st *store.Store, runID string, expectedEvents int) {
	t.Helper()
	events, err := st.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != expectedEvents {
		t.Fatalf("events at terminal = %d, want %d", len(events), expectedEvents)
	}
	for i, event := range events {
		var payload struct {
			SourceSequence int    `json:"source_seq"`
			Text           string `json:"text"`
			Timestamp      string `json:"timestamp"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("event %d payload %q: %v", i+1, event.Payload, err)
		}
		if payload.SourceSequence != i+1 || payload.Text != fmt.Sprintf("event %d", i+1) {
			t.Errorf("event %d payload = %+v, want source_seq=%d text=%q", i+1, payload, i+1, fmt.Sprintf("event %d", i+1))
		}
		if _, err := time.Parse(time.RFC3339Nano, payload.Timestamp); err != nil {
			t.Errorf("event %d timestamp %q: %v", i+1, payload.Timestamp, err)
		}
	}
	progress, err := st.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if progress.FinalSize <= 0 || progress.ConsumedOffset != progress.FinalSize {
		t.Fatalf("terminal transcript progress = %+v, want sealed and fully consumed", progress)
	}
}
