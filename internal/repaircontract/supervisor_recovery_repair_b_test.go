package repaircontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

type canonicalRecoverySnapshotFixture struct {
	currentBootHash  string
	recordedBootHash string
	attempt          canonicalSupervisorAttempt
	records          [5]SupervisorRecoveryRecord
	canonical        [5][]byte
}

func TestP6Slice3RepairBRejectsCallerForgedRecoveryEvidence(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot *VerifiedSupervisorRecoverySnapshot
	}{
		{name: "nil snapshot", snapshot: nil},
		{name: "zero snapshot", snapshot: &VerifiedSupervisorRecoverySnapshot{}},
		{name: "invented hashes without journal verification", snapshot: &VerifiedSupervisorRecoverySnapshot{
			valid: true, journalVerified: false, durabilityVerified: true, completenessVerified: true, ambiguityChecked: true, unambiguous: true,
			currentBootEpochHash: testHash("invented-current-boot"), recordedBootEpochHash: testHash("invented-recorded-boot"),
			attemptHash: testHash("invented-attempt"), attemptNumber: 1, attemptCap: AttemptCap,
			phase: MaterializationClaimPhase, claimHash: testHash("invented-claim"), slotID: "invented_slot", journalHeadHash: testHash("invented-journal-head"),
			cutPoint: AfterResponsePersistence,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := ClassifySupervisorRecovery(test.snapshot, RecoveryReplayResponse)
			if !errors.Is(err, ErrInvalidSupervisorRecovery) || result != (SupervisorRecoveryResult{}) {
				t.Fatalf("forged recovery evidence classified: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestP6Slice3RepairBCanonicalRecoveryRecordSchema(t *testing.T) {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	for index := range fixture.records {
		decoded, err := DecodeSupervisorRecoveryRecord(fixture.canonical[index])
		if err != nil {
			t.Fatalf("record %d decode: %v", index+1, err)
		}
		if decoded != fixture.records[index] {
			t.Fatalf("record %d decoded value drifted", index+1)
		}
		if !recordHashMatches(decoded, "record_hash", decoded.RecordHash) {
			t.Fatalf("record %d self-hash mismatch", index+1)
		}
	}
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterResponsePersistence)
	retained := cloneVerifiedSupervisorRecoverySnapshotForTest(snapshot)
	fixture.records[0].RecordID = "caller_mutated_record"
	fixture.canonical[0][0] ^= 1
	assertRecoverySnapshotDeepCopy(t, snapshot, retained)
	if _, err := ClassifySupervisorRecovery(snapshot, RecoveryReplayResponse); err != nil {
		t.Fatalf("caller mutation corrupted retained canonical snapshot: %v", err)
	}

}

func TestP6Slice3RepairBRejectsMalformedCanonicalRecoveryRecords(t *testing.T) {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	base := fixture.canonical[0]
	var generic map[string]any
	decoder := json.NewDecoder(bytes.NewReader(base))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	unknown := cloneGeneric(t, generic).(map[string]any)
	unknown["unknown_recovery_member"] = true
	duplicate := append([]byte(`{"record_id":"duplicate",`), base[1:]...)

	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{name: "unknown", raw: canonicalTestArtifact(t, unknown)},
		{name: "duplicate", raw: duplicate},
		{name: "trailing", raw: append(append([]byte(nil), base...), []byte(`{}`)...)},
		{name: "noncanonical", raw: append([]byte{' '}, base...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeSupervisorRecoveryRecord(test.raw); !errors.Is(err, ErrInvalidSupervisorRecovery) {
				t.Fatalf("malformed recovery record error=%v", err)
			}
		})
	}
}

func TestP6Slice3RepairBRejectsWrongRecoveryRecordBindings(t *testing.T) {
	for _, test := range []struct {
		name     string
		cutPoint SupervisorCrashCutPoint
		index    int
		mutate   func(*SupervisorRecoveryRecord)
	}{
		{name: "tampered record hash", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.RecordHash = testHash("tampered-recovery-record") }},
		{name: "wrong sequence", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.Sequence = 2 }},
		{name: "wrong kind", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.Kind = SupervisorRecoveryPhaseLaunchRecord }},
		{name: "wrong attempt", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.AttemptHash = testHash("wrong-recovery-attempt") }},
		{name: "wrong phase", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.Phase = AdapterClaimPhase }},
		{name: "wrong claim", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.ClaimHash = testHash("wrong-recovery-claim") }},
		{name: "wrong slot", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.SlotID = "wrong_recovery_slot" }},
		{name: "wrong boot", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.SupervisorBootEpochHash = testHash("wrong-recovery-boot") }},
		{name: "wrong journal", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.JournalHeadHash = testHash("wrong-recovery-journal") }},
		{name: "wrong predecessor", cutPoint: AfterPhaseLaunch, index: 1, mutate: func(value *SupervisorRecoveryRecord) {
			value.PredecessorRecordHash = testHash("wrong-recovery-predecessor")
		}},
		{name: "wrong durability", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.DurabilityPolicyID = "NORMAL" }},
		{name: "wrong payload", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.SemanticPayloadHash = testHash("wrong-recovery-payload") }},
		{name: "wrong time", cutPoint: AfterClaimCommit, mutate: func(value *SupervisorRecoveryRecord) { value.OccurredAt = "2026-07-26T12:00:01+00:00" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
			snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, test.cutPoint)
			test.mutate(&snapshot.records[test.index])
			result, err := ClassifySupervisorRecovery(snapshot, RecoveryStatusOnly)
			if !errors.Is(err, ErrInvalidSupervisorRecovery) || result != (SupervisorRecoveryResult{}) {
				t.Fatalf("wrong recovery binding classified: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestP6Slice3RepairBRejectsInvalidDurablePrefixes(t *testing.T) {
	for _, test := range []struct {
		name  string
		probe func(*testing.T) error
	}{
		{name: "truncated canonical record", probe: truncatedRecoveryProbe},
		{name: "missing canonical record", probe: missingRecoveryRecordProbe},
		{name: "unknown canonical record", probe: unknownRecoveryRecordProbe},
		{name: "duplicate canonical record", probe: duplicateRecoveryRecordProbe},
		{name: "out of order canonical record", probe: outOfOrderRecoveryRecordProbe},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.probe(t); !errors.Is(err, ErrInvalidSupervisorRecovery) {
				t.Fatalf("invalid durable prefix error=%v", err)
			}
		})
	}
}

func TestP6Slice3RepairBOpaqueSnapshotMutationIsolation(t *testing.T) {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	base := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterClaimCommit)
	mutations := []struct {
		name   string
		mutate func(*VerifiedSupervisorRecoverySnapshot)
	}{
		{name: "valid", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.valid = false }},
		{name: "journal verified", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.journalVerified = false }},
		{name: "durability verified", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.durabilityVerified = false }},
		{name: "completeness verified", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.completenessVerified = false }},
		{name: "ambiguity checked", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.ambiguityChecked = false }},
		{name: "unambiguous", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.unambiguous = false }},
		{name: "current boot", mutate: func(value *VerifiedSupervisorRecoverySnapshot) {
			value.currentBootEpochHash = testHash("mutated-current-boot")
		}},
		{name: "recorded boot", mutate: func(value *VerifiedSupervisorRecoverySnapshot) {
			value.recordedBootEpochHash = testHash("mutated-recorded-boot")
		}},
		{name: "attempt", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.attemptHash = testHash("mutated-attempt") }},
		{name: "attempt number", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.attemptNumber = 2 }},
		{name: "attempt cap", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.attemptCap = 1 }},
		{name: "phase", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.phase = AdapterClaimPhase }},
		{name: "claim", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.claimHash = testHash("mutated-claim") }},
		{name: "slot", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.slotID = "mutated_slot" }},
		{name: "journal head", mutate: func(value *VerifiedSupervisorRecoverySnapshot) {
			value.journalHeadHash = testHash("mutated-journal-head")
		}},
		{name: "cut point", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.cutPoint = AfterPhaseLaunch }},
		{name: "record value", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.records[0].RecordID = "mutated_record" }},
		{name: "canonical byte", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.canonicalRecords[0][0] ^= 1 }},
		{name: "canonical hash", mutate: func(value *VerifiedSupervisorRecoverySnapshot) {
			value.canonicalRecordHashes[0] = testHash("mutated-canonical")
		}},
		{name: "integrity hash", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.integrityHash = testHash("mutated-integrity") }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			value := cloneVerifiedSupervisorRecoverySnapshotForTest(base)
			mutation.mutate(value)
			result, err := ClassifySupervisorRecovery(value, RecoveryStatusOnly)
			if !errors.Is(err, ErrInvalidSupervisorRecovery) || result != (SupervisorRecoveryResult{}) {
				t.Fatalf("mutated snapshot classified: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestP6Slice3RepairBRejectsAliasedAttemptPhaseChain(t *testing.T) {
	first := canonicalRecoverySnapshotFixtureForTest(t, false)
	_, second := canonicalSupervisorAttempts(t)

	for _, test := range []struct {
		name   string
		mutate func(*VerifiedSupervisorRecoverySnapshot)
	}{
		{name: "attempt alias", mutate: func(value *VerifiedSupervisorRecoverySnapshot) {
			value.attemptHash = second.claims[0].AttemptHash
			value.attemptNumber = 2
		}},
		{name: "phase alias", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.phase = AdapterClaimPhase }},
		{name: "slot alias", mutate: func(value *VerifiedSupervisorRecoverySnapshot) { value.slotID = second.claims[0].JournalSlotID }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := mintVerifiedSupervisorRecoverySnapshotForTest(t, first, AfterClaimCommit)
			test.mutate(value)
			value.integrityHash = supervisorRecoverySnapshotIntegrityHash(value)
			if result, err := ClassifySupervisorRecovery(value, RecoveryStatusOnly); !errors.Is(err, ErrInvalidSupervisorRecovery) || result != (SupervisorRecoveryResult{}) {
				t.Fatalf("aliased chain classified: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestP6Slice3RepairBResponseReplayExactness(t *testing.T) {
	fixture := canonicalRecoverySnapshotFixtureForTest(t, false)
	snapshot := mintVerifiedSupervisorRecoverySnapshotForTest(t, fixture, AfterResponsePersistence)
	result, err := ClassifySupervisorRecovery(snapshot, RecoveryReplayResponse)
	want := SupervisorRecoveryResult{DurableObservation: DurableResponsePersisted, Disposition: RecoveryResponseReplay, EffectAllowed: false, NextRequirement: NoFurtherEffectPermitted}
	if err != nil || result != want {
		t.Fatalf("response replay result=%+v err=%v", result, err)
	}
	for _, action := range []SupervisorRecoveryAction{RecoveryStatusOnly, SupervisorRecoveryAction("launch"), SupervisorRecoveryAction("replay_response_and_effect")} {
		result, err := ClassifySupervisorRecovery(snapshot, action)
		if !errors.Is(err, ErrInvalidSupervisorRecovery) || result != (SupervisorRecoveryResult{}) {
			t.Fatalf("invalid response action %q result=%+v err=%v", action, result, err)
		}
	}
}

func canonicalRecoverySnapshotFixtureForTest(t *testing.T, currentEpoch bool) canonicalRecoverySnapshotFixture {
	t.Helper()
	first, _ := canonicalSupervisorAttempts(t)
	currentBoot := testHash("trusted-supervisor-restart-boot-epoch")
	recordedBoot := first.claims[0].SupervisorBootEpochHash
	if currentEpoch {
		recordedBoot = currentBoot
	}
	fixture := canonicalRecoverySnapshotFixture{
		currentBootHash: currentBoot, recordedBootHash: recordedBoot, attempt: first,
	}
	journalHeadPrefix := "trusted-recovery-journal-head-attempt-1-materialization-"
	occurredAt := mustTime(t, first.claims[0].CreatedAt)
	kinds := [...]SupervisorRecoveryRecordKind{
		SupervisorRecoveryClaimCommitRecord,
		SupervisorRecoveryPhaseLaunchRecord,
		SupervisorRecoveryTerminalProofRecord,
		SupervisorRecoveryAttestationSignatureRecord,
		SupervisorRecoveryResponseRecord,
	}
	predecessorHash := ""
	for index, kind := range kinds {
		record := SupervisorRecoveryRecord{
			SchemaVersion:           SupervisorRecoveryRecordSchemaVersion,
			RecordID:                "attempt_1_materialization_recovery_" + string(rune('1'+index)),
			Sequence:                index + 1,
			Kind:                    kind,
			AttemptHash:             first.claims[0].AttemptHash,
			AttemptNumber:           1,
			AttemptCap:              AttemptCap,
			Phase:                   MaterializationClaimPhase,
			ClaimHash:               first.claims[0].ClaimHash,
			SlotID:                  first.claims[0].JournalSlotID,
			SupervisorBootEpochHash: recordedBoot,
			JournalHeadHash:         testHash(journalHeadPrefix + string(rune('1'+index))),
			PredecessorRecordHash:   predecessorHash,
			DurabilityPolicyID:      DurabilityPolicyFullFullFSync,
			OccurredAt:              occurredAt.Add(time.Duration(index+1) * time.Second).UTC().Format(time.RFC3339Nano),
			SemanticPayloadHash:     testHash("recovery-payload-" + string(kind)),
		}
		record.RecordHash = mustRecordHash(t, record, "record_hash")
		fixture.records[index] = record
		fixture.canonical[index] = canonicalTestArtifact(t, record)
		predecessorHash = record.RecordHash
	}
	return fixture
}

func mintVerifiedSupervisorRecoverySnapshotForTest(t *testing.T, fixture canonicalRecoverySnapshotFixture, cutPoint SupervisorCrashCutPoint) *VerifiedSupervisorRecoverySnapshot {
	t.Helper()
	count, _, valid := recoveryPrefixForCutPoint(cutPoint)
	if !valid {
		t.Fatalf("unknown recovery cut point %q", cutPoint)
	}
	claim := fixture.attempt.claims[0]
	journalHeadHash := testHash("trusted-empty-recovery-journal-head-attempt-1-materialization")
	if count > 0 {
		journalHeadHash = fixture.records[count-1].JournalHeadHash
	}
	value := &VerifiedSupervisorRecoverySnapshot{
		valid: true, journalVerified: true, durabilityVerified: true, completenessVerified: true, ambiguityChecked: true, unambiguous: true,
		currentBootEpochHash: fixture.currentBootHash, recordedBootEpochHash: fixture.recordedBootHash,
		attemptHash: claim.AttemptHash, attemptNumber: claim.AttemptNumber, attemptCap: claim.AttemptCap,
		phase: claim.Phase, claimHash: claim.ClaimHash, slotID: claim.JournalSlotID,
		journalHeadHash: journalHeadHash, cutPoint: cutPoint,
		records: make([]SupervisorRecoveryRecord, count), canonicalRecords: make([][]byte, count), canonicalRecordHashes: make([]string, count),
	}
	copy(value.records, fixture.records[:count])
	for index := range count {
		value.canonicalRecords[index] = append([]byte(nil), fixture.canonical[index]...)
		value.canonicalRecordHashes[index] = sha256Digest(value.canonicalRecords[index])
	}
	value.integrityHash = supervisorRecoverySnapshotIntegrityHash(value)
	return value
}

func cloneVerifiedSupervisorRecoverySnapshotForTest(value *VerifiedSupervisorRecoverySnapshot) *VerifiedSupervisorRecoverySnapshot {
	if value == nil {
		return nil
	}
	clone := *value
	clone.records = append([]SupervisorRecoveryRecord(nil), value.records...)
	clone.canonicalRecordHashes = append([]string(nil), value.canonicalRecordHashes...)
	clone.canonicalRecords = make([][]byte, len(value.canonicalRecords))
	for index := range value.canonicalRecords {
		clone.canonicalRecords[index] = append([]byte(nil), value.canonicalRecords[index]...)
	}
	return &clone
}

func assertRecoverySnapshotDeepCopy(t *testing.T, got, want *VerifiedSupervisorRecoverySnapshot) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot drifted after caller mutation")
	}
}
