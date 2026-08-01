// Repair IPC handlers for the Ananke daemon engine.
// These handlers are called via the Unix-socket API and dispatch to
// gui.RunRepair for the actual repair execution.
package lifecycle

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/gui"
)

// repairJobHandle tracks an in-flight or completed repair job.
type repairJobHandle struct {
	mu              sync.Mutex
	status          string // "running", "completed", "failed"
	attestationHash string
	diffPath        string
	errorMsg        string
	startedAt       string
}

// repairJobsMap is the repair job tracker embedded in Engine.
// It uses the same Engine mutex pattern as `active` and `tails`.
type repairJobsMap struct {
	mu   sync.Mutex
	jobs map[string]*repairJobHandle
}

func newRepairJobsMap() *repairJobsMap {
	return &repairJobsMap{jobs: make(map[string]*repairJobHandle)}
}

// handleRepairRequest starts a repair job asynchronously.
func (e *Engine) handleRepairRequest(ctx context.Context, req *apiRequest) apiResponse {
	if req.Root == "" {
		return apiResponse{OK: false, Error: "root (project path) is required"}
	}
	if req.RequestText == "" {
		return apiResponse{OK: false, Error: "request_text is required"}
	}

	adapterType := req.AdapterType
	if adapterType == "" {
		adapterType = "omp"
	}

	// Generate job ID.
	jobID := fmt.Sprintf("repair_%s", time.Now().UTC().Format("20060102_150405.000000000"))

	handle := &repairJobHandle{
		status:    "running",
		startedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	e.repairJobs.mu.Lock()
	if e.repairJobs.jobs == nil {
		e.repairJobs.jobs = make(map[string]*repairJobHandle)
	}
	e.repairJobs.jobs[jobID] = handle
	e.repairJobs.mu.Unlock()

	// Run repair in background goroutine.
	go func() {
		cfg := gui.RepairConfig{
			WrapperPath: defaultOMPWrapperPath(),
			Provider:    defaultOMPProvider(),
			Model:       defaultOMPModel(),
			Timeout:     300,
		}
		result := gui.RunRepair(gui.RepairRequest{
			ProjectPath:  req.Root,
			RequestText:  req.RequestText,
			AdapterType:  adapterType,
			OperatorName: req.OperatorName,
		}, cfg, e.store)

		handle.mu.Lock()
		if result.Error != "" {
			handle.status = "failed"
			handle.errorMsg = result.Error
		} else {
			handle.status = "completed"
			handle.attestationHash = result.AttestationHash
			handle.diffPath = result.DiffPath
		}
		handle.mu.Unlock()
	}()

	return apiResponse{
		OK: true,
		ID: jobID,
		RepairJob: &jsonRepairJob{
			Status:    "running",
			StartedAt: handle.startedAt,
		},
	}
}

// handleRepairPoll returns the status of a repair job.
func (e *Engine) handleRepairPoll(ctx context.Context, req *apiRequest) apiResponse {
	if req.ID == "" {
		return apiResponse{OK: false, Error: "id (job ID) is required"}
	}

	e.repairJobs.mu.Lock()
	handle, ok := e.repairJobs.jobs[req.ID]
	e.repairJobs.mu.Unlock()
	if !ok {
		return apiResponse{OK: false, Error: "repair job not found"}
	}

	handle.mu.Lock()
	defer handle.mu.Unlock()

	return apiResponse{
		OK: true,
		RepairJob: &jsonRepairJob{
			Status:          handle.status,
			AttestationHash: handle.attestationHash,
			DiffPath:        handle.diffPath,
			Error:           handle.errorMsg,
			StartedAt:       handle.startedAt,
		},
	}
}

// handleRepairReview accepts or rejects a repair attestation.
func (e *Engine) handleRepairReview(ctx context.Context, req *apiRequest) apiResponse {
	if req.AttestationHash == "" {
		return apiResponse{OK: false, Error: "attestation_hash is required"}
	}
	if req.Action != "accept" && req.Action != "reject" && req.Action != "ask_changes" {
		return apiResponse{OK: false, Error: "action must be accept, reject, or ask_changes"}
	}

	switch req.Action {
	case "accept":
		if err := e.store.AcknowledgeRepairAttestationOutbox(ctx, req.AttestationHash); err != nil {
			return apiResponse{OK: false, Error: fmt.Sprintf("accept: %v", err)}
		}
		return apiResponse{OK: true, Accepted: true}

	case "reject":
		if err := e.store.AbandonRepairAttestationOutbox(ctx, req.AttestationHash, "rejected by operator"); err != nil {
			return apiResponse{OK: false, Error: fmt.Sprintf("reject: %v", err)}
		}
		return apiResponse{OK: true, Accepted: false}

	case "ask_changes":
		// For MVP, "ask changes" is a no-op on the store side.
		// The frontend will call repair-request again with the message.
		return apiResponse{OK: true, Accepted: false}

	default:
		return apiResponse{OK: false, Error: "unknown action"}
	}
}

// handleRepairMessages returns conversation messages for a repair job.
// MVP: returns a single message with the repair result (diff, attestation).
func (e *Engine) handleRepairMessages(ctx context.Context, req *apiRequest) apiResponse {
	if req.ID == "" {
		return apiResponse{OK: false, Error: "id (job ID) is required"}
	}

	e.repairJobs.mu.Lock()
	handle, ok := e.repairJobs.jobs[req.ID]
	e.repairJobs.mu.Unlock()
	if !ok {
		return apiResponse{OK: false, Error: "repair job not found"}
	}

	handle.mu.Lock()
	defer handle.mu.Unlock()

	// MVP: return a single message representing the repair result.
	messages := []jsonRepairMessage{}
	if handle.status == "completed" {
		messages = append(messages, jsonRepairMessage{
			Type:            "agent_evidence",
			AttestationHash: handle.attestationHash,
			DiffPath:        handle.diffPath,
			Content:         "Repair completed. Attestation signed.",
		})
	} else if handle.status == "failed" {
		messages = append(messages, jsonRepairMessage{
			Type:    "error",
			Content: handle.errorMsg,
		})
	} else {
		messages = append(messages, jsonRepairMessage{
			Type:    "agent_reasoning",
			Content: "Repair in progress...",
		})
	}

	return apiResponse{
		OK:             true,
		RepairMessages: &messages,
	}
}

// handleRepairStatus queries attestation status from the store.
func (e *Engine) handleRepairStatus(ctx context.Context, req *apiRequest) apiResponse {
	if req.AttestationHash == "" {
		return apiResponse{OK: false, Error: "attestation_hash is required"}
	}

	row, err := e.store.GetRepairAttestation(ctx, req.AttestationHash)
	if err != nil {
		return apiResponse{OK: false, Error: fmt.Sprintf("get attestation: %v", err)}
	}

	return apiResponse{
		OK: true,
		RepairJob: &jsonRepairJob{
			Status:          row.State,
			AttestationHash: row.AttestationHash,
			StartedAt:       row.IssuedAt,
		},
	}
}

// handleRepairDiff reads the diff patch file content.
func (e *Engine) handleRepairDiff(ctx context.Context, req *apiRequest) apiResponse {
	if req.DiffPath == "" {
		return apiResponse{OK: false, Error: "diff_path is required"}
	}

	// Constrain to the ananke-diffs directory (security: no arbitrary file read).
	if !isAllowedDiffPath(req.DiffPath) {
		return apiResponse{OK: false, Error: "diff path not allowed"}
	}

	content, err := readFileContent(req.DiffPath)
	if err != nil {
		return apiResponse{OK: false, Error: fmt.Sprintf("read diff: %v", err)}
	}

	return apiResponse{
		OK:          true,
		DiffContent: content,
	}
}

// jsonRepairJob is the JSON representation of a repair job status.
type jsonRepairJob struct {
	Status          string `json:"status"`
	AttestationHash string `json:"attestation_hash,omitempty"`
	DiffPath        string `json:"diff_path,omitempty"`
	Error           string `json:"error,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
}

// jsonRepairMessage is a single message in the repair conversation.
type jsonRepairMessage struct {
	Type            string `json:"type"`
	Content         string `json:"content"`
	AttestationHash string `json:"attestation_hash,omitempty"`
	DiffPath        string `json:"diff_path,omitempty"`
}

// defaultOMPWrapperPath returns the default OMP wrapper path.
func defaultOMPWrapperPath() string {
	home := homeDir()
	return home + "/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh"
}

func defaultOMPProvider() string {
	return "custom:sudo-kimi-k3"
}

func defaultOMPModel() string {
	return "t9s/kimi-k3"
}

func homeDir() string {
	if h := envOr("HOME", ""); h != "" {
		return h
	}
	return "/tmp"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// isAllowedDiffPath validates that a diff path is within the ananke-diffs directory.
func isAllowedDiffPath(path string) bool {
	if path == "" {
		return false
	}
	// Must be under /tmp/ananke-diffs/ or /tmp/ananke-repair-*.patch
	allowedPrefixes := []string{
		"/tmp/ananke-diffs/",
		"/tmp/ananke-repair-",
	}
	for _, prefix := range allowedPrefixes {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			// Reject path traversal.
			if !containsDotDot(path) {
				return true
			}
		}
	}
	return false
}

func containsDotDot(path string) bool {
	for i := 0; i < len(path)-1; i++ {
		if path[i] == '.' && path[i+1] == '.' {
			return true
		}
	}
	return false
}

// readFileContent reads a file and returns its content as a string.
func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
