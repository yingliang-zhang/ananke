package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DeliverAndPersistExternalSupervisorReceipt holds the full-fence transaction
// through delivery, mandatory authentication, replay admission, and persistence.
// An exact durable receipt is returned without contacting the transport again.
func (s *Store) DeliverAndPersistExternalSupervisorReceipt(ctx context.Context, handoffID string, authenticator ExternalSupervisorAuthenticator, deliver func(ExternalSupervisorEnvelope) (ExternalSupervisorAuthenticatedReceipt, error)) (ExternalSupervisorAuthenticatedReceipt, error) {
	if authenticator == nil || deliver == nil {
		return ExternalSupervisorAuthenticatedReceipt{}, ErrExternalSupervisorTrustRoot
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, err := loadAuthenticatedHandoffAdmission(ctx, tx, handoffID)
	if err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, err
	}
	if authenticator.VerifyExternalSupervisorEnvelope(ctx, handoff.Envelope) != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, ErrExternalSupervisorTrustRoot
	}
	if existing, found, err := loadExternalSupervisorReceipt(ctx, tx, handoffID); err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, err
	} else if found {
		if authenticator.VerifyExternalSupervisorReceipt(ctx, handoff.Envelope, existing) != nil {
			return ExternalSupervisorAuthenticatedReceipt{}, ErrExternalSupervisorTrustRoot
		}
		return existing, nil
	}
	receipt, err := deliver(handoff.Envelope)
	if err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, err
	}
	if err := validateAuthenticatedExternalSupervisorReceipt(receipt, handoff.Envelope); err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, err
	}
	if authenticator.VerifyExternalSupervisorReceipt(ctx, handoff.Envelope, receipt) != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, ErrExternalSupervisorTrustRoot
	}
	if err := persistExternalSupervisorReceipt(ctx, tx, handoff.Envelope, receipt); err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExternalSupervisorAuthenticatedReceipt{}, err
	}
	return receipt, nil
}

// ReconcileAndPersistExternalSupervisorCallback binds reconciliation to the
// exact durable envelope, authorization chain, delivery, and receipt.
func (s *Store) ReconcileAndPersistExternalSupervisorCallback(ctx context.Context, handoffID string, authenticator ExternalSupervisorAuthenticator, reconcile func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) (*ExternalSupervisorAuthenticatedCallback, error)) (*ExternalSupervisorAuthenticatedCallback, error) {
	if authenticator == nil || reconcile == nil {
		return nil, ErrExternalSupervisorTrustRoot
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, err := loadAuthenticatedHandoffAdmission(ctx, tx, handoffID)
	if err != nil {
		return nil, err
	}
	if authenticator.VerifyExternalSupervisorEnvelope(ctx, handoff.Envelope) != nil {
		return nil, ErrExternalSupervisorTrustRoot
	}
	receipt, found, err := loadExternalSupervisorReceipt(ctx, tx, handoffID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrExternalSupervisorReceiptRequired
	}
	if authenticator.VerifyExternalSupervisorReceipt(ctx, handoff.Envelope, receipt) != nil {
		return nil, ErrExternalSupervisorTrustRoot
	}
	if _, found, err := loadExternalSupervisorCancellation(ctx, tx, handoffID); err != nil {
		return nil, err
	} else if found {
		return nil, ErrExternalSupervisorConflict
	}
	if existing, found, err := loadExternalSupervisorCallback(ctx, tx, handoffID); err != nil {
		return nil, err
	} else if found {
		if authenticator.VerifyExternalSupervisorCallback(ctx, handoff.Envelope, receipt, existing) != nil {
			return nil, ErrExternalSupervisorTrustRoot
		}
		return &existing, nil
	}
	callback, err := reconcile(handoff.Envelope, receipt)
	if err != nil || callback == nil {
		return callback, err
	}
	if err := validateAuthenticatedExternalSupervisorCallback(*callback, handoff.Envelope, receipt); err != nil {
		return nil, err
	}
	if authenticator.VerifyExternalSupervisorCallback(ctx, handoff.Envelope, receipt, *callback) != nil {
		return nil, ErrExternalSupervisorTrustRoot
	}
	if err := persistExternalSupervisorCallback(ctx, tx, handoff.Envelope, receipt, *callback); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return callback, nil
}

// CancelAndPersistExternalSupervisorCancellation holds the full-fence
// transaction through the signed acknowledgement and persists it atomically
// with its replay evidence. Callback and cancellation facts are exclusive.
func (s *Store) CancelAndPersistExternalSupervisorCancellation(ctx context.Context, cancellation ExternalSupervisorCancellation, fence LaunchFence, authenticator ExternalSupervisorAuthenticator, cancel func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt, ExternalSupervisorCancellation) (ExternalSupervisorAuthenticatedCancellation, error)) (ExternalSupervisorAuthenticatedCancellation, error) {
	if authenticator == nil || cancel == nil {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorTrustRoot
	}
	sealed, err := SealExternalSupervisorCancellation(cancellation)
	if err != nil || sealed != cancellation {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, err := loadAuthenticatedHandoffAdmissionWithFence(ctx, tx, cancellation.HandoffID, fence)
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if authenticator.VerifyExternalSupervisorEnvelope(ctx, handoff.Envelope) != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorTrustRoot
	}
	receipt, found, err := loadExternalSupervisorReceipt(ctx, tx, cancellation.HandoffID)
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if !found {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorReceiptRequired
	}
	if authenticator.VerifyExternalSupervisorReceipt(ctx, handoff.Envelope, receipt) != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorTrustRoot
	}
	if cancellation.EnvelopeHash != handoff.Envelope.EnvelopeHash || cancellation.ReceiptIdentityHash != receipt.Receipt.ReceiptHash || cancellation.AttemptNumber != handoff.Envelope.AttemptNumber {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorConflict
	}
	if _, found, err := loadExternalSupervisorCallback(ctx, tx, cancellation.HandoffID); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	} else if found {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorConflict
	}
	if existing, found, err := loadExternalSupervisorCancellation(ctx, tx, cancellation.HandoffID); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	} else if found {
		if existing.Cancellation != cancellation {
			return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorConflict
		}
		if authenticator.VerifyExternalSupervisorCancellation(ctx, handoff.Envelope, receipt, existing) != nil {
			return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorTrustRoot
		}
		return existing, nil
	}
	authenticated, err := cancel(handoff.Envelope, receipt, cancellation)
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if err := validateAuthenticatedExternalSupervisorCancellation(authenticated, handoff.Envelope, receipt); err != nil || authenticated.Cancellation != cancellation {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorConflict
	}
	if authenticator.VerifyExternalSupervisorCancellation(ctx, handoff.Envelope, receipt, authenticated) != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorTrustRoot
	}
	if err := persistExternalSupervisorCancellation(ctx, tx, handoff.Envelope, receipt, authenticated); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	return authenticated, nil
}

// AcceptExternalSupervisorCancellation is a secure persistence entry point for
// an already authenticated acknowledgement, used by recovery/import paths.
func (s *Store) AcceptExternalSupervisorCancellation(ctx context.Context, authenticated ExternalSupervisorAuthenticatedCancellation, fence LaunchFence, authenticator ExternalSupervisorAuthenticator) (ExternalSupervisorAuthenticatedCancellation, error) {
	if authenticator == nil {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorTrustRoot
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	handoff, err := loadAuthenticatedHandoffAdmissionWithFence(ctx, tx, authenticated.Cancellation.HandoffID, fence)
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	receipt, found, err := loadExternalSupervisorReceipt(ctx, tx, authenticated.Cancellation.HandoffID)
	if err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if !found {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorReceiptRequired
	}
	if existing, found, err := loadExternalSupervisorCancellation(ctx, tx, authenticated.Cancellation.HandoffID); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	} else if found {
		if existing != authenticated {
			return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorConflict
		}
		return existing, nil
	}
	if err := validateAuthenticatedExternalSupervisorCancellation(authenticated, handoff.Envelope, receipt); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if authenticator.VerifyExternalSupervisorCancellation(ctx, handoff.Envelope, receipt, authenticated) != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, ErrExternalSupervisorTrustRoot
	}
	if err := persistExternalSupervisorCancellation(ctx, tx, handoff.Envelope, receipt, authenticated); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExternalSupervisorAuthenticatedCancellation{}, err
	}
	return authenticated, nil
}

func loadAuthenticatedHandoffAdmission(ctx context.Context, tx *sql.Tx, handoffID string) (ExternalSupervisorHandoff, error) {
	handoff, found, err := loadExternalSupervisorHandoff(ctx, tx, handoffID)
	if err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	if !found {
		return ExternalSupervisorHandoff{}, ErrExternalSupervisorNotFound
	}
	boundary, err := loadLaunchRecoveryBoundary(ctx, tx, handoff.LaunchSpecHash)
	if err != nil {
		return ExternalSupervisorHandoff{}, fmt.Errorf("%w: %v", ErrExternalSupervisorFence, err)
	}
	if err := validateExternalSupervisorAdmission(ctx, tx, handoff.Envelope, boundary.Claim.Fence, time.Now().UTC()); err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	return handoff, nil
}

func loadAuthenticatedHandoffAdmissionWithFence(ctx context.Context, tx *sql.Tx, handoffID string, fence LaunchFence) (ExternalSupervisorHandoff, error) {
	handoff, err := loadAuthenticatedHandoffAdmission(ctx, tx, handoffID)
	if err != nil {
		return ExternalSupervisorHandoff{}, err
	}
	boundary, err := loadLaunchRecoveryBoundary(ctx, tx, handoff.LaunchSpecHash)
	if err != nil || boundary.Claim.Fence != fence || HashExternalSupervisorFenceBinding(fence) != handoff.Envelope.FenceBindingHash {
		return ExternalSupervisorHandoff{}, ErrExternalSupervisorFence
	}
	return handoff, nil
}

func persistExternalSupervisorReceipt(ctx context.Context, tx *sql.Tx, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt) error {
	fingerprint, err := canonicalExternalSupervisorProtocolHash(receipt)
	if err != nil {
		return err
	}
	if err := rememberExternalSupervisorReplay(ctx, tx, "delivery", receipt.Delivery.DeliveryID, receipt.Delivery.NonceHash, envelope.EnvelopeHash, receipt.Delivery.ChannelBindingHash, receipt.DeliveryAuthentication.SignatureHash, receipt.Delivery.DeliveryHash); err != nil {
		return err
	}
	if err := rememberExternalSupervisorReplay(ctx, tx, "receipt", receipt.Receipt.ReceiptID, receipt.Receipt.NonceHash, receipt.Delivery.DeliveryHash, receipt.Receipt.ChannelBindingHash, receipt.ReceiptAuthentication.SignatureHash, receipt.Receipt.ReceiptHash); err != nil {
		return err
	}
	raw, err := canonicalExternalSupervisorProtocolJSON(receipt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO external_supervisor_receipts
		(receipt_identity_hash, handoff_id, envelope_hash, attempt_number, root_id, trust_bundle_hash, receipt_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, receipt.Receipt.ReceiptHash, envelope.HandoffID, envelope.EnvelopeHash,
		receipt.Receipt.AttemptNumber, receipt.Receipt.TrustRootID, receipt.Delivery.TrustBundleHash, raw, nowStamp())
	if err != nil {
		return fmt.Errorf("insert external supervisor receipt %s: %w", fingerprint, err)
	}
	return nil
}

func persistExternalSupervisorCallback(ctx context.Context, tx *sql.Tx, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt, callback ExternalSupervisorAuthenticatedCallback) error {
	if err := rememberExternalSupervisorReplay(ctx, tx, "callback", callback.Callback.CallbackID, callback.Callback.NonceHash, receipt.Receipt.ReceiptHash, callback.Callback.CallbackChannelBindingHash, callback.CallbackAuthentication.SignatureHash, callback.Callback.CallbackHash); err != nil {
		return err
	}
	raw, err := canonicalExternalSupervisorProtocolJSON(callback)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO external_supervisor_callbacks
		(callback_identity_hash, handoff_id, envelope_hash, receipt_identity_hash, attempt_number, root_id, trust_bundle_hash, callback_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, callback.Callback.CallbackHash, envelope.HandoffID, envelope.EnvelopeHash,
		receipt.Receipt.ReceiptHash, callback.Callback.AttemptNumber, callback.Callback.TrustRootID,
		receipt.Delivery.TrustBundleHash, raw, nowStamp())
	if err != nil {
		return fmt.Errorf("insert external supervisor callback: %w", err)
	}
	return nil
}

func persistExternalSupervisorCancellation(ctx context.Context, tx *sql.Tx, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt, cancellation ExternalSupervisorAuthenticatedCancellation) error {
	ack := cancellation.Acknowledgement
	if err := rememberExternalSupervisorReplay(ctx, tx, "cancellation", ack.AcknowledgementID, ack.NonceHash, cancellation.Cancellation.CancellationIdentityHash, ack.ChannelBindingHash, cancellation.AcknowledgementAuthentication.SignatureHash, ack.AcknowledgementHash); err != nil {
		return err
	}
	raw, err := canonicalExternalSupervisorProtocolJSON(cancellation)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO external_supervisor_cancellations
		(cancellation_identity_hash, handoff_id, envelope_hash, receipt_identity_hash, attempt_number, cancellation_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, cancellation.Cancellation.CancellationIdentityHash, envelope.HandoffID,
		envelope.EnvelopeHash, receipt.Receipt.ReceiptHash, envelope.AttemptNumber, raw, nowStamp())
	if err != nil {
		return fmt.Errorf("insert external supervisor cancellation: %w", err)
	}
	return nil
}

func rememberExternalSupervisorReplay(ctx context.Context, tx *sql.Tx, messageType, recordID, nonceHash, priorHash, channelHash, signatureHash, fingerprint string) error {
	var existingNonce, existingPrior, existingChannel, existingSignature, existingFingerprint string
	err := tx.QueryRowContext(ctx, `SELECT nonce_hash, prior_hash, channel_binding_hash, signature_hash, record_fingerprint
		FROM external_supervisor_transport_replay WHERE message_type = ? AND record_id = ?`, messageType, recordID).
		Scan(&existingNonce, &existingPrior, &existingChannel, &existingSignature, &existingFingerprint)
	if err == nil {
		if existingNonce == nonceHash && existingPrior == priorHash && existingChannel == channelHash && existingSignature == signatureHash && existingFingerprint == fingerprint {
			return nil
		}
		return ErrExternalSupervisorConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var existingRecord string
	err = tx.QueryRowContext(ctx, `SELECT record_id FROM external_supervisor_transport_replay WHERE message_type = ? AND nonce_hash = ?`, messageType, nonceHash).Scan(&existingRecord)
	if err == nil {
		return ErrExternalSupervisorConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO external_supervisor_transport_replay
		(message_type, record_id, nonce_hash, prior_hash, channel_binding_hash, signature_hash, record_fingerprint, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, messageType, recordID, nonceHash, priorHash, channelHash, signatureHash, fingerprint, nowStamp()); err != nil {
		return ErrExternalSupervisorConflict
	}
	return nil
}

func validateAuthenticatedExternalSupervisorReceipt(value ExternalSupervisorAuthenticatedReceipt, envelope ExternalSupervisorEnvelope) error {
	if value.SchemaVersion != ExternalSupervisorAuthenticatedReceiptSchemaVersion {
		return ErrExternalSupervisorInvalid
	}
	attestation, approval, grant := value.Authorization.ReleaseAttestation, value.Authorization.ReleaseApproval, value.Authorization.MoARoleGrant
	if sealed, err := SealExternalSupervisorReleaseAttestation(attestation); err != nil || sealed != attestation {
		return ErrExternalSupervisorInvalid
	}
	if sealed, err := SealExternalSupervisorReleaseApproval(approval); err != nil || sealed != approval {
		return ErrExternalSupervisorInvalid
	}
	if sealed, err := SealExternalSupervisorMoARoleGrant(grant); err != nil || sealed != grant {
		return ErrExternalSupervisorInvalid
	}
	for _, signature := range []ExternalSupervisorDetachedSignature{value.Authorization.ReleaseAttestationSignature, value.Authorization.ReleaseApprovalSignature, value.Authorization.MoARoleGrantSignature} {
		if sealed, err := SealExternalSupervisorDetachedSignature(signature); err != nil || sealed != signature {
			return ErrExternalSupervisorInvalid
		}
	}
	delivery, receipt := value.Delivery, value.Receipt
	if sealed, err := SealExternalSupervisorSealedDelivery(delivery); err != nil || sealed != delivery {
		return ErrExternalSupervisorInvalid
	}
	if sealed, err := SealExternalSupervisorProtocolReceipt(receipt); err != nil || sealed != receipt {
		return ErrExternalSupervisorInvalid
	}
	if err := validateExternalSupervisorMessageAuthentication(value.DeliveryAuthentication, "delivery", delivery.DeliveryHash, delivery.NonceHash, delivery.ChannelBindingHash, delivery.IssuedAt); err != nil {
		return err
	}
	if err := validateExternalSupervisorMessageAuthentication(value.ReceiptAuthentication, "receipt", receipt.ReceiptHash, receipt.NonceHash, receipt.ChannelBindingHash, receipt.IssuedAt); err != nil {
		return err
	}
	if delivery.NonceHash == receipt.NonceHash || delivery.ChannelBindingHash == receipt.ChannelBindingHash ||
		delivery.PredecessorEnvelopeHash != envelope.EnvelopeHash || delivery.PredecessorIdempotencyKeyHash != envelope.IdempotencyKeyHash ||
		delivery.AttemptNumber != envelope.AttemptNumber || delivery.AttemptCap != envelope.AttemptCap || delivery.Deadline != envelope.Deadline ||
		delivery.RouteMappingHash != envelope.RouteMappingHash || delivery.ReleaseAttestationHash != attestation.AttestationHash ||
		delivery.ReleaseApprovalHash != approval.ApprovalHash || delivery.MoARoleGrantHash != grant.GrantHash ||
		attestation.AttestationHash == envelope.ReleaseAttestationHash || attestation.ArtifactSHA256 != envelope.SupervisorArtifactSHA256 ||
		attestation.BuildIdentityHash != envelope.BuildIdentityHash || attestation.RouteMappingHash != envelope.RouteMappingHash ||
		approval.ApprovalHash == envelope.ReleaseApprovalHash || approval.AttestationHash != attestation.AttestationHash || approval.RouteMappingHash != envelope.RouteMappingHash ||
		grant.ReleaseAttestationHash != attestation.AttestationHash || grant.ReleaseApprovalHash != approval.ApprovalHash || grant.RouteMappingHash != envelope.RouteMappingHash ||
		receipt.DeliveryHash != delivery.DeliveryHash || receipt.EnvelopeHash != envelope.EnvelopeHash || receipt.AttemptNumber != envelope.AttemptNumber ||
		receipt.ReleaseApprovalHash != approval.ApprovalHash || receipt.RouteMappingHash != envelope.RouteMappingHash {
		return ErrExternalSupervisorConflict
	}
	deliveryAt, deliveryErr := time.Parse(time.RFC3339Nano, delivery.IssuedAt)
	receiptAt, receiptErr := time.Parse(time.RFC3339Nano, receipt.IssuedAt)
	expiresAt, expiryErr := time.Parse(time.RFC3339Nano, delivery.DeliveryExpiresAt)
	if deliveryErr != nil || receiptErr != nil || expiryErr != nil || receiptAt.Before(deliveryAt) || !receiptAt.Before(expiresAt) {
		return ErrExternalSupervisorInvalid
	}
	return nil
}

func validateAuthenticatedExternalSupervisorCallback(value ExternalSupervisorAuthenticatedCallback, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt) error {
	callback := value.Callback
	if value.SchemaVersion != ExternalSupervisorAuthenticatedCallbackSchemaVersion {
		return ErrExternalSupervisorInvalid
	}
	if sealed, err := SealExternalSupervisorProtocolCallback(callback); err != nil || sealed != callback {
		return ErrExternalSupervisorInvalid
	}
	if err := validateExternalSupervisorMessageAuthentication(value.CallbackAuthentication, "callback", callback.CallbackHash, callback.NonceHash, callback.CallbackChannelBindingHash, callback.IssuedAt); err != nil {
		return err
	}
	if callback.NonceHash == receipt.Receipt.NonceHash || callback.CallbackChannelBindingHash == receipt.Receipt.ChannelBindingHash ||
		callback.DeliveryHash != receipt.Delivery.DeliveryHash || callback.EnvelopeHash != envelope.EnvelopeHash || callback.ReceiptHash != receipt.Receipt.ReceiptHash ||
		callback.RouteMappingHash != envelope.RouteMappingHash || callback.AttemptNumber != envelope.AttemptNumber {
		return ErrExternalSupervisorConflict
	}
	receiptAt, receiptErr := time.Parse(time.RFC3339Nano, receipt.Receipt.IssuedAt)
	callbackAt, callbackErr := time.Parse(time.RFC3339Nano, callback.IssuedAt)
	if receiptErr != nil || callbackErr != nil || callbackAt.Before(receiptAt) {
		return ErrExternalSupervisorInvalid
	}
	return nil
}

func validateAuthenticatedExternalSupervisorCancellation(value ExternalSupervisorAuthenticatedCancellation, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt) error {
	if value.SchemaVersion != ExternalSupervisorAuthenticatedCancellationSchemaVersion {
		return ErrExternalSupervisorInvalid
	}
	if sealed, err := SealExternalSupervisorCancellation(value.Cancellation); err != nil || sealed != value.Cancellation {
		return ErrExternalSupervisorInvalid
	}
	ack := value.Acknowledgement
	if sealed, err := SealExternalSupervisorCancellationAcknowledgement(ack); err != nil || sealed != ack {
		return ErrExternalSupervisorInvalid
	}
	if err := validateExternalSupervisorMessageAuthentication(value.AcknowledgementAuthentication, "cancellation", ack.AcknowledgementHash, ack.NonceHash, ack.ChannelBindingHash, ack.IssuedAt); err != nil {
		return err
	}
	if value.Cancellation.HandoffID != envelope.HandoffID || value.Cancellation.EnvelopeHash != envelope.EnvelopeHash ||
		value.Cancellation.ReceiptIdentityHash != receipt.Receipt.ReceiptHash || value.Cancellation.AttemptNumber != envelope.AttemptNumber ||
		ack.CancellationHash != value.Cancellation.CancellationIdentityHash || ack.DeliveryHash != receipt.Delivery.DeliveryHash ||
		ack.EnvelopeHash != envelope.EnvelopeHash || ack.ReceiptHash != receipt.Receipt.ReceiptHash || ack.AttemptNumber != envelope.AttemptNumber {
		return ErrExternalSupervisorConflict
	}
	return nil
}

func validateExternalSupervisorMessageAuthentication(value ExternalSupervisorMessageAuthentication, messageType, messageHash, nonceHash, channelHash, issuedAt string) error {
	if sealed, err := SealExternalSupervisorMessageAuthentication(value); err != nil || sealed != value || value.MessageType != messageType ||
		value.MessageHash != messageHash || value.NonceHash != nonceHash || value.ChannelBindingHash != channelHash || value.IssuedAt != issuedAt {
		return ErrExternalSupervisorInvalid
	}
	return nil
}

func canonicalExternalSupervisorProtocolJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(normalized)
	return string(canonical), err
}

func canonicalExternalSupervisorProtocolHash(value any) (string, error) {
	raw, err := canonicalExternalSupervisorProtocolJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func decodeCanonicalExternalSupervisorProtocol(raw string, target any) error {
	if err := jsonUnmarshalStrict([]byte(raw), target); err != nil {
		return err
	}
	canonical, err := canonicalExternalSupervisorProtocolJSON(target)
	if err != nil || canonical != raw {
		return ErrExternalSupervisorInvalid
	}
	return nil
}

func migrateV14(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE external_supervisor_transport_replay (
			message_type TEXT NOT NULL CHECK (message_type IN ('delivery', 'receipt', 'callback', 'cancellation')),
			record_id TEXT NOT NULL,
			nonce_hash TEXT NOT NULL,
			prior_hash TEXT NOT NULL,
			channel_binding_hash TEXT NOT NULL,
			signature_hash TEXT NOT NULL,
			record_fingerprint TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (message_type, record_id),
			UNIQUE (message_type, nonce_hash)
		)`,
		`CREATE TRIGGER external_supervisor_transport_replay_insert_only_update
			BEFORE UPDATE ON external_supervisor_transport_replay
			BEGIN SELECT RAISE(ABORT, 'external supervisor transport replay evidence is immutable'); END`,
		`CREATE TRIGGER external_supervisor_transport_replay_insert_only_delete
			BEFORE DELETE ON external_supervisor_transport_replay
			BEGIN SELECT RAISE(ABORT, 'external supervisor transport replay evidence is immutable'); END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
