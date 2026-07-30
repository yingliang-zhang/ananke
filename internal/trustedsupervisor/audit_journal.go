package trustedsupervisor

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	auditExecutionIntentSchemaVersion              = "ananke.local-trusted-supervisor-audit-intent.v1"
	auditExecutionEventSchemaVersion               = "ananke.local-trusted-supervisor-audit-event.v4"
	auditExecutionEventAuthenticationSchemaVersion = "ananke.local-trusted-supervisor-audit-event-authentication.v1"
	maxAuditEvidenceBytes                          = 256 * 1024

	auditStatePrepared        = "prepared"
	auditStateRunning         = "running"
	auditStateFinalizing      = "finalizing"
	auditStateCompleted       = "completed"
	auditStateFailed          = "failed"
	auditStateTimedOut        = "timed_out"
	auditStateCancelled       = "cancelled"
	auditStateWaitingForHuman = "waiting_for_human"
)

type auditExecutionIntent struct {
	SchemaVersion          string `json:"schema_version"`
	IntentID               string `json:"intent_id"`
	IntentHash             string `json:"intent_hash"`
	EnvelopeHash           string `json:"envelope_hash"`
	LaunchSpecHash         string `json:"launch_spec_hash"`
	HandoffID              string `json:"handoff_id"`
	ReceiptHash            string `json:"receipt_hash"`
	TaskID                 string `json:"task_id"`
	AttemptCap             int    `json:"attempt_cap"`
	PolicyHash             string `json:"policy_hash"`
	RouteMappingHash       string `json:"route_mapping_hash"`
	RepositoryIdentityHash string `json:"repository_identity_hash"`
	GitCommit              string `json:"git_commit"`
	GitTree                string `json:"git_tree"`
	SourceArchiveSHA256    string `json:"source_archive_sha256"`
	WrapperSHA256          string `json:"wrapper_sha256"`
	RunID                  string `json:"run_id"`
	CreatedAt              string `json:"created_at"`
}

type auditExecutionEvent struct {
	SchemaVersion         string                            `json:"schema_version"`
	EventID               string                            `json:"event_id"`
	EventHash             string                            `json:"event_hash"`
	IntentHash            string                            `json:"intent_hash"`
	Sequence              int                               `json:"sequence"`
	State                 string                            `json:"state"`
	Attempt               int                               `json:"attempt"`
	CommandDescriptorHash string                            `json:"command_descriptor_hash"`
	PromptSHA256          string                            `json:"prompt_sha256"`
	SessionRunID          string                            `json:"session_run_id"`
	ResumeSessionUUID     string                            `json:"resume_session_uuid"`
	SynthesizeOnly        bool                              `json:"synthesize_only"`
	OccurredAt            string                            `json:"occurred_at"`
	PID                   int                               `json:"pid"`
	PGID                  int                               `json:"pgid"`
	ProcessStartIdentity  string                            `json:"process_start_identity"`
	ProcessStartedAt      string                            `json:"process_started_at"`
	ProcessFinishedAt     string                            `json:"process_finished_at"`
	ExitCode              int                               `json:"exit_code"`
	OutputSHA256          string                            `json:"output_sha256"`
	OutputSize            int64                             `json:"output_size"`
	StdoutSHA256          string                            `json:"stdout_sha256"`
	StderrSHA256          string                            `json:"stderr_sha256"`
	SessionUUID           string                            `json:"session_uuid"`
	TimeoutObservation    auditTimeoutEvidence              `json:"timeout_observation"`
	EvidenceJSON          string                            `json:"evidence_json"`
	EvidenceHash          string                            `json:"evidence_hash"`
	FinalizingEventHash   string                            `json:"finalizing_event_hash"`
	FailureClass          string                            `json:"failure_class"`
	WorkPath              string                            `json:"work_path"`
	OutputPath            string                            `json:"output_path"`
	SessionPath           string                            `json:"session_path"`
	PromptPath            string                            `json:"prompt_path"`
	TemporaryPath         string                            `json:"temporary_path"`
	Authentication        auditExecutionEventAuthentication `json:"authentication"`
}

type auditExecutionEventAuthentication struct {
	SchemaVersion       string `json:"schema_version"`
	Algorithm           string `json:"algorithm"`
	EventHash           string `json:"event_hash"`
	IntentHash          string `json:"intent_hash"`
	Sequence            int    `json:"sequence"`
	SignerKeySPKISHA256 string `json:"signer_key_spki_sha256"`
	SignerRootID        string `json:"signer_root_id"`
	Signature           string `json:"signature"`
}

func sealAuditExecutionIntent(intent auditExecutionIntent) (auditExecutionIntent, error) {
	intent.IntentHash = ""
	if intent.SchemaVersion != auditExecutionIntentSchemaVersion || !executionTaskIDPattern.MatchString(intent.IntentID) ||
		!executionTaskIDPattern.MatchString(intent.HandoffID) || !executionTaskIDPattern.MatchString(intent.TaskID) ||
		!executionTaskIDPattern.MatchString(intent.RunID) || intent.AttemptCap < 1 || intent.AttemptCap > 10 ||
		!gitObjectIDPattern.MatchString(intent.GitCommit) || !gitObjectIDPattern.MatchString(intent.GitTree) ||
		!validServerJournalTimestamp(intent.CreatedAt) {
		return auditExecutionIntent{}, ErrProtocol
	}
	for _, hash := range []string{intent.EnvelopeHash, intent.LaunchSpecHash, intent.ReceiptHash, intent.PolicyHash, intent.RouteMappingHash,
		intent.RepositoryIdentityHash, intent.SourceArchiveSHA256, intent.WrapperSHA256} {
		if !protocolHashPattern.MatchString(hash) {
			return auditExecutionIntent{}, ErrProtocol
		}
	}
	hash, err := canonicalHash(intent)
	if err != nil {
		return auditExecutionIntent{}, err
	}
	intent.IntentHash = hash
	return intent, nil
}

func sealAuditExecutionEvent(event auditExecutionEvent) (auditExecutionEvent, error) {
	authentication := event.Authentication
	event.EventHash = ""
	event.Authentication = auditExecutionEventAuthentication{}
	if event.SchemaVersion != auditExecutionEventSchemaVersion || !executionTaskIDPattern.MatchString(event.EventID) ||
		!protocolHashPattern.MatchString(event.IntentHash) || event.Sequence < 1 || event.Attempt < 1 || event.Attempt > 10 ||
		!protocolHashPattern.MatchString(event.CommandDescriptorHash) || !protocolHashPattern.MatchString(event.PromptSHA256) ||
		!executionTaskIDPattern.MatchString(event.SessionRunID) ||
		(event.ResumeSessionUUID != "" && !auditSessionUUIDPattern.MatchString(event.ResumeSessionUUID)) ||
		(event.SynthesizeOnly && event.ResumeSessionUUID == "") || !validServerJournalTimestamp(event.OccurredAt) ||
		!validAuditPrivatePath(event.WorkPath) || !validAuditPrivatePath(event.OutputPath) ||
		!validAuditPrivatePath(event.SessionPath) || !validAuditPrivatePath(event.PromptPath) || !validAuditPrivatePath(event.TemporaryPath) {
		return auditExecutionEvent{}, ErrProtocol
	}
	if event.State != auditStateTimedOut && event.TimeoutObservation != (auditTimeoutEvidence{}) {
		return auditExecutionEvent{}, ErrProtocol
	}
	switch event.State {
	case auditStatePrepared:
		if event.PID != 0 || event.PGID != 0 || event.ProcessStartIdentity != "" || event.ProcessStartedAt != "" || event.ProcessFinishedAt != "" ||
			event.EvidenceJSON != "" || event.EvidenceHash != "" || event.FinalizingEventHash != "" || event.SessionUUID != "" {
			return auditExecutionEvent{}, ErrProtocol
		}
	case auditStateRunning:
		if event.PID <= 0 || event.PGID != event.PID || event.ProcessStartIdentity == "" ||
			!validServerJournalTimestamp(event.ProcessStartedAt) || event.ProcessFinishedAt != "" ||
			event.EvidenceJSON != "" || event.EvidenceHash != "" || event.FinalizingEventHash != "" {
			return auditExecutionEvent{}, ErrProtocol
		}
	case auditStateFinalizing, auditStateCompleted:
		if event.ExitCode != 0 || event.PID <= 0 || event.PGID != event.PID || event.ProcessStartIdentity == "" || event.FailureClass != "" ||
			!validServerJournalTimestamp(event.ProcessStartedAt) || !validServerJournalTimestamp(event.ProcessFinishedAt) ||
			!protocolHashPattern.MatchString(event.OutputSHA256) || event.OutputSize < 0 ||
			event.SessionUUID != "" && !auditSessionUUIDPattern.MatchString(event.SessionUUID) ||
			!validCanonicalAuditEvidence(event.EvidenceJSON, event.EvidenceHash) {
			return auditExecutionEvent{}, ErrProtocol
		}
		if event.State == auditStateFinalizing && event.FinalizingEventHash != "" ||
			event.State == auditStateCompleted && !protocolHashPattern.MatchString(event.FinalizingEventHash) {
			return auditExecutionEvent{}, ErrProtocol
		}
	case auditStateTimedOut:
		observation := event.TimeoutObservation
		if event.PID <= 0 || event.PGID != event.PID || event.ProcessStartIdentity == "" || !validServerJournalTimestamp(event.ProcessStartedAt) ||
			!validServerJournalTimestamp(event.ProcessFinishedAt) || !auditSessionUUIDPattern.MatchString(event.SessionUUID) || event.FinalizingEventHash != "" ||
			!validAuditTimeoutEvidence(observation) || observation.RunID != filepath.Base(filepath.Dir(event.PromptPath)) ||
			observation.SessionRunID != event.SessionRunID || observation.CommandDescriptorHash != event.CommandDescriptorHash ||
			observation.PromptSHA256 != event.PromptSHA256 || observation.ResumeSessionUUID != event.ResumeSessionUUID ||
			observation.SessionUUID != event.SessionUUID || observation.SessionRoot != event.SessionPath || observation.PID != event.PID ||
			observation.PGID != event.PGID || observation.ProcessStartIdentity != event.ProcessStartIdentity ||
			observation.ProcessStartedAt != event.ProcessStartedAt || observation.ProcessFinishedAt != event.ProcessFinishedAt ||
			observation.ExitCode != event.ExitCode || observation.StdoutSHA256 != event.StdoutSHA256 || observation.StderrSHA256 != event.StderrSHA256 {
			return auditExecutionEvent{}, ErrProtocol
		}
	case auditStateFailed:
		identityUnknown := event.PID == 0 && event.PGID == 0 && event.ProcessStartIdentity == "" && event.ProcessStartedAt == "" && event.ProcessFinishedAt == ""
		identityKnown := event.PID > 0 && event.PGID == event.PID && event.ProcessStartIdentity != "" &&
			validServerJournalTimestamp(event.ProcessStartedAt) && validServerJournalTimestamp(event.ProcessFinishedAt)
		if (!identityUnknown && !identityKnown) || event.FailureClass == "" || event.FinalizingEventHash != "" {
			return auditExecutionEvent{}, ErrProtocol
		}
	case auditStateCancelled:
		identityUnknown := event.PID == 0 && event.PGID == 0 && event.ProcessStartIdentity == "" && event.ProcessStartedAt == "" && event.ProcessFinishedAt == ""
		identityKnown := event.PID > 0 && event.PGID == event.PID && event.ProcessStartIdentity != "" && validServerJournalTimestamp(event.ProcessStartedAt) &&
			(event.ProcessFinishedAt == "" || validServerJournalTimestamp(event.ProcessFinishedAt))
		if (!identityUnknown && !identityKnown) || event.FailureClass == "" || event.FinalizingEventHash != "" {
			return auditExecutionEvent{}, ErrProtocol
		}
	case auditStateWaitingForHuman:
		if event.FailureClass == "" || event.FinalizingEventHash != "" {
			return auditExecutionEvent{}, ErrProtocol
		}
	default:
		return auditExecutionEvent{}, ErrProtocol
	}
	for _, hash := range []string{event.OutputSHA256, event.StdoutSHA256, event.StderrSHA256, event.EvidenceHash, event.FinalizingEventHash} {
		if hash != "" && !protocolHashPattern.MatchString(hash) {
			return auditExecutionEvent{}, ErrProtocol
		}
	}
	hash, err := canonicalHash(event)
	if err != nil {
		return auditExecutionEvent{}, err
	}
	event.EventHash = hash
	event.Authentication = authentication
	return event, nil
}

func validAuditPrivatePath(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value && strings.IndexByte(value, 0) < 0
}

func validCanonicalAuditEvidence(value, hash string) bool {
	if value == "" || len(value) > maxAuditEvidenceBytes || !protocolHashPattern.MatchString(hash) || hashJournalBytes([]byte(value)) != hash {
		return false
	}
	var report auditEvidenceReport
	if decodeCanonical([]byte(value), &report) != nil || report.SchemaVersion != auditEvidenceSchemaVersion ||
		!validAuditModelReport(report.ModelReport) || !validAuditEvidenceTests(report.TestsRun) ||
		validateAuditOwnedRootSequence(report.OwnedRoots) != nil {
		return false
	}
	modelBytes, err := marshalCanonical(report.ModelReport)
	return err == nil && report.ModelReportSHA256 == hashJournalBytes(modelBytes) && report.OutputSHA256 == report.ModelReportSHA256
}

func (journal *serverJournal) storeAuditIntent(ctx context.Context, intent auditExecutionIntent) error {
	if journal == nil || ctx == nil {
		return ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return ErrProtocol
	}
	if _, err := journal.requireAuditAuthority(intent); err != nil {
		return err
	}
	if err := journal.validateIdentity(); err != nil {
		return err
	}
	tx, err := journal.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	if err := validateServerJournalContent(ctx, tx, journal.auditAuthority); err != nil {
		return err
	}
	if err := insertAuditIntentTx(ctx, tx, intent); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audit intent: %w", err)
	}
	rollback = false
	return journal.validateIdentity()
}

func insertAuditIntentTx(ctx context.Context, tx *sql.Tx, intent auditExecutionIntent) error {
	sealed, err := sealAuditExecutionIntent(intent)
	if err != nil || sealed != intent {
		return ErrProtocol
	}
	encoded, err := marshalCanonical(intent)
	if err != nil {
		return ErrProtocol
	}
	var existingBytes []byte
	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT intent_bytes, intent_bytes_hash FROM trusted_supervisor_audit_intents WHERE envelope_hash = ?`, intent.EnvelopeHash).
		Scan(&existingBytes, &existingHash)
	if err == nil {
		if existingHash == hashJournalBytes(existingBytes) && bytes.Equal(existingBytes, encoded) {
			return nil
		}
		return ErrReplay
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return authenticationError("read audit intent conflict")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_audit_intents
		(intent_hash, envelope_hash, intent_bytes, intent_bytes_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		intent.IntentHash, intent.EnvelopeHash, encoded, hashJournalBytes(encoded), intent.CreatedAt); err != nil {
		if isSQLiteConstraint(err) {
			return ErrReplay
		}
		return fmt.Errorf("insert audit intent: %w", err)
	}
	return nil
}

func (journal *serverJournal) appendAuditEvent(ctx context.Context, event auditExecutionEvent) error {
	if journal == nil || ctx == nil {
		return ErrProtocol
	}
	sealed, err := sealAuditExecutionEvent(event)
	if err != nil || sealed != event {
		return ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return ErrProtocol
	}
	event, err = journal.auditAuthority.authenticateEvent(event)
	if err != nil {
		return err
	}
	if err := journal.validateIdentity(); err != nil {
		return err
	}
	tx, err := journal.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	if err := validateServerJournalContent(ctx, tx, journal.auditAuthority); err != nil {
		return err
	}
	intent, events, err := loadAuditExecutionTx(ctx, tx, journal.auditAuthority, "", event.IntentHash)
	if err != nil || intent.IntentHash != event.IntentHash {
		return ErrProtocol
	}
	var cancellationRequested int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM trusted_supervisor_cancellation_intents AS cancellation
		LEFT JOIN trusted_supervisor_cancellation_outcomes AS outcome ON outcome.intent_hash = cancellation.intent_hash
		WHERE cancellation.envelope_hash = ? AND outcome.intent_hash IS NULL
	)`, intent.EnvelopeHash).Scan(&cancellationRequested); err != nil {
		return authenticationError("inspect audit cancellation fence")
	}
	if cancellationRequested != 0 {
		return ErrReplay
	}
	if len(events) >= 256 {
		return ErrLimit
	}
	if event.Sequence <= len(events) {
		existing := events[event.Sequence-1]
		if existing == event {
			return nil
		}
		return ErrReplay
	}
	candidate := append(append([]auditExecutionEvent(nil), events...), event)
	if event.Sequence != len(events)+1 {
		return ErrProtocol
	}
	if err := validateAuditExecutionHistory(journal.auditAuthority, intent, candidate); err != nil {
		return err
	}
	encoded, err := marshalCanonical(event)
	if err != nil {
		return ErrProtocol
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_audit_events
		(intent_hash, sequence, state, event_bytes, event_bytes_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		event.IntentHash, event.Sequence, event.State, encoded, hashJournalBytes(encoded), event.OccurredAt); err != nil {
		if isSQLiteConstraint(err) {
			return ErrReplay
		}
		return fmt.Errorf("insert audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audit event: %w", err)
	}
	rollback = false
	return journal.validateIdentity()
}

func validateAuditExecutionHistory(authority *auditJournalAuthority, intent auditExecutionIntent, events []auditExecutionEvent) error {
	sealedIntent, err := sealAuditExecutionIntent(intent)
	if err != nil || sealedIntent != intent {
		return authenticationError("audit execution intent binding")
	}
	entry, err := authority.resolveIntent(intent)
	if err != nil {
		return err
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, intent.CreatedAt)
	var previous *auditExecutionEvent
	for index := range events {
		event := events[index]
		sealedEvent, sealErr := sealAuditExecutionEvent(event)
		if err := authority.verifyEvent(event); err != nil {
			return err
		}
		occurredAt, timestampErr := time.Parse(time.RFC3339Nano, event.OccurredAt)
		resume := auditResume{SessionUUID: event.ResumeSessionUUID, SynthesizeOnly: event.SynthesizeOnly}
		descriptorHash, descriptorErr := auditCommandDescriptorHash(entry, event.PromptSHA256, event.SessionRunID, resume)
		if sealErr != nil || sealedEvent != event || event.IntentHash != intent.IntentHash || event.Sequence != index+1 ||
			event.EventID != auditExecutionEventID(intent, event.Sequence) || event.Attempt < 1 || event.Attempt > intent.AttemptCap ||
			timestampErr != nil || occurredAt.Before(createdAt) || event.SessionRunID != intent.RunID ||
			event.PromptSHA256 != auditPromptSHA256(event.SynthesizeOnly) || descriptorErr != nil || descriptorHash != event.CommandDescriptorHash ||
			!validAuditExecutionEventPaths(intent, entry, event) {
			return authenticationError("audit execution event policy binding")
		}
		if previous == nil {
			if event.Attempt != 1 || event.ResumeSessionUUID != "" || event.SynthesizeOnly ||
				event.State != auditStatePrepared && event.State != auditStateCancelled && event.State != auditStateWaitingForHuman {
				return authenticationError("audit execution initial transition")
			}
		} else {
			previousAt, _ := time.Parse(time.RFC3339Nano, previous.OccurredAt)
			if occurredAt.Before(previousAt) || !validExactAuditTransition(*previous, event) {
				return authenticationError("audit execution transition")
			}
			if event.Attempt == previous.Attempt {
				if event.CommandDescriptorHash != previous.CommandDescriptorHash || event.PromptSHA256 != previous.PromptSHA256 ||
					event.SessionRunID != previous.SessionRunID || event.ResumeSessionUUID != previous.ResumeSessionUUID ||
					event.SynthesizeOnly != previous.SynthesizeOnly || event.WorkPath != previous.WorkPath ||
					event.OutputPath != previous.OutputPath || event.SessionPath != previous.SessionPath ||
					event.PromptPath != previous.PromptPath || event.TemporaryPath != previous.TemporaryPath {
					return authenticationError("audit attempt descriptor continuity")
				}
			} else if event.ResumeSessionUUID != previous.SessionUUID {
				return authenticationError("audit retry resume binding")
			}
			if previous.State == auditStateRunning && isAuditProcessExitState(event.State) ||
				previous.State == auditStateTimedOut && event.Attempt == previous.Attempt {
				if event.PID != previous.PID || event.PGID != previous.PGID || event.ProcessStartIdentity != previous.ProcessStartIdentity ||
					event.ProcessStartedAt != previous.ProcessStartedAt {
					return authenticationError("audit process identity continuity")
				}
			}
			if previous.State == auditStateFinalizing && !validAuditFinalizingCompletion(*previous, event) {
				return authenticationError("audit finalizing completion continuity")
			}
		}
		var startedAt time.Time
		if event.ProcessStartedAt != "" {
			var parseErr error
			startedAt, parseErr = time.Parse(time.RFC3339Nano, event.ProcessStartedAt)
			if parseErr != nil || occurredAt.Before(startedAt) {
				return authenticationError("audit process start timestamp")
			}
		}
		if event.ProcessFinishedAt != "" {
			finishedAt, parseErr := time.Parse(time.RFC3339Nano, event.ProcessFinishedAt)
			if parseErr != nil || occurredAt.Before(finishedAt) || !startedAt.IsZero() && finishedAt.Before(startedAt) {
				return authenticationError("audit process finish timestamp")
			}
		}
		switch event.State {
		case auditStateFinalizing, auditStateCompleted:
			report, err := decodeAuditEvidenceReport(intent, event)
			if err != nil {
				return err
			}
			if err := validateAuditEvidencePolicy(report, entry); err != nil {
				return err
			}
		default:
			if event.EvidenceJSON != "" || event.EvidenceHash != "" || event.OutputSHA256 != "" || event.OutputSize != 0 || event.FinalizingEventHash != "" {
				return authenticationError("non-finalizing audit evidence")
			}
		}
		previous = &events[index]
	}
	return nil
}

func auditExecutionEventID(intent auditExecutionIntent, sequence int) string {
	return fmt.Sprintf("audit_event_%s_%03d", hashIDFragment(intent.IntentHash), sequence)
}

func validAuditExecutionEventPaths(intent auditExecutionIntent, entry executionPolicyEntry, event auditExecutionEvent) bool {
	workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, entry, event.Attempt)
	return event.WorkPath == workPath && event.OutputPath == outputPath && event.SessionPath == sessionPath &&
		event.PromptPath == promptPath && event.TemporaryPath == temporaryPath
}

func validExactAuditTransition(previous, next auditExecutionEvent) bool {
	if previous.State == auditStateCompleted || previous.State == auditStateFailed || previous.State == auditStateCancelled || previous.State == auditStateWaitingForHuman {
		return false
	}
	switch previous.State {
	case auditStatePrepared:
		return next.Attempt == previous.Attempt && (next.State == auditStateRunning || next.State == auditStateCancelled || next.State == auditStateWaitingForHuman)
	case auditStateRunning:
		return next.Attempt == previous.Attempt && isAuditProcessExitState(next.State)
	case auditStateFinalizing:
		return next.Attempt == previous.Attempt && next.State == auditStateCompleted
	case auditStateTimedOut:
		return next.Attempt == previous.Attempt+1 && next.State == auditStatePrepared ||
			next.Attempt == previous.Attempt && (next.State == auditStateCancelled || next.State == auditStateWaitingForHuman)
	default:
		return false
	}
}

func isAuditProcessExitState(state string) bool {
	switch state {
	case auditStateFinalizing, auditStateFailed, auditStateTimedOut, auditStateCancelled, auditStateWaitingForHuman:
		return true
	default:
		return false
	}
}

func validAuditFinalizingCompletion(finalizing, completed auditExecutionEvent) bool {
	return completed.State == auditStateCompleted && completed.FinalizingEventHash == finalizing.EventHash &&
		completed.IntentHash == finalizing.IntentHash && completed.Attempt == finalizing.Attempt &&
		completed.CommandDescriptorHash == finalizing.CommandDescriptorHash && completed.PromptSHA256 == finalizing.PromptSHA256 &&
		completed.SessionRunID == finalizing.SessionRunID && completed.ResumeSessionUUID == finalizing.ResumeSessionUUID &&
		completed.SynthesizeOnly == finalizing.SynthesizeOnly && completed.PID == finalizing.PID && completed.PGID == finalizing.PGID &&
		completed.ProcessStartIdentity == finalizing.ProcessStartIdentity && completed.ProcessStartedAt == finalizing.ProcessStartedAt &&
		completed.ProcessFinishedAt == finalizing.ProcessFinishedAt && completed.ExitCode == finalizing.ExitCode &&
		completed.OutputSHA256 == finalizing.OutputSHA256 && completed.OutputSize == finalizing.OutputSize &&
		completed.StdoutSHA256 == finalizing.StdoutSHA256 && completed.StderrSHA256 == finalizing.StderrSHA256 &&
		completed.SessionUUID == finalizing.SessionUUID && completed.EvidenceJSON == finalizing.EvidenceJSON &&
		completed.EvidenceHash == finalizing.EvidenceHash && completed.FailureClass == finalizing.FailureClass &&
		completed.WorkPath == finalizing.WorkPath && completed.OutputPath == finalizing.OutputPath &&
		completed.SessionPath == finalizing.SessionPath && completed.PromptPath == finalizing.PromptPath &&
		completed.TemporaryPath == finalizing.TemporaryPath
}

func (journal *serverJournal) loadAuditExecution(ctx context.Context, envelopeHash string) (auditExecutionIntent, []auditExecutionEvent, error) {
	if journal == nil || ctx == nil || !protocolHashPattern.MatchString(envelopeHash) {
		return auditExecutionIntent{}, nil, ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return auditExecutionIntent{}, nil, ErrProtocol
	}
	if err := journal.validateIdentity(); err != nil {
		return auditExecutionIntent{}, nil, err
	}
	if err := validateServerJournalContent(ctx, journal.db, journal.auditAuthority); err != nil {
		return auditExecutionIntent{}, nil, err
	}
	return loadAuditExecutionTx(ctx, journal.db, journal.auditAuthority, envelopeHash, "")
}

type auditExecutionQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadAuditExecutionTx(ctx context.Context, queryer auditExecutionQueryer, authority *auditJournalAuthority, envelopeHash, intentHash string) (auditExecutionIntent, []auditExecutionEvent, error) {
	var intentBytes []byte
	var intentBytesHash string
	query := `SELECT intent_bytes, intent_bytes_hash FROM trusted_supervisor_audit_intents WHERE envelope_hash = ?`
	key := envelopeHash
	if intentHash != "" {
		query = `SELECT intent_bytes, intent_bytes_hash FROM trusted_supervisor_audit_intents WHERE intent_hash = ?`
		key = intentHash
	}
	if err := queryer.QueryRowContext(ctx, query, key).Scan(&intentBytes, &intentBytesHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auditExecutionIntent{}, nil, ErrReplay
		}
		return auditExecutionIntent{}, nil, authenticationError("load audit intent")
	}
	var intent auditExecutionIntent
	if intentBytesHash != hashJournalBytes(intentBytes) || decodeCanonical(intentBytes, &intent) != nil {
		return auditExecutionIntent{}, nil, authenticationError("audit intent fingerprint")
	}
	sealedIntent, err := sealAuditExecutionIntent(intent)
	if err != nil || sealedIntent != intent {
		return auditExecutionIntent{}, nil, authenticationError("audit intent binding")
	}
	rows, err := queryer.QueryContext(ctx, `SELECT event_bytes, event_bytes_hash FROM trusted_supervisor_audit_events WHERE intent_hash = ? ORDER BY sequence`, intent.IntentHash)
	if err != nil {
		return auditExecutionIntent{}, nil, authenticationError("load audit events")
	}
	defer rows.Close()
	events := make([]auditExecutionEvent, 0)
	for rows.Next() {
		var eventBytes []byte
		var eventBytesHash string
		if err := rows.Scan(&eventBytes, &eventBytesHash); err != nil || eventBytesHash != hashJournalBytes(eventBytes) {
			return auditExecutionIntent{}, nil, authenticationError("audit event fingerprint")
		}
		var event auditExecutionEvent
		if decodeCanonical(eventBytes, &event) != nil {
			return auditExecutionIntent{}, nil, authenticationError("audit event canonical schema")
		}
		sealed, err := sealAuditExecutionEvent(event)
		if err != nil || sealed != event {
			return auditExecutionIntent{}, nil, authenticationError("audit event binding")
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return auditExecutionIntent{}, nil, authenticationError("read audit events")
	}
	if err := validateAuditExecutionHistory(authority, intent, events); err != nil {
		return auditExecutionIntent{}, nil, err
	}
	return intent, events, nil
}

func validateAuditJournalContent(ctx context.Context, queryer auditExecutionQueryer, authority *auditJournalAuthority) error {
	rows, err := queryer.QueryContext(ctx, `SELECT envelope_hash FROM trusted_supervisor_audit_intents ORDER BY envelope_hash`)
	if err != nil {
		return authenticationError("inspect audit journal")
	}
	var envelopeHashes []string
	for rows.Next() {
		var envelopeHash string
		if err := rows.Scan(&envelopeHash); err != nil || !protocolHashPattern.MatchString(envelopeHash) {
			_ = rows.Close()
			return authenticationError("read audit journal")
		}
		envelopeHashes = append(envelopeHashes, envelopeHash)
	}
	if err := rows.Err(); err != nil || rows.Close() != nil {
		return authenticationError("read audit journal")
	}
	for _, envelopeHash := range envelopeHashes {
		if _, _, err := loadAuditExecutionTx(ctx, queryer, authority, envelopeHash, ""); err != nil {
			return err
		}
	}
	return nil
}

type auditExecutionRecord struct {
	Intent auditExecutionIntent
	Events []auditExecutionEvent
}

func (journal *serverJournal) listAuditExecutions(ctx context.Context) ([]auditExecutionRecord, error) {
	if journal == nil || ctx == nil {
		return nil, ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return nil, ErrProtocol
	}
	if err := journal.validateIdentity(); err != nil {
		return nil, err
	}
	if err := validateServerJournalContent(ctx, journal.db, journal.auditAuthority); err != nil {
		return nil, err
	}
	rows, err := journal.db.QueryContext(ctx, `SELECT envelope_hash FROM trusted_supervisor_audit_intents ORDER BY envelope_hash`)
	if err != nil {
		return nil, authenticationError("list audit executions")
	}
	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			_ = rows.Close()
			return nil, authenticationError("list audit execution hash")
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil || rows.Close() != nil {
		return nil, authenticationError("list audit executions")
	}
	records := make([]auditExecutionRecord, 0, len(hashes))
	for _, hash := range hashes {
		intent, events, err := loadAuditExecutionTx(ctx, journal.db, journal.auditAuthority, hash, "")
		if err != nil {
			return nil, err
		}
		records = append(records, auditExecutionRecord{Intent: intent, Events: events})
	}
	return records, nil
}
