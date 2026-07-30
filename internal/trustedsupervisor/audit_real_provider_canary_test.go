//go:build ananke_real_provider_canary

package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	realProviderCanaryOMPDeadline        = 1200
	realProviderCanaryWrapperGrace       = 5
	realProviderCanarySetupBudget        = 2 * time.Minute
	realProviderCanaryMaximumSetupBudget = 5 * time.Minute
	// Prelaunch covers durable executor queueing, snapshot materialization, and invocation preparation after Notify but before running.
	realProviderCanaryPrelaunchBudget        = 180 * time.Second
	realProviderCanaryMaximumPrelaunchBudget = 180 * time.Second
	// Terminal persistence is the only time outside the executor's OMP plus wrapper process hard deadline in the running stage.
	realProviderCanaryTerminalPersistenceMargin        = 60 * time.Second
	realProviderCanaryMaximumTerminalPersistenceMargin = 60 * time.Second
	realProviderCanaryRunningBudget                    = time.Duration(realProviderCanaryOMPDeadline+realProviderCanaryWrapperGrace)*time.Second +
		realProviderCanaryTerminalPersistenceMargin
	realProviderCanaryTimelineMaximumEvents = 32
	realProviderCanaryTimelineMaximumBytes  = 4096
)

func TestRealProviderCanaryBudgetInvariant(t *testing.T) {
	hardTimeout := time.Duration(realProviderCanaryOMPDeadline+realProviderCanaryWrapperGrace) * time.Second
	if realProviderCanarySetupBudget != 2*time.Minute || realProviderCanaryMaximumSetupBudget != 5*time.Minute || realProviderCanarySetupBudget > realProviderCanaryMaximumSetupBudget {
		t.Fatalf("real-provider canary setup budget = %s with maximum %s, want 2m0s within 5m0s", realProviderCanarySetupBudget, realProviderCanaryMaximumSetupBudget)
	}
	if realProviderCanaryPrelaunchBudget != 180*time.Second || realProviderCanaryMaximumPrelaunchBudget != 180*time.Second || realProviderCanaryPrelaunchBudget <= 0 || realProviderCanaryPrelaunchBudget > realProviderCanaryMaximumPrelaunchBudget {
		t.Fatalf("real-provider canary prelaunch budget = %s with maximum %s, want 3m0s within 3m0s", realProviderCanaryPrelaunchBudget, realProviderCanaryMaximumPrelaunchBudget)
	}
	if hardTimeout != 1205*time.Second {
		t.Fatalf("real-provider canary process hard timeout = %s, want 20m5s", hardTimeout)
	}
	if realProviderCanaryTerminalPersistenceMargin != 60*time.Second || realProviderCanaryMaximumTerminalPersistenceMargin != 60*time.Second || realProviderCanaryTerminalPersistenceMargin <= 0 || realProviderCanaryTerminalPersistenceMargin > realProviderCanaryMaximumTerminalPersistenceMargin {
		t.Fatalf("real-provider canary terminal persistence margin = %s with maximum %s, want 1m0s within 1m0s", realProviderCanaryTerminalPersistenceMargin, realProviderCanaryMaximumTerminalPersistenceMargin)
	}
	if realProviderCanaryRunningBudget != hardTimeout+realProviderCanaryTerminalPersistenceMargin || realProviderCanaryRunningBudget != 1265*time.Second {
		t.Fatalf("real-provider canary running budget = %s, want hard timeout %s plus terminal persistence %s = 21m5s", realProviderCanaryRunningBudget, hardTimeout, realProviderCanaryTerminalPersistenceMargin)
	}
}

func TestRealProviderCanaryStageWaitStateMachine(t *testing.T) {
	tests := []struct {
		name         string
		stage        realProviderCanaryWaitStage
		events       []auditExecutionEvent
		sawTimeout   bool
		wantDone     bool
		wantRunning  bool
		wantTerminal bool
		wantTimeout  bool
	}{
		{name: "prelaunch prepared", stage: realProviderCanaryWaitStagePrelaunch, events: []auditExecutionEvent{{Sequence: 1, State: auditStatePrepared, Attempt: 1}}},
		{name: "prelaunch running", stage: realProviderCanaryWaitStagePrelaunch, events: []auditExecutionEvent{{Sequence: 1, State: auditStatePrepared, Attempt: 1}, {Sequence: 2, State: auditStateRunning, Attempt: 1}}, wantDone: true, wantRunning: true},
		{name: "running remains active", stage: realProviderCanaryWaitStageRunning, events: []auditExecutionEvent{{Sequence: 1, State: auditStateRunning, Attempt: 1}}, wantRunning: true},
		{name: "running terminal", stage: realProviderCanaryWaitStageRunning, events: []auditExecutionEvent{{Sequence: 1, State: auditStateRunning, Attempt: 1}, {Sequence: 2, State: auditStateCompleted, Attempt: 1}}, wantDone: true, wantRunning: true, wantTerminal: true},
		{name: "prior timeout preserved", stage: realProviderCanaryWaitStageRunning, events: []auditExecutionEvent{{Sequence: 1, State: auditStateRunning, Attempt: 1}}, sawTimeout: true, wantRunning: true, wantTimeout: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			observation := inspectRealProviderCanaryStage(testCase.stage, testCase.events, testCase.sawTimeout)
			if observation.done != testCase.wantDone || observation.running != testCase.wantRunning || observation.terminal != testCase.wantTerminal || observation.sawTimeout != testCase.wantTimeout {
				t.Fatalf("stage observation = %+v, want done=%t running=%t terminal=%t saw_timeout=%t", observation, testCase.wantDone, testCase.wantRunning, testCase.wantTerminal, testCase.wantTimeout)
			}
		})
	}
}

func TestRealProviderCanaryTerminalBeforeRunning(t *testing.T) {
	observation := inspectRealProviderCanaryStage(realProviderCanaryWaitStagePrelaunch, []auditExecutionEvent{{Sequence: 1, State: auditStateCancelled, Attempt: 1}}, false)
	if !observation.done || observation.running || !observation.terminal {
		t.Fatalf("terminal-before-running observation = %+v, want terminal completion without running", observation)
	}
}

func TestRealProviderCanaryRunningAndTerminalInSameLoad(t *testing.T) {
	observation := inspectRealProviderCanaryStage(realProviderCanaryWaitStagePrelaunch, []auditExecutionEvent{
		{Sequence: 1, State: auditStatePrepared, Attempt: 1},
		{Sequence: 2, State: auditStateRunning, Attempt: 1},
		{Sequence: 3, State: auditStateFinalizing, Attempt: 1},
		{Sequence: 4, State: auditStateCompleted, Attempt: 1},
	}, false)
	if !observation.done || !observation.running || !observation.terminal {
		t.Fatalf("same-load observation = %+v, want running and terminal completion", observation)
	}
}

func TestRealProviderCanaryTimedOutWaitsForAttemptTerminal(t *testing.T) {
	timedOut := inspectRealProviderCanaryStage(realProviderCanaryWaitStageRunning, []auditExecutionEvent{
		{Sequence: 1, State: auditStateRunning, Attempt: 1},
		{Sequence: 2, State: auditStateTimedOut, Attempt: 1},
	}, false)
	if timedOut.done || !timedOut.running || timedOut.terminal || !timedOut.sawTimeout {
		t.Fatalf("timed-out observation = %+v, want nonterminal timeout", timedOut)
	}
	waiting := inspectRealProviderCanaryStage(realProviderCanaryWaitStageRunning, []auditExecutionEvent{
		{Sequence: 1, State: auditStateRunning, Attempt: 1},
		{Sequence: 2, State: auditStateTimedOut, Attempt: 1},
		{Sequence: 3, State: auditStateWaitingForHuman, Attempt: 1},
	}, false)
	if !waiting.done || !waiting.terminal || !waiting.sawTimeout {
		t.Fatalf("waiting-for-human observation = %+v, want terminal with preserved timeout", waiting)
	}
}

func TestRealProviderCanaryStageDeadlineClassification(t *testing.T) {
	loader := func(context.Context, string) (auditExecutionIntent, []auditExecutionEvent, error) {
		return auditExecutionIntent{}, nil, nil
	}
	for _, testCase := range []struct {
		name  string
		stage realProviderCanaryWaitStage
		want  string
	}{
		{name: "prelaunch", stage: realProviderCanaryWaitStagePrelaunch, want: "prelaunch_timeout"},
		{name: "running", stage: realProviderCanaryWaitStageRunning, want: "running_terminal_timeout"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			_, waitErr := waitForRealProviderCanaryStage(ctx, loader, "envelope", testCase.stage, false)
			if waitErr == nil {
				t.Fatal("expired stage wait succeeded")
			}
			if got := classifyRealProviderCanaryLifecycleFailure(testCase.stage, waitErr, ctx.Err(), nil); got != testCase.want {
				t.Fatalf("stage failure = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestRealProviderCanaryJournalAndCloseFailureClassification(t *testing.T) {
	journalErr := errors.New("fixture journal error")
	closeErr := errors.New("fixture close error")
	if got := classifyRealProviderCanaryLifecycleFailure(realProviderCanaryWaitStagePrelaunch, journalErr, nil, nil); got != "journal_wait_error" {
		t.Fatalf("journal failure = %q, want journal_wait_error", got)
	}
	if got := classifyRealProviderCanaryLifecycleFailure(realProviderCanaryWaitStageRunning, nil, nil, closeErr); got != "executor_close_error" {
		t.Fatalf("close failure = %q, want executor_close_error", got)
	}
	if got := classifyRealProviderCanaryLifecycleFailure(realProviderCanaryWaitStageRunning, ErrDeadline, context.DeadlineExceeded, closeErr); got != "running_terminal_timeout_and_executor_close_error" {
		t.Fatalf("combined failure = %q, want running_terminal_timeout_and_executor_close_error", got)
	}
}

func TestRealProviderCanarySafeTimelineRedactsSensitiveEventFields(t *testing.T) {
	const sensitive = "SENSITIVE_FIXTURE_VALUE"
	event := auditExecutionEvent{
		SchemaVersion: sensitive, EventID: sensitive, EventHash: sensitive, IntentHash: sensitive,
		Sequence: 7, State: auditStateFailed, Attempt: 1, CommandDescriptorHash: sensitive, PromptSHA256: sensitive,
		SessionRunID: sensitive, ResumeSessionUUID: sensitive, OccurredAt: "2026-07-27T00:00:00Z",
		PID: 987654321, PGID: 987654321, ProcessStartIdentity: sensitive, ProcessStartedAt: sensitive, ProcessFinishedAt: sensitive,
		OutputSHA256: sensitive, StdoutSHA256: sensitive, StderrSHA256: sensitive, SessionUUID: sensitive,
		EvidenceJSON: sensitive, EvidenceHash: sensitive, FinalizingEventHash: sensitive, FailureClass: "process_exit",
		WorkPath: sensitive, OutputPath: sensitive, SessionPath: sensitive, PromptPath: sensitive, TemporaryPath: sensitive,
		Authentication: auditExecutionEventAuthentication{Signature: sensitive, SignerRootID: sensitive},
	}
	timeline := safeRealProviderCanaryTimeline([]auditExecutionEvent{event})
	want := `[{"attempt":1,"failure_class":"process_exit","occurred_at":"2026-07-27T00:00:00Z","sequence":7,"state":"failed"}]`
	if string(timeline) != want {
		t.Fatalf("safe timeline = %s, want %s", timeline, want)
	}
	for _, forbidden := range []string{sensitive, "987654321", "pid", "path", "session", "evidence", "stdout", "stderr", "hash", "credential", "output"} {
		if bytes.Contains(bytes.ToLower(timeline), bytes.ToLower([]byte(forbidden))) {
			t.Fatalf("safe timeline contains forbidden fixture field %q: %s", forbidden, timeline)
		}
	}
}

func TestRealProviderCanarySafeTimelineClassifiesClosedWrapperExitCode(t *testing.T) {
	timeline := safeRealProviderCanaryTimeline([]auditExecutionEvent{{
		Sequence: 3, State: auditStateFailed, Attempt: 1, OccurredAt: "2026-07-27T00:00:00Z",
		FailureClass: "wrapper_exit_nonzero", ExitCode: 2,
	}})
	want := `[{"attempt":1,"failure_class":"wrapper_exit_nonzero_code_2","occurred_at":"2026-07-27T00:00:00Z","sequence":3,"state":"failed"}]`
	if string(timeline) != want {
		t.Fatalf("safe wrapper timeline = %s, want %s", timeline, want)
	}
	unknown := safeRealProviderCanaryTimeline([]auditExecutionEvent{{
		Sequence: 3, State: auditStateFailed, Attempt: 1, OccurredAt: "2026-07-27T00:00:00Z",
		FailureClass: "wrapper_exit_nonzero", ExitCode: 255,
	}})
	if !bytes.Contains(unknown, []byte(`"failure_class":"wrapper_exit_nonzero_code_other"`)) {
		t.Fatalf("unknown wrapper exit code was not closed: %s", unknown)
	}
	direct := withRealProviderCanaryWrapperDiagnostic([]auditExecutionEvent{{
		Sequence: 3, State: auditStateFailed, Attempt: 1, OccurredAt: "2026-07-27T00:00:00Z",
		FailureClass: "direct_omp_exit_nonzero", ExitCode: 1,
	}}, "sandbox_denied")
	directTimeline := safeRealProviderCanaryTimeline(direct)
	if !bytes.Contains(directTimeline, []byte(`"failure_class":"direct_omp_exit_nonzero_code_1_sandbox_denied"`)) {
		t.Fatalf("direct OMP diagnostic was not closed: %s", directTimeline)
	}
}

func TestRealProviderCanaryWrapperDiagnosticIsClosedAndDoesNotMutateEvents(t *testing.T) {
	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, "session.jsonl"), []byte(`{"error":"fetch failed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		result auditInvocationResult
		secret string
		want   string
	}{
		{name: "empty", result: auditInvocationResult{ExitCode: 1}, want: "empty_output"},
		{name: "exit zero empty", result: auditInvocationResult{ExitCode: 0}, want: "exit_zero_empty_output"},
		{name: "exit zero canonical", result: auditInvocationResult{ExitCode: 0, Stdout: `{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","verdict":"approved"}`}, want: "exit_zero_canonical_report"},
		{name: "exit zero noncanonical", result: auditInvocationResult{ExitCode: 0, Stdout: "Working..."}, want: "exit_zero_noncanonical_output"},
		{name: "authentication", result: auditInvocationResult{ExitCode: 1, Stderr: "API key rejected"}, want: "authentication"},
		{name: "sandbox", result: auditInvocationResult{ExitCode: 1, Stderr: "operation not permitted"}, want: "sandbox_denied"},
		{name: "native addon", result: auditInvocationResult{ExitCode: 1, Stderr: "Failed to load pi_natives native addon"}, want: "native_addon_load"},
		{name: "models configuration", result: auditInvocationResult{ExitCode: 1, Stderr: "models.yml validation failed"}, want: "models_configuration_load"},
		{name: "working", result: auditInvocationResult{ExitCode: 1, Stdout: "Working..."}, want: "working_only"},
		{name: "session provider connection", result: auditInvocationResult{ExitCode: 1, Stdout: "Working...", boundInvocation: auditInvocation{SessionDir: sessionDir}}, want: "provider_connection"},
		{name: "credential", result: auditInvocationResult{ExitCode: 1, Stderr: "fixture-secret"}, secret: "fixture-secret", want: "credential_disclosure"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classifyRealProviderCanaryWrapperExit(testCase.result, testCase.secret); got != testCase.want {
				t.Fatalf("diagnostic = %q, want %q", got, testCase.want)
			}
		})
	}
	events := []auditExecutionEvent{{Sequence: 3, State: auditStateFailed, Attempt: 1, OccurredAt: "2026-07-27T00:00:00Z", FailureClass: "wrapper_exit_nonzero", ExitCode: 1}}
	decorated := withRealProviderCanaryWrapperDiagnostic(events, "authentication")
	if events[0].FailureClass != "wrapper_exit_nonzero" || decorated[0].FailureClass != "wrapper_exit_nonzero_code_1_authentication" {
		t.Fatalf("diagnostic event mutation = original %q decorated %q", events[0].FailureClass, decorated[0].FailureClass)
	}
	captureEvents := []auditExecutionEvent{{Sequence: 3, State: auditStateFailed, Attempt: 1, OccurredAt: "2026-07-27T00:00:00Z", FailureClass: "direct_omp_or_capture_verification_failed"}}
	captureDecorated := withRealProviderCanaryWrapperDiagnostic(captureEvents, "exit_zero_canonical_report")
	if captureEvents[0].FailureClass != "direct_omp_or_capture_verification_failed" || captureDecorated[0].FailureClass != "direct_omp_or_capture_verification_failed_exit_zero_canonical_report" {
		t.Fatalf("capture diagnostic event mutation = original %q decorated %q", captureEvents[0].FailureClass, captureDecorated[0].FailureClass)
	}
	if !validRealProviderCanaryWrapperDiagnostic("authentication") || validRealProviderCanaryWrapperDiagnostic("caller_selected_value") {
		t.Fatal("wrapper diagnostic class set is not closed")
	}
	artifactEvents := []auditExecutionEvent{{
		Sequence: 3, State: auditStateFailed, Attempt: 1, OccurredAt: "2026-07-27T00:00:00Z",
		FailureClass: "artifact_scan_session_fresh_authentication",
	}}
	artifactDecorated := withRealProviderCanaryWrapperDiagnostic(artifactEvents, "working_only")
	if artifactDecorated[0].FailureClass != "artifact_scan_session_fresh_authentication" {
		t.Fatalf("scanner class was decorated with an unrelated diagnostic: %q", artifactDecorated[0].FailureClass)
	}
	artifactTimeline := safeRealProviderCanaryTimeline(artifactDecorated)
	if !bytes.Contains(artifactTimeline, []byte(`"failure_class":"artifact_scan_session_fresh_authentication"`)) {
		t.Fatalf("closed scanner class missing from safe timeline: %s", artifactTimeline)
	}
}

func TestRealProviderCanarySafeTimelineBounds(t *testing.T) {
	if realProviderCanaryTimelineMaximumEvents != 32 || realProviderCanaryTimelineMaximumBytes != 4096 {
		t.Fatalf("safe timeline bounds = %d events/%d bytes, want 32 events/4096 bytes", realProviderCanaryTimelineMaximumEvents, realProviderCanaryTimelineMaximumBytes)
	}
	events := make([]auditExecutionEvent, realProviderCanaryTimelineMaximumEvents+10)
	for index := range events {
		events[index] = auditExecutionEvent{Sequence: index + 1, State: auditStateRunning, Attempt: 1, OccurredAt: "2026-07-27T00:00:00Z"}
	}
	timeline := safeRealProviderCanaryTimeline(events)
	if count := bytes.Count(timeline, []byte(`"sequence"`)); count > realProviderCanaryTimelineMaximumEvents {
		t.Fatalf("safe timeline event count = %d, want at most %d", count, realProviderCanaryTimelineMaximumEvents)
	}
	if len(timeline) > realProviderCanaryTimelineMaximumBytes {
		t.Fatalf("safe timeline bytes = %d, want at most %d", len(timeline), realProviderCanaryTimelineMaximumBytes)
	}
	events[len(events)-1].FailureClass = strings.Repeat("x", realProviderCanaryTimelineMaximumBytes*2)
	if oversized := safeRealProviderCanaryTimeline(events[len(events)-1:]); len(oversized) > realProviderCanaryTimelineMaximumBytes {
		t.Fatalf("oversized safe timeline bytes = %d, want at most %d", len(oversized), realProviderCanaryTimelineMaximumBytes)
	}
}

func failRealProviderCanarySetup(t *testing.T, ctx context.Context, operation string) {
	t.Helper()
	failure := "setup_" + operation
	if ctx.Err() != nil {
		failure = "setup_timeout_" + operation
	}
	t.Fatalf("real-provider canary failed closed: %s", failure)
}

func requireRealProviderCanarySetupActive(t *testing.T, ctx context.Context, operation string) {
	t.Helper()
	if ctx.Err() != nil {
		failRealProviderCanarySetup(t, ctx, operation)
	}
}

type realProviderCanaryRepositoryState struct {
	commit      string
	tree        string
	branch      []byte
	refs        []byte
	status      []byte
	trackedHash string
	tracked     int
}

type realProviderCanaryTestSummary struct {
	ID       string `json:"id"`
	ExitCode int    `json:"exit_code"`
}

type realProviderCanarySummary struct {
	EvidenceHash string                          `json:"evidence_hash"`
	FindingCount int                             `json:"finding_count"`
	ReportHash   string                          `json:"report_hash"`
	Terminal     string                          `json:"terminal_state"`
	Tests        []realProviderCanaryTestSummary `json:"tests"`
	Verdict      string                          `json:"verdict"`
}

func TestAuditRealProviderCanary(t *testing.T) {
	if os.Getenv("ANANKE_REAL_PROVIDER_CANARY") != "1" {
		t.Skip("ANANKE_REAL_PROVIDER_CANARY=1 is required")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin real-provider canary only")
	}
	if value, exists := os.LookupEnv("SUDO_CODING_KEY"); !exists || value == "" {
		t.Fatal("SUDO_CODING_KEY is required")
	}

	repository := requireRealProviderCanaryPath(t, "ANANKE_REAL_CANARY_REPOSITORY")
	wrapper := requireRealProviderCanaryPath(t, "ANANKE_REAL_CANARY_WRAPPER")
	ompExecutable := requireRealProviderCanaryPath(t, "ANANKE_PINNED_OMP_FIXTURE")
	ompNativeAddon := requireRealProviderCanaryPath(t, "ANANKE_PINNED_OMP_NATIVE_FIXTURE")

	base := t.TempDir()
	_ = os.Chmod(base, 0o755)
	ephemeral := filepath.Join(base, "ephemeral")
	if err := os.Mkdir(ephemeral, 0o755); err != nil {
		t.Fatal("real-provider canary setup failed")
	}
	ephemeralPresent := true
	t.Cleanup(func() {
		if ephemeralPresent {
			_ = scrubAndRemoveAuditTree(ephemeral)
		}
	})

	roots, err := makeRealProviderCanaryRoots(ephemeral)
	if err != nil {
		t.Fatal("real-provider canary isolated-root setup failed")
	}
	setupContext, setupCancel := context.WithTimeout(context.Background(), realProviderCanarySetupBudget)
	defer setupCancel()
	before, err := captureRealProviderCanaryRepositoryState(setupContext, repository, roots.temporary)
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "repository_baseline")
	}
	defer before.zero()

	entry, policyPath, err := buildRealProviderCanaryPolicy(setupContext, ephemeral, roots, repository, wrapper, ompExecutable, ompNativeAddon)
	if err != nil {
		t.Logf("real-provider canary policy construction error_class=%v", err)
		failRealProviderCanarySetup(t, setupContext, "policy_construction")
	}
	resolutionClass, err := preflightAuditProviderResolution(setupContext, entry.HermesProvider, entry.ProviderEndpoint, net.DefaultResolver.LookupIPAddr)
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "provider_resolution_preflight")
	}
	t.Logf("provider_resolution_transport=%s", resolutionClass)
	policy, err := loadExecutionPolicyWithNamespaceAuthority(policyPath, uint32(os.Getuid()), testAuditNamespaceAuthorityOptions())
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "policy_admission")
	}
	var providerLookups, providerDialAttempts, providerDialSuccesses atomic.Int64
	dialer := &net.Dialer{Timeout: auditBrokerConnectTimeout, KeepAlive: -1}
	policy.testBrokerDependencies = auditBrokerDependencies{
		LookupIPAddr: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			providerLookups.Add(1)
			return net.DefaultResolver.LookupIPAddr(ctx, host)
		},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			providerDialAttempts.Add(1)
			connection, dialErr := dialer.DialContext(ctx, network, address)
			if dialErr == nil {
				providerDialSuccesses.Add(1)
			}
			return connection, dialErr
		},
	}
	policyOpen := true
	defer func() {
		if policyOpen {
			_ = policy.Close()
		}
	}()
	if err := admitAtomicOMPRuntimeAuthority(policy, realProviderCanaryRuntimeVerifier()); err != nil {
		failRealProviderCanarySetup(t, setupContext, "runtime_authority")
	}
	requireRealProviderCanarySetupActive(t, setupContext, "runtime_authority")

	now := time.Now().UTC()
	fixture := newProcessSignedAuthorizationFixture(t, now, "real_provider_canary")
	bundlePath, keyPath, err := writeRealProviderCanarySigningMaterial(ephemeral, fixture)
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "signing_material")
	}
	signing, err := loadServerSigningMaterial(bundlePath, keyPath, uint32(os.Getuid()), now)
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "signing_authority")
	}
	requireRealProviderCanarySetupActive(t, setupContext, "signing_authority")
	signingOpen := true
	defer func() {
		if signingOpen {
			signing.Close()
		}
	}()

	journalPath := filepath.Join(ephemeral, "audit-journal.sqlite")
	policy.setProtectedPaths(bundlePath, keyPath, policyPath, journalPath, journalPath+"-wal", journalPath+"-shm")
	journal, err := openServerJournal(journalPath)
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "journal_construction")
	}
	journalOpen := true
	defer func() {
		if journalOpen {
			_ = journal.Close()
		}
	}()
	if err := journal.bindAuditAuthority(policy, signing); err != nil {
		failRealProviderCanarySetup(t, setupContext, "journal_authority")
	}
	requireRealProviderCanarySetupActive(t, setupContext, "journal_authority")
	executor, err := newAuditExecutor(journal, policy)
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "executor_construction")
	}
	requireRealProviderCanarySetupActive(t, setupContext, "executor_construction")
	wrapperDiagnostic := &realProviderCanaryWrapperDiagnostic{}
	logDiagnostic := &realProviderCanaryLogDiagnostic{}
	var canaryExitCode atomic.Int32
	var canaryStderrLen atomic.Int32
	var canaryStderrSafe bytes.Buffer
	var canaryGatewayRejDiag string
	secret := os.Getenv("SUDO_CODING_KEY")
	var canaryBeforeArtifactScanCalled atomic.Bool
	var canaryEvidenceDiag atomic.Value
	executor.hooks.invocation.BeforeArtifactScan = func(invocation auditInvocation) {
		canaryBeforeArtifactScanCalled.Store(true)
		logDiagnostic.set(classifyRealProviderCanaryOMPLogs(auditInvocationResult{boundInvocation: invocation}, secret))
		diag := collectCanaryEvidenceDiagnostic(invocation, entry)
		canaryEvidenceDiag.Store(diag)
	}
	executor.hooks.afterInvocation = func(result auditInvocationResult) {
		canaryExitCode.Store(int32(result.ExitCode))
		canaryGatewayRejDiag = result.GatewayRejectionDiag
		canaryStderrLen.Store(int32(len(result.Stderr)))
		if len(result.Stderr) > 0 && len(result.Stderr) <= 8192 {
			sample := result.Stderr
			if secret != "" {
				sample = strings.ReplaceAll(sample, secret, "[REDACTED]")
			}
			if len(sample) > 512 {
				sample = sample[:512]
			}
			canaryStderrSafe.WriteString(sample)
		}
		wrapperDiagnostic.set(classifyRealProviderCanaryWrapperExit(result, secret))
	}
	executorOpen := true
	defer func() {
		if executorOpen {
			_ = executor.Close()
		}
	}()

	intent, err := newRealProviderCanaryIntent(entry, now)
	if err != nil {
		failRealProviderCanarySetup(t, setupContext, "intent_construction")
	}
	if err := journal.storeAuditIntent(setupContext, intent); err != nil {
		failRealProviderCanarySetup(t, setupContext, "intent_persistence")
	}
	setupCancel()

	executor.Notify(intent.EnvelopeHash)
	waitStage := realProviderCanaryWaitStagePrelaunch
	prelaunchContext, prelaunchCancel := context.WithTimeout(context.Background(), realProviderCanaryPrelaunchBudget)
	waitResult, waitErr := waitForRealProviderCanaryStage(prelaunchContext, journal.loadAuditExecution, intent.EnvelopeHash, waitStage, false)
	waitContextErr := prelaunchContext.Err()
	prelaunchCancel()
	if waitErr == nil && !waitResult.terminal {
		waitStage = realProviderCanaryWaitStageRunning
		runningContext, runningCancel := context.WithTimeout(context.Background(), realProviderCanaryRunningBudget)
		waitResult, waitErr = waitForRealProviderCanaryStage(runningContext, journal.loadAuditExecution, intent.EnvelopeHash, waitStage, waitResult.sawTimeout)
		waitContextErr = runningContext.Err()
		runningCancel()
	}
	events := waitResult.events
	sawTimeout := waitResult.sawTimeout
	failureTimeline := safeRealProviderCanaryTimeline(withRealProviderCanaryWrapperDiagnostic(events, wrapperDiagnostic.get()))
	closeErr := executor.Close()
	executorOpen = false

	postContext, postCancel := context.WithTimeout(context.Background(), 20*time.Second)
	after, stateErr := captureRealProviderCanaryRepositoryState(postContext, repository, roots.temporary)
	postCancel()
	if stateErr == nil {
		defer after.zero()
	}
	unchanged := stateErr == nil && before.equal(after)

	var summary realProviderCanarySummary
	failure := ""
	lifecycleFailure := classifyRealProviderCanaryLifecycleFailure(waitStage, waitErr, waitContextErr, closeErr)
	switch {
	case lifecycleFailure != "":
		failure = lifecycleFailure
	case !unchanged:
		failure = "repository_immutability"
	case sawTimeout:
		failure = "nonterminal_timeout"
	case len(events) == 0 || events[len(events)-1].State != auditStateCompleted:
		failure = "terminal_state"
	default:
		terminal := events[len(events)-1]
		report, decodeErr := decodeAuditEvidenceReport(intent, terminal)
		if decodeErr != nil || validateAuditEvidencePolicy(report, entry) != nil {
			failure = "typed_evidence"
		} else if report.Attempt != 1 || report.AttemptCap != 1 || report.ResumeSessionUUID != "" || report.SynthesizeOnly || report.SessionUUID != "" {
			failure = "attempt_or_resume"
		} else if len(report.TestsRun) != 1 || report.TestsRun[0].ID != entry.AllowedTests[0].ID || report.TestsRun[0].ExitCode != 0 {
			failure = "supervisor_test"
		} else if verifyAuditFinalizingRootsAbsent(policy.namespaceAuthority, intent, terminal, entry) != nil {
			failure = "cleanup_finalization"
		} else if secret, exists := os.LookupEnv("SUDO_CODING_KEY"); !exists || secret == "" || strings.Contains(terminal.EvidenceJSON, secret) {
			failure = "credential_disclosure"
		} else {
			tests := make([]realProviderCanaryTestSummary, len(report.TestsRun))
			for index, testResult := range report.TestsRun {
				tests[index] = realProviderCanaryTestSummary{ID: testResult.ID, ExitCode: testResult.ExitCode}
			}
			summary = realProviderCanarySummary{
				EvidenceHash: terminal.EvidenceHash,
				FindingCount: len(report.ModelReport.Findings),
				ReportHash:   report.ModelReportSHA256,
				Terminal:     terminal.State,
				Tests:        tests,
				Verdict:      report.ModelReport.Verdict,
			}
		}
	}

	if err := journal.Close(); err != nil && failure == "" {
		failure = "journal_close"
	}
	journalOpen = false
	signing.Close()
	signingOpen = false
	if err := policy.Close(); err != nil && failure == "" {
		failure = "policy_close"
	}
	policyOpen = false
	if failure != "" {
		t.Logf("ephemeral_retained_for_inspection=%s", ephemeral)
	} else if err := scrubAndRemoveAuditTree(ephemeral); err != nil && failure == "" {
		failure = "ephemeral_scrub"
	} else if err == nil {
		ephemeralPresent = false
	}

	if failure != "" {
		t.Logf("provider_transport lookups=%d dial_attempts=%d dial_successes=%d", providerLookups.Load(), providerDialAttempts.Load(), providerDialSuccesses.Load())
		t.Logf("omp_exit_code=%d", canaryExitCode.Load())
		t.Logf("omp_stderr_len=%d", canaryStderrLen.Load())
		if canaryStderrSafe.Len() > 0 {
			t.Logf("omp_stderr_sample=%q", canaryStderrSafe.String())
		}
		t.Logf("before_artifact_scan_called=%v", canaryBeforeArtifactScanCalled.Load())
		if diag, ok := canaryEvidenceDiag.Load().(string); ok && diag != "" {
			t.Logf("evidence_diag=%s", diag)
		}
		if canaryGatewayRejDiag != "" {
			t.Logf("gateway_rejection_diag=%s", canaryGatewayRejDiag)
		}
		t.Logf("wrapper_diagnostic=%s", wrapperDiagnostic.get())
		t.Logf("omp_log_diagnostic=%s", logDiagnostic.get())
		t.Logf("real-provider canary safe timeline=%s", failureTimeline)
		t.Fatalf("real-provider canary failed closed: %s", failure)
	}
	encoded, err := marshalCanonical(summary)
	if err != nil || len(encoded) > 4096 || bytes.Contains(encoded, []byte(repository)) || bytes.Contains(encoded, []byte(wrapper)) || bytes.Contains(encoded, []byte(ompExecutable)) || bytes.Contains(encoded, []byte(ompNativeAddon)) {
		t.Fatal("real-provider canary sanitized summary failed")
	}
	if secret, exists := os.LookupEnv("SUDO_CODING_KEY"); !exists || secret == "" || bytes.Contains(encoded, []byte(secret)) {
		t.Fatal("real-provider canary sanitized summary rejected a credential leak")
	}
	if err := os.WriteFile(filepath.Join(base, "sanitized-summary.json"), encoded, 0o600); err != nil {
		t.Fatal("real-provider canary sanitized summary write failed")
	}
	t.Logf("verdict=%s finding_count=%d evidence_hash=%s report_hash=%s tests=%s terminal_state=%s", summary.Verdict, summary.FindingCount, summary.EvidenceHash, summary.ReportHash, formatRealProviderCanaryTests(summary.Tests), summary.Terminal)
}

type realProviderCanaryRoots struct {
	prompt    string
	output    string
	session   string
	work      string
	temporary string
}

func requireRealProviderCanaryPath(t *testing.T, name string) string {
	t.Helper()
	value, exists := os.LookupEnv(name)
	if !exists || value == "" {
		t.Fatalf("%s is required", name)
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.IndexByte(value, 0) >= 0 {
		t.Fatalf("%s must be an absolute clean path", name)
	}
	return value
}

func makeRealProviderCanaryRoots(parent string) (realProviderCanaryRoots, error) {
	roots := realProviderCanaryRoots{
		prompt: filepath.Join(parent, "prompt"), output: filepath.Join(parent, "output"),
		session: filepath.Join(parent, "session"), work: filepath.Join(parent, "work"), temporary: filepath.Join(parent, "tmp"),
	}
	for _, root := range []string{roots.prompt, roots.output, roots.session, roots.work, roots.temporary} {
		if err := os.Mkdir(root, 0o755); err != nil {
			return realProviderCanaryRoots{}, errors.New("isolated root")
		}
	}
	return roots, nil
}

func buildRealProviderCanaryPolicy(
	ctx context.Context,
	ephemeral string,
	roots realProviderCanaryRoots,
	repository string,
	wrapper string,
	ompExecutable string,
	ompNativeAddon string,
) (executionPolicyEntry, string, error) {
	repositoryState, err := captureRealProviderCanaryGitIdentity(ctx, repository, roots.temporary)
	if err != nil {
		return executionPolicyEntry{}, "", err
	}
	repositoryIdentity, err := realProviderCanaryDirectoryIdentity(repository)
	if err != nil {
		return executionPolicyEntry{}, "", err
	}
	gitIdentity, err := realProviderCanaryFileIdentity(auditGitExecutable, maxExecutionPolicyBytes*8)
	if err != nil {
		return executionPolicyEntry{}, "", errors.New("git executable identity")
	}
	wrapperIdentity, err := realProviderCanaryFileIdentity(wrapper, maxExecutionPolicyBytes*8)
	if err != nil {
		return executionPolicyEntry{}, "", errors.New("wrapper identity")
	}
	ompIdentity, ompRootIdentity, nativeIdentity, err := materializeRealProviderCanaryRuntime(ephemeral, ompExecutable, ompNativeAddon)
	if err != nil {
		return executionPolicyEntry{}, "", err
	}
	testExecutable, err := realProviderCanaryFileIdentity("/bin/test", maxExecutionPolicyBytes*8)
	if err != nil {
		return executionPolicyEntry{}, "", errors.New("test executable identity")
	}
	testRoot, err := realProviderCanaryDirectoryIdentity(filepath.Dir(testExecutable.Path))
	if err != nil {
		return executionPolicyEntry{}, "", err
	}
	testCommand, err := sealExecutionPolicyTestCommand(executionPolicyTestCommand{
		ID: "repository_go_mod_readable", Executable: testExecutable, ExecutableRoot: testRoot,
		Arguments: []string{"-r", "go.mod"}, TimeoutSeconds: 30,
	})
	if err != nil {
		return executionPolicyEntry{}, "", errors.New("test command")
	}
	readRoots, err := realProviderCanaryDirectoryIdentities("/bin", "/usr/bin", "/usr/lib", "/usr/share", "/System/Library", "/Library/Apple", "/private/var/db/timezone")
	if err != nil {
		return executionPolicyEntry{}, "", err
	}
	executableRoots, err := realProviderCanaryDirectoryIdentities("/bin", "/usr/bin")
	if err != nil {
		return executionPolicyEntry{}, "", err
	}

	entry := executionPolicyEntry{
		SchemaVersion:  executionPolicyEntrySchemaVersion,
		LaunchSpecHash: testHash("real-provider-canary-launch"), TaskID: "audit_task_real_provider_canary_001",
		RepositoryIdentity: "local.ananke/self/read_only_canary", RepositoryIdentityHash: repositoryIdentityHash("local.ananke/self/read_only_canary"),
		Repository: repositoryIdentity, GitExecutable: gitIdentity,
		GitCommit: repositoryState.commit, GitCommitObjectSHA256: repositoryState.commitObjectHash,
		GitTree: repositoryState.tree, SourceArchiveSHA256: repositoryState.archiveHash,
		PromptTemplateID: readOnlyAuditPromptTemplateID, PromptTemplateHash: readOnlyAuditPromptTemplateHash(),
		RouteMappingHash: testHash("real-provider-canary-custom-sudo-route"),
		Wrapper:          wrapperIdentity, OMPExecutable: ompIdentity, OMPExecutableRoot: ompRootIdentity,
		OMPVersion: supportedOMPVersion, OMPNativeAddon: nativeIdentity,
		HermesProvider: "custom:sudo", HermesModel: "gpt-5.6-sol",
		ProviderEndpoint: executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		TaskTier:         "normal", InternalDeadlineSeconds: realProviderCanaryOMPDeadline,
		WrapperGraceSeconds: realProviderCanaryWrapperGrace, AttemptCap: 1,
		AllowedTests: []executionPolicyTestCommand{testCommand}, RuntimeReadRoots: readRoots, ExecutableRoots: executableRoots,
		CredentialEnvironmentNames: []string{"SUDO_CODING_KEY"},
		PromptRoot:                 roots.prompt, OutputRoot: roots.output, SessionRoot: roots.session, WorkRoot: roots.work, TemporaryRoot: roots.temporary,
	}
	authority, err := buildRealProviderCanaryRuntimeAuthority(ephemeral, entry)
	if err != nil {
		return executionPolicyEntry{}, "", err
	}
	entry.OMPRuntimeAuthority = authority
	entry, err = sealExecutionPolicyEntry(entry)
	if err != nil {
		return executionPolicyEntry{}, "", errors.New("policy seal")
	}
	encoded, err := marshalCanonical(executionPolicyFile{SchemaVersion: executionPolicySchemaVersion, Executions: []executionPolicyEntry{entry}})
	if err != nil {
		return executionPolicyEntry{}, "", errors.New("policy encode")
	}
	policyPath := filepath.Join(ephemeral, "execution-policy.json")
	if err := os.WriteFile(policyPath, encoded, 0o600); err != nil {
		return executionPolicyEntry{}, "", errors.New("policy write")
	}
	return entry, policyPath, nil
}

type realProviderCanaryGitIdentity struct {
	commit           string
	tree             string
	commitObjectHash string
	archiveHash      string
}

func captureRealProviderCanaryGitIdentity(ctx context.Context, repository, temporaryRoot string) (realProviderCanaryGitIdentity, error) {
	commitBytes, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, maxGitMetadataBytes, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return realProviderCanaryGitIdentity{}, err
	}
	commit := string(bytes.TrimSpace(commitBytes))
	zeroBytes(commitBytes)
	treeBytes, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, maxGitMetadataBytes, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return realProviderCanaryGitIdentity{}, err
	}
	tree := string(bytes.TrimSpace(treeBytes))
	zeroBytes(treeBytes)
	commitObject, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, maxGitMetadataBytes, "cat-file", "commit", commit)
	if err != nil {
		return realProviderCanaryGitIdentity{}, err
	}
	defer zeroBytes(commitObject)
	archive, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, maxGitArchiveBytes, "archive", "--format=tar", "--mtime=1970-01-01T00:00:00Z", tree)
	if err != nil {
		return realProviderCanaryGitIdentity{}, err
	}
	defer zeroBytes(archive)
	secondArchive, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, maxGitArchiveBytes, "archive", "--format=tar", "--mtime=1970-01-01T00:00:00Z", tree)
	if err != nil {
		return realProviderCanaryGitIdentity{}, err
	}
	defer zeroBytes(secondArchive)
	if !gitObjectIDPattern.MatchString(commit) || !gitObjectIDPattern.MatchString(tree) || !bytes.Equal(archive, secondArchive) {
		return realProviderCanaryGitIdentity{}, errors.New("deterministic git identity")
	}
	return realProviderCanaryGitIdentity{
		commit: commit, tree: tree, commitObjectHash: hashJournalBytes(commitObject), archiveHash: hashJournalBytes(archive),
	}, nil
}

func buildRealProviderCanaryRuntimeAuthority(ephemeral string, entry executionPolicyEntry) (executionPolicyOMPRuntimeAuthority, error) {
	wrapper, err := freezeAuditWrapper(entry.Wrapper)
	if err != nil {
		return executionPolicyOMPRuntimeAuthority{}, errors.New("wrapper compatibility oracle freeze")
	}
	defer zeroBytes(wrapper)
	executableAncestors, err := realProviderCanaryAtomicAncestors(entry.OMPExecutable.Path)
	if err != nil {
		return executionPolicyOMPRuntimeAuthority{}, err
	}
	nativeAncestors, err := realProviderCanaryAtomicAncestors(entry.OMPNativeAddon.Path)
	if err != nil {
		return executionPolicyOMPRuntimeAuthority{}, err
	}
	nativeDataRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(entry.OMPNativeAddon.Path))))
	authority := executionPolicyOMPRuntimeAuthority{
		SchemaVersion: atomicOMPRuntimeAuthoritySchemaVersion, AuthorityPolicyVersion: atomicOMPRuntimeAuthorityPolicyVersion,
		TrustedOwnerUID: 0, ExecutableAncestors: executableAncestors, NativeAddonAncestors: nativeAncestors,
		NativeDataRoot:                   nativeDataRoot,
		DeniedNativeFallbackRoots:        []string{filepath.Join(filepath.Dir(nativeDataRoot), "denied-native-home"), filepath.Join(filepath.Dir(nativeDataRoot), "denied-native-path")},
		LauncherMode:                     atomicOMPLauncherModeDirectPinned,
		OMPArgvPolicy:                    atomicOMPArgvPolicyExactSudoRoute,
		SandboxTargetPolicy:              atomicOMPSandboxTargetPolicyExactPinned,
		OutputTransport:                  atomicOMPOutputTransportSupervisorStdout,
		TimeoutOwner:                     atomicOMPTimeoutOwnerSupervisor,
		WrapperCompatibilityOracleSHA256: hashJournalBytes(wrapper),
		ArtifactFDPolicy:                 atomicOMPRuntimeArtifactFDPolicyParentRetainedCLOEXEC,
		ProcessGroupPolicy:               atomicOMPProcessGroupPolicySingleGroup,
	}
	sealed, err := sealExecutionPolicyOMPRuntimeAuthority(authority, entry.OMPExecutable, entry.OMPNativeAddon)
	if err != nil {
		return executionPolicyOMPRuntimeAuthority{}, errors.New("runtime authority seal")
	}
	return sealed, nil
}

func materializeRealProviderCanaryRuntime(ephemeral, executableFixture, nativeFixture string) (executionPolicyFileIdentity, executionPolicyDirectoryIdentity, executionPolicyFileIdentity, error) {
	sourceExecutable, err := realProviderCanaryFileIdentity(executableFixture, maxAuditOMPExecutableBytes)
	if err != nil || validateOMPExecutableIdentity(sourceExecutable) != nil {
		return executionPolicyFileIdentity{}, executionPolicyDirectoryIdentity{}, executionPolicyFileIdentity{}, errors.New("OMP fixture")
	}
	sourceNative, err := realProviderCanaryFileIdentity(nativeFixture, maxAuditOMPNativeAddonBytes)
	if err != nil || validateOMPNativeAddonIdentity(supportedOMPVersion, sourceNative, sourceNative.OwnerUID) != nil {
		return executionPolicyFileIdentity{}, executionPolicyDirectoryIdentity{}, executionPolicyFileIdentity{}, errors.New("native fixture")
	}
	physicalEphemeral, err := filepath.EvalSymlinks(ephemeral)
	if err != nil || !filepath.IsAbs(physicalEphemeral) || filepath.Clean(physicalEphemeral) != physicalEphemeral {
		return executionPolicyFileIdentity{}, executionPolicyDirectoryIdentity{}, executionPolicyFileIdentity{}, errors.New("runtime root")
	}
	executablePath := filepath.Join(physicalEphemeral, "runtime", "bin", "omp")
	nativePath := filepath.Join(physicalEphemeral, "runtime", "data", "omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
	for _, directory := range []string{filepath.Dir(executablePath), filepath.Dir(nativePath)} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return executionPolicyFileIdentity{}, executionPolicyDirectoryIdentity{}, executionPolicyFileIdentity{}, errors.New("runtime directory")
		}
	}
	executable, err := copyRealProviderCanaryRuntimeArtifact(sourceExecutable, executablePath, 0o555, maxAuditOMPExecutableBytes)
	if err != nil {
		return executionPolicyFileIdentity{}, executionPolicyDirectoryIdentity{}, executionPolicyFileIdentity{}, err
	}
	native, err := copyRealProviderCanaryRuntimeArtifact(sourceNative, nativePath, 0o444, maxAuditOMPNativeAddonBytes)
	if err != nil {
		return executionPolicyFileIdentity{}, executionPolicyDirectoryIdentity{}, executionPolicyFileIdentity{}, err
	}
	root, err := realProviderCanaryDirectoryIdentity(filepath.Dir(executablePath))
	if err != nil {
		return executionPolicyFileIdentity{}, executionPolicyDirectoryIdentity{}, executionPolicyFileIdentity{}, err
	}
	return executable, root, native, nil
}

func copyRealProviderCanaryRuntimeArtifact(source executionPolicyFileIdentity, destination string, mode os.FileMode, limit int64) (executionPolicyFileIdentity, error) {
	contents, current, err := readPinnedRegularFile(source.Path, source.OwnerUID, false, limit)
	if err != nil || current != source {
		zeroBytes(contents)
		return executionPolicyFileIdentity{}, errors.New("runtime fixture copy")
	}
	defer zeroBytes(contents)
	if err := writePrivateAuditFile(destination, contents, mode); err != nil || os.Chmod(destination, mode) != nil {
		return executionPolicyFileIdentity{}, errors.New("runtime fixture materialization")
	}
	identity, err := realProviderCanaryFileIdentity(destination, limit)
	if err != nil || identity.SHA256 != source.SHA256 || identity.Size != source.Size || identity.Mode != uint32(mode.Perm()) {
		return executionPolicyFileIdentity{}, errors.New("runtime fixture binding")
	}
	return identity, nil
}

func realProviderCanaryRuntimeVerifier() atomicRuntimeAuthorityVerifier {
	return atomicRuntimeAuthorityVerifierFunc(func(entry executionPolicyEntry, wrapper []byte) (*atomicRuntimeAuthorityLease, error) {
		if validateExecutionPolicyAtomicRuntimeAuthority(entry) != nil || entry.OMPExecutable.OwnerUID != uint32(os.Getuid()) || entry.OMPNativeAddon.OwnerUID != uint32(os.Getuid()) {
			return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding)
		}
		bridged := cloneExecutionPolicyEntry(entry)
		bridged.OMPExecutable.OwnerUID = 0
		bridged.OMPExecutableRoot.OwnerUID = 0
		bridged.OMPNativeAddon.OwnerUID = 0
		authority := cloneExecutionPolicyOMPRuntimeAuthority(bridged.OMPRuntimeAuthority)
		for index := range authority.ExecutableAncestors {
			authority.ExecutableAncestors[index].OwnerUID = 0
		}
		for index := range authority.NativeAddonAncestors {
			authority.NativeAddonAncestors[index].OwnerUID = 0
		}
		var err error
		bridged.OMPRuntimeAuthority, err = sealExecutionPolicyOMPRuntimeAuthority(authority, bridged.OMPExecutable, bridged.OMPNativeAddon)
		if err != nil {
			return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding)
		}
		dependencies := systemAtomicRuntimeAuthorityDependencies()
		fstat := dependencies.Fstat
		dependencies.Fstat = func(descriptor int, status *unix.Stat_t) error {
			if err := fstat(descriptor, status); err != nil {
				return err
			}
			status.Uid = 0
			return nil
		}
		dependencies.Faccessat = func(_ int, _ string, mode uint32, flags int) error {
			if mode != unix.W_OK || flags != unix.AT_EACCESS|unix.AT_SYMLINK_NOFOLLOW {
				return unix.EINVAL
			}
			return unix.EACCES
		}
		return verifyAtomicOMPRuntimeAuthority(bridged, wrapper, dependencies)
	})
}

func realProviderCanaryAtomicAncestors(artifact string) ([]executionPolicyDirectoryIdentity, error) {
	paths, ok := atomicRuntimeAncestorPaths(artifact)
	if !ok {
		return nil, errors.New("runtime ancestor path")
	}
	identities := make([]executionPolicyDirectoryIdentity, 0, len(paths))
	for _, path := range paths {
		identity, err := realProviderCanaryDirectoryIdentity(path)
		if err != nil {
			return nil, err
		}
		identities = append(identities, identity)
	}
	return identities, nil
}

func realProviderCanaryFileIdentity(path string, limit int64) (executionPolicyFileIdentity, error) {
	information, err := os.Lstat(path)
	status, ok := informationSyscallStat(information)
	if err != nil || !ok || information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() {
		return executionPolicyFileIdentity{}, errors.New("file identity")
	}
	contents, identity, err := readPinnedRegularFile(path, status.Uid, false, limit)
	if err != nil {
		return executionPolicyFileIdentity{}, errors.New("file identity")
	}
	zeroBytes(contents)
	return identity, nil
}

func realProviderCanaryDirectoryIdentity(path string) (executionPolicyDirectoryIdentity, error) {
	information, err := os.Lstat(path)
	status, ok := informationSyscallStat(information)
	if err != nil || !ok || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() {
		return executionPolicyDirectoryIdentity{}, errors.New("directory identity")
	}
	identity, err := inspectPinnedDirectory(path, status.Uid, false)
	if err != nil {
		return executionPolicyDirectoryIdentity{}, errors.New("directory identity")
	}
	return identity, nil
}

func realProviderCanaryDirectoryIdentities(paths ...string) ([]executionPolicyDirectoryIdentity, error) {
	identities := make([]executionPolicyDirectoryIdentity, 0, len(paths))
	for _, path := range paths {
		identity, err := realProviderCanaryDirectoryIdentity(path)
		if err != nil {
			return nil, err
		}
		identities = append(identities, identity)
	}
	return identities, nil
}

func writeRealProviderCanarySigningMaterial(ephemeral string, fixture signedAuthorizationFixture) (string, string, error) {
	bundleBytes, err := marshalCanonical(fixture.bundle)
	if err != nil {
		return "", "", errors.New("bundle encode")
	}
	bundlePath := filepath.Join(ephemeral, "trust-bundle.json")
	if err := os.WriteFile(bundlePath, bundleBytes, 0o600); err != nil {
		return "", "", errors.New("bundle write")
	}
	privateText := privateSigningKeyPrefix + hex.EncodeToString(fixture.keys["peer"])
	keyBytes, err := marshalCanonical(map[string]any{
		"keys": []any{map[string]any{
			"private_key": privateText, "public_key": fixture.bundle.SupervisorPeer.Certificate.SubjectPublicKey,
			"role": "independent_supervisor_protocol_adapter", "root_id": fixture.bundle.SupervisorPeer.Certificate.IssuerRootID,
			"spki_sha256": fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
		}},
		"schema_version": privateSigningKeyBundleSchemaVersion, "trust_bundle_hash": fixture.bundle.TrustBundleHash,
	})
	if err != nil {
		return "", "", errors.New("key encode")
	}
	keyPath := filepath.Join(ephemeral, "signing-key.json")
	if err := os.WriteFile(keyPath, keyBytes, 0o600); err != nil {
		zeroBytes(keyBytes)
		return "", "", errors.New("key write")
	}
	zeroBytes(keyBytes)
	return bundlePath, keyPath, nil
}

func newRealProviderCanaryIntent(entry executionPolicyEntry, now time.Time) (auditExecutionIntent, error) {
	return sealAuditExecutionIntent(auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_real_provider_canary_001",
		EnvelopeHash: testHash("real-provider-canary-envelope"), LaunchSpecHash: entry.LaunchSpecHash,
		HandoffID: "remote_handoff_real_provider_canary_001", ReceiptHash: testHash("real-provider-canary-receipt"),
		TaskID: entry.TaskID, AttemptCap: 1, PolicyHash: entry.PolicyHash, RouteMappingHash: entry.RouteMappingHash,
		RepositoryIdentityHash: entry.RepositoryIdentityHash, GitCommit: entry.GitCommit, GitTree: entry.GitTree,
		SourceArchiveSHA256: entry.SourceArchiveSHA256, WrapperSHA256: entry.Wrapper.SHA256,
		RunID: "audit_run_real_provider_canary_001", CreatedAt: now.Format(time.RFC3339Nano),
	})
}

type realProviderCanaryWaitStage uint8

const (
	realProviderCanaryWaitStagePrelaunch realProviderCanaryWaitStage = iota + 1
	realProviderCanaryWaitStageRunning
)

type realProviderCanaryStageObservation struct {
	done       bool
	running    bool
	terminal   bool
	sawTimeout bool
}

type realProviderCanaryWaitResult struct {
	events     []auditExecutionEvent
	terminal   bool
	sawTimeout bool
}

type realProviderCanaryJournalLoader func(context.Context, string) (auditExecutionIntent, []auditExecutionEvent, error)

func inspectRealProviderCanaryStage(stage realProviderCanaryWaitStage, events []auditExecutionEvent, sawTimeout bool) realProviderCanaryStageObservation {
	observation := realProviderCanaryStageObservation{sawTimeout: sawTimeout}
	for _, event := range events {
		observation.running = observation.running || event.State == auditStateRunning
		observation.sawTimeout = observation.sawTimeout || event.State == auditStateTimedOut
	}
	if len(events) != 0 {
		switch events[len(events)-1].State {
		case auditStateCompleted, auditStateFailed, auditStateCancelled, auditStateWaitingForHuman:
			observation.terminal = true
		}
	}
	switch stage {
	case realProviderCanaryWaitStagePrelaunch:
		observation.done = observation.running || observation.terminal
	case realProviderCanaryWaitStageRunning:
		observation.done = observation.terminal
	}
	return observation
}

func waitForRealProviderCanaryStage(ctx context.Context, load realProviderCanaryJournalLoader, envelopeHash string, stage realProviderCanaryWaitStage, sawTimeout bool) (realProviderCanaryWaitResult, error) {
	result := realProviderCanaryWaitResult{sawTimeout: sawTimeout}
	if ctx == nil || load == nil {
		return result, errors.New("journal wait")
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, events, err := load(ctx, envelopeHash)
		if err != nil {
			return result, errors.New("journal wait")
		}
		observation := inspectRealProviderCanaryStage(stage, events, result.sawTimeout)
		result = realProviderCanaryWaitResult{
			events: events, terminal: observation.terminal, sawTimeout: observation.sawTimeout,
		}
		if observation.done {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, ErrDeadline
		case <-ticker.C:
		}
	}
}

func classifyRealProviderCanaryLifecycleFailure(stage realProviderCanaryWaitStage, waitErr, contextErr, closeErr error) string {
	failure := ""
	if waitErr != nil {
		switch {
		case errors.Is(contextErr, context.DeadlineExceeded) && stage == realProviderCanaryWaitStagePrelaunch:
			failure = "prelaunch_timeout"
		case errors.Is(contextErr, context.DeadlineExceeded) && stage == realProviderCanaryWaitStageRunning:
			failure = "running_terminal_timeout"
		default:
			failure = "journal_wait_error"
		}
	}
	if closeErr != nil {
		if failure != "" {
			return failure + "_and_executor_close_error"
		}
		return "executor_close_error"
	}
	return failure
}

type realProviderCanaryTimelineEvent struct {
	Sequence     int    `json:"sequence"`
	State        string `json:"state"`
	Attempt      int    `json:"attempt"`
	OccurredAt   string `json:"occurred_at"`
	FailureClass string `json:"failure_class"`
}

type realProviderCanaryWrapperDiagnostic struct {
	mu    sync.Mutex
	class string
}

func (diagnostic *realProviderCanaryWrapperDiagnostic) set(class string) {
	if diagnostic == nil || !validRealProviderCanaryWrapperDiagnostic(class) {
		return
	}
	diagnostic.mu.Lock()
	diagnostic.class = class
	diagnostic.mu.Unlock()
}

func (diagnostic *realProviderCanaryWrapperDiagnostic) get() string {
	if diagnostic == nil {
		return ""
	}
	diagnostic.mu.Lock()
	defer diagnostic.mu.Unlock()
	return diagnostic.class
}

type realProviderCanaryLogDiagnostic struct {
	mu    sync.Mutex
	class string
}

func (diagnostic *realProviderCanaryLogDiagnostic) set(class string) {
	if diagnostic == nil {
		return
	}
	diagnostic.mu.Lock()
	diagnostic.class = class
	diagnostic.mu.Unlock()
}

func (diagnostic *realProviderCanaryLogDiagnostic) get() string {
	if diagnostic == nil {
		return ""
	}
	diagnostic.mu.Lock()
	defer diagnostic.mu.Unlock()
	return diagnostic.class
}

func classifyRealProviderCanaryOMPLogs(result auditInvocationResult, secret string) string {
	logsRoot := filepath.Join(result.boundInvocation.HomeRunDir, "logs")
	labels := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	total := int64(0)
	walkErr := filepath.Walk(logsRoot, func(path string, information os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if information.IsDir() || !information.Mode().IsRegular() {
			return nil
		}
		if information.Size() < 0 || information.Size() > 256*1024 || total+information.Size() > 1024*1024 {
			return errors.New("log bounds")
		}
		total += information.Size()
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		defer zeroBytes(contents)
		if secret != "" && bytes.Contains(contents, []byte(secret)) {
			return errors.New("credential disclosure")
		}
		for _, line := range bytes.Split(contents, []byte{'\n'}) {
			var record struct {
				Level    string `json:"level"`
				Message  string `json:"message"`
				Provider string `json:"provider"`
				Step     string `json:"step"`
			}
			if len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &record) != nil {
				continue
			}
			level, levelOK := safeRealProviderCanaryLogLabel(record.Level)
			message, messageOK := safeRealProviderCanaryLogLabel(record.Message)
			provider, providerOK := safeRealProviderCanaryLogLabel(record.Provider)
			step, stepOK := safeRealProviderCanaryLogLabel(record.Step)
			if !levelOK || (level != "info" && level != "warn" && level != "error" && level != "fatal") || !messageOK {
				continue
			}
			label := level + "." + message
			if providerOK && provider != "" {
				label += "." + provider
			}
			if stepOK && step != "" {
				label += "." + step
			}
			if _, exists := seen[label]; !exists {
				seen[label] = struct{}{}
				labels = append(labels, label)
				if len(labels) > 8 {
					delete(seen, labels[0])
					labels = labels[1:]
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		if strings.Contains(walkErr.Error(), "credential disclosure") {
			return "credential_disclosure"
		}
		if errors.Is(walkErr, os.ErrNotExist) {
			return "logs_absent"
		}
		return "logs_unreadable_or_unbounded"
	}
	if len(labels) == 0 {
		return "logs_without_safe_warning_or_error"
	}
	return strings.Join(labels, ",")
}

func safeRealProviderCanaryLogLabel(value string) (string, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", true
	}
	if len(value) > 96 {
		return "", false
	}
	for _, forbidden := range []string{"/", "\\", ":", "key", "token", "credential", "secret", "http"} {
		if strings.Contains(value, forbidden) {
			return "", false
		}
	}
	for _, character := range value {
		if character != ' ' && character != '.' && character != '_' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return "", false
		}
	}
	return strings.ReplaceAll(value, " ", "_"), true
}

func classifyRealProviderCanaryWrapperExit(result auditInvocationResult, secret string) string {
	if result.ExitCode == 0 {
		trimmed := bytes.TrimSpace([]byte(result.Stdout))
		if len(trimmed) == 0 {
			return "exit_zero_empty_output"
		}
		var report auditModelReport
		if decodeCanonical(trimmed, &report) == nil && validAuditModelReport(report) {
			return "exit_zero_canonical_report"
		}
		return "exit_zero_noncanonical_output"
	}
	sample := make([]byte, 0, len(result.Stdout)+len(result.Stderr)+64*1024)
	sample = append(sample, result.Stdout...)
	sample = append(sample, '\n')
	sample = append(sample, result.Stderr...)
	if path := result.boundInvocation.OutputPath; path != "" {
		if output, err := os.Open(path); err == nil {
			contents, _ := io.ReadAll(io.LimitReader(output, 64*1024))
			_ = output.Close()
			sample = append(sample, '\n')
			sample = append(sample, contents...)
			zeroBytes(contents)
		}
	}
	sample = appendRealProviderCanarySessionSample(sample, result.boundInvocation.SessionDir)
	defer zeroBytes(sample)
	lower := bytes.ToLower(sample)
	if secret != "" && bytes.Contains(sample, []byte(secret)) {
		return "credential_disclosure"
	}
	if len(bytes.TrimSpace(lower)) == 0 {
		return "empty_output"
	}
	classes := []struct {
		class   string
		needles [][]byte
	}{
		{class: "timeout", needles: [][]byte{[]byte("deadline exceeded"), []byte("[omp_timeout]")}},
		{class: "policy_rejected", needles: [][]byte{[]byte("cyber_policy"), []byte("request rejected by policy")}},
		{class: "rate_limited", needles: [][]byte{[]byte("rate limit"), []byte("too many requests"), []byte("status 429")}},
		{class: "authentication", needles: [][]byte{[]byte("api key"), []byte("unauthorized"), []byte("authentication"), []byte("status 401"), []byte("status 403")}},
		{class: "sandbox_denied", needles: [][]byte{[]byte("permission denied"), []byte("operation not permitted"), []byte("sandbox")}},
		{class: "native_addon_load", needles: [][]byte{[]byte("pi_natives"), []byte("native addon"), []byte("dlopen")}},
		{class: "models_configuration_load", needles: [][]byte{[]byte("models.yml"), []byte("model configuration")}},
		{class: "runtime_load", needles: [][]byte{[]byte("cannot find module"), []byte("failed to load"), []byte("native addon")}},
		{class: "route_or_model", needles: [][]byte{[]byte("unsupported hermes route"), []byte("unknown model"), []byte("unknown provider"), []byte("model not found")}},
		{class: "provider_connection", needles: [][]byte{[]byte("fetch failed"), []byte("connection refused"), []byte("connect error"), []byte("network error")}},
		{class: "session_present_unclassified", needles: [][]byte{[]byte("[ananke_session_present]")}},
		{class: "working_only", needles: [][]byte{[]byte("working...")}},
		{class: "generic_error", needles: [][]byte{[]byte("error"), []byte("failed")}},
	}
	for _, candidate := range classes {
		for _, needle := range candidate.needles {
			if bytes.Contains(lower, needle) {
				return candidate.class
			}
		}
	}
	return "other_nonempty"
}

func appendRealProviderCanarySessionSample(sample []byte, sessionDir string) []byte {
	if sessionDir == "" || !filepath.IsAbs(sessionDir) || filepath.Clean(sessionDir) != sessionDir {
		return sample
	}
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return sample
	}
	remaining := 64 * 1024
	for _, entry := range entries {
		if remaining == 0 || entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(sessionDir, entry.Name())
		information, statErr := os.Lstat(path)
		if statErr != nil || information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() {
			continue
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			continue
		}
		contents, _ := io.ReadAll(io.LimitReader(file, int64(remaining)))
		_ = file.Close()
		sample = append(sample, "\n[ananke_session_present]\n"...)
		sample = append(sample, '\n')
		sample = append(sample, contents...)
		remaining -= len(contents)
		zeroBytes(contents)
	}
	return sample
}

func validRealProviderCanaryWrapperDiagnostic(class string) bool {
	switch class {
	case "", "credential_disclosure", "empty_output", "timeout", "policy_rejected", "rate_limited", "authentication",
		"sandbox_denied", "native_addon_load", "models_configuration_load", "runtime_load", "route_or_model", "provider_connection", "session_present_unclassified", "working_only", "generic_error", "other_nonempty",
		"exit_zero_empty_output", "exit_zero_canonical_report", "exit_zero_noncanonical_output":
		return true
	default:
		return false
	}
}

func withRealProviderCanaryWrapperDiagnostic(events []auditExecutionEvent, diagnostic string) []auditExecutionEvent {
	if !validRealProviderCanaryWrapperDiagnostic(diagnostic) || diagnostic == "" || len(events) == 0 {
		return events
	}
	copyEvents := append([]auditExecutionEvent(nil), events...)
	last := &copyEvents[len(copyEvents)-1]
	if last.FailureClass == "wrapper_exit_nonzero" || last.FailureClass == "direct_omp_exit_nonzero" {
		prefix := last.FailureClass
		code := "other"
		switch last.ExitCode {
		case 1, 2, 124, 126, 127, 137, 143:
			code = strconv.Itoa(last.ExitCode)
		}
		last.FailureClass = prefix + "_code_" + code + "_" + diagnostic
	} else if last.FailureClass == "direct_omp_or_capture_verification_failed" && diagnostic != "" {
		last.FailureClass += "_" + diagnostic
	}
	return copyEvents
}

func safeRealProviderCanaryTimeline(events []auditExecutionEvent) []byte {
	start := len(events) - realProviderCanaryTimelineMaximumEvents
	if start < 0 {
		start = 0
	}
	encodedReverse := make([][]byte, 0, len(events)-start)
	total := 2
	for index := len(events) - 1; index >= start; index-- {
		event := events[index]
		failureClass := event.FailureClass
		if failureClass == "wrapper_exit_nonzero" {
			switch event.ExitCode {
			case 1, 2, 124, 126, 127, 137, 143:
				failureClass += "_code_" + strconv.Itoa(event.ExitCode)
			default:
				failureClass += "_code_other"
			}
		}
		encoded, err := marshalCanonical(realProviderCanaryTimelineEvent{
			Sequence: event.Sequence, State: event.State, Attempt: event.Attempt,
			OccurredAt: event.OccurredAt, FailureClass: failureClass,
		})
		if err != nil {
			return []byte("[]")
		}
		extra := len(encoded)
		if len(encodedReverse) != 0 {
			extra++
		}
		if total+extra > realProviderCanaryTimelineMaximumBytes {
			break
		}
		encodedReverse = append(encodedReverse, encoded)
		total += extra
	}
	timeline := make([]byte, 0, total)
	timeline = append(timeline, '[')
	for index := len(encodedReverse) - 1; index >= 0; index-- {
		if len(timeline) != 1 {
			timeline = append(timeline, ',')
		}
		timeline = append(timeline, encodedReverse[index]...)
	}
	return append(timeline, ']')
}

func captureRealProviderCanaryRepositoryState(ctx context.Context, repository, temporaryRoot string) (realProviderCanaryRepositoryState, error) {
	identity, err := captureRealProviderCanaryGitIdentity(ctx, repository, temporaryRoot)
	if err != nil {
		return realProviderCanaryRepositoryState{}, err
	}
	status, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, 16*1024*1024, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return realProviderCanaryRepositoryState{}, err
	}
	refs, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, 16*1024*1024, "for-each-ref", "--format=%(refname)%00%(objectname)%00")
	if err != nil {
		zeroBytes(status)
		return realProviderCanaryRepositoryState{}, err
	}
	branch, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, maxGitMetadataBytes, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		zeroBytes(status)
		zeroBytes(refs)
		return realProviderCanaryRepositoryState{}, err
	}
	trackedList, err := runRealProviderCanaryGit(ctx, repository, temporaryRoot, 16*1024*1024, "ls-files", "-z", "--cached")
	if err != nil {
		zeroBytes(status)
		zeroBytes(refs)
		zeroBytes(branch)
		return realProviderCanaryRepositoryState{}, err
	}
	trackedHash, tracked, err := hashRealProviderCanaryTrackedBytes(repository, trackedList)
	zeroBytes(trackedList)
	if err != nil {
		zeroBytes(status)
		zeroBytes(refs)
		zeroBytes(branch)
		return realProviderCanaryRepositoryState{}, err
	}
	return realProviderCanaryRepositoryState{
		commit: identity.commit, tree: identity.tree, branch: branch, refs: refs, status: status, trackedHash: trackedHash, tracked: tracked,
	}, nil
}

func (state realProviderCanaryRepositoryState) equal(other realProviderCanaryRepositoryState) bool {
	return state.commit == other.commit && state.tree == other.tree && state.trackedHash == other.trackedHash && state.tracked == other.tracked &&
		bytes.Equal(state.branch, other.branch) && bytes.Equal(state.refs, other.refs) && bytes.Equal(state.status, other.status)
}

func (state *realProviderCanaryRepositoryState) zero() {
	if state == nil {
		return
	}
	zeroBytes(state.branch)
	zeroBytes(state.refs)
	zeroBytes(state.status)
}

func TestRealProviderCanaryTrackedHashBindsDeletedFileTombstone(t *testing.T) {
	repository := t.TempDir()
	tracked := []byte("deleted.txt\x00")
	missingHash, count, err := hashRealProviderCanaryTrackedBytes(repository, tracked)
	if err != nil || count != 1 || !protocolHashPattern.MatchString(missingHash) {
		t.Fatalf("deleted tracked tombstone = %q, %d, %v", missingHash, count, err)
	}
	path := filepath.Join(repository, "deleted.txt")
	if err := os.WriteFile(path, []byte("restored"), 0o600); err != nil {
		t.Fatal(err)
	}
	restoredHash, count, err := hashRealProviderCanaryTrackedBytes(repository, tracked)
	if err != nil || count != 1 || restoredHash == missingHash {
		t.Fatalf("restored tracked hash = %q, %d, %v; tombstone %q", restoredHash, count, err, missingHash)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	repeatedHash, count, err := hashRealProviderCanaryTrackedBytes(repository, tracked)
	if err != nil || count != 1 || repeatedHash != missingHash {
		t.Fatalf("repeated tombstone = %q, %d, %v; want %q", repeatedHash, count, err, missingHash)
	}
}

func hashRealProviderCanaryTrackedBytes(repository string, trackedList []byte) (string, int, error) {
	if len(trackedList) == 0 || trackedList[len(trackedList)-1] != 0 {
		return "", 0, errors.New("tracked file list")
	}
	digest := sha256.New()
	count := 0
	for _, raw := range bytes.Split(trackedList[:len(trackedList)-1], []byte{0}) {
		name := string(raw)
		if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return "", 0, errors.New("tracked file name")
		}
		path := filepath.Join(repository, name)
		information, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeRealProviderCanaryLengthFramedHashPart(digest, raw)
				writeRealProviderCanaryLengthFramedHashPart(digest, []byte("tracked_path_absent"))
				count++
				continue
			}
			return "", 0, errors.New("tracked file stat")
		}
		writeRealProviderCanaryLengthFramedHashPart(digest, raw)
		writeRealProviderCanaryLengthFramedHashPart(digest, []byte(information.Mode().String()))
		switch {
		case information.Mode().IsRegular():
			if err := hashRealProviderCanaryRegularFile(digest, path, information); err != nil {
				return "", 0, err
			}
		case information.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return "", 0, errors.New("tracked symlink read")
			}
			writeRealProviderCanaryLengthFramedHashPart(digest, []byte(target))
		default:
			return "", 0, errors.New("unsupported tracked file type")
		}
		count++
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), count, nil
}

func writeRealProviderCanaryLengthFramedHashPart(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

func hashRealProviderCanaryRegularFile(digest hash.Hash, path string, before os.FileInfo) error {
	beforeStatus, ok := before.Sys().(*syscall.Stat_t)
	if !ok || before.Size() < 0 {
		return errors.New("tracked file identity")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("tracked file open")
	}
	file := os.NewFile(uintptr(fd), "tracked-file")
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("tracked file descriptor")
	}
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFREG || uint64(opened.Dev) != uint64(beforeStatus.Dev) || opened.Ino != beforeStatus.Ino || opened.Size != before.Size() {
		return errors.New("tracked file replacement")
	}
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(opened.Size))
	_, _ = digest.Write(length[:])
	copied, err := io.CopyN(digest, file, opened.Size)
	if err != nil || copied != opened.Size {
		return errors.New("tracked file read")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || uint64(after.Dev) != uint64(opened.Dev) || after.Ino != opened.Ino || after.Mode != opened.Mode || after.Size != opened.Size {
		return errors.New("tracked file changed")
	}
	return nil
}

func runRealProviderCanaryGit(ctx context.Context, repository, temporaryRoot string, limit int, arguments ...string) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || limit < 1 {
		return nil, ErrDeadline
	}
	commandContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	argv := append([]string{"-C", repository}, arguments...)
	command := exec.CommandContext(commandContext, auditGitExecutable, argv...)
	command.Env = []string{
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0",
		"HOME=/var/empty", "LANG=C", "LC_ALL=C", "TMPDIR=" + temporaryRoot, "TZ=UTC", "XDG_CONFIG_HOME=/var/empty",
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout := &boundedCommandBuffer{limit: limit}
	stderr := &boundedCommandBuffer{limit: 16 * 1024}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		stdout.zero()
		stderr.zero()
		if commandContext.Err() != nil {
			return nil, ErrDeadline
		}
		return nil, errors.New("pinned git command")
	}
	stderr.zero()
	if stdout.err != nil {
		stdout.zero()
		return nil, ErrLimit
	}
	return stdout.take(), nil
}

func formatRealProviderCanaryTests(tests []realProviderCanaryTestSummary) string {
	var output strings.Builder
	for index, testResult := range tests {
		if index != 0 {
			output.WriteByte(',')
		}
		output.WriteString(testResult.ID)
		output.WriteByte(':')
		output.WriteString(strconv.Itoa(testResult.ExitCode))
	}
	return output.String()
}

func collectCanaryEvidenceDiagnostic(invocation auditInvocation, entry executionPolicyEntry) string {
	var parts []string
	info, statErr := os.Lstat(invocation.OutputPath)
	if statErr != nil {
		parts = append(parts, "output_lstat_err="+statErr.Error())
	} else {
		uid := -1
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			uid = int(st.Uid)
		}
		parts = append(parts, fmt.Sprintf("output_mode=%o output_uid=%d parent_uid=%d output_size=%d",
			info.Mode().Perm(), uid, os.Getuid(), info.Size()))
	}
	contents, readSize, readErr := readAuditRegularFile(invocation.OutputPath, maxAuditOutputBytes)
	if readErr != nil {
		parts = append(parts, "read_output_err="+readErr.Error())
	} else {
		parts = append(parts, fmt.Sprintf("read_output_ok size=%d", readSize))
		if auditBytesLeakAuthority(contents, entry, invocation) {
			parts = append(parts, "output_leak_authority=true")
		}
		var report auditModelReport
		if decErr := decodeCanonical(contents, &report); decErr != nil {
			parts = append(parts, "output_json_err="+decErr.Error())
			if normalized, normErr := decodeJSONValue(contents); normErr == nil {
				var canonical bytes.Buffer
				if appendErr := appendCanonicalValue(&canonical, normalized); appendErr == nil {
					diffAt, left, right := firstCanonicalDiff(contents, canonical.Bytes())
					parts = append(parts, fmt.Sprintf("canonical_diff_at=%d have=%q want=%q", diffAt, left, right))
				}
			}
		} else {
			parts = append(parts, fmt.Sprintf("output_json_ok schema=%q verdict=%q findings=%d summary_len=%d",
				report.SchemaVersion, report.Verdict, len(report.Findings), len(report.Summary)))
			if !validAuditModelReport(report) {
				parts = append(parts, "model_report_invalid="+describeModelReportViolation(report))
			}
		}
	}
	if entries, dirErr := os.ReadDir(invocation.SessionDir); dirErr == nil {
		for _, file := range entries {
			if file.IsDir() || filepath.Ext(file.Name()) != ".jsonl" {
				continue
			}
			sessionPath := filepath.Join(invocation.SessionDir, file.Name())
			data, err := os.ReadFile(sessionPath)
			if err != nil {
				parts = append(parts, "session_read_err="+err.Error())
				break
			}
			head := data
			if len(head) > 150 {
				head = head[:150]
			}
			parts = append(parts, fmt.Sprintf("session_bytes=%d uuid_ok=%v head=%q",
				len(data), auditSessionJSONLHasValidUUID(data), string(head)))
			promptBytes, _, promptErr := readAuditRegularFile(invocation.PromptPath, maxAuditTimeoutSessionHeaderBytes)
			if promptErr == nil {
				parts = append(parts, fmt.Sprintf("session_contains_prompt=%v prompt_bytes=%d",
					auditSessionJSONLContainsPrompt(data, string(promptBytes)), len(promptBytes)))
			} else {
				parts = append(parts, "prompt_read_err="+promptErr.Error())
			}
			parts = append(parts, fmt.Sprintf("session_cwd_ok=%v", auditSessionJSONLHasValidCWD(data, invocation.WorkDir)))
			parts = append(parts, fmt.Sprintf("session_foreign_paths_ok=%v", auditSessionJSONLHasNoForeignPaths(data, entry, invocation)))
			break
		}
	}
	return strings.Join(parts, " ")
}

func firstCanonicalDiff(have, want []byte) (int, string, string) {
	limit := len(have)
	if len(want) < limit {
		limit = len(want)
	}
	at := -1
	for index := 0; index < limit; index++ {
		if have[index] != want[index] {
			at = index
			break
		}
	}
	if at == -1 {
		at = limit
	}
	window := func(data []byte) string {
		start := at - 24
		if start < 0 {
			start = 0
		}
		end := at + 24
		if end > len(data) {
			end = len(data)
		}
		if start >= len(data) {
			return ""
		}
		return string(data[start:end])
	}
	return at, window(have), window(want)
}

func describeModelReportViolation(report auditModelReport) string {
	if report.SchemaVersion != auditModelReportSchemaVersion {
		return "schema_version"
	}
	if len(report.Summary) == 0 || len(report.Summary) > maxAuditModelSummaryBytes || strings.TrimSpace(report.Summary) != report.Summary ||
		strings.ContainsAny(report.Summary, "\r\n\x00") {
		return "summary"
	}
	if containsAbsoluteAuditPath(report.Summary) {
		return "summary_abs_path"
	}
	if len(report.Findings) > maxAuditModelFindings {
		return "findings_count"
	}
	if report.Verdict != "approved" && report.Verdict != "rejected" {
		return "verdict"
	}
	if report.Verdict == "approved" && len(report.Findings) != 0 || report.Verdict == "rejected" && len(report.Findings) == 0 {
		return "verdict_findings_mismatch"
	}
	severityRank := map[string]int{"blocker": 0, "high": 1, "medium": 2, "low": 3, "info": 4}
	for index, finding := range report.Findings {
		if _, ok := severityRank[finding.Severity]; !ok {
			return fmt.Sprintf("finding_%d_severity:%q", index, finding.Severity)
		}
		if !auditModelFindingCodePattern.MatchString(finding.Code) {
			return fmt.Sprintf("finding_%d_code:%q", index, finding.Code)
		}
		if finding.Line < 1 {
			return fmt.Sprintf("finding_%d_line:%d", index, finding.Line)
		}
		if len(finding.Message) == 0 || len(finding.Message) > maxAuditModelMessageBytes ||
			strings.TrimSpace(finding.Message) != finding.Message || strings.ContainsAny(finding.Message, "\r\n\x00") {
			return fmt.Sprintf("finding_%d_message", index)
		}
		if containsAbsoluteAuditPath(finding.Message) {
			return fmt.Sprintf("finding_%d_message_abs_path", index)
		}
		if !validAuditModelFindingPath(finding.Path) {
			return fmt.Sprintf("finding_%d_path:%q", index, finding.Path)
		}
		if index > 0 && !auditModelFindingLess(report.Findings[index-1], finding, severityRank) {
			return fmt.Sprintf("finding_%d_unsorted", index)
		}
	}
	return "unknown"
}
