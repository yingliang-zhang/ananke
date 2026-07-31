package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type canonicalAttestationFixture struct {
	testSandboxFixture  canonicalTestSandboxFixture
	testSandbox         *VerifiedTestSandbox
	authority           SupervisorIntentAuthority
	authorization       *VerifiedAuthorization
	testClaim           *VerifiedSupervisorIntentClaim
	predecessorClaim    *VerifiedSupervisorIntentClaim
	predecessorTerminal *VerifiedSupervisorTerminalEvent
	worktree            *VerifiedRepositoryWorktree
	adapterSandbox      *VerifiedAdapterSandbox
	attestation         RepairReviewAttestation
	canonical           []byte
	snapshot            *VerifiedAttestationSnapshot
	now                 time.Time
}

func canonicalAttestationFixtureForTest(t *testing.T) canonicalAttestationFixture {
	t.Helper()
	adapterFixture := canonicalAdapterSandboxFixtureForTest(t)
	testFixture := canonicalTestSandboxFixtureForTest(t)
	_, testSandbox, err := evaluateCanonicalTestSandbox(t, testFixture, TestSandboxAdmitTerminal)
	if err != nil || testSandbox == nil {
		t.Fatalf("construct test sandbox capability: err=%v testSandbox=%v", err, testSandbox)
	}
	attempt := testFixture.attempt
	authority := testFixture.authority
	pins := FrozenReleasePins()
	bundle := FrozenTrustBundle()

	// Decode predecessor observations for true binding values
	worktreeObs, err := DecodeRepositoryWorktreeObservation(adapterFixture.worktree.canonical)
	if err != nil {
		t.Fatalf("decode worktree observation: %v", err)
	}
	adapterObs, err := DecodeAdapterSandboxObservation(testFixture.adapterSandbox.canonical)
	if err != nil {
		t.Fatalf("decode adapter observation: %v", err)
	}
	testObs := testFixture.observation

	attestation := RepairReviewAttestation{
		SchemaVersion:   AttestationSchemaVersion,
		AttestationID:   "attempt_1_review_attestation_001",
		IssuedAt:        authority.CreatedAt,
		State:           AttestationWaitingForReview,
		SignatureDomain: SignatureDomain,
		Signature:       testHash("attestation-signature-v1"),
		// Trust (Slice 1)
		ReleasePinsHash:               pins.ReleasePinsHash,
		TrustBundleHash:               bundle.TrustBundleHash,
		RepairAttestorCertificateHash: bundle.RepairAttestor.CertificateHash,
		RepairAttestorRootID:          bundle.RepairAttestor.IssuerRootID,
		RepairAttestorLeafSPKI:        bundle.RepairAttestor.SubjectSPKISHA256,
		// Transport
		RequestNonceHash:  testHash("request-nonce-v1"),
		ResponseNonceHash: testHash("response-nonce-v1"),
		ChannelHash:       testHash("channel-v1"),
		// Authorization (Slice 2)
		AuthorizationHash:             attempt.contract.Authorization.AuthorizationHash,
		ApprovalHash:                  attempt.contract.Authorization.ApprovalHash,
		RequestHash:                   attempt.contract.Dispatch.Request.RequestHash,
		DispatchHash:                  attempt.contract.Dispatch.DispatchHash,
		AttemptHash:                   authority.AttemptHash,
		AttemptNumber:                 authority.AttemptNumber,
		AttemptCap:                    authority.AttemptCap,
		EffectTimeValidationTimestamp: authority.CreatedAt,
		// Phase claims (Slice 3)
		MaterializationClaimHash:         attempt.claims[0].ClaimHash,
		AdapterClaimHash:                 attempt.claims[1].ClaimHash,
		TestClaimHash:                    attempt.claims[2].ClaimHash,
		PredecessorClaimHash:             authority.PredecessorClaimHash,
		SupervisorJournalHeadHash:        testHash("supervisor-journal-head-v1"),
		SupervisorJournalPredecessorHash: testHash("supervisor-journal-predecessor-v1"),
		BootEpochID:                      authority.BootEpochID,
		BootEpochHash:                    authority.BootEpochHash,
		// Repository (Slice 4)
		RepositoryBindingHash:             authority.Repository.RepositoryBindingHash,
		RepositoryIdentityHash:            authority.Repository.RepositoryIdentityHash,
		CommonGitIdentityHash:             testHash("common-git-identity-v1"),
		GitExecutableIdentityHash:         testHash("git-executable-identity-v1"),
		WorktreeParentHash:                testHash("worktree-parent-hash-v1"),
		WorktreeTargetHash:                testHash("worktree-target-hash-v1"),
		WorktreeAdminHash:                 testHash("worktree-admin-hash-v1"),
		WorktreeDescriptorHash:            testHash("worktree-descriptor-hash-v1"),
		WorktreeSlotID:                    adapterFixture.worktree.worktreeSlotID,
		WorktreeSlotPathHash:              worktreeObs.WorktreeSlotPathHash,
		InstalledWorktreeRootIdentityHash: worktreeObs.InstalledWorktreeRootIdentityHash,
		// Adapter (Slice 5)
		AdapterSeatbeltProfileHash: adapterObs.SeatbeltProfileHash,
		AdapterSandboxHash:         adapterObs.SandboxHash,
		AdapterTerminalProofHash:   adapterObs.TerminalProof.ProofHash,
		AdapterCapabilityHash:      testFixture.adapterSandbox.snapshotIntegrityHash,
		UIDPoolHash:                adapterObs.UIDPoolHash,
		UIDLeaseHash:               adapterObs.UIDLeaseHash,
		UID:                        adapterObs.UID,
		GroupID:                    adapterObs.GroupID,
		// Patch
		PatchHash:          testHash("patch-hash-v1"),
		PatchSize:          4096,
		OrderedPathsHash:   testHash("ordered-paths-hash-v1"),
		StatusHash:         testHash("status-hash-v1"),
		RawHash:            testHash("raw-hash-v1"),
		NumstatHash:        testHash("numstat-hash-v1"),
		IgnoredHash:        testHash("ignored-hash-v1"),
		FilesystemScanHash: testHash("filesystem-scan-hash-v1"),
		// Tests (Slice 6)
		ToolchainManifestHash: testObs.ToolchainManifestHash,
		TestProfileHash:       testObs.TestProfileHash,
		CandidateCopyHash:     testObs.CandidateCopy.CopyHash,
		TestSandboxHash:       testObs.SandboxHash,
		TestTerminalProofHash: testObs.TerminalProof.ProofHash,
		TestRootCleanupHash:   testHash("test-root-cleanup-hash-v1"),
		TestResultHash:        testHash("test-result-hash-v1"),
		TestOutputHash:        testHash("test-output-hash-v1"),
		TestOutputSize:        8192,
		TestCommandHash:       testHash("test-command-hash-v1"),
		TestCapabilityHash:    testSandbox.snapshotIntegrityHash,
	}
	hash, err := hashAttestationRecord(attestation)
	if err != nil {
		t.Fatalf("hash attestation record: %v", err)
	}
	attestation.AttestationHash = hash

	canonical := canonicalTestArtifact(t, attestation)
	verifier := FrozenAttestationVerifierAuthority()
	seals := deriveAttestationVerificationSeals(verifier, attestation, sha256Digest(canonical))
	snapshot := &VerifiedAttestationSnapshot{
		valid:                         true,
		trustVerified:                 true,
		authorizationVerified:         true,
		phaseClaimVerified:            true,
		repositoryVerified:            true,
		adapterVerified:               true,
		testVerified:                  true,
		integrityVerified:             true,
		attestation:                   attestation,
		canonical:                     canonical,
		canonicalHash:                 sha256Digest(canonical),
		verifierAuthorityHash:         verifier.VerifierAuthorityHash,
		trustVerificationSeal:         seals.trust,
		authorizationVerificationSeal: seals.authorization,
		phaseClaimVerificationSeal:    seals.phaseClaim,
		repositoryVerificationSeal:    seals.repository,
		adapterVerificationSeal:       seals.adapter,
		testVerificationSeal:          seals.test,
		integrityVerificationSeal:     seals.integrity,
	}
	snapshot.integrityHash = attestationSnapshotIntegrityHash(snapshot)

	return canonicalAttestationFixture{
		testSandboxFixture:  testFixture,
		testSandbox:         testSandbox,
		authority:           authority,
		authorization:       attempt.authorization,
		testClaim:           attempt.committedClaim[2],
		predecessorClaim:    attempt.committedClaim[1],
		predecessorTerminal: attempt.terminalEvents[1],
		worktree:            adapterFixture.worktree,
		adapterSandbox:      testFixture.adapterSandbox,
		attestation:         attestation,
		canonical:           canonical,
		snapshot:            snapshot,
		now:                 mustTime(t, authority.CreatedAt),
	}
}

func evaluateCanonicalAttestation(t *testing.T, fixture canonicalAttestationFixture, action AttestationAction) (AttestationAssessment, *VerifiedAttestation, error) {
	t.Helper()
	return EvaluateAttestation(
		fixture.authority, fixture.authorization, fixture.testClaim,
		fixture.predecessorClaim, fixture.predecessorTerminal,
		fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
		fixture.snapshot, action, fixture.now,
	)
}

func cloneAttestationSnapshotForTest(t *testing.T, value *VerifiedAttestationSnapshot) *VerifiedAttestationSnapshot {
	t.Helper()
	clone := *value
	clone.canonical = append([]byte(nil), value.canonical...)
	clone.attestation = value.attestation
	return &clone
}

func TestP6Slice7CanonicalAttestation(t *testing.T) {
	fixture := canonicalAttestationFixtureForTest(t)
	decoded, err := DecodeRepairReviewAttestation(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.attestation) {
		t.Fatalf("decode canonical attestation: decoded=%+v err=%v", decoded, err)
	}
	assessment, capability, err := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
	if err != nil {
		t.Fatal(err)
	}
	want := AttestationAssessment{
		Disposition:     AttestationCapabilityReady,
		NextRequirement: AttestationNextVerification,
	}
	if assessment != want || assessment.EffectAllowed || capability == nil || !verifiedAttestationIntact(capability) {
		t.Fatalf("canonical assessment=%+v capability=%v", assessment, capability)
	}
	if capability.attestationHash != fixture.attestation.AttestationHash ||
		capability.snapshotIntegrityHash != fixture.snapshot.integrityHash ||
		capability.authorizationHash != fixture.attestation.AuthorizationHash ||
		capability.testClaimHash != fixture.attestation.TestClaimHash ||
		capability.adapterCapabilityHash != fixture.attestation.AdapterCapabilityHash ||
		capability.testCapabilityHash != fixture.attestation.TestCapabilityHash ||
		capability.repositoryBindingHash != fixture.attestation.RepositoryBindingHash {
		t.Fatal("verified attestation capability omitted exact authority bindings")
	}
}

func TestP6Slice7OpaqueSnapshotDeepCopy(t *testing.T) {
	fixture := canonicalAttestationFixtureForTest(t)
	clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
	if !reflect.DeepEqual(clone, fixture.snapshot) {
		t.Fatal("deep copy not equal")
	}
	clone.canonical[0] ^= 0xFF
	if bytes.Equal(clone.canonical, fixture.snapshot.canonical) {
		t.Fatal("deep copy shares canonical buffer")
	}
}

func TestP6Slice7OpaqueSnapshotMutationIsolation(t *testing.T) {
	fixture := canonicalAttestationFixtureForTest(t)
	verifier := FrozenAttestationVerifierAuthority()

	mutations := []struct {
		name   string
		mutate func(snapshot *VerifiedAttestationSnapshot)
	}{
		{"attestation_hash", func(s *VerifiedAttestationSnapshot) { s.attestation.AttestationHash = testHash("mutated") }},
		{"verifier_authority_hash", func(s *VerifiedAttestationSnapshot) { s.verifierAuthorityHash = testHash("mutated") }},
		{"authorization_seal", func(s *VerifiedAttestationSnapshot) { s.authorizationVerificationSeal = testHash("mutated") }},
		{"trust_seal", func(s *VerifiedAttestationSnapshot) { s.trustVerificationSeal = testHash("mutated") }},
		{"test_seal", func(s *VerifiedAttestationSnapshot) { s.testVerificationSeal = testHash("mutated") }},
		{"integrity_seal", func(s *VerifiedAttestationSnapshot) { s.integrityVerificationSeal = testHash("mutated") }},
		{"canonical_bytes", func(s *VerifiedAttestationSnapshot) { s.canonical[0] ^= 0xFF }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			clone := cloneAttestationSnapshotForTest(t, fixture.snapshot)
			m.mutate(clone)
			if verifiedAttestationSnapshotIntact(clone, verifier) {
				t.Fatalf("mutation %s did not break snapshot intactness", m.name)
			}
		})
	}
}

func TestP6Slice7WrongStateRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("wrong state should reject, got err=%v", err)
	}
}

func TestP6Slice7WrongSignatureDomainRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("wrong signature domain should reject, got err=%v", err)
	}
}

func TestP6Slice7WrongTrustBundleHashRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("wrong trust bundle hash should reject, got err=%v", err)
	}
}

func TestP6Slice7WrongAuthorizationHashRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("wrong authorization hash should reject, got err=%v", err)
	}
}

func TestP6Slice7WrongRepositoryBindingHashRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("wrong repository binding hash should reject, got err=%v", err)
	}
}

func TestP6Slice7WrongAdapterCapabilityHashRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("wrong adapter capability hash should reject, got err=%v", err)
	}
}

func TestP6Slice7WrongTestCapabilityHashRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("wrong test capability hash should reject, got err=%v", err)
	}
}

func TestP6Slice7StatusOnlyReturnsRetainedStatus(t *testing.T) {
	fixture := canonicalAttestationFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalAttestation(t, fixture, AttestationStatusOnly)
	if err != nil {
		t.Fatal(err)
	}
	want := AttestationAssessment{
		Disposition:     AttestationRetainedStatus,
		NextRequirement: AttestationNoFurtherEffect,
	}
	if assessment != want || capability != nil {
		t.Fatalf("status-only assessment=%+v capability=%v", assessment, capability)
	}
}

func TestP6Slice7CanonicalJSONClosure(t *testing.T) {
	fixture := canonicalAttestationFixtureForTest(t)
	decoded, err := DecodeRepairReviewAttestation(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.attestation) {
		t.Fatalf("canonical JSON closure failed: decoded=%+v err=%v", decoded, err)
	}
}

func TestP6Slice7FrozenVerifierAuthorityDeterministic(t *testing.T) {
	v1 := FrozenAttestationVerifierAuthority()
	v2, err := deriveFrozenAttestationVerifierAuthority()
	if err != nil || !reflect.DeepEqual(v1, v2) {
		t.Fatalf("frozen verifier authority not deterministic: v1=%+v v2=%+v err=%v", v1, v2, err)
	}
}

func TestP6Slice7UnknownStateRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("unknown state should reject, got err=%v", err)
	}
}

func TestP6Slice7UnknownActionRejects(t *testing.T) {
	fixture := canonicalAttestationFixtureForTest(t)
	_, _, err := EvaluateAttestation(
		fixture.authority, fixture.authorization, fixture.testClaim,
		fixture.predecessorClaim, fixture.predecessorTerminal,
		fixture.worktree, fixture.adapterSandbox, fixture.testSandbox,
		fixture.snapshot, AttestationAction("unknown_action"), fixture.now,
	)
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("unknown action should reject, got err=%v", err)
	}
}

func TestP6Slice7AttestationHashMismatchRejects(t *testing.T) {
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
	if !errors.Is(err, ErrInvalidAttestation) {
		t.Fatalf("attestation hash mismatch should reject, got err=%v", err)
	}
}

func TestP6Slice7CapabilityMutationIsolation(t *testing.T) {
	fixture := canonicalAttestationFixtureForTest(t)
	_, capability, err := evaluateCanonicalAttestation(t, fixture, AttestationAdmitForReview)
	if err != nil || capability == nil {
		t.Fatalf("evaluate: err=%v capability=%v", err, capability)
	}

	mutations := []struct {
		name   string
		mutate func(cap *VerifiedAttestation)
	}{
		{"valid_bit", func(c *VerifiedAttestation) { c.valid = false }},
		{"attestation_hash", func(c *VerifiedAttestation) { c.attestationHash = testHash("mutated") }},
		{"verifier_authority_hash", func(c *VerifiedAttestation) { c.verifierAuthorityHash = testHash("mutated") }},
		{"authorization_hash", func(c *VerifiedAttestation) { c.authorizationHash = testHash("mutated") }},
		{"test_claim_hash", func(c *VerifiedAttestation) { c.testClaimHash = testHash("mutated") }},
		{"adapter_capability_hash", func(c *VerifiedAttestation) { c.adapterCapabilityHash = testHash("mutated") }},
		{"test_capability_hash", func(c *VerifiedAttestation) { c.testCapabilityHash = testHash("mutated") }},
		{"canonical_bytes", func(c *VerifiedAttestation) { c.canonical[0] ^= 0xFF }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			clone := *capability
			clone.canonical = append([]byte(nil), capability.canonical...)
			m.mutate(&clone)
			if verifiedAttestationIntact(&clone) {
				t.Fatalf("mutation %s did not break capability intactness", m.name)
			}
		})
	}
}
