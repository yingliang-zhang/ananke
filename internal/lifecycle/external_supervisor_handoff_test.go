package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestP3FExternalSupervisorFakeRuntimePersistsReceiptAndCallbackWithoutExecution(t *testing.T) {
	ctx := context.Background()
	runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)

	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	if fake.deliveries() != 1 {
		t.Fatalf("fake deliveries = %d, want one receipt-only delivery", fake.deliveries())
	}
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("GetExternalSupervisorRecoveryBoundary after submit: %v", err)
	}
	if boundary.Receipt == nil || boundary.Callback != nil || boundary.Cancellation != nil {
		t.Fatalf("post-submit recovery boundary = %+v, want receipt only", boundary)
	}
	assertP3CNoRealRuns(t, journal)

	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	if fake.deliveries() != 1 {
		t.Fatalf("idempotent delivery invoked fake supervisor %d times, want one", fake.deliveries())
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	if fake.reconciliations() != 1 {
		t.Fatalf("fake reconciliations = %d, want one explicit no-outcome reconciliation", fake.reconciliations())
	}
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("GetExternalSupervisorRecoveryBoundary after empty reconciliation: %v", err)
	}
	if boundary.Callback != nil {
		t.Fatalf("empty reconciliation inferred callback: %+v", boundary.Callback)
	}

	fake.publishCallback(envelope.HandoffID, "completed")
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("GetExternalSupervisorRecoveryBoundary after callback: %v", err)
	}
	if boundary.Callback == nil || boundary.Callback.Result.TerminalState != "completed" || boundary.Callback.EnvelopeHash != envelope.EnvelopeHash || boundary.Callback.ReceiptIdentityHash != boundary.Receipt.ReceiptIdentityHash {
		t.Fatalf("durable typed callback = %+v, want current-root envelope/receipt-bound result", boundary.Callback)
	}
	assertP3CNoRealRuns(t, journal)
}

func TestP3FExternalSupervisorFakeRuntimeRejectsPolicyRootFenceAndCancellationInference(t *testing.T) {
	ctx := context.Background()
	t.Run("policy drift", func(t *testing.T) {
		runtime, fake, _, envelope, fence := newP3FExternalSupervisorFixture(t)
		envelope.RouteMappingHash = p3fExternalSupervisorHash("different-route")
		sealed, err := store.SealExternalSupervisorEnvelope(envelope)
		if err != nil {
			t.Fatalf("seal policy-drift envelope: %v", err)
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, sealed, fence))
		if fake.deliveries() != 0 {
			t.Fatalf("policy-drift envelope reached fake supervisor %d times", fake.deliveries())
		}
	})

	t.Run("current root binding", func(t *testing.T) {
		runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
		assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
		fake.publishCallback(envelope.HandoffID, "completed")
		fake.setCurrentRoot(store.ExternalSupervisorTrustRoot{RootID: "remote_supervisor_root_v2", TrustBundleHash: p3fExternalSupervisorHash("trust-bundle-v2")})
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary with stale callback root: %v", err)
		}
		if boundary.Callback != nil {
			t.Fatalf("stale-root callback became durable: %+v", boundary.Callback)
		}
		fake.setCurrentRoot(p3fExternalSupervisorRoot())
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary after current-root callback: %v", err)
		}
		if boundary.Callback == nil {
			t.Fatal("current-root callback was not durable")
		}
	})

	t.Run("cancellation and stale recovery", func(t *testing.T) {
		runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
		assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
		receipt, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil || receipt.Receipt == nil {
			t.Fatalf("load durable receipt: boundary=%+v err=%v", receipt, err)
		}
		cancellation := store.ExternalSupervisorCancellation{
			SchemaVersion:            store.ExternalSupervisorCancellationSchemaVersion,
			HandoffID:                envelope.HandoffID,
			EnvelopeHash:             envelope.EnvelopeHash,
			ReceiptIdentityHash:      receipt.Receipt.ReceiptIdentityHash,
			CancellationIdentityHash: p3fExternalSupervisorHash("cancellation-001"),
			AttemptNumber:            envelope.AttemptNumber,
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.cancel(ctx, cancellation, fence))
		if fake.cancellations() != 1 {
			t.Fatalf("fake cancellations = %d, want one receipt-bound cancellation", fake.cancellations())
		}
		boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary after cancellation: %v", err)
		}
		if boundary.Cancellation == nil || boundary.Callback != nil {
			t.Fatalf("cancellation boundary = %+v, want cancellation identity without inferred callback", boundary)
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary after cancellation recovery: %v", err)
		}
		if boundary.Callback != nil {
			t.Fatalf("cancellation recovery inferred callback: %+v", boundary.Callback)
		}

		if _, err := journal.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
			ExpectedFence: fence,
			Claim: store.LaunchClaimRequest{
				LaunchSpecHash: envelope.LaunchSpecHash,
				ClaimID:        "claim_external_supervisor_reclaimed",
				ClaimTokenHash: p3fExternalSupervisorHash("reclaimed-token"),
				OwnerID:        "external_supervisor_runtime",
				Attempt:        2,
			},
		}); err != nil {
			t.Fatalf("reclaim active private fence: %v", err)
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		if fake.reconciliations() != 1 {
			t.Fatalf("stale fence recovery reconciliations = %d, want only the prior explicit attempt", fake.reconciliations())
		}
	})
}

func TestP3FExternalSupervisorProductionBuildExcludesFakeSupervisor(t *testing.T) {
	command := exec.Command("go", "list", "-json", ".")
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list production lifecycle package: %v", err)
	}
	var listed struct {
		GoFiles     []string
		TestGoFiles []string
	}
	if err := json.Unmarshal(output, &listed); err != nil {
		t.Fatalf("decode production lifecycle package listing: %v", err)
	}
	const fakeSource = "external_supervisor_handoff_fake_test.go"
	if !p3fListedFile(listed.TestGoFiles, fakeSource) || p3fListedFile(listed.GoFiles, fakeSource) {
		t.Fatalf("fake supervisor build selection = production:%v tests:%v", listed.GoFiles, listed.TestGoFiles)
	}
	for _, name := range listed.GoFiles {
		if strings.Contains(name, "fake_supervisor") || strings.Contains(name, "fake_runtime") {
			t.Fatalf("fake supervisor source %q compiled into production", name)
		}
	}
}

func newP3FExternalSupervisorFixture(t *testing.T) (*externalSupervisorHandoffRuntime, *p3fInProcessFakeSupervisor, *store.Store, store.ExternalSupervisorEnvelope, store.LaunchFence) {
	t.Helper()
	ctx := context.Background()
	orchestration, journal := newP3CTestOrchestration(t)
	admission := p3aAdmissionRequest()
	action, err := orchestration.admit(ctx, admission, p3cClaimRequest(admission.LaunchSpecHash))
	if err != nil {
		t.Fatalf("admit P3f external supervisor fence: %v", err)
	}
	action, err = orchestration.recordTrustedMaterializationReady(ctx, p3aMaterializationRequest(action.Boundary.Claim.Fence))
	if err != nil {
		t.Fatalf("record trusted materialization: %v", err)
	}
	action, err = orchestration.admitRunIntent(ctx, store.LaunchRunIntentRequest{
		Fence: action.Boundary.Claim.Fence, MaterializationID: "materialization_p3a_001", RunID: "run_p3f_external_supervisor_001", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("admit external supervisor run intent: %v", err)
	}
	fence := action.Boundary.Claim.Fence
	envelope := store.ExternalSupervisorEnvelope{
		SchemaVersion:            store.ExternalSupervisorEnvelopeSchemaVersion,
		HandoffID:                "remote_handoff_p3f_001",
		IdempotencyKeyHash:       p3fExternalSupervisorHash("idempotency-p3f-001"),
		LaunchSpecHash:           admission.LaunchSpecHash,
		FenceBindingHash:         store.HashExternalSupervisorFenceBinding(fence),
		Deadline:                 "2026-07-30T12:00:00Z",
		AttemptNumber:            1,
		AttemptCap:               3,
		RouteMappingHash:         externalSupervisorRouteMappingHash,
		SourceSnapshotHash:       externalSupervisorP3dSourceSnapshotHash,
		SourceManifestHash:       externalSupervisorSourceManifestHash,
		RepositoryIdentity:       externalSupervisorRepositoryIdentity,
		SupervisorArtifactSHA256: externalSupervisorArtifactSHA256,
		BuildIdentityHash:        externalSupervisorBuildIdentityHash,
		ReleaseAttestationHash:   externalSupervisorReleaseAttestationHash,
		ReleaseApprovalHash:      externalSupervisorReleaseApprovalHash,
		EvidenceContractHash:     externalSupervisorEvidenceContractHash,
		EvidenceSchemaVersion:    "ananke.remote-supervisor-evidence.v1",
	}
	sealed, err := store.SealExternalSupervisorEnvelope(envelope)
	if err != nil {
		t.Fatalf("seal P3f external supervisor envelope: %v", err)
	}
	fake := newP3FInProcessFakeSupervisor()
	runtime, err := newExternalSupervisorHandoffRuntime(journal, fake, fake, fake.currentRoot)
	if err != nil {
		t.Fatalf("construct external supervisor runtime: %v", err)
	}
	return runtime, fake, journal, sealed, fence
}

func p3fExternalSupervisorRoot() store.ExternalSupervisorTrustRoot {
	return store.ExternalSupervisorTrustRoot{RootID: "remote_supervisor_root_v1", TrustBundleHash: p3fExternalSupervisorHash("trust-bundle-v1")}
}

func p3fExternalSupervisorHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func assertP3FExternalSupervisorFailClosed(t *testing.T, output externalSupervisorPublicOutput) {
	t.Helper()
	if output.SchemaVersion != "ananke.omp-production-output.v1" || output.State != "waiting_for_human" || output.VerificationState != "not_run" || output.Result != nil || len(output.Events) != 0 {
		t.Fatalf("external supervisor output = %+v, want normalized waiting_for_human", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal external supervisor output: %v", err)
	}
	if string(encoded) != `{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}` {
		t.Fatalf("external supervisor output JSON = %s, want exact closed shape", encoded)
	}
}
