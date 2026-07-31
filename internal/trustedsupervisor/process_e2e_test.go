package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
)

const (
	processSignedSupervisorHelper = "ANANKE_SIGNED_SUPERVISOR_PROCESS_HELPER"
	processSignedSupervisorDir    = "ANANKE_SIGNED_SUPERVISOR_PROCESS_DIR"
	processSignedSupervisorMode   = "ANANKE_SIGNED_SUPERVISOR_PROCESS_MODE"
	processSignedSupervisorCount  = "ANANKE_SIGNED_SUPERVISOR_PROCESS_COUNT"

	processRouteMappingHash      = "sha256:a468e940e5dd5752285b8aba2533109cfde2d8b259a007647ca6f431e0736603"
	processSourceSnapshotHash    = "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4"
	processSourceManifestHash    = "sha256:842188d5ce1e461839bf33fb50a4040a3bf9f2e44d94c31be640058f5765cc15"
	processEvidenceContractHash  = "sha256:9309381f36076c263c60d6ef3db5e93b52694d645ffbbef25a4d87dce6459a05"
	processExternalDeadline      = "2031-07-31T12:00:00Z"
	processRepositoryIdentity    = "github.com/yingliang-zhang/ananke"
	processClosedLifecycleOutput = `{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}`
	processServerSocketName      = "supervisor.sock"
	processServerBundleName      = "trust-bundle.json"
	processServerReadyName       = "ready.json"
	processServerTraceName       = "wire.ndjson"
)

type processServerReady struct {
	ProcessID  int    `json:"process_id"`
	SocketPath string `json:"socket_path"`
}

func TestProcessServerReadinessPublicationIsAtomic(t *testing.T) {
	directory := t.TempDir()
	want := processServerReady{ProcessID: 1234, SocketPath: filepath.Join(directory, processServerSocketName)}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishProcessServerReady(directory, encoded); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(directory, processServerReadyName))
	if err != nil {
		t.Fatal(err)
	}
	var got processServerReady
	if err := json.Unmarshal(contents, &got); err != nil || got != want {
		t.Fatalf("atomic readiness = %+v, %v; want %+v", got, err, want)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != processServerReadyName {
		t.Fatalf("atomic readiness retained staging files: %v", entries)
	}
}

type processSignedSupervisor struct {
	bundle     store.ExternalSupervisorTrustBundle
	bundlePath string
	command    *exec.Cmd
	directory  string
	mode       string
	output     bytes.Buffer
	pid        int32
	socketPath string
	tracePath  string
	waitErr    error
	waitOnce   sync.Once
}

type processProductionHandoff struct {
	envelope   store.ExternalSupervisorEnvelope
	fence      store.LaunchFence
	staleFence store.LaunchFence
}

func TestSignedUnixSupervisorProcessHelper(t *testing.T) {
	if os.Getenv(processSignedSupervisorHelper) != "1" {
		return
	}
	directory := os.Getenv(processSignedSupervisorDir)
	mode := os.Getenv(processSignedSupervisorMode)
	connections, err := strconv.Atoi(os.Getenv(processSignedSupervisorCount))
	if err != nil || connections < 1 || directory == "" || (mode != "strict" && mode != "impostor") {
		t.Fatalf("invalid signed-supervisor helper configuration: dir=%q mode=%q connections=%q", directory, mode, os.Getenv(processSignedSupervisorCount))
	}
	identityNamespace := ""
	if mode == "impostor" {
		identityNamespace = "impostor"
	}
	fixture := newProcessSignedAuthorizationFixture(t, time.Now().UTC().Truncate(time.Second), identityNamespace)
	server := processSignedUnixServer{
		fixture:   fixture,
		mode:      mode,
		receipts:  make(map[string]store.ExternalSupervisorAuthenticatedReceipt),
		tracePath: filepath.Join(directory, processServerTraceName),
	}
	if err := server.run(directory, connections); err != nil {
		t.Fatalf("serve signed Unix helper: %v", err)
	}
}

func TestProductionCommandStoreLifecycleSignedPeerEndToEnd(t *testing.T) {
	if os.Getenv(processSignedSupervisorHelper) == "1" {
		return
	}
	binary := buildProductionTransportCommand(t)
	server := startProcessSignedSupervisor(t, "strict", 4)
	databasePath := filepath.Join(t.TempDir(), "production-composition.sqlite")

	current := seedProcessProductionHandoff(t, databasePath, "current", false)
	assertFrozenProcessEnvelope(t, current.envelope)
	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "submit", "envelope": current.envelope, "fence": current.fence,
	})
	currentBoundary := loadProcessRecoveryBoundary(t, databasePath, current.envelope.HandoffID)
	assertProcessReceiptBoundary(t, currentBoundary, current.envelope, server.bundle)

	// A fresh command process and Store handle must authenticate and return the
	// exact durable receipt without opening a second delivery connection.
	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "submit", "envelope": current.envelope, "fence": current.fence,
	})
	if replayed := loadProcessRecoveryBoundary(t, databasePath, current.envelope.HandoffID); replayed.Receipt == nil || *replayed.Receipt != *currentBoundary.Receipt {
		t.Fatalf("restart receipt replay = %+v, want exact durable receipt %+v", replayed.Receipt, currentBoundary.Receipt)
	}

	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "recover", "handoff_id": current.envelope.HandoffID,
	})
	currentBoundary = loadProcessRecoveryBoundary(t, databasePath, current.envelope.HandoffID)
	if currentBoundary.Callback == nil || currentBoundary.Callback.Callback.EnvelopeHash != current.envelope.EnvelopeHash ||
		currentBoundary.Callback.Callback.ReceiptHash != currentBoundary.Receipt.Receipt.ReceiptHash ||
		currentBoundary.Callback.Callback.TerminalState != "completed" || currentBoundary.Cancellation != nil {
		t.Fatalf("reopened callback boundary lost exact durable bindings: %+v", currentBoundary)
	}

	reclaimed := seedProcessProductionHandoff(t, databasePath, "reclaimed", true)
	assertFrozenProcessEnvelope(t, reclaimed.envelope)
	if reclaimed.fence.FenceGeneration != reclaimed.staleFence.FenceGeneration+1 || reclaimed.envelope.AttemptNumber != 2 {
		t.Fatalf("reclaimed fixture fence/attempt = %+v/%d, stale=%+v", reclaimed.fence, reclaimed.envelope.AttemptNumber, reclaimed.staleFence)
	}

	// The complete former fence cannot stage the envelope bound to the reclaimed
	// current generation.
	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "submit", "envelope": reclaimed.envelope, "fence": reclaimed.staleFence,
	})
	assertProcessHandoffAbsent(t, databasePath, reclaimed.envelope.HandoffID)
	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "submit", "envelope": reclaimed.envelope, "fence": reclaimed.fence,
	})
	reclaimedBoundary := loadProcessRecoveryBoundary(t, databasePath, reclaimed.envelope.HandoffID)
	assertProcessReceiptBoundary(t, reclaimedBoundary, reclaimed.envelope, server.bundle)

	cancellation, err := store.SealExternalSupervisorCancellation(store.ExternalSupervisorCancellation{
		SchemaVersion: store.ExternalSupervisorCancellationSchemaVersion,
		HandoffID:     reclaimed.envelope.HandoffID, EnvelopeHash: reclaimed.envelope.EnvelopeHash,
		ReceiptIdentityHash: reclaimedBoundary.Receipt.Receipt.ReceiptHash, AttemptNumber: reclaimed.envelope.AttemptNumber,
	})
	if err != nil {
		t.Fatalf("seal production cancellation: %v", err)
	}
	// The stale full fence cannot reach the signed peer or become durable.
	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "cancel", "cancellation": cancellation, "fence": reclaimed.staleFence,
	})
	reclaimedBoundary = loadProcessRecoveryBoundary(t, databasePath, reclaimed.envelope.HandoffID)
	if reclaimedBoundary.Cancellation != nil {
		t.Fatalf("stale full fence persisted cancellation: %+v", reclaimedBoundary.Cancellation)
	}
	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "cancel", "cancellation": cancellation, "fence": reclaimed.fence,
	})
	reclaimedBoundary = loadProcessRecoveryBoundary(t, databasePath, reclaimed.envelope.HandoffID)
	if reclaimedBoundary.Cancellation == nil || reclaimedBoundary.Cancellation.Cancellation != cancellation || reclaimedBoundary.Callback != nil {
		t.Fatalf("reclaimed cancellation boundary lost exact durable binding: %+v", reclaimedBoundary)
	}

	// Cancellation and callback are mutually exclusive across another command
	// restart. Recovery must not contact the peer or infer a callback.
	runProductionTransportCommand(t, binary, server, databasePath, map[string]any{
		"operation": "recover", "handoff_id": reclaimed.envelope.HandoffID,
	})
	reclaimedBoundary = loadProcessRecoveryBoundary(t, databasePath, reclaimed.envelope.HandoffID)
	if reclaimedBoundary.Callback != nil || reclaimedBoundary.Cancellation == nil {
		t.Fatalf("recovery crossed cancellation conflict: %+v", reclaimedBoundary)
	}

	server.wait(t)
	frames := readProcessWireFrames(t, server.tracePath)
	assertProcessWireFrames(t, frames, server.socketPath, map[string]bool{
		current.envelope.EnvelopeHash: true, reclaimed.envelope.EnvelopeHash: true,
	}, map[string]int{operationDeliver: 2, operationReconcile: 1, operationCancel: 1})
	assertProcessDurableCounts(t, databasePath)
}

func TestProductionCommandRejectsSameUIDWrongKeyPathReplacement(t *testing.T) {
	if os.Getenv(processSignedSupervisorHelper) == "1" {
		return
	}
	binary := buildProductionTransportCommand(t)
	legitimate := startProcessSignedSupervisor(t, "strict", 1)
	impostor := startProcessSignedSupervisor(t, "impostor", 1)
	assertProcessImpostorIdentityPreconditions(t, legitimate, impostor)
	if err := os.Remove(legitimate.socketPath); err != nil {
		t.Fatalf("unlink legitimate socket path: %v", err)
	}
	if err := os.Rename(impostor.socketPath, legitimate.socketPath); err != nil {
		t.Fatalf("replace socket path with same-UID wrong-key peer: %v", err)
	}

	databasePath := filepath.Join(t.TempDir(), "path-replacement.sqlite")
	handoff := seedProcessProductionHandoff(t, databasePath, "replacement", false)
	assertFrozenProcessEnvelope(t, handoff.envelope)
	runProductionTransportCommandWithPID(t, binary, legitimate.bundlePath, legitimate.socketPath, impostor.pid, databasePath, map[string]any{
		"operation": "submit", "envelope": handoff.envelope, "fence": handoff.fence,
	})
	boundary := loadProcessRecoveryBoundary(t, databasePath, handoff.envelope.HandoffID)
	if boundary.Receipt != nil || boundary.Callback != nil || boundary.Cancellation != nil {
		t.Fatalf("same-UID wrong-key path replacement became durable authority: %+v", boundary)
	}
	impostor.wait(t)
	frames := readProcessWireFrames(t, impostor.tracePath)
	assertProcessWireFrames(t, frames, legitimate.socketPath, map[string]bool{handoff.envelope.EnvelopeHash: true}, map[string]int{operationDeliver: 1})
}
func assertProcessImpostorIdentityPreconditions(t *testing.T, legitimate, impostor *processSignedSupervisor) {
	t.Helper()
	legitimatePeer := legitimate.bundle.SupervisorPeer.Certificate
	impostorPeer := impostor.bundle.SupervisorPeer.Certificate
	if legitimatePeer.SubjectPublicKey == impostorPeer.SubjectPublicKey {
		t.Fatal("legitimate and impostor peer public keys unexpectedly match")
	}
	if legitimatePeer.SubjectKeySPKISHA256 == impostorPeer.SubjectKeySPKISHA256 {
		t.Fatal("legitimate and impostor peer SPKI hashes unexpectedly match")
	}
	if legitimatePeer.IssuerRootID == impostorPeer.IssuerRootID ||
		legitimate.bundle.ReleaseRoots.Active.RootID == impostor.bundle.ReleaseRoots.Active.RootID {
		t.Fatal("legitimate and impostor peer root identities unexpectedly match")
	}
	if legitimate.bundle.TrustBundleHash == impostor.bundle.TrustBundleHash {
		t.Fatal("legitimate and impostor trust-bundle hashes unexpectedly match")
	}
	for _, server := range []*processSignedSupervisor{legitimate, impostor} {
		bundleBytes, err := os.ReadFile(server.bundlePath)
		if err != nil {
			t.Fatalf("read %s public trust bundle precondition: %v", server.mode, err)
		}
		if bytes.Contains(bytes.ToLower(bundleBytes), []byte("private")) {
			t.Fatalf("%s helper exposed private data through its public trust bundle", server.mode)
		}
	}
}

func TestSeparateProcessSignedSupervisorIsTestOnly(t *testing.T) {
	command := exec.Command("go", "list", "-json", ".")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list trustedsupervisor package: %v", err)
	}
	var listed struct {
		GoFiles     []string
		TestGoFiles []string
	}
	if err := json.Unmarshal(output, &listed); err != nil {
		t.Fatalf("decode trustedsupervisor package listing: %v", err)
	}
	const serverSource = "process_e2e_test.go"
	if processListedFile(listed.GoFiles, serverSource) || !processListedFile(listed.TestGoFiles, serverSource) {
		t.Fatalf("signed process server build selection = production:%v tests:%v", listed.GoFiles, listed.TestGoFiles)
	}
}

type processSignedUnixServer struct {
	fixture   signedAuthorizationFixture
	mode      string
	receipts  map[string]store.ExternalSupervisorAuthenticatedReceipt
	tracePath string
}

func publishProcessServerReady(directory string, ready []byte) error {
	stagingPath := filepath.Join(directory, "."+processServerReadyName+".tmp")
	readyPath := filepath.Join(directory, processServerReadyName)
	file, err := os.OpenFile(stagingPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(stagingPath)
		}
	}()
	written, writeErr := file.Write(ready)
	if writeErr != nil {
		return writeErr
	}
	if written != len(ready) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagingPath, readyPath); err != nil {
		return err
	}
	cleanup = false
	return fsyncDirectory(directory)
}

func (server *processSignedUnixServer) run(directory string, connections int) error {
	socketPath := filepath.Join(directory, processServerSocketName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return err
	}
	bundle, err := marshalCanonical(server.fixture.bundle)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, processServerBundleName), bundle, 0o600); err != nil {
		return err
	}
	ready, err := json.Marshal(processServerReady{ProcessID: os.Getpid(), SocketPath: socketPath})
	if err != nil {
		return err
	}
	if err := publishProcessServerReady(directory, ready); err != nil {
		return err
	}
	for range connections {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		if err := server.serve(connection); err != nil {
			return err
		}
	}
	return nil
}

func (server *processSignedUnixServer) serve(connection net.Conn) error {
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	frame, err := readFrame(connection, maxFrameBytes)
	if err != nil {
		return err
	}
	if err := appendProcessWireFrame(server.tracePath, frame); err != nil {
		return err
	}
	var request wireRequest
	if err := decodeCanonical(frame, &request); err != nil {
		return err
	}
	if err := server.validateRequest(request); err != nil {
		return err
	}
	response, err := signedResponse(server.fixture, request, "ok")
	if err != nil {
		return err
	}
	if request.Operation == operationDeliver && response.DeliveryAuthentication != nil && response.Receipt != nil && response.ReceiptAuthentication != nil {
		server.receipts[request.EnvelopeReference.DurableEnvelopeHash] = store.ExternalSupervisorAuthenticatedReceipt{
			SchemaVersion: store.ExternalSupervisorAuthenticatedReceiptSchemaVersion,
			Authorization: *request.Authorization, Delivery: *request.Delivery,
			DeliveryAuthentication: *response.DeliveryAuthentication,
			Receipt:                *response.Receipt, ReceiptAuthentication: *response.ReceiptAuthentication,
		}
	}
	encoded, err := marshalCanonical(response)
	if err != nil {
		return err
	}
	return writeFrame(connection, encoded, maxFrameBytes)
}

func (server *processSignedUnixServer) validateRequest(request wireRequest) error {
	if request.SchemaVersion != requestSchemaVersion || request.EnvelopeReference == nil ||
		validateWireEnvelopeReference(*request.EnvelopeReference) != nil || !protocolHashPattern.MatchString(request.RequestNonceHash) ||
		!protocolHashPattern.MatchString(request.ResponseNonceHash) || !protocolHashPattern.MatchString(request.ChannelBindingHash) ||
		!protocolHashPattern.MatchString(request.RequestHash) {
		return ErrProtocol
	}
	payloadHash, err := requestPayloadHash(request)
	if err != nil {
		return err
	}
	expectedChannel, err := canonicalHash(map[string]any{
		"binding_schema_version": "ananke.local-unix-peer-channel-binding.v2",
		"peer_primary_group_id":  uint32(os.Getgid()),
		"peer_process_id":        os.Getpid(),
		"peer_user_id":           uint32(os.Getuid()),
		"request_payload_hash":   payloadHash,
	})
	if err != nil || request.ChannelBindingHash != expectedChannel {
		return ErrAuthentication
	}
	expectedRequestHash, err := hashWireRequest(request)
	if err != nil || request.RequestHash != expectedRequestHash {
		return ErrProtocol
	}
	if server.mode == "impostor" {
		if request.Operation != operationDeliver {
			return ErrProtocol
		}
		return nil
	}

	durableHash := request.EnvelopeReference.DurableEnvelopeHash
	switch request.Operation {
	case operationDeliver:
		if request.Authorization == nil || request.Delivery == nil || *request.Authorization != server.fixture.bundle.Authorization {
			return ErrAuthentication
		}
		delivery := *request.Delivery
		sealed, err := store.SealExternalSupervisorSealedDelivery(delivery)
		if err != nil || sealed != delivery || delivery.PredecessorEnvelopeHash != durableHash ||
			delivery.TrustBundleHash != server.fixture.bundle.TrustBundleHash || delivery.RouteMappingHash != processRouteMappingHash ||
			delivery.ReleaseAttestationHash != server.fixture.bundle.Authorization.ReleaseAttestation.AttestationHash ||
			delivery.ReleaseApprovalHash != server.fixture.bundle.Authorization.ReleaseApproval.ApprovalHash ||
			delivery.MoARoleGrantHash != server.fixture.bundle.Authorization.MoARoleGrant.GrantHash {
			return ErrAuthentication
		}
		expectedDeliveryChannel, err := deriveMessageChannelBinding(request.ChannelBindingHash, "delivery", delivery.NonceHash, request.EnvelopeReference.PredecessorProjection.PredecessorProjectionHash)
		if err != nil || delivery.ChannelBindingHash != expectedDeliveryChannel {
			return ErrAuthentication
		}
	case operationReconcile, operationCancel:
		known, found := server.receipts[durableHash]
		if !found || request.Receipt == nil || *request.Receipt != known || verifyProcessReceiptSignatures(server.fixture, known) != nil {
			return ErrAuthentication
		}
		if request.Operation == operationCancel {
			if request.Cancellation == nil {
				return ErrProtocol
			}
			sealed, err := store.SealExternalSupervisorCancellation(*request.Cancellation)
			if err != nil || sealed != *request.Cancellation || request.Cancellation.EnvelopeHash != durableHash ||
				request.Cancellation.ReceiptIdentityHash != known.Receipt.ReceiptHash {
				return ErrAuthentication
			}
		}
	default:
		return ErrProtocol
	}
	return nil
}

func verifyProcessReceiptSignatures(fixture signedAuthorizationFixture, receipt store.ExternalSupervisorAuthenticatedReceipt) error {
	peer := fixture.keys["peer"].Public().(ed25519.PublicKey)
	for _, authentication := range []store.ExternalSupervisorMessageAuthentication{receipt.DeliveryAuthentication, receipt.ReceiptAuthentication} {
		payload, err := canonicalMessageAuthenticationPayload(authentication)
		if err != nil {
			return err
		}
		signature, err := hex.DecodeString(strings.TrimPrefix(authentication.Signature, "ed25519:"))
		if err != nil || !ed25519.Verify(peer, payload, signature) {
			return ErrAuthentication
		}
	}
	return nil
}

func newProcessSignedAuthorizationFixture(t *testing.T, now time.Time, identityNamespace string) signedAuthorizationFixture {
	t.Helper()
	seedNamespace := ""
	rootNamespace := ""
	if identityNamespace != "" {
		seedNamespace = identityNamespace + ":"
		rootNamespace = "_" + identityNamespace
	}
	keys := make(map[string]ed25519.PrivateKey)
	for _, name := range []string{
		"release-active", "release-successor", "approval-active", "approval-successor",
		"moa-active", "moa-successor", "attestor", "approver", "grantor", "peer",
	} {
		digest := sha256.Sum256([]byte("ananke-process-test-ed25519:" + seedNamespace + name))
		privateKey := ed25519.NewKeyFromSeed(digest[:])
		keys[name] = privateKey
	}
	rootLifecycle := func(kind, activeName, successorName string) store.ExternalSupervisorTrustRootLifecycle {
		activeKey, successorKey := keys[activeName], keys[successorName]
		activeID, successorID := "ananke_"+kind+"_root"+rootNamespace+"_v1", "ananke_"+kind+"_root"+rootNamespace+"_v2"
		activeNotAfter, successorValidFrom := now.Add(4*time.Hour), now.Add(2*time.Hour)
		rotation, err := store.SealExternalSupervisorRootRotation(store.ExternalSupervisorRootRotation{
			SchemaVersion:               store.ExternalSupervisorRootRotationSchemaVersion,
			CrossSignatureReferenceHash: testHash(kind + "-process-cross-signature"), OldRootID: activeID, NewRootID: successorID,
			NewRootSPKISHA256: testSPKIHash(t, successorKey.Public().(ed25519.PublicKey)), NewRootValidFrom: successorValidFrom.Format(time.RFC3339Nano),
			OldRootNotAfter: activeNotAfter.Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("seal process %s rotation: %v", kind, err)
		}
		revocation, err := store.SealExternalSupervisorRootRevocation(store.ExternalSupervisorRootRevocation{
			SchemaVersion: store.ExternalSupervisorRootRevocationSchemaVersion, RevokedRootID: activeID, IssuerRootID: successorID,
			EffectiveAt: successorValidFrom.Format(time.RFC3339Nano), RevocationReasonClass: "key_compromise_or_policy_withdrawal",
		})
		if err != nil {
			t.Fatalf("seal process %s revocation: %v", kind, err)
		}
		return store.ExternalSupervisorTrustRootLifecycle{
			Active:    store.ExternalSupervisorTrustRootKey{RootID: activeID, PublicKey: publicKeyText(activeKey.Public().(ed25519.PublicKey)), SPKISHA256: testSPKIHash(t, activeKey.Public().(ed25519.PublicKey)), ValidFrom: now.Add(-2 * time.Hour).Format(time.RFC3339Nano), NotAfter: activeNotAfter.Format(time.RFC3339Nano)},
			Successor: store.ExternalSupervisorTrustRootKey{RootID: successorID, PublicKey: publicKeyText(successorKey.Public().(ed25519.PublicKey)), SPKISHA256: testSPKIHash(t, successorKey.Public().(ed25519.PublicKey)), ValidFrom: successorValidFrom.Format(time.RFC3339Nano), NotAfter: now.Add(24 * time.Hour).Format(time.RFC3339Nano)},
			Rotation:  rotation, RotationSignature: detachedTestSignature(t, activeKey, rotation),
			Revocation: revocation, RevocationSignature: detachedTestSignature(t, successorKey, revocation),
		}
	}
	bundle := store.ExternalSupervisorTrustBundle{
		SchemaVersion: store.ExternalSupervisorTrustBundleSchemaVersion,
		ReleaseRoots:  rootLifecycle("release", "release-active", "release-successor"),
		ApprovalRoots: rootLifecycle("approval", "approval-active", "approval-successor"),
		MoARoots:      rootLifecycle("moa_role_grant", "moa-active", "moa-successor"),
	}
	bundle.ReleaseAttestor = signedTestCertificate(t, "release_attestor", bundle.ReleaseRoots.Active.RootID, keys["release-active"], keys["attestor"], now)
	bundle.ReleaseApprover = signedTestCertificate(t, "release_approver", bundle.ApprovalRoots.Active.RootID, keys["approval-active"], keys["approver"], now)
	bundle.MoAGrantor = signedTestCertificate(t, "moa_grantor", bundle.MoARoots.Active.RootID, keys["moa-active"], keys["grantor"], now)
	bundle.SupervisorPeer = signedTestCertificate(t, "independent_supervisor_protocol_adapter", bundle.ReleaseRoots.Active.RootID, keys["release-active"], keys["peer"], now)

	predecessor := lifecycle.ExternalSupervisorPredecessorReleaseIdentity()
	attestation, err := store.SealExternalSupervisorReleaseAttestation(store.ExternalSupervisorReleaseAttestation{
		SchemaVersion:  store.ExternalSupervisorReleaseAttestationSchemaVersion,
		ArtifactSHA256: predecessor.SupervisorArtifactSHA256, BuildIdentityHash: predecessor.BuildIdentityHash,
		RouteMappingHash: processRouteMappingHash, ReleaseRootID: bundle.ReleaseRoots.Active.RootID,
		AttestorKeySPKISHA256: bundle.ReleaseAttestor.Certificate.SubjectKeySPKISHA256,
		IssuedAt:              now.Add(-30 * time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(90 * time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seal process release attestation: %v", err)
	}
	approval, err := store.SealExternalSupervisorReleaseApproval(store.ExternalSupervisorReleaseApproval{
		SchemaVersion: store.ExternalSupervisorReleaseApprovalSchemaVersion, ApprovalID: "independent_release_approval_process_001",
		ApproverRootID: bundle.ApprovalRoots.Active.RootID, ApproverKeySPKISHA256: bundle.ReleaseApprover.Certificate.SubjectKeySPKISHA256,
		AttestationHash: attestation.AttestationHash, RouteMappingHash: processRouteMappingHash, Decision: "approved",
		IssuedAt: now.Add(-20 * time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(80 * time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seal process release approval: %v", err)
	}
	grant, err := store.SealExternalSupervisorMoARoleGrant(store.ExternalSupervisorMoARoleGrant{
		SchemaVersion: store.ExternalSupervisorMoARoleGrantSchemaVersion, GrantID: "moa_remote_supervisor_runner_process_001",
		GranteeRole: "remote_supervisor_runner", GrantorRootID: bundle.MoARoots.Active.RootID,
		GrantorKeySPKISHA256:   bundle.MoAGrantor.Certificate.SubjectKeySPKISHA256,
		ReleaseAttestationHash: attestation.AttestationHash, ReleaseApprovalHash: approval.ApprovalHash,
		RouteMappingHash: processRouteMappingHash, IssuedAt: now.Add(-10 * time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(70 * time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seal process MoA grant: %v", err)
	}
	bundle.Authorization = store.ExternalSupervisorAuthorizationChain{
		ReleaseAttestation: attestation, ReleaseAttestationSignature: detachedTestSignature(t, keys["attestor"], attestation),
		ReleaseApproval: approval, ReleaseApprovalSignature: detachedTestSignature(t, keys["approver"], approval),
		MoARoleGrant: grant, MoARoleGrantSignature: detachedTestSignature(t, keys["grantor"], grant),
	}
	bundle, err = store.SealExternalSupervisorTrustBundle(bundle)
	if err != nil {
		t.Fatalf("seal process trust bundle: %v", err)
	}
	envelope, err := store.SealExternalSupervisorEnvelope(store.ExternalSupervisorEnvelope{
		SchemaVersion: store.ExternalSupervisorEnvelopeSchemaVersion, HandoffID: "remote_handoff_process_helper",
		IdempotencyKeyHash: testHash("process-helper-idempotency"), LaunchSpecHash: testHash("process-helper-launch"), FenceBindingHash: testHash("process-helper-fence"),
		Deadline: processExternalDeadline, AttemptNumber: 1, AttemptCap: 3, RouteMappingHash: processRouteMappingHash,
		SourceSnapshotHash: processSourceSnapshotHash, SourceManifestHash: processSourceManifestHash, RepositoryIdentity: processRepositoryIdentity,
		SupervisorArtifactSHA256: predecessor.SupervisorArtifactSHA256, BuildIdentityHash: predecessor.BuildIdentityHash,
		ReleaseAttestationHash: predecessor.ReleaseAttestationHash, ReleaseApprovalHash: predecessor.ReleaseApprovalHash,
		EvidenceContractHash: processEvidenceContractHash, EvidenceSchemaVersion: "ananke.remote-supervisor-evidence.v1",
	})
	if err != nil {
		t.Fatalf("seal process helper envelope: %v", err)
	}
	return signedAuthorizationFixture{bundle: bundle, envelope: envelope, keys: keys}
}

func startProcessSignedSupervisor(t *testing.T, mode string, connections int) *processSignedSupervisor {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "ananke-signed-process-")
	if err != nil {
		t.Fatalf("create signed process directory: %v", err)
	}
	server := &processSignedSupervisor{
		directory: directory, mode: mode,
		bundlePath: filepath.Join(directory, processServerBundleName),
		socketPath: filepath.Join(directory, processServerSocketName),
		tracePath:  filepath.Join(directory, processServerTraceName),
	}
	command := exec.Command(os.Args[0], "-test.run=^TestSignedUnixSupervisorProcessHelper$")
	command.Env = append(os.Environ(),
		processSignedSupervisorHelper+"=1",
		processSignedSupervisorDir+"="+directory,
		processSignedSupervisorMode+"="+mode,
		processSignedSupervisorCount+"="+strconv.Itoa(connections),
	)
	command.Stdout, command.Stderr = &server.output, &server.output
	server.command = command
	if err := command.Start(); err != nil {
		_ = os.RemoveAll(directory)
		t.Fatalf("start separate signed supervisor process: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	readyPath := filepath.Join(directory, processServerReadyName)
	for {
		contents, readErr := os.ReadFile(readyPath)
		if readErr == nil {
			var ready processServerReady
			if err := json.Unmarshal(contents, &ready); err != nil {
				t.Fatalf("decode signed process readiness: %v", err)
			}
			if ready.ProcessID != command.Process.Pid || ready.SocketPath != server.socketPath {
				t.Fatalf("signed process readiness = %+v, command pid=%d socket=%q", ready, command.Process.Pid, server.socketPath)
			}
			server.pid = int32(ready.ProcessID)
			break
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			t.Fatalf("read signed process readiness: %v", readErr)
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("signed process did not become ready: %s", server.output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	bundleBytes, err := os.ReadFile(server.bundlePath)
	if err != nil {
		t.Fatalf("read child public trust bundle: %v", err)
	}
	bundle, err := DecodeTrustBundle(bundleBytes)
	if err != nil {
		t.Fatalf("decode child public trust bundle: %v", err)
	}
	if bytes.Contains(bytes.ToLower(bundleBytes), []byte("private")) {
		t.Fatal("child-only private key field escaped into public trust bundle")
	}
	server.bundle = bundle
	t.Cleanup(func() {
		server.stop()
		_ = os.RemoveAll(directory)
	})
	return server
}

func (server *processSignedSupervisor) wait(t *testing.T) {
	t.Helper()
	server.waitOnce.Do(func() { server.waitErr = server.command.Wait() })
	if server.waitErr != nil {
		t.Fatalf("separate signed supervisor process: %v\n%s", server.waitErr, server.output.String())
	}
}

func (server *processSignedSupervisor) stop() {
	server.waitOnce.Do(func() {
		_ = server.command.Process.Kill()
		server.waitErr = server.command.Wait()
	})
}

func buildProductionTransportCommand(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "ananke-trusted-supervisor-transport")
	command := exec.Command("go", "build", "-o", binary, "./cmd/ananke-trusted-supervisor-transport")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build production transport command: %v\n%s", err, output)
	}
	return binary
}

func runProductionTransportCommand(t *testing.T, binary string, server *processSignedSupervisor, databasePath string, request any) {
	t.Helper()
	runProductionTransportCommandWithPID(t, binary, server.bundlePath, server.socketPath, server.pid, databasePath, request)
}

func runProductionTransportCommandWithPID(t *testing.T, binary, bundlePath, socketPath string, processID int32, databasePath string, request any) {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("encode production command invocation: %v", err)
	}
	command := exec.Command(binary,
		"--store", databasePath,
		"--socket", socketPath,
		"--peer-uid", strconv.Itoa(os.Getuid()),
		"--peer-pid", strconv.Itoa(int(processID)),
		"--trust-bundle", bundlePath,
		"--timeout", "5s",
	)
	command.Stdin = bytes.NewReader(encoded)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("production command invocation failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != processClosedLifecycleOutput {
		t.Fatalf("production command output = %s, want exact closed projection %s; stderr=%s", got, processClosedLifecycleOutput, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("production command wrote unexpected stderr: %s", stderr.String())
	}
}

func seedProcessProductionHandoff(t *testing.T, databasePath, suffix string, reclaim bool) processProductionHandoff {
	t.Helper()
	ctx := context.Background()
	journal, err := store.Open(databasePath)
	if err != nil {
		t.Fatalf("open production composition store: %v", err)
	}
	defer journal.Close()
	created, err := journal.CreateProposal(ctx, store.CreateProposalRequest{
		IdempotencyKey: "create_" + suffix, ProjectID: "project_" + suffix, WorkstreamID: "workstream_" + suffix,
		RevisionInput: store.RevisionInput{
			Task:               store.ProposalTask{Title: "Signed supervisor composition", Instructions: "Prove the identity-only production composition."},
			AcceptanceCriteria: []string{"Persist exact authenticated identities."},
			Policy: store.ProposalPolicy{
				Adapter:   store.ProposalAdapterPolicy{Access: "read_only", Kind: "omp_audit", Status: "future"},
				Authority: "deterministic", Budget: store.ProposalBudgetPolicy{Dimensions: []string{"deadline", "attempt_cap"}, Status: "future"}, ModelRole: "advisory_only",
			},
		},
	})
	if err != nil {
		t.Fatalf("create production composition proposal: %v", err)
	}
	if _, err := journal.DecideProposalApproval(ctx, store.DecideProposalApprovalRequest{
		IdempotencyKey: "approve_" + suffix, ApprovalID: created.ApprovalID, ProposalID: created.ProposalID,
		Revision: created.Revision, RevisionHash: created.RevisionHash, Decision: store.ApprovalStateApproved, Reason: "Approve signed process composition proof.",
	}); err != nil {
		t.Fatalf("approve production composition proposal: %v", err)
	}
	spec := store.LaunchSpec{
		SchemaVersion: "ananke.launch-spec.v1",
		Revision:      store.LaunchRevisionIdentity{ProposalID: created.ProposalID, Revision: created.Revision, RevisionHash: created.RevisionHash},
		Model:         store.LaunchModelSpec{Provider: "omp", Model: "omp_audit_model_v1"}, Deadline: processExternalDeadline, AttemptCap: 3,
		ReadOnlyScope:  store.LaunchReadOnlyScope{Access: "read_only", Retrieval: "sealed_contract_only", ScopeFingerprint: "sha256:6f3be7b6f4e6a30cb6534c3270ce7a5707ec5e6880448fa71835345c0b900f5b", Writes: "forbidden"},
		SealedContract: store.LaunchSealedContract{MaterializationHash: testHash("materialization-" + suffix), Nonce: "nonce:" + strings.Repeat("a", 64)},
		HostSpec: store.LaunchHostSpec{
			Capabilities:                []string{"bounded_cancellation", "read_only_retrieval", "reconnect_recovery", "shape_only_transcript", "verification"},
			ExecutableRouteFingerprint:  "sha256:567db67008692962eeee67d287efba8b8a556608f99fd6ad33241b3c75e7a769",
			HostSpecFingerprint:         "sha256:bb4ffef286273f9d7f436f22fb6e54086cf6b6e659b5e0f534c89e57708ee65b",
			RequiredFilesFingerprint:    "sha256:5eb1148b3040f89853cd40260c2b2d9d5f209da1d1a1ff86ae3ca1f1c3e21bfe",
			TranscriptSourceFingerprint: "sha256:6c4a0f37a2e9d85914b2d0e9f8e7c6b5a4d3f2e1c0b9a887766554433221100f",
			WorktreeLayoutFingerprint:   "sha256:8671bd82188905703b1c72972b1440b4a8d958e76e13424e2ed61940508ff4c0",
		},
		Transcript:   store.LaunchTranscriptSpec{Dialect: "omp_shape_v1", DialectFingerprint: "sha256:744a452214d4e35d470f2e503e62bb04f60fd43423ffb1aa234b9b1fa4422e50", Parse: "shape_only"},
		Verification: store.LaunchVerificationSpec{Kind: "read_only", VerificationCommandFingerprint: "sha256:59c5402d5fca337a8488d6baa0e5989c192666e15f57d4bbfd8563f1ce6006bf"},
	}
	launchSpecHash, err := store.HashLaunchSpec(spec)
	if err != nil {
		t.Fatalf("hash production launch spec: %v", err)
	}
	if _, err := journal.StoreLaunchSpec(ctx, store.LaunchAdmissionRequest{Spec: spec, LaunchSpecHash: launchSpecHash, ApprovalID: created.ApprovalID}); err != nil {
		t.Fatalf("store production launch spec: %v", err)
	}
	claim, err := journal.AcquireLaunchClaim(ctx, store.LaunchClaimRequest{
		LaunchSpecHash: launchSpecHash, ClaimID: "claim_" + suffix + "_initial", ClaimTokenHash: testHash("claim-token-" + suffix + "-initial"), OwnerID: "external_supervisor_runtime", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("acquire production launch claim: %v", err)
	}
	staleFence := claim.Fence
	if reclaim {
		claim, err = journal.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
			ExpectedFence: claim.Fence,
			Claim:         store.LaunchClaimRequest{LaunchSpecHash: launchSpecHash, ClaimID: "claim_" + suffix + "_reclaimed", ClaimTokenHash: testHash("claim-token-" + suffix + "-reclaimed"), OwnerID: "external_supervisor_runtime", Attempt: 2},
		})
		if err != nil {
			t.Fatalf("reclaim production launch claim: %v", err)
		}
	}
	materialization, err := journal.RecordLaunchMaterializationReady(ctx, store.LaunchMaterializationRequest{
		Fence: claim.Fence, MaterializationID: "materialization_" + suffix, MaterializationHash: spec.SealedContract.MaterializationHash, Nonce: spec.SealedContract.Nonce,
	})
	if err != nil {
		t.Fatalf("record production materialization: %v", err)
	}
	if _, err := journal.CreateLaunchRunIntent(ctx, store.LaunchRunIntentRequest{
		Fence: claim.Fence, MaterializationID: materialization.MaterializationID, RunID: "run_" + suffix, Attempt: claim.Attempt,
	}); err != nil {
		t.Fatalf("create production run intent: %v", err)
	}
	predecessor := lifecycle.ExternalSupervisorPredecessorReleaseIdentity()
	envelope, err := store.SealExternalSupervisorEnvelope(store.ExternalSupervisorEnvelope{
		SchemaVersion: store.ExternalSupervisorEnvelopeSchemaVersion, HandoffID: "remote_handoff_" + suffix,
		IdempotencyKeyHash: testHash("idempotency-" + suffix), LaunchSpecHash: launchSpecHash, FenceBindingHash: store.HashExternalSupervisorFenceBinding(claim.Fence),
		Deadline: processExternalDeadline, AttemptNumber: claim.Attempt, AttemptCap: spec.AttemptCap, RouteMappingHash: processRouteMappingHash,
		SourceSnapshotHash: processSourceSnapshotHash, SourceManifestHash: processSourceManifestHash, RepositoryIdentity: processRepositoryIdentity,
		SupervisorArtifactSHA256: predecessor.SupervisorArtifactSHA256, BuildIdentityHash: predecessor.BuildIdentityHash,
		ReleaseAttestationHash: predecessor.ReleaseAttestationHash, ReleaseApprovalHash: predecessor.ReleaseApprovalHash,
		EvidenceContractHash: processEvidenceContractHash, EvidenceSchemaVersion: "ananke.remote-supervisor-evidence.v1",
	})
	if err != nil {
		t.Fatalf("seal production composition envelope: %v", err)
	}
	return processProductionHandoff{envelope: envelope, fence: claim.Fence, staleFence: staleFence}
}

func assertFrozenProcessEnvelope(t *testing.T, envelope store.ExternalSupervisorEnvelope) {
	t.Helper()
	predecessor := lifecycle.ExternalSupervisorPredecessorReleaseIdentity()
	if envelope.SupervisorArtifactSHA256 != predecessor.SupervisorArtifactSHA256 || envelope.BuildIdentityHash != predecessor.BuildIdentityHash ||
		envelope.ReleaseAttestationHash != predecessor.ReleaseAttestationHash || envelope.ReleaseApprovalHash != predecessor.ReleaseApprovalHash ||
		envelope.RouteMappingHash != processRouteMappingHash || envelope.SourceSnapshotHash != processSourceSnapshotHash ||
		envelope.SourceManifestHash != processSourceManifestHash || envelope.EvidenceContractHash != processEvidenceContractHash {
		t.Fatalf("production envelope lost frozen release/policy pins: %+v", envelope)
	}
}

func assertProcessReceiptBoundary(t *testing.T, boundary store.ExternalSupervisorRecoveryBoundary, envelope store.ExternalSupervisorEnvelope, bundle store.ExternalSupervisorTrustBundle) {
	t.Helper()
	if boundary.Handoff.Envelope != envelope || boundary.Receipt == nil || boundary.Callback != nil || boundary.Cancellation != nil {
		t.Fatalf("authenticated receipt boundary = %+v, want exact receipt only", boundary)
	}
	receipt := boundary.Receipt
	if receipt.Authorization != bundle.Authorization || receipt.Delivery.PredecessorEnvelopeHash != envelope.EnvelopeHash ||
		receipt.Delivery.PredecessorIdempotencyKeyHash != envelope.IdempotencyKeyHash || receipt.Delivery.TrustBundleHash != bundle.TrustBundleHash ||
		receipt.Delivery.ReleaseAttestationHash != bundle.Authorization.ReleaseAttestation.AttestationHash ||
		receipt.Delivery.ReleaseApprovalHash != bundle.Authorization.ReleaseApproval.ApprovalHash ||
		receipt.Receipt.EnvelopeHash != envelope.EnvelopeHash || receipt.Receipt.ReleaseApprovalHash != bundle.Authorization.ReleaseApproval.ApprovalHash {
		t.Fatalf("authenticated receipt lost durable envelope or release pins: %+v", receipt)
	}
	if receipt.Authorization.ReleaseAttestation.AttestationHash == envelope.ReleaseAttestationHash || receipt.Authorization.ReleaseApproval.ApprovalHash == envelope.ReleaseApprovalHash {
		t.Fatal("dynamic signed release records replaced frozen predecessor pins")
	}
}

func loadProcessRecoveryBoundary(t *testing.T, databasePath, handoffID string) store.ExternalSupervisorRecoveryBoundary {
	t.Helper()
	journal, err := store.Open(databasePath)
	if err != nil {
		t.Fatalf("reopen production composition store: %v", err)
	}
	defer journal.Close()
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(context.Background(), handoffID)
	if err != nil {
		t.Fatalf("load production recovery boundary: %v", err)
	}
	return boundary
}

func assertProcessHandoffAbsent(t *testing.T, databasePath, handoffID string) {
	t.Helper()
	journal, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := journal.GetExternalSupervisorHandoff(context.Background(), handoffID); !errors.Is(err, store.ErrExternalSupervisorNotFound) {
		t.Fatalf("stale fence handoff error = %v, want %v", err, store.ErrExternalSupervisorNotFound)
	}
}

func assertProcessDurableCounts(t *testing.T, databasePath string) {
	t.Helper()
	journal, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for table, want := range map[string]int{
		"external_supervisor_handoffs": 2, "external_supervisor_receipts": 2,
		"external_supervisor_callbacks": 1, "external_supervisor_cancellations": 1,
		"external_supervisor_transport_replay": 6, "runs": 0,
	} {
		var got int
		if err := journal.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}

func appendProcessWireFrame(path string, frame []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(append([]byte(nil), frame...), '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readProcessWireFrames(t *testing.T, path string) [][]byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read process wire trace: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(contents), []byte{'\n'})
	frames := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if len(line) != 0 {
			frames = append(frames, append([]byte(nil), line...))
		}
	}
	return frames
}

func assertProcessWireFrames(t *testing.T, frames [][]byte, socketPath string, allowedEnvelopeHashes map[string]bool, wantOperations map[string]int) {
	t.Helper()
	gotOperations := make(map[string]int)
	for _, frame := range frames {
		assertNoAuthorityFields(t, frame, socketPath, processRepositoryIdentity)
		var request map[string]any
		if err := json.Unmarshal(frame, &request); err != nil {
			t.Fatal(err)
		}
		reference, ok := request["envelope_reference"].(map[string]any)
		if !ok {
			t.Fatalf("separate-process frame lacks hash-only envelope reference: %v", request)
		}
		durableHash, ok := reference["durable_envelope_hash"].(string)
		if !ok || !allowedEnvelopeHashes[durableHash] {
			t.Fatalf("separate-process frame durable envelope hash = %v", reference["durable_envelope_hash"])
		}
		operation := assertClosedWireEnvelopeReference(t, frame, durableHash)
		gotOperations[operation]++
	}
	if fmt.Sprint(gotOperations) != fmt.Sprint(wantOperations) {
		t.Fatalf("separate-process operations = %v, want %v", gotOperations, wantOperations)
	}
}

func processListedFile(files []string, want string) bool {
	for _, file := range files {
		if file == want {
			return true
		}
	}
	return false
}
