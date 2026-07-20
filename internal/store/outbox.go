package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrOutboxNotFound is returned when no outbox row exists for a run, or when an
// acknowledge targets a row that is not pending.
var ErrOutboxNotFound = errors.New("outbox row not found or not pending")

// OutboxRow is a finalization outbox entry (ADR-0003 §2). A row is created in
// the same transaction as a terminal state commit and remains until the
// supervisor confirms finalization.
type OutboxRow struct {
	RunID          string
	TerminalState  State
	SupervisorPID  int
	SupervisorPGID int
	SocketPath     string
	Token          string
	Acknowledged   int // 0 pending, 1 acknowledged, -1 abandoned
	Diagnostic     string
	CreatedAt      time.Time
	AcknowledgedAt time.Time // zero if not acknowledged
}

// AcknowledgeOutbox marks the pending outbox row for a run as acknowledged. It
// is idempotent-failing: re-acknowledging an already-acknowledged row returns
// ErrOutboxNotFound.
func (s *Store) AcknowledgeOutbox(ctx context.Context, runID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE finalization_outbox SET acknowledged = 1, acknowledged_at = ?
		WHERE run_id = ? AND acknowledged = 0`,
		nowStamp(), runID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrOutboxNotFound
	}
	return nil
}

// AbandonOutbox marks a pending outbox row as abandoned (acknowledged = -1)
// when the supervisor is confirmed dead and identity is irrecoverably lost
// (ADR-0003 §2 step 3). The run stays terminal; the leak is recorded.
func (s *Store) AbandonOutbox(ctx context.Context, runID string, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("outbox abandonment diagnostic required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE finalization_outbox
		SET acknowledged = -1, acknowledged_at = ?, diagnostic = ?
		WHERE run_id = ? AND acknowledged = 0`,
		nowStamp(), reason, runID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrOutboxNotFound
	}
	return nil
}

// GetOutbox loads the outbox row for a run.
func (s *Store) GetOutbox(ctx context.Context, runID string) (OutboxRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		run_id, terminal_state, supervisor_pid, supervisor_pgid,
		socket_path, token, acknowledged, created_at, acknowledged_at, diagnostic
	FROM finalization_outbox WHERE run_id = ?`, runID)
	r, err := scanOutbox(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OutboxRow{}, ErrOutboxNotFound
		}
		return OutboxRow{}, err
	}
	return r, nil
}

// ListPendingOutbox returns all outbox rows with acknowledged = 0. These are
// the terminal runs whose supervisor finalization is still owed; they must not
// be dropped from active processing.
func (s *Store) ListPendingOutbox(ctx context.Context) ([]OutboxRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		run_id, terminal_state, supervisor_pid, supervisor_pgid,
		socket_path, token, acknowledged, created_at, acknowledged_at, diagnostic
	FROM finalization_outbox WHERE acknowledged = 0 ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		r, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanOutbox(row interface {
	Scan(dest ...any) error
}) (OutboxRow, error) {
	var (
		r          OutboxRow
		supervisor sql.NullInt64
		pgid       sql.NullInt64
		socket     sql.NullString
		token      sql.NullString
		created    string
		acked      sql.NullString
		diagnostic sql.NullString
	)
	err := row.Scan(
		&r.RunID, &r.TerminalState, &supervisor, &pgid,
		&socket, &token, &r.Acknowledged, &created, &acked, &diagnostic,
	)
	if err != nil {
		return OutboxRow{}, err
	}
	if supervisor.Valid {
		r.SupervisorPID = int(supervisor.Int64)
	}
	if pgid.Valid {
		r.SupervisorPGID = int(pgid.Int64)
	}
	if socket.Valid {
		r.SocketPath = socket.String
	}
	if token.Valid {
		r.Token = token.String
	}
	if diagnostic.Valid {
		r.Diagnostic = diagnostic.String
	}
	if r.CreatedAt, err = parseStamp(created); err != nil {
		return OutboxRow{}, fmt.Errorf("parse outbox created_at: %w", err)
	}
	if acked.Valid && acked.String != "" {
		if r.AcknowledgedAt, err = parseStamp(acked.String); err != nil {
			return OutboxRow{}, fmt.Errorf("parse outbox acknowledged_at: %w", err)
		}
	}
	return r, nil
}
