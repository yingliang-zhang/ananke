package trustedsupervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestDarwinAuditWrapperSandboxEnforcesReadonlySnapshotAndRootIsolation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	originalPath := filepath.Join(material.entry.Repository.Path, "audit.txt")
	script := `#!/bin/sh
set -eu
[ "$#" -eq 11 ]
[ "$1" = "60" ]
prompt=$2
output=$3
[ "$4" = "--hermes-provider" ]
[ "$5" = "custom:sudo" ]
[ "$6" = "--hermes-model" ]
[ "$7" = "gpt-5.6-sol" ]
[ "$8" = "--task-tier" ]
[ "$9" = "normal" ]
[ "${10}" = "--session-dir" ]
[ "${11}" = "$OMP_SESSION_ROOT" ]
[ "$SUDO_API_KEY" = "credential-must-not-leak" ]
if /bin/chmod u+w "$PWD/audit.txt" 2>/dev/null; then exit 70; fi
if /bin/mv "$PWD/audit.txt" "$PWD/moved.txt" 2>/dev/null; then exit 71; fi
if /bin/ln -s "$PWD/audit.txt" "$TMPDIR/source-link" && /bin/sh -c 'printf tamper > "$1"' sh "$TMPDIR/source-link" 2>/dev/null; then exit 72; fi
if /bin/sh -c 'printf tamper > "$1"' sh '` + originalPath + `' 2>/dev/null; then exit 73; fi
native_addon="$HOME/.omp/natives/17.1.3/pi_natives.darwin-arm64.node"
case "$native_addon" in
  /var/*) native_alias="/private$native_addon" ;;
  /private/var/*) native_alias="${native_addon#/private}" ;;
  *) exit 69 ;;
esac
if /bin/sh -c 'printf tamper > "$1"' sh "$native_addon" 2>/dev/null; then exit 74; fi
if /bin/mkdir "$HOME/.omp/natives/17.1.3/child-mutation" 2>/dev/null; then exit 75; fi
if /bin/sh -c 'printf tamper > "$1"' sh "$native_alias" 2>/dev/null; then exit 76; fi
if /bin/mkdir "${native_alias%/*}/alias-child-mutation" 2>/dev/null; then exit 77; fi
/bin/sh -c 'printf prompt-write > "$1"' sh "$prompt.touch"
/bin/sh -c 'printf temporary > "$1"' sh "$TMPDIR/temporary"
/bin/sh -c 'printf session > "$1"' sh "$OMP_SESSION_ROOT/session.uuid"
/bin/sh -c 'printf audit-success > "$1"' sh "$output"
`
	setFakeAuditWrapperForTest(t, &material, script, "/bin/ln", "/bin/sh")
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_sandbox_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_sandbox_001", "audit_run_sandbox_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_API_KEY", "credential-must-not-leak")
	var sandboxProfile string
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		BrokerReady: func(_ string, profile string) { sandboxProfile = profile },
	})
	if err != nil || result.ExitCode != 0 || result.TimedOut {
		t.Fatalf("sandboxed fake wrapper result = %+v, %v\n%s", result, err, sandboxProfile)
	}
	assertAuditNativeWriteDenyVariants(t, sandboxProfile, result.boundInvocation.NativeAddonPath)
	if output, err := os.ReadFile(invocation.OutputPath); err != nil || string(output) != "audit-success" {
		t.Fatalf("wrapper output = %q, %v", output, err)
	}
	if source, err := os.ReadFile(filepath.Join(snapshot.SourceRoot, "audit.txt")); err != nil || string(source) != "immutable audit source\n" {
		t.Fatalf("snapshot mutated = %q, %v", source, err)
	}
	if original, err := os.ReadFile(originalPath); err != nil || string(original) != "immutable audit source\n" {
		t.Fatalf("original repository mutated = %q, %v", original, err)
	}
	for _, path := range []string{invocation.PromptPath + ".touch", filepath.Join(invocation.TemporaryDir, "temporary"), filepath.Join(invocation.SessionDir, "session.uuid")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("allowed isolated root write %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(invocation.TemporaryDir, "source-link")); err != nil {
		t.Fatalf("sandbox did not permit isolated symlink creation: %v", err)
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
	script := `#!/bin/sh
set -eu
[ "$0" = "/bin/bash" ]
[ "$#" -eq 11 ]
[ "$1" = "60" ]
[ "$4" = "--hermes-provider" ] && [ "$5" = "custom:sudo" ]
[ "$6" = "--hermes-model" ] && [ "$7" = "gpt-5.6-sol" ]
[ "$8" = "--task-tier" ] && [ "$9" = "normal" ]
[ "${10}" = "--session-dir" ] && [ "${11}" = "$OMP_SESSION_ROOT" ]
/usr/bin/env | /usr/bin/sed 's/=.*//' | /usr/bin/sort > "$OMP_SESSION_ROOT/env.names"
printf '%s' "$XDG_DATA_HOME" > "$OMP_SESSION_ROOT/xdg-data-home"
[ "${HTTP_PROXY+x}" != x ] && [ "${HTTPS_PROXY+x}" != x ] && [ "${ALL_PROXY+x}" != x ] && [ "${NO_PROXY+x}" != x ]
printf exact > "$3"
`
	setFakeAuditWrapperForTest(t, &material, script, "/usr/bin/env", "/usr/bin/sed", "/usr/bin/sort")
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
	t.Setenv("SUDO_API_KEY", "credential-must-not-leak")
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
	want := []string{"HOME", "LANG", "LC_ALL", "OMP_SESSION_ROOT", "OMP_WRAPPER_HARD_GRACE_SECONDS", "OMP_WRAPPER_STATE_DIR", "PATH", "PI_CODING_AGENT_DIR", "PWD", "SHLVL", "SUDO_API_KEY", "TMPDIR", "TZ", "XDG_DATA_HOME", "_"}
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("wrapper environment names = %q, want %q", names, want)
	}
	xdgDataHome, err := os.ReadFile(filepath.Join(invocation.SessionDir, "xdg-data-home"))
	if err != nil || string(xdgDataHome) != material.entry.OMPRuntimeAuthority.NativeDataRoot {
		t.Fatalf("XDG_DATA_HOME = %q, %v; want %q", xdgDataHome, err, material.entry.OMPRuntimeAuthority.NativeDataRoot)
	}
	commandInvocation := invocation
	commandInvocation.SandboxProfile = sandboxProfile
	commandInvocation.SandboxProfileHash = hashJournalBytes([]byte(sandboxProfile))
	wantCommandArguments := append([]string{"-p", sandboxProfile, auditBashExecutable, "-s", "--"}, invocation.Arguments...)
	if got := auditSandboxCommandArguments(commandInvocation); !reflect.DeepEqual(got, wantCommandArguments) {
		t.Fatalf("sandbox command arguments = %q, want %q", got, wantCommandArguments)
	}
	for _, private := range []string{"credential-must-not-leak", material.entry.Wrapper.Path, material.entry.Repository.Path, invocation.PromptPath} {
		if strings.Contains(result.Stdout, private) || strings.Contains(result.Stderr, private) {
			t.Fatalf("wrapper capture leaked private value %q", private)
		}
	}
}

func TestAuditInvocationExecutesFrozenWrapperBytesAcrossAllMutationWindows(t *testing.T) {
	if auditPlatformSupported("linux") {
		t.Fatal("unsupported production platform was admitted")
	}
	if runtime.GOOS != "darwin" {
		return
	}
	for _, testCase := range []struct {
		name  string
		hooks func(*testing.T, *gitArchivePolicyMaterial) auditInvocationHooks
	}{
		{
			name: "path replacement after final hash",
			hooks: func(t *testing.T, material *gitArchivePolicyMaterial) auditInvocationHooks {
				return auditInvocationHooks{BeforeStart: func() {
					if err := os.Rename(material.entry.Wrapper.Path, material.entry.Wrapper.Path+".pinned"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nprintf replacement > \"$3\"\n"), 0o700); err != nil {
						t.Fatal(err)
					}
				}}
			},
		},
		{
			name: "in-place rewrite after final hash",
			hooks: func(t *testing.T, material *gitArchivePolicyMaterial) auditInvocationHooks {
				return auditInvocationHooks{BeforeStart: func() {
					if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nprintf replacement > \"$3\"\n"), 0o700); err != nil {
						t.Fatal(err)
					}
				}}
			},
		},
		{
			name: "in-place rewrite while bash waits for pipe bytes",
			hooks: func(t *testing.T, material *gitArchivePolicyMaterial) auditInvocationHooks {
				return auditInvocationHooks{BeforeWrapperWrite: func() {
					if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nprintf replacement > \"$3\"\n"), 0o700); err != nil {
						t.Fatal(err)
					}
				}}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			setFakeAuditWrapperForTest(t, &material, "#!/bin/sh\nprintf frozen-original > \"$3\"\n")
			snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_toctou_001")
			invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_toctou_001", "audit_run_toctou_001", auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, testCase.hooks(t, &material))
			if err != nil || result.ExitCode != 0 {
				t.Fatalf("frozen wrapper result = %+v, %v", result, err)
			}
			contents, err := os.ReadFile(invocation.OutputPath)
			if err != nil || string(contents) != "frozen-original" {
				t.Fatalf("frozen wrapper output = %q, %v", contents, err)
			}
		})
	}
}

func TestAuditInvocationRejectsWrapperMutationBeforeFreeze(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	setFakeAuditWrapperForTest(t, &material, "#!/bin/sh\nprintf frozen-original > \"$3\"\n")
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
	setFakeAuditWrapperForTest(t, &material, "#!/bin/sh\nprintf frozen-original > \"$3\"\n")
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
	setFakeAuditWrapperForTest(t, &material, "#!/bin/sh\ntrap 'exit 0' TERM\n/bin/sleep 30\n")
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
	script := fmt.Sprintf(`#!/bin/sh
set -eu
[ "$SUDO_API_KEY" = "selected-route-credential" ]
[ "${OPENAI_API_KEY+x}" != x ]
for path in %q %q %q %q %q %q %q %q; do
  if /bin/cat "$path" >/dev/null 2>&1; then exit 80; fi
done
if /bin/ps -p "$PPID" -o command= >/dev/null 2>&1; then exit 81; fi
if /bin/kill -0 "$PPID" >/dev/null 2>&1; then exit 82; fi
if /usr/bin/nc -z -w 1 -U %q >/dev/null 2>&1; then exit 83; fi
if /usr/bin/nc -z -w 1 %s >/dev/null 2>&1; then exit 84; fi
broker=$(/usr/bin/sed -n 's|^[[:space:]]*baseUrl: http://\([^/]*\)/v1$|\1|p' "$PI_CODING_AGENT_DIR/models.yml")
broker_host=${broker%%:*}
broker_port=${broker##*:}
[ -n "$broker_host" ] && [ -n "$broker_port" ] || exit 85
if ! /usr/bin/nc -z -w 1 "$broker_host" "$broker_port" >/dev/null 2>&1; then exit 85; fi
if /usr/bin/nc -z -w 1 93.184.216.34 443 >/dev/null 2>&1; then exit 86; fi
if /usr/bin/nc -z -w 1 api.anthropic.com 443 >/dev/null 2>&1; then exit 87; fi
if /bin/sh -c 'printf tamper > "$1"' sh "$PWD/audit.txt" 2>/dev/null; then exit 88; fi
if /bin/sh -c 'printf tamper > "$1"' sh %q 2>/dev/null; then exit 89; fi
native_addon="$XDG_DATA_HOME/omp/natives/17.1.3/pi_natives.darwin-arm64.node"
case "$native_addon" in
  /var/*) native_alias="/private$native_addon" ;;
  /private/var/*) native_alias="${native_addon#/private}" ;;
  *) exit 79 ;;
esac
if /bin/sh -c 'printf tamper > "$1"' sh "$native_addon" 2>/dev/null; then exit 90; fi
if /bin/mkdir "${native_addon%%/*}/child-mutation" 2>/dev/null; then exit 91; fi
if /bin/sh -c 'printf tamper > "$1"' sh "$native_alias" 2>/dev/null; then exit 92; fi
if /bin/mkdir "${native_alias%%/*}/alias-child-mutation" 2>/dev/null; then exit 93; fi
printf prompt-write > "$2.touch"
printf temporary > "$TMPDIR/temporary"
printf session > "$OMP_SESSION_ROOT/session.uuid"
printf fake-api-and-write-root-success > "$3"
`, protected[0], protected[1], protected[2], protected[3], protected[4], protected[5], protected[6], material.policyPath,
		unixPath, tcpListener.Addr().String(), originalPath)
	setFakeAuditWrapperForTest(t, &material, script, "/bin/sh", "/usr/bin/nc", "/usr/bin/sed")
	material.policy.setProtectedPaths(append(protected, material.policyPath, unixPath)...)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_least_authority_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_least_authority_001", "audit_run_least_authority_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_API_KEY", "selected-route-credential")
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
		t.Fatalf("least-authority fake wrapper result = %+v, %v", result, err)
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
	script := `#!/bin/sh
set -eu
broker=$(/usr/bin/sed -n 's|^[[:space:]]*baseUrl: http://\([^/]*\)/v1$|\1|p' "$PI_CODING_AGENT_DIR/models.yml")
broker_host=${broker%:*}
broker_port=${broker##*:}
[ "$broker_host" = "127.0.0.1" ]
[ "$SUDO_API_KEY" = "` + fakeCredential + `" ]
if ! /usr/bin/printf 'POST /v1/responses HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}' "$broker" "$SUDO_API_KEY" | /usr/bin/nc -4 -w 2 127.0.0.1 "$broker_port" | /usr/bin/grep -q '^HTTP/1.1 502 Bad Gateway'; then exit 94; fi
if ! /usr/bin/printf 'POST /v1/responses HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}' "$broker" "$SUDO_API_KEY" | /usr/bin/nc -6 -w 2 ::1 "$broker_port" | /usr/bin/grep -q '^HTTP/1.1 502 Bad Gateway'; then exit 95; fi
/usr/bin/printf dual-stack-gateway > "$3"
`
	setFakeAuditWrapperForTest(t, &material, script, "/usr/bin/grep", "/usr/bin/nc", "/usr/bin/printf", "/usr/bin/sed")
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
	t.Setenv("SUDO_API_KEY", fakeCredential)
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

func TestFrozenAuditWrapperWriterHandlesPartialWritesAndErrors(t *testing.T) {
	frozen := []byte("#!/bin/sh\nprintf frozen\n")
	partial := &partialAuditWrapperWriter{maximum: 3}
	if err := writeFrozenAuditWrapper(partial, frozen, time.Second); err != nil {
		t.Fatalf("partial frozen wrapper write: %v", err)
	}
	if !partial.closed || !bytes.Equal(partial.Bytes(), frozen) || partial.deadline.IsZero() {
		t.Fatalf("partial writer lifecycle = closed %v, bytes %q, deadline %v", partial.closed, partial.Bytes(), partial.deadline)
	}
	failing := &partialAuditWrapperWriter{maximum: 2, failAfter: 4}
	if err := writeFrozenAuditWrapper(failing, frozen, time.Second); !errors.Is(err, ErrAuthentication) || !failing.closed {
		t.Fatalf("failing writer result = %v, closed %v", err, failing.closed)
	}
}

func TestFrozenAuditWrapperWriterBoundsEarlyCloseAndHang(t *testing.T) {
	t.Run("early reader close", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		if err := writeFrozenAuditWrapper(writer, []byte("#!/bin/sh\nexit 0\n"), time.Second); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("early-close writer error = %v, want %v", err, ErrAuthentication)
		}
	})
	t.Run("reader never consumes", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		started := time.Now()
		err = writeFrozenAuditWrapper(writer, bytes.Repeat([]byte{'x'}, 2*1024*1024), 25*time.Millisecond)
		if !errors.Is(err, ErrAuthentication) || time.Since(started) > time.Second {
			t.Fatalf("hung writer error = %v after %v", err, time.Since(started))
		}
	})
}

func TestAuditCommandStartFailureClosesPrivatePipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(t.TempDir(), "missing-sandbox"))
	if err := startAuditCommand(command, reader, writer); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("start failure = %v, want %v", err, ErrAuthentication)
	}
	if _, err := writer.Write([]byte("must-not-block")); err == nil {
		t.Fatal("writer remained open after command start failure")
	}
}

type partialAuditWrapperWriter struct {
	bytes.Buffer
	maximum   int
	failAfter int
	closed    bool
	deadline  time.Time
}

func (writer *partialAuditWrapperWriter) Write(value []byte) (int, error) {
	if writer.failAfter > 0 && writer.Len() >= writer.failAfter {
		return 0, errors.New("injected writer failure")
	}
	if len(value) > writer.maximum {
		value = value[:writer.maximum]
	}
	return writer.Buffer.Write(value)
}

func (writer *partialAuditWrapperWriter) Close() error {
	writer.closed = true
	return nil
}

func (writer *partialAuditWrapperWriter) SetWriteDeadline(deadline time.Time) error {
	writer.deadline = deadline
	return nil
}

func setFakeAuditWrapperForTest(t *testing.T, material *gitArchivePolicyMaterial, script string, executablePaths ...string) {
	t.Helper()
	if err := os.WriteFile(material.entry.Wrapper.Path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	material.entry.Wrapper = fileIdentityForTest(t, material.entry.Wrapper.Path)
	seenExecutables := make(map[string]struct{}, len(material.entry.WrapperExecutables)+len(executablePaths))
	for _, identity := range material.entry.WrapperExecutables {
		seenExecutables[identity.Path] = struct{}{}
	}
	for _, identity := range fileIdentitiesForTest(t, executablePaths...) {
		if _, exists := seenExecutables[identity.Path]; exists {
			continue
		}
		material.entry.WrapperExecutables = append(material.entry.WrapperExecutables, identity)
		seenExecutables[identity.Path] = struct{}{}
	}
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
