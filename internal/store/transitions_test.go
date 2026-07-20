package store

import (
	"context"
	"errors"
	"testing"
)

func TestCommitTerminalHappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")

	if err := s.Transition(ctx, "run-1", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition to running: %v", err)
	}
	outbox := OutboxRow{
		SupervisorPID:  4242,
		SupervisorPGID: 4242,
		SocketPath:     "/tmp/sock-1",
		Token:          "tok-1",
	}
	if err := s.CommitTerminal(ctx, "run-1", StateCompleted, "zero exit + quiescent", outbox); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}

	run, err := s.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != StateCompleted {
		t.Errorf("State = %q, want completed", run.State)
	}

	row, err := s.GetOutbox(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if row.RunID != "run-1" {
		t.Errorf("outbox RunID = %q", row.RunID)
	}
	if row.TerminalState != StateCompleted {
		t.Errorf("outbox TerminalState = %q, want completed", row.TerminalState)
	}
	if row.SupervisorPID != 4242 || row.SupervisorPGID != 4242 {
		t.Errorf("outbox pid/pgid = %d/%d, want 4242/4242", row.SupervisorPID, row.SupervisorPGID)
	}
	if row.SocketPath != "/tmp/sock-1" || row.Token != "tok-1" {
		t.Errorf("outbox socket/token = %q/%q", row.SocketPath, row.Token)
	}
	if row.Acknowledged != 0 {
		t.Errorf("outbox Acknowledged = %d, want 0 (pending)", row.Acknowledged)
	}
}

func TestCommitTerminalAtomicRollbackOnOutboxFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")
	if err := s.Transition(ctx, "run-1", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	// Pre-insert an outbox row so the terminal commit's outbox INSERT collides
	// on the primary key. If the transaction is atomic, the state change must
	// roll back and the run stays `running`.
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO finalization_outbox
		(run_id, terminal_state, acknowledged, created_at)
		VALUES ('run-1', 'failed', 0, ?)`, nowStamp()); err != nil {
		t.Fatalf("seed outbox collision: %v", err)
	}

	err := s.CommitTerminal(ctx, "run-1", StateCompleted, "should roll back", OutboxRow{})
	if err == nil {
		t.Fatalf("CommitTerminal unexpectedly succeeded; atomicity violated")
	}

	run, err := s.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != StateRunning {
		t.Errorf("State = %q, want running (terminal commit must roll back on outbox failure)", run.State)
	}
}

func TestCommitTerminalRejectsNonTerminalTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")
	if err := s.Transition(ctx, "run-1", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if err := s.CommitTerminal(ctx, "run-1", StateRunning, "nope", OutboxRow{}); err == nil {
		t.Errorf("CommitTerminal to non-terminal state running returned nil error")
	}
}

func TestTransitionRejectsTerminalTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")
	if err := s.Transition(ctx, "run-1", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	// A plain (non-terminal) Transition must refuse to commit a terminal state.
	// Terminal commits must go through CommitTerminal, which inserts the
	// outbox row in the same transaction.
	if err := s.Transition(ctx, "run-1", StateCompleted, "sneaky"); err == nil {
		t.Errorf("Transition to terminal state completed returned nil error; must require CommitTerminal")
	}
	run, _ := s.GetRun(ctx, "run-1")
	if run.State != StateRunning {
		t.Errorf("State = %q, want running (non-terminal Transition must not reach terminal)", run.State)
	}
}

func TestCommitTerminalRejectsDisallowedTransition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")
	// created -> completed is not allowed; must go through running.
	err := s.CommitTerminal(ctx, "run-1", StateCompleted, "skip", OutboxRow{})
	if err == nil {
		t.Fatalf("CommitTerminal from created to completed returned nil; want disallowed")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error = %v, want ErrInvalidTransition", err)
	}
	run, _ := s.GetRun(ctx, "run-1")
	if run.State != StateCreated {
		t.Errorf("State = %q, want created (disallowed terminal commit must not mutate)", run.State)
	}
}

func TestAcknowledgeOutbox(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-1")
	if err := s.Transition(ctx, "run-1", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if err := s.CommitTerminal(ctx, "run-1", StateCompleted, "done", OutboxRow{SupervisorPID: 99}); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}
	if err := s.AcknowledgeOutbox(ctx, "run-1"); err != nil {
		t.Fatalf("AcknowledgeOutbox: %v", err)
	}
	row, err := s.GetOutbox(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if row.Acknowledged != 1 {
		t.Errorf("Acknowledged = %d, want 1", row.Acknowledged)
	}
	if row.AcknowledgedAt.IsZero() {
		t.Errorf("AcknowledgedAt is zero after acknowledge")
	}
	// Idempotent re-acknowledge on an already-acknowledged row reports not found.
	if err := s.AcknowledgeOutbox(ctx, "run-1"); err == nil {
		t.Errorf("re-acknowledge returned nil; want not-found/error")
	}
}

func TestAbandonOutboxPersistsDiagnostic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "run-abandoned")
	if err := s.Transition(ctx, "run-abandoned", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if err := s.CommitTerminal(ctx, "run-abandoned", StateFailed, "failed", OutboxRow{SupervisorPID: 99}); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}
	if err := s.AbandonOutbox(ctx, "run-abandoned", "  "); err == nil {
		t.Fatal("AbandonOutbox accepted an empty diagnostic")
	}
	if err := s.AbandonOutbox(ctx, "run-abandoned", "validated identity; supervisor dead; worker and group quiescent"); err != nil {
		t.Fatalf("AbandonOutbox: %v", err)
	}
	row, err := s.GetOutbox(ctx, "run-abandoned")
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if row.Acknowledged != -1 {
		t.Fatalf("Acknowledged = %d, want -1", row.Acknowledged)
	}
	if row.Diagnostic != "validated identity; supervisor dead; worker and group quiescent" {
		t.Fatalf("Diagnostic = %q", row.Diagnostic)
	}
	if row.AcknowledgedAt.IsZero() {
		t.Fatal("AcknowledgedAt is zero after abandonment")
	}
}
