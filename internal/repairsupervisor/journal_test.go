package repairsupervisor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
)

func newTestJournal(t *testing.T) *Journal {
	t.Helper()
	dir := t.TempDir()
	j, err := Open(filepath.Join(dir, "test_journal.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

func testClaim(seq int, attemptHash string) repaircontract.SupervisorIntentClaim {
	phase := phaseBySequence(seq)
	return repaircontract.SupervisorIntentClaim{
		SchemaVersion:     repaircontract.SupervisorIntentClaimSchemaVersion,
		ClaimHash:         "sha256:claim_" + attemptHash + "_" + string(phase),
		ClaimID:           "claim_" + string(phase) + "_" + attemptHash,
		Phase:             phase,
		Sequence:          seq,
		AttemptHash:       attemptHash,
		AttemptNumber:     1,
		AttemptCap:        repaircontract.AttemptCap,
		AuthorizationHash: "sha256:auth",
		ApprovalHash:      "sha256:approval",
		RequestHash:       "sha256:request",
		DispatchHash:      "sha256:dispatch",
	}
}

func testBootEpoch(id string) BootEpoch {
	return BootEpoch{
		BootEpochID:   id,
		BootEpochHash: "sha256:epoch_" + id,
		StartedAt:     time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	}
}

func TestJournalOpen(t *testing.T) {
	j := newTestJournal(t)
	if j == nil {
		t.Fatal("Open returned nil")
	}
}

func TestRecordBootEpoch(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	if err := j.RecordBootEpoch(ctx, testBootEpoch("epoch_1")); err != nil {
		t.Fatalf("RecordBootEpoch: %v", err)
	}
}

func TestClaimPhaseMaterialization(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	claim := testClaim(1, "sha256:attempt_1")
	entry, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1")
	if err != nil {
		t.Fatalf("ClaimPhase: %v", err)
	}
	if entry.ClaimHash != claim.ClaimHash {
		t.Errorf("hash mismatch: got %s, want %s", entry.ClaimHash, claim.ClaimHash)
	}
	if entry.Phase != claim.Phase {
		t.Errorf("phase mismatch: got %s, want %s", entry.Phase, claim.Phase)
	}
	if entry.Sequence != 1 {
		t.Errorf("sequence should be 1, got %d", entry.Sequence)
	}
	if entry.Launched {
		t.Error("should not be launched")
	}
}

func TestClaimPhaseFullChain(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	for seq := 1; seq <= 3; seq++ {
		claim := testClaim(seq, "sha256:attempt_chain")
		entry, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1")
		if err != nil {
			t.Fatalf("ClaimPhase seq %d: %v", seq, err)
		}
		if entry.Sequence != seq {
			t.Errorf("seq %d: got sequence %d", seq, entry.Sequence)
		}
	}
}

func TestClaimPhaseIdempotentReplay(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	claim := testClaim(1, "sha256:attempt_replay")
	entry1, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	entry2, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1")
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if entry1.ClaimHash != entry2.ClaimHash {
		t.Error("replay should return same claim")
	}
}

func TestClaimPhaseConflict(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	claim1 := testClaim(1, "sha256:attempt_conflict")
	claim1.ClaimID = "claim_original"
	if _, err := j.ClaimPhase(ctx, claim1, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// Same attempt_hash + phase, but different claim content → conflict.
	claim2 := testClaim(1, "sha256:attempt_conflict")
	claim2.ClaimID = "claim_DIFFERENT"
	claim2.ClaimHash = "sha256:different_hash"
	_, err := j.ClaimPhase(ctx, claim2, "epoch_1", "sha256:epoch_1")
	if !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("expected ErrJournalConflict, got: %v", err)
	}
}

func TestClaimPhasePredecessorMissing(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	// Try to claim adapter (seq 2) without materialization (seq 1).
	claim := testClaim(2, "sha256:attempt_no_pred")
	_, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1")
	if !errors.Is(err, ErrJournalPredecessorMissing) {
		t.Fatalf("expected ErrJournalPredecessorMissing, got: %v", err)
	}
}

func TestGetClaim(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	claim := testClaim(1, "sha256:attempt_get")
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	entry, err := j.GetClaim(ctx, claim.ClaimHash)
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if entry.ClaimHash != claim.ClaimHash {
		t.Errorf("hash mismatch: got %s, want %s", entry.ClaimHash, claim.ClaimHash)
	}
}

func TestGetClaimNotFound(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_, err := j.GetClaim(ctx, "sha256:nonexistent")
	if !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("expected ErrJournalNotFound, got: %v", err)
	}
}

func TestGetClaimByAttemptPhase(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	claim := testClaim(1, "sha256:attempt_ap")
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	entry, err := j.GetClaimByAttemptPhase(ctx, "sha256:attempt_ap", repaircontract.MaterializationClaimPhase)
	if err != nil {
		t.Fatalf("GetClaimByAttemptPhase: %v", err)
	}
	if entry.ClaimHash != claim.ClaimHash {
		t.Errorf("hash mismatch")
	}
}

func TestGetClaimsForAttempt(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	for seq := 1; seq <= 3; seq++ {
		claim := testClaim(seq, "sha256:attempt_list")
		if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err != nil {
			t.Fatalf("claim seq %d: %v", seq, err)
		}
	}

	entries, err := j.GetClaimsForAttempt(ctx, "sha256:attempt_list")
	if err != nil {
		t.Fatalf("GetClaimsForAttempt: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 claims, got %d", len(entries))
	}
	for i, e := range entries {
		if e.Sequence != i+1 {
			t.Errorf("entry %d: sequence %d, want %d", i, e.Sequence, i+1)
		}
	}
}

func TestMarkLaunched(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	claim := testClaim(1, "sha256:attempt_launch")
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if err := j.MarkLaunched(ctx, claim.ClaimHash, "sha256:terminal_1", "phase_launched", claim, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("MarkLaunched: %v", err)
	}

	// Idempotent: re-mark should not error.
	if err := j.MarkLaunched(ctx, claim.ClaimHash, "sha256:terminal_1", "phase_launched", claim, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("re-mark: %v", err)
	}
}

func TestMarkPriorEpochNonterminal(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_2"))

	// Create a claim in epoch_1 (no terminal event).
	claim := testClaim(1, "sha256:attempt_prior")
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Mark prior epoch nonterminal.
	if err := j.MarkPriorEpochNonterminal(ctx, "epoch_2"); err != nil {
		t.Fatalf("MarkPriorEpochNonterminal: %v", err)
	}

	// The claim should now have a terminal event with waiting_for_human.
	// (Verify via checking that a terminal event exists for this claim.)
	var terminalStatus string
	err := j.db.QueryRowContext(ctx,
		`SELECT terminal_status FROM supervisor_terminal_events WHERE claim_hash = ?`,
		claim.ClaimHash).Scan(&terminalStatus)
	if err != nil {
		t.Fatalf("expected terminal event, got: %v", err)
	}
	if terminalStatus != "waiting_for_human" {
		t.Errorf("terminal status: got %s, want waiting_for_human", terminalStatus)
	}
}

func TestClaimImmutability(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	claim := testClaim(1, "sha256:attempt_imm")
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Attempt UPDATE — should be blocked.
	_, err := j.db.ExecContext(ctx,
		`UPDATE supervisor_claims SET terminal_status = 'completed' WHERE claim_hash = ?`,
		claim.ClaimHash)
	if err == nil {
		t.Fatal("UPDATE should be blocked by immutability trigger")
	}

	// Attempt DELETE — should be blocked.
	_, err = j.db.ExecContext(ctx,
		`DELETE FROM supervisor_claims WHERE claim_hash = ?`, claim.ClaimHash)
	if err == nil {
		t.Fatal("DELETE should be blocked by immutability trigger")
	}
}

func TestClaimPhaseRejectsEmptyFields(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	// Empty claim hash.
	claim := testClaim(1, "sha256:attempt_empty")
	claim.ClaimHash = ""
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err == nil {
		t.Fatal("empty claim hash should be rejected")
	}

	// Empty claim ID.
	claim = testClaim(1, "sha256:attempt_empty_id")
	claim.ClaimID = ""
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err == nil {
		t.Fatal("empty claim ID should be rejected")
	}

	// Invalid sequence.
	claim = testClaim(1, "sha256:attempt_bad_seq")
	claim.Sequence = 5
	if _, err := j.ClaimPhase(ctx, claim, "epoch_1", "sha256:epoch_1"); err == nil {
		t.Fatal("invalid sequence should be rejected")
	}
}

func TestJournalHeadHashChaining(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()
	_ = j.RecordBootEpoch(ctx, testBootEpoch("epoch_1"))

	// Seq 1: predecessor hash should be empty.
	claim1 := testClaim(1, "sha256:attempt_chain_hash")
	entry1, err := j.ClaimPhase(ctx, claim1, "epoch_1", "sha256:epoch_1")
	if err != nil {
		t.Fatalf("claim 1: %v", err)
	}
	if entry1.JournalPredecessorHash != "" {
		t.Errorf("seq 1 predecessor should be empty, got %s", entry1.JournalPredecessorHash)
	}
	if entry1.JournalHeadHash == "" {
		t.Error("seq 1 journal head should not be empty")
	}

	// Seq 2: predecessor hash should be seq 1's journal head hash.
	claim2 := testClaim(2, "sha256:attempt_chain_hash")
	entry2, err := j.ClaimPhase(ctx, claim2, "epoch_1", "sha256:epoch_1")
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if entry2.JournalPredecessorHash != entry1.JournalHeadHash {
		t.Errorf("seq 2 predecessor: got %s, want %s", entry2.JournalPredecessorHash, entry1.JournalHeadHash)
	}
	if entry2.JournalHeadHash == "" {
		t.Error("seq 2 journal head should not be empty")
	}
	if entry2.JournalHeadHash == entry1.JournalHeadHash {
		t.Error("journal head hashes should differ between sequences")
	}
}
