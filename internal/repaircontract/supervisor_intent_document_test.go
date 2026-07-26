package repaircontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const supervisorIntentDocumentPath = "../../docs/experiments/p6-controlled-repair-supervisor-intent.md"

const (
	supervisorIntentMachineContractStart = "<!-- BEGIN P6 SLICE 3 MACHINE CONTRACT -->\n```json\n"
	supervisorIntentMachineContractEnd   = "\n```\n<!-- END P6 SLICE 3 MACHINE CONTRACT -->"
)

type supervisorIntentDocumentManifest struct {
	SchemaVersion                string                                   `json:"schema_version"`
	Status                       string                                   `json:"status"`
	ClaimSchemaVersion           string                                   `json:"claim_schema_version"`
	AttemptIdentitySchemaVersion string                                   `json:"attempt_identity_schema_version"`
	RecoveryRecordSchemaVersion  string                                   `json:"recovery_record_schema_version"`
	DurabilityPolicyID           string                                   `json:"durability_policy_id"`
	MaxClaimLifetimeNanoseconds  int64                                    `json:"max_claim_lifetime_nanoseconds"`
	PriorSliceVectorCount        int                                      `json:"prior_slice_vector_count"`
	Slice3VectorCount            int                                      `json:"slice_3_vector_count"`
	EffectAllowedValues          []bool                                   `json:"effect_allowed_values"`
	Phases                       []supervisorIntentDocumentPhase          `json:"phases"`
	ClaimDispositions            []SupervisorClaimDisposition             `json:"claim_dispositions"`
	RecoveryActions              []SupervisorRecoveryAction               `json:"recovery_actions"`
	RecoveryDispositions         []SupervisorRecoveryDisposition          `json:"recovery_dispositions"`
	CrashCutPoints               []SupervisorCrashCutPoint                `json:"crash_cut_points"`
	CanonicalFixtures            []supervisorIntentDocumentAttemptFixture `json:"canonical_fixtures"`
	CanonicalRecoveryFixture     supervisorIntentDocumentRecoveryFixture  `json:"canonical_recovery_fixture"`
	VectorIDs                    []string                                 `json:"vector_ids"`
}

type supervisorIntentDocumentPhase struct {
	Phase    SupervisorIntentPhase `json:"phase"`
	Sequence int                   `json:"sequence"`
}

type supervisorIntentDocumentAttemptFixture struct {
	AttemptNumber     int                                    `json:"attempt_number"`
	AttemptHash       string                                 `json:"attempt_hash"`
	AuthorizationHash string                                 `json:"authorization_hash"`
	ApprovalHash      string                                 `json:"approval_hash"`
	DispatchHash      string                                 `json:"dispatch_hash"`
	Claims            []supervisorIntentDocumentClaimFixture `json:"claims"`
}

type supervisorIntentDocumentClaimFixture struct {
	Phase           SupervisorIntentPhase `json:"phase"`
	Sequence        int                   `json:"sequence"`
	ClaimHash       string                `json:"claim_hash"`
	CanonicalSHA256 string                `json:"canonical_sha256"`
}

type supervisorIntentDocumentRecoveryFixture struct {
	CurrentBootEpochHash  string                                          `json:"current_boot_epoch_hash"`
	RecordedBootEpochHash string                                          `json:"recorded_boot_epoch_hash"`
	AttemptHash           string                                          `json:"attempt_hash"`
	Phase                 SupervisorIntentPhase                           `json:"phase"`
	ClaimHash             string                                          `json:"claim_hash"`
	SlotID                string                                          `json:"slot_id"`
	Records               []supervisorIntentDocumentRecoveryRecordFixture `json:"records"`
}

type supervisorIntentDocumentRecoveryRecordFixture struct {
	Sequence        int                          `json:"sequence"`
	Kind            SupervisorRecoveryRecordKind `json:"record_kind"`
	RecordHash      string                       `json:"record_hash"`
	CanonicalSHA256 string                       `json:"canonical_sha256"`
}

func TestP6Slice3NormativeDocumentMatchesTypesFixturesAndInventory(t *testing.T) {
	raw, err := os.ReadFile(supervisorIntentDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, supervisorIntentMachineContractStart)
	end := strings.Index(text, supervisorIntentMachineContractEnd)
	if start < 0 || end < 0 || end <= start || strings.Count(text, supervisorIntentMachineContractStart) != 1 || strings.Count(text, supervisorIntentMachineContractEnd) != 1 {
		t.Fatal("normative document must contain exactly one machine-contract JSON block")
	}
	start += len(supervisorIntentMachineContractStart)
	var got supervisorIntentDocumentManifest
	decoder := json.NewDecoder(strings.NewReader(text[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode normative machine contract: %v", err)
	}
	want := expectedSupervisorIntentDocumentManifest(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("normative machine contract drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func expectedSupervisorIntentDocumentManifest(t *testing.T) supervisorIntentDocumentManifest {
	t.Helper()
	first, second := canonicalSupervisorAttempts(t)
	recovery := canonicalRecoverySnapshotFixtureForTest(t, false)
	return supervisorIntentDocumentManifest{
		SchemaVersion:                "ananke.controlled-repair-supervisor-intent-document.v1",
		Status:                       "repair_a_b_candidate_pending_independent_frozen_source_review",
		ClaimSchemaVersion:           SupervisorIntentClaimSchemaVersion,
		AttemptIdentitySchemaVersion: SupervisorAttemptIdentitySchemaVersion,
		RecoveryRecordSchemaVersion:  SupervisorRecoveryRecordSchemaVersion,
		DurabilityPolicyID:           DurabilityPolicyFullFullFSync,
		MaxClaimLifetimeNanoseconds:  int64(MaxSupervisorIntentLifetime),
		PriorSliceVectorCount:        len(canonicalAcceptanceVectorIDs),
		Slice3VectorCount:            len(canonicalSupervisorIntentVectorIDs),
		EffectAllowedValues:          []bool{false},
		Phases: []supervisorIntentDocumentPhase{
			{Phase: MaterializationClaimPhase, Sequence: 1},
			{Phase: AdapterClaimPhase, Sequence: 2},
			{Phase: TestClaimPhase, Sequence: 3},
		},
		ClaimDispositions: []SupervisorClaimDisposition{
			SupervisorClaimAwaitingCommit,
			SupervisorClaimExactReplay,
			SupervisorClaimConflict,
		},
		RecoveryActions: []SupervisorRecoveryAction{
			RecoveryStatusOnly,
			RecoveryReplayResponse,
		},
		RecoveryDispositions: []SupervisorRecoveryDisposition{
			RecoveryNoDurableClaim,
			RecoveryWaitingForHuman,
			RecoveryCurrentEpochStatusOnly,
			RecoveryTerminalStatus,
			RecoveryResponseReplay,
		},
		CrashCutPoints: []SupervisorCrashCutPoint{
			BeforeClaimCommit,
			AfterClaimCommit,
			BeforePhaseLaunch,
			AfterPhaseLaunch,
			BeforeTerminalProofPersistence,
			AfterTerminalProofPersistence,
			BeforeAttestationSignaturePersistence,
			AfterAttestationSignaturePersistence,
			BeforeResponsePersistence,
			AfterResponsePersistence,
		},
		CanonicalFixtures: []supervisorIntentDocumentAttemptFixture{
			supervisorIntentDocumentFixture(first),
			supervisorIntentDocumentFixture(second),
		},
		CanonicalRecoveryFixture: supervisorIntentDocumentRecoveryFixtureForTest(recovery),
		VectorIDs:                append([]string(nil), canonicalSupervisorIntentVectorIDs[:]...),
	}
}

func supervisorIntentDocumentFixture(attempt canonicalSupervisorAttempt) supervisorIntentDocumentAttemptFixture {
	claims := make([]supervisorIntentDocumentClaimFixture, len(attempt.claims))
	for index, claim := range attempt.claims {
		claims[index] = supervisorIntentDocumentClaimFixture{
			Phase:           claim.Phase,
			Sequence:        claim.Sequence,
			ClaimHash:       claim.ClaimHash,
			CanonicalSHA256: sha256Digest(attempt.canonical[index]),
		}
	}
	return supervisorIntentDocumentAttemptFixture{
		AttemptNumber:     attempt.contract.Authorization.Scope.Attempt.AttemptNumber,
		AttemptHash:       attempt.claims[0].AttemptHash,
		AuthorizationHash: attempt.contract.Authorization.AuthorizationHash,
		ApprovalHash:      attempt.contract.Authorization.ApprovalHash,
		DispatchHash:      attempt.contract.Dispatch.DispatchHash,
		Claims:            claims,
	}
}

func supervisorIntentDocumentRecoveryFixtureForTest(fixture canonicalRecoverySnapshotFixture) supervisorIntentDocumentRecoveryFixture {
	records := make([]supervisorIntentDocumentRecoveryRecordFixture, len(fixture.records))
	for index, record := range fixture.records {
		records[index] = supervisorIntentDocumentRecoveryRecordFixture{
			Sequence:        record.Sequence,
			Kind:            record.Kind,
			RecordHash:      record.RecordHash,
			CanonicalSHA256: sha256Digest(fixture.canonical[index]),
		}
	}
	claim := fixture.attempt.claims[0]
	return supervisorIntentDocumentRecoveryFixture{
		CurrentBootEpochHash:  fixture.currentBootHash,
		RecordedBootEpochHash: fixture.recordedBootHash,
		AttemptHash:           claim.AttemptHash,
		Phase:                 claim.Phase,
		ClaimHash:             claim.ClaimHash,
		SlotID:                claim.JournalSlotID,
		Records:               records,
	}
}
