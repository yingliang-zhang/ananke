package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSchemaVersionMigrationFromV3DefaultsCancellationIntentFalse(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "v3fixture.sqlite")
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	for _, migration := range migrations[:3] {
		tx, err := rawDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin migration v%d: %v", migration.version, err)
		}
		if err := migration.up(ctx, tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply migration v%d: %v", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`, migration.version, nowStamp()); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record migration v%d: %v", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration v%d: %v", migration.version, err)
		}
	}
	now := nowStamp()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, root, created_at) VALUES (?, ?, ?, ?)`, []any{"p", "p", "/r", now}},
		{`INSERT INTO workstreams (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`, []any{"w", "p", "m", now}},
		{`INSERT INTO runs (id, project_id, workstream_id, state, worker_path, worker_args, worker_env,
			transcript_path, socket_path, token, supervisor_pid, supervisor_pgid, worker_pid,
			committed_offset, created_at, updated_at, identity_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"r", "p", "w", StateRunning, "/bin/true", "[]", "[]", "/t", "/s", "tok", 1, 1, 2, 0, now, now, "/id"}},
	}
	for _, statement := range statements {
		if _, err := rawDB.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed v3 fixture: %v", err)
		}
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close v3 fixture: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after v3 fixture: %v", err)
	}
	defer s.Close()
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("SchemaVersion = %d, want %d", version, len(migrations))
	}
	run, err := s.GetRun(ctx, "r")
	if err != nil {
		t.Fatalf("GetRun after v3 migration: %v", err)
	}
	if run.CancelRequested {
		t.Fatal("v3 row cancel_requested = true, want false default")
	}
	if run.TranscriptIdentity != (TranscriptFileIdentity{}) {
		t.Fatalf("v3 row transcript identity = %+v, want explicit unknown", run.TranscriptIdentity)
	}
}
