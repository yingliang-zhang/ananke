package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ExternalSupervisorEnvelopeSchemaVersion     = "ananke.remote-supervisor-sealed-launch-envelope.v1"
	ExternalSupervisorCancellationSchemaVersion = "ananke.remote-supervisor-cancellation.v1"
)

var (
	ErrExternalSupervisorInvalid         = errors.New("external supervisor handoff is invalid")
	ErrExternalSupervisorConflict        = errors.New("external supervisor handoff conflicts with durable authority")
	ErrExternalSupervisorNotFound        = errors.New("external supervisor handoff not found")
	ErrExternalSupervisorFence           = errors.New("external supervisor handoff private fence is invalid")
	ErrExternalSupervisorDeadline        = errors.New("external supervisor handoff deadline is invalid")
	ErrExternalSupervisorAttempt         = errors.New("external supervisor handoff attempt is invalid")
	ErrExternalSupervisorReceiptRequired = errors.New("external supervisor handoff requires a durable receipt")
	ErrExternalSupervisorTrustRoot       = errors.New("external supervisor handoff trust root is invalid")
)

// ExternalSupervisorPredecessorReleaseIdentity is local admission configuration.
// It is never serialized; later detached authorization records remain separate.
type ExternalSupervisorPredecessorReleaseIdentity struct {
	SupervisorArtifactSHA256 string `json:"-"`
	BuildIdentityHash        string `json:"-"`
	ReleaseAttestationHash   string `json:"-"`
	ReleaseApprovalHash      string `json:"-"`
}

// ExternalSupervisorEnvelope contains only sealed identity bindings. It has no
// executable, endpoint, credential, path, source, or evidence-content field.
type ExternalSupervisorEnvelope struct {
	SchemaVersion            string `json:"schema_version"`
	HandoffID                string `json:"handoff_id"`
	IdempotencyKeyHash       string `json:"idempotency_key_hash"`
	LaunchSpecHash           string `json:"launch_spec_hash"`
	FenceBindingHash         string `json:"fence_binding_hash"`
	Deadline                 string `json:"deadline"`
	AttemptNumber            int    `json:"attempt_number"`
	AttemptCap               int    `json:"attempt_cap"`
	RouteMappingHash         string `json:"route_mapping_hash"`
	SourceSnapshotHash       string `json:"source_snapshot_hash"`
	SourceManifestHash       string `json:"source_manifest_hash"`
	RepositoryIdentity       string `json:"repository_identity"`
	SupervisorArtifactSHA256 string `json:"supervisor_artifact_sha256"`
	BuildIdentityHash        string `json:"build_identity_hash"`
	ReleaseAttestationHash   string `json:"release_attestation_hash"`
	ReleaseApprovalHash      string `json:"release_approval_hash"`
	EvidenceContractHash     string `json:"evidence_contract_hash"`
	EvidenceSchemaVersion    string `json:"evidence_schema_version"`
	EnvelopeHash             string `json:"envelope_hash"`
}

// ExternalSupervisorCancellation records an authenticated cancellation intent.
// It intentionally has no terminal result or outcome field.
type ExternalSupervisorCancellation struct {
	SchemaVersion            string `json:"schema_version"`
	HandoffID                string `json:"handoff_id"`
	EnvelopeHash             string `json:"envelope_hash"`
	ReceiptIdentityHash      string `json:"receipt_identity_hash"`
	CancellationIdentityHash string `json:"cancellation_identity_hash"`
	AttemptNumber            int    `json:"attempt_number"`
}

// ExternalSupervisorAuthenticator re-verifies self-contained durable transport
// evidence from pinned public roots. It retains no process-local replay state.
type ExternalSupervisorAuthenticator interface {
	VerifyExternalSupervisorEnvelope(context.Context, ExternalSupervisorEnvelope) error
	VerifyExternalSupervisorReceipt(context.Context, ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) error
	VerifyExternalSupervisorCallback(context.Context, ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt, ExternalSupervisorAuthenticatedCallback) error
	VerifyExternalSupervisorCancellation(context.Context, ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt, ExternalSupervisorAuthenticatedCancellation) error
}

// ExternalSupervisorHandoff is a durable staged envelope. The complete private
// fence remains in the existing fenced-launch authority; only its binding hash
// is persisted here.
type ExternalSupervisorHandoff struct {
	Envelope       ExternalSupervisorEnvelope
	LaunchSpecHash string
	CreatedAt      time.Time
}

// ExternalSupervisorRecoveryBoundary reports exact authenticated durable
// records. Nil means absent, never an inferred execution state.
type ExternalSupervisorRecoveryBoundary struct {
	Handoff      ExternalSupervisorHandoff
	Receipt      *ExternalSupervisorAuthenticatedReceipt
	Callback     *ExternalSupervisorAuthenticatedCallback
	Cancellation *ExternalSupervisorAuthenticatedCancellation
}

// SealExternalSupervisorEnvelope validates the immutable content and derives
// its RFC 8785 JCS self-hash, excluding EnvelopeHash itself.
func SealExternalSupervisorEnvelope(envelope ExternalSupervisorEnvelope) (ExternalSupervisorEnvelope, error) {
	envelope.EnvelopeHash = ""
	if err := validateExternalSupervisorEnvelopeContent(envelope); err != nil {
		return ExternalSupervisorEnvelope{}, err
	}
	hash, err := canonicalJSONHash(externalSupervisorEnvelopeCanonicalValue(envelope, false))
	if err != nil {
		return ExternalSupervisorEnvelope{}, fmt.Errorf("canonical external supervisor envelope: %w", err)
	}
	envelope.EnvelopeHash = hash
	return envelope, nil
}

// ValidateExternalSupervisorEnvelope verifies both its closed content and its
// exact self-hash.
func ValidateExternalSupervisorEnvelope(envelope ExternalSupervisorEnvelope) error {
	if err := validateExternalSupervisorEnvelopeContent(envelope); err != nil {
		return err
	}
	if !launchHashPattern.MatchString(envelope.EnvelopeHash) {
		return fmt.Errorf("%w: envelope hash", ErrExternalSupervisorInvalid)
	}
	computed, err := canonicalJSONHash(externalSupervisorEnvelopeCanonicalValue(envelope, false))
	if err != nil {
		return fmt.Errorf("%w: canonical envelope: %v", ErrExternalSupervisorInvalid, err)
	}
	if envelope.EnvelopeHash != computed {
		return fmt.Errorf("%w: envelope self-hash", ErrExternalSupervisorInvalid)
	}
	return nil
}

// HashExternalSupervisorFenceBinding derives the opaque envelope binding for a
// complete fenced-launch claim. The token is already a durable hash; no raw
// credential is materialized.
func HashExternalSupervisorFenceBinding(fence LaunchFence) string {
	hash, err := canonicalJSONHash(map[string]any{
		"claim_id":         fence.ClaimID,
		"claim_token_hash": fence.ClaimTokenHash,
		"fence_generation": fence.FenceGeneration,
	})
	if err != nil {
		panic("external supervisor fence binding must be canonicalizable")
	}
	return hash
}

// StageExternalSupervisorHandoff transactionally stores one immutable sealed
// envelope and its immutable delivery outbox obligation after proving the
// current complete private P3b fence, deadline, and bounded attempt.
func (s *Store) StageExternalSupervisorHandoff(ctx context.Context, envelope ExternalSupervisorEnvelope, fence LaunchFence) (ExternalSupervisorHandoff, error) {
	if err := ValidateExternalSupervisorEnvelope(envelope); err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	defer func() { _ = tx.Rollback() }()

	existing, found, err := loadExternalSupervisorHandoffByAnyIdentity(ctx, tx, envelope)
	if err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	if found {
		if existing.Envelope != envelope {
			return ExternalSupervisorHandoff{}, ErrExternalSupervisorConflict
		}
		return existing, nil
	}
	if err := validateExternalSupervisorAdmission(ctx, tx, envelope, fence, time.Now().UTC()); err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	canonical, err := canonicalJSON(externalSupervisorEnvelopeCanonicalValue(envelope, true))
	if err != nil {
		return ExternalSupervisorHandoff{}, fmt.Errorf("canonical external supervisor envelope: %w", err)
	}
	createdText := nowStamp()
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return ExternalSupervisorHandoff{}, fmt.Errorf("parse handoff timestamp: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_supervisor_handoffs
		(handoff_id, envelope_hash, idempotency_key_hash, launch_spec_hash, envelope_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		envelope.HandoffID, envelope.EnvelopeHash, envelope.IdempotencyKeyHash, envelope.LaunchSpecHash, string(canonical), createdText); err != nil {
		return ExternalSupervisorHandoff{}, fmt.Errorf("insert external supervisor envelope: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_supervisor_delivery_outbox
		(handoff_id, envelope_hash, created_at) VALUES (?, ?, ?)`, envelope.HandoffID, envelope.EnvelopeHash, createdText); err != nil {
		return ExternalSupervisorHandoff{}, fmt.Errorf("insert external supervisor delivery outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	return ExternalSupervisorHandoff{Envelope: envelope, LaunchSpecHash: envelope.LaunchSpecHash, CreatedAt: createdAt}, nil
}

// GetExternalSupervisorHandoff loads and revalidates one immutable envelope.
func (s *Store) GetExternalSupervisorHandoff(ctx context.Context, handoffID string) (ExternalSupervisorHandoff, error) {
	handoff, found, err := loadExternalSupervisorHandoff(ctx, s.db, handoffID)
	if err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	if !found {
		return ExternalSupervisorHandoff{}, ErrExternalSupervisorNotFound
	}
	return handoff, nil
}

// ListPendingExternalSupervisorDeliveries lists staged obligations with no
// durable receipt. It makes no claim about whether any delivery occurred.
func (s *Store) ListPendingExternalSupervisorDeliveries(ctx context.Context) ([]ExternalSupervisorHandoff, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT h.handoff_id
		FROM external_supervisor_handoffs h
		JOIN external_supervisor_delivery_outbox o ON o.handoff_id = h.handoff_id
		LEFT JOIN external_supervisor_receipts r ON r.handoff_id = h.handoff_id
		WHERE r.handoff_id IS NULL
		ORDER BY h.created_at ASC, h.handoff_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var handoffID string
		if err := rows.Scan(&handoffID); err != nil {
			return nil, err
		}
		ids = append(ids, handoffID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	handoffs := make([]ExternalSupervisorHandoff, 0, len(ids))
	for _, handoffID := range ids {
		handoff, err := s.GetExternalSupervisorHandoff(ctx, handoffID)
		if err != nil {
			return nil, err
		}
		handoffs = append(handoffs, handoff)
	}
	return handoffs, nil
}

// GetExternalSupervisorRecoveryBoundary reads durable identities only. It never
// calls a target or derives an outcome from missing rows.
func (s *Store) GetExternalSupervisorRecoveryBoundary(ctx context.Context, handoffID string) (ExternalSupervisorRecoveryBoundary, error) {
	handoff, err := s.GetExternalSupervisorHandoff(ctx, handoffID)
	if err != nil {
		return ExternalSupervisorRecoveryBoundary{}, err
	}
	boundary := ExternalSupervisorRecoveryBoundary{Handoff: handoff}
	if receipt, found, err := loadExternalSupervisorReceipt(ctx, s.db, handoffID); err != nil {
		return ExternalSupervisorRecoveryBoundary{}, err
	} else if found {
		boundary.Receipt = &receipt
	}
	if callback, found, err := loadExternalSupervisorCallback(ctx, s.db, handoffID); err != nil {
		return ExternalSupervisorRecoveryBoundary{}, err
	} else if found {
		boundary.Callback = &callback
	}
	if cancellation, found, err := loadExternalSupervisorCancellation(ctx, s.db, handoffID); err != nil {
		return ExternalSupervisorRecoveryBoundary{}, err
	} else if found {
		boundary.Cancellation = &cancellation
	}
	return boundary, nil
}

func validateExternalSupervisorEnvelopeContent(envelope ExternalSupervisorEnvelope) error {
	if envelope.SchemaVersion != ExternalSupervisorEnvelopeSchemaVersion ||
		!validExternalSupervisorIdentifier(envelope.HandoffID) ||
		envelope.AttemptNumber < 1 || envelope.AttemptCap < 1 || envelope.AttemptNumber > envelope.AttemptCap ||
		envelope.EvidenceSchemaVersion != "ananke.remote-supervisor-evidence.v1" ||
		strings.TrimSpace(envelope.RepositoryIdentity) == "" {
		return ErrExternalSupervisorInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.Deadline); err != nil {
		return fmt.Errorf("%w: deadline", ErrExternalSupervisorInvalid)
	}
	for _, hash := range []string{
		envelope.IdempotencyKeyHash, envelope.LaunchSpecHash, envelope.FenceBindingHash, envelope.RouteMappingHash,
		envelope.SourceSnapshotHash, envelope.SourceManifestHash, envelope.SupervisorArtifactSHA256,
		envelope.BuildIdentityHash, envelope.ReleaseAttestationHash, envelope.ReleaseApprovalHash, envelope.EvidenceContractHash,
	} {
		if !launchHashPattern.MatchString(hash) {
			return fmt.Errorf("%w: identity hash", ErrExternalSupervisorInvalid)
		}
	}
	return nil
}

func validateExternalSupervisorAdmission(ctx context.Context, queryer externalSupervisorQueryer, envelope ExternalSupervisorEnvelope, fence LaunchFence, now time.Time) error {
	boundary, err := loadLaunchRecoveryBoundary(ctx, queryer, envelope.LaunchSpecHash)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExternalSupervisorFence, err)
	}
	if boundary.Action != LaunchRecoveryRetryProcessAdmission || boundary.Claim.Fence != fence || HashExternalSupervisorFenceBinding(fence) != envelope.FenceBindingHash {
		return ErrExternalSupervisorFence
	}
	stored, found, err := loadLaunchSpec(ctx, queryer, envelope.LaunchSpecHash)
	if err != nil {
		return err
	}
	if !found || stored.Spec.Deadline != envelope.Deadline {
		return ErrExternalSupervisorDeadline
	}
	deadline, err := time.Parse(time.RFC3339Nano, envelope.Deadline)
	if err != nil || !now.UTC().Before(deadline) {
		return ErrExternalSupervisorDeadline
	}
	if stored.Spec.AttemptCap != envelope.AttemptCap || boundary.Claim.Attempt != envelope.AttemptNumber || envelope.AttemptNumber > envelope.AttemptCap {
		return ErrExternalSupervisorAttempt
	}
	return nil
}

func validExternalSupervisorIdentifier(value string) bool {
	return validateIdentifier(value, "external supervisor identifier") == nil
}

func externalSupervisorEnvelopeCanonicalValue(envelope ExternalSupervisorEnvelope, includeHash bool) map[string]any {
	value := map[string]any{
		"schema_version":             envelope.SchemaVersion,
		"handoff_id":                 envelope.HandoffID,
		"idempotency_key_hash":       envelope.IdempotencyKeyHash,
		"launch_spec_hash":           envelope.LaunchSpecHash,
		"fence_binding_hash":         envelope.FenceBindingHash,
		"deadline":                   envelope.Deadline,
		"attempt_number":             envelope.AttemptNumber,
		"attempt_cap":                envelope.AttemptCap,
		"route_mapping_hash":         envelope.RouteMappingHash,
		"source_snapshot_hash":       envelope.SourceSnapshotHash,
		"source_manifest_hash":       envelope.SourceManifestHash,
		"repository_identity":        envelope.RepositoryIdentity,
		"supervisor_artifact_sha256": envelope.SupervisorArtifactSHA256,
		"build_identity_hash":        envelope.BuildIdentityHash,
		"release_attestation_hash":   envelope.ReleaseAttestationHash,
		"release_approval_hash":      envelope.ReleaseApprovalHash,
		"evidence_contract_hash":     envelope.EvidenceContractHash,
		"evidence_schema_version":    envelope.EvidenceSchemaVersion,
	}
	if includeHash {
		value["envelope_hash"] = envelope.EnvelopeHash
	}
	return value
}

type externalSupervisorQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadExternalSupervisorHandoffByAnyIdentity(ctx context.Context, queryer externalSupervisorQueryer, envelope ExternalSupervisorEnvelope) (ExternalSupervisorHandoff, bool, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT handoff_id FROM external_supervisor_handoffs
		WHERE handoff_id = ? OR envelope_hash = ? OR idempotency_key_hash = ?`, envelope.HandoffID, envelope.EnvelopeHash, envelope.IdempotencyKeyHash)
	if err != nil {
		return ExternalSupervisorHandoff{}, false, err
	}
	defer rows.Close()
	var found *ExternalSupervisorHandoff
	for rows.Next() {
		var handoffID string
		if err := rows.Scan(&handoffID); err != nil {
			return ExternalSupervisorHandoff{}, false, err
		}
		loaded, exists, err := loadExternalSupervisorHandoff(ctx, queryer, handoffID)
		if err != nil {
			return ExternalSupervisorHandoff{}, false, err
		}
		if !exists {
			return ExternalSupervisorHandoff{}, false, fmt.Errorf("%w: missing matched handoff", ErrExternalSupervisorInvalid)
		}
		if found != nil && *found != loaded {
			return ExternalSupervisorHandoff{}, false, ErrExternalSupervisorConflict
		}
		found = &loaded
	}
	if err := rows.Err(); err != nil {
		return ExternalSupervisorHandoff{}, false, err
	}
	if found == nil {
		return ExternalSupervisorHandoff{}, false, nil
	}
	return *found, true, nil
}

func loadExternalSupervisorHandoff(ctx context.Context, queryer externalSupervisorQueryer, handoffID string) (ExternalSupervisorHandoff, bool, error) {
	var envelopeJSON, launchSpecHash, createdText string
	err := queryer.QueryRowContext(ctx, `SELECT envelope_json, launch_spec_hash, created_at FROM external_supervisor_handoffs WHERE handoff_id = ?`, handoffID).Scan(&envelopeJSON, &launchSpecHash, &createdText)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSupervisorHandoff{}, false, nil
	}
	if err != nil {
		return ExternalSupervisorHandoff{}, false, err
	}
	var envelope ExternalSupervisorEnvelope
	if err := decodeCanonicalExternalSupervisorValue(envelopeJSON, &envelope, func(value ExternalSupervisorEnvelope) map[string]any {
		return externalSupervisorEnvelopeCanonicalValue(value, true)
	}); err != nil || ValidateExternalSupervisorEnvelope(envelope) != nil || envelope.LaunchSpecHash != launchSpecHash || envelope.HandoffID != handoffID {
		return ExternalSupervisorHandoff{}, false, fmt.Errorf("%w: corrupt sealed envelope", ErrExternalSupervisorInvalid)
	}
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return ExternalSupervisorHandoff{}, false, fmt.Errorf("%w: corrupt handoff timestamp", ErrExternalSupervisorInvalid)
	}
	return ExternalSupervisorHandoff{Envelope: envelope, LaunchSpecHash: launchSpecHash, CreatedAt: createdAt}, true, nil
}

func loadExternalSupervisorReceipt(ctx context.Context, queryer externalSupervisorQueryer, handoffID string) (ExternalSupervisorAuthenticatedReceipt, bool, error) {
	var raw string
	err := queryer.QueryRowContext(ctx, `SELECT receipt_json FROM external_supervisor_receipts WHERE handoff_id = ?`, handoffID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSupervisorAuthenticatedReceipt{}, false, nil
	}
	if err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, false, err
	}
	var receipt ExternalSupervisorAuthenticatedReceipt
	if err := decodeCanonicalExternalSupervisorProtocol(raw, &receipt); err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, false, fmt.Errorf("%w: corrupt authenticated receipt", ErrExternalSupervisorInvalid)
	}
	handoff, found, err := loadExternalSupervisorHandoff(ctx, queryer, handoffID)
	if err != nil || !found || validateAuthenticatedExternalSupervisorReceipt(receipt, handoff.Envelope) != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, false, fmt.Errorf("%w: corrupt authenticated receipt binding", ErrExternalSupervisorInvalid)
	}
	return receipt, true, nil
}

func loadExternalSupervisorCallback(ctx context.Context, queryer externalSupervisorQueryer, handoffID string) (ExternalSupervisorAuthenticatedCallback, bool, error) {
	var raw string
	err := queryer.QueryRowContext(ctx, `SELECT callback_json FROM external_supervisor_callbacks WHERE handoff_id = ?`, handoffID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSupervisorAuthenticatedCallback{}, false, nil
	}
	if err != nil {
		return ExternalSupervisorAuthenticatedCallback{}, false, err
	}
	var callback ExternalSupervisorAuthenticatedCallback
	if err := decodeCanonicalExternalSupervisorProtocol(raw, &callback); err != nil {
		return ExternalSupervisorAuthenticatedCallback{}, false, fmt.Errorf("%w: corrupt authenticated callback", ErrExternalSupervisorInvalid)
	}
	handoff, found, err := loadExternalSupervisorHandoff(ctx, queryer, handoffID)
	if err != nil || !found {
		return ExternalSupervisorAuthenticatedCallback{}, false, fmt.Errorf("%w: corrupt callback handoff", ErrExternalSupervisorInvalid)
	}
	receipt, found, err := loadExternalSupervisorReceipt(ctx, queryer, handoffID)
	if err != nil || !found || validateAuthenticatedExternalSupervisorCallback(callback, handoff.Envelope, receipt) != nil {
		return ExternalSupervisorAuthenticatedCallback{}, false, fmt.Errorf("%w: corrupt authenticated callback binding", ErrExternalSupervisorInvalid)
	}
	return callback, true, nil
}

func loadExternalSupervisorCancellation(ctx context.Context, queryer externalSupervisorQueryer, handoffID string) (ExternalSupervisorAuthenticatedCancellation, bool, error) {
	var raw string
	err := queryer.QueryRowContext(ctx, `SELECT cancellation_json FROM external_supervisor_cancellations WHERE handoff_id = ?`, handoffID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSupervisorAuthenticatedCancellation{}, false, nil
	}
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, false, err
	}
	var cancellation ExternalSupervisorAuthenticatedCancellation
	if err := decodeCanonicalExternalSupervisorProtocol(raw, &cancellation); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, false, fmt.Errorf("%w: corrupt authenticated cancellation", ErrExternalSupervisorInvalid)
	}
	handoff, found, err := loadExternalSupervisorHandoff(ctx, queryer, handoffID)
	if err != nil || !found {
		return ExternalSupervisorAuthenticatedCancellation{}, false, fmt.Errorf("%w: corrupt cancellation handoff", ErrExternalSupervisorInvalid)
	}
	receipt, found, err := loadExternalSupervisorReceipt(ctx, queryer, handoffID)
	if err != nil || !found || validateAuthenticatedExternalSupervisorCancellation(cancellation, handoff.Envelope, receipt) != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, false, fmt.Errorf("%w: corrupt authenticated cancellation binding", ErrExternalSupervisorInvalid)
	}
	return cancellation, true, nil
}

func decodeCanonicalExternalSupervisorValue[T any](raw string, target *T, canonical func(T) map[string]any) error {
	if err := jsonUnmarshalStrict([]byte(raw), target); err != nil {
		return err
	}
	canonicalBytes, err := canonicalJSON(canonical(*target))
	if err != nil || string(canonicalBytes) != raw {
		return ErrExternalSupervisorInvalid
	}
	return nil
}

func jsonUnmarshalStrict(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return ErrExternalSupervisorInvalid
	}
	return nil
}

func migrateV12(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE external_supervisor_handoffs (
			handoff_id TEXT PRIMARY KEY,
			envelope_hash TEXT NOT NULL UNIQUE,
			idempotency_key_hash TEXT NOT NULL UNIQUE,
			launch_spec_hash TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (launch_spec_hash) REFERENCES launch_specs(launch_spec_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE external_supervisor_delivery_outbox (
			handoff_id TEXT PRIMARY KEY,
			envelope_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			FOREIGN KEY (handoff_id) REFERENCES external_supervisor_handoffs(handoff_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE external_supervisor_receipts (
			receipt_identity_hash TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL UNIQUE,
			envelope_hash TEXT NOT NULL,
			attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
			root_id TEXT NOT NULL,
			trust_bundle_hash TEXT NOT NULL,
			receipt_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (handoff_id) REFERENCES external_supervisor_handoffs(handoff_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE external_supervisor_callbacks (
			callback_identity_hash TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL UNIQUE,
			envelope_hash TEXT NOT NULL,
			receipt_identity_hash TEXT NOT NULL,
			attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
			root_id TEXT NOT NULL,
			trust_bundle_hash TEXT NOT NULL,
			callback_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (handoff_id) REFERENCES external_supervisor_handoffs(handoff_id)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (receipt_identity_hash) REFERENCES external_supervisor_receipts(receipt_identity_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE external_supervisor_cancellations (
			cancellation_identity_hash TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL UNIQUE,
			envelope_hash TEXT NOT NULL,
			receipt_identity_hash TEXT NOT NULL,
			attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
			cancellation_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (handoff_id) REFERENCES external_supervisor_handoffs(handoff_id)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (receipt_identity_hash) REFERENCES external_supervisor_receipts(receipt_identity_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE INDEX idx_external_supervisor_delivery_pending ON external_supervisor_delivery_outbox (created_at, handoff_id)`,
		`CREATE TRIGGER external_supervisor_handoffs_insert_only_update BEFORE UPDATE ON external_supervisor_handoffs
			BEGIN SELECT RAISE(ABORT, 'external supervisor handoffs are immutable'); END`,
		`CREATE TRIGGER external_supervisor_handoffs_insert_only_delete BEFORE DELETE ON external_supervisor_handoffs
			BEGIN SELECT RAISE(ABORT, 'external supervisor handoffs are immutable'); END`,
		`CREATE TRIGGER external_supervisor_delivery_outbox_insert_only_update BEFORE UPDATE ON external_supervisor_delivery_outbox
			BEGIN SELECT RAISE(ABORT, 'external supervisor delivery outbox is immutable'); END`,
		`CREATE TRIGGER external_supervisor_delivery_outbox_insert_only_delete BEFORE DELETE ON external_supervisor_delivery_outbox
			BEGIN SELECT RAISE(ABORT, 'external supervisor delivery outbox is immutable'); END`,
		`CREATE TRIGGER external_supervisor_receipts_insert_only_update BEFORE UPDATE ON external_supervisor_receipts
			BEGIN SELECT RAISE(ABORT, 'external supervisor receipts are immutable'); END`,
		`CREATE TRIGGER external_supervisor_receipts_insert_only_delete BEFORE DELETE ON external_supervisor_receipts
			BEGIN SELECT RAISE(ABORT, 'external supervisor receipts are immutable'); END`,
		`CREATE TRIGGER external_supervisor_callbacks_insert_only_update BEFORE UPDATE ON external_supervisor_callbacks
			BEGIN SELECT RAISE(ABORT, 'external supervisor callbacks are immutable'); END`,
		`CREATE TRIGGER external_supervisor_callbacks_insert_only_delete BEFORE DELETE ON external_supervisor_callbacks
			BEGIN SELECT RAISE(ABORT, 'external supervisor callbacks are immutable'); END`,
		`CREATE TRIGGER external_supervisor_cancellations_insert_only_update BEFORE UPDATE ON external_supervisor_cancellations
			BEGIN SELECT RAISE(ABORT, 'external supervisor cancellations are immutable'); END`,
		`CREATE TRIGGER external_supervisor_cancellations_insert_only_delete BEFORE DELETE ON external_supervisor_cancellations
			BEGIN SELECT RAISE(ABORT, 'external supervisor cancellations are immutable'); END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
