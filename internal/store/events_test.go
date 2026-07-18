package store

import (
	"context"
	"testing"
)

// seedRun creates a project, workstream, and run with sensible defaults for
// store-layer tests. It fails the test on any error.
func seedRun(t *testing.T, s *Store, runID string) RunSpec {
	t.Helper()
	ctx := context.Background()
	projID := "proj-" + runID
	wsID := "ws-" + runID
	if err := s.CreateProject(ctx, projID, "p", "/tmp/root"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := s.CreateWorkstream(ctx, wsID, projID, "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	spec := RunSpec{
		WorkerPath:     "/bin/echo",
		WorkerArgs:     []string{"hi"},
		TranscriptPath: "/tmp/t.ndjson",
		SocketPath:     "/tmp/sock",
		Token:          "tok",
		IdentityPath:   "/tmp/id.json",
	}
	if err := s.CreateRun(ctx, runID, projID, wsID, spec); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	return spec
}

func TestAppendEventMonotonicSequenceAndAtomicOffset(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")

	cases := []struct {
		typ     string
		payload string
		offset  int64
	}{
		{"started", `{"v":1}`, 100},
		{"progress", `{"n":1}`, 200},
		{"progress", `{"n":2}`, 300},
	}
	var wantSeq int64 = 1
	var lastOffset int64
	for i, c := range cases {
		ev, err := s.AppendEvent(ctx, "run-1", c.typ, []byte(c.payload), c.offset)
		if err != nil {
			t.Fatalf("case %d AppendEvent: %v", i, err)
		}
		if ev.Seq != wantSeq {
			t.Errorf("case %d Seq = %d, want %d", i, ev.Seq, wantSeq)
		}
		if ev.RunID != "run-1" {
			t.Errorf("case %d RunID = %q", i, ev.RunID)
		}
		if ev.Type != c.typ {
			t.Errorf("case %d Type = %q, want %q", i, ev.Type, c.typ)
		}
		if string(ev.Payload) != c.payload {
			t.Errorf("case %d Payload = %q, want %q", i, ev.Payload, c.payload)
		}
		if ev.TranscriptOffset != c.offset {
			t.Errorf("case %d TranscriptOffset = %d, want %d", i, ev.TranscriptOffset, c.offset)
		}

		// Atomic offset commit: the committed offset advances with the event
		// row in the same transaction, observable immediately after append.
		got, err := s.CommittedOffset(ctx, "run-1")
		if err != nil {
			t.Fatalf("case %d CommittedOffset: %v", i, err)
		}
		if got != c.offset {
			t.Errorf("case %d committed offset = %d, want %d (atomic with event)", i, got, c.offset)
		}
		lastOffset = c.offset
		wantSeq++
	}

	run, err := s.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.CommittedOffset != lastOffset {
		t.Errorf("run.CommittedOffset = %d, want %d", run.CommittedOffset, lastOffset)
	}
}

func TestAppendEventOutOfOrderOffsetDoesNotRewind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")

	if _, err := s.AppendEvent(ctx, "run-1", "started", []byte(`{}`), 500); err != nil {
		t.Fatalf("AppendEvent 500: %v", err)
	}
	// A late-arriving event with a lower offset must never rewind the durable
	// committed high-water mark. The committed offset is monotonic.
	if _, err := s.AppendEvent(ctx, "run-1", "progress", []byte(`{}`), 100); err != nil {
		t.Fatalf("AppendEvent 100: %v", err)
	}
	got, err := s.CommittedOffset(ctx, "run-1")
	if err != nil {
		t.Fatalf("CommittedOffset: %v", err)
	}
	if got != 500 {
		t.Errorf("committed offset = %d, want 500 (must not rewind to 100)", got)
	}
}

func TestAppendEventUnknownRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.AppendEvent(ctx, "nope", "started", []byte(`{}`), 1); err == nil {
		t.Errorf("AppendEvent on unknown run returned nil error")
	}
}
