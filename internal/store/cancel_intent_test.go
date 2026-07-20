package store

import (
	"context"
	"errors"
	"testing"
)

func TestRequestCancellationDurableAndIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "cancel-idempotent")
	if err := s.Transition(ctx, "cancel-idempotent", StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}

	state, err := s.RequestCancellation(ctx, "cancel-idempotent")
	if err != nil {
		t.Fatalf("RequestCancellation first: %v", err)
	}
	if state != StateCancelling {
		t.Fatalf("first state = %q, want cancelling", state)
	}
	state, err = s.RequestCancellation(ctx, "cancel-idempotent")
	if err != nil {
		t.Fatalf("RequestCancellation duplicate: %v", err)
	}
	if state != StateCancelling {
		t.Fatalf("duplicate state = %q, want cancelling", state)
	}

	run, err := s.GetRun(ctx, "cancel-idempotent")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if !run.CancelRequested || run.State != StateCancelling {
		t.Fatalf("run = %+v, want durable cancel intent in cancelling", run)
	}
	transitions, err := s.Transitions(ctx, run.ID)
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	cancellingTransitions := 0
	for _, transition := range transitions {
		if transition.ToState == StateCancelling {
			cancellingTransitions++
		}
	}
	if cancellingTransitions != 1 {
		t.Fatalf("cancelling transitions = %d, want 1", cancellingTransitions)
	}

	active, err := s.ListActiveRuns(ctx)
	if err != nil {
		t.Fatalf("ListActiveRuns: %v", err)
	}
	if len(active) != 1 || !active[0].CancelRequested {
		t.Fatalf("active runs = %+v, want round-tripped cancellation intent", active)
	}
	projectRuns, err := s.ListRunsByProject(ctx, run.ProjectID)
	if err != nil {
		t.Fatalf("ListRunsByProject: %v", err)
	}
	if len(projectRuns) != 1 || !projectRuns[0].CancelRequested {
		t.Fatalf("project runs = %+v, want round-tripped cancellation intent", projectRuns)
	}
}

func TestRequestCancellationPreservesCreatedAndStrongerStates(t *testing.T) {
	tests := []struct {
		name  string
		state State
	}{
		{name: "created", state: StateCreated},
		{name: "cleanup required", state: StateCleanupRequired},
		{name: "recovery unknown", state: StateRecoveryUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			runID := "cancel-" + string(tc.state)
			seedRun(t, s, runID)
			if tc.state != StateCreated {
				if err := s.Transition(ctx, runID, StateRunning, "launched"); err != nil {
					t.Fatalf("Transition running: %v", err)
				}
				if err := s.Transition(ctx, runID, tc.state, "seed stronger state"); err != nil {
					t.Fatalf("Transition %s: %v", tc.state, err)
				}
			}

			state, err := s.RequestCancellation(ctx, runID)
			if err != nil {
				t.Fatalf("RequestCancellation: %v", err)
			}
			if state != tc.state {
				t.Fatalf("state = %q, want preserved %q", state, tc.state)
			}
			run, err := s.GetRun(ctx, runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if !run.CancelRequested || run.State != tc.state {
				t.Fatalf("run = %+v, want intent with state %q", run, tc.state)
			}
		})
	}
}

func TestRequestCancellationRejectsTerminalRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "cancel-terminal")
	if err := s.Transition(ctx, "cancel-terminal", StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	if err := s.CommitTerminal(ctx, "cancel-terminal", StateCompleted, "done", OutboxRow{}); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}

	if _, err := s.RequestCancellation(ctx, "cancel-terminal"); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("RequestCancellation error = %v, want ErrRunTerminal", err)
	}
	run, err := s.GetRun(ctx, "cancel-terminal")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.CancelRequested {
		t.Fatal("terminal rejection persisted cancellation intent")
	}
}

func TestAcceptedCancellationPreventsCompletedCommit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "cancel-completion-race")
	if err := s.Transition(ctx, "cancel-completion-race", StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	if _, err := s.RequestCancellation(ctx, "cancel-completion-race"); err != nil {
		t.Fatalf("RequestCancellation: %v", err)
	}

	err := s.CommitTerminal(ctx, "cancel-completion-race", StateCompleted, "natural zero exit", OutboxRow{})
	if !errors.Is(err, ErrCancellationRequested) {
		t.Fatalf("CommitTerminal completed error = %v, want ErrCancellationRequested", err)
	}
	run, getErr := s.GetRun(ctx, "cancel-completion-race")
	if getErr != nil {
		t.Fatalf("GetRun: %v", getErr)
	}
	if run.State != StateCancelling || !run.CancelRequested {
		t.Fatalf("run = %+v, completed escaped accepted cancellation", run)
	}
}
