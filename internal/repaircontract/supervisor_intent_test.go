package repaircontract

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

type canonicalSupervisorAttempt struct {
	contract       ContractFixture
	authorization  *VerifiedAuthorization
	authorities    [3]SupervisorIntentAuthority
	claims         [3]SupervisorIntentClaim
	canonical      [3][]byte
	slotCommits    [3]*VerifiedSupervisorClaimSlotCommit
	committedClaim [3]*VerifiedSupervisorIntentClaim
	terminalEvents [2]*VerifiedSupervisorTerminalEvent
}

func TestP6Slice3CanonicalAttemptClaimChains(t *testing.T) {
	first, second := canonicalSupervisorAttempts(t)
	wantPhases := [...]SupervisorIntentPhase{MaterializationClaimPhase, AdapterClaimPhase, TestClaimPhase}

	for _, attempt := range []*canonicalSupervisorAttempt{&first, &second} {
		seenSlots := make(map[string]struct{}, len(wantPhases))
		for index, wantPhase := range wantPhases {
			claim := attempt.claims[index]
			authority := attempt.authorities[index]
			var predecessor *VerifiedSupervisorIntentClaim
			var predecessorTerminal *VerifiedSupervisorTerminalEvent
			if index > 0 {
				predecessor = attempt.committedClaim[index-1]
				predecessorTerminal = attempt.terminalEvents[index-1]
			}
			assessment, verified, err := EvaluateSupervisorIntentClaim(authority, attempt.authorization, nil, predecessor, predecessorTerminal, attempt.canonical[index], mustTime(t, claim.CreatedAt))
			if err != nil {
				t.Fatalf("attempt %d phase %s empty-slot evaluation: %v", claim.AttemptNumber, wantPhase, err)
			}
			if assessment.Disposition != SupervisorClaimAwaitingCommit || assessment.EffectAllowed || verified != nil {
				t.Fatalf("attempt %d phase %s empty-slot assessment = %+v, verified=%v", claim.AttemptNumber, wantPhase, assessment, verified)
			}
			if claim.SchemaVersion != SupervisorIntentClaimSchemaVersion || claim.Phase != wantPhase || claim.Sequence != index+1 ||
				claim.AttemptNumber != attempt.contract.Authorization.Scope.Attempt.AttemptNumber || claim.AttemptCap != AttemptCap ||
				claim.DurabilityPolicyID != DurabilityPolicyFullFullFSync || claim.ClaimHash == "" || claim.ClaimID == "" {
				t.Fatalf("attempt %d phase %s canonical claim identity drifted: %+v", claim.AttemptNumber, wantPhase, claim)
			}
			if _, duplicate := seenSlots[authority.JournalSlotID]; duplicate {
				t.Fatalf("attempt %d duplicate slot %q", claim.AttemptNumber, authority.JournalSlotID)
			}
			seenSlots[authority.JournalSlotID] = struct{}{}
			if index == 0 {
				if claim.PredecessorClaimHash != "" || claim.PredecessorTerminalEventHash != "" {
					t.Fatalf("attempt %d materialization claim has predecessor", claim.AttemptNumber)
				}
			} else if claim.PredecessorClaimHash != attempt.claims[index-1].ClaimHash || claim.PredecessorTerminalEventHash == "" {
				t.Fatalf("attempt %d phase %s predecessor binding drifted", claim.AttemptNumber, wantPhase)
			}
		}
	}

	if first.claims[0].AttemptHash == second.claims[0].AttemptHash ||
		first.contract.Authorization.AuthorizationHash == second.contract.Authorization.AuthorizationHash ||
		first.contract.Authorization.ApprovalHash == second.contract.Authorization.ApprovalHash {
		t.Fatal("attempt 2 did not start a distinct authorization, approval, and claim chain")
	}
	for index := range first.claims {
		if first.claims[index].ClaimHash == second.claims[index].ClaimHash || bytes.Equal(first.canonical[index], second.canonical[index]) {
			t.Fatalf("attempt-1 phase %d claim reused in attempt 2", index+1)
		}
	}
}

func TestP6Slice3OpaqueClaimCapabilityIntegrityAndMutationIsolation(t *testing.T) {
	first, _ := canonicalSupervisorAttempts(t)
	slotInput := cloneSupervisorClaimSlotCommit(*first.slotCommits[0])
	raw := append([]byte(nil), first.canonical[0]...)
	assessment, verified, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, &slotInput, nil, nil, raw, mustTime(t, first.claims[0].CreatedAt))
	if err != nil || assessment.Disposition != SupervisorClaimExactReplay || assessment.EffectAllowed || verified == nil {
		t.Fatalf("mint committed claim capability: assessment=%+v verified=%v err=%v", assessment, verified, err)
	}

	for index := range raw {
		raw[index] = 'x'
	}
	for index := range slotInput.committedCanonical {
		slotInput.committedCanonical[index] = 'y'
	}
	if _, next, err := EvaluateSupervisorIntentClaim(first.authorities[1], first.authorization, first.slotCommits[1], verified, first.terminalEvents[0], first.canonical[1], mustTime(t, first.claims[1].CreatedAt)); err != nil || next == nil {
		t.Fatalf("caller mutation corrupted retained predecessor capability: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*VerifiedSupervisorIntentClaim)
	}{
		{name: "valid bit", mutate: func(value *VerifiedSupervisorIntentClaim) { value.valid = false }},
		{name: "claim", mutate: func(value *VerifiedSupervisorIntentClaim) { value.claim.ClaimID = "mutated_claim" }},
		{name: "canonical bytes", mutate: func(value *VerifiedSupervisorIntentClaim) { value.canonical[0] ^= 1 }},
		{name: "slot canonical bytes", mutate: func(value *VerifiedSupervisorIntentClaim) { value.slotCommit.committedCanonical[0] ^= 1 }},
		{name: "slot authority", mutate: func(value *VerifiedSupervisorIntentClaim) { value.slotCommit.slotID = "mutated_slot" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			copyValue := *verified
			copyValue.canonical = append([]byte(nil), verified.canonical...)
			copyValue.slotCommit = cloneSupervisorClaimSlotCommit(verified.slotCommit)
			mutation.mutate(&copyValue)
			assessment, next, err := EvaluateSupervisorIntentClaim(first.authorities[1], first.authorization, first.slotCommits[1], &copyValue, first.terminalEvents[0], first.canonical[1], mustTime(t, first.claims[1].CreatedAt))
			if !errors.Is(err, ErrInvalidSupervisorIntent) || next != nil || assessment.EffectAllowed {
				t.Fatalf("mutated opaque predecessor assessment=%+v verified=%v err=%v", assessment, next, err)
			}
		})
	}
}

func TestP6Slice3CrashRecoveryMatrix(t *testing.T) {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	matrix := []struct {
		cutPoint        SupervisorCrashCutPoint
		observation     SupervisorDurableObservation
		disposition     SupervisorRecoveryDisposition
		nextRequirement SupervisorRecoveryRequirement
		action          SupervisorRecoveryAction
	}{
		{BeforeClaimCommit, NoDurableSupervisorClaim, RecoveryNoDurableClaim, FreshLiveIntentRequired, RecoveryStatusOnly},
		{AfterClaimCommit, DurableClaimCommitted, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly},
		{BeforePhaseLaunch, DurableClaimCommitted, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly},
		{AfterPhaseLaunch, DurablePhaseLaunched, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly},
		{BeforeTerminalProofPersistence, DurablePhaseLaunched, RecoveryWaitingForHuman, HumanReviewRequired, RecoveryStatusOnly},
		{AfterTerminalProofPersistence, DurableTerminalProofPersisted, RecoveryTerminalStatus, AttestationStatusRequired, RecoveryStatusOnly},
		{BeforeAttestationSignaturePersistence, DurableTerminalProofPersisted, RecoveryTerminalStatus, AttestationStatusRequired, RecoveryStatusOnly},
		{AfterAttestationSignaturePersistence, DurableAttestationSignaturePersisted, RecoveryTerminalStatus, ResponseStatusRequired, RecoveryStatusOnly},
		{BeforeResponsePersistence, DurableAttestationSignaturePersisted, RecoveryTerminalStatus, ResponseStatusRequired, RecoveryStatusOnly},
		{AfterResponsePersistence, DurableResponsePersisted, RecoveryResponseReplay, NoFurtherEffectPermitted, RecoveryReplayResponse},
	}
	for _, test := range matrix {
		t.Run(string(test.cutPoint), func(t *testing.T) {
			snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, test.cutPoint)
			result, err := ClassifySupervisorRecovery(snapshot, test.action)
			if err != nil {
				t.Fatal(err)
			}
			if result.DurableObservation != test.observation || result.Disposition != test.disposition ||
				result.NextRequirement != test.nextRequirement || result.EffectAllowed {
				t.Fatalf("result = %+v, want observation=%q disposition=%q effect=false next=%q", result, test.observation, test.disposition, test.nextRequirement)
			}
		})
	}
}

func TestP6Slice3CurrentEpochRecoveryNeverReconstructsEffectAuthority(t *testing.T) {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, true)
	for _, cutPoint := range []SupervisorCrashCutPoint{AfterClaimCommit, AfterPhaseLaunch} {
		snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, cutPoint)
		result, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
		if err != nil {
			t.Fatal(err)
		}
		if result.Disposition != RecoveryCurrentEpochStatusOnly || result.EffectAllowed || result.NextRequirement != LiveCommitConfirmationRequired {
			t.Fatalf("current-epoch recovery cut point %q = %+v", cutPoint, result)
		}
	}
}

func canonicalSupervisorAttempts(t *testing.T) (canonicalSupervisorAttempt, canonicalSupervisorAttempt) {
	t.Helper()
	firstContract := CanonicalFixture()
	firstAuthorization, err := VerifyAuthorization(authorityFromAuthorization(firstContract.Authorization), firstContract.Authorization, nil, mustTime(t, firstContract.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	first := canonicalSupervisorAttemptFor(t, firstContract, firstAuthorization)

	secondContract := CanonicalAttemptTwoFixture()
	secondAuthorization, err := VerifyAuthorization(authorityFromAuthorization(secondContract.Authorization), secondContract.Authorization, firstAuthorization, mustTime(t, secondContract.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	second := canonicalSupervisorAttemptFor(t, secondContract, secondAuthorization)
	return first, second
}

func canonicalSupervisorAttemptFor(t *testing.T, contract ContractFixture, authorization *VerifiedAuthorization) canonicalSupervisorAttempt {
	t.Helper()
	phases := [...]SupervisorIntentPhase{MaterializationClaimPhase, AdapterClaimPhase, TestClaimPhase}
	attemptNumber := contract.Authorization.Scope.Attempt.AttemptNumber
	attemptHash := supervisorAttemptIdentityHash(contract.Authorization, contract.Dispatch)
	if attemptHash == "" {
		t.Fatal("construct canonical supervisor attempt identity")
	}
	bootEpochID := "trusted_supervisor_boot_epoch_00" + string(rune('0'+attemptNumber))
	bootEpochHash := testHash(bootEpochID)
	journalHeads := [...]string{
		testHash("trusted-journal-head-attempt-" + string(rune('0'+attemptNumber))),
		testHash("trusted-journal-head-after-materialization-attempt-" + string(rune('0'+attemptNumber))),
		testHash("trusted-journal-head-after-adapter-attempt-" + string(rune('0'+attemptNumber))),
	}
	baseTime := mustTime(t, contract.Dispatch.CreatedAt)
	var result canonicalSupervisorAttempt
	result.contract = contract
	result.authorization = authorization

	for index, phase := range phases {
		createdAt := baseTime.Add(time.Duration(index+1) * 10 * time.Second)
		notAfter := canonicalSupervisorClaimNotAfter(t, contract, createdAt)
		authority := SupervisorIntentAuthority{
			BootEpochID:            bootEpochID,
			BootEpochHash:          bootEpochHash,
			JournalHeadHash:        journalHeads[index],
			JournalSlotID:          "attempt_" + string(rune('0'+attemptNumber)) + "_" + string(phase) + "_slot",
			AttemptHash:            attemptHash,
			AttemptNumber:          attemptNumber,
			AttemptCap:             AttemptCap,
			Phase:                  phase,
			Sequence:               index + 1,
			ClaimID:                "attempt_" + string(rune('0'+attemptNumber)) + "_" + string(phase),
			AcceptedDispatch:       contract.Dispatch,
			Repository:             contract.Authorization.Scope.Repository,
			ExecutableIdentityHash: testHash("installed-" + string(phase) + "-executable"),
			SandboxProfileID:       "installed_" + string(phase) + "_sandbox_v1",
			SandboxProfileHash:     testHash("installed-" + string(phase) + "-sandbox-v1"),
			NamespaceID:            "installed_" + string(phase) + "_namespace_v1",
			NamespaceIdentityHash:  testHash("installed-" + string(phase) + "-namespace-v1"),
			RootIdentityHash:       testHash("installed-" + string(phase) + "-root-v1"),
			DurabilityPolicyID:     DurabilityPolicyFullFullFSync,
			CreatedAt:              createdAt.Format(time.RFC3339Nano),
			NotAfter:               notAfter.Format(time.RFC3339Nano),
		}
		var predecessor *VerifiedSupervisorIntentClaim
		var predecessorTerminal *VerifiedSupervisorTerminalEvent
		predecessorTerminalEventHash := ""
		if index > 0 {
			authority.PredecessorClaimHash = result.claims[index-1].ClaimHash
			predecessor = result.committedClaim[index-1]
			predecessorTerminal = result.terminalEvents[index-1]
			predecessorTerminalEventHash = predecessorTerminal.terminalEventHash
		}
		claim := supervisorClaimFromTestAuthority(contract, authority, predecessorTerminalEventHash)
		claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
		raw := canonicalTestArtifact(t, claim)
		slotCommit := mintSupervisorClaimSlotCommitForTest(t, authority, claim, raw, true, true)
		assessment, committed, err := EvaluateSupervisorIntentClaim(authority, authorization, slotCommit, predecessor, predecessorTerminal, raw, createdAt)
		if err != nil || assessment.Disposition != SupervisorClaimExactReplay || assessment.EffectAllowed || committed == nil {
			t.Fatalf("construct attempt %d phase %s fixture: assessment=%+v committed=%v err=%v", attemptNumber, phase, assessment, committed, err)
		}
		result.authorities[index] = authority
		result.claims[index] = claim
		result.canonical[index] = raw
		result.slotCommits[index] = slotCommit
		result.committedClaim[index] = committed
		if index < len(result.terminalEvents) {
			eventHash := testHash("attempt-" + string(rune('0'+attemptNumber)) + "-" + string(phase) + "-terminal-event")
			result.terminalEvents[index] = mintSupervisorTerminalEventForTest(t, committed, eventHash, journalHeads[index+1])
		}
	}
	return result
}

func canonicalSupervisorClaimNotAfter(t *testing.T, contract ContractFixture, createdAt time.Time) time.Time {
	t.Helper()
	approvedAt := mustTime(t, contract.Authorization.Approval.ApprovedAt)
	candidates := [...]time.Time{
		createdAt.Add(MaxSupervisorIntentLifetime),
		mustTime(t, contract.Dispatch.DispatchNotAfter),
		mustTime(t, contract.Authorization.Approval.NotAfter),
		approvedAt.Add(MaxApprovalAge).Add(time.Nanosecond),
	}
	notAfter := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Before(notAfter) {
			notAfter = candidate
		}
	}
	if !notAfter.After(createdAt) {
		t.Fatalf("attempt %d phase claim has no positive freshness intersection: created_at=%s not_after=%s", contract.Authorization.Scope.Attempt.AttemptNumber, createdAt.Format(time.RFC3339Nano), notAfter.Format(time.RFC3339Nano))
	}
	return notAfter.UTC()
}

func supervisorClaimFromTestAuthority(contract ContractFixture, authority SupervisorIntentAuthority, predecessorTerminalEventHash string) SupervisorIntentClaim {
	p4 := contract.Authorization.Scope.P4
	return SupervisorIntentClaim{
		SchemaVersion:                SupervisorIntentClaimSchemaVersion,
		ClaimID:                      authority.ClaimID,
		Phase:                        authority.Phase,
		Sequence:                     authority.Sequence,
		AttemptHash:                  authority.AttemptHash,
		AttemptNumber:                authority.AttemptNumber,
		AttemptCap:                   authority.AttemptCap,
		AuthorizationHash:            contract.Authorization.AuthorizationHash,
		ApprovalHash:                 contract.Authorization.ApprovalHash,
		PolicyHash:                   contract.Authorization.PolicyHash,
		P4FactHash:                   p4.P4FactHash,
		P4ProposalHash:               p4.P4ProposalHash,
		P4InputHash:                  p4.P4InputHash,
		P4EvidenceBundleHash:         p4.P4EvidenceBundleHash,
		P4AdmissionHash:              p4.P4AdmissionHash,
		FenceHash:                    p4.FullFence.FenceHash,
		FenceClaimTokenHash:          p4.FullFence.ClaimTokenHash,
		RequestHash:                  contract.Dispatch.Request.RequestHash,
		DispatchHash:                 contract.Dispatch.DispatchHash,
		ChannelBindingHash:           contract.Dispatch.ChannelBindingHash,
		PeerIdentityHash:             contract.Dispatch.ExpectedPeer.PeerIdentityHash,
		PredecessorClaimHash:         authority.PredecessorClaimHash,
		PredecessorTerminalEventHash: predecessorTerminalEventHash,
		SupervisorBootEpochID:        authority.BootEpochID,
		SupervisorBootEpochHash:      authority.BootEpochHash,
		JournalHeadHash:              authority.JournalHeadHash,
		JournalSlotID:                authority.JournalSlotID,
		RepositoryBindingHash:        authority.Repository.RepositoryBindingHash,
		RepositoryIdentityHash:       authority.Repository.RepositoryIdentityHash,
		BaseCommit:                   authority.Repository.BaseCommit,
		BaseTree:                     authority.Repository.BaseTree,
		ExecutableIdentityHash:       authority.ExecutableIdentityHash,
		SandboxProfileID:             authority.SandboxProfileID,
		SandboxProfileHash:           authority.SandboxProfileHash,
		NamespaceID:                  authority.NamespaceID,
		NamespaceIdentityHash:        authority.NamespaceIdentityHash,
		RootIdentityHash:             authority.RootIdentityHash,
		DurabilityPolicyID:           authority.DurabilityPolicyID,
		CreatedAt:                    authority.CreatedAt,
		NotAfter:                     authority.NotAfter,
	}
}
