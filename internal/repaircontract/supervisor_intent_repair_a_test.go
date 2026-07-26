package repaircontract

import (
	"errors"
	"testing"
	"time"
)

func TestP6Slice3RepairARejectsConflictingOpaqueSlotProofs(t *testing.T) {
	first, _ := canonicalSupervisorAttempts(t)

	assessment, verified, err := EvaluateSupervisorIntentClaim(
		first.authorities[0], first.authorization, first.slotCommits[0], nil, nil,
		first.canonical[0], mustTime(t, first.claims[0].CreatedAt),
	)
	if err != nil || assessment.Disposition != SupervisorClaimExactReplay || verified == nil || assessment.EffectAllowed {
		t.Fatalf("first unique committed slot: assessment=%+v verified=%v err=%v", assessment, verified, err)
	}

	t.Run("second committed slot for same tuple", func(t *testing.T) {
		authority := first.authorities[0]
		authority.ClaimID = "attempt_1_materialization_claim_second_slot"
		authority.JournalSlotID = "attempt_1_materialization_claim_second_slot"
		claim := supervisorClaimFromTestAuthority(first.contract, authority, "")
		claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
		raw := canonicalTestArtifact(t, claim)
		conflicting := mintSupervisorClaimSlotCommitForTest(t, authority, claim, raw, false, true)

		assessment, capability, err := EvaluateSupervisorIntentClaim(
			authority, first.authorization, conflicting, nil, nil, raw, mustTime(t, claim.CreatedAt),
		)
		if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("second same-tuple slot: assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("slot ID aliased across phases", func(t *testing.T) {
		authority := first.authorities[1]
		authority.JournalSlotID = first.authorities[0].JournalSlotID
		claim := supervisorClaimFromTestAuthority(first.contract, authority, first.claims[1].PredecessorTerminalEventHash)
		claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
		raw := canonicalTestArtifact(t, claim)
		conflicting := mintSupervisorClaimSlotCommitForTest(t, authority, claim, raw, true, false)

		assessment, capability, err := EvaluateSupervisorIntentClaim(
			authority, first.authorization, conflicting, first.committedClaim[0], first.terminalEvents[0],
			raw, mustTime(t, claim.CreatedAt),
		)
		if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("cross-phase slot alias: assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})
}

func TestP6Slice3RepairARequiresOpaquePredecessorTerminalEvent(t *testing.T) {
	first, second := canonicalSupervisorAttempts(t)
	authority := first.authorities[1]

	t.Run("invented hash without capability", func(t *testing.T) {
		claim := first.claims[1]
		claim.PredecessorTerminalEventHash = testHash("invented-terminal-event")
		claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
		raw := canonicalTestArtifact(t, claim)
		assessment, capability, err := EvaluateSupervisorIntentClaim(
			authority, first.authorization, nil, first.committedClaim[0], nil, raw, mustTime(t, claim.CreatedAt),
		)
		if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("invented event hash: assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("wrong attempt event capability", func(t *testing.T) {
		assessment, capability, err := EvaluateSupervisorIntentClaim(
			authority, first.authorization, nil, first.committedClaim[0], second.terminalEvents[0],
			first.canonical[1], mustTime(t, first.claims[1].CreatedAt),
		)
		if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("wrong-attempt event: assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	t.Run("prior phase event aliased for later phase", func(t *testing.T) {
		assessment, capability, err := EvaluateSupervisorIntentClaim(
			first.authorities[2], first.authorization, nil, first.committedClaim[1], first.terminalEvents[0],
			first.canonical[2], mustTime(t, first.claims[2].CreatedAt),
		)
		if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil || assessment.EffectAllowed {
			t.Fatalf("aliased event: assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})
}

func TestP6Slice3RepairAStaleExactReplayCapabilityBoundary(t *testing.T) {
	first, _ := canonicalSupervisorAttempts(t)
	notAfter := mustTime(t, first.claims[0].NotAfter)
	for _, test := range []struct {
		name           string
		now            time.Time
		wantCapability bool
	}{
		{name: "N-1ns", now: notAfter.Add(-time.Nanosecond), wantCapability: true},
		{name: "N", now: notAfter, wantCapability: false},
		{name: "N+1ns", now: notAfter.Add(time.Nanosecond), wantCapability: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			assessment, capability, err := EvaluateSupervisorIntentClaim(
				first.authorities[0], first.authorization, first.slotCommits[0], nil, nil,
				first.canonical[0], test.now,
			)
			if err != nil || assessment.Disposition != SupervisorClaimExactReplay || assessment.EffectAllowed {
				t.Fatalf("stale boundary assessment=%+v err=%v", assessment, err)
			}
			if (capability != nil) != test.wantCapability {
				t.Fatalf("capability present=%t, want %t at %s", capability != nil, test.wantCapability, test.now.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestP6Slice3RepairAOpaqueProofMutationIsolation(t *testing.T) {
	first, _ := canonicalSupervisorAttempts(t)

	slotMutations := []struct {
		name   string
		mutate func(*VerifiedSupervisorClaimSlotCommit)
	}{
		{name: "valid", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.valid = false }},
		{name: "commit verified", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.commitVerified = false }},
		{name: "tuple unique", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.tupleUnique = false }},
		{name: "slot unique", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.slotUnique = false }},
		{name: "attempt", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.attemptHash = testHash("mutated-attempt") }},
		{name: "phase", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.phase = AdapterClaimPhase }},
		{name: "sequence", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.sequence = 2 }},
		{name: "slot", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.slotID = "mutated_slot" }},
		{name: "journal head", mutate: func(value *VerifiedSupervisorClaimSlotCommit) {
			value.journalHeadHash = testHash("mutated-journal-head")
		}},
		{name: "boot epoch", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.bootEpochHash = testHash("mutated-boot") }},
		{name: "claim", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.claimHash = testHash("mutated-claim") }},
		{name: "authorization", mutate: func(value *VerifiedSupervisorClaimSlotCommit) {
			value.authorizationHash = testHash("mutated-authorization")
		}},
		{name: "approval", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.approvalHash = testHash("mutated-approval") }},
		{name: "request", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.requestHash = testHash("mutated-request") }},
		{name: "dispatch", mutate: func(value *VerifiedSupervisorClaimSlotCommit) { value.dispatchHash = testHash("mutated-dispatch") }},
		{name: "canonical", mutate: func(value *VerifiedSupervisorClaimSlotCommit) {
			value.committedCanonical[0] ^= 1
			value.committedCanonicalHash = sha256Digest(value.committedCanonical)
		}},
	}
	for _, mutation := range slotMutations {
		t.Run("slot "+mutation.name, func(t *testing.T) {
			value := cloneSupervisorClaimSlotCommit(*first.slotCommits[0])
			mutation.mutate(&value)
			value.integrityHash = supervisorClaimSlotCommitIntegrityHash(&value)
			assessment, capability, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, &value, nil, nil, first.canonical[0], mustTime(t, first.claims[0].CreatedAt))
			if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil || assessment.EffectAllowed {
				t.Fatalf("rehashed slot mutation assessment=%+v capability=%v err=%v", assessment, capability, err)
			}
		})
	}

	t.Run("slot integrity hash", func(t *testing.T) {
		value := cloneSupervisorClaimSlotCommit(*first.slotCommits[0])
		value.integrityHash = testHash("mutated-slot-integrity")
		_, capability, err := EvaluateSupervisorIntentClaim(first.authorities[0], first.authorization, &value, nil, nil, first.canonical[0], mustTime(t, first.claims[0].CreatedAt))
		if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil {
			t.Fatalf("slot integrity mutation capability=%v err=%v", capability, err)
		}
	})

	terminalMutations := []struct {
		name   string
		mutate func(*VerifiedSupervisorTerminalEvent)
	}{
		{name: "valid", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.valid = false }},
		{name: "terminal verified", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.terminalVerified = false }},
		{name: "attempt", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.attemptHash = testHash("mutated-attempt") }},
		{name: "phase", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.phase = AdapterClaimPhase }},
		{name: "sequence", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.sequence = 2 }},
		{name: "claim", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.claimHash = testHash("mutated-claim") }},
		{name: "event", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.terminalEventHash = testHash("mutated-event") }},
		{name: "boot epoch", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.bootEpochHash = testHash("mutated-boot") }},
		{name: "slot", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.slotID = "mutated_slot" }},
		{name: "claim journal head", mutate: func(value *VerifiedSupervisorTerminalEvent) {
			value.claimJournalHeadHash = testHash("mutated-claim-head")
		}},
		{name: "terminal journal head", mutate: func(value *VerifiedSupervisorTerminalEvent) {
			value.terminalJournalHeadHash = testHash("mutated-terminal-head")
		}},
		{name: "terminal status", mutate: func(value *VerifiedSupervisorTerminalEvent) { value.terminalStatus = "failed" }},
	}
	for _, mutation := range terminalMutations {
		t.Run("terminal "+mutation.name, func(t *testing.T) {
			value := *first.terminalEvents[0]
			mutation.mutate(&value)
			value.integrityHash = supervisorTerminalEventIntegrityHash(&value)
			assessment, capability, err := EvaluateSupervisorIntentClaim(first.authorities[1], first.authorization, nil, first.committedClaim[0], &value, first.canonical[1], mustTime(t, first.claims[1].CreatedAt))
			if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil || assessment.EffectAllowed {
				t.Fatalf("rehashed terminal mutation assessment=%+v capability=%v err=%v", assessment, capability, err)
			}
		})
	}

	t.Run("terminal integrity hash", func(t *testing.T) {
		value := *first.terminalEvents[0]
		value.integrityHash = testHash("mutated-terminal-integrity")
		_, capability, err := EvaluateSupervisorIntentClaim(first.authorities[1], first.authorization, nil, first.committedClaim[0], &value, first.canonical[1], mustTime(t, first.claims[1].CreatedAt))
		if !errors.Is(err, ErrInvalidSupervisorIntent) || capability != nil {
			t.Fatalf("terminal integrity mutation capability=%v err=%v", capability, err)
		}
	})
}

func mintSupervisorClaimSlotCommitForTest(t *testing.T, authority SupervisorIntentAuthority, claim SupervisorIntentClaim, raw []byte, tupleUnique, slotUnique bool) *VerifiedSupervisorClaimSlotCommit {
	t.Helper()
	value := &VerifiedSupervisorClaimSlotCommit{
		valid:                  true,
		commitVerified:         true,
		tupleUnique:            tupleUnique,
		slotUnique:             slotUnique,
		attemptHash:            authority.AttemptHash,
		phase:                  authority.Phase,
		sequence:               authority.Sequence,
		slotID:                 authority.JournalSlotID,
		journalHeadHash:        authority.JournalHeadHash,
		bootEpochID:            authority.BootEpochID,
		bootEpochHash:          authority.BootEpochHash,
		claimID:                claim.ClaimID,
		claimHash:              claim.ClaimHash,
		authorizationHash:      claim.AuthorizationHash,
		approvalHash:           claim.ApprovalHash,
		requestHash:            claim.RequestHash,
		dispatchHash:           claim.DispatchHash,
		committedCanonical:     append([]byte(nil), raw...),
		committedCanonicalHash: sha256Digest(raw),
	}
	value.integrityHash = supervisorClaimSlotCommitIntegrityHash(value)
	return value
}

func mintSupervisorTerminalEventForTest(t *testing.T, predecessor *VerifiedSupervisorIntentClaim, eventHash, terminalJournalHeadHash string) *VerifiedSupervisorTerminalEvent {
	t.Helper()
	value := &VerifiedSupervisorTerminalEvent{
		valid:                   true,
		terminalVerified:        true,
		attemptHash:             predecessor.claim.AttemptHash,
		phase:                   predecessor.claim.Phase,
		sequence:                predecessor.claim.Sequence,
		claimHash:               predecessor.claim.ClaimHash,
		terminalEventHash:       eventHash,
		bootEpochHash:           predecessor.claim.SupervisorBootEpochHash,
		slotID:                  predecessor.claim.JournalSlotID,
		claimJournalHeadHash:    predecessor.claim.JournalHeadHash,
		terminalJournalHeadHash: terminalJournalHeadHash,
		terminalStatus:          "succeeded",
	}
	value.integrityHash = supervisorTerminalEventIntegrityHash(value)
	return value
}
