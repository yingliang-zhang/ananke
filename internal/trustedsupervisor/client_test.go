package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestUnixClientAuthenticatesDeliveryReceiptCallbackAndCancellation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newSignedAuthorizationFixture(t, now)
	server := startSignedUnixSupervisor(t, fixture, "ok", 3)
	client := newSignedTestClient(t, server.socketPath, server.pid, fixture.bundle, now, nil)

	receipt, err := client.Deliver(context.Background(), fixture.envelope)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if err := client.VerifyExternalSupervisorReceipt(context.Background(), fixture.envelope, receipt); err != nil {
		t.Fatalf("VerifyExternalSupervisorReceipt: %v", err)
	}
	callback, err := client.Reconcile(context.Background(), fixture.envelope, receipt)
	if err != nil || callback == nil {
		t.Fatalf("Reconcile = %+v, %v", callback, err)
	}
	if err := client.VerifyExternalSupervisorCallback(context.Background(), fixture.envelope, receipt, *callback); err != nil {
		t.Fatalf("VerifyExternalSupervisorCallback: %v", err)
	}
	cancellation, err := store.SealExternalSupervisorCancellation(store.ExternalSupervisorCancellation{
		SchemaVersion: store.ExternalSupervisorCancellationSchemaVersion,
		HandoffID:     fixture.envelope.HandoffID, EnvelopeHash: fixture.envelope.EnvelopeHash,
		ReceiptIdentityHash: receipt.Receipt.ReceiptHash, AttemptNumber: fixture.envelope.AttemptNumber,
	})
	if err != nil {
		t.Fatalf("seal cancellation: %v", err)
	}
	acknowledged, err := client.Cancel(context.Background(), fixture.envelope, receipt, cancellation)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := client.VerifyExternalSupervisorCancellation(context.Background(), fixture.envelope, receipt, acknowledged); err != nil {
		t.Fatalf("VerifyExternalSupervisorCancellation: %v", err)
	}
	server.wait(t)

	if receipt.Delivery.NonceHash == receipt.Receipt.NonceHash || receipt.Delivery.ChannelBindingHash == receipt.Receipt.ChannelBindingHash ||
		callback.Callback.NonceHash == receipt.Receipt.NonceHash || callback.Callback.CallbackChannelBindingHash == receipt.Receipt.ChannelBindingHash {
		t.Fatal("delivery, receipt, and callback did not retain separate nonce/channel evidence")
	}
	for name, evidence := range map[string]store.ExternalSupervisorMessageAuthentication{
		"delivery":     receipt.DeliveryAuthentication,
		"receipt":      receipt.ReceiptAuthentication,
		"callback":     callback.CallbackAuthentication,
		"cancellation": acknowledged.AcknowledgementAuthentication,
	} {
		if evidence.Signature == "" || evidence.SignatureHash == "" || evidence.ChannelBindingHash == "" || evidence.NonceHash == "" {
			t.Fatalf("%s authentication evidence incomplete: %+v", name, evidence)
		}
	}
	frames := server.frames()
	if len(frames) != 3 {
		t.Fatalf("wire frame count = %d, want delivery/reconcile/cancel", len(frames))
	}
	operations := make(map[string]bool, len(frames))
	for _, frame := range frames {
		assertNoAuthorityFields(t, frame, server.socketPath, fixture.envelope.RepositoryIdentity)
		operations[assertClosedWireEnvelopeReference(t, frame, fixture.envelope.EnvelopeHash)] = true
	}
	for _, operation := range []string{operationDeliver, operationReconcile, operationCancel} {
		if !operations[operation] {
			t.Fatalf("wire did not prove closed envelope reference for %s", operation)
		}
	}
}

func TestUnixClientNeverTransmitsSignedForbiddenAuthorizationIdentifierValues(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	validFixture := newSignedAuthorizationFixture(t, now)
	server := startSignedUnixSupervisor(t, validFixture, "ok", 1)
	validClient := newSignedTestClient(t, server.socketPath, server.pid, validFixture.bundle, now, nil)
	validReceipt, err := validClient.Deliver(context.Background(), validFixture.envelope)
	if err != nil {
		t.Fatalf("prepare valid receipt: %v", err)
	}
	server.wait(t)

	directory, err := os.MkdirTemp("/tmp", "ananke-forbidden-wire-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socketPath := filepath.Join(directory, "must-not-dial.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}

	dialCalls := 0
	wireAttempt := errors.New("forbidden authorization reached dial")
	for _, testCase := range forbiddenAuthorizationIdentifierCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, identifier := range signedAuthorizationIdentifierFields {
				t.Run(identifier.name, func(t *testing.T) {
					fixture := forgedSignedAuthorizationIdentifier(t, validFixture, identifier.field, testCase.value)
					forgedReceipt := rebindSignedReceiptAuthorization(t, validReceipt, fixture)
					cancellation, err := store.SealExternalSupervisorCancellation(store.ExternalSupervisorCancellation{
						SchemaVersion: store.ExternalSupervisorCancellationSchemaVersion,
						HandoffID:     fixture.envelope.HandoffID, EnvelopeHash: fixture.envelope.EnvelopeHash,
						ReceiptIdentityHash: forgedReceipt.Receipt.ReceiptHash, AttemptNumber: fixture.envelope.AttemptNumber,
					})
					if err != nil {
						t.Fatalf("seal forged-receipt cancellation: %v", err)
					}
					config := signedTestConfig(socketPath, int32(os.Getpid()), fixture.bundle, now, nil)
					config.DialContext = func(context.Context, string, string) (net.Conn, error) {
						dialCalls++
						return nil, wireAttempt
					}
					client, err := NewClient(config)
					if err != nil {
						t.Fatalf("construct denial client: %v", err)
					}
					for _, operation := range []struct {
						name string
						call func() error
					}{
						{name: operationDeliver, call: func() error { _, err := client.Deliver(context.Background(), fixture.envelope); return err }},
						{name: operationReconcile, call: func() error {
							_, err := client.Reconcile(context.Background(), fixture.envelope, forgedReceipt)
							return err
						}},
						{name: operationCancel, call: func() error {
							_, err := client.Cancel(context.Background(), fixture.envelope, forgedReceipt, cancellation)
							return err
						}},
					} {
						t.Run(operation.name, func(t *testing.T) {
							if err := operation.call(); !errors.Is(err, ErrAuthentication) {
								t.Fatalf("%s error = %v, want %v before transport", operation.name, err, ErrAuthentication)
							}
							if dialCalls != 0 {
								t.Fatalf("%s dial-call count = %d after forbidden signed %s value %q, want zero", operation.name, dialCalls, identifier.field, testCase.value)
							}
						})
					}
				})
			}
		})
	}
}

func TestWireAuthorityScannerRejectsFieldsAndValuesWithoutLegitimateFalsePositives(t *testing.T) {
	legitimate, err := json.Marshal(map[string]any{
		"schema_version":  "ananke.remote-supervisor-evidence.v1",
		"artifact_sha256": testHash("artifact"), "evidence_hash": testHash("evidence"),
		"public_key":              "ed25519:" + strings.Repeat("a", 64),
		"revocation_reason_class": "key_compromise_or_policy_withdrawal",
		"terminal_state":          "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if violation, err := wireAuthorityViolation(legitimate); err != nil || violation != "" {
		t.Fatalf("legitimate schema enums and hashes triggered wire scanner: violation=%q err=%v", violation, err)
	}
	for _, value := range []string{
		"https://approval.invalid/release", "raw_authority_001", "credential_secret_001", "private_key_001",
		"command_payload_001", "argv_payload_001", "environment_payload_001", "source_payload_001",
		"artifact_payload_001", "evidence_payload_001", "/private/operator/path",
	} {
		frame, err := json.Marshal(map[string]any{"opaque_value": value})
		if err != nil {
			t.Fatal(err)
		}
		if violation, err := wireAuthorityViolation(frame); err != nil || violation == "" {
			t.Fatalf("forbidden wire string %q was not detected: violation=%q err=%v", value, violation, err)
		}
	}
	for _, field := range []string{"command_payload", "credential_value", "private_key", "raw_source", "artifact_content", "evidence_contents", "socket_path"} {
		frame, err := json.Marshal(map[string]any{field: testHash(field)})
		if err != nil {
			t.Fatal(err)
		}
		if violation, err := wireAuthorityViolation(frame); err != nil || violation == "" {
			t.Fatalf("forbidden wire field %q was not detected: violation=%q err=%v", field, violation, err)
		}
	}
}

func rebindSignedReceiptAuthorization(t *testing.T, receipt store.ExternalSupervisorAuthenticatedReceipt, fixture signedAuthorizationFixture) store.ExternalSupervisorAuthenticatedReceipt {
	t.Helper()
	receipt.Authorization = fixture.bundle.Authorization
	receipt.Delivery.ReleaseApprovalHash = fixture.bundle.Authorization.ReleaseApproval.ApprovalHash
	receipt.Delivery.MoARoleGrantHash = fixture.bundle.Authorization.MoARoleGrant.GrantHash
	receipt.Delivery.TrustBundleHash = fixture.bundle.TrustBundleHash
	sealedDelivery, err := store.SealExternalSupervisorSealedDelivery(receipt.Delivery)
	if err != nil {
		t.Fatalf("rebind forged delivery: %v", err)
	}
	receipt.Delivery = sealedDelivery
	receipt.Receipt.DeliveryHash = sealedDelivery.DeliveryHash
	receipt.Receipt.ReleaseApprovalHash = fixture.bundle.Authorization.ReleaseApproval.ApprovalHash
	sealedReceipt, err := store.SealExternalSupervisorProtocolReceipt(receipt.Receipt)
	if err != nil {
		t.Fatalf("rebind forged receipt: %v", err)
	}
	receipt.Receipt = sealedReceipt
	peerKey := fixture.keys["peer"]
	receipt.DeliveryAuthentication = *signedMessageAuthentication(peerKey, "delivery", sealedDelivery.DeliveryHash, sealedDelivery.NonceHash, sealedDelivery.ChannelBindingHash, receipt.DeliveryAuthentication.RequestHash, sealedDelivery.IssuedAt)
	receipt.ReceiptAuthentication = *signedMessageAuthentication(peerKey, "receipt", sealedReceipt.ReceiptHash, sealedReceipt.NonceHash, sealedReceipt.ChannelBindingHash, receipt.ReceiptAuthentication.RequestHash, sealedReceipt.IssuedAt)
	return receipt
}

func TestUnixClientRejectsSameUIDImpostorAndSignatureDrift(t *testing.T) {
	for _, mode := range []string{"impostor_key", "signature_drift", "authentication_request_drift"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			fixture := newSignedAuthorizationFixture(t, now)
			server := startSignedUnixSupervisor(t, fixture, mode, 1)
			client := newSignedTestClient(t, server.socketPath, server.pid, fixture.bundle, now, nil)
			if _, err := client.Deliver(context.Background(), fixture.envelope); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("Deliver error = %v, want %v", err, ErrAuthentication)
			}
			server.wait(t)
		})
	}
}

func TestUnixClientRejectsPathReplacementEvenForSameUIDAndPID(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newSignedAuthorizationFixture(t, now)
	legitimate := startSignedUnixSupervisor(t, fixture, "accept_only", 1)
	impostor := startSignedUnixSupervisor(t, fixture, "accept_only", 1)
	config := signedTestConfig(legitimate.socketPath, legitimate.pid, fixture.bundle, now, nil)
	config.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if err := os.Remove(address); err != nil {
			return nil, err
		}
		if err := os.Rename(impostor.socketPath, address); err != nil {
			return nil, err
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Deliver(context.Background(), fixture.envelope); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("path replacement error = %v, want %v", err, ErrAuthentication)
	}
	legitimate.stop(t)
	impostor.wait(t)
}

func TestUnixClientRejectsReleasePinDriftBeforeDial(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newSignedAuthorizationFixture(t, now)
	server := startSignedUnixSupervisor(t, fixture, "accept_only", 1)
	client := newSignedTestClient(t, server.socketPath, server.pid, fixture.bundle, now, nil)
	drifted := fixture.envelope
	drifted.ReleaseApprovalHash = testHash("fresh-cli-pin-drift")
	drifted, err := store.SealExternalSupervisorEnvelope(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Deliver(context.Background(), drifted); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("release pin drift error = %v, want %v", err, ErrAuthentication)
	}
	server.stop(t)
}

func TestUnixClientBoundsAuthenticationHooksByExchangeDeadline(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newSignedAuthorizationFixture(t, now)
	server := startSignedUnixSupervisor(t, fixture, "ok", 1)
	hooks := blockingAuthenticationHooks{}
	config := signedTestConfig(server.socketPath, server.pid, fixture.bundle, now, hooks)
	config.Timeout = 50 * time.Millisecond
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := client.Deliver(context.Background(), fixture.envelope); !errors.Is(err, ErrDeadline) {
		t.Fatalf("hook deadline error = %v, want %v", err, ErrDeadline)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("hook deadline elapsed %s", elapsed)
	}
	server.wait(t)
}

func TestUnixClientRejectsUnknownPartialEmptyAndBoundaryFrames(t *testing.T) {
	for _, testCase := range []struct {
		mode string
		want error
	}{
		{mode: "unknown_field", want: ErrProtocol},
		{mode: "partial_frame", want: ErrProtocol},
		{mode: "empty_frame", want: ErrLimit},
		{mode: "oversized_frame", want: ErrLimit},
	} {
		t.Run(testCase.mode, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			fixture := newSignedAuthorizationFixture(t, now)
			server := startSignedUnixSupervisor(t, fixture, testCase.mode, 1)
			client := newSignedTestClient(t, server.socketPath, server.pid, fixture.bundle, now, nil)
			if _, err := client.Deliver(context.Background(), fixture.envelope); !errors.Is(err, testCase.want) {
				t.Fatalf("Deliver error = %v, want %v", err, testCase.want)
			}
			server.wait(t)
		})
	}

	now := time.Now().UTC().Truncate(time.Second)
	fixture := newSignedAuthorizationFixture(t, now)
	for _, limit := range []uint32{minFrameBytes, maxFrameBytes} {
		config := signedTestConfig(filepath.Join(t.TempDir(), "absent.sock"), int32(os.Getpid()), fixture.bundle, now, nil)
		config.MaxFrameBytes = limit
		if client, err := NewClient(config); err != nil || client == nil {
			t.Fatalf("frame boundary %d rejected: %v", limit, err)
		}
	}
	for _, limit := range []uint32{minFrameBytes - 1, maxFrameBytes + 1} {
		config := signedTestConfig(filepath.Join(t.TempDir(), "absent.sock"), int32(os.Getpid()), fixture.bundle, now, nil)
		config.MaxFrameBytes = limit
		if client, err := NewClient(config); err == nil || client != nil {
			t.Fatalf("out-of-bound frame limit %d accepted", limit)
		}
	}
}

type blockingAuthenticationHooks struct{}

func (blockingAuthenticationHooks) Authenticate(ctx context.Context, _ AuthenticationBoundary) error {
	<-ctx.Done()
	return ctx.Err()
}

type signedUnixSupervisor struct {
	listener   net.Listener
	done       chan struct{}
	err        error
	pid        int32
	socketPath string
	mu         sync.Mutex
	trace      [][]byte
}

func startSignedUnixSupervisor(t *testing.T, fixture signedAuthorizationFixture, mode string, connections int) *signedUnixSupervisor {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "ananke-signed-ts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socketPath := filepath.Join(directory, "supervisor.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}
	server := &signedUnixSupervisor{listener: listener, done: make(chan struct{}), pid: int32(os.Getpid()), socketPath: socketPath}
	go func() {
		defer close(server.done)
		defer listener.Close()
		for range connections {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				server.err = acceptErr
				return
			}
			if serveErr := server.serve(connection, fixture, mode); serveErr != nil {
				server.err = serveErr
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = server.listener.Close()
		select {
		case <-server.done:
		case <-time.After(time.Second):
		}
	})
	return server
}

func (server *signedUnixSupervisor) serve(connection net.Conn, fixture signedAuthorizationFixture, mode string) error {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if mode == "accept_only" {
		return nil
	}
	requestBytes, err := readFrame(connection, maxFrameBytes)
	if err != nil {
		return err
	}
	server.mu.Lock()
	server.trace = append(server.trace, append([]byte(nil), requestBytes...))
	server.mu.Unlock()
	var request wireRequest
	if err := decodeCanonical(requestBytes, &request); err != nil {
		return err
	}
	if mode == "partial_frame" {
		_, _ = connection.Write([]byte{0, 0, 0, 8, '{'})
		return nil
	}
	if mode == "empty_frame" {
		_, _ = connection.Write([]byte{0, 0, 0, 0})
		return nil
	}
	if mode == "oversized_frame" {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], maxFrameBytes+1)
		_, _ = connection.Write(header[:])
		return nil
	}
	response, err := signedResponse(fixture, request, mode)
	if err != nil {
		return err
	}
	responseBytes, err := marshalCanonical(response)
	if err != nil {
		return err
	}
	if mode == "unknown_field" {
		var value map[string]any
		if err := json.Unmarshal(responseBytes, &value); err != nil {
			return err
		}
		value["unknown"] = "rejected"
		responseBytes, err = marshalCanonical(value)
		if err != nil {
			return err
		}
	}
	return writeFrame(connection, responseBytes, maxFrameBytes)
}

func signedResponse(fixture signedAuthorizationFixture, request wireRequest, mode string) (wireResponse, error) {
	peerKey := fixture.keys["peer"]
	if mode == "impostor_key" {
		digest := sha256.Sum256([]byte("same-uid-impostor"))
		peerKey = ed25519.NewKeyFromSeed(digest[:])
	}
	response := wireResponse{
		SchemaVersion: responseSchemaVersion, Operation: request.Operation, RequestHash: request.RequestHash,
		PeerSignerSPKISHA256: fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256, Status: "accepted",
	}
	switch request.Operation {
	case operationDeliver:
		if request.EnvelopeReference == nil || request.Delivery == nil {
			return wireResponse{}, errors.New("delivery request missing envelope reference or delivery")
		}
		delivery := *request.Delivery
		response.DeliveryAuthentication = signedMessageAuthentication(peerKey, "delivery", delivery.DeliveryHash, delivery.NonceHash, delivery.ChannelBindingHash, request.RequestHash, delivery.IssuedAt)
		receiptChannel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "receipt", request.ResponseNonceHash, delivery.DeliveryHash)
		if err != nil {
			return wireResponse{}, err
		}
		receipt, err := store.SealExternalSupervisorProtocolReceipt(store.ExternalSupervisorProtocolReceipt{
			SchemaVersion: store.ExternalSupervisorProtocolReceiptSchemaVersion, ReceiptID: signedRecordID("acceptance_receipt", request.EnvelopeReference.DurableEnvelopeHash),
			DeliveryHash: delivery.DeliveryHash, EnvelopeHash: request.EnvelopeReference.DurableEnvelopeHash,
			RouteMappingHash: delivery.RouteMappingHash, ReleaseApprovalHash: delivery.ReleaseApprovalHash,
			AttemptNumber: delivery.AttemptNumber, ChannelBindingHash: receiptChannel,
			SignerKeySPKISHA256: fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
			TrustRootID:         fixture.bundle.ReleaseRoots.Active.RootID, NonceHash: request.ResponseNonceHash,
			IssuedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return wireResponse{}, err
		}
		response.Receipt = &receipt
		response.ReceiptAuthentication = signedMessageAuthentication(peerKey, "receipt", receipt.ReceiptHash, receipt.NonceHash, receipt.ChannelBindingHash, request.RequestHash, receipt.IssuedAt)
	case operationReconcile:
		if request.EnvelopeReference == nil || request.Receipt == nil {
			return wireResponse{}, errors.New("reconcile request missing envelope reference or receipt")
		}
		callbackChannel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "callback", request.ResponseNonceHash, request.Receipt.Receipt.ReceiptHash)
		if err != nil {
			return wireResponse{}, err
		}
		callback, err := store.SealExternalSupervisorProtocolCallback(store.ExternalSupervisorProtocolCallback{
			SchemaVersion: store.ExternalSupervisorProtocolCallbackSchemaVersion, CallbackID: signedRecordID("completion_callback", request.Receipt.Receipt.ReceiptHash),
			DeliveryHash: request.Receipt.Delivery.DeliveryHash, EnvelopeHash: request.EnvelopeReference.DurableEnvelopeHash,
			ReceiptHash: request.Receipt.Receipt.ReceiptHash, RouteMappingHash: request.Receipt.Receipt.RouteMappingHash,
			AttemptNumber: request.Receipt.Receipt.AttemptNumber, CallbackChannelBindingHash: callbackChannel,
			SignerKeySPKISHA256: fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
			TrustRootID:         fixture.bundle.ReleaseRoots.Active.RootID, NonceHash: request.ResponseNonceHash,
			IssuedAt: time.Now().UTC().Format(time.RFC3339Nano), EvidenceHash: testHash("callback-evidence"),
			ResultSchemaVersion: "ananke.independent-supervisor-result.v1", TerminalState: "completed",
		})
		if err != nil {
			return wireResponse{}, err
		}
		response.Callback = &callback
		response.CallbackAuthentication = signedMessageAuthentication(peerKey, "callback", callback.CallbackHash, callback.NonceHash, callback.CallbackChannelBindingHash, request.RequestHash, callback.IssuedAt)
	case operationCancel:
		if request.EnvelopeReference == nil || request.Receipt == nil || request.Cancellation == nil {
			return wireResponse{}, errors.New("cancel request missing durable envelope reference binding")
		}
		channel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "cancellation", request.ResponseNonceHash, request.Cancellation.CancellationIdentityHash)
		if err != nil {
			return wireResponse{}, err
		}
		acknowledgement, err := store.SealExternalSupervisorCancellationAcknowledgement(store.ExternalSupervisorCancellationAcknowledgement{
			SchemaVersion:     store.ExternalSupervisorCancellationAcknowledgementSchemaVersion,
			AcknowledgementID: signedRecordID("cancellation_acknowledgement", request.Cancellation.CancellationIdentityHash), CancellationHash: request.Cancellation.CancellationIdentityHash,
			DeliveryHash: request.Receipt.Delivery.DeliveryHash, EnvelopeHash: request.EnvelopeReference.DurableEnvelopeHash,
			ReceiptHash: request.Receipt.Receipt.ReceiptHash, AttemptNumber: request.Receipt.Receipt.AttemptNumber,
			ChannelBindingHash: channel, NonceHash: request.ResponseNonceHash, IssuedAt: time.Now().UTC().Format(time.RFC3339Nano),
			SignerKeySPKISHA256: fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
			TrustRootID:         fixture.bundle.ReleaseRoots.Active.RootID,
		})
		if err != nil {
			return wireResponse{}, err
		}
		response.CancellationAcknowledgement = &acknowledgement
		response.CancellationAuthentication = signedMessageAuthentication(peerKey, "cancellation", acknowledgement.AcknowledgementHash, acknowledgement.NonceHash, acknowledgement.ChannelBindingHash, request.RequestHash, acknowledgement.IssuedAt)
	default:
		return wireResponse{}, errors.New("unknown operation")
	}
	if mode == "signature_drift" {
		response.ReceiptAuthentication.Signature = signatureText(bytes.Repeat([]byte{0x44}, ed25519.SignatureSize))
	}
	if mode == "authentication_request_drift" && response.Receipt != nil {
		response.ReceiptAuthentication = signedMessageAuthentication(peerKey, "receipt", response.Receipt.ReceiptHash, response.Receipt.NonceHash, response.Receipt.ChannelBindingHash, testHash("other-request"), response.Receipt.IssuedAt)
	}
	return response, nil
}

func signedRecordID(prefix, hash string) string {
	const hashPrefix = "sha256:"
	if !strings.HasPrefix(hash, hashPrefix) || len(hash) < len(hashPrefix)+16 {
		panic("signed test record requires a SHA-256 identity")
	}
	return prefix + "_" + hash[len(hashPrefix):len(hashPrefix)+16]
}

func signedMessageAuthentication(key ed25519.PrivateKey, messageType, messageHash, nonceHash, channelHash, requestHash, issuedAt string) *store.ExternalSupervisorMessageAuthentication {
	evidence := store.ExternalSupervisorMessageAuthentication{
		SchemaVersion: store.ExternalSupervisorMessageAuthenticationSchemaVersion, MessageType: messageType,
		MessageHash: messageHash, NonceHash: nonceHash, ChannelBindingHash: channelHash, RequestHash: requestHash,
		IssuedAt: issuedAt, SignerKeySPKISHA256: mustSPKIHash(key.Public().(ed25519.PublicKey)),
	}
	canonical, err := canonicalMessageAuthenticationPayload(evidence)
	if err != nil {
		panic(err)
	}
	evidence.Signature = signatureText(ed25519.Sign(key, canonical))
	sealed, err := store.SealExternalSupervisorMessageAuthentication(evidence)
	if err != nil {
		panic(err)
	}
	return &sealed
}

func newSignedTestClient(t *testing.T, socketPath string, pid int32, bundle store.ExternalSupervisorTrustBundle, now time.Time, hooks AuthenticationHooks) *Client {
	t.Helper()
	client, err := NewClient(signedTestConfig(socketPath, pid, bundle, now, hooks))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func signedTestConfig(socketPath string, pid int32, bundle store.ExternalSupervisorTrustBundle, now time.Time, hooks AuthenticationHooks) Config {
	return Config{
		TrustBundle: bundle, ExpectedUserID: uint32(os.Getuid()), ExpectedProcessID: pid,
		ExpectedPredecessorReleaseIdentity: store.ExternalSupervisorPredecessorReleaseIdentity{
			SupervisorArtifactSHA256: bundle.Authorization.ReleaseAttestation.ArtifactSHA256,
			BuildIdentityHash:        bundle.Authorization.ReleaseAttestation.BuildIdentityHash,
			ReleaseAttestationHash:   testHash("predecessor-release-attestation"),
			ReleaseApprovalHash:      testHash("predecessor-release-approval"),
		},
		MaxFrameBytes: maxFrameBytes, SocketPath: socketPath, Timeout: time.Second,
		Now: func() time.Time { return now }, Authentication: hooks,
	}
}

func (server *signedUnixSupervisor) frames() [][]byte {
	server.mu.Lock()
	defer server.mu.Unlock()
	result := make([][]byte, len(server.trace))
	for index := range server.trace {
		result[index] = append([]byte(nil), server.trace[index]...)
	}
	return result
}

func (server *signedUnixSupervisor) wait(t *testing.T) {
	t.Helper()
	select {
	case <-server.done:
		if server.err != nil {
			t.Fatalf("signed Unix supervisor: %v", server.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("signed Unix supervisor did not exit")
	}
}

func (server *signedUnixSupervisor) stop(t *testing.T) {
	t.Helper()
	_ = server.listener.Close()
	select {
	case <-server.done:
	case <-time.After(time.Second):
		t.Fatal("signed Unix supervisor did not stop")
	}
}

func assertNoAuthorityFields(t *testing.T, frame []byte, forbiddenValues ...string) {
	t.Helper()
	violation, err := wireAuthorityViolation(frame, forbiddenValues...)
	if err != nil {
		t.Fatal(err)
	}
	if violation != "" {
		t.Fatal(violation)
	}
}

func wireAuthorityViolation(frame []byte, forbiddenValues ...string) (string, error) {
	var value any
	if err := json.Unmarshal(frame, &value); err != nil {
		return "", err
	}
	forbiddenFields := map[string]struct{}{
		"args": {}, "argv": {}, "artifact_content": {}, "artifact_payload": {}, "command": {}, "command_payload": {},
		"credential": {}, "credentials": {}, "credential_value": {}, "endpoint": {}, "env": {}, "environment": {},
		"evidence_contents": {}, "evidence_payload": {}, "path": {}, "pid": {}, "private_key": {}, "raw_artifact": {},
		"raw_evidence": {}, "raw_source": {}, "repository": {}, "repository_identity": {}, "secret": {}, "socket": {},
		"socket_path": {}, "source_contents": {}, "source_payload": {}, "uri": {}, "url": {},
	}
	forbiddenFragments := []string{"github.com/", "git@", "http://", "https://", "unix://", "/tmp/", "/private/"}
	forbiddenMarkers := []string{
		"argument", "argv", "artifact", "command", "credential", "environment", "evidence", "exec", "instruction",
		"password", "path", "privatekey", "prompt", "prose", "raw", "secret", "socket", "source", "token",
	}
	for _, forbidden := range forbiddenValues {
		if forbidden != "" {
			forbiddenFragments = append(forbiddenFragments, strings.ToLower(forbidden))
		}
	}
	var inspect func(any) string
	inspect = func(current any) string {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				loweredKey := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
				if _, found := forbiddenFields[loweredKey]; found {
					return fmt.Sprintf("authority-bearing field %q crossed sealed wire", key)
				}
				if violation := inspect(nested); violation != "" {
					return violation
				}
			}
		case []any:
			for _, nested := range typed {
				if violation := inspect(nested); violation != "" {
					return violation
				}
			}
		case string:
			lowered := strings.ToLower(typed)
			for _, forbidden := range forbiddenFragments {
				if strings.Contains(lowered, forbidden) {
					return fmt.Sprintf("authority-bearing string value %q crossed sealed wire", typed)
				}
			}
			if protocolHashPattern.MatchString(typed) || strings.HasPrefix(typed, "ed25519:") ||
				(strings.HasPrefix(lowered, "ananke.") && strings.Contains(lowered, ".v")) || lowered == "key_compromise_or_policy_withdrawal" {
				return ""
			}
			normalized := strings.Map(func(character rune) rune {
				if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
					return character
				}
				return -1
			}, lowered)
			for _, marker := range forbiddenMarkers {
				if strings.Contains(normalized, marker) {
					return fmt.Sprintf("authority-bearing string value %q crossed sealed wire", typed)
				}
			}
		}
		return ""
	}
	return inspect(value), nil
}

func assertClosedWireEnvelopeReference(t *testing.T, frame []byte, durableEnvelopeHash string) string {
	t.Helper()
	var request map[string]any
	if err := json.Unmarshal(frame, &request); err != nil {
		t.Fatal(err)
	}
	if _, found := request["envelope"]; found {
		t.Fatal("complete durable envelope crossed sealed wire")
	}
	reference, ok := request["envelope_reference"].(map[string]any)
	if !ok {
		t.Fatal("wire request omitted closed envelope reference")
	}
	exactFields := map[string]struct{}{
		"durable_envelope_hash": {}, "envelope_reference_hash": {}, "predecessor_projection": {}, "schema_version": {},
	}
	actualFields := make(map[string]struct{}, len(reference))
	for field := range reference {
		actualFields[field] = struct{}{}
	}
	if len(actualFields) != len(exactFields) {
		t.Fatalf("wire envelope reference field count = %d, want %d: %v", len(actualFields), len(exactFields), actualFields)
	}
	for field := range exactFields {
		if _, found := actualFields[field]; !found {
			t.Fatalf("wire envelope reference omitted frozen field %q: %v", field, actualFields)
		}
	}
	if reference["schema_version"] != wireEnvelopeReferenceSchemaVersion || reference["durable_envelope_hash"] != durableEnvelopeHash {
		t.Fatalf("wire envelope reference lost durable binding: %v", reference)
	}
	projection, ok := reference["predecessor_projection"].(map[string]any)
	if !ok {
		t.Fatalf("wire predecessor projection = %T", reference["predecessor_projection"])
	}
	projectionFields := []string{
		"schema_version", "envelope_schema_version", "handoff_id", "idempotency_key_hash", "launch_spec_hash",
		"fence_binding_hash", "deadline", "attempt_number", "attempt_cap", "route_mapping_hash", "source_snapshot_hash",
		"source_manifest_hash", "repository_identity_hash", "supervisor_artifact_sha256", "build_identity_hash",
		"release_attestation_hash", "release_approval_hash", "evidence_contract_hash", "evidence_schema_version",
		"envelope_hash", "predecessor_projection_hash",
	}
	if len(projection) != len(projectionFields) {
		t.Fatalf("wire predecessor projection field count = %d, want %d: %v", len(projection), len(projectionFields), projection)
	}
	for _, field := range projectionFields {
		if _, found := projection[field]; !found {
			t.Fatalf("wire predecessor projection omitted %q", field)
		}
	}
	if projection["schema_version"] != wirePredecessorProjectionSchemaVersion ||
		projection["envelope_schema_version"] != store.ExternalSupervisorEnvelopeSchemaVersion ||
		projection["evidence_schema_version"] != "ananke.remote-supervisor-evidence.v1" ||
		projection["envelope_hash"] != durableEnvelopeHash {
		t.Fatalf("wire predecessor projection lost fixed or durable binding: %v", projection)
	}
	projectionSelfHash, ok := projection["predecessor_projection_hash"].(string)
	if !ok {
		t.Fatalf("wire predecessor projection hash = %T", projection["predecessor_projection_hash"])
	}
	projectionInput := make(map[string]any, len(projection)-1)
	for field, fieldValue := range projection {
		if field != "predecessor_projection_hash" {
			projectionInput[field] = fieldValue
		}
	}
	canonicalProjection, err := canonicalHash(projectionInput)
	if err != nil || projectionSelfHash != canonicalProjection {
		t.Fatalf("wire predecessor projection hash = %q, want %q: %v", projectionSelfHash, canonicalProjection, err)
	}
	selfHash, ok := reference["envelope_reference_hash"].(string)
	if !ok {
		t.Fatalf("wire envelope reference self hash = %T", reference["envelope_reference_hash"])
	}
	hashInput := make(map[string]any, len(reference)-1)
	for field, fieldValue := range reference {
		if field != "envelope_reference_hash" {
			hashInput[field] = fieldValue
		}
	}
	canonical, err := canonicalHash(hashInput)
	if err != nil {
		t.Fatal(err)
	}
	if selfHash != canonical {
		t.Fatalf("wire envelope reference hash = %q, want canonical %q", selfHash, canonical)
	}
	operation, ok := request["operation"].(string)
	if !ok {
		t.Fatal("wire request operation is not a string")
	}
	switch operation {
	case operationDeliver:
		delivery, deliveryOK := request["delivery"].(map[string]any)
		channelHash, channelOK := request["channel_binding_hash"].(string)
		nonceHash, nonceOK := delivery["nonce_hash"].(string)
		if !deliveryOK || !channelOK || !nonceOK || delivery["predecessor_envelope_hash"] != durableEnvelopeHash {
			t.Fatalf("delivery lost envelope-reference predecessor binding: %v", request)
		}
		expectedChannel, err := deriveMessageChannelBinding(channelHash, "delivery", nonceHash, projectionSelfHash)
		if err != nil || delivery["channel_binding_hash"] != expectedChannel {
			t.Fatalf("delivery channel did not transitively bind envelope reference: %v, %v", delivery, err)
		}
	case operationReconcile, operationCancel:
		receipt, receiptOK := request["receipt"].(map[string]any)
		delivery, deliveryOK := receipt["delivery"].(map[string]any)
		protocolReceipt, protocolReceiptOK := receipt["receipt"].(map[string]any)
		if !receiptOK || !deliveryOK || !protocolReceiptOK || delivery["predecessor_envelope_hash"] != durableEnvelopeHash || protocolReceipt["envelope_hash"] != durableEnvelopeHash {
			t.Fatalf("%s lost durable receipt envelope-reference binding: %v", operation, request)
		}
		if operation == operationCancel {
			cancellation, cancellationOK := request["cancellation"].(map[string]any)
			if !cancellationOK || cancellation["envelope_hash"] != durableEnvelopeHash {
				t.Fatalf("cancellation lost envelope-reference binding: %v", request)
			}
		}
	default:
		t.Fatalf("unexpected wire operation %q", operation)
	}
	return operation
}

func mustSPKIHash(publicKey ed25519.PublicKey) string {
	hash, err := spkiHash(publicKey)
	if err != nil {
		panic(err)
	}
	return hash
}

func testHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		value = value[written:]
	}
	return nil
}
