package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestSchemaVersionMigrationFromV5DefaultsTranscriptIdentityUnknown(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "v5fixture.sqlite")
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	for _, migration := range migrations[:5] {
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
	if _, err := rawDB.ExecContext(ctx, `INSERT INTO projects (id, name, root, created_at) VALUES (?, ?, ?, ?)`, "p", "p", "/r", now); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `INSERT INTO workstreams (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`, "w", "p", "main", now); err != nil {
		t.Fatalf("seed workstream: %v", err)
	}
	// This is the complete v5 projection. v6 columns intentionally do not exist yet.
	if _, err := rawDB.ExecContext(ctx, `INSERT INTO runs (
		id, project_id, workstream_id, state, worker_path, worker_args, worker_env,
		transcript_path, socket_path, token, supervisor_pid, supervisor_pgid, worker_pid,
		committed_offset, created_at, updated_at, identity_path, transcript_required,
		transcript_consumed_offset, transcript_final_size, cancel_requested
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"r", "p", "w", StateRunning, "/bin/true", "[]", "[]", "/t", "/s", "tok",
		1, 1, 2, 0, now, now, "/id", 1, 0, 0, 0); err != nil {
		t.Fatalf("seed v5 run: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close v5 fixture: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after v5 fixture: %v", err)
	}
	defer s.Close()
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != 6 {
		t.Fatalf("SchemaVersion = %d, want 6", version)
	}
	run, err := s.GetRun(ctx, "r")
	if err != nil {
		t.Fatalf("GetRun after v5 migration: %v", err)
	}
	if run.TranscriptIdentity != (TranscriptFileIdentity{}) {
		t.Fatalf("migrated transcript identity = %+v, want explicit unknown", run.TranscriptIdentity)
	}
	if err := s.CommitTerminal(ctx, run.ID, StateFailed, "must remain fail-closed", OutboxRow{}); !errors.Is(err, ErrTerminalTranscriptIdentity) {
		t.Fatalf("CommitTerminal error = %v, want ErrTerminalTranscriptIdentity", err)
	}
}

func TestTranscriptIdentityRoundTripAndValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRun(t, s, "identity-round-trip")
	want := TranscriptFileIdentity{Device: 101, Inode: 202}
	if err := s.SetTranscriptIdentity(ctx, "identity-round-trip", want); err != nil {
		t.Fatalf("SetTranscriptIdentity: %v", err)
	}
	if err := s.SetTranscriptIdentity(ctx, "identity-round-trip", want); err != nil {
		t.Fatalf("idempotent SetTranscriptIdentity: %v", err)
	}
	run, err := s.GetRun(ctx, "identity-round-trip")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.TranscriptIdentity != want {
		t.Fatalf("GetRun transcript identity = %+v, want %+v", run.TranscriptIdentity, want)
	}
	active, err := s.ListActiveRuns(ctx)
	if err != nil {
		t.Fatalf("ListActiveRuns: %v", err)
	}
	if len(active) != 1 || active[0].TranscriptIdentity != want {
		t.Fatalf("ListActiveRuns identity = %+v, want %+v", active, want)
	}
	projectRuns, err := s.ListRunsByProject(ctx, run.ProjectID)
	if err != nil {
		t.Fatalf("ListRunsByProject: %v", err)
	}
	if len(projectRuns) != 1 || projectRuns[0].TranscriptIdentity != want {
		t.Fatalf("ListRunsByProject identity = %+v, want %+v", projectRuns, want)
	}
	if err := s.SetTranscriptIdentity(ctx, run.ID, TranscriptFileIdentity{Device: 101, Inode: 303}); !errors.Is(err, ErrTranscriptIdentityConflict) {
		t.Fatalf("identity replacement error = %v, want ErrTranscriptIdentityConflict", err)
	}

	for _, invalid := range []TranscriptFileIdentity{
		{},
		{Device: 1},
		{Inode: 1},
		{Device: -1, Inode: 1},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("Validate(%+v) succeeded", invalid)
		}
	}
}

func TestCommitTerminalRequiresTranscriptIdentityButNoProcessFailureClearsIt(t *testing.T) {
	t.Run("normal terminal guard", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		seedRun(t, s, "terminal-identity")
		if _, err := s.DB().ExecContext(ctx, `UPDATE runs SET transcript_required = 1, transcript_final_size = 0 WHERE id = ?`, "terminal-identity"); err != nil {
			t.Fatalf("require transcript: %v", err)
		}
		if err := s.Transition(ctx, "terminal-identity", StateRunning, "launch"); err != nil {
			t.Fatalf("Transition: %v", err)
		}
		if err := s.CommitTerminal(ctx, "terminal-identity", StateCompleted, "complete", OutboxRow{}); !errors.Is(err, ErrTerminalTranscriptIdentity) {
			t.Fatalf("CommitTerminal error = %v, want ErrTerminalTranscriptIdentity", err)
		}
		if err := s.SetTranscriptIdentity(ctx, "terminal-identity", TranscriptFileIdentity{Device: 11, Inode: 22}); err != nil {
			t.Fatalf("SetTranscriptIdentity: %v", err)
		}
		if err := s.CommitTerminal(ctx, "terminal-identity", StateCompleted, "complete", OutboxRow{}); err != nil {
			t.Fatalf("CommitTerminal with identity: %v", err)
		}
	})

	t.Run("no process exception", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		seedRun(t, s, "no-process-identity")
		if _, err := s.DB().ExecContext(ctx, `UPDATE runs SET transcript_required = 1 WHERE id = ?`, "no-process-identity"); err != nil {
			t.Fatalf("require transcript: %v", err)
		}
		if err := s.SetTranscriptIdentity(ctx, "no-process-identity", TranscriptFileIdentity{Device: 33, Inode: 44}); err != nil {
			t.Fatalf("SetTranscriptIdentity: %v", err)
		}
		if err := s.CommitNoProcessFailure(ctx, "no-process-identity", "worker never launched"); err != nil {
			t.Fatalf("CommitNoProcessFailure: %v", err)
		}
		run, err := s.GetRun(ctx, "no-process-identity")
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if run.TranscriptIdentity != (TranscriptFileIdentity{}) || run.TranscriptConsumedOffset != 0 || run.TranscriptFinalSize != 0 {
			t.Fatalf("no-process transcript authority = identity %+v consumed=%d final=%d, want unknown/0/0",
				run.TranscriptIdentity, run.TranscriptConsumedOffset, run.TranscriptFinalSize)
		}
	})
}
