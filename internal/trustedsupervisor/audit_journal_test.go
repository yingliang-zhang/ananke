package trustedsupervisor

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"modernc.org/sqlite"
)

func TestAuditJournalPersistsImmutableLifecycleAndEvidenceAcrossRestart(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "server-journal.sqlite")
	journal, err := openServerJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := validAuditExecutionHistoryForTest(t)
	intent, seedEvents := fixture.Intent, fixture.Events
	if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
		t.Fatal(err)
	}
	if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
		t.Fatalf("store audit intent: %v", err)
	}
	for _, event := range seedEvents {
		if err := journal.appendAuditEvent(context.Background(), event); err != nil {
			t.Fatalf("append %s event: %v", event.State, err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = openServerJournal(journalPath)
	if err != nil {
		t.Fatalf("reopen audit journal: %v", err)
	}
	if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
		t.Fatalf("rebind audit authority: %v", err)
	}
	defer journal.Close()
	loaded, events, err := journal.loadAuditExecution(context.Background(), intent.EnvelopeHash)
	if err != nil || loaded != intent || len(events) != 4 {
		t.Fatalf("reloaded audit execution = %+v / %+v, %v; want prepared, running, finalizing, completed", loaded, events, err)
	}
	for index, want := range seedEvents {
		if events[index] != want {
			t.Fatalf("reloaded audit event %d = %+v, want %+v", index, events[index], want)
		}
	}
	finalizing, completed := events[2], events[3]
	if finalizing.State != auditStateFinalizing || completed.State != auditStateCompleted ||
		completed.FinalizingEventHash != finalizing.EventHash || completed.EvidenceHash != finalizing.EvidenceHash ||
		completed.EvidenceJSON != finalizing.EvidenceJSON || completed.OutputSHA256 != finalizing.OutputSHA256 ||
		completed.OutputSize != finalizing.OutputSize {
		t.Fatalf("reloaded finalizing/completed continuity = %+v / %+v", finalizing, completed)
	}
	if _, err := journal.db.Exec(`UPDATE trusted_supervisor_audit_intents SET intent_bytes = intent_bytes`); err == nil {
		t.Fatal("immutable audit intent accepted UPDATE")
	}
	if _, err := journal.db.Exec(`DELETE FROM trusted_supervisor_audit_events`); err == nil {
		t.Fatal("immutable audit events accepted DELETE")
	}
}

func TestAuditJournalRejectsInvalidStateTransitionsAndExactConflicts(t *testing.T) {
	journal, err := openServerJournal(filepath.Join(t.TempDir(), "server-journal.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	fixture := validAuditExecutionHistoryForTest(t)
	intent, events := fixture.Intent, fixture.Events
	if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
		t.Fatal(err)
	}
	if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if err := journal.appendAuditEvent(context.Background(), events[2]); !errors.Is(err, ErrProtocol) {
		t.Fatalf("completed-before-running error = %v, want %v", err, ErrProtocol)
	}
	prepared := events[0]
	if err := journal.appendAuditEvent(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if err := journal.appendAuditEvent(context.Background(), prepared); err != nil {
		t.Fatalf("exact event replay was not idempotent: %v", err)
	}
	conflict := prepared
	conflict.CommandDescriptorHash = testHash("conflicting-command")
	conflict.Authentication = auditExecutionEventAuthentication{}
	conflict = resealAuditEventForHistoryTest(t, conflict)
	conflict, err = fixture.Authority.authenticateEvent(conflict)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.appendAuditEvent(context.Background(), conflict); !errors.Is(err, ErrReplay) {
		t.Fatalf("conflicting sequence error = %v, want %v", err, ErrReplay)
	}
}

func TestServerJournalMigratesAcceptedV1SchemaToImmutableAuditV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-journal-v1.sqlite")
	createAcceptedServerJournalSchema(t, path, 1)
	journal, err := openServerJournal(path)
	if err != nil {
		t.Fatalf("migrate accepted v1 journal: %v", err)
	}
	defer journal.Close()
	assertMigratedServerJournalV3(t, journal)
}

func TestServerJournalMigratesAcceptedV2SchemaDirectlyToCancellationV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-journal-v2.sqlite")
	createAcceptedServerJournalSchema(t, path, 2)
	journal, err := openServerJournal(path)
	if err != nil {
		t.Fatalf("migrate accepted v2 journal: %v", err)
	}
	defer journal.Close()
	assertMigratedServerJournalV3(t, journal)
}

func TestServerJournalRollsBackV3MigrationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-journal-v2-full.sqlite")
	createAcceptedServerJournalSchema(t, path, 2)
	commitRejected := false
	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
		if !strings.HasPrefix(dsn, "file:"+path+"?") {
			return nil
		}
		hooks, ok := conn.(sqlite.HookRegisterer)
		if !ok {
			return errors.New("sqlite connection does not support commit hooks")
		}
		hooks.RegisterCommitHook(func() int32 {
			commitRejected = true
			return 1
		})
		return nil
	})
	journal, err := openServerJournal(path)
	if journal != nil {
		_ = journal.Close()
	}
	if !commitRejected || err == nil || !strings.Contains(err.Error(), "commit server journal v3 migration") {
		t.Fatalf("v3 migration failure = %v, commit rejected = %t; want SQLite commit-hook rejection after v3 schema mutation", err, commitRejected)
	}
	database := openRawJournal(t, path)
	defer database.Close()
	var migrations, maximumVersion, previousTables, cancellationObjects int
	if err := database.QueryRow(`SELECT COUNT(*), MAX(version) FROM trusted_supervisor_schema`).Scan(&migrations, &maximumVersion); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'trusted_supervisor_schema_previous'`).Scan(&previousTables); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'trusted_supervisor_cancellation_%' OR tbl_name LIKE 'trusted_supervisor_cancellation_%'`).Scan(&cancellationObjects); err != nil {
		t.Fatal(err)
	}
	if migrations != 2 || maximumVersion != 2 || previousTables != 0 || cancellationObjects != 0 {
		t.Fatalf("rolled-back v2 journal = migrations %d maximum %d previous %d cancellation %d; want 2, 2, 0, 0", migrations, maximumVersion, previousTables, cancellationObjects)
	}
	if err := validateServerJournalSchemaVersion(context.Background(), database, 2); err != nil {
		t.Fatalf("rolled-back v2 schema: %v", err)
	}
	if version, err := validateServerJournalMigrationHistory(context.Background(), database); err != nil || version != 2 {
		t.Fatalf("rolled-back v2 migration history = version %d, %v; want exact accepted v2 history", version, err)
	}
}

func TestServerJournalRejectsPopulatedAcceptedV2AuditHistoryAtomically(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		populateIntents   bool
		populateEvents    bool
		correlatedReseal  bool
		wantPopulated     string
		wantIntentHistory bool
		wantEventHistory  bool
	}{
		{name: "intent and event", populateIntents: true, populateEvents: true, wantPopulated: "trusted_supervisor_audit_intents and trusted_supervisor_audit_events", wantIntentHistory: true, wantEventHistory: true},
		{name: "only intent", populateIntents: true, wantPopulated: "trusted_supervisor_audit_intents", wantIntentHistory: true},
		{name: "only event", populateEvents: true, wantPopulated: "trusted_supervisor_audit_events", wantEventHistory: true},
		{name: "correlated legacy rehash and reseal", populateIntents: true, populateEvents: true, correlatedReseal: true, wantPopulated: "trusted_supervisor_audit_intents and trusted_supervisor_audit_events", wantIntentHistory: true, wantEventHistory: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "populated-v2.sqlite")
			createAcceptedServerJournalSchema(t, path, 2)
			populateAcceptedV2AuditHistory(t, path, testCase.populateIntents, testCase.populateEvents, testCase.correlatedReseal)
			before := acceptedV2JournalFingerprint(t, path)
			for attempt := 1; attempt <= 2; attempt++ {
				journal, err := openServerJournal(path)
				if journal != nil {
					_ = journal.Close()
					t.Fatalf("reopen attempt %d returned a migrated journal", attempt)
				}
				var migrationErr *LegacyAuditHistoryMigrationError
				if !errors.As(err, &migrationErr) || !errors.Is(err, ErrAuthentication) || !errors.Is(err, ErrLegacyAuditHistoryMigration) {
					t.Fatalf("reopen attempt %d error = %#v, %v; want typed closed legacy-history rejection", attempt, migrationErr, err)
				}
				if migrationErr.IntentHistoryPresent != testCase.wantIntentHistory || migrationErr.EventHistoryPresent != testCase.wantEventHistory {
					t.Fatalf("reopen attempt %d populated history = %#v", attempt, migrationErr)
				}
				wantMessage := "local trusted supervisor authentication failed: populated legacy V2 audit history in " + testCase.wantPopulated + "; archive/export the legacy database and start a fresh journal; no in-place signing migration is supported"
				if err.Error() != wantMessage {
					t.Fatalf("reopen attempt %d error = %q, want %q", attempt, err, wantMessage)
				}
				if strings.Contains(err.Error(), testHash("legacy-v2-intent")) || strings.Contains(err.Error(), testHash("legacy-v2-envelope")) {
					t.Fatalf("reopen attempt %d leaked legacy row contents: %v", attempt, err)
				}
				after := acceptedV2JournalFingerprint(t, path)
				if after != before {
					t.Fatalf("reopen attempt %d changed V2 user_version, schema, or row bytes\nbefore:\n%s\nafter:\n%s", attempt, before, after)
				}
				database := openRawJournal(t, path)
				if err := validateServerJournalSchemaVersion(context.Background(), database, 2); err != nil {
					_ = database.Close()
					t.Fatalf("reopen attempt %d no longer has accepted V2 schema: %v", attempt, err)
				}
				if version, err := validateServerJournalMigrationHistory(context.Background(), database); err != nil || version != 2 {
					_ = database.Close()
					t.Fatalf("reopen attempt %d migration history = %d, %v; want V2", attempt, version, err)
				}
				if err := database.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func populateAcceptedV2AuditHistory(t *testing.T, path string, populateIntents, populateEvents, correlatedReseal bool) {
	t.Helper()
	database := openRawJournal(t, path)
	defer database.Close()
	intent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_legacy_v2_001",
		EnvelopeHash: testHash("legacy-v2-envelope"), LaunchSpecHash: testHash("legacy-v2-launch"),
		HandoffID: "remote_handoff_legacy_v2_001", ReceiptHash: testHash("legacy-v2-receipt"),
		TaskID: "audit_task_legacy_v2_001", AttemptCap: 3, PolicyHash: testHash("legacy-v2-policy"),
		RouteMappingHash: testHash("legacy-v2-route"), RepositoryIdentityHash: testHash("legacy-v2-repository"),
		GitCommit: "0123456789abcdef0123456789abcdef01234567", GitTree: "89abcdef0123456789abcdef0123456789abcdef",
		SourceArchiveSHA256: testHash("legacy-v2-source"), WrapperSHA256: testHash("legacy-v2-wrapper"),
		RunID: "audit_run_legacy_v2_001", CreatedAt: "2026-07-25T00:00:01Z",
	})
	intentBytes, err := marshalCanonical(intent)
	if err != nil {
		t.Fatal(err)
	}
	if populateIntents {
		if _, err := database.Exec(`INSERT INTO trusted_supervisor_audit_intents
			(intent_hash, envelope_hash, intent_bytes, intent_bytes_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
			intent.IntentHash, intent.EnvelopeHash, intentBytes, hashJournalBytes(intentBytes), intent.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	if !populateEvents {
		return
	}
	legacyEvent := map[string]any{
		"attempt": 1, "command_descriptor_hash": testHash("legacy-v2-command"), "event_hash": "",
		"event_id": "audit_event_legacy_v2_001", "intent_hash": intent.IntentHash, "occurred_at": "2026-07-25T00:00:02Z",
		"output_path": "/private/legacy-v2/output.json", "prompt_path": "/private/legacy-v2/prompt.md",
		"prompt_sha256": testHash("legacy-v2-prompt"), "resume_session_uuid": "", "schema_version": "ananke.local-trusted-supervisor-audit-event.v2",
		"sequence": 1, "session_path": "/private/legacy-v2/session", "session_run_id": intent.RunID,
		"state": auditStatePrepared, "synthesize_only": false, "temporary_path": "/private/legacy-v2/temporary", "work_path": "/private/legacy-v2/work",
	}
	if correlatedReseal {
		legacyEvent["command_descriptor_hash"] = testHash("legacy-v2-correlated-rewrite")
	}
	eventHash, err := canonicalHash(legacyEvent)
	if err != nil {
		t.Fatal(err)
	}
	legacyEvent["event_hash"] = eventHash
	eventBytes, err := marshalCanonical(legacyEvent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(eventBytes), "authentication") || hashJournalBytes(eventBytes) == "" {
		t.Fatalf("legacy V2 event is not unsigned and byte-bound: %s", eventBytes)
	}
	if _, err := database.Exec(`INSERT INTO trusted_supervisor_audit_events
		(intent_hash, sequence, state, event_bytes, event_bytes_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		intent.IntentHash, 1, auditStatePrepared, eventBytes, hashJournalBytes(eventBytes), "2026-07-25T00:00:02Z"); err != nil {
		t.Fatal(err)
	}
}

func acceptedV2JournalFingerprint(t *testing.T, path string) string {
	t.Helper()
	database := openRawJournal(t, path)
	defer database.Close()
	var userVersion int
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		t.Fatal(err)
	}
	var fingerprint strings.Builder
	fingerprint.WriteString("user_version=")
	fingerprint.WriteString(string(rune('0' + userVersion)))
	fingerprint.WriteByte('\n')
	for _, query := range []string{
		`SELECT quote(type) || '|' || quote(name) || '|' || quote(tbl_name) || '|' || quote(COALESCE(sql, '')) FROM sqlite_master ORDER BY type, name`,
		`SELECT quote(version) || '|' || quote(migration_id) || '|' || quote(applied_at) FROM trusted_supervisor_schema ORDER BY version`,
		`SELECT quote(request_hash) || '|' || quote(operation) || '|' || quote(operation_key) || '|' || quote(request_nonce_hash) || '|' || quote(response_nonce_hash) || '|' || quote(additional_nonce_hash) || '|' || quote(request_bytes) || '|' || quote(request_bytes_hash) || '|' || quote(response_bytes) || '|' || quote(response_bytes_hash) || '|' || quote(created_at) FROM trusted_supervisor_requests ORDER BY request_hash`,
		`SELECT quote(nonce_hash) || '|' || quote(request_hash) || '|' || quote(nonce_role) FROM trusted_supervisor_nonces ORDER BY nonce_hash`,
		`SELECT quote(replay_id) || '|' || quote(request_hash) || '|' || quote(request_bytes_hash) || '|' || quote(response_bytes_hash) || '|' || quote(observed_at) FROM trusted_supervisor_replays ORDER BY replay_id`,
		`SELECT quote(intent_hash) || '|' || quote(envelope_hash) || '|' || quote(intent_bytes) || '|' || quote(intent_bytes_hash) || '|' || quote(created_at) FROM trusted_supervisor_audit_intents ORDER BY intent_hash`,
		`SELECT quote(event_id) || '|' || quote(intent_hash) || '|' || quote(sequence) || '|' || quote(state) || '|' || quote(event_bytes) || '|' || quote(event_bytes_hash) || '|' || quote(created_at) FROM trusted_supervisor_audit_events ORDER BY event_id`,
		`SELECT quote(name) || '|' || quote(seq) FROM sqlite_sequence ORDER BY name`,
	} {
		rows, err := database.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		fingerprint.WriteString(query)
		fingerprint.WriteByte('\n')
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			fingerprint.WriteString(row)
			fingerprint.WriteByte('\n')
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return fingerprint.String()
}

func createAcceptedServerJournalSchema(t *testing.T, path string, version int) {
	t.Helper()
	objects := serverJournalSchemaObjectsForVersion(version)
	if version != 1 && version != 2 || len(objects) == 0 {
		t.Fatalf("unsupported accepted journal version %d", version)
	}
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if !object.Create {
			continue
		}
		if _, err := database.Exec(object.SQL); err != nil {
			_ = database.Close()
			t.Fatalf("create accepted v%d object %s: %v", version, object.Name, err)
		}
	}
	for migrationVersion := 1; migrationVersion <= version; migrationVersion++ {
		migrationID, valid := serverJournalMigrationIDForVersion(migrationVersion)
		if !valid {
			_ = database.Close()
			t.Fatalf("migration id for accepted v%d journal", migrationVersion)
		}
		if _, err := database.Exec(`INSERT INTO trusted_supervisor_schema (version, migration_id, applied_at) VALUES (?, ?, '2026-07-25T00:00:00Z')`, migrationVersion, migrationID); err != nil {
			_ = database.Close()
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertMigratedServerJournalV3(t *testing.T, journal *serverJournal) {
	t.Helper()
	var migrations, auditTables, cancellationTables int
	if err := journal.db.QueryRow(`SELECT COUNT(*) FROM trusted_supervisor_schema`).Scan(&migrations); err != nil || migrations != serverJournalSchemaVersion {
		t.Fatalf("migration count = %d, %v; want %d", migrations, err, serverJournalSchemaVersion)
	}
	if err := journal.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('trusted_supervisor_audit_intents', 'trusted_supervisor_audit_events')`).Scan(&auditTables); err != nil || auditTables != 2 {
		t.Fatalf("audit table count = %d, %v; want 2", auditTables, err)
	}
	if err := journal.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('trusted_supervisor_cancellation_intents', 'trusted_supervisor_cancellation_outcomes')`).Scan(&cancellationTables); err != nil || cancellationTables != 2 {
		t.Fatalf("cancellation table count = %d, %v; want 2", cancellationTables, err)
	}
	if err := validateServerJournalSchema(context.Background(), journal.db); err != nil {
		t.Fatalf("migrated canonical v3 schema: %v", err)
	}
}

func mustSealAuditIntentForTest(t *testing.T, intent auditExecutionIntent) auditExecutionIntent {
	t.Helper()
	sealed, err := sealAuditExecutionIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func mustSealAuditEventForTest(t *testing.T, event auditExecutionEvent) auditExecutionEvent {
	t.Helper()
	sealed, err := sealAuditExecutionEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
