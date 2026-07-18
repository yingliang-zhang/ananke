package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidTransition is returned when a requested transition is not
// permitted by the cleanup state machine.
var ErrInvalidTransition = errors.New("invalid state transition")

// ErrTerminalRequiresOutbox is returned when a non-terminal Transition call
// targets a terminal state. Terminal commits must go through CommitTerminal,
// which inserts the outbox row in the same transaction.
var ErrTerminalRequiresOutbox = errors.New("terminal transitions require CommitTerminal with an outbox row")

// ErrTransitionNotTerminal is returned when CommitTerminal is called with a
// non-terminal target state.
var ErrTransitionNotTerminal = errors.New("CommitTerminal requires a terminal state")

// Transition performs a non-terminal state transition. Terminal target states
// are refused; use CommitTerminal so the outbox row is committed atomically.
func (s *Store) Transition(ctx context.Context, runID string, to State, reason string) error {
	if IsTerminal(to) {
		return fmt.Errorf("%w: %q is terminal", ErrTerminalRequiresOutbox, to)
	}
	return s.commitTransition(ctx, runID, to, reason, false, OutboxRow{})
}

// CommitTerminal performs a terminal state transition and inserts the
// finalization outbox row in the same SQLite transaction. Either both commit
// or neither does (ADR-0003 §2).
func (s *Store) CommitTerminal(ctx context.Context, runID string, to State, reason string, outbox OutboxRow) error {
	if !IsTerminal(to) {
		return fmt.Errorf("%w: %q is not terminal", ErrTransitionNotTerminal, to)
	}
	return s.commitTransition(ctx, runID, to, reason, true, outbox)
}

func (s *Store) commitTransition(ctx context.Context, runID string, to State, reason string, terminal bool, outbox OutboxRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var from State
	err = tx.QueryRowContext(ctx, `SELECT state FROM runs WHERE id = ?`, runID).Scan(&from)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRunNotFound
		}
		return fmt.Errorf("select state: %w", err)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
	}

	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM state_transitions WHERE run_id = ?`, runID).Scan(&maxSeq); err != nil {
		return fmt.Errorf("select max transition seq: %w", err)
	}
	nextSeq := int64(1)
	if maxSeq.Valid {
		nextSeq = maxSeq.Int64 + 1
	}
	now := nowStamp()

	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET state = ?, updated_at = ? WHERE id = ?`, to, now, runID); err != nil {
		return fmt.Errorf("update run state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO state_transitions
		(run_id, seq, from_state, to_state, reason, written_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		runID, nextSeq, from, to, reason, now); err != nil {
		return fmt.Errorf("insert transition: %w", err)
	}
	if terminal && !mutationHooks.noOutbox {
		if _, err := tx.ExecContext(ctx, `INSERT INTO finalization_outbox
			(run_id, terminal_state, supervisor_pid, supervisor_pgid, socket_path, token, acknowledged, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
			runID, to, outbox.SupervisorPID, outbox.SupervisorPGID,
			outbox.SocketPath, outbox.Token, now); err != nil {
			return fmt.Errorf("insert outbox row: %w", err)
		}
	}
	return tx.Commit()
}

// Transitions returns the ordered state-transition history for a run.
func (s *Store) Transitions(ctx context.Context, runID string) ([]TransitionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		run_id, seq, from_state, to_state, reason, written_at
	FROM state_transitions WHERE run_id = ? ORDER BY seq ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TransitionRecord
	for rows.Next() {
		var (
			tr  TransitionRecord
			whn string
		)
		if err := rows.Scan(&tr.RunID, &tr.Seq, &tr.FromState, &tr.ToState, &tr.Reason, &whn); err != nil {
			return nil, err
		}
		if tr.WrittenAt, err = parseStamp(whn); err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

// TransitionRecord is a single state-transition journal entry.
type TransitionRecord struct {
	RunID     string
	Seq       int64
	FromState State
	ToState   State
	Reason    string
	WrittenAt time.Time
}
