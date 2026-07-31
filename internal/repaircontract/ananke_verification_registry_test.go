package repaircontract

import (
	"errors"
	"reflect"
	"testing"
)

type anankeVerificationVector struct {
	id      string
	run     func(*testing.T) error
	wantErr error
}

var canonicalAnankeVerificationVectorIDs = []string{
	"canonical_verification",
	"opaque_snapshot_deep_copy",
	"opaque_snapshot_mutation_isolation",
	"wrong_state_rejects",
	"wrong_signer_rejects",
	"wrong_release_pins_rejects",
	"wrong_trust_bundle_rejects",
	"wrong_authorization_hash_rejects",
	"foreign_attestation_rejects",
	"status_only_returns_retained_status",
	"canonical_json_closure",
	"capability_mutation_valid_bit",
	"capability_mutation_verification_hash",
	"capability_mutation_verifier_authority_hash",
	"capability_mutation_attestation_hash",
	"capability_mutation_authorization_hash",
	"capability_mutation_verification_seals_hash",
	"capability_mutation_canonical_bytes",
	"frozen_verifier_authority_deterministic",
	"unknown_state_rejects",
	"unknown_action_rejects",
	"verification_hash_mismatch_rejects",
}

func anankeVerificationVectors() []anankeVerificationVector {
	return []anankeVerificationVector{
		{
			id: "canonical_verification",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, capability, err := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				if err != nil || capability == nil || !verifiedAnankeCapabilityIntact(capability) {
					return err
				}
				return nil
			},
		},
		{
			id: "opaque_snapshot_deep_copy",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
				if !reflect.DeepEqual(clone, fixture.snapshot) {
					return errors.New("deep copy not equal")
				}
				return nil
			},
		},
		{
			id: "opaque_snapshot_mutation_isolation",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				verifier := FrozenAnankeVerifierAuthority()
				clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
				clone.record.VerificationHash = testHash("mutated")
				if verifiedAnankeRecordIntact(clone, verifier) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "wrong_state_rejects",
			run: func(t *testing.T) error {
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
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "wrong_signer_rejects",
			run: func(t *testing.T) error {
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
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "wrong_release_pins_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				wrongPins := fixture.pins
				wrongPins.ReleasePinsHash = testHash("wrong-pins")
				_, _, err := EvaluateAnankeVerification(
					fixture.attestation, fixture.snapshot, wrongPins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
				)
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "wrong_trust_bundle_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				wrongBundle := fixture.bundle
				wrongBundle.TrustBundleHash = testHash("wrong-bundle")
				_, _, err := EvaluateAnankeVerification(
					fixture.attestation, fixture.snapshot, fixture.pins, wrongBundle, fixture.now, AnankeVerificationAdmitReview,
				)
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "wrong_authorization_hash_rejects",
			run: func(t *testing.T) error {
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
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "foreign_attestation_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
				clone.record.AttestationHash = testHash("foreign-attestation")
				clone.record.VerificationHash, _ = hashRecord(clone.record, "verification_hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
				_, _, err := EvaluateAnankeVerification(
					fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
				)
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "status_only_returns_retained_status",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				assessment, capability, err := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationStatusOnly)
				if err != nil || capability != nil {
					return err
				}
				if assessment.Disposition != AnankeVerificationRetainedStatus {
					return errors.New("expected retained status")
				}
				return nil
			},
		},
		{
			id: "canonical_json_closure",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				decoded, err := DecodeAnankeAttestationRecord(fixture.canonical)
				if err != nil || !reflect.DeepEqual(decoded, fixture.record) {
					return errors.New("canonical JSON closure failed")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_valid_bit",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				clone := *cap
				clone.valid = false
				if verifiedAnankeCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_verification_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				clone := *cap
				clone.verificationHash = testHash("mutated")
				if verifiedAnankeCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_verifier_authority_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				clone := *cap
				clone.verifierAuthorityHash = testHash("mutated")
				if verifiedAnankeCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_attestation_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				clone := *cap
				clone.attestationHash = testHash("mutated")
				if verifiedAnankeCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_authorization_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				clone := *cap
				clone.authorizationHash = testHash("mutated")
				if verifiedAnankeCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_verification_seals_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				clone := *cap
				clone.verificationSealsHash = testHash("mutated")
				if verifiedAnankeCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_canonical_bytes",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAnankeVerification(t, fixture, AnankeVerificationAdmitReview)
				clone := *cap
				clone.canonical = append([]byte(nil), cap.canonical...)
				clone.canonical[0] ^= 0xFF
				if verifiedAnankeCapabilityIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "frozen_verifier_authority_deterministic",
			run: func(t *testing.T) error {
				v1 := FrozenAnankeVerifierAuthority()
				v2, err := deriveFrozenAnankeVerifierAuthority()
				if err != nil || !reflect.DeepEqual(v1, v2) {
					return errors.New("frozen verifier authority not deterministic")
				}
				return nil
			},
		},
		{
			id: "unknown_state_rejects",
			run: func(t *testing.T) error {
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
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "unknown_action_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				_, _, err := EvaluateAnankeVerification(
					fixture.attestation, fixture.snapshot, fixture.pins, fixture.bundle, fixture.now,
					AnankeVerificationAction("unknown_action"),
				)
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
		{
			id: "verification_hash_mismatch_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAnankeVerificationFixtureForTest(t)
				clone := cloneAnankeSnapshotForTest(t, fixture.snapshot)
				clone.record.VerificationHash = testHash("mismatched-hash")
				clone.canonical = canonicalTestArtifact(t, clone.record)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = verifiedAnankeRecordIntegrityHash(clone)
				_, _, err := EvaluateAnankeVerification(
					fixture.attestation, clone, fixture.pins, fixture.bundle, fixture.now, AnankeVerificationAdmitReview,
				)
				return err
			},
			wantErr: ErrInvalidAnankeVerification,
		},
	}
}

func TestP6Slice8OrderedVectorRegistry(t *testing.T) {
	vectors := anankeVerificationVectors()
	if len(vectors) != len(canonicalAnankeVerificationVectorIDs) {
		t.Fatalf("vector count mismatch: got=%d want=%d", len(vectors), len(canonicalAnankeVerificationVectorIDs))
	}
	var executedIDs []string
	for i, v := range vectors {
		if v.id != canonicalAnankeVerificationVectorIDs[i] {
			t.Fatalf("vector %d id mismatch: got=%q want=%q", i, v.id, canonicalAnankeVerificationVectorIDs[i])
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
	if !reflect.DeepEqual(executedIDs, canonicalAnankeVerificationVectorIDs) {
		t.Fatalf("executed vector order mismatch:\n got=%v\nwant=%v", executedIDs, canonicalAnankeVerificationVectorIDs)
	}
}
