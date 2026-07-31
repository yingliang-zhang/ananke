package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/store"
)

func newTestAPI(t *testing.T) (*API, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewAPI(s, "127.0.0.1:0", RepairConfig{
		WrapperPath: "/tmp/fake-wrapper.sh",
		Provider:    "custom:sudo",
		Model:       "glm-5.2",
		Timeout:     60,
	}), s
}

func TestAPIHealth(t *testing.T) {
	api, _ := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status: got %s, want ok", resp["status"])
	}
}

func TestAPISubmitValidation(t *testing.T) {
	api, _ := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	// Missing project_path.
	body := `{"request_text": "fix bug"}`
	req := httptest.NewRequest("POST", "/api/repair/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing project_path: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	// Valid request.
	body = `{"project_path": "/tmp/test", "request_text": "fix bug"}`
	req = httptest.NewRequest("POST", "/api/repair/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("valid submit: got %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestAPIStatusNotFound(t *testing.T) {
	api, _ := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	req := httptest.NewRequest("GET", "/api/repair/status?hash=sha256:nonexistent", nil)
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("not found: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIStatusAfterPersist(t *testing.T) {
	api, s := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	// Persist a test attestation.
	record := repaircontract.RepairReviewAttestation{
		SchemaVersion:   repaircontract.AttestationSchemaVersion,
		AttestationHash: "sha256:test-status-1",
		AttestationID:   "attestation_status_1",
		IssuedAt:        "2026-07-31T12:00:00Z",
		State:           repaircontract.AttestationWaitingForReview,
		SignatureDomain: repaircontract.SignatureDomain,
		Signature:       "ed25519:abc",
	}
	if _, err := s.PersistRepairAttestation(context.Background(), record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/repair/status?hash=sha256:test-status-1", nil)
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp RepairStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != string(repaircontract.AttestationWaitingForReview) {
		t.Errorf("state: got %s, want %s", resp.State, repaircontract.AttestationWaitingForReview)
	}
}

func TestAPIReviewAccept(t *testing.T) {
	api, s := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	// Persist a test attestation.
	record := repaircontract.RepairReviewAttestation{
		SchemaVersion:   repaircontract.AttestationSchemaVersion,
		AttestationHash: "sha256:test-review-1",
		AttestationID:   "attestation_review_1",
		IssuedAt:        "2026-07-31T12:00:00Z",
		State:           repaircontract.AttestationWaitingForReview,
		SignatureDomain: repaircontract.SignatureDomain,
		Signature:       "ed25519:abc",
	}
	if _, err := s.PersistRepairAttestation(context.Background(), record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Accept the repair.
	body := `{"attestation_hash": "sha256:test-review-1", "action": "accept", "reviewer_name": "test"}`
	req := httptest.NewRequest("POST", "/api/repair/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("accept: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp ReviewActionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Accepted {
		t.Error("should be accepted")
	}

	// Verify outbox is delivered.
	row, err := s.GetRepairAttestation(context.Background(), "sha256:test-review-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.OutboxDelivered != 1 {
		t.Errorf("delivered: got %d, want 1", row.OutboxDelivered)
	}
}

func TestAPIReviewReject(t *testing.T) {
	api, s := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	// Persist a test attestation.
	record := repaircontract.RepairReviewAttestation{
		SchemaVersion:   repaircontract.AttestationSchemaVersion,
		AttestationHash: "sha256:test-reject-1",
		AttestationID:   "attestation_reject_1",
		IssuedAt:        "2026-07-31T12:00:00Z",
		State:           repaircontract.AttestationWaitingForReview,
		SignatureDomain: repaircontract.SignatureDomain,
		Signature:       "ed25519:abc",
	}
	if _, err := s.PersistRepairAttestation(context.Background(), record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Reject the repair.
	body := `{"attestation_hash": "sha256:test-reject-1", "action": "reject", "reviewer_name": "test"}`
	req := httptest.NewRequest("POST", "/api/repair/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reject: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp ReviewActionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Accepted {
		t.Error("should not be accepted")
	}

	// Verify outbox is abandoned (reject sets outbox_delivered to -1).
	row, err := s.GetRepairAttestation(context.Background(), "sha256:test-reject-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.OutboxDelivered >= 0 {
		t.Errorf("delivered: got %d, want negative (abandoned)", row.OutboxDelivered)
	}
}

func TestAPIReviewInvalidAction(t *testing.T) {
	api, _ := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	body := `{"attestation_hash": "sha256:test", "action": "invalid"}`
	req := httptest.NewRequest("POST", "/api/repair/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid action: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIEvidence(t *testing.T) {
	api, s := newTestAPI(t)
	if err := api.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer api.Stop()

	// Persist a test attestation.
	record := repaircontract.RepairReviewAttestation{
		SchemaVersion:     repaircontract.AttestationSchemaVersion,
		AttestationHash:   "sha256:test-evidence-1",
		AttestationID:     "attestation_evidence_1",
		IssuedAt:          "2026-07-31T12:00:00Z",
		State:             repaircontract.AttestationWaitingForReview,
		SignatureDomain:   repaircontract.SignatureDomain,
		Signature:         "ed25519:abc",
		AuthorizationHash: "sha256:auth",
		AttemptHash:       "sha256:attempt",
	}
	if _, err := s.PersistRepairAttestation(context.Background(), record); err != nil {
		t.Fatalf("persist: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/repair/evidence?hash=sha256:test-evidence-1", nil)
	w := httptest.NewRecorder()
	api.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("evidence: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp RepairEvidenceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AttestationHash != "sha256:test-evidence-1" {
		t.Errorf("hash: got %s, want sha256:test-evidence-1", resp.AttestationHash)
	}
}
