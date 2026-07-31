package repaircontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const testProfileDocumentPath = "../../docs/experiments/p6-controlled-repair-test-profile.md"

const (
	testProfileMachineContractStart = "<!-- BEGIN P6 SLICE 6 MACHINE CONTRACT -->\n```json\n"
	testProfileMachineContractEnd   = "\n```\n<!-- END P6 SLICE 6 MACHINE CONTRACT -->"
)

type testProfileDocumentManifest struct {
	SchemaVersion         string                          `json:"schema_version"`
	Status                string                          `json:"status"`
	ObservationSchema     string                          `json:"observation_schema_version"`
	PriorSlice1To2Vectors int                             `json:"prior_slice_1_to_2_vector_count"`
	PriorSlice3Vectors    int                             `json:"prior_slice_3_vector_count"`
	PriorSlice4Vectors    int                             `json:"prior_slice_4_vector_count"`
	PriorSlice5Vectors    int                             `json:"prior_slice_5_vector_count"`
	Slice6VectorCount     int                             `json:"slice_6_vector_count"`
	EffectAllowedValues   []bool                          `json:"effect_allowed_values"`
	AllowedActions        []TestSandboxAction             `json:"allowed_actions"`
	SandboxStates         []TestSandboxState              `json:"sandbox_states"`
	AmbiguityReasons      []TestSandboxAmbiguityReason    `json:"ambiguity_reasons"`
	CleanupResults        []TestTerminalCleanupResult     `json:"cleanup_results"`
	Dispositions          []TestSandboxDisposition        `json:"dispositions"`
	Requirements          []TestSandboxRequirement        `json:"requirements"`
	VerifierAuthority     testProfileDocumentVerifierAuth `json:"verifier_authority"`
	ToolchainManifest     testProfileDocumentToolchain    `json:"toolchain_manifest"`
	TestProfile           testProfileDocumentProfile      `json:"test_profile"`
	UIDPool               testProfileDocumentUIDPool      `json:"uid_pool"`
	UIDLeaseGrammar       string                          `json:"uid_lease_grammar"`
	CanonicalFixture      testProfileDocumentFixture      `json:"canonical_fixture"`
	VectorIDs             []string                        `json:"vector_ids"`
}

type testProfileDocumentVerifierAuth struct {
	SchemaVersion         string                        `json:"schema_version"`
	VerifierID            string                        `json:"verifier_id"`
	VerifierAuthorityHash string                        `json:"verifier_authority_hash"`
	ReleasePinsHash       string                        `json:"release_pins_hash"`
	VerificationKinds     []TestSandboxVerificationKind `json:"verification_kinds"`
}

type testProfileDocumentToolchain struct {
	SchemaVersion string `json:"schema_version"`
	ManifestID    string `json:"manifest_id"`
	ManifestHash  string `json:"manifest_hash"`
	GoVersion     string `json:"go_version"`
}

type testProfileDocumentProfile struct {
	SchemaVersion string `json:"schema_version"`
	ProfileID     string `json:"profile_id"`
	ProfileHash   string `json:"profile_hash"`
	Command       string `json:"command"`
}

type testProfileDocumentUIDPool struct {
	SchemaVersion string `json:"schema_version"`
	PoolID        string `json:"pool_id"`
	PoolHash      string `json:"pool_hash"`
	GroupID       uint32 `json:"group_id"`
	GroupName     string `json:"group_name"`
	PoolSize      int    `json:"pool_size"`
}

type testProfileDocumentFixture struct {
	ObservationHash       string `json:"observation_hash"`
	CanonicalSHA256       string `json:"canonical_sha256"`
	SnapshotIntegrityHash string `json:"snapshot_integrity_hash"`
	AuthorizationHash     string `json:"authorization_hash"`
	ClaimHash             string `json:"claim_hash"`
	PredecessorClaimHash  string `json:"predecessor_claim_hash"`
	AttemptHash           string `json:"attempt_hash"`
	AdapterCapabilityHash string `json:"adapter_capability_hash"`
	UIDLeaseHash          string `json:"uid_lease_hash"`
	TerminalProofHash     string `json:"terminal_proof_hash"`
	UID                   uint32 `json:"uid"`
	GroupID               uint32 `json:"group_id"`
}

func TestP6Slice6NormativeDocumentMatchesTypesFixtureAndInventory(t *testing.T) {
	raw, err := os.ReadFile(testProfileDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, testProfileMachineContractStart)
	end := strings.Index(text, testProfileMachineContractEnd)
	if start < 0 || end < 0 || end <= start ||
		strings.Count(text, testProfileMachineContractStart) != 1 ||
		strings.Count(text, testProfileMachineContractEnd) != 1 {
		t.Fatal("normative document must contain exactly one Slice-6 machine-contract JSON block")
	}
	start += len(testProfileMachineContractStart)
	var got testProfileDocumentManifest
	decoder := json.NewDecoder(strings.NewReader(text[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode Slice-6 normative machine contract: %v", err)
	}
	want := expectedTestProfileDocumentManifest(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Slice-6 normative machine contract drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func expectedTestProfileDocumentManifest(t *testing.T) testProfileDocumentManifest {
	t.Helper()
	fixture := canonicalTestSandboxFixtureForTest(t)
	verifier := FrozenTestSandboxVerifierAuthority()
	manifest := FrozenGoToolchainManifest()
	profile := FrozenGoTestProfile()
	pool := FrozenAdapterUIDPool()
	return testProfileDocumentManifest{
		SchemaVersion:         "ananke.controlled-repair-test-sandbox-document.v1",
		Status:                "slice_6_candidate_pending_independent_frozen_source_review",
		ObservationSchema:     TestSandboxObservationSchemaVersion,
		PriorSlice1To2Vectors: len(canonicalAcceptanceVectorIDs),
		PriorSlice3Vectors:    len(canonicalSupervisorIntentVectorIDs),
		PriorSlice4Vectors:    len(canonicalRepositoryWorktreeVectorIDs),
		PriorSlice5Vectors:    len(canonicalAdapterSandboxVectorIDs),
		Slice6VectorCount:     len(canonicalTestSandboxVectorIDs),
		EffectAllowedValues:   []bool{false},
		AllowedActions:        []TestSandboxAction{TestSandboxAdmitTerminal, TestSandboxStatusOnly},
		SandboxStates: []TestSandboxState{
			TestSandboxTerminalProven,
			TestSandboxRetainedReplay,
			TestSandboxRetainForHuman,
		},
		AmbiguityReasons: []TestSandboxAmbiguityReason{
			TestAmbiguityUIDNotEmpty,
			TestAmbiguityRootsNotScrubbed,
			TestAmbiguityStalePID,
			TestAmbiguityUIDReuse,
			TestAmbiguityGitPush,
			TestAmbiguityRefWrite,
			TestAmbiguityNetworkAccess,
			TestAmbiguityExternalWrite,
			TestAmbiguityOriginalWorktreeMutation,
			TestAmbiguityArbitraryExec,
			TestAmbiguityForkEscape,
			TestAmbiguitySetsidEscape,
			TestAmbiguityDelayedMutation,
			TestAmbiguityMissingModule,
			TestAmbiguityCacheDrift,
			TestAmbiguityToolchainReplacement,
		},
		CleanupResults: []TestTerminalCleanupResult{
			TestCleanupUIDEmptyRootsScrubbed,
			TestCleanupUIDNonemptyRetained,
			TestCleanupPartialRetained,
		},
		Dispositions: []TestSandboxDisposition{
			TestSandboxCapabilityReady,
			TestSandboxRetainedStatus,
			TestSandboxWaitingForHuman,
		},
		Requirements: []TestSandboxRequirement{
			TestSandboxNextAttestation,
			TestSandboxNoFurtherEffect,
			TestSandboxHumanReviewRequired,
		},
		VerifierAuthority: testProfileDocumentVerifierAuth{
			SchemaVersion:         TestSandboxVerifierAuthoritySchemaVersion,
			VerifierID:            verifier.VerifierID,
			VerifierAuthorityHash: verifier.VerifierAuthorityHash,
			ReleasePinsHash:       verifier.ReleasePinsHash,
			VerificationKinds:     verifier.VerificationKinds,
		},
		ToolchainManifest: testProfileDocumentToolchain{
			SchemaVersion: GoToolchainManifestSchemaVersion,
			ManifestID:    manifest.ManifestID,
			ManifestHash:  manifest.ManifestHash,
			GoVersion:     manifest.GoVersion,
		},
		TestProfile: testProfileDocumentProfile{
			SchemaVersion: GoTestProfileSchemaVersion,
			ProfileID:     profile.ProfileID,
			ProfileHash:   profile.ProfileHash,
			Command:       profile.Command,
		},
		UIDPool: testProfileDocumentUIDPool{
			SchemaVersion: AdapterUIDPoolSchemaVersion,
			PoolID:        pool.PoolID,
			PoolHash:      pool.PoolHash,
			GroupID:       pool.GroupID,
			GroupName:     pool.GroupName,
			PoolSize:      pool.PoolSize,
		},
		UIDLeaseGrammar: testUIDLeaseSlotPrefix + "<attempt_number>" + testUIDLeaseSlotSuffix,
		CanonicalFixture: testProfileDocumentFixture{
			ObservationHash:       fixture.observation.ObservationHash,
			CanonicalSHA256:       sha256Digest(fixture.canonical),
			SnapshotIntegrityHash: fixture.snapshot.integrityHash,
			AuthorizationHash:     fixture.observation.AuthorizationHash,
			ClaimHash:             fixture.observation.ClaimHash,
			PredecessorClaimHash:  fixture.observation.PredecessorClaimHash,
			AttemptHash:           fixture.observation.AttemptHash,
			AdapterCapabilityHash: fixture.observation.AdapterCapabilityHash,
			UIDLeaseHash:          fixture.observation.UIDLeaseHash,
			TerminalProofHash:     fixture.observation.TerminalProof.ProofHash,
			UID:                   fixture.observation.UID,
			GroupID:               fixture.observation.GroupID,
		},
		VectorIDs: append([]string(nil), canonicalTestSandboxVectorIDs...),
	}
}
