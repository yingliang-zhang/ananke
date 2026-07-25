package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const (
	p4AcceptanceArtifactPath       = "../../contracts/p4/self-development-acceptance-v1.canonical.json"
	p4AcceptanceEvidenceFixtureSHA = "sha256:aa7d94f96b123ff200bf4f84ec55d7b5edbd157f4578ba99ed3b4fdbc93ee36c"
	p4AcceptanceRedFlagsFixtureSHA = "sha256:91c900ce7cc2c53ce360775be0909b3e679a971756075d643f3b0d0e3eb4ce0f"
	p4AcceptanceP4DenialCases      = 38
)

func TestP4SelfDevelopmentAcceptance(t *testing.T) {
	report := runP4SelfDevelopmentAcceptance(t)
	assertP4SelfDevelopmentAcceptanceReport(t, report)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal P4 self-development acceptance report: %v", err)
	}
	t.Logf("P4 self-development acceptance report: %s", encoded)
	assertP4SelfDevelopmentAcceptanceArtifact(t, report)
}

// runP4SelfDevelopmentAcceptance composes the frozen P1 identity, the pure
// P2 Grill, P3 fenced fake-supervisor handoff, and durable P4 admission in one
// in-memory journal. It intentionally does not bridge a P3 callback into P4
// execution authority.
func runP4SelfDevelopmentAcceptance(t *testing.T) map[string]any {
	t.Helper()
	ctx := context.Background()
	orchestration, journal := newP3CTestOrchestration(t)

	proposal, err := journal.GetProposal(ctx, "proposal_p1a_001")
	if err != nil {
		t.Fatalf("load frozen P1 proposal: %v", err)
	}
	revision, err := journal.GetRevision(ctx, proposal.ProposalID, 1)
	if err != nil {
		t.Fatalf("load frozen P1 revision: %v", err)
	}
	approval, err := journal.GetApproval(ctx, "approval_p1a_001")
	if err != nil {
		t.Fatalf("load frozen P1 approval: %v", err)
	}
	if proposal.State != store.ProposalStateApproved || approval.State != store.ApprovalStateApproved ||
		proposal.CurrentRevisionHash != approval.RevisionHash {
		t.Fatalf("P1 proposal/revision/approval chain = %#v / %#v / %#v", proposal, revision, approval)
	}

	p2Input := p4AcceptanceGrillInput(revision, proposal.CurrentRevisionHash)
	p2InputHash, err := store.HashGrillInput(p2Input)
	if err != nil {
		t.Fatalf("hash P2 Grill input: %v", err)
	}
	p2First, err := journal.EvaluateGrill(ctx, store.GrillEvaluationRequest{
		Input: p2Input, InputHash: p2InputHash, RuleVersion: store.GrillRuleVersion,
	})
	if err != nil {
		t.Fatalf("evaluate P2 Grill: %v", err)
	}
	p2Replay, err := journal.EvaluateGrill(ctx, store.GrillEvaluationRequest{
		Input: p2Input, InputHash: p2InputHash, RuleVersion: store.GrillRuleVersion,
	})
	if err != nil {
		t.Fatalf("replay P2 Grill: %v", err)
	}
	if p2First.Status != store.GrillStatusClear || p2First.NewRecords != 1 || p2Replay.Status != store.GrillStatusClear || p2Replay.NewRecords != 0 {
		t.Fatalf("P2 deterministic Grill result = %#v then %#v, want clear plus zero-record replay", p2First, p2Replay)
	}
	p2Records, err := journal.ListGrillRecords(ctx, store.GrillRevisionIdentity{
		ProposalID: revision.ProposalID, Revision: revision.Revision, RevisionHash: proposal.CurrentRevisionHash,
	})
	if err != nil {
		t.Fatalf("list P2 Grill records: %v", err)
	}
	if len(p2Records) != 0 {
		t.Fatalf("clear P2 Grill created question records: %#v", p2Records)
	}

	envelope, p3Fence := stageP3FExternalSupervisorFixture(t, orchestration)
	p3FakeSupervisor := newP3FInProcessFakeSupervisor()
	p3Runtime, err := newExternalSupervisorHandoffRuntime(journal, p3FakeSupervisor, p3FakeSupervisor, p3FakeSupervisor.currentRoot)
	if err != nil {
		t.Fatalf("construct P3 fake-supervisor runtime: %v", err)
	}
	p3Submitted := p3Runtime.submit(ctx, envelope, p3Fence)
	p4AcceptanceAssertExternalSupervisorWaiting(t, p3Submitted)
	if p3FakeSupervisor.deliveries() != 1 || p3FakeSupervisor.deliveryAttempts() != 1 {
		t.Fatalf("P3 fake supervisor delivery counts = %d/%d, want 1/1", p3FakeSupervisor.deliveries(), p3FakeSupervisor.deliveryAttempts())
	}
	p3Boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("load P3 receipt boundary: %v", err)
	}
	if p3Boundary.Receipt == nil || p3Boundary.Callback != nil || p3Boundary.Cancellation != nil {
		t.Fatalf("P3 receipt boundary = %#v, want receipt only", p3Boundary)
	}
	p3FakeSupervisor.publishCallback(envelope.HandoffID, "completed")
	p3Recovered := p3Runtime.recover(ctx, envelope.HandoffID)
	p4AcceptanceAssertExternalSupervisorWaiting(t, p3Recovered)
	p3Boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("load P3 callback boundary: %v", err)
	}
	if p3Boundary.Receipt == nil || p3Boundary.Callback == nil || p3Boundary.Callback.Result.TerminalState != "completed" || p3FakeSupervisor.reconciliations() != 1 {
		t.Fatalf("P3 receipt/callback boundary = %#v reconciliations=%d", p3Boundary, p3FakeSupervisor.reconciliations())
	}

	fact := store.CanonicalP4EvidenceAdmission()
	p4AcceptanceAssertImmutableChain(t, fact, proposal.CurrentRevisionHash, envelope, p3Fence)
	p4AcceptanceAssertEvidenceAdmission(t, fact)
	p4FakeVerifier := newP4AcceptanceFakeVerifier(fact)
	p4Runtime, err := newP4EvidenceAdmissionRuntime(journal, p4FakeVerifier)
	if err != nil {
		t.Fatalf("construct P4 fake-verifier runtime: %v", err)
	}
	p4Output := p4Runtime.submit(ctx, fact)
	p4AcceptanceAssertVerifiedWaiting(t, p4Output)
	if p4FakeVerifier.callCount() != 1 {
		t.Fatalf("P4 fake verifier calls after first admission = %d, want one", p4FakeVerifier.callCount())
	}
	persisted, err := journal.GetP4EvidenceAdmission(ctx, fact.VerifierRequest.InputHash)
	if err != nil {
		t.Fatalf("load durable P4 admission: %v", err)
	}
	if persisted != fact {
		t.Fatalf("durable P4 admission = %#v, want %#v", persisted, fact)
	}
	p4Replay := p4Runtime.submit(ctx, fact)
	p4AcceptanceAssertVerifiedWaiting(t, p4Replay)
	if p4FakeVerifier.callCount() != 1 {
		t.Fatalf("P4 exact replay called fake verifier %d times, want one", p4FakeVerifier.callCount())
	}
	p4AcceptanceAssertP4TableCount(t, journal, 1)

	for _, tc := range []struct {
		name   string
		mutate func(*store.P4EvidenceAdmission)
	}{
		{
			name: "repair attempt cap exceeded",
			mutate: func(value *store.P4EvidenceAdmission) {
				value.RepairAdmission.RepairAttemptNumber = value.RepairAdmission.RepairAttemptCap + 1
			},
		},
		{
			name: "fresh fence absent",
			mutate: func(value *store.P4EvidenceAdmission) {
				value.RepairAdmission.PriorFenceEvidenceHash = value.RepairAdmission.FreshFenceEvidenceHash
			},
		},
		{
			name: "typed MoA role drift",
			mutate: func(value *store.P4EvidenceAdmission) {
				value.RepairAdmission.TypedMoAGrant.GranteeRole = "other_repair_role"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := fact
			tc.mutate(&invalid)
			p4AcceptanceAssertFailureWaiting(t, p4Runtime.submit(ctx, invalid))
			if p4FakeVerifier.callCount() != 1 {
				t.Fatalf("invalid P4 admission reached fake verifier %d times", p4FakeVerifier.callCount())
			}
			p4AcceptanceAssertP4TableCount(t, journal, 1)
		})
	}

	localRuns := p4AcceptanceCount(t, journal, `SELECT COUNT(*) FROM runs`)
	repairRunTables := p4AcceptanceCount(t, journal, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE '%repair%run%'`)
	ompSessionTables := p4AcceptanceCount(t, journal, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE '%omp%'`)
	if localRuns != 0 || repairRunTables != 0 || ompSessionTables != 0 {
		t.Fatalf("acceptance harness created forbidden local execution facts: runs=%d repair-run-tables=%d omp-tables=%d", localRuns, repairRunTables, ompSessionTables)
	}
	p4AcceptanceAssertNoRuntimeCapabilities(t)

	p3AdapterCases, p3AdapterDigest := p4AcceptanceReadOracle(t, "../../contracts/p3f/fixtures/independent-supervisor-protocol-adapter-red-flags-v1.canonical.json")
	p4DenialCases, p4DenialDigest := p4AcceptanceReadOracle(t, "../../contracts/p4/fixtures/repair-admission-red-flags-v1.canonical.json")
	p4EvidenceDigest := p4AcceptanceFileDigest(t, "../../contracts/p4/fixtures/evidence-repair-admission-v1.canonical.json")
	if p3AdapterCases != fact.PredecessorChain.P3FAdapterRedFlagsCount || p3AdapterDigest != fact.PredecessorChain.P3FAdapterRedFlagsFixtureSHA256 ||
		p4DenialCases != p4AcceptanceP4DenialCases || p4DenialDigest != p4AcceptanceRedFlagsFixtureSHA || p4EvidenceDigest != p4AcceptanceEvidenceFixtureSHA {
		t.Fatalf("P3f/P4 oracle bindings = P3f:%d/%s P4:%d/%s evidence:%s", p3AdapterCases, p3AdapterDigest, p4DenialCases, p4DenialDigest, p4EvidenceDigest)
	}

	return map[string]any{
		"schema_version": "ananke.p4-self-development-acceptance.v1",
		"status":         "accepted_test_only_waiting_for_human",
		"harness": map[string]any{
			"fake_supervisor": "p3f_in_process_test_only",
			"fake_verifier":   "p4_in_process_test_only",
			"real_supervisor": "not_connected",
		},
		"immutable_chain": map[string]any{
			"p1_revision_hash":                     fact.PredecessorChain.P1RevisionHash,
			"p2_grill_fixture_sha256":              fact.PredecessorChain.P2GrillFixtureSHA256,
			"p3a_launch_admission_fixture_sha256":  fact.PredecessorChain.P3ALaunchAdmissionFixtureSHA256,
			"p3a_launch_spec_hash":                 fact.PredecessorChain.P3ALaunchSpecHash,
			"p3b_fence_contract":                   fact.PredecessorChain.P3BFenceContract,
			"p3c_recovery_action":                  fact.PredecessorChain.P3CRecoveryAction,
			"p3d_omp_audit_fixture_sha256":         fact.PredecessorChain.P3DOMPAuditFixtureSHA256,
			"p3f_adapter_fixture_sha256":           fact.PredecessorChain.P3FAdapterFixtureSHA256,
			"p3f_adapter_red_flags_count":          fact.PredecessorChain.P3FAdapterRedFlagsCount,
			"p3f_adapter_red_flags_fixture_sha256": fact.PredecessorChain.P3FAdapterRedFlagsFixtureSHA256,
			"p3f_predecessor_envelope_hash":        fact.PredecessorChain.P3FPredecessorEnvelopeHash,
			"p3f_route_mapping_hash":               fact.PredecessorChain.P3FRouteMappingHash,
		},
		"oracle_bindings": map[string]any{
			"p3f_adapter_red_flag_cases":           p3AdapterCases,
			"p3f_adapter_red_flags_fixture_sha256": p3AdapterDigest,
			"p4_evidence_fixture_sha256":           p4EvidenceDigest,
			"p4_repair_red_flag_cases":             p4DenialCases,
			"p4_repair_red_flags_fixture_sha256":   p4DenialDigest,
		},
		"p1": map[string]any{
			"proposal_id": proposal.ProposalID,
			"revision":    revision.Revision,
			"approval_id": approval.ApprovalID,
		},
		"p2_grill": map[string]any{
			"input_hash":         p2InputHash,
			"first_status":       string(p2First.Status),
			"first_new_records":  p2First.NewRecords,
			"replay_status":      string(p2Replay.Status),
			"replay_new_records": p2Replay.NewRecords,
		},
		"p3_handoff": map[string]any{
			"envelope_hash":             envelope.EnvelopeHash,
			"receipt_identity_hash":     p3Boundary.Receipt.ReceiptIdentityHash,
			"callback_identity_hash":    p3Boundary.Callback.CallbackIdentityHash,
			"callback_terminal_state":   p3Boundary.Callback.Result.TerminalState,
			"public_state":              p3Recovered.State,
			"public_verification_state": p3Recovered.VerificationState,
		},
		"p4_admission": map[string]any{
			"bundle_hash":                   fact.Bundle.BundleHash,
			"evidence_record_count":         12,
			"receipt_evidence_hash":         fact.Bundle.EvidenceHashes.ReceiptHash,
			"callback_evidence_hash":        fact.Bundle.EvidenceHashes.CallbackHash,
			"fresh_approval_evidence_hash":  fact.RepairAdmission.FreshApprovalEvidenceHash,
			"fresh_fence_evidence_hash":     fact.RepairAdmission.FreshFenceEvidenceHash,
			"full_fence_generation":         fact.FullFence.FenceGeneration,
			"repair_attempt_cap":            fact.RepairAdmission.RepairAttemptCap,
			"repair_attempt_number":         fact.RepairAdmission.RepairAttemptNumber,
			"typed_moa_role":                fact.RepairAdmission.TypedMoAGrant.GranteeRole,
			"typed_moa_route_evidence_hash": fact.RepairAdmission.TypedMoAGrant.RouteEvidenceHash,
			"output_admission":              p4Output.Admission,
			"output_repair_execution":       p4Output.RepairExecution,
			"output_state":                  p4Output.State,
			"output_verification_state":     p4Output.VerificationState,
			"replay_new_durable_facts":      persisted.VerifierReplay.NewDurableFacts,
		},
		"denied_before_verifier": []any{"repair_attempt_cap_exceeded", "fresh_fence_absent", "typed_moa_role_drift"},
		"observed_effects": map[string]any{
			"p3_fake_supervisor_deliveries":      p3FakeSupervisor.deliveries(),
			"p3_fake_supervisor_reconciliations": p3FakeSupervisor.reconciliations(),
			"p4_fake_verifier_calls":             p4FakeVerifier.callCount(),
			"durable_p4_fact_sets":               1,
			"local_run_rows":                     localRuns,
			"repair_run_tables":                  repairRunTables,
			"omp_session_tables":                 ompSessionTables,
			"runtime_network_capability":         "absent",
			"runtime_child_capability":           "absent",
			"runtime_source_artifact_capability": "hash_only_no_io",
		},
		"operator_prerequisites": p4AcceptanceOperatorPrerequisites(),
		"verification_commands": []any{
			"node --check contracts/p4/verify.mjs",
			"node contracts/p4/verify.mjs",
			"node contracts/p4/verify.mjs --self-test",
			"go test ./internal/lifecycle -run '^TestP4SelfDevelopmentAcceptance$' -count=1",
		},
	}
}

func p4AcceptanceGrillInput(revision store.Revision, revisionHash string) store.GrillInput {
	deadline := "2026-12-31T23:59:59Z"
	attemptCap := 3
	return store.GrillInput{
		SchemaVersion: store.GrillInputSchemaVersion,
		ProposalID:    revision.ProposalID,
		Revision:      revision.Revision,
		RevisionHash:  revisionHash,
		Declarations: store.GrillDeclarations{
			ObservableOutcome:   "declared",
			ScopeCompatibility:  "declared",
			AcceptanceEvidence:  "declared",
			DestructiveExternal: "none",
			LocalAuthorization:  "not_required",
			AdapterMode:         "none",
			WorktreeIsolation:   "not_applicable",
			Autonomy:            store.GrillAutonomy{Deadline: &deadline, AttemptCap: &attemptCap},
		},
	}
}

func p4AcceptanceAssertImmutableChain(t *testing.T, fact store.P4EvidenceAdmission, revisionHash string, envelope store.ExternalSupervisorEnvelope, fence store.LaunchFence) {
	t.Helper()
	chain := fact.PredecessorChain
	if chain.P1RevisionHash != revisionHash || chain.P3ALaunchSpecHash != envelope.LaunchSpecHash ||
		chain.P3FRouteMappingHash != envelope.RouteMappingHash || chain.P3FAdapterRedFlagsCount != 37 ||
		chain.P3FAdapterFixtureSHA256 != "sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44" ||
		chain.P3FAdapterRedFlagsFixtureSHA256 != "sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525" ||
		chain.P2GrillFixtureSHA256 != "sha256:d9301e896e1cd223c6a05df37eea8fd862c955a0ba9e0985616bffcae0e35caa" ||
		chain.P3ALaunchAdmissionFixtureSHA256 != "sha256:4e6afde3722009df0447ef95271cb72629d7ca3bff103cee15fe229a6f4bea16" ||
		chain.P3BFenceContract != "current_full_fence_required_no_token_projection" || chain.P3CRecoveryAction != "retry_process_admission" ||
		chain.P3DOMPAuditFixtureSHA256 != "sha256:9c8ca561416c82f98ad49d08c625bb5b11be468fb306cd254e7700468ac0e7f3" ||
		fence.ClaimID == "" || fence.ClaimTokenHash == "" {
		t.Fatalf("P1 through P3f immutable chain = %#v with P3 fence %#v", chain, fence)
	}
}

func p4AcceptanceAssertEvidenceAdmission(t *testing.T, fact store.P4EvidenceAdmission) {
	t.Helper()
	hashes := fact.Bundle.EvidenceHashes
	for _, value := range []string{
		hashes.ProposalHash, hashes.RevisionHash, hashes.ApprovalHash, hashes.FenceHash, hashes.EnvelopeHash, hashes.ReceiptHash,
		hashes.CallbackHash, hashes.SourceHash, hashes.ArtifactHash, hashes.RouteHash, hashes.TestHash, hashes.EvaluationHash,
	} {
		if value == "" {
			t.Fatalf("P4 immutable evidence bundle contains an empty evidence hash: %#v", hashes)
		}
	}
	repair := fact.RepairAdmission
	grant := repair.TypedMoAGrant
	if repair.RepairAttemptCap != 2 || repair.RepairAttemptNumber != 1 || repair.ExactEvidenceBundleHash != fact.Bundle.BundleHash ||
		repair.ExactEvidenceHashes != hashes || repair.PriorApprovalEvidenceHash == repair.FreshApprovalEvidenceHash ||
		repair.PriorFenceEvidenceHash == repair.FreshFenceEvidenceHash || repair.FreshFenceEvidenceHash != hashes.FenceHash ||
		grant.GranteeRole != repair.AllowedRole || grant.RouteEvidenceHash != repair.AllowedRouteEvidenceHash ||
		grant.EvidenceBundleHash != fact.Bundle.BundleHash || grant.ApprovalEvidenceHash != repair.FreshApprovalEvidenceHash ||
		grant.FenceEvidenceHash != repair.FreshFenceEvidenceHash || fact.FullFence.ClaimID == "" || fact.FullFence.ClaimTokenHash == "" || fact.FullFence.FenceGeneration != 8 {
		t.Fatalf("P4 evidence/admission/fresh-fence/typed-MoA binding = %#v / %#v / %#v", fact.Bundle, repair, fact.FullFence)
	}
}

func p4AcceptanceAssertExternalSupervisorWaiting(t *testing.T, output externalSupervisorPublicOutput) {
	t.Helper()
	if output.SchemaVersion != externalSupervisorOutputSchemaVersion || output.State != "waiting_for_human" ||
		output.VerificationState != "not_run" || output.Result != nil || len(output.Events) != 0 {
		t.Fatalf("P3 external-supervisor output = %#v, want closed waiting_for_human", output)
	}
}

func p4AcceptanceAssertVerifiedWaiting(t *testing.T, output p4EvidenceAdmissionPublicOutput) {
	t.Helper()
	if output.SchemaVersion != p4EvidenceAdmissionPublicOutputSchemaVersion || output.Admission != "bounded_repair_admissible_design_only" ||
		output.BundleHash == nil || output.RepairExecution != "not_authorized_by_verifier" || output.State != "waiting_for_human" ||
		output.VerificationState != "verified" {
		t.Fatalf("P4 verified public output = %#v, want design-only waiting_for_human", output)
	}
}

func p4AcceptanceAssertFailureWaiting(t *testing.T, output p4EvidenceAdmissionPublicOutput) {
	t.Helper()
	if output.SchemaVersion != p4EvidenceAdmissionPublicOutputSchemaVersion || output.Admission != "rejected" || output.BundleHash != nil ||
		output.RepairExecution != "not_authorized" || output.State != "waiting_for_human" || output.VerificationState != "not_run" {
		t.Fatalf("P4 rejected public output = %#v, want closed waiting_for_human", output)
	}
}

func p4AcceptanceAssertP4TableCount(t *testing.T, journal *store.Store, want int) {
	t.Helper()
	for _, table := range []string{"p4_evidence_bundles", "p4_repair_admissions", "p4_verifier_requests", "p4_verifier_outputs", "p4_verifier_replays"} {
		if got := p4AcceptanceCount(t, journal, "SELECT COUNT(*) FROM "+table); got != want {
			t.Fatalf("%s rows = %d, want %d", table, got, want)
		}
	}
}

func p4AcceptanceCount(t *testing.T, journal *store.Store, query string) int {
	t.Helper()
	var count int
	if err := journal.DB().QueryRowContext(context.Background(), query).Scan(&count); err != nil {
		t.Fatalf("count acceptance effect with %q: %v", query, err)
	}
	return count
}

func p4AcceptanceReadOracle(t *testing.T, path string) (int, string) {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read fixture oracle %s: %v", path, err)
	}
	var oracle struct {
		Cases []json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(bytes, &oracle); err != nil {
		t.Fatalf("decode fixture oracle %s: %v", path, err)
	}
	return len(oracle.Cases), p4AcceptanceDigest(bytes)
}

func p4AcceptanceFileDigest(t *testing.T, path string) string {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read canonical fixture %s: %v", path, err)
	}
	return p4AcceptanceDigest(bytes)
}

func p4AcceptanceDigest(bytes []byte) string {
	sum := sha256.Sum256(bytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The tested P3/P4 route and both fakes are deliberately capability-free. The
// source audit excludes network, OS/file, process, syscall, and OMP plumbing;
// SQLite is the only permitted durable effect in this acceptance scenario.
func p4AcceptanceAssertNoRuntimeCapabilities(t *testing.T) {
	t.Helper()
	for _, path := range []string{
		"fenced_launch.go",
		"external_supervisor_handoff.go",
		"p4_evidence_admission.go",
		"external_supervisor_handoff_fake_test.go",
		"p4_self_development_acceptance_fake_test.go",
		"../store/launch_admission.go",
		"../store/external_supervisor_handoff.go",
		"../store/p4_evidence_admission.go",
	} {
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filepath.Clean(path), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse runtime capability surface %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			value := imported.Path.Value[1 : len(imported.Path.Value)-1]
			for _, forbidden := range []string{"net", "net/", "os", "os/", "syscall", "golang.org/x/sys/unix"} {
				if value == forbidden || (len(forbidden) > 1 && forbidden[len(forbidden)-1] == '/' && len(value) >= len(forbidden) && value[:len(forbidden)] == forbidden) {
					t.Fatalf("runtime capability surface %s imports forbidden %q", path, value)
				}
			}
		}
	}
}

func p4AcceptanceOperatorPrerequisites() []any {
	return []any{
		map[string]any{"id": "independent_supervisor_release", "required_before_connection": "Validate an independently released supervisor artifact and its provenance; the in-process fake is never deployable."},
		map[string]any{"id": "current_trust_root", "required_before_connection": "Install the current supervisor trust root and implement signature, rotation, and revocation validation for receipt and callback identities."},
		map[string]any{"id": "authenticated_protocol_transport", "required_before_connection": "Implement the P3f sealed-envelope protocol transport with authenticated channel binding; do not serialize endpoints or credentials into P4 evidence."},
		map[string]any{"id": "full_fence_reauthentication", "required_before_connection": "Reauthenticate the complete current P3 fence at delivery, reconciliation, and cancellation; token-only projection is prohibited."},
		map[string]any{"id": "fresh_evidence_and_typed_moa", "required_before_connection": "Obtain and independently verify a fresh approval, fresh fence, exact evidence bundle, and valid typed MoA grant for the single allowed role and route."},
		map[string]any{"id": "production_failure_protocol", "required_before_connection": "Prove fail-closed behavior for absent, invalid, replayed, rotated-root, and cancellation callbacks without inferring repair or terminal success."},
		map[string]any{"id": "explicit_human_rollout", "required_before_connection": "Record a human-approved production integration and security review; P4 design admission alone authorizes neither connection nor repair."},
	}
}

func assertP4SelfDevelopmentAcceptanceReport(t *testing.T, report map[string]any) {
	t.Helper()
	if report["schema_version"] != "ananke.p4-self-development-acceptance.v1" || report["status"] != "accepted_test_only_waiting_for_human" {
		t.Fatalf("acceptance report identity = %#v", report)
	}
	oracle, ok := report["oracle_bindings"].(map[string]any)
	if !ok || oracle["p3f_adapter_red_flag_cases"] != 37 || oracle["p4_repair_red_flag_cases"] != p4AcceptanceP4DenialCases {
		t.Fatalf("acceptance report oracle bindings = %#v", oracle)
	}
	p4Admission, ok := report["p4_admission"].(map[string]any)
	if !ok || p4Admission["repair_attempt_cap"] != 2 || p4Admission["typed_moa_role"] != "self_development_repair_runner" ||
		p4Admission["output_state"] != "waiting_for_human" || p4Admission["output_repair_execution"] != "not_authorized_by_verifier" ||
		p4Admission["replay_new_durable_facts"] != 0 {
		t.Fatalf("acceptance report P4 admission = %#v", p4Admission)
	}
	effects, ok := report["observed_effects"].(map[string]any)
	if !ok || effects["local_run_rows"] != 0 || effects["repair_run_tables"] != 0 || effects["omp_session_tables"] != 0 ||
		effects["runtime_network_capability"] != "absent" || effects["runtime_child_capability"] != "absent" || effects["runtime_source_artifact_capability"] != "hash_only_no_io" {
		t.Fatalf("acceptance report effects = %#v", effects)
	}
	prerequisites, ok := report["operator_prerequisites"].([]any)
	if !ok || len(prerequisites) != 7 {
		t.Fatalf("acceptance report operator prerequisites = %#v", report["operator_prerequisites"])
	}
}

func assertP4SelfDevelopmentAcceptanceArtifact(t *testing.T, report map[string]any) {
	t.Helper()
	artifact, err := os.ReadFile(filepath.Clean(p4AcceptanceArtifactPath))
	if err != nil {
		t.Fatalf("read machine-readable P4 acceptance artifact: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(artifact))
	decoder.UseNumber()
	var declared map[string]any
	if err := decoder.Decode(&declared); err != nil {
		t.Fatalf("decode machine-readable P4 acceptance artifact: %v", err)
	}
	canonical, err := json.Marshal(declared)
	if err != nil {
		t.Fatalf("canonicalize machine-readable P4 acceptance artifact: %v", err)
	}
	if !bytes.Equal(artifact, canonical) {
		t.Fatalf("machine-readable P4 acceptance artifact is not canonical JSON")
	}
	expected, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("canonicalize observed P4 acceptance report: %v", err)
	}
	if !bytes.Equal(canonical, expected) {
		t.Fatalf("machine-readable P4 acceptance artifact drifted\n got: %s\nwant: %s", canonical, expected)
	}
}
