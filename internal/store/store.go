package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the durable SQLite journal. All terminal state transitions are
// committed in the same transaction as their finalization outbox row.
type Store struct {
	db *sql.DB

	mu       sync.Mutex
	closed   bool
	migrOnce sync.Once
	migrErr  error
}

// Open opens (or creates) the SQLite database at path and runs all pending
// schema migrations to completion before returning.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Each handle uses one connection; `_txlock=immediate` in sqliteDSN
	// serializes write transactions across every handle and process.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// sqliteDSN builds the modernc.org/sqlite DSN with the durable pragmas
// (WAL journal, bounded busy timeout, enforced foreign keys) applied to every
// connection. Shared with migration-fixture builders so fixtures use the same
// durability configuration as production.
func sqliteDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)",
		path,
	)
}

// Close releases the database handle.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// DB exposes the underlying handle for engine-level transaction composition
// (e.g. crash-injection tests). Callers must not close it.
func (s *Store) DB() *sql.DB { return s.db }

// nowStamp returns a UTC RFC3339 timestamp with nanosecond precision.
func nowStamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// parseStamp parses a UTC RFC3339Nano timestamp produced by nowStamp.
func parseStamp(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// migration is a single schema upgrade step applied inside its own transaction.
type migration struct {
	version int
	up      func(ctx context.Context, tx *sql.Tx) error
}

// migrations is the ordered, immutable list of schema upgrades. v1 establishes
// the base journal; v2 adds supervisor identity; v3 adds outbox diagnostics;
// v4 adds the durable transcript-finalization handoff; v5 adds durable
// cancellation intent; v6 adds durable transcript file identity; v7 adds the
// P1b durable task-proposal journal; v8 repairs v7 identity constraints; and
// v9 makes the Approval and RevisionLifecycle pair reciprocal.
var migrations = []migration{
	{version: 1, up: migrateV1},
	{version: 2, up: migrateV2},
	{version: 3, up: migrateV3},
	{version: 4, up: migrateV4},
	{version: 5, up: migrateV5},
	{version: 6, up: migrateV6},
	{version: 7, up: migrateV7},
	{version: 8, up: migrateV8},
	{version: 9, up: migrateV9},
}

// SchemaVersion reports the highest applied migration version.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func (s *Store) migrate(ctx context.Context) error {
	s.migrOnce.Do(func() {
		s.migrErr = s.runMigrations(ctx)
	})
	return s.migrErr
}

func (s *Store) runMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	current, err := s.validateSchemaVersionHistory(ctx)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := m.up(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration v%d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`,
			m.version, nowStamp()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateSchemaVersionHistory(ctx context.Context) (int, error) {
	head := 0
	for i, migration := range migrations {
		expected := i + 1
		if migration.version != expected {
			return 0, fmt.Errorf("inconsistent migration definitions: position %d has version %d", expected, migration.version)
		}
		head = migration.version
	}

	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_version ORDER BY version`)
	if err != nil {
		return 0, fmt.Errorf("read schema version history: %w", err)
	}
	defer rows.Close()

	current := 0
	expected := 1
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return 0, fmt.Errorf("scan schema version history: %w", err)
		}
		if version <= 0 {
			return 0, fmt.Errorf("schema version history contains non-positive version %d", version)
		}
		if version > head {
			return 0, fmt.Errorf("schema version %d is newer than supported head %d", version, head)
		}
		if version < expected {
			return 0, fmt.Errorf("schema version history contains duplicate version %d", version)
		}
		if version > expected {
			return 0, fmt.Errorf("schema version history has gap: expected version %d, found %d", expected, version)
		}
		current = version
		expected++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read schema version history: %w", err)
	}
	return current, nil
}

func migrateV1(_ context.Context, tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE projects (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			root       TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE workstreams (
			id         TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			name       TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id)
		)`,
		// runs.identity_path is added in v2.
		`CREATE TABLE runs (
			id               TEXT PRIMARY KEY,
			project_id       TEXT NOT NULL,
			workstream_id    TEXT NOT NULL,
			state            TEXT NOT NULL,
			worker_path      TEXT NOT NULL,
			worker_args      TEXT NOT NULL,
			worker_env       TEXT NOT NULL,
			transcript_path  TEXT NOT NULL,
			socket_path      TEXT NOT NULL,
			token            TEXT NOT NULL,
			supervisor_pid   INTEGER NOT NULL DEFAULT 0,
			supervisor_pgid INTEGER NOT NULL DEFAULT 0,
			worker_pid        INTEGER NOT NULL DEFAULT 0,
			committed_offset  INTEGER NOT NULL DEFAULT 0,
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id),
			FOREIGN KEY (workstream_id) REFERENCES workstreams(id)
		)`,
		`CREATE TABLE events (
			run_id            TEXT NOT NULL,
			seq               INTEGER NOT NULL,
			type              TEXT NOT NULL,
			payload           TEXT NOT NULL,
			transcript_offset INTEGER NOT NULL DEFAULT 0,
			written_at        TEXT NOT NULL,
			PRIMARY KEY (run_id, seq),
			FOREIGN KEY (run_id) REFERENCES runs(id)
		)`,
		`CREATE TABLE state_transitions (
			run_id      TEXT NOT NULL,
			seq         INTEGER NOT NULL,
			from_state  TEXT NOT NULL,
			to_state    TEXT NOT NULL,
			reason      TEXT,
			written_at  TEXT NOT NULL,
			PRIMARY KEY (run_id, seq),
			FOREIGN KEY (run_id) REFERENCES runs(id)
		)`,
		`CREATE TABLE finalization_outbox (
			run_id           TEXT PRIMARY KEY,
			terminal_state   TEXT NOT NULL,
			supervisor_pid   INTEGER,
			supervisor_pgid  INTEGER,
			socket_path      TEXT,
			token            TEXT,
			acknowledged      INTEGER NOT NULL DEFAULT 0,
			created_at       TEXT NOT NULL,
			acknowledged_at   TEXT,
			FOREIGN KEY (run_id) REFERENCES runs(id)
		)`,
		`CREATE INDEX idx_runs_state ON runs(state)`,
		`CREATE INDEX idx_events_run_seq ON events(run_id, seq)`,
		`CREATE INDEX idx_outbox_ack ON finalization_outbox(acknowledged)`,
	}
	for _, st := range stmts {
		if _, err := tx.Exec(st); err != nil {
			return err
		}
	}
	return nil
}

func migrateV2(_ context.Context, tx *sql.Tx) error {
	// The daemon monitors the supervisor identity file to reconnect after a
	// crash (ADR-0002 §3). Record its path on the run row.
	_, err := tx.Exec(`ALTER TABLE runs ADD COLUMN identity_path TEXT NOT NULL DEFAULT ''`)
	return err
}

func migrateV3(_ context.Context, tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE finalization_outbox ADD COLUMN diagnostic TEXT NOT NULL DEFAULT ''`)
	return err
}

func migrateV4(_ context.Context, tx *sql.Tx) error {
	statements := []string{
		`ALTER TABLE runs ADD COLUMN transcript_required INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN transcript_consumed_offset INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN transcript_final_size INTEGER NOT NULL DEFAULT -1`,
		`UPDATE runs SET transcript_consumed_offset = committed_offset`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateV5(_ context.Context, tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE runs ADD COLUMN cancel_requested INTEGER NOT NULL DEFAULT 0`)
	return err

}

func migrateV6(_ context.Context, tx *sql.Tx) error {
	statements := []string{
		`ALTER TABLE runs ADD COLUMN transcript_device INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE runs ADD COLUMN transcript_inode INTEGER NOT NULL DEFAULT 0`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateV7(ctx context.Context, tx *sql.Tx) error {
	return createTaskProposalSchemaV7(ctx, tx)
}

// migrateV8 rebuilds the original v7 task-proposal tables so databases created
// before the identity fix receive the same composite foreign-key guarantees as
// new databases.
func migrateV8(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return fmt.Errorf("defer proposal foreign keys: %w", err)
	}
	legacyTables := []string{
		"task_proposal_idempotency",
		"task_proposal_activity",
		"task_proposal_revision_lifecycles",
		"task_proposal_approvals",
		"task_proposal_revisions",
		"task_proposals",
	}
	for _, table := range legacyTables {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s_v7`, table, table)); err != nil {
			return fmt.Errorf("rename legacy %s: %w", table, err)
		}
	}
	if err := createTaskProposalSchemaV8(ctx, tx); err != nil {
		return err
	}
	for _, copy := range []struct {
		table   string
		columns string
	}{
		{"task_proposals", "proposal_id, project_id, workstream_id, created_at, created_by, state, current_revision, current_revision_hash"},
		{"task_proposal_revisions", "proposal_id, revision, revision_hash, snapshot_json"},
		{"task_proposal_approvals", "approval_id, proposal_id, revision, revision_hash, created_at, created_by, state, decided_at, decided_by, decision_idempotency_key, reason"},
		{"task_proposal_revision_lifecycles", "proposal_id, revision, revision_hash, approval_id, state, created_at, updated_at, version"},
		{"task_proposal_activity", "proposal_id, sequence, operation, revision, revision_hash, approval_id, written_at"},
		{"task_proposal_idempotency", "actor, operation, resource, idempotency_key, request_body_hash, proposal_id, revision, revision_hash, approval_id"},
	} {
		statement := fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s_v7`, copy.table, copy.columns, copy.columns, copy.table)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("copy legacy %s: %w", copy.table, err)
		}
	}
	for _, table := range legacyTables {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE %s_v7`, table)); err != nil {
			return fmt.Errorf("drop legacy %s: %w", table, err)
		}
	}
	return nil
}

// migrateV9 rebuilds the v8 task-proposal tables so each Approval must name
// its one RevisionLifecycle pair. The reciprocal foreign key is deferred
// because a mutation creates the Approval and its lifecycle atomically.
func migrateV9(ctx context.Context, tx *sql.Tx) error {
	return rebuildTaskProposalTables(ctx, tx, "v8", createTaskProposalSchemaV9)
}

func rebuildTaskProposalTables(ctx context.Context, tx *sql.Tx, suffix string, createSchema func(context.Context, *sql.Tx) error) error {
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return fmt.Errorf("defer proposal foreign keys: %w", err)
	}
	legacyTables := []string{
		"task_proposal_idempotency",
		"task_proposal_activity",
		"task_proposal_revision_lifecycles",
		"task_proposal_approvals",
		"task_proposal_revisions",
		"task_proposals",
	}
	for _, table := range legacyTables {
		legacyTable := fmt.Sprintf("%s_%s", table, suffix)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, table, legacyTable)); err != nil {
			return fmt.Errorf("rename legacy %s: %w", table, err)
		}
	}
	if err := createSchema(ctx, tx); err != nil {
		return err
	}
	for _, copy := range []struct {
		table   string
		columns string
	}{
		{"task_proposals", "proposal_id, project_id, workstream_id, created_at, created_by, state, current_revision, current_revision_hash"},
		{"task_proposal_revisions", "proposal_id, revision, revision_hash, snapshot_json"},
		{"task_proposal_approvals", "approval_id, proposal_id, revision, revision_hash, created_at, created_by, state, decided_at, decided_by, decision_idempotency_key, reason"},
		{"task_proposal_revision_lifecycles", "proposal_id, revision, revision_hash, approval_id, state, created_at, updated_at, version"},
		{"task_proposal_activity", "proposal_id, sequence, operation, revision, revision_hash, approval_id, written_at"},
		{"task_proposal_idempotency", "actor, operation, resource, idempotency_key, request_body_hash, proposal_id, revision, revision_hash, approval_id"},
	} {
		legacyTable := fmt.Sprintf("%s_%s", copy.table, suffix)
		statement := fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s`, copy.table, copy.columns, copy.columns, legacyTable)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("copy legacy %s: %w", copy.table, err)
		}
	}
	for _, table := range legacyTables {
		legacyTable := fmt.Sprintf("%s_%s", table, suffix)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE %s`, legacyTable)); err != nil {
			return fmt.Errorf("drop legacy %s: %w", table, err)
		}
	}
	return nil
}

// createTaskProposalSchemaV7 preserves the released v7 proposal DDL. Later
// migrations must rebuild this shape rather than revise it in place.
func createTaskProposalSchemaV7(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE task_proposals (
			proposal_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			workstream_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL CHECK (created_by = 'local_gui_operator'),
			state TEXT NOT NULL CHECK (state IN ('open', 'approved', 'withdrawn')),
			current_revision INTEGER NOT NULL CHECK (current_revision > 0),
			current_revision_hash TEXT NOT NULL
		)`,
		`CREATE TABLE task_proposal_revisions (
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			snapshot_json TEXT NOT NULL,
			PRIMARY KEY (proposal_id, revision),
			FOREIGN KEY (proposal_id) REFERENCES task_proposals(proposal_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_approvals (
			approval_id TEXT PRIMARY KEY,
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL CHECK (created_by = 'local_gui_operator'),
			state TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'superseded', 'withdrawn')),
			decided_at TEXT,
			decided_by TEXT,
			decision_idempotency_key TEXT,
			reason TEXT,
			UNIQUE (proposal_id, revision),
			FOREIGN KEY (proposal_id, revision)
				REFERENCES task_proposal_revisions(proposal_id, revision)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_revision_lifecycles (
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			approval_id TEXT NOT NULL UNIQUE,
			state TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'superseded', 'withdrawn')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			version INTEGER NOT NULL CHECK (version > 0),
			PRIMARY KEY (proposal_id, revision),
			FOREIGN KEY (proposal_id, revision)
				REFERENCES task_proposal_revisions(proposal_id, revision)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (approval_id) REFERENCES task_proposal_approvals(approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_activity (
			proposal_id TEXT NOT NULL,
			sequence INTEGER NOT NULL CHECK (sequence > 0),
			operation TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL,
			approval_id TEXT NOT NULL,
			written_at TEXT NOT NULL,
			PRIMARY KEY (proposal_id, sequence),
			FOREIGN KEY (proposal_id, revision)
				REFERENCES task_proposal_revisions(proposal_id, revision)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_idempotency (
			actor TEXT NOT NULL CHECK (actor = 'local_gui_operator'),
			operation TEXT NOT NULL,
			resource TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_body_hash TEXT NOT NULL,
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL,
			approval_id TEXT NOT NULL,
			PRIMARY KEY (actor, operation, resource, idempotency_key),
			FOREIGN KEY (proposal_id, revision)
				REFERENCES task_proposal_revisions(proposal_id, revision)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE INDEX idx_task_proposal_revisions_hash ON task_proposal_revisions(revision_hash)`,
		`CREATE INDEX idx_task_proposal_approvals_revision ON task_proposal_approvals(proposal_id, revision)`,
		`CREATE INDEX idx_task_proposal_activity_proposal ON task_proposal_activity(proposal_id, sequence)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// createTaskProposalSchemaV8 preserves the repaired composite-identity DDL
// introduced by v8. The v9 reciprocal Approval FK belongs only in v9.
func createTaskProposalSchemaV8(_ context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE task_proposals (
			proposal_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			workstream_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL CHECK (created_by = 'local_gui_operator'),
			state TEXT NOT NULL CHECK (state IN ('open', 'approved', 'withdrawn')),
			current_revision INTEGER NOT NULL CHECK (current_revision > 0),
			current_revision_hash TEXT NOT NULL,
			UNIQUE (proposal_id, current_revision, current_revision_hash),
			FOREIGN KEY (proposal_id, current_revision, current_revision_hash)
				REFERENCES task_proposal_revisions(proposal_id, revision, revision_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_revisions (
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			snapshot_json TEXT NOT NULL,
			PRIMARY KEY (proposal_id, revision),
			UNIQUE (proposal_id, revision, revision_hash),
			FOREIGN KEY (proposal_id) REFERENCES task_proposals(proposal_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_approvals (
			approval_id TEXT PRIMARY KEY,
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL CHECK (created_by = 'local_gui_operator'),
			state TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'superseded', 'withdrawn')),
			decided_at TEXT,
			decided_by TEXT,
			decision_idempotency_key TEXT,
			reason TEXT,
			UNIQUE (proposal_id, revision),
			UNIQUE (proposal_id, revision, revision_hash, approval_id),
			FOREIGN KEY (proposal_id, revision, revision_hash)
				REFERENCES task_proposal_revisions(proposal_id, revision, revision_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_revision_lifecycles (
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			approval_id TEXT NOT NULL UNIQUE,
			state TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'superseded', 'withdrawn')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			version INTEGER NOT NULL CHECK (version > 0),
			PRIMARY KEY (proposal_id, revision),
			UNIQUE (proposal_id, revision, revision_hash, approval_id),
			FOREIGN KEY (proposal_id, revision, revision_hash)
				REFERENCES task_proposal_revisions(proposal_id, revision, revision_hash)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (proposal_id, revision, revision_hash, approval_id)
				REFERENCES task_proposal_approvals(proposal_id, revision, revision_hash, approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_activity (
			proposal_id TEXT NOT NULL,
			sequence INTEGER NOT NULL CHECK (sequence > 0),
			operation TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL,
			approval_id TEXT NOT NULL,
			written_at TEXT NOT NULL,
			PRIMARY KEY (proposal_id, sequence),
			FOREIGN KEY (proposal_id, revision, revision_hash, approval_id)
				REFERENCES task_proposal_revision_lifecycles(proposal_id, revision, revision_hash, approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_idempotency (
			actor TEXT NOT NULL CHECK (actor = 'local_gui_operator'),
			operation TEXT NOT NULL,
			resource TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_body_hash TEXT NOT NULL,
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL,
			approval_id TEXT NOT NULL,
			PRIMARY KEY (actor, operation, resource, idempotency_key),
			FOREIGN KEY (proposal_id, revision, revision_hash, approval_id)
				REFERENCES task_proposal_revision_lifecycles(proposal_id, revision, revision_hash, approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// createTaskProposalSchemaV9 is distinct from the released v8 schema so its
// migration definition remains immutable. It adds only the reciprocal
// Approval-to-RevisionLifecycle full-identity foreign key.
func createTaskProposalSchemaV9(_ context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE task_proposals (
			proposal_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			workstream_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL CHECK (created_by = 'local_gui_operator'),
			state TEXT NOT NULL CHECK (state IN ('open', 'approved', 'withdrawn')),
			current_revision INTEGER NOT NULL CHECK (current_revision > 0),
			current_revision_hash TEXT NOT NULL,
			UNIQUE (proposal_id, current_revision, current_revision_hash),
			FOREIGN KEY (proposal_id, current_revision, current_revision_hash)
				REFERENCES task_proposal_revisions(proposal_id, revision, revision_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_revisions (
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			snapshot_json TEXT NOT NULL,
			PRIMARY KEY (proposal_id, revision),
			UNIQUE (proposal_id, revision, revision_hash),
			FOREIGN KEY (proposal_id) REFERENCES task_proposals(proposal_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_approvals (
			approval_id TEXT PRIMARY KEY,
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL CHECK (created_by = 'local_gui_operator'),
			state TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'superseded', 'withdrawn')),
			decided_at TEXT,
			decided_by TEXT,
			decision_idempotency_key TEXT,
			reason TEXT,
			UNIQUE (proposal_id, revision),
			UNIQUE (proposal_id, revision, revision_hash, approval_id),
			FOREIGN KEY (proposal_id, revision, revision_hash)
				REFERENCES task_proposal_revisions(proposal_id, revision, revision_hash)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (proposal_id, revision, revision_hash, approval_id)
				REFERENCES task_proposal_revision_lifecycles(proposal_id, revision, revision_hash, approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_revision_lifecycles (
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL UNIQUE,
			approval_id TEXT NOT NULL UNIQUE,
			state TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'superseded', 'withdrawn')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			version INTEGER NOT NULL CHECK (version > 0),
			PRIMARY KEY (proposal_id, revision),
			UNIQUE (proposal_id, revision, revision_hash, approval_id),
			FOREIGN KEY (proposal_id, revision, revision_hash)
				REFERENCES task_proposal_revisions(proposal_id, revision, revision_hash)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (proposal_id, revision, revision_hash, approval_id)
				REFERENCES task_proposal_approvals(proposal_id, revision, revision_hash, approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_activity (
			proposal_id TEXT NOT NULL,
			sequence INTEGER NOT NULL CHECK (sequence > 0),
			operation TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL,
			approval_id TEXT NOT NULL,
			written_at TEXT NOT NULL,
			PRIMARY KEY (proposal_id, sequence),
			FOREIGN KEY (proposal_id, revision, revision_hash, approval_id)
				REFERENCES task_proposal_revision_lifecycles(proposal_id, revision, revision_hash, approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE task_proposal_idempotency (
			actor TEXT NOT NULL CHECK (actor = 'local_gui_operator'),
			operation TEXT NOT NULL,
			resource TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_body_hash TEXT NOT NULL,
			proposal_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK (revision > 0),
			revision_hash TEXT NOT NULL,
			approval_id TEXT NOT NULL,
			PRIMARY KEY (actor, operation, resource, idempotency_key),
			FOREIGN KEY (proposal_id, revision, revision_hash, approval_id)
				REFERENCES task_proposal_revision_lifecycles(proposal_id, revision, revision_hash, approval_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
