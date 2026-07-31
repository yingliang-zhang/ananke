package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type canonicalAnankeVerificationFixture struct {
	attestationFixture canonicalAttestationFixture
	attestation        *VerifiedAttestation
	authority          SupervisorIntentAuthority
	authorization      *VerifiedAuthorization
	pins               ReleasePins
	bundle             TrustBundle
	record             AnankeAttestationRecord
	canonical          []byte
	snapshot           *VerifiedAnankeRecord
	now                time.Time
}

func canonicalAnankeVerificationFixtureForTest(t *testing.T) canonicalAnankeVerificationFixture {
	t.Helper()
	attFixture := canonicalAttestationFixtureForTest(t)
	_, attestation, err := evaluateCanonicalAttestation(t, attFixture, AttestationAdmitForReview)
	if err != nil || attestation == nil {
		t.Fatalf("construct attestation capability: err=%v attestation=%v", err, attestation)
	}
	pins := FrozenReleasePins()
	bundle := FrozenTrustBundle()
	decoded, err := DecodeRepairReviewAttestation(attestation.canonical)
	if err != nil {
		t.Fatalf("decode attestation: %v", err)
	}

	record := AnankeAttestationRecord{
		SchemaVersion:                 AnankeVerificationSchemaVersion,
		VerificationID:                "attempt_1_ananke_verification_001",
		AttestationHash:               decoded.AttestationHash,
		VerifiedAt:                    attFixture.now.Format(time.RFC3339Nano),
		State:                         AnankeVerificationWaitingForReview,
		ReleasePinsHash:               pins.ReleasePinsHash,
		VerifierAuthorityHash:         FrozenAnankeVerifierAuthority().VerifierAuthorityHash,
		RepairAttestorCertificateHash: decoded.RepairAttestorCertificateHash,
		RepairAttestorRootID:          decoded.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        decoded.RepairAttestorLeafSPKI,
		AttestationIssuedAt:           decoded.IssuedAt,
		FreshnessCheckedAt:            attFixture.now.Format(time.RFC3339Nano),
		RequestNonceHash:              decoded.RequestNonceHash,
		ResponseNonceHash:             decoded.ResponseNonceHash,
		ChannelHash:                   decoded.ChannelHash,
		AuthorizationHash:             decoded.AuthorizationHash,
		AttemptHash:                   decoded.AttemptHash,
		AttemptNumber:                 decoded.AttemptNumber,
		SupervisorJournalHeadHash:     decoded.SupervisorJournalHeadHash,
	}
	hash, err := hashRecord(record, "verification_hash")
	if err != nil {
		t.Fatalf("hash ananke record: %v", err)
	}
	record.VerificationHash = hash

	canonical := canonicalTestArtifact(t, record)
	verifier := FrozenAnankeVerifierAuthority()
	seals := deriveAnankeVerificationSeals(verifier, record)
	snapshot := &VerifiedAnankeRecord{
		valid:                               true,
		signatureVerified:                   true,
		signerRoleVerified:                  true,
		certificateValidityVerified:         true,
		freshnessVerified:                   true,
		channelVerified:                     true,
		requestBindingVerified:              true,
		headConsistencyVerified:             true,
		record:                              record,
		canonical:                           canonical,
		canonicalHash:                       sha256Digest(canonical),
		verifierAuthorityHash:               verifier.VerifierAuthorityHash,
		signatureVerificationSeal:           seals.signature,
		signerRoleVerificationSeal:          seals.signerRole,
		certificateValidityVerificationSeal: seals.certificateValidity,
		freshnessVerificationSeal:           seals.freshness,
		channelVerificationSeal:             seals.channel,
		requestBindingVerificationSeal:      seals.requestBinding,
		headConsistencyVerificationSeal:     seals.headConsistency,
	}
	snapshot.integrityHash = verifiedAnankeRecordIntegrityHash(snapshot)

	return canonicalAnankeVerificationFixture{
		attestationFixture: attFixture,
		attestation:        attestation,
		authority:          attFixture.authority,
		authorization:      attFixture.authorization,
		pins:               pins,
		bundle:             bundle,
		record:             record,
		canonical:          canonical,
		snapshot:           snapshot,
		now:                attFixture.now,
	}
}

func evaluateCanonicalAnankeVerification(t *testing.T, fixture canonicalAnankeVerificationFixture, action AnankeVerificationAction) (AnankeVerificationAssessment, *VerifiedAnankeCapability, error) {
	t.Helper()
	return EvaluateAnankeVerification(
		fixture.attestation, fixture.snapshot,
		fixture.pins, fixture.bundle, fixture.now, action,
	)
}

func cloneAnankeSnapshotForTest(t *testing.T, value *VerifiedAnankeRecord) *VerifiedAnankeRecord {
	t.Helper()
	clone := *value
	clone.canonical = append([]byte(nil), value.canonical...)
	clone.record = value.record
	return &clone
}

func TestP6Slice8CanonicalVerification(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	decoded, err := DecodeAnankeAttestationRecord(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.record) {
		t.Fatalf("decode canonical record: decoded=%+v err=%v", decoded, err)
	}
	assessment, capability, err := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
	if err != nil {
		t.Fatal(err)
	}
	want := AnankeVerificationAssessment{
		Disposition:     AnankeVerificationRecordReady,
		NextRequirement: AnankeVerificationNextHuman,
	}
	if assessment != want || assessment.EffectAllowed || capability == nil || !verifiedAnankeCapabilityIntact(capability) {
		t.Fatalf("canonical assessment=%+v capability=%v", assessment, capability)
	}
	if capability.verificationHash != fixture.record.VerificationHash ||
		capability.attestationHash != fixture.record.AttestationHash ||
		capability.authorizationHash != fixture.record.AuthorizationHash {
		t.Fatal("verified ananke capability omitted exact authority bindings")
	}
}

func TestP6Slice8OpaqueSnapshotDeepCopy(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
	if !reflect.DeepEqual(clone, fixture.snapshot) {
		t.Fatal("deep copy not equal")
	}
	clone.canonical[0] ^= 0xFF
	if bytes.Equal(clone.canonical, fixture.snapshot.canonical) {
		t.Fatal("deep copy shares canonical buffer")
	}
}

func TestP6Slice8OpaqueSnapshotMutationIsolation(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	verifier := FrozenAnankeVerifierAuthority()

	mutations := []struct {
		name   string
		mutate func(snapshot *VerifiedAnankeRecord)
	}{
		{"verification_hash", func(s *VerifiedAnankeRecord) { s.record.VerificationHash = testHash("mutated") }},
		{"verifier_authority_hash", func(s *VerifiedAnankeRecord) { s.verifierAuthorityHash = testHash("mutated") }},
		{"signature_seal", func(s *VerifiedAnankeRecord) { s.signatureVerificationSeal = testHash("mutated") }},
		{"freshness_seal", func(s *VerifiedAnankeRecord) { s.freshnessVerificationSeal = testHash("mutated") }},
		{"channel_seal", func(s *VerifiedAnankeRecord) { s.channelVerificationSeal = testHash("mutated") }},
		{"canonical_bytes", func(s *VerifiedAnankeRecord) { s.canonical[0] ^= 0xFF }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
			m.mutate(clone)
			if verifiedAnankeRecordIntact(clone, verifier) {
				t.Fatalf("mutation %s did not break snapshot intactness", m.name)
			}
		})
	}
}

func TestP6Slice8WrongStateRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
	clone.record.State = AnankeVerificationState("invalid_state")
	clone.record.VerificationHash, _ = hashRecord(clone.record, "verification_hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("wrong state should reject, got err=%v", err)
	}
}

func TestP6Slice8WrongSignerRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
	clone.record.RepairAttestorCertificateHash = testHash("wrong-signer")
	clone.record.VerificationHash, _ = hashRecord(clone.record, "verification_hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("wrong signer should reject, got err=%v", err)
	}
}

func TestP6Slice8WrongReleasePinsRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	wrongPins := fixture.pins
	wrongPins.ReleasePinsHash = testHash("wrong-pins")
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, fixture.snapshot, wrongPins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("wrong release pins should reject, got err=%v", err)
	}
}

func TestP6Slice8WrongTrustBundleRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	wrongBundle := fixture.bundle
	wrongBundle.TrustBundleHash = testHash("wrong-bundle")
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, fixture.snapshot, fixture.pins, wrongBundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("wrong trust bundle should reject, got err=%v", err)
	}
}

func TestP6Slice8WrongAuthorizationHashRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
	clone.record.AuthorizationHash = testHash("mutated-auth")
	clone.record.VerificationHash, _ = hashRecord(clone.record, "verification_hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("wrong authorization hash should reject, got err=%v", err)
	}
}

func TestP6Slice8ForeignAttestationRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	// Build a foreign attestation with wrong attestation hash
	clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
	clone.record.AttestationHash = testHash("foreign-attestation")
	clone.record.VerificationHash, _ = hashRecord(clone.record, "verification_hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("foreign attestation should reject, got err=%v", err)
	}
}

func TestP6Slice8StatusOnlyReturnsRetainedStatus(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationStatusOnly)
	if err != nil {
		t.Fatal(err)
	}
	want := AnankeVerificationAssessment{
		Disposition:     AnankeVerificationRetainedStatus,
		NextRequirement: AnankeVerificationNoEffect,
	}
	if assessment != want || capability != nil {
		t.Fatalf("status-only assessment=%+v capability=%v", assessment, capability)
	}
}

func TestP6Slice8CanonicalJSONClosure(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	decoded, err := DecodeAnankeAttestationRecord(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.record) {
		t.Fatalf("canonical JSON closure failed: decoded=%+v err=%v", decoded, err)
	}
}

func TestP6Slice8FrozenVerifierAuthorityDeterministic(t *testing.T) {
	v1 := FrozenAnankeVerifierAuthority()
	v2, err := deriveFrozenAnankeVerifierAuthority()
	if err != nil || !reflect.DeepEqual(v1, v2) {
		t.Fatalf("frozen verifier authority not deterministic: v1=%+v v2=%+v err=%v", v1, v2, err)
	}
}

func TestP6Slice8UnknownStateRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
	clone.record.State = AnankeVerificationState("unknown_state")
	clone.record.VerificationHash, _ = hashRecord(clone.record, "verification_hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("unknown state should reject, got err=%v", err)
	}
}

func TestP6Slice8UnknownActionRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, fixture.snapshot, fixture.pins, fixture.bundle, fixture.now,
		AnankeVerificationAction("unknown_action"),
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("unknown action should reject, got err=%v", err)
	}
}

func TestP6Slice8VerificationHashMismatchRejects(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
	clone.record.VerificationHash = testHash("mismatched-hash")
	clone.canonical = canonicalTestArtifact(t, clone.record)
	clone.canonicalHash = sha256Digest(clone.canonical)
	clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
	_, _, err := EvaluateAnankeVerification(
		fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
	)
	if !errors.Is(err, ErrInvalidAnankeVerification) {
		t.Fatalf("verification hash mismatch should reject, got err=%v", err)
	}
}

func TestP6Slice8CapabilityMutationIsolation(t *testing.T) {
	fixture := canonicalAnankeVerificationFixtureForTest(t)
	_, capability, err := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
	if err != nil || capability == nil {
		t.Fatalf("evaluate: err=%v capability=%v", err, capability)
	}

	mutations := []struct {
		name   string
		mutate func(cap *VerifiedAnankeCapability)
	}{
		{"valid_bit", func(c *VerifiedAnankeCapability) { c.valid = false }},
		{"verification_hash", func(c *VerifiedAnankeCapability) { c.verificationHash = testHash("mutated") }},
		{"verifier_authority_hash", func(c *VerifiedAnankeCapability) { c.verifierAuthorityHash = testHash("mutated") }},
		{"attestation_hash", func(c *VerifiedAnankeCapability) { c.attestationHash = testHash("mutated") }},
		{"authorization_hash", func(c *VerifiedAnankeCapability) { c.authorizationHash = testHash("mutated") }},
		{"verification_seals_hash", func(c *VerifiedAnankeCapability) { c.verificationSealsHash = testHash("mutated") }},
		{"canonical_bytes", func(c *VerifiedAnankeCapability) { c.canonical[0] ^= 0xFF }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			clone := *capability
			clone.canonical = append([]byte(nil), capability.canonical...)
			m.mutate(&clone)
			if verifiedAnankeCapabilityIntact(&clone) {
				t.Fatalf("mutation %s did not break capability intactness", m.name)
			}
		})
	}
}
