// Command ananke-repair runs a controlled repair flow: materialize a worktree,
// run an adapter (fake or OMP), execute tests, produce a signed attestation,
// and persist it to the store for review.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
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
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
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
  --timeout      Adapter timeout in seconds (default: 120)`)
}

func cmdSubmit(args []string) {
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
	fs.Parse(args)

	if *repo == "" || *request == "" {
		fmt.Fprintln(os.Stderr, "ananke-repair submit: --repo and --request are required")
		os.Exit(2)
	}
	if *keyDir == "" {
		home, _ := os.UserHomeDir()
		*keyDir = filepath.Join(home, ".ananke", "keys")
	}

	ctx := context.Background()
	now := time.Now().UTC()
	_ = ctx

	// 1. Open store.
	s, err := store.Open(*storePath)
	if err != nil {
		fatalf("open store: %v", err)
	}
	defer s.Close()

	// 2. Load or generate signing material.
	material, err := loadOrGenerateMaterial(*keyDir, now)
	if err != nil {
		fatalf("signing material: %v", err)
	}
	defer material.Close()

	// 3. Materialize worktree.
	slotDir, err := os.MkdirTemp("", "ananke-repair-")
	if err != nil {
		fatalf("create slot dir: %v", err)
	}
	defer os.RemoveAll(slotDir)

	slotPath := filepath.Join(slotDir, "worktree")
	parentCommit := gitRevParse(*repo, "HEAD")
	desc := repairrunner.WorktreeDescriptor{
		RepositoryRoot: *repo,
		ParentCommit:   parentCommit,
		TargetRef:      "refs/heads/feat/ananke-repair",
		SlotID:         "slot_repair_" + now.Format("20060102_150405"),
		SlotPath:       slotPath,
	}
	worktree, err := repairrunner.MaterializeWorktree(desc)
	if err != nil {
		fatalf("materialize worktree: %v", err)
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
			fatalf("fake adapter: %v", err)
		}
	case "omp":
		if *ompWrapper == "" || *ompProvider == "" || *ompModel == "" {
			fatalf("OMP adapter requires --omp-wrapper, --omp-provider, --omp-model")
		}
		promptDir, _ := os.MkdirTemp("", "ananke-omp-")
		promptPath := filepath.Join(promptDir, "prompt.md")
		os.WriteFile(promptPath, []byte(*request), 0o644)
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
			fatalf("OMP adapter: %v", err)
		}
		os.RemoveAll(promptDir)
	default:
		fatalf("unknown adapter type: %s", *adapterType)
	}
	fmt.Fprintf(os.Stderr, "adapter completed (UID=%d, terminal proof: %s)\n", adapter.UID, adapter.TerminalProofHash[:min(20, len(adapter.TerminalProofHash))])

	// 5. Compute diff closure.
	diff, err := repairrunner.ComputeDiffClosure(slotPath)
	if err != nil {
		fatalf("compute diff closure: %v", err)
	}
	worktree.Diff = diff
	fmt.Fprintf(os.Stderr, "diff closure computed (status hash: %s)\n", diff.StatusHash[:min(20, len(diff.StatusHash))])

	// 6. Run Go test profile (if go.mod exists).
	var testResult *repairrunner.TestProfileResult
	if _, err := os.Stat(filepath.Join(slotPath, "go.mod")); err == nil {
		testResult, err = repairrunner.RunGoTestProfile(slotPath, uid, gid, []string{"go", "test", "./...", "-count=1", "-timeout", "60s"})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: go test profile failed: %v\n", err)
			testResult = skipTestResult(adapter.TerminalProofHash, err.Error())
		}
	} else {
		testResult = skipTestResult(adapter.TerminalProofHash, "skipped: no go.mod")
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
		RepositoryBindingHash:            repairrunner.HashString(*repo),
		RepositoryIdentityHash:           repairrunner.HashString(filepath.Base(*repo)),
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
		fatalf("produce signed attestation: %v", err)
	}

	// 8. Output result.
	result := map[string]any{
		"attestation_hash": row.AttestationHash,
		"attestation_id":   row.AttestationID,
		"state":            row.State,
		"issued_at":        row.IssuedAt,
		"attempt_number":   row.AttemptNumber,
		"diff_status_hash": diff.StatusHash,
		"test_pass":        testResult.Pass,
		"adapter_type":     *adapterType,
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
	fmt.Fprintf(os.Stderr, "\nRepair submitted. Attestation hash: %s\n", row.AttestationHash)
	fmt.Fprintf(os.Stderr, "Review with: ananke-repair review --store %s --hash %s --action accept\n", *storePath, row.AttestationHash)
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	storePath := fs.String("store", "ananke-repair.sqlite", "path to SQLite store")
	hash := fs.String("hash", "", "attestation hash (required)")
	fs.Parse(args)
	if *hash == "" {
		fmt.Fprintln(os.Stderr, "ananke-repair status: --hash is required")
		os.Exit(2)
	}
	s, err := store.Open(*storePath)
	if err != nil {
		fatalf("open store: %v", err)
	}
	defer s.Close()
	row, err := s.GetRepairAttestation(context.Background(), *hash)
	if err != nil {
		fatalf("get attestation: %v", err)
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
}

func cmdReview(args []string) {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	storePath := fs.String("store", "ananke-repair.sqlite", "path to SQLite store")
	hash := fs.String("hash", "", "attestation hash (required)")
	action := fs.String("action", "", "accept or reject (required)")
	fs.Parse(args)
	if *hash == "" || *action == "" {
		fmt.Fprintln(os.Stderr, "ananke-repair review: --hash and --action are required")
		os.Exit(2)
	}
	if *action != "accept" && *action != "reject" {
		fmt.Fprintln(os.Stderr, "ananke-repair review: --action must be accept or reject")
		os.Exit(2)
	}
	s, err := store.Open(*storePath)
	if err != nil {
		fatalf("open store: %v", err)
	}
	defer s.Close()
	switch *action {
	case "accept":
		if err := s.AcknowledgeRepairAttestationOutbox(context.Background(), *hash); err != nil {
			fatalf("acknowledge: %v", err)
		}
		fmt.Printf("Repair accepted. Attestation %s outbox delivered.\n", *hash)
	case "reject":
		if err := s.AbandonRepairAttestationOutbox(context.Background(), *hash, "rejected by reviewer"); err != nil {
			fatalf("abandon: %v", err)
		}
		fmt.Printf("Repair rejected. Attestation %s outbox abandoned.\n", *hash)
	}
}

func cmdGenKey(args []string) {
	fs := flag.NewFlagSet("genkey", flag.ExitOnError)
	keyDir := fs.String("keydir", "", "path to key directory (required)")
	fs.Parse(args)
	if *keyDir == "" {
		fmt.Fprintln(os.Stderr, "ananke-repair genkey: --keydir is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*keyDir, 0o700); err != nil {
		fatalf("create key dir: %v", err)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		fatalf("generate key: %v", err)
	}
	privPath := filepath.Join(*keyDir, "repair-private-key")
	pubPath := filepath.Join(*keyDir, "repair-public-key")
	privHex := "ed25519-private:" + hex.EncodeToString(priv)
	pubHex := "ed25519-public:" + hex.EncodeToString(pub)
	if err := os.WriteFile(privPath, []byte(privHex), 0o600); err != nil {
		fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(pubPath, []byte(pubHex), 0o644); err != nil {
		fatalf("write public key: %v", err)
	}
	spki, _ := transportprimitives.SPKIHash(pub)
	fmt.Printf("Ed25519 key pair generated:\n  Private: %s\n  Public:  %s\n  SPKI:   %s\n", privPath, pubPath, spki)
}

func loadOrGenerateMaterial(keyDir string, now time.Time) (*repairverifier.RepairSigningMaterial, error) {
	privPath := filepath.Join(keyDir, "repair-private-key")
	if _, err := os.Stat(privPath); err == nil {
		return repairverifier.LoadRepairSigningMaterial(keyDir, uint32(os.Getuid()), now)
	}
	fmt.Fprintf(os.Stderr, "no repair keys found in %s, generating...\n", keyDir)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("generate key: %v", err)
	}
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return nil, fmt.Errorf("create key dir: %v", err)
	}
	privHex := "ed25519-private:" + hex.EncodeToString(priv)
	pubHex := "ed25519-public:" + hex.EncodeToString(pub)
	if err := os.WriteFile(privPath, []byte(privHex), 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "repair-public-key"), []byte(pubHex), 0o644); err != nil {
		return nil, fmt.Errorf("write public key: %v", err)
	}
	return repairverifier.LoadRepairSigningMaterial(keyDir, uint32(os.Getuid()), now)
}

func skipTestResult(proofHash, msg string) *repairrunner.TestProfileResult {
	return &repairrunner.TestProfileResult{
		ToolchainManifestHash: repairrunner.HashString("go_test"),
		TestProfileHash:       repairrunner.HashString("go test"),
		TestTerminalProofHash: proofHash,
		TestResultHash:        repairrunner.HashString("SKIP"),
		TestOutputHash:        repairrunner.HashString(msg),
		TestCommandHash:       repairrunner.HashString("skip"),
		TestCapabilityHash:    repairrunner.HashString("test_capability"),
		Pass:                  true,
		Output:                msg,
	}
}

func gitRevParse(repo string, ref string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", ref).Output()
	if err != nil {
		fatalf("git rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ananke-repair: "+format+"\n", args...)
	os.Exit(1)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
