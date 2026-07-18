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

// RunSpec is the immutable launch configuration recorded at run creation.
type RunSpec struct {
	WorkerPath     string
	WorkerArgs     []string
	WorkerEnv      []string
	TranscriptPath string
	SocketPath     string
	Token          string
	IdentityPath   string
}

// Run is a run row projected from the journal.
type Run struct {
	ID              string
	ProjectID       string
	WorkstreamID    string
	State           State
	WorkerPath      string
	WorkerArgs      []string
	WorkerEnv       []string
	TranscriptPath  string
	SocketPath      string
	Token           string
	IdentityPath    string
	SupervisorPID   int
	SupervisorPGID  int
	WorkerPID       int
	CommittedOffset int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
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
		transcript_path, socket_path, token, identity_path,
		supervisor_pid, supervisor_pgid, worker_pid, committed_offset,
		created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, workstreamID, StateCreated,
		spec.WorkerPath, string(args), string(env),
		spec.TranscriptPath, spec.SocketPath, spec.Token, spec.IdentityPath,
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
		transcript_path, socket_path, token, identity_path,
		supervisor_pid, supervisor_pgid, worker_pid, committed_offset,
		created_at, updated_at
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
		r         Run
		argsJSON  string
		envJSON   string
		createdAt string
		updatedAt string
	)
	err := row.Scan(
		&r.ID, &r.ProjectID, &r.WorkstreamID, &r.State,
		&r.WorkerPath, &argsJSON, &envJSON,
		&r.TranscriptPath, &r.SocketPath, &r.Token, &r.IdentityPath,
		&r.SupervisorPID, &r.SupervisorPGID, &r.WorkerPID, &r.CommittedOffset,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return Run{}, err
	}
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
