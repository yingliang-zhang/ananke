package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestSchemaVersionMigrationFromV1Fixture builds a database at schema v1 only
// (the historical, pre-identity-path schema), inserts a run, then reopens it
// through Open. The migration machinery must advance it to the current schema
// version, add the identity_path column, and preserve all existing data.
func TestSchemaVersionMigrationFromV1Fixture(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v1fixture.sqlite")
	ctx := context.Background()

	// 1. Build a v1-only fixture by applying just the first migration. The
	// schema_version table is normally bootstrapped by runMigrations; the
	// fixture creates it and records version 1 directly.
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	defer rawDB.Close()

	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin v1 fixture tx: %v", err)
	}
	if err := migrations[0].up(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("apply v1 migration: %v", err)
	}
	now := nowStamp()
	seedStmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_version (version, applied_at) VALUES (1, '` + now + `')`,
		`INSERT INTO projects (id, name, root, created_at) VALUES ('p', 'p', '/r', '` + now + `')`,
		`INSERT INTO workstreams (id, project_id, name, created_at) VALUES ('w', 'p', 'm', '` + now + `')`,
		// A run inserted against the v1 schema: no identity_path column yet.
		`INSERT INTO runs (id, project_id, workstream_id, state,
			worker_path, worker_args, worker_env, transcript_path, socket_path, token,
			supervisor_pid, supervisor_pgid, worker_pid, committed_offset,
			created_at, updated_at)
			VALUES ('r1', 'p', 'w', 'created',
				'/bin/echo', '[]', '[]', '/t', '/s', 'tok',
				0, 0, 0, 0, '` + now + `', '` + now + `')`,
		`INSERT INTO events (run_id, seq, type, payload, transcript_offset, written_at)
			VALUES ('r1', 1, 'started', '{}', 10, '` + now + `')`,
	}
	for _, stmt := range seedStmts {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed v1 row: %v\nstmt: %s", err, stmt)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit v1 fixture: %v", err)
	}

	// 2. Reopen through Open, which must migrate v1 -> v2 (add identity_path).
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after v1 fixture: %v", err)
	}
	defer s.Close()

	v, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != len(migrations) {
		t.Errorf("SchemaVersion = %d, want %d (head)", v, len(migrations))
	}

	// 3. The identity_path column now exists; the v1 row gets its default ''.
	run, err := s.GetRun(ctx, "r1")
	if err != nil {
		t.Fatalf("GetRun r1 after migration: %v", err)
	}
	if run.ID != "r1" || run.State != StateCreated {
		t.Errorf("post-migration run = %+v, want r1/created", run)
	}
	if run.IdentityPath != "" {
		t.Errorf("v1 row identity_path = %q, want '' (default after ADD COLUMN)", run.IdentityPath)
	}
	// Existing event survives the migration with its sequence intact.
	evs, err := s.ListEvents(ctx, "r1", 0)
	if err != nil {
		t.Fatalf("ListEvents after migration: %v", err)
	}
	if len(evs) != 1 || evs[0].Seq != 1 {
		t.Errorf("events after migration = %v, want [seq=1]", evs)
	}

	// 4. A run created against the migrated schema can set identity_path.
	if err := s.CreateProject(ctx, "p2", "p2", "/r2"); err != nil {
		t.Fatalf("CreateProject p2: %v", err)
	}
	if err := s.CreateWorkstream(ctx, "w2", "p2", "m2"); err != nil {
		t.Fatalf("CreateWorkstream w2: %v", err)
	}
	if err := s.CreateRun(ctx, "r2", "p2", "w2", RunSpec{
		WorkerPath:     "/bin/echo",
		TranscriptPath: "/t2",
		SocketPath:     "/s2",
		Token:          "tok2",
		IdentityPath:   "/id2.json",
	}); err != nil {
		t.Fatalf("CreateRun r2: %v", err)
	}
	r2, err := s.GetRun(ctx, "r2")
	if err != nil {
		t.Fatalf("GetRun r2: %v", err)
	}
	if r2.IdentityPath != "/id2.json" {
		t.Errorf("r2 identity_path = %q, want /id2.json", r2.IdentityPath)
	}
}
