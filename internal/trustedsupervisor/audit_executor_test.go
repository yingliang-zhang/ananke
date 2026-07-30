package trustedsupervisor

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestDarwinDirectOMPSandboxEnforcesReadonlySnapshotAndRootIsolation(t *testing.T) {

	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	originalPath := filepath.Join(material.entry.Repository.Path, "audit.txt")
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "sandbox_isolation", OriginalPath: originalPath})

	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_sandbox_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_sandbox_001", "audit_run_sandbox_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_CODING_KEY", "credential-must-not-leak")
	var sandboxProfile string
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		BrokerReady: func(_ string, profile string) { sandboxProfile = profile },
	})
	if err != nil || result.ExitCode != 0 || result.TimedOut {
		t.Fatalf("sandboxed fake direct OMP result = %+v, %v\n%s", result, err, sandboxProfile)

	}
	assertAuditNativeWriteDenyVariants(t, sandboxProfile, result.boundInvocation.NativeAddonPath)
	if output, err := os.ReadFile(invocation.OutputPath); err != nil || string(output) != "audit-success" {
		t.Fatalf("direct OMP output = %q, %v", output, err)

	}
	if source, err := os.ReadFile(filepath.Join(snapshot.SourceRoot, "audit.txt")); err != nil || string(source) != "immutable audit source\n" {
		t.Fatalf("snapshot mutated = %q, %v", source, err)
	}
	if original, err := os.ReadFile(originalPath); err != nil || string(original) != "immutable audit source\n" {
		t.Fatalf("original repository mutated = %q, %v", original, err)
	}
	for _, path := range []string{filepath.Join(invocation.TemporaryDir, "temporary"), filepath.Join(invocation.SessionDir, "session.uuid")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("allowed isolated root write %s: %v", path, err)
		}
	}
	if _, err := os.Stat(invocation.PromptPath + ".touch"); !os.IsNotExist(err) {
		t.Fatalf("direct OMP wrote supervisor-owned prompt root: %v", err)
	}

	if _, err := os.Stat(filepath.Join(invocation.TemporaryDir, "source-link")); err != nil {
		t.Fatalf("sandbox did not permit isolated symlink creation: %v", err)
	}
}

func TestDarwinDirectOMPSandboxSealsHomeStateAndAllowsOnlyRunWrites(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "sealed_home"})
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_sealed_home_001")
	if err := os.WriteFile(filepath.Join(filepath.Dir(snapshot.RunRoot), "ancestor-secret"), []byte("must remain unreadable"), 0o600); err != nil {
		t.Fatal(err)
	}
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_sealed_home_001", "audit_run_sealed_home_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	var profile string
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		BrokerReady: func(_ string, sandbox string) { profile = sandbox },
	})
	if err != nil || result.ExitCode != 0 || result.Stdout != "sealed-home-success" {
		t.Fatalf("sealed HOME result = %+v, %v\n%s", result, err, profile)
	}
	bound := result.boundInvocation
	restoreAuditSealedHomeModeForTest(t, bound.HomeDir)
	for _, expected := range []struct {
		path string
		mode os.FileMode
	}{
		{path: bound.HomeDir, mode: 0o700},
		{path: bound.HomeStateDir, mode: 0o500},
		{path: bound.HomeRunDir, mode: 0o700},
	} {
		information, statErr := os.Lstat(expected.path)
		if statErr != nil || !information.IsDir() || information.Mode().Perm() != expected.mode {
			t.Fatalf("sealed HOME path mode = %v, %v; want %04o", information, statErr, expected.mode)
		}
	}
	if contents, readErr := os.ReadFile(filepath.Join(bound.HomeRunDir, "runtime-state")); readErr != nil || string(contents) != "run-state" {
		t.Fatalf("required HOME run state = %q, %v", contents, readErr)
	}
	for _, forbidden := range []string{filepath.Join(bound.HomeStateDir, "logs"), filepath.Join(bound.HomeStateDir, "sibling"), bound.HomeStateDir + ".moved"} {
		if _, statErr := os.Lstat(forbidden); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("forbidden HOME sibling exists: %v", statErr)
		}
	}
}

func TestDarwinDirectOMPSandboxExecutesPinnedGitWithoutParentRepositoryDiscovery(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox Git startup contract")
	}
	physicalTemporaryRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", physicalTemporaryRoot)
	material := newGitArchivePolicyMaterial(t)
	if output, err := exec.Command(material.entry.GitExecutable.Path, "init", "--quiet", material.entry.WorkRoot).CombinedOutput(); err != nil {
		t.Fatalf("initialize snapshot-parent repository: %v\n%s", err, output)
	}
	parentHeadPath := filepath.Join(material.entry.WorkRoot, ".git", "HEAD")
	parentHeadBefore, err := os.ReadFile(parentHeadPath)
	if err != nil {
		t.Fatal(err)
	}
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "git_startup_boundary"})
	for _, path := range []string{
		"/Applications/Xcode-beta.app/Contents/Developer/usr/bin/git",
		"/Library/Developer/CommandLineTools/usr/bin/make",
	} {
		information, statErr := os.Stat(path)
		if statErr != nil || !information.Mode().IsRegular() || information.Mode().Perm()&0o111 == 0 {
			t.Fatalf("blocked executable probe fixture %q unavailable: %v", path, statErr)
		}
	}
	ambientPath := filepath.Join(material.directory, "ambient-path")
	if err := os.Mkdir(ambientPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ambientPath, "git"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", ambientPath)

	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_git_startup_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_git_startup_001", "audit_run_git_startup_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_CODING_KEY", "credential-must-not-leak")
	var sandboxProfile string
	t.Setenv("GIT_CEILING_DIRECTORIES", material.entry.WorkRoot)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(material.entry.WorkRoot, "ambient-global-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "ananke.boundary")
	t.Setenv("GIT_CONFIG_VALUE_0", "ambient-command-config")
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		BrokerReady: func(_ string, profile string) { sandboxProfile = profile },
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("pinned Git startup boundary result = %+v, %v\n%s", result, err, sandboxProfile)
	}
	output, err := os.ReadFile(invocation.OutputPath)
	if err != nil || string(output) != "git-startup-isolated" {
		t.Fatalf("pinned Git startup output = %q, %v", output, err)
	}
	if source, err := os.ReadFile(filepath.Join(snapshot.SourceRoot, "audit.txt")); err != nil || string(source) != "immutable audit source\n" {
		t.Fatalf("Git startup boundary mutated snapshot source = %q, %v", source, err)
	}
	if parentHeadAfter, err := os.ReadFile(parentHeadPath); err != nil || string(parentHeadAfter) != string(parentHeadBefore) {
		t.Fatalf("Git startup boundary mutated parent repository HEAD = %q, %v", parentHeadAfter, err)
	}
}

func assertAuditNativeWriteDenyVariants(t *testing.T, profile, nativeAddonPath string) {
	t.Helper()
	versionVariants := sandboxPathVariants(filepath.Dir(nativeAddonPath))
	addonVariants := sandboxPathVariants(nativeAddonPath)
	if len(versionVariants) != 2 || len(addonVariants) != 2 {
		t.Fatalf("native Darwin aliases = version %q addon %q, want /var and /private/var variants", versionVariants, addonVariants)
	}
	for _, variants := range [][]string{versionVariants, addonVariants} {
		var sawVar, sawPrivateVar bool
		for _, path := range variants {
			sawVar = sawVar || strings.HasPrefix(path, "/var/")
			sawPrivateVar = sawPrivateVar || strings.HasPrefix(path, "/private/var/")
		}
		if !sawVar || !sawPrivateVar {
			t.Fatalf("native Darwin aliases = %q, want /var and /private/var", variants)
		}
	}
	for _, path := range versionVariants {
		rule := `(deny file-write* (subpath "` + sandboxLiteral(path) + `"))`
		if strings.Count(profile, rule) != 1 {
			t.Fatalf("sandbox profile native version deny count for %q = %d, want 1:\n%s", rule, strings.Count(profile, rule), profile)
		}
	}
	for _, path := range addonVariants {
		rule := `(deny file-write* (literal "` + sandboxLiteral(path) + `"))`
		if strings.Count(profile, rule) != 1 {
			t.Fatalf("sandbox profile native addon deny count for %q = %d, want 1:\n%s", rule, strings.Count(profile, rule), profile)
		}
	}
}

func TestAuditInvocationUsesExactArgvMinimalEnvironmentAndFourRoots(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "environment"})

	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_argv_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_argv_001", "audit_run_argv_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{invocation.PromptDir, invocation.OutputDir, invocation.SessionDir, snapshot.RunRoot}
	for left := range roots {
		for right := left + 1; right < len(roots); right++ {
			if pathsOverlap(roots[left], roots[right]) {
				t.Fatalf("invocation roots overlap: %q and %q", roots[left], roots[right])
			}
		}
	}
	t.Setenv("SUDO_CODING_KEY", "credential-must-not-leak")
	t.Setenv("UNRELATED_SECRET", "must-not-inherit")
	t.Setenv("OPENAI_API_KEY", "unselected-route-credential")
	var sandboxProfile string
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		BrokerReady: func(_ string, profile string) {
			sandboxProfile = profile
		},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("exact invocation result = %+v, %v", result, err)
	}
	namesBytes, err := os.ReadFile(filepath.Join(invocation.SessionDir, "env.names"))
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Fields(string(namesBytes))
	sort.Strings(names)
	want := []string{"GIT_CEILING_DIRECTORIES", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "HOME", "LANG", "LC_ALL", "OMP_SESSION_ROOT", "PATH", "PI_CODING_AGENT_DIR", "SUDO_API_KEY", "TMPDIR", "TZ", "XDG_DATA_HOME"}

	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("direct OMP environment names = %q, want %q", names, want)

	}
	xdgDataHome, err := os.ReadFile(filepath.Join(invocation.SessionDir, "xdg-data-home"))
	if err != nil || string(xdgDataHome) != material.entry.OMPRuntimeAuthority.NativeDataRoot {
		t.Fatalf("XDG_DATA_HOME = %q, %v; want %q", xdgDataHome, err, material.entry.OMPRuntimeAuthority.NativeDataRoot)
	}
	commandInvocation := invocation
	commandInvocation.SandboxProfile = sandboxProfile
	commandInvocation.SandboxProfileHash = hashJournalBytes([]byte(sandboxProfile))
	wantCommandArguments := append([]string{"-p", sandboxProfile, material.entry.OMPExecutable.Path}, invocation.Arguments...)
	if got := auditSandboxCommandArguments(commandInvocation); !reflect.DeepEqual(got, wantCommandArguments) {
		t.Fatalf("sandbox command arguments = %q, want %q", got, wantCommandArguments)
	}
	for _, private := range []string{"credential-must-not-leak", material.entry.Wrapper.Path, material.entry.Repository.Path, invocation.PromptPath} {
		if strings.Contains(result.Stdout, private) || strings.Contains(result.Stderr, private) {
			t.Fatalf("wrapper capture leaked private value %q", private)
		}
	}
}

func TestAuditInvocationRejectsWrapperMutationBeforeFreeze(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "report", Output: "frozen-original"})

	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_prefreeze_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_prefreeze_001", "audit_run_prefreeze_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nprintf replacement > \"$3\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{})
	if !errors.Is(err, ErrAuthentication) || result.PID != 0 {
		t.Fatalf("pre-freeze wrapper mutation result = %+v, %v; want closed rejection before start", result, err)
	}
}

func TestAuditInvocationFrozenWrapperBoundaryStillRejectsModelsMutation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "report", Output: "frozen-original"})

	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_frozen_models_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_frozen_models_001", "audit_run_frozen_models_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{TransportReady: func(bound auditInvocation) error {
		if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nprintf replacement > \"$3\"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bound.ModelsPath, []byte("providers: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return nil
	}})
	if !errors.Is(err, ErrAuthentication) || result.PID != 0 {
		t.Fatalf("post-freeze models mutation result = %+v, %v; want closed rejection before start", result, err)
	}
}

func TestAuditInvocationHardTimeoutConfirmsTERMExitBeforeTimedOut(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin process identity contract")
	}
	material := newGitArchivePolicyMaterial(t)
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "hang"})

	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_hard_timeout_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_hard_timeout_001", "audit_run_hard_timeout_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	hardTimeout := make(chan time.Time, 1)
	hardTimeout <- time.Now()
	operations := &countingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		HardTimeout: hardTimeout, ProcessOperations: operations, TerminationBounds: testAuditTerminationBounds(),
	})
	if !errors.Is(err, ErrAuthentication) || !result.TimedOut || result.Cancelled || !result.ProcessGroupGone || result.TimeoutEvidence.SessionUUID != "" {
		t.Fatalf("hard timeout result = %+v, %v; want closed rejection without wrapper-authored evidence", result, err)
	}
	if got := operations.signalCount(); got != 1 {
		t.Fatalf("hard timeout signals = %d, want TERM only", got)
	}
	if _, err := inspectAuditProcess(result.PID); err == nil {
		t.Fatalf("hard-timeout PID %d remains alive", result.PID)
	}
}

func TestDarwinAuditSandboxDeniesPrivateAuthorityParentAndLocalEndpoints(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	protected := make([]string, 0, 7)
	for _, name := range []string{"signing-key.json", "trust-policy.json", "repository-policy.json", "journal.sqlite", "journal.sqlite-wal", "journal.sqlite-shm", "unrelated-home-secret"} {
		path := filepath.Join(material.directory, name)
		if err := os.WriteFile(path, []byte("private-authority"), 0o600); err != nil {
			t.Fatal(err)
		}
		protected = append(protected, path)
	}
	socketDirectory, err := os.MkdirTemp("/tmp", "ananke-sb-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDirectory) })
	unixPath := filepath.Join(socketDirectory, "u.sock")
	unixListener, err := net.Listen("unix", unixPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unixListener.Close()
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpListener.Close()
	material.policy.setProtectedPaths(append(protected, material.policyPath, unixPath)...)
	originalPath := filepath.Join(material.entry.Repository.Path, "audit.txt")
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{
		Scenario:       "least_authority",
		Output:         "fake-api-and-write-root-success",
		OriginalPath:   originalPath,
		ProtectedPaths: append(append([]string(nil), protected...), material.policyPath),
		UnixSocketPath: unixPath,
		TCPAddress:     tcpListener.Addr().String(),
	})

	material.policy.setProtectedPaths(append(protected, material.policyPath, unixPath)...)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_least_authority_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_least_authority_001", "audit_run_least_authority_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_CODING_KEY", "selected-route-credential")
	t.Setenv("OPENAI_API_KEY", "unselected-route-credential")
	var brokerAddress, sandboxProfile string
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		BrokerReady: func(address, profile string) {
			brokerAddress, sandboxProfile = address, profile
			_, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				t.Fatal(splitErr)
			}
			for network, authority := range map[string]string{
				"tcp4": net.JoinHostPort("127.0.0.1", port),
				"tcp6": net.JoinHostPort("::1", port),
			} {
				listener, listenErr := net.Listen(network, authority)
				if listenErr == nil {
					_ = listener.Close()
					t.Fatalf("trusted gateway did not own %s authority %q", network, authority)
				}
				if !errors.Is(listenErr, syscall.EADDRINUSE) {
					t.Fatalf("trusted gateway %s ownership error = %v, want address in use", network, listenErr)
				}
			}
		},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("least-authority fake direct OMP result = %+v, %v", result, err)

	}
	contents, err := os.ReadFile(invocation.OutputPath)
	if err != nil || string(contents) != "fake-api-and-write-root-success" {
		t.Fatalf("least-authority output = %q, %v", contents, err)
	}
	_, brokerPort, err := net.SplitHostPort(brokerAddress)
	if err != nil {
		t.Fatal(err)
	}
	wantNetworkRule := "(allow network-outbound (remote tcp \"localhost:" + brokerPort + "\"))"
	if !strings.Contains(sandboxProfile, wantNetworkRule) {
		t.Fatalf("sandbox profile omitted exact dual-stack broker rule %q:\n%s", wantNetworkRule, sandboxProfile)
	}
	nativeAddonPath := result.boundInvocation.NativeAddonPath
	assertAuditNativeWriteDenyVariants(t, sandboxProfile, nativeAddonPath)
	for _, path := range sandboxPathVariants(nativeAddonPath) {
		rule := `(path-ancestors "` + path + `")`
		if strings.Count(sandboxProfile, rule) != 1 {
			t.Fatalf("sandbox profile native metadata rule count for %q = %d, want 1:\n%s", rule, strings.Count(sandboxProfile, rule), sandboxProfile)
		}
	}
	for _, identity := range append(append([]executionPolicyDirectoryIdentity(nil), material.entry.OMPRuntimeAuthority.ExecutableAncestors...), material.entry.OMPRuntimeAuthority.NativeAddonAncestors...) {
		for _, path := range sandboxPathVariants(identity.Path) {
			if path == "/" {
				continue
			}
			rule := `(literal "` + path + `")`
			if !strings.Contains(sandboxProfile, rule) {
				t.Fatalf("sandbox profile omitted exact runtime ancestor read-data rule %q:\n%s", rule, sandboxProfile)
			}
		}
	}
	wantRuntimeRules := []string{
		`(allow file-read-data (literal "/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld") (literal "/dev/dtracehelper"))`,
		`(allow file-read-metadata (literal "/System/Cryptexes/OS") (literal "/System/Volumes/Data"))`,
		`(allow sysctl-read (sysctl-name "security.mac.lockdown_mode_state" "kern.bootargs" "kern.osproductversion" "kern.iossupportversion" "kern.osvariant_status" "hw.ephemeral_storage" "hw.pagesize_compat"))`,
	}
	for _, rule := range wantRuntimeRules {
		if strings.Count(sandboxProfile, rule) != 1 {
			t.Fatalf("sandbox profile exact runtime rule count for %q = %d, want 1:\n%s", rule, strings.Count(sandboxProfile, rule), sandboxProfile)
		}
	}
	if strings.Count(sandboxProfile, "(allow network-outbound") != 1 {
		t.Fatalf("sandbox profile network-outbound allow count = %d, want 1:\n%s", strings.Count(sandboxProfile, "(allow network-outbound"), sandboxProfile)
	}
	if strings.Contains(sandboxProfile, "/private/var/run/syslog") {
		t.Fatalf("sandbox profile allowed syslog endpoint:\n%s", sandboxProfile)
	}
	for _, forbidden := range []string{"(allow process*)", "(allow mach-lookup)", "(allow network-outbound)\n", "(allow file-read*)\n", "mDNSResponder", "*:443", "*:53", "remote udp"} {
		if strings.Contains(sandboxProfile, forbidden) {
			t.Fatalf("sandbox profile retained broad authority %q:\n%s", forbidden, sandboxProfile)
		}
	}
}

func TestDarwinAuditSandboxOwnsLocalhostGatewayOnIPv4AndIPv6(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin dual-stack localhost sandbox authority proof")
	}
	material := newGitArchivePolicyMaterial(t)
	const fakeCredential = "sandbox-dual-stack-fake-credential"
	setNativeFakeAuditOMPForTest(t, &material, fakeAuditOMPFixture{Scenario: "dual_stack", Output: "dual-stack-gateway"})

	var gatewayDials atomic.Int32
	dependencies := material.policy.testBrokerDependencies
	dependencies.DialContext = func(context.Context, string, string) (net.Conn, error) {
		gatewayDials.Add(1)
		return nil, errors.New("trusted gateway upstream intentionally unavailable")
	}
	material.policy.testBrokerDependencies = dependencies
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_dual_stack_gateway_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_dual_stack_gateway_001", "audit_run_dual_stack_gateway_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_CODING_KEY", fakeCredential)
	var sandboxProfile string
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		BrokerReady: func(address, profile string) {
			_, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				t.Fatal(splitErr)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestAuditGatewayIPv6BindProbeProcess$")
			command.Env = []string{auditGatewayIPv6BindProbeEnvironment + "=" + net.JoinHostPort("::1", port)}
			if output, probeErr := command.CombinedOutput(); probeErr != nil {
				t.Fatalf("external IPv6 bind probe unexpectedly acquired gateway authority: %v\n%s", probeErr, output)
			} else if strings.Contains(string(output), fakeCredential) {
				t.Fatalf("external bind probe observed fake credential: %q", output)
			}
			sandboxProfile = profile
		},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("dual-stack sandbox probe = %+v, %v\n%s", result, err, sandboxProfile)
	}
	if gatewayDials.Load() != 2 {
		t.Fatalf("trusted gateway validated %d sandbox requests, want one IPv4 and one IPv6 request", gatewayDials.Load())
	}
	contents, readErr := os.ReadFile(invocation.OutputPath)
	if readErr != nil || string(contents) != "dual-stack-gateway" {
		t.Fatalf("dual-stack sandbox output = %q, %v", contents, readErr)
	}
	_, port, splitErr := net.SplitHostPort(resultBrokerAddressForProfileTest(t, sandboxProfile))
	if splitErr != nil {
		t.Fatal(splitErr)
	}
	wantRule := `(allow network-outbound (remote tcp "localhost:` + port + `"))`
	if !strings.Contains(sandboxProfile, wantRule) || strings.Contains(sandboxProfile, `(remote tcp "127.0.0.1:`) || strings.Contains(sandboxProfile, `(remote tcp "[::1]:`) {
		t.Fatalf("sandbox profile did not use the exclusively owned localhost authority %q:\n%s", wantRule, sandboxProfile)
	}
}

const auditGatewayIPv6BindProbeEnvironment = "ANANKE_TEST_AUDIT_GATEWAY_IPV6_BIND_AUTHORITY"

type auditSessionJSONLTestSpec struct {
	UUID       string
	CWD        string
	Prompt     string
	PathRefs   []string
	Additional []any
}

func TestAuditFreshSessionArtifactAllowsAuthenticatedInvocationOwnedPaths(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_fresh_session_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_fresh_session_001", "audit_run_fresh_session_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	spec := authenticatedFreshAuditSessionSpecForTest(t, invocation)
	writeAuditSessionJSONLForTest(t, filepath.Join(invocation.SessionDir, "fresh.jsonl"), spec)
	if err := scanAuditInvocationWritableTrees(material.policy, material.entry, invocation); err != nil {
		t.Fatalf("authenticated fresh session artifact rejected: %v", err)
	}
}

func TestAuditArtifactScanFailuresAreClosedAndTyped(t *testing.T) {
	type scanCase struct {
		name      string
		wantClass string
		wantCause error
		mutate    func(*testing.T, *gitArchivePolicyMaterial, auditInvocation)
		scan      func(*executionPolicy, executionPolicyEntry, auditInvocation) error
	}
	cases := []scanCase{
		{
			name: "walk", wantClass: "artifact_scan_prompt_walk", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				if err := os.RemoveAll(invocation.PromptDir); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink", wantClass: "artifact_scan_session_symlink", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(invocation.SessionDir, "session-link")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "special", wantClass: "artifact_scan_temporary_special", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				if err := syscall.Mkfifo(filepath.Join(invocation.TemporaryDir, "special"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "limit", wantClass: "artifact_scan_output_limit", wantCause: ErrLimit,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				for index := range maxAuditTreeScanFiles {
					if err := os.WriteFile(filepath.Join(invocation.OutputDir, "artifact-"+strconv.Itoa(index)), nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
		{
			name: "read", wantClass: "artifact_scan_output_read", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				path := filepath.Join(invocation.OutputDir, "unreadable")
				if err := os.WriteFile(path, []byte("bounded"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o200); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "timeout secret", wantClass: "artifact_scan_session_timeout_secret", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				t.Setenv("SUDO_API_KEY", "timeout-secret-must-not-leak")
				if err := os.WriteFile(filepath.Join(invocation.SessionDir, "timeout.jsonl"), []byte("timeout-secret-must-not-leak"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			scan: func(policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation) error {
				sessionPath := filepath.Join(invocation.SessionDir, "timeout.jsonl")
				return scanAuditInvocationWritableTreesExcept(policy, entry, invocation, invocation.OutputPath, auditTimeoutEvidence{SessionPath: sessionPath})
			},
		},
		{
			name: "fresh authentication", wantClass: "artifact_scan_session_fresh_authentication", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				if err := os.WriteFile(filepath.Join(invocation.SessionDir, "fresh.jsonl"), []byte("{malformed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "fresh authority", wantClass: "artifact_scan_session_fresh_authority", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, _ *gitArchivePolicyMaterial, invocation auditInvocation) {
				t.Setenv("SUDO_API_KEY", "fresh-secret-must-not-leak")
				spec := authenticatedFreshAuditSessionSpecForTest(t, invocation)
				spec.Additional = append(spec.Additional, map[string]any{"credential": "fresh-secret-must-not-leak"})
				writeAuditSessionJSONLForTest(t, filepath.Join(invocation.SessionDir, "fresh.jsonl"), spec)
			},
		},
		{
			name: "authority", wantClass: "artifact_scan_temporary_authority_temporary_root_repository", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, material *gitArchivePolicyMaterial, invocation auditInvocation) {
				if err := os.WriteFile(filepath.Join(invocation.TemporaryDir, "authority"), []byte(material.entry.Repository.Path), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "protected", wantClass: "artifact_scan_output_protected", wantCause: ErrAuthentication,
			mutate: func(t *testing.T, material *gitArchivePolicyMaterial, invocation auditInvocation) {
				const protected = "/fixture/closed-protected-path"
				material.policy.protectedPaths = []string{protected}
				if err := os.WriteFile(filepath.Join(invocation.OutputDir, "protected"), []byte(protected), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			runID := "audit_run_scan_class_" + strconv.Itoa(len(testCase.name))
			snapshot := materializeSnapshotForExecutorTest(t, material, runID)
			invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, runID, runID, auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			testCase.mutate(t, &material, invocation)
			scan := testCase.scan
			if scan == nil {
				scan = scanAuditInvocationWritableTrees
			}
			err = scan(material.policy, material.entry, invocation)
			var failure *auditArtifactScanError
			if !errors.As(err, &failure) || failure.failureClass() != testCase.wantClass || err.Error() != testCase.wantClass {
				t.Fatalf("scan error = %T %v, class %q, want typed class %q", err, err, failure.failureClass(), testCase.wantClass)
			}
			if !errors.Is(err, testCase.wantCause) {
				t.Fatalf("scan error = %v, want errors.Is(..., %v)", err, testCase.wantCause)
			}
			if !protocolIdentifierPattern.MatchString(failure.failureClass()) {
				t.Fatalf("scan class is not a protocol identifier: %q", failure.failureClass())
			}
		})
	}
}

func TestAuditArtifactScanFailureVocabularyIsClosed(t *testing.T) {
	roles := []auditArtifactScanRole{
		auditArtifactScanRolePrompt,
		auditArtifactScanRoleOutput,
		auditArtifactScanRoleSession,
		auditArtifactScanRoleTemporary,
		auditArtifactScanRoleUnclassified,
	}
	reasons := []auditArtifactScanReason{
		auditArtifactScanReasonWalk,
		auditArtifactScanReasonSymlink,
		auditArtifactScanReasonSpecial,
		auditArtifactScanReasonLimit,
		auditArtifactScanReasonRead,
		auditArtifactScanReasonTimeoutSecret,
		auditArtifactScanReasonFreshAuthentication,
		auditArtifactScanReasonFreshAuthority,
		auditArtifactScanReasonAuthority,
		auditArtifactScanReasonProtected,
	}
	for _, role := range roles {
		for _, reason := range reasons {
			failure := &auditArtifactScanError{role: role, reason: reason, cause: errors.New("sensitive-unbounded-cause")}
			if reason == auditArtifactScanReasonAuthority {
				failure.authorityKind = auditAuthorityKindRepository
				if role == auditArtifactScanRoleTemporary {
					failure.temporaryLocation = auditArtifactTemporaryLocationOther
				}
			}
			class := failure.failureClass()
			if !protocolIdentifierPattern.MatchString(class) || failure.Error() != class || strings.Contains(failure.Error(), "sensitive") {
				t.Fatalf("role %q reason %q produced unsafe class/error %q/%q", role, reason, class, failure.Error())
			}
		}
	}
	for _, failure := range []*auditArtifactScanError{
		{role: "path", reason: auditArtifactScanReasonRead, cause: ErrAuthentication},
		{role: auditArtifactScanRoleSession, reason: "content", cause: ErrAuthentication},
	} {
		if class := failure.failureClass(); class != "" {
			t.Fatalf("open scanner vocabulary produced class %q", class)
		}
	}
}

func TestAuditTemporaryArtifactAuthorityLocationVocabularyIsClosed(t *testing.T) {
	invocation := auditInvocation{
		TemporaryDir: "/closed-temporary",
		AgentDir:     "/closed-temporary/omp-agent",
		HomeDir:      "/closed-temporary/home",
	}
	for _, testCase := range []struct {
		name string
		path string
		want auditArtifactTemporaryLocation
	}{
		{name: "agent", path: "/closed-temporary/omp-agent/nested/artifact", want: auditArtifactTemporaryLocationAgent},
		{name: "home", path: "/closed-temporary/home/artifact", want: auditArtifactTemporaryLocationHome},
		{name: "temporary root", path: "/closed-temporary/artifact", want: auditArtifactTemporaryLocationRoot},
		{name: "other temporary", path: "/closed-temporary/supervisor_tests/artifact", want: auditArtifactTemporaryLocationOther},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			location := classifyAuditArtifactTemporaryLocation(invocation, testCase.path)
			failure := &auditArtifactScanError{
				role: auditArtifactScanRoleTemporary, reason: auditArtifactScanReasonAuthority,
				authorityKind: auditAuthorityKindWorkPath, temporaryLocation: location,
				cause: errors.New("sensitive-unbounded-cause"),
			}
			wantClass := "artifact_scan_temporary_authority_" + string(testCase.want) + "_work_path"
			if location != testCase.want || failure.failureClass() != wantClass || failure.Error() != wantClass || strings.Contains(failure.Error(), "sensitive") {
				t.Fatalf("temporary path classified as location/class/error %q/%q/%q, want %q/%q", location, failure.failureClass(), failure.Error(), testCase.want, wantClass)
			}
		})
	}
	if location := classifyAuditArtifactTemporaryLocation(invocation, "/outside/artifact"); location != "" {
		t.Fatalf("outside path received temporary location %q", location)
	}
	for _, failure := range []*auditArtifactScanError{
		{
			role: auditArtifactScanRoleTemporary, reason: auditArtifactScanReasonAuthority,
			authorityKind: "raw-kind", temporaryLocation: auditArtifactTemporaryLocationHome,
			cause: errors.New("sensitive-unbounded-cause"),
		},
		{
			role: auditArtifactScanRoleTemporary, reason: auditArtifactScanReasonAuthority,
			authorityKind: auditAuthorityKindWorkPath, temporaryLocation: "raw-location",
			cause: errors.New("sensitive-unbounded-cause"),
		},
	} {
		if class := failure.failureClass(); class != "" || strings.Contains(failure.Error(), "sensitive") {
			t.Fatalf("open authority vocabulary produced class/error %q/%q", class, failure.Error())
		}
	}
}

func TestAuditFreshSessionArtifactDenialVectorsFailClosed(t *testing.T) {
	type denialCase struct {
		name      string
		wantLimit bool
		create    func(*testing.T, gitArchivePolicyMaterial, auditInvocation, string, auditSessionJSONLTestSpec)
	}
	writeValid := func(t *testing.T, _ gitArchivePolicyMaterial, _ auditInvocation, path string, spec auditSessionJSONLTestSpec) {
		writeAuditSessionJSONLForTest(t, path, spec)
	}
	for _, testCase := range []denialCase{
		{name: "malformed JSONL", create: func(t *testing.T, _ gitArchivePolicyMaterial, invocation auditInvocation, path string, _ auditSessionJSONLTestSpec) {
			if err := os.WriteFile(path, []byte("{malformed\n"+invocation.WorkDir), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "non JSONL", create: func(t *testing.T, _ gitArchivePolicyMaterial, _ auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			writeAuditSessionJSONLForTest(t, strings.TrimSuffix(path, ".jsonl")+".log", spec)
		}},
		{name: "symlink", create: func(t *testing.T, _ gitArchivePolicyMaterial, _ auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			target := filepath.Join(t.TempDir(), "foreign.jsonl")
			writeAuditSessionJSONLForTest(t, target, spec)
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "special file", create: func(t *testing.T, _ gitArchivePolicyMaterial, _ auditInvocation, path string, _ auditSessionJSONLTestSpec) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "nested artifact", create: func(t *testing.T, _ gitArchivePolicyMaterial, invocation auditInvocation, _ string, spec auditSessionJSONLTestSpec) {
			nested := filepath.Join(invocation.SessionDir, "nested")
			if err := os.Mkdir(nested, 0o700); err != nil {
				t.Fatal(err)
			}
			writeAuditSessionJSONLForTest(t, filepath.Join(nested, "fresh.jsonl"), spec)
		}},
		{name: "wrong UUID", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.UUID = "not-a-session-uuid"
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "wrong CWD", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.CWD = filepath.Join(spec.CWD, "foreign")
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "wrong prompt", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.Prompt += "\nforeign prompt"
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "credential bytes", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			t.Setenv("SUDO_API_KEY", "fresh-session-credential-must-not-leak")
			spec.Additional = append(spec.Additional, map[string]any{"credential": "fresh-session-credential-must-not-leak"})
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "original repository path", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.Additional = append(spec.Additional, map[string]any{"path": material.entry.Repository.Path})
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "wrapper path", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.Additional = append(spec.Additional, map[string]any{"path": material.entry.Wrapper.Path})
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "protected path", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			if len(material.policy.protectedPaths) == 0 {
				t.Fatal("test policy has no protected paths")
			}
			spec.Additional = append(spec.Additional, map[string]any{"path": material.policy.protectedPaths[0]})
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "stale invocation path", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.Additional = append(spec.Additional, map[string]any{"path": filepath.Join(material.entry.WorkRoot, "stale", "source")})
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "foreign path", create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.Additional = append(spec.Additional, map[string]any{"path": "/private/var/tmp/ananke-foreign-session-path"})
			writeValid(t, material, invocation, path, spec)
		}},
		{name: "too many artifacts", wantLimit: true, create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			writeValid(t, material, invocation, path, spec)
			for index := range maxAuditTreeScanFiles {
				if err := os.WriteFile(filepath.Join(invocation.SessionDir, "artifact-"+strconv.Itoa(index)), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{name: "oversized artifact", wantLimit: true, create: func(t *testing.T, material gitArchivePolicyMaterial, invocation auditInvocation, path string, spec auditSessionJSONLTestSpec) {
			spec.Additional = append(spec.Additional, map[string]any{"blob": strings.Repeat("x", maxAuditTreeScanBytes)})
			writeValid(t, material, invocation, path, spec)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			runID := "audit_run_fresh_deny_" + strconv.Itoa(len(testCase.name))
			snapshot := materializeSnapshotForExecutorTest(t, material, runID)
			invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, runID, runID, auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			spec := authenticatedFreshAuditSessionSpecForTest(t, invocation)
			testCase.create(t, material, invocation, filepath.Join(invocation.SessionDir, "fresh.jsonl"), spec)
			err = scanAuditInvocationWritableTrees(material.policy, material.entry, invocation)
			if err == nil || testCase.wantLimit && !errors.Is(err, ErrLimit) || !testCase.wantLimit && !errors.Is(err, ErrAuthentication) {
				t.Fatalf("denial vector error = %v, want %v", err, map[bool]error{true: ErrLimit, false: ErrAuthentication}[testCase.wantLimit])
			}
		})
	}
}

func authenticatedFreshAuditSessionSpecForTest(t *testing.T, invocation auditInvocation) auditSessionJSONLTestSpec {
	t.Helper()
	physicalWorkDir, err := filepath.EvalSymlinks(invocation.WorkDir)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(invocation.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	return auditSessionJSONLTestSpec{
		UUID: "019f9a4a-a904-7000-b341-e07ecf0e3baf", CWD: physicalWorkDir, Prompt: string(prompt),
		PathRefs: []string{
			invocation.PromptDir, invocation.PromptPath, invocation.OutputDir, invocation.OutputPath,
			invocation.SessionDir, invocation.TemporaryDir, physicalWorkDir,
		},
	}
}

func writeAuditSessionJSONLForTest(t *testing.T, path string, spec auditSessionJSONLTestSpec) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	records := []any{
		map[string]any{"type": "session", "id": spec.UUID, "cwd": spec.CWD},
		map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": spec.Prompt}},
		map[string]any{"type": "invocation_paths", "paths": spec.PathRefs},
	}
	records = append(records, spec.Additional...)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAuditGatewayIPv6BindProbeProcess(t *testing.T) {
	authority := os.Getenv(auditGatewayIPv6BindProbeEnvironment)
	if authority == "" {
		return
	}
	if os.Getenv("SUDO_API_KEY") != "" {
		t.Fatal("external bind probe inherited audit credential")
	}
	listener, err := net.Listen("tcp6", authority)
	if err == nil {
		_ = listener.Close()
		t.Fatalf("external process acquired live gateway IPv6 authority %q", authority)
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("external IPv6 bind error = %v, want address in use", err)
	}
}

func resultBrokerAddressForProfileTest(t *testing.T, profile string) string {
	t.Helper()
	const prefix = `(allow network-outbound (remote tcp "`
	start := strings.Index(profile, prefix)
	if start < 0 {
		t.Fatalf("sandbox profile has no network authority:\n%s", profile)
	}
	start += len(prefix)
	end := strings.Index(profile[start:], `"))`)
	if end < 0 {
		t.Fatalf("sandbox profile has malformed network authority:\n%s", profile)
	}
	return profile[start : start+end]
}

type fakeAuditOMPFixture struct {
	Scenario                    string   `json:"scenario"`
	Output                      string   `json:"output,omitempty"`
	ResumeOutput                string   `json:"resume_output,omitempty"`
	SessionUUID                 string   `json:"session_uuid,omitempty"`
	ExitCode                    int      `json:"exit_code,omitempty"`
	DelayMilliseconds           int      `json:"delay_milliseconds,omitempty"`
	OriginalPath                string   `json:"original_path,omitempty"`
	ExpectedGitExecutablePath   string   `json:"expected_git_executable_path,omitempty"`
	ProtectedPaths              []string `json:"protected_paths,omitempty"`
	UnixSocketPath              string   `json:"unix_socket_path,omitempty"`
	TCPAddress                  string   `json:"tcp_address,omitempty"`
	SpoofLog                    string   `json:"spoof_log,omitempty"`
	EmitCredential              bool     `json:"emit_credential,omitempty"`
	WriteCredentialArtifacts    bool     `json:"write_credential_artifacts,omitempty"`
	WriteTemporaryWorkAuthority bool     `json:"write_temporary_work_authority,omitempty"`
	FreshSessionMode            string   `json:"fresh_session_mode,omitempty"`
}

func TestNativeFakeAuditOMPRejectsCallerSelectedGitPath(t *testing.T) {
	entry := executionPolicyEntry{GitExecutable: executionPolicyFileIdentity{Path: auditGitExecutable}}
	fixture := fakeAuditOMPFixture{Scenario: "git_startup_boundary", ExpectedGitExecutablePath: "/usr/bin/git"}
	if _, err := bindFakeAuditOMPFixtureGitExecutable(entry, fixture); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("caller-selected fake OMP Git path error = %v, want %v", err, ErrAuthentication)
	}
}

func bindFakeAuditOMPFixtureGitExecutable(entry executionPolicyEntry, fixture fakeAuditOMPFixture) (fakeAuditOMPFixture, error) {
	if entry.GitExecutable.Path != auditGitExecutable ||
		fixture.ExpectedGitExecutablePath != "" && fixture.ExpectedGitExecutablePath != entry.GitExecutable.Path {
		return fakeAuditOMPFixture{}, authenticationError("fake OMP Git executable binding")
	}
	fixture.ExpectedGitExecutablePath = entry.GitExecutable.Path
	return fixture, nil
}

func installNativeFakeAuditOMPForTest(t *testing.T, entry *executionPolicyEntry, fixture fakeAuditOMPFixture) {
	t.Helper()
	if entry == nil || fixture.Scenario == "" {
		t.Fatal("invalid native fake OMP fixture")
	}
	var err error
	fixture, err = bindFakeAuditOMPFixtureGitExecutable(*entry, fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixtureBytes, err := marshalCanonical(fixture)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(fixtureBytes)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(entry.OMPExecutable.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry.OMPExecutable.Path); err != nil {
		t.Fatal(err)
	}
	linkerFlags := "-X main.encodedSpec=" + encoded + " -X main.expectedGitExecutablePath=" + entry.GitExecutable.Path
	command := exec.Command("go", "build", "-trimpath", "-ldflags", linkerFlags, "-o", entry.OMPExecutable.Path, "./internal/trustedsupervisor/testdata/fakeomp")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build native fake OMP: %v: %s", err, output)
	}
	if err := os.Chmod(entry.OMPExecutable.Path, 0o500); err != nil {
		t.Fatal(err)
	}
	entry.OMPExecutable = fileIdentityForTest(t, entry.OMPExecutable.Path)
	entry.OMPRuntimeAuthority = executionPolicyAtomicRuntimeAuthorityForTest(t, *entry)
}

func setNativeFakeAuditOMPForTest(t *testing.T, material *gitArchivePolicyMaterial, fixture fakeAuditOMPFixture) {
	t.Helper()
	installNativeFakeAuditOMPForTest(t, &material.entry, fixture)
	material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
	writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
	policy, err := loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	policy.testBrokerDependencies = fakeAuditBrokerDependencies()
	material.policy = policy
}

func materializeSnapshotForExecutorTest(t *testing.T, material gitArchivePolicyMaterial, runID string) auditSnapshot {
	t.Helper()
	snapshot, err := materializeAuditSnapshot(context.Background(), material.policy, material.entry, runID, auditSnapshotHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { makeTreeRemovableForTest(snapshot.RunRoot) })
	return snapshot
}
