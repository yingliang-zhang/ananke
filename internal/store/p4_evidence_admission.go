package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	P4EvidenceBundleSchemaVersion  = "ananke.self-development-evidence-bundle.v1"
	P4RepairAdmissionSchemaVersion = "ananke.self-development-bounded-repair-admission.v1"
	P4VerifierInputSchemaVersion   = "ananke.self-development-evidence-verifier-input.v1"
	P4VerifierOutputSchemaVersion  = "ananke.self-development-evidence-verifier-output.v1"
	P4VerifierReplaySchemaVersion  = "ananke.self-development-evidence-verifier-replay.v1"
	P4TypedMoAGrantSchemaVersion   = "ananke.moa-typed-role-grant.v1"
	p4AllowedRepairRole            = "self_development_repair_runner"
	p4RepairAttemptCap             = 2
	p4P3FAdapterRedFlagsCount      = 37
	p4FullFenceClaimID             = "p4_repair_fence_claim_001"
	p4FullFenceClaimTokenHash      = "sha256:7506737a97ecf137840f1f6ec0c2c9c210733fc35751fcda967a75dfe084eacd"
)

var (
	ErrP4EvidenceInvalid  = errors.New("P4 evidence admission is invalid")
	ErrP4EvidenceConflict = errors.New("P4 evidence admission conflicts with durable facts")
	ErrP4EvidenceNotFound = errors.New("P4 evidence admission is not found")
)

// P4EvidenceHashes is the closed immutable hash map for the twelve P4 evidence
// records. It stores hashes only; it cannot carry source or artifact bytes.
type P4EvidenceHashes struct {
	ApprovalHash   string `json:"approval_hash"`
	ArtifactHash   string `json:"artifact_hash"`
	CallbackHash   string `json:"callback_hash"`
	EnvelopeHash   string `json:"envelope_hash"`
	EvaluationHash string `json:"evaluation_hash"`
	FenceHash      string `json:"fence_hash"`
	ProposalHash   string `json:"proposal_hash"`
	ReceiptHash    string `json:"receipt_hash"`
	RevisionHash   string `json:"revision_hash"`
	RouteHash      string `json:"route_hash"`
	SourceHash     string `json:"source_hash"`
	TestHash       string `json:"test_hash"`
}

// P4PredecessorChain pins the complete P1 through P3f fixture ancestry before
// P4 evidence becomes durable.
type P4PredecessorChain struct {
	P1RevisionHash                  string `json:"p1_revision_hash"`
	P2GrillFixtureSHA256            string `json:"p2_grill_fixture_sha256"`
	P3ALaunchAdmissionFixtureSHA256 string `json:"p3a_launch_admission_fixture_sha256"`
	P3ALaunchSpecHash               string `json:"p3a_launch_spec_hash"`
	P3BFenceContract                string `json:"p3b_fence_contract"`
	P3CRecoveryAction               string `json:"p3c_recovery_action"`
	P3DOMPAuditFixtureSHA256        string `json:"p3d_omp_audit_fixture_sha256"`
	P3FAdapterFixtureSHA256         string `json:"p3f_adapter_fixture_sha256"`
	P3FAdapterRedFlagsCount         int    `json:"p3f_adapter_red_flags_count"`
	P3FAdapterRedFlagsFixtureSHA256 string `json:"p3f_adapter_red_flags_fixture_sha256"`
	P3FPredecessorEnvelopeHash      string `json:"p3f_predecessor_envelope_hash"`
	P3FRouteMappingHash             string `json:"p3f_route_mapping_hash"`
}

// P4EvidenceBundle is a hash-only immutable bundle. Trust and release
// identities are separated below so their exact binding remains explicit.
type P4EvidenceBundle struct {
	BundleHash                      string           `json:"bundle_hash"`
	BundleID                        string           `json:"bundle_id"`
	EvidenceHashes                  P4EvidenceHashes `json:"evidence_hashes"`
	IssuedAt                        string           `json:"issued_at"`
	P3FAdapterFixtureSHA256         string           `json:"p3f_adapter_fixture_sha256"`
	P3FAdapterRedFlagsCount         int              `json:"p3f_adapter_red_flags_count"`
	P3FAdapterRedFlagsFixtureSHA256 string           `json:"p3f_adapter_red_flags_fixture_sha256"`
	SchemaVersion                   string           `json:"schema_version"`
	VerifierReleaseIdentityHash     string           `json:"verifier_release_identity_hash"`
	VerifierTrustIdentityHash       string           `json:"verifier_trust_identity_hash"`
}

// P4VerifierTrustIdentity identifies a trust root by hashes only; it is not a
// key, signature, endpoint, or executable verifier.
type P4VerifierTrustIdentity struct {
	SchemaVersion       string `json:"schema_version"`
	TrustIdentityHash   string `json:"trust_identity_hash"`
	TrustRootID         string `json:"trust_root_id"`
	TrustRootSPKISHA256 string `json:"trust_root_spki_sha256"`
}

// P4VerifierReleaseIdentity identifies the verifier release bound to the trust
// identity. It is evidence only, not a released verifier implementation.
type P4VerifierReleaseIdentity struct {
	ReleaseArtifactSHA256 string `json:"release_artifact_sha256"`
	ReleaseID             string `json:"release_id"`
	ReleaseIdentityHash   string `json:"release_identity_hash"`
	ReleaseManifestHash   string `json:"release_manifest_hash"`
	SchemaVersion         string `json:"schema_version"`
	TrustIdentityHash     string `json:"trust_identity_hash"`
}

type P4VerifierIdentities struct {
	Release P4VerifierReleaseIdentity `json:"release"`
	Trust   P4VerifierTrustIdentity   `json:"trust"`
}

// P4TypedMoAGrant is the typed, time-bounded, evidence-bound grant checked by
// P4 admission. It authorizes no local execution.
type P4TypedMoAGrant struct {
	ApprovalEvidenceHash    string `json:"approval_evidence_hash"`
	EvidenceBundleHash      string `json:"evidence_bundle_hash"`
	FenceEvidenceHash       string `json:"fence_evidence_hash"`
	GrantHash               string `json:"grant_hash"`
	GrantID                 string `json:"grant_id"`
	GranteeRole             string `json:"grantee_role"`
	IssuedAt                string `json:"issued_at"`
	IssuerTrustIdentityHash string `json:"issuer_trust_identity_hash"`
	NotAfter                string `json:"not_after"`
	RouteEvidenceHash       string `json:"route_evidence_hash"`
	SchemaVersion           string `json:"schema_version"`
}

// P4RepairAdmission contains the fixed, design-only two-attempt repair policy.
// Its successful validation remains evidence, not local repair permission.
type P4RepairAdmission struct {
	AdmissionHash             string           `json:"admission_hash"`
	AdmissionID               string           `json:"admission_id"`
	AdmissionState            string           `json:"admission_state"`
	AllowedRole               string           `json:"allowed_role"`
	AllowedRouteEvidenceHash  string           `json:"allowed_route_evidence_hash"`
	ExactEvidenceBundleHash   string           `json:"exact_evidence_bundle_hash"`
	ExactEvidenceHashes       P4EvidenceHashes `json:"exact_evidence_hashes"`
	FreshApprovalEvidenceHash string           `json:"fresh_approval_evidence_hash"`
	FreshFenceEvidenceHash    string           `json:"fresh_fence_evidence_hash"`
	InferredSuccess           string           `json:"inferred_success"`
	PriorApprovalEvidenceHash string           `json:"prior_approval_evidence_hash"`
	PriorFenceEvidenceHash    string           `json:"prior_fence_evidence_hash"`
	RepairAttemptCap          int              `json:"repair_attempt_cap"`
	RepairAttemptNumber       int              `json:"repair_attempt_number"`
	SchemaVersion             string           `json:"schema_version"`
	TypedMoAGrant             P4TypedMoAGrant  `json:"typed_moa_grant"`
}

// P4VerifierRequest is the immutable request handed only to an injected test
// verifier. Production supplies no verifier, transport, or network client.
type P4VerifierRequest struct {
	BundleHash                      string `json:"bundle_hash"`
	InputHash                       string `json:"input_hash"`
	P3FAdapterFixtureSHA256         string `json:"p3f_adapter_fixture_sha256"`
	P3FAdapterRedFlagsCount         int    `json:"p3f_adapter_red_flags_count"`
	P3FAdapterRedFlagsFixtureSHA256 string `json:"p3f_adapter_red_flags_fixture_sha256"`
	RepairAdmissionHash             string `json:"repair_admission_hash"`
	SchemaVersion                   string `json:"schema_version"`
	VerifierReleaseIdentityHash     string `json:"verifier_release_identity_hash"`
	VerifierTrustIdentityHash       string `json:"verifier_trust_identity_hash"`
}

// P4VerifierOutput remains waiting_for_human even when its sealed evidence is
// verified. It cannot state repair execution success or authorization.
type P4VerifierOutput struct {
	Admission         string `json:"admission"`
	BundleHash        string `json:"bundle_hash"`
	OutputHash        string `json:"output_hash"`
	RepairExecution   string `json:"repair_execution"`
	SchemaVersion     string `json:"schema_version"`
	State             string `json:"state"`
	VerificationState string `json:"verification_state"`
}

// P4VerifierReplay proves exact output replay without appending durable facts.
type P4VerifierReplay struct {
	InputHash       string `json:"input_hash"`
	NewDurableFacts int    `json:"new_durable_facts"`
	OutputHash      string `json:"output_hash"`
	ReplayHash      string `json:"replay_hash"`
	ReplayResult    string `json:"replay_result"`
	SchemaVersion   string `json:"schema_version"`
}

// P4VerifierResponse is an injected verifier response. The repository has no
// concrete production implementation; the sole fake lives in a _test.go file.
type P4VerifierResponse struct {
	Output P4VerifierOutput
	Replay P4VerifierReplay
}

// P4FullFence retains every opaque member of the P3b fence. It deliberately
// cannot be reduced to a generation or claim identifier alone.
type P4FullFence struct {
	ClaimID         string `json:"claim_id"`
	ClaimTokenHash  string `json:"claim_token_hash"`
	FenceGeneration int    `json:"fence_generation"`
}

// P4Verifier is intentionally a narrow in-process test seam. It has no URI,
// credentials, process, OMP, source, artifact, or repair capability.
type P4Verifier interface {
	VerifyP4Evidence(context.Context, P4VerifierRequest) (P4VerifierResponse, error)
}

// P4EvidenceAdmission is the complete transactionally durable P4 fact. The
// complete LaunchFence is retained as an opaque hash-bearing tuple, never
// projected to a token-only fence or used to create a run.
type P4EvidenceAdmission struct {
	PredecessorChain   P4PredecessorChain
	Bundle             P4EvidenceBundle
	VerifierIdentities P4VerifierIdentities
	RepairAdmission    P4RepairAdmission
	FullFence          P4FullFence
	VerifierRequest    P4VerifierRequest
	VerifierOutput     P4VerifierOutput
	VerifierReplay     P4VerifierReplay
}

// CanonicalP4EvidenceAdmission returns the one byte-pinned P4 fact frozen by
// contracts/p4. It reads no fixture, source, artifact, verifier, or network.
func CanonicalP4EvidenceAdmission() P4EvidenceAdmission {
	hashes := P4EvidenceHashes{
		ApprovalHash:   "sha256:813a115813a2f77b21c186d286a3c75ac288ae87dc39ba4c9b3ea7f55fb5df1c",
		ArtifactHash:   "sha256:3ce83fc3d4e64e8a3398e6fa1d73453a1d51d3ef00b3224dc4272b8e902ee681",
		CallbackHash:   "sha256:491cc0f8b2d0f4db3f5905144ed9a37218f6d5176f1e1cada6dd19a3e9bf4b4f",
		EnvelopeHash:   "sha256:e49bf9b4bf1704fb415bb4593215f5293d93fe7dfcc8369258bc01a9dc156634",
		EvaluationHash: "sha256:6b22a74ce9e5a764ee5b0a27017a0ddbc443dbedd8d24d44d699078fa857db3c",
		FenceHash:      "sha256:5d806ac929f58e4c3d1a3022b9eb4519b0a347394414d3db64a9f01ba6443b81",
		ProposalHash:   "sha256:abcff70415d6dbf27ebcb0acb607e99106d4776eff027651f0d508e3f65c0caa",
		ReceiptHash:    "sha256:356c4c55ff87312a74bd77454bb249617cb54b97718617b8a98a18db9349ad30",
		RevisionHash:   "sha256:ee57c340e2ffd89af1835cb56d6d76385c23e2fd331b074c8477e90fd902b179",
		RouteHash:      "sha256:d6ef174437a6db28dbc916c014db14228b933eb23da706d77aaa6be0ff412a21",
		SourceHash:     "sha256:0431228a04bd62100d2ddbb029c66b03fb6d9eac5d0d95fd128635e1a14f600a",
		TestHash:       "sha256:b6a6295a8dd057a3336b4c1e1ce9bd3dcd2448965bbe4ea68d6be38373e68666",
	}
	chain := P4PredecessorChain{
		P1RevisionHash:                  "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263",
		P2GrillFixtureSHA256:            "sha256:d9301e896e1cd223c6a05df37eea8fd862c955a0ba9e0985616bffcae0e35caa",
		P3ALaunchAdmissionFixtureSHA256: "sha256:4e6afde3722009df0447ef95271cb72629d7ca3bff103cee15fe229a6f4bea16",
		P3ALaunchSpecHash:               "sha256:bbc43093a3b00c49c1d2ac26db08e6dd36ff72174ded15de9408702af3a9e658",
		P3BFenceContract:                "current_full_fence_required_no_token_projection",
		P3CRecoveryAction:               "retry_process_admission",
		P3DOMPAuditFixtureSHA256:        "sha256:9c8ca561416c82f98ad49d08c625bb5b11be468fb306cd254e7700468ac0e7f3",
		P3FAdapterFixtureSHA256:         "sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44",
		P3FAdapterRedFlagsCount:         p4P3FAdapterRedFlagsCount,
		P3FAdapterRedFlagsFixtureSHA256: "sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525",
		P3FPredecessorEnvelopeHash:      "sha256:3dc8c169234fcd2e496e38ab5de327c058f276be91b65cf13f1c9ae7faa12473",
		P3FRouteMappingHash:             "sha256:a468e940e5dd5752285b8aba2533109cfde2d8b259a007647ca6f431e0736603",
	}
	trust := P4VerifierTrustIdentity{
		SchemaVersion:       "ananke.self-development-verifier-trust-identity.v1",
		TrustIdentityHash:   "sha256:a6e3ee0e4a6d2f8787395c1e5a3100db3e84aa0b2dc6cb9fa24942718ce09b51",
		TrustRootID:         "ananke_p4_verifier_trust_root_v1",
		TrustRootSPKISHA256: "sha256:9a3d25b39c74e4ca3721454d1e1ed47c8a502b476e557bc4f89e6a35be2730e7",
	}
	release := P4VerifierReleaseIdentity{
		ReleaseArtifactSHA256: "sha256:949c6d839dc966d306a3f792daf75f305b54951ddc9e563cdf19163f2ead08f1",
		ReleaseID:             "ananke_p4_verifier_release_v1",
		ReleaseIdentityHash:   "sha256:ecbaaf39e5fe23418e98718e957240318f9ec8d3ed35449d10d13063d2273590",
		ReleaseManifestHash:   "sha256:bdf36316acd6d9f041e803d5979057607aaa96b1c72587abd9a51e6328bf52d6",
		SchemaVersion:         "ananke.self-development-verifier-release-identity.v1",
		TrustIdentityHash:     trust.TrustIdentityHash,
	}
	bundle := P4EvidenceBundle{
		BundleHash:                      "sha256:12ec67830ffa00eb637ed0594b46b89be79c28cce3854574f540f9dc2b6a5c0d",
		BundleID:                        "evidence_bundle_p4_001",
		EvidenceHashes:                  hashes,
		IssuedAt:                        "2026-07-25T00:06:00Z",
		P3FAdapterFixtureSHA256:         chain.P3FAdapterFixtureSHA256,
		P3FAdapterRedFlagsCount:         chain.P3FAdapterRedFlagsCount,
		P3FAdapterRedFlagsFixtureSHA256: chain.P3FAdapterRedFlagsFixtureSHA256,
		SchemaVersion:                   P4EvidenceBundleSchemaVersion,
		VerifierReleaseIdentityHash:     release.ReleaseIdentityHash,
		VerifierTrustIdentityHash:       trust.TrustIdentityHash,
	}
	grant := P4TypedMoAGrant{
		ApprovalEvidenceHash:    hashes.ApprovalHash,
		EvidenceBundleHash:      bundle.BundleHash,
		FenceEvidenceHash:       hashes.FenceHash,
		GrantHash:               "sha256:18e46658224785bd61fee8793069d621de78d83dc4e32d035f4492342a71fc3e",
		GrantID:                 "p4_repair_grant_001",
		GranteeRole:             p4AllowedRepairRole,
		IssuedAt:                "2026-07-25T00:05:00Z",
		IssuerTrustIdentityHash: trust.TrustIdentityHash,
		NotAfter:                "2026-07-25T00:07:00Z",
		RouteEvidenceHash:       hashes.RouteHash,
		SchemaVersion:           P4TypedMoAGrantSchemaVersion,
	}
	admission := P4RepairAdmission{
		AdmissionHash:             "sha256:54446404a8e615d1abf63abd396b303ae86047be14a1eeeaabb6176c2d9deedb",
		AdmissionID:               "repair_admission_p4_001",
		AdmissionState:            "design_only_no_repair_execution",
		AllowedRole:               p4AllowedRepairRole,
		AllowedRouteEvidenceHash:  hashes.RouteHash,
		ExactEvidenceBundleHash:   bundle.BundleHash,
		ExactEvidenceHashes:       hashes,
		FreshApprovalEvidenceHash: hashes.ApprovalHash,
		FreshFenceEvidenceHash:    hashes.FenceHash,
		InferredSuccess:           "forbidden",
		PriorApprovalEvidenceHash: "sha256:c833eaf770a5955865fda75b57f3f0b56fcd6ed6cf6af214988d987afe6e332c",
		PriorFenceEvidenceHash:    "sha256:a09d605ca3fd11346d5ded758037ea1f0dd5551eb6b112717c4479b0c3650214",
		RepairAttemptCap:          p4RepairAttemptCap,
		RepairAttemptNumber:       1,
		SchemaVersion:             P4RepairAdmissionSchemaVersion,
		TypedMoAGrant:             grant,
	}
	request := P4VerifierRequest{
		BundleHash:                      bundle.BundleHash,
		InputHash:                       "sha256:c7d9a26636b16df70d77d443a37df7c91d640731c1dbbb9ad339990cd9b77eb8",
		P3FAdapterFixtureSHA256:         chain.P3FAdapterFixtureSHA256,
		P3FAdapterRedFlagsCount:         chain.P3FAdapterRedFlagsCount,
		P3FAdapterRedFlagsFixtureSHA256: chain.P3FAdapterRedFlagsFixtureSHA256,
		RepairAdmissionHash:             admission.AdmissionHash,
		SchemaVersion:                   P4VerifierInputSchemaVersion,
		VerifierReleaseIdentityHash:     release.ReleaseIdentityHash,
		VerifierTrustIdentityHash:       trust.TrustIdentityHash,
	}
	output := P4VerifierOutput{
		Admission:         "bounded_repair_admissible_design_only",
		BundleHash:        bundle.BundleHash,
		OutputHash:        "sha256:320722b436d6876022ba7b1ae0428e666e7c168835b1464516e745a0e5ff3818",
		RepairExecution:   "not_authorized_by_verifier",
		SchemaVersion:     P4VerifierOutputSchemaVersion,
		State:             "waiting_for_human",
		VerificationState: "verified",
	}
	replay := P4VerifierReplay{
		InputHash:       request.InputHash,
		NewDurableFacts: 0,
		OutputHash:      output.OutputHash,
		ReplayHash:      "sha256:966441563f4f798185d93e722df20bb2577ca7aa9cae270204df2627e0cf5cfb",
		ReplayResult:    "exact_canonical_output",
		SchemaVersion:   P4VerifierReplaySchemaVersion,
	}
	return P4EvidenceAdmission{
		PredecessorChain:   chain,
		Bundle:             bundle,
		VerifierIdentities: P4VerifierIdentities{Release: release, Trust: trust},
		RepairAdmission:    admission,
		FullFence: P4FullFence{
			ClaimID:         p4FullFenceClaimID,
			ClaimTokenHash:  p4FullFenceClaimTokenHash,
			FenceGeneration: 8,
		},
		VerifierRequest: request,
		VerifierOutput:  output,
		VerifierReplay:  replay,
	}
}

// PersistP4EvidenceAdmission writes the immutable evidence bundle, repair
// admission, verifier request, output, and zero-fact replay in one SQLite
// transaction. Exact replay returns the existing fact without a new write.
func (s *Store) PersistP4EvidenceAdmission(ctx context.Context, fact P4EvidenceAdmission) (P4EvidenceAdmission, error) {
	if err := validateP4EvidenceAdmission(fact); err != nil {
		return P4EvidenceAdmission{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	defer func() { _ = tx.Rollback() }()
	return s.persistP4EvidenceAdmissionTx(ctx, tx, fact)
}

// VerifyAndPersistP4EvidenceAdmission holds the immediate SQLite transaction
// across the injected test verifier and all immutable writes. A prior exact
// request returns its durable output before invoking the verifier, so replay
// appends zero facts and concurrent submissions cannot call the fake twice.
func (s *Store) VerifyAndPersistP4EvidenceAdmission(ctx context.Context, fact P4EvidenceAdmission, verifier P4Verifier) (P4EvidenceAdmission, error) {
	if verifier == nil {
		return P4EvidenceAdmission{}, fmt.Errorf("%w: nil verifier", ErrP4EvidenceInvalid)
	}
	if err := validateP4EvidenceAdmissionInput(fact); err != nil {
		return P4EvidenceAdmission{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	defer func() { _ = tx.Rollback() }()

	existing, found, err := loadP4EvidenceAdmission(ctx, tx, fact.VerifierRequest.InputHash)
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	if found {
		return existing, nil
	}
	response, err := verifier.VerifyP4Evidence(ctx, fact.VerifierRequest)
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	fact.VerifierOutput = response.Output
	fact.VerifierReplay = response.Replay
	if err := validateP4EvidenceAdmission(fact); err != nil {
		return P4EvidenceAdmission{}, err
	}
	return s.persistP4EvidenceAdmissionTx(ctx, tx, fact)
}

func (s *Store) persistP4EvidenceAdmissionTx(ctx context.Context, tx *sql.Tx, fact P4EvidenceAdmission) (P4EvidenceAdmission, error) {
	existing, found, err := loadP4EvidenceAdmission(ctx, tx, fact.VerifierRequest.InputHash)
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	if found {
		if existing != fact {
			return P4EvidenceAdmission{}, ErrP4EvidenceConflict
		}
		return existing, nil
	}
	createdAt := nowStamp()
	bundleJSON, err := p4CanonicalJSON(p4EvidenceBundleValue(fact.Bundle))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	chainJSON, err := p4CanonicalJSON(p4PredecessorChainValue(fact.PredecessorChain))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	identitiesJSON, err := p4CanonicalJSON(p4VerifierIdentitiesValue(fact.VerifierIdentities))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	admissionJSON, err := p4CanonicalJSON(p4RepairAdmissionValue(fact.RepairAdmission))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	fenceJSON, err := p4CanonicalJSON(p4FullFenceValue(fact.FullFence))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	requestJSON, err := p4CanonicalJSON(p4VerifierRequestValue(fact.VerifierRequest))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	outputJSON, err := p4CanonicalJSON(p4VerifierOutputValue(fact.VerifierOutput))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	replayJSON, err := p4CanonicalJSON(p4VerifierReplayValue(fact.VerifierReplay))
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO p4_evidence_bundles
		(bundle_hash, bundle_id, bundle_json, predecessor_chain_json, verifier_identities_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, fact.Bundle.BundleHash, fact.Bundle.BundleID, bundleJSON, chainJSON, identitiesJSON, createdAt); err != nil {
		return P4EvidenceAdmission{}, fmt.Errorf("insert P4 evidence bundle: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO p4_repair_admissions
		(admission_hash, admission_id, bundle_hash, admission_json, full_fence_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, fact.RepairAdmission.AdmissionHash, fact.RepairAdmission.AdmissionID, fact.Bundle.BundleHash, admissionJSON, fenceJSON, createdAt); err != nil {
		return P4EvidenceAdmission{}, fmt.Errorf("insert P4 repair admission: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO p4_verifier_requests
		(input_hash, bundle_hash, admission_hash, request_json, created_at)
		VALUES (?, ?, ?, ?, ?)`, fact.VerifierRequest.InputHash, fact.Bundle.BundleHash, fact.RepairAdmission.AdmissionHash, requestJSON, createdAt); err != nil {
		return P4EvidenceAdmission{}, fmt.Errorf("insert P4 verifier request: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO p4_verifier_outputs
		(output_hash, input_hash, bundle_hash, output_json, created_at)
		VALUES (?, ?, ?, ?, ?)`, fact.VerifierOutput.OutputHash, fact.VerifierRequest.InputHash, fact.Bundle.BundleHash, outputJSON, createdAt); err != nil {
		return P4EvidenceAdmission{}, fmt.Errorf("insert P4 verifier output: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO p4_verifier_replays
		(replay_hash, input_hash, output_hash, new_durable_facts, replay_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, fact.VerifierReplay.ReplayHash, fact.VerifierRequest.InputHash, fact.VerifierOutput.OutputHash, fact.VerifierReplay.NewDurableFacts, replayJSON, createdAt); err != nil {
		return P4EvidenceAdmission{}, fmt.Errorf("insert P4 verifier replay: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return P4EvidenceAdmission{}, err
	}
	return fact, nil
}

// GetP4EvidenceAdmission loads one P4 fact and revalidates every canonical
// JSON record and every exact P1-P3f/P4 binding before returning it.
func (s *Store) GetP4EvidenceAdmission(ctx context.Context, inputHash string) (P4EvidenceAdmission, error) {
	fact, found, err := loadP4EvidenceAdmission(ctx, s.db, inputHash)
	if err != nil {
		return P4EvidenceAdmission{}, err
	}
	if !found {
		return P4EvidenceAdmission{}, ErrP4EvidenceNotFound
	}
	return fact, nil
}

type p4Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadP4EvidenceAdmission(ctx context.Context, queryer p4Queryer, inputHash string) (P4EvidenceAdmission, bool, error) {
	if !launchHashPattern.MatchString(inputHash) {
		return P4EvidenceAdmission{}, false, fmt.Errorf("%w: input hash", ErrP4EvidenceInvalid)
	}
	var bundleJSON, chainJSON, identitiesJSON, admissionJSON, fenceJSON, requestJSON, outputJSON, replayJSON string
	err := queryer.QueryRowContext(ctx, `SELECT b.bundle_json, b.predecessor_chain_json, b.verifier_identities_json,
		a.admission_json, a.full_fence_json, r.request_json, o.output_json, p.replay_json
		FROM p4_verifier_requests r
		JOIN p4_evidence_bundles b ON b.bundle_hash = r.bundle_hash
		JOIN p4_repair_admissions a ON a.admission_hash = r.admission_hash
		JOIN p4_verifier_outputs o ON o.input_hash = r.input_hash
		JOIN p4_verifier_replays p ON p.input_hash = r.input_hash
		WHERE r.input_hash = ?`, inputHash).Scan(&bundleJSON, &chainJSON, &identitiesJSON, &admissionJSON, &fenceJSON, &requestJSON, &outputJSON, &replayJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return P4EvidenceAdmission{}, false, nil
	}
	if err != nil {
		return P4EvidenceAdmission{}, false, err
	}
	var fact P4EvidenceAdmission
	if err := decodeCanonicalP4Value(bundleJSON, &fact.Bundle, p4EvidenceBundleValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 bundle: %w", err)
	}
	if err := decodeCanonicalP4Value(chainJSON, &fact.PredecessorChain, p4PredecessorChainValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 predecessor chain: %w", err)
	}
	if err := decodeCanonicalP4Value(identitiesJSON, &fact.VerifierIdentities, p4VerifierIdentitiesValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 verifier identities: %w", err)
	}
	if err := decodeCanonicalP4Value(admissionJSON, &fact.RepairAdmission, p4RepairAdmissionValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 repair admission: %w", err)
	}
	if err := decodeCanonicalP4Value(fenceJSON, &fact.FullFence, p4FullFenceValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 full fence: %w", err)
	}
	if err := decodeCanonicalP4Value(requestJSON, &fact.VerifierRequest, p4VerifierRequestValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 verifier request: %w", err)
	}
	if err := decodeCanonicalP4Value(outputJSON, &fact.VerifierOutput, p4VerifierOutputValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 verifier output: %w", err)
	}
	if err := decodeCanonicalP4Value(replayJSON, &fact.VerifierReplay, p4VerifierReplayValue); err != nil {
		return P4EvidenceAdmission{}, false, fmt.Errorf("decode P4 verifier replay: %w", err)
	}
	if err := validateP4EvidenceAdmission(fact); err != nil {
		return P4EvidenceAdmission{}, false, err
	}
	return fact, true, nil
}

func validateP4EvidenceAdmissionInput(fact P4EvidenceAdmission) error {
	expected := CanonicalP4EvidenceAdmission()
	if fact.PredecessorChain != expected.PredecessorChain ||
		fact.Bundle != expected.Bundle ||
		fact.VerifierIdentities != expected.VerifierIdentities ||
		fact.RepairAdmission != expected.RepairAdmission ||
		fact.FullFence != expected.FullFence ||
		fact.VerifierRequest != expected.VerifierRequest {
		return fmt.Errorf("%w: exact P1-P3f, bundle, trust, full-fence, MoA, and request binding", ErrP4EvidenceInvalid)
	}
	return nil
}

func validateP4EvidenceAdmission(fact P4EvidenceAdmission) error {
	if err := validateP4EvidenceAdmissionInput(fact); err != nil {
		return err
	}
	expected := CanonicalP4EvidenceAdmission()
	if fact.VerifierOutput != expected.VerifierOutput || fact.VerifierReplay != expected.VerifierReplay {
		return fmt.Errorf("%w: verifier output or zero-fact replay binding", ErrP4EvidenceInvalid)
	}
	return nil
}

func p4CanonicalJSON(value any) (string, error) {
	canonical, err := canonicalJSON(value)
	if err != nil {
		return "", fmt.Errorf("%w: canonical JSON: %v", ErrP4EvidenceInvalid, err)
	}
	return string(canonical), nil
}

func decodeCanonicalP4Value[T any](raw string, target *T, canonical func(T) map[string]any) error {
	if err := jsonUnmarshalStrict([]byte(raw), target); err != nil {
		return fmt.Errorf("%w: decode canonical record", ErrP4EvidenceInvalid)
	}
	encoded, err := p4CanonicalJSON(canonical(*target))
	if err != nil || encoded != raw {
		return fmt.Errorf("%w: canonical record", ErrP4EvidenceInvalid)
	}
	return nil
}

func p4EvidenceHashesValue(value P4EvidenceHashes) map[string]any {
	return map[string]any{
		"approval_hash": value.ApprovalHash, "artifact_hash": value.ArtifactHash, "callback_hash": value.CallbackHash,
		"envelope_hash": value.EnvelopeHash, "evaluation_hash": value.EvaluationHash, "fence_hash": value.FenceHash,
		"proposal_hash": value.ProposalHash, "receipt_hash": value.ReceiptHash, "revision_hash": value.RevisionHash,
		"route_hash": value.RouteHash, "source_hash": value.SourceHash, "test_hash": value.TestHash,
	}
}

func p4PredecessorChainValue(value P4PredecessorChain) map[string]any {
	return map[string]any{
		"p1_revision_hash": value.P1RevisionHash, "p2_grill_fixture_sha256": value.P2GrillFixtureSHA256,
		"p3a_launch_admission_fixture_sha256": value.P3ALaunchAdmissionFixtureSHA256, "p3a_launch_spec_hash": value.P3ALaunchSpecHash,
		"p3b_fence_contract": value.P3BFenceContract, "p3c_recovery_action": value.P3CRecoveryAction,
		"p3d_omp_audit_fixture_sha256": value.P3DOMPAuditFixtureSHA256, "p3f_adapter_fixture_sha256": value.P3FAdapterFixtureSHA256,
		"p3f_adapter_red_flags_count": value.P3FAdapterRedFlagsCount, "p3f_adapter_red_flags_fixture_sha256": value.P3FAdapterRedFlagsFixtureSHA256,
		"p3f_predecessor_envelope_hash": value.P3FPredecessorEnvelopeHash, "p3f_route_mapping_hash": value.P3FRouteMappingHash,
	}
}

func p4EvidenceBundleValue(value P4EvidenceBundle) map[string]any {
	return map[string]any{
		"bundle_hash": value.BundleHash, "bundle_id": value.BundleID, "evidence_hashes": p4EvidenceHashesValue(value.EvidenceHashes),
		"issued_at": value.IssuedAt, "p3f_adapter_fixture_sha256": value.P3FAdapterFixtureSHA256,
		"p3f_adapter_red_flags_count": value.P3FAdapterRedFlagsCount, "p3f_adapter_red_flags_fixture_sha256": value.P3FAdapterRedFlagsFixtureSHA256,
		"schema_version": value.SchemaVersion, "verifier_release_identity_hash": value.VerifierReleaseIdentityHash,
		"verifier_trust_identity_hash": value.VerifierTrustIdentityHash,
	}
}

func p4TrustIdentityValue(value P4VerifierTrustIdentity) map[string]any {
	return map[string]any{
		"schema_version": value.SchemaVersion, "trust_identity_hash": value.TrustIdentityHash,
		"trust_root_id": value.TrustRootID, "trust_root_spki_sha256": value.TrustRootSPKISHA256,
	}
}

func p4ReleaseIdentityValue(value P4VerifierReleaseIdentity) map[string]any {
	return map[string]any{
		"release_artifact_sha256": value.ReleaseArtifactSHA256, "release_id": value.ReleaseID,
		"release_identity_hash": value.ReleaseIdentityHash, "release_manifest_hash": value.ReleaseManifestHash,
		"schema_version": value.SchemaVersion, "trust_identity_hash": value.TrustIdentityHash,
	}
}

func p4VerifierIdentitiesValue(value P4VerifierIdentities) map[string]any {
	return map[string]any{"release": p4ReleaseIdentityValue(value.Release), "trust": p4TrustIdentityValue(value.Trust)}
}

func p4TypedMoAGrantValue(value P4TypedMoAGrant) map[string]any {
	return map[string]any{
		"approval_evidence_hash": value.ApprovalEvidenceHash, "evidence_bundle_hash": value.EvidenceBundleHash,
		"fence_evidence_hash": value.FenceEvidenceHash, "grant_hash": value.GrantHash, "grant_id": value.GrantID,
		"grantee_role": value.GranteeRole, "issued_at": value.IssuedAt,
		"issuer_trust_identity_hash": value.IssuerTrustIdentityHash, "not_after": value.NotAfter,
		"route_evidence_hash": value.RouteEvidenceHash, "schema_version": value.SchemaVersion,
	}
}

func p4RepairAdmissionValue(value P4RepairAdmission) map[string]any {
	return map[string]any{
		"admission_hash": value.AdmissionHash, "admission_id": value.AdmissionID, "admission_state": value.AdmissionState,
		"allowed_role": value.AllowedRole, "allowed_route_evidence_hash": value.AllowedRouteEvidenceHash,
		"exact_evidence_bundle_hash": value.ExactEvidenceBundleHash, "exact_evidence_hashes": p4EvidenceHashesValue(value.ExactEvidenceHashes),
		"fresh_approval_evidence_hash": value.FreshApprovalEvidenceHash, "fresh_fence_evidence_hash": value.FreshFenceEvidenceHash,
		"inferred_success": value.InferredSuccess, "prior_approval_evidence_hash": value.PriorApprovalEvidenceHash,
		"prior_fence_evidence_hash": value.PriorFenceEvidenceHash, "repair_attempt_cap": value.RepairAttemptCap,
		"repair_attempt_number": value.RepairAttemptNumber, "schema_version": value.SchemaVersion,
		"typed_moa_grant": p4TypedMoAGrantValue(value.TypedMoAGrant),
	}
}

func p4FullFenceValue(value P4FullFence) map[string]any {
	return map[string]any{
		"claim_id": value.ClaimID, "claim_token_hash": value.ClaimTokenHash, "fence_generation": value.FenceGeneration,
	}
}

func p4VerifierRequestValue(value P4VerifierRequest) map[string]any {
	return map[string]any{
		"bundle_hash": value.BundleHash, "input_hash": value.InputHash,
		"p3f_adapter_fixture_sha256": value.P3FAdapterFixtureSHA256, "p3f_adapter_red_flags_count": value.P3FAdapterRedFlagsCount,
		"p3f_adapter_red_flags_fixture_sha256": value.P3FAdapterRedFlagsFixtureSHA256,
		"repair_admission_hash":                value.RepairAdmissionHash, "schema_version": value.SchemaVersion,
		"verifier_release_identity_hash": value.VerifierReleaseIdentityHash, "verifier_trust_identity_hash": value.VerifierTrustIdentityHash,
	}
}

func p4VerifierOutputValue(value P4VerifierOutput) map[string]any {
	return map[string]any{
		"admission": value.Admission, "bundle_hash": value.BundleHash, "output_hash": value.OutputHash,
		"repair_execution": value.RepairExecution, "schema_version": value.SchemaVersion,
		"state": value.State, "verification_state": value.VerificationState,
	}
}

func p4VerifierReplayValue(value P4VerifierReplay) map[string]any {
	return map[string]any{
		"input_hash": value.InputHash, "new_durable_facts": value.NewDurableFacts,
		"output_hash": value.OutputHash, "replay_hash": value.ReplayHash,
		"replay_result": value.ReplayResult, "schema_version": value.SchemaVersion,
	}
}

func migrateV13(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE p4_evidence_bundles (
			bundle_hash TEXT PRIMARY KEY,
			bundle_id TEXT NOT NULL UNIQUE,
			bundle_json TEXT NOT NULL,
			predecessor_chain_json TEXT NOT NULL,
			verifier_identities_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE p4_repair_admissions (
			admission_hash TEXT PRIMARY KEY,
			admission_id TEXT NOT NULL UNIQUE,
			bundle_hash TEXT NOT NULL,
			admission_json TEXT NOT NULL,
			full_fence_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (bundle_hash) REFERENCES p4_evidence_bundles(bundle_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE p4_verifier_requests (
			input_hash TEXT PRIMARY KEY,
			bundle_hash TEXT NOT NULL,
			admission_hash TEXT NOT NULL,
			request_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (bundle_hash) REFERENCES p4_evidence_bundles(bundle_hash)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (admission_hash) REFERENCES p4_repair_admissions(admission_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE p4_verifier_outputs (
			output_hash TEXT PRIMARY KEY,
			input_hash TEXT NOT NULL UNIQUE,
			bundle_hash TEXT NOT NULL,
			output_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (input_hash) REFERENCES p4_verifier_requests(input_hash)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (bundle_hash) REFERENCES p4_evidence_bundles(bundle_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TABLE p4_verifier_replays (
			replay_hash TEXT PRIMARY KEY,
			input_hash TEXT NOT NULL UNIQUE,
			output_hash TEXT NOT NULL UNIQUE,
			new_durable_facts INTEGER NOT NULL CHECK (new_durable_facts = 0),
			replay_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (input_hash) REFERENCES p4_verifier_requests(input_hash)
				DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (output_hash) REFERENCES p4_verifier_outputs(output_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TRIGGER p4_evidence_bundles_insert_only_update BEFORE UPDATE ON p4_evidence_bundles
			BEGIN SELECT RAISE(ABORT, 'P4 evidence bundles are immutable'); END`,
		`CREATE TRIGGER p4_evidence_bundles_insert_only_delete BEFORE DELETE ON p4_evidence_bundles
			BEGIN SELECT RAISE(ABORT, 'P4 evidence bundles are immutable'); END`,
		`CREATE TRIGGER p4_repair_admissions_insert_only_update BEFORE UPDATE ON p4_repair_admissions
			BEGIN SELECT RAISE(ABORT, 'P4 repair admissions are immutable'); END`,
		`CREATE TRIGGER p4_repair_admissions_insert_only_delete BEFORE DELETE ON p4_repair_admissions
			BEGIN SELECT RAISE(ABORT, 'P4 repair admissions are immutable'); END`,
		`CREATE TRIGGER p4_verifier_requests_insert_only_update BEFORE UPDATE ON p4_verifier_requests
			BEGIN SELECT RAISE(ABORT, 'P4 verifier requests are immutable'); END`,
		`CREATE TRIGGER p4_verifier_requests_insert_only_delete BEFORE DELETE ON p4_verifier_requests
			BEGIN SELECT RAISE(ABORT, 'P4 verifier requests are immutable'); END`,
		`CREATE TRIGGER p4_verifier_outputs_insert_only_update BEFORE UPDATE ON p4_verifier_outputs
			BEGIN SELECT RAISE(ABORT, 'P4 verifier outputs are immutable'); END`,
		`CREATE TRIGGER p4_verifier_outputs_insert_only_delete BEFORE DELETE ON p4_verifier_outputs
			BEGIN SELECT RAISE(ABORT, 'P4 verifier outputs are immutable'); END`,
		`CREATE TRIGGER p4_verifier_replays_insert_only_update BEFORE UPDATE ON p4_verifier_replays
			BEGIN SELECT RAISE(ABORT, 'P4 verifier replays are immutable'); END`,
		`CREATE TRIGGER p4_verifier_replays_insert_only_delete BEFORE DELETE ON p4_verifier_replays
			BEGIN SELECT RAISE(ABORT, 'P4 verifier replays are immutable'); END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
