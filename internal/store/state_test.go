package store

import "testing"

func TestIsTerminal(t *testing.T) {
	terminal := []State{StateCompleted, StateFailed, StateCancelled}
	nonterminal := []State{
		StateCreated, StateRunning, StateCancelling,
		StateCleanupRequired, StateRecoveryUnknown,
	}
	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = false, want true", s)
		}
		if !s.Terminal() {
			t.Errorf("%q.Terminal() = false, want true", s)
		}
	}
	for _, s := range nonterminal {
		if IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = true, want false", s)
		}
		if s.Terminal() {
			t.Errorf("%q.Terminal() = true, want false", s)
		}
	}
}

func TestAllowedTransitions(t *testing.T) {
	allowed := []struct{ from, to State }{
		{StateCreated, StateRunning},
		{StateCreated, StateCleanupRequired},
		{StateCreated, StateRecoveryUnknown},

		{StateRunning, StateCancelling},
		{StateRunning, StateCleanupRequired},
		{StateRunning, StateRecoveryUnknown},
		{StateRunning, StateCompleted},
		{StateRunning, StateFailed},
		{StateRunning, StateCancelled},

		{StateCancelling, StateCancelled},
		{StateCancelling, StateRecoveryUnknown},
		{StateCancelling, StateCleanupRequired},
		{StateCancelling, StateRunning},

		{StateCleanupRequired, StateFailed},
		{StateCleanupRequired, StateCancelled},
		{StateCleanupRequired, StateRecoveryUnknown},

		{StateRecoveryUnknown, StateRunning},
		{StateRecoveryUnknown, StateCleanupRequired},
		{StateRecoveryUnknown, StateCancelling},
		{StateRecoveryUnknown, StateCompleted},
		{StateRecoveryUnknown, StateFailed},
		{StateRecoveryUnknown, StateCancelled},
	}
	for _, c := range allowed {
		if !CanTransition(c.from, c.to) {
			t.Errorf("CanTransition(%q -> %q) = false, want true", c.from, c.to)
		}
	}
}

func TestRejectedTransitions(t *testing.T) {
	rejected := []struct{ from, to State }{
		// created cannot publish a terminal state or enter cancellation directly.
		{StateCreated, StateCompleted},
		{StateCreated, StateFailed},
		{StateCreated, StateCancelled},
		{StateCreated, StateCancelling},
		{StateCreated, StateCreated},

		// terminal states have no outgoing transitions.
		{StateCompleted, StateRunning},
		{StateCompleted, StateFailed},
		{StateCompleted, StateCompleted},
		{StateFailed, StateRunning},
		{StateFailed, StateCompleted},
		{StateFailed, StateFailed},
		{StateCancelled, StateRunning},
		{StateCancelled, StateCompleted},
		{StateCancelled, StateCancelled},

		// cleanup_required is a one-way cleanup obligation.
		{StateCleanupRequired, StateCompleted},
		{StateCleanupRequired, StateRunning},
		// no self-loops / no rewinding into created.
		{StateRunning, StateRunning},
		{StateRunning, StateCreated},
		{StateCancelling, StateCancelling},
		{StateCleanupRequired, StateCleanupRequired},
		{StateRecoveryUnknown, StateRecoveryUnknown},
	}
	for _, c := range rejected {
		if CanTransition(c.from, c.to) {
			t.Errorf("CanTransition(%q -> %q) = true, want false", c.from, c.to)
		}
	}
}

func TestTerminalRequiresQuiescenceEvidence(t *testing.T) {
	// Every terminal target state must be reachable (the state machine does
	// not forbid terminals per se), but the *store* layer is what enforces
	// that a terminal commit carries an outbox row. Here we only assert the
	// enumerated terminal set is stable.
	for _, s := range []State{StateCompleted, StateFailed, StateCancelled} {
		if !IsTerminal(s) {
			t.Errorf("terminal state %q not flagged terminal", s)
		}
	}
}
