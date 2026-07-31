package repairrunner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/repairverifier"
	"github.com/yingliang-zhang/ananke/internal/store"
)

// TestOMPIntegrationE2E runs a full E2E flow using the real OMP adapter
// instead of the fake adapter. This test is skipped unless the
// ANANKE_OMP_INTEGRATION_TEST environment variable is set to "1" and
// the OMP wrapper script exists.
//
// The test creates a real git repo, materializes a worktree, runs OMP to
// produce real source edits, computes the diff closure, runs the Go test
// profile (if the worktree has a go.mod), produces a signed attestation,
// and verifies it with Ananke.
func TestOMPIntegrationE2E(t *testing.T) {
	// Skip unless explicitly enabled.
	if os.Getenv("ANANKE_OMP_INTEGRATION_TEST") != "1" {
		t.Skip("skipping OMP integration test; set ANANKE_OMP_INTEGRATION_TEST=1 to enable")
	}

	wrapperPath := os.Getenv("OMP_WRAPPER_PATH")
	if wrapperPath == "" {
		wrapperPath = os.ExpandEnv("$HOME/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh")
	}
	if _, err := os.Stat(wrapperPath); err != nil {
		t.Skipf("OMP wrapper not found at %s: %v", wrapperPath, err)
	}

	provider := os.Getenv("OMP_PROVIDER")
	model := os.Getenv("OMP_MODEL")
	if provider == "" || model == "" {
		t.Skip("OMP_PROVIDER and OMP_MODEL must be set for integration test")
	}

	ctx := context.Background()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	// --- Setup: create a test git repo with a simple Go file ---
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	// Create a simple Go file for OMP to edit.
	goFile := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte(goFile), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	// Create a go.mod so `go test` can work.
	goMod := "module test-repo\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
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
	slotPath := filepath.Join(slotDir, "worktree")
	desc := WorktreeDescriptor{
		RepositoryRoot: repoDir,
		ParentCommit:   parentCommit,
		TargetRef:      "refs/heads/feat/repair-omp",
		SlotID:         "slot_omp_test",
		SlotPath:       slotPath,
	}
	worktree, err := MaterializeWorktree(desc)
	if err != nil {
		t.Fatalf("MaterializeWorktree: %v", err)
	}
	defer RemoveWorktree(slotPath)

	// --- Step 6: Run real OMP adapter ---
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())

	// Write a simple prompt for OMP.
	promptDir := t.TempDir()
	promptPath := filepath.Join(promptDir, "prompt.md")
	promptContent := `# Repair Task

Add a function called ` + "`add`" + ` that takes two integers and returns their sum.
Write the function in main.go.
`
	if err := os.WriteFile(promptPath, []byte(promptContent), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	outputPath := filepath.Join(promptDir, "output.md")
	sessionDir := filepath.Join(promptDir, "omp-session")

	adapter, err := RunOMPAdapter(slotPath, uid, gid, OMPAdapterConfig{
		WrapperPath: wrapperPath,
		Workflow:    "coupled-v1",
		Provider:    provider,
		Model:       model,
		TaskTier:    "mechanical",
		Role:        "implement",
		RunID:       "omp-integration-test-1",
		SessionDir:  sessionDir,
		Timeout:     120,
		PromptPath:  promptPath,
		OutputPath:  outputPath,
		Workdir:     slotPath,
	})
	if err != nil {
		t.Fatalf("RunOMPAdapter: %v", err)
	}
	if adapter.UID != uid {
		t.Errorf("adapter UID: got %d, want %d", adapter.UID, uid)
	}
	if adapter.TerminalProofHash == "" {
		t.Error("terminal proof hash should not be empty")
	}

	// --- Step 5b: Compute diff closure after adapter execution ---
	diff, err := ComputeDiffClosure(slotPath)
	if err != nil {
		t.Fatalf("ComputeDiffClosure: %v", err)
	}
	worktree.Diff = diff
	if diff.StatusHash == hashString("") {
		t.Error("diff closure status hash should reflect adapter changes, not be empty")
	}
	t.Logf("diff status: %s", diff.StatusHash[:min(20, len(diff.StatusHash))])

	// --- Step 7: Run Go test profile (if go.mod exists) ---
	var testResult *TestProfileResult
	if _, err := os.Stat(filepath.Join(slotPath, "go.mod")); err == nil {
		testResult, err = RunGoTestProfile(slotPath, uid, gid, []string{"go", "test", "./...", "-count=1", "-timeout", "30s"})
		if err != nil {
			t.Fatalf("RunGoTestProfile: %v", err)
		}
		t.Logf("test pass: %v, output size: %d", testResult.Pass, testResult.TestOutputSize)
	} else {
		// Fall back to fabricated test result if no go.mod.
		testResult = &TestProfileResult{
			ToolchainManifestHash: hashString("go_test"),
			TestProfileHash:       hashString("go test ./..."),
			TestTerminalProofHash: adapter.TerminalProofHash,
			TestResultHash:        hashString("SKIP"),
			TestOutputHash:        hashString("no go.mod"),
			TestOutputSize:        0,
			TestCommandHash:       hashString("skip"),
			TestCapabilityHash:    hashString("test_capability"),
			Pass:                  true,
			Output:                "skipped: no go.mod",
		}
	}

	// --- Step 8: Produce signed attestation ---
	repairCtx := RepairContext{
		AuthorizationHash:                "sha256:auth_omp",
		ApprovalHash:                     "sha256:approval_omp",
		RequestHash:                      "sha256:request_omp",
		DispatchHash:                     "sha256:dispatch_omp",
		AttemptHash:                      "sha256:attempt_omp_1",
		AttemptNumber:                    1,
		AttemptCap:                       repaircontract.AttemptCap,
		ReleasePinsHash:                  material.Verifier().Pins().ReleasePinsHash,
		TrustBundleHash:                  material.Verifier().Bundle().TrustBundleHash,
		RepairAttestorCertificateHash:    material.Verifier().ExpectedAttestorCertificateHash(),
		RepairAttestorRootID:             material.RootID(),
		RepairAttestorLeafSPKI:           material.SignerSPKI(),
		RequestNonceHash:                 "sha256:nonce_req_omp",
		ResponseNonceHash:                "sha256:nonce_resp_omp",
		ChannelHash:                      "sha256:channel_omp",
		RepositoryBindingHash:            "sha256:repo_binding_omp",
		RepositoryIdentityHash:           "sha256:repo_identity_omp",
		CommonGitIdentityHash:            "sha256:git_identity_omp",
		GitExecutableIdentityHash:        "sha256:git_exec_omp",
		EffectTimeValidationTimestamp:    now.UTC().Format(time.RFC3339Nano),
		MaterializationClaimHash:         "sha256:mat_claim_omp",
		AdapterClaimHash:                 "sha256:adapter_claim_omp",
		TestClaimHash:                    "sha256:test_claim_omp",
		PredecessorClaimHash:             "sha256:pred_claim_omp",
		SupervisorJournalHeadHash:        "sha256:journal_head_omp",
		SupervisorJournalPredecessorHash: "sha256:journal_pred_omp",
		BootEpochID:                      "boot_epoch_omp_v1",
		BootEpochHash:                    "sha256:boot_epoch_omp",
	}

	row, err := ProduceSignedAttestation(repairCtx, worktree, adapter, testResult, material, s, now)
	if err != nil {
		t.Fatalf("ProduceSignedAttestation: %v", err)
	}

	// --- Verify: attestation state ---
	if row.State != string(repaircontract.AttestationWaitingForReview) {
		t.Errorf("state: got %s, want %s", row.State, repaircontract.AttestationWaitingForReview)
	}

	// --- Verify: signature round-trip ---
	retrieved, err := s.GetRepairAttestation(ctx, row.AttestationHash)
	if err != nil {
		t.Fatalf("GetRepairAttestation: %v", err)
	}
	if err := VerifyAttestationWithAnanke(retrieved, material.Verifier(), material.PublicKey()); err != nil {
		t.Fatalf("VerifyAttestationWithAnanke: %v", err)
	}

	t.Logf("OMP E2E PASS: attestation %s in state %s, signature verified, diff status hash %s",
		row.AttestationHash[:min(20, len(row.AttestationHash))], row.State,
		diff.StatusHash[:min(20, len(diff.StatusHash))])
}

// (removed duplicate min — use builtin min from Go 1.21+)

// TestOMPAdapterConfigValidation tests the OMP adapter config validation.
func TestOMPAdapterConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  OMPAdapterConfig
		wantErr bool
	}{
		{"empty wrapper", OMPAdapterConfig{Workflow: "w", Provider: "p", Model: "m", PromptPath: "/p", OutputPath: "/o", Timeout: 60}, true},
		{"empty workflow", OMPAdapterConfig{WrapperPath: "/w", Provider: "p", Model: "m", PromptPath: "/p", OutputPath: "/o", Timeout: 60}, true},
		{"empty provider", OMPAdapterConfig{WrapperPath: "/w", Workflow: "w", Model: "m", PromptPath: "/p", OutputPath: "/o", Timeout: 60}, true},
		{"empty model", OMPAdapterConfig{WrapperPath: "/w", Workflow: "w", Provider: "p", PromptPath: "/p", OutputPath: "/o", Timeout: 60, Role: "r", RunID: "id"}, true},
		{"empty role", OMPAdapterConfig{WrapperPath: "/w", Workflow: "w", Provider: "p", Model: "m", PromptPath: "/p", OutputPath: "/o", Timeout: 60, RunID: "id"}, true},
		{"empty run-id", OMPAdapterConfig{WrapperPath: "/w", Workflow: "w", Provider: "p", Model: "m", PromptPath: "/p", OutputPath: "/o", Timeout: 60, Role: "r"}, true},
		{"empty prompt", OMPAdapterConfig{WrapperPath: "/w", Workflow: "w", Provider: "p", Model: "m", OutputPath: "/o", Timeout: 60, Role: "r", RunID: "id"}, true},
		{"zero timeout", OMPAdapterConfig{WrapperPath: "/w", Workflow: "w", Provider: "p", Model: "m", PromptPath: "/p", OutputPath: "/o", Role: "r", RunID: "id"}, true},
		{"valid", OMPAdapterConfig{WrapperPath: "/w", Workflow: "w", Provider: "p", Model: "m", PromptPath: "/p", OutputPath: "/o", Timeout: 60, Role: "r", RunID: "id"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOMPConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOMPConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestEstimateOMPTimeout tests timeout estimation by task tier.
func TestEstimateOMPTimeout(t *testing.T) {
	tests := []struct {
		tier string
		want int
	}{
		{"mechanical", 120},
		{"normal", 600},
		{"hard", 1200},
		{"unknown", 600},
	}
	for _, tt := range tests {
		if got := EstimateOMPTimeout(tt.tier); got != tt.want {
			t.Errorf("EstimateOMPTimeout(%q) = %d, want %d", tt.tier, got, tt.want)
		}
	}
}

// TestIsOMPTimeoutError tests timeout error detection.
func TestIsOMPTimeoutError(t *testing.T) {
	if IsOMPTimeoutError(nil) {
		t.Error("nil should not be timeout")
	}
	if !IsOMPTimeoutError(strErr("OMP_TIMEOUT: process exceeded deadline")) {
		t.Error("OMP_TIMEOUT should be detected")
	}
	if IsOMPTimeoutError(strErr("some other error")) {
		t.Error("non-timeout error should not be detected")
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func strErr(msg string) error { return stringError(msg) }
