package trustedsupervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	directAuditLauncherMode        = "sandbox_exec_direct_pinned_omp_v1"
	directAuditArgvPolicy          = "omp_print_exact_prompt_sudo_route_v1"
	directAuditOutputTransport     = "supervisor_bounded_stdout_private_file_v1"
	directAuditSandboxTargetPolicy = "exact_pinned_omp_executable_v1"
	directAuditTimeoutOwner        = "trusted_supervisor_typed_observation_v1"
)

func TestP5DirectInvocationDerivesExactOracleOMPArgv(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_direct_argv_001")
	const sessionUUID = "019f9a4a-a904-7000-b341-e07ecf0e3baf"

	for _, testCase := range []struct {
		name   string
		resume auditResume
	}{
		{name: "fresh"},
		{name: "exact resume", resume: auditResume{SessionUUID: sessionUUID, SynthesizeOnly: true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runID := "audit_run_direct_argv_001_attempt_1"
			if testCase.resume.SessionUUID != "" {
				runID = "audit_run_direct_argv_001_attempt_2"
			}
			invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, runID, "audit_run_direct_argv_001", testCase.resume)
			if err != nil {
				t.Fatal(err)
			}
			prompt := readOnlyAuditPromptTemplate
			if testCase.resume.SynthesizeOnly {
				prompt += readOnlyAuditSynthesizePromptSuffix
			}
			want := []string{"-p", prompt, "--yolo", "--max-time", "60"}
			if testCase.resume.SessionUUID != "" {
				want = append(want, "--resume", sessionUUID)
			}
			want = append(want,
				"--model", "sudo/gpt-5.6-sol",
				"--thinking", "xhigh",
				"--session-dir", invocation.SessionDir,
			)
			if !reflect.DeepEqual(invocation.Arguments, want) {
				t.Fatalf("direct OMP argv mismatch: got %q", invocation.Arguments)
			}
			for _, forbidden := range []string{"--continue", "--hermes-provider", "--hermes-model", "--task-tier"} {
				if slicesContainString(invocation.Arguments, forbidden) {
					t.Fatalf("direct OMP argv retained caller/oracle-only flag %q", forbidden)
				}
			}
		})
	}
}

func TestP5DirectRouteIsExactClosedSealedDeclaration(t *testing.T) {
	valid := executionPolicyEntry{
		HermesProvider:             "custom:sudo",
		HermesModel:                "gpt-5.6-sol",
		TaskTier:                   "normal",
		ProviderEndpoint:           executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		CredentialEnvironmentNames: []string{"SUDO_CODING_KEY"},
	}
	if !validAuditModelRoute(valid) {
		t.Fatal("sealed direct Sudo route was rejected")
	}
	for _, mutate := range []func(*executionPolicyEntry){
		func(entry *executionPolicyEntry) { entry.HermesProvider = "sudo" },
		func(entry *executionPolicyEntry) { entry.HermesModel = "gpt-5.6-terra" },
		func(entry *executionPolicyEntry) { entry.TaskTier = "hard" },
		func(entry *executionPolicyEntry) { entry.CredentialEnvironmentNames = []string{"SUDO_API_KEY"} },
		func(entry *executionPolicyEntry) {
			entry.CredentialEnvironmentNames = []string{"SUDO_CODING_KEY", "SUDO_API_KEY"}
		},
	} {
		changed := valid
		changed.CredentialEnvironmentNames = append([]string(nil), valid.CredentialEnvironmentNames...)
		mutate(&changed)
		if validAuditModelRoute(changed) {
			t.Fatalf("unsupported direct route was admitted: provider=%q model=%q tier=%q credentials=%q", changed.HermesProvider, changed.HermesModel, changed.TaskTier, changed.CredentialEnvironmentNames)
		}
	}
}

func TestP5DirectCommandUsesPinnedOMPTargetWithoutShellTransport(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_direct_target_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_direct_target_001", "audit_run_direct_target_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	invocation.SandboxProfile = "(version 1)\n(deny default)\n"
	want := append([]string{"-p", invocation.SandboxProfile, material.entry.OMPExecutable.Path}, invocation.Arguments...)
	if got := auditSandboxCommandArguments(invocation); !reflect.DeepEqual(got, want) {
		t.Fatalf("sandbox target argv mismatch: got %q", got)
	}
}

func TestP5ProductionCommandHasNoStdinOrInheritedFiles(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_direct_transport_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_direct_transport_001", "audit_run_direct_transport_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_CODING_KEY", "direct-command-test-credential")
	var inspected bool
	_, runErr := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		StartCommand: func(command *exec.Cmd, reader, writer *os.File) error {
			inspected = true
			if reader != nil || writer != nil || command.Stdin != nil || len(command.ExtraFiles) != 0 {
				t.Fatal("production direct command retained stdin, wrapper pipe, or inherited descriptors")
			}
			want := append([]string{auditSandboxExecutable, "-p", command.Args[2], material.entry.OMPExecutable.Path}, invocation.Arguments...)
			if command.Path != auditSandboxExecutable || !reflect.DeepEqual(command.Args, want) {
				t.Fatalf("production command did not target exact pinned OMP: path=%q argv=%q", command.Path, command.Args)
			}
			return errors.New("injected post-inspection start failure")
		},
	})
	var stage *auditInvocationStageError
	if !inspected || !errors.As(runErr, &stage) || stage.failureClass != "command_start_failed" {
		t.Fatalf("direct command inspection did not reach the closed start boundary: inspected=%v error=%v", inspected, runErr)
	}
}

func TestDarwinP5SandboxGrantsNoSharedTempHeredocAuthority(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_direct_temp_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_direct_temp_001", "audit_run_direct_temp_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	profile := auditSandboxProfile(material.policy, material.entry, invocation.WorkDir, invocation.PromptDir, invocation.OutputDir, invocation.SessionDir, invocation.TemporaryDir, material.entry.OMPNativeAddon.Path, "localhost:43210")
	invocation.SandboxProfile = profile
	for _, prefix := range []string{"(allow file-read* file-test-existence", "(allow file-map-executable", "(allow process-exec"} {
		var rule string
		for _, line := range strings.Split(profile, "\n") {
			if strings.HasPrefix(line, prefix) {
				rule = line
				break
			}
		}
		if rule == "" {
			t.Fatalf("sandbox profile omitted %q rule", prefix)
		}
		for _, identity := range []executionPolicyFileIdentity{material.entry.OMPExecutable, material.entry.GitExecutable} {
			for _, path := range sandboxPathVariants(identity.Path) {
				literal := `(literal "` + sandboxLiteral(path) + `")`
				if !strings.Contains(rule, literal) {
					t.Fatalf("sandbox %q rule omitted exact pinned executable literal %q:\n%s", prefix, literal, rule)
				}
			}
		}
		for _, forbiddenPath := range []string{
			"/usr/bin/git",
			"/usr/bin",
			"/bin/sh",
			"/bin/bash",
			"/usr/bin/env",
			material.entry.AllowedTests[0].Executable.Path,
			"/Applications/Xcode.app/Contents/Developer/usr/bin/git",
			"/Applications/Xcode-beta.app/Contents/Developer/usr/bin/git",
			"/Library/Developer/CommandLineTools/usr/bin/make",
		} {
			for _, filter := range []string{"literal", "subpath"} {
				authority := `(` + filter + ` "` + sandboxLiteral(forbiddenPath) + `")`
				if strings.Contains(rule, authority) {
					t.Fatalf("sandbox %q rule granted forbidden executable authority %q:\n%s", prefix, authority, rule)
				}
			}
		}
		if strings.Contains(rule, `(subpath "/bin")`) {
			t.Fatalf("sandbox %q rule granted executable-root subpath authority:\n%s", prefix, rule)
		}
	}
	arguments := auditSandboxCommandArguments(invocation)
	if len(arguments) < 3 || arguments[2] != material.entry.OMPExecutable.Path {
		t.Fatalf("sandbox target = %q, want exact pinned OMP", arguments)
	}
	for _, forbidden := range []string{"/bin/bash", "/bin/sh", "/usr/bin/env", "sh-thd-"} {
		if strings.Contains(profile, forbidden) || slicesContainString(arguments, forbidden) {
			t.Fatalf("direct sandbox retained shared-temp/Bash authority %q", forbidden)
		}
	}
	for _, sharedTemp := range sandboxPathVariants(os.TempDir()) {
		rule := `(allow file-write* (subpath "` + sandboxLiteral(filepath.Clean(sharedTemp)) + `"))`
		if strings.Contains(profile, rule) {
			t.Fatalf("sandbox granted shared OS temp write authority")
		}
	}
}

func slicesContainString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
