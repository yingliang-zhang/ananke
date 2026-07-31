package repaircontract

import (
	"errors"
	"reflect"
	"strconv"
	"time"
)

const (
	GoToolchainManifestSchemaVersion          = "ananke.controlled-repair-go-toolchain-manifest.v1"
	GoTestProfileSchemaVersion                = "ananke.controlled-repair-go-test-profile.v1"
	TestCandidateCopyObservationSchemaVersion = "ananke.controlled-repair-test-candidate-copy-observation.v1"
	TestSandboxObservationSchemaVersion       = "ananke.controlled-repair-test-sandbox-observation.v1"
	TestTerminalProofSchemaVersion            = "ananke.controlled-repair-test-terminal-proof.v1"
	TestSandboxVerifierAuthoritySchemaVersion = "ananke.controlled-repair-test-sandbox-verifier-authority.v1"
	TestSandboxVerificationSealSchemaVersion  = "ananke.controlled-repair-test-sandbox-verification-seal.v1"

	goToolchainManifestID       = "controlled_repair_go_toolchain_v1"
	goToolchainGoVersion        = "1.24.0"
	goTestProfileID             = "controlled_repair_go_test_profile_v1"
	testSandboxVerifierID       = "controlled_repair_test_sandbox_verifier_v1"
	testUIDLeaseSlotPrefix      = "attempt_"
	testUIDLeaseSlotSuffix      = "_test_uid_lease_001"
	testCommand                 = "go test ./... -count=1 -mod=readonly"
	testTimeoutSeconds          = 300
	testMaxOutputBytes          = 1048576
	testMaxCPUPercent           = 100
	testMaxMemoryBytes          = 1073741824
	testModuleCacheHash         = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	testToolchainExecutableHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	TestSandboxAdmitTerminal TestSandboxAction = "admit_terminal_proof"
	TestSandboxStatusOnly    TestSandboxAction = "status_only"

	TestSandboxCapabilityReady TestSandboxDisposition = "capability_ready"
	TestSandboxRetainedStatus  TestSandboxDisposition = "retained_status"
	TestSandboxWaitingForHuman TestSandboxDisposition = "waiting_for_human"

	TestSandboxNextAttestation     TestSandboxRequirement = "next_attestation_phase"
	TestSandboxNoFurtherEffect     TestSandboxRequirement = "no_further_effect_permitted"
	TestSandboxHumanReviewRequired TestSandboxRequirement = "human_review_required"

	TestSandboxTerminalProven TestSandboxState = "terminal_proven"
	TestSandboxRetainedReplay TestSandboxState = "retained_terminal_replay"
	TestSandboxRetainForHuman TestSandboxState = "retain_for_human"

	TestCleanupUIDEmptyRootsScrubbed TestTerminalCleanupResult = "uid_empty_roots_scrubbed"
	TestCleanupUIDNonemptyRetained   TestTerminalCleanupResult = "uid_nonempty_retained"
	TestCleanupPartialRetained       TestTerminalCleanupResult = "partial_retained"

	TestAmbiguityUIDNotEmpty              TestSandboxAmbiguityReason = "uid_not_empty"
	TestAmbiguityRootsNotScrubbed         TestSandboxAmbiguityReason = "roots_not_scrubbed"
	TestAmbiguityStalePID                 TestSandboxAmbiguityReason = "stale_pid_epoch"
	TestAmbiguityUIDReuse                 TestSandboxAmbiguityReason = "uid_reuse_contention"
	TestAmbiguityGitPush                  TestSandboxAmbiguityReason = "git_push_attempt"
	TestAmbiguityRefWrite                 TestSandboxAmbiguityReason = "local_ref_write_attempt"
	TestAmbiguityNetworkAccess            TestSandboxAmbiguityReason = "network_access_attempt"
	TestAmbiguityExternalWrite            TestSandboxAmbiguityReason = "external_write_attempt"
	TestAmbiguityOriginalWorktreeMutation TestSandboxAmbiguityReason = "original_worktree_mutation"
	TestAmbiguityArbitraryExec            TestSandboxAmbiguityReason = "arbitrary_exec_attempt"
	TestAmbiguityForkEscape               TestSandboxAmbiguityReason = "fork_escape"
	TestAmbiguitySetsidEscape             TestSandboxAmbiguityReason = "setsid_new_session"
	TestAmbiguityDelayedMutation          TestSandboxAmbiguityReason = "delayed_write_ref_update"
	TestAmbiguityMissingModule            TestSandboxAmbiguityReason = "missing_module"
	TestAmbiguityCacheDrift               TestSandboxAmbiguityReason = "cache_drift"
	TestAmbiguityToolchainReplacement     TestSandboxAmbiguityReason = "toolchain_replacement"

	TestToolchainVerification       TestSandboxVerificationKind = "toolchain_manifest_verification"
	TestProfileVerification         TestSandboxVerificationKind = "test_profile_verification"
	TestCandidateCopyVerification   TestSandboxVerificationKind = "candidate_copy_verification"
	TestSandboxBoundaryVerification TestSandboxVerificationKind = "test_sandbox_boundary_verification"
	TestTerminalProofVerification   TestSandboxVerificationKind = "terminal_proof_verification"
	TestRootCleanupVerification     TestSandboxVerificationKind = "root_cleanup_verification"
	TestUIDEmptyVerification        TestSandboxVerificationKind = "uid_empty_verification"

	maxTestSandboxIDBytes = 192
)

var ErrInvalidTestSandbox = errors.New("controlled repair test sandbox observation is invalid")

type TestSandboxAction string
type TestSandboxDisposition string
type TestSandboxRequirement string
type TestSandboxState string
type TestSandboxAmbiguityReason string
type TestTerminalCleanupResult string
type TestSandboxVerificationKind string

// GoToolchainManifest is the release-pinned root-owned Go toolchain. It binds
// the exact Go version, executable hash, and module-cache hash.
type GoToolchainManifest struct {
	SchemaVersion       string `json:"schema_version"`
	ManifestHash        string `json:"manifest_hash"`
	ManifestID          string `json:"manifest_id"`
	GoVersion           string `json:"go_version"`
	ExecutableHash      string `json:"executable_hash"`
	ModuleCacheHash     string `json:"module_cache_hash"`
	RootOwned           bool   `json:"root_owned"`
	ReadOnlyModuleCache bool   `json:"read_only_module_cache"`
}

// GoTestProfile is the compiled closed offline Go test profile. It freezes
// the exact command, environment, timeout, and resource bounds.
type GoTestProfile struct {
	SchemaVersion       string `json:"schema_version"`
	ProfileHash         string `json:"profile_hash"`
	ProfileID           string `json:"profile_id"`
	Command             string `json:"command"`
	TimeoutSeconds      int    `json:"timeout_seconds"`
	MaxOutputBytes      int    `json:"max_output_bytes"`
	MaxCPUPercent       int    `json:"max_cpu_percent"`
	MaxMemoryBytes      int    `json:"max_memory_bytes"`
	CGOEnabled          string `json:"cgo_enabled"`
	GOENV               string `json:"goenv"`
	GOTOOLCHAIN         string `json:"gotoolchain"`
	GOPROXY             string `json:"goproxy"`
	GOSUMDB             string `json:"gosumdb"`
	GOVCS               string `json:"govcs"`
	GOWORK              string `json:"gowork"`
	NoNetwork           bool   `json:"no_network"`
	PrivateHome         bool   `json:"private_home"`
	PrivateTmpdir       bool   `json:"private_tmpdir"`
	PrivateGocache      bool   `json:"private_gocache"`
	ReadOnlyModuleCache bool   `json:"read_only_module_cache"`
}

// TestCandidateCopyObservation records the clean candidate copy facts.
type TestCandidateCopyObservation struct {
	SchemaVersion             string `json:"schema_version"`
	CopyHash                  string `json:"copy_hash"`
	NoDotGit                  bool   `json:"no_dot_git"`
	NoRemotes                 bool   `json:"no_remotes"`
	NoCredentials             bool   `json:"no_credentials"`
	NoOriginalRepo            bool   `json:"no_original_repo"`
	NoRetainedWorktree        bool   `json:"no_retained_worktree"`
	NoJournalPaths            bool   `json:"no_journal_paths"`
	NoKeyPaths                bool   `json:"no_key_paths"`
	CandidateRootIdentityHash string `json:"candidate_root_identity_hash"`
}

// TestTerminalProof binds the UID lease, leader/process-group identities,
// UID-empty observation, sandbox hash, root identities, descriptor closure,
// root cleanup, and boot epoch.
type TestTerminalProof struct {
	SchemaVersion               string                    `json:"schema_version"`
	ProofHash                   string                    `json:"proof_hash"`
	ProofID                     string                    `json:"proof_id"`
	UIDLeaseHash                string                    `json:"uid_lease_hash"`
	LeaderIdentityHash          string                    `json:"leader_identity_hash"`
	ProcessGroupIdentityHash    string                    `json:"process_group_identity_hash"`
	UIDEmptyObservationHash     string                    `json:"uid_empty_observation_hash"`
	SandboxHash                 string                    `json:"sandbox_hash"`
	RootIdentityHash            string                    `json:"root_identity_hash"`
	DescriptorClosureHash       string                    `json:"descriptor_closure_hash"`
	BootEpochHash               string                    `json:"boot_epoch_hash"`
	CleanupResult               TestTerminalCleanupResult `json:"cleanup_result"`
	RootScrubbedAndProvenAbsent bool                      `json:"root_scrubbed_and_proven_absent"`
	UIDEmptyVerified            bool                      `json:"uid_empty_verified"`
	DescriptorsClosed           bool                      `json:"descriptors_closed"`
	RootsScrubbed               bool                      `json:"roots_scrubbed"`
	ObservedAt                  string                    `json:"observed_at"`
}

// TestSandboxObservation is canonical, closed, self-hashed data. Decoding it
// proves no process, sandbox, or terminal fact.
type TestSandboxObservation struct {
	SchemaVersion                string                       `json:"schema_version"`
	ObservationHash              string                       `json:"observation_hash"`
	ObservationID                string                       `json:"observation_id"`
	State                        TestSandboxState             `json:"state"`
	AmbiguityReason              TestSandboxAmbiguityReason   `json:"ambiguity_reason"`
	AuthorizationHash            string                       `json:"authorization_hash"`
	ApprovalHash                 string                       `json:"approval_hash"`
	RequestHash                  string                       `json:"request_hash"`
	DispatchHash                 string                       `json:"dispatch_hash"`
	AttemptHash                  string                       `json:"attempt_hash"`
	AttemptNumber                int                          `json:"attempt_number"`
	AttemptCap                   int                          `json:"attempt_cap"`
	ClaimHash                    string                       `json:"claim_hash"`
	PredecessorClaimHash         string                       `json:"predecessor_claim_hash"`
	PredecessorTerminalEventHash string                       `json:"predecessor_terminal_event_hash"`
	RepositoryBindingHash        string                       `json:"repository_binding_hash"`
	RepositoryIdentityHash       string                       `json:"repository_identity_hash"`
	WorktreeSlotID               string                       `json:"worktree_slot_id"`
	AdapterCapabilityHash        string                       `json:"adapter_capability_hash"`
	UIDPoolHash                  string                       `json:"uid_pool_hash"`
	UIDLeaseHash                 string                       `json:"uid_lease_hash"`
	UID                          uint32                       `json:"uid"`
	GroupID                      uint32                       `json:"group_id"`
	ToolchainManifestID          string                       `json:"toolchain_manifest_id"`
	ToolchainManifestHash        string                       `json:"toolchain_manifest_hash"`
	TestProfileID                string                       `json:"test_profile_id"`
	TestProfileHash              string                       `json:"test_profile_hash"`
	CandidateCopy                TestCandidateCopyObservation `json:"candidate_copy"`
	TerminalProof                TestTerminalProof            `json:"terminal_proof"`
	RootIdentityHash             string                       `json:"root_identity_hash"`
	SandboxHash                  string                       `json:"sandbox_hash"`
	DescriptorClosureHash        string                       `json:"descriptor_closure_hash"`
	BootEpochID                  string                       `json:"boot_epoch_id"`
	BootEpochHash                string                       `json:"boot_epoch_hash"`
}

// TestSandboxVerifierAuthority is the release-pinned verifier identity.
type TestSandboxVerifierAuthority struct {
	SchemaVersion         string                        `json:"schema_version"`
	VerifierAuthorityHash string                        `json:"verifier_authority_hash"`
	VerifierID            string                        `json:"verifier_id"`
	ToolchainManifestID   string                        `json:"toolchain_manifest_id"`
	ToolchainManifestHash string                        `json:"toolchain_manifest_hash"`
	TestProfileID         string                        `json:"test_profile_id"`
	TestProfileHash       string                        `json:"test_profile_hash"`
	UIDPoolHash           string                        `json:"uid_pool_hash"`
	ReleasePinsHash       string                        `json:"release_pins_hash"`
	VerificationKinds     []TestSandboxVerificationKind `json:"verification_kinds"`
}

// TestSandboxVerificationSeal is a self-hashed provenance record.
type TestSandboxVerificationSeal struct {
	SchemaVersion         string                      `json:"schema_version"`
	SealHash              string                      `json:"seal_hash"`
	SealKind              TestSandboxVerificationKind `json:"seal_kind"`
	VerifierAuthorityHash string                      `json:"verifier_authority_hash"`
	ObservationHash       string                      `json:"observation_hash"`
	CanonicalHash         string                      `json:"canonical_hash"`
	EvidenceHash          string                      `json:"evidence_hash"`
}

// VerifiedTestSandboxSnapshot is opaque evidence.
type VerifiedTestSandboxSnapshot struct {
	valid                           bool
	toolchainVerified               bool
	testProfileVerified             bool
	candidateCopyVerified           bool
	sandboxBoundaryVerified         bool
	terminalProofVerified           bool
	rootCleanupVerified             bool
	uidEmptyVerified                bool
	observation                     TestSandboxObservation
	canonical                       []byte
	canonicalHash                   string
	verifierAuthorityHash           string
	toolchainVerificationSeal       string
	testProfileVerificationSeal     string
	candidateCopyVerificationSeal   string
	sandboxBoundaryVerificationSeal string
	terminalProofVerificationSeal   string
	rootCleanupVerificationSeal     string
	uidEmptyVerificationSeal        string
	integrityHash                   string
}

type testSandboxSnapshotIntegrity struct {
	IntegrityHash                   string `json:"integrity_hash"`
	Valid                           bool   `json:"valid"`
	ToolchainVerified               bool   `json:"toolchain_verified"`
	TestProfileVerified             bool   `json:"test_profile_verified"`
	CandidateCopyVerified           bool   `json:"candidate_copy_verified"`
	SandboxBoundaryVerified         bool   `json:"sandbox_boundary_verified"`
	TerminalProofVerified           bool   `json:"terminal_proof_verified"`
	RootCleanupVerified             bool   `json:"root_cleanup_verified"`
	UIDEmptyVerified                bool   `json:"uid_empty_verified"`
	ObservationHash                 string `json:"observation_hash"`
	CanonicalHash                   string `json:"canonical_hash"`
	VerifierAuthorityHash           string `json:"verifier_authority_hash"`
	ToolchainVerificationSeal       string `json:"toolchain_verification_seal"`
	TestProfileVerificationSeal     string `json:"test_profile_verification_seal"`
	CandidateCopyVerificationSeal   string `json:"candidate_copy_verification_seal"`
	SandboxBoundaryVerificationSeal string `json:"sandbox_boundary_verification_seal"`
	TerminalProofVerificationSeal   string `json:"terminal_proof_verification_seal"`
	RootCleanupVerificationSeal     string `json:"root_cleanup_verification_seal"`
	UIDEmptyVerificationSeal        string `json:"uid_empty_verification_seal"`
}

// TestSandboxAssessment is classification only. EffectAllowed is always false.
type TestSandboxAssessment struct {
	Disposition     TestSandboxDisposition
	EffectAllowed   bool
	NextRequirement TestSandboxRequirement
}

// VerifiedTestSandbox is an opaque predecessor capability for the attestation
// phase. It grants no filesystem, process, cleanup, or launch effect.
type VerifiedTestSandbox struct {
	valid                 bool
	observationHash       string
	snapshotIntegrityHash string
	verifierAuthorityHash string
	verificationSealsHash string
	authorizationHash     string
	claimHash             string
	attemptHash           string
	predecessorClaimHash  string
	adapterCapabilityHash string
	uidLeaseHash          string
	terminalProofHash     string
	canonical             []byte
	canonicalHash         string
	integrityHash         string
}

type verifiedTestSandboxIntegrity struct {
	IntegrityHash         string `json:"integrity_hash"`
	Valid                 bool   `json:"valid"`
	ObservationHash       string `json:"observation_hash"`
	SnapshotIntegrityHash string `json:"snapshot_integrity_hash"`
	VerifierAuthorityHash string `json:"verifier_authority_hash"`
	VerificationSealsHash string `json:"verification_seals_hash"`
	AuthorizationHash     string `json:"authorization_hash"`
	ClaimHash             string `json:"claim_hash"`
	AttemptHash           string `json:"attempt_hash"`
	PredecessorClaimHash  string `json:"predecessor_claim_hash"`
	AdapterCapabilityHash string `json:"adapter_capability_hash"`
	UIDLeaseHash          string `json:"uid_lease_hash"`
	TerminalProofHash     string `json:"terminal_proof_hash"`
	CanonicalHash         string `json:"canonical_hash"`
}

// --- Frozen compiled values ---

var compiledGoToolchainManifest = mustDeriveGoToolchainManifest()
var compiledGoTestProfile = mustDeriveGoTestProfile()
var compiledTestSandboxVerifierAuthority = mustDeriveTestSandboxVerifierAuthority()

func FrozenGoToolchainManifest() GoToolchainManifest {
	return compiledGoToolchainManifest
}

func FrozenGoTestProfile() GoTestProfile {
	return compiledGoTestProfile
}

func FrozenTestSandboxVerifierAuthority() TestSandboxVerifierAuthority {
	return compiledTestSandboxVerifierAuthority
}

func mustDeriveGoToolchainManifest() GoToolchainManifest {
	manifest, err := deriveGoToolchainManifest()
	if err != nil || !recordHashMatches(manifest, "manifest_hash", manifest.ManifestHash) {
		panic("invalid compiled Go toolchain manifest")
	}
	return manifest
}

func deriveGoToolchainManifest() (GoToolchainManifest, error) {
	manifest := GoToolchainManifest{
		SchemaVersion:       GoToolchainManifestSchemaVersion,
		ManifestID:          goToolchainManifestID,
		GoVersion:           goToolchainGoVersion,
		ExecutableHash:      testToolchainExecutableHash,
		ModuleCacheHash:     testModuleCacheHash,
		RootOwned:           true,
		ReadOnlyModuleCache: true,
	}
	manifest.ManifestHash = mustHashRecord(manifest, "manifest_hash")
	if !recordHashMatches(manifest, "manifest_hash", manifest.ManifestHash) {
		return GoToolchainManifest{}, ErrInvalidTestSandbox
	}
	return manifest, nil
}

func deriveFrozenGoToolchainManifest() (GoToolchainManifest, error) {
	derived, err := deriveGoToolchainManifest()
	if err != nil || !reflect.DeepEqual(derived, FrozenGoToolchainManifest()) {
		return GoToolchainManifest{}, ErrInvalidTestSandbox
	}
	return derived, nil
}

func mustDeriveGoTestProfile() GoTestProfile {
	profile, err := deriveGoTestProfile()
	if err != nil || !recordHashMatches(profile, "profile_hash", profile.ProfileHash) {
		panic("invalid compiled Go test profile")
	}
	return profile
}

func deriveGoTestProfile() (GoTestProfile, error) {
	profile := GoTestProfile{
		SchemaVersion:       GoTestProfileSchemaVersion,
		ProfileID:           goTestProfileID,
		Command:             testCommand,
		TimeoutSeconds:      testTimeoutSeconds,
		MaxOutputBytes:      testMaxOutputBytes,
		MaxCPUPercent:       testMaxCPUPercent,
		MaxMemoryBytes:      testMaxMemoryBytes,
		CGOEnabled:          "0",
		GOENV:               "off",
		GOTOOLCHAIN:         "local",
		GOPROXY:             "off",
		GOSUMDB:             "off",
		GOVCS:               "*:off",
		GOWORK:              "off",
		NoNetwork:           true,
		PrivateHome:         true,
		PrivateTmpdir:       true,
		PrivateGocache:      true,
		ReadOnlyModuleCache: true,
	}
	profile.ProfileHash = mustHashRecord(profile, "profile_hash")
	if !recordHashMatches(profile, "profile_hash", profile.ProfileHash) {
		return GoTestProfile{}, ErrInvalidTestSandbox
	}
	return profile, nil
}

func deriveFrozenGoTestProfile() (GoTestProfile, error) {
	derived, err := deriveGoTestProfile()
	if err != nil || !reflect.DeepEqual(derived, FrozenGoTestProfile()) {
		return GoTestProfile{}, ErrInvalidTestSandbox
	}
	return derived, nil
}

func mustDeriveTestSandboxVerifierAuthority() TestSandboxVerifierAuthority {
	authority, err := deriveTestSandboxVerifierAuthority()
	if err != nil || !recordHashMatches(authority, "verifier_authority_hash", authority.VerifierAuthorityHash) {
		panic("invalid compiled test sandbox verifier authority")
	}
	return authority
}

func deriveTestSandboxVerifierAuthority() (TestSandboxVerifierAuthority, error) {
	manifest, err := deriveGoToolchainManifest()
	if err != nil || !reflect.DeepEqual(manifest, FrozenGoToolchainManifest()) {
		return TestSandboxVerifierAuthority{}, ErrInvalidTestSandbox
	}
	profile, err := deriveGoTestProfile()
	if err != nil || !reflect.DeepEqual(profile, FrozenGoTestProfile()) {
		return TestSandboxVerifierAuthority{}, ErrInvalidTestSandbox
	}
	pool := FrozenAdapterUIDPool()
	authority := TestSandboxVerifierAuthority{
		SchemaVersion:         TestSandboxVerifierAuthoritySchemaVersion,
		VerifierID:            testSandboxVerifierID,
		ToolchainManifestID:   manifest.ManifestID,
		ToolchainManifestHash: manifest.ManifestHash,
		TestProfileID:         profile.ProfileID,
		TestProfileHash:       profile.ProfileHash,
		UIDPoolHash:           pool.PoolHash,
		ReleasePinsHash:       FrozenReleasePins().ReleasePinsHash,
		VerificationKinds:     testSandboxVerificationKinds(),
	}
	authority.VerifierAuthorityHash = mustHashRecord(authority, "verifier_authority_hash")
	if !recordHashMatches(authority, "verifier_authority_hash", authority.VerifierAuthorityHash) {
		return TestSandboxVerifierAuthority{}, ErrInvalidTestSandbox
	}
	return authority, nil
}

func deriveFrozenTestSandboxVerifierAuthority() (TestSandboxVerifierAuthority, error) {
	derived, err := deriveTestSandboxVerifierAuthority()
	if err != nil || !reflect.DeepEqual(derived, FrozenTestSandboxVerifierAuthority()) {
		return TestSandboxVerifierAuthority{}, ErrInvalidTestSandbox
	}
	return derived, nil
}

func testSandboxVerificationKinds() []TestSandboxVerificationKind {
	return []TestSandboxVerificationKind{
		TestToolchainVerification,
		TestProfileVerification,
		TestCandidateCopyVerification,
		TestSandboxBoundaryVerification,
		TestTerminalProofVerification,
		TestRootCleanupVerification,
		TestUIDEmptyVerification,
	}
}

func validTestSandboxVerificationKind(value TestSandboxVerificationKind) bool {
	switch value {
	case TestToolchainVerification, TestProfileVerification,
		TestCandidateCopyVerification, TestSandboxBoundaryVerification,
		TestTerminalProofVerification, TestRootCleanupVerification,
		TestUIDEmptyVerification:
		return true
	default:
		return false
	}
}

// --- UID lease derivation ---

func deriveTestUIDLeaseID(attemptNumber int) string {
	return testUIDLeaseSlotPrefix + strconv.Itoa(attemptNumber) + testUIDLeaseSlotSuffix
}

// --- Verification seals ---

type testSandboxToolchainSealEvidence struct {
	ToolchainManifestHash string `json:"toolchain_manifest_hash"`
	GoVersion             string `json:"go_version"`
}

type testSandboxProfileSealEvidence struct {
	TestProfileHash string `json:"test_profile_hash"`
	Command         string `json:"command"`
}

type testSandboxCandidateCopySealEvidence struct {
	CopyHash           string `json:"copy_hash"`
	NoDotGit           bool   `json:"no_dot_git"`
	NoRemotes          bool   `json:"no_remotes"`
	NoCredentials      bool   `json:"no_credentials"`
	NoOriginalRepo     bool   `json:"no_original_repo"`
	NoRetainedWorktree bool   `json:"no_retained_worktree"`
}

type testSandboxBoundarySealEvidence struct {
	SandboxHash           string `json:"sandbox_hash"`
	DescriptorClosureHash string `json:"descriptor_closure_hash"`
}

type testSandboxTerminalProofSealEvidence struct {
	TerminalProofHash string                    `json:"terminal_proof_hash"`
	CleanupResult     TestTerminalCleanupResult `json:"cleanup_result"`
	UIDEmptyVerified  bool                      `json:"uid_empty_verified"`
}

type testSandboxRootCleanupSealEvidence struct {
	RootIdentityHash            string `json:"root_identity_hash"`
	RootScrubbedAndProvenAbsent bool   `json:"root_scrubbed_and_proven_absent"`
	RootsScrubbed               bool   `json:"roots_scrubbed"`
}

type testSandboxUIDEmptySealEvidence struct {
	UIDEmptyObservationHash string `json:"uid_empty_observation_hash"`
	UIDEmptyVerified        bool   `json:"uid_empty_verified"`
}

type testSandboxVerificationSealSet struct {
	toolchain       string
	testProfile     string
	candidateCopy   string
	sandboxBoundary string
	terminalProof   string
	rootCleanup     string
	uidEmpty        string
}

func (seals testSandboxVerificationSealSet) ordered() []string {
	return []string{seals.toolchain, seals.testProfile, seals.candidateCopy,
		seals.sandboxBoundary, seals.terminalProof, seals.rootCleanup, seals.uidEmpty}
}

func testSandboxSealEvidenceHash(kind TestSandboxVerificationKind, observation TestSandboxObservation) string {
	var evidence any
	switch kind {
	case TestToolchainVerification:
		evidence = testSandboxToolchainSealEvidence{
			ToolchainManifestHash: observation.ToolchainManifestHash,
			GoVersion:             FrozenGoToolchainManifest().GoVersion,
		}
	case TestProfileVerification:
		evidence = testSandboxProfileSealEvidence{
			TestProfileHash: observation.TestProfileHash,
			Command:         FrozenGoTestProfile().Command,
		}
	case TestCandidateCopyVerification:
		evidence = testSandboxCandidateCopySealEvidence{
			CopyHash:           observation.CandidateCopy.CopyHash,
			NoDotGit:           observation.CandidateCopy.NoDotGit,
			NoRemotes:          observation.CandidateCopy.NoRemotes,
			NoCredentials:      observation.CandidateCopy.NoCredentials,
			NoOriginalRepo:     observation.CandidateCopy.NoOriginalRepo,
			NoRetainedWorktree: observation.CandidateCopy.NoRetainedWorktree,
		}
	case TestSandboxBoundaryVerification:
		evidence = testSandboxBoundarySealEvidence{
			SandboxHash:           observation.SandboxHash,
			DescriptorClosureHash: observation.DescriptorClosureHash,
		}
	case TestTerminalProofVerification:
		evidence = testSandboxTerminalProofSealEvidence{
			TerminalProofHash: observation.TerminalProof.ProofHash,
			CleanupResult:     observation.TerminalProof.CleanupResult,
			UIDEmptyVerified:  observation.TerminalProof.UIDEmptyVerified,
		}
	case TestRootCleanupVerification:
		evidence = testSandboxRootCleanupSealEvidence{
			RootIdentityHash:            observation.RootIdentityHash,
			RootScrubbedAndProvenAbsent: observation.TerminalProof.RootScrubbedAndProvenAbsent,
			RootsScrubbed:               observation.TerminalProof.RootsScrubbed,
		}
	case TestUIDEmptyVerification:
		evidence = testSandboxUIDEmptySealEvidence{
			UIDEmptyObservationHash: observation.TerminalProof.UIDEmptyObservationHash,
			UIDEmptyVerified:        observation.TerminalProof.UIDEmptyVerified,
		}
	default:
		return ""
	}
	raw, err := canonicalBytes(evidence)
	if err != nil {
		return ""
	}
	return sha256Digest(raw)
}

func deriveTestSandboxVerificationSeal(kind TestSandboxVerificationKind, verifier TestSandboxVerifierAuthority, observation TestSandboxObservation, canonicalHash string) string {
	evidenceHash := testSandboxSealEvidenceHash(kind, observation)
	if !validHash(evidenceHash) {
		return ""
	}
	seal := TestSandboxVerificationSeal{
		SchemaVersion:         TestSandboxVerificationSealSchemaVersion,
		SealKind:              kind,
		VerifierAuthorityHash: verifier.VerifierAuthorityHash,
		ObservationHash:       observation.ObservationHash,
		CanonicalHash:         canonicalHash,
		EvidenceHash:          evidenceHash,
	}
	seal.SealHash = mustHashRecord(seal, "seal_hash")
	return seal.SealHash
}

func deriveTestSandboxVerificationSeals(verifier TestSandboxVerifierAuthority, observation TestSandboxObservation, canonicalHash string) testSandboxVerificationSealSet {
	return testSandboxVerificationSealSet{
		toolchain:       deriveTestSandboxVerificationSeal(TestToolchainVerification, verifier, observation, canonicalHash),
		testProfile:     deriveTestSandboxVerificationSeal(TestProfileVerification, verifier, observation, canonicalHash),
		candidateCopy:   deriveTestSandboxVerificationSeal(TestCandidateCopyVerification, verifier, observation, canonicalHash),
		sandboxBoundary: deriveTestSandboxVerificationSeal(TestSandboxBoundaryVerification, verifier, observation, canonicalHash),
		terminalProof:   deriveTestSandboxVerificationSeal(TestTerminalProofVerification, verifier, observation, canonicalHash),
		rootCleanup:     deriveTestSandboxVerificationSeal(TestRootCleanupVerification, verifier, observation, canonicalHash),
		uidEmpty:        deriveTestSandboxVerificationSeal(TestUIDEmptyVerification, verifier, observation, canonicalHash),
	}
}

func testSandboxVerificationSealsHash(seals testSandboxVerificationSealSet) string {
	ordered := seals.ordered()
	if len(ordered) != len(testSandboxVerificationKinds()) {
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

// --- Decoding ---

func DecodeTestSandboxObservation(raw []byte) (TestSandboxObservation, error) {
	value, err := decodeCanonicalRecord[TestSandboxObservation](raw)
	if err != nil || validateTestSandboxObservation(value) != nil {
		return TestSandboxObservation{}, ErrInvalidTestSandbox
	}
	return value, nil
}

// --- Evaluation ---

func EvaluateTestSandbox(expected SupervisorIntentAuthority, authorization *VerifiedAuthorization, claim *VerifiedSupervisorIntentClaim, predecessor *VerifiedSupervisorIntentClaim, predecessorTerminal *VerifiedSupervisorTerminalEvent, adapterSandbox *VerifiedAdapterSandbox, snapshot *VerifiedTestSandboxSnapshot, action TestSandboxAction, now time.Time) (TestSandboxAssessment, *VerifiedTestSandbox, error) {
	invalid := TestSandboxAssessment{}
	now = now.UTC()
	verifier, err := deriveFrozenTestSandboxVerifierAuthority()
	if err != nil || VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now) != nil {
		return invalid, nil, ErrInvalidTestSandbox
	}
	if expected.Phase != TestClaimPhase || expected.Sequence != 3 ||
		validateSupervisorIntentAuthority(expected, authorization) != nil ||
		validateSupervisorIntentFreshness(expected, authorization, now) != nil ||
		!verifiedSupervisorIntentClaimIntact(claim) ||
		validateSupervisorIntentClaim(expected, authorization, predecessor, predecessorTerminal, claim.claim) != nil ||
		!verifiedAdapterSandboxIntact(adapterSandbox) ||
		!verifiedTestSandboxSnapshotIntact(snapshot, verifier) ||
		!testSandboxSnapshotMatchesAuthority(snapshot, expected, authorization, claim, adapterSandbox) {
		return invalid, nil, ErrInvalidTestSandbox
	}

	observation := snapshot.observation
	switch observation.State {
	case TestSandboxTerminalProven:
		if action != TestSandboxAdmitTerminal ||
			!snapshot.toolchainVerified || !snapshot.testProfileVerified || !snapshot.candidateCopyVerified ||
			!snapshot.sandboxBoundaryVerified || !snapshot.terminalProofVerified || !snapshot.rootCleanupVerified || !snapshot.uidEmptyVerified {
			return invalid, nil, ErrInvalidTestSandbox
		}
		if !testSandboxTerminalProvenClosure(observation) {
			return invalid, nil, ErrInvalidTestSandbox
		}
		seals := deriveTestSandboxVerificationSeals(verifier, observation, snapshot.canonicalHash)
		capability := &VerifiedTestSandbox{
			valid:                 true,
			observationHash:       observation.ObservationHash,
			snapshotIntegrityHash: snapshot.integrityHash,
			verifierAuthorityHash: verifier.VerifierAuthorityHash,
			verificationSealsHash: testSandboxVerificationSealsHash(seals),
			authorizationHash:     observation.AuthorizationHash,
			claimHash:             observation.ClaimHash,
			attemptHash:           observation.AttemptHash,
			predecessorClaimHash:  observation.PredecessorClaimHash,
			adapterCapabilityHash: observation.AdapterCapabilityHash,
			uidLeaseHash:          observation.UIDLeaseHash,
			terminalProofHash:     observation.TerminalProof.ProofHash,
			canonical:             append([]byte(nil), snapshot.canonical...),
			canonicalHash:         snapshot.canonicalHash,
		}
		capability.integrityHash = verifiedTestSandboxIntegrityHash(capability)
		if !verifiedTestSandboxIntact(capability) {
			return invalid, nil, ErrInvalidTestSandbox
		}
		return TestSandboxAssessment{
			Disposition:     TestSandboxCapabilityReady,
			NextRequirement: TestSandboxNextAttestation,
		}, capability, nil
	case TestSandboxRetainedReplay:
		if action != TestSandboxStatusOnly ||
			!snapshot.toolchainVerified || !snapshot.testProfileVerified || !snapshot.candidateCopyVerified ||
			!snapshot.sandboxBoundaryVerified || !snapshot.terminalProofVerified || !snapshot.rootCleanupVerified || !snapshot.uidEmptyVerified {
			return invalid, nil, ErrInvalidTestSandbox
		}
		if !testSandboxTerminalProvenClosure(observation) {
			return invalid, nil, ErrInvalidTestSandbox
		}
		return TestSandboxAssessment{
			Disposition:     TestSandboxRetainedStatus,
			NextRequirement: TestSandboxNoFurtherEffect,
		}, nil, nil
	case TestSandboxRetainForHuman:
		if action != TestSandboxStatusOnly || !validTestSandboxWaitingReason(observation.AmbiguityReason) {
			return invalid, nil, ErrInvalidTestSandbox
		}
		return TestSandboxAssessment{
			Disposition:     TestSandboxWaitingForHuman,
			NextRequirement: TestSandboxHumanReviewRequired,
		}, nil, nil
	default:
		return invalid, nil, ErrInvalidTestSandbox
	}
}

// --- Validators ---

func validateGoToolchainManifest(value GoToolchainManifest) error {
	if value.SchemaVersion != GoToolchainManifestSchemaVersion ||
		!validClosedIdentifier(value.ManifestID, maxTestSandboxIDBytes) ||
		!validHash(value.ManifestHash) || !validHash(value.ExecutableHash) ||
		!validHash(value.ModuleCacheHash) || !recordHashMatches(value, "manifest_hash", value.ManifestHash) ||
		!value.RootOwned || !value.ReadOnlyModuleCache || value.GoVersion == "" {
		return ErrInvalidTestSandbox
	}
	return nil
}

func validateGoTestProfile(value GoTestProfile) error {
	if value.SchemaVersion != GoTestProfileSchemaVersion ||
		!validClosedIdentifier(value.ProfileID, maxTestSandboxIDBytes) ||
		!validHash(value.ProfileHash) || !recordHashMatches(value, "profile_hash", value.ProfileHash) ||
		value.Command != testCommand || value.TimeoutSeconds != testTimeoutSeconds ||
		value.MaxOutputBytes != testMaxOutputBytes || value.MaxCPUPercent != testMaxCPUPercent ||
		value.MaxMemoryBytes != testMaxMemoryBytes ||
		value.CGOEnabled != "0" || value.GOENV != "off" || value.GOTOOLCHAIN != "local" ||
		value.GOPROXY != "off" || value.GOSUMDB != "off" || value.GOVCS != "*:off" || value.GOWORK != "off" ||
		!value.NoNetwork || !value.PrivateHome || !value.PrivateTmpdir ||
		!value.PrivateGocache || !value.ReadOnlyModuleCache {
		return ErrInvalidTestSandbox
	}
	return nil
}

func validateTestCandidateCopy(value TestCandidateCopyObservation) error {
	if value.SchemaVersion != TestCandidateCopyObservationSchemaVersion ||
		!validHash(value.CopyHash) || !recordHashMatches(value, "copy_hash", value.CopyHash) ||
		!value.NoDotGit || !value.NoRemotes || !value.NoCredentials ||
		!value.NoOriginalRepo || !value.NoRetainedWorktree ||
		!value.NoJournalPaths || !value.NoKeyPaths ||
		!validHash(value.CandidateRootIdentityHash) {
		return ErrInvalidTestSandbox
	}
	return nil
}

func validateTestTerminalProof(value TestTerminalProof) error {
	if value.SchemaVersion != TestTerminalProofSchemaVersion ||
		!validClosedIdentifier(value.ProofID, maxTestSandboxIDBytes) ||
		!validHash(value.ProofHash) || !validHash(value.UIDLeaseHash) ||
		!validHash(value.LeaderIdentityHash) || !validHash(value.ProcessGroupIdentityHash) ||
		!validHash(value.UIDEmptyObservationHash) || !validHash(value.SandboxHash) ||
		!validHash(value.RootIdentityHash) || !validHash(value.DescriptorClosureHash) ||
		!validHash(value.BootEpochHash) ||
		!validTestTerminalCleanupResult(value.CleanupResult) ||
		!recordHashMatches(value, "proof_hash", value.ProofHash) {
		return ErrInvalidTestSandbox
	}
	if _, err := parseUTC(value.ObservedAt); err != nil {
		return ErrInvalidTestSandbox
	}
	return nil
}

func validateTestSandboxObservation(value TestSandboxObservation) error {
	if value.SchemaVersion != TestSandboxObservationSchemaVersion ||
		!validClosedIdentifier(value.ObservationID, maxTestSandboxIDBytes) ||
		!recordHashMatches(value, "observation_hash", value.ObservationHash) ||
		!validHash(value.AuthorizationHash) || !validHash(value.ApprovalHash) || !validHash(value.RequestHash) ||
		!validHash(value.DispatchHash) || !validHash(value.AttemptHash) || !validHash(value.ClaimHash) ||
		!validHash(value.PredecessorClaimHash) || !validHash(value.PredecessorTerminalEventHash) ||
		!validHash(value.RepositoryBindingHash) || !validHash(value.RepositoryIdentityHash) ||
		!validClosedIdentifier(value.WorktreeSlotID, maxTestSandboxIDBytes) || !validHash(value.AdapterCapabilityHash) ||
		!validHash(value.UIDPoolHash) || !validHash(value.UIDLeaseHash) ||
		!validClosedIdentifier(value.ToolchainManifestID, maxTestSandboxIDBytes) || !validHash(value.ToolchainManifestHash) ||
		!validClosedIdentifier(value.TestProfileID, maxTestSandboxIDBytes) || !validHash(value.TestProfileHash) ||
		!validHash(value.RootIdentityHash) || !validHash(value.SandboxHash) || !validHash(value.DescriptorClosureHash) ||
		!validClosedIdentifier(value.BootEpochID, maxTestSandboxIDBytes) || !validHash(value.BootEpochHash) ||
		value.AttemptNumber < 1 || value.AttemptNumber > AttemptCap || value.AttemptCap != AttemptCap ||
		value.UID == 0 || value.GroupID == 0 {
		return ErrInvalidTestSandbox
	}
	if !validTestSandboxStateReason(value.State, value.AmbiguityReason) {
		return ErrInvalidTestSandbox
	}
	if validateTestTerminalProof(value.TerminalProof) != nil || validateTestCandidateCopy(value.CandidateCopy) != nil {
		return ErrInvalidTestSandbox
	}
	pool := FrozenAdapterUIDPool()
	if value.UIDPoolHash != pool.PoolHash || value.GroupID != pool.GroupID {
		return ErrInvalidTestSandbox
	}
	manifest := FrozenGoToolchainManifest()
	if value.ToolchainManifestID != manifest.ManifestID || value.ToolchainManifestHash != manifest.ManifestHash {
		return ErrInvalidTestSandbox
	}
	profile := FrozenGoTestProfile()
	if value.TestProfileID != profile.ProfileID || value.TestProfileHash != profile.ProfileHash {
		return ErrInvalidTestSandbox
	}
	found := false
	for _, entry := range pool.Entries {
		if entry.UID == value.UID {
			found = true
			break
		}
	}
	if !found {
		return ErrInvalidTestSandbox
	}
	return nil
}

func validTestSandboxStateReason(state TestSandboxState, reason TestSandboxAmbiguityReason) bool {
	switch state {
	case TestSandboxTerminalProven, TestSandboxRetainedReplay:
		return reason == ""
	case TestSandboxRetainForHuman:
		return validTestSandboxWaitingReason(reason)
	default:
		return false
	}
}

func validTestSandboxWaitingReason(value TestSandboxAmbiguityReason) bool {
	switch value {
	case TestAmbiguityUIDNotEmpty, TestAmbiguityRootsNotScrubbed,
		TestAmbiguityStalePID, TestAmbiguityUIDReuse,
		TestAmbiguityGitPush, TestAmbiguityRefWrite,
		TestAmbiguityNetworkAccess, TestAmbiguityExternalWrite,
		TestAmbiguityOriginalWorktreeMutation, TestAmbiguityArbitraryExec,
		TestAmbiguityForkEscape, TestAmbiguitySetsidEscape,
		TestAmbiguityDelayedMutation, TestAmbiguityMissingModule,
		TestAmbiguityCacheDrift, TestAmbiguityToolchainReplacement:
		return true
	default:
		return false
	}
}

func validTestTerminalCleanupResult(value TestTerminalCleanupResult) bool {
	switch value {
	case TestCleanupUIDEmptyRootsScrubbed, TestCleanupUIDNonemptyRetained, TestCleanupPartialRetained:
		return true
	default:
		return false
	}
}

func testSandboxTerminalProvenClosure(observation TestSandboxObservation) bool {
	proof := observation.TerminalProof
	return proof.UIDLeaseHash == observation.UIDLeaseHash &&
		proof.SandboxHash == observation.SandboxHash &&
		proof.RootIdentityHash == observation.RootIdentityHash &&
		proof.DescriptorClosureHash == observation.DescriptorClosureHash &&
		proof.BootEpochHash == observation.BootEpochHash &&
		proof.CleanupResult == TestCleanupUIDEmptyRootsScrubbed &&
		proof.UIDEmptyVerified && proof.DescriptorsClosed &&
		proof.RootsScrubbed && proof.RootScrubbedAndProvenAbsent
}

// --- Snapshot integrity ---

func testSandboxSnapshotIntegrityHash(value *VerifiedTestSandboxSnapshot) string {
	if value == nil {
		return ""
	}
	record := testSandboxSnapshotIntegrity{
		Valid: value.valid, ToolchainVerified: value.toolchainVerified,
		TestProfileVerified: value.testProfileVerified, CandidateCopyVerified: value.candidateCopyVerified,
		SandboxBoundaryVerified: value.sandboxBoundaryVerified, TerminalProofVerified: value.terminalProofVerified,
		RootCleanupVerified: value.rootCleanupVerified, UIDEmptyVerified: value.uidEmptyVerified,
		ObservationHash: value.observation.ObservationHash, CanonicalHash: value.canonicalHash,
		VerifierAuthorityHash:           value.verifierAuthorityHash,
		ToolchainVerificationSeal:       value.toolchainVerificationSeal,
		TestProfileVerificationSeal:     value.testProfileVerificationSeal,
		CandidateCopyVerificationSeal:   value.candidateCopyVerificationSeal,
		SandboxBoundaryVerificationSeal: value.sandboxBoundaryVerificationSeal,
		TerminalProofVerificationSeal:   value.terminalProofVerificationSeal,
		RootCleanupVerificationSeal:     value.rootCleanupVerificationSeal,
		UIDEmptyVerificationSeal:        value.uidEmptyVerificationSeal,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedTestSandboxSnapshotIntact(value *VerifiedTestSandboxSnapshot, verifier TestSandboxVerifierAuthority) bool {
	if value == nil || !recordHashMatches(verifier, "verifier_authority_hash", verifier.VerifierAuthorityHash) ||
		!value.valid || !value.toolchainVerified || !value.testProfileVerified || !value.candidateCopyVerified ||
		!value.sandboxBoundaryVerified || !value.terminalProofVerified || !value.rootCleanupVerified || !value.uidEmptyVerified ||
		!validHash(value.integrityHash) || value.integrityHash != testSandboxSnapshotIntegrityHash(value) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	decoded, err := DecodeTestSandboxObservation(value.canonical)
	if err != nil || !reflect.DeepEqual(decoded, value.observation) {
		return false
	}
	seals := deriveTestSandboxVerificationSeals(verifier, decoded, value.canonicalHash)
	return value.toolchainVerificationSeal == seals.toolchain &&
		value.testProfileVerificationSeal == seals.testProfile &&
		value.candidateCopyVerificationSeal == seals.candidateCopy &&
		value.sandboxBoundaryVerificationSeal == seals.sandboxBoundary &&
		value.terminalProofVerificationSeal == seals.terminalProof &&
		value.rootCleanupVerificationSeal == seals.rootCleanup &&
		value.uidEmptyVerificationSeal == seals.uidEmpty
}

func testSandboxSnapshotMatchesAuthority(value *VerifiedTestSandboxSnapshot, expected SupervisorIntentAuthority, authorization *VerifiedAuthorization, claim *VerifiedSupervisorIntentClaim, adapterSandbox *VerifiedAdapterSandbox) bool {
	observation := value.observation
	return observation.AuthorizationHash == authorization.authorization.AuthorizationHash &&
		observation.ApprovalHash == authorization.authorization.ApprovalHash &&
		observation.RequestHash == expected.AcceptedDispatch.Request.RequestHash &&
		observation.DispatchHash == expected.AcceptedDispatch.DispatchHash &&
		observation.AttemptHash == expected.AttemptHash && observation.AttemptNumber == expected.AttemptNumber &&
		observation.AttemptCap == expected.AttemptCap && observation.ClaimHash == claim.claim.ClaimHash &&
		observation.PredecessorClaimHash == expected.PredecessorClaimHash &&
		observation.RepositoryBindingHash == expected.Repository.RepositoryBindingHash &&
		observation.RepositoryIdentityHash == expected.Repository.RepositoryIdentityHash &&
		observation.BootEpochID == expected.BootEpochID && observation.BootEpochHash == expected.BootEpochHash &&
		observation.AdapterCapabilityHash == adapterSandbox.snapshotIntegrityHash
}

// --- Capability integrity ---

func verifiedTestSandboxIntegrityHash(value *VerifiedTestSandbox) string {
	if value == nil {
		return ""
	}
	record := verifiedTestSandboxIntegrity{
		Valid: value.valid, ObservationHash: value.observationHash,
		SnapshotIntegrityHash: value.snapshotIntegrityHash,
		VerifierAuthorityHash: value.verifierAuthorityHash, VerificationSealsHash: value.verificationSealsHash,
		AuthorizationHash: value.authorizationHash, ClaimHash: value.claimHash, AttemptHash: value.attemptHash,
		PredecessorClaimHash: value.predecessorClaimHash, AdapterCapabilityHash: value.adapterCapabilityHash,
		UIDLeaseHash: value.uidLeaseHash, TerminalProofHash: value.terminalProofHash, CanonicalHash: value.canonicalHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedTestSandboxIntact(value *VerifiedTestSandbox) bool {
	if value == nil || !value.valid || !validHash(value.integrityHash) ||
		value.integrityHash != verifiedTestSandboxIntegrityHash(value) ||
		!validHash(value.snapshotIntegrityHash) || !validHash(value.authorizationHash) || !validHash(value.claimHash) ||
		!validHash(value.attemptHash) || !validHash(value.predecessorClaimHash) || !validHash(value.adapterCapabilityHash) ||
		!validHash(value.uidLeaseHash) || !validHash(value.terminalProofHash) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		!validHash(value.verifierAuthorityHash) || !validHash(value.verificationSealsHash) {
		return false
	}
	verifier := FrozenTestSandboxVerifierAuthority()
	if value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	observation, err := DecodeTestSandboxObservation(value.canonical)
	if err != nil || observation.State != TestSandboxTerminalProven ||
		observation.ObservationHash != value.observationHash ||
		observation.AuthorizationHash != value.authorizationHash ||
		observation.ClaimHash != value.claimHash || observation.AttemptHash != value.attemptHash ||
		observation.PredecessorClaimHash != value.predecessorClaimHash ||
		observation.AdapterCapabilityHash != value.adapterCapabilityHash ||
		observation.UIDLeaseHash != value.uidLeaseHash ||
		observation.TerminalProof.ProofHash != value.terminalProofHash {
		return false
	}
	seals := deriveTestSandboxVerificationSeals(verifier, observation, value.canonicalHash)
	return value.verificationSealsHash == testSandboxVerificationSealsHash(seals)
}
