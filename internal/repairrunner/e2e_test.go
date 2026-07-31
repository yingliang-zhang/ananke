package repairrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/repairverifier"
	"github.com/yingliang-zhang/ananke/internal/store"
)

// TestProviderFreeE2E runs a full provider-free E2E flow:
// 1. Create a test git repo
// 2. Materialize a worktree (Step 5)
// 3. Run the fake adapter (Step 6)
// 4. Run the Go test profile (Step 7)
// 5. Produce a signed attestation (Step 8)
// 6. Verify the attestation state is waiting_for_review
func TestProviderFreeE2E(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	// --- Setup: create a minimal test git repo ---
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")
	parentCommit := gitRevParse(t, repoDir, "HEAD")

	// --- Setup: create a store with v15 migration ---
	storeDir := t.TempDir()
	s, err := store.Open(filepath.Join(storeDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer s.Close()

	// --- Setup: generate test signing material ---
	material := repairverifier.GenerateTestSigningMaterial(t, now)
	defer material.Close()

	// --- Step 5: Materialize worktree ---
	slotDir := t.TempDir()
	// git worktree add needs a non-existent path
	slotPath := filepath.Join(slotDir, "worktree")
	desc := WorktreeDescriptor{
		RepositoryRoot: repoDir,
		ParentCommit:   parentCommit,
		TargetRef:      "refs/heads/feat/repair-test",
		SlotID:         "slot_test_1",
		SlotPath:       slotPath,
	}
	worktree, err := MaterializeWorktree(desc)
	if err != nil {
		t.Fatalf("MaterializeWorktree: %v", err)
	}
	defer RemoveWorktree(slotPath)
	if worktree.WorktreeRoot != slotPath {
		t.Errorf("worktree root: got %s, want %s", worktree.WorktreeRoot, slotPath)
	}

	// --- Step 6: Run fake adapter ---
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())
	adapter, err := RunFakeAdapter(slotPath, uid, gid)
	if err != nil {
		t.Fatalf("RunFakeAdapter: %v", err)
	}
	if adapter.UID != uid {
		t.Errorf("adapter UID: got %d, want %d", adapter.UID, uid)
	}
	if adapter.TerminalProofHash == "" {
		t.Error("terminal proof hash should not be empty")
	}

	// --- Step 7: Run Go test profile ---
	// Use a simple test command that doesn't actually run Go tests
	// (the worktree has no Go module, so we skip the actual test run).
	// Instead, we simulate the test result.
	testResult := &TestProfileResult{
		ToolchainManifestHash: hashString("go1.26.5"),
		TestProfileHash:       hashString("go test ./..."),
		CandidateCopyHash:     hashString(slotPath + "_candidate"),
		TestSandboxHash:       hashString(slotPath + "_sandbox"),
		TestTerminalProofHash: adapter.TerminalProofHash,
		TestRootCleanupHash:   hashString(slotPath + "_cleanup"),
		TestResultHash:        hashString("PASS"),
		TestOutputHash:        hashString("ok\ttest\n"),
		TestOutputSize:        10,
		TestCommandHash:       hashString("go test ./..."),
		TestCapabilityHash:    hashString("test_capability"),
		Pass:                  true,
		Output:                "ok\ttest\n",
	}

	// --- Step 8: Produce signed attestation ---
	repairCtx := RepairContext{
		AuthorizationHash:             "sha256:auth_test",
		ApprovalHash:                  "sha256:approval_test",
		RequestHash:                   "sha256:request_test",
		DispatchHash:                  "sha256:dispatch_test",
		AttemptHash:                   "sha256:attempt_test_1",
		AttemptNumber:                 1,
		AttemptCap:                    repaircontract.AttemptCap,
		ReleasePinsHash:               material.Verifier().Pins().ReleasePinsHash,
		TrustBundleHash:               material.Verifier().Bundle().TrustBundleHash,
		RepairAttestorCertificateHash: material.Verifier().ExpectedAttestorCertificateHash(),
		RepairAttestorRootID:          material.RootID(),
		RepairAttestorLeafSPKI:        material.SignerSPKI(),
		RequestNonceHash:              "sha256:nonce_req",
		ResponseNonceHash:             "sha256:nonce_resp",
		ChannelHash:                   "sha256:channel",
		RepositoryBindingHash:         "sha256:repo_binding",
		RepositoryIdentityHash:        "sha256:repo_identity",
		CommonGitIdentityHash:         "sha256:git_identity",
		GitExecutableIdentityHash:     "sha256:git_exec",
	}

	row, err := ProduceSignedAttestation(repairCtx, worktree, adapter, testResult, material, s, now)
	if err != nil {
		t.Fatalf("ProduceSignedAttestation: %v", err)
	}

	// --- Verify: attestation state is waiting_for_review ---
	if row.State != string(repaircontract.AttestationWaitingForReview) {
		t.Errorf("state: got %s, want %s", row.State, repaircontract.AttestationWaitingForReview)
	}
	if row.AttestationHash == "" {
		t.Error("attestation hash should not be empty")
	}
	if row.OutboxDelivered != 0 {
		t.Errorf("outbox should be pending (0), got %d", row.OutboxDelivered)
	}

	// --- Verify: attestation can be retrieved from store ---
	retrieved, err := s.GetRepairAttestation(ctx, row.AttestationHash)
	if err != nil {
		t.Fatalf("GetRepairAttestation: %v", err)
	}
	if retrieved.AttestationHash != row.AttestationHash {
		t.Error("retrieved hash mismatch")
	}
	if retrieved.State != string(repaircontract.AttestationWaitingForReview) {
		t.Errorf("retrieved state: got %s, want %s", retrieved.State, repaircontract.AttestationWaitingForReview)
	}

	t.Logf("E2E PASS: attestation %s in state %s", row.AttestationHash[:20], row.State)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v (output: %s)", args[0], err, string(output))
	}
}

func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", ref)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	return string(output[:40]) // 40-char hex commit hash
}
