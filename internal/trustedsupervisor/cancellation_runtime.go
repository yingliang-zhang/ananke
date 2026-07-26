package trustedsupervisor

import (
	"context"
	"errors"
	"time"
)

var errAuditExecutorClosing = errors.New("audit executor closing")
var errAuditCancellationRequested = errors.New("durable audit cancellation requested")

func (executor *auditExecutor) startGate(envelopeHash string, active *activeAuditExecution) (func(), error) {
	if executor.hooks.beforeStart != nil {
		executor.hooks.beforeStart(envelopeHash)
	}
	active.effectMu.Lock()
	record, found, err := executor.journal.loadAuditCancellation(context.Background(), envelopeHash)
	if err != nil {
		active.effectMu.Unlock()
		return nil, err
	}
	if found && record.State == auditCancellationStateRequested {
		active.cancellation = &record
		active.cancel()
		active.effectMu.Unlock()
		return nil, errAuditCancellationRequested
	}
	if active.ctx.Err() != nil || executor.ctx.Err() != nil {
		active.effectMu.Unlock()
		return nil, errAuditExecutorClosing
	}
	return active.effectMu.Unlock, nil
}
func (executor *auditExecutor) supervisorTestStartGate(active *activeAuditExecution) (func(), error) {
	active.effectMu.Lock()
	if active.ctx.Err() != nil || executor.ctx.Err() != nil {
		active.effectMu.Unlock()
		return nil, errAuditExecutorClosing
	}
	return active.effectMu.Unlock, nil
}

func (executor *auditExecutor) finishSupervisorTest(active *activeAuditExecution, identity auditProcessIdentity) {
	active.effectMu.Lock()
	if sameAuditProcessIdentity(active.identity, identity) {
		active.identity = auditProcessIdentity{}
	}
	active.effectMu.Unlock()
}

func (executor *auditExecutor) finalizeRequestedCancellation(
	intent auditExecutionIntent,
	active *activeAuditExecution,
	entry executionPolicyEntry,
	invocation *auditInvocation,
	result auditInvocationResult,
	effectErr error,
	cleanup func() error,
) (bool, error) {
	active.effectMu.Lock()
	defer active.effectMu.Unlock()
	record, found, err := executor.journal.loadAuditCancellation(context.Background(), intent.EnvelopeHash)
	if err != nil || !found || record.State != auditCancellationStateRequested {
		return false, err
	}
	active.cancellation = &record
	_, events, err := executor.journal.loadAuditExecution(context.Background(), intent.EnvelopeHash)
	if err != nil {
		return true, err
	}
	derived, err := deriveRecoveredAuditInvocations(intent, events, entry)
	if err != nil {
		return true, err
	}
	failureClass := ""
	if effectErr != nil && !errors.Is(effectErr, errAuditCancellationRequested) {
		failureClass = cancellationFailureClass(effectErr)
	}
	expected := auditProcessIdentity{
		PID: record.Intent.ExpectedPID, PGID: record.Intent.ExpectedPGID,
		ProcessStartIdentity: record.Intent.ExpectedStartIdentity,
	}
	if failureClass == "" && expected.PID > 0 {
		if result.PID > 0 && !sameAuditProcessIdentity(auditProcessIdentity{
			PID: result.PID, PGID: result.PGID, ProcessStartIdentity: result.ProcessStartIdentity,
		}, expected) {
			failureClass = "process_identity_mismatch"
		} else {
			termination := terminateOwnedAuditProcess(context.Background(), expected, nil, executor.processOperations, executor.terminationBounds)
			if termination.Outcome != auditTerminationConfirmedExit {
				failureClass = cancellationFailureClass(termination.Failure)
			} else if result.FinishedAt == "" {
				result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
		}
	}
	if failureClass == "" {
		if cleanup == nil {
			failureClass = "artifact_cleanup_failed"
		} else if cleanupErr := cleanup(); cleanupErr != nil {
			failureClass = "artifact_cleanup_failed"
		}
	}
	attempt := 1
	processStartedAt := result.StartedAt
	sessionUUID := ""
	var descriptorSource *auditExecutionEvent
	if len(events) != 0 {
		last := &events[len(events)-1]
		descriptorSource = last
		attempt = last.Attempt
		processStartedAt, sessionUUID = last.ProcessStartedAt, last.SessionUUID
	}
	if attempt < 1 || attempt > len(derived) {
		return true, authenticationError("audit cancellation recovery attempt")
	}
	paths := derived[attempt-1]
	state := auditStateCancelled
	outcome := auditCancellationOutcomeCompleted
	if failureClass != "" {
		state = auditStateWaitingForHuman
		outcome = auditCancellationOutcomeFailed
	}
	event := executor.newEvent(intent, events, state, attempt)
	if descriptorSource != nil {
		event.CommandDescriptorHash, event.PromptSHA256, event.SessionRunID = descriptorSource.CommandDescriptorHash, descriptorSource.PromptSHA256, descriptorSource.SessionRunID
		event.ResumeSessionUUID, event.SynthesizeOnly = descriptorSource.ResumeSessionUUID, descriptorSource.SynthesizeOnly
	} else if invocation != nil {
		bindAuditEventInvocation(&event, *invocation)
	} else if err := bindAuditEventDescriptor(&event, entry, intent, auditResume{}); err != nil {
		return true, err
	}
	event.PID, event.PGID, event.ProcessStartIdentity = expected.PID, expected.PGID, expected.ProcessStartIdentity
	event.ProcessStartedAt, event.ProcessFinishedAt = processStartedAt, result.FinishedAt
	event.ExitCode, event.StdoutSHA256, event.StderrSHA256 = result.ExitCode, result.StdoutSHA256, result.StderrSHA256
	event.WorkPath, event.OutputPath = paths.WorkDir, paths.OutputPath
	event.SessionPath, event.PromptPath, event.TemporaryPath = paths.SessionDir, paths.PromptPath, paths.TemporaryDir
	event.SessionUUID = sessionUUID
	if state == auditStateCancelled {
		if expected.PID == 0 {
			event.FailureClass = "operator_cancelled_before_start"
		} else {
			event.FailureClass = "operator_cancelled_owned_process"
		}
	} else {
		event.FailureClass = failureClass
	}
	sealed, err := sealAuditExecutionEvent(event)
	if err != nil {
		return true, err
	}
	if executor.completeCancellation == nil {
		return true, ErrProtocol
	}
	return true, executor.completeCancellation(record, sealed, outcome, failureClass)
}
