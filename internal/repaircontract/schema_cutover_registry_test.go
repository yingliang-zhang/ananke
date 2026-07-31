package repaircontract

import (
	"errors"
	"reflect"
	"testing"
)

type schemaCutoverVector struct {
	id      string
	run     func(*testing.T) error
	wantErr error
}

var canonicalSchemaCutoverVectorIDs = []string{
	"canonical_cutover",
	"opaque_snapshot_deep_copy",
	"opaque_snapshot_mutation_isolation",
	"wrong_state_rejects",
	"wrong_cutover_id_rejects",
	"wrong_store_schema_version_rejects",
	"wrong_release_pins_rejects",
	"wrong_trust_bundle_rejects",
	"wrong_accepted_contract_schemas_rejects",
	"wrong_rejected_schema_versions_rejects",
	"wrong_rejected_api_markers_rejects",
	"wrong_forbidden_binary_markers_rejects",
	"protocol_key_reuse_rejects",
	"foreign_cutover_rejects",
	"status_only_returns_retained_status",
	"canonical_json_closure",
	"capability_mutation_valid_bit",
	"capability_mutation_cutover_hash",
	"capability_mutation_cutover_authority_hash",
	"capability_mutation_cutover_seals_hash",
	"capability_mutation_accepted_store_schema_version",
	"capability_mutation_release_pins_hash",
	"capability_mutation_canonical_bytes",
	"frozen_cutover_authority_deterministic",
	"unknown_state_rejects",
	"unknown_action_rejects",
	"cutover_hash_mismatch_rejects",
}

func schemaCutoverVectors() []schemaCutoverVector {
	return []schemaCutoverVector{
		{
			id: "canonical_cutover",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, capability, err := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				if err != nil || capability == nil || !verifiedSchemaCutoverCapabilityIntact(capability) {
					return err
				}
				return nil
			},
		},
		{
			id: "opaque_snapshot_deep_copy",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				if !reflect.DeepEqual(clone, fixture.snapshot) {
					return errors.New("deep copy not equal")
				}
				return nil
			},
		},
		{
			id: "opaque_snapshot_mutation_isolation",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				authority := FrozenSchemaCutoverAuthority()
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.CutoverHash = testHash("mutated")
				if verifiedSchemaCutoverRecordIntact(clone, authority) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "wrong_state_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.State = SchemaCutoverState("invalid_state")
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_cutover_id_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.CutoverID = "wrong_cutover_id"
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_store_schema_version_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.AcceptedStoreSchemaVersion = 15
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_release_pins_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				wrongPins := fixture.pins
				wrongPins.ReleasePinsHash = testHash("wrong-pins")
				_, _, err := EvaluateSchemaCutover(
					fixture.snapshot, wrongPins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_trust_bundle_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				wrongBundle := fixture.bundle
				wrongBundle.TrustBundleHash = testHash("wrong-bundle")
				_, _, err := EvaluateSchemaCutover(
					fixture.snapshot, fixture.pins, wrongBundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_accepted_contract_schemas_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.AcceptedContractSchemas = append([]string(nil), clone.record.AcceptedContractSchemas...)
				clone.record.AcceptedContractSchemas[0] = "ananke.controlled-repair-foreign-schema.v1"
				clone.record.AcceptedContractSchemasHash = canonicalStringArrayHash(clone.record.AcceptedContractSchemas)
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_rejected_schema_versions_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.RejectedSchemaVersions = []int{15, 16, 17}
				clone.record.RejectedSchemaVersionsHash = canonicalIntArrayHash(clone.record.RejectedSchemaVersions)
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_rejected_api_markers_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.RejectedAPIMarkers = append([]string(nil), clone.record.RejectedAPIMarkers...)
				clone.record.RejectedAPIMarkers = append(clone.record.RejectedAPIMarkers, "extra_rejected_api")
				clone.record.RejectedAPIMarkersHash = canonicalStringArrayHash(clone.record.RejectedAPIMarkers)
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "wrong_forbidden_binary_markers_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.ForbiddenBinaryMarkers = append([]string(nil), clone.record.ForbiddenBinaryMarkers...)
				clone.record.ForbiddenBinaryMarkers = append(clone.record.ForbiddenBinaryMarkers, "extra_forbidden_marker")
				clone.record.ForbiddenBinaryMarkersHash = canonicalStringArrayHash(clone.record.ForbiddenBinaryMarkers)
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "protocol_key_reuse_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.AcceptedRepairAttestorLeafSPKI = clone.record.ForbiddenProtocolAdapterSPKIHash
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "foreign_cutover_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.VerifierAuthorityHash = testHash("foreign-authority")
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "status_only_returns_retained_status",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				assessment, capability, err := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverStatusOnly)
				if err != nil || capability != nil {
					return err
				}
				if assessment.Disposition != SchemaCutoverRetainedStatus {
					return errors.New("expected retained status")
				}
				return nil
			},
		},
		{
			id: "canonical_json_closure",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				decoded, err := DecodeSchemaCutoverRecord(fixture.canonical)
				if err != nil || !reflect.DeepEqual(decoded, fixture.record) {
					return errors.New("canonical JSON closure failed")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_valid_bit",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, cap, _ := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				clone := *cap
				clone.valid = false
				if verifiedSchemaCutoverCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_cutover_hash",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, cap, _ := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				clone := *cap
				clone.cutoverHash = testHash("mutated")
				if verifiedSchemaCutoverCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_cutover_authority_hash",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, cap, _ := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				clone := *cap
				clone.cutoverAuthorityHash = testHash("mutated")
				if verifiedSchemaCutoverCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_cutover_seals_hash",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, cap, _ := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				clone := *cap
				clone.cutoverSealsHash = testHash("mutated")
				if verifiedSchemaCutoverCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_accepted_store_schema_version",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, cap, _ := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				clone := *cap
				clone.acceptedStoreSchemaVersion = 99
				if verifiedSchemaCutoverCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_release_pins_hash",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, cap, _ := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				clone := *cap
				clone.releasePinsHash = testHash("mutated")
				if verifiedSchemaCutoverCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_canonical_bytes",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, cap, _ := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
				clone := *cap
				clone.canonical = append([]byte(nil), cap.canonical...)
				clone.canonical[0] ^= 0xFF
				if verifiedSchemaCutoverCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "frozen_cutover_authority_deterministic",
			run: func(t *testing.T) error {
				v1 := FrozenSchemaCutoverAuthority()
				v2, err := deriveFrozenSchemaCutoverAuthority()
				if err != nil || !reflect.DeepEqual(v1, v2) {
					return errors.New("frozen cutover authority not deterministic")
				}
				return nil
			},
		},
		{
			id: "unknown_state_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.State = SchemaCutoverState("unknown_state")
				clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "unknown_action_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				_, _, err := EvaluateSchemaCutover(
					fixture.snapshot, fixture.pins, fixture.bundle, fixture.now,
					SchemaCutoverAction("unknown_action"),
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
		{
			id: "cutover_hash_mismatch_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalSchemaCutoverFixtureForTest(t)
				clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
				clone.record.CutoverHash = testHash("mismatched-hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
				_, _, err := EvaluateSchemaCutover(
					clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
				)
				return err
			},
			wantErr: ErrInvalidSchemaCutover,
		},
	}
}

func TestP6Slice9OrderedVectorRegistry(t *testing.T) {
	vectors := schemaCutoverVectors()
	if len(vectors) != len(canonicalSchemaCutoverVectorIDs) {
		t.Fatalf("vector count mismatch: got=%d want=%d", len(vectors), len(canonicalSchemaCutoverVectorIDs))
	}
	var executedIDs []string
	for i, v := range vectors {
		if v.id != canonicalSchemaCutoverVectorIDs[i] {
			t.Fatalf("vector %d id mismatch: got=%q want=%q", i, v.id, canonicalSchemaCutoverVectorIDs[i])
		}
		err := v.run(t)
		if !errors.Is(err, v.wantErr) && (err != nil || v.wantErr != nil) {
			if err == nil {
				t.Fatalf("vector %q expected error %v, got nil", v.id, v.wantErr)
			}
			if v.wantErr == nil && err != nil {
				t.Fatalf("vector %q unexpected error: %v", v.id, err)
			}
			if !errors.Is(err, v.wantErr) {
				t.Fatalf("vector %q error mismatch: got=%v want=%v", v.id, err, v.wantErr)
			}
		}
		executedIDs = append(executedIDs, v.id)
	}
	if !reflect.DeepEqual(executedIDs, canonicalSchemaCutoverVectorIDs) {
		t.Fatalf("executed vector order mismatch:\n got=%v\nwant=%v", executedIDs, canonicalSchemaCutoverVectorIDs)
	}
}
