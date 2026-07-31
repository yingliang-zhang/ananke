package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/store"
)

// ErrGUI is the sentinel for GUI API errors.
var ErrGUI = errors.New("GUI API error")

// RepairSubmissionRequest is the API request for submitting a repair.
type RepairSubmissionRequest struct {
	ProjectPath  string `json:"project_path"`
	RequestText  string `json:"request_text"`
	OperatorName string `json:"operator_name"`
	ParentCommit string `json:"parent_commit"`
	TargetBranch string `json:"target_branch"`
}

// RepairStatusResponse is the API response for repair status.
type RepairStatusResponse struct {
	AttestationHash string `json:"attestation_hash"`
	State           string `json:"state"`
	AttemptNumber   int    `json:"attempt_number"`
	OutboxDelivered int    `json:"outbox_delivered"`
	IssuedAt        string `json:"issued_at"`
	CreatedAt       string `json:"created_at"`
}

// RepairEvidenceResponse is the API response for repair evidence (attestation details).
type RepairEvidenceResponse struct {
	AttestationHash   string `json:"attestation_hash"`
	AttestationJSON   string `json:"attestation_json"`
	Signature         string `json:"signature"`
	AuthorizationHash string `json:"authorization_hash"`
	AttemptHash       string `json:"attempt_hash"`
	RepositoryBinding string `json:"repository_binding"`
	AdapterSandbox    string `json:"adapter_sandbox"`
	TestResult        string `json:"test_result"`
}

// ReviewActionRequest is the API request for accept/reject actions.
type ReviewActionRequest struct {
	AttestationHash string `json:"attestation_hash"`
	Action          string `json:"action"` // "accept" or "reject"
	ReviewerName    string `json:"reviewer_name"`
	ReviewComment   string `json:"review_comment"`
}

// ReviewActionResponse is the API response for accept/reject actions.
type ReviewActionResponse struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message"`
}

// API is the GUI API server. It provides HTTP endpoints for the Tauri 2
// frontend to interact with the controlled-repair flow.
type API struct {
	mu     sync.Mutex
	store  *store.Store
	server *http.Server
	addr   string
}

// NewAPI creates a new GUI API server bound to the given address.
func NewAPI(s *store.Store, addr string) *API {
	return &API{
		store: s,
		addr:  addr,
	}
}

// Start starts the HTTP server.
func (a *API) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		return fmt.Errorf("%w: server already started", ErrGUI)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/repair/submit", a.handleSubmit)
	mux.HandleFunc("/api/repair/status", a.handleStatus)
	mux.HandleFunc("/api/repair/evidence", a.handleEvidence)
	mux.HandleFunc("/api/repair/review", a.handleReview)
	mux.HandleFunc("/api/health", a.handleHealth)

	a.server = &http.Server{
		Addr:    a.addr,
		Handler: mux,
	}
	go func() { _ = a.server.ListenAndServe() }()
	return nil
}

// Stop stops the HTTP server.
func (a *API) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.server.Shutdown(ctx)
}

func (a *API) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req RepairSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.ProjectPath == "" || req.RequestText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_path and request_text are required"})
		return
	}
	// In the MVP, the actual repair dispatch is handled by the repairrunner.
	// The GUI API just validates the request and returns a placeholder.
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "submitted",
		"message": "repair request submitted, supervisor will process",
	})
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hash parameter is required"})
		return
	}
	ctx := r.Context()
	row, err := a.store.GetRepairAttestation(ctx, hash)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "attestation not found"})
		return
	}
	writeJSON(w, http.StatusOK, RepairStatusResponse{
		AttestationHash: row.AttestationHash,
		State:           row.State,
		AttemptNumber:   row.AttemptNumber,
		OutboxDelivered: row.OutboxDelivered,
		IssuedAt:        row.IssuedAt,
		CreatedAt:       row.CreatedAt.Format(time.RFC3339Nano),
	})
}

func (a *API) handleEvidence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hash parameter is required"})
		return
	}
	ctx := r.Context()
	row, err := a.store.GetRepairAttestation(ctx, hash)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "attestation not found"})
		return
	}
	// Parse the attestation JSON to extract evidence fields.
	var record repaircontract.RepairReviewAttestation
	_ = json.Unmarshal([]byte(row.AttestationJSON), &record)

	writeJSON(w, http.StatusOK, RepairEvidenceResponse{
		AttestationHash:   row.AttestationHash,
		AttestationJSON:   row.AttestationJSON,
		Signature:         row.SignatureHash,
		AuthorizationHash: row.AuthorizationHash,
		AttemptHash:       row.AttemptHash,
	})
}

func (a *API) handleReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ReviewActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.AttestationHash == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "attestation_hash and action are required"})
		return
	}
	if req.Action != "accept" && req.Action != "reject" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be accept or reject"})
		return
	}
	// Verify the attestation exists.
	ctx := r.Context()
	_, err := a.store.GetRepairAttestation(ctx, req.AttestationHash)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "attestation not found"})
		return
	}
	// Acknowledge the outbox if accepted.
	if req.Action == "accept" {
		if err := a.store.AcknowledgeRepairAttestationOutbox(ctx, req.AttestationHash); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to acknowledge"})
			return
		}
	}
	// In the MVP, reject does not modify the attestation (it stays in waiting_for_review).
	writeJSON(w, http.StatusOK, ReviewActionResponse{
		Accepted: req.Action == "accept",
		Message:  fmt.Sprintf("repair %s by %s", req.Action, req.ReviewerName),
	})
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
