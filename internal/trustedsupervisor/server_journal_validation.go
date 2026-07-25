package trustedsupervisor

import (
	"context"
	"database/sql"
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
}

var serverJournalSchemaObjects = []serverJournalSchemaObject{
	{
		ObjectType: "table", Name: "trusted_supervisor_schema", TableName: "trusted_supervisor_schema", Create: true,
		SQL: `CREATE TABLE trusted_supervisor_schema (
			version INTEGER PRIMARY KEY CHECK(version = 1),
			migration_id TEXT NOT NULL UNIQUE,
			applied_at TEXT NOT NULL
		) STRICT`,
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
}

func validateServerJournalSchema(ctx context.Context, queryer serverJournalQueryer) error {
	expected := make(map[serverJournalSchemaObjectKey]serverJournalSchemaObject, len(serverJournalSchemaObjects))
	for _, object := range serverJournalSchemaObjects {
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

func validateServerJournalContent(ctx context.Context, queryer serverJournalQueryer) error {
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
	return nil
}

func validServerJournalTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC
}
