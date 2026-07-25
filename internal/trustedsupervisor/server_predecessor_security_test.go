package trustedsupervisor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestRepositoryPolicyResolvesGenericExactMappings(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	policy, err := loadRepositoryPolicy(material.repositoryPolicyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatalf("load repository policy: %v", err)
	}
	for _, identity := range []string{material.fixture.envelope.RepositoryIdentity, "code.example/operator/second-repository"} {
		resolved, err := policy.Resolve(repositoryIdentityHash(identity))
		if err != nil || resolved != identity {
			t.Fatalf("resolve %q = %q, %v", identity, resolved, err)
		}
	}
	if _, err := policy.Resolve(testHash("unknown-repository")); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("unknown repository hash error = %v, want %v", err, ErrAuthentication)
	}
}

func TestRepositoryPolicyRejectsDuplicateHashSymlinkModeAndReplacement(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	identity := material.fixture.envelope.RepositoryIdentity

	t.Run("duplicate hash", func(t *testing.T) {
		path := writeServerRepositoryPolicy(t, material.directory, "duplicate-repository-policy.json", identity, identity)
		if _, err := loadRepositoryPolicy(path, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("duplicate repository hash error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		path := filepath.Join(material.directory, "repository-policy-link.json")
		if err := os.Symlink(material.repositoryPolicyPath, path); err != nil {
			t.Fatal(err)
		}
		if _, err := loadRepositoryPolicy(path, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("repository policy symlink error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("mode", func(t *testing.T) {
		path := writeServerRepositoryPolicy(t, material.directory, "wide-repository-policy.json", identity)
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := loadRepositoryPolicy(path, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("repository policy mode error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("replacement", func(t *testing.T) {
		path := writeServerRepositoryPolicy(t, material.directory, "replaceable-repository-policy.json", identity)
		policy, err := loadRepositoryPolicy(path, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path, path+".pinned"); err != nil {
			t.Fatal(err)
		}
		writeServerRepositoryPolicy(t, material.directory, filepath.Base(path), identity)
		if _, err := policy.Resolve(repositoryIdentityHash(identity)); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("repository policy replacement error = %v, want %v", err, ErrAuthentication)
		}
	})
}

func TestProductionServerRejectsRepositoryPolicyReplacementBeforeJournal(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	running := startInProcessProductionServer(t, material, now)
	defer running.stop(t)
	if err := os.Rename(material.repositoryPolicyPath, material.repositoryPolicyPath+".pinned"); err != nil {
		t.Fatal(err)
	}
	writeServerRepositoryPolicy(t, material.directory, filepath.Base(material.repositoryPolicyPath), material.fixture.envelope.RepositoryIdentity)
	request := validServerDeliveryRequest(t, material.fixture, now)
	assertServerRejectsRequestWithoutJournal(t, material, request, 0)
}

func TestProductionServerRejectsEveryCanonicalPredecessorProjectionMutationBeforeJournal(t *testing.T) {
	mutations := []struct {
		name             string
		mutate           func(*wirePredecessorProjection)
		resealProjection bool
	}{
		{"projection_schema", func(value *wirePredecessorProjection) {
			value.SchemaVersion = "ananke.local-trusted-supervisor-predecessor-projection.v2"
		}, true},
		{"envelope_schema", func(value *wirePredecessorProjection) {
			value.EnvelopeSchemaVersion = "ananke.remote-supervisor-sealed-launch-envelope.v2"
		}, true},
		{"handoff_id", func(value *wirePredecessorProjection) { value.HandoffID = "remote_handoff_forged_002" }, true},
		{"idempotency", func(value *wirePredecessorProjection) { value.IdempotencyKeyHash = testHash("forged-idempotency") }, true},
		{"launch_spec", func(value *wirePredecessorProjection) { value.LaunchSpecHash = testHash("forged-launch-spec") }, true},
		{"fence", func(value *wirePredecessorProjection) { value.FenceBindingHash = testHash("forged-fence") }, true},
		{"deadline", func(value *wirePredecessorProjection) { value.Deadline = "2027-01-01T00:00:00Z" }, true},
		{"attempt_number", func(value *wirePredecessorProjection) { value.AttemptNumber = 2 }, true},
		{"attempt_cap", func(value *wirePredecessorProjection) { value.AttemptCap = 4 }, true},
		{"route", func(value *wirePredecessorProjection) { value.RouteMappingHash = testHash("forged-route") }, true},
		{"source_snapshot", func(value *wirePredecessorProjection) { value.SourceSnapshotHash = testHash("forged-source-snapshot") }, true},
		{"source_manifest", func(value *wirePredecessorProjection) { value.SourceManifestHash = testHash("forged-source-manifest") }, true},
		{"repository_identity_hash", func(value *wirePredecessorProjection) { value.RepositoryIdentityHash = testHash("unknown-repository") }, true},
		{"artifact", func(value *wirePredecessorProjection) { value.SupervisorArtifactSHA256 = testHash("forged-artifact") }, true},
		{"build", func(value *wirePredecessorProjection) { value.BuildIdentityHash = testHash("forged-build") }, true},
		{"predecessor_attestation", func(value *wirePredecessorProjection) {
			value.ReleaseAttestationHash = testHash("forged-predecessor-attestation")
		}, true},
		{"predecessor_approval", func(value *wirePredecessorProjection) {
			value.ReleaseApprovalHash = testHash("forged-predecessor-approval")
		}, true},
		{"evidence_contract", func(value *wirePredecessorProjection) {
			value.EvidenceContractHash = testHash("forged-evidence-contract")
		}, true},
		{"evidence_schema", func(value *wirePredecessorProjection) {
			value.EvidenceSchemaVersion = "ananke.remote-supervisor-evidence.v2"
		}, true},
		{"envelope_hash", func(value *wirePredecessorProjection) { value.EnvelopeHash = testHash("forged-envelope") }, true},
		{"projection_hash", func(value *wirePredecessorProjection) {
			value.PredecessorProjectionHash = testHash("forged-projection")
		}, false},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			material := newServerTestMaterial(t, now)
			running := startInProcessProductionServer(t, material, now)
			defer running.stop(t)
			request := validServerDeliveryRequest(t, material.fixture, now)
			mutation.mutate(&request.EnvelopeReference.PredecessorProjection)
			rebindForgedServerRequest(t, &request, mutation.resealProjection)
			assertServerRejectsRequestWithoutJournal(t, material, request, 0)
		})
	}
}

func TestProductionServerRejectsFullyResealedFrozenPredecessorPinForgeries(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*store.ExternalSupervisorEnvelope)
	}{
		{"release_attestation", func(envelope *store.ExternalSupervisorEnvelope) {
			envelope.ReleaseAttestationHash = testHash("fully-resealed-predecessor-attestation")
		}},
		{"release_approval", func(envelope *store.ExternalSupervisorEnvelope) {
			envelope.ReleaseApprovalHash = testHash("fully-resealed-predecessor-approval")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			material := newServerTestMaterial(t, now)
			forgedFixture := material.fixture
			testCase.mutate(&forgedFixture.envelope)
			sealed, err := store.SealExternalSupervisorEnvelope(forgedFixture.envelope)
			if err != nil {
				t.Fatal(err)
			}
			forgedFixture.envelope = sealed
			running := startInProcessProductionServer(t, material, now)
			defer running.stop(t)
			request := validServerDeliveryRequest(t, forgedFixture, now)
			assertRequestHasNoRawRepositoryIdentity(t, request, forgedFixture.envelope.RepositoryIdentity)
			assertServerRejectsRequestWithoutJournal(t, material, request, 0)
		})
	}
}

func TestProductionServerRejectsReconcileAndCancellationBindingDrift(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		operation string
		mutate    func(*wireRequest)
	}{
		{"reconcile_handoff", operationReconcile, func(request *wireRequest) {
			request.EnvelopeReference.PredecessorProjection.HandoffID = "remote_handoff_reconcile_forged"
		}},
		{"reconcile_attempt", operationReconcile, func(request *wireRequest) { request.EnvelopeReference.PredecessorProjection.AttemptNumber++ }},
		{"reconcile_receipt_identity", operationReconcile, func(request *wireRequest) { request.Receipt.Receipt.ReceiptHash = testHash("forged-receipt") }},
		{"reconcile_envelope", operationReconcile, func(request *wireRequest) {
			request.EnvelopeReference.PredecessorProjection.EnvelopeHash = testHash("forged-envelope")
		}},
		{"cancel_handoff", operationCancel, func(request *wireRequest) {
			request.Cancellation.HandoffID = "remote_handoff_cancel_forged"
			resealCancellationForTest(t, request.Cancellation)
		}},
		{"cancel_attempt", operationCancel, func(request *wireRequest) {
			request.Cancellation.AttemptNumber++
			resealCancellationForTest(t, request.Cancellation)
		}},
		{"cancel_receipt_identity", operationCancel, func(request *wireRequest) {
			request.Cancellation.ReceiptIdentityHash = testHash("forged-receipt")
			resealCancellationForTest(t, request.Cancellation)
		}},
		{"cancel_envelope", operationCancel, func(request *wireRequest) {
			request.Cancellation.EnvelopeHash = testHash("forged-envelope")
			resealCancellationForTest(t, request.Cancellation)
		}},
		{"reconcile_operation_shape", operationReconcile, func(request *wireRequest) {
			cancellation := validCancellationForTest(t, materialEnvelope(request), *request.Receipt)
			request.Cancellation = &cancellation
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			material := newServerTestMaterial(t, now)
			running := startInProcessProductionServer(t, material, now)
			defer running.stop(t)
			client := newServerTestClient(t, material, int32(os.Getpid()), now)
			receipt, err := client.Deliver(context.Background(), material.fixture.envelope)
			if err != nil {
				t.Fatalf("seed delivery: %v", err)
			}
			request := validServerFollowupRequest(t, testCase.operation, material.fixture.envelope, receipt)
			testCase.mutate(&request)
			resealProjection := testCase.name == "reconcile_handoff" || testCase.name == "reconcile_attempt" || testCase.name == "reconcile_envelope"
			rebindForgedServerRequest(t, &request, resealProjection)
			assertServerRejectsRequestWithoutJournal(t, material, request, 1)
		})
	}
}

func TestProductionServerMakesReconcileAndCancelReceiptExclusive(t *testing.T) {
	for _, firstOperation := range []string{operationReconcile, operationCancel} {
		t.Run(firstOperation+"_first", func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			material := newServerTestMaterial(t, now)
			running := startInProcessProductionServer(t, material, now)
			defer running.stop(t)
			client := newServerTestClient(t, material, int32(os.Getpid()), now)
			receipt, err := client.Deliver(context.Background(), material.fixture.envelope)
			if err != nil {
				t.Fatal(err)
			}
			cancellation := validCancellationForTest(t, material.fixture.envelope, receipt)
			if firstOperation == operationReconcile {
				if callback, err := client.Reconcile(context.Background(), material.fixture.envelope, receipt); err != nil || callback == nil {
					t.Fatalf("first reconcile = %+v, %v", callback, err)
				}
				if _, err := client.Cancel(context.Background(), material.fixture.envelope, receipt, cancellation); err == nil {
					t.Fatal("cancel after reconcile did not conflict")
				}
			} else {
				if _, err := client.Cancel(context.Background(), material.fixture.envelope, receipt, cancellation); err != nil {
					t.Fatalf("first cancel: %v", err)
				}
				if _, err := client.Reconcile(context.Background(), material.fixture.envelope, receipt); err == nil {
					t.Fatal("reconcile after cancel did not conflict")
				}
			}
			assertServerJournalRows(t, material.journalPath, 2)
		})
	}
}

func TestProductionServerConflictsDifferentlySealedHandoffIDsForOneReceipt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	running := startInProcessProductionServer(t, material, now)
	defer running.stop(t)
	client := newServerTestClient(t, material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	cancellation := validCancellationForTest(t, material.fixture.envelope, receipt)
	if _, err := client.Cancel(context.Background(), material.fixture.envelope, receipt, cancellation); err != nil {
		t.Fatalf("seed cancellation: %v", err)
	}
	request := validServerFollowupRequest(t, operationCancel, material.fixture.envelope, receipt)
	request.Cancellation.HandoffID = "remote_handoff_differently_sealed"
	resealCancellationForTest(t, request.Cancellation)
	rebindForgedServerRequest(t, &request, false)
	assertServerRejectsRequestWithoutJournal(t, material, request, 2)
}

func validServerDeliveryRequest(t *testing.T, fixture signedAuthorizationFixture, now time.Time) wireRequest {
	t.Helper()
	reference, err := sealWireEnvelopeReference(fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	delivery := store.ExternalSupervisorSealedDelivery{
		SchemaVersion: store.ExternalSupervisorSealedDeliverySchemaVersion,
		AttemptCap:    fixture.envelope.AttemptCap, AttemptNumber: fixture.envelope.AttemptNumber,
		Deadline: fixture.envelope.Deadline, DeliveryExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
		DeliveryID: deliveryID(fixture.envelope.EnvelopeHash), IssuedAt: now.Format(time.RFC3339Nano),
		MoARoleGrantHash: fixture.bundle.Authorization.MoARoleGrant.GrantHash, NonceHash: testHash("forged-delivery-nonce"),
		PredecessorEnvelopeHash: fixture.envelope.EnvelopeHash, PredecessorIdempotencyKeyHash: fixture.envelope.IdempotencyKeyHash,
		ReleaseApprovalHash:    fixture.bundle.Authorization.ReleaseApproval.ApprovalHash,
		ReleaseAttestationHash: fixture.bundle.Authorization.ReleaseAttestation.AttestationHash,
		RouteMappingHash:       fixture.envelope.RouteMappingHash, TrustBundleHash: fixture.bundle.TrustBundleHash,
	}
	request := wireRequest{SchemaVersion: requestSchemaVersion, Operation: operationDeliver, EnvelopeReference: &reference,
		Authorization: &fixture.bundle.Authorization, Delivery: &delivery,
		RequestNonceHash: testHash("forged-request-nonce"), ResponseNonceHash: testHash("forged-response-nonce")}
	rebindForgedServerRequest(t, &request, true)
	return request
}

func validServerFollowupRequest(t *testing.T, operation string, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) wireRequest {
	t.Helper()
	reference, err := sealWireEnvelopeReference(envelope)
	if err != nil {
		t.Fatal(err)
	}
	request := wireRequest{SchemaVersion: requestSchemaVersion, Operation: operation, EnvelopeReference: &reference, Receipt: &receipt,
		RequestNonceHash: testHash("followup-request-" + operation), ResponseNonceHash: testHash("followup-response-" + operation)}
	if operation == operationCancel {
		cancellation := validCancellationForTest(t, envelope, receipt)
		request.Cancellation = &cancellation
	}
	rebindForgedServerRequest(t, &request, true)
	return request
}

func validCancellationForTest(t *testing.T, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) store.ExternalSupervisorCancellation {
	t.Helper()
	cancellation, err := store.SealExternalSupervisorCancellation(store.ExternalSupervisorCancellation{
		SchemaVersion: store.ExternalSupervisorCancellationSchemaVersion, HandoffID: envelope.HandoffID,
		EnvelopeHash: envelope.EnvelopeHash, ReceiptIdentityHash: receipt.Receipt.ReceiptHash, AttemptNumber: envelope.AttemptNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cancellation
}

func resealCancellationForTest(t *testing.T, cancellation *store.ExternalSupervisorCancellation) {
	t.Helper()
	sealed, err := store.SealExternalSupervisorCancellation(*cancellation)
	if err != nil {
		t.Fatal(err)
	}
	*cancellation = sealed
}

func materialEnvelope(request *wireRequest) store.ExternalSupervisorEnvelope {
	projection := request.EnvelopeReference.PredecessorProjection
	return store.ExternalSupervisorEnvelope{SchemaVersion: projection.EnvelopeSchemaVersion, HandoffID: projection.HandoffID,
		IdempotencyKeyHash: projection.IdempotencyKeyHash, LaunchSpecHash: projection.LaunchSpecHash, FenceBindingHash: projection.FenceBindingHash,
		Deadline: projection.Deadline, AttemptNumber: projection.AttemptNumber, AttemptCap: projection.AttemptCap,
		RouteMappingHash: projection.RouteMappingHash, SourceSnapshotHash: projection.SourceSnapshotHash, SourceManifestHash: projection.SourceManifestHash,
		SupervisorArtifactSHA256: projection.SupervisorArtifactSHA256, BuildIdentityHash: projection.BuildIdentityHash,
		ReleaseAttestationHash: projection.ReleaseAttestationHash, ReleaseApprovalHash: projection.ReleaseApprovalHash,
		EvidenceContractHash: projection.EvidenceContractHash, EvidenceSchemaVersion: projection.EvidenceSchemaVersion, EnvelopeHash: projection.EnvelopeHash}
}

func rebindForgedServerRequest(t *testing.T, request *wireRequest, resealProjection bool) {
	t.Helper()
	if resealProjection {
		projection := request.EnvelopeReference.PredecessorProjection
		projection.PredecessorProjectionHash = ""
		hash, err := canonicalHash(projection)
		if err != nil {
			t.Fatal(err)
		}
		projection.PredecessorProjectionHash = hash
		request.EnvelopeReference.PredecessorProjection = projection
	}
	reference := *request.EnvelopeReference
	reference.EnvelopeReferenceHash = ""
	referenceHash, err := canonicalHash(reference)
	if err != nil {
		t.Fatal(err)
	}
	reference.EnvelopeReferenceHash = referenceHash
	request.EnvelopeReference = &reference
	request.RequestHash, request.ChannelBindingHash = "", ""
	if request.Delivery != nil {
		delivery := *request.Delivery
		delivery.ChannelBindingHash, delivery.DeliveryHash = "", ""
		request.Delivery = &delivery
	}
	payloadHash, err := canonicalHash(*request)
	if err != nil {
		t.Fatal(err)
	}
	request.ChannelBindingHash, err = canonicalHash(map[string]any{
		"binding_schema_version": "ananke.local-unix-peer-channel-binding.v2", "peer_primary_group_id": uint32(os.Getgid()),
		"peer_process_id": os.Getpid(), "peer_user_id": uint32(os.Getuid()), "request_payload_hash": payloadHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Delivery != nil {
		delivery := *request.Delivery
		delivery.ChannelBindingHash, err = deriveMessageChannelBinding(request.ChannelBindingHash, "delivery", delivery.NonceHash,
			request.EnvelopeReference.PredecessorProjection.PredecessorProjectionHash)
		if err != nil {
			t.Fatal(err)
		}
		delivery, err = store.SealExternalSupervisorSealedDelivery(delivery)
		if err != nil {
			t.Fatal(err)
		}
		request.Delivery = &delivery
	}
	request.RequestHash, err = hashWireRequest(*request)
	if err != nil {
		t.Fatal(err)
	}
}

func assertServerRejectsRequestWithoutJournal(t *testing.T, material serverTestMaterial, request wireRequest, wantRows int) {
	t.Helper()
	encoded, err := marshalCanonical(request)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("unix", material.socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(encoded)))
	if _, err := connection.Write(append(header[:], encoded...)); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	if closeWriter, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
	response, _ := io.ReadAll(connection)
	_ = connection.Close()
	if len(response) != 0 {
		t.Fatalf("forged canonical request received %d response bytes", len(response))
	}
	assertServerJournalRows(t, material.journalPath, wantRows)
}

func assertServerJournalRows(t *testing.T, path string, want int) {
	t.Helper()
	journal, err := openServerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	var got int
	if err := journal.db.QueryRow(`SELECT COUNT(*) FROM trusted_supervisor_requests`).Scan(&got); err != nil || got != want {
		t.Fatalf("server request row count = %d, %v; want %d", got, err, want)
	}
}

func writeServerRepositoryPolicy(t *testing.T, directory, name string, identities ...string) string {
	t.Helper()
	entries := make([]repositoryPolicyEntry, 0, len(identities))
	for _, identity := range identities {
		entries = append(entries, repositoryPolicyEntry{SchemaVersion: repositoryPolicyEntrySchemaVersion,
			RepositoryIdentityHash: repositoryIdentityHash(identity), RepositoryIdentity: identity})
	}
	encoded, err := marshalCanonical(repositoryPolicyFile{SchemaVersion: repositoryPolicySchemaVersion, Repositories: entries})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertRequestHasNoRawRepositoryIdentity(t *testing.T, request wireRequest, raw string) {
	t.Helper()
	encoded, err := marshalCanonical(request)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(raw)) || bytes.Contains(encoded, []byte(`"repository_identity"`)) {
		t.Fatalf("raw repository authority crossed wire: %s", encoded)
	}
}
