package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func (server *Server) validateRequest(ctx context.Context, request wireRequest) (string, error) {
	if server == nil || ctx == nil || ctx.Err() != nil || request.SchemaVersion != requestSchemaVersion ||
		request.EnvelopeReference == nil || validateWireEnvelopeReference(*request.EnvelopeReference) != nil ||
		!protocolHashPattern.MatchString(request.ChannelBindingHash) || !protocolHashPattern.MatchString(request.RequestHash) ||
		!protocolHashPattern.MatchString(request.RequestNonceHash) || !protocolHashPattern.MatchString(request.ResponseNonceHash) ||
		request.RequestNonceHash == request.ResponseNonceHash {
		return "", ErrProtocol
	}
	payloadHash, err := requestPayloadHash(request)
	if err != nil {
		return "", err
	}
	expectedChannel, err := canonicalHash(map[string]any{
		"binding_schema_version": "ananke.local-unix-peer-channel-binding.v2",
		"peer_primary_group_id":  uint32(os.Getgid()),
		"peer_process_id":        os.Getpid(),
		"peer_user_id":           uint32(os.Getuid()),
		"request_payload_hash":   payloadHash,
	})
	if err != nil || request.ChannelBindingHash != expectedChannel {
		return "", authenticationError("server channel binding")
	}
	expectedRequestHash, err := hashWireRequest(request)
	if err != nil || request.RequestHash != expectedRequestHash {
		return "", authenticationError("server request hash")
	}
	envelope, err := server.reconstructEnvelope(*request.EnvelopeReference)
	if err != nil {
		return "", err
	}
	switch request.Operation {
	case operationDeliver:
		if err := server.validateDeliveryRequest(ctx, request, envelope); err != nil {
			return "", err
		}
		return operationDeliver + ":" + envelope.EnvelopeHash, nil
	case operationReconcile:
		receipt, err := server.validateReceiptRequest(ctx, request, envelope)
		if err != nil {
			return "", err
		}
		return operationReconcile + ":" + receipt.Receipt.ReceiptHash, nil
	case operationCancel:
		receipt, err := server.validateReceiptRequest(ctx, request, envelope)
		if err != nil {
			return "", err
		}
		if request.Cancellation == nil {
			return "", ErrProtocol
		}
		sealed, err := store.SealExternalSupervisorCancellation(*request.Cancellation)
		if err != nil || sealed != *request.Cancellation || request.Cancellation.HandoffID != envelope.HandoffID ||
			request.Cancellation.EnvelopeHash != envelope.EnvelopeHash ||
			request.Cancellation.ReceiptIdentityHash != receipt.Receipt.ReceiptHash ||
			request.Cancellation.AttemptNumber != envelope.AttemptNumber ||
			request.Cancellation.AttemptNumber != receipt.Receipt.AttemptNumber {
			return "", authenticationError("server cancellation binding")
		}
		return operationCancel + ":" + receipt.Receipt.ReceiptHash, nil
	default:
		return "", ErrProtocol
	}
}

func (server *Server) reconstructEnvelope(reference wireEnvelopeReference) (store.ExternalSupervisorEnvelope, error) {
	if server == nil || server.repositoryPolicy == nil || validateWireEnvelopeReference(reference) != nil {
		return store.ExternalSupervisorEnvelope{}, ErrProtocol
	}
	projection := reference.PredecessorProjection
	repositoryIdentity, err := server.repositoryPolicy.Resolve(projection.RepositoryIdentityHash)
	if err != nil {
		return store.ExternalSupervisorEnvelope{}, err
	}
	envelope := store.ExternalSupervisorEnvelope{
		SchemaVersion: projection.EnvelopeSchemaVersion, HandoffID: projection.HandoffID,
		IdempotencyKeyHash: projection.IdempotencyKeyHash, LaunchSpecHash: projection.LaunchSpecHash,
		FenceBindingHash: projection.FenceBindingHash, Deadline: projection.Deadline,
		AttemptNumber: projection.AttemptNumber, AttemptCap: projection.AttemptCap,
		RouteMappingHash: projection.RouteMappingHash, SourceSnapshotHash: projection.SourceSnapshotHash,
		SourceManifestHash: projection.SourceManifestHash, RepositoryIdentity: repositoryIdentity,
		SupervisorArtifactSHA256: projection.SupervisorArtifactSHA256, BuildIdentityHash: projection.BuildIdentityHash,
		ReleaseAttestationHash: projection.ReleaseAttestationHash, ReleaseApprovalHash: projection.ReleaseApprovalHash,
		EvidenceContractHash: projection.EvidenceContractHash, EvidenceSchemaVersion: projection.EvidenceSchemaVersion,
		EnvelopeHash: projection.EnvelopeHash,
	}
	sealed, err := store.SealExternalSupervisorEnvelope(envelope)
	if err != nil || sealed != envelope || sealed.EnvelopeHash != reference.DurableEnvelopeHash {
		return store.ExternalSupervisorEnvelope{}, authenticationError("predecessor envelope reconstruction")
	}
	expectedProjection, err := sealWirePredecessorProjection(sealed)
	if err != nil || expectedProjection != projection {
		return store.ExternalSupervisorEnvelope{}, authenticationError("predecessor projection reconstruction")
	}
	return sealed, nil
}

func (server *Server) validateDeliveryRequest(ctx context.Context, request wireRequest, envelope store.ExternalSupervisorEnvelope) error {
	if request.Authorization == nil || request.Delivery == nil || request.Receipt != nil || request.Cancellation != nil ||
		*request.Authorization != server.material.bundle.Authorization {
		return ErrProtocol
	}
	delivery := *request.Delivery
	sealed, err := store.SealExternalSupervisorSealedDelivery(delivery)
	if err != nil || sealed != delivery || delivery.PredecessorEnvelopeHash != envelope.EnvelopeHash ||
		delivery.PredecessorIdempotencyKeyHash != envelope.IdempotencyKeyHash ||
		delivery.AttemptNumber != envelope.AttemptNumber || delivery.AttemptCap != envelope.AttemptCap ||
		delivery.Deadline != envelope.Deadline || delivery.RouteMappingHash != envelope.RouteMappingHash ||
		delivery.TrustBundleHash != server.material.bundle.TrustBundleHash ||
		delivery.ReleaseAttestationHash != request.Authorization.ReleaseAttestation.AttestationHash ||
		delivery.ReleaseApprovalHash != request.Authorization.ReleaseApproval.ApprovalHash ||
		delivery.MoARoleGrantHash != request.Authorization.MoARoleGrant.GrantHash ||
		delivery.RouteMappingHash != request.Authorization.ReleaseAttestation.RouteMappingHash ||
		delivery.RouteMappingHash != request.Authorization.ReleaseApproval.RouteMappingHash ||
		delivery.RouteMappingHash != request.Authorization.MoARoleGrant.RouteMappingHash ||
		delivery.NonceHash == request.RequestNonceHash || delivery.NonceHash == request.ResponseNonceHash {
		return authenticationError("server delivery binding")
	}
	expectedChannel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "delivery", delivery.NonceHash,
		request.EnvelopeReference.PredecessorProjection.PredecessorProjectionHash)
	if err != nil || delivery.ChannelBindingHash != expectedChannel {
		return authenticationError("server delivery channel")
	}
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, delivery.IssuedAt)
	expiresAt, expiryErr := time.Parse(time.RFC3339Nano, delivery.DeliveryExpiresAt)
	deadline, deadlineErr := time.Parse(time.RFC3339Nano, delivery.Deadline)
	now := server.config.Now()
	if issuedErr != nil || expiryErr != nil || deadlineErr != nil || now.Location() != time.UTC || issuedAt.After(now) ||
		!now.Before(expiresAt) || !now.Before(deadline) {
		return ErrDeadline
	}
	if err := server.material.verifier.verifyAuthorizationAt(ctx, envelope, *request.Authorization, issuedAt); err != nil {
		return err
	}
	_, err = server.signerAt(issuedAt)
	return err
}

func (server *Server) validateReceiptRequest(ctx context.Context, request wireRequest, envelope store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAuthenticatedReceipt, error) {
	if request.Authorization != nil || request.Delivery != nil || request.Receipt == nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, ErrProtocol
	}
	if request.Operation == operationReconcile && request.Cancellation != nil ||
		request.Operation == operationCancel && request.Cancellation == nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, ErrProtocol
	}
	known, err := server.loadKnownReceipt(ctx, envelope)
	if err != nil || known != *request.Receipt {
		return store.ExternalSupervisorAuthenticatedReceipt{}, authenticationError("unknown durable server receipt")
	}
	if err := server.verifyKnownReceipt(ctx, envelope, known); err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	now := server.config.Now()
	deadline, deadlineErr := time.Parse(time.RFC3339Nano, envelope.Deadline)
	receiptAt, receiptErr := time.Parse(time.RFC3339Nano, known.Receipt.IssuedAt)
	if deadlineErr != nil || receiptErr != nil || now.Location() != time.UTC || now.Before(receiptAt) || !now.Before(deadline) ||
		request.RequestNonceHash == known.Delivery.NonceHash || request.RequestNonceHash == known.Receipt.NonceHash ||
		request.ResponseNonceHash == known.Delivery.NonceHash || request.ResponseNonceHash == known.Receipt.NonceHash {
		return store.ExternalSupervisorAuthenticatedReceipt{}, ErrDeadline
	}
	if err := server.material.verifier.verifyAuthorizationAt(ctx, envelope, known.Authorization, now); err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	if _, err := server.signerAt(now); err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	return known, nil
}

func (server *Server) loadKnownReceipt(ctx context.Context, envelope store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAuthenticatedReceipt, error) {
	requestBytes, responseBytes, err := server.journal.loadOperation(ctx, operationDeliver+":"+envelope.EnvelopeHash)
	if err != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, err
	}
	var request wireRequest
	var response wireResponse
	if decodeCanonical(requestBytes, &request) != nil || decodeCanonical(responseBytes, &response) != nil ||
		request.Operation != operationDeliver || request.EnvelopeReference == nil ||
		request.EnvelopeReference.DurableEnvelopeHash != envelope.EnvelopeHash || request.Authorization == nil || request.Delivery == nil ||
		response.Operation != operationDeliver || response.RequestHash != request.RequestHash || response.Status != "accepted" ||
		response.DeliveryAuthentication == nil || response.Receipt == nil || response.ReceiptAuthentication == nil ||
		response.Callback != nil || response.CallbackAuthentication != nil ||
		response.CancellationAcknowledgement != nil || response.CancellationAuthentication != nil {
		return store.ExternalSupervisorAuthenticatedReceipt{}, authenticationError("durable server receipt row")
	}
	storedEnvelope, err := server.reconstructEnvelope(*request.EnvelopeReference)
	if err != nil || storedEnvelope != envelope {
		return store.ExternalSupervisorAuthenticatedReceipt{}, authenticationError("durable predecessor projection")
	}
	return store.ExternalSupervisorAuthenticatedReceipt{
		SchemaVersion: store.ExternalSupervisorAuthenticatedReceiptSchemaVersion,
		Authorization: *request.Authorization, Delivery: *request.Delivery,
		DeliveryAuthentication: *response.DeliveryAuthentication,
		Receipt:                *response.Receipt, ReceiptAuthentication: *response.ReceiptAuthentication,
	}, nil
}

func (server *Server) verifyKnownReceipt(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) error {
	if receipt.SchemaVersion != store.ExternalSupervisorAuthenticatedReceiptSchemaVersion || receipt.Authorization != server.material.bundle.Authorization ||
		receipt.Delivery.TrustBundleHash != server.material.bundle.TrustBundleHash ||
		receipt.Delivery.PredecessorEnvelopeHash != envelope.EnvelopeHash ||
		receipt.Delivery.PredecessorIdempotencyKeyHash != envelope.IdempotencyKeyHash ||
		receipt.Delivery.AttemptNumber != envelope.AttemptNumber || receipt.Delivery.AttemptCap != envelope.AttemptCap ||
		receipt.Delivery.Deadline != envelope.Deadline || receipt.Delivery.RouteMappingHash != envelope.RouteMappingHash {
		return authenticationError("durable server receipt trust binding")
	}
	sealedDelivery, deliveryErr := store.SealExternalSupervisorSealedDelivery(receipt.Delivery)
	sealedReceipt, receiptErr := store.SealExternalSupervisorProtocolReceipt(receipt.Receipt)
	if deliveryErr != nil || receiptErr != nil || sealedDelivery != receipt.Delivery || sealedReceipt != receipt.Receipt ||
		receipt.Receipt.DeliveryHash != receipt.Delivery.DeliveryHash || receipt.Receipt.EnvelopeHash != envelope.EnvelopeHash ||
		receipt.Receipt.AttemptNumber != envelope.AttemptNumber || receipt.Receipt.RouteMappingHash != envelope.RouteMappingHash ||
		receipt.Receipt.ReleaseApprovalHash != receipt.Delivery.ReleaseApprovalHash ||
		receipt.Receipt.SignerKeySPKISHA256 != server.material.signerSPKI {
		return authenticationError("durable server receipt transitive binding")
	}
	deliveryAt, deliveryTimeErr := time.Parse(time.RFC3339Nano, receipt.Delivery.IssuedAt)
	receiptAt, receiptTimeErr := time.Parse(time.RFC3339Nano, receipt.Receipt.IssuedAt)
	expiresAt, expiryErr := time.Parse(time.RFC3339Nano, receipt.Delivery.DeliveryExpiresAt)
	if deliveryTimeErr != nil || receiptTimeErr != nil || expiryErr != nil || receiptAt.Before(deliveryAt) || !receiptAt.Before(expiresAt) {
		return authenticationError("durable server receipt temporal binding")
	}
	if err := server.material.verifier.verifyAuthorizationAt(ctx, envelope, receipt.Authorization, deliveryAt); err != nil {
		return err
	}
	if err := server.material.verifier.verifyAuthorizationAt(ctx, envelope, receipt.Authorization, receiptAt); err != nil {
		return err
	}
	if err := server.verifyServerMessage("delivery", receipt.Delivery.DeliveryHash, receipt.Delivery.NonceHash, receipt.Delivery.ChannelBindingHash, receipt.Delivery.IssuedAt, receipt.DeliveryAuthentication); err != nil {
		return err
	}
	if err := server.verifyServerMessage("receipt", receipt.Receipt.ReceiptHash, receipt.Receipt.NonceHash, receipt.Receipt.ChannelBindingHash, receipt.Receipt.IssuedAt, receipt.ReceiptAuthentication); err != nil {
		return err
	}
	root, _, err := server.material.verifier.rootAt(server.material.bundle.ReleaseRoots, receiptAt)
	if err != nil || receipt.Receipt.TrustRootID != root.RootID {
		return authenticationError("durable server receipt root")
	}
	return nil
}

func (server *Server) buildResponse(ctx context.Context, request wireRequest) (wireResponse, error) {
	if ctx.Err() != nil {
		return wireResponse{}, ErrDeadline
	}
	response := wireResponse{
		SchemaVersion: responseSchemaVersion, Operation: request.Operation, RequestHash: request.RequestHash,
		PeerSignerSPKISHA256: server.material.signerSPKI, Status: "accepted",
	}
	now := server.config.Now()
	switch request.Operation {
	case operationDeliver:
		delivery := *request.Delivery
		deliveryAuthentication, err := server.signMessage("delivery", delivery.DeliveryHash, delivery.NonceHash, delivery.ChannelBindingHash, request.RequestHash, delivery.IssuedAt)
		if err != nil {
			return wireResponse{}, err
		}
		root, err := server.signerAt(now)
		if err != nil {
			return wireResponse{}, err
		}
		receiptChannel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "receipt", request.ResponseNonceHash, delivery.DeliveryHash)
		if err != nil {
			return wireResponse{}, err
		}
		receipt, err := store.SealExternalSupervisorProtocolReceipt(store.ExternalSupervisorProtocolReceipt{
			SchemaVersion: store.ExternalSupervisorProtocolReceiptSchemaVersion,
			ReceiptID:     serverRecordID("acceptance_receipt", request.EnvelopeReference.DurableEnvelopeHash),
			DeliveryHash:  delivery.DeliveryHash, EnvelopeHash: request.EnvelopeReference.DurableEnvelopeHash,
			IssuedAt: now.Format(time.RFC3339Nano), NonceHash: request.ResponseNonceHash,
			AttemptNumber: delivery.AttemptNumber, ChannelBindingHash: receiptChannel,
			ReleaseApprovalHash: delivery.ReleaseApprovalHash, RouteMappingHash: delivery.RouteMappingHash,
			SignerKeySPKISHA256: server.material.signerSPKI, TrustRootID: root.RootID,
		})
		if err != nil {
			return wireResponse{}, ErrProtocol
		}
		receiptAuthentication, err := server.signMessage("receipt", receipt.ReceiptHash, receipt.NonceHash, receipt.ChannelBindingHash, request.RequestHash, receipt.IssuedAt)
		if err != nil {
			return wireResponse{}, err
		}
		response.DeliveryAuthentication = &deliveryAuthentication
		response.Receipt = &receipt
		response.ReceiptAuthentication = &receiptAuthentication
	case operationReconcile:
		receipt := *request.Receipt
		root, err := server.signerAt(now)
		if err != nil {
			return wireResponse{}, err
		}
		callbackChannel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "callback", request.ResponseNonceHash, receipt.Receipt.ReceiptHash)
		if err != nil {
			return wireResponse{}, err
		}
		evidenceHash, err := canonicalHash(map[string]any{
			"audit_state": "audit_not_run", "schema_version": store.ExternalSupervisorAuditNotRunResultSchemaVersion,
			"state": store.ExternalSupervisorWaitingForHumanState, "verification_state": "not_run",
		})
		if err != nil {
			return wireResponse{}, err
		}
		callback, err := store.SealExternalSupervisorProtocolCallback(store.ExternalSupervisorProtocolCallback{
			SchemaVersion: store.ExternalSupervisorProtocolCallbackSchemaVersion,
			CallbackID:    serverRecordID("audit_callback", receipt.Receipt.ReceiptHash),
			DeliveryHash:  receipt.Delivery.DeliveryHash, EnvelopeHash: request.EnvelopeReference.DurableEnvelopeHash,
			ReceiptHash: receipt.Receipt.ReceiptHash, RouteMappingHash: receipt.Receipt.RouteMappingHash,
			AttemptNumber: receipt.Receipt.AttemptNumber, CallbackChannelBindingHash: callbackChannel,
			IssuedAt: now.Format(time.RFC3339Nano), NonceHash: request.ResponseNonceHash, EvidenceHash: evidenceHash,
			ResultSchemaVersion: store.ExternalSupervisorAuditNotRunResultSchemaVersion,
			TerminalState:       store.ExternalSupervisorWaitingForHumanState,
			SignerKeySPKISHA256: server.material.signerSPKI, TrustRootID: root.RootID,
		})
		if err != nil {
			return wireResponse{}, ErrProtocol
		}
		callbackAuthentication, err := server.signMessage("callback", callback.CallbackHash, callback.NonceHash, callback.CallbackChannelBindingHash, request.RequestHash, callback.IssuedAt)
		if err != nil {
			return wireResponse{}, err
		}
		response.Callback = &callback
		response.CallbackAuthentication = &callbackAuthentication
	case operationCancel:
		receipt := *request.Receipt
		cancellation := *request.Cancellation
		root, err := server.signerAt(now)
		if err != nil {
			return wireResponse{}, err
		}
		channel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "cancellation", request.ResponseNonceHash, cancellation.CancellationIdentityHash)
		if err != nil {
			return wireResponse{}, err
		}
		acknowledgement, err := store.SealExternalSupervisorCancellationAcknowledgement(store.ExternalSupervisorCancellationAcknowledgement{
			SchemaVersion:     store.ExternalSupervisorCancellationAcknowledgementSchemaVersion,
			AcknowledgementID: serverRecordID("cancellation_acknowledgement", cancellation.CancellationIdentityHash),
			CancellationHash:  cancellation.CancellationIdentityHash, DeliveryHash: receipt.Delivery.DeliveryHash,
			EnvelopeHash: request.EnvelopeReference.DurableEnvelopeHash, ReceiptHash: receipt.Receipt.ReceiptHash,
			AttemptNumber: receipt.Receipt.AttemptNumber, ChannelBindingHash: channel, NonceHash: request.ResponseNonceHash,
			IssuedAt: now.Format(time.RFC3339Nano), SignerKeySPKISHA256: server.material.signerSPKI, TrustRootID: root.RootID,
		})
		if err != nil {
			return wireResponse{}, ErrProtocol
		}
		authentication, err := server.signMessage("cancellation", acknowledgement.AcknowledgementHash, acknowledgement.NonceHash, acknowledgement.ChannelBindingHash, request.RequestHash, acknowledgement.IssuedAt)
		if err != nil {
			return wireResponse{}, err
		}
		response.CancellationAcknowledgement = &acknowledgement
		response.CancellationAuthentication = &authentication
	default:
		return wireResponse{}, ErrProtocol
	}
	return response, nil
}

func (server *Server) signMessage(messageType, messageHash, nonceHash, channelHash, requestHash, issuedAtText string) (store.ExternalSupervisorMessageAuthentication, error) {
	issuedAt, err := time.Parse(time.RFC3339Nano, issuedAtText)
	if err != nil {
		return store.ExternalSupervisorMessageAuthentication{}, authenticationError("server signing time")
	}
	if _, err := server.signerAt(issuedAt); err != nil {
		return store.ExternalSupervisorMessageAuthentication{}, err
	}
	authentication := store.ExternalSupervisorMessageAuthentication{
		SchemaVersion: store.ExternalSupervisorMessageAuthenticationSchemaVersion,
		MessageType:   messageType, MessageHash: messageHash, NonceHash: nonceHash,
		ChannelBindingHash: channelHash, RequestHash: requestHash, IssuedAt: issuedAtText,
		SignerKeySPKISHA256: server.material.signerSPKI,
	}
	payload, err := canonicalMessageAuthenticationPayload(authentication)
	if err != nil {
		return store.ExternalSupervisorMessageAuthentication{}, ErrProtocol
	}
	signature := ed25519.Sign(server.material.privateKey, payload)
	authentication.Signature = "ed25519:" + hex.EncodeToString(signature)
	zeroBytes(signature)
	sealed, err := store.SealExternalSupervisorMessageAuthentication(authentication)
	if err != nil {
		return store.ExternalSupervisorMessageAuthentication{}, ErrProtocol
	}
	return sealed, nil
}

func (server *Server) verifyServerMessage(messageType, messageHash, nonceHash, channelHash, issuedAtText string, authentication store.ExternalSupervisorMessageAuthentication) error {
	sealed, err := store.SealExternalSupervisorMessageAuthentication(authentication)
	if err != nil || sealed != authentication || authentication.MessageType != messageType || authentication.MessageHash != messageHash ||
		authentication.NonceHash != nonceHash || authentication.ChannelBindingHash != channelHash || authentication.IssuedAt != issuedAtText ||
		authentication.SignerKeySPKISHA256 != server.material.signerSPKI || !protocolHashPattern.MatchString(authentication.RequestHash) {
		return authenticationError("server message authentication binding")
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, issuedAtText)
	if err != nil {
		return authenticationError("server message authentication time")
	}
	if _, err := server.signerAt(issuedAt); err != nil {
		return err
	}
	publicKey, err := parsePublicKey(server.material.bundle.SupervisorPeer.Certificate.SubjectPublicKey, server.material.signerSPKI)
	if err != nil {
		return authenticationError("server message public key")
	}
	if err := verifyMessageAuthenticationSignature(publicKey, authentication); err != nil {
		return authenticationError("server message possession")
	}
	return nil
}

func (server *Server) signerAt(at time.Time) (store.ExternalSupervisorTrustRootKey, error) {
	root, rootKey, err := server.material.verifier.rootAt(server.material.bundle.ReleaseRoots, at)
	if err != nil || root.RootID != server.material.bundle.SupervisorPeer.Certificate.IssuerRootID {
		return store.ExternalSupervisorTrustRootKey{}, authenticationError("server signer root")
	}
	certified, err := verifyCertificateAt(server.material.bundle.SupervisorPeer, "independent_supervisor_protocol_adapter", root, rootKey, at)
	if err != nil {
		return store.ExternalSupervisorTrustRootKey{}, authenticationError("server signer certificate")
	}
	privatePublic, ok := server.material.privateKey.Public().(ed25519.PublicKey)
	if !ok || !bytes.Equal(certified, privatePublic) {
		return store.ExternalSupervisorTrustRootKey{}, authenticationError("server signer key possession")
	}
	return root, nil
}

func serverRecordID(prefix, hash string) string {
	const hashPrefix = "sha256:"
	if len(hash) < len(hashPrefix)+16 || hash[:len(hashPrefix)] != hashPrefix {
		return prefix + "_invalid"
	}
	return fmt.Sprintf("%s_%s", prefix, hash[len(hashPrefix):len(hashPrefix)+16])
}
