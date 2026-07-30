package trustedsupervisor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultAuditTermGrace         = 250 * time.Millisecond
	defaultAuditKillGrace         = 750 * time.Millisecond
	defaultAuditResidualExitGrace = 5 * time.Second
	defaultAuditPoll              = 10 * time.Millisecond
	maximumAuditTerminationGrace  = 30 * time.Second
	auditTerminationControlMargin = 250 * time.Millisecond
)

type auditProcessOperations interface {
	Inspect(int) (auditProcessIdentity, bool, error)
	SignalGroup(int, unix.Signal) error
	GroupExists(int) (bool, error)
}

type auditTerminationBounds struct {
	TermGrace         time.Duration
	KillGrace         time.Duration
	ResidualExitGrace time.Duration
	PollInterval      time.Duration
}

type auditTerminationOutcome string

const (
	auditTerminationConfirmedExit auditTerminationOutcome = "confirmed_exit"
	auditTerminationFailure       auditTerminationOutcome = "failure"
)

type auditTerminationResult struct {
	Outcome auditTerminationOutcome
	Failure error
}

func failedAuditTermination(class string, cause error) auditTerminationResult {
	return auditTerminationResult{
		Outcome: auditTerminationFailure,
		Failure: &auditTerminationError{FailureClass: class, Cause: cause},
	}
}

func confirmedAuditTermination() auditTerminationResult {
	return auditTerminationResult{Outcome: auditTerminationConfirmedExit}
}

func isAuditTerminationFailureClass(failureClass string) bool {
	switch failureClass {
	case "group_exit_unconfirmed", "group_inspection_failed", "kill_signal_failed", "process_identity_mismatch",
		"process_identity_unavailable", "process_inspection_failed", "process_wait_failed", "process_wait_unconfirmed", "term_signal_failed":
		return true
	default:
		return false
	}
}

type auditTerminationError struct {
	FailureClass string
	Cause        error
}

func (failure *auditTerminationError) Error() string {
	if failure == nil {
		return "audit termination failed"
	}
	if failure.Cause == nil {
		return "audit termination failed: " + failure.FailureClass
	}
	return fmt.Sprintf("audit termination failed: %s: %v", failure.FailureClass, failure.Cause)
}

func (failure *auditTerminationError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

type systemAuditProcessOperations struct{}

func (systemAuditProcessOperations) Inspect(pid int) (auditProcessIdentity, bool, error) {
	return inspectAuditProcessState(pid)
}

func (systemAuditProcessOperations) SignalGroup(pgid int, signal unix.Signal) error {
	if pgid <= 0 {
		return ErrProtocol
	}
	return unix.Kill(-pgid, signal)
}

func (systemAuditProcessOperations) GroupExists(pgid int) (bool, error) {
	if pgid <= 0 {
		return false, ErrProtocol
	}
	err := unix.Kill(-pgid, 0)
	if err == nil || errors.Is(err, unix.EPERM) {
		return true, nil
	}
	if errors.Is(err, unix.ESRCH) {
		return false, nil
	}
	return false, err
}

func defaultAuditTerminationBounds() auditTerminationBounds {
	return auditTerminationBounds{
		TermGrace: defaultAuditTermGrace, KillGrace: defaultAuditKillGrace,
		ResidualExitGrace: defaultAuditResidualExitGrace, PollInterval: defaultAuditPoll,
	}
}

func resolveAuditTerminationBounds(bounds auditTerminationBounds) (auditTerminationBounds, error) {
	if bounds == (auditTerminationBounds{}) {
		bounds = defaultAuditTerminationBounds()
	}
	if !validAuditTerminationBounds(bounds) {
		return auditTerminationBounds{}, ErrProtocol
	}
	return bounds, nil
}

func validAuditTerminationBounds(bounds auditTerminationBounds) bool {
	return bounds.TermGrace > 0 && bounds.TermGrace <= maximumAuditTerminationGrace &&
		bounds.KillGrace > 0 && bounds.KillGrace <= maximumAuditTerminationGrace &&
		bounds.ResidualExitGrace > 0 && bounds.ResidualExitGrace <= maximumAuditTerminationGrace &&
		bounds.PollInterval > 0 && bounds.PollInterval <= bounds.TermGrace &&
		bounds.PollInterval <= bounds.KillGrace && bounds.PollInterval <= bounds.ResidualExitGrace
}

func auditTerminationControlBound(bounds auditTerminationBounds) (time.Duration, error) {
	if !validAuditTerminationBounds(bounds) {
		return 0, ErrProtocol
	}
	return bounds.TermGrace + bounds.KillGrace + bounds.ResidualExitGrace + auditTerminationControlMargin, nil
}

func terminateOwnedAuditProcess(
	ctx context.Context,
	expected auditProcessIdentity,
	waiter *auditProcessWaiter,
	operations auditProcessOperations,
	bounds auditTerminationBounds,
) auditTerminationResult {
	if ctx == nil || operations == nil || expected.PID <= 0 || expected.PGID != expected.PID || expected.ProcessStartIdentity == "" ||
		!validAuditTerminationBounds(bounds) {
		return auditTerminationResult{Outcome: auditTerminationFailure, Failure: ErrProtocol}
	}
	observed, leaderExists, err := operations.Inspect(expected.PID)
	if err != nil {
		return failedAuditTermination("process_inspection_failed", err)
	}
	if leaderExists && !sameAuditProcessIdentity(observed, expected) {
		return failedAuditTermination("process_identity_mismatch", ErrAuthentication)
	}
	if !leaderExists {
		return confirmAuditProcessExit(ctx, expected, waiter, operations, bounds)
	}
	groupExists, err := operations.GroupExists(expected.PGID)
	if err != nil {
		return failedAuditTermination("group_inspection_failed", err)
	}
	if !groupExists {
		return failedAuditTermination("group_inspection_failed", ErrAuthentication)
	}
	if err := operations.SignalGroup(expected.PGID, unix.SIGTERM); err != nil {
		return failedAuditTermination("term_signal_failed", err)
	}
	exited, waitErr := waitForOwnedAuditProcessExit(ctx, expected, waiter, operations, bounds.TermGrace, bounds.PollInterval)
	if waitErr != nil {
		return auditTerminationResult{Outcome: auditTerminationFailure, Failure: waitErr}
	}
	if !exited {
		leaderExists, groupExists, inspectErr := inspectAuditTerminationTarget(expected, operations)
		if inspectErr != nil {
			return auditTerminationResult{Outcome: auditTerminationFailure, Failure: inspectErr}
		}
		if !leaderExists {
			return confirmAuditProcessExit(ctx, expected, waiter, operations, bounds)
		}
		if !groupExists {
			return failedAuditTermination("group_inspection_failed", ErrAuthentication)
		}
		if err := operations.SignalGroup(expected.PGID, unix.SIGKILL); err != nil {
			return failedAuditTermination("kill_signal_failed", err)
		}
		exited, waitErr = waitForOwnedAuditProcessExit(ctx, expected, waiter, operations, bounds.KillGrace, bounds.PollInterval)
		if waitErr != nil {
			return auditTerminationResult{Outcome: auditTerminationFailure, Failure: waitErr}
		}
		if !exited {
			leaderExists, groupExists, inspectErr = inspectAuditTerminationTarget(expected, operations)
			if inspectErr != nil {
				return auditTerminationResult{Outcome: auditTerminationFailure, Failure: inspectErr}
			}
			if !leaderExists {
				return confirmAuditProcessExit(ctx, expected, waiter, operations, bounds)
			}
			if !groupExists {
				return failedAuditTermination("group_inspection_failed", ErrAuthentication)
			}
			return failedAuditTermination("group_exit_unconfirmed", ErrDeadline)
		}
	}
	return confirmAuditProcessExit(ctx, expected, waiter, operations, bounds)
}

func waitForOwnedAuditProcessExit(
	ctx context.Context,
	expected auditProcessIdentity,
	waiter *auditProcessWaiter,
	operations auditProcessOperations,
	timeout time.Duration,
	poll time.Duration,
) (bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if waitErr, complete := waiter.poll(); complete && waitErr != nil {
			return false, waitErr
		}
		leaderExists, groupExists, err := inspectAuditTerminationTarget(expected, operations)
		if err != nil {
			return false, err
		}
		if !leaderExists && !groupExists {
			return true, nil
		}
		if leaderExists && !groupExists {
			return false, &auditTerminationError{FailureClass: "group_inspection_failed", Cause: ErrAuthentication}
		}
		select {
		case <-ctx.Done():
			return false, &auditTerminationError{FailureClass: "group_exit_unconfirmed", Cause: ErrDeadline}
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}

func confirmAuditProcessExit(
	ctx context.Context,
	expected auditProcessIdentity,
	waiter *auditProcessWaiter,
	operations auditProcessOperations,
	bounds auditTerminationBounds,
) auditTerminationResult {
	if ctx == nil || operations == nil || expected.PID <= 0 || expected.PGID != expected.PID ||
		expected.ProcessStartIdentity == "" || !validAuditTerminationBounds(bounds) {
		return auditTerminationResult{Outcome: auditTerminationFailure, Failure: ErrProtocol}
	}
	if waiter != nil {
		if err := waiter.await(ctx, bounds.KillGrace); err != nil {
			return auditTerminationResult{Outcome: auditTerminationFailure, Failure: err}
		}
	}
	observed, exists, err := operations.Inspect(expected.PID)
	if err != nil {
		return failedAuditTermination("process_inspection_failed", err)
	}
	if exists {
		if !sameAuditProcessIdentity(observed, expected) {
			return failedAuditTermination("process_identity_mismatch", ErrAuthentication)
		}
		return failedAuditTermination("group_exit_unconfirmed", ErrDeadline)
	}
	if waiter == nil {
		groupExists, err := operations.GroupExists(expected.PGID)
		if err != nil {
			return failedAuditTermination("group_inspection_failed", err)
		}
		if groupExists {
			return failedAuditTermination("group_exit_unconfirmed", ErrDeadline)
		}
		return confirmedAuditTermination()
	}
	return observeNaturalAuditProcessGroupExit(ctx, expected.PGID, operations, bounds)
}

func observeNaturalAuditProcessGroupExit(
	ctx context.Context,
	pgid int,
	operations auditProcessOperations,
	bounds auditTerminationBounds,
) auditTerminationResult {
	deadline := time.NewTimer(bounds.ResidualExitGrace)
	defer deadline.Stop()
	ticker := time.NewTicker(bounds.PollInterval)
	defer ticker.Stop()
	for {
		groupExists, err := operations.GroupExists(pgid)
		if err != nil {
			return failedAuditTermination("group_inspection_failed", err)
		}
		if !groupExists {
			return confirmedAuditTermination()
		}
		select {
		case <-ctx.Done():
			return failedAuditTermination("group_exit_unconfirmed", ErrDeadline)
		case <-deadline.C:
			return failedAuditTermination("group_exit_unconfirmed", ErrDeadline)
		case <-ticker.C:
		}
	}
}

type auditProcessWaiter struct {
	done    chan struct{}
	once    sync.Once
	waitErr error
}

func newAuditProcessWaiter() *auditProcessWaiter {
	return &auditProcessWaiter{done: make(chan struct{})}
}

func startAuditProcessWaiter(command *exec.Cmd) *auditProcessWaiter {
	waiter := newAuditProcessWaiter()
	go func() { waiter.complete(command.Wait()) }()
	return waiter
}

func (waiter *auditProcessWaiter) complete(waitErr error) {
	if waiter == nil {
		return
	}
	waiter.once.Do(func() {
		waiter.waitErr = waitErr
		close(waiter.done)
	})
}

func (waiter *auditProcessWaiter) poll() (error, bool) {
	if waiter == nil {
		return nil, true
	}
	select {
	case <-waiter.done:
		return validateAuditProcessWait(waiter.waitErr), true
	default:
		return nil, false
	}
}

func (waiter *auditProcessWaiter) result() error {
	if waiter == nil {
		return nil
	}
	<-waiter.done
	return waiter.waitErr
}

func (waiter *auditProcessWaiter) await(ctx context.Context, timeout time.Duration) error {
	if waitErr, complete := waiter.poll(); complete {
		return waitErr
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-waiter.done:
		return validateAuditProcessWait(waiter.waitErr)
	case <-ctx.Done():
		return &auditTerminationError{FailureClass: "process_wait_unconfirmed", Cause: ErrDeadline}
	case <-timer.C:
		return &auditTerminationError{FailureClass: "process_wait_unconfirmed", Cause: ErrDeadline}
	}
}

func validateAuditProcessWait(waitErr error) error {
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return &auditTerminationError{FailureClass: "process_wait_failed", Cause: waitErr}
	}
	return nil
}

func inspectAuditTerminationTarget(expected auditProcessIdentity, operations auditProcessOperations) (bool, bool, error) {
	observed, exists, err := operations.Inspect(expected.PID)
	if err != nil {
		return false, false, &auditTerminationError{FailureClass: "process_inspection_failed", Cause: err}
	}
	if exists && !sameAuditProcessIdentity(observed, expected) {
		return false, false, &auditTerminationError{FailureClass: "process_identity_mismatch", Cause: ErrAuthentication}
	}
	groupExists, err := operations.GroupExists(expected.PGID)
	if err != nil {
		return false, false, &auditTerminationError{FailureClass: "group_inspection_failed", Cause: err}
	}
	return exists, groupExists, nil
}

func sameAuditProcessIdentity(left, right auditProcessIdentity) bool {
	return left.PID == right.PID && left.PGID == right.PGID && left.ProcessStartIdentity == right.ProcessStartIdentity
}

func inspectAuditProcessState(pid int) (auditProcessIdentity, bool, error) {
	if pid <= 0 {
		return auditProcessIdentity{}, false, ErrProtocol
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.EIO) && errors.Is(unix.Kill(pid, 0), unix.ESRCH) {
			return auditProcessIdentity{}, false, nil
		}
		return auditProcessIdentity{}, false, authenticationError("audit process identity")
	}
	if int(process.Proc.P_pid) == 0 {
		return auditProcessIdentity{}, false, nil
	}
	if int(process.Proc.P_pid) != pid {
		return auditProcessIdentity{}, false, authenticationError("audit process identity")
	}
	return auditProcessIdentity{
		PID: pid, PGID: int(process.Eproc.Pgid),
		ProcessStartIdentity: fmt.Sprintf("%d:%d", process.Proc.P_starttime.Sec, process.Proc.P_starttime.Usec),
	}, true, nil
}
