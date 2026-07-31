package repaircontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const schemaCutoverDocumentPath = "../../docs/experiments/p6-schema-cutover.md"

const (
	schemaCutoverMachineContractStart = "<!-- BEGIN P6 SLICE 9 MACHINE CONTRACT -->\n```json\n"
	schemaCutoverMachineContractEnd   = "\n```\n<!-- END P6 SLICE 9 MACHINE CONTRACT -->"
)

type schemaCutoverDocumentManifest struct {
	SchemaVersion         string                         `json:"schema_version"`
	Status                string                         `json:"status"`
	ObservationSchema     string                         `json:"observation_schema_version"`
	PriorSlice1To2Vectors int                            `json:"prior_slice_1_to_2_vector_count"`
	PriorSlice3Vectors    int                            `json:"prior_slice_3_vector_count"`
	PriorSlice4Vectors    int                            `json:"prior_slice_4_vector_count"`
	PriorSlice5Vectors    int                            `json:"prior_slice_5_vector_count"`
	PriorSlice6Vectors    int                            `json:"prior_slice_6_vector_count"`
	PriorSlice7Vectors    int                            `json:"prior_slice_7_vector_count"`
	PriorSlice8Vectors    int                            `json:"prior_slice_8_vector_count"`
	Slice9VectorCount     int                            `json:"slice_9_vector_count"`
	EffectAllowedValues   []bool                         `json:"effect_allowed_values"`
	AllowedActions        []SchemaCutoverAction          `json:"allowed_actions"`
	CutoverStates         []SchemaCutoverState           `json:"cutover_states"`
	VerificationKinds     []SchemaCutoverSealKind        `json:"verification_kinds"`
	Dispositions          []SchemaCutoverDisposition     `json:"dispositions"`
	Requirements          []SchemaCutoverRequirement     `json:"requirements"`
	CutoverAuthority      schemaCutoverDocumentAuthority `json:"cutover_authority"`
	CanonicalFixture      schemaCutoverDocumentFixture   `json:"canonical_fixture"`
	VectorIDs             []string                       `json:"vector_ids"`
}

type schemaCutoverDocumentAuthority struct {
	SchemaVersion                    string                  `json:"schema_version"`
	CutoverID                        string                  `json:"cutover_id"`
	CutoverAuthorityHash             string                  `json:"cutover_authority_hash"`
	ReleasePinsHash                  string                  `json:"release_pins_hash"`
	AcceptedStoreSchemaVersion       int                     `json:"accepted_store_schema_version"`
	AcceptedContractSchemas          []string                `json:"accepted_contract_schemas"`
	RejectedSchemaVersions           []int                   `json:"rejected_schema_versions"`
	RejectedAPIMarkers               []string                `json:"rejected_api_markers"`
	ForbiddenBinaryMarkers           []string                `json:"forbidden_binary_markers"`
	ForbiddenProtocolAdapterSPKIHash string                  `json:"forbidden_protocol_adapter_spki_hash"`
	AcceptedRepairAttestorLeafSPKI   string                  `json:"accepted_repair_attestor_leaf_spki"`
	VerificationKinds                []SchemaCutoverSealKind `json:"verification_kinds"`
}

type schemaCutoverDocumentFixture struct {
	CutoverHash                      string `json:"cutover_hash"`
	CanonicalSHA256                  string `json:"canonical_sha256"`
	RecordIntegrityHash              string `json:"record_integrity_hash"`
	AcceptedStoreSchemaVersion       int    `json:"accepted_store_schema_version"`
	ReleasePinsHash                  string `json:"release_pins_hash"`
	ForbiddenProtocolAdapterSPKIHash string `json:"forbidden_protocol_adapter_spki_hash"`
	AcceptedRepairAttestorLeafSPKI   string `json:"accepted_repair_attestor_leaf_spki"`
	State                            string `json:"state"`
}

func TestP6Slice9NormativeDocumentMatchesTypesFixtureAndInventory(t *testing.T) {
	raw, err := os.ReadFile(schemaCutoverDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, schemaCutoverMachineContractStart)
	end := strings.Index(text, schemaCutoverMachineContractEnd)
	if start < 0 || end < 0 || end <= start ||
		strings.Count(text, schemaCutoverMachineContractStart) != 1 ||
		strings.Count(text, schemaCutoverMachineContractEnd) != 1 {
		t.Fatal("normative document must contain exactly one Slice-9 machine-contract JSON block")
	}
	start += len(schemaCutoverMachineContractStart)
	var got schemaCutoverDocumentManifest
	decoder := json.NewDecoder(strings.NewReader(text[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode Slice-9 normative machine contract: %v", err)
	}
	want := expectedSchemaCutoverDocumentManifest(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Slice-9 normative machine contract drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func expectedSchemaCutoverDocumentManifest(t *testing.T) schemaCutoverDocumentManifest {
	t.Helper()
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	authority := FrozenSchemaCutoverAuthority()
	return schemaCutoverDocumentManifest{
		SchemaVersion:         "ananke.controlled-repair-schema-cutover-document.v1",
		Status:                "slice_9_candidate_pending_independent_frozen_source_review",
		ObservationSchema:     SchemaCutoverRecordSchemaVersion,
		PriorSlice1To2Vectors: len(canonicalAcceptanceVectorIDs),
		PriorSlice3Vectors:    len(canonicalSupervisorIntentVectorIDs),
		PriorSlice4Vectors:    len(canonicalRepositoryWorktreeVectorIDs),
		PriorSlice5Vectors:    len(canonicalAdapterSandboxVectorIDs),
		PriorSlice6Vectors:    len(canonicalTestSandboxVectorIDs),
		PriorSlice7Vectors:    len(canonicalAttestationVectorIDs),
		PriorSlice8Vectors:    len(canonicalAnankeVerificationVectorIDs),
		Slice9VectorCount:     len(canonicalSchemaCutoverVectorIDs),
		EffectAllowedValues:   []bool{false},
		AllowedActions:        []SchemaCutoverAction{SchemaCutoverAdmitCutover, SchemaCutoverStatusOnly},
		CutoverStates:         []SchemaCutoverState{SchemaCutoverCutoverAccepted},
		VerificationKinds:     authority.VerificationKinds,
		Dispositions: []SchemaCutoverDisposition{
			SchemaCutoverRecordReady,
			SchemaCutoverRetainedStatus,
		},
		Requirements: []SchemaCutoverRequirement{
			SchemaCutoverNextCutover,
			SchemaCutoverNoEffect,
		},
		CutoverAuthority: schemaCutoverDocumentAuthority{
			SchemaVersion:                    SchemaCutoverAuthoritySchemaVersion,
			CutoverID:                        authority.CutoverID,
			CutoverAuthorityHash:             authority.CutoverAuthorityHash,
			ReleasePinsHash:                  authority.ReleasePinsHash,
			AcceptedStoreSchemaVersion:       authority.AcceptedStoreSchemaVersion,
			AcceptedContractSchemas:          authority.AcceptedContractSchemas,
			RejectedSchemaVersions:           authority.RejectedSchemaVersions,
			RejectedAPIMarkers:               authority.RejectedAPIMarkers,
			ForbiddenBinaryMarkers:           authority.ForbiddenBinaryMarkers,
			ForbiddenProtocolAdapterSPKIHash: authority.ForbiddenProtocolAdapterSPKIHash,
			AcceptedRepairAttestorLeafSPKI:   authority.AcceptedRepairAttestorLeafSPKI,
			VerificationKinds:                authority.VerificationKinds,
		},
		CanonicalFixture: schemaCutoverDocumentFixture{
			CutoverHash:                      fixture.record.CutoverHash,
			CanonicalSHA256:                  sha256Digest(fixture.canonical),
			RecordIntegrityHash:              fixture.snapshot.integrityHash,
			AcceptedStoreSchemaVersion:       fixture.record.AcceptedStoreSchemaVersion,
			ReleasePinsHash:                  fixture.record.ReleasePinsHash,
			ForbiddenProtocolAdapterSPKIHash: fixture.record.ForbiddenProtocolAdapterSPKIHash,
			AcceptedRepairAttestorLeafSPKI:   fixture.record.AcceptedRepairAttestorLeafSPKI,
			State:                            string(fixture.record.State),
		},
		VectorIDs: append([]string(nil), canonicalSchemaCutoverVectorIDs...),
	}
}
