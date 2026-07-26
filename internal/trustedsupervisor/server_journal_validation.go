package trustedsupervisor

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type serverJournalSchemaObject struct {
	ObjectType string
	Name       string
	TableName  string
	SQL        string
	Create     bool
}

type serverJournalSchemaObjectKey struct {
	ObjectType string
	Name       string
}

type serverJournalQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const (
	serverJournalSchemaSQLV1 = `CREATE TABLE trusted_supervisor_schema (
			version INTEGER PRIMARY KEY CHECK(version = 1),
			migration_id TEXT NOT NULL UNIQUE,
			applied_at TEXT NOT NULL
		) STRICT`
	serverJournalSchemaSQLV2 = `CREATE TABLE trusted_supervisor_schema (
			version INTEGER PRIMARY KEY CHECK(version IN (1, 2)),
			migration_id TEXT NOT NULL UNIQUE,
			applied_at TEXT NOT NULL
		) STRICT`
	serverJournalSchemaSQLV3 = `CREATE TABLE trusted_supervisor_schema (
			version INTEGER PRIMARY KEY CHECK(version IN (1, 2, 3)),
			migration_id TEXT NOT NULL UNIQUE,
			applied_at TEXT NOT NULL
		) STRICT`
	serverJournalSchemaSQLV4 = `CREATE TABLE trusted_supervisor_schema (
			version INTEGER PRIMARY KEY CHECK(version IN (1, 2, 3, 4)),
			migration_id TEXT NOT NULL UNIQUE,
			applied_at TEXT NOT NULL
		) STRICT`
	serverJournalAuditEventsSQLV3 = `CREATE TABLE trusted_supervisor_audit_events (
			event_id INTEGER PRIMARY KEY AUTOINCREMENT,
			intent_hash TEXT NOT NULL REFERENCES trusted_supervisor_audit_intents(intent_hash),
			sequence INTEGER NOT NULL CHECK(sequence >= 1),
			state TEXT NOT NULL CHECK(state IN ('prepared', 'running', 'completed', 'failed', 'timed_out', 'cancelled', 'waiting_for_human')),
			event_bytes BLOB NOT NULL,
			event_bytes_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(intent_hash, sequence)
		) STRICT`
	serverJournalAuditEventsSQLV4 = `CREATE TABLE trusted_supervisor_audit_events (
			event_id INTEGER PRIMARY KEY AUTOINCREMENT,
			intent_hash TEXT NOT NULL REFERENCES trusted_supervisor_audit_intents(intent_hash),
			sequence INTEGER NOT NULL CHECK(sequence >= 1),
			state TEXT NOT NULL CHECK(state IN ('prepared', 'running', 'finalizing', 'completed', 'failed', 'timed_out', 'cancelled', 'waiting_for_human')),
			event_bytes BLOB NOT NULL,
			event_bytes_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(intent_hash, sequence)
		) STRICT`
)

var serverJournalSchemaObjects = []serverJournalSchemaObject{
	{
		ObjectType: "table", Name: "trusted_supervisor_schema", TableName: "trusted_supervisor_schema", Create: true,
		SQL: serverJournalSchemaSQLV4,
	},
	{
		ObjectType: "table", Name: "trusted_supervisor_requests", TableName: "trusted_supervisor_requests", Create: true,
		SQL: `CREATE TABLE trusted_supervisor_requests (
			request_hash TEXT PRIMARY KEY,
			operation TEXT NOT NULL CHECK(operation IN ('deliver', 'reconcile', 'cancel')),
			operation_key TEXT NOT NULL UNIQUE,
			request_nonce_hash TEXT NOT NULL UNIQUE,
			response_nonce_hash TEXT NOT NULL UNIQUE,
			additional_nonce_hash TEXT NOT NULL,
			request_bytes BLOB NOT NULL,
			request_bytes_hash TEXT NOT NULL,
			response_bytes BLOB NOT NULL,
			response_bytes_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		) STRICT`,
	},
	{
		ObjectType: "table", Name: "trusted_supervisor_nonces", TableName: "trusted_supervisor_nonces", Create: true,
		SQL: `CREATE TABLE trusted_supervisor_nonces (
			nonce_hash TEXT PRIMARY KEY,
			request_hash TEXT NOT NULL REFERENCES trusted_supervisor_requests(request_hash),
			nonce_role TEXT NOT NULL CHECK(nonce_role IN ('request', 'response', 'message'))
		) STRICT`,
	},
	{
		ObjectType: "table", Name: "trusted_supervisor_replays", TableName: "trusted_supervisor_replays", Create: true,
		SQL: `CREATE TABLE trusted_supervisor_replays (
			replay_id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_hash TEXT NOT NULL REFERENCES trusted_supervisor_requests(request_hash),
			request_bytes_hash TEXT NOT NULL,
			response_bytes_hash TEXT NOT NULL,
			observed_at TEXT NOT NULL
		) STRICT`,
	},
	{
		ObjectType: "table", Name: "trusted_supervisor_audit_intents", TableName: "trusted_supervisor_audit_intents", Create: true,
		SQL: `CREATE TABLE trusted_supervisor_audit_intents (
			intent_hash TEXT PRIMARY KEY,
			envelope_hash TEXT NOT NULL UNIQUE,
			intent_bytes BLOB NOT NULL,
			intent_bytes_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		) STRICT`,
	},
	{
		ObjectType: "table", Name: "trusted_supervisor_audit_events", TableName: "trusted_supervisor_audit_events", Create: true,
		SQL: serverJournalAuditEventsSQLV4,
	},
	{
		ObjectType: "table", Name: "trusted_supervisor_cancellation_intents", TableName: "trusted_supervisor_cancellation_intents", Create: true,
		SQL: `CREATE TABLE trusted_supervisor_cancellation_intents (
			intent_hash TEXT PRIMARY KEY,
			request_hash TEXT NOT NULL UNIQUE,
			operation_key TEXT NOT NULL UNIQUE,
			request_nonce_hash TEXT NOT NULL UNIQUE,
			response_nonce_hash TEXT NOT NULL UNIQUE,
			exclusivity_nonce_hash TEXT NOT NULL UNIQUE,
			envelope_hash TEXT NOT NULL UNIQUE,
			receipt_hash TEXT NOT NULL UNIQUE,
			cancellation_hash TEXT NOT NULL UNIQUE,
			intent_bytes BLOB NOT NULL,
			intent_bytes_hash TEXT NOT NULL,
			requested_at TEXT NOT NULL
		) STRICT`,
	},
	{
		ObjectType: "table", Name: "trusted_supervisor_cancellation_outcomes", TableName: "trusted_supervisor_cancellation_outcomes", Create: true,
		SQL: `CREATE TABLE trusted_supervisor_cancellation_outcomes (
			intent_hash TEXT PRIMARY KEY REFERENCES trusted_supervisor_cancellation_intents(intent_hash),
			outcome TEXT NOT NULL CHECK(outcome IN ('completed', 'failed')),
			audit_event_hash TEXT NOT NULL UNIQUE,
			response_bytes BLOB NOT NULL,
			response_bytes_hash TEXT NOT NULL,
			completed_at TEXT NOT NULL
		) STRICT`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_cancellation_intents_no_update", TableName: "trusted_supervisor_cancellation_intents", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_cancellation_intents_no_update BEFORE UPDATE ON trusted_supervisor_cancellation_intents BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor cancellation intent'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_cancellation_intents_no_delete", TableName: "trusted_supervisor_cancellation_intents", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_cancellation_intents_no_delete BEFORE DELETE ON trusted_supervisor_cancellation_intents BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor cancellation intent'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_cancellation_outcomes_no_update", TableName: "trusted_supervisor_cancellation_outcomes", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_cancellation_outcomes_no_update BEFORE UPDATE ON trusted_supervisor_cancellation_outcomes BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor cancellation outcome'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_cancellation_outcomes_no_delete", TableName: "trusted_supervisor_cancellation_outcomes", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_cancellation_outcomes_no_delete BEFORE DELETE ON trusted_supervisor_cancellation_outcomes BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor cancellation outcome'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_audit_intents_no_update", TableName: "trusted_supervisor_audit_intents", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_audit_intents_no_update BEFORE UPDATE ON trusted_supervisor_audit_intents BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor audit intent'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_audit_intents_no_delete", TableName: "trusted_supervisor_audit_intents", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_audit_intents_no_delete BEFORE DELETE ON trusted_supervisor_audit_intents BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor audit intent'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_audit_events_no_update", TableName: "trusted_supervisor_audit_events", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_audit_events_no_update BEFORE UPDATE ON trusted_supervisor_audit_events BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor audit event'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_audit_events_no_delete", TableName: "trusted_supervisor_audit_events", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_audit_events_no_delete BEFORE DELETE ON trusted_supervisor_audit_events BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor audit event'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_requests_no_update", TableName: "trusted_supervisor_requests", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_requests_no_update BEFORE UPDATE ON trusted_supervisor_requests BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor request'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_requests_no_delete", TableName: "trusted_supervisor_requests", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_requests_no_delete BEFORE DELETE ON trusted_supervisor_requests BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor request'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_replays_no_update", TableName: "trusted_supervisor_replays", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_replays_no_update BEFORE UPDATE ON trusted_supervisor_replays BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor replay'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_replays_no_delete", TableName: "trusted_supervisor_replays", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_replays_no_delete BEFORE DELETE ON trusted_supervisor_replays BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor replay'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_nonces_no_update", TableName: "trusted_supervisor_nonces", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_nonces_no_update BEFORE UPDATE ON trusted_supervisor_nonces BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor nonce'); END`,
	},
	{
		ObjectType: "trigger", Name: "trusted_supervisor_nonces_no_delete", TableName: "trusted_supervisor_nonces", Create: true,
		SQL: `CREATE TRIGGER trusted_supervisor_nonces_no_delete BEFORE DELETE ON trusted_supervisor_nonces BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor nonce'); END`,
	},
	{ObjectType: "table", Name: "sqlite_sequence", TableName: "sqlite_sequence", SQL: `CREATE TABLE sqlite_sequence(name,seq)`},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_schema_1", TableName: "trusted_supervisor_schema"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_requests_1", TableName: "trusted_supervisor_requests"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_requests_2", TableName: "trusted_supervisor_requests"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_requests_3", TableName: "trusted_supervisor_requests"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_requests_4", TableName: "trusted_supervisor_requests"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_nonces_1", TableName: "trusted_supervisor_nonces"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_audit_intents_1", TableName: "trusted_supervisor_audit_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_audit_intents_2", TableName: "trusted_supervisor_audit_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_audit_events_1", TableName: "trusted_supervisor_audit_events"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_1", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_2", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_3", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_4", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_5", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_6", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_7", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_8", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_intents_9", TableName: "trusted_supervisor_cancellation_intents"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_outcomes_1", TableName: "trusted_supervisor_cancellation_outcomes"},
	{ObjectType: "index", Name: "sqlite_autoindex_trusted_supervisor_cancellation_outcomes_2", TableName: "trusted_supervisor_cancellation_outcomes"},
}

func serverJournalSchemaObjectsForVersion(version int) []serverJournalSchemaObject {
	if version < 1 || version > serverJournalSchemaVersion {
		return nil
	}
	objects := make([]serverJournalSchemaObject, 0, len(serverJournalSchemaObjects))
	for _, object := range serverJournalSchemaObjects {
		if version < 2 && strings.Contains(object.Name, "audit_") {
			continue
		}
		if version < 3 && strings.Contains(object.Name, "cancellation_") {
			continue
		}
		if object.Name == "trusted_supervisor_schema" {
			switch version {
			case 1:
				object.SQL = serverJournalSchemaSQLV1
			case 2:
				object.SQL = serverJournalSchemaSQLV2
			}
		}
		if object.Name == "trusted_supervisor_audit_events" && version < 4 {
			object.SQL = serverJournalAuditEventsSQLV3
		}
		objects = append(objects, object)
	}
	return objects
}

func validateServerJournalSchema(ctx context.Context, queryer serverJournalQueryer) error {
	return validateServerJournalSchemaVersion(ctx, queryer, serverJournalSchemaVersion)
}

func validateServerJournalSchemaVersion(ctx context.Context, queryer serverJournalQueryer, version int) error {
	objects := serverJournalSchemaObjectsForVersion(version)
	if len(objects) == 0 {
		return authenticationError("server journal schema version")
	}
	expected := make(map[serverJournalSchemaObjectKey]serverJournalSchemaObject, len(objects))
	for _, object := range objects {
		expected[serverJournalSchemaObjectKey{ObjectType: object.ObjectType, Name: object.Name}] = object
	}
	rows, err := queryer.QueryContext(ctx, `SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		return authenticationError("inspect canonical server journal schema")
	}
	for rows.Next() {
		var actual serverJournalSchemaObject
		if err := rows.Scan(&actual.ObjectType, &actual.Name, &actual.TableName, &actual.SQL); err != nil {
			_ = rows.Close()
			return authenticationError("read canonical server journal schema")
		}
		key := serverJournalSchemaObjectKey{ObjectType: actual.ObjectType, Name: actual.Name}
		want, exists := expected[key]
		if !exists || actual.TableName != want.TableName || actual.SQL != want.SQL {
			_ = rows.Close()
			return authenticationError("server journal schema definition")
		}
		delete(expected, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return authenticationError("read canonical server journal schema")
	}
	if err := rows.Close(); err != nil || len(expected) != 0 {
		return authenticationError("server journal schema inventory")
	}
	return nil
}

type storedServerJournalRequest struct {
	Request           serverJournalRequest
	RequestBytesHash  string
	ResponseBytes     []byte
	ResponseBytesHash string
}

type expectedServerJournalNonce struct {
	RequestHash string
	Role        string
}

func validateServerJournalStorageContent(ctx context.Context, queryer serverJournalQueryer) error {
	return validateServerJournalContentInternal(ctx, queryer, nil, false)
}

func validateServerJournalContent(ctx context.Context, queryer serverJournalQueryer, authority *auditJournalAuthority) error {
	return validateServerJournalContentInternal(ctx, queryer, authority, true)
}

func validateServerJournalContentInternal(ctx context.Context, queryer serverJournalQueryer, authority *auditJournalAuthority, validateAudit bool) error {
	requests := make(map[string]storedServerJournalRequest)
	rows, err := queryer.QueryContext(ctx, `SELECT request_hash, operation, operation_key, request_nonce_hash, response_nonce_hash,
		additional_nonce_hash, request_bytes, request_bytes_hash, response_bytes, response_bytes_hash, created_at
		FROM trusted_supervisor_requests ORDER BY request_hash`)
	if err != nil {
		return authenticationError("inspect durable server requests")
	}
	for rows.Next() {
		var stored storedServerJournalRequest
		var createdAt string
		if err := rows.Scan(
			&stored.Request.RequestHash, &stored.Request.Operation, &stored.Request.OperationKey,
			&stored.Request.RequestNonceHash, &stored.Request.ResponseNonceHash, &stored.Request.AdditionalNonceHash,
			&stored.Request.RequestBytes, &stored.RequestBytesHash, &stored.ResponseBytes, &stored.ResponseBytesHash, &createdAt,
		); err != nil {
			_ = rows.Close()
			return authenticationError("read durable server request")
		}
		if !validServerJournalRequest(stored.Request) || len(stored.ResponseBytes) == 0 || len(stored.ResponseBytes) > int(maxFrameBytes) ||
			stored.RequestBytesHash != hashJournalBytes(stored.Request.RequestBytes) || stored.ResponseBytesHash != hashJournalBytes(stored.ResponseBytes) ||
			!validServerJournalTimestamp(createdAt) {
			_ = rows.Close()
			return authenticationError("durable server request fingerprints")
		}
		requests[stored.Request.RequestHash] = stored
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return authenticationError("read durable server requests")
	}
	if err := rows.Close(); err != nil {
		return authenticationError("close durable server requests")
	}

	expectedNonces := make(map[string]expectedServerJournalNonce, len(requests)*3)
	for _, stored := range requests {
		expectedNonces[stored.Request.RequestNonceHash] = expectedServerJournalNonce{RequestHash: stored.Request.RequestHash, Role: "request"}
		expectedNonces[stored.Request.ResponseNonceHash] = expectedServerJournalNonce{RequestHash: stored.Request.RequestHash, Role: "response"}
		if stored.Request.AdditionalNonceHash != "" {
			expectedNonces[stored.Request.AdditionalNonceHash] = expectedServerJournalNonce{RequestHash: stored.Request.RequestHash, Role: "message"}
		}
	}
	rows, err = queryer.QueryContext(ctx, `SELECT nonce_hash, request_hash, nonce_role FROM trusted_supervisor_nonces ORDER BY nonce_hash`)
	if err != nil {
		return authenticationError("inspect durable server nonces")
	}
	for rows.Next() {
		var nonceHash, requestHash, role string
		if err := rows.Scan(&nonceHash, &requestHash, &role); err != nil {
			_ = rows.Close()
			return authenticationError("read durable server nonce")
		}
		expected, exists := expectedNonces[nonceHash]
		if !exists || requestHash != expected.RequestHash || role != expected.Role {
			_ = rows.Close()
			return authenticationError("durable server nonce fingerprints")
		}
		delete(expectedNonces, nonceHash)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return authenticationError("read durable server nonces")
	}
	if err := rows.Close(); err != nil || len(expectedNonces) != 0 {
		return authenticationError("durable server nonce inventory")
	}

	rows, err = queryer.QueryContext(ctx, `SELECT replay_id, request_hash, request_bytes_hash, response_bytes_hash, observed_at
		FROM trusted_supervisor_replays ORDER BY replay_id`)
	if err != nil {
		return authenticationError("inspect durable server replays")
	}
	for rows.Next() {
		var replayID int64
		var requestHash, requestBytesHash, responseBytesHash, observedAt string
		if err := rows.Scan(&replayID, &requestHash, &requestBytesHash, &responseBytesHash, &observedAt); err != nil {
			_ = rows.Close()
			return authenticationError("read durable server replay")
		}
		request, exists := requests[requestHash]
		if replayID < 1 || !exists || requestBytesHash != request.RequestBytesHash || responseBytesHash != request.ResponseBytesHash ||
			!validServerJournalTimestamp(observedAt) {
			_ = rows.Close()
			return authenticationError("durable server replay fingerprints")
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return authenticationError("read durable server replays")
	}
	if err := rows.Close(); err != nil {
		return authenticationError("close durable server replays")
	}
	if !validateAudit {
		return nil
	}
	if err := validateAuditJournalContent(ctx, queryer, authority); err != nil {
		return err
	}
	return validateAuditCancellationContent(ctx, queryer, authority)
}

func validServerJournalTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC
}
