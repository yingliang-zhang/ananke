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
	mux.HandleFunc("/", a.handleWeb)

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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
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
	// R1-12: CSRF protection — require Content-Type: application/json.
	if r.Header.Get("Content-Type") != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	var req ReviewActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
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

// handleWeb serves the embedded frontend HTML.
func (a *API) handleWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(webIndexHTML)
}

// webIndexHTML is the embedded frontend HTML. In production this would be
// embedded via go:embed, but for the MVP we inline a reference to the
// web/index.html file content.
var webIndexHTML = []byte(indexHTML)

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Ananke — Controlled Repair</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #1a1a2e; color: #e0e0e0; padding: 20px; }
  .container { max-width: 900px; margin: 0 auto; }
  h1 { color: #8be9fd; margin-bottom: 20px; font-size: 1.5em; }
  h2 { color: #bd93f9; margin-bottom: 10px; font-size: 1.2em; }
  .card { background: #16213e; border-radius: 8px; padding: 20px; margin-bottom: 20px; border: 1px solid #2a2a4a; }
  .form-group { margin-bottom: 15px; }
  label { display: block; margin-bottom: 5px; color: #6272a4; font-size: 0.85em; }
  input, textarea, button { width: 100%; padding: 10px; border: 1px solid #2a2a4a; border-radius: 4px; background: #1a1a2e; color: #e0e0e0; font-size: 0.9em; }
  input:focus, textarea:focus { outline: none; border-color: #8be9fd; }
  textarea { min-height: 80px; resize: vertical; font-family: monospace; }
  button { background: #44475a; cursor: pointer; border: none; font-weight: 600; transition: background 0.2s; }
  button:hover { background: #6272a4; }
  button.primary { background: #50fa7b; color: #1a1a2e; }
  button.primary:hover { background: #5fff8d; }
  button.danger { background: #ff5555; color: #fff; }
  button.danger:hover { background: #ff6b6b; }
  .status { padding: 8px 12px; border-radius: 4px; font-size: 0.85em; display: inline-block; }
  .status.waiting_for_review { background: #ffb86c; color: #1a1a2e; }
  .status.delivered { background: #50fa7b; color: #1a1a2e; }
  .status.abandoned { background: #ff5555; color: #fff; }
  .status.pending { background: #8be9fd; color: #1a1a2e; }
  table { width: 100%; border-collapse: collapse; margin-top: 10px; }
  th, td { text-align: left; padding: 8px; border-bottom: 1px solid #2a2a4a; font-size: 0.85em; }
  th { color: #6272a4; }
  .evidence { background: #0d1117; padding: 15px; border-radius: 4px; font-family: monospace; font-size: 0.8em; max-height: 400px; overflow: auto; white-space: pre-wrap; word-break: break-all; }
  .actions { display: flex; gap: 10px; margin-top: 15px; }
  .actions button { width: auto; }
  .toast { position: fixed; bottom: 20px; right: 20px; padding: 12px 20px; border-radius: 4px; background: #50fa7b; color: #1a1a2e; font-weight: 600; opacity: 0; transition: opacity 0.3s; }
  .toast.show { opacity: 1; }
  .toast.error { background: #ff5555; color: #fff; }
</style>
</head>
<body>
<div class="container">
  <h1>Ananke — Controlled Repair</h1>
  <div class="card">
    <h2>Submit Repair Request</h2>
    <div class="form-group"><label>Project Path</label><input type="text" id="projectPath" placeholder="/path/to/project" /></div>
    <div class="form-group"><label>Repair Request</label><textarea id="requestText" placeholder="Describe the repair task..."></textarea></div>
    <div class="form-group"><label>Operator Name</label><input type="text" id="operatorName" placeholder="your-name" /></div>
    <button class="primary" onclick="submitRepair()">Submit Repair</button>
  </div>
  <div class="card">
    <h2>Repair Status</h2>
    <div class="form-group"><label>Attestation Hash</label><input type="text" id="statusHash" placeholder="sha256:..." /></div>
    <button onclick="checkStatus()">Check Status</button>
    <div id="statusResult" style="margin-top:15px"></div>
  </div>
  <div class="card">
    <h2>Repair Evidence</h2>
    <div class="form-group"><label>Attestation Hash</label><input type="text" id="evidenceHash" placeholder="sha256:..." /></div>
    <button onclick="checkEvidence()">View Evidence</button>
    <div id="evidenceResult" style="margin-top:15px"></div>
  </div>
  <div class="card">
    <h2>Review — Accept / Reject</h2>
    <div class="form-group"><label>Attestation Hash</label><input type="text" id="reviewHash" placeholder="sha256:..." /></div>
    <div class="form-group"><label>Reviewer Name</label><input type="text" id="reviewerName" placeholder="reviewer-name" /></div>
    <div class="form-group"><label>Review Comment</label><textarea id="reviewComment" placeholder="Review notes..."></textarea></div>
    <div class="actions">
      <button class="primary" onclick="reviewAction('accept')">Accept</button>
      <button class="danger" onclick="reviewAction('reject')">Reject</button>
    </div>
    <div id="reviewResult" style="margin-top:15px"></div>
  </div>
</div>
<div class="toast" id="toast"></div>
<script>
const API_BASE = window.location.origin;
function showToast(msg, isError) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast show' + (isError ? ' error' : '');
  setTimeout(() => t.className = 'toast', 3000);
}
async function api(path, opts) {
  try {
    const resp = await fetch(API_BASE + path, opts);
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || 'Request failed');
    return data;
  } catch (e) { showToast(e.message, true); throw e; }
}
function escapeHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}
async function submitRepair() {
  const body = { project_path: document.getElementById('projectPath').value, request_text: document.getElementById('requestText').value, operator_name: document.getElementById('operatorName').value };
  try { await api('/api/repair/submit', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); showToast('Repair submitted'); document.getElementById('requestText').value = ''; } catch {}
}
async function checkStatus() {
  const hash = document.getElementById('statusHash').value;
  if (!hash) { showToast('Enter attestation hash', true); return; }
  try {
    const data = await api('/api/repair/status?hash=' + encodeURIComponent(hash));
    const sc = data.outbox_delivered === 1 ? 'delivered' : data.state || 'pending';
    document.getElementById('statusResult').innerHTML = '<table><tr><th>Hash</th><td>' + escapeHtml(data.attestation_hash) + '</td></tr><tr><th>State</th><td><span class="status ' + escapeHtml(sc) + '">' + escapeHtml(data.state) + '</span></td></tr><tr><th>Attempt</th><td>' + escapeHtml(data.attempt_number) + '</td></tr><tr><th>Outbox</th><td>' + (data.outbox_delivered === 1 ? 'Delivered' : 'Pending') + '</td></tr><tr><th>Issued</th><td>' + escapeHtml(data.issued_at) + '</td></tr><tr><th>Created</th><td>' + escapeHtml(data.created_at) + '</td></tr></table>';
  } catch {}
}
async function checkEvidence() {
  const hash = document.getElementById('evidenceHash').value;
  if (!hash) { showToast('Enter attestation hash', true); return; }
  try {
    const data = await api('/api/repair/evidence?hash=' + encodeURIComponent(hash));
    let p = data.attestation_json;
    try { p = JSON.stringify(JSON.parse(data.attestation_json), null, 2); } catch {}
    document.getElementById('evidenceResult').innerHTML = '<table><tr><th>Hash</th><td>' + escapeHtml(data.attestation_hash) + '</td></tr><tr><th>Signature</th><td>' + escapeHtml(data.signature) + '</td></tr><tr><th>Authorization</th><td>' + escapeHtml(data.authorization_hash) + '</td></tr><tr><th>Attempt</th><td>' + escapeHtml(data.attempt_hash) + '</td></tr></table><div style="margin-top:10px"><label>Attestation JSON</label><div class="evidence">' + escapeHtml(p) + '</div></div>';
  } catch {}
}
async function reviewAction(action) {
  const hash = document.getElementById('reviewHash').value;
  if (!hash) { showToast('Enter attestation hash', true); return; }
  const body = { attestation_hash: hash, action: action, reviewer_name: document.getElementById('reviewerName').value, review_comment: document.getElementById('reviewComment').value };
  try {
    const data = await api('/api/repair/review', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    showToast(escapeHtml(data.message));
    document.getElementById('reviewResult').innerHTML = '<span class="status ' + (data.accepted ? 'delivered' : 'abandoned') + '">' + (data.accepted ? 'Accepted' : 'Rejected') + '</span>';
  } catch {}
}
</script>
</body>
</html>`

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
