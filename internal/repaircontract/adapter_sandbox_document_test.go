package repaircontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const adapterSandboxDocumentPath = "../../docs/experiments/p6-controlled-repair-adapter-sandbox.md"

const (
	adapterSandboxMachineContractStart = "<!-- BEGIN P6 SLICE 5 MACHINE CONTRACT -->\n```json\n"
	adapterSandboxMachineContractEnd   = "\n```\n<!-- END P6 SLICE 5 MACHINE CONTRACT -->"
)

type adapterSandboxDocumentManifest struct {
	SchemaVersion         string                             `json:"schema_version"`
	Status                string                             `json:"status"`
	ObservationSchema     string                             `json:"observation_schema_version"`
	PriorSlice1To2Vectors int                                `json:"prior_slice_1_to_2_vector_count"`
	PriorSlice3Vectors    int                                `json:"prior_slice_3_vector_count"`
	PriorSlice4Vectors    int                                `json:"prior_slice_4_vector_count"`
	Slice5VectorCount     int                                `json:"slice_5_vector_count"`
	EffectAllowedValues   []bool                             `json:"effect_allowed_values"`
	AllowedActions        []AdapterSandboxAction             `json:"allowed_actions"`
	SandboxStates         []AdapterSandboxState              `json:"sandbox_states"`
	AmbiguityReasons      []AdapterSandboxAmbiguityReason    `json:"ambiguity_reasons"`
	CleanupResults        []AdapterTerminalCleanupResult     `json:"cleanup_results"`
	Dispositions          []AdapterSandboxDisposition        `json:"dispositions"`
	Requirements          []AdapterSandboxRequirement        `json:"requirements"`
	VerifierAuthority     adapterSandboxDocumentVerifierAuth `json:"verifier_authority"`
	SeatbeltProfile       adapterSandboxDocumentSeatbeltProf `json:"seatbelt_profile"`
	UIDPool               adapterSandboxDocumentUIDPool      `json:"uid_pool"`
	UIDLeaseGrammar       string                             `json:"uid_lease_grammar"`
	CanonicalFixture      adapterSandboxDocumentFixture      `json:"canonical_fixture"`
	VectorIDs             []string                           `json:"vector_ids"`
}

type adapterSandboxDocumentVerifierAuth struct {
	SchemaVersion         string                           `json:"schema_version"`
	VerifierID            string                           `json:"verifier_id"`
	VerifierAuthorityHash string                           `json:"verifier_authority_hash"`
	ReleasePinsHash       string                           `json:"release_pins_hash"`
	VerificationKinds     []AdapterSandboxVerificationKind `json:"verification_kinds"`
}

type adapterSandboxDocumentSeatbeltProf struct {
	SchemaVersion string `json:"schema_version"`
	ProfileID     string `json:"profile_id"`
	ProfileHash   string `json:"profile_hash"`
}

type adapterSandboxDocumentUIDPool struct {
	SchemaVersion string `json:"schema_version"`
	PoolID        string `json:"pool_id"`
	PoolHash      string `json:"pool_hash"`
	GroupID       uint32 `json:"group_id"`
	GroupName     string `json:"group_name"`
	PoolSize      int    `json:"pool_size"`
}

type adapterSandboxDocumentFixture struct {
	ObservationHash        string `json:"observation_hash"`
	CanonicalSHA256        string `json:"canonical_sha256"`
	SnapshotIntegrityHash  string `json:"snapshot_integrity_hash"`
	AuthorizationHash      string `json:"authorization_hash"`
	ClaimHash              string `json:"claim_hash"`
	PredecessorClaimHash   string `json:"predecessor_claim_hash"`
	AttemptHash            string `json:"attempt_hash"`
	WorktreeCapabilityHash string `json:"worktree_capability_hash"`
	UIDLeaseHash           string `json:"uid_lease_hash"`
	TerminalProofHash      string `json:"terminal_proof_hash"`
	UID                    uint32 `json:"uid"`
	GroupID                uint32 `json:"group_id"`
}

func TestP6Slice5NormativeDocumentMatchesTypesFixtureAndInventory(t *testing.T) {
	raw, err := os.ReadFile(adapterSandboxDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, adapterSandboxMachineContractStart)
	end := strings.Index(text, adapterSandboxMachineContractEnd)
	if start < 0 || end < 0 || end <= start ||
		strings.Count(text, adapterSandboxMachineContractStart) != 1 ||
		strings.Count(text, adapterSandboxMachineContractEnd) != 1 {
		t.Fatal("normative document must contain exactly one Slice-5 machine-contract JSON block")
	}
	start += len(adapterSandboxMachineContractStart)
	var got adapterSandboxDocumentManifest
	decoder := json.NewDecoder(strings.NewReader(text[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode Slice-5 normative machine contract: %v", err)
	}
	want := expectedAdapterSandboxDocumentManifest(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Slice-5 normative machine contract drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func expectedAdapterSandboxDocumentManifest(t *testing.T) adapterSandboxDocumentManifest {
	t.Helper()
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	verifier := FrozenAdapterSandboxVerifierAuthority()
	profile := FrozenAdapterSeatbeltProfile()
	pool := FrozenAdapterUIDPool()
	return adapterSandboxDocumentManifest{
		SchemaVersion:         "ananke.controlled-repair-adapter-sandbox-document.v1",
		Status:                "slice_5_candidate_pending_independent_frozen_source_review",
		ObservationSchema:     AdapterSandboxObservationSchemaVersion,
		PriorSlice1To2Vectors: len(canonicalAcceptanceVectorIDs),
		PriorSlice3Vectors:    len(canonicalSupervisorIntentVectorIDs),
		PriorSlice4Vectors:    len(canonicalRepositoryWorktreeVectorIDs),
		Slice5VectorCount:     len(canonicalAdapterSandboxVectorIDs),
		EffectAllowedValues:   []bool{false},
		AllowedActions:        []AdapterSandboxAction{AdapterSandboxAdmitTerminal, AdapterSandboxStatusOnly},
		SandboxStates: []AdapterSandboxState{
			AdapterSandboxTerminalProven,
			AdapterSandboxRetainedReplay,
			AdapterSandboxRetainForHuman,
		},
		AmbiguityReasons: []AdapterSandboxAmbiguityReason{
			AdapterAmbiguityUIDNotEmpty,
			AdapterAmbiguityDescriptorsOpen,
			AdapterAmbiguityRootsNotFrozen,
			AdapterAmbiguityStalePID,
			AdapterAmbiguityUIDReuse,
			AdapterAmbiguityChildAlive,
			AdapterAmbiguityBrokerEscape,
			AdapterAmbiguityIgnoredContext,
			AdapterAmbiguityDoubleFork,
			AdapterAmbiguitySetsidEscape,
			AdapterAmbiguityClosedStdio,
			AdapterAmbiguityDelayedMutation,
		},
		CleanupResults: []AdapterTerminalCleanupResult{
			AdapterCleanupUIDEmptyRootsFrozen,
			AdapterCleanupUIDNonemptyRetained,
			AdapterCleanupPartialRetained,
		},
		Dispositions: []AdapterSandboxDisposition{
			AdapterSandboxCapabilityReady,
			AdapterSandboxRetainedStatus,
			AdapterSandboxWaitingForHuman,
		},
		Requirements: []AdapterSandboxRequirement{
			AdapterSandboxNextTestPhase,
			AdapterSandboxNoFurtherEffect,
			AdapterSandboxHumanReviewRequired,
		},
		VerifierAuthority: adapterSandboxDocumentVerifierAuth{
			SchemaVersion:         AdapterSandboxVerifierAuthoritySchemaVersion,
			VerifierID:            verifier.VerifierID,
			VerifierAuthorityHash: verifier.VerifierAuthorityHash,
			ReleasePinsHash:       verifier.ReleasePinsHash,
			VerificationKinds:     verifier.VerificationKinds,
		},
		SeatbeltProfile: adapterSandboxDocumentSeatbeltProf{
			SchemaVersion: AdapterSeatbeltProfileSchemaVersion,
			ProfileID:     profile.ProfileID,
			ProfileHash:   profile.ProfileHash,
		},
		UIDPool: adapterSandboxDocumentUIDPool{
			SchemaVersion: AdapterUIDPoolSchemaVersion,
			PoolID:        pool.PoolID,
			PoolHash:      pool.PoolHash,
			GroupID:       pool.GroupID,
			GroupName:     pool.GroupName,
			PoolSize:      pool.PoolSize,
		},
		UIDLeaseGrammar: adapterUIDLeaseSlotPrefix + "<attempt_number>" + adapterUIDLeaseSlotSuffix,
		CanonicalFixture: adapterSandboxDocumentFixture{
			ObservationHash:        fixture.observation.ObservationHash,
			CanonicalSHA256:        sha256Digest(fixture.canonical),
			SnapshotIntegrityHash:  fixture.snapshot.integrityHash,
			AuthorizationHash:      fixture.observation.AuthorizationHash,
			ClaimHash:              fixture.observation.ClaimHash,
			PredecessorClaimHash:   fixture.observation.PredecessorClaimHash,
			AttemptHash:            fixture.observation.AttemptHash,
			WorktreeCapabilityHash: fixture.observation.WorktreeCapabilityHash,
			UIDLeaseHash:           fixture.observation.UIDLeaseHash,
			TerminalProofHash:      fixture.observation.TerminalProof.ProofHash,
			UID:                    fixture.observation.UID,
			GroupID:                fixture.observation.GroupID,
		},
		VectorIDs: append([]string(nil), canonicalAdapterSandboxVectorIDs...),
	}
}
