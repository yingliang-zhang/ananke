package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const (
	p3aLaunchSpecHash            = "sha256:bbc43093a3b00c49c1d2ac26db08e6dd36ff72174ded15de9408702af3a9e658"
	p3aRevisionHash              = "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263"
	p3aMaterializationHash       = "sha256:27f6f25e5a3cd790f634a3541d60d5681fa23c5d4a19c1b294ea70e168363ef7"
	p3aMaterializationNonce      = "nonce:034836706f8a359785406c36188f90edb94522896c44e3f000e9eede2d658f29"
	p3aClaimTokenHash            = "sha256:e33aef2dbe7e18f714ec929bd61c657c8771edabc56832b2a790f174233513c4"
	p3aP1RevisionCanonicalJSON   = `{"acceptance_criteria":["Define immutable proposal revisions.","Require one local-operator approval before execution."],"created_at":"2026-07-22T09:00:00Z","created_by":"local_gui_operator","idempotency_key":"create_p1a_001","parent_revision":null,"parent_revision_hash":null,"policy":{"adapter":{"access":"read_only","kind":"omp_audit","status":"future"},"authority":"deterministic","budget":{"dimensions":["deadline","attempt_cap"],"status":"future"},"model_role":"advisory_only"},"proposal_id":"proposal_p1a_001","revision":1,"schema_version":"ananke.proposal-revision.v1","task":{"instructions":"Freeze the P1a proposal contract and fixtures only.","title":"Freeze P1a proposal contract"}}`
	p3cReplacementClaimTokenHash = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestP3CFencedLaunchRecoversP3aBoundariesWithoutCreatingRun(t *testing.T) {
	ctx := context.Background()
	orchestration, journal := newP3CTestOrchestration(t)
	admission := p3aAdmissionRequest()
	claimRequest := p3cClaimRequest(admission.LaunchSpecHash)

	materializationAction, err := orchestration.admit(ctx, admission, claimRequest)
	if err != nil {
		t.Fatalf("admit fenced launch: %v", err)
	}
	assertP3CSafeAction(t, materializationAction, store.LaunchRecoveryRetryMaterialization, claimRequest)
	assertP3CNoRealRuns(t, journal)

	restarted := newFencedLaunchOrchestrator(journal)
	materializationAction, err = restarted.recover(ctx, admission.LaunchSpecHash)
	if err != nil {
		t.Fatalf("recover claim-to-materialization crash boundary: %v", err)
	}
	assertP3CSafeAction(t, materializationAction, store.LaunchRecoveryRetryMaterialization, claimRequest)

	readyRequest := p3aMaterializationRequest(materializationAction.Boundary.Claim.Fence)
	runIntentAction, err := restarted.recordTrustedMaterializationReady(ctx, readyRequest)
	if err != nil {
		t.Fatalf("record trusted materialization readiness: %v", err)
	}
	assertP3CSafeAction(t, runIntentAction, store.LaunchRecoveryRetryRunAdmission, claimRequest)
	if got := runIntentAction.Boundary.Materialization; got == nil || got.MaterializationID != "materialization_p3a_001" || got.MaterializationHash != p3aMaterializationHash || got.Nonce != p3aMaterializationNonce {
		t.Fatalf("materialization boundary = %+v, want exact P3a materialization identity", got)
	}
	assertP3CNoRealRuns(t, journal)

	restarted = newFencedLaunchOrchestrator(journal)
	runIntentAction, err = restarted.recover(ctx, admission.LaunchSpecHash)
	if err != nil {
		t.Fatalf("recover materialization-to-run crash boundary: %v", err)
	}
	assertP3CSafeAction(t, runIntentAction, store.LaunchRecoveryRetryRunAdmission, claimRequest)

	processAction, err := restarted.admitRunIntent(ctx, store.LaunchRunIntentRequest{
		Fence:             runIntentAction.Boundary.Claim.Fence,
		MaterializationID: "materialization_p3a_001",
		RunID:             "run_p3a_001",
		Attempt:           claimRequest.Attempt,
	})
	if err != nil {
		t.Fatalf("admit P3a run intent: %v", err)
	}
	assertP3CSafeAction(t, processAction, store.LaunchRecoveryRetryProcessAdmission, claimRequest)
	if got := processAction.Boundary.RunIntent; got == nil || got.RunID != "run_p3a_001" || got.MaterializationID != "materialization_p3a_001" || got.StateFact.Kind != store.LaunchStateFactCreated || got.StateFact.Sequence != 1 || got.StateFact.TokenHash != claimRequest.ClaimTokenHash {
		t.Fatalf("process-admission boundary run intent = %+v, want exact current-token P3a created fact", got)
	}
	assertP3CNoRealRuns(t, journal)

	restarted = newFencedLaunchOrchestrator(journal)
	processAction, err = restarted.recover(ctx, admission.LaunchSpecHash)
	if err != nil {
		t.Fatalf("recover run-to-process crash boundary: %v", err)
	}
	assertP3CSafeAction(t, processAction, store.LaunchRecoveryRetryProcessAdmission, claimRequest)
	actions, err := restarted.recoverAll(ctx)
	if err != nil {
		t.Fatalf("recover all fenced launch boundaries: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("recovered boundary count = %d, want 1", len(actions))
	}
	assertP3CSafeAction(t, actions[0], store.LaunchRecoveryRetryProcessAdmission, claimRequest)
	assertP3CNoRealRuns(t, journal)
}

func TestP3CFencedLaunchReconnectsConcurrentStagesAndRejectsStaleFence(t *testing.T) {
	ctx := context.Background()
	orchestration, journal := newP3CTestOrchestration(t)
	admission := p3aAdmissionRequest()
	claimRequest := p3cClaimRequest(admission.LaunchSpecHash)

	admitResults := runP3CConcurrently(2, func() (fencedLaunchAction, error) {
		return orchestration.admit(ctx, admission, claimRequest)
	})
	for _, result := range admitResults {
		if result.err != nil {
			t.Fatalf("concurrent claim admission: %v", result.err)
		}
		assertP3CSafeAction(t, result.action, store.LaunchRecoveryRetryMaterialization, claimRequest)
	}

	claim, err := journal.GetLaunchClaim(ctx, admission.LaunchSpecHash)
	if err != nil {
		t.Fatalf("load admitted claim: %v", err)
	}
	readyRequest := p3aMaterializationRequest(claim.Fence)
	readyResults := runP3CConcurrently(2, func() (fencedLaunchAction, error) {
		return newFencedLaunchOrchestrator(journal).recordTrustedMaterializationReady(ctx, readyRequest)
	})
	for _, result := range readyResults {
		if result.err != nil {
			t.Fatalf("concurrent readiness recording: %v", result.err)
		}
		assertP3CSafeAction(t, result.action, store.LaunchRecoveryRetryRunAdmission, claimRequest)
	}

	runRequest := store.LaunchRunIntentRequest{
		Fence:             claim.Fence,
		MaterializationID: "materialization_p3a_001",
		RunID:             "run_p3a_001",
		Attempt:           claimRequest.Attempt,
	}
	runResults := runP3CConcurrently(2, func() (fencedLaunchAction, error) {
		return newFencedLaunchOrchestrator(journal).admitRunIntent(ctx, runRequest)
	})
	for _, result := range runResults {
		if result.err != nil {
			t.Fatalf("concurrent run-intent admission: %v", result.err)
		}
		assertP3CSafeAction(t, result.action, store.LaunchRecoveryRetryProcessAdmission, claimRequest)
	}
	assertP3CNoRealRuns(t, journal)

	active, err := journal.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
		ExpectedFence: claim.Fence,
		Claim: store.LaunchClaimRequest{
			LaunchSpecHash: admission.LaunchSpecHash,
			ClaimID:        "claim_p3c_reclaimed",
			ClaimTokenHash: p3cReplacementClaimTokenHash,
			OwnerID:        "launch_admitter",
			Attempt:        2,
		},
	})
	if err != nil {
		t.Fatalf("reclaim active P3c claim: %v", err)
	}
	if _, err := orchestration.recordTrustedMaterializationReady(ctx, readyRequest); !errors.Is(err, store.ErrLaunchStaleFence) {
		t.Fatalf("stale materialization fence error = %v, want %v", err, store.ErrLaunchStaleFence)
	}
	recovered, err := orchestration.recover(ctx, admission.LaunchSpecHash)
	if err != nil {
		t.Fatalf("recover reclaimed fence: %v", err)
	}
	if recovered.Boundary.Claim.Fence != active.Fence {
		t.Fatalf("recovered fence = %+v, want active %+v", recovered.Boundary.Claim.Fence, active.Fence)
	}
	if recovered.Boundary.Action != store.LaunchRecoveryRetryMaterialization {
		t.Fatalf("reclaimed boundary action = %q, want materialization retry", recovered.Boundary.Action)
	}
	assertP3CNoRealRuns(t, journal)
}

func TestP3CFencedLaunchFailsClosedOnUnknownCorruptAndOutcomeFacts(t *testing.T) {
	ctx := context.Background()
	t.Run("unknown claim is waiting for human", func(t *testing.T) {
		orchestration, _ := newP3CTestOrchestration(t)
		action, err := orchestration.recover(ctx, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
		if !errors.Is(err, store.ErrLaunchClaimNotFound) {
			t.Fatalf("unknown recovery error = %v, want %v", err, store.ErrLaunchClaimNotFound)
		}
		if !action.WaitingForHuman || action.Cause != err {
			t.Fatalf("unknown recovery action = %+v, want waiting_for_human retaining the store error", action)
		}
	})

	t.Run("corrupt sealed materialization is waiting for human", func(t *testing.T) {
		orchestration, journal := newP3CTestOrchestration(t)
		admission := p3aAdmissionRequest()
		claimRequest := p3cClaimRequest(admission.LaunchSpecHash)
		action, err := orchestration.admit(ctx, admission, claimRequest)
		if err != nil {
			t.Fatalf("admit fenced launch: %v", err)
		}
		claim := action.Boundary.Claim
		if _, err := journal.DB().ExecContext(ctx, `INSERT INTO launch_materializations
			(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'ready', ?)`,
			"materialization_p3c_corrupt", admission.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration,
			"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "nonce:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "2026-07-23T00:00:00Z"); err != nil {
			t.Fatalf("seed FK-valid corrupt materialization: %v", err)
		}
		if _, err := journal.DB().ExecContext(ctx, `INSERT INTO launch_admission_outbox
			(outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at)
			VALUES (?, ?, ?, ?, ?, 2, 'pending_run_admission', ?)`,
			"launch_outbox_p3c_corrupt", admission.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration, "2026-07-23T00:00:00Z"); err != nil {
			t.Fatalf("seed corrupt stage-two outbox: %v", err)
		}

		action, err = orchestration.recover(ctx, admission.LaunchSpecHash)
		if !errors.Is(err, store.ErrLaunchRecordCorrupt) {
			t.Fatalf("corrupt recovery error = %v, want %v", err, store.ErrLaunchRecordCorrupt)
		}
		if !action.WaitingForHuman || action.Cause != err || action.LaunchSpecHash != admission.LaunchSpecHash {
			t.Fatalf("corrupt recovery action = %+v, want exact waiting_for_human action", action)
		}
		assertP3CNoRealRuns(t, journal)
	})

	t.Run("unexpected terminal intent cannot infer process outcome", func(t *testing.T) {
		orchestration, journal := newP3CTestOrchestration(t)
		admission := p3aAdmissionRequest()
		claimRequest := p3cClaimRequest(admission.LaunchSpecHash)
		action, err := orchestration.admit(ctx, admission, claimRequest)
		if err != nil {
			t.Fatalf("admit fenced launch: %v", err)
		}
		action, err = orchestration.recordTrustedMaterializationReady(ctx, p3aMaterializationRequest(action.Boundary.Claim.Fence))
		if err != nil {
			t.Fatalf("record trusted materialization: %v", err)
		}
		action, err = orchestration.admitRunIntent(ctx, store.LaunchRunIntentRequest{
			Fence: action.Boundary.Claim.Fence, MaterializationID: "materialization_p3a_001", RunID: "run_p3a_001", Attempt: claimRequest.Attempt,
		})
		if err != nil {
			t.Fatalf("admit run intent: %v", err)
		}
		if _, err := journal.AppendLaunchTerminalIntent(ctx, store.LaunchTerminalIntentRequest{
			Fence: action.Boundary.Claim.Fence, RunID: "run_p3a_001", IntentID: "terminal_p3c_unexpected",
		}); err != nil {
			t.Fatalf("seed unexpected terminal intent: %v", err)
		}
		action, err = orchestration.recover(ctx, admission.LaunchSpecHash)
		if !errors.Is(err, errFencedLaunchOutcomeUnknown) {
			t.Fatalf("terminal-intent recovery error = %v, want %v", err, errFencedLaunchOutcomeUnknown)
		}
		if !action.WaitingForHuman || action.Cause != err {
			t.Fatalf("terminal-intent recovery action = %+v, want waiting_for_human", action)
		}
		assertP3CNoRealRuns(t, journal)
	})
	t.Run("unexpected evidence intent cannot infer evidence outcome", func(t *testing.T) {
		orchestration, journal := newP3CTestOrchestration(t)
		admission := p3aAdmissionRequest()
		claimRequest := p3cClaimRequest(admission.LaunchSpecHash)
		action, err := orchestration.admit(ctx, admission, claimRequest)
		if err != nil {
			t.Fatalf("admit fenced launch: %v", err)
		}
		action, err = orchestration.recordTrustedMaterializationReady(ctx, p3aMaterializationRequest(action.Boundary.Claim.Fence))
		if err != nil {
			t.Fatalf("record trusted materialization: %v", err)
		}
		action, err = orchestration.admitRunIntent(ctx, store.LaunchRunIntentRequest{
			Fence: action.Boundary.Claim.Fence, MaterializationID: "materialization_p3a_001", RunID: "run_p3a_001", Attempt: claimRequest.Attempt,
		})
		if err != nil {
			t.Fatalf("admit run intent: %v", err)
		}
		if _, err := journal.SettleLaunchEvidenceIntent(ctx, store.LaunchEvidenceIntentRequest{
			Fence: action.Boundary.Claim.Fence, RunID: "run_p3a_001", IntentID: "evidence_p3c_unexpected",
		}); err != nil {
			t.Fatalf("seed unexpected evidence intent: %v", err)
		}
		action, err = orchestration.recover(ctx, admission.LaunchSpecHash)
		if !errors.Is(err, errFencedLaunchOutcomeUnknown) {
			t.Fatalf("evidence-intent recovery error = %v, want %v", err, errFencedLaunchOutcomeUnknown)
		}
		if !action.WaitingForHuman || action.Cause != err || action.LaunchSpecHash != admission.LaunchSpecHash {
			t.Fatalf("evidence-intent recovery action = %+v, want exact waiting_for_human", action)
		}
		if action.Boundary.EvidenceIntent == nil || action.Boundary.TerminalIntent != nil {
			t.Fatalf("evidence-intent recovery boundary = %+v, want evidence only", action.Boundary)
		}
		assertP3CNoRealRuns(t, journal)
	})
}

func TestP3CFencedLaunchAggregateRecoveryIsolatesCorruptBoundary(t *testing.T) {
	ctx := context.Background()
	orchestration, journal := newP3CTestOrchestration(t)
	validAdmission := p3aAdmissionRequest()
	validClaimRequest := p3cClaimRequest(validAdmission.LaunchSpecHash)
	if _, err := orchestration.admit(ctx, validAdmission, validClaimRequest); err != nil {
		t.Fatalf("admit valid fenced launch: %v", err)
	}
	corruptAdmission := p3cAlternateAdmissionRequest(t, journal)
	corruptClaimRequest := p3cAlternateClaimRequest(corruptAdmission.LaunchSpecHash)
	corruptAction, err := orchestration.admit(ctx, corruptAdmission, corruptClaimRequest)
	if err != nil {
		t.Fatalf("admit corrupt fenced launch: %v", err)
	}
	corruptClaim := corruptAction.Boundary.Claim
	if _, err := journal.DB().ExecContext(ctx, `INSERT INTO launch_materializations
		(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'ready', ?)`,
		"materialization_p3c_aggregate_corrupt", corruptAdmission.LaunchSpecHash, corruptClaim.ClaimID, corruptClaim.ClaimTokenHash, corruptClaim.FenceGeneration,
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "nonce:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "2026-07-23T00:00:00Z"); err != nil {
		t.Fatalf("seed corrupt materialization: %v", err)
	}
	if _, err := journal.DB().ExecContext(ctx, `INSERT INTO launch_admission_outbox
		(outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at)
		VALUES (?, ?, ?, ?, ?, 2, 'pending_run_admission', ?)`,
		"launch_outbox_p3c_aggregate_corrupt", corruptAdmission.LaunchSpecHash, corruptClaim.ClaimID, corruptClaim.ClaimTokenHash, corruptClaim.FenceGeneration, "2026-07-23T00:00:00Z"); err != nil {
		t.Fatalf("seed corrupt outbox: %v", err)
	}

	actions, err := newFencedLaunchOrchestrator(journal).recoverAll(ctx)
	if err != nil {
		t.Fatalf("aggregate fenced-launch recovery: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("aggregate recovery action count = %d, want 2", len(actions))
	}
	valid, found := p3cActionForLaunchSpec(actions, validAdmission.LaunchSpecHash)
	if !found {
		t.Fatalf("missing valid aggregate action for %q", validAdmission.LaunchSpecHash)
	}
	assertP3CSafeAction(t, valid, store.LaunchRecoveryRetryMaterialization, validClaimRequest)
	corrupt, found := p3cActionForLaunchSpec(actions, corruptAdmission.LaunchSpecHash)
	if !found || !corrupt.WaitingForHuman || !errors.Is(corrupt.Cause, store.ErrLaunchRecordCorrupt) || corrupt.Boundary.LaunchSpecHash != "" {
		t.Fatalf("corrupt aggregate action = %+v, want exact waiting_for_human action", corrupt)
	}
	assertP3CNoRealRuns(t, journal)
}
func TestP3CFencedLaunchAggregateRecoveryPropagatesOperationalErrors(t *testing.T) {
	readFailure := errors.New("injected aggregate recovery read failure")
	for _, tc := range []struct {
		name  string
		cause error
	}{
		{name: "context cancellation", cause: context.Canceled},
		{name: "non-authority read failure", cause: readFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			orchestration, journal := newP3CTestOrchestration(t)
			admission := p3aAdmissionRequest()
			if _, err := orchestration.admit(ctx, admission, p3cClaimRequest(admission.LaunchSpecHash)); err != nil {
				t.Fatalf("admit fenced launch: %v", err)
			}

			faultingStore := &p3cAggregateRecoveryFaultStore{
				Store:               journal,
				afterDiscoveryCause: tc.cause,
				expectedHash:        admission.LaunchSpecHash,
			}
			orchestration.store = faultingStore

			actions, err := orchestration.recoverAll(ctx)
			if err != tc.cause {
				t.Fatalf("aggregate recovery error = %v, want exact injected error %v", err, tc.cause)
			}
			if len(actions) != 0 {
				t.Fatalf("aggregate recovery actions = %+v, want no waiting_for_human masking", actions)
			}
			if faultingStore.discoveredHash != admission.LaunchSpecHash {
				t.Fatalf("discovered hash = %q, want %q before injected failure", faultingStore.discoveredHash, admission.LaunchSpecHash)
			}
			assertP3CNoRealRuns(t, journal)
		})
	}
}

type p3cAggregateRecoveryFaultStore struct {
	*store.Store
	afterDiscoveryCause error
	expectedHash        string
	discoveredHash      string
}

func (s *p3cAggregateRecoveryFaultStore) ListLaunchRecoveryBoundaries(ctx context.Context) ([]store.LaunchRecoveryResult, error) {
	results, err := s.Store.ListLaunchRecoveryBoundaries(ctx)
	if err != nil {
		return nil, err
	}
	if len(results) != 1 || results[0].LaunchSpecHash != s.expectedHash {
		return nil, errors.New("aggregate recovery did not discover the expected hash")
	}
	s.discoveredHash = results[0].LaunchSpecHash
	return []store.LaunchRecoveryResult{{
		LaunchSpecHash: s.discoveredHash,
		Cause:          s.afterDiscoveryCause,
	}}, nil
}

func newP3CTestOrchestration(t *testing.T) (*fencedLaunchOrchestrator, *store.Store) {
	t.Helper()
	journal, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory journal: %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	seedP3aApprovedRevision(t, journal)
	return newFencedLaunchOrchestrator(journal), journal
}

func seedP3aApprovedRevision(t *testing.T, journal *store.Store) {
	t.Helper()
	ctx := context.Background()
	tx, err := journal.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin P3a identity seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	const createdAt = "2026-07-22T09:00:00Z"
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposals
		(proposal_id, project_id, workstream_id, created_at, created_by, state, current_revision, current_revision_hash)
		VALUES ('proposal_p1a_001', 'project_p1a', 'workstream_main', ?, 'local_gui_operator', 'approved', 1, ?)`, createdAt, p3aRevisionHash); err != nil {
		t.Fatalf("seed P3a proposal: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revisions
		(proposal_id, revision, revision_hash, snapshot_json) VALUES ('proposal_p1a_001', 1, ?, ?)`, p3aRevisionHash, p3aP1RevisionCanonicalJSON); err != nil {
		t.Fatalf("seed P3a revision: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_approvals
		(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state, decided_at, decided_by, decision_idempotency_key, reason)
		VALUES ('approval_p1a_001', 'proposal_p1a_001', 1, ?, '2026-07-22T09:00:01Z', 'local_gui_operator', 'approved', '2026-07-22T09:00:02Z', 'local_gui_operator', 'approve_p1a_001', 'Meets the frozen P1a contract.')`, p3aRevisionHash); err != nil {
		t.Fatalf("seed P3a approval: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revision_lifecycles
		(proposal_id, revision, revision_hash, approval_id, state, created_at, updated_at, version)
		VALUES ('proposal_p1a_001', 1, ?, 'approval_p1a_001', 'approved', '2026-07-22T09:00:01Z', '2026-07-22T09:00:02Z', 2)`, p3aRevisionHash); err != nil {
		t.Fatalf("seed P3a revision lifecycle: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit P3a identity seed: %v", err)
	}
}

func p3aAdmissionRequest() store.LaunchAdmissionRequest {
	return store.LaunchAdmissionRequest{
		LaunchSpecHash: p3aLaunchSpecHash,
		ApprovalID:     "approval_p1a_001",
		Spec: store.LaunchSpec{
			SchemaVersion: "ananke.launch-spec.v1",
			Revision:      store.LaunchRevisionIdentity{ProposalID: "proposal_p1a_001", Revision: 1, RevisionHash: p3aRevisionHash},
			Model:         store.LaunchModelSpec{Provider: "omp", Model: "omp_audit_model_v1"},
			Deadline:      "2026-07-30T12:00:00Z",
			AttemptCap:    3,
			ReadOnlyScope: store.LaunchReadOnlyScope{
				Access: "read_only", Retrieval: "sealed_contract_only", ScopeFingerprint: "sha256:6f3be7b6f4e6a30cb6534c3270ce7a5707ec5e6880448fa71835345c0b900f5b", Writes: "forbidden",
			},
			SealedContract: store.LaunchSealedContract{MaterializationHash: p3aMaterializationHash, Nonce: p3aMaterializationNonce},
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
		},
	}
}

func p3cAlternateAdmissionRequest(t *testing.T, journal *store.Store) store.LaunchAdmissionRequest {
	t.Helper()
	created, err := journal.CreateProposal(context.Background(), store.CreateProposalRequest{
		IdempotencyKey: "create_p3c_aggregate_002",
		ProjectID:      "project_p3c",
		WorkstreamID:   "workstream_main",
		RevisionInput: store.RevisionInput{
			Task:               store.ProposalTask{Title: "Recover aggregate boundary", Instructions: "Keep each active durable launch boundary isolated."},
			AcceptanceCriteria: []string{"Preserve exact durable launch boundaries."},
			Policy: store.ProposalPolicy{
				Adapter:   store.ProposalAdapterPolicy{Access: "read_only", Kind: "omp_audit", Status: "future"},
				Authority: "deterministic",
				Budget:    store.ProposalBudgetPolicy{Dimensions: []string{"deadline", "attempt_cap"}, Status: "future"},
				ModelRole: "advisory_only",
			},
		},
	})
	if err != nil {
		t.Fatalf("create alternate P1 proposal: %v", err)
	}
	if _, err := journal.DecideProposalApproval(context.Background(), store.DecideProposalApprovalRequest{
		IdempotencyKey: "approve_p3c_aggregate_002",
		ApprovalID:     created.ApprovalID,
		ProposalID:     created.ProposalID,
		Revision:       created.Revision,
		RevisionHash:   created.RevisionHash,
		Decision:       store.ApprovalStateApproved,
		Reason:         "Approve aggregate recovery coverage.",
	}); err != nil {
		t.Fatalf("approve alternate P1 proposal: %v", err)
	}
	admission := p3aAdmissionRequest()
	admission.Spec.Revision = store.LaunchRevisionIdentity{ProposalID: created.ProposalID, Revision: created.Revision, RevisionHash: created.RevisionHash}
	admission.ApprovalID = created.ApprovalID
	admission.LaunchSpecHash, err = store.HashLaunchSpec(admission.Spec)
	if err != nil {
		t.Fatalf("hash alternate P3c launch spec: %v", err)
	}
	return admission
}

func p3cClaimRequest(launchSpecHash string) store.LaunchClaimRequest {
	return store.LaunchClaimRequest{
		LaunchSpecHash: launchSpecHash,
		ClaimID:        "claim_p3c_001",
		ClaimTokenHash: p3aClaimTokenHash,
		OwnerID:        "launch_admitter",
		Attempt:        1,
	}
}

func p3cAlternateClaimRequest(launchSpecHash string) store.LaunchClaimRequest {
	return store.LaunchClaimRequest{
		LaunchSpecHash: launchSpecHash,
		ClaimID:        "claim_p3c_aggregate_corrupt",
		ClaimTokenHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		OwnerID:        "launch_admitter",
		Attempt:        1,
	}
}

func p3aMaterializationRequest(fence store.LaunchFence) store.LaunchMaterializationRequest {
	return store.LaunchMaterializationRequest{
		Fence:               fence,
		MaterializationID:   "materialization_p3a_001",
		MaterializationHash: p3aMaterializationHash,
		Nonce:               p3aMaterializationNonce,
	}
}

func p3cActionForLaunchSpec(actions []fencedLaunchAction, launchSpecHash string) (fencedLaunchAction, bool) {
	for _, action := range actions {
		if action.LaunchSpecHash == launchSpecHash {
			return action, true
		}
	}
	return fencedLaunchAction{}, false
}

func assertP3CSafeAction(t *testing.T, action fencedLaunchAction, want store.LaunchRecoveryAction, request store.LaunchClaimRequest) {
	t.Helper()
	if action.WaitingForHuman || action.Cause != nil {
		t.Fatalf("action = %+v, want safe retry action", action)
	}
	boundary := action.Boundary
	if boundary.Action != want || boundary.LaunchSpecHash != request.LaunchSpecHash {
		t.Fatalf("boundary action/spec = %q/%q, want %q/%q", boundary.Action, boundary.LaunchSpecHash, want, request.LaunchSpecHash)
	}
	if boundary.Claim.ClaimID != request.ClaimID || boundary.Claim.ClaimTokenHash != request.ClaimTokenHash || boundary.Claim.LaunchSpecHash != request.LaunchSpecHash || boundary.Claim.Attempt != request.Attempt {
		t.Fatalf("boundary claim = %+v, want exact request %+v", boundary.Claim, request)
	}
	if boundary.Outbox.LaunchFence != boundary.Claim.LaunchFence || boundary.Outbox.LaunchSpecHash != boundary.LaunchSpecHash {
		t.Fatalf("boundary outbox = %+v, want exact active claim binding %+v", boundary.Outbox, boundary.Claim.LaunchFence)
	}
	if boundary.TerminalIntent != nil || boundary.EvidenceIntent != nil {
		t.Fatalf("safe boundary inferred terminal/evidence intent: %+v", boundary)
	}
}

func assertP3CNoRealRuns(t *testing.T, journal *store.Store) {
	t.Helper()
	var count int
	if err := journal.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM runs`).Scan(&count); err != nil {
		t.Fatalf("count real runs: %v", err)
	}
	if count != 0 {
		t.Fatalf("real runs = %d, want none", count)
	}
}

type p3cConcurrentResult struct {
	action fencedLaunchAction
	err    error
}

func runP3CConcurrently(count int, call func() (fencedLaunchAction, error)) []p3cConcurrentResult {
	results := make([]p3cConcurrentResult, count)
	var ready sync.WaitGroup
	ready.Add(count)
	start := make(chan struct{})
	var done sync.WaitGroup
	done.Add(count)
	for index := range count {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			results[index].action, results[index].err = call()
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return results
}
