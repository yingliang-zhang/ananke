package trustedsupervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSupervisorAuditTestTimeoutConfirmsTERMExit(t *testing.T) {
	fixture := newSupervisorTerminationFixture(t, `#!/bin/sh
set -eu
trap 'exit 0' TERM
ready_staging="$TMPDIR/.ready.$$"
: > "$ready_staging"
/bin/mv "$ready_staging" "$TMPDIR/ready"
while :; do :; done
`)
	operations := &recordingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	timeout := make(chan time.Time, 1)
	var identity auditProcessIdentity
	results, err := runSupervisorAuditTests(context.Background(), fixture.material.policy, fixture.material.entry, fixture.snapshot, fixture.invocation, auditSupervisorTestHooks{
		ProcessOperations: operations,
		TerminationBounds: testAuditTerminationBounds(),
		HardTimeout:       timeout,
		AfterStart: func(started auditProcessIdentity) error {
			identity = started
			if err := waitForSupervisorTestMarker(fixture.readyPath, started); err != nil {
				return err
			}
			timeout <- time.Now()
			return nil
		},
	})
	processResult, hasProcessResult := auditProcessResultFromError(err)
	var ownedFailure *auditOwnedProcessError
	if !errors.Is(err, ErrDeadline) || results != nil || !hasProcessResult || errors.As(err, &ownedFailure) ||
		processResult.PID != identity.PID || processResult.PGID != identity.PGID || processResult.ProcessStartIdentity != identity.ProcessStartIdentity ||
		processResult.StartedAt != identity.StartedAt || processResult.FinishedAt == "" || processResult.ExitCode != 0 || processResult.processWaiter != nil {
		t.Fatalf("timed-out supervisor test = %+v, %v; want confirmed TERM exit/reap", results, err)
	}
	if got := operations.signalsSnapshot(); len(got) != 1 || got[0] != unix.SIGTERM {
		t.Fatalf("supervisor test signals = %v, want TERM", got)
	}
	assertSupervisorProcessGone(t, identity)
}

func TestSupervisorAuditTestTimeoutEscalatesIgnoredTERMToKILL(t *testing.T) {
	fixture := newSupervisorTerminationFixture(t, supervisorIgnoredTERMScript)
	operations := &recordingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	timeout := make(chan time.Time, 1)
	var identity auditProcessIdentity
	results, err := runSupervisorAuditTests(context.Background(), fixture.material.policy, fixture.material.entry, fixture.snapshot, fixture.invocation, auditSupervisorTestHooks{
		ProcessOperations: operations,
		TerminationBounds: testAuditTerminationBounds(),
		HardTimeout:       timeout,
		AfterStart: func(started auditProcessIdentity) error {
			identity = started
			if err := waitForSupervisorTestMarker(fixture.readyPath, started); err != nil {
				return err
			}
			timeout <- time.Now()
			return nil
		},
	})
	if err == nil || results != nil {
		t.Fatalf("ignored-TERM supervisor test = %+v, %v; want closed failure", results, err)
	}
	if got := operations.signalsSnapshot(); len(got) != 2 || got[0] != unix.SIGTERM || got[1] != unix.SIGKILL {
		t.Fatalf("supervisor test signals = %v, want TERM then KILL", got)
	}
	assertSupervisorProcessGone(t, identity)
}

func TestSupervisorAuditTestKillErrorReturnsOwnedTypedFailure(t *testing.T) {
	fixture := newSupervisorTerminationFixture(t, supervisorIgnoredTERMScript)
	operations := &recordingAuditProcessOperations{delegate: systemAuditProcessOperations{}, killErr: errors.New("injected supervisor test kill failure")}
	timeout := make(chan time.Time, 1)
	var identity auditProcessIdentity
	results, err := runSupervisorAuditTests(context.Background(), fixture.material.policy, fixture.material.entry, fixture.snapshot, fixture.invocation, auditSupervisorTestHooks{
		ProcessOperations: operations,
		TerminationBounds: testAuditTerminationBounds(),
		HardTimeout:       timeout,
		AfterStart: func(started auditProcessIdentity) error {
			identity = started
			if err := waitForSupervisorTestMarker(fixture.readyPath, started); err != nil {
				return err
			}
			timeout <- time.Now()
			return nil
		},
	})
	failure := requireOwnedSupervisorFailure(t, err, "kill_signal_failed")
	if results != nil || !sameAuditProcessIdentity(failure.identity, identity) || failure.identity.StartedAt == "" {
		t.Fatalf("owned kill failure lost exact identity: results=%+v failure=%+v started=%+v", results, failure, identity)
	}
	killAndJoinOwnedSupervisorFailure(t, failure)
}

func TestSupervisorAuditTestWaitTimeoutReturnsOwnedTypedFailure(t *testing.T) {
	fixture := newSupervisorTerminationFixture(t, supervisorIgnoredTERMScript)
	operations := &vanishingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	timeout := make(chan time.Time, 1)
	var identity auditProcessIdentity
	started := time.Now()
	results, err := runSupervisorAuditTests(context.Background(), fixture.material.policy, fixture.material.entry, fixture.snapshot, fixture.invocation, auditSupervisorTestHooks{
		ProcessOperations: operations,
		TerminationBounds: testAuditTerminationBounds(),
		HardTimeout:       timeout,
		AfterStart: func(observed auditProcessIdentity) error {
			identity = observed
			if err := waitForSupervisorTestMarker(fixture.readyPath, observed); err != nil {
				return err
			}
			timeout <- time.Now()
			return nil
		},
	})
	failure := requireOwnedSupervisorFailure(t, err, "process_wait_unconfirmed")
	if results != nil || !sameAuditProcessIdentity(failure.identity, identity) {
		t.Fatalf("owned wait failure lost exact identity: results=%+v failure=%+v started=%+v", results, failure, identity)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("supervisor test wait failure was unbounded: %v", elapsed)
	}
	killAndJoinOwnedSupervisorFailure(t, failure)
}

func TestSupervisorAuditTestContextShutdownUsesOwnedTermination(t *testing.T) {
	fixture := newSupervisorTerminationFixture(t, `#!/bin/sh
set -eu
: > "$TMPDIR/ready"
exec /bin/sleep 30
`)
	operations := &recordingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	ctx, cancel := context.WithCancel(context.Background())
	var identity auditProcessIdentity
	results, err := runSupervisorAuditTests(ctx, fixture.material.policy, fixture.material.entry, fixture.snapshot, fixture.invocation, auditSupervisorTestHooks{
		ProcessOperations: operations,
		TerminationBounds: testAuditTerminationBounds(),
		AfterStart: func(started auditProcessIdentity) error {
			identity = started
			if err := waitForSupervisorTestMarker(fixture.readyPath, started); err != nil {
				return err
			}
			cancel()
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) || results != nil {
		t.Fatalf("context-cancelled supervisor test = %+v, %v; want context cancellation", results, err)
	}
	if got := operations.signalsSnapshot(); len(got) != 1 || got[0] != unix.SIGTERM {
		t.Fatalf("context shutdown signals = %v, want TERM", got)
	}
	assertSupervisorProcessGone(t, identity)
}

func TestSupervisorAuditTestIdentityFailureIsTypedAndBounded(t *testing.T) {
	fixture := newSupervisorTerminationFixture(t, "#!/bin/sh\nexec /bin/sleep 0.05\n")
	operations := &firstInspectFailingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	started := time.Now()
	results, err := runSupervisorAuditTests(context.Background(), fixture.material.policy, fixture.material.entry, fixture.snapshot, fixture.invocation, auditSupervisorTestHooks{
		ProcessOperations: operations,
		TerminationBounds: testAuditTerminationBounds(),
	})
	failure := requireOwnedSupervisorFailure(t, err, "process_identity_unavailable")
	if results != nil || failure.identity.PID <= 0 || failure.identity.PGID != failure.identity.PID || failure.identity.ProcessStartIdentity != "" {
		t.Fatalf("identity-capture failure result = %+v, %+v", results, failure)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("identity-capture failure was unbounded: %v", elapsed)
	}
	killAndJoinOwnedSupervisorFailure(t, failure)
}

func TestUnconfirmedAuditInvocationJoinIsBoundedReplayableAndRecoverable(t *testing.T) {
	identity := auditProcessIdentity{PID: 4911, PGID: 4911, ProcessStartIdentity: "900:11", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	operations := newFakeAuditProcessOperations()
	operations.identity = identity
	waiter := newAuditProcessWaiter()
	result := auditInvocationResult{
		PID: identity.PID, PGID: identity.PGID, ProcessStartIdentity: identity.ProcessStartIdentity, StartedAt: identity.StartedAt,
		processWaiter: waiter,
	}
	started := time.Now()
	first := joinUnconfirmedAuditInvocation(context.Background(), result, operations, testAuditTerminationBounds())
	var termination *auditTerminationError
	if first.Outcome != auditTerminationFailure || !errors.As(first.Failure, &termination) || termination.FailureClass != "process_wait_unconfirmed" {
		t.Fatalf("bounded unconfirmed join = %+v, want process_wait_unconfirmed", first)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("unconfirmed join was unbounded: %v", elapsed)
	}
	operations.mu.Lock()
	operations.leaderExists = false
	operations.groupExists = false
	operations.mu.Unlock()
	waiter.complete(nil)
	for attempt := range 2 {
		joined := joinUnconfirmedAuditInvocation(context.Background(), result, operations, testAuditTerminationBounds())
		if joined.Outcome != auditTerminationConfirmedExit || joined.Failure != nil {
			t.Fatalf("recoverable/replayed join attempt %d = %+v", attempt+1, joined)
		}
	}
}

func TestProductionServerSupervisorTestTerminationFailureRetainsResourcesUntilCloseRetry(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin process identity contract")
	}
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newExecutingServerTestMaterial(t, now, fakeAuditOMPFixture{Scenario: "report", Output: validAuditModelReportJSONForTest})
	testRoot := filepath.Join(fixture.material.directory, "supervisor-test-failure-bin")
	if err := os.Mkdir(testRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	testPath := filepath.Join(testRoot, "ignore-term.sh")
	if err := os.WriteFile(testPath, []byte(supervisorIgnoredTERMScript), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.entry.AllowedTests = []executionPolicyTestCommand{executionPolicyTestCommandForTest(t, "focused_go_test", testPath)}
	fixture.entry = mustSealExecutionPolicyEntryForTest(t, fixture.entry)
	writeExecutionPolicyFileForTest(t, fixture.material.executionPolicyPath, []executionPolicyEntry{fixture.entry})
	running := startInProcessProductionServer(t, fixture.material, now)
	cleanupPID := 0
	t.Cleanup(func() {
		if cleanupPID > 0 {
			_ = unix.Kill(-cleanupPID, unix.SIGKILL)
		}
		running.cancel()
		_ = running.server.Close()
	})
	operations := &recordingAuditProcessOperations{delegate: systemAuditProcessOperations{}, killErr: errors.New("injected supervisor test kill failure")}
	running.server.auditExecutor.processOperations = operations
	running.server.auditExecutor.terminationBounds = testAuditTerminationBounds()
	testTimeout := make(chan time.Time, 1)
	testStarted := make(chan auditProcessIdentity, 1)
	testReady := make(chan error, 1)
	readyPath := filepath.Join(fixture.entry.TemporaryRoot,
		"audit_run_"+hashIDFragment(fixture.material.fixture.envelope.EnvelopeHash)+"_attempt_1", "supervisor_tests", "ready")
	running.server.auditExecutor.hooks.supervisorTestHardTimeout = testTimeout
	running.server.auditExecutor.hooks.supervisorTestAfterStart = func(identity auditProcessIdentity) {
		testStarted <- identity
		deadline := time.Now().Add(time.Second)
		for {
			if _, err := os.Lstat(readyPath); err == nil {
				testReady <- nil
				testTimeout <- time.Now()
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				testReady <- err
				return
			}
			if time.Now().After(deadline) {
				testReady <- errors.New("supervisor test did not install TERM handler")
				return
			}
			time.Sleep(time.Millisecond)
		}
	}
	privateKeyAlias := running.server.material.privateKey
	client := newServerTestClient(t, fixture.material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), fixture.material.fixture.envelope); err != nil {
		t.Fatal(err)
	}
	var supervisorIdentity auditProcessIdentity
	select {
	case supervisorIdentity = <-testStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("valid canonical wrapper never reached supervisor-owned test")
	}
	_, events := waitForAnyAuditTerminalState(t, running.server.journal, fixture.material.fixture.envelope.EnvelopeHash)
	terminal := events[len(events)-1]
	cleanupPID = supervisorIdentity.PGID
	if terminal.FailureClass != "kill_signal_failed" {
		t.Fatalf("supervisor test termination failure = %+v, want typed kill_signal_failed", terminal)
	}
	if len(events) < 2 || terminal.PID != events[1].PID || terminal.PGID != events[1].PGID ||
		terminal.ProcessStartIdentity != events[1].ProcessStartIdentity {
		t.Fatalf("durable failure lost parent wrapper history continuity: %+v", events)
	}
	select {
	case err := <-testReady:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor test TERM handler readiness was not observed")
	}
	for _, event := range events {
		if event.State == auditStateCompleted || event.State == auditStateCancelled || event.State == auditStateTimedOut ||
			event.EvidenceJSON != "" || event.EvidenceHash != "" {
			t.Fatalf("supervisor test termination failure produced false terminal authority: %+v", event)
		}
	}
	if _, err := inspectAuditProcess(supervisorIdentity.PID); err != nil {
		t.Fatalf("injected kill failure did not retain exact owned supervisor test for retry: identity=%+v err=%v", supervisorIdentity, err)
	}
	started := time.Now()
	if err := running.server.Close(); !errors.Is(err, ErrDeadline) {
		t.Fatalf("stuck supervisor-test Close error = %v, want %v", err, ErrDeadline)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stuck supervisor-test Close was unbounded: %v", elapsed)
	}
	if bytes.Count(privateKeyAlias, []byte{0}) == len(privateKeyAlias) {
		t.Fatal("private key was zeroed while supervisor test remained unjoined")
	}
	if err := running.server.journal.db.Ping(); err != nil {
		t.Fatalf("journal closed while supervisor test remained unjoined: %v", err)
	}
	if running.server.executionPolicy == nil || running.server.repositoryPolicy == nil {
		t.Fatal("policies released while supervisor test remained unjoined")
	}
	assertAuditExecutionRootsRetained(t, fixture.entry)
	_ = unix.Kill(-supervisorIdentity.PGID, unix.SIGKILL)
	cleanupPID = 0
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := running.server.Close(); err == nil {
			break
		} else if !errors.Is(err, ErrDeadline) {
			t.Fatalf("retry Close after supervisor-test reap: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("Close retry did not reap supervisor test and release resources")
		}
		time.Sleep(time.Millisecond)
	}
	assertZeroedLifecycleAlias(t, privateKeyAlias, "private key after supervisor-test Close retry")
	assertAuditExecutionRootsEmpty(t, fixture.entry)
}

const supervisorIgnoredTERMScript = `#!/bin/sh
set -eu
trap '' TERM
: > "$TMPDIR/ready"
exec /bin/sleep 30
`

type supervisorTerminationFixture struct {
	material   gitArchivePolicyMaterial
	snapshot   auditSnapshot
	invocation auditInvocation
	readyPath  string
}

func newSupervisorTerminationFixture(t *testing.T, script string) supervisorTerminationFixture {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin process identity contract")
	}
	material := newGitArchivePolicyMaterial(t)
	testRoot := filepath.Join(material.directory, "supervisor-termination-bin")
	if err := os.Mkdir(testRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	testPath := filepath.Join(testRoot, "supervisor-termination-test.sh")
	if err := os.WriteFile(testPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := sealExecutionPolicyTestCommand(executionPolicyTestCommand{
		ID: "focused_go_test", Executable: fileIdentityForTest(t, testPath), ExecutableRoot: directoryIdentityForTest(t, testRoot), TimeoutSeconds: 5,
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
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_supervisor_termination_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_supervisor_termination_attempt_1", "audit_run_supervisor_termination_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	return supervisorTerminationFixture{
		material: material, snapshot: snapshot, invocation: invocation,
		readyPath: filepath.Join(invocation.TemporaryDir, "supervisor_tests", "ready"),
	}
}

func waitForSupervisorTestMarker(path string, expected auditProcessIdentity) error {
	if path == "" || expected.PID <= 0 || expected.PGID != expected.PID || expected.ProcessStartIdentity == "" {
		return ErrProtocol
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	operations := systemAuditProcessOperations{}
	for {
		if _, err := os.Lstat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect supervisor test marker: %w", err)
		}
		observed, exists, err := operations.Inspect(expected.PID)
		if err != nil {
			return fmt.Errorf("inspect supervisor test process before marker: %w", err)
		}
		if !exists {
			return fmt.Errorf("supervisor test process %d exited before publishing marker %s", expected.PID, path)
		}
		if !sameAuditProcessIdentity(observed, expected) {
			return fmt.Errorf("supervisor test process identity changed before publishing marker %s: expected=%+v observed=%+v", path, expected, observed)
		}
		select {
		case <-timer.C:
			return fmt.Errorf("supervisor test process %d remained live but did not publish marker %s before bounded deadline", expected.PID, path)
		case <-ticker.C:
		}
	}
}

func requireOwnedSupervisorFailure(t *testing.T, err error, failureClass string) *auditOwnedProcessError {
	t.Helper()
	var owned *auditOwnedProcessError
	var termination *auditTerminationError
	if !errors.As(err, &owned) || !errors.As(err, &termination) || termination.FailureClass != failureClass || owned.waiter == nil {
		t.Fatalf("supervisor test error = %v, want owned typed %s", err, failureClass)
	}
	return owned
}

func killAndJoinOwnedSupervisorFailure(t *testing.T, failure *auditOwnedProcessError) {
	t.Helper()
	if failure == nil || failure.identity.PGID <= 0 || failure.waiter == nil {
		t.Fatal("invalid owned supervisor failure cleanup")
	}
	if err := unix.Kill(-failure.identity.PGID, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		t.Fatalf("kill retained supervisor test: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := failure.waiter.await(ctx, time.Second); err != nil {
		t.Fatalf("join retained supervisor test: %v", err)
	}
}

func assertSupervisorProcessGone(t *testing.T, identity auditProcessIdentity) {
	t.Helper()
	if identity.PID <= 0 || identity.PGID != identity.PID || identity.ProcessStartIdentity == "" || identity.StartedAt == "" {
		t.Fatalf("invalid recorded supervisor process identity: %+v", identity)
	}
	if _, exists, err := (systemAuditProcessOperations{}).Inspect(identity.PID); err != nil || exists {
		t.Fatalf("supervisor test leader remains after confirmed reap: exists=%t err=%v", exists, err)
	}
	if exists, err := (systemAuditProcessOperations{}).GroupExists(identity.PGID); err != nil || exists {
		t.Fatalf("supervisor test group remains after confirmed reap: exists=%t err=%v", exists, err)
	}
}

func assertAuditExecutionRootsRetained(t *testing.T, entry executionPolicyEntry) {
	t.Helper()
	for _, root := range []string{entry.PromptRoot, entry.OutputRoot, entry.SessionRoot, entry.WorkRoot, entry.TemporaryRoot} {
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) == 0 {
			t.Fatalf("owned audit root released before child join %s: entries=%d err=%v", root, len(entries), err)
		}
	}
}

type recordingAuditProcessOperations struct {
	delegate auditProcessOperations
	killErr  error
	mu       sync.Mutex
	signals  []unix.Signal
}

func (operations *recordingAuditProcessOperations) Inspect(pid int) (auditProcessIdentity, bool, error) {
	return operations.delegate.Inspect(pid)
}

func (operations *recordingAuditProcessOperations) SignalGroup(pgid int, signal unix.Signal) error {
	operations.mu.Lock()
	operations.signals = append(operations.signals, signal)
	operations.mu.Unlock()
	if signal == unix.SIGKILL && operations.killErr != nil {
		return operations.killErr
	}
	return operations.delegate.SignalGroup(pgid, signal)
}

func (operations *recordingAuditProcessOperations) GroupExists(pgid int) (bool, error) {
	return operations.delegate.GroupExists(pgid)
}

func (operations *recordingAuditProcessOperations) signalsSnapshot() []unix.Signal {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return append([]unix.Signal(nil), operations.signals...)
}

type vanishingAuditProcessOperations struct {
	delegate auditProcessOperations
	mu       sync.Mutex
	vanished bool
}

func (operations *vanishingAuditProcessOperations) Inspect(pid int) (auditProcessIdentity, bool, error) {
	operations.mu.Lock()
	vanished := operations.vanished
	operations.mu.Unlock()
	if vanished {
		return auditProcessIdentity{}, false, nil
	}
	return operations.delegate.Inspect(pid)
}

func (operations *vanishingAuditProcessOperations) SignalGroup(_ int, signal unix.Signal) error {
	if signal != unix.SIGTERM {
		return errors.New("unexpected signal after synthetic disappearance")
	}
	operations.mu.Lock()
	operations.vanished = true
	operations.mu.Unlock()
	return nil
}

func (operations *vanishingAuditProcessOperations) GroupExists(pgid int) (bool, error) {
	operations.mu.Lock()
	vanished := operations.vanished
	operations.mu.Unlock()
	if vanished {
		return false, nil
	}
	return operations.delegate.GroupExists(pgid)
}

type firstInspectFailingAuditProcessOperations struct {
	delegate auditProcessOperations
	once     sync.Once
}

func (operations *firstInspectFailingAuditProcessOperations) Inspect(pid int) (identity auditProcessIdentity, exists bool, err error) {
	failed := false
	operations.once.Do(func() { failed = true })
	if failed {
		return auditProcessIdentity{}, false, errors.New("injected supervisor identity failure")
	}
	return operations.delegate.Inspect(pid)
}

func (operations *firstInspectFailingAuditProcessOperations) SignalGroup(pgid int, signal unix.Signal) error {
	return operations.delegate.SignalGroup(pgid, signal)
}

func (operations *firstInspectFailingAuditProcessOperations) GroupExists(pgid int) (bool, error) {
	return operations.delegate.GroupExists(pgid)
}

func waitForAnyAuditTerminalState(t *testing.T, journal *serverJournal, envelopeHash string) (auditExecutionIntent, []auditExecutionEvent) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		intent, events, err := journal.loadAuditExecution(context.Background(), envelopeHash)
		if err == nil && len(events) != 0 {
			last := events[len(events)-1]
			if last.State != auditStatePrepared && last.State != auditStateRunning && last.State != auditStateTimedOut {
				return intent, events
			}
		}
		if err != nil && !errorsIsAuditNotReady(err) {
			t.Fatalf("load audit terminal state: %v", err)
		}
		if time.Now().After(deadline) {
			states := make([]string, 0, len(events))
			for _, event := range events {
				states = append(states, event.State+":"+event.FailureClass)
			}
			t.Fatalf("audit did not persist a terminal state; states=%v err=%v", states, err)
		}
		time.Sleep(time.Millisecond)
	}
}
