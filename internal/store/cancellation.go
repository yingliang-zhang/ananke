package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrRunTerminal is returned when cancellation is requested after a run has
// already committed a terminal state.
var ErrRunTerminal = errors.New("run is already terminal")

// ErrCancellationRequested prevents a natural successful exit from committing
// completed after cancellation intent was durably accepted.
var ErrCancellationRequested = errors.New("cancellation was requested")

// RequestCancellation durably records cancellation intent. Running runs also
// enter cancelling in the same transaction. Created runs retain their bootstrap
// state, while cleanup_required and recovery_unknown retain their stronger
// recovery obligations. Repeated requests are idempotent.
func (s *Store) RequestCancellation(ctx context.Context, runID string) (State, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		from      State
		requested int
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT state, cancel_requested FROM runs WHERE id = ?`, runID).
		Scan(&from, &requested); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrRunNotFound
		}
		return "", fmt.Errorf("select cancellation state: %w", err)
	}
	if IsTerminal(from) {
		return from, fmt.Errorf("%w: %q", ErrRunTerminal, from)
	}

	to := from
	if from == StateRunning {
		to = StateCancelling
	}
	if requested != 0 && to == from {
		return to, nil
	}

	now := nowStamp()
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET state = ?, cancel_requested = 1, updated_at = ? WHERE id = ?`,
		to, now, runID); err != nil {
		return "", fmt.Errorf("persist cancellation intent: %w", err)
	}
	if to != from {
		var maxSeq sql.NullInt64
		if err := tx.QueryRowContext(ctx,
			`SELECT MAX(seq) FROM state_transitions WHERE run_id = ?`, runID).
			Scan(&maxSeq); err != nil {
			return "", fmt.Errorf("select max cancellation transition seq: %w", err)
		}
		nextSeq := int64(1)
		if maxSeq.Valid {
			nextSeq = maxSeq.Int64 + 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO state_transitions
			(run_id, seq, from_state, to_state, reason, written_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			runID, nextSeq, from, to, "cancellation requested", now); err != nil {
			return "", fmt.Errorf("insert cancellation transition: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return to, nil
}
