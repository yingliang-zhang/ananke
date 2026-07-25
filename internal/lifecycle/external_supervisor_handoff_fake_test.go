package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// p3fInProcessFakeSupervisor is test-only. It models the authenticated seam
// without a listener, process, key, source, artifact, evidence, or OMP access.
type p3fInProcessFakeSupervisor struct {
	mu                       sync.Mutex
	receipts                 map[string]store.ExternalSupervisorAuthenticatedReceipt
	callbacks                map[string]store.ExternalSupervisorAuthenticatedCallback
	deliveryCount            int
	deliveryAttemptCount     int
	withheldDeliveryResponse bool
	reconcileCount           int
	cancelCount              int
}

func newP3FInProcessFakeSupervisor() *p3fInProcessFakeSupervisor {
	return &p3fInProcessFakeSupervisor{
		receipts:  make(map[string]store.ExternalSupervisorAuthenticatedReceipt),
		callbacks: make(map[string]store.ExternalSupervisorAuthenticatedCallback),
	}
}

func (fake *p3fInProcessFakeSupervisor) Deliver(_ context.Context, envelope store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAuthenticatedReceipt, error) {
	if store.ValidateExternalSupervisorEnvelope(envelope) != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, errors.New("fake supervisor requires sealed envelope")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.deliveryAttemptCount++
	if existing, found := fake.receipts[envelope.HandoffID]; found {
		return existing, nil
	}
	if fake.withheldDeliveryResponse {
		return store.ExternalSupervisorAuthenticatedReceipt{}, errors.New("fake supervisor withheld delivery response")
	}
	receipt := p3fTestAuthenticatedReceipt(envelope, fake.deliveryAttemptCount)
	fake.receipts[envelope.HandoffID] = receipt
	fake.deliveryCount++
	return receipt, nil
}

func (fake *p3fInProcessFakeSupervisor) Reconcile(_ context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) (*store.ExternalSupervisorAuthenticatedCallback, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	known, found := fake.receipts[envelope.HandoffID]
	if !found || known != receipt {
		return nil, errors.New("fake supervisor reconciliation binding")
	}
	fake.reconcileCount++
	callback, found := fake.callbacks[envelope.HandoffID]
	if !found {
		return nil, nil
	}
	copy := callback
	return &copy, nil
}

func (fake *p3fInProcessFakeSupervisor) Cancel(_ context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, cancellation store.ExternalSupervisorCancellation) (store.ExternalSupervisorAuthenticatedCancellation, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	known, found := fake.receipts[envelope.HandoffID]
	if !found || known != receipt || cancellation.ReceiptIdentityHash != receipt.Receipt.ReceiptHash {
		return store.ExternalSupervisorAuthenticatedCancellation{}, errors.New("fake supervisor cancellation binding")
	}
	fake.cancelCount++
	return p3fTestAuthenticatedCancellation(envelope, receipt, cancellation), nil
}

func (fake *p3fInProcessFakeSupervisor) VerifyExternalSupervisorEnvelope(_ context.Context, envelope store.ExternalSupervisorEnvelope) error {
	chain := p3fTestAuthorization(envelope)
	if chain.ReleaseAttestation.ArtifactSHA256 != envelope.SupervisorArtifactSHA256 ||
		chain.ReleaseAttestation.BuildIdentityHash != envelope.BuildIdentityHash ||
		chain.ReleaseAttestation.RouteMappingHash != envelope.RouteMappingHash ||
		chain.ReleaseApproval.AttestationHash != chain.ReleaseAttestation.AttestationHash ||
		chain.MoARoleGrant.ReleaseAttestationHash != chain.ReleaseAttestation.AttestationHash ||
		chain.MoARoleGrant.ReleaseApprovalHash != chain.ReleaseApproval.ApprovalHash ||
		envelope.ReleaseAttestationHash == chain.ReleaseAttestation.AttestationHash ||
		envelope.ReleaseApprovalHash == chain.ReleaseApproval.ApprovalHash {
		return errors.New("fake envelope authentication failed")
	}
	return nil
}

func (fake *p3fInProcessFakeSupervisor) VerifyExternalSupervisorReceipt(_ context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	known, found := fake.receipts[envelope.HandoffID]
	if !found || known != receipt {
		return errors.New("fake receipt authentication failed")
	}
	return nil
}

func (fake *p3fInProcessFakeSupervisor) VerifyExternalSupervisorCallback(_ context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, callback store.ExternalSupervisorAuthenticatedCallback) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	knownReceipt, receiptFound := fake.receipts[envelope.HandoffID]
	knownCallback, callbackFound := fake.callbacks[envelope.HandoffID]
	if !receiptFound || !callbackFound || knownReceipt != receipt || knownCallback != callback ||
		callback.Callback.EnvelopeHash != envelope.EnvelopeHash || callback.Callback.ReceiptHash != receipt.Receipt.ReceiptHash ||
		callback.Callback.DeliveryHash != receipt.Delivery.DeliveryHash || callback.Callback.AttemptNumber != envelope.AttemptNumber ||
		callback.Callback.RouteMappingHash != envelope.RouteMappingHash || callback.Callback.TrustRootID != receipt.Receipt.TrustRootID ||
		callback.Callback.SignerKeySPKISHA256 != receipt.Receipt.SignerKeySPKISHA256 ||
		callback.CallbackAuthentication.SignerKeySPKISHA256 != receipt.Receipt.SignerKeySPKISHA256 {
		return errors.New("fake callback authentication failed")
	}
	return nil
}

func (fake *p3fInProcessFakeSupervisor) VerifyExternalSupervisorCancellation(_ context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, cancellation store.ExternalSupervisorAuthenticatedCancellation) error {
	if cancellation.Cancellation.HandoffID != envelope.HandoffID || cancellation.Cancellation.ReceiptIdentityHash != receipt.Receipt.ReceiptHash {
		return errors.New("fake cancellation authentication failed")
	}
	return nil
}

func (fake *p3fInProcessFakeSupervisor) publishCallback(handoffID, terminalState string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	receipt := fake.receipts[handoffID]
	fake.callbacks[handoffID] = p3fTestAuthenticatedCallback(receipt, terminalState)
}

func (fake *p3fInProcessFakeSupervisor) callbackFor(handoffID string) (store.ExternalSupervisorAuthenticatedCallback, bool) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	callback, found := fake.callbacks[handoffID]
	return callback, found
}

func (fake *p3fInProcessFakeSupervisor) replaceCallback(callback store.ExternalSupervisorAuthenticatedCallback) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for handoffID, receipt := range fake.receipts {
		if receipt.Receipt.EnvelopeHash == callback.Callback.EnvelopeHash || receipt.Receipt.ReceiptHash == callback.Callback.ReceiptHash {
			fake.callbacks[handoffID] = callback
			return
		}
	}
}

func (fake *p3fInProcessFakeSupervisor) withholdDeliveryResponse() {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.withheldDeliveryResponse = true
}

func (fake *p3fInProcessFakeSupervisor) releaseDeliveryResponse() {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.withheldDeliveryResponse = false
}

func (fake *p3fInProcessFakeSupervisor) receiptFor(handoffID string) (store.ExternalSupervisorAuthenticatedReceipt, bool) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	receipt, found := fake.receipts[handoffID]
	return receipt, found
}

func (fake *p3fInProcessFakeSupervisor) deliveries() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.deliveryCount
}

func (fake *p3fInProcessFakeSupervisor) deliveryAttempts() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.deliveryAttemptCount
}

func (fake *p3fInProcessFakeSupervisor) reconciliations() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.reconcileCount
}

func (fake *p3fInProcessFakeSupervisor) cancellations() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.cancelCount
}

func p3fTestAuthorization(envelope store.ExternalSupervisorEnvelope) store.ExternalSupervisorAuthorizationChain {
	attestation := mustP3FSealReleaseAttestation(store.ExternalSupervisorReleaseAttestation{
		SchemaVersion:  store.ExternalSupervisorReleaseAttestationSchemaVersion,
		ArtifactSHA256: envelope.SupervisorArtifactSHA256, AttestorKeySPKISHA256: p3fExternalSupervisorHash("attestor-spki"),
		BuildIdentityHash: envelope.BuildIdentityHash, IssuedAt: "2026-07-25T00:00:00Z", NotAfter: envelope.Deadline,
		ReleaseRootID: "independent_supervisor_release_root_v1", RouteMappingHash: envelope.RouteMappingHash,
	})
	approval := mustP3FSealReleaseApproval(store.ExternalSupervisorReleaseApproval{
		SchemaVersion: store.ExternalSupervisorReleaseApprovalSchemaVersion, ApprovalID: "independent_release_approval_p3f_001",
		ApproverKeySPKISHA256: p3fExternalSupervisorHash("approver-spki"), ApproverRootID: "independent_supervisor_approval_root_v1",
		AttestationHash: attestation.AttestationHash, Decision: "approved", IssuedAt: "2026-07-25T00:01:00Z",
		NotAfter: envelope.Deadline, RouteMappingHash: envelope.RouteMappingHash,
	})
	grant := mustP3FSealMoAGrant(store.ExternalSupervisorMoARoleGrant{
		SchemaVersion: store.ExternalSupervisorMoARoleGrantSchemaVersion, GrantID: "moa_remote_supervisor_runner_grant_p3f_001",
		GranteeRole: "remote_supervisor_runner", GrantorKeySPKISHA256: p3fExternalSupervisorHash("grantor-spki"),
		GrantorRootID: "moa_role_grant_root_v1", IssuedAt: "2026-07-25T00:02:00Z", NotAfter: envelope.Deadline,
		ReleaseApprovalHash: approval.ApprovalHash, ReleaseAttestationHash: attestation.AttestationHash, RouteMappingHash: envelope.RouteMappingHash,
	})
	return store.ExternalSupervisorAuthorizationChain{
		ReleaseAttestation:          attestation,
		ReleaseAttestationSignature: p3fDummyDetachedSignature(attestation.AttestorKeySPKISHA256),
		ReleaseApproval:             approval,
		ReleaseApprovalSignature:    p3fDummyDetachedSignature(approval.ApproverKeySPKISHA256),
		MoARoleGrant:                grant,
		MoARoleGrantSignature:       p3fDummyDetachedSignature(grant.GrantorKeySPKISHA256),
	}
}

func p3fTestAuthenticatedReceipt(envelope store.ExternalSupervisorEnvelope, attempt int) store.ExternalSupervisorAuthenticatedReceipt {
	chain := p3fTestAuthorization(envelope)
	issued := fmt.Sprintf("2026-07-25T00:05:%02dZ", attempt)
	delivery := mustP3FSealDelivery(store.ExternalSupervisorSealedDelivery{
		SchemaVersion: store.ExternalSupervisorSealedDeliverySchemaVersion, AttemptCap: envelope.AttemptCap, AttemptNumber: envelope.AttemptNumber,
		ChannelBindingHash: p3fExternalSupervisorHash(fmt.Sprintf("delivery-channel-%d", attempt)), Deadline: envelope.Deadline,
		DeliveryExpiresAt: "2026-07-25T00:10:00Z", DeliveryID: "sealed_delivery_p3f_001", IssuedAt: issued,
		MoARoleGrantHash: chain.MoARoleGrant.GrantHash, NonceHash: p3fExternalSupervisorHash(fmt.Sprintf("delivery-nonce-%d", attempt)),
		PredecessorEnvelopeHash: envelope.EnvelopeHash, PredecessorIdempotencyKeyHash: envelope.IdempotencyKeyHash,
		ReleaseApprovalHash: chain.ReleaseApproval.ApprovalHash, ReleaseAttestationHash: chain.ReleaseAttestation.AttestationHash,
		RouteMappingHash: envelope.RouteMappingHash, TrustBundleHash: p3fExternalSupervisorHash("trust-bundle-v1"),
	})
	receipt := mustP3FSealProtocolReceipt(store.ExternalSupervisorProtocolReceipt{
		SchemaVersion: store.ExternalSupervisorProtocolReceiptSchemaVersion, AttemptNumber: envelope.AttemptNumber,
		ChannelBindingHash: p3fExternalSupervisorHash(fmt.Sprintf("receipt-channel-%d", attempt)), DeliveryHash: delivery.DeliveryHash,
		EnvelopeHash: envelope.EnvelopeHash, IssuedAt: "2026-07-25T00:06:00Z", NonceHash: p3fExternalSupervisorHash(fmt.Sprintf("receipt-nonce-%d", attempt)),
		ReceiptID: "acceptance_receipt_p3f_001", ReleaseApprovalHash: chain.ReleaseApproval.ApprovalHash, RouteMappingHash: envelope.RouteMappingHash,
		SignerKeySPKISHA256: p3fExternalSupervisorHash("peer-spki"), TrustRootID: "independent_supervisor_release_root_v1",
	})
	return store.ExternalSupervisorAuthenticatedReceipt{
		SchemaVersion: store.ExternalSupervisorAuthenticatedReceiptSchemaVersion, Authorization: chain, Delivery: delivery,
		DeliveryAuthentication: p3fDummyMessageAuthentication("delivery", delivery.DeliveryHash, delivery.NonceHash, delivery.ChannelBindingHash, delivery.IssuedAt),
		Receipt:                receipt, ReceiptAuthentication: p3fDummyMessageAuthentication("receipt", receipt.ReceiptHash, receipt.NonceHash, receipt.ChannelBindingHash, receipt.IssuedAt),
	}
}

func p3fTestAuthenticatedCallback(receipt store.ExternalSupervisorAuthenticatedReceipt, terminalState string) store.ExternalSupervisorAuthenticatedCallback {
	callback := mustP3FSealProtocolCallback(store.ExternalSupervisorProtocolCallback{
		SchemaVersion: store.ExternalSupervisorProtocolCallbackSchemaVersion, AttemptNumber: receipt.Receipt.AttemptNumber,
		CallbackChannelBindingHash: p3fExternalSupervisorHash("callback-channel"), CallbackID: "completion_callback_p3f_001",
		DeliveryHash: receipt.Delivery.DeliveryHash, EnvelopeHash: receipt.Receipt.EnvelopeHash, EvidenceHash: p3fExternalSupervisorHash("evidence"),
		IssuedAt: "2026-07-25T00:07:00Z", NonceHash: p3fExternalSupervisorHash("callback-nonce"), ReceiptHash: receipt.Receipt.ReceiptHash,
		ResultSchemaVersion: "ananke.independent-supervisor-result.v1", RouteMappingHash: receipt.Receipt.RouteMappingHash,
		SignerKeySPKISHA256: receipt.Receipt.SignerKeySPKISHA256, TerminalState: terminalState, TrustRootID: receipt.Receipt.TrustRootID,
	})
	return store.ExternalSupervisorAuthenticatedCallback{
		SchemaVersion: store.ExternalSupervisorAuthenticatedCallbackSchemaVersion, Callback: callback,
		CallbackAuthentication: p3fDummyMessageAuthentication("callback", callback.CallbackHash, callback.NonceHash, callback.CallbackChannelBindingHash, callback.IssuedAt),
	}
}

func p3fTestAuthenticatedCancellation(envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, cancellation store.ExternalSupervisorCancellation) store.ExternalSupervisorAuthenticatedCancellation {
	ack := mustP3FSealCancellationAcknowledgement(store.ExternalSupervisorCancellationAcknowledgement{
		SchemaVersion: store.ExternalSupervisorCancellationAcknowledgementSchemaVersion, AcknowledgementID: "cancellation_acknowledgement_p3f_001",
		CancellationHash: cancellation.CancellationIdentityHash, DeliveryHash: receipt.Delivery.DeliveryHash, EnvelopeHash: envelope.EnvelopeHash,
		ReceiptHash: receipt.Receipt.ReceiptHash, AttemptNumber: envelope.AttemptNumber, ChannelBindingHash: p3fExternalSupervisorHash("cancellation-channel"),
		NonceHash: p3fExternalSupervisorHash("cancellation-nonce"), IssuedAt: time.Now().UTC().Format(time.RFC3339Nano),
		SignerKeySPKISHA256: receipt.Receipt.SignerKeySPKISHA256, TrustRootID: receipt.Receipt.TrustRootID,
	})
	return store.ExternalSupervisorAuthenticatedCancellation{
		SchemaVersion: store.ExternalSupervisorAuthenticatedCancellationSchemaVersion, Cancellation: cancellation, Acknowledgement: ack,
		AcknowledgementAuthentication: p3fDummyMessageAuthentication("cancellation", ack.AcknowledgementHash, ack.NonceHash, ack.ChannelBindingHash, ack.IssuedAt),
	}
}

func p3fDummyDetachedSignature(signer string) store.ExternalSupervisorDetachedSignature {
	value, err := store.SealExternalSupervisorDetachedSignature(store.ExternalSupervisorDetachedSignature{
		SchemaVersion: store.ExternalSupervisorDetachedSignatureSchemaVersion, Algorithm: "ed25519", SignerKeySPKISHA256: signer,
		Signature: "ed25519:" + fmt.Sprintf("%0128x", 0),
	})
	if err != nil {
		panic(err)
	}
	return value
}

func p3fDummyMessageAuthentication(kind, message, nonce, channel, issued string) store.ExternalSupervisorMessageAuthentication {
	value, err := store.SealExternalSupervisorMessageAuthentication(store.ExternalSupervisorMessageAuthentication{
		SchemaVersion: store.ExternalSupervisorMessageAuthenticationSchemaVersion, MessageType: kind, MessageHash: message,
		NonceHash: nonce, ChannelBindingHash: channel, RequestHash: p3fExternalSupervisorHash(kind + "-request"), IssuedAt: issued,
		SignerKeySPKISHA256: p3fExternalSupervisorHash("peer-spki"), Signature: "ed25519:" + fmt.Sprintf("%0128x", 0),
	})
	if err != nil {
		panic(err)
	}
	return value
}

func mustP3FSealReleaseAttestation(value store.ExternalSupervisorReleaseAttestation) store.ExternalSupervisorReleaseAttestation {
	sealed, err := store.SealExternalSupervisorReleaseAttestation(value)
	if err != nil {
		panic(err)
	}
	return sealed
}
func mustP3FSealReleaseApproval(value store.ExternalSupervisorReleaseApproval) store.ExternalSupervisorReleaseApproval {
	sealed, err := store.SealExternalSupervisorReleaseApproval(value)
	if err != nil {
		panic(err)
	}
	return sealed
}
func mustP3FSealMoAGrant(value store.ExternalSupervisorMoARoleGrant) store.ExternalSupervisorMoARoleGrant {
	sealed, err := store.SealExternalSupervisorMoARoleGrant(value)
	if err != nil {
		panic(err)
	}
	return sealed
}
func mustP3FSealDelivery(value store.ExternalSupervisorSealedDelivery) store.ExternalSupervisorSealedDelivery {
	sealed, err := store.SealExternalSupervisorSealedDelivery(value)
	if err != nil {
		panic(err)
	}
	return sealed
}
func mustP3FSealProtocolReceipt(value store.ExternalSupervisorProtocolReceipt) store.ExternalSupervisorProtocolReceipt {
	sealed, err := store.SealExternalSupervisorProtocolReceipt(value)
	if err != nil {
		panic(err)
	}
	return sealed
}
func mustP3FSealProtocolCallback(value store.ExternalSupervisorProtocolCallback) store.ExternalSupervisorProtocolCallback {
	sealed, err := store.SealExternalSupervisorProtocolCallback(value)
	if err != nil {
		panic(err)
	}
	return sealed
}
func mustP3FSealCancellationAcknowledgement(value store.ExternalSupervisorCancellationAcknowledgement) store.ExternalSupervisorCancellationAcknowledgement {
	sealed, err := store.SealExternalSupervisorCancellationAcknowledgement(value)
	if err != nil {
		panic(err)
	}
	return sealed
}
