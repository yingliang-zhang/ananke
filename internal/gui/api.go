package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/repairrunner"
	"github.com/yingliang-zhang/ananke/internal/repairverifier"
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
	AdapterType  string `json:"adapter_type"` // "fake" or "omp" (default: omp)
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

// RepairConfig holds the default OMP adapter settings for the GUI.
type RepairConfig struct {
	WrapperPath string // path to omp_with_timeout.sh
	Provider    string // e.g. "custom:sudo-kimi-k3"
	Model       string // e.g. "t9s/kimi-k3"
	Timeout     int    // adapter timeout in seconds
}

// API is the GUI API server. It provides HTTP endpoints for the Tauri 2
// frontend to interact with the controlled-repair flow.
type API struct {
	mu        sync.Mutex
	store     *store.Store
	server    *http.Server
	addr      string
	repairCfg RepairConfig
	repairs   map[string]*RepairJob // track running repairs by job ID
}

// RepairJob tracks an async repair submission.
type RepairJob struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"` // "running", "completed", "failed"
	AttestationHash string    `json:"attestation_hash,omitempty"`
	DiffPath        string    `json:"diff_path,omitempty"`
	Error           string    `json:"error,omitempty"`
	StartedAt       time.Time `json:"started_at"`
}

// NewAPI creates a new GUI API server bound to the given address.
func NewAPI(s *store.Store, addr string, cfg RepairConfig) *API {
	return &API{
		store:     s,
		addr:      addr,
		repairCfg: cfg,
		repairs:   make(map[string]*RepairJob),
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
	mux.HandleFunc("/api/repair/job", a.handleJob)
	mux.HandleFunc("/api/repair/diff", a.handleDiff)
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
	// R1-10 fix: CSRF protection on submit (same as review).
	if r.Header.Get("Content-Type") != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
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
	if req.AdapterType == "" {
		req.AdapterType = "omp"
	}

	// R1-11 fix: use nanosecond-resolution job ID to prevent same-second collisions.
	jobID := "job_" + time.Now().UTC().Format("20060102_150405.000000000")
	job := &RepairJob{
		ID:        jobID,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}
	a.mu.Lock()
	a.repairs[jobID] = job
	a.mu.Unlock()

	// Run repair flow in goroutine.
	go a.runRepair(jobID, req)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "started",
		"job_id":  jobID,
		"message": fmt.Sprintf("repair started with adapter=%s model=%s", req.AdapterType, a.repairCfg.Model),
	})
}

// runRepair executes the full repair flow asynchronously.
func (a *API) runRepair(jobID string, req RepairSubmissionRequest) {
	now := time.Now().UTC()

	// Helper to update job status.
	updateJob := func(status, hash, diffPath, errMsg string) {
		a.mu.Lock()
		if j, ok := a.repairs[jobID]; ok {
			j.Status = status
			j.AttestationHash = hash
			j.DiffPath = diffPath
			j.Error = errMsg
		}
		a.mu.Unlock()
	}

	// 1. Generate signing material.
	material, err := repairverifier.GenerateSigningMaterial(now)
	if err != nil {
		updateJob("failed", "", "", fmt.Sprintf("signing material: %v", err))
		return
	}
	defer material.Close()

	// 2. Materialize worktree.
	slotDir, err := os.MkdirTemp("", "ananke-gui-repair-")
	if err != nil {
		updateJob("failed", "", "", fmt.Sprintf("create slot dir: %v", err))
		return
	}
	defer os.RemoveAll(slotDir)

	absRepo, err := filepath.Abs(req.ProjectPath)
	if err != nil {
		updateJob("failed", "", "", fmt.Sprintf("abs path: %v", err))
		return
	}

	parentOut, err := exec.Command("git", "-C", absRepo, "rev-parse", "HEAD").Output()
	if err != nil {
		updateJob("failed", "", "", fmt.Sprintf("git rev-parse: %v", err))
		return
	}
	parentCommit := string(parentOut[:len(parentOut)-1]) // trim newline

	slotPath := filepath.Join(slotDir, "worktree")
	desc := repairrunner.WorktreeDescriptor{
		RepositoryRoot: absRepo,
		ParentCommit:   parentCommit,
		TargetRef:      "refs/heads/feat/ananke-repair",
		SlotID:         "slot_gui_" + now.Format("20060102_150405"),
		SlotPath:       slotPath,
	}
	worktree, err := repairrunner.MaterializeWorktree(desc)
	if err != nil {
		updateJob("failed", "", "", fmt.Sprintf("materialize worktree: %v", err))
		return
	}
	defer repairrunner.RemoveWorktree(slotPath)

	// 3. Run adapter.
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())
	var adapter *repairrunner.AdapterResult

	switch req.AdapterType {
	case "fake":
		adapter, err = repairrunner.RunFakeAdapter(slotPath, uid, gid)
		if err != nil {
			updateJob("failed", "", "", fmt.Sprintf("fake adapter: %v", err))
			return
		}
	case "omp":
		promptDir, err := os.MkdirTemp("", "ananke-gui-omp-")
		if err != nil {
			updateJob("failed", "", "", fmt.Sprintf("create prompt dir: %v", err))
			return
		}
		defer os.RemoveAll(promptDir)
		promptPath := filepath.Join(promptDir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte(req.RequestText), 0o644); err != nil {
			updateJob("failed", "", "", fmt.Sprintf("write prompt: %v", err))
			return
		}
		outputPath := filepath.Join(promptDir, "output.md")
		sessionDir := filepath.Join(promptDir, "session")
		adapter, err = repairrunner.RunOMPAdapter(slotPath, uid, gid, repairrunner.OMPAdapterConfig{
			WrapperPath: a.repairCfg.WrapperPath,
			Workflow:    "coupled-v1",
			Provider:    a.repairCfg.Provider,
			Model:       a.repairCfg.Model,
			TaskTier:    "normal",
			Role:        "implement",
			RunID:       "ananke-gui-" + now.Format("20060102_150405"),
			SessionDir:  sessionDir,
			Timeout:     a.repairCfg.Timeout,
			PromptPath:  promptPath,
			OutputPath:  outputPath,
			Workdir:     slotPath,
		})
		if err != nil {
			updateJob("failed", "", "", fmt.Sprintf("OMP adapter: %v", err))
			return
		}
	default:
		updateJob("failed", "", "", fmt.Sprintf("unknown adapter: %s", req.AdapterType))
		return
	}

	// 4. Compute diff closure + save diff patch.
	diff, err := repairrunner.ComputeDiffClosure(slotPath)
	if err != nil {
		updateJob("failed", "", "", fmt.Sprintf("diff closure: %v", err))
		return
	}
	worktree.Diff = diff

	// Save git diff to a temp file for download.
	diffDir, _ := os.MkdirTemp("", "ananke-diff-")
	defer os.RemoveAll(diffDir) // R1-05 fix: clean up diff temp dir
	diffPath := filepath.Join(diffDir, "repair.patch")
	// R1-02 fix: git add -N to include untracked files, then git diff HEAD.
	exec.Command("git", "-C", slotPath, "add", "-N", ".").Run()
	diffBytes, _ := exec.Command("git", "-C", slotPath, "diff", "HEAD").Output()
	if len(diffBytes) > 0 {
		os.WriteFile(diffPath, diffBytes, 0o644)
	}

	// 5. Run Go test profile.
	var testResult *repairrunner.TestProfileResult
	if _, err := os.Stat(filepath.Join(slotPath, "go.mod")); err == nil {
		testResult, err = repairrunner.RunGoTestProfile(slotPath, uid, gid, []string{"go", "test", "./...", "-count=1", "-timeout", "60s"})
		if err != nil {
			testResult = skipTestResult(adapter.TerminalProofHash, err.Error(), false)
		}
	} else {
		testResult = skipTestResult(adapter.TerminalProofHash, "skipped: no go.mod", true)
	}

	// 6. Produce signed attestation.
	repairCtx := repairrunner.RepairContext{
		AuthorizationHash:                repairrunner.HashString("ananke-gui-auth"),
		ApprovalHash:                     repairrunner.HashString("ananke-gui-approval"),
		RequestHash:                      repairrunner.HashString(req.RequestText),
		DispatchHash:                     repairrunner.HashString("ananke-gui-dispatch"),
		AttemptHash:                      repairrunner.HashString("ananke-gui-attempt-1"),
		AttemptNumber:                    1,
		AttemptCap:                       repaircontract.AttemptCap,
		ReleasePinsHash:                  material.Verifier().Pins().ReleasePinsHash,
		TrustBundleHash:                  material.Verifier().Bundle().TrustBundleHash,
		RepairAttestorCertificateHash:    material.Verifier().ExpectedAttestorCertificateHash(),
		RepairAttestorRootID:             material.RootID(),
		RepairAttestorLeafSPKI:           material.SignerSPKI(),
		RequestNonceHash:                 repairrunner.HashString("nonce_req"),
		ResponseNonceHash:                repairrunner.HashString("nonce_resp"),
		ChannelHash:                      repairrunner.HashString("channel"),
		RepositoryBindingHash:            repairrunner.HashString(absRepo),
		RepositoryIdentityHash:           repairrunner.HashString(filepath.Base(absRepo)),
		CommonGitIdentityHash:            repairrunner.HashString("git_identity"),
		GitExecutableIdentityHash:        repairrunner.HashString("git_exec"),
		EffectTimeValidationTimestamp:    now.Format(time.RFC3339Nano),
		MaterializationClaimHash:         repairrunner.HashString("mat_claim"),
		AdapterClaimHash:                 repairrunner.HashString("adapter_claim"),
		TestClaimHash:                    repairrunner.HashString("test_claim"),
		PredecessorClaimHash:             repairrunner.HashString("pred_claim"),
		SupervisorJournalHeadHash:        repairrunner.HashString("journal_head"),
		SupervisorJournalPredecessorHash: repairrunner.HashString("journal_pred"),
		BootEpochID:                      "boot_epoch_repair_v1",
		BootEpochHash:                    repairrunner.HashString("boot_epoch"),
	}
	row, err := repairrunner.ProduceSignedAttestation(repairCtx, worktree, adapter, testResult, material, a.store, now)
	if err != nil {
		updateJob("failed", "", "", fmt.Sprintf("signed attestation: %v", err))
		return
	}

	updateJob("completed", row.AttestationHash, diffPath, "")
}

func (a *API) handleJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id parameter is required"})
		return
	}
	a.mu.Lock()
	job, ok := a.repairs[jobID]
	// R1-11 fix: snapshot the struct under lock to avoid data race.
	snapshot := RepairJob{}
	if ok {
		snapshot = *job
	}
	a.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (a *API) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id parameter is required"})
		return
	}
	a.mu.Lock()
	job, ok := a.repairs[jobID]
	diffPath := ""
	if ok {
		diffPath = job.DiffPath
	}
	a.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	if diffPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no diff available"})
		return
	}
	data, err := os.ReadFile(diffPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("read diff: %v", err)})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=repair.patch")
	w.Write(data)
}

// skipTestResult creates a TestProfileResult for skipped/failed tests.
// R1-06 fix: Pass=false on error, Pass=true only for "no go.mod" skip.
func skipTestResult(proofHash, msg string, pass bool) *repairrunner.TestProfileResult {
	return &repairrunner.TestProfileResult{
		ToolchainManifestHash: repairrunner.HashString("go_test"),
		TestProfileHash:       repairrunner.HashString("go test"),
		TestTerminalProofHash: proofHash,
		TestResultHash:        repairrunner.HashString("SKIP"),
		TestOutputHash:        repairrunner.HashString(msg),
		TestCommandHash:       repairrunner.HashString("skip"),
		TestCapabilityHash:    repairrunner.HashString("test_capability"),
		Pass:                  pass,
		Output:                msg,
	}
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
	} else {
		// R1-02 fix: reject should abandon the outbox (matching CLI behavior).
		if err := a.store.AbandonRepairAttestationOutbox(ctx, req.AttestationHash, "rejected by "+req.ReviewerName); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to abandon"})
			return
		}
	}
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
    <div class="form-group"><label>Adapter</label>
      <label style="display:inline;margin-left:10px"><input type="radio" name="adapterType" value="omp" checked style="width:auto"> OMP (real model)</label>
      <label style="display:inline;margin-left:10px"><input type="radio" name="adapterType" value="fake" style="width:auto"> Fake (test)</label>
    </div>
    <button class="primary" onclick="submitRepair()">Submit Repair</button>
    <div id="jobResult" style="margin-top:15px"></div>
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
  const body = { project_path: document.getElementById('projectPath').value, request_text: document.getElementById('requestText').value, operator_name: document.getElementById('operatorName').value, adapter_type: document.querySelector('input[name=adapterType]:checked').value };
  try {
    const data = await api('/api/repair/submit', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    showToast('Repair started: ' + escapeHtml(data.message));
    document.getElementById('requestText').value = '';
    pollJob(data.job_id);
  } catch {}
}
async function pollJob(jobId) {
  const el = document.getElementById('jobResult');
  el.innerHTML = '<p>Repair running... (job: ' + escapeHtml(jobId) + ')</p>';
  const poll = async () => {
    try {
      const data = await api('/api/repair/job?id=' + encodeURIComponent(jobId));
      if (data.status === 'running') {
        el.innerHTML = '<p>Repair running... (started: ' + escapeHtml(data.started_at) + ')</p>';
        setTimeout(poll, 3000);
      } else if (data.status === 'completed') {
        el.innerHTML = '<table><tr><th>Job</th><td>' + escapeHtml(data.id) + '</td></tr><tr><th>Status</th><td><span class="status delivered">Completed</span></td></tr><tr><th>Attestation Hash</th><td>' + escapeHtml(data.attestation_hash) + '</td></tr></table>'
          + '<div class="actions" style="margin-top:10px"><a href="/api/repair/diff?id=' + encodeURIComponent(jobId) + '" download="repair.patch"><button>Download Diff Patch</button></a>'
          + '<button onclick="viewDiff(\'' + escapeHtml(jobId) + '\')">View Diff</button></div>'
          + '<div id="diffView" style="margin-top:10px"></div>';
        document.getElementById('statusHash').value = data.attestation_hash;
        showToast('Repair completed!');
      } else {
        el.innerHTML = '<p><span class="status abandoned">Failed</span>: ' + escapeHtml(data.error) + '</p>';
      }
    } catch { setTimeout(poll, 5000); }
  };
  poll();
}
async function viewDiff(jobId) {
  try {
    const resp = await fetch('/api/repair/diff?id=' + encodeURIComponent(jobId));
    const text = await resp.text();
    document.getElementById('diffView').innerHTML = '<label>Diff Patch</label><div class="evidence">' + escapeHtml(text) + '</div>';
  } catch(e) { showToast('Failed to load diff: ' + e.message, true); }
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
