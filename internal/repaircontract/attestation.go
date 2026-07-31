package repaircontract

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

const (
	AttestationSchemaVersion                  = "ananke.controlled-repair-review-attestation.v1"
	AttestationVerifierAuthoritySchemaVersion = "ananke.controlled-repair-attestation-verifier-authority.v1"
	AttestationVerificationSealSchemaVersion  = "ananke.controlled-repair-attestation-verification-seal.v1"

	attestationVerifierID         = "controlled_repair_attestation_verifier_v1"
	attestationStateWaitingReview = "waiting_for_review"

	maxAttestationIDBytes = 128
)

// --- Closed string enums ---

type AttestationState string

const (
	AttestationWaitingForReview AttestationState = "waiting_for_review"
)

type AttestationAction string

const (
	AttestationAdmitForReview AttestationAction = "admit_for_review"
	AttestationStatusOnly     AttestationAction = "status_only"
)

type AttestationDisposition string

const (
	AttestationCapabilityReady AttestationDisposition = "capability_ready"
	AttestationRetainedStatus  AttestationDisposition = "retained_status"
)

type AttestationRequirement string

const (
	AttestationNextVerification AttestationRequirement = "next_verification_phase"
	AttestationNoFurtherEffect  AttestationRequirement = "no_further_effect_permitted"
)

type AttestationVerificationKind string

const (
	TrustBindingVerification         AttestationVerificationKind = "trust_binding_verification"
	AuthorizationBindingVerification AttestationVerificationKind = "authorization_binding_verification"
	PhaseClaimBindingVerification    AttestationVerificationKind = "phase_claim_binding_verification"
	RepositoryBindingVerification    AttestationVerificationKind = "repository_binding_verification"
	AdapterBindingVerification       AttestationVerificationKind = "adapter_binding_verification"
	TestBindingVerification          AttestationVerificationKind = "test_binding_verification"
	AttestationIntegrityVerification AttestationVerificationKind = "attestation_integrity_verification"
)

func validAttestationState(value AttestationState) bool {
	switch value {
	case AttestationWaitingForReview:
		return true
	default:
		return false
	}
}

func validAttestationAction(value AttestationAction) bool {
	switch value {
	case AttestationAdmitForReview, AttestationStatusOnly:
		return true
	default:
		return false
	}
}

func validAttestationVerificationKind(value AttestationVerificationKind) bool {
	switch value {
	case TrustBindingVerification, AuthorizationBindingVerification, PhaseClaimBindingVerification,
		RepositoryBindingVerification, AdapterBindingVerification, TestBindingVerification,
		AttestationIntegrityVerification:
		return true
	default:
		return false
	}
}

// --- Canonical record types ---

// RepairReviewAttestation is the canonical, closed, self-hashed terminal record
// that the supervisor signs after all phases complete. It binds every predecessor
// hash from Slices 1–6 plus runtime-only facts (patch, test results, transport).
// State is always exactly waiting_for_review. The signature field is a placeholder
// in the contract layer; actual Ed25519 signing is a runtime step. The attestation
// hash covers the entire record including the signature field; the signature covers
// the canonical record excluding only the signature field, with domain separation.
type RepairReviewAttestation struct {
	SchemaVersion string `json:"schema_version"`
	// Self-hash and identity
	AttestationHash string           `json:"attestation_hash"`
	AttestationID   string           `json:"attestation_id"`
	IssuedAt        string           `json:"issued_at"`
	State           AttestationState `json:"state"`
	// Signature
	SignatureDomain string `json:"signature_domain"`
	Signature       string `json:"signature"`
	// Trust (Slice 1)
	ReleasePinsHash               string `json:"release_pins_hash"`
	TrustBundleHash               string `json:"trust_bundle_hash"`
	RepairAttestorCertificateHash string `json:"repair_attestor_certificate_hash"`
	RepairAttestorRootID          string `json:"repair_attestor_root_id"`
	RepairAttestorLeafSPKI        string `json:"repair_attestor_leaf_spki"`
	// Transport
	RequestNonceHash  string `json:"request_nonce_hash"`
	ResponseNonceHash string `json:"response_nonce_hash"`
	ChannelHash       string `json:"channel_hash"`
	// Authorization (Slice 2)
	AuthorizationHash             string `json:"authorization_hash"`
	ApprovalHash                  string `json:"approval_hash"`
	RequestHash                   string `json:"request_hash"`
	DispatchHash                  string `json:"dispatch_hash"`
	AttemptHash                   string `json:"attempt_hash"`
	AttemptNumber                 int    `json:"attempt_number"`
	AttemptCap                    int    `json:"attempt_cap"`
	EffectTimeValidationTimestamp string `json:"effect_time_validation_timestamp"`
	// Phase claims (Slice 3)
	MaterializationClaimHash         string `json:"materialization_claim_hash"`
	AdapterClaimHash                 string `json:"adapter_claim_hash"`
	TestClaimHash                    string `json:"test_claim_hash"`
	PredecessorClaimHash             string `json:"predecessor_claim_hash"`
	SupervisorJournalHeadHash        string `json:"supervisor_journal_head_hash"`
	SupervisorJournalPredecessorHash string `json:"supervisor_journal_predecessor_hash"`
	BootEpochID                      string `json:"boot_epoch_id"`
	BootEpochHash                    string `json:"boot_epoch_hash"`
	// Repository (Slice 4)
	RepositoryBindingHash             string `json:"repository_binding_hash"`
	RepositoryIdentityHash            string `json:"repository_identity_hash"`
	CommonGitIdentityHash             string `json:"common_git_identity_hash"`
	GitExecutableIdentityHash         string `json:"git_executable_identity_hash"`
	WorktreeParentHash                string `json:"worktree_parent_hash"`
	WorktreeTargetHash                string `json:"worktree_target_hash"`
	WorktreeAdminHash                 string `json:"worktree_admin_hash"`
	WorktreeDescriptorHash            string `json:"worktree_descriptor_hash"`
	WorktreeSlotID                    string `json:"worktree_slot_id"`
	WorktreeSlotPathHash              string `json:"worktree_slot_path_hash"`
	InstalledWorktreeRootIdentityHash string `json:"installed_worktree_root_identity_hash"`
	// Adapter (Slice 5)
	AdapterSeatbeltProfileHash string `json:"adapter_seatbelt_profile_hash"`
	AdapterSandboxHash         string `json:"adapter_sandbox_hash"`
	AdapterTerminalProofHash   string `json:"adapter_terminal_proof_hash"`
	AdapterCapabilityHash      string `json:"adapter_capability_hash"`
	UIDPoolHash                string `json:"uid_pool_hash"`
	UIDLeaseHash               string `json:"uid_lease_hash"`
	UID                        uint32 `json:"uid"`
	GroupID                    uint32 `json:"group_id"`
	// Patch
	PatchHash          string `json:"patch_hash"`
	PatchSize          int64  `json:"patch_size"`
	OrderedPathsHash   string `json:"ordered_paths_hash"`
	StatusHash         string `json:"status_hash"`
	RawHash            string `json:"raw_hash"`
	NumstatHash        string `json:"numstat_hash"`
	IgnoredHash        string `json:"ignored_hash"`
	FilesystemScanHash string `json:"filesystem_scan_hash"`
	// Tests (Slice 6)
	ToolchainManifestHash string `json:"toolchain_manifest_hash"`
	TestProfileHash       string `json:"test_profile_hash"`
	CandidateCopyHash     string `json:"candidate_copy_hash"`
	TestSandboxHash       string `json:"test_sandbox_hash"`
	TestTerminalProofHash string `json:"test_terminal_proof_hash"`
	TestRootCleanupHash   string `json:"test_root_cleanup_hash"`
	TestResultHash        string `json:"test_result_hash"`
	TestOutputHash        string `json:"test_output_hash"`
	TestOutputSize        int64  `json:"test_output_size"`
	TestCommandHash       string `json:"test_command_hash"`
	TestCapabilityHash    string `json:"test_capability_hash"`
}

// AttestationVerifierAuthority is the frozen, release-pinned verifier for
// attestation structural validation. It is derived once at init and must
// match exactly on every re-derivation.
type AttestationVerifierAuthority struct {
	SchemaVersion         string                        `json:"schema_version"`
	VerifierAuthorityHash string                        `json:"verifier_authority_hash"`
	VerifierID            string                        `json:"verifier_id"`
	ReleasePinsHash       string                        `json:"release_pins_hash"`
	SignatureDomain       string                        `json:"signature_domain"`
	VerificationKinds     []AttestationVerificationKind `json:"verification_kinds"`
}

// AttestationVerificationSeal is a self-hashed provenance record binding one
// verification kind to the attestation it validates.
type AttestationVerificationSeal struct {
	SchemaVersion         string                      `json:"schema_version"`
	SealHash              string                      `json:"seal_hash"`
	SealKind              AttestationVerificationKind `json:"seal_kind"`
	VerifierAuthorityHash string                      `json:"verifier_authority_hash"`
	AttestationHash       string                      `json:"attestation_hash"`
	CanonicalHash         string                      `json:"canonical_hash"`
	EvidenceHash          string                      `json:"evidence_hash"`
}

// VerifiedAttestationSnapshot is opaque evidence. Its private fields bind the
// attestation, all verification seals, and the canonical bytes.
type VerifiedAttestationSnapshot struct {
	valid                         bool
	trustVerified                 bool
	authorizationVerified         bool
	phaseClaimVerified            bool
	repositoryVerified            bool
	adapterVerified               bool
	testVerified                  bool
	integrityVerified             bool
	attestation                   RepairReviewAttestation
	canonical                     []byte
	canonicalHash                 string
	verifierAuthorityHash         string
	trustVerificationSeal         string
	authorizationVerificationSeal string
	phaseClaimVerificationSeal    string
	repositoryVerificationSeal    string
	adapterVerificationSeal       string
	testVerificationSeal          string
	integrityVerificationSeal     string
	integrityHash                 string
}

type attestationSnapshotIntegrity struct {
	IntegrityHash                 string `json:"integrity_hash"`
	Valid                         bool   `json:"valid"`
	TrustVerified                 bool   `json:"trust_verified"`
	AuthorizationVerified         bool   `json:"authorization_verified"`
	PhaseClaimVerified            bool   `json:"phase_claim_verified"`
	RepositoryVerified            bool   `json:"repository_verified"`
	AdapterVerified               bool   `json:"adapter_verified"`
	TestVerified                  bool   `json:"test_verified"`
	IntegrityVerified             bool   `json:"integrity_verified"`
	AttestationHash               string `json:"attestation_hash"`
	CanonicalHash                 string `json:"canonical_hash"`
	VerifierAuthorityHash         string `json:"verifier_authority_hash"`
	TrustVerificationSeal         string `json:"trust_verification_seal"`
	AuthorizationVerificationSeal string `json:"authorization_verification_seal"`
	PhaseClaimVerificationSeal    string `json:"phase_claim_verification_seal"`
	RepositoryVerificationSeal    string `json:"repository_verification_seal"`
	AdapterVerificationSeal       string `json:"adapter_verification_seal"`
	TestVerificationSeal          string `json:"test_verification_seal"`
	IntegrityVerificationSeal     string `json:"integrity_verification_seal"`
}

// AttestationAssessment is classification only. EffectAllowed is always false.
type AttestationAssessment struct {
	Disposition     AttestationDisposition
	EffectAllowed   bool
	NextRequirement AttestationRequirement
}

// VerifiedAttestation is an opaque terminal capability for the Ananke
// verification phase (Slice 8). It grants no filesystem, process, or
// effect authority.
type VerifiedAttestation struct {
	valid                    bool
	attestationHash          string
	snapshotIntegrityHash    string
	verifierAuthorityHash    string
	verificationSealsHash    string
	authorizationHash        string
	testClaimHash            string
	adapterClaimHash         string
	materializationClaimHash string
	repositoryBindingHash    string
	adapterCapabilityHash    string
	testCapabilityHash       string
	canonical                []byte
	canonicalHash            string
	integrityHash            string
}

type verifiedAttestationIntegrity struct {
	IntegrityHash            string `json:"integrity_hash"`
	Valid                    bool   `json:"valid"`
	AttestationHash          string `json:"attestation_hash"`
	SnapshotIntegrityHash    string `json:"snapshot_integrity_hash"`
	VerifierAuthorityHash    string `json:"verifier_authority_hash"`
	VerificationSealsHash    string `json:"verification_seals_hash"`
	AuthorizationHash        string `json:"authorization_hash"`
	TestClaimHash            string `json:"test_claim_hash"`
	AdapterClaimHash         string `json:"adapter_claim_hash"`
	MaterializationClaimHash string `json:"materialization_claim_hash"`
	RepositoryBindingHash    string `json:"repository_binding_hash"`
	AdapterCapabilityHash    string `json:"adapter_capability_hash"`
	TestCapabilityHash       string `json:"test_capability_hash"`
	CanonicalHash            string `json:"canonical_hash"`
}

// --- Errors ---

var ErrInvalidAttestation = errors.New("invalid attestation")

// --- Frozen compiled values ---

var frozenAttestationVerifierAuthority = mustDeriveAttestationVerifierAuthority()

func mustDeriveAttestationVerifierAuthority() AttestationVerifierAuthority {
	pins := FrozenReleasePins()
	verifier := AttestationVerifierAuthority{
		SchemaVersion:   AttestationVerifierAuthoritySchemaVersion,
		VerifierID:      attestationVerifierID,
		ReleasePinsHash: pins.ReleasePinsHash,
		SignatureDomain: SignatureDomain,
		VerificationKinds: []AttestationVerificationKind{
			TrustBindingVerification,
			AuthorizationBindingVerification,
			PhaseClaimBindingVerification,
			RepositoryBindingVerification,
			AdapterBindingVerification,
			TestBindingVerification,
			AttestationIntegrityVerification,
		},
	}
	hash, err := hashRecord(verifier, "verifier_authority_hash")
	if err != nil {
		panic(err)
	}
	verifier.VerifierAuthorityHash = hash
	return verifier
}

func FrozenAttestationVerifierAuthority() AttestationVerifierAuthority {
	return frozenAttestationVerifierAuthority
}

func deriveFrozenAttestationVerifierAuthority() (AttestationVerifierAuthority, error) {
	derived := mustDeriveAttestationVerifierAuthority()
	frozen := FrozenAttestationVerifierAuthority()
	if !reflect.DeepEqual(derived, frozen) {
		return AttestationVerifierAuthority{}, ErrInvalidAttestation
	}
	return frozen, nil
}

// --- Attestation hash ---

// hashAttestationRecord computes the self-hash of the attestation, excluding
// only the attestation_hash field (the signature IS included in the self-hash
// so that record integrity covers the signature).
func hashAttestationRecord(record RepairReviewAttestation) (string, error) {
	return hashRecord(record, "attestation_hash")
}

// attestationSignatureCanonicalBytes returns the canonical bytes that the
// Ed25519 signature covers: the entire attestation excluding only the
// signature field, with domain separation prepended.
func attestationSignatureCanonicalBytes(record RepairReviewAttestation) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "signature")
	canonical, err := canonicalBytes(m)
	if err != nil {
		return nil, err
	}
	domainPrefix := []byte(SignatureDomain + "\x00")
	return append(domainPrefix, canonical...), nil
}

// --- Seal derivation ---

type attestationVerificationSealSet struct {
	trust         string
	authorization string
	phaseClaim    string
	repository    string
	adapter       string
	test          string
	integrity     string
}

func (seals attestationVerificationSealSet) ordered() []string {
	return []string{
		seals.trust, seals.authorization, seals.phaseClaim,
		seals.repository, seals.adapter, seals.test, seals.integrity,
	}
}

type attestationTrustSealEvidence struct {
	ReleasePinsHash               string `json:"release_pins_hash"`
	TrustBundleHash               string `json:"trust_bundle_hash"`
	RepairAttestorCertificateHash string `json:"repair_attestor_certificate_hash"`
	RepairAttestorRootID          string `json:"repair_attestor_root_id"`
	RepairAttestorLeafSPKI        string `json:"repair_attestor_leaf_spki"`
}

type attestationAuthorizationSealEvidence struct {
	AuthorizationHash             string `json:"authorization_hash"`
	ApprovalHash                  string `json:"approval_hash"`
	RequestHash                   string `json:"request_hash"`
	DispatchHash                  string `json:"dispatch_hash"`
	AttemptHash                   string `json:"attempt_hash"`
	AttemptNumber                 int    `json:"attempt_number"`
	AttemptCap                    int    `json:"attempt_cap"`
	EffectTimeValidationTimestamp string `json:"effect_time_validation_timestamp"`
}

type attestationPhaseClaimSealEvidence struct {
	MaterializationClaimHash         string `json:"materialization_claim_hash"`
	AdapterClaimHash                 string `json:"adapter_claim_hash"`
	TestClaimHash                    string `json:"test_claim_hash"`
	PredecessorClaimHash             string `json:"predecessor_claim_hash"`
	SupervisorJournalHeadHash        string `json:"supervisor_journal_head_hash"`
	SupervisorJournalPredecessorHash string `json:"supervisor_journal_predecessor_hash"`
	BootEpochID                      string `json:"boot_epoch_id"`
	BootEpochHash                    string `json:"boot_epoch_hash"`
}

type attestationRepositorySealEvidence struct {
	RepositoryBindingHash             string `json:"repository_binding_hash"`
	RepositoryIdentityHash            string `json:"repository_identity_hash"`
	CommonGitIdentityHash             string `json:"common_git_identity_hash"`
	GitExecutableIdentityHash         string `json:"git_executable_identity_hash"`
	WorktreeSlotID                    string `json:"worktree_slot_id"`
	WorktreeSlotPathHash              string `json:"worktree_slot_path_hash"`
	InstalledWorktreeRootIdentityHash string `json:"installed_worktree_root_identity_hash"`
}

type attestationAdapterSealEvidence struct {
	AdapterSeatbeltProfileHash string `json:"adapter_seatbelt_profile_hash"`
	AdapterSandboxHash         string `json:"adapter_sandbox_hash"`
	AdapterTerminalProofHash   string `json:"adapter_terminal_proof_hash"`
	AdapterCapabilityHash      string `json:"adapter_capability_hash"`
	UIDPoolHash                string `json:"uid_pool_hash"`
	UIDLeaseHash               string `json:"uid_lease_hash"`
	UID                        uint32 `json:"uid"`
	GroupID                    uint32 `json:"group_id"`
}

type attestationTestSealEvidence struct {
	ToolchainManifestHash string `json:"toolchain_manifest_hash"`
	TestProfileHash       string `json:"test_profile_hash"`
	CandidateCopyHash     string `json:"candidate_copy_hash"`
	TestSandboxHash       string `json:"test_sandbox_hash"`
	TestTerminalProofHash string `json:"test_terminal_proof_hash"`
	TestRootCleanupHash   string `json:"test_root_cleanup_hash"`
	TestResultHash        string `json:"test_result_hash"`
	TestOutputHash        string `json:"test_output_hash"`
	TestOutputSize        int64  `json:"test_output_size"`
	TestCommandHash       string `json:"test_command_hash"`
}

type attestationIntegritySealEvidence struct {
	AttestationHash    string `json:"attestation_hash"`
	State              string `json:"state"`
	SignatureDomain    string `json:"signature_domain"`
	PatchHash          string `json:"patch_hash"`
	PatchSize          int64  `json:"patch_size"`
	OrderedPathsHash   string `json:"ordered_paths_hash"`
	StatusHash         string `json:"status_hash"`
	RawHash            string `json:"raw_hash"`
	NumstatHash        string `json:"numstat_hash"`
	IgnoredHash        string `json:"ignored_hash"`
	FilesystemScanHash string `json:"filesystem_scan_hash"`
	RequestNonceHash   string `json:"request_nonce_hash"`
	ResponseNonceHash  string `json:"response_nonce_hash"`
	ChannelHash        string `json:"channel_hash"`
	IssuedAt           string `json:"issued_at"`
}

type attestationSealEvidence = any

func attestationSealEvidenceHash(kind AttestationVerificationKind, record RepairReviewAttestation) string {
	var evidence attestationSealEvidence
	switch kind {
	case TrustBindingVerification:
		evidence = attestationTrustSealEvidence{
			ReleasePinsHash:               record.ReleasePinsHash,
			TrustBundleHash:               record.TrustBundleHash,
			RepairAttestorCertificateHash: record.RepairAttestorCertificateHash,
			RepairAttestorRootID:          record.RepairAttestorRootID,
			RepairAttestorLeafSPKI:        record.RepairAttestorLeafSPKI,
		}
	case AuthorizationBindingVerification:
		evidence = attestationAuthorizationSealEvidence{
			AuthorizationHash:             record.AuthorizationHash,
			ApprovalHash:                  record.ApprovalHash,
			RequestHash:                   record.RequestHash,
			DispatchHash:                  record.DispatchHash,
			AttemptHash:                   record.AttemptHash,
			AttemptNumber:                 record.AttemptNumber,
			AttemptCap:                    record.AttemptCap,
			EffectTimeValidationTimestamp: record.EffectTimeValidationTimestamp,
		}
	case PhaseClaimBindingVerification:
		evidence = attestationPhaseClaimSealEvidence{
			MaterializationClaimHash:         record.MaterializationClaimHash,
			AdapterClaimHash:                 record.AdapterClaimHash,
			TestClaimHash:                    record.TestClaimHash,
			PredecessorClaimHash:             record.PredecessorClaimHash,
			SupervisorJournalHeadHash:        record.SupervisorJournalHeadHash,
			SupervisorJournalPredecessorHash: record.SupervisorJournalPredecessorHash,
			BootEpochID:                      record.BootEpochID,
			BootEpochHash:                    record.BootEpochHash,
		}
	case RepositoryBindingVerification:
		evidence = attestationRepositorySealEvidence{
			RepositoryBindingHash:             record.RepositoryBindingHash,
			RepositoryIdentityHash:            record.RepositoryIdentityHash,
			CommonGitIdentityHash:             record.CommonGitIdentityHash,
			GitExecutableIdentityHash:         record.GitExecutableIdentityHash,
			WorktreeSlotID:                    record.WorktreeSlotID,
			WorktreeSlotPathHash:              record.WorktreeSlotPathHash,
			InstalledWorktreeRootIdentityHash: record.InstalledWorktreeRootIdentityHash,
		}
	case AdapterBindingVerification:
		evidence = attestationAdapterSealEvidence{
			AdapterSeatbeltProfileHash: record.AdapterSeatbeltProfileHash,
			AdapterSandboxHash:         record.AdapterSandboxHash,
			AdapterTerminalProofHash:   record.AdapterTerminalProofHash,
			AdapterCapabilityHash:      record.AdapterCapabilityHash,
			UIDPoolHash:                record.UIDPoolHash,
			UIDLeaseHash:               record.UIDLeaseHash,
			UID:                        record.UID,
			GroupID:                    record.GroupID,
		}
	case TestBindingVerification:
		evidence = attestationTestSealEvidence{
			ToolchainManifestHash: record.ToolchainManifestHash,
			TestProfileHash:       record.TestProfileHash,
			CandidateCopyHash:     record.CandidateCopyHash,
			TestSandboxHash:       record.TestSandboxHash,
			TestTerminalProofHash: record.TestTerminalProofHash,
			TestRootCleanupHash:   record.TestRootCleanupHash,
			TestResultHash:        record.TestResultHash,
			TestOutputHash:        record.TestOutputHash,
			TestOutputSize:        record.TestOutputSize,
			TestCommandHash:       record.TestCommandHash,
		}
	case AttestationIntegrityVerification:
		evidence = attestationIntegritySealEvidence{
			AttestationHash:    record.AttestationHash,
			State:              string(record.State),
			SignatureDomain:    record.SignatureDomain,
			PatchHash:          record.PatchHash,
			PatchSize:          record.PatchSize,
			OrderedPathsHash:   record.OrderedPathsHash,
			StatusHash:         record.StatusHash,
			RawHash:            record.RawHash,
			NumstatHash:        record.NumstatHash,
			IgnoredHash:        record.IgnoredHash,
			FilesystemScanHash: record.FilesystemScanHash,
			RequestNonceHash:   record.RequestNonceHash,
			ResponseNonceHash:  record.ResponseNonceHash,
			ChannelHash:        record.ChannelHash,
			IssuedAt:           record.IssuedAt,
		}
	default:
		return ""
	}
	canonical, err := canonicalBytes(evidence)
	if err != nil {
		return ""
	}
	return sha256Digest(canonical)
}

func deriveAttestationVerificationSeal(kind AttestationVerificationKind, verifier AttestationVerifierAuthority, record RepairReviewAttestation, canonicalHash string) string {
	seal := AttestationVerificationSeal{
		SchemaVersion:         AttestationVerificationSealSchemaVersion,
		SealKind:              kind,
		VerifierAuthorityHash: verifier.VerifierAuthorityHash,
		AttestationHash:       record.AttestationHash,
		CanonicalHash:         canonicalHash,
		EvidenceHash:          attestationSealEvidenceHash(kind, record),
	}
	hash, err := hashRecord(seal, "seal_hash")
	if err != nil {
		return ""
	}
	seal.SealHash = hash
	raw, _ := json.Marshal(seal)
	return sha256Digest(raw)
}

func deriveAttestationVerificationSeals(verifier AttestationVerifierAuthority, record RepairReviewAttestation, canonicalHash string) attestationVerificationSealSet {
	return attestationVerificationSealSet{
		trust:         deriveAttestationVerificationSeal(TrustBindingVerification, verifier, record, canonicalHash),
		authorization: deriveAttestationVerificationSeal(AuthorizationBindingVerification, verifier, record, canonicalHash),
		phaseClaim:    deriveAttestationVerificationSeal(PhaseClaimBindingVerification, verifier, record, canonicalHash),
		repository:    deriveAttestationVerificationSeal(RepositoryBindingVerification, verifier, record, canonicalHash),
		adapter:       deriveAttestationVerificationSeal(AdapterBindingVerification, verifier, record, canonicalHash),
		test:          deriveAttestationVerificationSeal(TestBindingVerification, verifier, record, canonicalHash),
		integrity:     deriveAttestationVerificationSeal(AttestationIntegrityVerification, verifier, record, canonicalHash),
	}
}

func attestationVerificationSealsHash(seals attestationVerificationSealSet) string {
	ordered := seals.ordered()
	if len(ordered) != 7 {
		return ""
	}
	for _, seal := range ordered {
		if !validHash(seal) {
			return ""
		}
	}
	raw, err := canonicalBytes(ordered)
	if err != nil {
		return ""
	}
	return sha256Digest(raw)
}

// --- Decode ---

func DecodeRepairReviewAttestation(raw []byte) (RepairReviewAttestation, error) {
	return decodeCanonicalRecord[RepairReviewAttestation](raw)
}

// --- Validation ---

func validateAttestationRecord(value RepairReviewAttestation) error {
	if value.SchemaVersion != AttestationSchemaVersion ||
		!validAttestationState(value.State) ||
		value.State != AttestationWaitingForReview ||
		value.SignatureDomain != SignatureDomain ||
		!validClosedIdentifier(value.AttestationID, maxAttestationIDBytes) ||
		!validHash(value.AttestationHash) || !validHash(value.Signature) ||
		!validHash(value.ReleasePinsHash) || !validHash(value.TrustBundleHash) ||
		!validHash(value.RepairAttestorCertificateHash) ||
		!validClosedIdentifier(value.RepairAttestorRootID, maxAttestationIDBytes) ||
		!validHash(value.RepairAttestorLeafSPKI) ||
		!validHash(value.RequestNonceHash) || !validHash(value.ResponseNonceHash) || !validHash(value.ChannelHash) ||
		!validHash(value.AuthorizationHash) || !validHash(value.ApprovalHash) ||
		!validHash(value.RequestHash) || !validHash(value.DispatchHash) || !validHash(value.AttemptHash) ||
		value.AttemptNumber < 1 || value.AttemptNumber > AttemptCap || value.AttemptCap != AttemptCap ||
		!validHash(value.MaterializationClaimHash) || !validHash(value.AdapterClaimHash) ||
		!validHash(value.TestClaimHash) || !validHash(value.PredecessorClaimHash) ||
		!validHash(value.SupervisorJournalHeadHash) || !validHash(value.SupervisorJournalPredecessorHash) ||
		!validClosedIdentifier(value.BootEpochID, maxAttestationIDBytes) || !validHash(value.BootEpochHash) ||
		!validHash(value.RepositoryBindingHash) || !validHash(value.RepositoryIdentityHash) ||
		!validHash(value.CommonGitIdentityHash) || !validHash(value.GitExecutableIdentityHash) ||
		!validHash(value.WorktreeParentHash) || !validHash(value.WorktreeTargetHash) ||
		!validHash(value.WorktreeAdminHash) || !validHash(value.WorktreeDescriptorHash) ||
		!validClosedIdentifier(value.WorktreeSlotID, maxAttestationIDBytes) || !validHash(value.WorktreeSlotPathHash) ||
		!validHash(value.InstalledWorktreeRootIdentityHash) ||
		!validHash(value.AdapterSeatbeltProfileHash) || !validHash(value.AdapterSandboxHash) ||
		!validHash(value.AdapterTerminalProofHash) || !validHash(value.AdapterCapabilityHash) ||
		!validHash(value.UIDPoolHash) || !validHash(value.UIDLeaseHash) ||
		value.UID == 0 || value.GroupID == 0 ||
		!validHash(value.PatchHash) ||
		value.PatchSize < 0 ||
		!validHash(value.OrderedPathsHash) || !validHash(value.StatusHash) ||
		!validHash(value.RawHash) || !validHash(value.NumstatHash) ||
		!validHash(value.IgnoredHash) || !validHash(value.FilesystemScanHash) ||
		!validHash(value.ToolchainManifestHash) || !validHash(value.TestProfileHash) ||
		!validHash(value.CandidateCopyHash) || !validHash(value.TestSandboxHash) ||
		!validHash(value.TestTerminalProofHash) || !validHash(value.TestRootCleanupHash) ||
		!validHash(value.TestResultHash) || !validHash(value.TestOutputHash) ||
		value.TestOutputSize < 0 || !validHash(value.TestCommandHash) ||
		!validHash(value.TestCapabilityHash) {
		return ErrInvalidAttestation
	}
	if _, err := parseUTC(value.IssuedAt); err != nil {
		return ErrInvalidAttestation
	}
	if _, err := parseUTC(value.EffectTimeValidationTimestamp); err != nil {
		return ErrInvalidAttestation
	}
	if !recordHashMatches(value, "attestation_hash", value.AttestationHash) {
		return ErrInvalidAttestation
	}
	return nil
}

// attestationSnapshotMatchesAuthority validates that the attestation's
// predecessor binding hashes match the actual predecessor capabilities.
func attestationSnapshotMatchesAuthority(
	value *VerifiedAttestationSnapshot,
	expected SupervisorIntentAuthority,
	authorization *VerifiedAuthorization,
	testClaim *VerifiedSupervisorIntentClaim,
	predecessorClaim *VerifiedSupervisorIntentClaim,
	predecessorTerminal *VerifiedSupervisorTerminalEvent,
	worktree *VerifiedRepositoryWorktree,
	adapterSandbox *VerifiedAdapterSandbox,
	testSandbox *VerifiedTestSandbox,
) bool {
	record := value.attestation
	pins := FrozenReleasePins()
	bundle := FrozenTrustBundle()
	// Trust binding (Slice 1)
	if record.ReleasePinsHash != pins.ReleasePinsHash ||
		record.TrustBundleHash != bundle.TrustBundleHash ||
		record.RepairAttestorCertificateHash != bundle.RepairAttestor.CertificateHash ||
		record.RepairAttestorRootID != bundle.RepairAttestor.IssuerRootID ||
		record.RepairAttestorLeafSPKI != bundle.RepairAttestor.SubjectSPKISHA256 {
		return false
	}
	// Authorization binding (Slice 2)
	if record.AuthorizationHash != authorization.authorization.AuthorizationHash ||
		record.ApprovalHash != authorization.authorization.ApprovalHash ||
		record.RequestHash != expected.AcceptedDispatch.Request.RequestHash ||
		record.DispatchHash != expected.AcceptedDispatch.DispatchHash ||
		record.AttemptHash != expected.AttemptHash || record.AttemptNumber != expected.AttemptNumber ||
		record.AttemptCap != expected.AttemptCap ||
		record.RepositoryBindingHash != expected.Repository.RepositoryBindingHash ||
		record.RepositoryIdentityHash != expected.Repository.RepositoryIdentityHash {
		return false
	}
	// Phase claim binding (Slice 3)
	if record.MaterializationClaimHash == "" || record.AdapterClaimHash == "" ||
		record.TestClaimHash != testClaim.claim.ClaimHash ||
		record.PredecessorClaimHash != expected.PredecessorClaimHash ||
		record.BootEpochID != expected.BootEpochID || record.BootEpochHash != expected.BootEpochHash {
		return false
	}
	// Repository binding (Slice 4) — decode worktree observation for true values
	worktreeObs, err := DecodeRepositoryWorktreeObservation(worktree.canonical)
	if err != nil ||
		record.WorktreeSlotID != worktree.worktreeSlotID ||
		record.WorktreeSlotPathHash != worktreeObs.WorktreeSlotPathHash ||
		record.InstalledWorktreeRootIdentityHash != worktreeObs.InstalledWorktreeRootIdentityHash ||
		record.AdapterCapabilityHash != adapterSandbox.snapshotIntegrityHash {
		return false
	}
	// Adapter binding (Slice 5)
	adapterObs, err := DecodeAdapterSandboxObservation(adapterSandbox.canonical)
	if err != nil ||
		record.AdapterSandboxHash != adapterObs.SandboxHash ||
		record.AdapterTerminalProofHash != adapterObs.TerminalProof.ProofHash ||
		record.AdapterSeatbeltProfileHash != adapterObs.SeatbeltProfileHash ||
		record.UIDPoolHash != adapterObs.UIDPoolHash ||
		record.UIDLeaseHash != adapterObs.UIDLeaseHash ||
		record.UID != adapterObs.UID || record.GroupID != adapterObs.GroupID {
		return false
	}
	// Test binding (Slice 6) — decode test sandbox observation for true values
	testObs, err := DecodeTestSandboxObservation(testSandbox.canonical)
	if err != nil ||
		record.TestSandboxHash != testObs.SandboxHash ||
		record.TestTerminalProofHash != testObs.TerminalProof.ProofHash ||
		record.ToolchainManifestHash != testObs.ToolchainManifestHash ||
		record.TestProfileHash != testObs.TestProfileHash ||
		record.CandidateCopyHash != testObs.CandidateCopy.CopyHash ||
		record.TestCapabilityHash != testSandbox.snapshotIntegrityHash {
		return false
	}
	return true
}

// --- Snapshot integrity ---

func attestationSnapshotIntegrityHash(value *VerifiedAttestationSnapshot) string {
	if value == nil {
		return ""
	}
	record := attestationSnapshotIntegrity{
		IntegrityHash:                 value.integrityHash,
		Valid:                         value.valid,
		TrustVerified:                 value.trustVerified,
		AuthorizationVerified:         value.authorizationVerified,
		PhaseClaimVerified:            value.phaseClaimVerified,
		RepositoryVerified:            value.repositoryVerified,
		AdapterVerified:               value.adapterVerified,
		TestVerified:                  value.testVerified,
		IntegrityVerified:             value.integrityVerified,
		AttestationHash:               value.attestation.AttestationHash,
		CanonicalHash:                 value.canonicalHash,
		VerifierAuthorityHash:         value.verifierAuthorityHash,
		TrustVerificationSeal:         value.trustVerificationSeal,
		AuthorizationVerificationSeal: value.authorizationVerificationSeal,
		PhaseClaimVerificationSeal:    value.phaseClaimVerificationSeal,
		RepositoryVerificationSeal:    value.repositoryVerificationSeal,
		AdapterVerificationSeal:       value.adapterVerificationSeal,
		TestVerificationSeal:          value.testVerificationSeal,
		IntegrityVerificationSeal:     value.integrityVerificationSeal,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedAttestationSnapshotIntact(value *VerifiedAttestationSnapshot, verifier AttestationVerifierAuthority) bool {
	if value == nil || !value.valid || !value.trustVerified || !value.authorizationVerified ||
		!value.phaseClaimVerified || !value.repositoryVerified || !value.adapterVerified ||
		!value.testVerified || !value.integrityVerified ||
		!validHash(value.integrityHash) || value.integrityHash != attestationSnapshotIntegrityHash(value) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		value.verifierAuthorityHash != verifier.VerifierAuthorityHash ||
		!recordHashMatches(verifier, "verifier_authority_hash", verifier.VerifierAuthorityHash) {
		return false
	}
	decoded, err := DecodeRepairReviewAttestation(value.canonical)
	if err != nil || !reflect.DeepEqual(decoded, value.attestation) {
		return false
	}
	if validateAttestationRecord(decoded) != nil {
		return false
	}
	seals := deriveAttestationVerificationSeals(verifier, decoded, value.canonicalHash)
	return value.trustVerificationSeal == seals.trust &&
		value.authorizationVerificationSeal == seals.authorization &&
		value.phaseClaimVerificationSeal == seals.phaseClaim &&
		value.repositoryVerificationSeal == seals.repository &&
		value.adapterVerificationSeal == seals.adapter &&
		value.testVerificationSeal == seals.test &&
		value.integrityVerificationSeal == seals.integrity
}

// --- Capability integrity ---

func verifiedAttestationIntegrityHash(value *VerifiedAttestation) string {
	if value == nil {
		return ""
	}
	record := verifiedAttestationIntegrity{
		IntegrityHash:            value.integrityHash,
		Valid:                    value.valid,
		AttestationHash:          value.attestationHash,
		SnapshotIntegrityHash:    value.snapshotIntegrityHash,
		VerifierAuthorityHash:    value.verifierAuthorityHash,
		VerificationSealsHash:    value.verificationSealsHash,
		AuthorizationHash:        value.authorizationHash,
		TestClaimHash:            value.testClaimHash,
		AdapterClaimHash:         value.adapterClaimHash,
		MaterializationClaimHash: value.materializationClaimHash,
		RepositoryBindingHash:    value.repositoryBindingHash,
		AdapterCapabilityHash:    value.adapterCapabilityHash,
		TestCapabilityHash:       value.testCapabilityHash,
		CanonicalHash:            value.canonicalHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedAttestationIntact(value *VerifiedAttestation) bool {
	if value == nil || !value.valid || !validHash(value.integrityHash) ||
		value.integrityHash != verifiedAttestationIntegrityHash(value) ||
		!validHash(value.attestationHash) || !validHash(value.snapshotIntegrityHash) ||
		!validHash(value.verifierAuthorityHash) || !validHash(value.verificationSealsHash) ||
		!validHash(value.authorizationHash) || !validHash(value.testClaimHash) ||
		!validHash(value.adapterClaimHash) || !validHash(value.materializationClaimHash) ||
		!validHash(value.repositoryBindingHash) || !validHash(value.adapterCapabilityHash) ||
		!validHash(value.testCapabilityHash) || !validHash(value.canonicalHash) ||
		value.canonicalHash != sha256Digest(value.canonical) {
		return false
	}
	verifier, err := deriveFrozenAttestationVerifierAuthority()
	if err != nil || value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	decoded, err := DecodeRepairReviewAttestation(value.canonical)
	if err != nil {
		return false
	}
	if validateAttestationRecord(decoded) != nil {
		return false
	}
	if decoded.AttestationHash != value.attestationHash {
		return false
	}
	// Defense-in-depth: recompute all 7 verification seals from the decoded
	// attestation under the frozen verifier, matching the Slice 4/5/6 pattern.
	seals := deriveAttestationVerificationSeals(verifier, decoded, value.canonicalHash)
	return value.verificationSealsHash == attestationVerificationSealsHash(seals)
}

// --- Evaluator ---

// EvaluateAttestation validates a canonical repair-review attestation against
// all predecessor evidence from Slices 1–6. It first re-establishes fresh
// release trust, derives the frozen verifier authority, validates test-claim
// authority, checks all predecessor capabilities intact, validates the
// attestation snapshot, and mints an opaque VerifiedAttestation capability
// only if state is exactly waiting_for_review and all bindings match.
// EffectAllowed is always false; no production minter exists.
func EvaluateAttestation(
	expected SupervisorIntentAuthority,
	authorization *VerifiedAuthorization,
	testClaim *VerifiedSupervisorIntentClaim,
	predecessorClaim *VerifiedSupervisorIntentClaim,
	predecessorTerminal *VerifiedSupervisorTerminalEvent,
	worktree *VerifiedRepositoryWorktree,
	adapterSandbox *VerifiedAdapterSandbox,
	testSandbox *VerifiedTestSandbox,
	snapshot *VerifiedAttestationSnapshot,
	action AttestationAction,
	now time.Time,
) (AttestationAssessment, *VerifiedAttestation, error) {
	invalid := AttestationAssessment{}
	now = now.UTC()
	verifier, err := deriveFrozenAttestationVerifierAuthority()
	if err != nil || VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now) != nil {
		return invalid, nil, ErrInvalidAttestation
	}
	if expected.Phase != TestClaimPhase || expected.Sequence != 3 ||
		validateSupervisorIntentAuthority(expected, authorization) != nil ||
		validateSupervisorIntentFreshness(expected, authorization, now) != nil ||
		!verifiedSupervisorIntentClaimIntact(testClaim) ||
		validateSupervisorIntentClaim(expected, authorization, predecessorClaim, predecessorTerminal, testClaim.claim) != nil ||
		!verifiedRepositoryWorktreeIntact(worktree) ||
		!verifiedAdapterSandboxIntact(adapterSandbox) ||
		!verifiedTestSandboxIntact(testSandbox) ||
		!verifiedAttestationSnapshotIntact(snapshot, verifier) ||
		!attestationSnapshotMatchesAuthority(snapshot, expected, authorization, testClaim, predecessorClaim, predecessorTerminal, worktree, adapterSandbox, testSandbox) {
		return invalid, nil, ErrInvalidAttestation
	}

	record := snapshot.attestation
	switch record.State {
	case AttestationWaitingForReview:
		if action == AttestationStatusOnly {
			return AttestationAssessment{
				Disposition:     AttestationRetainedStatus,
				NextRequirement: AttestationNoFurtherEffect,
			}, nil, nil
		}
		if action != AttestationAdmitForReview ||
			!snapshot.trustVerified || !snapshot.authorizationVerified ||
			!snapshot.phaseClaimVerified || !snapshot.repositoryVerified ||
			!snapshot.adapterVerified || !snapshot.testVerified || !snapshot.integrityVerified {
			return invalid, nil, ErrInvalidAttestation
		}
		seals := deriveAttestationVerificationSeals(verifier, record, snapshot.canonicalHash)
		capability := &VerifiedAttestation{
			valid:                    true,
			attestationHash:          record.AttestationHash,
			snapshotIntegrityHash:    snapshot.integrityHash,
			verifierAuthorityHash:    verifier.VerifierAuthorityHash,
			verificationSealsHash:    attestationVerificationSealsHash(seals),
			authorizationHash:        record.AuthorizationHash,
			testClaimHash:            record.TestClaimHash,
			adapterClaimHash:         record.AdapterClaimHash,
			materializationClaimHash: record.MaterializationClaimHash,
			repositoryBindingHash:    record.RepositoryBindingHash,
			adapterCapabilityHash:    record.AdapterCapabilityHash,
			testCapabilityHash:       record.TestCapabilityHash,
			canonical:                append([]byte(nil), snapshot.canonical...),
			canonicalHash:            snapshot.canonicalHash,
		}
		capability.integrityHash = verifiedAttestationIntegrityHash(capability)
		if !verifiedAttestationIntact(capability) {
			return invalid, nil, ErrInvalidAttestation
		}
		return AttestationAssessment{
			Disposition:     AttestationCapabilityReady,
			NextRequirement: AttestationNextVerification,
		}, capability, nil
	default:
		return invalid, nil, ErrInvalidAttestation
	}
}
