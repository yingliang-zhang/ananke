package trustedsupervisor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func makeAuditTestDirectoriesRemovable(root string) {
	_ = filepath.Walk(root, func(path string, information os.FileInfo, err error) error {
		if err == nil && information.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
}

func restoreAuditSealedHomeModeForTest(t *testing.T, homeDir string) {
	t.Helper()
	t.Cleanup(func() { makeAuditTestDirectoriesRemovable(homeDir) })
}

func TestAuditModelsConfigIsExactClosedSudoRoute(t *testing.T) {
	want := "providers:\n" +
		"  sudo:\n" +
		"    baseUrl: http://127.0.0.1:43210/v1\n" +
		"    apiKey: SUDO_API_KEY\n" +
		"    api: openai-responses\n" +
		"    models:\n" +
		"    - id: gpt-5.6-sol\n" +
		"      name: Sudo GPT-5.6 Sol\n" +
		"      api: openai-responses\n" +
		"      reasoning: true\n" +
		"      input:\n" +
		"      - text\n" +
		"      contextWindow: 372000\n" +
		"      maxTokens: 65536\n" +
		"      omitMaxOutputTokens: true\n" +
		"      compat:\n" +
		"        supportsReasoningEffort: true\n" +
		"        supportsDeveloperRole: true\n"
	entry := executionPolicyEntry{
		HermesProvider: "custom:sudo", HermesModel: "gpt-5.6-sol", TaskTier: "normal",
		CredentialEnvironmentNames: []string{"SUDO_CODING_KEY"},
	}
	contents, err := auditModelsConfigBytes(entry, "127.0.0.1:43210")
	if err != nil || string(contents) != want {
		t.Fatalf("direct Sudo models.yml mismatch: %v", err)
	}
	for _, authority := range []string{"localhost:43210", "127.0.0.1:0", "127.0.0.1:43210/extra", "[::1]:43210", "attacker.example:43210"} {
		if _, err := auditModelsConfigBytes(entry, authority); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("models config authority %q error = %v, want %v", authority, err, ErrAuthentication)
		}
	}
	for _, mismatch := range []executionPolicyEntry{
		{HermesProvider: "anthropic", HermesModel: "gpt-5.6-sol", CredentialEnvironmentNames: []string{"SUDO_CODING_KEY"}},
		{HermesProvider: "custom:sudo", HermesModel: "claude-sonnet-4-5", CredentialEnvironmentNames: []string{"SUDO_CODING_KEY"}},
		{HermesProvider: "custom:sudo", HermesModel: "gpt-5.6-sol", CredentialEnvironmentNames: []string{"OPENAI_API_KEY"}},
		{HermesProvider: "custom:sudo", HermesModel: "gpt-5.6-sol", TaskTier: "normal", CredentialEnvironmentNames: []string{"SUDO_API_KEY"}},
		{HermesProvider: "custom:sudo", HermesModel: "gpt-5.6-sol", CredentialEnvironmentNames: []string{"SUDO_CODING_KEY", "SUDO_API_KEY"}},
	} {
		if _, err := auditModelsConfigBytes(mismatch, "127.0.0.1:43210"); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("mismatched models route %+v error = %v, want %v", mismatch, err, ErrAuthentication)
		}
	}
}

func TestAuditInvocationMaterializesPrivateStateAndBoundModels(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_private_state_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_private_state_attempt_1", "audit_run_private_state_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if err := bindAuditInvocationTransport(material.entry, &invocation, "127.0.0.1:43210"); err != nil {
		t.Fatal(err)
	}
	restoreAuditSealedHomeModeForTest(t, invocation.HomeDir)
	wantNativeAddonPath := filepath.Join(material.entry.OMPRuntimeAuthority.NativeDataRoot, "omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
	if invocation.AgentDir != filepath.Join(invocation.TemporaryDir, "omp-agent") ||
		invocation.ModelsPath != filepath.Join(invocation.AgentDir, "models.yml") ||
		invocation.HomeDir != filepath.Join(invocation.TemporaryDir, "home") ||
		invocation.HomeStateDir != filepath.Join(invocation.HomeDir, ".omp") ||
		invocation.HomeRunDir != filepath.Join(invocation.HomeStateDir, "run") ||
		invocation.NativeAddonPath != material.entry.OMPNativeAddon.Path || invocation.NativeAddonPath != wantNativeAddonPath {
		t.Fatalf("private direct OMP paths = agent %q models %q home %q state %q run %q pinned addon %q", invocation.AgentDir, invocation.ModelsPath, invocation.HomeDir, invocation.HomeStateDir, invocation.HomeRunDir, invocation.NativeAddonPath)
	}
	for _, directory := range []struct {
		path string
		mode os.FileMode
	}{
		{path: invocation.AgentDir, mode: 0o700},
		{path: invocation.HomeDir, mode: 0o700},
		{path: invocation.HomeStateDir, mode: 0o500},
		{path: invocation.HomeRunDir, mode: 0o700},
	} {
		information, err := os.Lstat(directory.path)
		if err != nil || !information.IsDir() || information.Mode().Perm() != directory.mode || information.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("private directory %q = %v, %v; want mode %04o", directory.path, information, err, directory.mode)
		}
	}
	currentRoots, err := currentAuditInvocationOwnedRoots(invocation)
	if err != nil || len(currentRoots) != 8 {
		t.Fatalf("sealed HOME owned roots = %+v, %v; want exact eight bindings", currentRoots, err)
	}
	contents, err := os.ReadFile(invocation.ModelsPath)
	if err != nil {
		t.Fatal(err)
	}
	information, err := os.Lstat(invocation.ModelsPath)
	if err != nil || information.Mode().Perm() != 0o600 || information.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("models.yml metadata = %v, %v", information, err)
	}
	addonContents, err := os.ReadFile(invocation.NativeAddonPath)
	if err != nil {
		t.Fatal(err)
	}
	addonInformation, err := os.Lstat(invocation.NativeAddonPath)
	if err != nil || addonInformation.Mode().Perm() != 0o400 || addonInformation.Mode()&os.ModeSymlink != 0 ||
		invocation.NativeAddonSHA256 != hashJournalBytes(addonContents) || invocation.NativeAddonSHA256 != material.entry.OMPNativeAddon.SHA256 ||
		invocation.NativeAddonSize != int64(len(addonContents)) {
		t.Fatalf("pinned native addon = %v, %v, hash %q, size %d", addonInformation, err, invocation.NativeAddonSHA256, invocation.NativeAddonSize)
	}
	if invocation.ModelsSHA256 != hashJournalBytes(contents) || invocation.ModelsSize != int64(len(contents)) ||
		!protocolHashPattern.MatchString(invocation.CommandDescriptorHash) {
		t.Fatalf("models/descriptor binding = hash %q size %d command %q", invocation.ModelsSHA256, invocation.ModelsSize, invocation.CommandDescriptorHash)
	}
	changed := material.entry
	changed.OMPNativeAddon.SHA256 = testHash("different-native-addon")
	changedDescriptor, err := auditCommandDescriptorHash(changed, invocation.PromptSHA256, invocation.SessionRunID, invocation.Resume)
	if err != nil || changedDescriptor == invocation.CommandDescriptorHash {
		t.Fatalf("command descriptor did not bind native addon hash: %q, %v", changedDescriptor, err)
	}
	changed = material.entry
	changed.GitExecutable.SHA256 = testHash("different-git-executable")
	changedDescriptor, err = auditCommandDescriptorHash(changed, invocation.PromptSHA256, invocation.SessionRunID, invocation.Resume)
	if err != nil || changedDescriptor == invocation.CommandDescriptorHash {
		t.Fatalf("command descriptor did not bind Git executable identity: %q, %v", changedDescriptor, err)
	}
	changed = material.entry
	changed.WorkRoot = filepath.Join(filepath.Dir(material.entry.WorkRoot), "different-work-root")
	changedDescriptor, err = auditCommandDescriptorHash(changed, invocation.PromptSHA256, invocation.SessionRunID, invocation.Resume)
	if err != nil || changedDescriptor == invocation.CommandDescriptorHash {
		t.Fatalf("command descriptor did not bind Git discovery ceiling: %q, %v", changedDescriptor, err)
	}
	if auditCommandDescriptorSchemaVersion != "ananke.local-trusted-supervisor-command-descriptor.v7" ||
		auditGitRepositoryDiscoveryPolicy != "exact_snapshot_parent_ceiling_no_global_or_system_config_v1" ||
		auditChildExecutablePath != "/Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin" {
		t.Fatal("command descriptor lost the fixed CLT Git startup policy contract")
	}
	if err := validateAuditInvocationTransport(material.entry, invocation); err != nil {
		t.Fatalf("validate exact transport: %v", err)
	}
	if err := os.WriteFile(invocation.ModelsPath, []byte("providers: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateAuditInvocationTransport(material.entry, invocation); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("mutated models error = %v, want %v", err, ErrAuthentication)
	}
	if err := os.WriteFile(invocation.ModelsPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(invocation.NativeAddonPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invocation.NativeAddonPath, []byte("mutated copied addon"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateAuditInvocationTransport(material.entry, invocation); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("mutated copied addon error = %v, want %v", err, ErrAuthentication)
	}
}

func TestAuditSealedHomeOwnedRootBindingsRejectDrift(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *auditInvocation)
	}{
		{name: "omitted", mutate: func(_ *testing.T, invocation *auditInvocation) {
			invocation.OwnedRoots = invocation.OwnedRoots[:len(invocation.OwnedRoots)-1]
		}},
		{name: "extra", mutate: func(_ *testing.T, invocation *auditInvocation) {
			invocation.OwnedRoots = append(invocation.OwnedRoots, invocation.OwnedRoots[len(invocation.OwnedRoots)-1])
		}},
		{name: "swapped", mutate: func(_ *testing.T, invocation *auditInvocation) {
			left, right := len(invocation.OwnedRoots)-2, len(invocation.OwnedRoots)-1
			invocation.OwnedRoots[left], invocation.OwnedRoots[right] = invocation.OwnedRoots[right], invocation.OwnedRoots[left]
		}},
		{name: "state mode binding", mutate: func(_ *testing.T, invocation *auditInvocation) {
			for index := range invocation.OwnedRoots {
				if invocation.OwnedRoots[index].Role == "direct_omp_home_state" {
					invocation.OwnedRoots[index].Mode ^= 0o200
				}
			}
		}},
		{name: "run parent binding", mutate: func(_ *testing.T, invocation *auditInvocation) {
			for index := range invocation.OwnedRoots {
				if invocation.OwnedRoots[index].Role == "direct_omp_home_run" {
					invocation.OwnedRoots[index].ParentInode = "999999999"
				}
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			_, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_home_binding_"+strconv.Itoa(len(testCase.name)))
			testCase.mutate(t, &invocation)
			if err := validateAuditInvocationTransport(material.entry, invocation); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("sealed HOME binding drift error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestAuditSealedHomeReplacementSymlinkAndModeDriftFailBeforeEffect(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, auditInvocation)
	}{
		{name: "state mode", mutate: func(t *testing.T, invocation auditInvocation) {
			if err := os.Chmod(invocation.HomeStateDir, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "run replacement", mutate: func(t *testing.T, invocation auditInvocation) {
			if err := os.Chmod(invocation.HomeStateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			original := invocation.HomeRunDir + ".original"
			if err := os.Rename(invocation.HomeRunDir, original); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(invocation.HomeRunDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(invocation.HomeStateDir, 0o500); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "state symlink", mutate: func(t *testing.T, invocation auditInvocation) {
			original := invocation.HomeStateDir + ".original"
			if err := os.Rename(invocation.HomeStateDir, original); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(original, invocation.HomeStateDir); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			_, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_home_effect_"+strconv.Itoa(len(testCase.name)))
			testCase.mutate(t, invocation)
			if err := validateAuditInvocationTransport(material.entry, invocation); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("sealed HOME live drift error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestCleanupAuditInvocationAllValidatesScrubsAndIsIdempotent(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_cleanup_all_001")
	writeAuditCleanupSecretsForTest(t, invocation)

	if err := cleanupAuditInvocationAll(material.entry, invocation); err != nil {
		t.Fatalf("first full cleanup: %v", err)
	}
	assertAuditCleanupPathsAbsent(t, invocation, snapshot)
	if err := cleanupAuditInvocationAll(material.entry, invocation); err != nil {
		t.Fatalf("repeated full cleanup: %v", err)
	}
	assertAuditCleanupPathsAbsent(t, invocation, snapshot)
}

func TestAuthenticatedAuditCleanupScrubsSealedHomeRunAndRejectsStateDecoy(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		material := newGitArchivePolicyMaterial(t)
		snapshot, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_home_cleanup_success")
		if err := os.WriteFile(filepath.Join(invocation.HomeRunDir, "runtime-state"), []byte("credential-must-be-scrubbed"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cleanupAuthenticatedAuditInvocationAll(material.entry, invocation); err != nil {
			t.Fatalf("authenticated sealed HOME cleanup: %v", err)
		}
		assertAuditCleanupPathsAbsent(t, invocation, snapshot)
	})
	t.Run("state decoy", func(t *testing.T) {
		material := newGitArchivePolicyMaterial(t)
		_, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_home_cleanup_decoy")
		retained := invocation.HomeStateDir + ".retained"
		if err := os.Rename(invocation.HomeStateDir, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(invocation.HomeStateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		decoy := filepath.Join(invocation.HomeStateDir, "decoy")
		if err := os.WriteFile(decoy, []byte("must-survive"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cleanupAuthenticatedAuditInvocationAll(material.entry, invocation); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("sealed HOME decoy cleanup error = %v, want %v", err, ErrAuthentication)
		}
		for _, path := range []string{retained, decoy} {
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("authenticated cleanup altered retained path: %v", err)
			}
		}
	})
}

func TestCleanupAuditInvocationTransientThenFullIsIdempotent(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_cleanup_timeout_001")
	writeAuditCleanupSecretsForTest(t, invocation)

	if err := cleanupAuditInvocationTransient(material.entry, invocation); err != nil {
		t.Fatalf("timeout transient cleanup: %v", err)
	}
	for _, path := range []string{invocation.PromptDir, invocation.OutputDir, invocation.TemporaryDir} {
		assertAuditCleanupPathAbsent(t, path)
	}
	for _, path := range []string{invocation.SessionDir, snapshot.RunRoot} {
		if information, err := os.Lstat(path); err != nil || !information.IsDir() {
			t.Fatalf("retained timeout path %q = %v, %v", path, information, err)
		}
	}
	if err := cleanupAuditInvocationTransient(material.entry, invocation); err != nil {
		t.Fatalf("repeated timeout transient cleanup: %v", err)
	}
	if err := cleanupAuditInvocationAll(material.entry, invocation); err != nil {
		t.Fatalf("full cleanup after timeout transient cleanup: %v", err)
	}
	assertAuditCleanupPathsAbsent(t, invocation, snapshot)
}

func TestCleanupOwnedAuditInvocationsFailsClosedOnMismatchedOrPartialSharedRoots(t *testing.T) {
	const runID = "audit_run_cleanup_owned_001"
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, executionPolicyEntry, auditSnapshot, *auditInvocation)
	}{
		{name: "mismatched session", mutate: func(_ *testing.T, entry executionPolicyEntry, _ auditSnapshot, invocation *auditInvocation) {
			invocation.SessionRunID = "audit_run_cleanup_other_001"
			invocation.SessionDir = filepath.Join(entry.SessionRoot, invocation.SessionRunID)
		}},
		{name: "mismatched work", mutate: func(_ *testing.T, entry executionPolicyEntry, _ auditSnapshot, invocation *auditInvocation) {
			invocation.WorkDir = filepath.Join(entry.WorkRoot, "audit_run_cleanup_other_001", "source")
		}},
		{name: "partial shared roots", mutate: func(t *testing.T, _ executionPolicyEntry, _ auditSnapshot, invocation *auditInvocation) {
			if err := os.Remove(invocation.SessionDir); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			snapshot := materializeSnapshotForExecutorTest(t, material, runID)
			first, err := prepareAuditInvocation(material.policy, material.entry, snapshot, runID+"_attempt_1", runID, auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			second, err := prepareAuditInvocation(material.policy, material.entry, snapshot, runID+"_attempt_2", runID, auditResume{
				SessionUUID: "019f9a4a-a904-7000-b341-e07ecf0e3baf", SynthesizeOnly: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			testCase.mutate(t, material.entry, snapshot, &second)
			if err := cleanupOwnedAuditInvocations(material.policy, material.entry, snapshot, runID, []auditInvocation{first, second}); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("closed owned cleanup error = %v, want %v", err, ErrAuthentication)
			}
			for _, invocation := range []auditInvocation{first, second} {
				for _, path := range []string{invocation.PromptDir, invocation.OutputDir, invocation.TemporaryDir} {
					assertAuditCleanupPathAbsent(t, path)
				}
			}
			if information, err := os.Lstat(snapshot.RunRoot); err != nil || !information.IsDir() {
				t.Fatalf("fail-closed snapshot root %q = %v, %v", snapshot.RunRoot, information, err)
			}
			if testCase.name != "partial shared roots" {
				if information, err := os.Lstat(first.SessionDir); err != nil || !information.IsDir() {
					t.Fatalf("fail-closed session root %q = %v, %v", first.SessionDir, information, err)
				}
			}
			if err := cleanupAuditInvocationAll(material.entry, first); err != nil {
				t.Fatalf("test cleanup after closed rejection: %v", err)
			}
		})
	}
}

func TestCleanupAuditInvocationRejectsPartialOrUnexpectedTransientState(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, auditInvocation)
	}{
		{name: "missing models", mutate: func(t *testing.T, invocation auditInvocation) {
			if err := os.Remove(invocation.ModelsPath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing native addon", mutate: func(t *testing.T, invocation auditInvocation) {
			if err := os.Remove(invocation.NativeAddonPath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing temporary root", mutate: func(t *testing.T, invocation auditInvocation) {
			if err := os.Chmod(invocation.HomeStateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(invocation.TemporaryDir); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "temporary root regular file", mutate: func(t *testing.T, invocation auditInvocation) {
			if err := os.Chmod(invocation.HomeStateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(invocation.TemporaryDir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(invocation.TemporaryDir, []byte("unexpected-cleanup-root"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "temporary root symlink", mutate: func(t *testing.T, invocation auditInvocation) {
			if err := os.Chmod(invocation.HomeStateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(invocation.TemporaryDir); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(invocation.PromptDir, invocation.TemporaryDir); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			_, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_cleanup_partial_001")
			testCase.mutate(t, invocation)
			if err := cleanupAuditInvocationAll(material.entry, invocation); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("closed cleanup error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestCleanupAuditInvocationAfterTransientRejectsPinnedSourceAddonMutation(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot, invocation := boundAuditInvocationForCleanupTest(t, material, "audit_run_cleanup_source_addon_001")
	if err := cleanupAuditInvocationTransient(material.entry, invocation); err != nil {
		t.Fatalf("timeout transient cleanup: %v", err)
	}
	if err := os.WriteFile(material.entry.OMPNativeAddon.Path, []byte("mutated-source-addon"), 0o700); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("sealed source addon mutation error = %v, want permission denied", err)
	}
	if err := cleanupAuditInvocationAll(material.entry, invocation); err != nil {
		t.Fatalf("cleanup after denied source addon mutation: %v", err)
	}
	for _, path := range []string{invocation.SessionDir, snapshot.RunRoot} {
		assertAuditCleanupPathAbsent(t, path)
	}
}

func boundAuditInvocationForCleanupTest(t *testing.T, material gitArchivePolicyMaterial, runID string) (auditSnapshot, auditInvocation) {
	t.Helper()
	snapshot := materializeSnapshotForExecutorTest(t, material, runID)
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, runID+"_attempt_1", runID, auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if err := bindAuditInvocationTransport(material.entry, &invocation, "127.0.0.1:43210"); err != nil {
		t.Fatal(err)
	}
	restoreAuditSealedHomeModeForTest(t, invocation.HomeDir)
	return snapshot, invocation
}

func writeAuditCleanupSecretsForTest(t *testing.T, invocation auditInvocation) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(invocation.PromptDir, "credential"),
		filepath.Join(invocation.OutputDir, "credential"),
		filepath.Join(invocation.SessionDir, "credential"),
		filepath.Join(invocation.TemporaryDir, "credential"),
	} {
		if err := os.WriteFile(path, []byte("credential-must-be-scrubbed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertAuditCleanupPathsAbsent(t *testing.T, invocation auditInvocation, snapshot auditSnapshot) {
	t.Helper()
	for _, path := range []string{invocation.PromptDir, invocation.OutputDir, invocation.SessionDir, invocation.TemporaryDir, snapshot.RunRoot} {
		assertAuditCleanupPathAbsent(t, path)
	}
}

func assertAuditCleanupPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup path %q remains: %v", path, err)
	}
}

func TestAuditInvocationEnvironmentPinsStateConfigSessionAndOMPPath(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_environment_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_environment_attempt_1", "audit_run_environment_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if err := bindAuditInvocationTransport(material.entry, &invocation, "127.0.0.1:43210"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(material.directory, "ambient-path-must-not-win"))
	environment, _, err := auditInvocationEnvironment(material.entry, invocation)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(environment))
	for _, item := range environment {
		name, value, found := strings.Cut(item, "=")
		if !found {
			t.Fatalf("malformed environment item %q", item)
		}
		got[name] = value
	}
	want := map[string]string{
		"GIT_CEILING_DIRECTORIES": filepath.Dir(invocation.WorkDir),
		"GIT_CONFIG_GLOBAL":       "/dev/null",
		"GIT_CONFIG_NOSYSTEM":     "1",
		"HOME":                    invocation.HomeDir,
		"LANG":                    "C",
		"LC_ALL":                  "C",
		"OMP_SESSION_ROOT":        invocation.SessionDir,
		"PATH":                    auditChildExecutablePath,
		"PI_CODING_AGENT_DIR":     invocation.AgentDir,
		"PI_CONFIG_DIR":           ".omp/run",
		"TMPDIR":                  invocation.TemporaryDir,
		"TZ":                      "UTC",
		"XDG_DATA_HOME":           material.entry.OMPRuntimeAuthority.NativeDataRoot,
	}
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("environment %s = %q, want %q", name, got[name], value)
		}
	}
	if material.entry.OMPRuntimeAuthority.LauncherMode != atomicOMPLauncherModeDirectPinned ||
		material.entry.OMPRuntimeAuthority.WrapperCompatibilityOracleSHA256 != material.entry.Wrapper.SHA256 ||
		material.entry.OMPRuntimeAuthority.SandboxTargetPolicy != atomicOMPSandboxTargetPolicyExactPinned {
		t.Fatal("runtime authority lost direct launcher or compatibility-oracle binding")
	}
	for _, forbidden := range []string{
		"ALL_PROXY", "DEVELOPER_DIR", "GIT_EXEC_PATH", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "OMP_PROFILE", "PI_PROFILE", "XDG_CONFIG_HOME",
	} {
		if _, exists := got[forbidden]; exists {
			t.Fatalf("environment retained ambient transport/config selector %s", forbidden)
		}
	}
}

func TestAuditInvocationEnvironmentProjectsPreferredSudoCredentialToRuntimeName(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_credential_projection_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_credential_projection_attempt_1", "audit_run_credential_projection_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if err := bindAuditInvocationTransport(material.entry, &invocation, "127.0.0.1:43210"); err != nil {
		t.Fatal(err)
	}
	const secret = "preferred-coding-key-fixture"
	material.entry.CredentialEnvironmentNames = []string{"SUDO_CODING_KEY"}
	invocation.CommandDescriptorHash, err = auditCommandDescriptorHash(material.entry, invocation.PromptSHA256, invocation.SessionRunID, invocation.Resume)
	if err != nil {
		t.Fatal(err)
	}
	environment, credentials, err := auditInvocationEnvironmentWithLookup(material.entry, invocation, func(name string) (string, bool) {
		switch name {
		case "SUDO_CODING_KEY":
			return secret, true
		case "SUDO_API_KEY":
			return "ambient-api-key-must-not-enter-child", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(environment))
	for _, item := range environment {
		name, value, found := strings.Cut(item, "=")
		if !found {
			t.Fatalf("malformed environment item %q", item)
		}
		got[name] = value
	}
	if got["SUDO_API_KEY"] != secret {
		t.Fatalf("child SUDO_API_KEY was not projected from the selected source")
	}
	if _, exists := got["SUDO_CODING_KEY"]; exists {
		t.Fatal("child retained source-only SUDO_CODING_KEY")
	}
	if !reflect.DeepEqual(credentials, []string{secret}) {
		t.Fatalf("tracked credential values = %#v, want selected source only", credentials)
	}
}

func TestExecutionPolicyRejectsOMPReplacementAndPathShadow(t *testing.T) {
	t.Run("replacement", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		policy, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		if err := policy.ValidateEffectBoundary(material.entry); err != nil {
			t.Fatal(err)
		}
		original := material.entry.OMPExecutable.Path
		if err := os.Rename(original, original+".pinned"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(original, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := policy.ValidateEffectBoundary(material.entry); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("replaced OMP error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("PATH shadow", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		shadow := filepath.Join(material.directory, "shadow")
		if err := os.Mkdir(shadow, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shadow, "omp"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		entry := material.entry
		entry.OMPExecutableRoot = directoryIdentityForTest(t, shadow)
		entry = mustSealExecutionPolicyEntryForTest(t, entry)
		writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{entry})
		if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("PATH-shadow policy error = %v, want %v", err, ErrAuthentication)
		}
	})
}

func TestAuditHTTPGatewayRoutesOnlyExactProviderRequestOverPinnedTLS(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	const requestBody = `{"model":"gpt-5.6-sol","input":"preserve exactly"}`
	const authorization = "Bearer probe-secret-never-log"
	var upstreamRequests, upstreamDials atomic.Int32
	var upstreamMethod, upstreamHost, upstreamRequestURI, upstreamSNI, upstreamAuthorization, upstreamContentType string
	var upstreamBody []byte
	server, roots := newAuditGatewayTLSServerForTest(t, "coding.sudoai.cc", func(writer http.ResponseWriter, request *http.Request) {
		upstreamRequests.Add(1)
		upstreamMethod = request.Method
		upstreamHost = request.Host
		upstreamRequestURI = request.RequestURI
		upstreamSNI = request.TLS.ServerName
		upstreamAuthorization = request.Header.Get("Authorization")
		upstreamContentType = request.Header.Get("Content-Type")
		upstreamBody, _ = io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Proxy-Authenticate", authorization)
		writer.Header().Set("X-Audit-Upstream", "preserved")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, `{"error":{"message":"deterministic probe rejection"}}`)
	})
	dependencies := auditBrokerDependencies{
		LookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("198.18.0.34")}}, nil
		},
		DialContext: func(ctx context.Context, network, authority string) (net.Conn, error) {
			upstreamDials.Add(1)
			if authority != "198.18.0.34:443" {
				t.Fatalf("TLS dial authority = %q, want pinned fake IP", authority)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
		TLSRootCAs: roots,
	}
	gateway, err := startAuditHTTPGateway(context.Background(), "custom:sudo", executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443}, time.Minute, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()

	request, err := http.NewRequest(http.MethodPost, "http://"+gateway.Address()+"/v1/responses", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("X-Audit-Upstream") != "preserved" || response.Header.Get("Proxy-Authenticate") != "" ||
		upstreamRequests.Load() != 1 || upstreamDials.Load() != 1 || upstreamMethod != http.MethodPost || upstreamHost != "coding.sudoai.cc" ||
		upstreamRequestURI != "/v1/responses" || upstreamSNI != "coding.sudoai.cc" || upstreamAuthorization != authorization ||
		upstreamContentType != "application/json" || !bytes.Equal(upstreamBody, []byte(requestBody)) {
		t.Fatalf("gateway route = status %d upstream_header %q proxy_auth %q requests %d dials %d method %q host %q target %q SNI %q auth_match %t content_type %q body_match %t",
			response.StatusCode, response.Header.Get("X-Audit-Upstream"), response.Header.Get("Proxy-Authenticate"), upstreamRequests.Load(), upstreamDials.Load(), upstreamMethod,
			upstreamHost, upstreamRequestURI, upstreamSNI, upstreamAuthorization == authorization, upstreamContentType, bytes.Equal(upstreamBody, []byte(requestBody)))
	}
}

func TestAuditHTTPGatewayTransparentFakeIPTLSRejectsWrongHostnameCertificate(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	var requests, dials atomic.Int32
	server, roots := newAuditGatewayTLSServerForTest(t, "attacker.example", func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	})
	gateway, err := startAuditHTTPGateway(context.Background(), "custom:sudo", executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443}, time.Minute, auditBrokerDependencies{
		LookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("198.18.0.34")}}, nil
		},
		DialContext: func(ctx context.Context, network, authority string) (net.Conn, error) {
			dials.Add(1)
			if authority != "198.18.0.34:443" {
				t.Fatalf("TLS dial authority = %q, want pinned fake IP", authority)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
		TLSRootCAs: roots,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	request, err := http.NewRequest(http.MethodPost, "http://"+gateway.Address()+"/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer must-not-cross-invalid-TLS")
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadGateway || requests.Load() != 0 || dials.Load() != 1 {
		t.Fatalf("wrong-host TLS result = status %d requests %d dials %d", response.StatusCode, requests.Load(), dials.Load())
	}
}

func TestAuditHTTPGatewayRejectsSmugglingAndAlternateRoutesBeforeDial(t *testing.T) {
	var dials atomic.Int32
	gateway, err := startAuditHTTPGateway(context.Background(), "custom:sudo", executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443}, time.Minute, auditBrokerDependencies{
		LookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("rejected request must not dial")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	authority := gateway.Address()
	for _, raw := range []string{
		fmt.Sprintf("GET /v1/responses HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\n\r\n", authority),
		fmt.Sprintf("POST /v1/chat/completions HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\n\r\n", authority),
		fmt.Sprintf("POST http://%s/v1/responses HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\n\r\n", authority, authority),
		fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost: attacker.example\r\nContent-Length: 0\r\n\r\n"),
		fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost: %s\r\nHost: %s\r\nContent-Length: 0\r\n\r\n", authority, authority),
		fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\nContent-Length: 1\r\n\r\nx", authority),
		fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost: %s\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n", authority),
		fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost : %s\r\nContent-Length: 0\r\n\r\n", authority),
		fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\n X-Smuggled: yes\r\n\r\n", authority),
		fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\n\r\nGET / HTTP/1.1\r\n\r\n", authority),
	} {
		connection, err := net.DialTimeout("tcp", authority, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(connection, raw); err != nil {
			connection.Close()
			t.Fatal(err)
		}
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		status, _ := bufio.NewReader(connection).ReadString('\n')
		_ = connection.Close()
		if !strings.Contains(status, " 400 ") {
			t.Fatalf("rejected gateway status = %q", status)
		}
	}
	if dials.Load() != 0 {
		t.Fatalf("rejected requests caused %d upstream dials", dials.Load())
	}
}

func TestAuditModelReportRejectsTimeoutMarkerSpoof(t *testing.T) {
	entry := executionPolicyEntry{Repository: executionPolicyDirectoryIdentity{Path: "/private/operator/repository"}}
	invocation := auditInvocation{PromptPath: "/private/operator/prompt", OutputPath: "/private/operator/output", SessionDir: "/private/operator/session", WorkDir: "/private/operator/work"}
	spoof := strings.Replace(validAuditModelReportJSONForTest, "One finding.", "[OMP_TIMEOUT]", 1)
	if _, err := decodeAuditModelReport([]byte(spoof), entry, invocation); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("timeout marker spoof error = %v, want %v", err, ErrAuthentication)
	}
}

func newAuditGatewayTLSServerForTest(t *testing.T, hostname string, handler http.HandlerFunc) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: hostname}, DNSNames: []string{hostname},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return server, roots
}

func TestAuditInstalledOMPProviderFreeTransportPreflight(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox transport proof")
	}
	executable := os.Getenv("ANANKE_PINNED_OMP_FIXTURE")
	nativeAddon := os.Getenv("ANANKE_PINNED_OMP_NATIVE_FIXTURE")
	if executable == "" || nativeAddon == "" {
		t.Skip("ANANKE_PINNED_OMP_FIXTURE and ANANKE_PINNED_OMP_NATIVE_FIXTURE not supplied")
	}
	result, err := runAuditInstalledOMPTransportProbe(
		context.Background(),
		fileIdentityForTest(t, executable),
		directoryIdentityForTest(t, filepath.Dir(executable)),
		fileIdentityForTest(t, nativeAddon),
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Method != http.MethodPost || result.Path != "/v1/responses" || result.Host == "" || result.RequestCount != 1 || !result.NetworkConfined ||
		!result.HomeLogsAbsent || result.OMPNativeAddonSHA256 != fileIdentityForTest(t, nativeAddon).SHA256 {
		t.Fatalf("installed OMP transport proof = %+v", result)
	}
	if strings.Contains(fmt.Sprint(result), "probe-secret") {
		t.Fatal("installed transport proof retained credential")
	}
}

type auditInstalledOMPTransportProbeResult struct {
	Method               string
	Path                 string
	Host                 string
	RequestCount         int
	NetworkConfined      bool
	HomeLogsAbsent       bool
	OMPNativeAddonSHA256 string
}

func runAuditInstalledOMPTransportProbe(ctx context.Context, executable executionPolicyFileIdentity, root executionPolicyDirectoryIdentity, nativeAddon executionPolicyFileIdentity) (auditInstalledOMPTransportProbeResult, error) {
	if ctx == nil || ctx.Err() != nil || !auditPlatformSupported(runtime.GOOS) {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("installed OMP transport probe platform")
	}
	entry := executionPolicyEntry{
		HermesProvider: "custom:sudo", HermesModel: "gpt-5.6-sol", TaskTier: "normal", CredentialEnvironmentNames: []string{"SUDO_CODING_KEY"},
		OMPVersion: supportedOMPVersion, OMPExecutable: executable, OMPExecutableRoot: root, OMPNativeAddon: nativeAddon,
	}
	if err := validatePinnedOMPExecutableRoot(entry.OMPExecutableRoot, entry.OMPExecutable); err != nil {
		return auditInstalledOMPTransportProbeResult{}, err
	}
	if err := validateOMPExecutableIdentity(entry.OMPExecutable); err != nil {
		return auditInstalledOMPTransportProbeResult{}, err
	}
	nativeContents, err := readValidatedOMPNativeAddon(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid()))
	if err != nil {
		return auditInstalledOMPTransportProbeResult{}, err
	}
	defer zeroBytes(nativeContents)
	const probeCredential = "probe-secret-never-print"
	var recordMu sync.Mutex
	credentialMatched := false
	result := auditInstalledOMPTransportProbeResult{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(request.Body, maxAuditBrokerBodyBytes+1))
		_ = request.Body.Close()
		recordMu.Lock()
		result.Method = request.Method
		result.Path = request.URL.Path
		result.Host = request.Host
		result.RequestCount++
		credentialMatched = request.Header.Get("Authorization") == "Bearer "+probeCredential
		recordMu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Connection", "close")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, `{"error":{"message":"deterministic transport preflight rejection"}}`)
	}))
	defer server.Close()
	authority := server.Listener.Addr().String()
	rootPath, err := os.MkdirTemp("", "ananke-omp-transport-probe-")
	if err != nil {
		return auditInstalledOMPTransportProbeResult{}, err
	}
	defer func() {
		_ = os.Chmod(filepath.Join(rootPath, "home", ".omp"), 0o700)
		_ = os.RemoveAll(rootPath)
	}()
	for _, name := range []string{"agent", "bin", "data", "home", "session", "tmp", "work"} {
		if err := os.Mkdir(filepath.Join(rootPath, name), 0o700); err != nil {
			return auditInstalledOMPTransportProbeResult{}, err
		}
	}
	agentDir := filepath.Join(rootPath, "agent")
	probeExecutablePath := filepath.Join(rootPath, "bin", "omp")
	dataDir := filepath.Join(rootPath, "data")
	homeDir := filepath.Join(rootPath, "home")
	sessionDir := filepath.Join(rootPath, "session")
	temporaryDir := filepath.Join(rootPath, "tmp")
	workDir := filepath.Join(rootPath, "work")
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.invalid/auditprobe\n\ngo 1.26\n"), 0o444); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("populate installed OMP probe work root")
	}
	if err := os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o444); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("populate installed OMP probe work root")
	}
	if err := os.Chmod(workDir, 0o555); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("seal installed OMP probe work root")
	}
	defer func() { _ = os.Chmod(workDir, 0o700) }()
	ompDir := filepath.Join(homeDir, ".omp")
	homeRunDir := filepath.Join(ompDir, "run")
	nativesDir := filepath.Join(dataDir, "omp", "natives")
	nativeVersionDir := filepath.Join(nativesDir, entry.OMPVersion)
	nativePath := filepath.Join(nativeVersionDir, auditOMPNativeAddonFilename)
	for _, directory := range []struct{ path, parent string }{
		{ompDir, homeDir}, {homeRunDir, ompDir}, {filepath.Dir(nativesDir), dataDir}, {nativesDir, filepath.Dir(nativesDir)}, {nativeVersionDir, nativesDir},
	} {
		if filepath.Dir(directory.path) != directory.parent || os.Mkdir(directory.path, 0o700) != nil {
			return auditInstalledOMPTransportProbeResult{}, authenticationError("create installed OMP probe runtime root")
		}
	}
	if err := os.Chmod(ompDir, 0o500); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("seal installed OMP probe HOME state")
	}

	models, err := auditModelsConfigBytes(entry, authority)
	if err != nil {
		return auditInstalledOMPTransportProbeResult{}, err
	}
	modelsPath := filepath.Join(agentDir, "models.yml")
	executableContents, _, err := readPinnedRegularFile(entry.OMPExecutable.Path, entry.OMPExecutable.OwnerUID, false, maxAuditOMPExecutableBytes)
	if err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("read installed OMP probe executable")
	}
	defer zeroBytes(executableContents)
	if err := writePrivateAuditFile(probeExecutablePath, executableContents, 0o500); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("copy installed OMP probe executable")
	}
	probeExecutableRead, probeExecutable, err := readPinnedRegularFile(probeExecutablePath, uint32(os.Getuid()), false, maxAuditOMPExecutableBytes)
	if err != nil || probeExecutable.SHA256 != entry.OMPExecutable.SHA256 || probeExecutable.Size != entry.OMPExecutable.Size {
		zeroBytes(probeExecutableRead)
		return auditInstalledOMPTransportProbeResult{}, authenticationError("pin copied installed OMP probe executable")
	}
	zeroBytes(probeExecutableRead)
	if err := writePrivateAuditFile(modelsPath, models, 0o600); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("create installed OMP probe models configuration")
	}
	if err := writePrivateAuditFile(nativePath, nativeContents, 0o400); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("copy installed OMP probe native addon")
	}
	modelsRead, _, err := readPinnedRegularFile(modelsPath, uint32(os.Getuid()), true, maxAuditModelsConfigBytes)
	if err != nil || !bytes.Equal(modelsRead, models) {
		zeroBytes(modelsRead)
		return auditInstalledOMPTransportProbeResult{}, authenticationError("installed OMP probe models binding")
	}
	zeroBytes(modelsRead)
	nativeRead, copiedNative, err := readPinnedRegularFile(nativePath, uint32(os.Getuid()), false, maxAuditOMPNativeAddonBytes)
	if err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("pin installed OMP probe native addon")
	}
	if copiedNative.Mode != 0o400 || copiedNative.SHA256 != entry.OMPNativeAddon.SHA256 || copiedNative.Size != entry.OMPNativeAddon.Size ||
		!bytes.Equal(nativeRead, nativeContents) {
		zeroBytes(nativeRead)
		return auditInstalledOMPTransportProbeResult{}, authenticationError("canonical installed OMP probe native addon")
	}
	zeroBytes(nativeRead)
	copiedRead, err := readPinnedOMPNativeAddon(copiedNative)
	if err != nil {
		return auditInstalledOMPTransportProbeResult{}, err
	}
	zeroBytes(copiedRead)
	if err := validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid())); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("pinned OMP native addon changed during probe copy")
	}
	result.OMPNativeAddonSHA256 = copiedNative.SHA256

	sandboxAuthority, err := auditSandboxBrokerAddress(authority)
	if err != nil {
		return auditInstalledOMPTransportProbeResult{}, err
	}
	profile := auditInstalledOMPProbeSandboxProfile(probeExecutable, executionPolicyDirectoryIdentity{Path: filepath.Dir(probeExecutable.Path)}, rootPath, nativePath, sandboxAuthority)
	nativeLiteral := `(literal "` + sandboxLiteral(nativePath) + `")`
	if strings.Count(profile, "(allow network-outbound") != 1 || !strings.Contains(profile, "(remote tcp \""+sandboxLiteral(sandboxAuthority)+"\")") ||
		strings.Contains(profile, "remote udp") || strings.Contains(profile, "*:443") || strings.Contains(profile, "*:53") || strings.Contains(profile, "mDNSResponder") ||
		strings.Count(profile, nativeLiteral) < 3 || !strings.Contains(profile, `(deny file-write* (subpath "`+sandboxLiteral(filepath.Dir(nativePath))+`"))`) ||
		!strings.Contains(profile, `(deny file-write* `+nativeLiteral+`)`) {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("installed OMP probe confinement")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.Command(auditSandboxExecutable, "-p", profile, probeExecutable.Path,
		"-p", readOnlyAuditPromptTemplate, "--yolo", "--max-time", "5",
		"--model", strings.TrimPrefix(entry.HermesProvider, "custom:")+"/"+entry.HermesModel, "--thinking", "xhigh", "--session-dir", sessionDir)
	command.Dir = workDir
	command.Env = []string{
		"HOME=" + homeDir, "LANG=C", "LC_ALL=C", "OMP_SESSION_ROOT=" + sessionDir,
		"PATH=" + root.Path + ":/usr/bin:/bin", "PI_CODING_AGENT_DIR=" + agentDir, "PI_CONFIG_DIR=.omp/run",
		"SUDO_API_KEY=" + probeCredential, "TMPDIR=" + temporaryDir, "TZ=UTC", "XDG_DATA_HOME=" + dataDir,
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout := &boundedCommandBuffer{limit: maxAuditCaptureBytes}
	stderr := &boundedCommandBuffer{limit: maxAuditCaptureBytes}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Start(); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("start installed OMP transport probe")
	}
	waiter := startAuditProcessWaiter(command)
	identity, inspectErr := inspectAuditProcess(command.Process.Pid)
	if inspectErr != nil {
		_ = command.Process.Kill()
		_ = waiter.await(context.Background(), defaultAuditKillGrace)
		return auditInstalledOMPTransportProbeResult{}, authenticationError("inspect installed OMP transport probe")
	}
	identity.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	select {
	case <-waiter.done:
	case <-probeCtx.Done():
		termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, systemAuditProcessOperations{}, defaultAuditTerminationBounds())
		if termination.Outcome != auditTerminationConfirmedExit {
			return auditInstalledOMPTransportProbeResult{}, termination.Failure
		}
		return auditInstalledOMPTransportProbeResult{}, ErrDeadline
	}
	confirmation := confirmAuditProcessExit(context.Background(), identity, waiter, systemAuditProcessOperations{}, defaultAuditTerminationBounds())
	if confirmation.Outcome != auditTerminationConfirmedExit {
		return auditInstalledOMPTransportProbeResult{}, confirmation.Failure
	}
	if stdout.err != nil || stderr.err != nil {
		return auditInstalledOMPTransportProbeResult{}, ErrLimit
	}
	stdoutBytes, stderrBytes := stdout.take(), stderr.take()
	defer zeroBytes(stdoutBytes)
	defer zeroBytes(stderrBytes)
	if bytes.Contains(stdoutBytes, []byte(probeCredential)) || bytes.Contains(stderrBytes, []byte(probeCredential)) {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("installed OMP probe credential output")
	}
	entries, err := os.ReadDir(ompDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "run" || !entries[0].IsDir() {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("installed OMP probe sealed HOME inventory")
	}
	homeEntries, err := os.ReadDir(homeDir)
	if err != nil || len(homeEntries) != 1 || homeEntries[0].Name() != ".omp" || !homeEntries[0].IsDir() {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("installed OMP probe exact HOME inventory")
	}
	result.HomeLogsAbsent = true
	copiedRead, err = readPinnedOMPNativeAddon(copiedNative)
	if err != nil || hashJournalBytes(copiedRead) != entry.OMPNativeAddon.SHA256 {
		zeroBytes(copiedRead)
		return auditInstalledOMPTransportProbeResult{}, authenticationError("installed OMP probe native addon replacement")
	}
	zeroBytes(copiedRead)
	if err := validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid())); err != nil {
		return auditInstalledOMPTransportProbeResult{}, authenticationError("pinned OMP native addon changed during probe")
	}

	recordMu.Lock()
	result.NetworkConfined = true
	final := result
	finalCredentialMatched := credentialMatched
	recordMu.Unlock()
	if final.RequestCount != 1 || final.Method != http.MethodPost || final.Path != "/v1/responses" || final.Host != authority || !finalCredentialMatched ||
		!final.HomeLogsAbsent || final.OMPNativeAddonSHA256 != entry.OMPNativeAddon.SHA256 {
		exitCode := -1
		if command.ProcessState != nil {
			exitCode = command.ProcessState.ExitCode()
		}
		return final, fmt.Errorf("%w: installed OMP explicit gateway route method=%q path=%q host=%q requests=%d credential=%t native=%q exit=%d wait=%v stdout=%q stderr=%q", ErrAuthentication, final.Method, final.Path, final.Host, final.RequestCount, finalCredentialMatched, final.OMPNativeAddonSHA256, exitCode, waiter.result(), stdoutBytes, stderrBytes)
	}
	return final, nil
}

func auditInstalledOMPProbeSandboxProfile(executable executionPolicyFileIdentity, root executionPolicyDirectoryIdentity, writableRoot, nativeAddonPath, gatewayAuthority string) string {
	var profile strings.Builder
	profile.WriteString("(version 1)\n(deny default)\n")
	profile.WriteString("(deny process-info* (target others))\n(deny signal (target others))\n")
	profile.WriteString("(allow process-fork)\n(allow process-info* (target self) (target children))\n(allow signal (target self) (target children))\n")
	profile.WriteString("(allow sysctl-read (sysctl-name \"security.mac.lockdown_mode_state\" \"kern.bootargs\" \"kern.osproductversion\" \"kern.iossupportversion\" \"kern.osvariant_status\" \"hw.ephemeral_storage\" \"hw.pagesize_compat\"))\n")
	readRoots := []string{writableRoot, root.Path, "/usr/lib", "/usr/share", "/System/Library", "/Library/Apple", "/private/var/db/timezone"}
	executables := []string{executable.Path}
	profile.WriteString("(allow file-read-metadata file-test-existence (literal \"/\")")
	for _, path := range sandboxPathVariants(append(readRoots, executables...)...) {
		writeSandboxFilter(&profile, "path-ancestors", path)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-read-metadata (literal \"/System/Cryptexes/OS\") (literal \"/System/Volumes/Data\"))\n")
	profile.WriteString("(allow file-read* file-test-existence (literal \"/\") (literal \"/dev/null\") (literal \"/dev/zero\") (literal \"/dev/urandom\")")
	for _, path := range sandboxPathVariants(readRoots...) {
		writeSandboxFilter(&profile, "subpath", path)
	}
	for _, path := range sandboxPathVariants(executables...) {
		writeSandboxFilter(&profile, "literal", path)
	}
	for _, path := range sandboxPathVariants(nativeAddonPath) {
		writeSandboxFilter(&profile, "literal", path)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-read-data (literal \"/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld\") (literal \"/dev/dtracehelper\"))\n")
	profile.WriteString("(allow file-map-executable (subpath \"/usr/lib\") (subpath \"/System/Library\") (subpath \"/Library/Apple\")")
	for _, path := range sandboxPathVariants(executables...) {
		writeSandboxFilter(&profile, "literal", path)
	}
	for _, path := range sandboxPathVariants(nativeAddonPath) {
		writeSandboxFilter(&profile, "literal", path)
	}
	profile.WriteString(")\n(allow file-write* (literal \"/dev/null\")")
	writeAuditSealedTemporaryWriteFilters(&profile, writableRoot)
	profile.WriteString(")\n")
	writeAuditNativeWriteDenials(&profile, nativeAddonPath)
	profile.WriteString("(allow process-exec")
	for _, path := range sandboxPathVariants(executables...) {
		writeSandboxFilter(&profile, "literal", path)
	}
	profile.WriteString(")\n(allow network-outbound (remote tcp \"")
	profile.WriteString(sandboxLiteral(gatewayAuthority))
	profile.WriteString("\"))\n")
	return profile.String()
}
