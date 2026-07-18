package store

import (
	"context"
	"fmt"
)

// terminalStateValues is the SQL-level enumeration of terminal states, kept in
// sync with IsTerminal. SQLite has no function form of IsTerminal, so the
// active-runs query enumerates them.
var terminalStateValues = []string{
	string(StateCompleted),
	string(StateFailed),
	string(StateCancelled),
}

// ListActiveRuns returns every non-terminal run PLUS every terminal run whose
// finalization outbox row is still pending (acknowledged = 0). A terminal run
// is never silently dropped from recovery until its outbox row is
// acknowledged (ADR-0003 §2 invariant 4).
func (s *Store) ListActiveRuns(ctx context.Context) ([]Run, error) {
	q := `SELECT
		id, project_id, workstream_id, state,
		worker_path, worker_args, worker_env,
		transcript_path, socket_path, token, identity_path,
		supervisor_pid, supervisor_pgid, worker_pid, committed_offset,
		created_at, updated_at
	FROM runs r
	WHERE r.state NOT IN ('completed','failed','cancelled')
	   OR EXISTS (
		SELECT 1 FROM finalization_outbox o
		WHERE o.run_id = r.id AND o.acknowledged = 0
	   )
	ORDER BY r.created_at ASC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list active runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListRunsByProject returns all runs under a project, ordered by creation.
func (s *Store) ListRunsByProject(ctx context.Context, projectID string) ([]Run, error) {
	q := `SELECT
		id, project_id, workstream_id, state,
		worker_path, worker_args, worker_env,
		transcript_path, socket_path, token, identity_path,
		supervisor_pid, supervisor_pgid, worker_pid, committed_offset,
		created_at, updated_at
	FROM runs WHERE project_id = ? ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetRunSupervisor records the supervisor identity (pid/pgid) and worker pid on
// the run row, used by the daemon after it has launched the supervisor and
// read the identity file.
func (s *Store) SetRunSupervisor(ctx context.Context, runID string, supervisorPID, supervisorPGID, workerPID int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET supervisor_pid = ?, supervisor_pgid = ?, worker_pid = ?, updated_at = ? WHERE id = ?`,
		supervisorPID, supervisorPGID, workerPID, nowStamp(), runID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRunNotFound
	}
	return nil
}
