package trustedsupervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

type auditCancellationCompleter func(auditCancellationRecord, auditExecutionEvent, string, string) error
type auditCancellationPersist func(auditProcessIdentity) (auditCancellationRecord, error)

type auditExecutorHooks struct {
	beforeStart               func(string)
	beforeFinalizingPersist   func(auditInvocation)
	afterFinalizingPersist    func(auditInvocation)
	beforeCompletedPersist    func(auditInvocation)
	afterCompletedPersist     func(auditInvocation)
	cleanupRecovered          func(auditExecutionIntent, []auditExecutionEvent, executionPolicyEntry) error
	afterInvocation           func(auditInvocationResult)
	supervisorTestAfterStart  func(auditProcessIdentity)
	supervisorTestHardTimeout <-chan time.Time
	invocation                auditInvocationHooks
}

type auditExecutor struct {
	journal              *serverJournal
	policy               *executionPolicy
	ctx                  context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	active               map[string]*activeAuditExecution
	processOperations    auditProcessOperations
	terminationBounds    auditTerminationBounds
	completeCancellation auditCancellationCompleter
	hooks                auditExecutorHooks
	closing              bool
	closed               bool
	closeAttempt         *auditExecutorCloseAttempt
	failure              error
}

type activeAuditExecution struct {
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	effectMu     sync.Mutex
	identity     auditProcessIdentity
	cancellation *auditCancellationRecord
	pending      *pendingAuditProcess
}

type pendingAuditProcess struct {
	result  auditInvocationResult
	cleanup func() error
	joined  bool
}

type auditExecutorCloseAttempt struct {
	done chan struct{}
	err  error
}

func newAuditExecutor(journal *serverJournal, policy *executionPolicy) (*auditExecutor, error) {
	executor, err := newUnrecoveredAuditExecutor(journal, policy)
	if err != nil {
		return nil, err
	}
	if err := executor.recover(); err != nil {
		executor.cancel()
		return nil, err
	}
	return executor, nil
}

func newUnrecoveredAuditExecutor(journal *serverJournal, policy *executionPolicy) (*auditExecutor, error) {
	if journal == nil || policy == nil || !auditPlatformSupported(runtimeGOOS()) {
		return nil, authenticationError("audit executor unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &auditExecutor{
		journal: journal, policy: policy, ctx: ctx, cancel: cancel, active: make(map[string]*activeAuditExecution),
		processOperations: systemAuditProcessOperations{}, terminationBounds: defaultAuditTerminationBounds(),
	}, nil
}

func (executor *auditExecutor) newActive() *activeAuditExecution {
	ctx, cancel := context.WithCancel(executor.ctx)
	return &activeAuditExecution{ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (executor *auditExecutor) Notify(envelopeHash string) {
	if executor == nil || !protocolHashPattern.MatchString(envelopeHash) {
		return
	}
	executor.mu.Lock()
	if _, exists := executor.active[envelopeHash]; exists || executor.closing || executor.ctx.Err() != nil {
		executor.mu.Unlock()
		return
	}
	active := executor.newActive()
	executor.active[envelopeHash] = active
	executor.mu.Unlock()
	go executor.run(envelopeHash, active)
}

func (executor *auditExecutor) Close() error {
	if executor == nil {
		return nil
	}
	bound, boundsErr := auditTerminationControlBound(executor.terminationBounds)
	if boundsErr != nil {
		return boundsErr
	}
	executor.mu.Lock()
	if executor.closed {
		failure := executor.failure
		executor.mu.Unlock()
		return failure
	}
	if executor.closeAttempt != nil {
		attempt := executor.closeAttempt
		executor.mu.Unlock()
		timer := time.NewTimer(2 * bound)
		defer timer.Stop()
		select {
		case <-attempt.done:
			return attempt.err
		case <-timer.C:
			return ErrDeadline
		}
	}
	if !executor.closing {
		executor.closing = true
		executor.cancel()
	}
	attempt := &auditExecutorCloseAttempt{done: make(chan struct{})}
	executor.closeAttempt = attempt
	type activeReference struct {
		envelopeHash string
		active       *activeAuditExecution
	}
	activeExecutions := make([]activeReference, 0, len(executor.active))
	for envelopeHash, active := range executor.active {
		active.cancel()
		activeExecutions = append(activeExecutions, activeReference{envelopeHash: envelopeHash, active: active})
	}
	executor.mu.Unlock()

	closeCtx, cancelClose := context.WithTimeout(context.Background(), bound)
	defer cancelClose()
	for _, reference := range activeExecutions {
		select {
		case <-reference.active.done:
		case <-closeCtx.Done():
			attempt.err = ErrDeadline
		}
		if attempt.err == nil {
			if err := executor.retryPendingAuditProcess(closeCtx, reference.envelopeHash, reference.active); err != nil {
				attempt.err = ErrDeadline
			}
		}
		if attempt.err != nil {
			break
		}
	}
	executor.mu.Lock()
	if attempt.err == nil {
		attempt.err = executor.failure
		executor.closed = true
	}
	executor.closeAttempt = nil
	close(attempt.done)
	executor.mu.Unlock()
	return attempt.err
}

func (executor *auditExecutor) recordFailure(err error) {
	if executor == nil || err == nil {
		return
	}
	executor.mu.Lock()
	if executor.failure == nil {
		executor.failure = err
	}
	executor.mu.Unlock()
}

func (executor *auditExecutor) Cancel(envelopeHash string, persist auditCancellationPersist) (auditCancellationRecord, error) {
	if executor == nil || persist == nil || !protocolHashPattern.MatchString(envelopeHash) || executor.completeCancellation == nil {
		return auditCancellationRecord{}, ErrProtocol
	}
	executor.mu.Lock()
	if executor.closing {
		executor.mu.Unlock()
		return auditCancellationRecord{}, ErrDeadline
	}
	active := executor.active[envelopeHash]
	if active == nil {
		expected := auditProcessIdentity{}
		_, events, loadErr := executor.journal.loadAuditExecution(context.Background(), envelopeHash)
		if loadErr != nil {
			executor.mu.Unlock()
			return auditCancellationRecord{}, loadErr
		}
		if len(events) != 0 && events[len(events)-1].State == auditStateRunning {
			last := events[len(events)-1]
			expected = auditProcessIdentity{PID: last.PID, PGID: last.PGID, ProcessStartIdentity: last.ProcessStartIdentity}
		}
		record, err := persist(expected)
		if err != nil {
			executor.mu.Unlock()
			return auditCancellationRecord{}, err
		}
		if record.State != auditCancellationStateRequested {
			executor.mu.Unlock()
			return record, nil
		}
		active = executor.newActive()
		active.cancellation = &record
		active.identity = expected
		active.cancel()
		executor.active[envelopeHash] = active
		executor.mu.Unlock()
		go executor.run(envelopeHash, active)
	} else {
		executor.mu.Unlock()
		active.effectMu.Lock()
		record, err := persist(active.identity)
		if err != nil {
			active.effectMu.Unlock()
			return auditCancellationRecord{}, err
		}
		if record.State != auditCancellationStateRequested {
			active.effectMu.Unlock()
			return record, nil
		}
		active.cancellation = &record
		active.cancel()
		active.effectMu.Unlock()
	}
	bound, boundsErr := auditTerminationControlBound(executor.terminationBounds)
	if boundsErr != nil {
		return auditCancellationRecord{}, boundsErr
	}
	deadline := time.NewTimer(bound)
	defer deadline.Stop()
	poll := time.NewTicker(executor.terminationBounds.PollInterval)
	defer poll.Stop()
	for {
		record, found, err := executor.journal.loadAuditCancellation(context.Background(), envelopeHash)
		if err != nil {
			return auditCancellationRecord{}, err
		}
		if found && record.State != auditCancellationStateRequested {
			return record, nil
		}
		select {
		case <-active.done:
			if !found {
				return auditCancellationRecord{}, ErrDeadline
			}
		case <-poll.C:
		case <-deadline.C:
			return auditCancellationRecord{}, ErrDeadline
		}
	}
}

func (executor *auditExecutor) finalizeCancellationOrStop(
	intent auditExecutionIntent,
	active *activeAuditExecution,
	entry executionPolicyEntry,
	invocation *auditInvocation,
	result auditInvocationResult,
	effectErr error,
	cleanup func() error,
) bool {
	finalized, err := executor.finalizeRequestedCancellation(intent, active, entry, invocation, result, effectErr, cleanup)
	if err != nil {
		executor.recordFailure(err)
		return true
	}
	return finalized
}

func (executor *auditExecutor) run(envelopeHash string, active *activeAuditExecution) {
	defer func() {
		executor.mu.Lock()
		if active.pending == nil {
			delete(executor.active, envelopeHash)
		}
		close(active.done)
		executor.mu.Unlock()
	}()
	intent, events, err := executor.journal.loadAuditExecution(context.Background(), envelopeHash)
	if err != nil {
		executor.recordFailure(err)
		return
	}
	resume := auditResume{}
	entry, err := executor.resolveEntry(intent)
	if err != nil {
		executor.recordFailure(executor.appendWaiting(intent, &events, 1, resume, "execution_policy_drift", entry))
		return
	}
	recoveredCleanup := func() error { return executor.cleanupRecoveredExecution(intent, events, entry) }
	if executor.finalizeCancellationOrStop(intent, active, entry, nil, auditInvocationResult{}, nil, recoveredCleanup) {
		return
	}
	if len(events) != 0 {
		if err := executor.recoverExisting(intent, events, active); err != nil {
			executor.recordFailure(err)
		}
		return
	}
	snapshot, err := materializeAuditSnapshot(active.ctx, executor.policy, entry, intent.RunID, auditSnapshotHooks{})
	if err != nil {
		if executor.finalizeCancellationOrStop(intent, active, entry, nil, auditInvocationResult{}, err, recoveredCleanup) {
			return
		}
		executor.recordFailure(executor.appendWaiting(intent, &events, 1, resume, classifyAuditSnapshotFailure(err), entry))
		return
	}
	ownedInvocations := make([]auditInvocation, 0, intent.AttemptCap)
	cleanupOnReturn := true
	cleanupOwned := func() error {
		return cleanupOwnedAuditInvocations(executor.policy, entry, snapshot, intent.RunID, ownedInvocations)
	}
	defer func() {
		if cleanupOnReturn {
			_ = cleanupOwned()
		}
	}()
	for attempt := 1; attempt <= intent.AttemptCap; attempt++ {
		if executor.finalizeCancellationOrStop(intent, active, entry, nil, auditInvocationResult{}, nil, cleanupOwned) {
			return
		}
		attemptRunID := intent.RunID + "_attempt_" + strconv.Itoa(attempt)
		invocation, err := prepareAuditInvocation(executor.policy, entry, snapshot, attemptRunID, intent.RunID, resume)
		if err != nil {
			if executor.finalizeCancellationOrStop(intent, active, entry, nil, auditInvocationResult{}, err, cleanupOwned) {
				return
			}
			executor.recordFailure(executor.appendWaiting(intent, &events, attempt, resume, "invocation_preparation_failed", entry))
			return
		}
		ownedInvocations = append(ownedInvocations, invocation)
		if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, auditInvocationResult{}, nil, cleanupOwned) {
			return
		}
		prepared := executor.newInvocationEvent(intent, events, auditStatePrepared, attempt, invocation)
		prepared.WorkPath, prepared.OutputPath = invocation.WorkDir, invocation.OutputPath
		prepared.SessionPath, prepared.PromptPath = invocation.SessionDir, invocation.PromptPath
		prepared.TemporaryPath = invocation.TemporaryDir
		if appendErr := executor.appendEvent(&events, prepared); appendErr != nil {
			if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, auditInvocationResult{}, appendErr, cleanupOwned) {
				return
			}
			failureClass := "prepared_event_persistence_failed"
			if cleanupErr := cleanupOwned(); cleanupErr != nil {
				failureClass = "artifact_cleanup_failed"
			}
			if waitingErr := executor.appendWaiting(intent, &events, attempt, resume, failureClass, entry); waitingErr != nil {
				executor.recordFailure(errors.Join(appendErr, waitingErr))
			}
			return
		}
		invocationHooks := executor.hooks.invocation
		invocationHooks.StartGate = func() (func(), error) {
			return executor.startGate(envelopeHash, active)
		}
		invocationHooks.AfterStart = func(identity auditProcessIdentity) error {
			active.identity = identity
			running := executor.newInvocationEvent(intent, events, auditStateRunning, attempt, invocation)
			running.PID, running.PGID, running.ProcessStartIdentity = identity.PID, identity.PGID, identity.ProcessStartIdentity
			running.WorkPath, running.OutputPath = invocation.WorkDir, invocation.OutputPath
			running.SessionPath, running.PromptPath = invocation.SessionDir, invocation.PromptPath
			running.ProcessStartedAt = identity.StartedAt
			running.TemporaryPath = invocation.TemporaryDir
			return executor.appendEvent(&events, running)
		}
		invocationHooks.ProcessOperations = executor.processOperations
		invocationHooks.TerminationBounds = executor.terminationBounds
		result, runErr := runAuditInvocation(active.ctx, executor.policy, entry, invocation, invocationHooks)
		if executor.hooks.afterInvocation != nil {
			executor.hooks.afterInvocation(result)
		}
		if result.boundInvocation.NativeAddonPath != "" {
			invocation = result.boundInvocation
			ownedInvocations[len(ownedInvocations)-1] = invocation
		}
		terminationUnconfirmed := result.processWaiter != nil
		if terminationUnconfirmed {
			cleanupOnReturn = false
		}
		if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, result, runErr, cleanupOwned) {
			if terminationUnconfirmed {
				executor.finishOrRetainUnconfirmedAuditProcess(active, result, cleanupOwned)
			}
			return
		}
		if runErr != nil {
			failureClass, unsafeToClean := classifyAuditRunFailure(runErr, result)
			if unsafeToClean {
				cleanupOnReturn = false
				waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
				waiting.FailureClass = failureClass
				if _, err := executor.appendRequiredTerminal(&events, waiting); err != nil {
					executor.recordFailure(err)
				}
				executor.finishOrRetainUnconfirmedAuditProcess(active, result, cleanupOwned)
				return
			}
			if cleanupErr := cleanupOwned(); cleanupErr != nil {
				waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
				waiting.FailureClass = "artifact_cleanup_failed"
				if _, err := executor.appendRequiredTerminal(&events, waiting); err != nil {
					executor.recordFailure(err)
				}
				return
			}
			state := auditStateFailed
			if len(events) != 0 && events[len(events)-1].State == auditStatePrepared {
				state = auditStateWaitingForHuman
			}
			terminal := executor.terminalEvent(intent, events, state, attempt, invocation, result)
			terminal.FailureClass = failureClass
			if _, err := executor.appendRequiredTerminal(&events, terminal); err != nil {
				executor.recordFailure(err)
			}
			return
		}
		if result.Cancelled || errors.Is(active.ctx.Err(), context.Canceled) && executor.ctx.Err() != nil {
			if cleanupErr := cleanupOwned(); cleanupErr != nil {
				waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
				waiting.FailureClass = "artifact_cleanup_failed"
				if _, err := executor.appendRequiredTerminal(&events, waiting); err != nil {
					executor.recordFailure(err)
				}
				return
			}
			cancelled := executor.terminalEvent(intent, events, auditStateCancelled, attempt, invocation, result)
			cancelled.FailureClass = "operator_or_server_cancelled"
			if _, err := executor.appendRequiredTerminal(&events, cancelled); err != nil {
				executor.recordFailure(err)
			}
			return
		}
		if result.TimeoutEvidence != (auditTimeoutEvidence{}) {
			timeoutEvidence := result.TimeoutEvidence
			if !result.ProcessGroupGone || !validAuditTimeoutEvidence(timeoutEvidence) {
				if cleanupErr := cleanupOwned(); cleanupErr != nil {
					waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
					waiting.FailureClass = "artifact_cleanup_failed"
					if _, err := executor.appendRequiredTerminal(&events, waiting); err != nil {
						executor.recordFailure(err)
					}
					return
				}
				failed := executor.terminalEvent(intent, events, auditStateFailed, attempt, invocation, result)
				failed.FailureClass = "malformed_timeout_evidence"
				if _, err := executor.appendRequiredTerminal(&events, failed); err != nil {
					executor.recordFailure(err)
				}
				return
			}
			timedOut := executor.terminalEvent(intent, events, auditStateTimedOut, attempt, invocation, result)
			timedOut.SessionUUID, timedOut.TimeoutObservation = timeoutEvidence.SessionUUID, timeoutEvidence
			persistedState, appendErr := executor.appendRequiredTerminal(&events, timedOut)
			if appendErr != nil {
				if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, result, appendErr, cleanupOwned) {
					return
				}
				executor.recordFailure(appendErr)
				return
			}
			if persistedState != auditStateTimedOut {
				return
			}
			if attempt == intent.AttemptCap {
				if cleanupErr := cleanupOwned(); cleanupErr != nil {
					executor.recordFailure(executor.appendWaiting(intent, &events, attempt, invocation.Resume, "artifact_cleanup_failed", entry))
					return
				}
				executor.recordFailure(executor.appendWaiting(intent, &events, attempt, invocation.Resume, "attempt_cap_exhausted", entry))
				return
			}
			resume = auditResume{SessionUUID: timeoutEvidence.SessionUUID, SynthesizeOnly: timeoutEvidence.SynthesizeOnly}
			continue
		}
		if result.ExitCode != 0 {
			if cleanupErr := cleanupOwned(); cleanupErr != nil {
				waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
				waiting.FailureClass = "artifact_cleanup_failed"
				if _, err := executor.appendRequiredTerminal(&events, waiting); err != nil {
					executor.recordFailure(err)
				}
				return
			}
			failed := executor.terminalEvent(intent, events, auditStateFailed, attempt, invocation, result)
			failed.FailureClass = "direct_omp_exit_nonzero"
			if _, err := executor.appendRequiredTerminal(&events, failed); err != nil {
				executor.recordFailure(err)
			}
			return
		}
		if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, result, nil, cleanupOwned) {
			return
		}
		finalizing := executor.terminalEvent(intent, events, auditStateFinalizing, attempt, invocation, result)
		finalizing.SessionUUID = invocation.Resume.SessionUUID
		testHooks := auditSupervisorTestHooks{
			StartGate: func() (func(), error) { return executor.supervisorTestStartGate(active) },
			AfterStart: func(identity auditProcessIdentity) error {
				active.identity = identity
				if executor.hooks.supervisorTestAfterStart != nil {
					executor.hooks.supervisorTestAfterStart(identity)
				}
				return nil
			},
			AfterFinish:       func(identity auditProcessIdentity) { executor.finishSupervisorTest(active, identity) },
			ProcessOperations: executor.processOperations, TerminationBounds: executor.terminationBounds,
			HardTimeout: executor.hooks.supervisorTestHardTimeout,
		}
		invocationRoots, rootErr := aggregateAuditInvocationOwnedRoots(ownedInvocations)
		if rootErr == nil {
			result.boundInvocation.OwnedRoots = invocationRoots
		}
		var evidence collectedAuditEvidence
		err = rootErr
		if err == nil {
			evidence, err = collectAuditEvidence(active.ctx, executor.policy, entry, intent, snapshot, invocation, result, finalizing, testHooks)
		}
		if err != nil {
			testResult, hasTestResult := auditProcessResultFromError(err)
			var ownedFailure *auditOwnedProcessError
			terminationUnconfirmed = errors.As(err, &ownedFailure)
			if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, func() auditInvocationResult {
				if hasTestResult {
					return testResult
				}
				return result
			}(), func() error {
				if terminationUnconfirmed {
					return err
				}
				return nil
			}(), cleanupOwned) {
				if terminationUnconfirmed {
					cleanupOnReturn = false
					executor.finishOrRetainUnconfirmedAuditProcess(active, testResult, cleanupOwned)
				}
				return
			}
			if terminationUnconfirmed {
				cleanupOnReturn = false
				waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
				waiting.FailureClass = cancellationFailureClass(err)
				if _, appendErr := executor.appendRequiredTerminal(&events, waiting); appendErr != nil {
					executor.recordFailure(appendErr)
				}
				executor.finishOrRetainUnconfirmedAuditProcess(active, testResult, cleanupOwned)
				return
			}
			if cleanupErr := cleanupOwned(); cleanupErr != nil {
				waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
				waiting.FailureClass = "artifact_cleanup_failed"
				if _, appendErr := executor.appendRequiredTerminal(&events, waiting); appendErr != nil {
					executor.recordFailure(appendErr)
				}
				return
			}
			failed := executor.terminalEvent(intent, events, auditStateFailed, attempt, invocation, result)
			failed.FailureClass = "evidence_verification_failed"
			if _, appendErr := executor.appendRequiredTerminal(&events, failed); appendErr != nil {
				executor.recordFailure(appendErr)
			}
			return
		}
		if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, result, nil, cleanupOwned) {
			return
		}
		finalizing.OutputSHA256, finalizing.OutputSize = evidence.OutputSHA256, evidence.OutputSize
		finalizing.EvidenceJSON, finalizing.EvidenceHash = evidence.EvidenceJSON, evidence.EvidenceHash
		if executor.hooks.beforeFinalizingPersist != nil {
			executor.hooks.beforeFinalizingPersist(invocation)
		}
		if err := verifyAuditOutputUnchanged(invocation.OutputPath, evidence.OutputSHA256, evidence.OutputSize); err != nil {
			if cleanupErr := cleanupOwned(); cleanupErr != nil {
				waiting := executor.terminalEvent(intent, events, auditStateWaitingForHuman, attempt, invocation, result)
				waiting.FailureClass = "artifact_cleanup_failed"
				if _, appendErr := executor.appendRequiredTerminal(&events, waiting); appendErr != nil {
					executor.recordFailure(appendErr)
				}
				return
			}
			failed := executor.terminalEvent(intent, events, auditStateFailed, attempt, invocation, result)
			failed.FailureClass = "evidence_verification_failed"
			if _, appendErr := executor.appendRequiredTerminal(&events, failed); appendErr != nil {
				executor.recordFailure(appendErr)
			}
			return
		}
		persistedState, appendErr := executor.appendRequiredTerminal(&events, finalizing)
		if appendErr != nil {
			if executor.finalizeCancellationOrStop(intent, active, entry, &invocation, result, appendErr, cleanupOwned) {
				return
			}
			executor.recordFailure(appendErr)
			return
		}
		if persistedState != auditStateFinalizing {
			return
		}
		cleanupOnReturn = false
		if executor.hooks.afterFinalizingPersist != nil {
			executor.hooks.afterFinalizingPersist(invocation)
		}
		if err := executor.resumeFinalizing(intent, &events, entry, invocation); err != nil {
			executor.recordFailure(err)
		}
		return
	}
}

func aggregateAuditInvocationOwnedRoots(invocations []auditInvocation) ([]auditOwnedRootIdentity, error) {
	if len(invocations) == 0 || len(invocations) > 10 {
		return nil, authenticationError("audit invocation owned root aggregation")
	}
	ordered := make([]auditOwnedRootIdentity, 0, len(invocations)*8+1)
	byPath := make(map[string]auditOwnedRootIdentity, len(invocations)*8+1)
	for _, invocation := range invocations {
		current, err := currentAuditInvocationOwnedRoots(invocation)
		if err != nil {
			return nil, err
		}
		for _, identity := range current {
			if prior, duplicate := byPath[identity.Path]; duplicate {
				if prior != identity {
					return nil, authenticationError("conflicting multi-attempt audit owned root identity")
				}
				continue
			}
			byPath[identity.Path] = identity
			ordered = append(ordered, identity)
		}
	}
	return ordered, nil
}

// cleanupOwnedAuditInvocations is used only while committing a non-completed
// failure, timeout, or cancellation outcome. Every destructive effect still
// requires the identities captured by the live invocation and snapshot.
func cleanupOwnedAuditInvocations(policy *executionPolicy, entry executionPolicyEntry, snapshot auditSnapshot, sessionRunID string, invocations []auditInvocation) error {
	if policy == nil || policy.namespaceAuthority == nil {
		return authenticationError("live audit cleanup policy authority")
	}
	var first error
	record := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}

	transientsClean := true
	for _, invocation := range invocations {
		if invocation.namespaceAuthority != policy.namespaceAuthority {
			transientsClean = false
			record(authenticationError("live audit cleanup namespace continuity"))
			continue
		}
		if err := cleanupAuditInvocationTransient(entry, invocation); err != nil {
			transientsClean = false
			record(err)
		}
	}

	expectedSessionDir := filepath.Join(entry.SessionRoot, sessionRunID)
	bindingsValid := executionTaskIDPattern.MatchString(sessionRunID) &&
		snapshot.RunRoot == filepath.Join(entry.WorkRoot, sessionRunID) &&
		snapshot.SourceRoot == filepath.Join(snapshot.RunRoot, "source") && len(snapshot.OwnedRoots) == 2
	sharedRoots := make([]auditOwnedRootIdentity, 0, 3)
	var sessionIdentity auditOwnedRootIdentity
	for index, invocation := range invocations {
		if invocation.SessionRunID != sessionRunID || invocation.SessionDir != expectedSessionDir ||
			invocation.WorkDir != snapshot.SourceRoot || filepath.Dir(invocation.WorkDir) != snapshot.RunRoot {
			bindingsValid = false
			continue
		}
		identity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "session")
		if !ok || identity.Path != expectedSessionDir || index > 0 && identity != sessionIdentity {
			bindingsValid = false
			continue
		}
		if index == 0 {
			sessionIdentity = identity
			sharedRoots = append(sharedRoots, identity)
		}
	}
	sharedRoots = append(sharedRoots, snapshot.OwnedRoots...)
	if !bindingsValid || len(invocations) > 0 && len(sharedRoots) != 3 || len(invocations) == 0 && len(sharedRoots) != 2 {
		record(authenticationError("owned audit cleanup shared root binding"))
	} else if err := validateAuditOwnedRootSequence(sharedRoots); err != nil {
		record(err)
		bindingsValid = false
	}
	if bindingsValid && (snapshot.OwnedRoots[0].Role != "work" || snapshot.OwnedRoots[0].Path != snapshot.RunRoot || !snapshot.OwnedRoots[0].CleanupRoot ||
		snapshot.OwnedRoots[1].Role != "source_snapshot" || snapshot.OwnedRoots[1].Path != snapshot.SourceRoot || snapshot.OwnedRoots[1].CleanupRoot) {
		bindingsValid = false
		record(authenticationError("owned audit cleanup snapshot identity binding"))
	}

	if transientsClean && bindingsValid {
		if len(invocations) > 0 {
			_, err := auditCleanupRootSetPresent(expectedSessionDir, snapshot.RunRoot)
			record(err)
		}
		if first == nil {
			record(scrubAndRemoveAuthenticatedAuditRoots(policy.namespaceAuthority, sharedRoots))
			record(verifyAuthenticatedAuditCleanupRootsAbsent(policy.namespaceAuthority, sharedRoots))
		}
	}
	record(validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid())))
	return first
}

func (executor *auditExecutor) resumeFinalizing(
	intent auditExecutionIntent,
	events *[]auditExecutionEvent,
	entry executionPolicyEntry,
	invocation auditInvocation,
) error {
	if events == nil || len(*events) == 0 {
		return ErrProtocol
	}
	finalizing := (*events)[len(*events)-1]
	if finalizing.State != auditStateFinalizing || invocation.RunID == "" || invocation.RunID != filepath.Base(filepath.Dir(finalizing.PromptPath)) {
		return authenticationError("audit finalizing recovery authority")
	}
	if err := executor.cleanupFinalizingExecution(intent, finalizing, entry); err != nil {
		return err
	}
	if executor.hooks.beforeCompletedPersist != nil {
		executor.hooks.beforeCompletedPersist(invocation)
	}
	if err := verifyAuditFinalizingRootsAbsent(executor.policy.namespaceAuthority, intent, finalizing, entry); err != nil {
		return err
	}
	completed := finalizing
	completed.EventID = auditExecutionEventID(intent, len(*events)+1)
	completed.EventHash = ""
	completed.Sequence = len(*events) + 1
	completed.State = auditStateCompleted
	completed.OccurredAt = time.Now().UTC().Format(time.RFC3339Nano)
	completed.FinalizingEventHash = finalizing.EventHash
	completed.Authentication = auditExecutionEventAuthentication{}
	if err := executor.appendEvent(events, completed); err != nil {
		return err
	}
	if executor.hooks.afterCompletedPersist != nil {
		executor.hooks.afterCompletedPersist(invocation)
	}
	return nil
}

func (executor *auditExecutor) cleanupFinalizingExecution(intent auditExecutionIntent, finalizing auditExecutionEvent, entry executionPolicyEntry) error {
	roots, err := auditFinalizingAuthenticatedRoots(intent, finalizing, entry)
	if err != nil {
		return err
	}
	var first error
	record := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	record(scrubAndRemoveAuthenticatedAuditRoots(executor.policy.namespaceAuthority, roots))
	record(executor.policy.ValidateEffectBoundary(entry))
	record(validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid())))
	record(verifyAuthenticatedAuditCleanupRootsAbsent(executor.policy.namespaceAuthority, roots))
	return first
}

func verifyAuditFinalizingRootsAbsent(authority *auditNamespaceAuthority, intent auditExecutionIntent, event auditExecutionEvent, entry executionPolicyEntry) error {
	roots, err := auditFinalizingAuthenticatedRoots(intent, event, entry)
	if err != nil {
		return err
	}
	return verifyAuthenticatedAuditCleanupRootsAbsent(authority, roots)
}

func auditFinalizingAuthenticatedRoots(intent auditExecutionIntent, event auditExecutionEvent, entry executionPolicyEntry) ([]auditOwnedRootIdentity, error) {
	report, err := decodeAuditEvidenceReport(intent, event)
	if err != nil {
		return nil, err
	}
	if err := validateAuditEvidencePolicy(report, entry); err != nil {
		return nil, err
	}
	return append([]auditOwnedRootIdentity(nil), report.OwnedRoots...), nil
}

var errAuditMalformedTimeoutEvidence = fmt.Errorf("%w: malformed timeout evidence", ErrAuthentication)

func classifyAuditRunFailure(runErr error, result auditInvocationResult) (string, bool) {
	var termination *auditTerminationError
	if errors.As(runErr, &termination) && protocolIdentifierPattern.MatchString(termination.FailureClass) {
		return termination.FailureClass, true
	}
	if result.processWaiter != nil {
		return "process_identity_unavailable", true
	}
	var stage *auditInvocationStageError
	if errors.As(runErr, &stage) && protocolIdentifierPattern.MatchString(stage.failureClass) {
		return stage.failureClass, false
	}
	if errors.Is(runErr, errAuditMalformedTimeoutEvidence) {
		return "malformed_timeout_evidence", false
	}
	return "direct_omp_or_capture_verification_failed", false
}

func joinUnconfirmedAuditInvocation(
	ctx context.Context,
	result auditInvocationResult,
	operations auditProcessOperations,
	bounds auditTerminationBounds,
) auditTerminationResult {
	if ctx == nil || operations == nil || result.processWaiter == nil || !validAuditTerminationBounds(bounds) {
		return auditTerminationResult{Outcome: auditTerminationFailure, Failure: ErrProtocol}
	}
	identity := auditProcessIdentity{
		PID: result.PID, PGID: result.PGID, ProcessStartIdentity: result.ProcessStartIdentity, StartedAt: result.StartedAt,
	}
	if identity.PID <= 0 || identity.PGID != identity.PID || identity.ProcessStartIdentity == "" {
		if err := result.processWaiter.await(ctx, bounds.KillGrace); err != nil {
			return auditTerminationResult{Outcome: auditTerminationFailure, Failure: err}
		}
		observation := observeNaturalAuditProcessGroupExit(ctx, identity.PGID, operations, bounds)
		if observation.Outcome == auditTerminationConfirmedExit {
			return observation
		}
		var termination *auditTerminationError
		if errors.As(observation.Failure, &termination) && termination.FailureClass == "group_inspection_failed" {
			return observation
		}
		return failedAuditTermination("process_identity_unavailable", ErrAuthentication)
	}
	return confirmAuditProcessExit(ctx, identity, result.processWaiter, operations, bounds)
}

func (executor *auditExecutor) finishOrRetainUnconfirmedAuditProcess(
	active *activeAuditExecution,
	result auditInvocationResult,
	cleanup func() error,
) {
	joined := joinUnconfirmedAuditInvocation(context.Background(), result, executor.processOperations, executor.terminationBounds).Outcome == auditTerminationConfirmedExit
	if joined && closeAuditRuntimeAuthorityLease(&result) == nil && cleanup != nil && cleanup() == nil {
		return
	}
	executor.mu.Lock()
	active.pending = &pendingAuditProcess{result: result, cleanup: cleanup, joined: joined}
	executor.mu.Unlock()
}

func closeAuditRuntimeAuthorityLease(result *auditInvocationResult) error {
	if result == nil || result.runtimeAuthorityLease == nil {
		return nil
	}
	if err := result.runtimeAuthorityLease.Close(); err != nil {
		return err
	}
	result.runtimeAuthorityLease = nil
	return nil
}

func (executor *auditExecutor) retryPendingAuditProcess(ctx context.Context, envelopeHash string, active *activeAuditExecution) error {
	executor.mu.Lock()
	pending := active.pending
	executor.mu.Unlock()
	if pending == nil {
		return nil
	}
	if !pending.joined {
		identity := auditProcessIdentity{
			PID: pending.result.PID, PGID: pending.result.PGID,
			ProcessStartIdentity: pending.result.ProcessStartIdentity, StartedAt: pending.result.StartedAt,
		}
		var termination auditTerminationResult
		if pending.result.processWaiter != nil && identity.ProcessStartIdentity == "" {
			termination = joinUnconfirmedAuditInvocation(ctx, pending.result, executor.processOperations, executor.terminationBounds)
		} else {
			termination = terminateOwnedAuditProcess(ctx, identity, pending.result.processWaiter, executor.processOperations, executor.terminationBounds)
		}
		if termination.Outcome != auditTerminationConfirmedExit {
			return termination.Failure
		}
		pending.joined = true
	}
	if err := closeAuditRuntimeAuthorityLease(&pending.result); err != nil {
		return err
	}
	if pending.cleanup == nil {
		return ErrProtocol
	}
	if err := pending.cleanup(); err != nil {
		return err
	}
	executor.mu.Lock()
	if active.pending == pending {
		active.pending = nil
		delete(executor.active, envelopeHash)
	}
	executor.mu.Unlock()
	return nil
}

func classifyAuditSnapshotFailure(err error) string {
	message := err.Error()
	for fragment, class := range map[string]string{
		"pinned Git commit identity": "snapshot_commit_identity_failed",
		"pinned Git commit object":   "snapshot_commit_object_failed",
		"pinned Git tree identity":   "snapshot_tree_identity_failed",
		"pinned Git command failed":  "snapshot_git_command_failed",
		"canonical Git archive hash": "snapshot_archive_hash_failed",
		"Git archive":                "snapshot_archive_validation_failed",
		"execution policy":           "snapshot_policy_boundary_failed",
		"audit snapshot":             "snapshot_filesystem_boundary_failed",
	} {
		if strings.Contains(message, fragment) {
			return class
		}
	}
	return "snapshot_materialization_failed"
}

func (executor *auditExecutor) resolveEntry(intent auditExecutionIntent) (executionPolicyEntry, error) {
	entry, exists := executor.policy.entries[intent.LaunchSpecHash]
	if !exists || entry.TaskID != intent.TaskID || entry.PolicyHash != intent.PolicyHash || entry.RouteMappingHash != intent.RouteMappingHash ||
		entry.RepositoryIdentityHash != intent.RepositoryIdentityHash || entry.GitCommit != intent.GitCommit || entry.GitTree != intent.GitTree ||
		entry.SourceArchiveSHA256 != intent.SourceArchiveSHA256 || entry.Wrapper.SHA256 != intent.WrapperSHA256 || entry.AttemptCap != intent.AttemptCap {
		return executionPolicyEntry{}, authenticationError("audit intent execution policy")
	}
	if err := executor.policy.ValidateEffectBoundary(entry); err != nil {
		return cloneExecutionPolicyEntry(entry), err
	}
	return cloneExecutionPolicyEntry(entry), nil
}

func (executor *auditExecutor) newEvent(intent auditExecutionIntent, events []auditExecutionEvent, state string, attempt int) auditExecutionEvent {
	sequence := len(events) + 1
	return auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion,
		EventID:       fmt.Sprintf("audit_event_%s_%03d", hashIDFragment(intent.IntentHash), sequence),
		IntentHash:    intent.IntentHash, Sequence: sequence, State: state, Attempt: attempt,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func bindAuditEventDescriptor(event *auditExecutionEvent, entry executionPolicyEntry, intent auditExecutionIntent, resume auditResume) error {
	if event == nil {
		return ErrProtocol
	}
	event.PromptSHA256 = auditPromptSHA256(resume.SynthesizeOnly)
	event.SessionRunID = intent.RunID
	event.ResumeSessionUUID = resume.SessionUUID
	event.SynthesizeOnly = resume.SynthesizeOnly
	descriptorHash, err := auditCommandDescriptorHash(entry, event.PromptSHA256, event.SessionRunID, resume)
	if err != nil {
		return err
	}
	event.CommandDescriptorHash = descriptorHash
	return nil
}

func bindAuditEventInvocation(event *auditExecutionEvent, invocation auditInvocation) {
	event.CommandDescriptorHash = invocation.CommandDescriptorHash
	event.PromptSHA256 = invocation.PromptSHA256
	event.SessionRunID = invocation.SessionRunID
	event.ResumeSessionUUID = invocation.Resume.SessionUUID
	event.SynthesizeOnly = invocation.Resume.SynthesizeOnly
}

func (executor *auditExecutor) newInvocationEvent(intent auditExecutionIntent, events []auditExecutionEvent, state string, attempt int, invocation auditInvocation) auditExecutionEvent {
	event := executor.newEvent(intent, events, state, attempt)
	bindAuditEventInvocation(&event, invocation)
	return event
}

func (executor *auditExecutor) terminalEvent(intent auditExecutionIntent, events []auditExecutionEvent, state string, attempt int, invocation auditInvocation, result auditInvocationResult) auditExecutionEvent {
	event := executor.newInvocationEvent(intent, events, state, attempt, invocation)
	event.PID, event.PGID, event.ProcessStartIdentity, event.ExitCode = result.PID, result.PGID, result.ProcessStartIdentity, result.ExitCode
	event.ProcessStartedAt, event.ProcessFinishedAt = result.StartedAt, result.FinishedAt
	event.StdoutSHA256, event.StderrSHA256 = result.StdoutSHA256, result.StderrSHA256
	event.WorkPath, event.OutputPath = invocation.WorkDir, invocation.OutputPath
	event.SessionPath, event.PromptPath, event.TemporaryPath = invocation.SessionDir, invocation.PromptPath, invocation.TemporaryDir
	return event
}

func (executor *auditExecutor) appendEvent(events *[]auditExecutionEvent, event auditExecutionEvent) error {
	sealed, err := sealAuditExecutionEvent(event)
	if err != nil {
		return err
	}
	if err := executor.journal.appendAuditEvent(context.Background(), sealed); err != nil {
		return err
	}
	*events = append(*events, sealed)
	return nil
}

func (executor *auditExecutor) appendRequiredTerminal(events *[]auditExecutionEvent, event auditExecutionEvent) (string, error) {
	requestedState := event.State
	if err := executor.appendEvent(events, event); err == nil {
		return requestedState, nil
	} else if requestedState == auditStateWaitingForHuman || events == nil ||
		len(*events) != 0 && (*events)[len(*events)-1].State != auditStatePrepared &&
			(*events)[len(*events)-1].State != auditStateRunning && (*events)[len(*events)-1].State != auditStateTimedOut {
		return "", err
	} else {
		fallback := event
		fallback.EventHash = ""
		fallback.Authentication = auditExecutionEventAuthentication{}
		fallback.State = auditStateWaitingForHuman
		fallback.FailureClass = "terminal_event_persistence_failed"
		fallback.OutputSHA256 = ""
		fallback.OutputSize = 0
		fallback.EvidenceJSON = ""
		fallback.EvidenceHash = ""
		fallback.FinalizingEventHash = ""
		if fallbackErr := executor.appendEvent(events, fallback); fallbackErr != nil {
			return "", errors.Join(err, fallbackErr)
		}
		return auditStateWaitingForHuman, nil
	}
}

func (executor *auditExecutor) appendWaiting(intent auditExecutionIntent, events *[]auditExecutionEvent, attempt int, resume auditResume, failure string, entry executionPolicyEntry) error {
	if events == nil {
		return ErrProtocol
	}
	event := executor.newEvent(intent, *events, auditStateWaitingForHuman, attempt)
	if err := bindAuditEventDescriptor(&event, entry, intent, resume); err != nil {
		return err
	}
	event.FailureClass = failure
	event.WorkPath, event.OutputPath, event.SessionPath, event.PromptPath, event.TemporaryPath = auditAttemptPaths(intent, entry, attempt)
	if len(*events) != 0 && (*events)[len(*events)-1].Attempt == attempt {
		last := (*events)[len(*events)-1]
		event.CommandDescriptorHash, event.PromptSHA256, event.SessionRunID = last.CommandDescriptorHash, last.PromptSHA256, last.SessionRunID
		event.ResumeSessionUUID, event.SynthesizeOnly = last.ResumeSessionUUID, last.SynthesizeOnly
		event.PID, event.PGID, event.ProcessStartIdentity = last.PID, last.PGID, last.ProcessStartIdentity
		event.ProcessStartedAt, event.ProcessFinishedAt = last.ProcessStartedAt, last.ProcessFinishedAt
		event.ExitCode, event.StdoutSHA256, event.StderrSHA256 = last.ExitCode, last.StdoutSHA256, last.StderrSHA256
		event.SessionUUID = last.SessionUUID
	}
	return executor.appendEvent(events, event)
}

func auditAttemptPaths(intent auditExecutionIntent, entry executionPolicyEntry, attempt int) (string, string, string, string, string) {
	attemptRunID := intent.RunID + "_attempt_" + strconv.Itoa(attempt)
	return filepath.Join(entry.WorkRoot, intent.RunID, "source"), filepath.Join(entry.OutputRoot, attemptRunID, "audit-output.json"),
		filepath.Join(entry.SessionRoot, intent.RunID), filepath.Join(entry.PromptRoot, attemptRunID, "audit-prompt.txt"), filepath.Join(entry.TemporaryRoot, attemptRunID)
}

func deriveRecoveredAuditInvocations(
	intent auditExecutionIntent,
	events []auditExecutionEvent,
	entry executionPolicyEntry,
) ([]auditInvocation, error) {
	// The journal authenticates and policy-validates history before recovery.
	lastAttempt := 1
	if len(events) != 0 {
		lastAttempt = events[len(events)-1].Attempt
	}
	for _, event := range events {
		workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, entry, event.Attempt)
		if event.WorkPath != workPath || event.OutputPath != outputPath || event.SessionPath != sessionPath ||
			event.PromptPath != promptPath || event.TemporaryPath != temporaryPath {
			return nil, authenticationError("audit recovery execution policy paths")
		}
	}
	invocations := make([]auditInvocation, 0, lastAttempt)
	for attempt := 1; attempt <= lastAttempt; attempt++ {
		attemptRunID := intent.RunID + "_attempt_" + strconv.Itoa(attempt)
		workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, entry, attempt)
		invocations = append(invocations, auditInvocation{
			RunID: attemptRunID, SessionRunID: intent.RunID,
			PromptDir: filepath.Dir(promptPath), PromptPath: promptPath,
			OutputDir: filepath.Dir(outputPath), OutputPath: outputPath,
			SessionDir: sessionPath, TemporaryDir: temporaryPath, WorkDir: workPath,
			OMPExecutablePath: entry.OMPExecutable.Path,
		})
	}
	return invocations, nil
}

// cleanupRecoveredAuditExecution is legacy path cleanup for non-completing
// prepared/running failure recovery. Finalizing recovery must use the signed
// identities in auditFinalizingAuthenticatedRoots and never enters here.
func cleanupRecoveredAuditExecution(intent auditExecutionIntent, events []auditExecutionEvent, entry executionPolicyEntry) error {
	if len(events) != 0 && (events[len(events)-1].State == auditStateFinalizing || events[len(events)-1].State == auditStateCompleted) {
		return authenticationError("path-only recovery cannot authorize completion")
	}
	invocations, err := deriveRecoveredAuditInvocations(intent, events, entry)
	if err != nil {
		return err
	}
	var first error
	for _, invocation := range invocations {
		if err := cleanupAuditInvocationAll(entry, invocation); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (executor *auditExecutor) cleanupRecoveredExecution(intent auditExecutionIntent, events []auditExecutionEvent, entry executionPolicyEntry) error {
	if executor.hooks.cleanupRecovered != nil {
		return executor.hooks.cleanupRecovered(intent, events, entry)
	}
	return cleanupRecoveredAuditExecution(intent, events, entry)
}

func auditFailureDescriptorHash(intent auditExecutionIntent, attempt int) string {
	hash, err := canonicalHash(map[string]any{
		"schema_version": "ananke.local-trusted-supervisor-failed-command-descriptor.v1",
		"intent_hash":    intent.IntentHash, "attempt": attempt,
	})
	if err != nil {
		panic("audit failure descriptor must be canonicalizable")
	}
	return hash
}

func buildAuditExecutionIntent(envelope store.ExternalSupervisorEnvelope, entry executionPolicyEntry, responseBytes []byte, createdAt time.Time) (auditExecutionIntent, error) {
	var response wireResponse
	if decodeCanonical(responseBytes, &response) != nil || response.Operation != operationDeliver || response.Receipt == nil ||
		response.Receipt.EnvelopeHash != envelope.EnvelopeHash || response.Receipt.AttemptNumber != envelope.AttemptNumber {
		return auditExecutionIntent{}, authenticationError("audit intent receipt binding")
	}
	intent := auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion,
		IntentID:      "audit_intent_" + hashIDFragment(envelope.EnvelopeHash), EnvelopeHash: envelope.EnvelopeHash,
		LaunchSpecHash: envelope.LaunchSpecHash, HandoffID: envelope.HandoffID, ReceiptHash: response.Receipt.ReceiptHash,
		TaskID: entry.TaskID, AttemptCap: entry.AttemptCap, PolicyHash: entry.PolicyHash,
		RouteMappingHash: entry.RouteMappingHash, RepositoryIdentityHash: entry.RepositoryIdentityHash,
		GitCommit: entry.GitCommit, GitTree: entry.GitTree, SourceArchiveSHA256: entry.SourceArchiveSHA256,
		WrapperSHA256: entry.Wrapper.SHA256, RunID: "audit_run_" + hashIDFragment(envelope.EnvelopeHash),
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
	}
	return sealAuditExecutionIntent(intent)
}

func hashIDFragment(hash string) string {
	if len(hash) >= len("sha256:")+16 && strings.HasPrefix(hash, "sha256:") {
		return hash[len("sha256:") : len("sha256:")+16]
	}
	return "invalid"
}

func (executor *auditExecutor) recover() error {
	executions, err := executor.journal.listAuditExecutions(context.Background())
	if err != nil {
		return err
	}
	for _, execution := range executions {
		cancellation, found, err := executor.journal.loadAuditCancellation(context.Background(), execution.Intent.EnvelopeHash)
		if err != nil {
			return err
		}
		if found {
			if cancellation.State != auditCancellationStateRequested {
				continue
			}
			if executor.completeCancellation == nil {
				return authenticationError("requested cancellation completion unavailable")
			}
			executor.mu.Lock()
			if _, exists := executor.active[execution.Intent.EnvelopeHash]; !exists {
				active := executor.newActive()
				active.cancellation = &cancellation
				active.identity = auditProcessIdentity{
					PID: cancellation.Intent.ExpectedPID, PGID: cancellation.Intent.ExpectedPGID,
					ProcessStartIdentity: cancellation.Intent.ExpectedStartIdentity,
				}
				active.cancel()
				executor.active[execution.Intent.EnvelopeHash] = active
				go executor.run(execution.Intent.EnvelopeHash, active)
			}
			executor.mu.Unlock()
			continue
		}
		if len(execution.Events) == 0 {
			executor.Notify(execution.Intent.EnvelopeHash)
			continue
		}
		last := execution.Events[len(execution.Events)-1]
		if last.State == auditStatePrepared || last.State == auditStateRunning || last.State == auditStateFinalizing ||
			last.State == auditStateWaitingForHuman && isAuditTerminationFailureClass(last.FailureClass) && last.PID > 0 {
			executor.Notify(execution.Intent.EnvelopeHash)
		}
	}
	return nil
}

func (executor *auditExecutor) recoverExisting(intent auditExecutionIntent, events []auditExecutionEvent, active *activeAuditExecution) error {
	if len(events) == 0 || active == nil {
		return ErrProtocol
	}
	last := events[len(events)-1]
	entry, err := executor.resolveEntry(intent)
	if err != nil {
		return err
	}
	derived, err := deriveRecoveredAuditInvocations(intent, events, entry)
	if err != nil {
		return err
	}
	if last.Attempt < 1 || last.Attempt > len(derived) {
		return authenticationError("audit recovery attempt")
	}
	cleanup := func() error { return executor.cleanupRecoveredExecution(intent, events, entry) }
	paths := derived[last.Attempt-1]
	if last.State == auditStateFinalizing {
		return executor.resumeFinalizing(intent, &events, entry, paths)
	}
	if last.State == auditStatePrepared {
		failureClass := "restart_after_prepared_unknown"
		if cleanupErr := cleanup(); cleanupErr != nil {
			failureClass = "artifact_cleanup_failed"
		}
		return executor.appendWaiting(intent, &events, last.Attempt, auditResume{SessionUUID: last.ResumeSessionUUID, SynthesizeOnly: last.SynthesizeOnly}, failureClass, entry)
	}
	if last.State == auditStateWaitingForHuman && isAuditTerminationFailureClass(last.FailureClass) && last.PID > 0 {
		expected := auditProcessIdentity{
			PID: last.PID, PGID: last.PGID, ProcessStartIdentity: last.ProcessStartIdentity, StartedAt: last.ProcessStartedAt,
		}
		active.identity = expected
		termination := terminateOwnedAuditProcess(context.Background(), expected, nil, executor.processOperations, executor.terminationBounds)
		if termination.Outcome != auditTerminationConfirmedExit {
			executor.mu.Lock()
			active.pending = &pendingAuditProcess{result: auditInvocationResult{
				PID: expected.PID, PGID: expected.PGID, ProcessStartIdentity: expected.ProcessStartIdentity,
				StartedAt: expected.StartedAt, ExitCode: -1,
			}, cleanup: cleanup}
			executor.mu.Unlock()
			return nil
		}
		return cleanup()
	}
	if last.State != auditStateRunning {
		return nil
	}
	expected := auditProcessIdentity{PID: last.PID, PGID: last.PGID, ProcessStartIdentity: last.ProcessStartIdentity, StartedAt: last.ProcessStartedAt}
	active.identity = expected
	termination := terminateOwnedAuditProcess(context.Background(), expected, nil, executor.processOperations, executor.terminationBounds)
	failureClass := "restart_recovered_process_terminated"
	finishedAt := ""
	if termination.Outcome != auditTerminationConfirmedExit {
		failureClass = cancellationFailureClass(termination.Failure)
		if failureClass != "process_identity_mismatch" {
			executor.mu.Lock()
			active.pending = &pendingAuditProcess{result: auditInvocationResult{
				PID: expected.PID, PGID: expected.PGID, ProcessStartIdentity: expected.ProcessStartIdentity,
				StartedAt: expected.StartedAt, ExitCode: -1,
			}, cleanup: cleanup}
			executor.mu.Unlock()
		}
	} else {
		finishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if cleanupErr := cleanup(); cleanupErr != nil {
			failureClass = "artifact_cleanup_failed"
		}
	}
	waiting := executor.newEvent(intent, events, auditStateWaitingForHuman, last.Attempt)
	waiting.CommandDescriptorHash, waiting.PromptSHA256, waiting.SessionRunID = last.CommandDescriptorHash, last.PromptSHA256, last.SessionRunID
	waiting.ResumeSessionUUID, waiting.SynthesizeOnly = last.ResumeSessionUUID, last.SynthesizeOnly
	waiting.FailureClass = failureClass
	waiting.WorkPath, waiting.OutputPath = paths.WorkDir, paths.OutputPath
	waiting.SessionPath, waiting.PromptPath, waiting.TemporaryPath = paths.SessionDir, paths.PromptPath, paths.TemporaryDir
	waiting.PID, waiting.PGID, waiting.ProcessStartIdentity = expected.PID, expected.PGID, expected.ProcessStartIdentity
	waiting.ProcessStartedAt, waiting.ProcessFinishedAt = last.ProcessStartedAt, finishedAt
	waiting.ExitCode = -1
	_, err = executor.appendRequiredTerminal(&events, waiting)
	return err
}

func inspectAuditProcess(pid int) (auditProcessIdentity, error) {
	identity, exists, err := inspectAuditProcessState(pid)
	if err != nil || !exists || identity.PGID != pid {
		return auditProcessIdentity{}, authenticationError("audit process identity")
	}
	return identity, nil
}

func runtimeGOOS() string {
	return runtime.GOOS
}
