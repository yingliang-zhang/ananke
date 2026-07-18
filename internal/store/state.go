// Package store is the durable SQLite journal for Ananke runs.
//
// It owns the cleanup state machine (ADR-0003) and the finalization outbox:
// terminal state transitions are committed in the same SQLite transaction as
// their outbox row, so a crash between commit and supervisor finalization can
// never lose the cleanup obligation.
package store

// State is a run's lifecycle state in the cleanup state machine (ADR-0003 §1).
type State string

const (
	// StateCreated: run record exists; worker not yet launched.
	StateCreated State = "created"
	// StateRunning: worker is executing; transcript streaming.
	StateRunning State = "running"
	// StateCancelling: cancellation requested; awaiting group quiescence.
	StateCancelling State = "cancelling"
	// StateCleanupRequired: error detected while the group was alive; cleanup
	// pending. Nonterminal.
	StateCleanupRequired State = "cleanup_required"
	// StateRecoveryUnknown: supervisor unreachable; identity ambiguous.
	// Nonterminal.
	StateRecoveryUnknown State = "recovery_unknown"
	// StateCompleted: trusted zero exit + full transcript + group quiescent.
	// Terminal.
	StateCompleted State = "completed"
	// StateFailed: nonzero exit or transcript error after authenticated
	// cleanup. Terminal.
	StateFailed State = "failed"
	// StateCancelled: group quiesced after a cancellation request. Terminal.
	StateCancelled State = "cancelled"
)

// Terminal reports whether the state is terminal (no outgoing transitions).
func (s State) Terminal() bool { return IsTerminal(s) }

// IsTerminal reports whether a state is terminal.
func IsTerminal(s State) bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled:
		return true
	}
	return false
}

// allowedTransitions is the adjacency table of the cleanup state machine.
// Terminal states have no outgoing edges. `created` may only advance to
// `running`. Self-loops are forbidden.
var allowedTransitions = map[State]map[State]struct{}{
	StateCreated: {StateRunning: {}},

	StateRunning: {
		StateCancelling:      {},
		StateCleanupRequired: {},
		StateRecoveryUnknown: {},
		StateCompleted:       {},
		StateFailed:          {},
		StateCancelled:       {},
	},

	StateCancelling: {
		StateCancelled:       {},
		StateRecoveryUnknown: {},
		StateCleanupRequired: {},
		StateRunning:         {},
	},

	StateCleanupRequired: {
		StateFailed:          {},
		StateCompleted:       {},
		StateCancelled:       {},
		StateRecoveryUnknown: {},
		StateRunning:         {},
	},

	StateRecoveryUnknown: {
		StateRunning:         {},
		StateCleanupRequired: {},
		StateCancelling:      {},
		StateCompleted:       {},
		StateFailed:          {},
		StateCancelled:       {},
	},
}

// CanTransition reports whether a state transition is permitted by the
// cleanup state machine. The store layer additionally enforces that a
// terminal transition commits an outbox row in the same transaction.
func CanTransition(from, to State) bool {
	if from == to {
		return false
	}
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = targets[to]
	return ok
}
