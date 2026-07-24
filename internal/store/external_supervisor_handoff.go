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
	ExternalSupervisorReceiptSchemaVersion      = "ananke.remote-supervisor-acceptance-receipt.v1"
	ExternalSupervisorCallbackSchemaVersion     = "ananke.remote-supervisor-callback.v1"
	ExternalSupervisorResultSchemaVersion       = "ananke.remote-supervisor-result.v1"
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

// ExternalSupervisorTrustRoot is the current, independently authenticated
// trust-bundle identity supplied by the caller that owns root verification.
type ExternalSupervisorTrustRoot struct {
	RootID          string
	TrustBundleHash string
}

// ExternalSupervisorAcceptanceReceipt is the only acknowledgement accepted by
// the handoff journal. It is identity-only and must be authenticated by the
// current trust root before persistence.
type ExternalSupervisorAcceptanceReceipt struct {
	SchemaVersion       string `json:"schema_version"`
	HandoffID           string `json:"handoff_id"`
	EnvelopeHash        string `json:"envelope_hash"`
	ReceiptIdentityHash string `json:"receipt_identity_hash"`
	AttemptNumber       int    `json:"attempt_number"`
	RootID              string `json:"root_id"`
	TrustBundleHash     string `json:"trust_bundle_hash"`
	SignatureHash       string `json:"signature_hash"`
}

// ExternalSupervisorResult carries a typed terminal claim and identity hashes
// only. It does not change any local Run state.
type ExternalSupervisorResult struct {
	SchemaVersion        string `json:"schema_version"`
	TerminalState        string `json:"terminal_state"`
	EnvelopeHash         string `json:"envelope_hash"`
	ReceiptIdentityHash  string `json:"receipt_identity_hash"`
	EvidenceIdentityHash string `json:"evidence_identity_hash"`
}

// ExternalSupervisorCallback is accepted only after its exact receipt is
// durable and an independently supplied verifier authenticates it under the
// current trust root.
type ExternalSupervisorCallback struct {
	SchemaVersion        string                   `json:"schema_version"`
	HandoffID            string                   `json:"handoff_id"`
	EnvelopeHash         string                   `json:"envelope_hash"`
	ReceiptIdentityHash  string                   `json:"receipt_identity_hash"`
	CallbackIdentityHash string                   `json:"callback_identity_hash"`
	AttemptNumber        int                      `json:"attempt_number"`
	RootID               string                   `json:"root_id"`
	TrustBundleHash      string                   `json:"trust_bundle_hash"`
	SignatureHash        string                   `json:"signature_hash"`
	Result               ExternalSupervisorResult `json:"result"`
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

// ExternalSupervisorAuthenticator owns detached receipt/callback authentication.
// Production provides no implementation; tests use a strictly in-process fake.
type ExternalSupervisorAuthenticator interface {
	VerifyExternalSupervisorReceipt(context.Context, ExternalSupervisorAcceptanceReceipt, ExternalSupervisorTrustRoot) error
	VerifyExternalSupervisorCallback(context.Context, ExternalSupervisorCallback, ExternalSupervisorTrustRoot) error
}

// ExternalSupervisorHandoff is a durable staged envelope. The complete private
// fence remains in the existing fenced-launch authority; only its binding hash
// is persisted here.
type ExternalSupervisorHandoff struct {
	Envelope       ExternalSupervisorEnvelope
	LaunchSpecHash string
	CreatedAt      time.Time
}

// ExternalSupervisorRecoveryBoundary reports identities already durable for a
// handoff. Nil means absent, never an inferred execution or cancellation state.
type ExternalSupervisorRecoveryBoundary struct {
	Handoff      ExternalSupervisorHandoff
	Receipt      *ExternalSupervisorAcceptanceReceipt
	Callback     *ExternalSupervisorCallback
	Cancellation *ExternalSupervisorCancellation
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

// WithExternalSupervisorDeliveryAdmission holds the SQLite immediate lock from
// full private-fence validation through the caller's in-process delivery. No
// delivery result is persisted by this method.
func (s *Store) WithExternalSupervisorDeliveryAdmission(ctx context.Context, handoffID string, invoke func(ExternalSupervisorEnvelope) error) error {
	if invoke == nil {
		return fmt.Errorf("%w: nil delivery", ErrExternalSupervisorInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, found, err := loadExternalSupervisorHandoff(ctx, tx, handoffID)
	if err != nil {
		return err
	}
	if !found {
		return ErrExternalSupervisorNotFound
	}
	boundary, err := loadLaunchRecoveryBoundary(ctx, tx, handoff.LaunchSpecHash)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExternalSupervisorFence, err)
	}
	if err := validateExternalSupervisorAdmission(ctx, tx, handoff.Envelope, boundary.Claim.Fence, time.Now().UTC()); err != nil {
		return err
	}
	return invoke(handoff.Envelope)
}

// AcceptExternalSupervisorReceipt authenticates and durably records the exact
// typed receipt. A duplicate exact receipt is idempotent; any other identity
// for the handoff is a conflict.
func (s *Store) AcceptExternalSupervisorReceipt(ctx context.Context, receipt ExternalSupervisorAcceptanceReceipt, root ExternalSupervisorTrustRoot, authenticator ExternalSupervisorAuthenticator) (ExternalSupervisorAcceptanceReceipt, error) {
	if err := validateExternalSupervisorReceipt(receipt); err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, err
	}
	if err := validateExternalSupervisorTrustRoot(root); err != nil || receipt.RootID != root.RootID || receipt.TrustBundleHash != root.TrustBundleHash {
		return ExternalSupervisorAcceptanceReceipt{}, ErrExternalSupervisorTrustRoot
	}
	if authenticator == nil || authenticator.VerifyExternalSupervisorReceipt(ctx, receipt, root) != nil {
		return ExternalSupervisorAcceptanceReceipt{}, ErrExternalSupervisorTrustRoot
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, found, err := loadExternalSupervisorHandoff(ctx, tx, receipt.HandoffID)
	if err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, err
	}
	if !found {
		return ExternalSupervisorAcceptanceReceipt{}, ErrExternalSupervisorNotFound
	}
	if err := validateExternalSupervisorReceiptBinding(receipt, handoff.Envelope); err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, err
	}
	boundary, err := loadLaunchRecoveryBoundary(ctx, tx, handoff.LaunchSpecHash)
	if err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, fmt.Errorf("%w: %v", ErrExternalSupervisorFence, err)
	}
	if err := validateExternalSupervisorAdmission(ctx, tx, handoff.Envelope, boundary.Claim.Fence, time.Now().UTC()); err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, err
	}
	existing, found, err := loadExternalSupervisorReceipt(ctx, tx, receipt.HandoffID)
	if err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, err
	}
	if found {
		if existing != receipt {
			return ExternalSupervisorAcceptanceReceipt{}, ErrExternalSupervisorConflict
		}
		return existing, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_supervisor_receipts
		(receipt_identity_hash, handoff_id, envelope_hash, attempt_number, root_id, trust_bundle_hash, receipt_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, receipt.ReceiptIdentityHash, receipt.HandoffID, receipt.EnvelopeHash, receipt.AttemptNumber, receipt.RootID, receipt.TrustBundleHash, mustCanonicalExternalSupervisorReceipt(receipt), nowStamp()); err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, fmt.Errorf("insert external supervisor receipt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, err
	}
	return receipt, nil
}

// AcceptExternalSupervisorCallback requires an existing exact durable receipt,
// verifies root and envelope binding, and appends the typed callback identity.
// It does not project a terminal local execution state.
func (s *Store) AcceptExternalSupervisorCallback(ctx context.Context, callback ExternalSupervisorCallback, root ExternalSupervisorTrustRoot, authenticator ExternalSupervisorAuthenticator) (ExternalSupervisorCallback, error) {
	if err := validateExternalSupervisorCallback(callback); err != nil {
		return ExternalSupervisorCallback{}, err
	}
	if err := validateExternalSupervisorTrustRoot(root); err != nil || callback.RootID != root.RootID || callback.TrustBundleHash != root.TrustBundleHash {
		return ExternalSupervisorCallback{}, ErrExternalSupervisorTrustRoot
	}
	if authenticator == nil || authenticator.VerifyExternalSupervisorCallback(ctx, callback, root) != nil {
		return ExternalSupervisorCallback{}, ErrExternalSupervisorTrustRoot
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalSupervisorCallback{}, err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, found, err := loadExternalSupervisorHandoff(ctx, tx, callback.HandoffID)
	if err != nil {
		return ExternalSupervisorCallback{}, err
	}
	if !found {
		return ExternalSupervisorCallback{}, ErrExternalSupervisorNotFound
	}
	receipt, found, err := loadExternalSupervisorReceipt(ctx, tx, callback.HandoffID)
	if err != nil {
		return ExternalSupervisorCallback{}, err
	}
	if !found {
		return ExternalSupervisorCallback{}, ErrExternalSupervisorReceiptRequired
	}
	if err := validateExternalSupervisorCallbackBinding(callback, handoff.Envelope, receipt, root); err != nil {
		return ExternalSupervisorCallback{}, err
	}
	boundary, err := loadLaunchRecoveryBoundary(ctx, tx, handoff.LaunchSpecHash)
	if err != nil {
		return ExternalSupervisorCallback{}, fmt.Errorf("%w: %v", ErrExternalSupervisorFence, err)
	}
	if err := validateExternalSupervisorAdmission(ctx, tx, handoff.Envelope, boundary.Claim.Fence, time.Now().UTC()); err != nil {
		return ExternalSupervisorCallback{}, err
	}
	existing, found, err := loadExternalSupervisorCallback(ctx, tx, callback.HandoffID)
	if err != nil {
		return ExternalSupervisorCallback{}, err
	}
	if found {
		if existing != callback {
			return ExternalSupervisorCallback{}, ErrExternalSupervisorConflict
		}
		return existing, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_supervisor_callbacks
		(callback_identity_hash, handoff_id, envelope_hash, receipt_identity_hash, attempt_number, root_id, trust_bundle_hash, callback_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, callback.CallbackIdentityHash, callback.HandoffID, callback.EnvelopeHash, callback.ReceiptIdentityHash, callback.AttemptNumber, callback.RootID, callback.TrustBundleHash, mustCanonicalExternalSupervisorCallback(callback), nowStamp()); err != nil {
		return ExternalSupervisorCallback{}, fmt.Errorf("insert external supervisor callback: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ExternalSupervisorCallback{}, err
	}
	return callback, nil
}

// RecordExternalSupervisorCancellation records a receipt-bound cancellation
// intent only after reauthenticating the complete active fence. It never infers
// a terminal result from cancellation, delivery, silence, or elapsed time.
func (s *Store) RecordExternalSupervisorCancellation(ctx context.Context, cancellation ExternalSupervisorCancellation, fence LaunchFence) (ExternalSupervisorCancellation, error) {
	if err := validateExternalSupervisorCancellation(cancellation); err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, found, err := loadExternalSupervisorHandoff(ctx, tx, cancellation.HandoffID)
	if err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	if !found {
		return ExternalSupervisorCancellation{}, ErrExternalSupervisorNotFound
	}
	boundary, err := loadLaunchRecoveryBoundary(ctx, tx, handoff.LaunchSpecHash)
	if err != nil {
		return ExternalSupervisorCancellation{}, fmt.Errorf("%w: %v", ErrExternalSupervisorFence, err)
	}
	if boundary.Claim.Fence != fence || HashExternalSupervisorFenceBinding(fence) != handoff.Envelope.FenceBindingHash {
		return ExternalSupervisorCancellation{}, ErrExternalSupervisorFence
	}
	if err := validateExternalSupervisorAdmission(ctx, tx, handoff.Envelope, fence, time.Now().UTC()); err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	receipt, found, err := loadExternalSupervisorReceipt(ctx, tx, cancellation.HandoffID)
	if err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	if !found {
		return ExternalSupervisorCancellation{}, ErrExternalSupervisorReceiptRequired
	}
	if cancellation.EnvelopeHash != handoff.Envelope.EnvelopeHash || cancellation.ReceiptIdentityHash != receipt.ReceiptIdentityHash || cancellation.AttemptNumber != handoff.Envelope.AttemptNumber {
		return ExternalSupervisorCancellation{}, ErrExternalSupervisorConflict
	}
	existing, found, err := loadExternalSupervisorCancellation(ctx, tx, cancellation.HandoffID)
	if err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	if found {
		if existing != cancellation {
			return ExternalSupervisorCancellation{}, ErrExternalSupervisorConflict
		}
		return existing, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_supervisor_cancellations
		(cancellation_identity_hash, handoff_id, envelope_hash, receipt_identity_hash, attempt_number, cancellation_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, cancellation.CancellationIdentityHash, cancellation.HandoffID, cancellation.EnvelopeHash, cancellation.ReceiptIdentityHash, cancellation.AttemptNumber, mustCanonicalExternalSupervisorCancellation(cancellation), nowStamp()); err != nil {
		return ExternalSupervisorCancellation{}, fmt.Errorf("insert external supervisor cancellation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	return cancellation, nil
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

func validateExternalSupervisorTrustRoot(root ExternalSupervisorTrustRoot) error {
	if !validExternalSupervisorIdentifier(root.RootID) || !launchHashPattern.MatchString(root.TrustBundleHash) {
		return ErrExternalSupervisorTrustRoot
	}
	return nil
}

func validateExternalSupervisorReceipt(receipt ExternalSupervisorAcceptanceReceipt) error {
	if receipt.SchemaVersion != ExternalSupervisorReceiptSchemaVersion || !validExternalSupervisorIdentifier(receipt.HandoffID) ||
		receipt.AttemptNumber < 1 || !validExternalSupervisorIdentifier(receipt.RootID) {
		return ErrExternalSupervisorInvalid
	}
	for _, hash := range []string{receipt.EnvelopeHash, receipt.ReceiptIdentityHash, receipt.TrustBundleHash, receipt.SignatureHash} {
		if !launchHashPattern.MatchString(hash) {
			return ErrExternalSupervisorInvalid
		}
	}
	return nil
}

func validateExternalSupervisorCallback(callback ExternalSupervisorCallback) error {
	if callback.SchemaVersion != ExternalSupervisorCallbackSchemaVersion || !validExternalSupervisorIdentifier(callback.HandoffID) ||
		callback.AttemptNumber < 1 || !validExternalSupervisorIdentifier(callback.RootID) {
		return ErrExternalSupervisorInvalid
	}
	for _, hash := range []string{callback.EnvelopeHash, callback.ReceiptIdentityHash, callback.CallbackIdentityHash, callback.TrustBundleHash, callback.SignatureHash} {
		if !launchHashPattern.MatchString(hash) {
			return ErrExternalSupervisorInvalid
		}
	}
	if callback.Result.SchemaVersion != ExternalSupervisorResultSchemaVersion || callback.Result.EnvelopeHash != callback.EnvelopeHash ||
		callback.Result.ReceiptIdentityHash != callback.ReceiptIdentityHash || !launchHashPattern.MatchString(callback.Result.EvidenceIdentityHash) {
		return ErrExternalSupervisorInvalid
	}
	switch callback.Result.TerminalState {
	case "completed", "failed", "cancelled":
		return nil
	default:
		return ErrExternalSupervisorInvalid
	}
}

func validateExternalSupervisorCancellation(cancellation ExternalSupervisorCancellation) error {
	if cancellation.SchemaVersion != ExternalSupervisorCancellationSchemaVersion || !validExternalSupervisorIdentifier(cancellation.HandoffID) || cancellation.AttemptNumber < 1 {
		return ErrExternalSupervisorInvalid
	}
	for _, hash := range []string{cancellation.EnvelopeHash, cancellation.ReceiptIdentityHash, cancellation.CancellationIdentityHash} {
		if !launchHashPattern.MatchString(hash) {
			return ErrExternalSupervisorInvalid
		}
	}
	return nil
}

func validateExternalSupervisorReceiptBinding(receipt ExternalSupervisorAcceptanceReceipt, envelope ExternalSupervisorEnvelope) error {
	if receipt.HandoffID != envelope.HandoffID || receipt.EnvelopeHash != envelope.EnvelopeHash || receipt.AttemptNumber != envelope.AttemptNumber {
		return ErrExternalSupervisorConflict
	}
	return nil
}

func validateExternalSupervisorCallbackBinding(callback ExternalSupervisorCallback, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAcceptanceReceipt, root ExternalSupervisorTrustRoot) error {
	if callback.HandoffID != envelope.HandoffID || callback.EnvelopeHash != envelope.EnvelopeHash ||
		callback.AttemptNumber != envelope.AttemptNumber || callback.ReceiptIdentityHash != receipt.ReceiptIdentityHash ||
		callback.RootID != receipt.RootID || callback.TrustBundleHash != receipt.TrustBundleHash ||
		callback.RootID != root.RootID || callback.TrustBundleHash != root.TrustBundleHash {
		return ErrExternalSupervisorConflict
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

func externalSupervisorReceiptCanonicalValue(receipt ExternalSupervisorAcceptanceReceipt) map[string]any {
	return map[string]any{
		"schema_version": receipt.SchemaVersion, "handoff_id": receipt.HandoffID, "envelope_hash": receipt.EnvelopeHash,
		"receipt_identity_hash": receipt.ReceiptIdentityHash, "attempt_number": receipt.AttemptNumber, "root_id": receipt.RootID,
		"trust_bundle_hash": receipt.TrustBundleHash, "signature_hash": receipt.SignatureHash,
	}
}

func externalSupervisorCallbackCanonicalValue(callback ExternalSupervisorCallback) map[string]any {
	return map[string]any{
		"schema_version": callback.SchemaVersion, "handoff_id": callback.HandoffID, "envelope_hash": callback.EnvelopeHash,
		"receipt_identity_hash": callback.ReceiptIdentityHash, "callback_identity_hash": callback.CallbackIdentityHash,
		"attempt_number": callback.AttemptNumber, "root_id": callback.RootID, "trust_bundle_hash": callback.TrustBundleHash,
		"signature_hash": callback.SignatureHash,
		"result": map[string]any{
			"schema_version": callback.Result.SchemaVersion, "terminal_state": callback.Result.TerminalState,
			"envelope_hash": callback.Result.EnvelopeHash, "receipt_identity_hash": callback.Result.ReceiptIdentityHash,
			"evidence_identity_hash": callback.Result.EvidenceIdentityHash,
		},
	}
}

func externalSupervisorCancellationCanonicalValue(cancellation ExternalSupervisorCancellation) map[string]any {
	return map[string]any{
		"schema_version": cancellation.SchemaVersion, "handoff_id": cancellation.HandoffID,
		"envelope_hash": cancellation.EnvelopeHash, "receipt_identity_hash": cancellation.ReceiptIdentityHash,
		"cancellation_identity_hash": cancellation.CancellationIdentityHash, "attempt_number": cancellation.AttemptNumber,
	}
}

func mustCanonicalExternalSupervisorReceipt(receipt ExternalSupervisorAcceptanceReceipt) string {
	value, err := canonicalJSON(externalSupervisorReceiptCanonicalValue(receipt))
	if err != nil {
		panic("external supervisor receipt must be canonicalizable")
	}
	return string(value)
}

func mustCanonicalExternalSupervisorCallback(callback ExternalSupervisorCallback) string {
	value, err := canonicalJSON(externalSupervisorCallbackCanonicalValue(callback))
	if err != nil {
		panic("external supervisor callback must be canonicalizable")
	}
	return string(value)
}

func mustCanonicalExternalSupervisorCancellation(cancellation ExternalSupervisorCancellation) string {
	value, err := canonicalJSON(externalSupervisorCancellationCanonicalValue(cancellation))
	if err != nil {
		panic("external supervisor cancellation must be canonicalizable")
	}
	return string(value)
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

func loadExternalSupervisorReceipt(ctx context.Context, queryer externalSupervisorQueryer, handoffID string) (ExternalSupervisorAcceptanceReceipt, bool, error) {
	var raw string
	err := queryer.QueryRowContext(ctx, `SELECT receipt_json FROM external_supervisor_receipts WHERE handoff_id = ?`, handoffID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSupervisorAcceptanceReceipt{}, false, nil
	}
	if err != nil {
		return ExternalSupervisorAcceptanceReceipt{}, false, err
	}
	var receipt ExternalSupervisorAcceptanceReceipt
	if err := decodeCanonicalExternalSupervisorValue(raw, &receipt, externalSupervisorReceiptCanonicalValue); err != nil || validateExternalSupervisorReceipt(receipt) != nil {
		return ExternalSupervisorAcceptanceReceipt{}, false, fmt.Errorf("%w: corrupt receipt", ErrExternalSupervisorInvalid)
	}
	return receipt, true, nil
}

func loadExternalSupervisorCallback(ctx context.Context, queryer externalSupervisorQueryer, handoffID string) (ExternalSupervisorCallback, bool, error) {
	var raw string
	err := queryer.QueryRowContext(ctx, `SELECT callback_json FROM external_supervisor_callbacks WHERE handoff_id = ?`, handoffID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSupervisorCallback{}, false, nil
	}
	if err != nil {
		return ExternalSupervisorCallback{}, false, err
	}
	var callback ExternalSupervisorCallback
	if err := decodeCanonicalExternalSupervisorValue(raw, &callback, externalSupervisorCallbackCanonicalValue); err != nil || validateExternalSupervisorCallback(callback) != nil {
		return ExternalSupervisorCallback{}, false, fmt.Errorf("%w: corrupt callback", ErrExternalSupervisorInvalid)
	}
	return callback, true, nil
}

func loadExternalSupervisorCancellation(ctx context.Context, queryer externalSupervisorQueryer, handoffID string) (ExternalSupervisorCancellation, bool, error) {
	var raw string
	err := queryer.QueryRowContext(ctx, `SELECT cancellation_json FROM external_supervisor_cancellations WHERE handoff_id = ?`, handoffID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSupervisorCancellation{}, false, nil
	}
	if err != nil {
		return ExternalSupervisorCancellation{}, false, err
	}
	var cancellation ExternalSupervisorCancellation
	if err := decodeCanonicalExternalSupervisorValue(raw, &cancellation, externalSupervisorCancellationCanonicalValue); err != nil || validateExternalSupervisorCancellation(cancellation) != nil {
		return ExternalSupervisorCancellation{}, false, fmt.Errorf("%w: corrupt cancellation", ErrExternalSupervisorInvalid)
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
