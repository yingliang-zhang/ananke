package trustedsupervisor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerJournalRejectsSQLiteURIMetacharacterPathsWithoutCreatingAnyDatabase(t *testing.T) {
	directory := t.TempDir()
	for _, testCase := range []struct {
		name       string
		filename   string
		redirected string
	}{
		{name: "question_and_ampersand", filename: "journal.sqlite?mode=memory&", redirected: "journal.sqlite"},
		{name: "ampersand", filename: "journal&mode=memory.sqlite"},
		{name: "percent_escape", filename: "journal%2eredirected.sqlite", redirected: "journal.redirected.sqlite"},
		{name: "fragment", filename: "journal.sqlite#redirected", redirected: "journal.sqlite"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(directory, testCase.filename)
			journal, err := openServerJournal(path)
			if journal != nil {
				_ = journal.Close()
			}
			if !errors.Is(err, ErrAuthentication) {
				t.Fatalf("open URI-significant journal path %q error = %v, want %v", path, err, ErrAuthentication)
			}
			for _, forbidden := range []string{path, filepath.Join(directory, testCase.redirected)} {
				if forbidden == directory || strings.HasSuffix(forbidden, string(filepath.Separator)) {
					continue
				}
				if _, statErr := os.Lstat(forbidden); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("rejected URI-significant path created %q: %v", forbidden, statErr)
				}
			}
		})
	}
}

func TestServerJournalPathReplacementStillFailsClosedBeforeSigning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-journal.sqlite")
	journal, err := openServerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	deferredClose := true
	defer func() {
		if deferredClose {
			_ = journal.Close()
		}
	}()
	if err := os.Rename(path, path+".pinned"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	signed := false
	if _, _, err := journal.transact(context.Background(), newJournalSecurityRecord("replacement", operationDeliver), func() ([]byte, error) {
		signed = true
		return []byte(`{"response":"must-not-sign"}`), nil
	}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("replacement transaction error = %v, want %v", err, ErrAuthentication)
	}
	if signed {
		t.Fatal("path replacement invoked signer")
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	deferredClose = false
}

func TestServerJournalRejectsRequestAndResponseCorruptionOnEveryReplayAndLoad(t *testing.T) {
	corruptions := []struct {
		name   string
		column string
		value  any
	}{
		{name: "request_bytes", column: "request_bytes", value: []byte(`{"request":"mutated"}`)},
		{name: "response_bytes", column: "response_bytes", value: []byte(`{"response":"mutated"}`)},
		{name: "request_bytes_hash", column: "request_bytes_hash", value: testHash("mutated-request-fingerprint")},
		{name: "response_bytes_hash", column: "response_bytes_hash", value: testHash("mutated-response-fingerprint")},
	}
	for _, access := range []string{"replay", "load"} {
		for _, corruption := range corruptions {
			t.Run(access+"_"+corruption.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "server-journal.sqlite")
				journal, err := openServerJournal(path)
				if err != nil {
					t.Fatal(err)
				}
				defer journal.Close()
				record := newJournalSecurityRecord(access+"-"+corruption.name, operationDeliver)
				seedJournalSecurityRecord(t, journal, record)
				mutateJournalRequestColumn(t, journal.db, record.RequestHash, corruption.column, corruption.value)
				assertSQLiteIntegrityClean(t, journal.db)

				var readErr error
				signed := false
				if access == "replay" {
					_, _, readErr = journal.transact(context.Background(), record, func() ([]byte, error) {
						signed = true
						return []byte(`{"response":"must-not-sign"}`), nil
					})
				} else {
					_, _, readErr = journal.loadOperation(context.Background(), record.OperationKey)
				}
				if !errors.Is(readErr, ErrAuthentication) {
					t.Fatalf("%s after %s corruption error = %v, want %v", access, corruption.name, readErr, ErrAuthentication)
				}
				if signed {
					t.Fatalf("%s corruption invoked signer", corruption.name)
				}
			})
		}
	}
}

func TestServerJournalCorruptionFailsClosedBeforeSigningNewOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-journal.sqlite")
	journal, err := openServerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	corrupted := newJournalSecurityRecord("corrupted-prior-operation", operationDeliver)
	seedJournalSecurityRecord(t, journal, corrupted)
	mutateJournalRequestColumn(t, journal.db, corrupted.RequestHash, "response_bytes_hash", testHash("corrupted-prior-response-fingerprint"))
	assertSQLiteIntegrityClean(t, journal.db)

	signed := false
	if _, _, err := journal.transact(context.Background(), newJournalSecurityRecord("new-operation", operationReconcile), func() ([]byte, error) {
		signed = true
		return []byte(`{"response":"must-not-sign"}`), nil
	}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("new transaction with corrupted history error = %v, want %v", err, ErrAuthentication)
	}
	if signed {
		t.Fatal("corrupted journal invoked signer for a new operation")
	}
}

func TestServerJournalRejectsIntegrityCleanContentCorruptionOnStartup(t *testing.T) {
	operations := []string{operationDeliver, operationReconcile, operationCancel, operationDeliver}
	corruptions := []struct {
		name   string
		column string
		value  any
	}{
		{name: "request_bytes", column: "request_bytes", value: []byte(`{"request":"mutated-on-disk"}`)},
		{name: "response_bytes", column: "response_bytes", value: []byte(`{"response":"mutated-on-disk"}`)},
		{name: "request_bytes_hash", column: "request_bytes_hash", value: testHash("mutated-on-disk-request-fingerprint")},
		{name: "response_bytes_hash", column: "response_bytes_hash", value: testHash("mutated-on-disk-response-fingerprint")},
	}
	for index, corruption := range corruptions {
		t.Run(corruption.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server-journal.sqlite")
			journal, err := openServerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			record := newJournalSecurityRecord("startup-"+corruption.name, operations[index])
			seedJournalSecurityRecord(t, journal, record)
			mutateJournalRequestColumn(t, journal.db, record.RequestHash, corruption.column, corruption.value)
			assertSQLiteIntegrityClean(t, journal.db)
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			assertServerJournalAuthenticationFailure(t, path)
		})
	}
}

func TestServerJournalRejectsTransitiveNonceAndReplayFingerprintCorruption(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		operation string
		mutate    func(*testing.T, *serverJournal, serverJournalRequest)
	}{
		{
			name: "deliver_nonce_fingerprint", operation: operationDeliver,
			mutate: func(t *testing.T, journal *serverJournal, record serverJournalRequest) {
				mutateJournalProtectedRows(t, journal.db,
					`DROP TRIGGER trusted_supervisor_nonces_no_update`,
					`UPDATE trusted_supervisor_nonces SET nonce_hash = ? WHERE request_hash = ? AND nonce_role = 'request'`,
					[]any{testHash("mutated-transitive-nonce"), record.RequestHash},
					`CREATE TRIGGER trusted_supervisor_nonces_no_update BEFORE UPDATE ON trusted_supervisor_nonces BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor nonce'); END`)
			},
		},
		{
			name: "reconcile_replay_request_fingerprint", operation: operationReconcile,
			mutate: func(t *testing.T, journal *serverJournal, record serverJournalRequest) {
				mutateJournalProtectedRows(t, journal.db,
					`DROP TRIGGER trusted_supervisor_replays_no_update`,
					`UPDATE trusted_supervisor_replays SET request_bytes_hash = ? WHERE request_hash = ?`,
					[]any{testHash("mutated-replay-request-fingerprint"), record.RequestHash},
					`CREATE TRIGGER trusted_supervisor_replays_no_update BEFORE UPDATE ON trusted_supervisor_replays BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor replay'); END`)
			},
		},
		{
			name: "cancel_replay_response_fingerprint", operation: operationCancel,
			mutate: func(t *testing.T, journal *serverJournal, record serverJournalRequest) {
				mutateJournalProtectedRows(t, journal.db,
					`DROP TRIGGER trusted_supervisor_replays_no_update`,
					`UPDATE trusted_supervisor_replays SET response_bytes_hash = ? WHERE request_hash = ?`,
					[]any{testHash("mutated-replay-response-fingerprint"), record.RequestHash},
					`CREATE TRIGGER trusted_supervisor_replays_no_update BEFORE UPDATE ON trusted_supervisor_replays BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor replay'); END`)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server-journal.sqlite")
			journal, err := openServerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			record := newJournalSecurityRecord(testCase.name, testCase.operation)
			response := seedJournalSecurityRecord(t, journal, record)
			replayed, exact, err := journal.transact(context.Background(), record, func() ([]byte, error) {
				return nil, errors.New("must not rebuild exact replay")
			})
			if err != nil || !exact || string(replayed) != string(response) {
				t.Fatalf("seed exact replay = %q exact=%t err=%v, want %q", replayed, exact, err, response)
			}
			testCase.mutate(t, journal, record)
			assertSQLiteIntegrityClean(t, journal.db)
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			assertServerJournalAuthenticationFailure(t, path)
		})
	}
}

func TestServerJournalPinsExactTableIndexAndTriggerDefinitions(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *sql.DB)
	}{
		{
			name: "altered_table_sql",
			mutate: func(t *testing.T, db *sql.DB) {
				execJournalSQL(t, db,
					`DROP TABLE trusted_supervisor_replays`,
					`CREATE TABLE trusted_supervisor_replays (
						replay_id INTEGER PRIMARY KEY AUTOINCREMENT,
						request_hash TEXT NOT NULL REFERENCES trusted_supervisor_requests(request_hash),
						request_bytes_hash TEXT NOT NULL,
						response_bytes_hash TEXT NOT NULL,
						observed_at TEXT
					) STRICT`,
					`CREATE TRIGGER trusted_supervisor_replays_no_update BEFORE UPDATE ON trusted_supervisor_replays BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor replay'); END`,
					`CREATE TRIGGER trusted_supervisor_replays_no_delete BEFORE DELETE ON trusted_supervisor_replays BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor replay'); END`)
			},
		},
		{
			name: "altered_index_inventory",
			mutate: func(t *testing.T, db *sql.DB) {
				execJournalSQL(t, db, `CREATE INDEX trusted_supervisor_requests_created_at ON trusted_supervisor_requests(created_at)`)
			},
		},
		{
			name: "same_name_no_op_trigger",
			mutate: func(t *testing.T, db *sql.DB) {
				execJournalSQL(t, db,
					`DROP TRIGGER trusted_supervisor_requests_no_update`,
					`CREATE TRIGGER trusted_supervisor_requests_no_update BEFORE UPDATE ON trusted_supervisor_requests BEGIN SELECT 1; END`)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server-journal.sqlite")
			journal, err := openServerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			db := openRawJournal(t, path)
			testCase.mutate(t, db)
			assertSQLiteIntegrityClean(t, db)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			assertServerJournalAuthenticationFailure(t, path)
		})
	}
}

func TestServerJournalRejectsFutureAndGappedMigrationHistory(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate string
	}{
		{name: "future", mutate: `UPDATE trusted_supervisor_schema SET version = 2, migration_id = 'ananke.local-trusted-supervisor.server-journal.v2'`},
		{name: "gapped", mutate: `INSERT INTO trusted_supervisor_schema (version, migration_id, applied_at) VALUES (3, 'ananke.local-trusted-supervisor.server-journal.v3', '2026-07-25T00:00:00Z')`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server-journal.sqlite")
			journal, err := openServerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			db := openRawJournal(t, path)
			execJournalSQL(t, db, `PRAGMA ignore_check_constraints = ON`, testCase.mutate)
			assertSQLiteIntegrityClean(t, db)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			assertServerJournalAuthenticationFailure(t, path)
		})
	}
}

func newJournalSecurityRecord(suffix, operation string) serverJournalRequest {
	additionalNonce := ""
	if operation == operationDeliver {
		additionalNonce = testHash("message-nonce-" + suffix)
	}
	return serverJournalRequest{
		RequestHash:         testHash("request-" + suffix),
		Operation:           operation,
		OperationKey:        operation + ":" + testHash("operation-"+suffix),
		RequestNonceHash:    testHash("request-nonce-" + suffix),
		ResponseNonceHash:   testHash("response-nonce-" + suffix),
		AdditionalNonceHash: additionalNonce,
		RequestBytes:        []byte(fmt.Sprintf(`{"request":%q}`, suffix)),
	}
}

func seedJournalSecurityRecord(t *testing.T, journal *serverJournal, record serverJournalRequest) []byte {
	t.Helper()
	response := []byte(fmt.Sprintf(`{"response":%q}`, record.OperationKey))
	actual, replay, err := journal.transact(context.Background(), record, func() ([]byte, error) {
		return append([]byte(nil), response...), nil
	})
	if err != nil || replay || string(actual) != string(response) {
		t.Fatalf("seed journal transaction = %q replay=%t err=%v, want %q", actual, replay, err, response)
	}
	return response
}

func mutateJournalRequestColumn(t *testing.T, db *sql.DB, requestHash, column string, value any) {
	t.Helper()
	switch column {
	case "request_bytes", "response_bytes", "request_bytes_hash", "response_bytes_hash":
	default:
		t.Fatalf("unsupported corruption column %q", column)
	}
	mutateJournalProtectedRows(t, db,
		`DROP TRIGGER trusted_supervisor_requests_no_update`,
		`UPDATE trusted_supervisor_requests SET `+column+` = ? WHERE request_hash = ?`,
		[]any{value, requestHash},
		`CREATE TRIGGER trusted_supervisor_requests_no_update BEFORE UPDATE ON trusted_supervisor_requests BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor request'); END`)
}

func mutateJournalProtectedRows(t *testing.T, db *sql.DB, drop, update string, args []any, restore string) {
	t.Helper()
	if _, err := db.Exec(drop); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(update, args...)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		t.Fatalf("corruption changed %d rows, err=%v; want 1", changed, err)
	}
	if _, err := db.Exec(restore); err != nil {
		t.Fatal(err)
	}
}

func assertSQLiteIntegrityClean(t *testing.T, db *sql.DB) {
	t.Helper()
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q, %v; want clean semantic corruption", integrity, err)
	}
}

func assertServerJournalAuthenticationFailure(t *testing.T, path string) {
	t.Helper()
	journal, err := openServerJournal(path)
	if journal != nil {
		_ = journal.Close()
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("open corrupted journal error = %v, want %v", err, ErrAuthentication)
	}
}

func openRawJournal(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func execJournalSQL(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("exec %q: %v", statement, err)
		}
	}
}
