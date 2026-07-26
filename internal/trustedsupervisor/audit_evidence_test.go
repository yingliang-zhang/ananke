package trustedsupervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAuditEvidenceRejectsOutputTamperSecretOversizeAndMalformedSession(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	for _, testCase := range []struct {
		name               string
		script             string
		executablePaths    []string
		mutate             func(*testing.T, auditInvocation)
		verifyBeforeMutate bool
		runRejected        bool
	}{
		{
			name: "output tamper",
			script: `#!/bin/sh
set -eu
printf '%s' '` + validAuditModelReportJSONForTest + `' > "$3"
`,
			verifyBeforeMutate: true,
			mutate: func(t *testing.T, invocation auditInvocation) {
				if err := os.WriteFile(invocation.OutputPath, []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "secret output",
			script: `#!/bin/sh
set -eu
printf '%s' "$SUDO_API_KEY" > "$3"
`,
			runRejected: true,
		},
		{
			name: "oversize output",
			script: `#!/bin/sh
set -eu
/bin/dd if=/dev/zero of="$3" bs=262145 count=1 2>/dev/null
`,
			executablePaths: []string{"/bin/dd"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			script := strings.ReplaceAll(testCase.script, "COMMAND_HASH", material.entry.AllowedTests[0].CommandSHA256)
			setFakeAuditWrapperForTest(t, &material, script, testCase.executablePaths...)
			snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_evidence_001")
			invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_evidence_001_attempt_1", "audit_run_evidence_001", auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SUDO_API_KEY", "credential-must-not-leak")
			result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{})
			if testCase.runRejected {
				if !errors.Is(err, ErrAuthentication) {
					t.Fatalf("run rejected evidence fixture error = %v, want %v", err, ErrAuthentication)
				}
				assertAuditExecutionRootsEmpty(t, material.entry)
				return
			}
			if err != nil {
				t.Fatalf("run evidence fixture: %v", err)
			}
			if testCase.verifyBeforeMutate {
				intent, completed := auditEvidenceLifecycleForTest(t, material.entry, invocation, result)
				collected, err := collectAuditEvidence(context.Background(), material.policy, material.entry, intent, snapshot, invocation, result, completed, auditSupervisorTestHooks{})
				if err != nil {
					t.Fatalf("verify retained success artifact: %v", err)
				}
				if collected.Report.OMPVersion != supportedOMPVersion || collected.Report.OMPNativeAddonSHA256 != material.entry.OMPNativeAddon.SHA256 {
					t.Fatalf("evidence lost OMP native binding: %+v", collected.Report)
				}
			}
			if testCase.mutate != nil {
				testCase.mutate(t, invocation)
			}
			intent, completed := auditEvidenceLifecycleForTest(t, material.entry, invocation, result)
			if _, err := collectAuditEvidence(context.Background(), material.policy, material.entry, intent, snapshot, invocation, result, completed, auditSupervisorTestHooks{}); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("adversarial evidence error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestCompletedAuditCallbackRequiresEveryOwnedRootAbsent(t *testing.T) {
	for _, rootName := range []string{"prompt", "output", "session", "temporary", "work", "wrapper_transport"} {
		t.Run(rootName, func(t *testing.T) {
			fixture := validAuditExecutionHistoryForTest(t)
			completed := fixture.Events[len(fixture.Events)-1]
			var report auditEvidenceReport
			if err := decodeCanonical([]byte(completed.EvidenceJSON), &report); err != nil {
				t.Fatal(err)
			}
			var root string
			switch rootName {
			case "prompt":
				root = filepath.Dir(completed.PromptPath)
			case "output":
				root = filepath.Dir(completed.OutputPath)
			case "session":
				root = completed.SessionPath
			case "temporary":
				root = completed.TemporaryPath
			case "work":
				root = filepath.Dir(completed.WorkPath)
			case "wrapper_transport":
				root = filepath.Join(completed.TemporaryPath, "wrapper-state")
			}
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if rootName == "output" {
				modelBytes, err := marshalCanonical(report.ModelReport)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(completed.OutputPath, modelBytes, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateAuditCallbackEvidence(fixture.Policy.namespaceAuthority, fixture.Intent, completed, fixture.Entry); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("completed callback with retained %s root error = %v, want %v", rootName, err, ErrAuthentication)
			}
		})
	}
}

func auditEvidenceLifecycleForTest(t *testing.T, entry executionPolicyEntry, invocation auditInvocation, result auditInvocationResult) (auditExecutionIntent, auditExecutionEvent) {
	t.Helper()
	intent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_evidence_001", EnvelopeHash: testHash("evidence-envelope"),
		LaunchSpecHash: entry.LaunchSpecHash, HandoffID: "remote_handoff_evidence_001", ReceiptHash: testHash("evidence-receipt"),
		TaskID: entry.TaskID, AttemptCap: entry.AttemptCap, PolicyHash: entry.PolicyHash, RouteMappingHash: entry.RouteMappingHash,
		RepositoryIdentityHash: entry.RepositoryIdentityHash, GitCommit: entry.GitCommit, GitTree: entry.GitTree,
		SourceArchiveSHA256: entry.SourceArchiveSHA256, WrapperSHA256: entry.Wrapper.SHA256, RunID: invocation.SessionRunID,
		CreatedAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	})
	completed := auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion, EventID: "audit_event_" + hashIDFragment(intent.IntentHash) + "_003",
		IntentHash: intent.IntentHash, Sequence: 3, State: auditStateFinalizing, Attempt: 1,
		CommandDescriptorHash: invocation.CommandDescriptorHash, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		PID: result.PID, PGID: result.PGID, ProcessStartIdentity: result.ProcessStartIdentity,
		ProcessStartedAt: result.StartedAt, ProcessFinishedAt: result.FinishedAt, ExitCode: result.ExitCode,
		StdoutSHA256: result.StdoutSHA256, StderrSHA256: result.StderrSHA256, WorkPath: invocation.WorkDir,
		OutputPath: invocation.OutputPath, SessionPath: invocation.SessionDir, PromptPath: invocation.PromptPath, TemporaryPath: invocation.TemporaryDir,
	}
	return intent, completed
}

func TestAuditWrapperCredentialLeakNeverReachesJournalOrCapture(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, `#!/bin/sh
set -eu
printf '%s' "$SUDO_API_KEY"
printf should-not-be-trusted > "$3"
`)
	t.Setenv("SUDO_API_KEY", "credential-must-not-leak")
	running := startInProcessProductionServer(t, fixture.material, now)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
		running.stop(t)
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
	running.stop(t)
	terminal := events[len(events)-1]
	if terminal.State != auditStateFailed || terminal.FailureClass != "wrapper_or_capture_verification_failed" {
		t.Fatalf("credential leak terminal = %+v", terminal)
	}
	encoded, err := marshalCanonical(terminal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "credential-must-not-leak") || terminal.EvidenceJSON != "" {
		t.Fatalf("terminal event leaked credential or unverified evidence: %s", encoded)
	}
	for _, path := range []string{fixture.material.journalPath, fixture.material.journalPath + "-wal", fixture.material.journalPath + "-shm"} {
		contents, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), "credential-must-not-leak") {
			t.Fatalf("credential leaked into %s", filepath.Base(path))
		}
	}
	assertAuditExecutionRootsEmpty(t, fixture.entry)
}

func TestAuditFailureAndTimeoutScrubCredentialBearingTrees(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	for _, testCase := range []struct {
		name         string
		script       string
		failureClass string
	}{
		{
			name: "nonzero credential artifacts",
			script: `#!/bin/sh
set -eu
printf '%s' "$SUDO_API_KEY" > "$3"
printf '%s' "$SUDO_API_KEY" > "${11}/secret"
printf '%s' "$SUDO_API_KEY" > "$TMPDIR/secret"
exit 9
`,
			failureClass: "wrapper_or_capture_verification_failed",
		},
		{
			name: "secret-bearing timeout",
			script: `#!/bin/sh
set -eu
printf '%s' "$SUDO_API_KEY" > "${11}/secret"
printf '[OMP_TIMEOUT]\ntimeout_source=internal\nsession_id=019f9a4a-a904-7000-b341-e07ecf0e3baf\nrecovery_hint=resume exact session\n' > "$3"
exit 124
`,
			failureClass: "wrapper_or_capture_verification_failed",
		},
		{
			name: "malformed timeout",
			script: `#!/bin/sh
set -eu
printf safe > "${11}/safe"
printf '[OMP_TIMEOUT]\ntimeout_source=internal\n' > "$3"
exit 124
`,
			failureClass: "malformed_timeout_evidence",
		},
		{
			name: "evidence rejection",
			script: `#!/bin/sh
set -eu
printf safe > "$3"
printf '[OMP_SESSION] session_id=019f9a4a-a904-7000-b341-e07ecf0e3baf\n' > "${11}/incomplete.log"
`,
			failureClass: "evidence_verification_failed",
		},
		{
			name: "closed nonzero exit",
			script: `#!/bin/sh
set -eu
exit 9
`,
			failureClass: "wrapper_exit_nonzero",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			fixture := newExecutingServerTestMaterial(t, now, testCase.script)
			t.Setenv("SUDO_API_KEY", "credential-must-not-persist")
			running := startInProcessProductionServer(t, fixture.material, now)
			client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
			if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
				running.stop(t)
				t.Fatal(err)
			}
			_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
			terminal := events[len(events)-1]
			if terminal.State != auditStateFailed || terminal.FailureClass != testCase.failureClass {
				running.stop(t)
				t.Fatalf("closed failure terminal = %+v, want class %s", terminal, testCase.failureClass)
			}
			running.stop(t)
		})
	}
}

func assertAuditExecutionRootsEmpty(t *testing.T, entry executionPolicyEntry) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		remaining := make(map[string]int)
		for _, root := range []string{entry.PromptRoot, entry.OutputRoot, entry.SessionRoot, entry.WorkRoot, entry.TemporaryRoot} {
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read cleanup root %s: %v", root, err)
			}
			if len(entries) != 0 {
				remaining[root] = len(entries)
			}
		}
		if len(remaining) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup roots retained entries: %+v", remaining)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
