package repaircontract

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type executableAcceptanceVector struct {
	id      string
	wantErr error
	run     func(*testing.T) error
}

var canonicalAcceptanceVectorIDs = [...]string{
	"duplicate_key",
	"unknown_key_every_nesting_level",
	"trailing_data",
	"invalid_utf8",
	"lone_unicode_surrogate",
	"unicode_noncharacter",
	"malformed_hash",
	"noncanonical_hash",
	"invalid_utc_timestamp",
	"self_consistent_attacker_trust_bundle_root",
	"release_pin_mismatch",
	"protocol_adapter_leaf_reused",
	"wrong_repair_role",
	"wrong_repair_leaf_spki",
	"wrong_repair_root",
	"wrong_repair_bundle",
	"revoked_certificate",
	"stale_certificate",
	"future_certificate",
	"changed_rotation_approver_root_certificate",
	"changed_rotation_approver_root_spki",
	"changed_rotation_approver_leaf_certificate",
	"changed_rotation_approver_leaf_spki",
	"wrong_rotation_approver_role",
	"wrong_rotation_approver_domain",
	"expired_rotation_approver_certificate",
	"future_rotation_approver_certificate",
	"repair_root_reused_for_rotation_approver",
	"repair_leaf_reused_for_rotation_approver",
	"rotation_approval_signer_id_mismatch",
	"rotation_approval_signer_spki_mismatch",
	"tofu_mode",
	"database_replacement_mode",
	"runtime_install_mode",
	"permissive_verifier_mode",
	"synthetic_descriptor_facts",
	"year_old_approval",
	"overlong_lifetime",
	"approval_age_n_minus_1",
	"approval_age_n",
	"approval_age_n_plus_1",
	"lifetime_n_minus_1",
	"lifetime_n",
	"lifetime_n_plus_1",
	"admitted_then_expired_dispatch",
	"admitted_then_expired_effect",
	"swapped_p4_input",
	"swapped_p4_bundle",
	"swapped_p4_admission",
	"foreign_fence_schema",
	"empty_fence_claim_id",
	"invalid_fence_claim_id",
	"zero_fence_generation",
	"negative_fence_generation",
	"empty_repository_identity",
	"invalid_repository_identity",
	"empty_route_id",
	"invalid_route_id",
	"empty_supervisor_profile_id",
	"invalid_supervisor_profile_id",
	"empty_peer_role",
	"invalid_peer_role",
	"swapped_fence_claim",
	"swapped_fence_token",
	"swapped_fence_generation",
	"swapped_repository",
	"swapped_base_commit",
	"swapped_base_tree",
	"swapped_writable_path",
	"swapped_test_profile",
	"swapped_route",
	"swapped_channel",
	"swapped_peer",
	"swapped_policy",
	"duplicate_writable_path",
	"duplicate_test_profile",
	"attempt_zero",
	"attempt_one",
	"attempt_two",
	"attempt_three",
	"attempt_cap_not_two",
	"normal_request_rotation",
	"unmaterialized_rotation_successor",
	"unmaterialized_rotation_cross_signature",
	"unmaterialized_rotation_release_approval",
	"duplicate_dispatch",
	"conflicting_dispatch",
	"restart_exact_replay",
	"restart_conflicting_replay",
	"unknown_secret_field_redacted_diagnostic",
	"fixture_has_no_authority_payload",
}

var executableAcceptanceVectorRegistry = []executableAcceptanceVector{
	{id: "duplicate_key", wantErr: ErrInvalidContract, run: probeDuplicateFixtureKey},
	{id: "unknown_key_every_nesting_level", wantErr: ErrInvalidContract, run: probeUnknownFixtureKeysEverywhere},
	{id: "trailing_data", wantErr: ErrInvalidContract, run: fixtureDecodeBytesProbe(func(raw []byte) []byte { return append(append([]byte(nil), raw...), []byte(`{}`)...) })},
	{id: "invalid_utf8", wantErr: ErrInvalidContract, run: probeInvalidFixtureUTF8},
	{id: "lone_unicode_surrogate", wantErr: ErrInvalidContract, run: fixtureDecodeBytesProbe(func(raw []byte) []byte {
		return bytes.Replace(raw, []byte(ContractFixtureSchemaVersion), []byte(`\ud800`), 1)
	})},
	{id: "unicode_noncharacter", wantErr: ErrInvalidContract, run: fixtureDecodeBytesProbe(func(raw []byte) []byte {
		return bytes.Replace(raw, []byte(ContractFixtureSchemaVersion), []byte(`\ufdd0`), 1)
	})},
	{id: "malformed_hash", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.Dispatch.ChannelBindingHash = "sha256:abc"
		rehashDispatchAndFixture(t, fixture)
	})},
	{id: "noncanonical_hash", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.Dispatch.ChannelBindingHash = "sha256:" + strings.Repeat("A", 64)
		rehashDispatchAndFixture(t, fixture)
	})},
	{id: "invalid_utc_timestamp", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.Dispatch.CreatedAt = "2026-02-30T12:00:00Z"
		rehashDispatchAndFixture(t, fixture)
	})},
	{id: "self_consistent_attacker_trust_bundle_root", wantErr: ErrInvalidContract, run: fixtureMutationProbe(probeRehashedAttackerRootMutation)},
	{id: "release_pin_mismatch", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.ReleasePins.BuildIdentityHash = testHash("other-build")
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "protocol_adapter_leaf_reused", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.TrustBundle.RepairAttestor.SubjectSPKISHA256 = ProtocolAdapterLeafSPKIHash
		fixture.ReleasePins.RepairAttestorLeafSPKI = ProtocolAdapterLeafSPKIHash
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "wrong_repair_role", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.TrustBundle.RepairAttestor.Role = ProtocolAdapterRole
		rehashTrustAndFixture(t, fixture)
	})},
	{id: "wrong_repair_leaf_spki", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		other := testHash("wrong-leaf")
		fixture.TrustBundle.RepairAttestor.SubjectSPKISHA256 = other
		fixture.ReleasePins.RepairAttestorLeafSPKI = other
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "wrong_repair_root", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.TrustBundle.Root.RootID = "other_root"
		fixture.TrustBundle.RepairAttestor.IssuerRootID = "other_root"
		fixture.ReleasePins.RepairAttestorRootID = "other_root"
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "wrong_repair_bundle", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.TrustBundle.BundleID = "other_bundle"
		fixture.TrustBundle.TrustBundleHash = testHash("other-bundle")
		fixture.ReleasePins.TrustBundleHash = fixture.TrustBundle.TrustBundleHash
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "revoked_certificate", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.TrustBundle.RepairAttestor.RevocationState = "revoked"
		rehashTrustAndFixture(t, fixture)
	})},
	{id: "stale_certificate", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.TrustBundle.RepairAttestor.NotAfter = "2026-07-26T12:03:59Z"
		rehashTrustAndFixture(t, fixture)
	})},
	{id: "future_certificate", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.TrustBundle.RepairAttestor.ValidFrom = "2026-07-26T12:04:01Z"
		rehashTrustAndFixture(t, fixture)
	})},
	{id: "changed_rotation_approver_root_certificate", wantErr: ErrInvalidContract, run: probeChangedRotationApproverRootCertificate},
	{id: "changed_rotation_approver_root_spki", wantErr: ErrInvalidContract, run: probeChangedRotationApproverRootSPKI},
	{id: "changed_rotation_approver_leaf_certificate", wantErr: ErrInvalidContract, run: probeChangedRotationApproverLeafCertificate},
	{id: "changed_rotation_approver_leaf_spki", wantErr: ErrInvalidContract, run: probeChangedRotationApproverLeafSPKI},
	{id: "wrong_rotation_approver_role", wantErr: ErrInvalidContract, run: probeWrongRotationApproverRole},
	{id: "wrong_rotation_approver_domain", wantErr: ErrInvalidContract, run: probeWrongRotationApproverDomain},
	{id: "expired_rotation_approver_certificate", wantErr: ErrInvalidContract, run: probeExpiredRotationApproverCertificate},
	{id: "future_rotation_approver_certificate", wantErr: ErrInvalidContract, run: probeFutureRotationApproverCertificate},
	{id: "repair_root_reused_for_rotation_approver", wantErr: ErrInvalidContract, run: probeRepairRootReusedForRotationApprover},
	{id: "repair_leaf_reused_for_rotation_approver", wantErr: ErrInvalidContract, run: probeRepairLeafReusedForRotationApprover},
	{id: "rotation_approval_signer_id_mismatch", wantErr: ErrInvalidContract, run: probeRotationApprovalSignerIDMismatch},
	{id: "rotation_approval_signer_spki_mismatch", wantErr: ErrInvalidContract, run: probeRotationApprovalSignerSPKIMismatch},
	{id: "tofu_mode", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.ReleasePins.TrustBootstrapMode = "trust_on_first_use"
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "database_replacement_mode", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.ReleasePins.DatabaseTrustMode = "database_replaceable"
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "runtime_install_mode", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.ReleasePins.RuntimeInstallMode = "runtime_install_allowed"
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "permissive_verifier_mode", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.ReleasePins.VerifierSelection = "caller_injected_permissive"
		rehashPinsAndFixture(t, fixture)
	})},
	{id: "synthetic_descriptor_facts", wantErr: ErrInvalidContract, run: probeSyntheticDescriptorFacts},
	{id: "year_old_approval", wantErr: ErrInvalidContract, run: approvalTimingProbe(365*24*time.Hour, MaxAuthorizationLifetime)},
	{id: "overlong_lifetime", wantErr: ErrInvalidContract, run: approvalTimingProbe(time.Minute, MaxAuthorizationLifetime+time.Nanosecond)},
	{id: "approval_age_n_minus_1", run: approvalTimingProbe(MaxApprovalAge-time.Nanosecond, MaxAuthorizationLifetime)},
	{id: "approval_age_n", run: approvalTimingProbe(MaxApprovalAge, MaxAuthorizationLifetime)},
	{id: "approval_age_n_plus_1", wantErr: ErrInvalidContract, run: approvalTimingProbe(MaxApprovalAge+time.Nanosecond, MaxAuthorizationLifetime)},
	{id: "lifetime_n_minus_1", run: approvalTimingProbe(time.Minute, MaxAuthorizationLifetime-time.Nanosecond)},
	{id: "lifetime_n", run: approvalTimingProbe(time.Minute, MaxAuthorizationLifetime)},
	{id: "lifetime_n_plus_1", wantErr: ErrInvalidContract, run: approvalTimingProbe(time.Minute, MaxAuthorizationLifetime+time.Nanosecond)},
	{id: "admitted_then_expired_dispatch", wantErr: ErrInvalidContract, run: probeDispatchCreatedAtAuthorizationExpiry},
	{id: "admitted_then_expired_effect", wantErr: ErrInvalidContract, run: probeExpiredDispatchEffect},
	{id: "swapped_p4_input", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.P4InputHash = testHash("swapped-p4-input")
	}, false)},
	{id: "swapped_p4_bundle", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.P4EvidenceBundleHash = testHash("swapped-p4-bundle")
	}, false)},
	{id: "swapped_p4_admission", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.P4AdmissionHash = testHash("swapped-p4-admission")
	}, false)},
	{id: "foreign_fence_schema", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.FullFence.SchemaVersion = "attacker.foreign-fence.v9"
	})},
	{id: "empty_fence_claim_id", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.FullFence.ClaimID = ""
	})},
	{id: "invalid_fence_claim_id", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityValuesProbe(
		[]string{"invalid claim", "invalid\tclaim", "invalid-claim", strings.Repeat("a", maxClaimIDBytes+1)},
		func(fixture *ContractFixture, value string) { fixture.Authorization.Scope.P4.FullFence.ClaimID = value },
	)},
	{id: "zero_fence_generation", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.FullFence.FenceGeneration = 0
	})},
	{id: "negative_fence_generation", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.FullFence.FenceGeneration = -1
	})},
	{id: "empty_repository_identity", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.Repository.RepositoryIdentity = ""
	})},
	{id: "invalid_repository_identity", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityValuesProbe(
		[]string{"example.invalid/invalid repository", "example.invalid/control\nrepository", "example.invalid//repository", "example.invalid/" + strings.Repeat("a", maxRepositoryIdentityBytes)},
		func(fixture *ContractFixture, value string) {
			fixture.Authorization.Scope.Repository.RepositoryIdentity = value
		},
	)},
	{id: "empty_route_id", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.Route.RouteID = ""
	})},
	{id: "invalid_route_id", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityValuesProbe(
		[]string{"invalid route", "invalid\troute", "invalid-route", strings.Repeat("a", maxRouteIDBytes+1)},
		func(fixture *ContractFixture, value string) { fixture.Authorization.Scope.Route.RouteID = value },
	)},
	{id: "empty_supervisor_profile_id", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.Route.SupervisorProfileID = ""
	})},
	{id: "invalid_supervisor_profile_id", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityValuesProbe(
		[]string{"invalid profile", "invalid\tprofile", "invalid-profile", strings.Repeat("a", maxSupervisorProfileIDBytes+1)},
		func(fixture *ContractFixture, value string) {
			fixture.Authorization.Scope.Route.SupervisorProfileID = value
		},
	)},
	{id: "empty_peer_role", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.ExpectedPeer.PeerRole = ""
	})},
	{id: "invalid_peer_role", wantErr: ErrInvalidContract, run: invalidMatchedAuthorityValuesProbe(
		[]string{"invalid role", "invalid\trole", "invalid-role", strings.Repeat("a", maxPeerRoleBytes+1)},
		func(fixture *ContractFixture, value string) {
			fixture.Authorization.Scope.ExpectedPeer.PeerRole = value
		},
	)},
	{id: "swapped_fence_claim", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) { fixture.Authorization.Scope.P4.FullFence.ClaimID = "swapped_claim" }, false)},
	{id: "swapped_fence_token", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.P4.FullFence.ClaimTokenHash = testHash("swapped-fence-token")
	}, false)},
	{id: "swapped_fence_generation", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) { fixture.Authorization.Scope.P4.FullFence.FenceGeneration++ }, false)},
	{id: "swapped_repository", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.Repository.RepositoryIdentityHash = testHash("swapped-repository")
	}, false)},
	{id: "swapped_base_commit", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.Repository.BaseCommit = strings.Repeat("a", 40)
	}, false)},
	{id: "swapped_base_tree", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.Repository.BaseTree = strings.Repeat("b", 40)
	}, false)},
	{id: "swapped_writable_path", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.WritablePaths[0].RepositoryRelativePathHash = testHash("swapped-path")
	}, false)},
	{id: "swapped_test_profile", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.TestProfiles[0].InstalledProfileHash = testHash("swapped-profile")
	}, false)},
	{id: "swapped_route", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.Route.RouteIdentityHash = testHash("swapped-route")
	}, false)},
	{id: "swapped_channel", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.ChannelBindingHash = testHash("swapped-channel")
	}, false)},
	{id: "swapped_peer", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) { fixture.Authorization.Scope.ExpectedPeer.UserID++ }, false)},
	{id: "swapped_policy", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) { fixture.Authorization.Scope.PolicyHash = testHash("swapped-policy") }, false)},
	{id: "duplicate_writable_path", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.WritablePaths = append(fixture.Authorization.Scope.WritablePaths, fixture.Authorization.Scope.WritablePaths[0])
	}, true)},
	{id: "duplicate_test_profile", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) {
		fixture.Authorization.Scope.TestProfiles = append(fixture.Authorization.Scope.TestProfiles, fixture.Authorization.Scope.TestProfiles[0])
	}, true)},
	{id: "attempt_zero", wantErr: ErrInvalidContract, run: attemptNumberProbe(0, AttemptCap)},
	{id: "attempt_one", run: probeAttemptOne},
	{id: "attempt_two", run: probeAttemptTwoWithVerifiedPredecessor},
	{id: "attempt_three", wantErr: ErrInvalidContract, run: attemptNumberProbe(3, AttemptCap)},
	{id: "attempt_cap_not_two", wantErr: ErrInvalidContract, run: attemptNumberProbe(1, 1)},
	{id: "normal_request_rotation", wantErr: ErrInvalidContract, run: authorizationMutationProbe(func(fixture *ContractFixture) { fixture.Authorization.Scope.RotationMode = "rotate" }, true)},
	{id: "unmaterialized_rotation_successor", wantErr: ErrInvalidContract, run: fixtureMutationProbe(func(t *testing.T, fixture *ContractFixture) {
		fixture.Rotation.State = "successor_authorized"
		rehashRotationAndFixture(t, fixture)
	})},
	{id: "unmaterialized_rotation_cross_signature", wantErr: ErrInvalidContract, run: unknownRotationFieldProbe("current_root_cross_signature_hash")},
	{id: "unmaterialized_rotation_release_approval", wantErr: ErrInvalidContract, run: unknownRotationFieldProbe("release_approval_hash")},
	{id: "duplicate_dispatch", wantErr: ErrDispatchConflict, run: replayMutationProbe(func(t *testing.T, dispatch *ImmutableDispatch) {
		dispatch.Request.RequestID = "duplicate_identity_changed_bytes"
		dispatch.Request.RequestHash = mustRecordHash(t, dispatch.Request, "request_hash")
		dispatch.DispatchHash = mustRecordHash(t, *dispatch, "dispatch_hash")
	})},
	{id: "conflicting_dispatch", wantErr: ErrDispatchConflict, run: replayMutationProbe(func(t *testing.T, dispatch *ImmutableDispatch) {
		dispatch.ChannelBindingHash = testHash("conflicting-dispatch-channel")
		dispatch.DispatchHash = mustRecordHash(t, *dispatch, "dispatch_hash")
	})},
	{id: "restart_exact_replay", run: probeExactDispatchReplay},
	{id: "restart_conflicting_replay", wantErr: ErrDispatchConflict, run: replayMutationProbe(func(t *testing.T, dispatch *ImmutableDispatch) {
		dispatch.DispatchNotAfter = "2026-07-26T12:07:59Z"
		dispatch.DispatchHash = mustRecordHash(t, *dispatch, "dispatch_hash")
	})},
	{id: "unknown_secret_field_redacted_diagnostic", wantErr: ErrInvalidContract, run: probeUnknownSecretDiagnostic},
	{id: "fixture_has_no_authority_payload", run: probeFixtureHasNoAuthorityPayload},
}

func TestP6A2ExecutableAcceptanceVectorRegistry(t *testing.T) {
	if len(executableAcceptanceVectorRegistry) != len(canonicalAcceptanceVectorIDs) {
		t.Fatalf("executable registry length = %d, canonical inventory length = %d", len(executableAcceptanceVectorRegistry), len(canonicalAcceptanceVectorIDs))
	}
	seen := make(map[string]struct{}, len(executableAcceptanceVectorRegistry))
	executed := make([]string, 0, len(executableAcceptanceVectorRegistry))
	for _, vector := range executableAcceptanceVectorRegistry {
		vector := vector
		if vector.id == "" || vector.run == nil {
			t.Fatalf("unexecutable acceptance-vector entry: %+v", vector)
		}
		if _, duplicate := seen[vector.id]; duplicate {
			t.Fatalf("duplicate acceptance-vector ID %q", vector.id)
		}
		seen[vector.id] = struct{}{}
		t.Run(vector.id, func(t *testing.T) {
			executed = append(executed, vector.id)
			err := vector.run(t)
			if vector.wantErr == nil {
				if err != nil {
					t.Fatalf("accepted vector returned %v", err)
				}
				return
			}
			if !errors.Is(err, vector.wantErr) {
				t.Fatalf("rejected vector error = %v, want %v", err, vector.wantErr)
			}
		})
	}
	assertExecutedVectorOrder(t, executed, canonicalAcceptanceVectorIDs[:])
}

func fixtureDecodeBytesProbe(mutate func([]byte) []byte) func(*testing.T) error {
	return func(t *testing.T) error {
		raw, _ := readFixture(t)
		_, err := DecodeFixture(mutate(raw))
		return err
	}
}

func probeDuplicateFixtureKey(t *testing.T) error {
	raw, _ := readFixture(t)
	comma := bytes.IndexByte(raw, ',')
	if comma < 0 {
		t.Fatal("fixture has no top-level member")
	}
	first := raw[1:comma]
	duplicate := append([]byte{'{'}, append(append(append([]byte(nil), first...), ','), raw[1:]...)...)
	_, err := DecodeFixture(duplicate)
	return err
}

func probeUnknownFixtureKeysEverywhere(t *testing.T) error {
	raw, _ := readFixture(t)
	var generic any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	variants := unknownFieldVariants(t, generic)
	if len(variants) < 20 {
		t.Fatalf("only %d object nesting points", len(variants))
	}
	for _, variant := range variants {
		mutated := canonicalTestArtifact(t, variant)
		if _, err := DecodeFixture(mutated); !errors.Is(err, ErrInvalidContract) {
			return err
		}
	}
	return ErrInvalidContract
}

func probeInvalidFixtureUTF8(t *testing.T) error {
	raw, _ := readFixture(t)
	mutated := append([]byte(nil), raw...)
	at := bytes.Index(mutated, []byte(ContractFixtureSchemaVersion))
	if at < 0 {
		t.Fatal("fixture schema version not found")
	}
	mutated[at] = 0xff
	_, err := DecodeFixture(mutated)
	return err
}

func fixtureMutationProbe(mutate func(*testing.T, *ContractFixture)) func(*testing.T) error {
	return func(t *testing.T) error {
		_, base := readFixture(t)
		fixture := cloneFixture(t, base)
		mutate(t, &fixture)
		return VerifyFixture(fixture, mustTime(t, base.Dispatch.CreatedAt), ValidationAdmission)
	}
}

func rehashDispatchAndFixture(t *testing.T, fixture *ContractFixture) {
	t.Helper()
	fixture.Dispatch.DispatchHash = mustRecordHash(t, fixture.Dispatch, "dispatch_hash")
	fixture.FixtureHash = mustRecordHash(t, *fixture, "fixture_hash")
}

func probeRehashedAttackerRootMutation(t *testing.T, fixture *ContractFixture) {
	t.Helper()
	fixture.TrustBundle.Root.RootID = "attacker_root"
	fixture.TrustBundle.Root.RootHash = testHash("attacker-root-certificate")
	fixture.TrustBundle.Root.RootSPKISHA256 = testHash("attacker-root-spki")
	fixture.TrustBundle.RepairAttestor.IssuerRootID = fixture.TrustBundle.Root.RootID
	fixture.TrustBundle.RepairAttestor.CertificateHash = testHash("attacker-leaf-certificate")
	fixture.TrustBundle.RepairAttestor.SubjectSPKISHA256 = testHash("attacker-leaf-spki")
	fixture.TrustBundle.TrustBundleHash = testHash("attacker-trust-bundle")
	fixture.ReleasePins.TrustBundleHash = fixture.TrustBundle.TrustBundleHash
	fixture.ReleasePins.RepairRootCertificateHash = fixture.TrustBundle.Root.RootHash
	fixture.ReleasePins.RepairRootSPKISHA256 = fixture.TrustBundle.Root.RootSPKISHA256
	fixture.ReleasePins.RepairAttestorCertificateHash = fixture.TrustBundle.RepairAttestor.CertificateHash
	fixture.ReleasePins.RepairAttestorRootID = fixture.TrustBundle.Root.RootID
	fixture.ReleasePins.RepairAttestorLeafSPKI = fixture.TrustBundle.RepairAttestor.SubjectSPKISHA256
	rehashPinsAndFixture(t, fixture)
	if fixture.Dispatch.ReleasePinsHash != fixture.ReleasePins.ReleasePinsHash ||
		!recordHashMatches(fixture.ReleasePins, "release_pins_hash", fixture.ReleasePins.ReleasePinsHash) ||
		!recordHashMatches(fixture.Dispatch, "dispatch_hash", fixture.Dispatch.DispatchHash) ||
		!recordHashMatches(*fixture, "fixture_hash", fixture.FixtureHash) {
		t.Fatal("attacker-root mutation did not recompute the complete release-pin dispatch chain")
	}
}

func probeChangedRotationApproverRootCertificate(t *testing.T) error {
	return probeRotationApproverArtifactDrift(t, "rotation_approver_root_certificate_der")
}

func probeChangedRotationApproverRootSPKI(t *testing.T) error {
	return probeRotationApproverArtifactDrift(t, "rotation_approver_root_spki_der")
}

func probeChangedRotationApproverLeafCertificate(t *testing.T) error {
	return probeRotationApproverArtifactDrift(t, "rotation_approver_certificate_der")
}

func probeChangedRotationApproverLeafSPKI(t *testing.T) error {
	return probeRotationApproverArtifactDrift(t, "rotation_approver_spki_der")
}

func probeRotationApproverArtifactDrift(t *testing.T, id string) error {
	t.Helper()
	base := embeddedReleaseArtifactSet()
	raw := base.byID()[id]
	if raw == "" {
		t.Fatalf("embedded approver artifact %q is missing", id)
	}
	mutatedBytes := append([]byte(nil), raw...)
	mutatedBytes[0] ^= 1
	mutated, ok := base.withArtifact(id, string(mutatedBytes))
	if !ok {
		t.Fatalf("unknown approver artifact %q", id)
	}
	return verifyReleaseArtifactSet(mutated, FrozenReleasePins(), time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC))
}

func probeWrongRotationApproverRole(t *testing.T) error {
	return probeRotationApproverExtensionDrift(t, "1.3.6.1.4.1.57264.1.6")
}

func probeWrongRotationApproverDomain(t *testing.T) error {
	return probeRotationApproverExtensionDrift(t, "1.3.6.1.4.1.57264.1.7")
}

func probeRotationApproverExtensionDrift(t *testing.T, oid string) error {
	t.Helper()
	repairRoot, repairLeaf, approverRoot, approverLeaf := parseEmbeddedCertificateChains(t)
	mutated := *approverLeaf
	mutated.Extensions = append(approverLeaf.Extensions[:0:0], approverLeaf.Extensions...)
	found := false
	for index := range mutated.Extensions {
		if mutated.Extensions[index].Id.String() == oid {
			mutated.Extensions[index].Value = []byte("drifted")
			found = true
		}
	}
	if !found {
		t.Fatalf("approver extension %s is missing", oid)
	}
	return validateRotationApproverCertificateSemantics(approverRoot, &mutated, repairRoot, repairLeaf,
		approverRoot.RawSubjectPublicKeyInfo, approverLeaf.RawSubjectPublicKeyInfo)
}

func probeExpiredRotationApproverCertificate(t *testing.T) error {
	_, _, root, leaf := parseEmbeddedCertificateChains(t)
	return verifyCertificateTime(root, leaf, leaf.NotAfter)
}

func probeFutureRotationApproverCertificate(t *testing.T) error {
	_, _, root, leaf := parseEmbeddedCertificateChains(t)
	return verifyCertificateTime(root, leaf, leaf.NotBefore.Add(-time.Nanosecond))
}

func probeRepairRootReusedForRotationApprover(t *testing.T) error {
	repairRoot, repairLeaf, _, approverLeaf := parseEmbeddedCertificateChains(t)
	return validateRotationApproverCertificateSemantics(repairRoot, approverLeaf, repairRoot, repairLeaf,
		repairRoot.RawSubjectPublicKeyInfo, approverLeaf.RawSubjectPublicKeyInfo)
}

func probeRepairLeafReusedForRotationApprover(t *testing.T) error {
	repairRoot, repairLeaf, approverRoot, _ := parseEmbeddedCertificateChains(t)
	return validateRotationApproverCertificateSemantics(approverRoot, repairLeaf, repairRoot, repairLeaf,
		approverRoot.RawSubjectPublicKeyInfo, repairLeaf.RawSubjectPublicKeyInfo)
}

func probeRotationApprovalSignerIDMismatch(t *testing.T) error {
	identity := releasedRotationApprovalSignerIdentity()
	identity.SignerKeyID = "other_rotation_approver"
	return validateRotationApprovalSignerIdentity(identity)
}

func probeRotationApprovalSignerSPKIMismatch(t *testing.T) error {
	identity := releasedRotationApprovalSignerIdentity()
	identity.SignerSPKISHA256 = testHash("other-rotation-approver-spki")
	return validateRotationApprovalSignerIdentity(identity)
}

func parseEmbeddedCertificateChains(t *testing.T) (repairRoot, repairLeaf, approverRoot, approverLeaf *x509.Certificate) {
	t.Helper()
	artifacts := embeddedReleaseArtifactSet()
	var err error
	repairRoot, err = x509.ParseCertificate([]byte(artifacts.repairRootCertificateDER))
	if err != nil {
		t.Fatalf("parse repair root: %v", err)
	}
	repairLeaf, err = x509.ParseCertificate([]byte(artifacts.repairAttestorCertificateDER))
	if err != nil {
		t.Fatalf("parse repair leaf: %v", err)
	}
	approverRoot, err = x509.ParseCertificate([]byte(artifacts.rotationApproverRootCertificateDER))
	if err != nil {
		t.Fatalf("parse rotation approver root: %v", err)
	}
	approverLeaf, err = x509.ParseCertificate([]byte(artifacts.rotationApproverCertificateDER))
	if err != nil {
		t.Fatalf("parse rotation approver leaf: %v", err)
	}
	return repairRoot, repairLeaf, approverRoot, approverLeaf
}

func probeSyntheticDescriptorFacts(t *testing.T) error {
	raw, err := canonicalBytes(CanonicalFixture())
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	generic["bundle_file_identity"] = map[string]any{"content_sha256": testHash("synthetic"), "device": 1, "inode": 2}
	_, err = DecodeFixture(canonicalTestArtifact(t, generic))
	return err
}

func approvalTimingProbe(age, lifetime time.Duration) func(*testing.T) error {
	return func(t *testing.T) error {
		_, base := readFixture(t)
		fixture := cloneFixture(t, base)
		now := mustTime(t, base.Dispatch.CreatedAt)
		approvedAt := now.Add(-age)
		fixture.Authorization.Approval.ApprovedAt = approvedAt.Format(time.RFC3339Nano)
		fixture.Authorization.Approval.NotAfter = approvedAt.Add(lifetime).Format(time.RFC3339Nano)
		rehashAuthorizationChain(t, &fixture)
		expected := authorityFromAuthorization(fixture.Authorization)
		_, err := VerifyAuthorization(expected, fixture.Authorization, nil, now, ValidationAdmission)
		return err
	}
}

func verifiedCanonicalAttemptOne(t *testing.T) (ContractFixture, AuthorityContext, *VerifiedAuthorization) {
	t.Helper()
	fixture := CanonicalFixture()
	expected := authorityFromAuthorization(fixture.Authorization)
	verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, expected, verified
}

func probeDispatchCreatedAtAuthorizationExpiry(t *testing.T) error {
	fixture, expected, verified := verifiedCanonicalAttemptOne(t)
	dispatch := cloneDispatch(t, fixture.Dispatch)
	dispatch.CreatedAt = fixture.Authorization.Approval.NotAfter
	dispatch.DispatchNotAfter = mustTime(t, dispatch.CreatedAt).Add(time.Second).Format(time.RFC3339Nano)
	dispatch.DispatchHash = mustRecordHash(t, dispatch, "dispatch_hash")
	_, err := DecodeDispatch(expected, verified, canonicalTestArtifact(t, dispatch), mustTime(t, dispatch.CreatedAt), ValidationAdmission)
	return err
}

func probeExpiredDispatchEffect(t *testing.T) error {
	fixture, expected, verified := verifiedCanonicalAttemptOne(t)
	_, err := DecodeDispatch(expected, verified, canonicalTestArtifact(t, fixture.Dispatch), mustTime(t, fixture.Dispatch.DispatchNotAfter), ValidationEffect)
	return err
}

func authorizationMutationProbe(mutate func(*ContractFixture), matchMutatedAuthority bool) func(*testing.T) error {
	return func(t *testing.T) error {
		_, base := readFixture(t)
		fixture := cloneFixture(t, base)
		expected := authorityFromAuthorization(base.Authorization)
		mutate(&fixture)
		rehashAuthorizationChain(t, &fixture)
		if matchMutatedAuthority {
			expected = authorityFromAuthorization(fixture.Authorization)
		}
		_, err := VerifyAuthorization(expected, fixture.Authorization, nil, mustTime(t, base.Dispatch.CreatedAt), ValidationAdmission)
		return err
	}
}

func invalidMatchedAuthorityMutationProbe(mutate func(*ContractFixture)) func(*testing.T) error {
	return func(t *testing.T) error {
		_, base := readFixture(t)
		fixture := cloneFixture(t, base)
		mutate(&fixture)
		rehashAuthorizationChain(t, &fixture)
		expected := authorityFromAuthorization(fixture.Authorization)
		if err := validateAuthorityContext(expected); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("matching authority accepted invalid closed value: %v", err)
		}
		if err := validateAuthorizationRecord(fixture.Authorization, mustTime(t, base.Dispatch.CreatedAt)); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("authorization record accepted invalid closed value: %v", err)
		}
		_, err := VerifyAuthorization(expected, fixture.Authorization, nil, mustTime(t, base.Dispatch.CreatedAt), ValidationAdmission)
		return err
	}
}

func invalidMatchedAuthorityValuesProbe(values []string, mutate func(*ContractFixture, string)) func(*testing.T) error {
	return func(t *testing.T) error {
		for index, value := range values {
			probe := invalidMatchedAuthorityMutationProbe(func(fixture *ContractFixture) {
				mutate(fixture, value)
			})
			if err := probe(t); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("invalid closed value case %d returned %v", index, err)
			}
		}
		return ErrInvalidContract
	}
}

func attemptNumberProbe(number, cap int) func(*testing.T) error {
	return authorizationMutationProbe(func(fixture *ContractFixture) {
		setAttempt(fixture, number, cap)
		if number == 1 {
			fixture.Authorization.Scope.Attempt.PreviousAuthorizationHash = ""
		} else {
			fixture.Authorization.Scope.Attempt.PreviousAuthorizationHash = testHash("unverified-predecessor")
		}
	}, true)
}

func probeAttemptOne(t *testing.T) error {
	fixture := CanonicalFixture()
	_, err := VerifyAuthorization(authorityFromAuthorization(fixture.Authorization), fixture.Authorization, nil, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission)
	return err
}

func probeAttemptTwoWithVerifiedPredecessor(t *testing.T) error {
	first, _, verifiedFirst := verifiedCanonicalAttemptOne(t)
	second := CanonicalAttemptTwoFixture()
	if second.Authorization.Scope.Attempt.PreviousAuthorizationHash != first.Authorization.AuthorizationHash {
		t.Fatal("attempt-2 oracle lost its exact predecessor hash")
	}
	_, err := VerifyAuthorization(authorityFromAuthorization(second.Authorization), second.Authorization, verifiedFirst, mustTime(t, second.Dispatch.CreatedAt), ValidationAdmission)
	return err
}

func unknownRotationFieldProbe(field string) func(*testing.T) error {
	return func(t *testing.T) error {
		raw, _ := readFixture(t)
		var generic map[string]any
		if err := json.Unmarshal(raw, &generic); err != nil {
			t.Fatal(err)
		}
		rotation, ok := generic["rotation"].(map[string]any)
		if !ok {
			t.Fatal("fixture rotation object missing")
		}
		rotation[field] = testHash("unmaterialized-" + field)
		_, err := DecodeFixture(canonicalTestArtifact(t, generic))
		return err
	}
}

func replayMutationProbe(mutate func(*testing.T, *ImmutableDispatch)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture, expected, verified := verifiedCanonicalAttemptOne(t)
		existing := canonicalTestArtifact(t, fixture.Dispatch)
		incoming := cloneDispatch(t, fixture.Dispatch)
		mutate(t, &incoming)
		_, err := ClassifyDispatchReplay(expected, verified, existing, canonicalTestArtifact(t, incoming))
		return err
	}
}

func probeExactDispatchReplay(t *testing.T) error {
	fixture, expected, verified := verifiedCanonicalAttemptOne(t)
	raw := canonicalTestArtifact(t, fixture.Dispatch)
	disposition, err := ClassifyDispatchReplay(expected, verified, raw, append([]byte(nil), raw...))
	if err == nil && disposition != DispatchExactReplay {
		return fmt.Errorf("disposition %q", disposition)
	}
	return err
}

func probeUnknownSecretDiagnostic(t *testing.T) error {
	raw, _ := readFixture(t)
	mutated := bytes.Replace(raw, []byte(`"schema_version":"`+ContractFixtureSchemaVersion+`"`), []byte(`"private_key":"probe-private-value","schema_version":"`+ContractFixtureSchemaVersion+`"`), 1)
	_, err := DecodeFixture(mutated)
	if err != nil && (strings.Contains(err.Error(), "private_key") || strings.Contains(err.Error(), "probe-private-value")) {
		t.Fatalf("secret-looking field leaked in diagnostic: %q", err)
	}
	return err
}

func probeFixtureHasNoAuthorityPayload(t *testing.T) error {
	raw, _ := readFixture(t)
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]struct{}{
		"argv": {}, "command": {}, "credential": {}, "credentials": {}, "env": {}, "environment": {},
		"executable": {}, "path": {}, "pid": {}, "private_key": {}, "process": {}, "raw_path": {}, "secret": {}, "socket_path": {},
	}
	var walk func(any) error
	walk = func(current any) error {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if _, found := forbidden[key]; found {
					return ErrInvalidContract
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}
