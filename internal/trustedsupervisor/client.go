package trustedsupervisor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// Client performs mandatory Ed25519 verification from pinned public roots.
// It stores no private key and retains no process-local replay authority.
type Client struct {
	config   Config
	verifier *ed25519Verifier
}

func NewClient(config Config) (*Client, error) {
	verifier, err := newEd25519Verifier(config.TrustBundle, config.ExpectedPredecessorReleaseIdentity)
	if err != nil {
		return nil, err
	}
	if config.SocketPath == "" || !filepath.IsAbs(config.SocketPath) || strings.IndexByte(config.SocketPath, 0) >= 0 {
		return nil, fmt.Errorf("%w: absolute operator socket path required", ErrProtocol)
	}
	if config.ExpectedProcessID <= 0 {
		return nil, fmt.Errorf("%w: expected peer process ID required", ErrAuthentication)
	}
	if config.MaxFrameBytes < minFrameBytes || config.MaxFrameBytes > maxFrameBytes {
		return nil, fmt.Errorf("%w: configured frame limit", ErrLimit)
	}
	if config.Timeout <= 0 || config.Timeout > maxTimeout {
		return nil, fmt.Errorf("%w: configured timeout", ErrDeadline)
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Binder == nil {
		config.Binder = unixPeerChannelBinder{}
	}
	if config.DialContext == nil {
		dialer := &net.Dialer{}
		config.DialContext = dialer.DialContext
	}
	return &Client{config: config, verifier: verifier}, nil
}

// DecodeTrustBundle requires closed canonical JSON and validates all public
// roots, rotations, revocations, certificates, and detached signatures.
func DecodeTrustBundle(contents []byte) (store.ExternalSupervisorTrustBundle, error) {
	var bundle store.ExternalSupervisorTrustBundle
	if err := decodeCanonical(contents, &bundle); err != nil {
		return store.ExternalSupervisorTrustBundle{}, err
	}
	if _, err := newEd25519Verifier(bundle); err != nil {
		return store.ExternalSupervisorTrustBundle{}, err
	}
	return bundle, nil
}

func (client *Client) Deliver(ctx context.Context, envelope store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAuthenticatedReceipt, error) {
	if client == nil || ctx == nil || store.ValidateExternalSupervisorEnvelope(envelope) != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, ErrProtocol
	}
	envelopeDeadline, err := time.Parse(time.RFC3339Nano, envelope.Deadline)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, ErrDeadline
	}
	exchangeCtx, cancel, err := client.exchangeContext(ctx, envelopeDeadline)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	defer cancel()
	issuedAt := client.config.Now().UTC()
	if !issuedAt.Before(envelopeDeadline) {
		return store.ExternalSupervisorAuthenticatedReceipt{}, ErrDeadline
	}
	if err := client.verifier.verifyAuthorizationAt(exchangeCtx, envelope, client.config.TrustBundle.Authorization, issuedAt); err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	envelopeReference, err := sealWireEnvelopeReference(envelope)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	deliveryNonce, err := newNonceHash()
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	responseNonce, err := newNonceHash()
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	expiresAt := issuedAt.Add(time.Minute)
	if envelopeDeadline.Before(expiresAt) {
		expiresAt = envelopeDeadline
	}
	delivery := store.ExternalSupervisorSealedDelivery{
		SchemaVersion: store.ExternalSupervisorSealedDeliverySchemaVersion,
		AttemptCap:    envelope.AttemptCap, AttemptNumber: envelope.AttemptNumber,
		Deadline: envelope.Deadline, DeliveryExpiresAt: expiresAt.Format(time.RFC3339Nano),
		DeliveryID: deliveryID(envelope.EnvelopeHash), IssuedAt: issuedAt.Format(time.RFC3339Nano),
		MoARoleGrantHash: client.config.TrustBundle.Authorization.MoARoleGrant.GrantHash,
		NonceHash:        deliveryNonce, PredecessorEnvelopeHash: envelope.EnvelopeHash,
		PredecessorIdempotencyKeyHash: envelope.IdempotencyKeyHash,
		ReleaseApprovalHash:           client.config.TrustBundle.Authorization.ReleaseApproval.ApprovalHash,
		ReleaseAttestationHash:        client.config.TrustBundle.Authorization.ReleaseAttestation.AttestationHash,
		RouteMappingHash:              envelope.RouteMappingHash, TrustBundleHash: client.config.TrustBundle.TrustBundleHash,
	}
	requestNonce, err := newNonceHash()
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	request := wireRequest{
		SchemaVersion: requestSchemaVersion, Operation: operationDeliver, EnvelopeReference: &envelopeReference,
		Authorization: &client.config.TrustBundle.Authorization, Delivery: &delivery,
		RequestNonceHash: requestNonce, ResponseNonceHash: responseNonce,
	}
	response, boundRequest, err := client.exchange(exchangeCtx, request)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	if response.Status != "accepted" || response.DeliveryAuthentication == nil || response.Receipt == nil || response.ReceiptAuthentication == nil ||
		response.Callback != nil || response.CallbackAuthentication != nil || response.CancellationAcknowledgement != nil || response.CancellationAuthentication != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, fmt.Errorf("%w: delivery response shape", ErrProtocol)
	}
	expectedReceiptChannel, err := deriveMessageChannelBinding(boundRequest.ChannelBindingHash, "receipt", responseNonce, boundRequest.Delivery.DeliveryHash)
	if err != nil || response.Receipt.ChannelBindingHash != expectedReceiptChannel || response.Receipt.NonceHash != responseNonce ||
		response.DeliveryAuthentication.RequestHash != boundRequest.RequestHash || response.ReceiptAuthentication.RequestHash != boundRequest.RequestHash {
		return store.ExternalSupervisorAuthenticatedReceipt{}, authenticationError("receipt channel or request binding")
	}
	authenticated := store.ExternalSupervisorAuthenticatedReceipt{
		SchemaVersion: store.ExternalSupervisorAuthenticatedReceiptSchemaVersion,
		Authorization: *boundRequest.Authorization, Delivery: *boundRequest.Delivery,
		DeliveryAuthentication: *response.DeliveryAuthentication,
		Receipt:                *response.Receipt, ReceiptAuthentication: *response.ReceiptAuthentication,
	}
	if err := client.verifyReceipt(exchangeCtx, envelope, authenticated, true); err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	return authenticated, nil
}

func (client *Client) Reconcile(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) (*store.ExternalSupervisorAuthenticatedCallback, error) {
	if client == nil || ctx == nil {
		return nil, ErrProtocol
	}
	envelopeDeadline, err := time.Parse(time.RFC3339Nano, envelope.Deadline)
	if err != nil {
		return nil, ErrDeadline
	}
	exchangeCtx, cancel, err := client.exchangeContext(ctx, envelopeDeadline)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if err := client.verifyReceipt(exchangeCtx, envelope, receipt, false); err != nil {
		return nil, err
	}
	envelopeReference, err := sealWireEnvelopeReference(envelope)
	if err != nil {
		return nil, err
	}
	requestNonce, err := newNonceHash()
	if err != nil {
		return nil, err
	}
	responseNonce, err := newNonceHash()
	if err != nil {
		return nil, err
	}
	request := wireRequest{
		SchemaVersion: requestSchemaVersion, Operation: operationReconcile,
		EnvelopeReference: &envelopeReference, Receipt: &receipt, RequestNonceHash: requestNonce, ResponseNonceHash: responseNonce,
	}
	response, boundRequest, err := client.exchange(exchangeCtx, request)
	if err != nil {
		return nil, err
	}
	if response.Status == "pending" && response.Callback == nil && response.CallbackAuthentication == nil {
		return nil, nil
	}
	if response.Status != "accepted" || response.Callback == nil || response.CallbackAuthentication == nil ||
		response.DeliveryAuthentication != nil || response.Receipt != nil || response.ReceiptAuthentication != nil ||
		response.CancellationAcknowledgement != nil || response.CancellationAuthentication != nil {
		return nil, fmt.Errorf("%w: callback response shape", ErrProtocol)
	}
	expectedChannel, err := deriveMessageChannelBinding(boundRequest.ChannelBindingHash, "callback", responseNonce, receipt.Receipt.ReceiptHash)
	if err != nil || response.Callback.CallbackChannelBindingHash != expectedChannel || response.Callback.NonceHash != responseNonce ||
		response.CallbackAuthentication.RequestHash != boundRequest.RequestHash {
		return nil, authenticationError("callback channel or request binding")
	}
	authenticated := store.ExternalSupervisorAuthenticatedCallback{
		SchemaVersion: store.ExternalSupervisorAuthenticatedCallbackSchemaVersion,
		Callback:      *response.Callback, CallbackAuthentication: *response.CallbackAuthentication,
	}
	if err := client.verifyCallback(exchangeCtx, envelope, receipt, authenticated, true); err != nil {
		return nil, err
	}
	return &authenticated, nil
}

func (client *Client) Cancel(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, cancellation store.ExternalSupervisorCancellation) (store.ExternalSupervisorAuthenticatedCancellation, error) {
	if client == nil || ctx == nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, ErrProtocol
	}
	envelopeDeadline, err := time.Parse(time.RFC3339Nano, envelope.Deadline)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, ErrDeadline
	}
	exchangeCtx, cancel, err := client.exchangeContext(ctx, envelopeDeadline)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, err
	}
	defer cancel()
	if err := client.verifyReceipt(exchangeCtx, envelope, receipt, false); err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, err
	}
	sealedCancellation, err := store.SealExternalSupervisorCancellation(cancellation)
	if err != nil || sealedCancellation != cancellation || cancellation.HandoffID != envelope.HandoffID ||
		cancellation.EnvelopeHash != envelope.EnvelopeHash || cancellation.ReceiptIdentityHash != receipt.Receipt.ReceiptHash ||
		cancellation.AttemptNumber != envelope.AttemptNumber {
		return store.ExternalSupervisorAuthenticatedCancellation{}, fmt.Errorf("%w: cancellation binding", ErrProtocol)
	}
	envelopeReference, err := sealWireEnvelopeReference(envelope)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, err
	}
	requestNonce, err := newNonceHash()
	if err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, err
	}
	responseNonce, err := newNonceHash()
	if err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, err
	}
	request := wireRequest{
		SchemaVersion: requestSchemaVersion, Operation: operationCancel, EnvelopeReference: &envelopeReference,
		Receipt: &receipt, Cancellation: &cancellation, RequestNonceHash: requestNonce, ResponseNonceHash: responseNonce,
	}
	response, boundRequest, err := client.exchange(exchangeCtx, request)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, err
	}
	if response.Status != "accepted" || response.CancellationAcknowledgement == nil || response.CancellationAuthentication == nil ||
		response.DeliveryAuthentication != nil || response.Receipt != nil || response.ReceiptAuthentication != nil ||
		response.Callback != nil || response.CallbackAuthentication != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, fmt.Errorf("%w: cancellation response shape", ErrProtocol)
	}
	expectedChannel, err := deriveMessageChannelBinding(boundRequest.ChannelBindingHash, "cancellation", responseNonce, cancellation.CancellationIdentityHash)
	if err != nil || response.CancellationAcknowledgement.ChannelBindingHash != expectedChannel || response.CancellationAcknowledgement.NonceHash != responseNonce ||
		response.CancellationAuthentication.RequestHash != boundRequest.RequestHash {
		return store.ExternalSupervisorAuthenticatedCancellation{}, authenticationError("cancellation channel or request binding")
	}
	authenticated := store.ExternalSupervisorAuthenticatedCancellation{
		SchemaVersion: store.ExternalSupervisorAuthenticatedCancellationSchemaVersion,
		Cancellation:  cancellation, Acknowledgement: *response.CancellationAcknowledgement,
		AcknowledgementAuthentication: *response.CancellationAuthentication,
	}
	if err := client.verifyCancellation(exchangeCtx, envelope, receipt, authenticated, true); err != nil {
		return store.ExternalSupervisorAuthenticatedCancellation{}, err
	}
	return authenticated, nil
}

func (client *Client) VerifyExternalSupervisorEnvelope(ctx context.Context, envelope store.ExternalSupervisorEnvelope) error {
	if client == nil || ctx == nil || ctx.Err() != nil {
		return ErrAuthentication
	}
	return client.verifier.verifyAuthorizationAt(ctx, envelope, client.config.TrustBundle.Authorization, client.config.Now().UTC())
}

func (client *Client) VerifyExternalSupervisorReceipt(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) error {
	return client.verifyReceipt(ctx, envelope, receipt, false)
}

func (client *Client) VerifyExternalSupervisorCallback(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, callback store.ExternalSupervisorAuthenticatedCallback) error {
	return client.verifyCallback(ctx, envelope, receipt, callback, false)
}

func (client *Client) VerifyExternalSupervisorCancellation(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, cancellation store.ExternalSupervisorAuthenticatedCancellation) error {
	return client.verifyCancellation(ctx, envelope, receipt, cancellation, false)
}

func (client *Client) verifyReceipt(ctx context.Context, envelope store.ExternalSupervisorEnvelope, authenticated store.ExternalSupervisorAuthenticatedReceipt, runHooks bool) error {
	if client == nil || ctx == nil || ctx.Err() != nil || authenticated.SchemaVersion != store.ExternalSupervisorAuthenticatedReceiptSchemaVersion ||
		authenticated.Authorization != client.config.TrustBundle.Authorization || authenticated.Delivery.TrustBundleHash != client.config.TrustBundle.TrustBundleHash {
		return authenticationError("durable receipt trust bundle")
	}
	delivery, receipt := authenticated.Delivery, authenticated.Receipt
	sealedDelivery, deliveryErr := store.SealExternalSupervisorSealedDelivery(delivery)
	sealedReceipt, receiptErr := store.SealExternalSupervisorProtocolReceipt(receipt)
	if deliveryErr != nil || sealedDelivery != delivery || receiptErr != nil || sealedReceipt != receipt ||
		delivery.PredecessorEnvelopeHash != envelope.EnvelopeHash || delivery.PredecessorIdempotencyKeyHash != envelope.IdempotencyKeyHash ||
		delivery.AttemptNumber != envelope.AttemptNumber || delivery.AttemptCap != envelope.AttemptCap || delivery.Deadline != envelope.Deadline ||
		delivery.RouteMappingHash != envelope.RouteMappingHash || delivery.ReleaseAttestationHash != authenticated.Authorization.ReleaseAttestation.AttestationHash ||
		delivery.ReleaseApprovalHash != authenticated.Authorization.ReleaseApproval.ApprovalHash || delivery.MoARoleGrantHash != authenticated.Authorization.MoARoleGrant.GrantHash ||
		receipt.DeliveryHash != delivery.DeliveryHash || receipt.EnvelopeHash != envelope.EnvelopeHash || receipt.AttemptNumber != envelope.AttemptNumber ||
		receipt.RouteMappingHash != envelope.RouteMappingHash || receipt.ReleaseApprovalHash != authenticated.Authorization.ReleaseApproval.ApprovalHash ||
		receipt.SignerKeySPKISHA256 != client.config.TrustBundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256 {
		return authenticationError("receipt transitive binding")
	}
	deliveryAt, deliveryTimeErr := time.Parse(time.RFC3339Nano, delivery.IssuedAt)
	receiptAt, receiptTimeErr := time.Parse(time.RFC3339Nano, receipt.IssuedAt)
	expiresAt, expiryErr := time.Parse(time.RFC3339Nano, delivery.DeliveryExpiresAt)
	if deliveryTimeErr != nil || receiptTimeErr != nil || expiryErr != nil || receiptAt.Before(deliveryAt) || !receiptAt.Before(expiresAt) {
		return authenticationError("receipt temporal order")
	}
	if err := client.verifier.verifyAuthorizationAt(ctx, envelope, authenticated.Authorization, deliveryAt); err != nil {
		return err
	}
	if err := client.verifier.verifyAuthorizationAt(ctx, envelope, authenticated.Authorization, receiptAt); err != nil {
		return err
	}
	if err := client.verifyPeerMessage(ctx, "delivery", delivery.DeliveryHash, delivery.NonceHash, delivery.ChannelBindingHash, delivery.IssuedAt, authenticated.DeliveryAuthentication); err != nil {
		return err
	}
	if err := client.verifyPeerMessage(ctx, "receipt", receipt.ReceiptHash, receipt.NonceHash, receipt.ChannelBindingHash, receipt.IssuedAt, authenticated.ReceiptAuthentication); err != nil {
		return err
	}
	root, _, err := client.verifier.rootAt(client.config.TrustBundle.ReleaseRoots, receiptAt)
	if err != nil || receipt.TrustRootID != root.RootID {
		return authenticationError("receipt release root")
	}
	if runHooks {
		if err := client.runHook(ctx, "delivery", delivery.DeliveryHash, deliveryAt); err != nil {
			return err
		}
		if err := client.runHook(ctx, "receipt", receipt.ReceiptHash, receiptAt); err != nil {
			return err
		}
	}
	return nil
}

func (client *Client) verifyCallback(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, authenticated store.ExternalSupervisorAuthenticatedCallback, runHooks bool) error {
	if err := client.verifyReceipt(ctx, envelope, receipt, false); err != nil {
		return err
	}
	callback := authenticated.Callback
	sealed, err := store.SealExternalSupervisorProtocolCallback(callback)
	if authenticated.SchemaVersion != store.ExternalSupervisorAuthenticatedCallbackSchemaVersion || err != nil || sealed != callback ||
		callback.DeliveryHash != receipt.Delivery.DeliveryHash || callback.EnvelopeHash != envelope.EnvelopeHash ||
		callback.ReceiptHash != receipt.Receipt.ReceiptHash || callback.RouteMappingHash != envelope.RouteMappingHash ||
		callback.AttemptNumber != envelope.AttemptNumber || callback.SignerKeySPKISHA256 != client.config.TrustBundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256 {
		return authenticationError("callback transitive binding")
	}
	callbackAt, err := time.Parse(time.RFC3339Nano, callback.IssuedAt)
	receiptAt, receiptErr := time.Parse(time.RFC3339Nano, receipt.Receipt.IssuedAt)
	deadline, deadlineErr := time.Parse(time.RFC3339Nano, envelope.Deadline)
	if err != nil || receiptErr != nil || deadlineErr != nil || callbackAt.Before(receiptAt) || !callbackAt.Before(deadline) {
		return authenticationError("callback temporal order")
	}
	if err := client.verifier.verifyAuthorizationAt(ctx, envelope, receipt.Authorization, callbackAt); err != nil {
		return err
	}
	if err := client.verifyPeerMessage(ctx, "callback", callback.CallbackHash, callback.NonceHash, callback.CallbackChannelBindingHash, callback.IssuedAt, authenticated.CallbackAuthentication); err != nil {
		return err
	}
	root, _, err := client.verifier.rootAt(client.config.TrustBundle.ReleaseRoots, callbackAt)
	if err != nil || callback.TrustRootID != root.RootID {
		return authenticationError("callback release root")
	}
	if runHooks {
		return client.runHook(ctx, "callback", callback.CallbackHash, callbackAt)
	}
	return nil
}

func (client *Client) verifyCancellation(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, authenticated store.ExternalSupervisorAuthenticatedCancellation, runHooks bool) error {
	if err := client.verifyReceipt(ctx, envelope, receipt, false); err != nil {
		return err
	}
	sealedCancellation, cancellationErr := store.SealExternalSupervisorCancellation(authenticated.Cancellation)
	sealedAcknowledgement, acknowledgementErr := store.SealExternalSupervisorCancellationAcknowledgement(authenticated.Acknowledgement)
	ack := authenticated.Acknowledgement
	if authenticated.SchemaVersion != store.ExternalSupervisorAuthenticatedCancellationSchemaVersion || cancellationErr != nil ||
		sealedCancellation != authenticated.Cancellation || acknowledgementErr != nil || sealedAcknowledgement != ack ||
		authenticated.Cancellation.HandoffID != envelope.HandoffID || authenticated.Cancellation.EnvelopeHash != envelope.EnvelopeHash ||
		authenticated.Cancellation.ReceiptIdentityHash != receipt.Receipt.ReceiptHash || authenticated.Cancellation.AttemptNumber != envelope.AttemptNumber ||
		ack.CancellationHash != authenticated.Cancellation.CancellationIdentityHash || ack.DeliveryHash != receipt.Delivery.DeliveryHash ||
		ack.EnvelopeHash != envelope.EnvelopeHash || ack.ReceiptHash != receipt.Receipt.ReceiptHash || ack.AttemptNumber != envelope.AttemptNumber ||
		ack.SignerKeySPKISHA256 != client.config.TrustBundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256 {
		return authenticationError("cancellation transitive binding")
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, ack.IssuedAt)
	deadline, deadlineErr := time.Parse(time.RFC3339Nano, envelope.Deadline)
	if err != nil || deadlineErr != nil || !issuedAt.Before(deadline) {
		return authenticationError("cancellation time")
	}
	if err := client.verifier.verifyAuthorizationAt(ctx, envelope, receipt.Authorization, issuedAt); err != nil {
		return err
	}
	if err := client.verifyPeerMessage(ctx, "cancellation", ack.AcknowledgementHash, ack.NonceHash, ack.ChannelBindingHash, ack.IssuedAt, authenticated.AcknowledgementAuthentication); err != nil {
		return err
	}
	root, _, err := client.verifier.rootAt(client.config.TrustBundle.ReleaseRoots, issuedAt)
	if err != nil || ack.TrustRootID != root.RootID {
		return authenticationError("cancellation release root")
	}
	if runHooks {
		return client.runHook(ctx, "cancellation", ack.AcknowledgementHash, issuedAt)
	}
	return nil
}

func (client *Client) verifyPeerMessage(ctx context.Context, messageType, messageHash, nonceHash, channelHash, issuedAt string, evidence store.ExternalSupervisorMessageAuthentication) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrDeadline, ctx.Err())
	}
	sealed, err := store.SealExternalSupervisorMessageAuthentication(evidence)
	if err != nil || sealed != evidence || evidence.MessageType != messageType || evidence.MessageHash != messageHash ||
		evidence.NonceHash != nonceHash || evidence.ChannelBindingHash != channelHash || evidence.IssuedAt != issuedAt ||
		evidence.SignerKeySPKISHA256 != client.config.TrustBundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256 {
		return authenticationError("message authentication binding")
	}
	at, err := time.Parse(time.RFC3339Nano, issuedAt)
	if err != nil {
		return authenticationError("message authentication time")
	}
	root, rootKey, err := client.verifier.rootAt(client.config.TrustBundle.ReleaseRoots, at)
	if err != nil {
		return authenticationError("peer root")
	}
	peerKey, err := verifyCertificateAt(client.config.TrustBundle.SupervisorPeer, "independent_supervisor_protocol_adapter", root, rootKey, at)
	if err != nil {
		return authenticationError("peer certificate")
	}
	if err := verifyMessageAuthenticationSignature(peerKey, evidence); err != nil {
		return authenticationError("peer possession signature")
	}
	return nil
}

func verifyMessageAuthenticationSignature(publicKey ed25519.PublicKey, evidence store.ExternalSupervisorMessageAuthentication) error {
	signature, err := decodePrefixedHex(evidence.Signature, "ed25519:", ed25519.SignatureSize)
	if err != nil {
		return err
	}
	payload, err := canonicalMessageAuthenticationPayload(evidence)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return ErrAuthentication
	}
	return nil
}

func canonicalMessageAuthenticationPayload(evidence store.ExternalSupervisorMessageAuthentication) ([]byte, error) {
	return marshalCanonical(map[string]any{
		"schema_version":         evidence.SchemaVersion,
		"message_type":           evidence.MessageType,
		"message_hash":           evidence.MessageHash,
		"nonce_hash":             evidence.NonceHash,
		"channel_binding_hash":   evidence.ChannelBindingHash,
		"request_hash":           evidence.RequestHash,
		"issued_at":              evidence.IssuedAt,
		"signer_key_spki_sha256": evidence.SignerKeySPKISHA256,
	})
}

func (client *Client) runHook(ctx context.Context, messageType, messageHash string, issuedAt time.Time) error {
	if client.config.Authentication == nil {
		return nil
	}
	if err := client.config.Authentication.Authenticate(ctx, AuthenticationBoundary{MessageType: messageType, MessageHash: messageHash, IssuedAt: issuedAt}); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: authentication hook: %v", ErrDeadline, ctx.Err())
		}
		return fmt.Errorf("%w: authentication hook: %v", ErrAuthentication, err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w: authentication hook: %v", ErrDeadline, ctx.Err())
	}
	return nil
}

func (client *Client) exchangeContext(parent context.Context, envelopeDeadline time.Time) (context.Context, context.CancelFunc, error) {
	if err := parent.Err(); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	deadline := time.Now().Add(client.config.Timeout)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	if !envelopeDeadline.IsZero() && envelopeDeadline.Before(deadline) {
		deadline = envelopeDeadline
	}
	if !time.Now().Before(deadline) {
		return nil, nil, ErrDeadline
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	return ctx, cancel, nil
}

func (client *Client) exchange(ctx context.Context, request wireRequest) (wireResponse, wireRequest, error) {
	if err := ctx.Err(); err != nil {
		return wireResponse{}, wireRequest{}, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	before, err := validateSocketFile(client.config.SocketPath, client.config.ExpectedUserID)
	if err != nil {
		return wireResponse{}, wireRequest{}, err
	}
	connection, err := client.config.DialContext(ctx, "unix", client.config.SocketPath)
	if err != nil {
		return wireResponse{}, wireRequest{}, classifyIOError(err)
	}
	defer connection.Close()
	after, err := validateSocketFile(client.config.SocketPath, client.config.ExpectedUserID)
	if err != nil || after != before {
		return wireResponse{}, wireRequest{}, authenticationError("Unix socket path replaced during connect")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return wireResponse{}, wireRequest{}, fmt.Errorf("%w: set socket deadline: %v", ErrDeadline, err)
		}
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = connection.SetDeadline(time.Now()) })
	defer stopCancellation()
	payloadHash, err := requestPayloadHash(request)
	if err != nil {
		return wireResponse{}, wireRequest{}, fmt.Errorf("%w: request payload: %v", ErrProtocol, err)
	}
	channel, err := client.config.Binder.Bind(ctx, connection, client.config.ExpectedUserID, client.config.ExpectedProcessID, payloadHash)
	if err != nil {
		return wireResponse{}, wireRequest{}, err
	}
	request.ChannelBindingHash = channel.BindingHash
	if request.Delivery != nil {
		deliveryChannel, err := deriveMessageChannelBinding(channel.BindingHash, "delivery", request.Delivery.NonceHash, request.EnvelopeReference.PredecessorProjection.PredecessorProjectionHash)
		if err != nil {
			return wireResponse{}, wireRequest{}, err
		}
		request.Delivery.ChannelBindingHash = deliveryChannel
		sealed, err := store.SealExternalSupervisorSealedDelivery(*request.Delivery)
		if err != nil {
			return wireResponse{}, wireRequest{}, fmt.Errorf("%w: delivery record", ErrProtocol)
		}
		request.Delivery = &sealed
	}
	requestHash, err := hashWireRequest(request)
	if err != nil {
		return wireResponse{}, wireRequest{}, err
	}
	request.RequestHash = requestHash
	requestBytes, err := marshalCanonical(request)
	if err != nil {
		return wireResponse{}, wireRequest{}, fmt.Errorf("%w: canonical request", ErrProtocol)
	}
	if err := writeFrame(connection, requestBytes, client.config.MaxFrameBytes); err != nil {
		return wireResponse{}, wireRequest{}, classifyIOError(err)
	}
	if closeWriter, ok := connection.(interface{ CloseWrite() error }); ok {
		if err := closeWriter.CloseWrite(); err != nil {
			return wireResponse{}, wireRequest{}, classifyIOError(err)
		}
	}
	responseBytes, err := readFrame(connection, client.config.MaxFrameBytes)
	if err != nil {
		return wireResponse{}, wireRequest{}, classifyIOError(err)
	}
	var trailing [1]byte
	if count, trailingErr := connection.Read(trailing[:]); count != 0 || (trailingErr != nil && !errors.Is(trailingErr, io.EOF)) {
		if trailingErr != nil {
			return wireResponse{}, wireRequest{}, classifyIOError(trailingErr)
		}
		return wireResponse{}, wireRequest{}, fmt.Errorf("%w: trailing frame bytes", ErrProtocol)
	}
	var response wireResponse
	if err := decodeCanonical(responseBytes, &response); err != nil {
		return wireResponse{}, wireRequest{}, err
	}
	if response.SchemaVersion != responseSchemaVersion || response.Operation != request.Operation || response.RequestHash != requestHash ||
		response.PeerSignerSPKISHA256 != client.config.TrustBundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256 {
		return wireResponse{}, wireRequest{}, authenticationError("response request or peer binding")
	}
	return response, request, nil
}

func requestPayloadHash(request wireRequest) (string, error) {
	request.RequestHash = ""
	request.ChannelBindingHash = ""
	if request.Delivery != nil {
		delivery := *request.Delivery
		delivery.ChannelBindingHash = ""
		delivery.DeliveryHash = ""
		request.Delivery = &delivery
	}
	if request.EnvelopeReference == nil || validateWireEnvelopeReference(*request.EnvelopeReference) != nil {
		return "", ErrProtocol
	}
	durableEnvelopeHash := request.EnvelopeReference.DurableEnvelopeHash
	switch request.Operation {
	case operationDeliver:
		if request.Authorization == nil || request.Delivery == nil || request.Receipt != nil || request.Cancellation != nil ||
			request.Delivery.PredecessorEnvelopeHash != durableEnvelopeHash {
			return "", ErrProtocol
		}
	case operationReconcile:
		if request.Receipt == nil || request.Authorization != nil || request.Delivery != nil || request.Cancellation != nil ||
			request.Receipt.Delivery.PredecessorEnvelopeHash != durableEnvelopeHash || request.Receipt.Receipt.EnvelopeHash != durableEnvelopeHash {
			return "", ErrProtocol
		}
	case operationCancel:
		if request.Receipt == nil || request.Cancellation == nil || request.Authorization != nil || request.Delivery != nil ||
			request.Receipt.Delivery.PredecessorEnvelopeHash != durableEnvelopeHash || request.Receipt.Receipt.EnvelopeHash != durableEnvelopeHash ||
			request.Cancellation.EnvelopeHash != durableEnvelopeHash {
			return "", ErrProtocol
		}
	default:
		return "", ErrProtocol
	}
	return canonicalHash(request)
}

func hashWireRequest(request wireRequest) (string, error) {
	request.RequestHash = ""
	return canonicalHash(request)
}

func deriveMessageChannelBinding(connectionHash, messageType, nonceHash, priorHash string) (string, error) {
	if !protocolHashPattern.MatchString(connectionHash) || !protocolHashPattern.MatchString(nonceHash) || !protocolHashPattern.MatchString(priorHash) {
		return "", ErrAuthentication
	}
	return canonicalHash(map[string]any{
		"schema_version":          "ananke.local-trusted-supervisor-channel-binding.v1",
		"connection_binding_hash": connectionHash,
		"message_type":            messageType,
		"nonce_hash":              nonceHash,
		"prior_hash":              priorHash,
	})
}

func canonicalHash(value any) (string, error) {
	canonical, err := marshalCanonical(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func newNonceHash() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("%w: nonce entropy: %v", ErrAuthentication, err)
	}
	digest := sha256.Sum256(nonce[:])
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func deliveryID(envelopeHash string) string {
	return "sealed_delivery_" + strings.TrimPrefix(envelopeHash, "sha256:")[:32]
}

func classifyIOError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: incomplete frame", ErrProtocol)
	}
	if errors.Is(err, ErrLimit) || errors.Is(err, ErrProtocol) || errors.Is(err, ErrAuthentication) {
		return err
	}
	return fmt.Errorf("%w: I/O failure: %v", ErrProtocol, err)
}
