package repaircontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const anankeVerificationDocumentPath = "../../docs/experiments/p6-controlled-repair-ananke-verification.md"

const (
	anankeVerificationMachineContractStart = "<!-- BEGIN P6 SLICE 8 MACHINE CONTRACT -->\n```json\n"
	anankeVerificationMachineContractEnd   = "\n```\n<!-- END P6 SLICE 8 MACHINE CONTRACT -->"
)

type anankeVerificationDocumentManifest struct {
	SchemaVersion         string                             `json:"schema_version"`
	Status                string                             `json:"status"`
	ObservationSchema     string                             `json:"observation_schema_version"`
	PriorSlice1To2Vectors int                                `json:"prior_slice_1_to_2_vector_count"`
	PriorSlice3Vectors    int                                `json:"prior_slice_3_vector_count"`
	PriorSlice4Vectors    int                                `json:"prior_slice_4_vector_count"`
	PriorSlice5Vectors    int                                `json:"prior_slice_5_vector_count"`
	PriorSlice6Vectors    int                                `json:"prior_slice_6_vector_count"`
	PriorSlice7Vectors    int                                `json:"prior_slice_7_vector_count"`
	Slice8VectorCount     int                                `json:"slice_8_vector_count"`
	EffectAllowedValues   []bool                             `json:"effect_allowed_values"`
	AllowedActions        []AnankeVerificationAction         `json:"allowed_actions"`
	VerificationStates    []AnankeVerificationState          `json:"verification_states"`
	VerificationKinds     []AnankeVerificationKind           `json:"verification_kinds"`
	Dispositions          []AnankeVerificationDisposition    `json:"dispositions"`
	Requirements          []AnankeVerificationRequirement    `json:"requirements"`
	VerifierAuthority     anankeVerificationDocumentVerifier `json:"verifier_authority"`
	CanonicalFixture      anankeVerificationDocumentFixture  `json:"canonical_fixture"`
	VectorIDs             []string                           `json:"vector_ids"`
}

type anankeVerificationDocumentVerifier struct {
	SchemaVersion         string                   `json:"schema_version"`
	VerifierID            string                   `json:"verifier_id"`
	VerifierAuthorityHash string                   `json:"verifier_authority_hash"`
	ReleasePinsHash       string                   `json:"release_pins_hash"`
	SignatureDomain       string                   `json:"signature_domain"`
	VerificationKinds     []AnankeVerificationKind `json:"verification_kinds"`
}

type anankeVerificationDocumentFixture struct {
	VerificationHash    string `json:"verification_hash"`
	CanonicalSHA256     string `json:"canonical_sha256"`
	RecordIntegrityHash string `json:"record_integrity_hash"`
	AttestationHash     string `json:"attestation_hash"`
	AuthorizationHash   string `json:"authorization_hash"`
	SignatureDomain     string `json:"signature_domain"`
	State               string `json:"state"`
}

func TestP6Slice8NormativeDocumentMatchesTypesFixtureAndInventory(t *testing.T) {
	raw, err := os.ReadFile(anankeVerificationDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, anankeVerificationMachineContractStart)
	end := strings.Index(text, anankeVerificationMachineContractEnd)
	if start < 0 || end < 0 || end <= start ||
		strings.Count(text, anankeVerificationMachineContractStart) != 1 ||
		strings.Count(text, anankeVerificationMachineContractEnd) != 1 {
		t.Fatal("normative document must contain exactly one Slice-8 machine-contract JSON block")
	}
	start += len(anankeVerificationMachineContractStart)
	var got anankeVerificationDocumentManifest
	decoder := json.NewDecoder(strings.NewReader(text[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode Slice-8 normative machine contract: %v", err)
	}
	want := expectedAnankeVerificationDocumentManifest(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Slice-8 normative machine contract drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func expectedAnankeVerificationDocumentManifest(t *testing.T) anankeVerificationDocumentManifest {
	t.Helper()
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	verifier := FrozenAnankeVerifierAuthority()
	return anankeVerificationDocumentManifest{
		SchemaVersion:         "ananke.controlled-repair-ananke-verification-document.v1",
		Status:                "slice_8_candidate_pending_independent_frozen_source_review",
		ObservationSchema:     AnankeVerificationSchemaVersion,
		PriorSlice1To2Vectors: len(canonicalAcceptanceVectorIDs),
		PriorSlice3Vectors:    len(canonicalSupervisorIntentVectorIDs),
		PriorSlice4Vectors:    len(canonicalRepositoryWorktreeVectorIDs),
		PriorSlice5Vectors:    len(canonicalAdapterSandboxVectorIDs),
		PriorSlice6Vectors:    len(canonicalTestSandboxVectorIDs),
		PriorSlice7Vectors:    len(canonicalAttestationVectorIDs),
		Slice8VectorCount:     len(canonicalAnankeVerificationVectorIDs),
		EffectAllowedValues:   []bool{false},
		AllowedActions:        []AnankeVerificationAction{AnankeVerificationAdmitReview, AnankeVerificationStatusOnly},
		VerificationStates:    []AnankeVerificationState{AnankeVerificationWaitingForReview},
		VerificationKinds:     verifier.VerificationKinds,
		Dispositions: []AnankeVerificationDisposition{
			AnankeVerificationRecordReady,
			AnankeVerificationRetainedStatus,
		},
		Requirements: []AnankeVerificationRequirement{
			AnankeVerificationNextHuman,
			AnankeVerificationNoEffect,
		},
		VerifierAuthority: anankeVerificationDocumentVerifier{
			SchemaVersion:         AnankeVerifierAuthoritySchemaVersion,
			VerifierID:            verifier.VerifierID,
			VerifierAuthorityHash: verifier.VerifierAuthorityHash,
			ReleasePinsHash:       verifier.ReleasePinsHash,
			SignatureDomain:       verifier.SignatureDomain,
			VerificationKinds:     verifier.VerificationKinds,
		},
		CanonicalFixture: anankeVerificationDocumentFixture{
			VerificationHash:    fixture.record.VerificationHash,
			CanonicalSHA256:     sha256Digest(fixture.canonical),
			RecordIntegrityHash: fixture.snapshot.integrityHash,
			AttestationHash:     fixture.record.AttestationHash,
			AuthorizationHash:   fixture.record.AuthorizationHash,
			SignatureDomain:     SignatureDomain,
			State:               string(fixture.record.State),
		},
		VectorIDs: append([]string(nil), canonicalAnankeVerificationVectorIDs...),
	}
}
