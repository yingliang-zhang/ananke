package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestExternalSupervisorCallbackCancellationConflictAndReplayRemainTransactional(t *testing.T) {
	ctx := context.Background()
	t.Run("callback then cancellation", func(t *testing.T) {
		s, _, claim, envelope := newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(time.Hour))
		envelope, receipt := externalSupervisorAuthenticatedReceiptFixture(t, envelope)
		if _, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence); err != nil {
			t.Fatal(err)
		}
		verifier := externalSupervisorPersistentAuthenticator{trustBundleHash: receipt.Delivery.TrustBundleHash}
		if _, err := s.DeliverAndPersistExternalSupervisorReceipt(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope) (ExternalSupervisorAuthenticatedReceipt, error) { return receipt, nil }); err != nil {
			t.Fatal(err)
		}
		callback := externalSupervisorAuthenticatedCallbackFixture(t, envelope, receipt)
		reconciliations := 0
		accepted, err := s.ReconcileAndPersistExternalSupervisorCallback(ctx, envelope.HandoffID, verifier, func(gotEnvelope ExternalSupervisorEnvelope, gotReceipt ExternalSupervisorAuthenticatedReceipt) (*ExternalSupervisorAuthenticatedCallback, error) {
			reconciliations++
			if gotEnvelope != envelope || gotReceipt != receipt {
				t.Fatal("reconcile did not receive exact durable envelope and receipt")
			}
			return &callback, nil
		})
		if err != nil || accepted == nil || *accepted != callback || reconciliations != 1 {
			t.Fatalf("callback = %+v, %v, calls=%d", accepted, err, reconciliations)
		}
		if replayed, err := s.ReconcileAndPersistExternalSupervisorCallback(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) (*ExternalSupervisorAuthenticatedCallback, error) {
			reconciliations++
			return nil, errors.New("durable callback replay reached transport")
		}); err != nil || replayed == nil || *replayed != callback || reconciliations != 1 {
			t.Fatalf("callback replay = %+v, %v, calls=%d", replayed, err, reconciliations)
		}
		cancellation := externalSupervisorAuthenticatedCancellationFixture(t, envelope, receipt)
		if _, err := s.CancelAndPersistExternalSupervisorCancellation(ctx, cancellation.Cancellation, claim.Fence, verifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt, ExternalSupervisorCancellation) (ExternalSupervisorAuthenticatedCancellation, error) {
			return cancellation, nil
		}); !errors.Is(err, ErrExternalSupervisorConflict) {
			t.Fatalf("cancellation after callback error = %v, want %v", err, ErrExternalSupervisorConflict)
		}
		assertExternalSupervisorTableCount(t, s, "external_supervisor_cancellations", 0)
	})

	t.Run("cancellation then callback and conflicting nonce", func(t *testing.T) {
		s, _, claim, envelope := newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(time.Hour))
		envelope, receipt := externalSupervisorAuthenticatedReceiptFixture(t, envelope)
		if _, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence); err != nil {
			t.Fatal(err)
		}
		verifier := externalSupervisorPersistentAuthenticator{trustBundleHash: receipt.Delivery.TrustBundleHash}
		if _, err := s.DeliverAndPersistExternalSupervisorReceipt(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope) (ExternalSupervisorAuthenticatedReceipt, error) { return receipt, nil }); err != nil {
			t.Fatal(err)
		}
		cancellation := externalSupervisorAuthenticatedCancellationFixture(t, envelope, receipt)
		cancellations := 0
		accepted, err := s.CancelAndPersistExternalSupervisorCancellation(ctx, cancellation.Cancellation, claim.Fence, verifier, func(gotEnvelope ExternalSupervisorEnvelope, gotReceipt ExternalSupervisorAuthenticatedReceipt, gotCancellation ExternalSupervisorCancellation) (ExternalSupervisorAuthenticatedCancellation, error) {
			cancellations++
			if gotEnvelope != envelope || gotReceipt != receipt || gotCancellation != cancellation.Cancellation {
				t.Fatal("cancel did not receive exact durable bindings")
			}
			return cancellation, nil
		})
		if err != nil || accepted != cancellation || cancellations != 1 {
			t.Fatalf("cancellation = %+v, %v, calls=%d", accepted, err, cancellations)
		}
		if replayed, err := s.CancelAndPersistExternalSupervisorCancellation(ctx, cancellation.Cancellation, claim.Fence, verifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt, ExternalSupervisorCancellation) (ExternalSupervisorAuthenticatedCancellation, error) {
			cancellations++
			return ExternalSupervisorAuthenticatedCancellation{}, errors.New("durable cancellation replay reached transport")
		}); err != nil || replayed != cancellation || cancellations != 1 {
			t.Fatalf("cancellation replay = %+v, %v, calls=%d", replayed, err, cancellations)
		}
		callback := externalSupervisorAuthenticatedCallbackFixture(t, envelope, receipt)
		if _, err := s.ReconcileAndPersistExternalSupervisorCallback(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) (*ExternalSupervisorAuthenticatedCallback, error) {
			return &callback, nil
		}); !errors.Is(err, ErrExternalSupervisorConflict) {
			t.Fatalf("callback after cancellation error = %v, want %v", err, ErrExternalSupervisorConflict)
		}
		conflicting := cancellation
		conflicting.Acknowledgement.AcknowledgementID = "cancellation_acknowledgement_conflict"
		conflicting.Acknowledgement = mustSealCancellationAcknowledgement(t, conflicting.Acknowledgement)
		if _, err := s.AcceptExternalSupervisorCancellation(ctx, conflicting, claim.Fence, verifier); !errors.Is(err, ErrExternalSupervisorConflict) {
			t.Fatalf("conflicting cancellation replay error = %v, want %v", err, ErrExternalSupervisorConflict)
		}
	})
}

func TestExternalSupervisorAuthenticatedOutcomesRequireReceiptAndCurrentTrust(t *testing.T) {
	ctx := context.Background()
	s, _, claim, envelope := newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(time.Hour))
	envelope, receipt := externalSupervisorAuthenticatedReceiptFixture(t, envelope)
	if _, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence); err != nil {
		t.Fatalf("stage authenticated handoff: %v", err)
	}
	verifier := externalSupervisorPersistentAuthenticator{trustBundleHash: receipt.Delivery.TrustBundleHash}
	callback := externalSupervisorAuthenticatedCallbackFixture(t, envelope, receipt)
	reconciliations := 0
	if _, err := s.ReconcileAndPersistExternalSupervisorCallback(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) (*ExternalSupervisorAuthenticatedCallback, error) {
		reconciliations++
		return &callback, nil
	}); !errors.Is(err, ErrExternalSupervisorReceiptRequired) {
		t.Fatalf("callback before receipt error = %v, want %v", err, ErrExternalSupervisorReceiptRequired)
	}
	cancellation := externalSupervisorAuthenticatedCancellationFixture(t, envelope, receipt)
	cancellations := 0
	if _, err := s.CancelAndPersistExternalSupervisorCancellation(ctx, cancellation.Cancellation, claim.Fence, verifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt, ExternalSupervisorCancellation) (ExternalSupervisorAuthenticatedCancellation, error) {
		cancellations++
		return cancellation, nil
	}); !errors.Is(err, ErrExternalSupervisorReceiptRequired) {
		t.Fatalf("cancellation before receipt error = %v, want %v", err, ErrExternalSupervisorReceiptRequired)
	}
	if reconciliations != 0 || cancellations != 0 {
		t.Fatalf("receipt-free outcome callbacks reached transport: reconcile=%d cancel=%d", reconciliations, cancellations)
	}

	driftedVerifier := externalSupervisorPersistentAuthenticator{trustBundleHash: externalSupervisorTestHash("drifted-trust-bundle")}
	if _, err := s.DeliverAndPersistExternalSupervisorReceipt(ctx, envelope.HandoffID, driftedVerifier, func(ExternalSupervisorEnvelope) (ExternalSupervisorAuthenticatedReceipt, error) {
		return receipt, nil
	}); !errors.Is(err, ErrExternalSupervisorTrustRoot) {
		t.Fatalf("drifted receipt trust error = %v, want %v", err, ErrExternalSupervisorTrustRoot)
	}
	assertExternalSupervisorTableCount(t, s, "external_supervisor_receipts", 0)
	assertExternalSupervisorTableCount(t, s, "external_supervisor_transport_replay", 0)
	if _, err := s.DeliverAndPersistExternalSupervisorReceipt(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope) (ExternalSupervisorAuthenticatedReceipt, error) {
		return receipt, nil
	}); err != nil {
		t.Fatalf("persist current-trust receipt: %v", err)
	}
	if _, err := s.ReconcileAndPersistExternalSupervisorCallback(ctx, envelope.HandoffID, driftedVerifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) (*ExternalSupervisorAuthenticatedCallback, error) {
		reconciliations++
		return &callback, nil
	}); !errors.Is(err, ErrExternalSupervisorTrustRoot) {
		t.Fatalf("drifted callback trust error = %v, want %v", err, ErrExternalSupervisorTrustRoot)
	}
	if reconciliations != 0 {
		t.Fatalf("drifted callback trust reached transport %d times", reconciliations)
	}
	assertExternalSupervisorTableCount(t, s, "external_supervisor_callbacks", 0)
}

type externalSupervisorPersistentAuthenticator struct{ trustBundleHash string }

func (auth externalSupervisorPersistentAuthenticator) VerifyExternalSupervisorEnvelope(_ context.Context, _ ExternalSupervisorEnvelope) error {
	return nil
}
func (auth externalSupervisorPersistentAuthenticator) VerifyExternalSupervisorReceipt(_ context.Context, _ ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt) error {
	if receipt.Delivery.TrustBundleHash != auth.trustBundleHash {
		return errors.New("trust bundle drift")
	}
	return nil
}
func (auth externalSupervisorPersistentAuthenticator) VerifyExternalSupervisorCallback(_ context.Context, _ ExternalSupervisorEnvelope, _ ExternalSupervisorAuthenticatedReceipt, callback ExternalSupervisorAuthenticatedCallback) error {
	if callback.CallbackAuthentication.SignatureHash == "" {
		return errors.New("missing callback authentication")
	}
	return nil
}
func (auth externalSupervisorPersistentAuthenticator) VerifyExternalSupervisorCancellation(_ context.Context, _ ExternalSupervisorEnvelope, _ ExternalSupervisorAuthenticatedReceipt, cancellation ExternalSupervisorAuthenticatedCancellation) error {
	if cancellation.AcknowledgementAuthentication.SignatureHash == "" {
		return errors.New("missing cancellation authentication")
	}
	return nil
}

func externalSupervisorAuthenticatedReceiptFixture(t *testing.T, envelope ExternalSupervisorEnvelope) (ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) {
	t.Helper()
	now := time.Now().UTC().Add(-time.Minute)
	rootID := "independent_supervisor_release_root_v1"
	signerHash := externalSupervisorTestHash("peer-spki")
	attestation := mustSealReleaseAttestation(t, ExternalSupervisorReleaseAttestation{
		SchemaVersion: ExternalSupervisorReleaseAttestationSchemaVersion, ArtifactSHA256: envelope.SupervisorArtifactSHA256,
		AttestorKeySPKISHA256: externalSupervisorTestHash("attestor-spki"), BuildIdentityHash: envelope.BuildIdentityHash,
		IssuedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(time.Hour).Format(time.RFC3339Nano),
		ReleaseRootID: rootID, RouteMappingHash: envelope.RouteMappingHash,
	})
	approval := mustSealReleaseApproval(t, ExternalSupervisorReleaseApproval{
		SchemaVersion: ExternalSupervisorReleaseApprovalSchemaVersion, ApprovalID: "independent_release_approval_001",
		ApproverKeySPKISHA256: externalSupervisorTestHash("approver-spki"), ApproverRootID: "independent_supervisor_approval_root_v1",
		AttestationHash: attestation.AttestationHash, Decision: "approved", IssuedAt: now.Add(-30 * time.Second).Format(time.RFC3339Nano),
		NotAfter: now.Add(time.Hour).Format(time.RFC3339Nano), RouteMappingHash: envelope.RouteMappingHash,
	})
	grant := mustSealMoAGrant(t, ExternalSupervisorMoARoleGrant{
		SchemaVersion: ExternalSupervisorMoARoleGrantSchemaVersion, GrantID: "moa_remote_supervisor_runner_grant_001",
		GranteeRole: "remote_supervisor_runner", GrantorKeySPKISHA256: externalSupervisorTestHash("grantor-spki"),
		GrantorRootID: "moa_role_grant_root_v1", IssuedAt: now.Format(time.RFC3339Nano), NotAfter: now.Add(time.Hour).Format(time.RFC3339Nano),
		ReleaseApprovalHash: approval.ApprovalHash, ReleaseAttestationHash: attestation.AttestationHash, RouteMappingHash: envelope.RouteMappingHash,
	})
	if envelope.ReleaseAttestationHash == attestation.AttestationHash || envelope.ReleaseApprovalHash == approval.ApprovalHash {
		t.Fatal("later authorization records replaced durable predecessor release identities")
	}
	chain := ExternalSupervisorAuthorizationChain{
		ReleaseAttestation: attestation, ReleaseAttestationSignature: externalSupervisorDummySignature(t, attestation.AttestorKeySPKISHA256, "attestation"),
		ReleaseApproval: approval, ReleaseApprovalSignature: externalSupervisorDummySignature(t, approval.ApproverKeySPKISHA256, "approval"),
		MoARoleGrant: grant, MoARoleGrantSignature: externalSupervisorDummySignature(t, grant.GrantorKeySPKISHA256, "grant"),
	}
	delivery := mustSealDelivery(t, ExternalSupervisorSealedDelivery{
		SchemaVersion: ExternalSupervisorSealedDeliverySchemaVersion, AttemptCap: envelope.AttemptCap, AttemptNumber: envelope.AttemptNumber,
		ChannelBindingHash: externalSupervisorTestHash("delivery-channel"), Deadline: envelope.Deadline,
		DeliveryExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339Nano), DeliveryID: "sealed_delivery_store_001",
		IssuedAt: now.Format(time.RFC3339Nano), MoARoleGrantHash: grant.GrantHash, NonceHash: externalSupervisorTestHash("delivery-nonce"),
		PredecessorEnvelopeHash: envelope.EnvelopeHash, PredecessorIdempotencyKeyHash: envelope.IdempotencyKeyHash,
		ReleaseApprovalHash: approval.ApprovalHash, ReleaseAttestationHash: attestation.AttestationHash,
		RouteMappingHash: envelope.RouteMappingHash, TrustBundleHash: externalSupervisorTestHash("trust-bundle"),
	})
	receipt := mustSealProtocolReceipt(t, ExternalSupervisorProtocolReceipt{
		SchemaVersion: ExternalSupervisorProtocolReceiptSchemaVersion, AttemptNumber: envelope.AttemptNumber,
		ChannelBindingHash: externalSupervisorTestHash("receipt-channel"), DeliveryHash: delivery.DeliveryHash,
		EnvelopeHash: envelope.EnvelopeHash, IssuedAt: now.Add(time.Second).Format(time.RFC3339Nano),
		NonceHash: externalSupervisorTestHash("receipt-nonce"), ReceiptID: "acceptance_receipt_store_001",
		ReleaseApprovalHash: approval.ApprovalHash, RouteMappingHash: envelope.RouteMappingHash,
		SignerKeySPKISHA256: signerHash, TrustRootID: rootID,
	})
	return envelope, ExternalSupervisorAuthenticatedReceipt{
		SchemaVersion: ExternalSupervisorAuthenticatedReceiptSchemaVersion, Authorization: chain, Delivery: delivery,
		DeliveryAuthentication: externalSupervisorDummyMessageAuthentication(t, "delivery", delivery.DeliveryHash, delivery.NonceHash, delivery.ChannelBindingHash, delivery.IssuedAt, signerHash),
		Receipt:                receipt,
		ReceiptAuthentication:  externalSupervisorDummyMessageAuthentication(t, "receipt", receipt.ReceiptHash, receipt.NonceHash, receipt.ChannelBindingHash, receipt.IssuedAt, signerHash),
	}
}

func externalSupervisorAuthenticatedCallbackFixture(t *testing.T, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt) ExternalSupervisorAuthenticatedCallback {
	t.Helper()
	callback := mustSealProtocolCallback(t, ExternalSupervisorProtocolCallback{
		SchemaVersion: ExternalSupervisorProtocolCallbackSchemaVersion, AttemptNumber: envelope.AttemptNumber,
		CallbackChannelBindingHash: externalSupervisorTestHash("callback-channel"), CallbackID: "completion_callback_store_001",
		DeliveryHash: receipt.Delivery.DeliveryHash, EnvelopeHash: envelope.EnvelopeHash, EvidenceHash: externalSupervisorTestHash("evidence"),
		IssuedAt: time.Now().UTC().Format(time.RFC3339Nano), NonceHash: externalSupervisorTestHash("callback-nonce"),
		ReceiptHash: receipt.Receipt.ReceiptHash, ResultSchemaVersion: "ananke.independent-supervisor-result.v1",
		RouteMappingHash: envelope.RouteMappingHash, SignerKeySPKISHA256: receipt.Receipt.SignerKeySPKISHA256,
		TerminalState: "completed", TrustRootID: receipt.Receipt.TrustRootID,
	})
	return ExternalSupervisorAuthenticatedCallback{
		SchemaVersion: ExternalSupervisorAuthenticatedCallbackSchemaVersion, Callback: callback,
		CallbackAuthentication: externalSupervisorDummyMessageAuthentication(t, "callback", callback.CallbackHash, callback.NonceHash, callback.CallbackChannelBindingHash, callback.IssuedAt, callback.SignerKeySPKISHA256),
	}
}

func externalSupervisorAuthenticatedCancellationFixture(t *testing.T, envelope ExternalSupervisorEnvelope, receipt ExternalSupervisorAuthenticatedReceipt) ExternalSupervisorAuthenticatedCancellation {
	t.Helper()
	cancellation, err := SealExternalSupervisorCancellation(ExternalSupervisorCancellation{
		SchemaVersion: ExternalSupervisorCancellationSchemaVersion, HandoffID: envelope.HandoffID,
		EnvelopeHash: envelope.EnvelopeHash, ReceiptIdentityHash: receipt.Receipt.ReceiptHash, AttemptNumber: envelope.AttemptNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	ack := mustSealCancellationAcknowledgement(t, ExternalSupervisorCancellationAcknowledgement{
		SchemaVersion: ExternalSupervisorCancellationAcknowledgementSchemaVersion, AcknowledgementID: "cancellation_acknowledgement_store_001",
		CancellationHash: cancellation.CancellationIdentityHash, DeliveryHash: receipt.Delivery.DeliveryHash,
		EnvelopeHash: envelope.EnvelopeHash, ReceiptHash: receipt.Receipt.ReceiptHash, AttemptNumber: envelope.AttemptNumber,
		ChannelBindingHash: externalSupervisorTestHash("cancellation-channel"), NonceHash: externalSupervisorTestHash("cancellation-nonce"),
		IssuedAt: time.Now().UTC().Format(time.RFC3339Nano), SignerKeySPKISHA256: receipt.Receipt.SignerKeySPKISHA256,
		TrustRootID: receipt.Receipt.TrustRootID,
	})
	return ExternalSupervisorAuthenticatedCancellation{
		SchemaVersion: ExternalSupervisorAuthenticatedCancellationSchemaVersion, Cancellation: cancellation, Acknowledgement: ack,
		AcknowledgementAuthentication: externalSupervisorDummyMessageAuthentication(t, "cancellation", ack.AcknowledgementHash, ack.NonceHash, ack.ChannelBindingHash, ack.IssuedAt, ack.SignerKeySPKISHA256),
	}
}

func externalSupervisorDummySignature(t *testing.T, signer, seed string) ExternalSupervisorDetachedSignature {
	t.Helper()
	signature, err := SealExternalSupervisorDetachedSignature(ExternalSupervisorDetachedSignature{
		SchemaVersion: ExternalSupervisorDetachedSignatureSchemaVersion, Algorithm: "ed25519", SignerKeySPKISHA256: signer,
		Signature: "ed25519:" + fmt.Sprintf("%0128x", seed),
	})
	if err != nil {
		// seed is not numeric; use a deterministic all-zero test-only signature.
		signature, err = SealExternalSupervisorDetachedSignature(ExternalSupervisorDetachedSignature{
			SchemaVersion: ExternalSupervisorDetachedSignatureSchemaVersion, Algorithm: "ed25519", SignerKeySPKISHA256: signer,
			Signature: "ed25519:" + fmt.Sprintf("%0128x", 0),
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

func externalSupervisorDummyMessageAuthentication(t *testing.T, kind, message, nonce, channel, issued, signer string) ExternalSupervisorMessageAuthentication {
	t.Helper()
	value, err := SealExternalSupervisorMessageAuthentication(ExternalSupervisorMessageAuthentication{
		SchemaVersion: ExternalSupervisorMessageAuthenticationSchemaVersion, MessageType: kind, MessageHash: message,
		NonceHash: nonce, ChannelBindingHash: channel, RequestHash: externalSupervisorTestHash(kind + "-request"), IssuedAt: issued,
		SignerKeySPKISHA256: signer, Signature: "ed25519:" + fmt.Sprintf("%0128x", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustSealReleaseAttestation(t *testing.T, value ExternalSupervisorReleaseAttestation) ExternalSupervisorReleaseAttestation {
	t.Helper()
	sealed, err := SealExternalSupervisorReleaseAttestation(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
func mustSealReleaseApproval(t *testing.T, value ExternalSupervisorReleaseApproval) ExternalSupervisorReleaseApproval {
	t.Helper()
	sealed, err := SealExternalSupervisorReleaseApproval(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
func mustSealMoAGrant(t *testing.T, value ExternalSupervisorMoARoleGrant) ExternalSupervisorMoARoleGrant {
	t.Helper()
	sealed, err := SealExternalSupervisorMoARoleGrant(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
func mustSealDelivery(t *testing.T, value ExternalSupervisorSealedDelivery) ExternalSupervisorSealedDelivery {
	t.Helper()
	sealed, err := SealExternalSupervisorSealedDelivery(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
func mustSealProtocolReceipt(t *testing.T, value ExternalSupervisorProtocolReceipt) ExternalSupervisorProtocolReceipt {
	t.Helper()
	sealed, err := SealExternalSupervisorProtocolReceipt(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
func mustSealProtocolCallback(t *testing.T, value ExternalSupervisorProtocolCallback) ExternalSupervisorProtocolCallback {
	t.Helper()
	sealed, err := SealExternalSupervisorProtocolCallback(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
func mustSealCancellationAcknowledgement(t *testing.T, value ExternalSupervisorCancellationAcknowledgement) ExternalSupervisorCancellationAcknowledgement {
	t.Helper()
	sealed, err := SealExternalSupervisorCancellationAcknowledgement(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
