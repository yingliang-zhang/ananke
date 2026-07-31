package repairrunner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ErrOMPAdapter is the sentinel for OMP adapter failures.
var ErrOMPAdapter = errors.New("OMP adapter failed")

// OMPAdapterConfig configures the route-aware OMP repair adapter.
type OMPAdapterConfig struct {
	WrapperPath string // path to omp_with_timeout.sh
	Workflow    string // HERMES_CODING_WORKFLOW value
	Provider    string // --hermes-provider value
	Model       string // --hermes-model value
	TaskTier    string // --task-tier value
	Role        string // --role value (implement, repair, review, audit)
	RunID       string // --run-id value
	SessionDir  string // --session-dir value
	Timeout     int    // OMP internal deadline in seconds
	PromptPath  string // path to the prompt file
	OutputPath  string // path to the output file
	Workdir     string // working directory for OMP
}

// RunOMPAdapter runs the route-aware OMP repair adapter. It invokes the
// omp_with_timeout.sh wrapper to produce real source edits in the worktree.
// The adapter captures the OMP output and computes the same hash structure
// as the fake adapter (Step 6).
//
// This implements Step 11 of the P6a runtime.
func RunOMPAdapter(worktreePath string, uid, gid uint32, config OMPAdapterConfig) (*AdapterResult, error) {
	if err := validateOMPConfig(config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOMPAdapter, err)
	}

	// Ensure session directory exists.
	if config.SessionDir != "" {
		if err := os.MkdirAll(config.SessionDir, 0o755); err != nil {
			return nil, fmt.Errorf("%w: mkdir session dir: %v", ErrOMPAdapter, err)
		}
	}

	// Build the OMP command.
	args := []string{
		fmt.Sprintf("%d", config.Timeout),
		config.PromptPath,
		config.OutputPath,
		"--workflow", config.Workflow,
		"--role", config.Role,
		"--run-id", config.RunID,
		"--hermes-provider", config.Provider,
		"--hermes-model", config.Model,
		"--task-tier", config.TaskTier,
		"--session-dir", config.SessionDir,
	}

	cmd := exec.Command(config.WrapperPath, args...)
	cmd.Dir = config.Workdir
	cmd.Env = append(os.Environ(),
		"HERMES_CODING_WORKFLOW="+config.Workflow,
	)

	// Run OMP with a timeout.
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Read the OMP output file if it exists.
	ompOutput := ""
	if config.OutputPath != "" {
		if data, readErr := os.ReadFile(config.OutputPath); readErr == nil {
			ompOutput = string(data)
		}
	}

	// Compute terminal proof (same as fake adapter).
	terminalProof := computeTerminalProof(uid, gid, worktreePath)

	// Compute hashes.
	seatbeltProfile := computeSeatbeltProfileHash()
	sandboxHash := hashString(worktreePath)
	capabilityHash := hashString(ompOutput)

	result := &AdapterResult{
		SeatbeltProfileHash: seatbeltProfile,
		SandboxHash:         sandboxHash,
		TerminalProofHash:   terminalProof,
		CapabilityHash:      capabilityHash,
		UIDPoolHash:         hashString(fmt.Sprintf("uid_pool_%d", uid)),
		UIDLeaseHash:        hashString(fmt.Sprintf("uid_lease_%d_%d", uid, gid)),
		UID:                 uid,
		GroupID:             gid,
		Output:              ompOutput,
	}

	if err != nil {
		result.Output = fmt.Sprintf("%s\n[OMP ERROR]: %v", ompOutput, err)
	}

	_ = outputStr // available for debugging
	return result, nil
}

func validateOMPConfig(config OMPAdapterConfig) error {
	if config.WrapperPath == "" {
		return fmt.Errorf("wrapper path is empty")
	}
	if config.Workflow == "" {
		return fmt.Errorf("workflow is empty")
	}
	if config.Provider == "" || config.Model == "" {
		return fmt.Errorf("provider or model is empty")
	}
	if config.PromptPath == "" || config.OutputPath == "" {
		return fmt.Errorf("prompt or output path is empty")
	}
	if config.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}

// OMPReviewConfig configures an independent OMP review session.
type OMPReviewConfig struct {
	OMPAdapterConfig
	ReviewPromptPath string // path to the review prompt file
	ReviewOutputPath string // path to the review output file
}

// RunOMPReview runs an independent OMP review of the repair. This is called
// after the repair attestation is produced to get an independent assessment.
func RunOMPReview(worktreePath string, config OMPReviewConfig) (string, error) {
	if config.ReviewPromptPath == "" || config.ReviewOutputPath == "" {
		return "", fmt.Errorf("%w: review prompt or output path is empty", ErrOMPAdapter)
	}

	// Use the adapter config but with review-specific paths.
	adapterConfig := config.OMPAdapterConfig
	adapterConfig.PromptPath = config.ReviewPromptPath
	adapterConfig.OutputPath = config.ReviewOutputPath
	adapterConfig.Role = "audit"

	_, err := RunOMPAdapter(worktreePath, 0, 0, adapterConfig)
	if err != nil {
		return "", err
	}

	// Read the review output.
	reviewOutput, err := os.ReadFile(config.ReviewOutputPath)
	if err != nil {
		return "", fmt.Errorf("%w: read review output: %v", ErrOMPAdapter, err)
	}

	return string(reviewOutput), nil
}

// CleanupOMPSessions removes the OMP session directory.
func CleanupOMPSessions(sessionDir string) {
	if sessionDir != "" {
		_ = os.RemoveAll(sessionDir)
	}
}

// FormatOMPCommand returns a human-readable representation of the OMP command
// for logging purposes.
func FormatOMPCommand(config OMPAdapterConfig) string {
	return fmt.Sprintf("%s %d %s %s --workflow %s --role %s --run-id %s --hermes-provider %s --hermes-model %s --task-tier %s --session-dir %s",
		filepath.Base(config.WrapperPath),
		config.Timeout,
		filepath.Base(config.PromptPath),
		filepath.Base(config.OutputPath),
		config.Workflow,
		config.Role,
		config.RunID,
		config.Provider,
		config.Model,
		config.TaskTier,
		filepath.Base(config.SessionDir),
	)
}

// IsOMPTimeoutError checks if an OMP error is a timeout.
func IsOMPTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "OMP_TIMEOUT") || contains(err.Error(), "Deadline exceeded")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && (indexOf(s, substr) >= 0)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// EstimateOMPTimeout returns a recommended OMP timeout based on the task tier.
func EstimateOMPTimeout(taskTier string) int {
	switch taskTier {
	case "mechanical":
		return 120
	case "normal":
		return 600
	case "hard":
		return 1200
	default:
		return 600
	}
}

// DurationSince returns the elapsed time since the given start time.
func DurationSince(start time.Time) time.Duration {
	return time.Since(start)
}
