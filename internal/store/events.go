package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Event is a single journaled transcript event for a run.
type Event struct {
	RunID            string
	Seq              int64
	Type             string
	Payload          []byte
	TranscriptOffset int64
	WrittenAt        time.Time
}

// AppendEvent appends a transcript event to the journal with a per-run
// monotonic sequence number and commits the transcript offset atomically in
// the same transaction. The committed offset never rewinds: it advances to
// max(committed_offset, transcriptOffset).
func (s *Store) AppendEvent(ctx context.Context, runID, typ string, payload []byte, transcriptOffset int64) (Event, error) {
	if runID == "" {
		return Event{}, errors.New("run id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM events WHERE run_id = ?`, runID).Scan(&maxSeq); err != nil {
		return Event{}, fmt.Errorf("select max seq: %w", err)
	}
	nextSeq := int64(1)
	if maxSeq.Valid {
		nextSeq = maxSeq.Int64 + 1
	}
	now := nowStamp()
	if _, err := tx.ExecContext(ctx, `INSERT INTO events
		(run_id, seq, type, payload, transcript_offset, written_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		runID, nextSeq, typ, string(payload), transcriptOffset, now); err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}
	// Atomic offset commit: the durable high-water mark advances with the
	// event row in this transaction and never moves backwards.
	res, err := tx.ExecContext(ctx,
		`UPDATE runs SET committed_offset = MAX(committed_offset, ?), updated_at = ? WHERE id = ?`,
		transcriptOffset, now, runID)
	if err != nil {
		return Event{}, fmt.Errorf("update committed offset: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Event{}, ErrRunNotFound
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	writtenAt, _ := time.Parse(time.RFC3339Nano, now)
	return Event{
		RunID:            runID,
		Seq:              nextSeq,
		Type:             typ,
		Payload:          payload,
		TranscriptOffset: transcriptOffset,
		WrittenAt:        writtenAt,
	}, nil
}

// CommittedOffset returns the durable transcript high-water mark for a run.
func (s *Store) CommittedOffset(ctx context.Context, runID string) (int64, error) {
	var off int64
	err := s.db.QueryRowContext(ctx,
		`SELECT committed_offset FROM runs WHERE id = ?`, runID).Scan(&off)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrRunNotFound
		}
		return 0, err
	}
	return off, nil
}

// ListEvents returns all events for a run with sequence strictly greater than
// afterSeq, ordered by ascending sequence. Pass afterSeq=0 to read from the
// start. This is the reconnectable read path: a reader persists its last
// consumed sequence and resumes without loss or duplication after a crash.
func (s *Store) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		run_id, seq, type, payload, transcript_offset, written_at
	FROM events WHERE run_id = ? AND seq > ? ORDER BY seq ASC`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var (
			e         Event
			payload   string
			writtenAt string
		)
		if err := rows.Scan(&e.RunID, &e.Seq, &e.Type, &payload, &e.TranscriptOffset, &writtenAt); err != nil {
			return nil, err
		}
		e.Payload = []byte(payload)
		if e.WrittenAt, err = time.Parse(time.RFC3339Nano, writtenAt); err != nil {
			return nil, fmt.Errorf("parse written_at: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// HighestEventSeq returns the highest committed event sequence for a run, or 0
// if the run has no events.
func (s *Store) HighestEventSeq(ctx context.Context, runID string) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM events WHERE run_id = ?`, runID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}
