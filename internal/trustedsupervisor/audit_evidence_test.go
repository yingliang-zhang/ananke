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

func TestReadOnlyAuditPromptEmbeddedExamplesDecodeWithoutRawAuthority(t *testing.T) {
	const approvedMarker = "Approved example:\n"
	const rejectedMarker = "Rejected example:\n"
	extract := func(marker string) []byte {
		t.Helper()
		start := strings.Index(readOnlyAuditPromptTemplate, marker)
		if start < 0 {
			t.Fatalf("audit prompt is missing %q", marker)
		}
		start += len(marker)
		end := strings.IndexByte(readOnlyAuditPromptTemplate[start:], '\n')
		if end < 0 {
			end = len(readOnlyAuditPromptTemplate) - start
		}
		return []byte(readOnlyAuditPromptTemplate[start : start+end])
	}

	for _, testCase := range []struct {
		marker       string
		wantVerdict  string
		wantFindings int
	}{
		{marker: approvedMarker, wantVerdict: "approved", wantFindings: 0},
		{marker: rejectedMarker, wantVerdict: "rejected", wantFindings: 1},
	} {
		report, err := decodeAuditModelReport(extract(testCase.marker), executionPolicyEntry{}, auditInvocation{})
		if err != nil {
			t.Fatalf("decode %s: %v", strings.TrimSpace(testCase.marker), err)
		}
		if report.Verdict != testCase.wantVerdict || len(report.Findings) != testCase.wantFindings {
			t.Fatalf("decoded %s = %+v, want verdict %q with %d findings", strings.TrimSpace(testCase.marker), report, testCase.wantVerdict, testCase.wantFindings)
		}
	}

	t.Setenv("SUDO_API_KEY", "prompt-contract-secret-sentinel")
	entry := executionPolicyEntry{
		Repository: executionPolicyDirectoryIdentity{Path: "/private/prompt-contract/repository"},
		Wrapper:    executionPolicyFileIdentity{Path: "/private/prompt-contract/wrapper"},
		PromptRoot: "/private/prompt-contract/prompt", OutputRoot: "/private/prompt-contract/output",
		SessionRoot: "/private/prompt-contract/session", WorkRoot: "/private/prompt-contract/work",
	}
	invocation := auditInvocation{
		PromptPath: "/private/prompt-contract/prompt/audit-prompt.txt", OutputPath: "/private/prompt-contract/output/audit-output.json",
		SessionDir: "/private/prompt-contract/session/run", WorkDir: "/private/prompt-contract/work/source",
	}
	if containsAbsoluteAuditPath(readOnlyAuditPromptTemplate) || auditBytesLeakAuthority([]byte(readOnlyAuditPromptTemplate), entry, invocation) {
		t.Fatal("fixed audit prompt contains raw machine authority or credential material")
	}
}

func TestAuditEvidenceRejectsKnownCredentialValues(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
	}{
		{name: "SUDO_CODING_KEY", value: "coding-credential-must-not-leak"},
		{name: "SUDO_API_KEY", value: "legacy-credential-must-not-leak"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(testCase.name, testCase.value)
			if !auditBytesLeakAuthority([]byte(testCase.value), executionPolicyEntry{}, auditInvocation{}) {
				t.Fatalf("evidence containing %s was accepted", testCase.name)
			}
		})
	}
}

func TestAuditAuthorityClassifierExhaustsMatchSetInPrecedenceOrder(t *testing.T) {
	t.Setenv("SUDO_API_KEY", "closed-authority-secret")
	entry := executionPolicyEntry{
		Repository:  executionPolicyDirectoryIdentity{Path: "/closed-authority/repository"},
		Wrapper:     executionPolicyFileIdentity{Path: "/closed-authority/wrapper"},
		PromptRoot:  "/closed-authority/prompt-root",
		OutputRoot:  "/closed-authority/output-root",
		SessionRoot: "/closed-authority/session-root",
		WorkRoot:    "/closed-authority/work-root",
	}
	invocation := auditInvocation{
		PromptPath: "/closed-authority/prompt-path",
		OutputPath: "/closed-authority/output-path",
		SessionDir: "/closed-authority/session-path",
		WorkDir:    "/closed-authority/work-path",
	}
	matches := []struct {
		kind  auditAuthorityKind
		value string
	}{
		{auditAuthorityKindSecret, "closed-authority-secret"},
		{auditAuthorityKindRepository, entry.Repository.Path},
		{auditAuthorityKindWrapper, entry.Wrapper.Path},
		{auditAuthorityKindPromptRoot, entry.PromptRoot},
		{auditAuthorityKindOutputRoot, entry.OutputRoot},
		{auditAuthorityKindSessionRoot, entry.SessionRoot},
		{auditAuthorityKindWorkRoot, entry.WorkRoot},
		{auditAuthorityKindPromptPath, invocation.PromptPath},
		{auditAuthorityKindOutputPath, invocation.OutputPath},
		{auditAuthorityKindSessionPath, invocation.SessionDir},
		{auditAuthorityKindWorkPath, invocation.WorkDir},
	}
	for index, match := range matches {
		values := make([]string, 0, len(matches)-index)
		for _, candidate := range matches[index:] {
			values = append(values, candidate.value)
		}
		contents := []byte(strings.Join(values, "\n"))
		kind, leaked := classifyAuditBytesLeakAuthority(contents, entry, invocation)
		if !leaked || kind != match.kind || !auditBytesLeakAuthority(contents, entry, invocation) {
			t.Fatalf("authority matches from %d classified as %q/%v, want %q/true", index, kind, leaked, match.kind)
		}
	}
	if kind, leaked := classifyAuditBytesLeakAuthority([]byte("bounded non-authority"), entry, invocation); leaked || kind != "" || auditBytesLeakAuthority([]byte("bounded non-authority"), entry, invocation) {
		t.Fatalf("non-authority classified as %q/%v", kind, leaked)
	}
}

func TestAuditEvidenceRejectsOutputTamperSecretOversizeAndMalformedSession(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	for _, testCase := range []struct {
		name               string
		fixture            fakeAuditOMPFixture
		mutate             func(*testing.T, auditInvocation)
		verifyBeforeMutate bool
		runRejected        bool
		limitRejected      bool
	}{
		{
			name: "output tamper", fixture: fakeAuditOMPFixture{Scenario: "report", Output: validAuditModelReportJSONForTest},
			verifyBeforeMutate: true,
			mutate: func(t *testing.T, invocation auditInvocation) {
				if err := os.WriteFile(invocation.OutputPath, []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{name: "secret output", fixture: fakeAuditOMPFixture{Scenario: "report", EmitCredential: true}, runRejected: true},
		{name: "oversize output", fixture: fakeAuditOMPFixture{Scenario: "oversize"}, limitRejected: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			setNativeFakeAuditOMPForTest(t, &material, testCase.fixture)

			snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_evidence_001")
			invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_evidence_001_attempt_1", "audit_run_evidence_001", auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SUDO_API_KEY", "credential-must-not-leak")
			result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{})
			if testCase.limitRejected {
				if !errors.Is(err, ErrLimit) {
					t.Fatalf("oversize direct capture error = %v, want %v", err, ErrLimit)
				}
				assertAuditExecutionRootsEmpty(t, material.entry)
				return
			}

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
	for _, rootName := range []string{"prompt", "output", "session", "temporary", "work"} {
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

func TestAuditDirectOMPCredentialLeakNeverReachesJournalOrCapture(t *testing.T) {

	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: "should-not-be-trusted", EmitCredential: true})

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
	if terminal.State != auditStateFailed || terminal.FailureClass != "direct_omp_or_capture_verification_failed" {

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

func TestAuditFreshSessionNonzeroExitSeparatesExitFromScannerFailureInJournal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	const sessionUUID = "019f9a4a-a904-7000-b341-e07ecf0e3baf"
	for _, testCase := range []struct {
		name         string
		fixture      fakeAuditOMPFixture
		failureClass string
	}{
		{
			name: "authenticated",
			fixture: fakeAuditOMPFixture{
				Scenario: "report", ExitCode: 9, SessionUUID: sessionUUID, FreshSessionMode: "authenticated",
			},
			failureClass: "direct_omp_exit_nonzero",
		},
		{
			name: "leaking",
			fixture: fakeAuditOMPFixture{
				Scenario: "report", ExitCode: 9, SessionUUID: sessionUUID, FreshSessionMode: "leaking",
				OriginalPath: "/private/var/tmp/ananke-fresh-session-leak",
			},
			failureClass: "artifact_scan_session_fresh_authentication",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			fixture := newExecutingServerTestMaterial(t, now, testCase.fixture)
			running := startInProcessProductionServer(t, fixture.material, now)
			client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
			if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
				running.stop(t)
				t.Fatal(err)
			}
			_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
			terminal := events[len(events)-1]
			running.stop(t)
			if terminal.State != auditStateFailed || terminal.FailureClass != testCase.failureClass {
				t.Fatalf("fresh session terminal = %+v, want class %q", terminal, testCase.failureClass)
			}
		})
	}
}

func TestAuditTemporaryAuthorityFailureClassInJournal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{
		Scenario: "report", ExitCode: 9, WriteTemporaryWorkAuthority: true,
	})
	running := startInProcessProductionServer(t, fixture.material, now)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
		running.stop(t)
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
	terminal := events[len(events)-1]
	running.stop(t)
	if terminal.State != auditStateFailed || terminal.FailureClass != "artifact_scan_temporary_authority_home_work_root" {
		t.Fatalf("temporary authority terminal = %+v, want closed home/work_root class", terminal)
	}
	assertAuditExecutionRootsEmpty(t, fixture.entry)
}

func TestAuditFailureAndTimeoutScrubCredentialBearingTrees(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	for _, testCase := range []struct {
		name         string
		fixture      fakeAuditOMPFixture
		failureClass string
	}{
		{
			name: "nonzero credential artifacts",
			fixture: fakeAuditOMPFixture{
				Scenario: "report", ExitCode: 9, EmitCredential: true, WriteCredentialArtifacts: true,
			},
			failureClass: "direct_omp_or_capture_verification_failed",
		},
		{
			name: "secret-bearing timeout",
			fixture: fakeAuditOMPFixture{
				Scenario: "timeout_always", SessionUUID: "019f9a4a-a904-7000-b341-e07ecf0e3baf", WriteCredentialArtifacts: true,
			},
			failureClass: "direct_omp_or_capture_verification_failed",
		},
		{
			name:         "malformed timeout",
			fixture:      fakeAuditOMPFixture{Scenario: "malformed_timeout", ExitCode: 124},
			failureClass: "malformed_timeout_evidence",
		},
		{
			name:         "evidence rejection",
			fixture:      fakeAuditOMPFixture{Scenario: "evidence_rejection", Output: "safe"},
			failureClass: "evidence_verification_failed",
		},
		{
			name: "closed nonzero exit with authenticated fresh session",
			fixture: fakeAuditOMPFixture{
				Scenario: "report", ExitCode: 9, SessionUUID: "019f9a4a-a904-7000-b341-e07ecf0e3baf", FreshSessionMode: "authenticated",
			},
			failureClass: "direct_omp_exit_nonzero",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			fixture := newExecutingServerTestMaterial(t, now, testCase.fixture)
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
