package repaircontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const attestationDocumentPath = "../../docs/experiments/p6-controlled-repair-attestation.md"

const (
	attestationMachineContractStart = "<!-- BEGIN P6 SLICE 7 MACHINE CONTRACT -->\n```json\n"
	attestationMachineContractEnd   = "\n```\n<!-- END P6 SLICE 7 MACHINE CONTRACT -->"
)

type attestationDocumentManifest struct {
	SchemaVersion         string                          `json:"schema_version"`
	Status                string                          `json:"status"`
	ObservationSchema     string                          `json:"observation_schema_version"`
	PriorSlice1To2Vectors int                             `json:"prior_slice_1_to_2_vector_count"`
	PriorSlice3Vectors    int                             `json:"prior_slice_3_vector_count"`
	PriorSlice4Vectors    int                             `json:"prior_slice_4_vector_count"`
	PriorSlice5Vectors    int                             `json:"prior_slice_5_vector_count"`
	PriorSlice6Vectors    int                             `json:"prior_slice_6_vector_count"`
	Slice7VectorCount     int                             `json:"slice_7_vector_count"`
	EffectAllowedValues   []bool                          `json:"effect_allowed_values"`
	AllowedActions        []AttestationAction             `json:"allowed_actions"`
	AttestationStates     []AttestationState              `json:"attestation_states"`
	VerificationKinds     []AttestationVerificationKind   `json:"verification_kinds"`
	Dispositions          []AttestationDisposition        `json:"dispositions"`
	Requirements          []AttestationRequirement        `json:"requirements"`
	VerifierAuthority     attestationDocumentVerifierAuth `json:"verifier_authority"`
	CanonicalFixture      attestationDocumentFixture      `json:"canonical_fixture"`
	VectorIDs             []string                        `json:"vector_ids"`
}

type attestationDocumentVerifierAuth struct {
	SchemaVersion         string                        `json:"schema_version"`
	VerifierID            string                        `json:"verifier_id"`
	VerifierAuthorityHash string                        `json:"verifier_authority_hash"`
	ReleasePinsHash       string                        `json:"release_pins_hash"`
	SignatureDomain       string                        `json:"signature_domain"`
	VerificationKinds     []AttestationVerificationKind `json:"verification_kinds"`
}

type attestationDocumentFixture struct {
	AttestationHash          string `json:"attestation_hash"`
	CanonicalSHA256          string `json:"canonical_sha256"`
	SnapshotIntegrityHash    string `json:"snapshot_integrity_hash"`
	AuthorizationHash        string `json:"authorization_hash"`
	TestClaimHash            string `json:"test_claim_hash"`
	AdapterClaimHash         string `json:"adapter_claim_hash"`
	MaterializationClaimHash string `json:"materialization_claim_hash"`
	RepositoryBindingHash    string `json:"repository_binding_hash"`
	AdapterCapabilityHash    string `json:"adapter_capability_hash"`
	TestCapabilityHash       string `json:"test_capability_hash"`
	SignatureDomain          string `json:"signature_domain"`
	State                    string `json:"state"`
}

func TestP6Slice7NormativeDocumentMatchesTypesFixtureAndInventory(t *testing.T) {
	raw, err := os.ReadFile(attestationDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, attestationMachineContractStart)
	end := strings.Index(text, attestationMachineContractEnd)
	if start < 0 || end < 0 || end <= start ||
		strings.Count(text, attestationMachineContractStart) != 1 ||
		strings.Count(text, attestationMachineContractEnd) != 1 {
		t.Fatal("normative document must contain exactly one Slice-7 machine-contract JSON block")
	}
	start += len(attestationMachineContractStart)
	var got attestationDocumentManifest
	decoder := json.NewDecoder(strings.NewReader(text[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode Slice-7 normative machine contract: %v", err)
	}
	want := expectedAttestationDocumentManifest(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Slice-7 normative machine contract drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func expectedAttestationDocumentManifest(t *testing.T) attestationDocumentManifest {
	t.Helper()
	fixture := canonicalAttestationFixtureForTest(t)
	verifier := FrozenAttestationVerifierAuthority()
	return attestationDocumentManifest{
		SchemaVersion:         "ananke.controlled-repair-attestation-document.v1",
		Status:                "slice_7_candidate_pending_independent_frozen_source_review",
		ObservationSchema:     AttestationSchemaVersion,
		PriorSlice1To2Vectors: len(canonicalAcceptanceVectorIDs),
		PriorSlice3Vectors:    len(canonicalSupervisorIntentVectorIDs),
		PriorSlice4Vectors:    len(canonicalRepositoryWorktreeVectorIDs),
		PriorSlice5Vectors:    len(canonicalAdapterSandboxVectorIDs),
		PriorSlice6Vectors:    len(canonicalTestSandboxVectorIDs),
		Slice7VectorCount:     len(canonicalAttestationVectorIDs),
		EffectAllowedValues:   []bool{false},
		AllowedActions:        []AttestationAction{AttestationAdmitForReview, AttestationStatusOnly},
		AttestationStates:     []AttestationState{AttestationWaitingForReview},
		VerificationKinds:     verifier.VerificationKinds,
		Dispositions: []AttestationDisposition{
			AttestationCapabilityReady,
			AttestationRetainedStatus,
		},
		Requirements: []AttestationRequirement{
			AttestationNextVerification,
			AttestationNoFurtherEffect,
		},
		VerifierAuthority: attestationDocumentVerifierAuth{
			SchemaVersion:         AttestationVerifierAuthoritySchemaVersion,
			VerifierID:            verifier.VerifierID,
			VerifierAuthorityHash: verifier.VerifierAuthorityHash,
			ReleasePinsHash:       verifier.ReleasePinsHash,
			SignatureDomain:       verifier.SignatureDomain,
			VerificationKinds:     verifier.VerificationKinds,
		},
		CanonicalFixture: attestationDocumentFixture{
			AttestationHash:          fixture.attestation.AttestationHash,
			CanonicalSHA256:          sha256Digest(fixture.canonical),
			SnapshotIntegrityHash:    fixture.snapshot.integrityHash,
			AuthorizationHash:        fixture.attestation.AuthorizationHash,
			TestClaimHash:            fixture.attestation.TestClaimHash,
			AdapterClaimHash:         fixture.attestation.AdapterClaimHash,
			MaterializationClaimHash: fixture.attestation.MaterializationClaimHash,
			RepositoryBindingHash:    fixture.attestation.RepositoryBindingHash,
			AdapterCapabilityHash:    fixture.attestation.AdapterCapabilityHash,
			TestCapabilityHash:       fixture.attestation.TestCapabilityHash,
			SignatureDomain:          fixture.attestation.SignatureDomain,
			State:                    string(fixture.attestation.State),
		},
		VectorIDs: append([]string(nil), canonicalAttestationVectorIDs...),
	}
}
