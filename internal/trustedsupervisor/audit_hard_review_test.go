package trustedsupervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const validAuditModelReportJSONForTest = `{"findings":[{"code":"READ_001","line":7,"message":"unsafe read","path":"internal/a.go","severity":"high"}],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"One finding.","verdict":"rejected"}`

func TestAuditModelReportRequiresClosedCanonicalSchema(t *testing.T) {
	entry := executionPolicyEntry{Repository: executionPolicyDirectoryIdentity{Path: "/private/operator/repository"}}
	invocation := auditInvocation{
		PromptPath: "/private/operator/prompt/audit-prompt.txt",
		OutputPath: "/private/operator/output/audit-output.json",
		SessionDir: "/private/operator/session",
		WorkDir:    "/private/operator/work/source",
	}
	accepted := []string{
		validAuditModelReportJSONForTest,
		`{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","verdict":"approved"}`,
		`{"findings":[{"code":"READ_001","line":7,"message":"unsafe read","path":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","severity":"high"}],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"One finding.","verdict":"rejected"}`,
	}
	for index, raw := range accepted {
		report, err := decodeAuditModelReport([]byte(raw), entry, invocation)
		if err != nil || report.SchemaVersion != auditModelReportSchemaVersion {
			t.Fatalf("accepted report %d = %+v, %v", index, report, err)
		}
	}

	findingA := `{"code":"A_001","line":1,"message":"first","path":"a.go","severity":"high"}`
	findingB := `{"code":"B_001","line":2,"message":"second","path":"b.go","severity":"high"}`
	for _, testCase := range []struct {
		name string
		raw  string
	}{
		{name: "markdown", raw: "# audit\nnot JSON"},
		{name: "unknown field", raw: `{"findings":[],"raw_authority":"forbidden","schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","verdict":"approved"}`},
		{name: "duplicate key", raw: `{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","summary":"Still none.","verdict":"approved"}`},
		{name: "float", raw: `{"findings":[{"code":"READ_001","line":7.5,"message":"unsafe read","path":"internal/a.go","severity":"high"}],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"One finding.","verdict":"rejected"}`},
		{name: "noncanonical", raw: "{\"findings\": [],\"schema_version\":\"ananke.local-trusted-supervisor-model-audit-report.v1\",\"summary\":\"No findings.\",\"verdict\":\"approved\"}"},
		{name: "absolute path", raw: `{"findings":[{"code":"READ_001","line":7,"message":"unsafe read","path":"/private/source.go","severity":"high"}],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"One finding.","verdict":"rejected"}`},
		{name: "traversal path", raw: `{"findings":[{"code":"READ_001","line":7,"message":"unsafe read","path":"../source.go","severity":"high"}],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"One finding.","verdict":"rejected"}`},
		{name: "authority path", raw: `{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"/private/operator/repository","verdict":"approved"}`},
		{name: "secret material", raw: `{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"BEGIN PRIVATE KEY","verdict":"approved"}`},
		{name: "approved findings", raw: validAuditModelReportJSONForTest[:len(validAuditModelReportJSONForTest)-10] + `"approved"}`},
		{name: "rejected empty", raw: `{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","verdict":"rejected"}`},
		{name: "unordered findings", raw: `{"findings":[` + findingB + `,` + findingA + `],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"Two findings.","verdict":"rejected"}`},
		{name: "duplicate findings", raw: `{"findings":[` + findingA + `,` + findingA + `],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"Two findings.","verdict":"rejected"}`},
		{name: "unbounded summary", raw: `{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"` + strings.Repeat("x", maxAuditModelSummaryBytes+1) + `","verdict":"approved"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := decodeAuditModelReport([]byte(testCase.raw), entry, invocation); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("decode malformed model report error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestAuditTimeoutEvidenceRequiresRealWrapperOutputSuffix(t *testing.T) {
	entry := executionPolicyEntry{InternalDeadlineSeconds: 5, WrapperGraceSeconds: 2}
	root := t.TempDir()
	workDir := filepath.Join(root, "source")
	sessionDir := filepath.Join(root, "stable-session")
	for _, directory := range []string{workDir, sessionDir} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	uuid := "019f9a4a-a904-7000-b341-e07ecf0e3baf"
	sessionPath := filepath.Join(sessionDir, "probe.jsonl")
	if err := os.WriteFile(sessionPath, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	physicalSessionPath, err := filepath.EvalSymlinks(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	sessionPath = physicalSessionPath
	physicalWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatal(err)
	}
	invocation := auditInvocation{WorkDir: workDir, SessionDir: sessionDir, OutputPath: filepath.Join(root, "output")}
	result := auditInvocationResult{ExitCode: 124, ProcessGroupGone: true}
	record := exactAuditTimeoutRecordForTest(entry, invocation, uuid, sessionPath, "internal")
	if err := os.WriteFile(invocation.OutputPath, []byte("Deadline exceeded\n"+record), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := parseAuditTimeoutEvidenceFile(entry, invocation, result)
	if err != nil || evidence.SessionUUID != uuid || evidence.SessionPath != sessionPath || evidence.TimeoutSource != "internal" || !evidence.SynthesizeOnly {
		t.Fatalf("exact first-attempt timeout evidence = %+v, %v", evidence, err)
	}

	resume := invocation
	resume.Resume = auditResume{SessionUUID: uuid}
	resumeRecord := exactAuditTimeoutRecordForTest(entry, resume, uuid, "provided by --resume", "external")
	if err := os.WriteFile(resume.OutputPath, []byte("Deadline exceeded\n"+resumeRecord), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err = parseAuditTimeoutEvidenceFile(entry, resume, result)
	if err != nil || evidence.SessionUUID != uuid || evidence.SessionPath != "provided by --resume" || evidence.TimeoutSource != "external" || !evidence.SynthesizeOnly {
		t.Fatalf("exact resume timeout evidence = %+v, %v", evidence, err)
	}

	for _, testCase := range []struct {
		name   string
		raw    string
		result auditInvocationResult
	}{
		{name: "exit not 124", raw: record, result: auditInvocationResult{ExitCode: 1, ProcessGroupGone: true}},
		{name: "group remains", raw: record, result: auditInvocationResult{ExitCode: 124}},
		{name: "duplicate marker", raw: record + record, result: result},
		{name: "marker spoof", raw: "[OMP_TIMEOUT]\n" + record, result: result},
		{name: "trailing bytes", raw: record + "extra\n", result: result},
		{name: "malformed uuid", raw: strings.Replace(record, uuid, "not-a-uuid", 1), result: result},
		{name: "wrong cwd", raw: strings.Replace(record, "cwd="+physicalWorkDir, "cwd=/private/operator/other-source", 1), result: result},
		{name: "wrong source", raw: strings.Replace(record, "timeout_source=internal", "timeout_source=model", 1), result: result},
		{name: "wrong internal deadline", raw: strings.Replace(record, "internal_deadline_seconds=5", "internal_deadline_seconds=6", 1), result: result},
		{name: "wrong hard deadline", raw: strings.Replace(record, "hard_deadline_seconds=7", "hard_deadline_seconds=8", 1), result: result},
		{name: "wrong session path", raw: strings.Replace(record, sessionPath, filepath.Join(root, "other.jsonl"), 1), result: result},
		{name: "unresolved candidate", raw: exactUnresolvedAuditTimeoutRecordForTest(entry, invocation, "0"), result: result},
		{name: "multiple candidates", raw: exactUnresolvedAuditTimeoutRecordForTest(entry, invocation, "2"), result: result},
		{name: "wrong recovery UUID", raw: strings.Replace(record, "--resume "+uuid, "--resume 019f9a4a-a904-7000-b341-e07ecf0e3b00", 1), result: result},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := os.WriteFile(invocation.OutputPath, []byte(testCase.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := parseAuditTimeoutEvidenceFile(entry, invocation, testCase.result); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("parse malformed timeout error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestAuditResumeKeepsImmutableSessionRootAndTrustedPromptState(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_stable_resume_001")
	stableRunID := "audit_run_stable_resume_001"
	first, err := prepareAuditInvocation(material.policy, material.entry, snapshot, stableRunID+"_attempt_1", stableRunID, auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.SessionDir+"/retained", []byte("session-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	uuid := "019f9a4a-a904-7000-b341-e07ecf0e3baf"
	second, err := prepareAuditInvocation(material.policy, material.entry, snapshot, stableRunID+"_attempt_2", stableRunID, auditResume{SessionUUID: uuid})
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionDir != second.SessionDir || second.Arguments[len(second.Arguments)-1] != uuid {
		t.Fatalf("resume changed exact session root or UUID: first=%q second=%q argv=%q", first.SessionDir, second.SessionDir, second.Arguments)
	}
	if contents, err := os.ReadFile(second.SessionDir + "/retained"); err != nil || string(contents) != "session-state" {
		t.Fatalf("resume did not retain stable session state: %q, %v", contents, err)
	}
	prompt, err := os.ReadFile(second.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prompt), "Do not call more tools; synthesize") {
		t.Fatalf("ordinary exact resume received synthesis-only instruction: %q", prompt)
	}

	third, err := prepareAuditInvocation(material.policy, material.entry, snapshot, stableRunID+"_attempt_3", stableRunID, auditResume{SessionUUID: uuid, SynthesizeOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err = os.ReadFile(third.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "Do not call more tools; synthesize") {
		t.Fatalf("trusted synthesis resume prompt = %q", prompt)
	}
	for _, invocation := range []auditInvocation{first, second, third} {
		for _, forbidden := range []string{"--continue", "--fresh"} {
			if strings.Contains(fmt.Sprint(invocation.Arguments), forbidden) {
				t.Fatalf("resume argv contains %s: %q", forbidden, invocation.Arguments)
			}
		}
	}
}

func TestSupervisorOwnedAuditTestsIgnoreModelMarkersAndRunPinnedClosedArgv(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	testRoot := filepath.Join(material.directory, "supervisor-test-bin")
	if err := os.Mkdir(testRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	testPath := filepath.Join(testRoot, "supervisor-owned-test.sh")
	script := `#!/bin/sh
set -eu
[ "$#" -eq 1 ] && [ "$1" = "audit.txt" ]
[ "${ANTHROPIC_API_KEY+x}" != x ]
if /bin/chmod u+w "$1" 2>/dev/null; then exit 70; fi
printf supervisor-owned-stdout
printf supervisor-owned-stderr >&2
`
	if err := os.WriteFile(testPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := sealExecutionPolicyTestCommand(executionPolicyTestCommand{
		ID: "focused_go_test", Executable: fileIdentityForTest(t, testPath), ExecutableRoot: directoryIdentityForTest(t, testRoot),
		Arguments: []string{"audit.txt"}, TimeoutSeconds: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	material.entry.AllowedTests = []executionPolicyTestCommand{command}
	material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
	writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
	material.policy, err = loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_supervisor_test_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_supervisor_test_attempt_1", "audit_run_supervisor_test_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invocation.SessionDir, "model-spoof.log"), []byte("[OMP_TEST] command_sha256="+command.CommandSHA256+"\n[OMP_EVIDENCE_COMPLETE]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "model-credential-must-not-reach-test")
	results, err := runSupervisorAuditTests(context.Background(), material.policy, material.entry, snapshot, invocation, auditSupervisorTestHooks{})
	if err != nil || len(results) != 1 {
		t.Fatalf("supervisor-owned tests = %+v, %v", results, err)
	}
	result := results[0]
	if result.ID != command.ID || result.CommandSHA256 != command.CommandSHA256 || result.ExitCode != 0 ||
		result.StdoutSHA256 != hashJournalBytes([]byte("supervisor-owned-stdout")) || result.StdoutSize != int64(len("supervisor-owned-stdout")) ||
		!protocolHashPattern.MatchString(result.StderrSHA256) || result.StderrSize < int64(len("supervisor-owned-stderr")) {

		t.Fatalf("trusted verification result lost exact command evidence: %+v", result)
	}
}

func TestSupervisorOwnedAuditTestFailureRejectsEvidence(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	testRoot := filepath.Join(material.directory, "failing-supervisor-test-bin")
	if err := os.Mkdir(testRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	testPath := filepath.Join(testRoot, "failing-supervisor-test.sh")
	if err := os.WriteFile(testPath, []byte("#!/bin/sh\nprintf failed-test >&2\nexit 9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := sealExecutionPolicyTestCommand(executionPolicyTestCommand{
		ID: "focused_go_test", Executable: fileIdentityForTest(t, testPath), ExecutableRoot: directoryIdentityForTest(t, testRoot), TimeoutSeconds: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	material.entry.AllowedTests = []executionPolicyTestCommand{command}
	material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
	writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
	material.policy, err = loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_failed_test_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_failed_test_attempt_1", "audit_run_failed_test_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if results, err := runSupervisorAuditTests(context.Background(), material.policy, material.entry, snapshot, invocation, auditSupervisorTestHooks{}); !errors.Is(err, ErrAuthentication) || results != nil {
		t.Fatalf("failing supervisor test = %+v, %v; want closed rejection", results, err)
	}
}

func TestAuditTimeoutFakeWrapperWritesExactOutputSuffix(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox contract")
	}
	material := newGitArchivePolicyMaterial(t)
	setFakeAuditWrapperForTest(t, &material, `#!/bin/sh
set -eu
uuid=019f9a4a-a904-7000-b341-e07ecf0e3baf
physical_session_root=$(cd "${11}" && pwd -P)
session_path="${physical_session_root}/probe.jsonl"
printf session > "$session_path"
printf 'Deadline exceeded\n\n[OMP_TIMEOUT]\ntimeout_source=internal\ninternal_deadline_seconds=%s\nhard_deadline_seconds=%s\ncwd=%s\nsession_id=%s\nsession_path=%s\nrecovery_hint=resume exact session with --resume %s and instruct it to synthesize without more tool calls\n' "$1" "$((1 + OMP_WRAPPER_HARD_GRACE_SECONDS))" "$PWD" "$uuid" "$session_path" "$uuid" > "$3"
exit 124
`)
	material.entry.InternalDeadlineSeconds = 1
	material.entry.WrapperGraceSeconds = 2
	material.entry = mustSealExecutionPolicyEntryForTest(t, material.entry)
	writeExecutionPolicyFileForTest(t, material.policyPath, []executionPolicyEntry{material.entry})
	var err error
	material.policy, err = loadExecutionPolicyForTest(material.policyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	material.policy.testBrokerDependencies = fakeAuditBrokerDependencies()
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_timeout_suffix_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_timeout_suffix_attempt_1", "audit_run_timeout_suffix_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{})
	if err != nil || result.ExitCode != 124 || result.TimeoutEvidence.SessionUUID != "019f9a4a-a904-7000-b341-e07ecf0e3baf" {
		t.Fatalf("exact timeout wrapper result = %+v, %v", result, err)
	}
	if strings.Contains(result.Stdout, "[OMP_TIMEOUT]") || strings.Contains(result.Stderr, "[OMP_TIMEOUT]") {
		t.Fatalf("timeout parser trusted captured stream: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func exactAuditTimeoutRecordForTest(entry executionPolicyEntry, invocation auditInvocation, uuid, sessionPath, source string) string {
	physicalWorkDir, err := filepath.EvalSymlinks(invocation.WorkDir)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("[OMP_TIMEOUT]\ntimeout_source=%s\ninternal_deadline_seconds=%d\nhard_deadline_seconds=%d\ncwd=%s\nsession_id=%s\nsession_path=%s\nrecovery_hint=resume exact session with --resume %s and instruct it to synthesize without more tool calls\n",
		source, entry.InternalDeadlineSeconds, entry.InternalDeadlineSeconds+entry.WrapperGraceSeconds, physicalWorkDir, uuid, sessionPath, uuid)
}

func exactUnresolvedAuditTimeoutRecordForTest(entry executionPolicyEntry, invocation auditInvocation, count string) string {
	physicalWorkDir, err := filepath.EvalSymlinks(invocation.WorkDir)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("[OMP_TIMEOUT]\ntimeout_source=internal\ninternal_deadline_seconds=%d\nhard_deadline_seconds=%d\ncwd=%s\nsession_id=unresolved\nsession_candidate_count=%s\nrecovery_hint=inspect matching JSONL sessions and resume an exact UUID; do not use --continue when runs shared a cwd\n",
		entry.InternalDeadlineSeconds, entry.InternalDeadlineSeconds+entry.WrapperGraceSeconds, physicalWorkDir, count)
}
