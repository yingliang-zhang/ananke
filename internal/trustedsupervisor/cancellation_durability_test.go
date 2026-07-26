package trustedsupervisor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAuditCancellationIntentCommitsRequestedBeforeEffectAndCompletesAtomically(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(t.TempDir(), "cancellation.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	defer journal.Close()

	auditIntent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion,
		IntentID:      "audit_intent_cancel_durable_001",
		EnvelopeHash:  testHash("cancel-durable-envelope"), LaunchSpecHash: material.entry.LaunchSpecHash,
		HandoffID: "remote_handoff_cancel_durable_001", ReceiptHash: testHash("cancel-durable-receipt"),
		TaskID: material.entry.TaskID, AttemptCap: material.entry.AttemptCap,
		PolicyHash: material.entry.PolicyHash, RouteMappingHash: material.entry.RouteMappingHash,
		RepositoryIdentityHash: material.entry.RepositoryIdentityHash, GitCommit: material.entry.GitCommit,
		GitTree: material.entry.GitTree, SourceArchiveSHA256: material.entry.SourceArchiveSHA256,
		WrapperSHA256: material.entry.Wrapper.SHA256, RunID: "audit_run_cancel_durable_001",
		CreatedAt: "2026-07-26T01:00:00Z",
	})
	if err := journal.storeAuditIntent(context.Background(), auditIntent); err != nil {
		t.Fatal(err)
	}

	requestBytes := []byte(`{"operation":"cancel","request":"exact"}`)
	cancellation := mustSealAuditCancellationIntentForTest(t, auditCancellationIntent{
		SchemaVersion: auditCancellationIntentSchemaVersion,
		IntentID:      "audit_cancellation_cancel_durable_001",
		RequestHash:   testHash("cancel-durable-request"), RequestBytes: requestBytes,
		OperationKey:     "cancel:" + auditIntent.ReceiptHash,
		RequestNonceHash: testHash("cancel-durable-request-nonce"), ResponseNonceHash: testHash("cancel-durable-response-nonce"),
		ExclusivityNonceHash: testHash("cancel-durable-exclusive-nonce"),
		EnvelopeHash:         auditIntent.EnvelopeHash, HandoffID: auditIntent.HandoffID, ReceiptHash: auditIntent.ReceiptHash,
		CancellationHash: testHash("cancel-durable-cancellation"), Attempt: 1,
		State: auditCancellationStateRequested, RequestedAt: "2026-07-26T01:00:01Z",
	})

	stored, err := journal.requestAuditCancellation(context.Background(), cancellation)
	if err != nil || stored.State != auditCancellationStateRequested || len(stored.ResponseBytes) != 0 {
		t.Fatalf("persist requested cancellation = %+v, %v", stored, err)
	}
	var requests, cancellationIntents, cancellationOutcomes, auditEvents int
	for table, destination := range map[string]*int{
		"trusted_supervisor_requests":              &requests,
		"trusted_supervisor_cancellation_intents":  &cancellationIntents,
		"trusted_supervisor_cancellation_outcomes": &cancellationOutcomes,
		"trusted_supervisor_audit_events":          &auditEvents,
	} {
		if err := journal.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 0 || cancellationIntents != 1 || cancellationOutcomes != 0 || auditEvents != 0 {
		t.Fatalf("requested phase rows requests=%d intents=%d outcomes=%d events=%d", requests, cancellationIntents, cancellationOutcomes, auditEvents)
	}

	replayedRequested, err := journal.requestAuditCancellation(context.Background(), cancellation)
	if err != nil || replayedRequested.State != auditCancellationStateRequested {
		t.Fatalf("requested replay = %+v, %v", replayedRequested, err)
	}
	conflict := cancellation
	conflict.RequestHash = testHash("cancel-durable-conflict")
	conflict.RequestBytes = []byte(`{"operation":"cancel","request":"conflict"}`)
	conflict = mustSealAuditCancellationIntentForTest(t, conflict)
	if _, err := journal.requestAuditCancellation(context.Background(), conflict); !errors.Is(err, ErrReplay) {
		t.Fatalf("conflicting cancellation error = %v, want %v", err, ErrReplay)
	}

	promptHash := auditPromptSHA256(false)
	commandHash, err := auditCommandDescriptorHash(material.entry, promptHash, auditIntent.RunID, auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(auditIntent, material.entry, 1)
	cancelled := mustSealAuditEventForTest(t, auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion,
		EventID:       auditExecutionEventID(auditIntent, 1), IntentHash: auditIntent.IntentHash,
		Sequence: 1, State: auditStateCancelled, Attempt: 1,
		CommandDescriptorHash: commandHash, PromptSHA256: promptHash, SessionRunID: auditIntent.RunID,
		OccurredAt:   "2026-07-26T01:00:02Z",
		FailureClass: "operator_cancelled_before_start", WorkPath: workPath,
		OutputPath:    outputPath,
		SessionPath:   sessionPath,
		PromptPath:    promptPath,
		TemporaryPath: temporaryPath,
	})
	responseBytes := []byte(`{"response":"signed-cancellation"}`)
	completed, err := journal.completeAuditCancellation(context.Background(), cancellation, cancelled, auditCancellationOutcomeCompleted, func() ([]byte, error) {
		return append([]byte(nil), responseBytes...), nil
	})
	if err != nil || completed.State != auditCancellationStateCompleted || !bytes.Equal(completed.ResponseBytes, responseBytes) {
		t.Fatalf("complete cancellation = %+v, %v", completed, err)
	}
	for table, destination := range map[string]*int{
		"trusted_supervisor_requests":              &requests,
		"trusted_supervisor_cancellation_intents":  &cancellationIntents,
		"trusted_supervisor_cancellation_outcomes": &cancellationOutcomes,
		"trusted_supervisor_audit_events":          &auditEvents,
	} {
		if err := journal.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 1 || cancellationIntents != 1 || cancellationOutcomes != 1 || auditEvents != 1 {
		t.Fatalf("completed phase rows requests=%d intents=%d outcomes=%d events=%d", requests, cancellationIntents, cancellationOutcomes, auditEvents)
	}

	exact, err := journal.requestAuditCancellation(context.Background(), cancellation)
	if err != nil || exact.State != auditCancellationStateCompleted || !bytes.Equal(exact.ResponseBytes, responseBytes) {
		t.Fatalf("completed exact replay = %+v, %v", exact, err)
	}
}

func TestAuditCancellationOwnedGroupTermSuccess(t *testing.T) {
	ops := newFakeAuditProcessOperations()
	ops.termStops = true
	waiter := newAuditProcessWaiter()
	waiter.complete(nil)
	identity := auditProcessIdentity{PID: 4101, PGID: 4101, ProcessStartIdentity: "100:1"}
	result := terminateOwnedAuditProcess(context.Background(), identity, waiter, ops, testAuditTerminationBounds())
	if result.Outcome != auditTerminationConfirmedExit || result.Failure != nil {
		t.Fatalf("TERM cancellation result = %+v, want confirmed_exit", result)
	}
	if got := ops.signalsSnapshot(); len(got) != 1 || got[0] != unix.SIGTERM {
		t.Fatalf("signals = %v, want TERM", got)
	}
}

func TestAuditCancellationTermIgnoredEscalatesToKill(t *testing.T) {
	ops := newFakeAuditProcessOperations()
	ops.killStops = true
	waiter := newAuditProcessWaiter()
	waiter.complete(nil)
	identity := auditProcessIdentity{PID: 4102, PGID: 4102, ProcessStartIdentity: "100:2"}
	ops.identity = identity
	result := terminateOwnedAuditProcess(context.Background(), identity, waiter, ops, testAuditTerminationBounds())
	if result.Outcome != auditTerminationConfirmedExit || result.Failure != nil {
		t.Fatalf("TERM/KILL cancellation result = %+v, want confirmed_exit", result)
	}
	if got := ops.signalsSnapshot(); len(got) != 2 || got[0] != unix.SIGTERM || got[1] != unix.SIGKILL {
		t.Fatalf("signals = %v, want TERM then KILL", got)
	}
}

func TestAuditCancellationKillErrorFailsClosed(t *testing.T) {
	ops := newFakeAuditProcessOperations()
	ops.killErr = errors.New("injected kill failure")
	identity := auditProcessIdentity{PID: 4103, PGID: 4103, ProcessStartIdentity: "100:3"}
	ops.identity = identity
	result := terminateOwnedAuditProcess(context.Background(), identity, nil, ops, testAuditTerminationBounds())
	var termination *auditTerminationError
	if result.Outcome != auditTerminationFailure || !errors.As(result.Failure, &termination) || termination.FailureClass != "kill_signal_failed" {
		t.Fatalf("kill failure = %+v, want typed kill_signal_failed", result)
	}
}

func TestAuditCancellationUnkillableGroupTimesOutFailsClosed(t *testing.T) {
	ops := newFakeAuditProcessOperations()
	identity := auditProcessIdentity{PID: 4104, PGID: 4104, ProcessStartIdentity: "100:4"}
	ops.identity = identity
	started := time.Now()
	result := terminateOwnedAuditProcess(context.Background(), identity, nil, ops, testAuditTerminationBounds())
	var termination *auditTerminationError
	if result.Outcome != auditTerminationFailure || !errors.As(result.Failure, &termination) || termination.FailureClass != "group_exit_unconfirmed" {
		t.Fatalf("unkillable failure = %+v, want typed group_exit_unconfirmed", result)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("unkillable cancellation was unbounded: %v", elapsed)
	}
}

func TestAuditCancellationExitWaitIsBoundedAndRequired(t *testing.T) {
	ops := newFakeAuditProcessOperations()
	ops.termStops = true
	waiter := newAuditProcessWaiter()
	identity := auditProcessIdentity{PID: 4106, PGID: 4106, ProcessStartIdentity: "100:6"}
	ops.identity = identity
	started := time.Now()
	result := terminateOwnedAuditProcess(context.Background(), identity, waiter, ops, testAuditTerminationBounds())
	var termination *auditTerminationError
	if result.Outcome != auditTerminationFailure || !errors.As(result.Failure, &termination) || termination.FailureClass != "process_wait_unconfirmed" {
		t.Fatalf("wait timeout = %+v, want typed process_wait_unconfirmed", result)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("process wait was unbounded: %v", elapsed)
	}
}

func TestAuditCancellationWrongOrReusedPIDFailsBeforeSignal(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		observed auditProcessIdentity
	}{
		{name: "wrong pgid", observed: auditProcessIdentity{PID: 4105, PGID: 9999, ProcessStartIdentity: "100:5"}},
		{name: "reused pid", observed: auditProcessIdentity{PID: 4105, PGID: 4105, ProcessStartIdentity: "200:5"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ops := newFakeAuditProcessOperations()
			ops.identity = testCase.observed
			identity := auditProcessIdentity{PID: 4105, PGID: 4105, ProcessStartIdentity: "100:5"}
			result := terminateOwnedAuditProcess(context.Background(), identity, nil, ops, testAuditTerminationBounds())
			if result.Outcome != auditTerminationFailure || !errors.Is(result.Failure, ErrAuthentication) {
				t.Fatalf("identity failure = %+v, want %v", result, ErrAuthentication)
			}
			if got := ops.signalsSnapshot(); len(got) != 0 {
				t.Fatalf("identity mismatch emitted signals: %v", got)
			}
		})
	}
}

func testAuditTerminationBounds() auditTerminationBounds {
	return auditTerminationBounds{TermGrace: 5 * time.Millisecond, KillGrace: 5 * time.Millisecond, PollInterval: time.Millisecond}
}

type fakeAuditProcessOperations struct {
	mu           sync.Mutex
	identity     auditProcessIdentity
	leaderExists bool
	groupExists  bool
	termStops    bool
	killStops    bool
	termErr      error
	killErr      error
	signals      []unix.Signal
}

func newFakeAuditProcessOperations() *fakeAuditProcessOperations {
	return &fakeAuditProcessOperations{
		identity:     auditProcessIdentity{PID: 4101, PGID: 4101, ProcessStartIdentity: "100:1"},
		leaderExists: true, groupExists: true,
	}
}

func (operations *fakeAuditProcessOperations) Inspect(pid int) (auditProcessIdentity, bool, error) {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	if !operations.leaderExists {
		return auditProcessIdentity{}, false, nil
	}
	identity := operations.identity
	identity.PID = pid
	return identity, true, nil
}

func (operations *fakeAuditProcessOperations) SignalGroup(_ int, signal unix.Signal) error {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	operations.signals = append(operations.signals, signal)
	if signal == unix.SIGTERM {
		if operations.termErr != nil {
			return operations.termErr
		}
		if operations.termStops {
			operations.leaderExists = false
			operations.groupExists = false
		}
	}
	if signal == unix.SIGKILL {
		if operations.killErr != nil {
			return operations.killErr
		}
		if operations.killStops {
			operations.leaderExists = false
			operations.groupExists = false
		}
	}
	return nil
}

func (operations *fakeAuditProcessOperations) GroupExists(int) (bool, error) {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return operations.groupExists, nil
}

func (operations *fakeAuditProcessOperations) signalsSnapshot() []unix.Signal {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return append([]unix.Signal(nil), operations.signals...)
}

func TestProductionCancellationRequestedAtDeterministicPreStartGateNeverLaunches(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, "#!/bin/sh\nset -eu\n/bin/sleep 30\n")
	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)

	beforeStart := make(chan struct{})
	releaseStart := make(chan struct{})
	running.server.auditExecutor.hooks = auditExecutorHooks{
		beforeStart: func(string) {
			close(beforeStart)
			<-releaseStart
		},
	}
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := client.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-beforeStart:
	case <-time.After(integrationTestExchangeBudget):
		t.Fatal("audit never reached deterministic pre-start gate")
	}

	type cancellationResult struct {
		acknowledged bool
		err          error
	}
	result := make(chan cancellationResult, 1)
	cancellation := validCancellationForTest(t, fixture.material.fixture.envelope, receipt)
	go func() {
		_, cancelErr := client.Cancel(context.Background(), fixture.material.fixture.envelope, receipt, cancellation)
		result <- cancellationResult{acknowledged: cancelErr == nil, err: cancelErr}
	}()
	waitForRequestedAuditCancellation(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash)
	close(releaseStart)
	select {
	case completed := <-result:
		if completed.err != nil || !completed.acknowledged {
			t.Fatalf("pre-start cancellation = %+v", completed)
		}
	case <-time.After(integrationTestExchangeBudget):
		t.Fatal("pre-start cancellation did not complete")
	}
	_, events := waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateCancelled)
	terminal := events[len(events)-1]
	if terminal.PID != 0 || terminal.PGID != 0 || terminal.ProcessStartIdentity != "" || terminal.FailureClass != "operator_cancelled_before_start" {
		t.Fatalf("pre-start cancellation launched or lost proof: %+v", terminal)
	}
}

func TestProductionCancellationExactCompletedReplayIsByteIdenticalWithoutEffect(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, "#!/bin/sh\nset -eu\n/bin/sleep 30\n")
	running := startInProcessProductionServer(t, fixture.material, now)
	defer running.stop(t)
	operations := &countingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	running.server.auditExecutor.processOperations = operations

	deliveryClient := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	receipt, err := deliveryClient.Deliver(context.Background(), fixture.material.fixture.envelope)
	if err != nil {
		t.Fatal(err)
	}
	waitForAuditState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash, auditStateRunning)

	capture := &wireCapture{}
	config := signedTestConfig(fixture.material.socketPath, int32(os.Getpid()), fixture.material.fixture.bundle, now, nil)
	config.ExpectedPredecessorReleaseIdentity = predecessorReleaseIdentityFromEnvelope(fixture.material.fixture.envelope)
	config.DialContext = capture.dial
	cancelClient, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	cancellation := validCancellationForTest(t, fixture.material.fixture.envelope, receipt)
	if _, err := cancelClient.Cancel(context.Background(), fixture.material.fixture.envelope, receipt, cancellation); err != nil {
		t.Fatal(err)
	}
	requestFrame, responseFrame := capture.snapshot()
	if len(requestFrame) < 4 || len(responseFrame) < 4 || int(binary.BigEndian.Uint32(requestFrame[:4])) != len(requestFrame)-4 ||
		int(binary.BigEndian.Uint32(responseFrame[:4])) != len(responseFrame)-4 {
		t.Fatalf("captured cancellation frames are incomplete: request=%d response=%d", len(requestFrame), len(responseFrame))
	}
	effects := operations.signalCount()
	if effects == 0 {
		t.Fatal("initial cancellation emitted no owned-group signal")
	}

	connection, err := net.DialTimeout("unix", fixture.material.socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetDeadline(time.Now().Add(integrationTestExchangeBudget))
	if _, err := connection.Write(requestFrame); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	if closeWriter, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
	replayed, err := io.ReadAll(connection)
	_ = connection.Close()
	if err != nil || !bytes.Equal(replayed, responseFrame) {
		t.Fatalf("exact cancellation replay equal=%t err=%v\n got=%x\nwant=%x", bytes.Equal(replayed, responseFrame), err, replayed, responseFrame)
	}
	if got := operations.signalCount(); got != effects {
		t.Fatalf("exact replay emitted %d additional process effects", got-effects)
	}

	_, err = deliveryClient.Cancel(context.Background(), fixture.material.fixture.envelope, receipt, cancellation)
	var failure *CancellationFailureError
	if !errors.As(err, &failure) || failure.FailureClass != "cancellation_conflict" {
		t.Fatalf("fresh conflicting cancellation = %v, want authenticated conflict", err)
	}
	if got := operations.signalCount(); got != effects {
		t.Fatalf("conflicting cancellation emitted %d additional process effects", got-effects)
	}
}

type countingAuditProcessOperations struct {
	delegate auditProcessOperations
	mu       sync.Mutex
	signals  int
}

func (operations *countingAuditProcessOperations) Inspect(pid int) (auditProcessIdentity, bool, error) {
	return operations.delegate.Inspect(pid)
}

func (operations *countingAuditProcessOperations) SignalGroup(pgid int, signal unix.Signal) error {
	operations.mu.Lock()
	operations.signals++
	operations.mu.Unlock()
	return operations.delegate.SignalGroup(pgid, signal)
}

func (operations *countingAuditProcessOperations) GroupExists(pgid int) (bool, error) {
	return operations.delegate.GroupExists(pgid)
}

func (operations *countingAuditProcessOperations) signalCount() int {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return operations.signals
}

func TestAuditCancellationBeforeActiveRegistrationCompletesWithoutLaunch(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(material.directory, "cancel-before-active.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	defer journal.Close()
	auditIntent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_cancel_before_active_001",
		EnvelopeHash: testHash("cancel-before-active-envelope"), LaunchSpecHash: material.entry.LaunchSpecHash,
		HandoffID: "remote_handoff_cancel_before_active_001", ReceiptHash: testHash("cancel-before-active-receipt"),
		TaskID: material.entry.TaskID, AttemptCap: material.entry.AttemptCap, PolicyHash: material.entry.PolicyHash,
		RouteMappingHash: material.entry.RouteMappingHash, RepositoryIdentityHash: material.entry.RepositoryIdentityHash,
		GitCommit: material.entry.GitCommit, GitTree: material.entry.GitTree, SourceArchiveSHA256: material.entry.SourceArchiveSHA256,
		WrapperSHA256: material.entry.Wrapper.SHA256, RunID: "audit_run_cancel_before_active_001",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := journal.storeAuditIntent(context.Background(), auditIntent); err != nil {
		t.Fatal(err)
	}
	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	responseBytes := []byte(`{"response":"signed-before-active"}`)
	executor.completeCancellation = func(record auditCancellationRecord, event auditExecutionEvent, outcome, _ string) error {
		_, completeErr := journal.completeAuditCancellation(context.Background(), record.Intent, event, outcome, func() ([]byte, error) {
			return append([]byte(nil), responseBytes...), nil
		})
		return completeErr
	}
	cancellation := mustSealAuditCancellationIntentForTest(t, auditCancellationIntent{
		SchemaVersion: auditCancellationIntentSchemaVersion, IntentID: "audit_cancellation_before_active_001",
		RequestHash: testHash("cancel-before-active-request"), RequestBytes: []byte(`{"operation":"cancel","request":"before-active"}`),
		OperationKey:     operationCancel + ":" + auditIntent.ReceiptHash,
		RequestNonceHash: testHash("cancel-before-active-request-nonce"), ResponseNonceHash: testHash("cancel-before-active-response-nonce"),
		ExclusivityNonceHash: testHash("cancel-before-active-exclusive-nonce"),
		EnvelopeHash:         auditIntent.EnvelopeHash, HandoffID: auditIntent.HandoffID, ReceiptHash: auditIntent.ReceiptHash,
		CancellationHash: testHash("cancel-before-active-cancellation"), Attempt: 1,
		State: auditCancellationStateRequested, RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	record, err := executor.Cancel(auditIntent.EnvelopeHash, func(expected auditProcessIdentity) (auditCancellationRecord, error) {
		if expected.PID != 0 || expected.PGID != 0 || expected.ProcessStartIdentity != "" {
			t.Fatalf("pre-registration cancellation received process identity: %+v", expected)
		}
		return journal.requestAuditCancellation(context.Background(), cancellation)
	})
	if err != nil || record.State != auditCancellationStateCompleted || !bytes.Equal(record.ResponseBytes, responseBytes) {
		t.Fatalf("pre-registration cancellation = %+v, %v", record, err)
	}
	_, events := waitForAuditState(t, journal, auditIntent.EnvelopeHash, auditStateCancelled)
	if terminal := events[len(events)-1]; terminal.PID != 0 || terminal.FailureClass != "operator_cancelled_before_start" {
		t.Fatalf("pre-registration cancellation launched: %+v", terminal)
	}
}

func TestAuditRequestedCancellationResumesAfterRestart(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journalPath := filepath.Join(material.directory, "cancel-restart.sqlite")
	journal, err := openServerJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	auditIntent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_cancel_restart_001",
		EnvelopeHash: testHash("cancel-restart-envelope"), LaunchSpecHash: material.entry.LaunchSpecHash,
		HandoffID: "remote_handoff_cancel_restart_001", ReceiptHash: testHash("cancel-restart-receipt"),
		TaskID: material.entry.TaskID, AttemptCap: material.entry.AttemptCap, PolicyHash: material.entry.PolicyHash,
		RouteMappingHash: material.entry.RouteMappingHash, RepositoryIdentityHash: material.entry.RepositoryIdentityHash,
		GitCommit: material.entry.GitCommit, GitTree: material.entry.GitTree, SourceArchiveSHA256: material.entry.SourceArchiveSHA256,
		WrapperSHA256: material.entry.Wrapper.SHA256, RunID: "audit_run_cancel_restart_001",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := journal.storeAuditIntent(context.Background(), auditIntent); err != nil {
		t.Fatal(err)
	}
	cancellation := mustSealAuditCancellationIntentForTest(t, auditCancellationIntent{
		SchemaVersion: auditCancellationIntentSchemaVersion, IntentID: "audit_cancellation_restart_001",
		RequestHash: testHash("cancel-restart-request"), RequestBytes: []byte(`{"operation":"cancel","request":"restart"}`),
		OperationKey:     operationCancel + ":" + auditIntent.ReceiptHash,
		RequestNonceHash: testHash("cancel-restart-request-nonce"), ResponseNonceHash: testHash("cancel-restart-response-nonce"),
		ExclusivityNonceHash: testHash("cancel-restart-exclusive-nonce"),
		EnvelopeHash:         auditIntent.EnvelopeHash, HandoffID: auditIntent.HandoffID, ReceiptHash: auditIntent.ReceiptHash,
		CancellationHash: testHash("cancel-restart-cancellation"), Attempt: 1,
		State: auditCancellationStateRequested, RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if record, err := journal.requestAuditCancellation(context.Background(), cancellation); err != nil || record.State != auditCancellationStateRequested {
		t.Fatalf("seed requested cancellation = %+v, %v", record, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal, err = openServerJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	defer journal.Close()
	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	responseBytes := []byte(`{"response":"signed-after-restart"}`)
	executor.completeCancellation = func(record auditCancellationRecord, event auditExecutionEvent, outcome, _ string) error {
		_, completeErr := journal.completeAuditCancellation(context.Background(), record.Intent, event, outcome, func() ([]byte, error) {
			return append([]byte(nil), responseBytes...), nil
		})
		return completeErr
	}
	if err := executor.recover(); err != nil {
		t.Fatal(err)
	}
	record := waitForAuditCancellationState(t, journal, auditIntent.EnvelopeHash, auditCancellationStateCompleted)
	if !bytes.Equal(record.ResponseBytes, responseBytes) {
		t.Fatalf("restart response = %q, want %q", record.ResponseBytes, responseBytes)
	}
	_, events := waitForAuditState(t, journal, auditIntent.EnvelopeHash, auditStateCancelled)
	if terminal := events[len(events)-1]; terminal.PID != 0 || terminal.FailureClass != "operator_cancelled_before_start" {
		t.Fatalf("restart cancellation proof = %+v", terminal)
	}
}

func TestAuditRequestedRunningCancellationAfterRestartScrubsAllDerivedTrees(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin process identity contract")
	}
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(material.directory, "recovered-running-cleanup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	defer journal.Close()
	command := exec.Command("/bin/sleep", "30")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	identity, err := inspectAuditProcess(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	processDone := false
	defer func() {
		if !processDone {
			_ = command.Process.Kill()
			select {
			case <-waited:
			case <-time.After(time.Second):
			}
		}
	}()
	intent, cancellation, roots := seedRecoveredCancellationForTest(t, journal, material, "running_cleanup", &identity)
	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	responseBytes := []byte(`{"response":"signed-recovered-cleanup"}`)
	setTestCancellationCompleter(executor, journal, responseBytes)
	if err := executor.recover(); err != nil {
		t.Fatal(err)
	}
	record := waitForAuditCancellationState(t, journal, intent.EnvelopeHash, auditCancellationStateCompleted)
	if record.Outcome != auditCancellationOutcomeCompleted || !bytes.Equal(record.ResponseBytes, responseBytes) {
		t.Fatalf("recovered cancellation = %+v, want completed", record)
	}
	select {
	case <-waited:
		processDone = true
	case <-time.After(2 * time.Second):
		t.Fatal("recovered owned process was not reaped")
	}
	if _, exists, inspectErr := (systemAuditProcessOperations{}).Inspect(identity.PID); inspectErr != nil || exists {
		t.Fatalf("recovered owned PID remains: exists=%t err=%v", exists, inspectErr)
	}
	for _, root := range roots {
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovered cancellation retained %s: %v", root, err)
		}
	}
	_, events := waitForAuditState(t, journal, intent.EnvelopeHash, auditStateCancelled)
	if terminal := events[len(events)-1]; terminal.FailureClass != "operator_cancelled_owned_process" || terminal.PID != identity.PID {
		t.Fatalf("recovered cancellation terminal = %+v", terminal)
	}
	replayed, err := journal.requestAuditCancellation(context.Background(), cancellation)
	if err != nil || replayed.State != auditCancellationStateCompleted || !bytes.Equal(replayed.ResponseBytes, responseBytes) {
		t.Fatalf("exact recovered cancellation replay = %+v, %v", replayed, err)
	}
}

func TestAuditRequestedRunningCancellationKillFailureIsDurableAndReplayable(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(material.directory, "recovered-running-kill-failure.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	defer journal.Close()
	identity := auditProcessIdentity{PID: 5101, PGID: 5101, ProcessStartIdentity: "500:1"}
	intent, cancellation, roots := seedRecoveredCancellationForTest(t, journal, material, "kill_failure", &identity)
	ops := newFakeAuditProcessOperations()
	ops.identity = identity
	ops.killErr = errors.New("injected recovered KILL failure")
	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	executor.processOperations = ops
	executor.terminationBounds = testAuditTerminationBounds()
	responseBytes := []byte(`{"response":"signed-recovered-kill-failure"}`)
	setTestCancellationCompleter(executor, journal, responseBytes)
	if err := executor.recover(); err != nil {
		t.Fatal(err)
	}
	record := waitForAuditCancellationState(t, journal, intent.EnvelopeHash, auditCancellationStateFailed)
	if record.Outcome != auditCancellationOutcomeFailed || !bytes.Equal(record.ResponseBytes, responseBytes) {
		t.Fatalf("recovered kill failure = %+v", record)
	}
	_, events := waitForAuditState(t, journal, intent.EnvelopeHash, auditStateWaitingForHuman)
	terminal := events[len(events)-1]
	if terminal.FailureClass != "kill_signal_failed" || terminal.PID != identity.PID {
		t.Fatalf("recovered kill failure terminal = %+v", terminal)
	}
	for _, event := range events {
		if event.State == auditStateCancelled {
			t.Fatalf("recovered kill failure produced cancellation: %+v", event)
		}
	}
	for _, root := range roots {
		if _, err := os.Lstat(root); err != nil {
			t.Fatalf("kill failure scrubbed active root %s: %v", root, err)
		}
	}
	effects := len(ops.signalsSnapshot())
	replayed, err := journal.requestAuditCancellation(context.Background(), cancellation)
	if err != nil || replayed.State != auditCancellationStateFailed || !bytes.Equal(replayed.ResponseBytes, responseBytes) {
		t.Fatalf("exact kill-failure replay = %+v, %v", replayed, err)
	}
	if got := len(ops.signalsSnapshot()); got != effects {
		t.Fatalf("exact kill-failure replay emitted %d effects", got-effects)
	}
}

func TestAuditRequestedCancellationCleanupFailureRemainsWaitingForHuman(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(material.directory, "recovered-cleanup-failure.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	authority.bind(t, journal)
	defer journal.Close()
	intent, _, roots := seedRecoveredCancellationForTest(t, journal, material, "cleanup_failure", nil)
	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()
	cleanupCalled := false
	executor.hooks.cleanupRecovered = func(auditExecutionIntent, []auditExecutionEvent, executionPolicyEntry) error {
		cleanupCalled = true
		return errors.New("injected recovered cleanup failure")
	}
	setTestCancellationCompleter(executor, journal, []byte(`{"response":"signed-cleanup-failure"}`))
	if err := executor.recover(); err != nil {
		t.Fatal(err)
	}
	record := waitForAuditCancellationState(t, journal, intent.EnvelopeHash, auditCancellationStateFailed)
	if !cleanupCalled || record.Outcome != auditCancellationOutcomeFailed {
		t.Fatalf("cleanup failure outcome = %+v called=%t", record, cleanupCalled)
	}
	_, events := waitForAuditState(t, journal, intent.EnvelopeHash, auditStateWaitingForHuman)
	if terminal := events[len(events)-1]; terminal.FailureClass != "artifact_cleanup_failed" || terminal.PID != 0 {
		t.Fatalf("cleanup failure terminal = %+v", terminal)
	}
	for _, root := range roots {
		if _, err := os.Lstat(root); err != nil {
			t.Fatalf("injected cleanup failure unexpectedly removed %s: %v", root, err)
		}
	}
}

func seedRecoveredCancellationForTest(
	t *testing.T,
	journal *serverJournal,
	material gitArchivePolicyMaterial,
	suffix string,
	identity *auditProcessIdentity,
) (auditExecutionIntent, auditCancellationIntent, []string) {
	t.Helper()
	created := time.Now().UTC().Add(-2 * time.Second)
	intent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_recovered_" + suffix,
		EnvelopeHash: testHash("recovered-envelope-" + suffix), LaunchSpecHash: material.entry.LaunchSpecHash,
		HandoffID: "remote_handoff_recovered_" + suffix, ReceiptHash: testHash("recovered-receipt-" + suffix),
		TaskID: material.entry.TaskID, AttemptCap: material.entry.AttemptCap, PolicyHash: material.entry.PolicyHash,
		RouteMappingHash: material.entry.RouteMappingHash, RepositoryIdentityHash: material.entry.RepositoryIdentityHash,
		GitCommit: material.entry.GitCommit, GitTree: material.entry.GitTree, SourceArchiveSHA256: material.entry.SourceArchiveSHA256,
		WrapperSHA256: material.entry.Wrapper.SHA256, RunID: "audit_run_recovered_" + suffix,
		CreatedAt: created.Format(time.RFC3339Nano),
	})
	if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, material.entry, 1)
	promptHash := auditPromptSHA256(false)
	commandHash, err := auditCommandDescriptorHash(material.entry, promptHash, intent.RunID, auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	if identity != nil {
		prepared := mustSealAuditEventForTest(t, auditExecutionEvent{
			SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditExecutionEventID(intent, 1), IntentHash: intent.IntentHash,
			Sequence: 1, State: auditStatePrepared, Attempt: 1, CommandDescriptorHash: commandHash,
			PromptSHA256: promptHash, SessionRunID: intent.RunID,
			OccurredAt: created.Add(250 * time.Millisecond).Format(time.RFC3339Nano), WorkPath: workPath, OutputPath: outputPath,
			SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
		})
		running := mustSealAuditEventForTest(t, auditExecutionEvent{
			SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditExecutionEventID(intent, 2), IntentHash: intent.IntentHash,
			Sequence: 2, State: auditStateRunning, Attempt: 1, CommandDescriptorHash: commandHash,
			PromptSHA256: promptHash, SessionRunID: intent.RunID,
			OccurredAt: created.Add(time.Second).Format(time.RFC3339Nano), PID: identity.PID, PGID: identity.PGID,
			ProcessStartIdentity: identity.ProcessStartIdentity, ProcessStartedAt: created.Add(500 * time.Millisecond).Format(time.RFC3339Nano),
			WorkPath: workPath, OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
		})
		if err := journal.appendAuditEvent(context.Background(), prepared); err != nil {
			t.Fatal(err)
		}
		if err := journal.appendAuditEvent(context.Background(), running); err != nil {
			t.Fatal(err)
		}
	}
	roots := []string{filepath.Dir(workPath), filepath.Dir(outputPath), sessionPath, filepath.Dir(promptPath), temporaryPath}
	for index, root := range roots {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("credential-%d", index)), []byte("credential-output-artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expected := auditProcessIdentity{}
	if identity != nil {
		expected = *identity
	}
	cancellation := mustSealAuditCancellationIntentForTest(t, auditCancellationIntent{
		SchemaVersion: auditCancellationIntentSchemaVersion, IntentID: "audit_cancellation_recovered_" + suffix,
		RequestHash: testHash("recovered-request-" + suffix), RequestBytes: []byte(`{"operation":"cancel","request":"recovered"}`),
		OperationKey:     operationCancel + ":" + intent.ReceiptHash,
		RequestNonceHash: testHash("recovered-request-nonce-" + suffix), ResponseNonceHash: testHash("recovered-response-nonce-" + suffix),
		ExclusivityNonceHash: testHash("recovered-exclusive-nonce-" + suffix), EnvelopeHash: intent.EnvelopeHash,
		HandoffID: intent.HandoffID, ReceiptHash: intent.ReceiptHash, CancellationHash: testHash("recovered-cancellation-" + suffix), Attempt: 1,
		ExpectedPID: expected.PID, ExpectedPGID: expected.PGID, ExpectedStartIdentity: expected.ProcessStartIdentity,
		State: auditCancellationStateRequested, RequestedAt: created.Add(1500 * time.Millisecond).Format(time.RFC3339Nano),
	})
	if record, err := journal.requestAuditCancellation(context.Background(), cancellation); err != nil || record.State != auditCancellationStateRequested {
		t.Fatalf("seed recovered cancellation = %+v, %v", record, err)
	}
	return intent, cancellation, roots
}

func setTestCancellationCompleter(executor *auditExecutor, journal *serverJournal, responseBytes []byte) {
	executor.completeCancellation = func(record auditCancellationRecord, event auditExecutionEvent, outcome, _ string) error {
		_, err := journal.completeAuditCancellation(context.Background(), record.Intent, event, outcome, func() ([]byte, error) {
			return append([]byte(nil), responseBytes...), nil
		})
		return err
	}
}

func waitForAuditCancellationState(t *testing.T, journal *serverJournal, envelopeHash, state string) auditCancellationRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		record, found, err := journal.loadAuditCancellation(context.Background(), envelopeHash)
		if err == nil && found && record.State == state {
			return record
		}
		if err != nil {
			t.Fatalf("load cancellation state: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancellation did not reach %s; record=%+v found=%t", state, record, found)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForRequestedAuditCancellation(t *testing.T, journal *serverJournal, envelopeHash string) auditCancellationRecord {
	t.Helper()
	deadline := time.Now().Add(integrationTestExchangeBudget)
	for {
		record, found, err := journal.loadAuditCancellation(context.Background(), envelopeHash)
		if err == nil && found {
			return record
		}
		if err != nil {
			t.Fatalf("load requested cancellation: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("cancellation intent was not durably requested")
		}
		time.Sleep(time.Millisecond)
	}
}

func mustSealAuditCancellationIntentForTest(t *testing.T, intent auditCancellationIntent) auditCancellationIntent {
	t.Helper()
	sealed, err := sealAuditCancellationIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
