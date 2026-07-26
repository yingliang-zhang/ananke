package trustedsupervisor

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	auditCancellationIntentSchemaVersion = "ananke.local-trusted-supervisor-cancellation-intent.v1"
	auditCancellationStateRequested      = "requested"
	auditCancellationStateCompleted      = "completed"
	auditCancellationStateFailed         = "failed"
	auditCancellationOutcomeCompleted    = "completed"
	auditCancellationOutcomeFailed       = "failed"
)

type auditCancellationIntent struct {
	SchemaVersion         string `json:"schema_version"`
	IntentID              string `json:"intent_id"`
	IntentHash            string `json:"intent_hash"`
	RequestHash           string `json:"request_hash"`
	RequestBytes          []byte `json:"request_bytes"`
	RequestBytesHash      string `json:"request_bytes_hash"`
	OperationKey          string `json:"operation_key"`
	RequestNonceHash      string `json:"request_nonce_hash"`
	ResponseNonceHash     string `json:"response_nonce_hash"`
	ExclusivityNonceHash  string `json:"exclusivity_nonce_hash"`
	EnvelopeHash          string `json:"envelope_hash"`
	HandoffID             string `json:"handoff_id"`
	ReceiptHash           string `json:"receipt_hash"`
	CancellationHash      string `json:"cancellation_hash"`
	Attempt               int    `json:"attempt"`
	ExpectedPID           int    `json:"expected_pid"`
	ExpectedPGID          int    `json:"expected_pgid"`
	ExpectedStartIdentity string `json:"expected_start_identity"`
	State                 string `json:"state"`
	RequestedAt           string `json:"requested_at"`
}

type auditCancellationRecord struct {
	Intent        auditCancellationIntent
	State         string
	Outcome       string
	ResponseBytes []byte
}

func sealAuditCancellationIntent(intent auditCancellationIntent) (auditCancellationIntent, error) {
	intent.IntentHash = ""
	intent.RequestBytesHash = hashJournalBytes(intent.RequestBytes)
	identityUnknown := intent.ExpectedPID == 0 && intent.ExpectedPGID == 0 && intent.ExpectedStartIdentity == ""
	identityKnown := intent.ExpectedPID > 0 && intent.ExpectedPGID == intent.ExpectedPID && intent.ExpectedStartIdentity != ""
	if intent.SchemaVersion != auditCancellationIntentSchemaVersion ||
		!executionTaskIDPattern.MatchString(intent.IntentID) || !executionTaskIDPattern.MatchString(intent.HandoffID) ||
		intent.OperationKey != operationCancel+":"+intent.ReceiptHash || intent.Attempt < 1 || intent.Attempt > 10 ||
		intent.State != auditCancellationStateRequested || !validServerJournalTimestamp(intent.RequestedAt) ||
		len(intent.RequestBytes) == 0 || len(intent.RequestBytes) > int(maxFrameBytes) || (!identityUnknown && !identityKnown) {
		return auditCancellationIntent{}, ErrProtocol
	}
	for _, hash := range []string{
		intent.RequestHash, intent.RequestBytesHash, intent.RequestNonceHash, intent.ResponseNonceHash,
		intent.ExclusivityNonceHash, intent.EnvelopeHash, intent.ReceiptHash, intent.CancellationHash,
	} {
		if !protocolHashPattern.MatchString(hash) {
			return auditCancellationIntent{}, ErrProtocol
		}
	}
	if intent.RequestNonceHash == intent.ResponseNonceHash || intent.RequestNonceHash == intent.ExclusivityNonceHash ||
		intent.ResponseNonceHash == intent.ExclusivityNonceHash {
		return auditCancellationIntent{}, ErrProtocol
	}
	hash, err := canonicalHash(intent)
	if err != nil {
		return auditCancellationIntent{}, err
	}
	intent.IntentHash = hash
	return intent, nil
}

func sameAuditCancellationRequest(left, right auditCancellationIntent) bool {
	return left.RequestHash == right.RequestHash && bytes.Equal(left.RequestBytes, right.RequestBytes) &&
		left.OperationKey == right.OperationKey && left.RequestNonceHash == right.RequestNonceHash &&
		left.ResponseNonceHash == right.ResponseNonceHash && left.ExclusivityNonceHash == right.ExclusivityNonceHash &&
		left.EnvelopeHash == right.EnvelopeHash && left.HandoffID == right.HandoffID && left.ReceiptHash == right.ReceiptHash &&
		left.CancellationHash == right.CancellationHash && left.Attempt == right.Attempt
}
func equalAuditCancellationIntent(left, right auditCancellationIntent) bool {
	leftBytes, leftErr := marshalCanonical(left)
	rightBytes, rightErr := marshalCanonical(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func (journal *serverJournal) requestAuditCancellation(ctx context.Context, intent auditCancellationIntent) (auditCancellationRecord, error) {
	if journal == nil || ctx == nil {
		return auditCancellationRecord{}, ErrProtocol
	}
	sealed, err := sealAuditCancellationIntent(intent)
	if err != nil || !equalAuditCancellationIntent(sealed, intent) {
		return auditCancellationRecord{}, ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return auditCancellationRecord{}, ErrProtocol
	}
	if err := journal.validateIdentity(); err != nil {
		return auditCancellationRecord{}, err
	}
	tx, err := journal.db.BeginTx(ctx, nil)
	if err != nil {
		return auditCancellationRecord{}, fmt.Errorf("begin cancellation request: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	if err := validateServerJournalContent(ctx, tx, journal.auditAuthority); err != nil {
		return auditCancellationRecord{}, err
	}

	existing, found, err := loadAuditCancellationTx(ctx, tx, journal.auditAuthority, intent.RequestHash, "")
	if err != nil {
		return auditCancellationRecord{}, err
	}
	if found {
		if !sameAuditCancellationRequest(existing.Intent, intent) {
			return auditCancellationRecord{}, ErrReplay
		}
		if err := tx.Commit(); err != nil {
			return auditCancellationRecord{}, fmt.Errorf("commit cancellation replay load: %w", err)
		}
		rollback = false
		return existing, journal.validateIdentity()
	}

	var conflict int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM trusted_supervisor_requests WHERE request_hash = ? OR operation_key = ?
		UNION ALL
		SELECT 1 FROM trusted_supervisor_nonces WHERE nonce_hash IN (?, ?, ?)
		UNION ALL
		SELECT 1 FROM trusted_supervisor_cancellation_intents
		 WHERE operation_key = ? OR request_nonce_hash IN (?, ?, ?) OR response_nonce_hash IN (?, ?, ?)
		    OR exclusivity_nonce_hash IN (?, ?, ?) OR envelope_hash = ? OR receipt_hash = ? OR cancellation_hash = ?
	)`,
		intent.RequestHash, intent.OperationKey,
		intent.RequestNonceHash, intent.ResponseNonceHash, intent.ExclusivityNonceHash,
		intent.OperationKey,
		intent.RequestNonceHash, intent.ResponseNonceHash, intent.ExclusivityNonceHash,
		intent.RequestNonceHash, intent.ResponseNonceHash, intent.ExclusivityNonceHash,
		intent.RequestNonceHash, intent.ResponseNonceHash, intent.ExclusivityNonceHash,
		intent.EnvelopeHash, intent.ReceiptHash, intent.CancellationHash,
	).Scan(&conflict); err != nil {
		return auditCancellationRecord{}, fmt.Errorf("check cancellation conflict: %w", err)
	}
	if conflict != 0 {
		return auditCancellationRecord{}, ErrReplay
	}
	auditIntent, events, err := loadAuditExecutionTx(ctx, tx, journal.auditAuthority, intent.EnvelopeHash, "")
	if err != nil || auditIntent.HandoffID != intent.HandoffID || auditIntent.ReceiptHash != intent.ReceiptHash || intent.Attempt > auditIntent.AttemptCap {
		return auditCancellationRecord{}, ErrReplay
	}
	if len(events) != 0 {
		last := events[len(events)-1]
		switch last.State {
		case auditStateFinalizing, auditStateCompleted, auditStateFailed, auditStateCancelled, auditStateWaitingForHuman:
			return auditCancellationRecord{}, ErrReplay
		case auditStateRunning:
			if intent.ExpectedPID != last.PID || intent.ExpectedPGID != last.PGID || intent.ExpectedStartIdentity != last.ProcessStartIdentity {
				return auditCancellationRecord{}, ErrReplay
			}
		default:
			if intent.ExpectedPID != 0 || intent.ExpectedPGID != 0 || intent.ExpectedStartIdentity != "" {
				return auditCancellationRecord{}, ErrReplay
			}
		}
	} else if intent.ExpectedPID != 0 || intent.ExpectedPGID != 0 || intent.ExpectedStartIdentity != "" {
		return auditCancellationRecord{}, ErrReplay
	}
	encoded, err := marshalCanonical(intent)
	if err != nil {
		return auditCancellationRecord{}, ErrProtocol
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_cancellation_intents
		(intent_hash, request_hash, operation_key, request_nonce_hash, response_nonce_hash, exclusivity_nonce_hash,
		 envelope_hash, receipt_hash, cancellation_hash, intent_bytes, intent_bytes_hash, requested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		intent.IntentHash, intent.RequestHash, intent.OperationKey, intent.RequestNonceHash, intent.ResponseNonceHash,
		intent.ExclusivityNonceHash, intent.EnvelopeHash, intent.ReceiptHash, intent.CancellationHash,
		encoded, hashJournalBytes(encoded), intent.RequestedAt,
	); err != nil {
		if isSQLiteConstraint(err) {
			return auditCancellationRecord{}, ErrReplay
		}
		return auditCancellationRecord{}, fmt.Errorf("insert cancellation intent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return auditCancellationRecord{}, fmt.Errorf("commit requested cancellation: %w", err)
	}
	rollback = false
	if err := journal.validateIdentity(); err != nil {
		return auditCancellationRecord{}, err
	}
	return auditCancellationRecord{Intent: intent, State: auditCancellationStateRequested}, nil
}

func (journal *serverJournal) completeAuditCancellation(
	ctx context.Context,
	intent auditCancellationIntent,
	event auditExecutionEvent,
	outcome string,
	buildResponse func() ([]byte, error),
) (auditCancellationRecord, error) {
	if journal == nil || ctx == nil || buildResponse == nil ||
		(outcome != auditCancellationOutcomeCompleted && outcome != auditCancellationOutcomeFailed) {
		return auditCancellationRecord{}, ErrProtocol
	}
	sealedIntent, intentErr := sealAuditCancellationIntent(intent)
	sealedEvent, eventErr := sealAuditExecutionEvent(event)
	if intentErr != nil || !equalAuditCancellationIntent(sealedIntent, intent) || eventErr != nil || sealedEvent != event ||
		(outcome == auditCancellationOutcomeCompleted && event.State != auditStateCancelled) ||
		(outcome == auditCancellationOutcomeFailed && event.State != auditStateWaitingForHuman) {
		return auditCancellationRecord{}, ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return auditCancellationRecord{}, ErrProtocol
	}
	authenticatedEvent, err := journal.auditAuthority.authenticateEvent(event)
	if err != nil {
		return auditCancellationRecord{}, err
	}
	event = authenticatedEvent
	if err := journal.validateIdentity(); err != nil {
		return auditCancellationRecord{}, err
	}
	tx, err := journal.db.BeginTx(ctx, nil)
	if err != nil {
		return auditCancellationRecord{}, fmt.Errorf("begin cancellation completion: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	if err := validateServerJournalContent(ctx, tx, journal.auditAuthority); err != nil {
		return auditCancellationRecord{}, err
	}
	existing, found, err := loadAuditCancellationTx(ctx, tx, journal.auditAuthority, intent.RequestHash, "")
	if err != nil {
		return auditCancellationRecord{}, err
	}
	if !found || !sameAuditCancellationRequest(existing.Intent, intent) {
		return auditCancellationRecord{}, ErrReplay
	}
	if existing.State != auditCancellationStateRequested {
		if existing.Outcome != outcome {
			return auditCancellationRecord{}, ErrReplay
		}
		if err := tx.Commit(); err != nil {
			return auditCancellationRecord{}, fmt.Errorf("commit completed cancellation replay: %w", err)
		}
		rollback = false
		return existing, journal.validateIdentity()
	}

	auditIntent, events, err := loadAuditExecutionTx(ctx, tx, journal.auditAuthority, intent.EnvelopeHash, "")
	candidate := append(append([]auditExecutionEvent(nil), events...), event)
	if err != nil || auditIntent.EnvelopeHash != intent.EnvelopeHash || auditIntent.HandoffID != intent.HandoffID ||
		auditIntent.ReceiptHash != intent.ReceiptHash || event.IntentHash != auditIntent.IntentHash ||
		event.Sequence != len(events)+1 || validateAuditExecutionHistory(journal.auditAuthority, auditIntent, candidate) != nil {
		return auditCancellationRecord{}, ErrProtocol
	}
	responseBytes, err := buildResponse()
	if err != nil {
		zeroBytes(responseBytes)
		return auditCancellationRecord{}, err
	}
	if len(responseBytes) == 0 || len(responseBytes) > int(maxFrameBytes) {
		zeroBytes(responseBytes)
		return auditCancellationRecord{}, ErrLimit
	}
	eventBytes, err := marshalCanonical(event)
	if err != nil {
		zeroBytes(responseBytes)
		return auditCancellationRecord{}, ErrProtocol
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_audit_events
		(intent_hash, sequence, state, event_bytes, event_bytes_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		event.IntentHash, event.Sequence, event.State, eventBytes, hashJournalBytes(eventBytes), event.OccurredAt); err != nil {
		zeroBytes(responseBytes)
		if isSQLiteConstraint(err) {
			return auditCancellationRecord{}, ErrReplay
		}
		return auditCancellationRecord{}, fmt.Errorf("insert cancellation audit event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_requests
		(request_hash, operation, operation_key, request_nonce_hash, response_nonce_hash, additional_nonce_hash,
		 request_bytes, request_bytes_hash, response_bytes, response_bytes_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		intent.RequestHash, operationCancel, intent.OperationKey, intent.RequestNonceHash, intent.ResponseNonceHash,
		intent.ExclusivityNonceHash, intent.RequestBytes, intent.RequestBytesHash, responseBytes, hashJournalBytes(responseBytes), event.OccurredAt); err != nil {
		zeroBytes(responseBytes)
		if isSQLiteConstraint(err) {
			return auditCancellationRecord{}, ErrReplay
		}
		return auditCancellationRecord{}, fmt.Errorf("insert completed cancellation request: %w", err)
	}
	for role, nonce := range map[string]string{
		"request": intent.RequestNonceHash, "response": intent.ResponseNonceHash, "message": intent.ExclusivityNonceHash,
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_nonces (nonce_hash, request_hash, nonce_role) VALUES (?, ?, ?)`, nonce, intent.RequestHash, role); err != nil {
			zeroBytes(responseBytes)
			if isSQLiteConstraint(err) {
				return auditCancellationRecord{}, ErrReplay
			}
			return auditCancellationRecord{}, fmt.Errorf("insert completed cancellation nonce: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trusted_supervisor_cancellation_outcomes
		(intent_hash, outcome, audit_event_hash, response_bytes, response_bytes_hash, completed_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		intent.IntentHash, outcome, event.EventHash, responseBytes, hashJournalBytes(responseBytes), event.OccurredAt); err != nil {
		zeroBytes(responseBytes)
		if isSQLiteConstraint(err) {
			return auditCancellationRecord{}, ErrReplay
		}
		return auditCancellationRecord{}, fmt.Errorf("insert cancellation outcome: %w", err)
	}
	if err := tx.Commit(); err != nil {
		zeroBytes(responseBytes)
		return auditCancellationRecord{}, fmt.Errorf("commit cancellation completion: %w", err)
	}
	rollback = false
	if err := journal.validateIdentity(); err != nil {
		zeroBytes(responseBytes)
		return auditCancellationRecord{}, err
	}
	state := auditCancellationStateCompleted
	if outcome == auditCancellationOutcomeFailed {
		state = auditCancellationStateFailed
	}
	return auditCancellationRecord{Intent: intent, State: state, Outcome: outcome, ResponseBytes: responseBytes}, nil
}

func (journal *serverJournal) loadAuditCancellation(ctx context.Context, envelopeHash string) (auditCancellationRecord, bool, error) {
	if journal == nil || ctx == nil || !protocolHashPattern.MatchString(envelopeHash) {
		return auditCancellationRecord{}, false, ErrProtocol
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return auditCancellationRecord{}, false, ErrProtocol
	}
	if err := journal.validateIdentity(); err != nil {
		return auditCancellationRecord{}, false, err
	}
	if err := validateServerJournalContent(ctx, journal.db, journal.auditAuthority); err != nil {
		return auditCancellationRecord{}, false, err
	}
	return loadAuditCancellationTx(ctx, journal.db, journal.auditAuthority, "", envelopeHash)
}

func loadAuditCancellationTx(ctx context.Context, queryer auditExecutionQueryer, authority *auditJournalAuthority, requestHash, envelopeHash string) (auditCancellationRecord, bool, error) {
	query := `SELECT intent_bytes, intent_bytes_hash FROM trusted_supervisor_cancellation_intents WHERE request_hash = ?`
	key := requestHash
	if envelopeHash != "" {
		query = `SELECT intent_bytes, intent_bytes_hash FROM trusted_supervisor_cancellation_intents WHERE envelope_hash = ?`
		key = envelopeHash
	}
	var intentBytes []byte
	var intentBytesHash string
	if err := queryer.QueryRowContext(ctx, query, key).Scan(&intentBytes, &intentBytesHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auditCancellationRecord{}, false, nil
		}
		return auditCancellationRecord{}, false, authenticationError("load cancellation intent")
	}
	var intent auditCancellationIntent
	sealed, err := decodeAndSealAuditCancellationIntent(intentBytes, intentBytesHash, &intent)
	if err != nil || !equalAuditCancellationIntent(sealed, intent) {
		return auditCancellationRecord{}, false, authenticationError("cancellation intent fingerprint")
	}
	record := auditCancellationRecord{Intent: intent, State: auditCancellationStateRequested}
	var outcome, auditEventHash, responseHash, completedAt string
	var responseBytes []byte
	err = queryer.QueryRowContext(ctx, `SELECT outcome, audit_event_hash, response_bytes, response_bytes_hash, completed_at
		FROM trusted_supervisor_cancellation_outcomes WHERE intent_hash = ?`, intent.IntentHash).
		Scan(&outcome, &auditEventHash, &responseBytes, &responseHash, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return record, true, nil
	}
	if err != nil || (outcome != auditCancellationOutcomeCompleted && outcome != auditCancellationOutcomeFailed) ||
		!protocolHashPattern.MatchString(auditEventHash) || responseHash != hashJournalBytes(responseBytes) ||
		len(responseBytes) == 0 || len(responseBytes) > int(maxFrameBytes) || !validServerJournalTimestamp(completedAt) {
		return auditCancellationRecord{}, false, authenticationError("cancellation outcome fingerprint")
	}
	auditIntent, events, executionErr := loadAuditExecutionTx(ctx, queryer, authority, intent.EnvelopeHash, "")
	if executionErr != nil || auditIntent.EnvelopeHash != intent.EnvelopeHash || len(events) == 0 {
		return auditCancellationRecord{}, false, authenticationError("cancellation outcome audit history")
	}
	terminal := events[len(events)-1]
	expectedState := auditStateCancelled
	if outcome == auditCancellationOutcomeFailed {
		expectedState = auditStateWaitingForHuman
	}
	if terminal.State != expectedState || terminal.EventHash != auditEventHash || terminal.OccurredAt != completedAt || terminal.Attempt != intent.Attempt {
		return auditCancellationRecord{}, false, authenticationError("cancellation outcome terminal binding")
	}
	record.Outcome = outcome
	record.ResponseBytes = append([]byte(nil), responseBytes...)
	if outcome == auditCancellationOutcomeCompleted {
		record.State = auditCancellationStateCompleted
	} else {
		record.State = auditCancellationStateFailed
	}
	return record, true, nil
}

func decodeAndSealAuditCancellationIntent(encoded []byte, encodedHash string, destination *auditCancellationIntent) (auditCancellationIntent, error) {
	if encodedHash != hashJournalBytes(encoded) || decodeCanonical(encoded, destination) != nil {
		return auditCancellationIntent{}, ErrAuthentication
	}
	return sealAuditCancellationIntent(*destination)
}

func validateAuditCancellationContent(ctx context.Context, queryer auditExecutionQueryer, authority *auditJournalAuthority) error {
	rows, err := queryer.QueryContext(ctx, `SELECT request_hash FROM trusted_supervisor_cancellation_intents ORDER BY request_hash`)
	if err != nil {
		return authenticationError("inspect cancellation journal")
	}
	var requestHashes []string
	for rows.Next() {
		var requestHash string
		if err := rows.Scan(&requestHash); err != nil || !protocolHashPattern.MatchString(requestHash) {
			_ = rows.Close()
			return authenticationError("read cancellation journal")
		}
		requestHashes = append(requestHashes, requestHash)
	}
	if err := rows.Err(); err != nil || rows.Close() != nil {
		return authenticationError("read cancellation journal")
	}
	for _, requestHash := range requestHashes {
		record, found, err := loadAuditCancellationTx(ctx, queryer, authority, requestHash, "")
		if err != nil || !found {
			return authenticationError("cancellation journal binding")
		}
		var intentHash, operationKey, requestNonce, responseNonce, exclusivityNonce, envelopeHash, receiptHash, cancellationHash string
		if err := queryer.QueryRowContext(ctx, `SELECT intent_hash, operation_key, request_nonce_hash, response_nonce_hash,
			exclusivity_nonce_hash, envelope_hash, receipt_hash, cancellation_hash
			FROM trusted_supervisor_cancellation_intents WHERE request_hash = ?`, requestHash).
			Scan(&intentHash, &operationKey, &requestNonce, &responseNonce, &exclusivityNonce, &envelopeHash, &receiptHash, &cancellationHash); err != nil ||
			intentHash != record.Intent.IntentHash || operationKey != record.Intent.OperationKey || requestNonce != record.Intent.RequestNonceHash ||
			responseNonce != record.Intent.ResponseNonceHash || exclusivityNonce != record.Intent.ExclusivityNonceHash ||
			envelopeHash != record.Intent.EnvelopeHash || receiptHash != record.Intent.ReceiptHash || cancellationHash != record.Intent.CancellationHash {
			return authenticationError("cancellation intent columns")
		}
		if record.State != auditCancellationStateRequested {
			var storedRequest, storedResponse []byte
			if err := queryer.QueryRowContext(ctx, `SELECT request_bytes, response_bytes FROM trusted_supervisor_requests WHERE request_hash = ? AND operation = 'cancel'`, requestHash).
				Scan(&storedRequest, &storedResponse); err != nil || !bytes.Equal(storedRequest, record.Intent.RequestBytes) || !bytes.Equal(storedResponse, record.ResponseBytes) {
				return authenticationError("completed cancellation request binding")
			}
		}
	}
	return nil
}
