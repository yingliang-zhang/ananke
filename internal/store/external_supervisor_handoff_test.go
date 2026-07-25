package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExternalSupervisorHandoffStagesImmutableEnvelopeAndOutboxAtomically(t *testing.T) {
	ctx := context.Background()
	s, request, claim, envelope := newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(time.Hour))

	if _, err := s.DB().ExecContext(ctx, `CREATE TRIGGER reject_external_supervisor_outbox
		BEFORE INSERT ON external_supervisor_delivery_outbox
		BEGIN SELECT RAISE(ABORT, 'injected outbox failure'); END`); err != nil {
		t.Fatalf("create injected outbox trigger: %v", err)
	}
	if _, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence); err == nil {
		t.Fatal("StageExternalSupervisorHandoff accepted a transaction whose outbox insert fails")
	}
	assertExternalSupervisorTableCount(t, s, "external_supervisor_handoffs", 0)
	assertExternalSupervisorTableCount(t, s, "external_supervisor_delivery_outbox", 0)
	if _, err := s.DB().ExecContext(ctx, `DROP TRIGGER reject_external_supervisor_outbox`); err != nil {
		t.Fatalf("drop injected outbox trigger: %v", err)
	}

	staged, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence)
	if err != nil {
		t.Fatalf("StageExternalSupervisorHandoff: %v", err)
	}
	if staged.Envelope != envelope || staged.LaunchSpecHash != request.LaunchSpecHash {
		t.Fatalf("staged handoff = %+v, want exact immutable envelope", staged)
	}
	pending, err := s.ListPendingExternalSupervisorDeliveries(ctx)
	if err != nil {
		t.Fatalf("ListPendingExternalSupervisorDeliveries: %v", err)
	}
	if len(pending) != 1 || pending[0] != staged {
		t.Fatalf("pending deliveries = %+v, want one exact staged handoff", pending)
	}

	replayed, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence)
	if err != nil {
		t.Fatalf("StageExternalSupervisorHandoff replay: %v", err)
	}
	if replayed != staged {
		t.Fatalf("replayed handoff = %+v, want %+v", replayed, staged)
	}
	assertExternalSupervisorTableCount(t, s, "external_supervisor_handoffs", 1)
	assertExternalSupervisorTableCount(t, s, "external_supervisor_delivery_outbox", 1)

	if _, err := s.DB().ExecContext(ctx, `UPDATE external_supervisor_handoffs SET envelope_json = '{}' WHERE handoff_id = ?`, envelope.HandoffID); err == nil {
		t.Fatal("immutable external supervisor envelope update unexpectedly succeeded")
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM external_supervisor_delivery_outbox WHERE handoff_id = ?`, envelope.HandoffID); err == nil {
		t.Fatal("immutable external supervisor delivery outbox delete unexpectedly succeeded")
	}
}

func TestExternalSupervisorHandoffRejectsFenceDeadlineAttemptAndReplayConflicts(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		mutate func(*ExternalSupervisorEnvelope, *LaunchFence)
		want   error
	}{
		{
			name: "full private fence",
			mutate: func(_ *ExternalSupervisorEnvelope, fence *LaunchFence) {
				fence.ClaimTokenHash = externalSupervisorTestHash("wrong-fence")
			},
			want: ErrExternalSupervisorFence,
		},
		{
			name: "fence binding",
			mutate: func(envelope *ExternalSupervisorEnvelope, _ *LaunchFence) {
				envelope.FenceBindingHash = externalSupervisorTestHash("wrong-binding")
				mustSealExternalSupervisorEnvelope(t, envelope)
			},
			want: ErrExternalSupervisorFence,
		},
		{
			name: "attempt number",
			mutate: func(envelope *ExternalSupervisorEnvelope, _ *LaunchFence) {
				envelope.AttemptNumber = 2
				mustSealExternalSupervisorEnvelope(t, envelope)
			},
			want: ErrExternalSupervisorAttempt,
		},
		{
			name: "attempt cap",
			mutate: func(envelope *ExternalSupervisorEnvelope, _ *LaunchFence) {
				envelope.AttemptCap++
				mustSealExternalSupervisorEnvelope(t, envelope)
			},
			want: ErrExternalSupervisorAttempt,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _, claim, envelope := newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(time.Hour))
			fence := claim.Fence
			tc.mutate(&envelope, &fence)
			if _, err := s.StageExternalSupervisorHandoff(ctx, envelope, fence); !errors.Is(err, tc.want) {
				t.Fatalf("StageExternalSupervisorHandoff error = %v, want %v", err, tc.want)
			}
			assertExternalSupervisorTableCount(t, s, "external_supervisor_handoffs", 0)
		})
	}

	s, _, claim, envelope := newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(-time.Second))
	if _, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence); !errors.Is(err, ErrExternalSupervisorDeadline) {
		t.Fatalf("expired StageExternalSupervisorHandoff error = %v, want %v", err, ErrExternalSupervisorDeadline)
	}

	s, _, claim, envelope = newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(time.Hour))
	if _, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence); err != nil {
		t.Fatalf("stage exact handoff: %v", err)
	}
	conflict := envelope
	conflict.IdempotencyKeyHash = externalSupervisorTestHash("other-idempotency")
	mustSealExternalSupervisorEnvelope(t, &conflict)
	if _, err := s.StageExternalSupervisorHandoff(ctx, conflict, claim.Fence); !errors.Is(err, ErrExternalSupervisorConflict) {
		t.Fatalf("same handoff conflicting replay error = %v, want %v", err, ErrExternalSupervisorConflict)
	}
	conflict = envelope
	conflict.HandoffID = "remote_handoff_other"
	mustSealExternalSupervisorEnvelope(t, &conflict)
	if _, err := s.StageExternalSupervisorHandoff(ctx, conflict, claim.Fence); !errors.Is(err, ErrExternalSupervisorConflict) {
		t.Fatalf("same idempotency conflicting replay error = %v, want %v", err, ErrExternalSupervisorConflict)
	}
}

func TestExternalSupervisorReceiptCallbackCancellationAndRecoveryRemainBound(t *testing.T) {
	ctx := context.Background()
	s, _, claim, envelope := newExternalSupervisorHandoffFixture(t, time.Now().UTC().Add(time.Hour))
	envelope, receipt := externalSupervisorAuthenticatedReceiptFixture(t, envelope)
	staged, err := s.StageExternalSupervisorHandoff(ctx, envelope, claim.Fence)
	if err != nil {
		t.Fatalf("stage handoff: %v", err)
	}
	verifier := externalSupervisorPersistentAuthenticator{trustBundleHash: receipt.Delivery.TrustBundleHash}
	acceptedReceipt, err := s.DeliverAndPersistExternalSupervisorReceipt(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope) (ExternalSupervisorAuthenticatedReceipt, error) {
		return receipt, nil
	})
	if err != nil || acceptedReceipt != receipt {
		t.Fatalf("authenticated receipt = %+v, %v", acceptedReceipt, err)
	}
	callback := externalSupervisorAuthenticatedCallbackFixture(t, envelope, receipt)
	acceptedCallback, err := s.ReconcileAndPersistExternalSupervisorCallback(ctx, envelope.HandoffID, verifier, func(ExternalSupervisorEnvelope, ExternalSupervisorAuthenticatedReceipt) (*ExternalSupervisorAuthenticatedCallback, error) {
		return &callback, nil
	})
	if err != nil || acceptedCallback == nil || *acceptedCallback != callback {
		t.Fatalf("authenticated callback = %+v, %v", acceptedCallback, err)
	}
	boundary, err := s.GetExternalSupervisorRecoveryBoundary(ctx, staged.Envelope.HandoffID)
	if err != nil || boundary.Handoff != staged || boundary.Receipt == nil || *boundary.Receipt != receipt || boundary.Callback == nil || *boundary.Callback != callback || boundary.Cancellation != nil {
		t.Fatalf("recovery boundary = %+v, %v", boundary, err)
	}
}

func newExternalSupervisorHandoffFixture(t *testing.T, deadline time.Time) (*Store, LaunchAdmissionRequest, TaskClaim, ExternalSupervisorEnvelope) {
	t.Helper()
	ctx := context.Background()
	s := newTestStore(t)
	request, _ := approvedLaunchAdmissionRequest(t, s)
	request.Spec.Deadline = deadline.UTC().Format(time.RFC3339Nano)
	request.LaunchSpecHash = mustHashLaunchSpec(t, request.Spec)
	if _, err := s.StoreLaunchSpec(ctx, request); err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}
	claim := mustAcquireP3BClaim(t, s, request.LaunchSpecHash, "claim_external_supervisor_001", externalSupervisorTestHash("claim-token"))
	materialization, err := s.RecordLaunchMaterializationReady(ctx, LaunchMaterializationRequest{
		Fence:               claim.Fence,
		MaterializationID:   "materialization_external_supervisor_001",
		MaterializationHash: request.Spec.SealedContract.MaterializationHash,
		Nonce:               request.Spec.SealedContract.Nonce,
	})
	if err != nil {
		t.Fatalf("RecordLaunchMaterializationReady: %v", err)
	}
	if _, err := s.CreateLaunchRunIntent(ctx, LaunchRunIntentRequest{
		Fence: claim.Fence, MaterializationID: materialization.MaterializationID, RunID: "run_external_supervisor_001", Attempt: claim.Attempt,
	}); err != nil {
		t.Fatalf("CreateLaunchRunIntent: %v", err)
	}
	envelope := ExternalSupervisorEnvelope{
		SchemaVersion:            ExternalSupervisorEnvelopeSchemaVersion,
		HandoffID:                "remote_handoff_001",
		IdempotencyKeyHash:       externalSupervisorTestHash("idempotency-001"),
		LaunchSpecHash:           request.LaunchSpecHash,
		FenceBindingHash:         HashExternalSupervisorFenceBinding(claim.Fence),
		Deadline:                 request.Spec.Deadline,
		AttemptNumber:            claim.Attempt,
		AttemptCap:               request.Spec.AttemptCap,
		RouteMappingHash:         externalSupervisorTestHash("route-mapping"),
		SourceSnapshotHash:       externalSupervisorTestHash("source-snapshot"),
		SourceManifestHash:       externalSupervisorTestHash("source-manifest"),
		RepositoryIdentity:       "github.com/yingliang-zhang/ananke",
		SupervisorArtifactSHA256: externalSupervisorTestHash("supervisor-artifact"),
		BuildIdentityHash:        externalSupervisorTestHash("build-identity"),
		ReleaseAttestationHash:   externalSupervisorTestHash("release-attestation"),
		ReleaseApprovalHash:      externalSupervisorTestHash("release-approval"),
		EvidenceContractHash:     externalSupervisorTestHash("evidence-contract"),
		EvidenceSchemaVersion:    "ananke.remote-supervisor-evidence.v1",
	}
	mustSealExternalSupervisorEnvelope(t, &envelope)
	return s, request, claim, envelope
}

func mustSealExternalSupervisorEnvelope(t *testing.T, envelope *ExternalSupervisorEnvelope) {
	t.Helper()
	sealed, err := SealExternalSupervisorEnvelope(*envelope)
	if err != nil {
		t.Fatalf("SealExternalSupervisorEnvelope: %v", err)
	}
	*envelope = sealed
}

func externalSupervisorTestHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func assertExternalSupervisorTableCount(t *testing.T, s *Store, table string, want int) {
	t.Helper()
	if !strings.HasPrefix(table, "external_supervisor_") {
		t.Fatalf("unsafe table name %q", table)
	}
	var got int
	if err := s.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
