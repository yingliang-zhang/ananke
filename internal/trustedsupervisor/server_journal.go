package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

const serverJournalMigrationID = "ananke.local-trusted-supervisor.server-journal.v1"

type serverJournalRequest struct {
	RequestHash         string
	Operation           string
	OperationKey        string
	RequestNonceHash    string
	ResponseNonceHash   string
	AdditionalNonceHash string
	RequestBytes        []byte
}

type serverJournal struct {
	db       *sql.DB
	path     string
	anchor   *os.File
	device   uint64
	inode    uint64
	ownerUID uint32
	mu       sync.Mutex
	closed   bool
}

func openServerJournal(path string) (*serverJournal, error) {
	if path == "" || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 || strings.ContainsAny(path, "?&%#") {
		return nil, authenticationError("absolute non-URI server journal path required")
	}
	ownerUID := uint32(os.Getuid())
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, authenticationError("open server journal")
	}
	anchor := os.NewFile(uintptr(fd), filepath.Base(path))
	if anchor == nil {
		_ = unix.Close(fd)
		return nil, authenticationError("open server journal descriptor")
	}
	closeAnchor := true
	defer func() {
		if closeAnchor {
			_ = anchor.Close()
		}
	}()
	var status unix.Stat_t
	if err := unix.Fstat(fd, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != ownerUID || status.Mode&0o777 != 0o600 {
		return nil, authenticationError("server journal type owner or mode")
	}
	journal := &serverJournal{path: path, anchor: anchor, device: uint64(status.Dev), inode: status.Ino, ownerUID: ownerUID}
	if err := journal.validateIdentity(); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=fullfsync(ON)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open server journal sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	journal.db = db
	if err := journal.migrateAndValidate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := journal.validateIdentity(); err != nil {
		_ = db.Close()
		return nil, err
	}
	closeAnchor = false
	return journal, nil
}

func (journal *serverJournal) Close() error {
	if journal == nil {
		return nil
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return nil
	}
	journal.closed = true
	var result error
	if journal.db != nil {
		result = journal.db.Close()
	}
	if journal.anchor != nil {
		if err := journal.anchor.Close(); result == nil {
			result = err
		}
	}
	return result
}

func (journal *serverJournal) transact(ctx context.Context, request serverJournalRequest, buildResponse func() ([]byte, error)) ([]byte, bool, error) {
	if journal == nil || ctx == nil || buildResponse == nil || !validServerJournalRequest(request) {
		return nil, false, ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return nil, false, ErrProtocol
	}
	if err := journal.validateIdentity(); err != nil {
		return nil, false, err
	}
	tx, err := journal.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin server journal transaction: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	if err := validateServerJournalContent(ctx, tx); err != nil {
		return nil, false, err
	}

	var existing serverJournalRequest
	var responseBytes, requestBytes []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash, operation, operation_key, request_nonce_hash, response_nonce_hash, additional_nonce_hash, request_bytes, response_bytes
		FROM trusted_supervisor_requests WHERE request_hash = ?`, request.RequestHash).Scan(
		&existing.RequestHash, &existing.Operation, &existing.OperationKey, &existing.RequestNonceHash,
		&existing.ResponseNonceHash, &existing.AdditionalNonceHash, &requestBytes, &responseBytes,
	)
	if err == nil {
		if existing.Operation != request.Operation || existing.OperationKey != request.OperationKey ||
			existing.RequestNonceHash != request.RequestNonceHash || existing.ResponseNonceHash != request.ResponseNonceHash ||
			existing.AdditionalNonceHash != request.AdditionalNonceHash || !bytes.Equal(requestBytes, request.RequestBytes) {
			return nil, false, ErrReplay
		}
		requestDigest := hashJournalBytes(request.RequestBytes)
		responseDigest := hashJournalBytes(responseBytes)
		if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_replays
			(request_hash, request_bytes_hash, response_bytes_hash, observed_at) VALUES (?, ?, ?, ?)`,
			request.RequestHash, requestDigest, responseDigest, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return nil, false, fmt.Errorf("record exact server replay: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit exact server replay: %w", err)
		}
		rollback = false
		if err := journal.validateIdentity(); err != nil {
			return nil, false, err
		}
		return append([]byte(nil), responseBytes...), true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("read server replay: %w", err)
	}

	var conflict int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM trusted_supervisor_requests WHERE operation_key = ?
	)`, request.OperationKey).Scan(&conflict); err != nil {
		return nil, false, fmt.Errorf("check server operation conflict: %w", err)
	}
	if conflict == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM trusted_supervisor_nonces WHERE nonce_hash IN (?, ?, ?)
		)`, request.RequestNonceHash, request.ResponseNonceHash, request.AdditionalNonceHash).Scan(&conflict); err != nil {
			return nil, false, fmt.Errorf("check server nonce conflict: %w", err)
		}
	}
	if conflict != 0 {
		return nil, false, ErrReplay
	}
	responseBytes, err = buildResponse()
	if err != nil {
		zeroBytes(responseBytes)
		return nil, false, err
	}
	if len(responseBytes) == 0 || len(responseBytes) > int(maxFrameBytes) {
		zeroBytes(responseBytes)
		return nil, false, ErrLimit
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_requests
		(request_hash, operation, operation_key, request_nonce_hash, response_nonce_hash, additional_nonce_hash,
		 request_bytes, request_bytes_hash, response_bytes, response_bytes_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		request.RequestHash, request.Operation, request.OperationKey, request.RequestNonceHash, request.ResponseNonceHash, request.AdditionalNonceHash,
		request.RequestBytes, hashJournalBytes(request.RequestBytes), responseBytes, hashJournalBytes(responseBytes),
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		zeroBytes(responseBytes)
		if isSQLiteConstraint(err) {
			return nil, false, ErrReplay
		}
		return nil, false, fmt.Errorf("insert server request: %w", err)
	}
	for role, nonceHash := range map[string]string{"request": request.RequestNonceHash, "response": request.ResponseNonceHash, "message": request.AdditionalNonceHash} {
		if nonceHash == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_nonces (nonce_hash, request_hash, nonce_role) VALUES (?, ?, ?)`, nonceHash, request.RequestHash, role); err != nil {
			zeroBytes(responseBytes)
			if isSQLiteConstraint(err) {
				return nil, false, ErrReplay
			}
			return nil, false, fmt.Errorf("insert server nonce: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		zeroBytes(responseBytes)
		return nil, false, fmt.Errorf("commit server request: %w", err)
	}
	rollback = false
	if err := journal.validateIdentity(); err != nil {
		zeroBytes(responseBytes)
		return nil, false, err
	}
	return responseBytes, false, nil
}

func (journal *serverJournal) loadOperation(ctx context.Context, operationKey string) ([]byte, []byte, error) {
	if journal == nil || ctx == nil || operationKey == "" {
		return nil, nil, ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return nil, nil, ErrProtocol
	}
	if err := journal.validateIdentity(); err != nil {
		return nil, nil, err
	}
	tx, err := journal.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin durable server operation load: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	if err := validateServerJournalContent(ctx, tx); err != nil {
		return nil, nil, err
	}
	var requestBytes, responseBytes []byte
	var requestBytesHash, responseBytesHash string
	if err := tx.QueryRowContext(ctx, `SELECT request_bytes, request_bytes_hash, response_bytes, response_bytes_hash
		FROM trusted_supervisor_requests WHERE operation_key = ?`, operationKey).
		Scan(&requestBytes, &requestBytesHash, &responseBytes, &responseBytesHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrReplay
		}
		return nil, nil, fmt.Errorf("load durable server operation: %w", err)
	}
	if requestBytesHash != hashJournalBytes(requestBytes) || responseBytesHash != hashJournalBytes(responseBytes) {
		return nil, nil, authenticationError("durable server operation fingerprints")
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit durable server operation load: %w", err)
	}
	rollback = false
	if err := journal.validateIdentity(); err != nil {
		return nil, nil, err
	}
	return append([]byte(nil), requestBytes...), append([]byte(nil), responseBytes...), nil
}

func validServerJournalRequest(request serverJournalRequest) bool {
	additionalNonceValid := request.AdditionalNonceHash == "" || protocolHashPattern.MatchString(request.AdditionalNonceHash)
	distinctNonces := request.RequestNonceHash != request.ResponseNonceHash && request.RequestNonceHash != request.AdditionalNonceHash &&
		(request.AdditionalNonceHash == "" || request.ResponseNonceHash != request.AdditionalNonceHash)
	return protocolHashPattern.MatchString(request.RequestHash) &&
		protocolHashPattern.MatchString(request.RequestNonceHash) && protocolHashPattern.MatchString(request.ResponseNonceHash) &&
		additionalNonceValid && distinctNonces &&
		(request.Operation == operationDeliver || request.Operation == operationReconcile || request.Operation == operationCancel) &&
		strings.HasPrefix(request.OperationKey, request.Operation+":sha256:") && len(request.RequestBytes) > 0 && len(request.RequestBytes) <= int(maxFrameBytes)
}

func (journal *serverJournal) migrateAndValidate(ctx context.Context) error {
	var exists int
	if err := journal.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'trusted_supervisor_schema'`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect server journal schema: %w", err)
	}
	if exists == 0 {
		tx, err := journal.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, object := range serverJournalSchemaObjects {
			if !object.Create {
				continue
			}
			if _, err := tx.ExecContext(ctx, object.SQL); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migrate server journal: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_schema (version, migration_id, applied_at)
			VALUES (1, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, serverJournalMigrationID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record server journal migration: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit server journal migration: %w", err)
		}
		if err := fsyncDirectory(filepath.Dir(journal.path)); err != nil {
			return err
		}
	}
	var versionCount, version int
	var migrationID, appliedAt string
	if err := journal.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(version), 0), COALESCE(MAX(migration_id), ''), COALESCE(MAX(applied_at), '')
		FROM trusted_supervisor_schema`).Scan(&versionCount, &version, &migrationID, &appliedAt); err != nil {
		return fmt.Errorf("validate server journal migration: %w", err)
	}
	if versionCount != 1 || version != 1 || migrationID != serverJournalMigrationID || !validServerJournalTimestamp(appliedAt) {
		return authenticationError("server journal migration history")
	}
	if err := validateServerJournalSchema(ctx, journal.db); err != nil {
		return err
	}
	var integrity string
	if err := journal.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return authenticationError("server journal integrity")
	}
	rows, err := journal.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("server journal foreign key check: %w", err)
	}
	if rows.Next() {
		_ = rows.Close()
		return authenticationError("server journal foreign key integrity")
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("server journal foreign key check: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close server journal foreign key check: %w", err)
	}
	return validateServerJournalContent(ctx, journal.db)
}

func (journal *serverJournal) validateIdentity() error {
	if journal == nil || journal.anchor == nil {
		return authenticationError("server journal identity")
	}
	information, err := os.Lstat(journal.path)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() || information.Mode().Perm() != 0o600 {
		return authenticationError("server journal path replacement")
	}
	pathStat, ok := information.Sys().(*syscall.Stat_t)
	if !ok || pathStat.Uid != journal.ownerUID || uint64(pathStat.Dev) != journal.device || pathStat.Ino != journal.inode {
		return authenticationError("server journal path replacement")
	}
	var anchorStat unix.Stat_t
	if err := unix.Fstat(int(journal.anchor.Fd()), &anchorStat); err != nil || anchorStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		anchorStat.Uid != journal.ownerUID || uint64(anchorStat.Dev) != journal.device || anchorStat.Ino != journal.inode || anchorStat.Mode&0o777 != 0o600 {
		return authenticationError("server journal descriptor replacement")
	}
	return nil
}

func fsyncDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return authenticationError("open operator directory for sync")
	}
	defer unix.Close(fd)
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync operator directory: %w", err)
	}
	return nil
}

func hashJournalBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func isSQLiteConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "constraint")
}
