package trustedsupervisor

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestP5ResidualExitWaitsForNaturalDisappearanceWithoutSignal(t *testing.T) {
	identity := auditProcessIdentity{PID: 4201, PGID: 4201, ProcessStartIdentity: "300:1"}
	for _, testCase := range []struct {
		name    string
		waitErr error
	}{
		{name: "normal exit"},
		{name: "nonzero exit", waitErr: &exec.ExitError{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			waiter := newAuditProcessWaiter()
			waiter.complete(testCase.waitErr)
			operations := &residualAuditProcessOperations{
				expected:     identity,
				groupResults: []bool{true, true, false},
			}

			result := confirmAuditProcessExit(context.Background(), identity, waiter, operations, p5ResidualTestBounds(80*time.Millisecond))
			if result.Outcome != auditTerminationConfirmedExit || result.Failure != nil {
				t.Fatalf("natural residual exit = %+v, want confirmed exit", result)
			}
			if signals := operations.signalsSnapshot(); len(signals) != 0 {
				t.Fatalf("natural residual exit emitted signals: %v", signals)
			}
			_, groupInspections := operations.inspectionCounts()
			if groupInspections < 3 {
				t.Fatalf("group inspections = %d, want polling through disappearance", groupInspections)
			}
		})
	}
}

func TestP5ResidualExitRemainingGroupIsBoundedAndUnsignaled(t *testing.T) {
	identity := auditProcessIdentity{PID: 4202, PGID: 4202, ProcessStartIdentity: "300:2"}
	waiter := newAuditProcessWaiter()
	waiter.complete(nil)
	operations := &residualAuditProcessOperations{expected: identity, groupResults: []bool{true}}
	bounds := p5ResidualTestBounds(30 * time.Millisecond)

	started := time.Now()
	result := confirmAuditProcessExit(context.Background(), identity, waiter, operations, bounds)
	elapsed := time.Since(started)
	requireP5ResidualTerminationClass(t, result, "group_exit_unconfirmed")
	if elapsed < bounds.ResidualExitGrace/2 || elapsed > 500*time.Millisecond {
		t.Fatalf("residual observation elapsed = %v, want bounded wait near %v", elapsed, bounds.ResidualExitGrace)
	}
	if signals := operations.signalsSnapshot(); len(signals) != 0 {
		t.Fatalf("remaining residual group emitted signals: %v", signals)
	}
}

func TestP5ResidualExitGroupInspectionFailureIsTypedAndUnsignaled(t *testing.T) {
	identity := auditProcessIdentity{PID: 4203, PGID: 4203, ProcessStartIdentity: "300:3"}
	waiter := newAuditProcessWaiter()
	waiter.complete(nil)
	operations := &residualAuditProcessOperations{expected: identity, groupErr: errors.New("injected group inspection failure")}

	result := confirmAuditProcessExit(context.Background(), identity, waiter, operations, p5ResidualTestBounds(30*time.Millisecond))
	requireP5ResidualTerminationClass(t, result, "group_inspection_failed")
	if signals := operations.signalsSnapshot(); len(signals) != 0 {
		t.Fatalf("group inspection failure emitted signals: %v", signals)
	}
}

func TestP5ResidualExitRequiresCompletedValidWaitBeforeInspection(t *testing.T) {
	identity := auditProcessIdentity{PID: 4204, PGID: 4204, ProcessStartIdentity: "300:4"}
	for _, testCase := range []struct {
		name         string
		completeWait bool
		waitErr      error
		failureClass string
	}{
		{name: "incomplete", failureClass: "process_wait_unconfirmed"},
		{name: "wait error", completeWait: true, waitErr: errors.New("injected wait failure"), failureClass: "process_wait_failed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			waiter := newAuditProcessWaiter()
			if testCase.completeWait {
				waiter.complete(testCase.waitErr)
			}
			operations := &residualAuditProcessOperations{expected: identity, groupResults: []bool{true, false}}

			result := confirmAuditProcessExit(context.Background(), identity, waiter, operations, p5ResidualTestBounds(30*time.Millisecond))
			requireP5ResidualTerminationClass(t, result, testCase.failureClass)
			inspectCalls, groupCalls := operations.inspectionCounts()
			if inspectCalls != 0 || groupCalls != 0 {
				t.Fatalf("invalid waiter triggered residual proof: inspect=%d group=%d", inspectCalls, groupCalls)
			}
			if signals := operations.signalsSnapshot(); len(signals) != 0 {
				t.Fatalf("invalid waiter emitted signals: %v", signals)
			}
		})
	}
}

func TestP5ResidualExitMissingWaiterAllowsOnlyImmediateAbsentGroupProof(t *testing.T) {
	identity := auditProcessIdentity{PID: 4209, PGID: 4209, ProcessStartIdentity: "300:9"}
	for _, testCase := range []struct {
		name         string
		groupResults []bool
		confirmed    bool
	}{
		{name: "already absent", groupResults: []bool{false}, confirmed: true},
		{name: "present cannot use residual polling", groupResults: []bool{true, false}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			operations := &residualAuditProcessOperations{expected: identity, groupResults: testCase.groupResults}
			result := confirmAuditProcessExit(context.Background(), identity, nil, operations, p5ResidualTestBounds(30*time.Millisecond))
			if testCase.confirmed {
				if result.Outcome != auditTerminationConfirmedExit || result.Failure != nil {
					t.Fatalf("missing-waiter absent group = %+v, want confirmed exit", result)
				}
			} else {
				requireP5ResidualTerminationClass(t, result, "group_exit_unconfirmed")
			}
			inspectCalls, groupCalls := operations.inspectionCounts()
			if inspectCalls != 1 || groupCalls != 1 {
				t.Fatalf("missing-waiter proof inspections = %d/%d, want one immediate leader/group check", inspectCalls, groupCalls)
			}
			if signals := operations.signalsSnapshot(); len(signals) != 0 {
				t.Fatalf("missing-waiter proof emitted signals: %v", signals)
			}
		})
	}
}

func TestP5ResidualExitContextCancellationIsTypedAndUnsignaled(t *testing.T) {
	identity := auditProcessIdentity{PID: 4205, PGID: 4205, ProcessStartIdentity: "300:5"}
	waiter := newAuditProcessWaiter()
	waiter.complete(nil)
	operations := &residualAuditProcessOperations{expected: identity, groupResults: []bool{true}}
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()

	result := confirmAuditProcessExit(ctx, identity, waiter, operations, p5ResidualTestBounds(100*time.Millisecond))
	requireP5ResidualTerminationClass(t, result, "group_exit_unconfirmed")
	if signals := operations.signalsSnapshot(); len(signals) != 0 {
		t.Fatalf("cancelled residual observation emitted signals: %v", signals)
	}
}

func TestP5ResidualExitJoinUsesNaturalObservationWithoutSignal(t *testing.T) {
	identity := auditProcessIdentity{PID: 4206, PGID: 4206, ProcessStartIdentity: "300:6", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	waiter := newAuditProcessWaiter()
	waiter.complete(nil)
	operations := &residualAuditProcessOperations{expected: identity, groupResults: []bool{true, false}}
	result := auditInvocationResult{
		PID: identity.PID, PGID: identity.PGID, ProcessStartIdentity: identity.ProcessStartIdentity, StartedAt: identity.StartedAt,
		processWaiter: waiter,
	}

	joined := joinUnconfirmedAuditInvocation(context.Background(), result, operations, p5ResidualTestBounds(80*time.Millisecond))
	if joined.Outcome != auditTerminationConfirmedExit || joined.Failure != nil {
		t.Fatalf("natural residual join = %+v, want confirmed exit", joined)
	}
	if signals := operations.signalsSnapshot(); len(signals) != 0 {
		t.Fatalf("natural residual join emitted signals: %v", signals)
	}
}

func TestP5ResidualExitGraceIsPositiveBoundedAndDefaulted(t *testing.T) {
	defaults := defaultAuditTerminationBounds()
	if defaults.ResidualExitGrace != 5*time.Second {
		t.Fatalf("default residual exit grace = %v, want 5s", defaults.ResidualExitGrace)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*auditTerminationBounds)
	}{
		{name: "zero", mutate: func(bounds *auditTerminationBounds) { bounds.ResidualExitGrace = 0 }},
		{name: "over maximum", mutate: func(bounds *auditTerminationBounds) { bounds.ResidualExitGrace = 31 * time.Second }},
		{name: "poll exceeds residual", mutate: func(bounds *auditTerminationBounds) {
			bounds.PollInterval = bounds.ResidualExitGrace + time.Millisecond
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bounds := p5ResidualTestBounds(30 * time.Millisecond)
			testCase.mutate(&bounds)
			identity := auditProcessIdentity{PID: 4207, PGID: 4207, ProcessStartIdentity: "300:7"}
			operations := &residualAuditProcessOperations{expected: identity, leaderExists: true, groupResults: []bool{true}}
			waiter := newAuditProcessWaiter()
			waiter.complete(nil)

			result := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
			if result.Outcome != auditTerminationFailure || !errors.Is(result.Failure, ErrProtocol) {
				t.Fatalf("invalid residual bounds result = %+v, want protocol failure", result)
			}
			inspectCalls, groupCalls := operations.inspectionCounts()
			if inspectCalls != 0 || groupCalls != 0 || len(operations.signalsSnapshot()) != 0 {
				t.Fatalf("invalid residual bounds performed process operations: inspect=%d group=%d signals=%v", inspectCalls, groupCalls, operations.signalsSnapshot())
			}
		})
	}
}

func TestP5ResidualExitCloseBudgetIncludesGraceForConcurrentCallers(t *testing.T) {
	identity := auditProcessIdentity{PID: 4208, PGID: 4208, ProcessStartIdentity: "300:8", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	bounds := p5ResidualTestBounds(350 * time.Millisecond)
	operations := &residualAuditProcessOperations{expected: identity, groupLifetime: 280 * time.Millisecond}
	waiter := newAuditProcessWaiter()
	waiter.complete(nil)
	rootCtx, rootCancel := context.WithCancel(context.Background())
	activeCtx, activeCancel := context.WithCancel(rootCtx)
	active := &activeAuditExecution{ctx: activeCtx, cancel: activeCancel, done: make(chan struct{})}
	close(active.done)
	var cleanupMu sync.Mutex
	cleanupCalls := 0
	active.pending = &pendingAuditProcess{
		result: auditInvocationResult{
			PID: identity.PID, PGID: identity.PGID, ProcessStartIdentity: identity.ProcessStartIdentity, StartedAt: identity.StartedAt,
			processWaiter: waiter,
		},
		cleanup: func() error {
			cleanupMu.Lock()
			cleanupCalls++
			cleanupMu.Unlock()
			return nil
		},
	}
	executor := &auditExecutor{
		ctx: rootCtx, cancel: rootCancel, active: map[string]*activeAuditExecution{"residual": active},
		processOperations: operations, terminationBounds: bounds,
	}

	const closers = 8
	start := make(chan struct{})
	results := make(chan error, closers)
	started := time.Now()
	for range closers {
		go func() {
			<-start
			results <- executor.Close()
		}()
	}
	close(start)
	for index := range closers {
		if err := <-results; err != nil {
			t.Fatalf("concurrent Close %d = %v, want nil", index, err)
		}
	}
	if elapsed := time.Since(started); elapsed < 250*time.Millisecond || elapsed > time.Second {
		t.Fatalf("concurrent Close elapsed = %v, want residual wait within extended bound", elapsed)
	}
	cleanupMu.Lock()
	gotCleanupCalls := cleanupCalls
	cleanupMu.Unlock()
	if gotCleanupCalls != 1 {
		t.Fatalf("concurrent Close cleanup calls = %d, want 1", gotCleanupCalls)
	}
	if signals := operations.signalsSnapshot(); len(signals) != 0 {
		t.Fatalf("concurrent residual Close emitted signals: %v", signals)
	}
}

func TestP5ResidualExitDarwinWaitsForShortLivedChildWithoutSignal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin process-group identity contract")
	}
	command := exec.Command("/bin/sh", "-c", "IFS= read -r line\n/bin/sleep 0.3 &\nexit 0\n")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := startAuditProcessWaiter(command)
	operations := &recordingAuditProcessOperations{delegate: systemAuditProcessOperations{}}
	identity, exists, err := operations.Inspect(command.Process.Pid)
	if err != nil || !exists || identity.PID != command.Process.Pid || identity.PGID != identity.PID || identity.ProcessStartIdentity == "" {
		t.Fatalf("capture residual leader identity = %+v exists=%t err=%v", identity, exists, err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = waiter.await(ctx, time.Second)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			groupExists, groupErr := operations.GroupExists(identity.PGID)
			if groupErr == nil && !groupExists {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
	if _, err := stdin.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waiter.await(ctx, time.Second); err != nil {
		t.Fatalf("reap residual leader: %v", err)
	}
	if groupExists, err := operations.GroupExists(identity.PGID); err != nil || !groupExists {
		t.Fatalf("short-lived child was not present after leader reap: exists=%t err=%v", groupExists, err)
	}

	started := time.Now()
	result := confirmAuditProcessExit(context.Background(), identity, waiter, operations, p5ResidualTestBounds(2*time.Second))
	if result.Outcome != auditTerminationConfirmedExit || result.Failure != nil {
		t.Fatalf("Darwin residual child confirmation = %+v, want confirmed exit", result)
	}
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond || elapsed > time.Second {
		t.Fatalf("Darwin residual child observation elapsed = %v", elapsed)
	}
	if signals := operations.signalsSnapshot(); len(signals) != 0 {
		t.Fatalf("Darwin residual child confirmation emitted signals: %v", signals)
	}
}

func p5ResidualTestBounds(residualGrace time.Duration) auditTerminationBounds {
	return auditTerminationBounds{
		TermGrace: 5 * time.Millisecond, KillGrace: 5 * time.Millisecond,
		ResidualExitGrace: residualGrace, PollInterval: time.Millisecond,
	}
}

func requireP5ResidualTerminationClass(t *testing.T, result auditTerminationResult, failureClass string) {
	t.Helper()
	var termination *auditTerminationError
	if result.Outcome != auditTerminationFailure || !errors.As(result.Failure, &termination) || termination.FailureClass != failureClass {
		t.Fatalf("residual termination = %+v, want typed %s", result, failureClass)
	}
}

type residualAuditProcessOperations struct {
	mu                 sync.Mutex
	expected           auditProcessIdentity
	leaderExists       bool
	groupResults       []bool
	groupErr           error
	groupLifetime      time.Duration
	groupFirstObserved time.Time
	inspectCalls       int
	groupCalls         int
	signals            []unix.Signal
}

func (operations *residualAuditProcessOperations) Inspect(pid int) (auditProcessIdentity, bool, error) {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	operations.inspectCalls++
	if !operations.leaderExists {
		return auditProcessIdentity{}, false, nil
	}
	observed := operations.expected
	observed.PID = pid
	return observed, true, nil
}

func (operations *residualAuditProcessOperations) SignalGroup(_ int, signal unix.Signal) error {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	operations.signals = append(operations.signals, signal)
	return nil
}

func (operations *residualAuditProcessOperations) GroupExists(int) (bool, error) {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	operations.groupCalls++
	if operations.groupErr != nil {
		return false, operations.groupErr
	}
	if operations.groupLifetime > 0 {
		if operations.groupFirstObserved.IsZero() {
			operations.groupFirstObserved = time.Now()
		}
		return time.Since(operations.groupFirstObserved) < operations.groupLifetime, nil
	}
	if len(operations.groupResults) == 0 {
		return false, nil
	}
	result := operations.groupResults[0]
	if len(operations.groupResults) > 1 {
		operations.groupResults = operations.groupResults[1:]
	}
	return result, nil
}

func (operations *residualAuditProcessOperations) signalsSnapshot() []unix.Signal {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return append([]unix.Signal(nil), operations.signals...)
}

func (operations *residualAuditProcessOperations) inspectionCounts() (int, int) {
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return operations.inspectCalls, operations.groupCalls
}
