package store

import (
	"context"
	"errors"
	"testing"
)

func TestTranscriptCompletionHandoffIsDurableAndMonotonic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "transcript-handoff")
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE runs SET transcript_required = 1 WHERE id = ?`, "transcript-handoff"); err != nil {
		t.Fatalf("require transcript durability: %v", err)
	}

	progress, err := s.GetTranscriptProgress(ctx, "transcript-handoff")
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if !progress.Required || progress.ConsumedOffset != 0 || progress.FinalSize != -1 {
		t.Fatalf("initial progress = %+v", progress)
	}
	if err := s.SealTranscript(ctx, "transcript-handoff", 100); err != nil {
		t.Fatalf("SealTranscript: %v", err)
	}
	if err := s.SealTranscript(ctx, "transcript-handoff", 100); err != nil {
		t.Fatalf("idempotent SealTranscript: %v", err)
	}
	if err := s.SealTranscript(ctx, "transcript-handoff", 101); err == nil {
		t.Fatal("SealTranscript accepted a different final size")
	}
	if err := s.AdvanceTranscriptConsumed(ctx, "transcript-handoff", 40); err != nil {
		t.Fatalf("AdvanceTranscriptConsumed 40: %v", err)
	}
	if err := s.AdvanceTranscriptConsumed(ctx, "transcript-handoff", 20); err != nil {
		t.Fatalf("AdvanceTranscriptConsumed rewind: %v", err)
	}
	progress, err = s.GetTranscriptProgress(ctx, "transcript-handoff")
	if err != nil {
		t.Fatalf("GetTranscriptProgress after advance: %v", err)
	}
	if progress.ConsumedOffset != 40 || progress.FinalSize != 100 {
		t.Fatalf("progress after advance = %+v", progress)
	}
	if err := s.AdvanceTranscriptConsumed(ctx, "transcript-handoff", 101); err == nil {
		t.Fatal("AdvanceTranscriptConsumed exceeded sealed final size")
	}
}

func TestAppendEventAtomicallyAdvancesTranscriptConsumption(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "event-handoff")
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE runs SET transcript_required = 1 WHERE id = ?`, "event-handoff"); err != nil {
		t.Fatalf("require transcript durability: %v", err)
	}
	if err := s.SealTranscript(ctx, "event-handoff", 25); err != nil {
		t.Fatalf("SealTranscript: %v", err)
	}
	if _, err := s.AppendEvent(ctx, "event-handoff", "message", []byte(`{"n":1}`), 25); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	progress, err := s.GetTranscriptProgress(ctx, "event-handoff")
	if err != nil {
		t.Fatalf("GetTranscriptProgress: %v", err)
	}
	if progress.ConsumedOffset != 25 || progress.FinalSize != 25 {
		t.Fatalf("progress = %+v, want fully consumed", progress)
	}
	if got, err := s.CommittedOffset(ctx, "event-handoff"); err != nil || got != 25 {
		t.Fatalf("committed offset = %d, error %v", got, err)
	}
}

func TestCommitTerminalRejectsIncompleteTranscriptHandoffAtomically(t *testing.T) {
	tests := []struct {
		name      string
		finalSize int64
		consumed  int64
	}{
		{name: "unsealed", finalSize: -1},
		{name: "partially consumed", finalSize: 10, consumed: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			seedRun(t, s, "incomplete-terminal")
			if _, err := s.DB().ExecContext(ctx, `UPDATE runs
				SET transcript_required = 1, transcript_final_size = ?, transcript_consumed_offset = ?
				WHERE id = ?`, tc.finalSize, tc.consumed, "incomplete-terminal"); err != nil {
				t.Fatalf("seed incomplete transcript handoff: %v", err)
			}
			if err := s.Transition(ctx, "incomplete-terminal", StateRunning, "launch"); err != nil {
				t.Fatalf("Transition running: %v", err)
			}

			err := s.CommitTerminal(ctx, "incomplete-terminal", StateFailed, "must roll back", OutboxRow{})
			if err == nil {
				t.Fatal("CommitTerminal accepted incomplete required transcript handoff")
			}
			if !errors.Is(err, ErrTerminalTranscriptIncomplete) {
				t.Fatalf("CommitTerminal error = %v, want ErrTerminalTranscriptIncomplete", err)
			}
			run, getErr := s.GetRun(ctx, "incomplete-terminal")
			if getErr != nil {
				t.Fatalf("GetRun: %v", getErr)
			}
			if run.State != StateRunning {
				t.Fatalf("state = %q, want running after rejected terminal transaction", run.State)
			}
			if _, outboxErr := s.GetOutbox(ctx, run.ID); outboxErr == nil {
				t.Fatal("outbox committed with rejected terminal transaction")
			}
		})
	}
}

func TestCommitTerminalAcceptsCompleteTranscriptHandoff(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "complete-terminal")
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE runs SET transcript_required = 1 WHERE id = ?`, "complete-terminal"); err != nil {
		t.Fatalf("require transcript handoff: %v", err)
	}
	if err := s.SetTranscriptIdentity(ctx, "complete-terminal", TranscriptFileIdentity{Device: 11, Inode: 22}); err != nil {
		t.Fatalf("SetTranscriptIdentity: %v", err)
	}
	if err := s.Transition(ctx, "complete-terminal", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	if err := s.SealTranscript(ctx, "complete-terminal", 10); err != nil {
		t.Fatalf("SealTranscript: %v", err)
	}
	if err := s.AdvanceTranscriptConsumed(ctx, "complete-terminal", 10); err != nil {
		t.Fatalf("AdvanceTranscriptConsumed: %v", err)
	}
	if err := s.CommitTerminal(ctx, "complete-terminal", StateCompleted, "complete", OutboxRow{}); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}
	run, err := s.GetRun(ctx, "complete-terminal")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != StateCompleted {
		t.Fatalf("state = %q, want completed", run.State)
	}
	if _, err := s.GetOutbox(ctx, run.ID); err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
}
