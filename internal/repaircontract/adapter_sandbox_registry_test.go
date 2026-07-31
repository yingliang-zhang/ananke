package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type adapterSandboxVector struct {
	id      string
	run     func(*testing.T) error
	wantErr error
}

var canonicalAdapterSandboxVectorIDs = []string{
	"canonical_terminal_proven",
	"opaque_snapshot_deep_copy",
	"opaque_snapshot_mutation_isolation",
	"retained_terminal_replay",
	"uid_not_empty_ambiguous",
	"descriptors_not_closed_ambiguous",
	"roots_not_frozen_ambiguous",
	"stale_pid_epoch_ambiguous",
	"child_still_alive_ambiguous",
	"broker_network_escape_ambiguous",
	"ignored_context_ambiguous",
	"double_fork_escape_ambiguous",
	"setsid_new_session_ambiguous",
	"closed_stdio_evade_ambiguous",
	"delayed_write_ref_update_ambiguous",
	"uid_reuse_contention_ambiguous",
	"expired_freshness_rejects",
	"wrong_phase_rejects",
	"status_only_on_terminal_proven_rejects",
	"admit_on_retain_for_human_rejects",
	"canonical_json_closure",
	"capability_mutation_valid_bit",
	"capability_mutation_observation_hash",
	"capability_mutation_verifier_authority_hash",
	"capability_mutation_authorization_hash",
	"capability_mutation_claim_hash",
	"capability_mutation_uid_lease_hash",
	"capability_mutation_terminal_proof_hash",
	"capability_mutation_canonical_bytes",
	"frozen_uid_pool_deterministic",
	"frozen_seatbelt_profile_deterministic",
	"frozen_verifier_authority_deterministic",
	"uid_not_in_pool_rejects",
	"wrong_pool_hash_rejects",
	"wrong_seatbelt_profile_rejects",
	"unknown_cleanup_result_rejects",
	"unknown_state_rejects",
	"unknown_ambiguity_reason_rejects",
	"terminal_proof_hash_mismatch_rejects",
	"observation_hash_mismatch_rejects",
}

var adapterSandboxVectorRegistry = []adapterSandboxVector{
	{id: "canonical_terminal_proven", run: adapterSandboxCanonicalProbe, wantErr: nil},
	{id: "opaque_snapshot_deep_copy", run: adapterSandboxDeepCopyProbe, wantErr: nil},
	{id: "opaque_snapshot_mutation_isolation", run: adapterSandboxMutationIsolationProbe, wantErr: nil},
	{id: "retained_terminal_replay", run: adapterSandboxRetainedReplayProbe, wantErr: nil},
	{id: "uid_not_empty_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityUIDNotEmpty, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.UIDEmptyVerified = false
		f.observation.TerminalProof.CleanupResult = AdapterCleanupUIDNonemptyRetained
	}), wantErr: nil},
	{id: "descriptors_not_closed_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityDescriptorsOpen, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.DescriptorsClosed = false
		f.observation.TerminalProof.CleanupResult = AdapterCleanupPartialRetained
	}), wantErr: nil},
	{id: "roots_not_frozen_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityRootsNotFrozen, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.RootsFrozen = false
		f.observation.TerminalProof.CleanupResult = AdapterCleanupPartialRetained
	}), wantErr: nil},
	{id: "stale_pid_epoch_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityStalePID, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.BootEpochHash = testHash("stale-boot-epoch")
	}), wantErr: nil},
	{id: "child_still_alive_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityChildAlive, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.UIDEmptyVerified = false
	}), wantErr: nil},
	{id: "broker_network_escape_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityBrokerEscape, func(f *canonicalAdapterSandboxFixture) {
		f.observation.SandboxHash = testHash("broker-escape-sandbox")
	}), wantErr: nil},
	{id: "ignored_context_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityIgnoredContext, func(f *canonicalAdapterSandboxFixture) {
		f.observation.RootIdentityHash = testHash("ignored-context-root")
	}), wantErr: nil},
	{id: "double_fork_escape_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityDoubleFork, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.ProcessGroupIdentityHash = testHash("double-fork-pgid")
	}), wantErr: nil},
	{id: "setsid_new_session_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguitySetsidEscape, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.LeaderIdentityHash = testHash("setsid-leader")
	}), wantErr: nil},
	{id: "closed_stdio_evade_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityClosedStdio, func(f *canonicalAdapterSandboxFixture) {
		f.observation.DescriptorClosureHash = testHash("closed-stdio-descriptors")
	}), wantErr: nil},
	{id: "delayed_write_ref_update_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityDelayedMutation, func(f *canonicalAdapterSandboxFixture) {
		f.observation.TerminalProof.SandboxHash = testHash("delayed-mutation-sandbox")
	}), wantErr: nil},
	{id: "uid_reuse_contention_ambiguous", run: adapterSandboxAmbiguousProbe(AdapterAmbiguityUIDReuse, func(f *canonicalAdapterSandboxFixture) {
		f.observation.UID = 62002
	}), wantErr: nil},
	{id: "expired_freshness_rejects", run: adapterSandboxExpiredFreshnessProbe, wantErr: nil},
	{id: "wrong_phase_rejects", run: adapterSandboxWrongPhaseProbe, wantErr: nil},
	{id: "status_only_on_terminal_proven_rejects", run: adapterSandboxStatusOnlyOnTerminalProvenProbe, wantErr: nil},
	{id: "admit_on_retain_for_human_rejects", run: adapterSandboxAdmitOnRetainProbe, wantErr: nil},
	{id: "canonical_json_closure", run: adapterSandboxCanonicalJSONClosureProbe, wantErr: nil},
	{id: "capability_mutation_valid_bit", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.valid = false }), wantErr: nil},
	{id: "capability_mutation_observation_hash", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.observationHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_verifier_authority_hash", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.verifierAuthorityHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_authorization_hash", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.authorizationHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_claim_hash", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.claimHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_uid_lease_hash", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.uidLeaseHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_terminal_proof_hash", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.terminalProofHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_canonical_bytes", run: adapterSandboxCapabilityMutationProbe(func(v *VerifiedAdapterSandbox) { v.canonical[0] ^= 1 }), wantErr: nil},
	{id: "frozen_uid_pool_deterministic", run: adapterSandboxFrozenPoolProbe, wantErr: nil},
	{id: "frozen_seatbelt_profile_deterministic", run: adapterSandboxFrozenProfileProbe, wantErr: nil},
	{id: "frozen_verifier_authority_deterministic", run: adapterSandboxFrozenVerifierProbe, wantErr: nil},
	{id: "uid_not_in_pool_rejects", run: adapterSandboxUIDNotInPoolProbe, wantErr: nil},
	{id: "wrong_pool_hash_rejects", run: adapterSandboxWrongPoolHashProbe, wantErr: nil},
	{id: "wrong_seatbelt_profile_rejects", run: adapterSandboxWrongProfileProbe, wantErr: nil},
	{id: "unknown_cleanup_result_rejects", run: adapterSandboxUnknownCleanupResultProbe, wantErr: nil},
	{id: "unknown_state_rejects", run: adapterSandboxUnknownStateProbe, wantErr: nil},
	{id: "unknown_ambiguity_reason_rejects", run: adapterSandboxUnknownReasonProbe, wantErr: nil},
	{id: "terminal_proof_hash_mismatch_rejects", run: adapterSandboxProofHashMismatchProbe, wantErr: nil},
	{id: "observation_hash_mismatch_rejects", run: adapterSandboxObservationHashMismatchProbe, wantErr: nil},
}

func TestP6Slice5ExecutableVectorRegistry(t *testing.T) {
	if len(adapterSandboxVectorRegistry) != len(canonicalAdapterSandboxVectorIDs) {
		t.Fatalf("Slice-5 executable registry length=%d canonical inventory length=%d", len(adapterSandboxVectorRegistry), len(canonicalAdapterSandboxVectorIDs))
	}
	seen := make(map[string]struct{}, len(adapterSandboxVectorRegistry))
	executed := make([]string, 0, len(adapterSandboxVectorRegistry))
	for _, vector := range adapterSandboxVectorRegistry {
		vector := vector
		if vector.id == "" || vector.run == nil {
			t.Fatalf("unexecutable Slice-5 vector: %+v", vector)
		}
		if _, duplicate := seen[vector.id]; duplicate {
			t.Fatalf("duplicate Slice-5 vector ID %q", vector.id)
		}
		seen[vector.id] = struct{}{}
		t.Run(vector.id, func(t *testing.T) {
			executed = append(executed, vector.id)
			err := vector.run(t)
			if vector.wantErr == nil {
				if err != nil {
					t.Fatalf("accepted Slice-5 vector returned %v", err)
				}
				return
			}
			if !errors.Is(err, vector.wantErr) {
				t.Fatalf("rejected Slice-5 vector error=%v want=%v", err, vector.wantErr)
			}
		})
	}
	assertExecutedVectorOrder(t, executed, canonicalAdapterSandboxVectorIDs[:])
}

// --- Vector probe implementations ---

func adapterSandboxCanonicalProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxAdmitTerminal)
	if err != nil {
		return err
	}
	if capability == nil || assessment.EffectAllowed || assessment.Disposition != AdapterSandboxCapabilityReady {
		return errors.New("canonical adapter sandbox did not mint status-only next-phase capability")
	}
	return nil
}

func adapterSandboxDeepCopyProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	want := append([]byte(nil), fixture.snapshot.canonical...)
	fixture.canonical[0] ^= 1
	if !bytes.Equal(fixture.snapshot.canonical, want) {
		return errors.New("snapshot aliased caller bytes")
	}
	return nil
}

func adapterSandboxMutationIsolationProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	mutated := cloneAdapterSandboxSnapshotForTest(t, fixture.snapshot)
	mutated.canonical[0] ^= 1
	_, capability, err := EvaluateAdapterSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[1],
		fixture.attempt.committedClaim[0], fixture.attempt.terminalEvents[0],
		fixture.worktree, mutated, AdapterSandboxAdmitTerminal, fixture.now,
	)
	if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil {
		return errors.New("mutated snapshot was accepted")
	}
	return nil
}

func adapterSandboxRetainedReplayProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.State = AdapterSandboxRetainedReplay
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = mintAdapterSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
	assessment, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxStatusOnly)
	if err != nil || capability != nil || assessment.EffectAllowed ||
		assessment.Disposition != AdapterSandboxRetainedStatus {
		return errors.New("retained replay did not return status-only")
	}
	return nil
}

func adapterSandboxAmbiguousProbe(reason AdapterSandboxAmbiguityReason, mutate func(*canonicalAdapterSandboxFixture)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalAdapterSandboxFixtureForTest(t)
		mutate(&fixture)
		fixture.observation.State = AdapterSandboxRetainForHuman
		fixture.observation.AmbiguityReason = reason
		sealAdapterSandboxObservationForTest(t, &fixture.observation)
		fixture.canonical = canonicalTestArtifact(t, fixture.observation)
		fixture.snapshot = mintAdapterSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
		assessment, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxStatusOnly)
		if err != nil || capability != nil || assessment.EffectAllowed ||
			assessment.Disposition != AdapterSandboxWaitingForHuman {
			return errors.New("ambiguous state did not return waiting-for-human")
		}
		return nil
	}
}

func adapterSandboxExpiredFreshnessProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	expired := fixture.now.Add(time.Minute)
	_, capability, err := EvaluateAdapterSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[1],
		fixture.attempt.committedClaim[0], fixture.attempt.terminalEvents[0],
		fixture.worktree, fixture.snapshot, AdapterSandboxAdmitTerminal, expired,
	)
	if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil {
		return errors.New("expired freshness was accepted")
	}
	return nil
}

func adapterSandboxWrongPhaseProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	wrong := fixture.authority
	wrong.Phase = MaterializationClaimPhase
	wrong.Sequence = 1
	_, capability, err := EvaluateAdapterSandbox(
		wrong, fixture.attempt.authorization, fixture.attempt.committedClaim[1],
		fixture.attempt.committedClaim[0], fixture.attempt.terminalEvents[0],
		fixture.worktree, fixture.snapshot, AdapterSandboxAdmitTerminal, fixture.now,
	)
	if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil {
		return errors.New("wrong phase was accepted")
	}
	return nil
}

func adapterSandboxStatusOnlyOnTerminalProvenProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	_, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxStatusOnly)
	if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil {
		return errors.New("status-only on terminal-proven was accepted")
	}
	return nil
}

func adapterSandboxAdmitOnRetainProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.State = AdapterSandboxRetainForHuman
	fixture.observation.AmbiguityReason = AdapterAmbiguityUIDNotEmpty
	fixture.observation.TerminalProof.UIDEmptyVerified = false
	fixture.observation.TerminalProof.CleanupResult = AdapterCleanupUIDNonemptyRetained
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = mintAdapterSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
	_, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxAdmitTerminal)
	if !errors.Is(err, ErrInvalidAdapterSandbox) || capability != nil {
		return errors.New("admit on retain-for-human was accepted")
	}
	return nil
}

func adapterSandboxCanonicalJSONClosureProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	raw, err := canonicalBytes(fixture.observation)
	if err != nil || !bytes.Equal(raw, fixture.canonical) {
		return errors.New("canonical bytes do not round-trip")
	}
	original := fixture.observation.ObservationHash
	fixture.observation.UID = 99999
	if mustRecordHash(t, fixture.observation, "observation_hash") == original {
		return errors.New("observation hash did not change after mutation")
	}
	return nil
}

func adapterSandboxCapabilityMutationProbe(mutate func(*VerifiedAdapterSandbox)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalAdapterSandboxFixtureForTest(t)
		_, capability, err := evaluateCanonicalAdapterSandbox(t, fixture, AdapterSandboxAdmitTerminal)
		if err != nil || capability == nil {
			return errors.New("failed to mint canonical capability")
		}
		copyValue := *capability
		copyValue.canonical = append([]byte(nil), capability.canonical...)
		mutate(&copyValue)
		if verifiedAdapterSandboxIntact(&copyValue) {
			return errors.New("mutation did not break capability integrity")
		}
		return nil
	}
}

func adapterSandboxFrozenPoolProbe(t *testing.T) error {
	pool, err := deriveAdapterUIDPool()
	if err != nil || !reflect.DeepEqual(pool, FrozenAdapterUIDPool()) {
		return errors.New("UID pool derivation is not deterministic")
	}
	if pool.PoolSize != adapterUIDPoolSize || pool.GroupID != adapterUIDPoolGroupID || len(pool.Entries) != adapterUIDPoolSize {
		return errors.New("UID pool values drifted")
	}
	return nil
}

func adapterSandboxFrozenProfileProbe(t *testing.T) error {
	profile, err := deriveAdapterSeatbeltProfile()
	if err != nil || !reflect.DeepEqual(profile, FrozenAdapterSeatbeltProfile()) {
		return errors.New("seatbelt profile derivation is not deterministic")
	}
	return nil
}

func adapterSandboxFrozenVerifierProbe(t *testing.T) error {
	verifier, err := deriveAdapterSandboxVerifierAuthority()
	if err != nil || !reflect.DeepEqual(verifier, FrozenAdapterSandboxVerifierAuthority()) {
		return errors.New("verifier authority derivation is not deterministic")
	}
	for _, kind := range verifier.VerificationKinds {
		if !validAdapterSandboxVerificationKind(kind) {
			return errors.New("verifier authority contains invalid verification kind")
		}
	}
	// Exercise validateAdapterUIDLease with the canonical fixture lease.
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	if err := validateAdapterUIDLease(fixture.uidLease); err != nil {
		return errors.New("canonical fixture UID lease failed validation")
	}
	// Negative: wrong UID should reject.
	wrong := fixture.uidLease
	wrong.UID = 99999
	if err := validateAdapterUIDLease(wrong); !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("wrong UID lease was accepted")
	}
	// Negative: non-exclusive should reject.
	wrongExclusive := fixture.uidLease
	wrongExclusive.Exclusive = false
	if err := validateAdapterUIDLease(wrongExclusive); !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("non-exclusive lease was accepted")
	}
	return nil
}

func adapterSandboxUIDNotInPoolProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.UID = 99999
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("UID not in pool was accepted")
	}
	return nil
}

func adapterSandboxWrongPoolHashProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.UIDPoolHash = testHash("wrong-pool")
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("wrong pool hash was accepted")
	}
	return nil
}

func adapterSandboxWrongProfileProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.SeatbeltProfileID = "wrong_seatbelt_profile"
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("wrong seatbelt profile was accepted")
	}
	return nil
}

func adapterSandboxUnknownCleanupResultProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.TerminalProof.CleanupResult = "unknown_cleanup"
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("unknown cleanup result was accepted")
	}
	return nil
}

func adapterSandboxUnknownStateProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.State = "unknown_state"
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("unknown state was accepted")
	}
	return nil
}

func adapterSandboxUnknownReasonProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.State = AdapterSandboxRetainForHuman
	fixture.observation.AmbiguityReason = "unknown_reason"
	sealAdapterSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("unknown ambiguity reason was accepted")
	}
	return nil
}

func adapterSandboxProofHashMismatchProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.TerminalProof.ProofHash = testHash("wrong-proof-hash")
	// Do NOT re-seal; the observation hash still matches the old valid hash.
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("terminal proof hash mismatch was accepted")
	}
	return nil
}

func adapterSandboxObservationHashMismatchProbe(t *testing.T) error {
	fixture := canonicalAdapterSandboxFixtureForTest(t)
	fixture.observation.ObservationHash = testHash("wrong-observation-hash")
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeAdapterSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidAdapterSandbox) {
		return errors.New("observation hash mismatch was accepted")
	}
	return nil
}
