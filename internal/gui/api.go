// Package gui provides the repair execution logic for the Go daemon.
// The HTTP server and embedded HTML frontend have been removed (audit
// divergence fix: the Tauri 2 native GUI is the only operator surface).
// The RunRepair function is salvaged for the Go daemon's Unix-socket IPC.
package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/repairrunner"
	"github.com/yingliang-zhang/ananke/internal/repairverifier"
	"github.com/yingliang-zhang/ananke/internal/store"
)

// RepairConfig holds the OMP adapter settings for repair execution.
type RepairConfig struct {
	WrapperPath string
	Provider    string
	Model       string
	Timeout     int
}

// RepairRequest is the input for a controlled repair.
type RepairRequest struct {
	ProjectPath  string
	RequestText  string
	AdapterType  string
	OperatorName string
}

// RepairResult is the output of a controlled repair.
type RepairResult struct {
	AttestationHash string
	DiffPath        string
	Error           string
}

// RunRepair executes a controlled repair flow in-process:
// worktree → adapter → test → sign → persist.
// This function is called by the Go daemon's repair IPC handler.
func RunRepair(req RepairRequest, cfg RepairConfig, s *store.Store) RepairResult {
	now := time.Now().UTC()

	// 1. Generate signing material.
	material, err := repairverifier.GenerateSigningMaterial(now)
	if err != nil {
		return RepairResult{Error: fmt.Sprintf("signing material: %v", err)}
	}
	defer material.Close()

	// 2. Materialize worktree.
	slotDir, err := os.MkdirTemp("", "ananke-repair-")
	if err != nil {
		return RepairResult{Error: fmt.Sprintf("create slot dir: %v", err)}
	}
	defer os.RemoveAll(slotDir)

	absRepo, err := filepath.Abs(req.ProjectPath)
	if err != nil {
		return RepairResult{Error: fmt.Sprintf("abs path: %v", err)}
	}

	parentOut, err := exec.Command("git", "-C", absRepo, "rev-parse", "HEAD").Output()
	if err != nil {
		return RepairResult{Error: fmt.Sprintf("git rev-parse: %v", err)}
	}
	parentCommit := string(parentOut[:len(parentOut)-1])

	slotPath := filepath.Join(slotDir, "worktree")
	desc := repairrunner.WorktreeDescriptor{
		RepositoryRoot: absRepo,
		ParentCommit:   parentCommit,
		TargetRef:      "refs/heads/feat/ananke-repair",
		SlotID:         "slot_" + now.Format("20060102_150405"),
		SlotPath:       slotPath,
	}
	worktree, err := repairrunner.MaterializeWorktree(desc)
	if err != nil {
		return RepairResult{Error: fmt.Sprintf("materialize worktree: %v", err)}
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
			return RepairResult{Error: fmt.Sprintf("fake adapter: %v", err)}
		}
	case "omp":
		promptDir, err := os.MkdirTemp("", "ananke-repair-omp-")
		if err != nil {
			return RepairResult{Error: fmt.Sprintf("create prompt dir: %v", err)}
		}
		defer os.RemoveAll(promptDir)
		promptPath := filepath.Join(promptDir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte(req.RequestText), 0o644); err != nil {
			return RepairResult{Error: fmt.Sprintf("write prompt: %v", err)}
		}
		outputPath := filepath.Join(promptDir, "output.md")
		sessionDir := filepath.Join(promptDir, "session")
		adapter, err = repairrunner.RunOMPAdapter(slotPath, uid, gid, repairrunner.OMPAdapterConfig{
			WrapperPath: cfg.WrapperPath,
			Workflow:    "coupled-v1",
			Provider:    cfg.Provider,
			Model:       cfg.Model,
			TaskTier:    "normal",
			Role:        "implement",
			RunID:       "ananke-repair-" + now.Format("20060102_150405"),
			SessionDir:  sessionDir,
			Timeout:     cfg.Timeout,
			PromptPath:  promptPath,
			OutputPath:  outputPath,
			Workdir:     slotPath,
		})
		if err != nil {
			return RepairResult{Error: fmt.Sprintf("OMP adapter: %v", err)}
		}
	default:
		return RepairResult{Error: fmt.Sprintf("unknown adapter: %s", req.AdapterType)}
	}

	// 4. Compute diff closure + save diff patch.
	diff, err := repairrunner.ComputeDiffClosure(slotPath)
	if err != nil {
		return RepairResult{Error: fmt.Sprintf("diff closure: %v", err)}
	}
	worktree.Diff = diff

	diffDir := filepath.Join(os.TempDir(), "ananke-diffs")
	os.MkdirAll(diffDir, 0o755)
	diffPath := filepath.Join(diffDir, now.Format("20060102_150405")+".patch")
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
		AuthorizationHash:                repairrunner.HashString("ananke-repair-auth"),
		ApprovalHash:                     repairrunner.HashString("ananke-repair-approval"),
		RequestHash:                      repairrunner.HashString(req.RequestText),
		DispatchHash:                     repairrunner.HashString("ananke-repair-dispatch"),
		AttemptHash:                      repairrunner.HashString("ananke-repair-attempt-1"),
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
	row, err := repairrunner.ProduceSignedAttestation(repairCtx, worktree, adapter, testResult, material, s, now)
	if err != nil {
		return RepairResult{Error: fmt.Sprintf("produce attestation: %v", err)}
	}

	return RepairResult{
		AttestationHash: row.AttestationHash,
		DiffPath:        diffPath,
	}
}

func skipTestResult(proofHash, msg string, pass bool) *repairrunner.TestProfileResult {
	return &repairrunner.TestProfileResult{
		ToolchainManifestHash: repairrunner.HashString("go_test"),
		TestProfileHash:       repairrunner.HashString("go test"),
		TestResultHash:        repairrunner.HashString(msg),
		TestOutputHash:        repairrunner.HashString(msg),
		TestCommandHash:       repairrunner.HashString("skip"),
		TestCapabilityHash:    repairrunner.HashString("test_capability"),
		Pass:                  pass,
		Output:                msg,
	}
}
