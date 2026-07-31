package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type testSandboxVector struct {
	id      string
	run     func(*testing.T) error
	wantErr error
}

var canonicalTestSandboxVectorIDs = []string{
	"canonical_terminal_proven",
	"opaque_snapshot_deep_copy",
	"opaque_snapshot_mutation_isolation",
	"retained_terminal_replay",
	"uid_not_empty_ambiguous",
	"roots_not_scrubbed_ambiguous",
	"stale_pid_epoch_ambiguous",
	"uid_reuse_contention_ambiguous",
	"git_push_attempt_ambiguous",
	"ref_write_attempt_ambiguous",
	"network_access_attempt_ambiguous",
	"external_write_attempt_ambiguous",
	"original_worktree_mutation_ambiguous",
	"arbitrary_exec_attempt_ambiguous",
	"fork_escape_ambiguous",
	"setsid_new_session_ambiguous",
	"delayed_write_ref_update_ambiguous",
	"missing_module_ambiguous",
	"cache_drift_ambiguous",
	"toolchain_replacement_ambiguous",
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
	"capability_mutation_adapter_capability_hash",
	"capability_mutation_uid_lease_hash",
	"capability_mutation_terminal_proof_hash",
	"capability_mutation_canonical_bytes",
	"frozen_toolchain_manifest_deterministic",
	"frozen_test_profile_deterministic",
	"frozen_verifier_authority_deterministic",
	"uid_not_in_pool_rejects",
	"wrong_pool_hash_rejects",
	"wrong_toolchain_manifest_rejects",
	"wrong_test_profile_rejects",
	"unknown_cleanup_result_rejects",
	"unknown_state_rejects",
	"unknown_ambiguity_reason_rejects",
	"terminal_proof_hash_mismatch_rejects",
	"observation_hash_mismatch_rejects",
}

var testSandboxVectorRegistry = []testSandboxVector{
	{id: "canonical_terminal_proven", run: testSandboxCanonicalProbe, wantErr: nil},
	{id: "opaque_snapshot_deep_copy", run: testSandboxDeepCopyProbe, wantErr: nil},
	{id: "opaque_snapshot_mutation_isolation", run: testSandboxMutationIsolationProbe, wantErr: nil},
	{id: "retained_terminal_replay", run: testSandboxRetainedReplayProbe, wantErr: nil},
	{id: "uid_not_empty_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityUIDNotEmpty, func(f *canonicalTestSandboxFixture) {
		f.observation.TerminalProof.UIDEmptyVerified = false
		f.observation.TerminalProof.CleanupResult = TestCleanupUIDNonemptyRetained
	}), wantErr: nil},
	{id: "roots_not_scrubbed_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityRootsNotScrubbed, func(f *canonicalTestSandboxFixture) {
		f.observation.TerminalProof.RootsScrubbed = false
		f.observation.TerminalProof.CleanupResult = TestCleanupPartialRetained
	}), wantErr: nil},
	{id: "stale_pid_epoch_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityStalePID, func(f *canonicalTestSandboxFixture) {
		f.observation.TerminalProof.BootEpochHash = testHash("stale-boot-epoch")
	}), wantErr: nil},
	{id: "uid_reuse_contention_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityUIDReuse, func(f *canonicalTestSandboxFixture) {
		f.observation.UID = 62002
	}), wantErr: nil},
	{id: "git_push_attempt_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityGitPush, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("git-push-sandbox")
	}), wantErr: nil},
	{id: "ref_write_attempt_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityRefWrite, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("ref-write-sandbox")
	}), wantErr: nil},
	{id: "network_access_attempt_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityNetworkAccess, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("network-access-sandbox")
	}), wantErr: nil},
	{id: "external_write_attempt_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityExternalWrite, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("external-write-sandbox")
	}), wantErr: nil},
	{id: "original_worktree_mutation_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityOriginalWorktreeMutation, func(f *canonicalTestSandboxFixture) {
		f.observation.RootIdentityHash = testHash("original-worktree-mutated")
	}), wantErr: nil},
	{id: "arbitrary_exec_attempt_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityArbitraryExec, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("arbitrary-exec-sandbox")
	}), wantErr: nil},
	{id: "fork_escape_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityForkEscape, func(f *canonicalTestSandboxFixture) {
		f.observation.TerminalProof.ProcessGroupIdentityHash = testHash("fork-escape-pgid")
	}), wantErr: nil},
	{id: "setsid_new_session_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguitySetsidEscape, func(f *canonicalTestSandboxFixture) {
		f.observation.TerminalProof.LeaderIdentityHash = testHash("setsid-leader")
	}), wantErr: nil},
	{id: "delayed_write_ref_update_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityDelayedMutation, func(f *canonicalTestSandboxFixture) {
		f.observation.TerminalProof.SandboxHash = testHash("delayed-mutation-sandbox")
	}), wantErr: nil},
	{id: "missing_module_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityMissingModule, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("missing-module-sandbox")
	}), wantErr: nil},
	{id: "cache_drift_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityCacheDrift, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("cache-drift-sandbox")
	}), wantErr: nil},
	{id: "toolchain_replacement_ambiguous", run: testSandboxAmbiguousProbe(TestAmbiguityToolchainReplacement, func(f *canonicalTestSandboxFixture) {
		f.observation.SandboxHash = testHash("toolchain-replacement-sandbox")
	}), wantErr: nil},
	{id: "expired_freshness_rejects", run: testSandboxExpiredFreshnessProbe, wantErr: nil},
	{id: "wrong_phase_rejects", run: testSandboxWrongPhaseProbe, wantErr: nil},
	{id: "status_only_on_terminal_proven_rejects", run: testSandboxStatusOnlyOnTerminalProvenProbe, wantErr: nil},
	{id: "admit_on_retain_for_human_rejects", run: testSandboxAdmitOnRetainProbe, wantErr: nil},
	{id: "canonical_json_closure", run: testSandboxCanonicalJSONClosureProbe, wantErr: nil},
	{id: "capability_mutation_valid_bit", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.valid = false }), wantErr: nil},
	{id: "capability_mutation_observation_hash", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.observationHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_verifier_authority_hash", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.verifierAuthorityHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_authorization_hash", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.authorizationHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_claim_hash", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.claimHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_adapter_capability_hash", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.adapterCapabilityHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_uid_lease_hash", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.uidLeaseHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_terminal_proof_hash", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.terminalProofHash = testHash("mutated") }), wantErr: nil},
	{id: "capability_mutation_canonical_bytes", run: testSandboxCapabilityMutationProbe(func(v *VerifiedTestSandbox) { v.canonical[0] ^= 1 }), wantErr: nil},
	{id: "frozen_toolchain_manifest_deterministic", run: testSandboxFrozenManifestProbe, wantErr: nil},
	{id: "frozen_test_profile_deterministic", run: testSandboxFrozenProfileProbe, wantErr: nil},
	{id: "frozen_verifier_authority_deterministic", run: testSandboxFrozenVerifierProbe, wantErr: nil},
	{id: "uid_not_in_pool_rejects", run: testSandboxUIDNotInPoolProbe, wantErr: nil},
	{id: "wrong_pool_hash_rejects", run: testSandboxWrongPoolHashProbe, wantErr: nil},
	{id: "wrong_toolchain_manifest_rejects", run: testSandboxWrongManifestProbe, wantErr: nil},
	{id: "wrong_test_profile_rejects", run: testSandboxWrongProfileProbe, wantErr: nil},
	{id: "unknown_cleanup_result_rejects", run: testSandboxUnknownCleanupResultProbe, wantErr: nil},
	{id: "unknown_state_rejects", run: testSandboxUnknownStateProbe, wantErr: nil},
	{id: "unknown_ambiguity_reason_rejects", run: testSandboxUnknownReasonProbe, wantErr: nil},
	{id: "terminal_proof_hash_mismatch_rejects", run: testSandboxProofHashMismatchProbe, wantErr: nil},
	{id: "observation_hash_mismatch_rejects", run: testSandboxObservationHashMismatchProbe, wantErr: nil},
}

func TestP6Slice6ExecutableVectorRegistry(t *testing.T) {
	if len(testSandboxVectorRegistry) != len(canonicalTestSandboxVectorIDs) {
		t.Fatalf("Slice-6 executable registry length=%d canonical inventory length=%d", len(testSandboxVectorRegistry), len(canonicalTestSandboxVectorIDs))
	}
	seen := make(map[string]struct{}, len(testSandboxVectorRegistry))
	executed := make([]string, 0, len(testSandboxVectorRegistry))
	for _, vector := range testSandboxVectorRegistry {
		vector := vector
		if vector.id == "" || vector.run == nil {
			t.Fatalf("unexecutable Slice-6 vector: %+v", vector)
		}
		if _, duplicate := seen[vector.id]; duplicate {
			t.Fatalf("duplicate Slice-6 vector ID %q", vector.id)
		}
		seen[vector.id] = struct{}{}
		t.Run(vector.id, func(t *testing.T) {
			executed = append(executed, vector.id)
			err := vector.run(t)
			if vector.wantErr == nil {
				if err != nil {
					t.Fatalf("accepted Slice-6 vector returned %v", err)
				}
				return
			}
			if !errors.Is(err, vector.wantErr) {
				t.Fatalf("rejected Slice-6 vector error=%v want=%v", err, vector.wantErr)
			}
		})
	}
	assertExecutedVectorOrder(t, executed, canonicalTestSandboxVectorIDs[:])
}

// --- Vector probe implementations ---

func testSandboxCanonicalProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxAdmitTerminal)
	if err != nil {
		return err
	}
	if capability == nil || assessment.EffectAllowed || assessment.Disposition != TestSandboxCapabilityReady {
		return errors.New("canonical test sandbox did not mint status-only next-phase capability")
	}
	return nil
}

func testSandboxDeepCopyProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	want := append([]byte(nil), fixture.snapshot.canonical...)
	fixture.canonical[0] ^= 1
	if !bytes.Equal(fixture.snapshot.canonical, want) {
		return errors.New("snapshot aliased caller bytes")
	}
	return nil
}

func testSandboxMutationIsolationProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	mutated := cloneTestSandboxSnapshotForTest(t, fixture.snapshot)
	mutated.canonical[0] ^= 1
	_, capability, err := EvaluateTestSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[2],
		fixture.attempt.committedClaim[1], fixture.attempt.terminalEvents[1],
		fixture.adapterSandbox, mutated, TestSandboxAdmitTerminal, fixture.now,
	)
	if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil {
		return errors.New("mutated snapshot was accepted")
	}
	return nil
}

func testSandboxRetainedReplayProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.State = TestSandboxRetainedReplay
	sealTestSandboxObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = mintTestSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
	assessment, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxStatusOnly)
	if err != nil || capability != nil || assessment.EffectAllowed ||
		assessment.Disposition != TestSandboxRetainedStatus {
		return errors.New("retained replay did not return status-only")
	}
	return nil
}

func testSandboxAmbiguousProbe(reason TestSandboxAmbiguityReason, mutate func(*canonicalTestSandboxFixture)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalTestSandboxFixtureForTest(t)
		mutate(&fixture)
		fixture.observation.State = TestSandboxRetainForHuman
		fixture.observation.AmbiguityReason = reason
		sealTestSandboxObservationForTest(t, &fixture.observation)
		fixture.canonical = canonicalTestArtifact(t, fixture.observation)
		fixture.snapshot = mintTestSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
		assessment, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxStatusOnly)
		if err != nil || capability != nil || assessment.EffectAllowed ||
			assessment.Disposition != TestSandboxWaitingForHuman {
			return errors.New("ambiguous state did not return waiting-for-human")
		}
		return nil
	}
}

func testSandboxExpiredFreshnessProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	expired := fixture.now.Add(time.Minute)
	_, capability, err := EvaluateTestSandbox(
		fixture.authority, fixture.attempt.authorization, fixture.attempt.committedClaim[2],
		fixture.attempt.committedClaim[1], fixture.attempt.terminalEvents[1],
		fixture.adapterSandbox, fixture.snapshot, TestSandboxAdmitTerminal, expired,
	)
	if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil {
		return errors.New("expired freshness was accepted")
	}
	return nil
}

func testSandboxWrongPhaseProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	wrong := fixture.authority
	wrong.Phase = MaterializationClaimPhase
	wrong.Sequence = 1
	_, capability, err := EvaluateTestSandbox(
		wrong, fixture.attempt.authorization, fixture.attempt.committedClaim[2],
		fixture.attempt.committedClaim[1], fixture.attempt.terminalEvents[1],
		fixture.adapterSandbox, fixture.snapshot, TestSandboxAdmitTerminal, fixture.now,
	)
	if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil {
		return errors.New("wrong phase was accepted")
	}
	return nil
}

func testSandboxStatusOnlyOnTerminalProvenProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	_, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxStatusOnly)
	if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil {
		return errors.New("status-only on terminal-proven was accepted")
	}
	return nil
}

func testSandboxAdmitOnRetainProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.State = TestSandboxRetainForHuman
	fixture.observation.AmbiguityReason = TestAmbiguityUIDNotEmpty
	fixture.observation.TerminalProof.UIDEmptyVerified = false
	fixture.observation.TerminalProof.CleanupResult = TestCleanupUIDNonemptyRetained
	sealTestSandboxObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = mintTestSandboxSnapshotForTest(t, fixture.observation, true, true, true, true, true, true, true)
	_, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxAdmitTerminal)
	if !errors.Is(err, ErrInvalidTestSandbox) || capability != nil {
		return errors.New("admit on retain-for-human was accepted")
	}
	return nil
}

func testSandboxCanonicalJSONClosureProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
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

func testSandboxCapabilityMutationProbe(mutate func(*VerifiedTestSandbox)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalTestSandboxFixtureForTest(t)
		_, capability, err := evaluateCanonicalTestSandbox(t, fixture, TestSandboxAdmitTerminal)
		if err != nil || capability == nil {
			return errors.New("failed to mint canonical capability")
		}
		copyValue := *capability
		copyValue.canonical = append([]byte(nil), capability.canonical...)
		mutate(&copyValue)
		if verifiedTestSandboxIntact(&copyValue) {
			return errors.New("mutation did not break capability integrity")
		}
		return nil
	}
}

func testSandboxFrozenManifestProbe(t *testing.T) error {
	manifest, err := deriveGoToolchainManifest()
	if err != nil || !reflect.DeepEqual(manifest, FrozenGoToolchainManifest()) {
		return errors.New("toolchain manifest derivation is not deterministic")
	}
	if !manifest.RootOwned || !manifest.ReadOnlyModuleCache || manifest.GoVersion != goToolchainGoVersion {
		return errors.New("toolchain manifest values drifted")
	}
	return nil
}

func testSandboxFrozenProfileProbe(t *testing.T) error {
	profile, err := deriveGoTestProfile()
	if err != nil || !reflect.DeepEqual(profile, FrozenGoTestProfile()) {
		return errors.New("test profile derivation is not deterministic")
	}
	if profile.Command != testCommand || profile.CGOEnabled != "0" || profile.GOENV != "off" ||
		profile.GOTOOLCHAIN != "local" || profile.GOPROXY != "off" || profile.GOSUMDB != "off" ||
		profile.GOVCS != "*:off" || profile.GOWORK != "off" {
		return errors.New("test profile values drifted")
	}
	return nil
}

func testSandboxFrozenVerifierProbe(t *testing.T) error {
	verifier, err := deriveTestSandboxVerifierAuthority()
	if err != nil || !reflect.DeepEqual(verifier, FrozenTestSandboxVerifierAuthority()) {
		return errors.New("verifier authority derivation is not deterministic")
	}
	for _, kind := range verifier.VerificationKinds {
		if !validTestSandboxVerificationKind(kind) {
			return errors.New("verifier authority contains invalid verification kind")
		}
	}
	return nil
}

func testSandboxUIDNotInPoolProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.UID = 99999
	sealTestSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("UID not in pool was accepted")
	}
	return nil
}

func testSandboxWrongPoolHashProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.UIDPoolHash = testHash("wrong-pool")
	sealTestSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("wrong pool hash was accepted")
	}
	return nil
}

func testSandboxWrongManifestProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.ToolchainManifestID = "wrong_toolchain"
	sealTestSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("wrong toolchain manifest was accepted")
	}
	return nil
}

func testSandboxWrongProfileProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.TestProfileID = "wrong_test_profile"
	sealTestSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("wrong test profile was accepted")
	}
	return nil
}

func testSandboxUnknownCleanupResultProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.TerminalProof.CleanupResult = "unknown_cleanup"
	sealTestSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("unknown cleanup result was accepted")
	}
	return nil
}

func testSandboxUnknownStateProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.State = "unknown_state"
	sealTestSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("unknown state was accepted")
	}
	return nil
}

func testSandboxUnknownReasonProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.State = TestSandboxRetainForHuman
	fixture.observation.AmbiguityReason = "unknown_reason"
	sealTestSandboxObservationForTest(t, &fixture.observation)
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("unknown ambiguity reason was accepted")
	}
	return nil
}

func testSandboxProofHashMismatchProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.TerminalProof.ProofHash = testHash("wrong-proof-hash")
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("terminal proof hash mismatch was accepted")
	}
	return nil
}

func testSandboxObservationHashMismatchProbe(t *testing.T) error {
	fixture := canonicalTestSandboxFixtureForTest(t)
	fixture.observation.ObservationHash = testHash("wrong-observation-hash")
	raw := canonicalTestArtifact(t, fixture.observation)
	_, err := DecodeTestSandboxObservation(raw)
	if !errors.Is(err, ErrInvalidTestSandbox) {
		return errors.New("observation hash mismatch was accepted")
	}
	return nil
}
