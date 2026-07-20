package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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
	if run.CancelRequested {
		t.Fatal("v1 row cancel_requested = true, want false default")
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

func TestSchemaVersionMigrationFromV2AddsOutboxDiagnostic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v2fixture.sqlite")
	ctx := context.Background()
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	rawDB.SetMaxOpenConns(1)

	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	for _, migration := range migrations[:2] {
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
	seed := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, root, created_at) VALUES (?, ?, ?, ?)`, []any{"p", "p", "/r", now}},
		{`INSERT INTO workstreams (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`, []any{"w", "p", "m", now}},
		{`INSERT INTO runs (id, project_id, workstream_id, state, worker_path, worker_args, worker_env,
			transcript_path, socket_path, token, supervisor_pid, supervisor_pgid, worker_pid,
			committed_offset, created_at, updated_at, identity_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"r", "p", "w", StateFailed, "/bin/false", "[]", "[]", "/t", "/s", "tok", 101, 101, 102, 0, now, now, "/id"}},
		{`INSERT INTO finalization_outbox (run_id, terminal_state, supervisor_pid, supervisor_pgid,
			socket_path, token, acknowledged, created_at) VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
			[]any{"r", StateFailed, 101, 101, "/s", "tok", now}},
	}
	for _, statement := range seed {
		if _, err := rawDB.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed v2 fixture: %v", err)
		}
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close raw fixture: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after v2 fixture: %v", err)
	}
	defer s.Close()
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("SchemaVersion = %d, want %d", version, len(migrations))
	}
	row, err := s.GetOutbox(ctx, "r")
	if err != nil {
		t.Fatalf("GetOutbox after migration: %v", err)
	}
	if row.RunID != "r" || row.Acknowledged != 0 || row.SupervisorPID != 101 {
		t.Fatalf("migrated outbox = %+v", row)
	}
	if row.Diagnostic != "" {
		t.Fatalf("migrated diagnostic = %q, want empty default", row.Diagnostic)
	}
	run, err := s.GetRun(ctx, "r")
	if err != nil {
		t.Fatalf("GetRun after v2 migration: %v", err)
	}
	if run.CancelRequested {
		t.Fatal("v2 row cancel_requested = true, want false default")
	}
}

func TestOpenRejectsInvalidSchemaVersionHistory(t *testing.T) {
	head := migrations[len(migrations)-1].version
	tests := []struct {
		name     string
		versions []int
		want     string
	}{
		{name: "future version", versions: []int{head + 1}, want: "newer than supported"},
		{name: "gapped history", versions: []int{1, 3}, want: "gap"},
		{name: "duplicate version", versions: []int{1, 1}, want: "duplicate"},
		{name: "non-positive version", versions: []int{0}, want: "non-positive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "invalid-history.sqlite")
			rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
			if err != nil {
				t.Fatalf("raw open: %v", err)
			}
			if _, err := rawDB.Exec(`CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
				t.Fatalf("create schema_version: %v", err)
			}
			for _, version := range tc.versions {
				if _, err := rawDB.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`, version, nowStamp()); err != nil {
					t.Fatalf("insert schema version %d: %v", version, err)
				}
			}
			if err := rawDB.Close(); err != nil {
				t.Fatalf("close invalid fixture: %v", err)
			}

			s, err := Open(dbPath)
			if err == nil {
				_ = s.Close()
				t.Fatalf("Open accepted schema history %v", tc.versions)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Open error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestOpenMigratesValidOldSchemaHistoryToHead(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "valid-old-history.sqlite")
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	for _, migration := range migrations[:len(migrations)-1] {
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
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close old fixture: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open valid old history: %v", err)
	}
	defer s.Close()
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != migrations[len(migrations)-1].version {
		t.Fatalf("SchemaVersion = %d, want head %d", version, migrations[len(migrations)-1].version)
	}
}
