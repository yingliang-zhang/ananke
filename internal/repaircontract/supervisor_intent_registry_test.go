package repaircontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

type supervisorIntentExecutableVector struct {
	id      string
	wantErr error
	run     func(*testing.T) error
}

var canonicalSupervisorIntentVectorIDs = [...]string{
	"canonical_attempt_1_chain",
	"canonical_attempt_2_chain",
	"wrong_phase",
	"missing_phase",
	"same_tuple_second_committed_slot",
	"wrong_sequence",
	"slot_id_aliased_across_phases",
	"same_slot_changed_bytes",
	"swapped_authorization",
	"swapped_approval",
	"swapped_p4_fact",
	"swapped_p4_proposal",
	"swapped_p4_input",
	"swapped_p4_evidence_bundle",
	"swapped_p4_admission",
	"swapped_fence",
	"swapped_fence_claim_token",
	"swapped_request",
	"swapped_dispatch",
	"swapped_channel",
	"swapped_peer",
	"swapped_policy",
	"wrong_boot_epoch",
	"prior_epoch_live_claim",
	"wrong_repository_identity",
	"wrong_base_identity",
	"wrong_executable_identity",
	"wrong_sandbox_identity",
	"wrong_namespace_identity",
	"wrong_root_identity",
	"missing_predecessor_claim",
	"wrong_predecessor_claim",
	"missing_predecessor_terminal_event",
	"wrong_predecessor_terminal_event",
	"invented_predecessor_terminal_event",
	"wrong_predecessor_terminal_event_capability",
	"aliased_predecessor_terminal_event_capability",
	"missing_verified_predecessor",
	"wrong_verified_predecessor",
	"attempt_1_claim_reused_in_attempt_2",
	"attempt_2_chain_with_old_approval",
	"attempt_2_chain_with_attempt_1_predecessor",
	"noncanonical_json",
	"unknown_key",
	"duplicate_key",
	"changed_timestamp",
	"overlong_lifetime",
	"exact_replay_capability_n_minus_1ns",
	"exact_replay_no_capability_n",
	"exact_replay_no_capability_n_plus_1ns",
	"stale_authorization_exact_replay_no_capability",
	"stale_dispatch_exact_replay_no_capability",
	"expired_release_exact_replay_no_capability",
	"canonical_recovery_record_chain",
	"recovery_record_unknown_key",
	"recovery_record_duplicate_key",
	"recovery_record_trailing_data",
	"recovery_record_noncanonical_json",
	"recovery_record_hash_mismatch",
	"recovery_record_wrong_sequence",
	"recovery_record_wrong_kind",
	"recovery_record_wrong_attempt",
	"recovery_record_wrong_phase",
	"recovery_record_wrong_claim",
	"recovery_record_wrong_slot",
	"recovery_record_wrong_boot",
	"recovery_record_wrong_journal",
	"recovery_record_wrong_predecessor",
	"recovery_record_wrong_durability",
	"recovery_record_wrong_payload",
	"recovery_record_wrong_time",
	"nil_opaque_recovery_snapshot",
	"zero_opaque_recovery_snapshot",
	"invented_unverified_recovery_snapshot",
	"opaque_recovery_snapshot_private_mutation",
	"recovery_chain_aliased_across_attempt_phase_slot",
	"verified_empty_journal_observation",
	"crash_before_claim_commit",
	"crash_after_claim_commit",
	"crash_before_phase_launch",
	"crash_after_phase_launch",
	"crash_before_terminal_proof_persistence",
	"crash_after_terminal_proof_persistence",
	"crash_before_attestation_signature_persistence",
	"crash_after_attestation_signature_persistence",
	"crash_before_response_persistence",
	"crash_after_response_persistence",
	"prior_epoch_nonterminal_waiting_for_human",
	"current_epoch_recovery_status_only",
	"exact_response_replay_only",
	"invalid_recovery_launch_action",
	"forged_recovery_completeness",
	"forged_recovery_ambiguity",
	"truncated_recovery_record",
	"missing_recovery_record",
	"unknown_recovery_record",
	"duplicate_recovery_record",
	"out_of_order_recovery_record",
	"response_replay_tries_second_effect",
}

var supervisorIntentVectorRegistry = []supervisorIntentExecutableVector{
	{id: "canonical_attempt_1_chain", run: canonicalSupervisorChainProbe(1)},
	{id: "canonical_attempt_2_chain", run: canonicalSupervisorChainProbe(2)},
	{id: "wrong_phase", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.Phase = AdapterClaimPhase })},
	{id: "missing_phase", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.Phase = "" })},
	{id: "same_tuple_second_committed_slot", wantErr: ErrInvalidSupervisorIntent, run: sameTupleSecondCommittedSlotProbe},
	{id: "wrong_sequence", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.Sequence = 2 })},
	{id: "slot_id_aliased_across_phases", wantErr: ErrInvalidSupervisorIntent, run: slotIDCrossPhaseAliasProbe},
	{id: "same_slot_changed_bytes", wantErr: ErrSupervisorClaimConflict, run: changedCommittedSlotProbe},
	{id: "swapped_authorization", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.AuthorizationHash = testHash("swapped-authorization") })},
	{id: "swapped_approval", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.ApprovalHash = testHash("swapped-approval") })},
	{id: "swapped_p4_fact", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.P4FactHash = testHash("swapped-p4-fact") })},
	{id: "swapped_p4_proposal", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.P4ProposalHash = testHash("swapped-p4-proposal") })},
	{id: "swapped_p4_input", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.P4InputHash = testHash("swapped-p4-input") })},
	{id: "swapped_p4_evidence_bundle", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) {
		claim.P4EvidenceBundleHash = testHash("swapped-p4-evidence-bundle")
	})},
	{id: "swapped_p4_admission", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.P4AdmissionHash = testHash("swapped-p4-admission") })},
	{id: "swapped_fence", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.FenceHash = testHash("swapped-fence") })},
	{id: "swapped_fence_claim_token", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.FenceClaimTokenHash = testHash("swapped-fence-token") })},
	{id: "swapped_request", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.RequestHash = testHash("swapped-request") })},
	{id: "swapped_dispatch", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.DispatchHash = testHash("swapped-dispatch") })},
	{id: "swapped_channel", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.ChannelBindingHash = testHash("swapped-channel") })},
	{id: "swapped_peer", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.PeerIdentityHash = testHash("swapped-peer") })},
	{id: "swapped_policy", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.PolicyHash = testHash("swapped-policy") })},
	{id: "wrong_boot_epoch", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.SupervisorBootEpochID = "wrong_boot_epoch" })},
	{id: "prior_epoch_live_claim", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.SupervisorBootEpochHash = testHash("prior-boot-epoch") })},
	{id: "wrong_repository_identity", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.RepositoryIdentityHash = testHash("wrong-repository") })},
	{id: "wrong_base_identity", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.BaseCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" })},
	{id: "wrong_executable_identity", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.ExecutableIdentityHash = testHash("wrong-executable") })},
	{id: "wrong_sandbox_identity", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.SandboxProfileHash = testHash("wrong-sandbox") })},
	{id: "wrong_namespace_identity", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.NamespaceIdentityHash = testHash("wrong-namespace") })},
	{id: "wrong_root_identity", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.RootIdentityHash = testHash("wrong-root") })},
	{id: "missing_predecessor_claim", wantErr: ErrInvalidSupervisorIntent, run: predecessorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.PredecessorClaimHash = "" }, false)},
	{id: "wrong_predecessor_claim", wantErr: ErrInvalidSupervisorIntent, run: predecessorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.PredecessorClaimHash = testHash("wrong-predecessor") }, false)},
	{id: "missing_predecessor_terminal_event", wantErr: ErrInvalidSupervisorIntent, run: predecessorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.PredecessorTerminalEventHash = "" }, false)},
	{id: "wrong_predecessor_terminal_event", wantErr: ErrInvalidSupervisorIntent, run: predecessorClaimMutationProbe(func(claim *SupervisorIntentClaim) {
		claim.PredecessorTerminalEventHash = testHash("wrong-predecessor-terminal")
	}, false)},
	{id: "invented_predecessor_terminal_event", wantErr: ErrInvalidSupervisorIntent, run: inventedPredecessorTerminalEventProbe},
	{id: "wrong_predecessor_terminal_event_capability", wantErr: ErrInvalidSupervisorIntent, run: wrongPredecessorTerminalEventCapabilityProbe},
	{id: "aliased_predecessor_terminal_event_capability", wantErr: ErrInvalidSupervisorIntent, run: aliasedPredecessorTerminalEventCapabilityProbe},
	{id: "missing_verified_predecessor", wantErr: ErrInvalidSupervisorIntent, run: predecessorClaimMutationProbe(nil, true)},
	{id: "wrong_verified_predecessor", wantErr: ErrInvalidSupervisorIntent, run: wrongVerifiedPredecessorProbe},
	{id: "attempt_1_claim_reused_in_attempt_2", wantErr: ErrInvalidSupervisorIntent, run: attemptOneClaimReusedProbe},
	{id: "attempt_2_chain_with_old_approval", wantErr: ErrInvalidSupervisorIntent, run: attemptTwoOldApprovalProbe},
	{id: "attempt_2_chain_with_attempt_1_predecessor", wantErr: ErrInvalidSupervisorIntent, run: attemptTwoOldPredecessorProbe},
	{id: "noncanonical_json", wantErr: ErrInvalidSupervisorIntent, run: noncanonicalSupervisorClaimProbe},
	{id: "unknown_key", wantErr: ErrInvalidSupervisorIntent, run: unknownSupervisorClaimKeyProbe},
	{id: "duplicate_key", wantErr: ErrInvalidSupervisorIntent, run: duplicateSupervisorClaimKeyProbe("claim_id")},
	{id: "changed_timestamp", wantErr: ErrInvalidSupervisorIntent, run: supervisorClaimMutationProbe(func(claim *SupervisorIntentClaim) { claim.CreatedAt = "2026-07-26T12:04:11Z" })},
	{id: "overlong_lifetime", wantErr: ErrInvalidSupervisorIntent, run: overlongSupervisorClaimProbe},
	{id: "exact_replay_capability_n_minus_1ns", run: exactReplayCapabilityBoundaryProbe(-time.Nanosecond, true)},
	{id: "exact_replay_no_capability_n", run: exactReplayCapabilityBoundaryProbe(0, false)},
	{id: "exact_replay_no_capability_n_plus_1ns", run: exactReplayCapabilityBoundaryProbe(time.Nanosecond, false)},
	{id: "stale_authorization_exact_replay_no_capability", run: staleAuthorizationExactReplayProbe},
	{id: "stale_dispatch_exact_replay_no_capability", run: staleDispatchExactReplayProbe},
	{id: "expired_release_exact_replay_no_capability", run: expiredReleaseExactReplayProbe},
	{id: "canonical_recovery_record_chain", run: canonicalRecoveryRecordChainProbe},
	{id: "recovery_record_unknown_key", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordUnknownKeyProbe},
	{id: "recovery_record_duplicate_key", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordDuplicateKeyProbe},
	{id: "recovery_record_trailing_data", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordTrailingDataProbe},
	{id: "recovery_record_noncanonical_json", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordNoncanonicalJSONProbe},
	{id: "recovery_record_hash_mismatch", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordHashMismatchProbe},
	{id: "recovery_record_wrong_sequence", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongSequenceProbe},
	{id: "recovery_record_wrong_kind", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongKindProbe},
	{id: "recovery_record_wrong_attempt", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongAttemptProbe},
	{id: "recovery_record_wrong_phase", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongPhaseProbe},
	{id: "recovery_record_wrong_claim", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongClaimProbe},
	{id: "recovery_record_wrong_slot", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongSlotProbe},
	{id: "recovery_record_wrong_boot", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongBootProbe},
	{id: "recovery_record_wrong_journal", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongJournalProbe},
	{id: "recovery_record_wrong_predecessor", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongPredecessorProbe},
	{id: "recovery_record_wrong_durability", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongDurabilityProbe},
	{id: "recovery_record_wrong_payload", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongPayloadProbe},
	{id: "recovery_record_wrong_time", wantErr: ErrInvalidSupervisorRecovery, run: recoveryRecordWrongTimeProbe},
	{id: "nil_opaque_recovery_snapshot", wantErr: ErrInvalidSupervisorRecovery, run: nilRecoverySnapshotProbe},
	{id: "zero_opaque_recovery_snapshot", wantErr: ErrInvalidSupervisorRecovery, run: zeroRecoverySnapshotProbe},
	{id: "invented_unverified_recovery_snapshot", wantErr: ErrInvalidSupervisorRecovery, run: inventedUnverifiedRecoverySnapshotProbe},
	{id: "opaque_recovery_snapshot_private_mutation", run: opaqueRecoverySnapshotPrivateMutationProbe},
	{id: "recovery_chain_aliased_across_attempt_phase_slot", run: aliasedRecoveryChainProbe},
	{id: "verified_empty_journal_observation", run: verifiedEmptyJournalProbe},
	{id: "crash_before_claim_commit", run: crashMatrixVectorProbe(BeforeClaimCommit, 0, NoDurableSupervisorClaim, RecoveryNoDurableClaim, FreshLiveIntentRequired, RecoveryStatusOnly)},
	{id: "crash_after_claim_commit", run: crashMatrixVectorProbe(AfterClaimCommit, 1, DurableClaimCommitted, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly)},
	{id: "crash_before_phase_launch", run: crashMatrixVectorProbe(BeforePhaseLaunch, 1, DurableClaimCommitted, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly)},
	{id: "crash_after_phase_launch", run: crashMatrixVectorProbe(AfterPhaseLaunch, 2, DurablePhaseLaunched, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly)},
	{id: "crash_before_terminal_proof_persistence", run: crashMatrixVectorProbe(BeforeTerminalProofPersistence, 2, DurablePhaseLaunched, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly)},
	{id: "crash_after_terminal_proof_persistence", run: crashMatrixVectorProbe(AfterTerminalProofPersistence, 3, DurableTerminalProofPersisted, RecoveryTerminalStatus, AttestationStatusRequired, RecoveryStatusOnly)},
	{id: "crash_before_attestation_signature_persistence", run: crashMatrixVectorProbe(BeforeAttestationSignaturePersistence, 3, DurableTerminalProofPersisted, RecoveryTerminalStatus, AttestationStatusRequired, RecoveryStatusOnly)},
	{id: "crash_after_attestation_signature_persistence", run: crashMatrixVectorProbe(AfterAttestationSignaturePersistence, 4, DurableAttestationSignaturePersisted, RecoveryTerminalStatus, ResponseStatusRequired, RecoveryStatusOnly)},
	{id: "crash_before_response_persistence", run: crashMatrixVectorProbe(BeforeResponsePersistence, 4, DurableAttestationSignaturePersisted, RecoveryTerminalStatus, ResponseStatusRequired, RecoveryStatusOnly)},
	{id: "crash_after_response_persistence", run: crashMatrixVectorProbe(AfterResponsePersistence, 5, DurableResponsePersisted, RecoveryResponseReplay, NoFurtherEffectPermitted, RecoveryReplayResponse)},
	{id: "prior_epoch_nonterminal_waiting_for_human", run: priorEpochWaitingForHumanProbe},
	{id: "current_epoch_recovery_status_only", run: currentEpochStatusOnlyProbe},
	{id: "exact_response_replay_only", run: exactResponseReplayProbe},
	{id: "invalid_recovery_launch_action", wantErr: ErrInvalidSupervisorRecovery, run: invalidRecoveryActionProbe(SupervisorRecoveryAction("launch"))},
	{id: "forged_recovery_completeness", wantErr: ErrInvalidSupervisorRecovery, run: forgedRecoveryCompletenessProbe},
	{id: "forged_recovery_ambiguity", wantErr: ErrInvalidSupervisorRecovery, run: ambiguousRecoveryProbe},
	{id: "truncated_recovery_record", wantErr: ErrInvalidSupervisorRecovery, run: truncatedRecoveryProbe},
	{id: "missing_recovery_record", wantErr: ErrInvalidSupervisorRecovery, run: missingRecoveryRecordProbe},
	{id: "unknown_recovery_record", wantErr: ErrInvalidSupervisorRecovery, run: unknownRecoveryRecordProbe},
	{id: "duplicate_recovery_record", wantErr: ErrInvalidSupervisorRecovery, run: duplicateRecoveryRecordProbe},
	{id: "out_of_order_recovery_record", wantErr: ErrInvalidSupervisorRecovery, run: outOfOrderRecoveryRecordProbe},
	{id: "response_replay_tries_second_effect", wantErr: ErrInvalidSupervisorRecovery, run: invalidRecoveryActionProbe(SupervisorRecoveryAction("replay_response_and_effect"))},
}

func TestP6Slice3ExecutableVectorRegistry(t *testing.T) {
	if len(supervisorIntentVectorRegistry) != len(canonicalSupervisorIntentVectorIDs) {
		t.Fatalf("Slice-3 executable registry length = %d, canonical inventory length = %d", len(supervisorIntentVectorRegistry), len(canonicalSupervisorIntentVectorIDs))
	}
	seen := make(map[string]struct{}, len(supervisorIntentVectorRegistry))
	executed := make([]string, 0, len(supervisorIntentVectorRegistry))
	for _, vector := range supervisorIntentVectorRegistry {
		vector := vector
		if vector.id == "" || vector.run == nil {
			t.Fatalf("unexecutable Slice-3 vector: %+v", vector)
		}
		if _, duplicate := seen[vector.id]; duplicate {
			t.Fatalf("duplicate Slice-3 vector ID %q", vector.id)
		}
		seen[vector.id] = struct{}{}
		t.Run(vector.id, func(t *testing.T) {
			executed = append(executed, vector.id)
			err := vector.run(t)
			if vector.wantErr == nil {
				if err != nil {
					t.Fatalf("accepted Slice-3 vector returned %v", err)
				}
				return
			}
			if !errors.Is(err, vector.wantErr) {
				t.Fatalf("rejected Slice-3 vector error = %v, want %v", err, vector.wantErr)
			}
		})
	}
	assertExecutedVectorOrder(t, executed, canonicalSupervisorIntentVectorIDs[:])
}

func canonicalSupervisorChainProbe(attemptNumber int) func(*testing.T) error {
	return func(t *testing.T) error {
		first, second := canonicalSupervisorAttempts(t)
		attempt := first
		if attemptNumber == 2 {
			attempt = second
		}
		for index := range attempt.claims {
			assessment, verified, err := EvaluateSupervisorIntentClaim(
				attempt.authorities[index], attempt.authorization, attempt.slotCommits[index],
				predecessorAt(attempt, index), predecessorTerminalAt(attempt, index),
				attempt.canonical[index], mustTime(t, attempt.claims[index].CreatedAt),
			)
			if err != nil {
				return err
			}
			if assessment.Disposition != SupervisorClaimExactReplay || assessment.EffectAllowed || verified == nil {
				return fmt.Errorf("phase %d assessment %+v", index+1, assessment)
			}
		}
		return nil
	}
}

func supervisorClaimMutationProbe(mutate func(*SupervisorIntentClaim)) func(*testing.T) error {
	return func(t *testing.T) error {
		first, _ := canonicalSupervisorAttempts(t)
		claim := first.claims[0]
		mutate(&claim)
		claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
		assessment, verified, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, nil, nil, nil, canonicalTestArtifact(t, claim), mustTime(t, first.claims[0].CreatedAt))
		if assessment.EffectAllowed || verified != nil {
			t.Fatalf("rehashed mutation granted effect or capability: assessment=%+v verified=%v", assessment, verified)
		}
		return err
	}
}

func predecessorClaimMutationProbe(mutate func(*SupervisorIntentClaim), omitCapability bool) func(*testing.T) error {
	return func(t *testing.T) error {
		first, _ := canonicalSupervisorAttempts(t)
		claim := first.claims[1]
		if mutate != nil {
			mutate(&claim)
			claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
		}
		predecessor := first.committedClaim[0]
		if omitCapability {
			predecessor = nil
		}
		assessment, verified, err := EvaluateSupervisorIntentClaim(
			first.authorities[1], first.authorization, nil, predecessor, first.terminalEvents[0],
			canonicalTestArtifact(t, claim), mustTime(t, claim.CreatedAt),
		)
		if assessment.EffectAllowed || verified != nil {
			t.Fatalf("predecessor mutation granted effect or capability: assessment=%+v verified=%v", assessment, verified)
		}
		return err
	}
}

func duplicateSupervisorClaimKeyProbe(key string) func(*testing.T) error {
	return func(t *testing.T) error {
		first, _ := canonicalSupervisorAttempts(t)
		raw := first.canonical[0]
		needle := []byte(`"` + key + `":`)
		at := bytes.Index(raw, needle)
		if at < 0 {
			t.Fatalf("claim key %q not found", key)
		}
		valueStart := at + len(needle)
		valueEnd := valueStart
		if raw[valueStart] == '"' {
			valueEnd++
			for valueEnd < len(raw) && raw[valueEnd] != '"' {
				valueEnd++
			}
			valueEnd++
		} else {
			for valueEnd < len(raw) && raw[valueEnd] != ',' && raw[valueEnd] != '}' {
				valueEnd++
			}
		}
		member := append([]byte(nil), raw[at:valueEnd]...)
		mutated := append([]byte(nil), raw[:at]...)
		mutated = append(mutated, member...)
		mutated = append(mutated, ',')
		mutated = append(mutated, raw[at:]...)
		_, _, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, nil, nil, nil, mutated, mustTime(t, first.claims[0].CreatedAt))
		return err
	}
}

func sameTupleSecondCommittedSlotProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	authority := first.authorities[0]
	authority.ClaimID = "attempt_1_materialization_claim_second_slot"
	authority.JournalSlotID = "attempt_1_materialization_claim_second_slot"
	claim := supervisorClaimFromTestAuthority(first.contract, authority, "")
	claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
	raw := canonicalTestArtifact(t, claim)
	proof := mintSupervisorClaimSlotCommitForTest(t, authority, claim, raw, false, true)
	_, _, err := EvaluateSupervisorIntentClaim(authority, first.authorization, proof, nil, nil, raw, mustTime(t, claim.CreatedAt))
	return err
}

func slotIDCrossPhaseAliasProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	authority := first.authorities[1]
	authority.JournalSlotID = first.authorities[0].JournalSlotID
	claim := supervisorClaimFromTestAuthority(first.contract, authority, first.claims[1].PredecessorTerminalEventHash)
	claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
	raw := canonicalTestArtifact(t, claim)
	proof := mintSupervisorClaimSlotCommitForTest(t, authority, claim, raw, true, false)
	_, _, err := EvaluateSupervisorIntentClaim(authority, first.authorization, proof, first.committedClaim[0], first.terminalEvents[0], raw, mustTime(t, claim.CreatedAt))
	return err
}

func changedCommittedSlotProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	claim := first.claims[0]
	claim.ClaimID = "changed_same_slot_claim"
	claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
	assessment, verified, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, first.slotCommits[0], nil, nil, canonicalTestArtifact(t, claim), mustTime(t, claim.CreatedAt))
	if assessment.Disposition != SupervisorClaimConflict || assessment.EffectAllowed || verified != nil {
		t.Fatalf("changed committed slot assessment=%+v verified=%v", assessment, verified)
	}
	return err
}

func inventedPredecessorTerminalEventProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	claim := first.claims[1]
	claim.PredecessorTerminalEventHash = testHash("invented-predecessor-terminal-event")
	claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
	_, _, err := EvaluateSupervisorIntentClaim(first.authorities[1], first.authorization, nil, first.committedClaim[0], nil, canonicalTestArtifact(t, claim), mustTime(t, claim.CreatedAt))
	return err
}

func wrongPredecessorTerminalEventCapabilityProbe(t *testing.T) error {
	first, second := canonicalSupervisorAttempts(t)
	_, _, err := EvaluateSupervisorIntentClaim(first.authorities[1], first.authorization, nil, first.committedClaim[0], second.terminalEvents[0], first.canonical[1], mustTime(t, first.claims[1].CreatedAt))
	return err
}

func aliasedPredecessorTerminalEventCapabilityProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	_, _, err := EvaluateSupervisorIntentClaim(first.authorities[2], first.authorization, nil, first.committedClaim[1], first.terminalEvents[0], first.canonical[2], mustTime(t, first.claims[2].CreatedAt))
	return err
}

func wrongVerifiedPredecessorProbe(t *testing.T) error {
	first, second := canonicalSupervisorAttempts(t)
	_, _, err := EvaluateSupervisorIntentClaim(first.authorities[1], first.authorization, nil, second.committedClaim[0], first.terminalEvents[0], first.canonical[1], mustTime(t, first.claims[1].CreatedAt))
	return err
}

func attemptOneClaimReusedProbe(t *testing.T) error {
	first, second := canonicalSupervisorAttempts(t)
	_, _, err := EvaluateSupervisorIntentClaim(second.authorities[0], second.authorization, nil, nil, nil, first.canonical[0], mustTime(t, second.claims[0].CreatedAt))
	return err
}

func attemptTwoOldApprovalProbe(t *testing.T) error {
	first, second := canonicalSupervisorAttempts(t)
	claim := second.claims[0]
	claim.ApprovalHash = first.claims[0].ApprovalHash
	claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
	_, _, err := EvaluateSupervisorIntentClaim(second.authorities[0], second.authorization, nil, nil, nil, canonicalTestArtifact(t, claim), mustTime(t, claim.CreatedAt))
	return err
}

func attemptTwoOldPredecessorProbe(t *testing.T) error {
	first, second := canonicalSupervisorAttempts(t)
	_, _, err := EvaluateSupervisorIntentClaim(second.authorities[1], second.authorization, nil, first.committedClaim[0], second.terminalEvents[0], second.canonical[1], mustTime(t, second.claims[1].CreatedAt))
	return err
}

func noncanonicalSupervisorClaimProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	raw := append([]byte{' '}, first.canonical[0]...)
	_, _, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, nil, nil, nil, raw, mustTime(t, first.claims[0].CreatedAt))
	return err
}

func unknownSupervisorClaimKeyProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	var generic map[string]any
	decoder := json.NewDecoder(bytes.NewReader(first.canonical[0]))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	generic["unknown_claim_member"] = testHash("unknown")
	_, _, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, nil, nil, nil, canonicalTestArtifact(t, generic), mustTime(t, first.claims[0].CreatedAt))
	return err
}

func overlongSupervisorClaimProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	authority := first.authorities[0]
	authority.NotAfter = mustTime(t, authority.CreatedAt).Add(MaxSupervisorIntentLifetime + time.Nanosecond).Format(time.RFC3339Nano)
	claim := first.claims[0]
	claim.NotAfter = authority.NotAfter
	claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
	_, _, err := EvaluateSupervisorIntentClaim(authority, first.authorization, nil, nil, nil, canonicalTestArtifact(t, claim), mustTime(t, claim.CreatedAt))
	return err
}

func exactReplayCapabilityBoundaryProbe(offset time.Duration, wantCapability bool) func(*testing.T) error {
	return func(t *testing.T) error {
		first, _ := canonicalSupervisorAttempts(t)
		now := mustTime(t, first.claims[0].NotAfter).Add(offset)
		assessment, capability, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, first.slotCommits[0], nil, nil, first.canonical[0], now)
		if err != nil || assessment.Disposition != SupervisorClaimExactReplay || assessment.EffectAllowed || (capability != nil) != wantCapability {
			return fmt.Errorf("assessment=%+v capability=%v error=%v", assessment, capability, err)
		}
		return nil
	}
}

func staleAuthorizationExactReplayProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	now := mustTime(t, first.contract.Authorization.Approval.ApprovedAt).Add(MaxApprovalAge + time.Nanosecond)
	return historicalExactReplayNoCapability(t, first, now)
}

func staleDispatchExactReplayProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	return historicalExactReplayNoCapability(t, first, mustTime(t, first.contract.Dispatch.DispatchNotAfter))
}

func expiredReleaseExactReplayProbe(t *testing.T) error {
	first, _ := canonicalSupervisorAttempts(t)
	return historicalExactReplayNoCapability(t, first, mustTime(t, FrozenTrustBundle().RepairAttestor.NotAfter))
}

func historicalExactReplayNoCapability(t *testing.T, attempt canonicalSupervisorAttempt, now time.Time) error {
	t.Helper()
	assessment, capability, err := EvaluateSupervisorIntentClaim(attempt.authorities[0], attempt.authorization, attempt.slotCommits[0], nil, nil, attempt.canonical[0], now)
	if err != nil || assessment.Disposition != SupervisorClaimExactReplay || assessment.EffectAllowed || capability != nil {
		return fmt.Errorf("historical replay assessment=%+v capability=%v error=%v", assessment, capability, err)
	}
	return nil
}

func canonicalRecoveryRecordChainProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	for index, raw := range fixture.canonical {
		record, err := DecodeSupervisorRecoveryRecord(raw)
		if err != nil {
			return fmt.Errorf("record %d: %w", index+1, err)
		}
		if record != fixture.records[index] {
			return fmt.Errorf("record %d decoded value drifted", index+1)
		}
	}
	return nil
}

func recoveryRecordUnknownKeyProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(fixture.canonical[0]))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	value["unknown_recovery_member"] = true
	_, err := DecodeSupervisorRecoveryRecord(canonicalTestArtifact(t, value))
	return err
}

func recoveryRecordDuplicateKeyProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	raw := append([]byte(`{"record_id":"duplicate",`), fixture.canonical[0][1:]...)
	_, err := DecodeSupervisorRecoveryRecord(raw)
	return err
}

func recoveryRecordTrailingDataProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	raw := append(append([]byte(nil), fixture.canonical[0]...), []byte(`{}`)...)
	_, err := DecodeSupervisorRecoveryRecord(raw)
	return err
}

func recoveryRecordNoncanonicalJSONProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	_, err := DecodeSupervisorRecoveryRecord(append([]byte{' '}, fixture.canonical[0]...))
	return err
}

func recoveryRecordHashMismatchProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, false, func(value *SupervisorRecoveryRecord) {
		value.RecordHash = testHash("mismatched-recovery-record-hash")
	})
}

func recoveryRecordWrongSequenceProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.Sequence = 2 })
}

func recoveryRecordWrongKindProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.Kind = SupervisorRecoveryPhaseLaunchRecord })
}

func recoveryRecordWrongAttemptProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.AttemptHash = testHash("wrong-recovery-attempt") })
}

func recoveryRecordWrongPhaseProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.Phase = AdapterClaimPhase })
}

func recoveryRecordWrongClaimProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.ClaimHash = testHash("wrong-recovery-claim") })
}

func recoveryRecordWrongSlotProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.SlotID = "wrong_recovery_slot" })
}

func recoveryRecordWrongBootProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.SupervisorBootEpochHash = testHash("wrong-recovery-boot") })
}

func recoveryRecordWrongJournalProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.JournalHeadHash = testHash("wrong-recovery-journal") })
}

func recoveryRecordWrongPredecessorProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterPhaseLaunch, 1, true, func(value *SupervisorRecoveryRecord) {
		value.PredecessorRecordHash = testHash("wrong-recovery-predecessor")
	})
}

func recoveryRecordWrongDurabilityProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.DurabilityPolicyID = "NORMAL" })
}

func recoveryRecordWrongPayloadProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	snapshot.records[0].SemanticPayloadHash = testHash("wrong-recovery-payload")
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func recoveryRecordWrongTimeProbe(t *testing.T) error {
	return reencodedRecoveryRecordMutationProbe(t, AfterClaimCommit, 0, true, func(value *SupervisorRecoveryRecord) { value.OccurredAt = "2026-07-26T12:00:01+00:00" })
}

func reencodedRecoveryRecordMutationProbe(t *testing.T, cutPoint SupervisorCrashCutPoint, index int, rehash bool, mutate func(*SupervisorRecoveryRecord)) error {
	t.Helper()
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, cutPoint)
	record := snapshot.records[index]
	mutate(&record)
	if rehash {
		record.RecordHash = mustRecordHash(t, record, "record_hash")
	}
	snapshot.records[index] = record
	snapshot.canonicalRecords[index] = canonicalTestArtifact(t, record)
	snapshot.canonicalRecordHashes[index] = sha256Digest(snapshot.canonicalRecords[index])
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func nilRecoverySnapshotProbe(*testing.T) error {
	_, err := ClassifySupervisorRecovery(nil, RecoveryStatusOnly)
	return err
}

func zeroRecoverySnapshotProbe(*testing.T) error {
	_, err := ClassifySupervisorRecovery(&VerifiedSupervisorRecoverySnapshot{}, RecoveryStatusOnly)
	return err
}

func inventedUnverifiedRecoverySnapshotProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterResponsePersistence)
	snapshot.currentBootEpochHash = testHash("invented-current-boot")
	snapshot.recordedBootEpochHash = testHash("invented-recorded-boot")
	for index := range snapshot.records {
		snapshot.records[index].RecordHash = testHash(fmt.Sprintf("invented-unrelated-record-%d", index+1))
	}
	snapshot.journalVerified = false
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryReplayResponse)
	return err
}

func opaqueRecoverySnapshotPrivateMutationProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	base := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	mutations := []func(*VerifiedSupervisorRecoverySnapshot){
		func(value *VerifiedSupervisorRecoverySnapshot) { value.valid = false },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.journalVerified = false },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.durabilityVerified = false },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.completenessVerified = false },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.ambiguityChecked = false },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.unambiguous = false },
		func(value *VerifiedSupervisorRecoverySnapshot) {
			value.attemptHash = testHash("mutated-recovery-attempt")
		},
		func(value *VerifiedSupervisorRecoverySnapshot) { value.phase = AdapterClaimPhase },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.claimHash = testHash("mutated-recovery-claim") },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.slotID = "mutated_recovery_slot" },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.canonicalRecords[0][0] ^= 1 },
		func(value *VerifiedSupervisorRecoverySnapshot) {
			value.canonicalRecordHashes[0] = testHash("mutated-recovery-canonical-hash")
		},
		func(value *VerifiedSupervisorRecoverySnapshot) {
			value.integrityHash = testHash("mutated-recovery-integrity")
		},
	}
	for index, mutate := range mutations {
		value := cloneVerifiedSupervisorRecoverySnapshotForTest(base)
		mutate(value)
		if result, err := ClassifySupervisorRecovery(value, RecoveryStatusOnly); !errors.Is(err, ErrInvalidSupervisorRecovery) || result != (SupervisorRecoveryResult{}) {
			return fmt.Errorf("private mutation %d classified: result=%+v err=%v", index, result, err)
		}
	}
	return nil
}

func aliasedRecoveryChainProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	_, second := canonicalSupervisorAttempts(t)
	mutations := []func(*VerifiedSupervisorRecoverySnapshot){
		func(value *VerifiedSupervisorRecoverySnapshot) {
			value.attemptHash = second.claims[0].AttemptHash
			value.attemptNumber = 2
		},
		func(value *VerifiedSupervisorRecoverySnapshot) { value.phase = AdapterClaimPhase },
		func(value *VerifiedSupervisorRecoverySnapshot) { value.slotID = second.claims[0].JournalSlotID },
	}
	for index, mutate := range mutations {
		value := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
		mutate(value)
		value.integrityHash = supervisorRecoverySnapshotIntegrityHash(value)
		if result, err := ClassifySupervisorRecovery(value, RecoveryStatusOnly); !errors.Is(err, ErrInvalidSupervisorRecovery) || result != (SupervisorRecoveryResult{}) {
			return fmt.Errorf("recovery alias %d classified: result=%+v err=%v", index, result, err)
		}
	}
	return nil
}

func verifiedEmptyJournalProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, BeforeClaimCommit)
	result, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	want := SupervisorRecoveryResult{DurableObservation: NoDurableSupervisorClaim, Disposition: RecoveryNoDurableClaim, NextRequirement: FreshLiveIntentRequired}
	if err != nil {
		return err
	}
	if result != want || result.EffectAllowed {
		return fmt.Errorf("verified empty journal result %+v, want %+v", result, want)
	}
	return nil
}

func exactResponseReplayProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterResponsePersistence)
	result, err := ClassifySupervisorRecovery(snapshot, RecoveryReplayResponse)
	want := SupervisorRecoveryResult{DurableObservation: DurableResponsePersisted, Disposition: RecoveryResponseReplay, NextRequirement: NoFurtherEffectPermitted}
	if err != nil {
		return err
	}
	if result != want || result.EffectAllowed {
		return fmt.Errorf("response replay result %+v, want %+v", result, want)
	}
	return nil
}

func forgedRecoveryCompletenessProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	snapshot.completenessVerified = false
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func crashMatrixVectorProbe(cutPoint SupervisorCrashCutPoint, count int, durable SupervisorDurableObservation, disposition SupervisorRecoveryDisposition, next SupervisorRecoveryRequirement, action SupervisorRecoveryAction) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
		snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, cutPoint)
		if len(snapshot.records) != count {
			return fmt.Errorf("cut point %q retained %d records, want %d", cutPoint, len(snapshot.records), count)
		}
		result, err := ClassifySupervisorRecovery(snapshot, action)
		if err != nil {
			return err
		}
		want := SupervisorRecoveryResult{DurableObservation: durable, Disposition: disposition, EffectAllowed: false, NextRequirement: next}
		if result != want {
			return fmt.Errorf("result %+v, want %+v", result, want)
		}
		return nil
	}
}

func priorEpochWaitingForHumanProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	for _, test := range []struct {
		count    int
		cutPoint SupervisorCrashCutPoint
	}{
		{count: 1, cutPoint: AfterClaimCommit},
		{count: 2, cutPoint: AfterPhaseLaunch},
	} {
		snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, test.cutPoint)
		result, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
		if err != nil {
			return err
		}
		if len(snapshot.records) != test.count || result.Disposition != RecoveryWaitingForHuman || result.EffectAllowed || result.NextRequirement != HumanReviewRequired {
			return fmt.Errorf("prior-epoch count %d result %+v", test.count, result)
		}
	}
	return nil
}

func currentEpochStatusOnlyProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, true)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	result, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	if err != nil {
		return err
	}
	if result.Disposition != RecoveryCurrentEpochStatusOnly || result.EffectAllowed || result.NextRequirement != LiveCommitConfirmationRequired {
		return fmt.Errorf("current-epoch result %+v", result)
	}
	return nil
}

func invalidRecoveryActionProbe(action SupervisorRecoveryAction) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
		cutPoint := AfterClaimCommit
		if action == SupervisorRecoveryAction("replay_response_and_effect") {
			cutPoint = AfterResponsePersistence
		}
		snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, cutPoint)
		result, err := ClassifySupervisorRecovery(snapshot, action)
		if result.EffectAllowed {
			t.Fatal("invalid recovery action granted an effect")
		}
		return err
	}
}

func ambiguousRecoveryProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	snapshot.unambiguous = false
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func truncatedRecoveryProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	snapshot.canonicalRecords[0] = snapshot.canonicalRecords[0][:len(snapshot.canonicalRecords[0])-1]
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func missingRecoveryRecordProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterResponsePersistence)
	snapshot.records = snapshot.records[:4]
	snapshot.canonicalRecords = snapshot.canonicalRecords[:4]
	snapshot.canonicalRecordHashes = snapshot.canonicalRecordHashes[:4]
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryReplayResponse)
	return err
}

func unknownRecoveryRecordProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	record := snapshot.records[0]
	record.Kind = "unknown_record"
	setSnapshotRecordForTest(t, snapshot, 0, record)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func duplicateRecoveryRecordProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterPhaseLaunch)
	snapshot.records[1] = snapshot.records[0]
	snapshot.canonicalRecords[1] = append([]byte(nil), snapshot.canonicalRecords[0]...)
	snapshot.canonicalRecordHashes[1] = snapshot.canonicalRecordHashes[0]
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func outOfOrderRecoveryRecordProbe(t *testing.T) error {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterTerminalProofPersistence)
	snapshot.records[1], snapshot.records[2] = snapshot.records[2], snapshot.records[1]
	snapshot.canonicalRecords[1], snapshot.canonicalRecords[2] = snapshot.canonicalRecords[2], snapshot.canonicalRecords[1]
	snapshot.canonicalRecordHashes[1], snapshot.canonicalRecordHashes[2] = snapshot.canonicalRecordHashes[2], snapshot.canonicalRecordHashes[1]
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
	_, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
	return err
}

func setSnapshotRecordForTest(t *testing.T, snapshot *VerifiedSupervisorRecoverySnapshot, index int, record SupervisorRecoveryRecord) {
	t.Helper()
	record.RecordHash = mustRecordHash(t, record, "record_hash")
	snapshot.records[index] = record
	snapshot.canonicalRecords[index] = canonicalTestArtifact(t, record)
	snapshot.canonicalRecordHashes[index] = sha256Digest(snapshot.canonicalRecords[index])
	snapshot.integrityHash = supervisorRecoverySnapshotIntegrityHash(snapshot)
}

func predecessorAt(attempt canonicalSupervisorAttempt, index int) *VerifiedSupervisorIntentClaim {
	if index == 0 {
		return nil
	}
	return attempt.committedClaim[index-1]
}

func predecessorTerminalAt(attempt canonicalSupervisorAttempt, index int) *VerifiedSupervisorTerminalEvent {
	if index == 0 {
		return nil
	}
	return attempt.terminalEvents[index-1]
}
