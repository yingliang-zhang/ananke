package repaircontract

import (
	"errors"
	"reflect"
	"testing"
)

type attestationVector struct {
	id      string
	run     func(*testing.T) error
	wantErr error
}

var canonicalAttestationVectorIDs = []string{
	"canonical_attestation",
	"opaque_snapshot_deep_copy",
	"opaque_snapshot_mutation_isolation",
	"wrong_state_rejects",
	"wrong_signature_domain_rejects",
	"wrong_trust_bundle_hash_rejects",
	"wrong_authorization_hash_rejects",
	"wrong_repository_binding_hash_rejects",
	"wrong_adapter_capability_hash_rejects",
	"wrong_test_capability_hash_rejects",
	"status_only_returns_retained_status",
	"canonical_json_closure",
	"capability_mutation_valid_bit",
	"capability_mutation_attestation_hash",
	"capability_mutation_verifier_authority_hash",
	"capability_mutation_authorization_hash",
	"capability_mutation_test_claim_hash",
	"capability_mutation_adapter_capability_hash",
	"capability_mutation_test_capability_hash",
	"capability_mutation_canonical_bytes",
	"frozen_verifier_authority_deterministic",
	"unknown_state_rejects",
	"unknown_action_rejects",
	"attestation_hash_mismatch_rejects",
}

func attestationVectors() []attestationVector {
	return []attestationVector{
		{
			id: "canonical_attestation",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, capability, err := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				if err != nil || capability == nil || !verifiedAttestationIntact(capability) {
					return err
				}
				return nil
			},
		},
		{
			id: "opaque_snapshot_deep_copy",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				if !reflect.DeepEqual(clone, fixture.snapshot) {
					return errors.New("deep copy not equal")
				}
				return nil
			},
		},
		{
			id: "opaque_snapshot_mutation_isolation",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				verifier := FrozenAttestationVerifierAuthority()
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.AttestationHash = testHash("mutated")
				if verifiedAttestationSnapshotIntact(clone, verifier) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "wrong_state_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.State = AttestationState("invalid_state")
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "wrong_signature_domain_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.SignatureDomain = "wrong.domain"
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "wrong_trust_bundle_hash_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.TrustBundleHash = testHash("mutated-trust-bundle")
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "wrong_authorization_hash_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.AuthorizationHash = testHash("mutated-authorization")
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "wrong_repository_binding_hash_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.RepositoryBindingHash = testHash("mutated-repository-binding")
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "wrong_adapter_capability_hash_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.AdapterCapabilityHash = testHash("mutated-adapter-capability")
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "wrong_test_capability_hash_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.TestCapabilityHash = testHash("mutated-test-capability")
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "status_only_returns_retained_status",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				assessment, capability, err := evaluateCanonicalAttestation(t, fixture, AttestationStatusOnly)
				if err != nil || capability != nil {
					return err
				}
				if assessment.Disposition != AttestationRetainedStatus {
					return errors.New("expected retained status")
				}
				return nil
			},
		},
		{
			id: "canonical_json_closure",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				decoded, err := DecodeRepairReviewAttestation(fixture.canonical)
				if err != nil || !reflect.DeepEqual(decoded, fixture.attestation) {
					return errors.New("canonical JSON closure failed")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_valid_bit",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.valid = false
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_attestation_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.attestationHash = testHash("mutated")
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_verifier_authority_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.verifierAuthorityHash = testHash("mutated")
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_authorization_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.authorizationHash = testHash("mutated")
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_test_claim_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.testClaimHash = testHash("mutated")
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_adapter_capability_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.adapterCapabilityHash = testHash("mutated")
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_test_capability_hash",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.testCapabilityHash = testHash("mutated")
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "capability_mutation_canonical_bytes",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, cap, _ := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
				clone := *cap
				clone.canonical = append([]byte(nil), cap.canonical...)
				clone.canonical[0] ^= 0xFF
				if verifiedAttestationIntact(&clone) {
					return errors.New("mutation did not break intactness")
				}
				return nil
			},
		},
		{
			id: "frozen_verifier_authority_deterministic",
			run: func(t *testing.T) error {
				v1 := FrozenAttestationVerifierAuthority()
				v2, err := deriveFrozenAttestationVerifierAuthority()
				if err != nil || !reflect.DeepEqual(v1, v2) {
					return errors.New("frozen verifier authority not deterministic")
				}
				return nil
			},
		},
		{
			id: "unknown_state_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.State = AttestationState("unknown_state")
				clone.attestation.AttestationHash, _ = hashAttestationRecord(clone.attestation)
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "unknown_action_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					fixture.snapshot, AttestationAction("unknown_action"), fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
		{
			id: "attestation_hash_mismatch_rejects",
			run: func(t *testing.T) error {
				fixture := canonicalAttestationFixtureForTest(t)
				clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
				clone.attestation.AttestationHash = testHash("mismatched-hash")
				clone.canonical = canonicalTestArtifact(t, clone.attestation)
				clone.canonicalHash = sha256Digest(clone.canonical)
				clone.integrityHash = attestationSnapshotIntegrityHash(clone)
				_, _, err := EvaluateAttestation(
					fixture.authority, fixture.authorization, fixture.testClaim,
					fixture.predecessorClaim, fixture.predecessorTerminal,
					fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
					clone, AttestationAdmitForReview, fixture.now,
				)
				return err
			},
			wantErr: ErrInvalidAttestation,
		},
	}
}

func TestP6Slice7OrderedVectorRegistry(t *testing.T) {
	vectors := attestationVectors()
	if len(vectors) != len(canonicalAttestationVectorIDs) {
		t.Fatalf("vector count mismatch: got=%d want=%d", len(vectors), len(canonicalAttestationVectorIDs))
	}
	var executedIDs []string
	for i, v := range vectors {
		if v.id != canonicalAttestationVectorIDs[i] {
			t.Fatalf("vector %d id mismatch: got=%q want=%q", i, v.id, canonicalAttestationVectorIDs[i])
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
	if !reflect.DeepEqual(executedIDs, canonicalAttestationVectorIDs) {
		t.Fatalf("executed vector order mismatch:\n got=%v\nwant=%v", executedIDs, canonicalAttestationVectorIDs)
	}
}
