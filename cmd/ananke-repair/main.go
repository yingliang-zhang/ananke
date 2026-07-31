// Command ananke-repair runs a controlled repair flow: materialize a worktree,
// run an adapter (fake or OMP), execute tests, produce a signed attestation,
// and persist it to the store for review.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/repairrunner"
	"github.com/yingliang-zhang/ananke/internal/repairverifier"
	"github.com/yingliang-zhang/ananke/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "submit":
		cmdSubmit(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "review":
		cmdReview(os.Args[2:])
	case "genkey":
		cmdGenKey(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "ananke-repair: unknown command %q\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `ananke-repair — controlled repair CLI

Commands:
  submit   Run a controlled repair: worktree → adapter → test → sign → persist
  status   Query attestation status by hash
  review   Accept or reject a repair attestation
  genkey   Generate an Ed25519 key pair for repair signing

Usage:
  ananke-repair submit --repo <path> --request <text> --store <path> [options]
  ananke-repair status --store <path> --hash <sha256:...>
  ananke-repair review --store <path> --hash <sha256:...> --action accept|reject
  ananke-repair genkey --keydir <path>

Options for submit:
  --adapter      Adapter type: "fake" (default) or "omp"
  --keydir       Path to Ed25519 key directory (default: ~/.ananke/keys)
  --omp-wrapper  Path to OMP wrapper script (for --adapter omp)
  --omp-provider OMP provider name
  --omp-model    OMP model name
  --timeout      Adapter timeout in seconds (default: 120)
  --diff-out     Path to save git diff patch (for git apply after review)`)
}

// runSubmit executes the full repair submit flow and returns an error.
// It does NOT call os.Exit — defers run correctly on all paths.
func runSubmit(args []string) error {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	repo := fs.String("repo", "", "path to git repository (required)")
	request := fs.String("request", "", "repair request text (required)")
	storePath := fs.String("store", "ananke-repair.sqlite", "path to SQLite store")
	adapterType := fs.String("adapter", "fake", "adapter type: fake or omp")
	keyDir := fs.String("keydir", "", "path to Ed25519 key directory")
	ompWrapper := fs.String("omp-wrapper", "", "path to OMP wrapper script")
	ompProvider := fs.String("omp-provider", "", "OMP provider name")
	ompModel := fs.String("omp-model", "", "OMP model name")
	timeoutSec := fs.Int("timeout", 120, "adapter timeout in seconds")
	diffOutput := fs.String("diff-out", "", "path to save git diff patch (for git apply)")
	fs.Parse(args)

	if *repo == "" || *request == "" {
		return errors.New("submit: --repo and --request are required")
	}
	if *keyDir == "" {
		home, _ := os.UserHomeDir()
		*keyDir = filepath.Join(home, ".ananke", "keys")
	}

	// F5: absolutize repo path before passing to MaterializeWorktree.
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return fmt.Errorf("absolutize repo path: %v", err)
	}

	now := time.Now().UTC()

	// 1. Open store.
	s, err := store.Open(*storePath)
	if err != nil {
		return fmt.Errorf("open store: %v", err)
	}
	defer s.Close()

	// 2. Load or generate signing material.
	material, err := loadOrGenerateMaterial(*keyDir, now)
	if err != nil {
		return fmt.Errorf("signing material: %v", err)
	}
	defer material.Close()

	// 3. Materialize worktree.
	slotDir, err := os.MkdirTemp("", "ananke-repair-")
	if err != nil {
		return fmt.Errorf("create slot dir: %v", err)
	}
	defer os.RemoveAll(slotDir)

	slotPath := filepath.Join(slotDir, "worktree")
	parentCommit, err := gitRevParse(absRepo, "HEAD")
	if err != nil {
		return err
	}
	desc := repairrunner.WorktreeDescriptor{
		RepositoryRoot: absRepo,
		ParentCommit:   parentCommit,
		TargetRef:      "refs/heads/feat/ananke-repair",
		SlotID:         "slot_repair_" + now.Format("20060102_150405"),
		SlotPath:       slotPath,
	}
	worktree, err := repairrunner.MaterializeWorktree(desc)
	if err != nil {
		return fmt.Errorf("materialize worktree: %v", err)
	}
	defer repairrunner.RemoveWorktree(slotPath)
	fmt.Fprintf(os.Stderr, "worktree materialized at %s (parent: %s)\n", slotPath, parentCommit[:min(12, len(parentCommit))])

	// 4. Run adapter.
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())
	var adapter *repairrunner.AdapterResult
	switch *adapterType {
	case "fake":
		adapter, err = repairrunner.RunFakeAdapter(slotPath, uid, gid)
		if err != nil {
			return fmt.Errorf("fake adapter: %v", err)
		}
	case "omp":
		if *ompWrapper == "" || *ompProvider == "" || *ompModel == "" {
			return errors.New("OMP adapter requires --omp-wrapper, --omp-provider, --omp-model")
		}
		// F4: use defer for promptDir cleanup.
		promptDir, err := os.MkdirTemp("", "ananke-omp-")
		if err != nil {
			return fmt.Errorf("create OMP prompt dir: %v", err)
		}
		defer os.RemoveAll(promptDir)
		promptPath := filepath.Join(promptDir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte(*request), 0o644); err != nil {
			return fmt.Errorf("write OMP prompt: %v", err)
		}
		outputPath := filepath.Join(promptDir, "output.md")
		sessionDir := filepath.Join(promptDir, "session")
		adapter, err = repairrunner.RunOMPAdapter(slotPath, uid, gid, repairrunner.OMPAdapterConfig{
			WrapperPath: *ompWrapper,
			Workflow:    "coupled-v1",
			Provider:    *ompProvider,
			Model:       *ompModel,
			TaskTier:    "normal",
			Role:        "implement",
			RunID:       "ananke-repair-" + now.Format("20060102_150405"),
			SessionDir:  sessionDir,
			Timeout:     *timeoutSec,
			PromptPath:  promptPath,
			OutputPath:  outputPath,
			Workdir:     slotPath,
		})
		if err != nil {
			return fmt.Errorf("OMP adapter: %v", err)
		}
	default:
		return fmt.Errorf("unknown adapter type: %s", *adapterType)
	}
	fmt.Fprintf(os.Stderr, "adapter completed (UID=%d, terminal proof: %s)\n", adapter.UID, adapter.TerminalProofHash[:min(20, len(adapter.TerminalProofHash))])

	// 5. Compute diff closure.
	diff, err := repairrunner.ComputeDiffClosure(slotPath)
	if err != nil {
		return fmt.Errorf("compute diff closure: %v", err)
	}
	worktree.Diff = diff
	fmt.Fprintf(os.Stderr, "diff closure computed (status hash: %s)\n", diff.StatusHash[:min(20, len(diff.StatusHash))])

	// 6. Run Go test profile (if go.mod exists).
	var testResult *repairrunner.TestProfileResult
	if _, err := os.Stat(filepath.Join(slotPath, "go.mod")); err == nil {
		testResult, err = repairrunner.RunGoTestProfile(slotPath, uid, gid, []string{"go", "test", "./...", "-count=1", "-timeout", "60s"})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: go test profile failed: %v\n", err)
			testResult = skipTestResult(adapter.TerminalProofHash, err.Error(), false)
		}
	} else {
		testResult = skipTestResult(adapter.TerminalProofHash, "skipped: no go.mod", true)
	}
	fmt.Fprintf(os.Stderr, "test profile: pass=%v\n", testResult.Pass)

	// 7. Produce signed attestation.
	repairCtx := repairrunner.RepairContext{
		AuthorizationHash:                repairrunner.HashString("ananke-repair-auth"),
		ApprovalHash:                     repairrunner.HashString("ananke-repair-approval"),
		RequestHash:                      repairrunner.HashString(*request),
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
		return fmt.Errorf("produce signed attestation: %v", err)
	}

	// 8. Save diff patch before worktree cleanup.
	var diffPath string
	if *diffOutput != "" {
		// R1-02 fix: git add -N to include untracked files, then git diff HEAD.
		exec.Command("git", "-C", slotPath, "add", "-N", ".").Run()
		diffBytes, err := exec.Command("git", "-C", slotPath, "diff", "HEAD").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: git diff capture failed: %v\n", err)
		} else if len(diffBytes) > 0 {
			if err := os.WriteFile(*diffOutput, diffBytes, 0o644); err != nil {
				return fmt.Errorf("write diff output: %v", err)
			}
			diffPath, _ = filepath.Abs(*diffOutput)
			fmt.Fprintf(os.Stderr, "diff patch saved to %s (%d bytes)\n", diffPath, len(diffBytes))
		}
	}

	// 9. Output result.
	result := map[string]any{
		"attestation_hash": row.AttestationHash,
		"attestation_id":   row.AttestationID,
		"state":            row.State,
		"issued_at":        row.IssuedAt,
		"attempt_number":   row.AttemptNumber,
		"diff_status_hash": diff.StatusHash,
		"diff_patch_path":  diffPath,
		"test_pass":        testResult.Pass,
		"adapter_type":     *adapterType,
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
	fmt.Fprintf(os.Stderr, "\nRepair submitted. Attestation hash: %s\n", row.AttestationHash)
	fmt.Fprintf(os.Stderr, "Review with: ananke-repair review --store %s --hash %s --action accept\n", *storePath, row.AttestationHash)
	return nil
}

func cmdSubmit(args []string) {
	if err := runSubmit(args); err != nil {
		fmt.Fprintf(os.Stderr, "ananke-repair: %v\n", err)
		os.Exit(1)
	}
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	storePath := fs.String("store", "ananke-repair.sqlite", "path to SQLite store")
	hash := fs.String("hash", "", "attestation hash (required)")
	fs.Parse(args)
	if *hash == "" {
		return errors.New("status: --hash is required")
	}
	s, err := store.Open(*storePath)
	if err != nil {
		return fmt.Errorf("open store: %v", err)
	}
	defer s.Close()
	row, err := s.GetRepairAttestation(context.Background(), *hash)
	if err != nil {
		return fmt.Errorf("get attestation: %v", err)
	}
	result := map[string]any{
		"attestation_hash": row.AttestationHash,
		"attestation_id":   row.AttestationID,
		"state":            row.State,
		"attempt_number":   row.AttemptNumber,
		"issued_at":        row.IssuedAt,
		"signature_prefix": row.SignatureHash[:min(30, len(row.SignatureHash))] + "...",
		"outbox_delivered": row.OutboxDelivered,
		"created_at":       row.CreatedAt,
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func cmdStatus(args []string) {
	if err := runStatus(args); err != nil {
		fmt.Fprintf(os.Stderr, "ananke-repair: %v\n", err)
		os.Exit(1)
	}
}

func runReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	storePath := fs.String("store", "ananke-repair.sqlite", "path to SQLite store")
	hash := fs.String("hash", "", "attestation hash (required)")
	action := fs.String("action", "", "accept or reject (required)")
	fs.Parse(args)
	if *hash == "" || *action == "" {
		return errors.New("review: --hash and --action are required")
	}
	if *action != "accept" && *action != "reject" {
		return errors.New("review: --action must be accept or reject")
	}
	s, err := store.Open(*storePath)
	if err != nil {
		return fmt.Errorf("open store: %v", err)
	}
	defer s.Close()
	switch *action {
	case "accept":
		if err := s.AcknowledgeRepairAttestationOutbox(context.Background(), *hash); err != nil {
			return fmt.Errorf("acknowledge: %v", err)
		}
		fmt.Printf("Repair accepted. Attestation %s outbox delivered.\n", *hash)
	case "reject":
		if err := s.AbandonRepairAttestationOutbox(context.Background(), *hash, "rejected by reviewer"); err != nil {
			return fmt.Errorf("abandon: %v", err)
		}
		fmt.Printf("Repair rejected. Attestation %s outbox abandoned.\n", *hash)
	}
	return nil
}

func cmdReview(args []string) {
	if err := runReview(args); err != nil {
		fmt.Fprintf(os.Stderr, "ananke-repair: %v\n", err)
		os.Exit(1)
	}
}

func runGenKey(args []string) error {
	fs := flag.NewFlagSet("genkey", flag.ExitOnError)
	keyDir := fs.String("keydir", "", "path to key directory (required)")
	fs.Parse(args)
	if *keyDir == "" {
		return errors.New("genkey: --keydir is required")
	}
	if err := os.MkdirAll(*keyDir, 0o700); err != nil {
		return fmt.Errorf("create key dir: %v", err)
	}
	mat, err := repairverifier.GenerateSigningMaterial(time.Now().UTC())
	if err != nil {
		return fmt.Errorf("generate key: %v", err)
	}
	defer mat.Close()
	fmt.Printf("Repair signing material generated (SPKI: %s)\n", mat.SignerSPKI())
	fmt.Printf("Note: MVP keys are ephemeral per-run. Store --keydir for future use.\n")
	return nil
}

func cmdGenKey(args []string) {
	if err := runGenKey(args); err != nil {
		fmt.Fprintf(os.Stderr, "ananke-repair: %v\n", err)
		os.Exit(1)
	}
}

// loadOrGenerateMaterial generates an Ed25519 key pair for repair signing.
// MVP: uses GenerateSigningMaterial which overrides the verifier's pinned
// SPKI to accept the generated key. The --keydir flag is currently unused
// (keys are ephemeral per run). TODO: when real release-pinned keys are
// available, switch to LoadRepairSigningMaterial with a key file path.
func loadOrGenerateMaterial(_ string, now time.Time) (*repairverifier.RepairSigningMaterial, error) {
	return repairverifier.GenerateSigningMaterial(now)
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

func gitRevParse(repo string, ref string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "rev-parse", ref).Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
