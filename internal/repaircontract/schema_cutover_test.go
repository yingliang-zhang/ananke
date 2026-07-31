package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type canonicalSchemaCutoverFixture struct {
	pins      ReleasePins
	bundle    TrustBundle
	authority SchemaCutoverAuthority
	record    SchemaCutoverRecord
	canonical []byte
	snapshot  *VerifiedSchemaCutoverRecord
	now       time.Time
}

func canonicalSchemaCutoverFixtureForTest(t *testing.T) canonicalSchemaCutoverFixture {
	t.Helper()
	pins := FrozenReleasePins()
	bundle := FrozenTrustBundle()
	authority := FrozenSchemaCutoverAuthority()

	record := SchemaCutoverRecord{
		SchemaVersion:                    SchemaCutoverRecordSchemaVersion,
		CutoverID:                        schemaCutoverID,
		State:                            SchemaCutoverCutoverAccepted,
		AcceptedStoreSchemaVersion:       authority.AcceptedStoreSchemaVersion,
		StoreSchemaVersionHash:           storeSchemaVersionHash(authority.AcceptedStoreSchemaVersion),
		AcceptedContractSchemas:          append([]string(nil), authority.AcceptedContractSchemas...),
		AcceptedContractSchemasHash:      canonicalStringArrayHash(authority.AcceptedContractSchemas),
		RejectedSchemaVersions:           append([]int(nil), authority.RejectedSchemaVersions...),
		RejectedSchemaVersionsHash:       canonicalIntArrayHash(authority.RejectedSchemaVersions),
		RejectedAPIMarkers:               append([]string(nil), authority.RejectedAPIMarkers...),
		RejectedAPIMarkersHash:           canonicalStringArrayHash(authority.RejectedAPIMarkers),
		ForbiddenBinaryMarkers:           append([]string(nil), authority.ForbiddenBinaryMarkers...),
		ForbiddenBinaryMarkersHash:       canonicalStringArrayHash(authority.ForbiddenBinaryMarkers),
		ForbiddenProtocolAdapterSPKIHash: authority.ForbiddenProtocolAdapterSPKIHash,
		AcceptedRepairAttestorLeafSPKI:   authority.AcceptedRepairAttestorLeafSPKI,
		ReleasePinsHash:                  pins.ReleasePinsHash,
		VerifierAuthorityHash:            authority.CutoverAuthorityHash,
		VerifiedAt:                       "2026-07-26T12:04:00Z",
	}
	hash, err := hashRecord(record, "cutover_hash")
	if err != nil {
		t.Fatalf("hash schema cutover record: %v", err)
	}
	record.CutoverHash = hash

	canonical := canonicalTestArtifact(t, record)
	seals := deriveSchemaCutoverSeals(authority, record)
	snapshot := &VerifiedSchemaCutoverRecord{
		valid:                         true,
		storeSchemaBindingVerified:    true,
		contractSchemaBindingVerified: true,
		rejectedSchemaForeignVerified: true,
		rejectedAPIAbsenceVerified:    true,
		binaryPurityVerified:          true,
		protocolKeySeparationVerified: true,
		record:                        record,
		canonical:                     canonical,
		canonicalHash:                 sha256Digest(canonical),
		cutoverAuthorityHash:          authority.CutoverAuthorityHash,
		storeSchemaBindingSeal:        seals.storeSchemaBinding,
		contractSchemaBindingSeal:     seals.contractSchemaBinding,
		rejectedSchemaForeignSeal:     seals.rejectedSchemaForeign,
		rejectedAPIAbsenceSeal:        seals.rejectedAPIAbsence,
		binaryPuritySeal:              seals.binaryPurity,
		protocolKeySeparationSeal:     seals.protocolKeySeparation,
	}
	snapshot.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(snapshot)

	return canonicalSchemaCutoverFixture{
		pins:      pins,
		bundle:    bundle,
		authority: authority,
		record:    record,
		canonical: canonical,
		snapshot:  snapshot,
		now:       mustTime(t, "2026-07-26T12:04:00Z"),
	}
}

func evaluateCanonicalSchemaCutover(t *testing.T, fixture canonicalSchemaCutoverFixture, action SchemaCutoverAction) (SchemaCutoverAssessment, *VerifiedSchemaCutoverCapability, error) {
	t.Helper()
	return EvaluateSchemaCutover(
		fixture.snapshot,
		fixture.pins, fixture.bundle, fixture.now, action,
	)
}

func cloneSchemaCutoverSnapshotForTest(t *testing.T, value *VerifiedSchemaCutoverRecord) *VerifiedSchemaCutoverRecord {
	t.Helper()
	clone := *value
	clone.canonical = append([]byte(nil), value.canonical...)
	clone.record = value.record
	return &clone
}

func TestP6Slice9CanonicalCutover(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	decoded, err := DecodeSchemaCutoverRecord(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.record) {
		t.Fatalf("decode canonical record: decoded=%+v err=%v", decoded, err)
	}
	assessment, capability, err := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
	if err != nil {
		t.Fatal(err)
	}
	want := SchemaCutoverAssessment{
		Disposition:     SchemaCutoverRecordReady,
		NextRequirement: SchemaCutoverNextCutover,
	}
	if assessment != want || assessment.EffectAllowed || capability == nil || !verifiedSchemaCutoverCapabilityIntact(capability) {
		t.Fatalf("canonical assessment=%+v capability=%v", assessment, capability)
	}
	if capability.cutoverHash != fixture.record.CutoverHash ||
		capability.acceptedStoreSchemaVersion != fixture.record.AcceptedStoreSchemaVersion ||
		capability.releasePinsHash != fixture.record.ReleasePinsHash {
		t.Fatal("verified schema cutover capability omitted exact authority bindings")
	}
}

func TestP6Slice9OpaqueSnapshotDeepCopy(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
	if !reflect.DeepEqual(clone, fixture.snapshot) {
		t.Fatal("deep copy not equal")
	}
	clone.canonical[0] ^= 0xFF
	if bytes.Equal(clone.canonical, fixture.snapshot.canonical) {
		t.Fatal("deep copy shares canonical buffer")
	}
}

func TestP6Slice9OpaqueSnapshotMutationIsolation(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	authority := FrozenSchemaCutoverAuthority()

	mutations := []struct {
		name   string
		mutate func(snapshot *VerifiedSchemaCutoverRecord)
	}{
		{"cutover_hash", func(s *VerifiedSchemaCutoverRecord) { s.record.CutoverHash = testHash("mutated") }},
		{"cutover_authority_hash", func(s *VerifiedSchemaCutoverRecord) { s.cutoverAuthorityHash = testHash("mutated") }},
		{"store_schema_binding_seal", func(s *VerifiedSchemaCutoverRecord) { s.storeSchemaBindingSeal = testHash("mutated") }},
		{"binary_purity_seal", func(s *VerifiedSchemaCutoverRecord) { s.binaryPuritySeal = testHash("mutated") }},
		{"protocol_key_separation_seal", func(s *VerifiedSchemaCutoverRecord) { s.protocolKeySeparationSeal = testHash("mutated") }},
		{"canonical_bytes", func(s *VerifiedSchemaCutoverRecord) { s.canonical[0] ^= 0xFF }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
			m.mutate(clone)
			if verifiedSchemaCutoverRecordIntact(clone, authority) {
				t.Fatalf("mutation %s did not break snapshot intactness", m.name)
			}
		})
	}
}

func TestP6Slice9WrongStateRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong state should reject, got err=%v", err)
	}
}

func TestP6Slice9WrongStoreSchemaVersionRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong store schema version should reject, got err=%v", err)
	}
}

func TestP6Slice9WrongReleasePinsRejects(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	wrongPins := fixture.pins
	wrongPins.ReleasePinsHash = testHash("wrong-pins")
	_, _, err := EvaluateSchemaCutover(
		fixture.snapshot, wrongPins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
	)
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong release pins should reject, got err=%v", err)
	}
}

func TestP6Slice9WrongTrustBundleRejects(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	wrongBundle := fixture.bundle
	wrongBundle.TrustBundleHash = testHash("wrong-bundle")
	_, _, err := EvaluateSchemaCutover(
		fixture.snapshot, fixture.pins, wrongBundle, fixture.now, SchemaCutoverAdmitCutover,
	)
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong trust bundle should reject, got err=%v", err)
	}
}

func TestP6Slice9WrongAcceptedContractSchemasRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong accepted contract schemas should reject, got err=%v", err)
	}
}

func TestP6Slice9WrongRejectedSchemaVersionsRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong rejected schema versions should reject, got err=%v", err)
	}
}

func TestP6Slice9WrongRejectedAPIMarkersRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong rejected API markers should reject, got err=%v", err)
	}
}

func TestP6Slice9WrongForbiddenBinaryMarkersRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("wrong forbidden binary markers should reject, got err=%v", err)
	}
}

func TestP6Slice9ProtocolKeyReuseRejects(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
	// Set the accepted repair attestor SPKI to the forbidden P5 protocol adapter SPKI
	clone.record.AcceptedRepairAttestorLeafSPKI = clone.record.ForbiddenProtocolAdapterSPKIHash
	clone.record.CutoverHash, _ = hashRecord(clone.record, "cutover_hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
	_, _, err := EvaluateSchemaCutover(
		clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
	)
	// validateSchemaCutoverRecord should reject this because the forbidden
	// and accepted SPKI hashes are equal.
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("protocol key reuse should reject, got err=%v", err)
	}
}

func TestP6Slice9ForeignCutoverRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("foreign cutover authority should reject, got err=%v", err)
	}
}

func TestP6Slice9StatusOnlyReturnsRetainedStatus(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverStatusOnly)
	if err != nil {
		t.Fatal(err)
	}
	want := SchemaCutoverAssessment{
		Disposition:     SchemaCutoverRetainedStatus,
		NextRequirement: SchemaCutoverNoEffect,
	}
	if assessment != want || capability != nil {
		t.Fatalf("status-only assessment=%+v capability=%v", assessment, capability)
	}
}

func TestP6Slice9CanonicalJSONClosure(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	decoded, err := DecodeSchemaCutoverRecord(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.record) {
		t.Fatalf("canonical JSON closure failed: decoded=%+v err=%v", decoded, err)
	}
}

func TestP6Slice9FrozenCutoverAuthorityDeterministic(t *testing.T) {
	v1 := FrozenSchemaCutoverAuthority()
	v2, err := deriveFrozenSchemaCutoverAuthority()
	if err != nil || !reflect.DeepEqual(v1, v2) {
		t.Fatalf("frozen cutover authority not deterministic: v1=%+v v2=%+v err=%v", v1, v2, err)
	}
}

func TestP6Slice9UnknownStateRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("unknown state should reject, got err=%v", err)
	}
}

func TestP6Slice9UnknownActionRejects(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	_, _, err := EvaluateSchemaCutover(
		fixture.snapshot, fixture.pins, fixture.bundle, fixture.now,
		SchemaCutoverAction("unknown_action"),
	)
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("unknown action should reject, got err=%v", err)
	}
}

func TestP6Slice9CutoverHashMismatchRejects(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	clone := cloneSchemaCutoverSnapshotForTest(t, fixture.snapshot)
	clone.record.CutoverHash = testHash("mismatched-hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedSchemaCutoverRecordIntegrityHash(clone)
	_, _, err := EvaluateSchemaCutover(
		clone, fixture.pins, fixture.bundle, fixture.now, SchemaCutoverAdmitCutover,
	)
	if !errors.Is(err, ErrInvalidSchemaCutover) {
		t.Fatalf("cutover hash mismatch should reject, got err=%v", err)
	}
}

func TestP6Slice9CapabilityMutationIsolation(t *testing.T) {
	fixture := canonicalSchemaCutoverFixtureForTest(t)
	_, capability, err := evaluateCanonicalSchemaCutover(t, fixture, SchemaCutoverAdmitCutover)
	if err != nil || capability == nil {
		t.Fatalf("evaluate: err=%v capability=%v", err, capability)
	}

	mutations := []struct {
		name   string
		mutate func(cap *VerifiedSchemaCutoverCapability)
	}{
		{"valid_bit", func(c *VerifiedSchemaCutoverCapability) { c.valid = false }},
		{"cutover_hash", func(c *VerifiedSchemaCutoverCapability) { c.cutoverHash = testHash("mutated") }},
		{"cutover_authority_hash", func(c *VerifiedSchemaCutoverCapability) { c.cutoverAuthorityHash = testHash("mutated") }},
		{"cutover_seals_hash", func(c *VerifiedSchemaCutoverCapability) { c.cutoverSealsHash = testHash("mutated") }},
		{"accepted_store_schema_version", func(c *VerifiedSchemaCutoverCapability) { c.acceptedStoreSchemaVersion = 99 }},
		{"release_pins_hash", func(c *VerifiedSchemaCutoverCapability) { c.releasePinsHash = testHash("mutated") }},
		{"canonical_bytes", func(c *VerifiedSchemaCutoverCapability) { c.canonical[0] ^= 0xFF }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			clone := *capability
			clone.canonical = append([]byte(nil), capability.canonical...)
			m.mutate(&clone)
			if verifiedSchemaCutoverCapabilityIntact(&clone) {
				t.Fatalf("mutation %s did not break capability intactness", m.name)
			}
		})
	}
}
