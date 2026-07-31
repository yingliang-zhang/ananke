package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

// ErrAttestationNotFound is returned when no attestation row exists for a hash.
var ErrAttestationNotFound = errors.New("repair attestation not found")

// ErrAttestationConflict is returned when an attestation with the same hash
// already exists but has different content (a security-relevant collision).
var ErrAttestationConflict = errors.New("repair attestation conflict")

// ErrAttestationInvalid is returned when an attestation record fails input
// validation (empty required fields, etc.). This is distinct from
// ErrAttestationConflict to avoid false-positive collision signals.
var ErrAttestationInvalid = errors.New("repair attestation invalid")

// RepairAttestationRow is a stored repair-review attestation with its outbox
// delivery status. The attestation_json field holds the canonical JSON of the
// full RepairReviewAttestation record.
type RepairAttestationRow struct {
	AttestationHash     string
	AttestationID       string
	AuthorizationHash   string
	AttemptHash         string
	AttemptNumber       int
	SignatureDomain     string
	SignatureHash       string
	State               string
	IssuedAt            string
	AttestationJSON     string
	OutboxDelivered     int    // 0 pending, 1 delivered, -1 abandoned
	DeliveredDiagnostic string // empty unless abandoned
	CreatedAt           time.Time
	DeliveredAt         time.Time // zero if not delivered
}

// PersistRepairAttestation atomically stores a signed repair-review attestation
// and creates an outbox mirror row for delivery to Ananke. Both writes occur in
// a single transaction. If the attestation already exists with identical
// content, the existing row is returned (idempotent replay). If the hash
// matches but content differs, ErrAttestationConflict is returned.
func (s *Store) PersistRepairAttestation(ctx context.Context, record repaircontract.RepairReviewAttestation) (RepairAttestationRow, error) {
	if err := validateRepairAttestationRecord(record); err != nil {
		return RepairAttestationRow{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RepairAttestationRow{}, err
	}
	defer func() { _ = tx.Rollback() }()
	return s.persistRepairAttestationTx(ctx, tx, record)
}

func (s *Store) persistRepairAttestationTx(ctx context.Context, tx *sql.Tx, record repaircontract.RepairReviewAttestation) (RepairAttestationRow, error) {
	// Check for existing attestation with the same hash.
	existing, found, err := loadRepairAttestationByHash(ctx, tx, record.AttestationHash)
	if err != nil {
		return RepairAttestationRow{}, err
	}
	if found {
		// Idempotent replay: return existing if content matches.
		if existing.AttestationJSON != canonicalJSONString(record) {
			return RepairAttestationRow{}, fmt.Errorf("%w: attestation_hash %s", ErrAttestationConflict, record.AttestationHash)
		}
		return existing, nil
	}

	// Marshal the attestation to canonical JSON.
	attestationJSON, err := transportprimitives.MarshalCanonical(record)
	if err != nil {
		return RepairAttestationRow{}, fmt.Errorf("marshal attestation: %w", err)
	}

	createdAt := nowStamp()

	// Insert into the immutable attestation table.
	if _, err := tx.ExecContext(ctx, `INSERT INTO repair_review_attestations
		(attestation_hash, attestation_id, authorization_hash, attempt_hash, attempt_number,
		 signature_domain, signature_hash, state, issued_at, attestation_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.AttestationHash, record.AttestationID,
		record.AuthorizationHash, record.AttemptHash, record.AttemptNumber,
		record.SignatureDomain, record.Signature,
		string(record.State), record.IssuedAt,
		attestationJSON, createdAt); err != nil {
		return RepairAttestationRow{}, fmt.Errorf("insert repair attestation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO repair_attestation_outbox
		(attestation_hash, delivered, created_at)
		VALUES (?, 0, ?)`,
		record.AttestationHash, createdAt); err != nil {
		return RepairAttestationRow{}, fmt.Errorf("insert attestation outbox: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return RepairAttestationRow{}, err
	}

	return RepairAttestationRow{
		AttestationHash:   record.AttestationHash,
		AttestationID:     record.AttestationID,
		AuthorizationHash: record.AuthorizationHash,
		AttemptHash:       record.AttemptHash,
		AttemptNumber:     record.AttemptNumber,
		SignatureDomain:   record.SignatureDomain,
		SignatureHash:     record.Signature,
		State:             string(record.State),
		IssuedAt:          record.IssuedAt,
		AttestationJSON:   string(attestationJSON),
		OutboxDelivered:   0,
		CreatedAt:         stampToTime(createdAt),
	}, nil
}

// AcknowledgeRepairAttestationOutbox marks the pending outbox row for an
// attestation as delivered. Idempotent-failing: re-acknowledging an already
// delivered row returns ErrAttestationNotFound.
func (s *Store) AcknowledgeRepairAttestationOutbox(ctx context.Context, attestationHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE repair_attestation_outbox SET delivered = 1, delivered_at = ?
		WHERE attestation_hash = ? AND delivered = 0`,
		nowStamp(), attestationHash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAttestationNotFound
	}
	return nil
}

// AbandonRepairAttestationOutbox marks a pending outbox row as abandoned
// when delivery is irrecoverably lost.
func (s *Store) AbandonRepairAttestationOutbox(ctx context.Context, attestationHash string, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("outbox abandonment diagnostic required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE repair_attestation_outbox SET delivered = -1, delivered_at = ?, delivered_diagnostic = ?
		WHERE attestation_hash = ? AND delivered = 0`,
		nowStamp(), reason, attestationHash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAttestationNotFound
	}
	return nil
}

// GetRepairAttestation loads a stored repair-review attestation by its hash.
func (s *Store) GetRepairAttestation(ctx context.Context, attestationHash string) (RepairAttestationRow, error) {
	row, found, err := loadRepairAttestationByHash(ctx, s.db, attestationHash)
	if err != nil {
		return RepairAttestationRow{}, err
	}
	if !found {
		return RepairAttestationRow{}, ErrAttestationNotFound
	}
	return row, nil
}

// GetLatestRepairAttestation returns the most recently created attestation.
func (s *Store) GetLatestRepairAttestation(ctx context.Context) (RepairAttestationRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.attestation_hash, a.attestation_id, a.authorization_hash,
			a.attempt_hash, a.attempt_number, a.signature_domain,
			a.signature_hash, a.state, a.issued_at, a.attestation_json,
			COALESCE(o.delivered, 0), a.created_at, COALESCE(o.delivered_at, ''), COALESCE(o.delivered_diagnostic, '')
		FROM repair_review_attestations a
		LEFT JOIN repair_attestation_outbox o ON a.attestation_hash = o.attestation_hash
		ORDER BY a.created_at DESC LIMIT 1`)
	if err != nil {
		return RepairAttestationRow{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return RepairAttestationRow{}, ErrAttestationNotFound
	}
	var row RepairAttestationRow
	var deliveredAt, createdAtStr, diagnostic string
	if err := rows.Scan(
		&row.AttestationHash, &row.AttestationID, &row.AuthorizationHash,
		&row.AttemptHash, &row.AttemptNumber, &row.SignatureDomain,
		&row.SignatureHash, &row.State, &row.IssuedAt, &row.AttestationJSON,
		&row.OutboxDelivered, &createdAtStr, &deliveredAt, &diagnostic); err != nil {
		return RepairAttestationRow{}, err
	}
	row.CreatedAt = stampToTime(createdAtStr)
	row.DeliveredDiagnostic = diagnostic
	if deliveredAt != "" {
		row.DeliveredAt = stampToTime(deliveredAt)
	}
	return row, rows.Err()
}

// GetPendingRepairAttestationOutbox returns all pending (undelivered)
func (s *Store) GetPendingRepairAttestationOutbox(ctx context.Context) ([]RepairAttestationRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.attestation_hash, a.attestation_id, a.authorization_hash,
			a.attempt_hash, a.attempt_number, a.signature_domain,
			a.signature_hash, a.state, a.issued_at, a.attestation_json,
			o.delivered, a.created_at, COALESCE(o.delivered_at, ''), COALESCE(o.delivered_diagnostic, '')
		FROM repair_review_attestations a
		JOIN repair_attestation_outbox o ON a.attestation_hash = o.attestation_hash
		WHERE o.delivered = 0
		ORDER BY a.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RepairAttestationRow
	for rows.Next() {
		var row RepairAttestationRow
		var deliveredAt string
		var createdAtStr string
		var diagnostic string
		if err := rows.Scan(
			&row.AttestationHash, &row.AttestationID, &row.AuthorizationHash,
			&row.AttemptHash, &row.AttemptNumber, &row.SignatureDomain,
			&row.SignatureHash, &row.State, &row.IssuedAt, &row.AttestationJSON,
			&row.OutboxDelivered, &createdAtStr, &deliveredAt, &diagnostic); err != nil {
			return nil, err
		}
		row.CreatedAt = stampToTime(createdAtStr)
		row.DeliveredDiagnostic = diagnostic
		if deliveredAt != "" {
			row.DeliveredAt = stampToTime(deliveredAt)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// --- helpers ---

func validateRepairAttestationRecord(record repaircontract.RepairReviewAttestation) error {
	if record.AttestationHash == "" {
		return fmt.Errorf("%w: empty attestation hash", ErrAttestationInvalid)
	}
	if record.AttestationID == "" {
		return fmt.Errorf("%w: empty attestation id", ErrAttestationInvalid)
	}
	if record.SignatureDomain == "" {
		return fmt.Errorf("%w: empty signature domain", ErrAttestationInvalid)
	}
	return nil
}

func loadRepairAttestationByHash(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, attestationHash string) (RepairAttestationRow, bool, error) {
	row := queryer.QueryRowContext(ctx,
		`SELECT a.attestation_hash, a.attestation_id, a.authorization_hash,
			a.attempt_hash, a.attempt_number, a.signature_domain,
			a.signature_hash, a.state, a.issued_at, a.attestation_json,
			COALESCE(o.delivered, 0), a.created_at, COALESCE(o.delivered_at, ''), COALESCE(o.delivered_diagnostic, '')
		FROM repair_review_attestations a
		LEFT JOIN repair_attestation_outbox o ON a.attestation_hash = o.attestation_hash
		WHERE a.attestation_hash = ?`, attestationHash)
	var r RepairAttestationRow
	var deliveredAt string
	var createdAtStr string
	var diagnostic string
	if err := row.Scan(
		&r.AttestationHash, &r.AttestationID, &r.AuthorizationHash,
		&r.AttemptHash, &r.AttemptNumber, &r.SignatureDomain,
		&r.SignatureHash, &r.State, &r.IssuedAt, &r.AttestationJSON,
		&r.OutboxDelivered, &createdAtStr, &deliveredAt, &diagnostic); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RepairAttestationRow{}, false, nil
		}
		return RepairAttestationRow{}, false, err
	}
	r.CreatedAt = stampToTime(createdAtStr)
	r.DeliveredDiagnostic = diagnostic
	if deliveredAt != "" {
		r.DeliveredAt = stampToTime(deliveredAt)
	}
	return r, true, nil
}

// canonicalJSONString returns the canonical JSON string of a record.
func canonicalJSONString(record repaircontract.RepairReviewAttestation) string {
	b, err := transportprimitives.MarshalCanonical(record)
	if err != nil {
		return ""
	}
	return string(b)
}

// stampToTime parses an RFC3339Nano timestamp, returning zero on error.
func stampToTime(stamp string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

// migrateV15 creates the repair-review attestation storage and outbox mirror
// tables. The attestation table is immutable (insert-only triggers). The
// outbox table tracks delivery state (pending/delivered/abandoned).
func migrateV15(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE repair_review_attestations (
			attestation_hash TEXT PRIMARY KEY,
			attestation_id TEXT NOT NULL,
			authorization_hash TEXT NOT NULL,
			attempt_hash TEXT NOT NULL,
			attempt_number INTEGER NOT NULL,
			signature_domain TEXT NOT NULL,
			signature_hash TEXT NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('waiting_for_review')),
			issued_at TEXT NOT NULL,
			attestation_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX idx_repair_attestations_authorization
			ON repair_review_attestations (authorization_hash)`,
		`CREATE INDEX idx_repair_attestations_attempt
			ON repair_review_attestations (attempt_hash, attempt_number)`,
		`CREATE TRIGGER repair_review_attestations_insert_only_update
			BEFORE UPDATE ON repair_review_attestations
			BEGIN SELECT RAISE(ABORT, 'repair review attestations are immutable'); END`,
		`CREATE TRIGGER repair_review_attestations_insert_only_delete
			BEFORE DELETE ON repair_review_attestations
			BEGIN SELECT RAISE(ABORT, 'repair review attestations are immutable'); END`,
		`CREATE TABLE repair_attestation_outbox (
			attestation_hash TEXT PRIMARY KEY,
			delivered INTEGER NOT NULL DEFAULT 0 CHECK (delivered IN (0, 1, -1)),
			created_at TEXT NOT NULL,
			delivered_at TEXT,
			delivered_diagnostic TEXT,
			FOREIGN KEY (attestation_hash) REFERENCES repair_review_attestations(attestation_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE INDEX idx_repair_attestation_outbox_pending
			ON repair_attestation_outbox (created_at, attestation_hash) WHERE delivered = 0`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
