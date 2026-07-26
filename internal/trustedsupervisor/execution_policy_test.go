package trustedsupervisor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestExecutionPolicyLoadsCanonicalPinnedLaunchEntry(t *testing.T) {
	material := newExecutionPolicyTestMaterial(t)
	policy, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatalf("load execution policy: %v", err)
	}
	resolved, err := policy.Resolve(material.envelope)
	if err != nil {
		t.Fatalf("resolve exact launch policy: %v", err)
	}
	if resolved.PolicyHash != material.entry.PolicyHash || resolved.LaunchSpecHash != material.envelope.LaunchSpecHash ||
		resolved.RepositoryIdentity != material.envelope.RepositoryIdentity || resolved.RouteMappingHash != material.envelope.RouteMappingHash ||
		resolved.ProviderEndpoint != (executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443}) ||
		resolved.AttemptCap != material.envelope.AttemptCap || resolved.PromptTemplateID != readOnlyAuditPromptTemplateID ||
		resolved.PromptTemplateHash != readOnlyAuditPromptTemplateHash() {
		t.Fatalf("resolved execution policy lost exact authority: %+v", resolved)
	}
	if err := policy.ValidateEffectBoundary(resolved); err != nil {
		t.Fatalf("validate pinned execution effect boundary: %v", err)
	}
	contents, err := os.ReadFile(material.policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(policy.canonicalBytes, contents) || policy.contentSHA256 != hashJournalBytes(contents) || policy.size != int64(len(contents)) {
		t.Fatalf("loaded policy did not retain canonical content authority")
	}
}

func TestRunbookMatchesExecutionPolicySchemaAndNamesInstalledPreflightSeparately(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "docs", "local-trusted-supervisor-transport-runbook.md"))
	if err != nil {
		t.Fatal(err)
	}
	runbook := string(contents)
	const schemaPrefix = "The execution-policy document has schema version `"
	schemaStart := strings.Index(runbook, schemaPrefix)
	if schemaStart < 0 {
		t.Fatal("runbook execution-policy schema statement is missing")
	}
	schemaStart += len(schemaPrefix)
	schemaEnd := strings.IndexByte(runbook[schemaStart:], '`')
	if schemaEnd < 0 {
		t.Fatal("runbook execution-policy schema statement is malformed")
	}
	if documented := runbook[schemaStart : schemaStart+schemaEnd]; documented != executionPolicySchemaVersion {
		t.Fatalf("runbook execution-policy schema = %q, want %q", documented, executionPolicySchemaVersion)
	}
	operationalStart := strings.Index(runbook, "## Operational gate")
	if operationalStart < 0 {
		t.Fatal("runbook operational gate is missing")
	}
	operational := runbook[operationalStart:]
	for _, required := range []string{
		"Most executor tests use dynamically created fake route-aware",
		"Separately, `TestAuditInstalledOMPProviderFreeTransportPreflight`",
		"provider-free installed OMP v17.1.3 preflight",
		"fixed fake credential",
		"local deterministic rejection",
		"no real model or provider API canary",
		"No repair execution, source write, or Run creation",
	} {
		if !strings.Contains(operational, required) {
			t.Fatalf("runbook operational gate does not state %q", required)
		}
	}
}

func TestExecutionPolicyRejectsCanonicalAuthorityDrift(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*executionPolicyEntry)
	}{
		{"repository identity hash", func(entry *executionPolicyEntry) { entry.RepositoryIdentityHash = testHash("wrong-repository") }},
		{"prompt template", func(entry *executionPolicyEntry) { entry.PromptTemplateHash = testHash("raw-prompt-substitution") }},
		{"wrapper basename", func(entry *executionPolicyEntry) {
			entry.Wrapper.Path = filepath.Join(filepath.Dir(entry.Wrapper.Path), "omp")
		}},
		{"attempt cap", func(entry *executionPolicyEntry) { entry.AttemptCap = 0 }},
		{"deadline", func(entry *executionPolicyEntry) { entry.InternalDeadlineSeconds = 0 }},
		{"test command hash", func(entry *executionPolicyEntry) { entry.AllowedTests[0].CommandSHA256 = "go test ./..." }},
		{"overlapping roots", func(entry *executionPolicyEntry) { entry.OutputRoot = entry.PromptRoot }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newExecutionPolicyTestMaterial(t)
			testCase.mutate(&material.entry)
			material.entry.PolicyHash = mustSealExecutionPolicyEntryForTest(t, material.entry).PolicyHash
			writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
			if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("load drifted policy error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestExecutionPolicyRejectsDarwinPhysicalAuthorityOverlap(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin physical path alias proof")
	}
	roots := []executionPolicyRootFieldForTest{
		{name: "prompt", set: func(entry *executionPolicyEntry, path string) { entry.PromptRoot = path }},
		{name: "output", set: func(entry *executionPolicyEntry, path string) { entry.OutputRoot = path }},
		{name: "session", set: func(entry *executionPolicyEntry, path string) { entry.SessionRoot = path }},
		{name: "work", set: func(entry *executionPolicyEntry, path string) { entry.WorkRoot = path }},
		{name: "temporary", set: func(entry *executionPolicyEntry, path string) { entry.TemporaryRoot = path }},
	}
	for left := range roots {
		for right := left + 1; right < len(roots); right++ {
			for _, direction := range []string{"alias-parent", "physical-parent"} {
				t.Run("invocation/"+roots[left].name+"/"+roots[right].name+"/"+direction, func(t *testing.T) {
					material := newExecutionPolicyTestMaterial(t)
					aliasParent, physicalParent, aliasChild, physicalChild := darwinPhysicalAliasDirectoriesForTest(t, material.directory)
					if direction == "alias-parent" {
						roots[left].set(&material.entry, aliasParent)
						roots[right].set(&material.entry, physicalChild)
					} else {
						roots[left].set(&material.entry, physicalParent)
						roots[right].set(&material.entry, aliasChild)
					}
					assertExecutionPolicyLoadRejectedForTest(t, material)
				})
			}
		}
	}
	for _, root := range roots {
		for _, direction := range []string{"alias-parent", "physical-parent"} {
			t.Run("repository/"+root.name+"/"+direction, func(t *testing.T) {
				material := newExecutionPolicyTestMaterial(t)
				aliasParent, physicalParent, aliasChild, physicalChild := darwinPhysicalAliasDirectoriesForTest(t, material.directory)
				if direction == "alias-parent" {
					material.entry.Repository = directoryIdentityForTest(t, aliasParent)
					root.set(&material.entry, physicalChild)
				} else {
					material.entry.Repository = directoryIdentityForTest(t, physicalParent)
					root.set(&material.entry, aliasChild)
				}
				assertExecutionPolicyLoadRejectedForTest(t, material)
			})
		}
	}
	for _, authority := range append([]executionPolicyRootFieldForTest{{
		name: "repository",
		set:  func(entry *executionPolicyEntry, path string) { entry.Repository = directoryIdentityForTest(t, path) },
	}}, roots...) {
		for _, direction := range []string{"alias-container", "physical-container"} {
			t.Run("native-addon/"+authority.name+"/"+direction, func(t *testing.T) {
				material := newExecutionPolicyTestMaterial(t)
				aliasParent, physicalParent, _, _ := darwinPhysicalAliasDirectoriesForTest(t, material.directory)
				aliasAddon, physicalAddon := darwinNativeAddonPathsForTest(t, aliasParent)
				if direction == "alias-container" {
					authority.set(&material.entry, aliasParent)
					material.entry.OMPNativeAddon = fileIdentityForTest(t, physicalAddon)
				} else {
					authority.set(&material.entry, physicalParent)
					material.entry.OMPNativeAddon = fileIdentityForTest(t, aliasAddon)
				}
				assertExecutionPolicyLoadRejectedForTest(t, material)
			})
		}
	}
	for _, authority := range append([]executionPolicyRootFieldForTest{{
		name: "repository",
		set:  func(entry *executionPolicyEntry, path string) { entry.Repository = directoryIdentityForTest(t, path) },
	}}, roots...) {
		for _, direction := range []string{"authority-parent", "test-root-parent"} {
			t.Run("allowed-test-root/"+authority.name+"/"+direction, func(t *testing.T) {
				material := newExecutionPolicyTestMaterial(t)
				aliasParent, physicalParent, aliasChild, physicalChild := darwinPhysicalAliasDirectoriesForTest(t, material.directory)
				if direction == "authority-parent" {
					authority.set(&material.entry, aliasParent)
					material.entry.AllowedTests = []executionPolicyTestCommand{executionPolicyTestCommandInRootForTest(t, "nested_test", physicalChild)}
				} else {
					authority.set(&material.entry, physicalChild)
					material.entry.AllowedTests = []executionPolicyTestCommand{executionPolicyTestCommandInRootForTest(t, "parent_test", aliasParent)}
				}
				_ = physicalParent
				_ = aliasChild
				assertExecutionPolicyLoadRejectedForTest(t, material)
			})
		}
	}
	for _, direction := range []string{"alias-test-root", "physical-test-root"} {
		t.Run("allowed-test-root/native-addon/"+direction, func(t *testing.T) {
			material := newExecutionPolicyTestMaterial(t)
			aliasParent, physicalParent, _, _ := darwinPhysicalAliasDirectoriesForTest(t, material.directory)
			aliasAddon, physicalAddon := darwinNativeAddonPathsForTest(t, aliasParent)
			if direction == "alias-test-root" {
				material.entry.AllowedTests = []executionPolicyTestCommand{executionPolicyTestCommandInRootForTest(t, "parent_test", aliasParent)}
				material.entry.OMPNativeAddon = fileIdentityForTest(t, physicalAddon)
			} else {
				material.entry.AllowedTests = []executionPolicyTestCommand{executionPolicyTestCommandInRootForTest(t, "parent_test", physicalParent)}
				material.entry.OMPNativeAddon = fileIdentityForTest(t, aliasAddon)
			}
			assertExecutionPolicyLoadRejectedForTest(t, material)
		})
	}
}

type executionPolicyRootFieldForTest struct {
	name string
	set  func(*executionPolicyEntry, string)
}

func darwinPhysicalAliasDirectoriesForTest(t *testing.T, base string) (string, string, string, string) {
	t.Helper()
	physicalBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(physicalBase, "/private/var/") {
		t.Skipf("temporary directory %q has no /var Darwin alias", base)
	}
	aliasBase := strings.TrimPrefix(physicalBase, "/private")
	aliasParent := filepath.Join(aliasBase, "physical-authority")
	if err := os.Mkdir(aliasParent, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasChild := filepath.Join(aliasParent, "nested")
	if err := os.Mkdir(aliasChild, 0o700); err != nil {
		t.Fatal(err)
	}
	physicalParent := filepath.Join(physicalBase, "physical-authority")
	physicalChild := filepath.Join(physicalParent, "nested")
	if resolved, err := filepath.EvalSymlinks(aliasChild); err != nil || resolved != physicalChild {
		t.Fatalf("Darwin alias resolution = %q, %v; want %q", resolved, err, physicalChild)
	}
	if lexicalPathsOverlapForTest(aliasParent, physicalChild) || lexicalPathsOverlapForTest(physicalParent, aliasChild) {
		t.Fatal("physical overlap fixture also overlaps lexically")
	}
	return aliasParent, physicalParent, aliasChild, physicalChild
}

func lexicalPathsOverlapForTest(left, right string) bool {
	leftToRight, leftErr := filepath.Rel(left, right)
	rightToLeft, rightErr := filepath.Rel(right, left)
	return leftErr == nil && leftToRight != ".." && !strings.HasPrefix(leftToRight, ".."+string(filepath.Separator)) ||
		rightErr == nil && rightToLeft != ".." && !strings.HasPrefix(rightToLeft, ".."+string(filepath.Separator))
}

func darwinNativeAddonPathsForTest(t *testing.T, aliasRoot string) (string, string) {
	t.Helper()
	aliasPath := filepath.Join(aliasRoot, "native-home", ".omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aliasPath, []byte("physical-overlap-native-addon"), 0o700); err != nil {
		t.Fatal(err)
	}
	physicalPath, err := filepath.EvalSymlinks(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	return aliasPath, physicalPath
}

func executionPolicyTestCommandInRootForTest(t *testing.T, id, root string) executionPolicyTestCommand {
	t.Helper()
	path := filepath.Join(root, id)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return executionPolicyTestCommandForTest(t, id, path)
}

func assertExecutionPolicyLoadRejectedForTest(t *testing.T, material executionPolicyTestMaterial) {
	t.Helper()
	material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
	writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
	if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("physically overlapping policy error = %v, want %v", err, ErrAuthentication)
	}
}

func TestExecutionPolicyBindsOnlyExactRouteProviderEndpoints(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		provider string
		endpoint executionPolicyEndpoint
	}{
		{"provider mismatch", "anthropic", executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443}},
		{"IP literal", "anthropic", executionPolicyEndpoint{Hostname: "93.184.216.34", Port: 443}},
		{"localhost", "anthropic", executionPolicyEndpoint{Hostname: "localhost", Port: 443}},
		{"userinfo", "anthropic", executionPolicyEndpoint{Hostname: "user@api.anthropic.com", Port: 443}},
		{"path", "anthropic", executionPolicyEndpoint{Hostname: "api.anthropic.com/v1", Port: 443}},
		{"wildcard", "anthropic", executionPolicyEndpoint{Hostname: "*.anthropic.com", Port: 443}},
		{"arbitrary port", "anthropic", executionPolicyEndpoint{Hostname: "api.anthropic.com", Port: 8443}},
		{"unsupported provider", "custom:attacker", executionPolicyEndpoint{Hostname: "attacker.example", Port: 443}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newExecutionPolicyTestMaterial(t)
			material.entry.HermesProvider = testCase.provider
			material.entry.ProviderEndpoint = testCase.endpoint
			material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
			writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
			if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("invalid endpoint policy error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
	t.Run("custom sudo exact mapping", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		beforeHash := material.entry.PolicyHash
		material.entry.OMPVersion = "17.1.2"
		material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
		if material.entry.PolicyHash == beforeHash {
			t.Fatal("OMP version was not bound into policy hash")
		}
		material.entry.OMPVersion = supportedOMPVersion
		material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
		writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
		policy, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
		if err != nil {
			t.Fatalf("load exact custom:sudo endpoint: %v", err)
		}
		resolved, err := policy.Resolve(material.envelope)
		if err != nil || resolved.ProviderEndpoint != material.entry.ProviderEndpoint || resolved.OMPVersion != supportedOMPVersion ||
			resolved.OMPNativeAddon.SHA256 != material.entry.OMPNativeAddon.SHA256 {
			t.Fatalf("resolve exact custom:sudo OMP route = %+v, %v", resolved, err)
		}
		mutated := resolved
		mutated.ProviderEndpoint.Hostname = "attacker.example"
		if err := policy.ValidateEffectBoundary(mutated); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("mutated selected endpoint error = %v, want %v", err, ErrAuthentication)
		}
	})
}

func TestExecutionPolicyRejectsDuplicateUnknownNoncanonicalModeAndSymlink(t *testing.T) {
	t.Run("duplicate launch hash", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry, material.entry})
		if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("duplicate launch policy error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("unknown raw authority", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		contents, err := os.ReadFile(material.policyPath)
		if err != nil {
			t.Fatal(err)
		}
		contents = append(contents[:len(contents)-1], []byte(`,"raw_prompt":"forbidden"}`)...)
		if err := os.WriteFile(material.policyPath, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("raw authority error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("noncanonical", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		contents, err := os.ReadFile(material.policyPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(material.policyPath, append(contents, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("noncanonical policy error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("mode", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		if err := os.Chmod(material.policyPath, 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("wide policy mode error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		link := filepath.Join(material.directory, "execution-policy-link.json")
		if err := os.Symlink(material.policyPath, link); err != nil {
			t.Fatal(err)
		}
		if _, err := loadExecutionPolicyForTest(link, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("policy symlink error = %v, want %v", err, ErrAuthentication)
		}
	})
}

func TestExecutionPolicyRechecksPolicyWrapperAndRepositoryIdentityAtEveryEffectBoundary(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, executionPolicyTestMaterial)
	}{
		{"policy replacement", func(t *testing.T, material executionPolicyTestMaterial) {
			if err := os.Rename(material.policyPath, material.policyPath+".pinned"); err != nil {
				t.Fatal(err)
			}
			writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
		}},
		{"policy in-place content", func(t *testing.T, material executionPolicyTestMaterial) {
			before, err := os.Lstat(material.policyPath)
			if err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(material.policyPath)
			if err != nil {
				t.Fatal(err)
			}
			at := bytes.Index(contents, []byte(`"task_tier":"normal"`))
			if at < 0 {
				t.Fatal("task tier not found in canonical policy")
			}
			file, err := os.OpenFile(material.policyPath, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := file.WriteAt([]byte("S"), int64(at+len(`"task_tier":"`)))
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				t.Fatalf("rewrite policy in place: %v / %v", writeErr, closeErr)
			}
			after, err := os.Lstat(material.policyPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeStat := before.Sys().(*syscall.Stat_t)
			afterStat := after.Sys().(*syscall.Stat_t)
			if beforeStat.Ino != afterStat.Ino || before.Size() != after.Size() {
				t.Fatal("fixture did not preserve policy inode and size")
			}
		}},
		{"wrapper content", func(t *testing.T, material executionPolicyTestMaterial) {
			if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nexit 9\n"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrapper replacement", func(t *testing.T, material executionPolicyTestMaterial) {
			if err := os.Rename(material.entry.Wrapper.Path, material.entry.Wrapper.Path+".pinned"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"repository replacement", func(t *testing.T, material executionPolicyTestMaterial) {
			if err := os.Rename(material.entry.Repository.Path, material.entry.Repository.Path+".pinned"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(material.entry.Repository.Path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newExecutionPolicyTestMaterial(t)
			policy, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := policy.Resolve(material.envelope)
			if err != nil {
				t.Fatal(err)
			}
			testCase.mutate(t, material)
			if err := policy.ValidateEffectBoundary(resolved); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("effect-boundary drift error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestExecutionPolicyPinsExactOMPVersionAndNativeAddonAtEveryEffect(t *testing.T) {
	t.Run("addon content authority", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		material.entry.OMPNativeAddon.SHA256 = testHash("mutated-native-addon")
		material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
		writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
		if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("pre-seal native addon authority drift error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("addon replacement", func(t *testing.T) {
		material := newExecutionPolicyTestMaterial(t)
		policy, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		path := material.entry.OMPNativeAddon.Path
		if err := os.Rename(path, path+".pinned"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fake-native-addon"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := policy.ValidateEffectBoundary(material.entry); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("native addon replacement error = %v, want %v", err, ErrAuthentication)
		}
	})
}

func TestExecutionPolicyRejectsInvalidOMPVersionAddonAuthorityAndRoute(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *executionPolicyEntry)
	}{
		{"OMP version", func(_ *testing.T, entry *executionPolicyEntry) { entry.OMPVersion = "17.1.2" }},
		{"addon basename", func(_ *testing.T, entry *executionPolicyEntry) {
			entry.OMPNativeAddon.Path = filepath.Join(filepath.Dir(entry.OMPNativeAddon.Path), "attacker.node")
		}},
		{"addon version directory", func(_ *testing.T, entry *executionPolicyEntry) {
			entry.OMPNativeAddon.Path = filepath.Join(filepath.Dir(filepath.Dir(entry.OMPNativeAddon.Path)), "17.1.2", auditOMPNativeAddonFilename)
		}},
		{"addon overlaps repository", func(_ *testing.T, entry *executionPolicyEntry) {
			entry.OMPNativeAddon.Path = filepath.Join(entry.Repository.Path, ".omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
		}},
		{"provider", func(_ *testing.T, entry *executionPolicyEntry) { entry.HermesProvider = "anthropic" }},
		{"model", func(_ *testing.T, entry *executionPolicyEntry) { entry.HermesModel = "gpt-5.6" }},
		{"credential name", func(_ *testing.T, entry *executionPolicyEntry) {
			entry.CredentialEnvironmentNames = []string{"OPENAI_API_KEY"}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newExecutionPolicyTestMaterial(t)
			testCase.mutate(t, &material.entry)
			material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
			writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
			if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("invalid OMP route policy error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestExecutionPolicyRejectsOverbroadSandboxAuthorityAndMutableGit(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *executionPolicyEntry)
	}{
		{"route credential", func(_ *testing.T, entry *executionPolicyEntry) {
			entry.CredentialEnvironmentNames = []string{"OPENAI_API_KEY"}
		}},
		{"broad read root", func(t *testing.T, entry *executionPolicyEntry) {
			entry.RuntimeReadRoots = []executionPolicyDirectoryIdentity{directoryIdentityForTest(t, "/")}
		}},
		{"mutable executable root", func(t *testing.T, entry *executionPolicyEntry) {
			entry.ExecutableRoots = []executionPolicyDirectoryIdentity{directoryIdentityForTest(t, filepath.Dir(entry.Wrapper.Path))}
		}},
		{"arbitrary Git executable", func(t *testing.T, entry *executionPolicyEntry) {
			path := filepath.Join(filepath.Dir(entry.Wrapper.Path), "git")
			if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			entry.GitExecutable = fileIdentityForTest(t, path)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newExecutionPolicyTestMaterial(t)
			testCase.mutate(t, &material.entry)
			material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
			writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
			if _, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid())); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("overbroad policy error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

type executionPolicyTestMaterial struct {
	directory  string
	policyPath string
	envelope   store.ExternalSupervisorEnvelope
	entry      executionPolicyEntry
}

func newExecutionPolicyTestMaterial(t *testing.T) executionPolicyTestMaterial {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	makeRoot := func(name string) string {
		path := filepath.Join(directory, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	repositoryPath := makeRoot("repository")
	wrapperPath := filepath.Join(directory, "omp_with_timeout.sh")
	if err := os.WriteFile(wrapperPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ompRoot := makeRoot("omp-bin")
	ompPath := filepath.Join(ompRoot, "omp")
	if err := os.WriteFile(ompPath, []byte("#!/bin/sh\nexit 0\n"), 0o500); err != nil {
		t.Fatal(err)
	}
	gitPath := "/usr/bin/git"
	envelope := store.ExternalSupervisorEnvelope{
		LaunchSpecHash: testHash("p5-launch-spec"), RepositoryIdentity: "code.example/operator/p5-audit-target",
		RouteMappingHash: testHash("p5-route"), AttemptCap: 3,
	}
	entry := executionPolicyEntry{
		SchemaVersion: executionPolicyEntrySchemaVersion, LaunchSpecHash: envelope.LaunchSpecHash,
		TaskID: "audit_task_p5_001", RepositoryIdentity: envelope.RepositoryIdentity,
		RepositoryIdentityHash: repositoryIdentityHash(envelope.RepositoryIdentity), Repository: directoryIdentityForTest(t, repositoryPath),
		GitExecutable: fileIdentityForTest(t, gitPath), GitCommit: "0123456789abcdef0123456789abcdef01234567",
		GitCommitObjectSHA256: testHash("commit-object"), GitTree: "89abcdef0123456789abcdef0123456789abcdef",
		SourceArchiveSHA256: testHash("archive"), PromptTemplateID: readOnlyAuditPromptTemplateID,
		PromptTemplateHash: readOnlyAuditPromptTemplateHash(), RouteMappingHash: envelope.RouteMappingHash,
		Wrapper: fileIdentityForTest(t, wrapperPath), OMPExecutable: fileIdentityForTest(t, ompPath),
		OMPExecutableRoot: directoryIdentityForTest(t, ompRoot), OMPVersion: supportedOMPVersion,
		OMPNativeAddon:     ompNativeAddonIdentityForTest(t, directory, "pinned-home"),
		WrapperExecutables: fileIdentitiesForTest(t, auditWrapperDependencyPaths()...),
		HermesProvider:     "custom:sudo", HermesModel: "gpt-5.6-sol",
		ProviderEndpoint: executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		TaskTier:         "normal", InternalDeadlineSeconds: 60, WrapperGraceSeconds: 5, AttemptCap: envelope.AttemptCap,
		AllowedTests: []executionPolicyTestCommand{executionPolicyTestCommandForTest(t, "focused_go_test", "/usr/bin/true")},
		PromptRoot:   makeRoot("prompt"), OutputRoot: makeRoot("output"), SessionRoot: makeRoot("session"),
		WorkRoot: makeRoot("work"), TemporaryRoot: makeRoot("tmp"),
		RuntimeReadRoots:           directoryIdentitiesForTest(t, "/bin", "/usr/bin", "/usr/lib", "/usr/share", "/System/Library", "/Library/Apple", "/private/var/db/timezone"),
		ExecutableRoots:            directoryIdentitiesForTest(t, "/bin", "/usr/bin"),
		CredentialEnvironmentNames: []string{"SUDO_API_KEY"},
	}
	entry = mustSealExecutionPolicyEntryForTest(t, entry)
	policyPath := filepath.Join(directory, "execution-policy.json")
	writeExecutionPolicyFileForTest(t, policyPath, []executionPolicyEntry{entry})
	return executionPolicyTestMaterial{directory: directory, policyPath: policyPath, envelope: envelope, entry: entry}
}

func fileIdentityForTest(t *testing.T, path string) executionPolicyFileIdentity {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	information, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("file stat has no syscall identity")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return executionPolicyFileIdentity{
		Path: path, SHA256: hashJournalBytes(contents), Device: statDecimal(uint64(status.Dev)), Inode: statDecimal(status.Ino),
		OwnerUID: status.Uid, Mode: uint32(information.Mode().Perm()), Size: information.Size(),
	}
}

func ompNativeAddonIdentityForTest(t *testing.T, directory, dataRootName string) executionPolicyFileIdentity {
	t.Helper()
	path := filepath.Join(directory, dataRootName, "omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test-pinned-omp-native-addon"), 0o400); err != nil {
		t.Fatal(err)
	}
	return fileIdentityForTest(t, path)
}

func fileIdentitiesForTest(t *testing.T, paths ...string) []executionPolicyFileIdentity {
	t.Helper()
	identities := make([]executionPolicyFileIdentity, 0, len(paths))
	for _, path := range paths {
		identities = append(identities, fileIdentityForTest(t, path))
	}
	return identities
}

func executionPolicyTestCommandForTest(t *testing.T, id, executable string, arguments ...string) executionPolicyTestCommand {
	t.Helper()
	command, err := sealExecutionPolicyTestCommand(executionPolicyTestCommand{
		ID: id, Executable: fileIdentityForTest(t, executable), ExecutableRoot: directoryIdentityForTest(t, filepath.Dir(executable)),
		Arguments: append([]string(nil), arguments...), TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func directoryIdentityForTest(t *testing.T, path string) executionPolicyDirectoryIdentity {
	t.Helper()
	information, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("directory stat has no syscall identity")
	}
	return executionPolicyDirectoryIdentity{Path: path, Device: statDecimal(uint64(status.Dev)), Inode: statDecimal(status.Ino), OwnerUID: status.Uid, Mode: uint32(information.Mode().Perm())}
}

func directoryIdentitiesForTest(t *testing.T, paths ...string) []executionPolicyDirectoryIdentity {
	t.Helper()
	identities := make([]executionPolicyDirectoryIdentity, 0, len(paths))
	for _, path := range paths {
		identities = append(identities, directoryIdentityForTest(t, path))
	}
	return identities
}

func executionPolicyAtomicRuntimeAuthorityForTest(t *testing.T, entry executionPolicyEntry) executionPolicyOMPRuntimeAuthority {
	t.Helper()
	wrapper, err := os.ReadFile(entry.Wrapper.Path)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := auditOMPBootstrap(entry.OMPExecutable.Path)
	if err != nil {
		t.Fatal(err)
	}
	nativeDataRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(entry.OMPNativeAddon.Path))))
	authority := executionPolicyOMPRuntimeAuthority{
		SchemaVersion:             atomicOMPRuntimeAuthoritySchemaVersion,
		AuthorityPolicyVersion:    atomicOMPRuntimeAuthorityPolicyVersion,
		TrustedOwnerUID:           0,
		ExecutableAncestors:       atomicRuntimeAncestorIdentitiesForTest(t, entry.OMPExecutable.Path),
		NativeAddonAncestors:      atomicRuntimeAncestorIdentitiesForTest(t, entry.OMPNativeAddon.Path),
		NativeDataRoot:            nativeDataRoot,
		DeniedNativeFallbackRoots: []string{filepath.Join(filepath.Dir(nativeDataRoot), "denied-home", ".omp"), filepath.Join(entry.OMPExecutableRoot.Path, "natives")},
		BootstrapSHA256:           hashJournalBytes(bootstrap),
		FramedWrapperStreamSHA256: auditFramedOMPWrapperStreamSHA256(bootstrap, wrapper),
		ArtifactFDPolicy:          atomicOMPRuntimeArtifactFDPolicyParentRetainedCLOEXEC,
	}
	authority, err = sealExecutionPolicyOMPRuntimeAuthority(authority, entry.OMPExecutable, entry.OMPNativeAddon)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func atomicRuntimeAncestorIdentitiesForTest(t *testing.T, artifact string) []executionPolicyDirectoryIdentity {
	t.Helper()
	paths, ok := atomicRuntimeAncestorPaths(artifact)
	if !ok {
		t.Fatalf("invalid atomic runtime artifact path %q", artifact)
	}
	identities := make([]executionPolicyDirectoryIdentity, 0, len(paths))
	for _, path := range paths {
		identities = append(identities, directoryIdentityForTest(t, path))
	}
	return identities
}

func mustSealExecutionPolicyEntryForTest(t *testing.T, entry executionPolicyEntry) executionPolicyEntry {
	t.Helper()
	if entry.OMPRuntimeAuthority.SchemaVersion == "" || canRebindExecutionPolicyAtomicRuntimeAuthorityForTest(entry) {
		entry.OMPRuntimeAuthority = executionPolicyAtomicRuntimeAuthorityForTest(t, entry)
	}
	sealed, err := sealExecutionPolicyEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func canRebindExecutionPolicyAtomicRuntimeAuthorityForTest(entry executionPolicyEntry) bool {
	authority := entry.OMPRuntimeAuthority
	if _, err := os.Lstat(entry.Wrapper.Path); err != nil || !validAtomicNativeLayout(entry, authority) ||
		len(authority.ExecutableAncestors) == 0 || entry.OMPExecutableRoot != authority.ExecutableAncestors[len(authority.ExecutableAncestors)-1] {
		return false
	}
	if _, err := os.Lstat(entry.OMPExecutable.Path); err != nil {
		return false
	}
	if _, err := os.Lstat(entry.OMPNativeAddon.Path); err != nil {
		return false
	}
	return true
}

func writeExecutionPolicyFileForTest(t *testing.T, path string, entries []executionPolicyEntry) {
	t.Helper()
	contents, err := marshalCanonical(executionPolicyFile{SchemaVersion: executionPolicySchemaVersion, Executions: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionPolicyResolveRejectsEnvelopeBindingDrift(t *testing.T) {
	material := newExecutionPolicyTestMaterial(t)
	policy, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*store.ExternalSupervisorEnvelope)
	}{
		{"launch", func(envelope *store.ExternalSupervisorEnvelope) { envelope.LaunchSpecHash = testHash("unknown-launch") }},
		{"repository", func(envelope *store.ExternalSupervisorEnvelope) {
			envelope.RepositoryIdentity = "code.example/operator/other"
		}},
		{"route", func(envelope *store.ExternalSupervisorEnvelope) { envelope.RouteMappingHash = testHash("other-route") }},
		{"attempt cap", func(envelope *store.ExternalSupervisorEnvelope) { envelope.AttemptCap++ }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			drifted := material.envelope
			testCase.mutate(&drifted)
			if _, err := policy.Resolve(drifted); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("resolve drifted envelope error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestProductionServerRequiresAndRechecksExecutionPolicyBeforeJournal(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	t.Run("required", func(t *testing.T) {
		material := newServerTestMaterial(t, now)
		config := serverConfigForTest(material, now)
		config.ExecutionPolicyPath = ""
		if _, err := newServerForTest(config); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("missing execution policy error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("no matching launch", func(t *testing.T) {
		material := newServerTestMaterial(t, now)
		loaded, err := loadExecutionPolicyForTest(material.executionPolicyPath, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		var entry executionPolicyEntry
		for _, candidate := range loaded.entries {
			entry = candidate
		}
		entry.LaunchSpecHash = testHash("unrelated-launch")
		entry = mustSealExecutionPolicyEntryForTest(t, entry)
		writeExecutionPolicyFileForTest(t, material.executionPolicyPath, []executionPolicyEntry{entry})
		running := startInProcessProductionServer(t, material, now)
		defer running.stop(t)
		assertServerRejectsRequestWithoutJournal(t, material, validServerDeliveryRequest(t, material.fixture, now), 0)
	})
	t.Run("replacement", func(t *testing.T) {
		material := newServerTestMaterial(t, now)
		running := startInProcessProductionServer(t, material, now)
		defer running.stop(t)
		if err := os.Rename(material.executionPolicyPath, material.executionPolicyPath+".pinned"); err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(material.executionPolicyPath + ".pinned")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(material.executionPolicyPath, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		assertServerRejectsRequestWithoutJournal(t, material, validServerDeliveryRequest(t, material.fixture, now), 0)
	})
}

func writeServerExecutionPolicyForTest(t *testing.T, directory string, envelope store.ExternalSupervisorEnvelope) string {
	t.Helper()
	makeRoot := func(name string) string {
		path := filepath.Join(directory, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	repositoryPath := makeRoot("audit-repository")
	wrapperPath := filepath.Join(directory, "omp_with_timeout.sh")
	if err := os.WriteFile(wrapperPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ompRoot := makeRoot("audit-omp-bin")
	ompPath := filepath.Join(ompRoot, "omp")
	if err := os.WriteFile(ompPath, []byte("#!/bin/sh\nexit 0\n"), 0o500); err != nil {
		t.Fatal(err)
	}
	entry := executionPolicyEntry{
		SchemaVersion: executionPolicyEntrySchemaVersion, LaunchSpecHash: envelope.LaunchSpecHash,
		TaskID: "audit_task_server_001", RepositoryIdentity: envelope.RepositoryIdentity,
		RepositoryIdentityHash: repositoryIdentityHash(envelope.RepositoryIdentity), Repository: directoryIdentityForTest(t, repositoryPath),
		GitExecutable: fileIdentityForTest(t, "/usr/bin/git"), GitCommit: "0123456789abcdef0123456789abcdef01234567",
		GitCommitObjectSHA256: testHash("server-commit-object"), GitTree: "89abcdef0123456789abcdef0123456789abcdef",
		SourceArchiveSHA256: testHash("server-archive"), PromptTemplateID: readOnlyAuditPromptTemplateID,
		PromptTemplateHash: readOnlyAuditPromptTemplateHash(), RouteMappingHash: envelope.RouteMappingHash,
		Wrapper: fileIdentityForTest(t, wrapperPath), OMPExecutable: fileIdentityForTest(t, ompPath),
		OMPExecutableRoot: directoryIdentityForTest(t, ompRoot), OMPVersion: supportedOMPVersion,
		OMPNativeAddon:     ompNativeAddonIdentityForTest(t, directory, "pinned-server-home"),
		WrapperExecutables: fileIdentitiesForTest(t, auditWrapperDependencyPaths()...),
		HermesProvider:     "custom:sudo", HermesModel: "gpt-5.6-sol",
		ProviderEndpoint: executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		TaskTier:         "normal", InternalDeadlineSeconds: 60, WrapperGraceSeconds: 5, AttemptCap: envelope.AttemptCap,
		AllowedTests: []executionPolicyTestCommand{executionPolicyTestCommandForTest(t, "focused_go_test", "/usr/bin/true")},
		PromptRoot:   makeRoot("audit-prompt"), OutputRoot: makeRoot("audit-output"), SessionRoot: makeRoot("audit-session"),
		WorkRoot: makeRoot("audit-work"), TemporaryRoot: makeRoot("audit-tmp"),
		RuntimeReadRoots:           directoryIdentitiesForTest(t, "/bin", "/usr/bin", "/usr/lib", "/usr/share", "/System/Library", "/Library/Apple", "/private/var/db/timezone"),
		ExecutableRoots:            directoryIdentitiesForTest(t, "/bin", "/usr/bin"),
		CredentialEnvironmentNames: []string{"SUDO_API_KEY"},
	}
	entry = mustSealExecutionPolicyEntryForTest(t, entry)
	path := filepath.Join(directory, "execution-policy.json")
	writeExecutionPolicyFileForTest(t, path, []executionPolicyEntry{entry})
	return path
}
