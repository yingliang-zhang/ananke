package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type canonicalTestSandboxFixture struct {
	attempt        canonicalSupervisorAttempt
	authority      SupervisorIntentAuthority
	adapterSandbox *VerifiedAdapterSandbox
	uidLease       AdapterUIDLease
	terminalProof  TestTerminalProof
	observation    TestSandboxObservation
	canonical      []byte
	snapshot       *VerifiedTestSandboxSnapshot
	now            time.Time
}

func TestP6Slice6CanonicalTerminalProven(t *testing.T) {
	fixture := canonicalTestSandboxFixtureForTest(t)
	decoded, err := DecodeTestSandboxObservation(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.observation) {
		t.Fatalf("decode canonical observation: decoded=%+v err=%v", decoded, err)
	}

	assessment, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxAdmitTerminal)
	if err != nil {
		t.Fatal(err)
	}
	want := TestSandboxAssessment{
		Disposition:     TestSandboxCapabilityReady,
		NextRequirement: TestSandboxNextAttestation,
	}
	if assessment != want || assessment.EffectAllowed || capability == nil || !verifiedTestSandboxIntact(capability) {
		t.Fatalf("terminal proven assessment=%+v capability=%v", assessment, capability)
	}
	if capability.observationHash != fixture.observation.ObservationHash ||
		capability.snapshotIntegrityHash != fixture.snapshot.integrityHash ||
		capability.claimHash != fixture.attempt.claims[2].ClaimHash ||
		capability.authorizationHash != fixture.attempt.contract.Authorization.AuthorizationHash ||
		capability.predecessorClaimHash != fixture.attempt.claims[1].ClaimHash ||
		capability.adapterCapabilityHash != fixture.adapterSandbox.snapshotIntegrityHash ||
		capability.uidLeaseHash != fixture.uidLease.LeaseHash ||
		capability.terminalProofHash != fixture.terminalProof.ProofHash {
		t.Fatal("verified test sandbox capability omitted exact authority bindings")
	}
}

func TestP6Slice6OpaqueSnapshotDeepCopyAndIntegrity(t *testing.T) {
	fixture := canonicalTestSandboxFixtureForTest(t)
	originalCanonical := append([]byte(nil), fixture.snapshot.canonical...)
	originalObservation := fixture.snapshot.observation

	fixture.canonical[0] ^= 1
	fixture.observation.TerminalProof.CleanupResult = TestCleanupUIDNonemptyRetained
	if !bytes.Equal(fixture.snapshot.canonical, originalCanonical) || !reflect.DeepEqual(fixture.snapshot.observation, originalObservation) {
		t.Fatal("opaque snapshot retained caller-owned observation bytes")
	}
	if _, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxAdmitTerminal); err != nil || capability == nil {
		t.Fatalf("caller mutation reached opaque snapshot: capability=%v err=%v", capability, err)
	}

	mutated := cloneTestSandboxSnapshotForTest(t, fixture.snapshot)
	mutated.canonical[0] ^= 1
	assessment, capability, err := EvaluateTestSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[2],
		fixture.attempt.committedClaim[1], fixture.attempt.terminalEvents[1],
		fixture.adapterSandbox, mutated, TestSandboxAdmitTerminal, fixture.now,
	)
	if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil || assessment.EffectAllowed {
		t.Fatalf("mutated opaque snapshot assessment=%+v capability=%v err=%v", assessment, capability, err)
	}
}

func TestP6Slice6RetainedAndAmbiguousStatesNeverMintCapability(t *testing.T) {
	t.Run("exact retained replay", func(t *testing.T) {
		fixture := canonicalTestSandboxFixtureForTest(t)
		fixture.observation.State = TestSandboxRetainedReplay
		sealTestSandboxObservationForTest(t, &fixture.observation)
		fixture.canonical = canonicalTestArtifact(t, fixture.observation)
		fixture.snapshot = mintTestSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
		assessment, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxStatusOnly)
		if err != nil || capability != nil || assessment.EffectAllowed ||
			assessment.Disposition != TestSandboxRetainedStatus ||
			assessment.NextRequirement != TestSandboxNoFurtherEffect {
			t.Fatalf("retained replay assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	for _, test := range []struct {
		name   string
		reason TestSandboxAmbiguityReason
		mutate func(*canonicalTestSandboxFixture)
	}{
		{name: "uid not empty", reason: TestAmbiguityUIDNotEmpty, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.TerminalProof.UIDEmptyVerified = false
			f.observation.TerminalProof.CleanupResult = TestCleanupUIDNonemptyRetained
		}},
		{name: "roots not scrubbed", reason: TestAmbiguityRootsNotScrubbed, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.TerminalProof.RootsScrubbed = false
			f.observation.TerminalProof.CleanupResult = TestCleanupPartialRetained
		}},
		{name: "stale pid epoch", reason: TestAmbiguityStalePID, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.TerminalProof.BootEpochHash = testHash("stale-boot-epoch")
		}},
		{name: "uid reuse contention", reason: TestAmbiguityUIDReuse, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.UID = 62002
		}},
		{name: "git push attempt", reason: TestAmbiguityGitPush, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("git-push-sandbox")
		}},
		{name: "ref write attempt", reason: TestAmbiguityRefWrite, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("ref-write-sandbox")
		}},
		{name: "network access attempt", reason: TestAmbiguityNetworkAccess, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("network-access-sandbox")
		}},
		{name: "external write attempt", reason: TestAmbiguityExternalWrite, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("external-write-sandbox")
		}},
		{name: "original worktree mutation", reason: TestAmbiguityOriginalWorktreeMutation, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.RootIdentityHash = testHash("original-worktree-mutated")
		}},
		{name: "arbitrary exec attempt", reason: TestAmbiguityArbitraryExec, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("arbitrary-exec-sandbox")
		}},
		{name: "fork escape", reason: TestAmbiguityForkEscape, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.TerminalProof.ProcessGroupIdentityHash = testHash("fork-escape-pgid")
		}},
		{name: "setsid new session", reason: TestAmbiguitySetsidEscape, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.TerminalProof.LeaderIdentityHash = testHash("setsid-leader")
		}},
		{name: "delayed mutation", reason: TestAmbiguityDelayedMutation, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.TerminalProof.SandboxHash = testHash("delayed-mutation-sandbox")
		}},
		{name: "missing module", reason: TestAmbiguityMissingModule, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("missing-module-sandbox")
		}},
		{name: "cache drift", reason: TestAmbiguityCacheDrift, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("cache-drift-sandbox")
		}},
		{name: "toolchain replacement", reason: TestAmbiguityToolchainReplacement, mutate: func(f *canonicalTestSandboxFixture) {
			f.observation.SandboxHash = testHash("toolchain-replacement-sandbox")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := canonicalTestSandboxFixtureForTest(t)
			test.mutate(&fixture)
			fixture.observation.State = TestSandboxRetainForHuman
			fixture.observation.AmbiguityReason = test.reason
			sealTestSandboxObservationForTest(t, &fixture.observation)
			fixture.canonical = canonicalTestArtifact(t, fixture.observation)
			fixture.snapshot = mintTestSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
			assessment, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxStatusOnly)
			if err != nil || capability != nil || assessment.EffectAllowed ||
				assessment.Disposition != TestSandboxWaitingForHuman ||
				assessment.NextRequirement != TestSandboxHumanReviewRequired {
				t.Fatalf("ambiguous state assessment=%+v capability=%v err=%v", assessment, capability, err)
			}
		})
	}
}

func TestP6Slice6FreshnessAndForbiddenActions(t *testing.T) {
	fixture := canonicalTestSandboxFixtureForTest(t)

	t.Run("expired freshness rejects", func(t *testing.T) {
		expired := fixture.now.Add(time.Minute)
		assessment, capability, err := EvaluateTestSandbox(
			fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[2],
			fixture.attempt.committedClaim[1], fixture.attempt.terminalEvents[1],
			fixture.adapterSandbox, fixture.snapshot, TestSandboxAdmitTerminal, expired,
		)
		if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("expired assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("wrong phase rejects", func(t *testing.T) {
		wrongAuthority := fixture.authority
		wrongAuthority.Phase = MaterializationClaimPhase
		wrongAuthority.Sequence = 1
		assessment, capability, err := EvaluateTestSandbox(
			wrongAuthority, fixture.attempt.authorization, fixture.attempt.committedClaim[2],
			fixture.attempt.committedClaim[1], fixture.attempt.terminalEvents[1],
			fixture.adapterSandbox, fixture.snapshot, TestSandboxAdmitTerminal, fixture.now,
		)
		if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("wrong phase assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("status only on terminal proven rejects capability", func(t *testing.T) {
		assessment, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxStatusOnly)
		if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("status only on terminal proven assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("admit on retain for human rejects", func(t *testing.T) {
		retainFixture := canonicalTestSandboxFixtureForTest(t)
		retainFixture.observation.State = TestSandboxRetainForHuman
		retainFixture.observation.AmbiguityReason = TestAmbiguityUIDNotEmpty
		retainFixture.observation.TerminalProof.UIDEmptyVerified = false
		retainFixture.observation.TerminalProof.CleanupResult = TestCleanupUIDNonemptyRetained
		sealTestSandboxObservationForTest(t, &retainFixture.observation)
		retainFixture.canonical = canonicalTestArtifact(t, retainFixture.observation)
		retainFixture.snapshot = mintTestSandboxSnapshotForTest(t, retainFixture.observation, true, true, true, true, true, true, true)
		assessment, capability, err := evaluateCanonicalTestSandbox(t, retainFixture, TestSandboxAdmitTerminal)
		if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("admit on retain assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})
}

func TestP6Slice6ObservationCanonicalJSONClosure(t *testing.T) {
	fixture := canonicalTestSandboxFixtureForTest(t)
	raw, err := canonicalBytes(fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, fixture.canonical) {
		t.Fatal("observation canonical bytes do not round-trip")
	}
	original := fixture.observation.ObservationHash
	fixture.observation.UID = 99999
	if mustRecordHash(t, fixture.observation, "observation_hash") == original {
		t.Fatal("observation hash did not change after UID mutation")
	}
}

func TestP6Slice6CapabilityIntegrityMutationIsolation(t *testing.T) {
	fixture := canonicalTestSandboxFixtureForTest(t)
	_, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxAdmitTerminal)
	if err != nil || capability == nil {
		t.Fatal(err)
	}
	original := *capability
	original.canonical = append([]byte(nil), capability.canonical...)

	mutations := []struct {
		name   string
		mutate func(*VerifiedTestSandbox)
	}{
		{name: "valid bit", mutate: func(v *VerifiedTestSandbox) { v.valid = false }},
		{name: "observation hash", mutate: func(v *VerifiedTestSandbox) { v.observationHash = testHash("mutated") }},
		{name: "verifier authority hash", mutate: func(v *VerifiedTestSandbox) { v.verifierAuthorityHash = testHash("mutated") }},
		{name: "authorization hash", mutate: func(v *VerifiedTestSandbox) { v.authorizationHash = testHash("mutated") }},
		{name: "claim hash", mutate: func(v *VerifiedTestSandbox) { v.claimHash = testHash("mutated") }},
		{name: "adapter capability hash", mutate: func(v *VerifiedTestSandbox) { v.adapterCapabilityHash = testHash("mutated") }},
		{name: "uid lease hash", mutate: func(v *VerifiedTestSandbox) { v.uidLeaseHash = testHash("mutated") }},
		{name: "terminal proof hash", mutate: func(v *VerifiedTestSandbox) { v.terminalProofHash = testHash("mutated") }},
		{name: "canonical bytes", mutate: func(v *VerifiedTestSandbox) { v.canonical[0] ^= 1 }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			copyValue := original
			mutation.mutate(&copyValue)
			if verifiedTestSandboxIntact(&copyValue) {
				t.Fatal("mutation did not break capability integrity")
			}
		})
	}
}

func TestP6Slice6FrozenCompiledValuesAreDeterministic(t *testing.T) {
	manifest, err := deriveGoToolchainManifest()
	if err != nil || !reflect.DeepEqual(manifest, FrozenGoToolchainManifest()) {
		t.Fatal("Go toolchain manifest derivation is not deterministic")
	}
	profile, err := deriveGoTestProfile()
	if err != nil || !reflect.DeepEqual(profile, FrozenGoTestProfile()) {
		t.Fatal("Go test profile derivation is not deterministic")
	}
	verifier, err := deriveTestSandboxVerifierAuthority()
	if err != nil || !reflect.DeepEqual(verifier, FrozenTestSandboxVerifierAuthority()) {
		t.Fatal("verifier authority derivation is not deterministic")
	}
	if profile.Command != testCommand || profile.CGOEnabled != "0" || profile.GOENV != "off" ||
		profile.GOTOOLCHAIN != "local" || profile.GOPROXY != "off" || profile.GOSUMDB != "off" ||
		profile.GOVCS != "*:off" || profile.GOWORK != "off" {
		t.Fatal("Go test profile values drifted")
	}
	if manifest.GoVersion != goToolchainGoVersion || !manifest.RootOwned || !manifest.ReadOnlyModuleCache {
		t.Fatal("Go toolchain manifest values drifted")
	}
}

// --- Test helpers ---

func canonicalTestSandboxFixtureForTest(t *testing.T) canonicalTestSandboxFixture {
	t.Helper()
	adapterFixture := canonicalAdapterSandboxFixtureForTest(t)
	_, adapterSandbox, err := evaluateCanonicalAdapterSandbox(t, adapterFixture, AdapterSandboxAdmitTerminal)
	if err != nil || adapterSandbox == nil {
		t.Fatalf("construct adapter sandbox capability: err=%v adapterSandbox=%v", err, adapterSandbox)
	}
	attempt := adapterFixture.attempt
	authority := attempt.authorities[2]

	pool := FrozenAdapterUIDPool()
	manifest := FrozenGoToolchainManifest()
	profile := FrozenGoTestProfile()
	uidLease := AdapterUIDLease{
		SchemaVersion: AdapterUIDLeaseSchemaVersion,
		LeaseID:       deriveTestUIDLeaseID(authority.AttemptNumber),
		AttemptHash:   authority.AttemptHash,
		AttemptNumber: authority.AttemptNumber,
		UID:           pool.Entries[1].UID,
		GroupID:       pool.GroupID,
		PoolID:        pool.PoolID,
		AcquiredAt:    authority.CreatedAt,
		Exclusive:     true,
	}
	uidLease.LeaseHash = mustHashRecord(uidLease, "lease_hash")

	terminalProof := TestTerminalProof{
		SchemaVersion:               TestTerminalProofSchemaVersion,
		ProofID:                     "attempt_1_test_terminal_proof_001",
		UIDLeaseHash:                uidLease.LeaseHash,
		LeaderIdentityHash:          testHash("test-leader-identity-v1"),
		ProcessGroupIdentityHash:    testHash("test-process-group-identity-v1"),
		UIDEmptyObservationHash:     testHash("test-uid-empty-observation-v1"),
		SandboxHash:                 testHash("test-sandbox-state-hash-v1"),
		RootIdentityHash:            testHash("test-root-identity-hash-v1"),
		DescriptorClosureHash:       testHash("test-descriptor-closure-hash-v1"),
		BootEpochHash:               authority.BootEpochHash,
		CleanupResult:               TestCleanupUIDEmptyRootsScrubbed,
		RootScrubbedAndProvenAbsent: true,
		UIDEmptyVerified:            true,
		DescriptorsClosed:           true,
		RootsScrubbed:               true,
		ObservedAt:                  authority.CreatedAt,
	}
	terminalProof.ProofHash = mustHashRecord(terminalProof, "proof_hash")

	candidateCopy := TestCandidateCopyObservation{
		SchemaVersion:             TestCandidateCopyObservationSchemaVersion,
		NoDotGit:                  true,
		NoRemotes:                 true,
		NoCredentials:             true,
		NoOriginalRepo:            true,
		NoRetainedWorktree:        true,
		NoJournalPaths:            true,
		NoKeyPaths:                true,
		CandidateRootIdentityHash: testHash("test-candidate-root-identity-v1"),
	}
	candidateCopy.CopyHash = mustHashRecord(candidateCopy, "copy_hash")

	observation := TestSandboxObservation{
		SchemaVersion:                TestSandboxObservationSchemaVersion,
		ObservationID:                "attempt_1_test_sandbox_observation_001",
		State:                        TestSandboxTerminalProven,
		AuthorizationHash:            attempt.contract.Authorization.AuthorizationHash,
		ApprovalHash:                 attempt.contract.Authorization.ApprovalHash,
		RequestHash:                  attempt.contract.Dispatch.Request.RequestHash,
		DispatchHash:                 attempt.contract.Dispatch.DispatchHash,
		AttemptHash:                  authority.AttemptHash,
		AttemptNumber:                authority.AttemptNumber,
		AttemptCap:                   authority.AttemptCap,
		ClaimHash:                    attempt.claims[2].ClaimHash,
		PredecessorClaimHash:         attempt.claims[1].ClaimHash,
		PredecessorTerminalEventHash: attempt.terminalEvents[1].terminalEventHash,
		RepositoryBindingHash:        authority.Repository.RepositoryBindingHash,
		RepositoryIdentityHash:       authority.Repository.RepositoryIdentityHash,
		WorktreeSlotID:               adapterFixture.worktree.worktreeSlotID,
		AdapterCapabilityHash:        adapterSandbox.snapshotIntegrityHash,
		UIDPoolHash:                  pool.PoolHash,
		UIDLeaseHash:                 uidLease.LeaseHash,
		UID:                          uidLease.UID,
		GroupID:                      uidLease.GroupID,
		ToolchainManifestID:          manifest.ManifestID,
		ToolchainManifestHash:        manifest.ManifestHash,
		TestProfileID:                profile.ProfileID,
		TestProfileHash:              profile.ProfileHash,
		CandidateCopy:                candidateCopy,
		TerminalProof:                terminalProof,
		RootIdentityHash:             terminalProof.RootIdentityHash,
		SandboxHash:                  terminalProof.SandboxHash,
		DescriptorClosureHash:        terminalProof.DescriptorClosureHash,
		BootEpochID:                  authority.BootEpochID,
		BootEpochHash:                authority.BootEpochHash,
	}
	sealTestSandboxObservationForTest(t, &observation)
	canonical := canonicalTestArtifact(t, observation)
	snapshot := mintTestSandboxSnapshotForTest(t, observation, true, true, true, true, true, true, true)
	return canonicalTestSandboxFixture{
		attempt:        attempt,
		authority:      authority,
		adapterSandbox: adapterSandbox,
		uidLease:       uidLease,
		terminalProof:  terminalProof,
		observation:    observation,
		canonical:      canonical,
		snapshot:       snapshot,
		now:            mustTime(t, authority.CreatedAt),
	}
}

func sealTestSandboxObservationForTest(t *testing.T, value *TestSandboxObservation) {
	t.Helper()
	value.TerminalProof.ProofHash = mustRecordHash(t, value.TerminalProof, "proof_hash")
	value.CandidateCopy.CopyHash = mustRecordHash(t, value.CandidateCopy, "copy_hash")
	value.ObservationHash = mustRecordHash(t, *value, "observation_hash")
}

func mintTestSandboxSnapshotForTest(t *testing.T, observation TestSandboxObservation, toolchainV, profileV, candidateV, sandboxV, terminalV, rootV, uidEmptyV bool) *VerifiedTestSandboxSnapshot {
	t.Helper()
	canonical := canonicalTestArtifact(t, observation)
	verifier := FrozenTestSandboxVerifierAuthority()
	seals := deriveTestSandboxVerificationSeals(verifier, observation, sha256Digest(canonical))
	snapshot := &VerifiedTestSandboxSnapshot{
		valid:                           true,
		toolchainVerified:               toolchainV,
		testProfileVerified:             profileV,
		candidateCopyVerified:           candidateV,
		sandboxBoundaryVerified:         sandboxV,
		terminalProofVerified:           terminalV,
		rootCleanupVerified:             rootV,
		uidEmptyVerified:                uidEmptyV,
		observation:                     observation,
		canonical:                       canonical,
		canonicalHash:                   sha256Digest(canonical),
		verifierAuthorityHash:           verifier.VerifierAuthorityHash,
		toolchainVerificationSeal:       seals.toolchain,
		testProfileVerificationSeal:     seals.testProfile,
		candidateCopyVerificationSeal:   seals.candidateCopy,
		sandboxBoundaryVerificationSeal: seals.sandboxBoundary,
		terminalProofVerificationSeal:   seals.terminalProof,
		rootCleanupVerificationSeal:     seals.rootCleanup,
		uidEmptyVerificationSeal:        seals.uidEmpty,
	}
	snapshot.integrityHash = testSandboxSnapshotIntegrityHash(snapshot)
	return snapshot
}

func cloneTestSandboxSnapshotForTest(t *testing.T, value *VerifiedTestSandboxSnapshot) *VerifiedTestSandboxSnapshot {
	t.Helper()
	clone := *value
	clone.canonical = append([]byte(nil), value.canonical...)
	clone.observation = value.observation
	return &clone
}

func evaluateCanonicalTestSandbox(t *testing.T, fixture canonicalTestSandboxFixture, action TestSandboxAction) (TestSandboxAssessment, *VerifiedTestSandbox, error) {
	t.Helper()
	return EvaluateTestSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[2],
		fixture.attempt.committedClaim[1], fixture.attempt.terminalEvents[1],
		fixture.adapterSandbox, fixture.snapshot, action, fixture.now,
	)
}
