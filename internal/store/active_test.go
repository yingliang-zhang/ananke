package store

import (
	"context"
	"testing"
)

func TestListActiveRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// run-1: nonterminal (running) — always active.
	seedRun(t, s, "run-1")
	if err := s.Transition(ctx, "run-1", StateRunning, "launch"); err != nil {
		t.Fatalf("run-1 transition: %v", err)
	}
	// run-2: terminal completed with PENDING outbox — must stay active.
	seedRun(t, s, "run-2")
	if err := s.Transition(ctx, "run-2", StateRunning, "launch"); err != nil {
		t.Fatalf("run-2 transition: %v", err)
	}
	if err := s.CommitTerminal(ctx, "run-2", StateCompleted, "done", OutboxRow{SupervisorPID: 2}); err != nil {
		t.Fatalf("run-2 commit terminal: %v", err)
	}
	// run-3: terminal completed with ACKNOWLEDGED outbox — no longer active.
	seedRun(t, s, "run-3")
	if err := s.Transition(ctx, "run-3", StateRunning, "launch"); err != nil {
		t.Fatalf("run-3 transition: %v", err)
	}
	if err := s.CommitTerminal(ctx, "run-3", StateCompleted, "done", OutboxRow{SupervisorPID: 3}); err != nil {
		t.Fatalf("run-3 commit terminal: %v", err)
	}
	if err := s.AcknowledgeOutbox(ctx, "run-3"); err != nil {
		t.Fatalf("run-3 acknowledge: %v", err)
	}
	// run-4: terminal cancelled, outbox ABANDONED (acknowledged=-1) — excluded.
	seedRun(t, s, "run-4")
	if err := s.Transition(ctx, "run-4", StateRunning, "launch"); err != nil {
		t.Fatalf("run-4 transition: %v", err)
	}
	if err := s.CommitTerminal(ctx, "run-4", StateCancelled, "cancelled", OutboxRow{SupervisorPID: 4}); err != nil {
		t.Fatalf("run-4 commit terminal: %v", err)
	}
	if err := s.AbandonOutbox(ctx, "run-4", "supervisor dead, identity lost"); err != nil {
		t.Fatalf("run-4 abandon: %v", err)
	}

	runs, err := s.ListActiveRuns(ctx)
	if err != nil {
		t.Fatalf("ListActiveRuns: %v", err)
	}
	got := map[string]bool{}
	for _, r := range runs {
		got[r.ID] = true
	}
	// Nonterminal run + pending-outbox terminal run must both be present.
	if !got["run-1"] {
		t.Errorf("run-1 (nonterminal running) missing from ListActiveRuns")
	}
	if !got["run-2"] {
		t.Errorf("run-2 (terminal completed, pending outbox) missing from ListActiveRuns")
	}
	// Acknowledged and abandoned terminal runs must be excluded.
	if got["run-3"] {
		t.Errorf("run-3 (acknowledged terminal) must not appear in ListActiveRuns")
	}
	if got["run-4"] {
		t.Errorf("run-4 (abandoned terminal) must not appear in ListActiveRuns")
	}
	if len(runs) != 2 {
		t.Errorf("len(ListActiveRuns) = %d, want 2 (got %v)", len(runs), keysOf(got))
	}
}

func keysOf(m map[string]bool) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
