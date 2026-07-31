package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
)

func testAttestation(hash, id string) repaircontract.RepairReviewAttestation {
	return repaircontract.RepairReviewAttestation{
		SchemaVersion:                 repaircontract.AttestationSchemaVersion,
		AttestationHash:               hash,
		AttestationID:                 id,
		IssuedAt:                      "2026-07-31T12:00:00Z",
		State:                         repaircontract.AttestationWaitingForReview,
		SignatureDomain:               repaircontract.SignatureDomain,
		Signature:                     "ed25519:abcdef0123456789",
		ReleasePinsHash:               "sha256:aaa",
		TrustBundleHash:               "sha256:bbb",
		RepairAttestorCertificateHash: "sha256:ccc",
		RepairAttestorRootID:          "test_root",
		RepairAttestorLeafSPKI:        "sha256:ddd",
		AuthorizationHash:             "sha256:eee",
		AttemptHash:                   "sha256:fff",
		AttemptNumber:                 1,
		AttemptCap:                    repaircontract.AttemptCap,
	}
}

func TestPersistRepairAttestation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-attestation-1", "attestation_1")
	row, err := s.PersistRepairAttestation(ctx, record)
	if err != nil {
		t.Fatalf("PersistRepairAttestation: %v", err)
	}
	if row.AttestationHash != record.AttestationHash {
		t.Errorf("hash mismatch: got %s, want %s", row.AttestationHash, record.AttestationHash)
	}
	if row.AttestationID != record.AttestationID {
		t.Errorf("id mismatch: got %s, want %s", row.AttestationID, record.AttestationID)
	}
	if row.State != string(record.State) {
		t.Errorf("state mismatch: got %s, want %s", row.State, record.State)
	}
	if row.OutboxDelivered != 0 {
		t.Errorf("outbox delivered should be 0 (pending), got %d", row.OutboxDelivered)
	}
	if row.AttestationJSON == "" {
		t.Error("attestation JSON should not be empty")
	}
}

func TestPersistRepairAttestationIdempotentReplay(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-replay-1", "attestation_replay_1")
	row1, err := s.PersistRepairAttestation(ctx, record)
	if err != nil {
		t.Fatalf("first persist: %v", err)
	}

	// Replay with identical record — should return existing row.
	row2, err := s.PersistRepairAttestation(ctx, record)
	if err != nil {
		t.Fatalf("replay persist: %v", err)
	}
	if row1.AttestationHash != row2.AttestationHash {
		t.Errorf("replay hash mismatch: %s vs %s", row1.AttestationHash, row2.AttestationHash)
	}
	if row1.AttestationJSON != row2.AttestationJSON {
		t.Error("replay JSON mismatch")
	}
}

func TestPersistRepairAttestationConflict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record1 := testAttestation("sha256:test-conflict-1", "attestation_conflict_1")
	if _, err := s.PersistRepairAttestation(ctx, record1); err != nil {
		t.Fatalf("first persist: %v", err)
	}

	// Same hash, different content — should conflict.
	record2 := testAttestation("sha256:test-conflict-1", "attestation_conflict_DIFFERENT")
	_, err := s.PersistRepairAttestation(ctx, record2)
	if !errors.Is(err, ErrAttestationConflict) {
		t.Fatalf("expected ErrAttestationConflict, got: %v", err)
	}
}

func TestGetRepairAttestation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-get-1", "attestation_get_1")
	if _, err := s.PersistRepairAttestation(ctx, record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	row, err := s.GetRepairAttestation(ctx, record.AttestationHash)
	if err != nil {
		t.Fatalf("GetRepairAttestation: %v", err)
	}
	if row.AttestationHash != record.AttestationHash {
		t.Errorf("hash mismatch: got %s, want %s", row.AttestationHash, record.AttestationHash)
	}
	if row.AttestationID != record.AttestationID {
		t.Errorf("id mismatch: got %s, want %s", row.AttestationID, record.AttestationID)
	}
}

func TestGetRepairAttestationNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetRepairAttestation(ctx, "sha256:nonexistent")
	if !errors.Is(err, ErrAttestationNotFound) {
		t.Fatalf("expected ErrAttestationNotFound, got: %v", err)
	}
}

func TestAcknowledgeRepairAttestationOutbox(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-ack-1", "attestation_ack_1")
	if _, err := s.PersistRepairAttestation(ctx, record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	if err := s.AcknowledgeRepairAttestationOutbox(ctx, record.AttestationHash); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}

	// Re-acknowledge should fail (already delivered).
	if err := s.AcknowledgeRepairAttestationOutbox(ctx, record.AttestationHash); !errors.Is(err, ErrAttestationNotFound) {
		t.Fatalf("expected ErrAttestationNotFound on re-ack, got: %v", err)
	}

	// Verify delivered state.
	row, err := s.GetRepairAttestation(ctx, record.AttestationHash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.OutboxDelivered != 1 {
		t.Errorf("delivered should be 1, got %d", row.OutboxDelivered)
	}
}

func TestAbandonRepairAttestationOutbox(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-abandon-1", "attestation_abandon_1")
	if _, err := s.PersistRepairAttestation(ctx, record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	if err := s.AbandonRepairAttestationOutbox(ctx, record.AttestationHash, "test abandonment"); err != nil {
		t.Fatalf("Abandon: %v", err)
	}

	// Re-abandon should fail (already abandoned).
	if err := s.AbandonRepairAttestationOutbox(ctx, record.AttestationHash, "test abandonment"); !errors.Is(err, ErrAttestationNotFound) {
		t.Fatalf("expected ErrAttestationNotFound on re-abandon, got: %v", err)
	}

	row, err := s.GetRepairAttestation(ctx, record.AttestationHash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.OutboxDelivered != -1 {
		t.Errorf("delivered should be -1 (abandoned), got %d", row.OutboxDelivered)
	}
}

func TestAbandonRepairAttestationOutboxRejectsEmptyReason(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-abandon-empty", "attestation_abandon_empty")
	if _, err := s.PersistRepairAttestation(ctx, record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	if err := s.AbandonRepairAttestationOutbox(ctx, record.AttestationHash, ""); err == nil {
		t.Fatal("empty reason should be rejected")
	}
}

func TestAbandonRepairAttestationOutboxPersistsDiagnostic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-abandon-diag", "attestation_abandon_diag")
	if _, err := s.PersistRepairAttestation(ctx, record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	if err := s.AbandonRepairAttestationOutbox(ctx, record.AttestationHash, "supervisor process died"); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	row, err := s.GetRepairAttestation(ctx, record.AttestationHash)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.OutboxDelivered != -1 {
		t.Errorf("delivered should be -1 (abandoned), got %d", row.OutboxDelivered)
	}
	if row.DeliveredDiagnostic != "supervisor process died" {
		t.Errorf("diagnostic: got %q, want %q", row.DeliveredDiagnostic, "supervisor process died")
	}
}

func TestGetPendingRepairAttestationOutbox(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record1 := testAttestation("sha256:test-pending-1", "attestation_pending_1")
	record2 := testAttestation("sha256:test-pending-2", "attestation_pending_2")
	if _, err := s.PersistRepairAttestation(ctx, record1); err != nil {
		t.Fatalf("persist 1: %v", err)
	}
	if _, err := s.PersistRepairAttestation(ctx, record2); err != nil {
		t.Fatalf("persist 2: %v", err)
	}

	// Acknowledge first one.
	if err := s.AcknowledgeRepairAttestationOutbox(ctx, record1.AttestationHash); err != nil {
		t.Fatalf("ack 1: %v", err)
	}

	pending, err := s.GetPendingRepairAttestationOutbox(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].AttestationHash != record2.AttestationHash {
		t.Errorf("pending hash mismatch: got %s, want %s", pending[0].AttestationHash, record2.AttestationHash)
	}
}

func TestRepairAttestationImmutability(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	record := testAttestation("sha256:test-immutable-1", "attestation_immutable_1")
	if _, err := s.PersistRepairAttestation(ctx, record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Attempt direct UPDATE — should be blocked by trigger.
	_, err := s.db.ExecContext(ctx,
		`UPDATE repair_review_attestations SET state = 'completed' WHERE attestation_hash = ?`,
		record.AttestationHash)
	if err == nil {
		t.Fatal("UPDATE should be blocked by immutability trigger")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Errorf("expected immutability error, got: %v", err)
	}

	// Attempt direct DELETE — should be blocked by trigger.
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM repair_review_attestations WHERE attestation_hash = ?`,
		record.AttestationHash)
	if err == nil {
		t.Fatal("DELETE should be blocked by immutability trigger")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Errorf("expected immutability error, got: %v", err)
	}
}

func TestPersistRepairAttestationRejectsEmptyFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Empty attestation hash.
	record := testAttestation("", "attestation_empty_hash")
	if _, err := s.PersistRepairAttestation(ctx, record); !errors.Is(err, ErrAttestationInvalid) {
		t.Fatalf("empty hash should return ErrAttestationInvalid, got: %v", err)
	}

	// Empty attestation ID.
	record = testAttestation("sha256:empty-id", "")
	if _, err := s.PersistRepairAttestation(ctx, record); !errors.Is(err, ErrAttestationInvalid) {
		t.Fatalf("empty id should return ErrAttestationInvalid, got: %v", err)
	}

	// Empty signature domain.
	record = testAttestation("sha256:empty-domain", "attestation_empty_domain")
	record.SignatureDomain = ""
	if _, err := s.PersistRepairAttestation(ctx, record); !errors.Is(err, ErrAttestationInvalid) {
		t.Fatalf("empty domain should return ErrAttestationInvalid, got: %v", err)
	}
}

func TestStoreSchemaVersionAfterV15(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version < 15 {
		t.Errorf("schema version should be >= 15, got %d", version)
	}
}
