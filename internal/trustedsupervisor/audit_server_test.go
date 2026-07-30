package trustedsupervisor

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

func TestProductionServerExecutesFakeAuditAndReconcilesWaitingThenCompleted(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: validAuditModelReportJSONForTest, DelayMilliseconds: 1000})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatalf("deliver executable audit: %v", err)
	}
	waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateRunning)
	callback, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt)
	if err != nil || callback != nil {
		t.Fatalf("running audit reconcile = %+v, %v; want pending", callback, err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateCompleted)
	callback, err = client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt)
	if err != nil || callback == nil {
		t.Fatalf("completed audit reconcile = %+v, %v", callback, err)
	}
	terminal := events[len(events)-1]
	if callback.Callback.TerminalState != auditStateCompleted || callback.Callback.ResultSchemaVersion != "ananke.independent-supervisor-result.v1" ||
		callback.Callback.EvidenceHash != terminal.EvidenceHash || terminal.SessionUUID != "" {
		t.Fatalf("completed callback/evidence mismatch: %+v / %+v", callback.Callback, terminal)
	}
	for _, forbidden := range []string{fixture.entry.Repository.Path, fixture.entry.Wrapper.Path, terminal.OutputPath} {
		encoded, err := marshalCanonical(callback.Callback)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("signed callback leaked private/raw value %q: %s", forbidden, encoded)
		}
	}
	var report auditEvidenceReport
	if err := decodeCanonical([]byte(terminal.EvidenceJSON), &report); err != nil {
		t.Fatalf("decode durable evidence report: %v", err)
	}
	if report.SourceArchiveSHA256 != fixture.entry.SourceArchiveSHA256 || report.GitCommit != fixture.entry.GitCommit ||
		report.PolicyHash != fixture.entry.PolicyHash || report.RouteMappingHash != fixture.entry.RouteMappingHash ||
		report.WrapperSHA256 != fixture.entry.Wrapper.SHA256 || report.ModelReport.SchemaVersion != auditModelReportSchemaVersion ||
		report.ModelReport.Summary != "One finding." || report.ModelReport.Verdict != "rejected" || len(report.ModelReport.Findings) != 1 ||
		report.ModelReport.Findings[0] != (auditModelFinding{Code: "READ_001", Line: 7, Message: "unsafe read", Path: "internal/a.go", Severity: "high"}) ||
		len(report.TestsRun) != 1 || report.TestsRun[0].CommandSHA256 != fixture.entry.AllowedTests[0].CommandSHA256 {
		t.Fatalf("durable audit evidence lost exact bindings: %+v", report)
	}
}

func TestProductionServerRejectsMalformedModelOutputDespiteSessionMarkers(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		output string
	}{
		{name: "plaintext", output: "bounded read-only audit report\n"},
		{name: "unknown field", output: `{"findings":[],"raw_authority":"forbidden","schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","verdict":"approved"}`},
		{name: "noncanonical", output: "{\"findings\": [],\"schema_version\":\"ananke.local-trusted-supervisor-model-audit-report.v1\",\"summary\":\"No findings.\",\"verdict\":\"approved\"}"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{
				Scenario: "report", Output: testCase.output,
				SpoofLog: "[OMP_SESSION] session_id=019f9a4a-a904-7000-b341-e07ecf0e3baf\n[OMP_TEST] command_sha256=COMMAND_HASH\n[OMP_EVIDENCE_COMPLETE]\n",
			})

			running := startInProcessProductionServer(t, fixture.material, now)
			defer running.stop(t)
			client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
			receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
			if err != nil {
				t.Fatal(err)
			}
			_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
			terminal := events[len(events)-1]
			if terminal.FailureClass != "evidence_verification_failed" || terminal.EvidenceJSON != "" {
				t.Fatalf("malformed output produced authority: %+v", terminal)
			}
			if callback, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt); err != nil || callback == nil ||
				callback.Callback.TerminalState == auditStateCompleted {
				t.Fatalf("malformed output reconcile = %+v, %v", callback, err)
			}
		})
	}
}

func TestProductionServerRejectsModelTestSpoofWhenSupervisorTestFails(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{
		Scenario: "report", Output: validAuditModelReportJSONForTest,
		SpoofLog: "[OMP_TEST] command_sha256=COMMAND_HASH\n[OMP_EVIDENCE_COMPLETE]\n",
	})

	fixture.entry.AllowedTests = []executionPolicyTestCommand{executionPolicyTestCommandForTest(t, "focused_go_test", "/usr/bin/false")}
	fixture.entry = mustSealExecutionPolicyEntryForTest(t, fixture.entry)
	writeExecutionPolicyFileForTest(t, fixture.material.executionPolicyPath, []executionPolicyEntry{fixture.entry})
	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
	terminal := events[len(events)-1]
	if terminal.FailureClass != "evidence_verification_failed" || terminal.EvidenceJSON != "" {
		t.Fatalf("model test spoof produced authority: %+v", terminal)
	}
}

func TestProductionServerRejectsOutputMutationBeforeLiveCallback(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: validAuditModelReportJSONForTest})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	beforeFinalizing := make(chan auditInvocation, 1)
	releaseFinalizing := make(chan struct{})
	defer func() {
		select {
		case <-releaseFinalizing:
		default:
			close(releaseFinalizing)
		}
	}()
	running.server.auditExecutor.hooks.beforeFinalizingPersist = func(invocation auditInvocation) {
		beforeFinalizing <- invocation
		<-releaseFinalizing
	}
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	var invocation auditInvocation
	select {
	case invocation = <-beforeFinalizing:
	case <-time.After(10 * time.Second):
		t.Fatal("evidence finalization did not reach pre-persistence output gate")
	}
	if err := os.WriteFile(invocation.OutputPath, []byte(`{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","verdict":"approved"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	close(releaseFinalizing)
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
	terminal := events[len(events)-1]
	if terminal.FailureClass != "evidence_verification_failed" || terminal.EvidenceJSON != "" {
		t.Fatalf("pre-finalizing output mutation produced evidence authority: %+v", terminal)
	}
	for _, event := range events {
		if event.State == auditStateFinalizing || event.State == auditStateCompleted {
			t.Fatalf("pre-finalizing output mutation persisted %s authority: %+v", event.State, events)
		}
	}
	callback, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt)
	if err != nil || callback == nil || callback.Callback.TerminalState == auditStateCompleted {
		t.Fatalf("pre-finalizing output mutation callback = %+v, %v; want non-completed closed result", callback, err)
	}
}

func TestProductionServerFinalizingRemainsPendingBeforeCleanup(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: validAuditModelReportJSONForTest})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	persisted := make(chan auditInvocation, 1)
	releaseCleanup := make(chan struct{})
	defer close(releaseCleanup)
	running.server.auditExecutor.hooks.afterFinalizingPersist = func(invocation auditInvocation) {
		persisted <- invocation
		<-releaseCleanup
	}
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	var invocation auditInvocation
	select {
	case invocation = <-persisted:
	case <-time.After(10 * time.Second):
		t.Fatal("verified evidence was not persisted before cleanup")
	}
	if err := verifyAuditOutputUnchanged(invocation.OutputPath, hashJournalBytes([]byte(validAuditModelReportJSONForTest)), int64(len(validAuditModelReportJSONForTest))); err != nil {
		t.Fatalf("unchanged finalizing output: %v", err)
	}
	_, events, err := running.server.journal.loadAuditExecution(context.Background(), fixture.material.fixture.envelope.EnvelopeHash)
	if err != nil || len(events) == 0 || events[len(events)-1].State != "finalizing" {
		t.Fatalf("paused lifecycle = %+v, %v; want durable nonterminal finalizing", events, err)
	}
	if callback, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt); err != nil || callback != nil {
		t.Fatalf("finalizing reconcile = %+v, %v; want pending without callback", callback, err)
	}
}

func TestAuditFinalizingRecoveryCleanupFailureForEveryOwnedRootIsNonterminal(t *testing.T) {
	for _, rootName := range []string{"prompt", "output", "session", "temporary", "work"} {
		t.Run(rootName, func(t *testing.T) {
			fixture := finalizingAuditExecutionHistoryForTest(t)
			identities := materializeSignedFinalizingRootsForPhaseBTest(t, &fixture)
			journal, err := openServerJournal(filepath.Join(t.TempDir(), "finalizing-cleanup.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
				t.Fatal(err)
			}
			if err := journal.storeAuditIntent(context.Background(), fixture.Intent); err != nil {
				t.Fatal(err)
			}
			for _, event := range fixture.Events {
				if err := journal.appendAuditEvent(context.Background(), event); err != nil {
					t.Fatal(err)
				}
			}
			roots := make(map[string]string)
			for _, identity := range identities {
				if identity.CleanupRoot {
					roots[identity.Role] = identity.Path
				}
			}
			target := roots[rootName]
			makeAuditTestDirectoriesRemovable(target)
			if err := os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
			escape := t.TempDir()
			if err := os.Symlink(escape, target); err != nil {
				t.Fatal(err)
			}
			executor, err := newUnrecoveredAuditExecutor(journal, fixture.Policy)
			if err != nil {
				t.Fatal(err)
			}
			started := false
			executor.hooks.beforeStart = func(string) { started = true }
			executor.recoverExisting(fixture.Intent, fixture.Events, executor.newActive())
			_, events, err := journal.loadAuditExecution(context.Background(), fixture.Intent.EnvelopeHash)
			if err != nil {
				t.Fatal(err)
			}
			if started || len(events) != len(fixture.Events) || events[len(events)-1].State != "finalizing" {
				t.Fatalf("failed %s cleanup reran child or exposed completion: started=%t events=%+v", rootName, started, events)
			}
			if information, err := os.Lstat(target); err != nil || information.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("failed %s cleanup target = %v, %v; want retained symlink", rootName, information, err)
			}
			for name, root := range roots {
				if root == target || strings.HasPrefix(root, target+string(filepath.Separator)) {
					continue
				}
				if _, err := os.Lstat(root); err != nil {
					t.Fatalf("failed %s cleanup altered independently authenticated %s root %q: %v", rootName, name, root, err)
				}
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "retryable-owned-artifact"), []byte("retry"), 0o600); err != nil {
				t.Fatal(err)
			}
			executor.recoverExisting(fixture.Intent, events, executor.newActive())
			_, retried, err := journal.loadAuditExecution(context.Background(), fixture.Intent.EnvelopeHash)
			if err != nil || len(retried) != len(fixture.Events) || retried[len(retried)-1].State != auditStateFinalizing {
				t.Fatalf("replacement %s identity escaped nonterminal finalizing = %+v, %v", rootName, retried, err)
			}
			if _, err := os.Lstat(target); err != nil {
				t.Fatalf("replacement %s identity was mistaken for signed original %q: %v", rootName, target, err)
			}
			executor.cancel()
		})
	}
}

func TestAuditFinalizingRestartResumesCleanupWithoutRerunningEffects(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		cleanupBeforeRun bool
	}{
		{name: "crash after finalizing before cleanup"},
		{name: "crash after cleanup before completed", cleanupBeforeRun: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := finalizingAuditExecutionHistoryForTest(t)
			identities := materializeSignedFinalizingRootsForPhaseBTest(t, &fixture)
			finalizing := fixture.Events[len(fixture.Events)-1]
			journal, err := openServerJournal(filepath.Join(t.TempDir(), "finalizing-restart.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
				t.Fatal(err)
			}
			if err := journal.storeAuditIntent(context.Background(), fixture.Intent); err != nil {
				t.Fatal(err)
			}
			for _, event := range fixture.Events {
				if err := journal.appendAuditEvent(context.Background(), event); err != nil {
					t.Fatal(err)
				}
			}
			roots := make(map[string]string)
			for _, identity := range identities {
				if identity.CleanupRoot {
					roots[identity.Role] = identity.Path
				}
			}
			if testCase.cleanupBeforeRun {
				for _, root := range roots {
					makeAuditTestDirectoriesRemovable(root)
					if err := os.RemoveAll(root); err != nil {
						t.Fatal(err)
					}
				}
			}
			executor, err := newUnrecoveredAuditExecutor(journal, fixture.Policy)
			if err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{}, 1)
			testStarted := make(chan struct{}, 1)
			executor.hooks.beforeStart = func(string) { started <- struct{}{} }
			executor.hooks.supervisorTestAfterStart = func(auditProcessIdentity) { testStarted <- struct{}{} }
			if err := executor.recover(); err != nil {
				t.Fatal(err)
			}
			_, events := waitForAuditState(t, journal, fixture.Intent.EnvelopeHash, auditStateCompleted)
			completed := events[len(events)-1]
			if len(events) != len(fixture.Events)+1 || completed.EvidenceJSON != finalizing.EvidenceJSON ||
				completed.EvidenceHash != finalizing.EvidenceHash || completed.OutputSHA256 != finalizing.OutputSHA256 ||
				completed.OutputSize != finalizing.OutputSize {
				t.Fatalf("recovered completion lost finalizing evidence binding: finalizing=%+v completed=%+v", finalizing, completed)
			}
			select {
			case <-started:
				t.Fatal("finalizing recovery reran child/provider")
			default:
			}
			select {
			case <-testStarted:
				t.Fatal("finalizing recovery reran supervisor test")
			default:
			}
			for name, root := range roots {
				if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("completed with retained %s root %q: %v", name, root, err)
				}
			}
			if err := executor.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProductionServerRejectsOutputMutationAtCompletedPersistence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: validAuditModelReportJSONForTest})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	running.server.auditExecutor.hooks.beforeFinalizingPersist = func(invocation auditInvocation) {
		if err := os.WriteFile(invocation.OutputPath, []byte("mutated before persist"), 0o600); err != nil {
			panic(err)
		}
	}
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateFailed)
	terminal := events[len(events)-1]
	if terminal.FailureClass != "evidence_verification_failed" || terminal.EvidenceJSON != "" {
		t.Fatalf("persist-time output mutation produced completed authority: %+v", terminal)
	}
}

func TestProductionServerExactCallbackReplayUsesJournalEvidenceAndRejectsRecreatedOutput(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: validAuditModelReportJSONForTest})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateCompleted)
	assertAuditExecutionRootsEmpty(t, fixture.entry)
	if callback, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt); err != nil || callback == nil {
		t.Fatalf("journal-only callback = %+v, %v", callback, err)
	}
	var requestBytes, responseBytes []byte
	if err := running.server.journal.db.QueryRow(`SELECT request_bytes, response_bytes FROM trusted_supervisor_requests WHERE operation = 'reconcile'`).Scan(&requestBytes, &responseBytes); err != nil {
		t.Fatal(err)
	}
	replayed, replayErr := exchangeExactServerRequestForTest(running.server.config.SocketPath, requestBytes)
	if replayErr != nil || !bytes.Equal(replayed, responseBytes) {
		t.Fatalf("journal-only exact replay equal=%t err=%v", bytes.Equal(replayed, responseBytes), replayErr)
	}
	terminal := events[len(events)-1]
	if err := os.MkdirAll(filepath.Dir(terminal.OutputPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(terminal.OutputPath, []byte(`{"findings":[],"schema_version":"ananke.local-trusted-supervisor-model-audit-report.v1","summary":"No findings.","verdict":"approved"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	replayed, replayErr = exchangeExactServerRequestForTest(running.server.config.SocketPath, requestBytes)
	if replayErr == nil && bytes.Equal(replayed, responseBytes) {
		t.Fatal("mutated recreated output received exact completed callback replay")
	}
}

func exchangeExactServerRequestForTest(socketPath string, requestBytes []byte) ([]byte, error) {
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	if err := writeFrame(connection, requestBytes, maxFrameBytes); err != nil {
		return nil, err
	}
	if err := connection.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(connection, maxFrameBytes)
}

func TestProductionServerCancelRunningFakeAuditKillsAndReapsPGID(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "hang_child"})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateRunning)
	runningEvent := events[len(events)-1]
	cancellation := validCancellationForTest(t, fixture.material.fixture.envelope, receipt)
	acknowledged, err := client.Cancel(context.Background(), fixture.material.fixture.envelope, receipt, cancellation)
	if err != nil {
		t.Fatalf("cancel running audit: %v", err)
	}
	if acknowledged.Acknowledgement.CancellationHash != cancellation.CancellationIdentityHash {
		t.Fatalf("cancellation acknowledgement lost binding: %+v", acknowledged.Acknowledgement)
	}
	_, events = waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateCancelled)
	terminal := events[len(events)-1]
	if terminal.PID != runningEvent.PID || terminal.PGID != runningEvent.PGID || terminal.ProcessStartIdentity != runningEvent.ProcessStartIdentity {
		t.Fatalf("cancelled evidence lost process ownership: running=%+v terminal=%+v", runningEvent, terminal)
	}
	if _, err := inspectAuditProcess(runningEvent.PID); err == nil {
		t.Fatalf("cancelled audit PID %d remains alive", runningEvent.PID)
	}
	if _, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt); err == nil {
		t.Fatal("reconcile after durable cancellation did not conflict")
	}
	assertAuditExecutionRootsEmpty(t, fixture.entry)
}

func TestAuditExecutorServerContextKillFailurePersistsWaitingNotCancelled(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", DelayMilliseconds: 1000})

	running := startInProcessProductionServer(t, fixture.material, now)
	running.server.auditExecutor.processOperations = killFailingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	running.server.auditExecutor.terminationBounds = testAuditTerminationBounds()
	privateKeyAlias := running.server.material.privateKey
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateRunning)
	runningEvent := events[len(events)-1]
	running.server.auditExecutor.cancel()
	_, events = waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateWaitingForHuman)
	terminal := events[len(events)-1]
	if terminal.FailureClass != "kill_signal_failed" || terminal.PID != runningEvent.PID || terminal.PGID != runningEvent.PGID ||
		terminal.ProcessStartIdentity != runningEvent.ProcessStartIdentity {
		t.Fatalf("server-context kill failure terminal = %+v, running = %+v", terminal, runningEvent)
	}
	for _, event := range events {
		if event.State == auditStateCancelled || event.State == auditStateTimedOut {
			t.Fatalf("termination failure produced false terminal state: %+v", event)
		}
	}
	if err := running.server.Close(); !errors.Is(err, ErrDeadline) {
		t.Fatalf("shutdown kill failure Close error = %v, want %v", err, ErrDeadline)
	}
	select {
	case err := <-running.done:
		if !errors.Is(err, ErrDeadline) {
			t.Fatalf("Serve shutdown kill failure = %v, want %v", err, ErrDeadline)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return bounded shutdown error")
	}
	if bytes.Count(privateKeyAlias, []byte{0}) == len(privateKeyAlias) {
		t.Fatal("shutdown deadline zeroed signing key under active audit process")
	}
	if err := running.server.journal.db.Ping(); err != nil {
		t.Fatalf("shutdown deadline closed journal under active audit process: %v", err)
	}
	if running.server.executionPolicy == nil || running.server.repositoryPolicy == nil {
		t.Fatal("shutdown deadline released policies under active audit process")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := running.server.Close(); err == nil {
			break
		} else if !errors.Is(err, ErrDeadline) {
			t.Fatalf("retry shutdown after process exit: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown retry did not join exited process")
		}
	}
	running.cancel()
	assertZeroedLifecycleAlias(t, privateKeyAlias, "private key after shutdown retry")
}

type killFailingAuditProcessOperations struct {
	delegate auditProcessOperations
}

func (operations killFailingAuditProcessOperations) Inspect(pid int) (auditProcessIdentity, bool, error) {
	return operations.delegate.Inspect(pid)
}

func (operations killFailingAuditProcessOperations) SignalGroup(_ int, signal unix.Signal) error {
	if signal == unix.SIGKILL {
		return errors.New("injected kill failure")
	}
	return nil
}

func (operations killFailingAuditProcessOperations) GroupExists(pgid int) (bool, error) {
	return operations.delegate.GroupExists(pgid)
}

func TestProductionServerTimeoutRetriesExactSessionThenCompletes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{
		Scenario: "timeout_once", SessionUUID: "019f9a4a-a904-7000-b341-e07ecf0e3baf",
		ResumeOutput: validAuditModelReportJSONForTest, DelayMilliseconds: 200,
	})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateCompleted)
	wantStates := []string{auditStatePrepared, auditStateRunning, auditStateTimedOut, auditStatePrepared, auditStateRunning, auditStateFinalizing, auditStateCompleted}
	if len(events) != len(wantStates) {
		t.Fatalf("timeout/resume event count = %d, want %d: %+v", len(events), len(wantStates), events)
	}
	for index, want := range wantStates {
		if events[index].State != want {
			t.Fatalf("timeout/resume event %d state = %s, want %s", index, events[index].State, want)
		}
	}
	const resumeUUID = "019f9a4a-a904-7000-b341-e07ecf0e3baf"
	if events[2].SessionUUID != resumeUUID || events[3].Attempt != 2 ||
		events[3].CommandDescriptorHash == events[0].CommandDescriptorHash || events[0].SessionPath != events[3].SessionPath {
		t.Fatalf("timeout exact resume binding lost: %+v", events)
	}
	for index := 3; index <= 6; index++ {
		if events[index].Attempt != 2 || events[index].ResumeSessionUUID != resumeUUID || !events[index].SynthesizeOnly ||
			events[index].SessionPath != events[3].SessionPath || events[index].CommandDescriptorHash != events[3].CommandDescriptorHash {
			t.Fatalf("timeout resume event %d lost exact attempt/session/command binding: %+v", index, events[index])
		}
	}
	finalizing, completed := events[5], events[6]
	if completed.FinalizingEventHash != finalizing.EventHash || completed.EvidenceHash != finalizing.EvidenceHash ||
		completed.EvidenceJSON != finalizing.EvidenceJSON || completed.OutputSHA256 != finalizing.OutputSHA256 ||
		completed.OutputSize != finalizing.OutputSize {
		t.Fatalf("timeout completion lost finalizing evidence binding: %+v / %+v", finalizing, completed)
	}
	assertAuditExecutionRootsEmpty(t, fixture.entry)
	for _, attemptEvent := range []auditExecutionEvent{events[0], events[3]} {
		for _, path := range []string{filepath.Dir(attemptEvent.PromptPath), filepath.Dir(attemptEvent.OutputPath), attemptEvent.TemporaryPath} {
			assertAuditCleanupPathAbsent(t, path)
		}
	}
	for _, path := range []string{events[0].SessionPath, filepath.Dir(events[0].WorkPath)} {
		assertAuditCleanupPathAbsent(t, path)
	}
}

func TestProductionServerTimeoutAttemptCapWaitsForHuman(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{
		Scenario: "timeout_always", SessionUUID: "019f9a4a-a904-7000-b341-e07ecf0e3baf", DelayMilliseconds: 200,
	})

	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateWaitingForHuman)
	terminal := events[len(events)-1]
	if terminal.FailureClass != "attempt_cap_exhausted" || terminal.Attempt != fixture.entry.AttemptCap {
		t.Fatalf("attempt cap terminal evidence = %+v", terminal)
	}
	var timedOut int
	for _, event := range events {
		if event.State == auditStateTimedOut {
			timedOut++
			if event.SessionUUID != "019f9a4a-a904-7000-b341-e07ecf0e3baf" {
				t.Fatalf("attempt %d lost exact timeout UUID: %+v", event.Attempt, event)
			}
		}
	}
	if timedOut != fixture.entry.AttemptCap {
		t.Fatalf("timed out attempts = %d, want %d", timedOut, fixture.entry.AttemptCap)
	}
	callback, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt)
	if err != nil || callback == nil || callback.Callback.TerminalState != store.ExternalSupervisorWaitingForHumanState {
		t.Fatalf("attempt-cap reconcile = %+v, %v", callback, err)
	}
}

func TestAuditExecutorRestartFailsClosedOnWrongPIDStartIdentity(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(material.directory, "restart-journal.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	intent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_wrong_pid_001",
		EnvelopeHash: testHash("wrong-pid-envelope"), LaunchSpecHash: material.entry.LaunchSpecHash, HandoffID: "remote_handoff_wrong_pid_001",
		ReceiptHash: testHash("wrong-pid-receipt"), TaskID: material.entry.TaskID, AttemptCap: material.entry.AttemptCap,
		PolicyHash: material.entry.PolicyHash, RouteMappingHash: material.entry.RouteMappingHash, RepositoryIdentityHash: material.entry.RepositoryIdentityHash,
		GitCommit: material.entry.GitCommit, GitTree: material.entry.GitTree, SourceArchiveSHA256: material.entry.SourceArchiveSHA256,
		WrapperSHA256: material.entry.Wrapper.SHA256, RunID: "audit_run_wrong_pid_001", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	promptHash := auditPromptSHA256(false)
	commandHash, err := auditCommandDescriptorHash(material.entry, promptHash, intent.RunID, auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, material.entry, 1)
	prepared := mustSealAuditEventForTest(t, auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditExecutionEventID(intent, 1), IntentHash: intent.IntentHash,
		Sequence: 1, State: auditStatePrepared, Attempt: 1, CommandDescriptorHash: commandHash,
		PromptSHA256: promptHash, SessionRunID: intent.RunID,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), WorkPath: workPath,
		OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
	})
	running := mustSealAuditEventForTest(t, auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditExecutionEventID(intent, 2), IntentHash: intent.IntentHash,
		Sequence: 2, State: auditStateRunning, Attempt: 1, CommandDescriptorHash: commandHash,
		PromptSHA256: promptHash, SessionRunID: intent.RunID,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), PID: os.Getpid(), PGID: os.Getpid(), ProcessStartIdentity: "0:0",
		ProcessStartedAt: intent.CreatedAt, WorkPath: workPath, OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
	})
	if err := journal.appendAuditEvent(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if err := journal.appendAuditEvent(context.Background(), running); err != nil {
		t.Fatal(err)
	}
	sentinels := []string{
		filepath.Join(filepath.Dir(workPath), "unrelated-work"),
		filepath.Join(filepath.Dir(outputPath), "unrelated-output"),
		filepath.Join(sessionPath, "unrelated-session"),
		filepath.Join(filepath.Dir(promptPath), "unrelated-prompt"),
		filepath.Join(temporaryPath, "unrelated-temporary"),
	}
	for _, sentinel := range sentinels {
		if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("must-not-clean"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	operations := &countingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	executor.processOperations = operations
	if err := executor.recover(); err != nil {
		t.Fatal(err)
	}
	_, events := waitForAuditState(t, journal, intent.EnvelopeHash, auditStateWaitingForHuman)
	if events[len(events)-1].FailureClass != "process_identity_mismatch" {
		t.Fatalf("wrong PID recovery = %+v", events[len(events)-1])
	}
	if got := operations.signalCount(); got != 0 {
		t.Fatalf("wrong PID recovery signalled unrelated process group %d times", got)
	}
	for _, sentinel := range sentinels {
		if contents, err := os.ReadFile(sentinel); err != nil || string(contents) != "must-not-clean" {
			t.Fatalf("wrong PID recovery cleaned unrelated artifact %s: %q, %v", sentinel, contents, err)
		}
	}
	if err := executor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProductionServerCrashRestartReconcilesRunningWithoutGuessingSuccess(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: "completed after parent crash\n", DelayMilliseconds: 2000})

	authority := newServerAuditAuthorityTestFixture(t, fixture.material, now)
	first := startFakeBrokerServerProcess(t, fixture.material, now)
	client := newServerTestClient(t, fixture.material, int32(first.command.Process.Pid), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		first.killAndWait(t)
		t.Fatal(err)
	}
	waitForAuditStatePath(t, authority, fixture.material.journalPath, fixture.material.fixture.envelope.EnvelopeHash, auditStateRunning)
	first.killAndWait(t)
	second := startFakeBrokerServerProcess(t, fixture.material, now)
	defer second.terminateAndWait(t)
	client = newServerTestClient(t, fixture.material, int32(second.command.Process.Pid), now)
	callback, err := client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt)
	if err != nil {
		t.Fatalf("restart running reconcile: %v", err)
	}
	if callback == nil {
		deadline := time.Now().Add(10 * time.Second)
		for callback == nil {
			callback, err = client.Reconcile(context.Background(), fixture.material.fixture.envelope, receipt)
			if err != nil {
				t.Fatal(err)
			}
			if callback == nil {
				if time.Now().After(deadline) {
					t.Fatal("recovered orphan never reached bounded terminal state")
				}
				time.Sleep(25 * time.Millisecond)
			}
		}
	}
	_, events := waitForAuditStatePath(t, authority, fixture.material.journalPath, fixture.material.fixture.envelope.EnvelopeHash, auditStateWaitingForHuman)
	terminal := events[len(events)-1]
	expectedEvidenceHash, err := canonicalHash(map[string]any{
		"audit_state": terminal.State, "failure_class": terminal.FailureClass,
		"schema_version": store.ExternalSupervisorAuditNotRunResultSchemaVersion,
		"state":          store.ExternalSupervisorWaitingForHumanState, "verification_state": "not_run",
	})
	if err != nil {
		t.Fatal(err)
	}
	authentication := callback.CallbackAuthentication
	if callback.Callback.TerminalState != store.ExternalSupervisorWaitingForHumanState ||
		callback.Callback.ResultSchemaVersion != store.ExternalSupervisorAuditNotRunResultSchemaVersion ||
		callback.Callback.EvidenceHash != expectedEvidenceHash || authentication.MessageType != "callback" ||
		authentication.MessageHash != callback.Callback.CallbackHash || authentication.NonceHash != callback.Callback.NonceHash ||
		authentication.ChannelBindingHash != callback.Callback.CallbackChannelBindingHash || authentication.Signature == "" ||
		authentication.SignatureHash == "" {
		t.Fatalf("restart guessed terminal success or returned unbound audit-not-run evidence: callback=%+v authentication=%+v terminal=%+v", callback.Callback, authentication, terminal)
	}
}

type executingServerTestMaterial struct {
	material serverTestMaterial
	entry    executionPolicyEntry
}

func newExecutingServerTestMaterial(t *testing.T, now time.Time, fixture fakeAuditOMPFixture) executingServerTestMaterial {

	t.Helper()
	material := newServerTestMaterial(t, now)
	gitMaterial := newGitArchivePolicyMaterial(t)
	entry := gitMaterial.entry
	t.Cleanup(func() { makeTreeRemovableForTest(entry.WorkRoot) })
	entry.LaunchSpecHash = material.fixture.envelope.LaunchSpecHash
	entry.TaskID = "audit_task_server_execution_001"
	entry.RepositoryIdentity = material.fixture.envelope.RepositoryIdentity
	entry.RepositoryIdentityHash = repositoryIdentityHash(entry.RepositoryIdentity)
	entry.RouteMappingHash = material.fixture.envelope.RouteMappingHash
	entry.AttemptCap = material.fixture.envelope.AttemptCap
	entry.InternalDeadlineSeconds = 5
	entry.WrapperGraceSeconds = 2
	entry.AllowedTests = []executionPolicyTestCommand{executionPolicyTestCommandForTest(t, "focused_go_test", "/usr/bin/true")}
	fixture.SpoofLog = strings.ReplaceAll(fixture.SpoofLog, "COMMAND_HASH", entry.AllowedTests[0].CommandSHA256)
	installNativeFakeAuditOMPForTest(t, &entry, fixture)
	entry = mustSealExecutionPolicyEntryForTest(t, entry)
	writeExecutionPolicyFileForTest(t, material.executionPolicyPath, []executionPolicyEntry{entry})
	return executingServerTestMaterial{material: material, entry: entry}
}

func waitForAuditState(t *testing.T, journal *serverJournal, envelopeHash, want string) (auditExecutionIntent, []auditExecutionEvent) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		intent, events, err := journal.loadAuditExecution(context.Background(), envelopeHash)
		if err == nil && len(events) != 0 && events[len(events)-1].State == want {
			return intent, events
		}
		if err != nil && !errorsIsAuditNotReady(err) {
			t.Fatalf("load audit state: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("audit state did not reach %s; last events=%+v err=%v", want, events, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForAuditStatePath(t *testing.T, authority auditAuthorityTestFixture, journalPath, envelopeHash, want string) (auditExecutionIntent, []auditExecutionEvent) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		journal, err := openServerJournal(journalPath)
		if err != nil {
			t.Fatal(err)
		}
		authority.bind(t, journal)
		intent, events, loadErr := journal.loadAuditExecution(context.Background(), envelopeHash)
		closeErr := journal.Close()
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if loadErr == nil && len(events) != 0 && events[len(events)-1].State == want {
			return intent, events
		}
		if time.Now().After(deadline) {
			t.Fatalf("external audit state did not reach %s; events=%+v err=%v", want, events, loadErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func errorsIsAuditNotReady(err error) bool {
	return err == nil || err == ErrReplay
}

func materializeFinalizingAuditRootsForTest(t *testing.T, event auditExecutionEvent) map[string]string {
	t.Helper()
	roots := map[string]string{
		"prompt":            filepath.Dir(event.PromptPath),
		"output":            filepath.Dir(event.OutputPath),
		"session":           event.SessionPath,
		"temporary":         event.TemporaryPath,
		"work":              filepath.Dir(event.WorkPath),
		"wrapper_transport": event.TemporaryPath,
	}
	for _, root := range []string{roots["prompt"], roots["output"], roots["session"], roots["temporary"], event.WorkPath} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "owned-finalizing-artifact"), []byte("sensitive"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, transportRoot := range []string{
		filepath.Join(event.TemporaryPath, "wrapper-state"),
		filepath.Join(event.TemporaryPath, "omp-agent"),
		filepath.Join(event.TemporaryPath, "home"),
	} {
		if err := os.MkdirAll(transportRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(transportRoot, "owned-transport-artifact"), []byte("transport"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return roots
}
