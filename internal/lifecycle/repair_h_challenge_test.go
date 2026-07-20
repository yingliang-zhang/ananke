package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestChallengeEmptyInodeReplacementCannotTerminalize(t *testing.T) {
	env := newEngineEnvWithTick(t, 10*time.Millisecond)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS":        "0",
		"ANANKE_FW_EXIT":          "0",
		"ANANKE_FW_EXIT_DELAY_MS": "30000",
	})
	run := waitRunRunningIdentity(t, env.store, runID, 10*time.Second)
	waitUntil(t, "empty supervisor-created transcript", 5*time.Second, func() bool {
		info, err := os.Stat(run.TranscriptPath)
		return err == nil && info.Mode().IsRegular() && info.Size() == 0
	})
	env.eng.startTailing(context.Background(), runID, run.TranscriptPath, 0)

	replacement := filepath.Join(filepath.Dir(run.TranscriptPath), "replacement-empty.ndjson")
	if err := os.WriteFile(replacement, nil, 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, run.TranscriptPath); err != nil {
		t.Fatalf("replace transcript inode: %v", err)
	}
	env.eng.tailTranscript(context.Background(), runID, run.TranscriptPath)
	afterReplacement, err := env.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after replacement: %v", err)
	}
	if afterReplacement.State != store.StateCleanupRequired {
		t.Fatalf("replacement was not detected: state=%q", afterReplacement.State)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		env.eng.tick(context.Background())
		current, err := env.store.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun during fail-closed window: %v", err)
		}
		if store.IsTerminal(current.State) {
			t.Fatalf("replacement terminalized: state=%s", current.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
	progress, err := env.store.GetTranscriptProgress(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	events, err := env.store.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if progress.ConsumedOffset != 0 || progress.FinalSize != -1 || len(events) != 0 {
		t.Fatalf("replacement bytes were accepted: consumed=%d final=%d events=%d", progress.ConsumedOffset, progress.FinalSize, len(events))
	}
}

func TestChallengeTerminalRunReleasesTranscriptTail(t *testing.T) {
	env := newEngineEnvWithTick(t, 10*time.Millisecond)
	runID := launchRun(t, env, map[string]string{
		"ANANKE_FW_EVENTS": "1",
		"ANANKE_FW_EXIT":   "0",
	})
	waitRunState(t, env.store, runID, store.StateCompleted, 15*time.Second)
	env.eng.tick(context.Background())

	env.eng.mu.Lock()
	tail := env.eng.tails[runID]
	env.eng.mu.Unlock()
	if tail == nil {
		return
	}
	statErr := error(nil)
	if _, err := tail.file.Stat(); err != nil {
		statErr = err
	}
	t.Fatalf("OBSERVED terminal tail retained: run=%s tail_present=true file_stat_error=%v", runID, statErr)
}

func TestTranscriptEnvelopeMissingFieldsAreMalformedAndAccounted(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{name: "missing type", line: `{"payload":{}}` + "\n"},
		{name: "blank type", line: `{"type":"  ","payload":[]}` + "\n"},
		{name: "missing payload", line: `{"type":"message"}` + "\n"},
		{name: "null payload", line: `{"type":"message","payload":null}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, transcriptPath := newTranscriptPersistenceEngine(t, []byte(tc.line))
			ctx := context.Background()
			engine.tailTranscript(ctx, transcriptPersistenceRunID, transcriptPath)
			run, err := engine.store.GetRun(ctx, transcriptPersistenceRunID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.State != store.StateCleanupRequired {
				t.Fatalf("state = %q, want cleanup_required", run.State)
			}
			progress, err := engine.store.GetTranscriptProgress(ctx, transcriptPersistenceRunID)
			if err != nil {
				t.Fatalf("GetTranscriptProgress: %v", err)
			}
			if progress.ConsumedOffset != int64(len(tc.line)) {
				t.Fatalf("consumed offset = %d, want %d", progress.ConsumedOffset, len(tc.line))
			}
			events, err := engine.store.ListEvents(ctx, transcriptPersistenceRunID, 0)
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if len(events) != 0 {
				t.Fatalf("malformed envelope fabricated %d events", len(events))
			}
		})
	}
}

func TestTranscriptEnvelopeAcceptsNonNullJSONPayloadKinds(t *testing.T) {
	for _, payload := range []string{`{}`, `[]`, `"text"`, `1`, `true`, `false`} {
		t.Run(payload, func(t *testing.T) {
			line := `{"type":"message","payload":` + payload + `}` + "\n"
			engine, transcriptPath := newTranscriptPersistenceEngine(t, []byte(line))
			ctx := context.Background()
			engine.tailTranscript(ctx, transcriptPersistenceRunID, transcriptPath)
			run, err := engine.store.GetRun(ctx, transcriptPersistenceRunID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.State != store.StateRunning {
				t.Fatalf("state = %q, want running", run.State)
			}
			events, err := engine.store.ListEvents(ctx, transcriptPersistenceRunID, 0)
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if len(events) != 1 || events[0].Type != "message" || string(events[0].Payload) != payload {
				t.Fatalf("events = %+v, want one message payload %s", events, payload)
			}
		})
	}
}

func TestDaemonRestartRefusesReplacementTranscriptAtOffsetZero(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	dataDir := filepath.Join(dir, "data")
	config := EngineConfig{
		StorePath:     storePath,
		SocketPath:    filepath.Join(dir, "daemon.sock"),
		SupervisorBin: "/bin/true",
		DataDir:       dataDir,
		Token:         "restart-identity-token",
	}
	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := engine.store.CreateProject(ctx, "restart-project", "restart", dir); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := engine.store.CreateWorkstream(ctx, "restart-workstream", "restart-project", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	runID := "restart-offset-zero"
	transcriptPath := filepath.Join(dir, "transcript.ndjson")
	if err := os.WriteFile(transcriptPath, nil, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := engine.store.CreateRun(ctx, runID, "restart-project", "restart-workstream", store.RunSpec{
		WorkerPath:         "/bin/true",
		TranscriptPath:     transcriptPath,
		TranscriptRequired: true,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	persistTranscriptIdentity(t, engine.store, runID, transcriptPath)
	if err := engine.store.Transition(ctx, runID, store.StateRunning, "fixture running"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	engine.startTailing(ctx, runID, transcriptPath, 0)
	if err := engine.Close(); err != nil {
		t.Fatalf("Close first engine: %v", err)
	}

	replacement := filepath.Join(dir, "replacement.ndjson")
	if err := os.WriteFile(replacement, nil, 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, transcriptPath); err != nil {
		t.Fatalf("replace transcript: %v", err)
	}
	restarted, err := NewEngine(config)
	if err != nil {
		t.Fatalf("NewEngine restart: %v", err)
	}
	defer restarted.Close()
	restarted.startTailing(ctx, runID, transcriptPath, 0)
	run, err := restarted.store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateCleanupRequired {
		t.Fatalf("state = %q, want cleanup_required", run.State)
	}
	restarted.mu.Lock()
	tail := restarted.tails[runID]
	restarted.mu.Unlock()
	if tail != nil {
		t.Fatal("restart bound replacement transcript at offset zero")
	}
}
