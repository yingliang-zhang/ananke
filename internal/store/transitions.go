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

// ErrNoProcessIdentity is returned when an already-finalized no-process
// failure is requested after any supervisor or worker identity was recorded.
var ErrNoProcessIdentity = errors.New("no-process failure requires zero process identity")

// ErrTerminalTranscriptIncomplete is returned when a transcript-required run
// has not durably sealed and consumed every transcript byte.
var ErrTerminalTranscriptIncomplete = errors.New("terminal transition requires complete transcript handoff")

// ErrTerminalTranscriptIdentity is returned when a process-backed transcript
// has no durable device/inode authority.
var ErrTerminalTranscriptIdentity = errors.New("terminal transition requires durable transcript file identity")

// Transition performs a non-terminal state transition. Terminal target states
// are refused; use CommitTerminal so the outbox row is committed atomically.
func (s *Store) Transition(ctx context.Context, runID string, to State, reason string) error {
	if IsTerminal(to) {
		return fmt.Errorf("%w: %q is terminal", ErrTerminalRequiresOutbox, to)
	}
	return s.commitTransition(ctx, runID, to, reason, transitionCommit{})
}

// CommitTerminal performs a terminal state transition and inserts the
// finalization outbox row in the same SQLite transaction. Either both commit
// or neither does (ADR-0003 §2).
func (s *Store) CommitTerminal(ctx context.Context, runID string, to State, reason string, outbox OutboxRow) error {
	if !IsTerminal(to) {
		return fmt.Errorf("%w: %q is not terminal", ErrTransitionNotTerminal, to)
	}
	return s.commitTransition(ctx, runID, to, reason, transitionCommit{terminal: true, outbox: outbox})
}

// CommitNoProcessFailure atomically records a failed run and an already
// acknowledged outbox row when launch proved that no supervisor/worker
// process exists. It is the only terminal path allowed directly from created.
func (s *Store) CommitNoProcessFailure(ctx context.Context, runID string, reason string) error {
	return s.commitTransition(ctx, runID, StateFailed, reason, transitionCommit{
		terminal:  true,
		noProcess: true,
	})
}

type transitionCommit struct {
	terminal  bool
	noProcess bool
	outbox    OutboxRow
}

func (s *Store) commitTransition(ctx context.Context, runID string, to State, reason string, commit transitionCommit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		from                                     State
		supervisorPID, supervisorPGID, workerPID int
		cancelRequested, transcriptRequired      int
		transcriptConsumed, transcriptFinal      int64
		transcriptIdentity                       TranscriptFileIdentity
	)
	err = tx.QueryRowContext(ctx, `SELECT state, supervisor_pid, supervisor_pgid, worker_pid,
		cancel_requested, transcript_required, transcript_consumed_offset, transcript_final_size,
		transcript_device, transcript_inode
		FROM runs WHERE id = ?`, runID).
		Scan(&from, &supervisorPID, &supervisorPGID, &workerPID, &cancelRequested,
			&transcriptRequired, &transcriptConsumed, &transcriptFinal,
			&transcriptIdentity.Device, &transcriptIdentity.Inode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRunNotFound
		}
		return fmt.Errorf("select state and process identity: %w", err)
	}
	if commit.terminal && to == StateCompleted && cancelRequested != 0 {
		return fmt.Errorf("%w: %q cannot complete", ErrCancellationRequested, from)
	}
	if commit.noProcess {
		if to != StateFailed || (from != StateCreated && from != StateRunning) {
			return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
		}
		if supervisorPID != 0 || supervisorPGID != 0 || workerPID != 0 {
			return fmt.Errorf("%w: supervisor=%d pgid=%d worker=%d",
				ErrNoProcessIdentity, supervisorPID, supervisorPGID, workerPID)
		}
	} else if !CanTransition(from, to) {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
	}
	if commit.noProcess {
		if _, err := tx.ExecContext(ctx, `UPDATE runs
			SET transcript_device = 0, transcript_inode = 0, updated_at = ? WHERE id = ?`,
			nowStamp(), runID); err != nil {
			return fmt.Errorf("clear no-process transcript identity: %w", err)
		}
		transcriptIdentity = TranscriptFileIdentity{}
	}
	now := nowStamp()
	if commit.noProcess && transcriptRequired != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE runs
			SET transcript_consumed_offset = 0, transcript_final_size = 0, updated_at = ?
			WHERE id = ?`, now, runID); err != nil {
			return fmt.Errorf("seal empty no-process transcript: %w", err)
		}
		transcriptConsumed = 0
		transcriptFinal = 0
	}
	if commit.terminal && !commit.noProcess && transcriptRequired != 0 {
		if !mutationHooks.allowIncompleteTerminalTranscript &&
			(transcriptFinal < 0 || transcriptConsumed != transcriptFinal) {
			return fmt.Errorf("%w: consumed=%d final=%d", ErrTerminalTranscriptIncomplete,
				transcriptConsumed, transcriptFinal)
		}
		if err := transcriptIdentity.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrTerminalTranscriptIdentity, err)
		}
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
	if commit.terminal && !mutationHooks.noOutbox {
		acknowledged := 0
		var acknowledgedAt any
		if commit.noProcess {
			acknowledged = 1
			acknowledgedAt = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO finalization_outbox
			(run_id, terminal_state, supervisor_pid, supervisor_pgid, socket_path, token,
			 acknowledged, created_at, acknowledged_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			runID, to, commit.outbox.SupervisorPID, commit.outbox.SupervisorPGID,
			commit.outbox.SocketPath, commit.outbox.Token, acknowledged, now, acknowledgedAt); err != nil {
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
