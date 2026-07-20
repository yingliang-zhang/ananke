package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrRunNotFound is returned when a run id does not exist.
var ErrRunNotFound = errors.New("run not found")

// ErrProjectNotFound is returned when a project id does not exist.
var ErrProjectNotFound = errors.New("project not found")

// ErrWorkstreamNotFound is returned when a workstream id does not exist.
var ErrWorkstreamNotFound = errors.New("workstream not found")

var (
	// ErrTranscriptIdentityUnknown reports the explicit v5 migration value.
	ErrTranscriptIdentityUnknown = errors.New("transcript file identity is unknown")
	// ErrTranscriptIdentityInvalid reports a partial or non-positive identity.
	ErrTranscriptIdentityInvalid = errors.New("transcript file identity is invalid")
	// ErrTranscriptIdentityConflict prevents replacing immutable transcript authority.
	ErrTranscriptIdentityConflict = errors.New("transcript file identity conflicts with durable authority")
)

// TranscriptFileIdentity is the durable device/inode identity of a transcript.
// Zero/zero is the explicit unknown value assigned to rows migrated from v5.
type TranscriptFileIdentity struct {
	Device int64 `json:"device"`
	Inode  int64 `json:"inode"`
}

// Validate rejects unknown, partial, negative, or otherwise non-positive identities.
func (id TranscriptFileIdentity) Validate() error {
	if id.Device == 0 && id.Inode == 0 {
		return ErrTranscriptIdentityUnknown
	}
	if id.Device <= 0 || id.Inode <= 0 {
		return fmt.Errorf("%w: device=%d inode=%d", ErrTranscriptIdentityInvalid, id.Device, id.Inode)
	}
	return nil
}

// RunSpec is the immutable launch configuration recorded at run creation.
type RunSpec struct {
	WorkerPath         string
	WorkerArgs         []string
	WorkerEnv          []string
	TranscriptPath     string
	SocketPath         string
	Token              string
	IdentityPath       string
	TranscriptRequired bool
}

// Run is a run row projected from the journal.
type Run struct {
	ID                       string
	ProjectID                string
	WorkstreamID             string
	State                    State
	WorkerPath               string
	WorkerArgs               []string
	WorkerEnv                []string
	TranscriptPath           string
	SocketPath               string
	Token                    string
	IdentityPath             string
	TranscriptIdentity       TranscriptFileIdentity
	SupervisorPID            int
	SupervisorPGID           int
	WorkerPID                int
	CommittedOffset          int64
	TranscriptRequired       bool
	CancelRequested          bool
	TranscriptConsumedOffset int64
	TranscriptFinalSize      int64
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// CreateProject inserts a project row.
func (s *Store) CreateProject(ctx context.Context, id, name, root string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, root, created_at) VALUES (?, ?, ?, ?)`,
		id, name, root, nowStamp())
	return err
}

// CreateWorkstream inserts a workstream row under an existing project.
func (s *Store) CreateWorkstream(ctx context.Context, id, projectID, name string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workstreams (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
		id, projectID, name, nowStamp())
	if err != nil {
		return fmt.Errorf("create workstream: %w", err)
	}
	return nil
}

// CreateRun inserts a run row in the `created` state and records the initial
// state transition (seq 0) in the journal.
func (s *Store) CreateRun(ctx context.Context, id, projectID, workstreamID string, spec RunSpec) error {
	if id == "" {
		return errors.New("run id required")
	}
	if spec.WorkerPath == "" {
		return errors.New("worker path required")
	}
	now := nowStamp()
	args, err := json.Marshal(spec.WorkerArgs)
	if err != nil {
		return fmt.Errorf("marshal worker args: %w", err)
	}
	env, err := json.Marshal(spec.WorkerEnv)
	if err != nil {
		return fmt.Errorf("marshal worker env: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `INSERT INTO runs (
		id, project_id, workstream_id, state,
		worker_path, worker_args, worker_env,
		transcript_path, transcript_device, transcript_inode,
		socket_path, token, identity_path, transcript_required,
		supervisor_pid, supervisor_pgid, worker_pid, committed_offset,
		created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, workstreamID, StateCreated,
		spec.WorkerPath, string(args), string(env),
		spec.TranscriptPath, 0, 0, spec.SocketPath, spec.Token, spec.IdentityPath, spec.TranscriptRequired,
		0, 0, 0, 0, now, now,
	); err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO state_transitions
		(run_id, seq, from_state, to_state, reason, written_at)
		VALUES (?, 0, '', ?, 'created', ?)`,
		id, StateCreated, now); err != nil {
		return fmt.Errorf("insert initial transition: %w", err)
	}
	return tx.Commit()
}

// GetRun loads a single run by id.
func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, project_id, workstream_id, state,
		worker_path, worker_args, worker_env,
		transcript_path, transcript_device, transcript_inode,
		socket_path, token, identity_path, transcript_required, cancel_requested,
		supervisor_pid, supervisor_pgid, worker_pid, committed_offset,
		transcript_consumed_offset, transcript_final_size, created_at, updated_at
	FROM runs WHERE id = ?`, id)
	r, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrRunNotFound
		}
		return Run{}, err
	}
	return r, nil
}

// scanRun decodes a run row from a single-row scanner.
func scanRun(row interface {
	Scan(dest ...any) error
}) (Run, error) {
	var (
		r                  Run
		argsJSON           string
		envJSON            string
		createdAt          string
		updatedAt          string
		transcriptRequired int
		cancelRequested    int
	)
	err := row.Scan(
		&r.ID, &r.ProjectID, &r.WorkstreamID, &r.State,
		&r.WorkerPath, &argsJSON, &envJSON,
		&r.TranscriptPath, &r.TranscriptIdentity.Device, &r.TranscriptIdentity.Inode,
		&r.SocketPath, &r.Token, &r.IdentityPath, &transcriptRequired, &cancelRequested,
		&r.SupervisorPID, &r.SupervisorPGID, &r.WorkerPID, &r.CommittedOffset,
		&r.TranscriptConsumedOffset, &r.TranscriptFinalSize, &createdAt, &updatedAt,
	)
	if err != nil {
		return Run{}, err
	}
	r.TranscriptRequired = transcriptRequired != 0
	r.CancelRequested = cancelRequested != 0
	if err := json.Unmarshal([]byte(argsJSON), &r.WorkerArgs); err != nil {
		return Run{}, fmt.Errorf("unmarshal worker args: %w", err)
	}
	if err := json.Unmarshal([]byte(envJSON), &r.WorkerEnv); err != nil {
		return Run{}, fmt.Errorf("unmarshal worker env: %w", err)
	}
	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return Run{}, fmt.Errorf("parse created_at: %w", err)
	}
	if r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return Run{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return r, nil
}

// SetTranscriptIdentity publishes immutable transcript authority before worker launch.
func (s *Store) SetTranscriptIdentity(ctx context.Context, runID string, identity TranscriptFileIdentity) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var current TranscriptFileIdentity
	if err := tx.QueryRowContext(ctx, `SELECT transcript_device, transcript_inode FROM runs WHERE id = ?`, runID).
		Scan(&current.Device, &current.Inode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRunNotFound
		}
		return fmt.Errorf("read transcript identity: %w", err)
	}
	if current == identity {
		return nil
	}
	if current != (TranscriptFileIdentity{}) {
		return fmt.Errorf("%w: have device=%d inode=%d, got device=%d inode=%d",
			ErrTranscriptIdentityConflict, current.Device, current.Inode, identity.Device, identity.Inode)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs
		SET transcript_device = ?, transcript_inode = ?, updated_at = ? WHERE id = ?`,
		identity.Device, identity.Inode, nowStamp(), runID); err != nil {
		return fmt.Errorf("set transcript identity: %w", err)
	}
	return tx.Commit()
}
