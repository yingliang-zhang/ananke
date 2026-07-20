package store

import (
	"context"
	"testing"
)

func TestCommitNoProcessFailureFinalizesAtomically(t *testing.T) {
	for _, start := range []State{StateCreated, StateRunning} {
		t.Run(string(start), func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			seedRun(t, s, "run-no-process")
			if _, err := s.DB().ExecContext(ctx,
				`UPDATE runs SET transcript_required = 1 WHERE id = ?`, "run-no-process"); err != nil {
				t.Fatalf("require transcript handoff: %v", err)
			}
			if start == StateRunning {
				if err := s.Transition(ctx, "run-no-process", StateRunning, "supervisor started"); err != nil {
					t.Fatalf("Transition running: %v", err)
				}
			}

			if err := s.CommitNoProcessFailure(ctx, "run-no-process", "exec failed before child creation"); err != nil {
				t.Fatalf("CommitNoProcessFailure: %v", err)
			}
			run, err := s.GetRun(ctx, "run-no-process")
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.State != StateFailed {
				t.Fatalf("state = %q, want failed", run.State)
			}
			row, err := s.GetOutbox(ctx, "run-no-process")
			if err != nil {
				t.Fatalf("GetOutbox: %v", err)
			}
			if row.TerminalState != StateFailed || row.Acknowledged != 1 {
				t.Fatalf("outbox = %+v, want atomically acknowledged failed", row)
			}
			if row.SupervisorPID != 0 || row.SupervisorPGID != 0 || row.SocketPath != "" || row.Token != "" {
				t.Fatalf("no-process outbox carries authority: %+v", row)
			}
			progress, err := s.GetTranscriptProgress(ctx, "run-no-process")
			if err != nil {
				t.Fatalf("GetTranscriptProgress: %v", err)
			}
			if !progress.Required || progress.ConsumedOffset != 0 || progress.FinalSize != 0 {
				t.Fatalf("no-process transcript progress = %+v, want durable empty seal", progress)
			}
		})
	}
}

func TestCommitNoProcessFailureRollsBackOnOutboxFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-no-process")
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO finalization_outbox
		(run_id, terminal_state, acknowledged, created_at)
		VALUES ('run-no-process', 'failed', 1, ?)`, nowStamp()); err != nil {
		t.Fatalf("seed outbox collision: %v", err)
	}

	if err := s.CommitNoProcessFailure(ctx, "run-no-process", "launch failed"); err == nil {
		t.Fatal("CommitNoProcessFailure succeeded despite outbox collision")
	}
	run, err := s.GetRun(ctx, "run-no-process")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != StateCreated {
		t.Fatalf("state = %q, want created after atomic rollback", run.State)
	}
}

func TestCommitNoProcessFailureRejectsRecordedProcessIdentity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-has-process")
	if err := s.SetRunSupervisor(ctx, "run-has-process", 101, 101, 102); err != nil {
		t.Fatalf("SetRunSupervisor: %v", err)
	}

	if err := s.CommitNoProcessFailure(ctx, "run-has-process", "invalid no-process claim"); err == nil {
		t.Fatal("CommitNoProcessFailure accepted recorded process identity")
	}
	run, err := s.GetRun(ctx, "run-has-process")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != StateCreated {
		t.Fatalf("state = %q, want created", run.State)
	}
}
