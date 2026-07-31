package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type canonicalAdapterSandboxFixture struct {
	attempt       canonicalSupervisorAttempt
	authority     SupervisorIntentAuthority
	worktree      *VerifiedRepositoryWorktree
	uidLease      AdapterUIDLease
	terminalProof AdapterTerminalProof
	observation   AdapterSandboxObservation
	canonical     []byte
	snapshot      *VerifiedAdapterSandboxSnapshot
	now           time.Time
}

func TestP6Slice5CanonicalTerminalProven(t *testing.T) {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	decoded, err := DecodeAdapterSandboxObservation(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.observation) {
		t.Fatalf("decode canonical observation: decoded=%+v err=%v", decoded, err)
	}

	assessment, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxAdmitTerminal)
	if err != nil {
		t.Fatal(err)
	}
	want := AdapterSandboxAssessment{
		Disposition:     AdapterSandboxCapabilityReady,
		NextRequirement: AdapterSandboxNextTestPhase,
	}
	if assessment != want || assessment.EffectAllowed || capability == nil || !verifiedAdapterSandboxIntact(capability) {
		t.Fatalf("terminal proven assessment=%+v capability=%v", assessment, capability)
	}
	if capability.observationHash != fixture.observation.ObservationHash ||
		capability.snapshotIntegrityHash != fixture.snapshot.integrityHash ||
		capability.claimHash != fixture.attempt.claims[1].ClaimHash ||
		capability.authorizationHash != fixture.attempt.contract.Authorization.AuthorizationHash ||
		capability.predecessorClaimHash != fixture.attempt.claims[0].ClaimHash ||
		capability.worktreeCapabilityHash != fixture.worktree.snapshotIntegrityHash ||
		capability.uidLeaseHash != fixture.uidLease.LeaseHash ||
		capability.terminalProofHash != fixture.terminalProof.ProofHash {
		t.Fatal("verified adapter sandbox capability omitted exact authority bindings")
	}
}

func TestP6Slice5OpaqueSnapshotDeepCopyAndIntegrity(t *testing.T) {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	originalCanonical := append([]byte(nil), fixture.snapshot.canonical...)
	originalObservation := fixture.snapshot.observation

	fixture.canonical[0] ^= 1
	fixture.observation.TerminalProof.CleanupResult = AdapterCleanupUIDNonemptyRetained
	if !bytes.Equal(fixture.snapshot.canonical, originalCanonical) || !reflect.DeepEqual(fixture.snapshot.observation, originalObservation) {
		t.Fatal("opaque snapshot retained caller-owned observation bytes")
	}
	if _, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxAdmitTerminal); err != nil || capability == nil {
		t.Fatalf("caller mutation reached opaque snapshot: capability=%v err=%v", capability, err)
	}

	mutated := cloneAdapterSandboxSnapshotForTest(t, fixture.snapshot)
	mutated.canonical[0] ^= 1
	assessment, capability, err := EvaluateAdapterSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[1],
		fixture.attempt.committedClaim[0], fixture.attempt.terminalEvents[0],
		fixture.worktree, mutated, AdapterSandboxAdmitTerminal, fixture.now,
	)
	if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil || assessment.EffectAllowed {
		t.Fatalf("mutated opaque snapshot assessment=%+v capability=%v err=%v", assessment, capability, err)
	}
}

func TestP6Slice5RetainedAndAmbiguousStatesNeverMintCapability(t *testing.T) {
	t.Run("exact retained replay", func(t *testing.T) {
		fixture := canonicalAdapterSandboxFixtureForTest(t)
		fixture.observation.State = AdapterSandboxRetainedReplay
		sealAdapterSandboxObservationForTest(t, &fixture.observation)
		fixture.canonical = canonicalTestArtifact(t, fixture.observation)
		fixture.snapshot = mintAdapterSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
		assessment, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxStatusOnly)
		if err != nil || capability != nil || assessment.EffectAllowed ||
			assessment.Disposition != AdapterSandboxRetainedStatus ||
			assessment.NextRequirement != AdapterSandboxNoFurtherEffect {
			t.Fatalf("retained replay assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	for _, test := range []struct {
		name   string
		reason AdapterSandboxAmbiguityReason
		mutate func(*canonicalAdapterSandboxFixture)
	}{
		{name: "uid not empty", reason: AdapterAmbiguityUIDNotEmpty, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.UIDEmptyVerified = false
			f.observation.TerminalProof.CleanupResult = AdapterCleanupUIDNonemptyRetained
		}},
		{name: "descriptors not closed", reason: AdapterAmbiguityDescriptorsOpen, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.DescriptorsClosed = false
			f.observation.TerminalProof.CleanupResult = AdapterCleanupPartialRetained
		}},
		{name: "roots not frozen", reason: AdapterAmbiguityRootsNotFrozen, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.RootsFrozen = false
			f.observation.TerminalProof.CleanupResult = AdapterCleanupPartialRetained
		}},
		{name: "stale pid epoch", reason: AdapterAmbiguityStalePID, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.BootEpochHash = testHash("stale-boot-epoch")
		}},
		{name: "child still alive", reason: AdapterAmbiguityChildAlive, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.UIDEmptyVerified = false
		}},
		{name: "broker network escape", reason: AdapterAmbiguityBrokerEscape, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.SandboxHash = testHash("broker-escape-sandbox")
		}},
		{name: "ignored context", reason: AdapterAmbiguityIgnoredContext, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.RootIdentityHash = testHash("ignored-context-root")
		}},
		{name: "double fork escape", reason: AdapterAmbiguityDoubleFork, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.ProcessGroupIdentityHash = testHash("double-fork-pgid")
		}},
		{name: "setsid new session", reason: AdapterAmbiguitySetsidEscape, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.LeaderIdentityHash = testHash("setsid-leader")
		}},
		{name: "closed stdio evade", reason: AdapterAmbiguityClosedStdio, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.DescriptorClosureHash = testHash("closed-stdio-descriptors")
		}},
		{name: "delayed write ref update", reason: AdapterAmbiguityDelayedMutation, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.TerminalProof.SandboxHash = testHash("delayed-mutation-sandbox")
		}},
		{name: "uid reuse contention", reason: AdapterAmbiguityUIDReuse, mutate: func(f *canonicalAdapterSandboxFixture) {
			f.observation.UID = 62002
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := canonicalAdapterSandboxFixtureForTest(t)
			test.mutate(&fixture)
			fixture.observation.State = AdapterSandboxRetainForHuman
			fixture.observation.AmbiguityReason = test.reason
			sealAdapterSandboxObservationForTest(t, &fixture.observation)
			fixture.canonical = canonicalTestArtifact(t, fixture.observation)
			fixture.snapshot = mintAdapterSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
			assessment, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxStatusOnly)
			if err != nil || capability != nil || assessment.EffectAllowed ||
				assessment.Disposition != AdapterSandboxWaitingForHuman ||
				assessment.NextRequirement != AdapterSandboxHumanReviewRequired {
				t.Fatalf("ambiguous state assessment=%+v capability=%v err=%v", assessment, capability, err)
			}
		})
	}
}

func TestP6Slice5FreshnessAndForbiddenActions(t *testing.T) {
	fixture := canonicalAdapterSandboxFixtureForTest(t)

	t.Run("expired freshness rejects", func(t *testing.T) {
		expired := fixture.now.Add(time.Minute)
		assessment, capability, err := EvaluateAdapterSandbox(
			fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[1],
			fixture.attempt.committedClaim[0], fixture.attempt.terminalEvents[0],
			fixture.worktree, fixture.snapshot, AdapterSandboxAdmitTerminal, expired,
		)
		if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("expired assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("wrong phase rejects", func(t *testing.T) {
		wrongAuthority := fixture.authority
		wrongAuthority.Phase = MaterializationClaimPhase
		wrongAuthority.Sequence = 1
		assessment, capability, err := EvaluateAdapterSandbox(
			wrongAuthority, fixture.attempt.authorization, fixture.attempt.committedClaim[1],
			fixture.attempt.committedClaim[0], fixture.attempt.terminalEvents[0],
			fixture.worktree, fixture.snapshot, AdapterSandboxAdmitTerminal, fixture.now,
		)
		if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("wrong phase assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("status only on terminal proven rejects capability", func(t *testing.T) {
		assessment, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxStatusOnly)
		if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("status only on terminal proven assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("admit on retain for human rejects", func(t *testing.T) {
		retainFixture := canonicalAdapterSandboxFixtureForTest(t)
		retainFixture.observation.State = AdapterSandboxRetainForHuman
		retainFixture.observation.AmbiguityReason = AdapterAmbiguityUIDNotEmpty
		retainFixture.observation.TerminalProof.UIDEmptyVerified = false
		retainFixture.observation.TerminalProof.CleanupResult = AdapterCleanupUIDNonemptyRetained
		sealAdapterSandboxObservationForTest(t, &retainFixture.observation)
		retainFixture.canonical = canonicalTestArtifact(t, retainFixture.observation)
		retainFixture.snapshot = mintAdapterSandboxSnapshotForTest(t, retainFixture.observation, true, true, true, true, true, true, true)
		assessment, capability, err := evaluateCanonicalAdapterSandbox(t, retainFixture, AdapterSandboxAdmitTerminal)
		if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("admit on retain assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})
}

func TestP6Slice5ObservationCanonicalJSONClosure(t *testing.T) {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	raw, err := canonicalBytes(fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, fixture.canonical) {
		t.Fatal("observation canonical bytes do not round-trip")
	}
	// Mutating any field must change the observation hash.
	original := fixture.observation.ObservationHash
	fixture.observation.UID = 99999
	newHash := mustRecordHash(t, fixture.observation, "observation_hash")
	if newHash == original {
		t.Fatal("observation hash did not change after UID mutation")
	}
}

func TestP6Slice5CapabilityIntegrityMutationIsolation(t *testing.T) {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	_, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxAdmitTerminal)
	if err != nil || capability == nil {
		t.Fatal(err)
	}
	original := *capability
	original.canonical = append([]byte(nil), capability.canonical...)

	mutations := []struct {
		name   string
		mutate func(*VerifiedAdapterSandbox)
	}{
		{name: "valid bit", mutate: func(v *VerifiedAdapterSandbox) { v.valid = false }},
		{name: "observation hash", mutate: func(v *VerifiedAdapterSandbox) { v.observationHash = testHash("mutated") }},
		{name: "verifier authority hash", mutate: func(v *VerifiedAdapterSandbox) { v.verifierAuthorityHash = testHash("mutated") }},
		{name: "authorization hash", mutate: func(v *VerifiedAdapterSandbox) { v.authorizationHash = testHash("mutated") }},
		{name: "claim hash", mutate: func(v *VerifiedAdapterSandbox) { v.claimHash = testHash("mutated") }},
		{name: "uid lease hash", mutate: func(v *VerifiedAdapterSandbox) { v.uidLeaseHash = testHash("mutated") }},
		{name: "terminal proof hash", mutate: func(v *VerifiedAdapterSandbox) { v.terminalProofHash = testHash("mutated") }},
		{name: "canonical bytes", mutate: func(v *VerifiedAdapterSandbox) { v.canonical[0] ^= 1 }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			copyValue := original
			mutation.mutate(&copyValue)
			if verifiedAdapterSandboxIntact(&copyValue) {
				t.Fatal("mutation did not break capability integrity")
			}
		})
	}
}

func TestP6Slice5FrozenCompiledValuesAreDeterministic(t *testing.T) {
	pool, err := deriveAdapterUIDPool()
	if err != nil || !reflect.DeepEqual(pool, FrozenAdapterUIDPool()) {
		t.Fatal("UID pool derivation is not deterministic")
	}
	profile, err := deriveAdapterSeatbeltProfile()
	if err != nil || !reflect.DeepEqual(profile, FrozenAdapterSeatbeltProfile()) {
		t.Fatal("seatbelt profile derivation is not deterministic")
	}
	verifier, err := deriveAdapterSandboxVerifierAuthority()
	if err != nil || !reflect.DeepEqual(verifier, FrozenAdapterSandboxVerifierAuthority()) {
		t.Fatal("verifier authority derivation is not deterministic")
	}
	if pool.PoolSize != adapterUIDPoolSize || pool.GroupID != adapterUIDPoolGroupID {
		t.Fatalf("UID pool values drifted: size=%d group=%d", pool.PoolSize, pool.GroupID)
	}
	if len(pool.Entries) != adapterUIDPoolSize {
		t.Fatalf("UID pool entry count=%d want=%d", len(pool.Entries), adapterUIDPoolSize)
	}
	for i, entry := range pool.Entries {
		if entry.UID != adapterUIDPoolBaseUID+uint32(i) || entry.GroupID != adapterUIDPoolGroupID {
			t.Fatalf("UID pool entry %d drifted: uid=%d group=%d", i, entry.UID, entry.GroupID)
		}
	}
}

// --- Test helpers ---

func canonicalAdapterSandboxFixtureForTest(t *testing.T) canonicalAdapterSandboxFixture {
	t.Helper()
	worktreeFixture := canonicalRepositoryWorktreeFixtureForTest(t)
	_, worktree, err := evaluateCanonicalRepositoryWorktree(t, worktreeFixture, RepositoryWorktreeAdmitNew)
	if err != nil || worktree == nil {
		t.Fatalf("construct worktree capability: err=%v worktree=%v", err, worktree)
	}
	attempt := worktreeFixture.attempt
	authority := attempt.authorities[1]

	pool := FrozenAdapterUIDPool()
	profile := FrozenAdapterSeatbeltProfile()
	uidLease := AdapterUIDLease{
		SchemaVersion: AdapterUIDLeaseSchemaVersion,
		LeaseID:       deriveAdapterUIDLeaseID(authority.AttemptNumber),
		AttemptHash:   authority.AttemptHash,
		AttemptNumber: authority.AttemptNumber,
		UID:           pool.Entries[0].UID,
		GroupID:       pool.GroupID,
		PoolID:        pool.PoolID,
		AcquiredAt:    authority.CreatedAt,
		Exclusive:     true,
	}
	uidLease.LeaseHash = mustHashRecord(uidLease, "lease_hash")

	terminalProof := AdapterTerminalProof{
		SchemaVersion:            AdapterTerminalProofSchemaVersion,
		ProofID:                  "attempt_1_adapter_terminal_proof_001",
		UIDLeaseHash:             uidLease.LeaseHash,
		LeaderIdentityHash:       testHash("adapter-leader-identity-v1"),
		ProcessGroupIdentityHash: testHash("adapter-process-group-identity-v1"),
		UIDEmptyObservationHash:  testHash("adapter-uid-empty-observation-v1"),
		SandboxHash:              testHash("adapter-sandbox-state-hash-v1"),
		RootIdentityHash:         testHash("adapter-root-identity-hash-v1"),
		DescriptorClosureHash:    testHash("adapter-descriptor-closure-hash-v1"),
		BootEpochHash:            authority.BootEpochHash,
		CleanupResult:            AdapterCleanupUIDEmptyRootsFrozen,
		UIDEmptyVerified:         true,
		DescriptorsClosed:        true,
		RootsFrozen:              true,
		ObservedAt:               authority.CreatedAt,
	}
	terminalProof.ProofHash = mustHashRecord(terminalProof, "proof_hash")

	observation := AdapterSandboxObservation{
		SchemaVersion:                     AdapterSandboxObservationSchemaVersion,
		ObservationID:                     "attempt_1_adapter_sandbox_observation_001",
		State:                             AdapterSandboxTerminalProven,
		AuthorizationHash:                 attempt.contract.Authorization.AuthorizationHash,
		ApprovalHash:                      attempt.contract.Authorization.ApprovalHash,
		RequestHash:                       attempt.contract.Dispatch.Request.RequestHash,
		DispatchHash:                      attempt.contract.Dispatch.DispatchHash,
		AttemptHash:                       authority.AttemptHash,
		AttemptNumber:                     authority.AttemptNumber,
		AttemptCap:                        authority.AttemptCap,
		ClaimHash:                         attempt.claims[1].ClaimHash,
		PredecessorClaimHash:              attempt.claims[0].ClaimHash,
		PredecessorTerminalEventHash:      attempt.terminalEvents[0].terminalEventHash,
		RepositoryBindingHash:             authority.Repository.RepositoryBindingHash,
		RepositoryIdentityHash:            authority.Repository.RepositoryIdentityHash,
		WorktreeSlotID:                    worktree.worktreeSlotID,
		WorktreeSlotPathHash:              worktree.writablePathSetHash,
		WorktreeCapabilityHash:            worktree.snapshotIntegrityHash,
		InstalledWorktreeRootIdentityHash: worktree.candidateRootIdentityHash,
		UIDPoolHash:                       pool.PoolHash,
		UIDLeaseHash:                      uidLease.LeaseHash,
		UID:                               uidLease.UID,
		GroupID:                           uidLease.GroupID,
		SeatbeltProfileID:                 profile.ProfileID,
		SeatbeltProfileHash:               profile.ProfileHash,
		TerminalProof:                     terminalProof,
		RootIdentityHash:                  terminalProof.RootIdentityHash,
		SandboxHash:                       terminalProof.SandboxHash,
		DescriptorClosureHash:             terminalProof.DescriptorClosureHash,
		BootEpochID:                       authority.BootEpochID,
		BootEpochHash:                     authority.BootEpochHash,
	}
	sealAdapterSandboxObservationForTest(t, &observation)
	canonical := canonicalTestArtifact(t, observation)
	snapshot := mintAdapterSandboxSnapshotForTest(t, observation, true, true, true, true, true, true, true)
	return canonicalAdapterSandboxFixture{
		attempt:       attempt,
		authority:     authority,
		worktree:      worktree,
		uidLease:      uidLease,
		terminalProof: terminalProof,
		observation:   observation,
		canonical:     canonical,
		snapshot:      snapshot,
		now:           mustTime(t, authority.CreatedAt),
	}
}

func sealAdapterSandboxObservationForTest(t *testing.T, value *AdapterSandboxObservation) {
	t.Helper()
	value.ObservationHash = mustRecordHash(t, *value, "observation_hash")
	if value.TerminalProof.ProofHash == "" {
		value.TerminalProof.ProofHash = mustRecordHash(t, value.TerminalProof, "proof_hash")
	} else {
		value.TerminalProof.ProofHash = mustRecordHash(t, value.TerminalProof, "proof_hash")
	}
	// Re-seal observation after terminal proof hash may have changed.
	value.ObservationHash = mustRecordHash(t, *value, "observation_hash")
}

func mintAdapterSandboxSnapshotForTest(t *testing.T, observation AdapterSandboxObservation, uidLeaseV, seatbeltV, terminalV, sandboxV, descriptorV, rootV, uidEmptyV bool) *VerifiedAdapterSandboxSnapshot {
	t.Helper()
	canonical := canonicalTestArtifact(t, observation)
	verifier := FrozenAdapterSandboxVerifierAuthority()
	seals := deriveAdapterSandboxVerificationSeals(verifier, observation, sha256Digest(canonical))
	snapshot := &VerifiedAdapterSandboxSnapshot{
		valid:                             true,
		uidLeaseVerified:                  uidLeaseV,
		seatbeltProfileVerified:           seatbeltV,
		terminalProofVerified:             terminalV,
		sandboxBoundaryVerified:           sandboxV,
		descriptorClosureVerified:         descriptorV,
		rootIdentityVerified:              rootV,
		uidEmptyVerified:                  uidEmptyV,
		observation:                       observation,
		canonical:                         canonical,
		canonicalHash:                     sha256Digest(canonical),
		verifierAuthorityHash:             verifier.VerifierAuthorityHash,
		uidLeaseVerificationSeal:          seals.uidLease,
		seatbeltProfileVerificationSeal:   seals.seatbeltProfile,
		terminalProofVerificationSeal:     seals.terminalProof,
		sandboxBoundaryVerificationSeal:   seals.sandboxBoundary,
		descriptorClosureVerificationSeal: seals.descriptorClosure,
		rootIdentityVerificationSeal:      seals.rootIdentity,
		uidEmptyVerificationSeal:          seals.uidEmpty,
	}
	snapshot.integrityHash = adapterSandboxSnapshotIntegrityHash(snapshot)
	return snapshot
}

func cloneAdapterSandboxSnapshotForTest(t *testing.T, value *VerifiedAdapterSandboxSnapshot) *VerifiedAdapterSandboxSnapshot {
	t.Helper()
	clone := *value
	clone.canonical = append([]byte(nil), value.canonical...)
	clone.observation = value.observation
	return &clone
}

func evaluateCanonicalAdapterSandbox(t *testing.T, fixture canonicalAdapterSandboxFixture, action AdapterSandboxAction) (AdapterSandboxAssessment, *VerifiedAdapterSandbox, error) {
	t.Helper()
	return EvaluateAdapterSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[1],
		fixture.attempt.committedClaim[0], fixture.attempt.terminalEvents[0],
		fixture.worktree, fixture.snapshot, action, fixture.now,
	)
}
