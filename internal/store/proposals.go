package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	localGUIOperator = "local_gui_operator"

	proposalRevisionSchemaVersion = "ananke.proposal-revision.v1"
	createRequestSchemaVersion    = "ananke.proposal.create-request.v1"
)

var (
	identifierPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,127}$`)

	ErrProposalNotFound      = errors.New("proposal not found")
	ErrRevisionNotFound      = errors.New("proposal revision not found")
	ErrApprovalNotFound      = errors.New("proposal approval not found")
	ErrProposalConflict      = errors.New("proposal_conflict")
	ErrRevisionConflict      = errors.New("revision_conflict")
	ErrApprovalConflict      = errors.New("approval_conflict")
	ErrIdempotencyConflict   = errors.New("idempotency_conflict")
	ErrProposalRecordCorrupt = errors.New("proposal record is not canonical")
)

// ProposalState is the mutable state of a Proposal record.
type ProposalState string

const (
	ProposalStateOpen      ProposalState = "open"
	ProposalStateApproved  ProposalState = "approved"
	ProposalStateWithdrawn ProposalState = "withdrawn"
)

// RevisionLifecycleState is the mutable state paired one-to-one with an
// immutable Revision snapshot.
type RevisionLifecycleState string

const (
	RevisionLifecycleStatePending    RevisionLifecycleState = "pending"
	RevisionLifecycleStateApproved   RevisionLifecycleState = "approved"
	RevisionLifecycleStateRejected   RevisionLifecycleState = "rejected"
	RevisionLifecycleStateSuperseded RevisionLifecycleState = "superseded"
	RevisionLifecycleStateWithdrawn  RevisionLifecycleState = "withdrawn"
)

// ApprovalState is the state of the Approval paired one-to-one with a Revision.
type ApprovalState string

const (
	ApprovalStatePending    ApprovalState = "pending"
	ApprovalStateApproved   ApprovalState = "approved"
	ApprovalStateRejected   ApprovalState = "rejected"
	ApprovalStateSuperseded ApprovalState = "superseded"
	ApprovalStateWithdrawn  ApprovalState = "withdrawn"
)

// Proposal is the immutable target and mutable current-revision pointer.
type Proposal struct {
	ProposalID          string        `json:"proposal_id"`
	ProjectID           string        `json:"project_id"`
	WorkstreamID        string        `json:"workstream_id"`
	CreatedAt           time.Time     `json:"created_at"`
	CreatedBy           string        `json:"created_by"`
	State               ProposalState `json:"state"`
	CurrentRevision     int           `json:"current_revision"`
	CurrentRevisionHash string        `json:"current_revision_hash"`
}

// Revision is the exact immutable revision snapshot. RevisionHash is deliberately
// absent: it is the hash of this canonical object, not an embedded field.
type Revision struct {
	SchemaVersion      string         `json:"schema_version"`
	ProposalID         string         `json:"proposal_id"`
	Revision           int            `json:"revision"`
	ParentRevision     *int           `json:"parent_revision"`
	ParentRevisionHash *string        `json:"parent_revision_hash"`
	CreatedAt          time.Time      `json:"created_at"`
	CreatedBy          string         `json:"created_by"`
	IdempotencyKey     string         `json:"idempotency_key"`
	Task               ProposalTask   `json:"task"`
	AcceptanceCriteria []string       `json:"acceptance_criteria"`
	Policy             ProposalPolicy `json:"policy"`
}

// ProposalTask is the secret-free operator task text allowed by P1a.
type ProposalTask struct {
	Title        string `json:"title"`
	Instructions string `json:"instructions"`
}

// ProposalPolicy is deliberately fixed by P1a. It carries no executable
// budget, adapter parameter, model output, or policy evaluation result.
type ProposalPolicy struct {
	Adapter   ProposalAdapterPolicy `json:"adapter"`
	Authority string                `json:"authority"`
	Budget    ProposalBudgetPolicy  `json:"budget"`
	ModelRole string                `json:"model_role"`
}

type ProposalAdapterPolicy struct {
	Access string `json:"access"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type ProposalBudgetPolicy struct {
	Dimensions []string `json:"dimensions"`
	Status     string   `json:"status"`
}

// RevisionInput is the create/append body portion that becomes a new snapshot.
type RevisionInput struct {
	Task               ProposalTask   `json:"task"`
	AcceptanceCriteria []string       `json:"acceptance_criteria"`
	Policy             ProposalPolicy `json:"policy"`
}

// RevisionLifecycle is the sole mutable record for a Revision snapshot.
type RevisionLifecycle struct {
	ProposalID   string                 `json:"proposal_id"`
	Revision     int                    `json:"revision"`
	RevisionHash string                 `json:"revision_hash"`
	ApprovalID   string                 `json:"approval_id"`
	State        RevisionLifecycleState `json:"state"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Version      int                    `json:"version"`
}

// Approval is the one local-operator approval paired with a Revision.
type Approval struct {
	ApprovalID             string        `json:"approval_id"`
	ProposalID             string        `json:"proposal_id"`
	Revision               int           `json:"revision"`
	RevisionHash           string        `json:"revision_hash"`
	CreatedAt              time.Time     `json:"created_at"`
	CreatedBy              string        `json:"created_by"`
	State                  ApprovalState `json:"state"`
	DecidedAt              *time.Time    `json:"decided_at"`
	DecidedBy              *string       `json:"decided_by"`
	DecisionIdempotencyKey *string       `json:"decision_idempotency_key"`
	Reason                 *string       `json:"reason"`
}

// ProposalActivity is the durable, append-only audit activity for a mutation.
type ProposalActivity struct {
	ProposalID   string    `json:"proposal_id"`
	Sequence     int       `json:"sequence"`
	Operation    string    `json:"operation"`
	Revision     int       `json:"revision"`
	RevisionHash string    `json:"revision_hash"`
	ApprovalID   string    `json:"approval_id"`
	WrittenAt    time.Time `json:"written_at"`
}

const ProposalActivityCreate = "create_proposal"

// ProposalMutation is the exact durable identity returned by a mutation and by
// its idempotent replay. It intentionally does not project mutable state.
type ProposalMutation struct {
	ProposalID   string
	Revision     int
	RevisionHash string
	ApprovalID   string
}

// CreateProposalRequest is the fixed P1a create body plus its caller key. The
// actor and operation scope are fixed to local_gui_operator/create_proposal.
type CreateProposalRequest struct {
	IdempotencyKey string
	ProjectID      string
	WorkstreamID   string
	RevisionInput  RevisionInput
}

// CreateProposal atomically writes the Proposal, its immutable root Revision,
// its pending RevisionLifecycle/Approval pair, one activity record, and the
// durable idempotency response. The idempotency lookup deliberately happens
// before validation or mutable-record checks so exact replays remain valid
// after a restart or later state transition.
func (s *Store) CreateProposal(ctx context.Context, request CreateProposalRequest) (ProposalMutation, error) {
	bodyHash, err := canonicalJSONHash(createRequestBody(request))
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("canonical create body: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProposalMutation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if replay, found, err := lookupProposalIdempotency(ctx, tx, "create_proposal", "proposal_collection", request.IdempotencyKey, bodyHash); err != nil {
		return ProposalMutation{}, err
	} else if found {
		return replay, nil
	}
	if err := validateCreateProposalRequest(request); err != nil {
		return ProposalMutation{}, err
	}

	proposalID, err := newProposalIdentifier("proposal")
	if err != nil {
		return ProposalMutation{}, err
	}
	approvalID, err := newProposalIdentifier("approval")
	if err != nil {
		return ProposalMutation{}, err
	}
	nowText := nowStamp()
	now, err := parseStamp(nowText)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("parse creation timestamp: %w", err)
	}
	revision := Revision{
		SchemaVersion:      proposalRevisionSchemaVersion,
		ProposalID:         proposalID,
		Revision:           1,
		CreatedAt:          now,
		CreatedBy:          localGUIOperator,
		IdempotencyKey:     request.IdempotencyKey,
		Task:               request.RevisionInput.Task,
		AcceptanceCriteria: append([]string(nil), request.RevisionInput.AcceptanceCriteria...),
		Policy:             request.RevisionInput.Policy,
	}
	snapshot, revisionHash, err := canonicalRevisionSnapshot(revision)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("canonical revision snapshot: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposals
		(proposal_id, project_id, workstream_id, created_at, created_by, state, current_revision, current_revision_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		proposalID, request.ProjectID, request.WorkstreamID, nowText, localGUIOperator,
		ProposalStateOpen, 1, revisionHash); err != nil {
		return ProposalMutation{}, fmt.Errorf("insert proposal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revisions
		(proposal_id, revision, revision_hash, snapshot_json)
		VALUES (?, ?, ?, ?)`, proposalID, 1, revisionHash, string(snapshot)); err != nil {
		return ProposalMutation{}, fmt.Errorf("insert proposal revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_approvals
		(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state,
		 decided_at, decided_by, decision_idempotency_key, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL)`,
		approvalID, proposalID, 1, revisionHash, nowText, localGUIOperator, ApprovalStatePending); err != nil {
		return ProposalMutation{}, fmt.Errorf("insert approval: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revision_lifecycles
		(proposal_id, revision, revision_hash, approval_id, state, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		proposalID, 1, revisionHash, approvalID, RevisionLifecycleStatePending, nowText, nowText, 1); err != nil {
		return ProposalMutation{}, fmt.Errorf("insert revision lifecycle: %w", err)
	}
	result := ProposalMutation{ProposalID: proposalID, Revision: 1, RevisionHash: revisionHash, ApprovalID: approvalID}
	if err := insertProposalIdempotency(ctx, tx, "create_proposal", "proposal_collection", request.IdempotencyKey, bodyHash, result); err != nil {
		return ProposalMutation{}, err
	}
	if err := appendProposalActivity(ctx, tx, result, ProposalActivityCreate, nowText); err != nil {
		return ProposalMutation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProposalMutation{}, err
	}
	return result, nil
}

func createRequestBody(request CreateProposalRequest) map[string]any {
	return map[string]any{
		"project_id":     request.ProjectID,
		"revision_input": revisionInputCanonicalValue(request.RevisionInput),
		"schema_version": createRequestSchemaVersion,
		"workstream_id":  request.WorkstreamID,
	}
}

func revisionInputCanonicalValue(input RevisionInput) map[string]any {
	return map[string]any{
		"acceptance_criteria": append([]string(nil), input.AcceptanceCriteria...),
		"policy": map[string]any{
			"adapter": map[string]any{
				"access": input.Policy.Adapter.Access,
				"kind":   input.Policy.Adapter.Kind,
				"status": input.Policy.Adapter.Status,
			},
			"authority": input.Policy.Authority,
			"budget": map[string]any{
				"dimensions": append([]string(nil), input.Policy.Budget.Dimensions...),
				"status":     input.Policy.Budget.Status,
			},
			"model_role": input.Policy.ModelRole,
		},
		"task": map[string]any{
			"instructions": input.Task.Instructions,
			"title":        input.Task.Title,
		},
	}
}

func canonicalRevisionSnapshot(revision Revision) ([]byte, string, error) {
	value := map[string]any{
		"acceptance_criteria":  append([]string(nil), revision.AcceptanceCriteria...),
		"created_at":           revision.CreatedAt.UTC().Format(time.RFC3339Nano),
		"created_by":           revision.CreatedBy,
		"idempotency_key":      revision.IdempotencyKey,
		"parent_revision":      revision.ParentRevision,
		"parent_revision_hash": revision.ParentRevisionHash,
		"policy":               revisionInputCanonicalValue(RevisionInput{Task: revision.Task, AcceptanceCriteria: revision.AcceptanceCriteria, Policy: revision.Policy})["policy"],
		"proposal_id":          revision.ProposalID,
		"revision":             revision.Revision,
		"schema_version":       revision.SchemaVersion,
		"task":                 revisionInputCanonicalValue(RevisionInput{Task: revision.Task, AcceptanceCriteria: revision.AcceptanceCriteria, Policy: revision.Policy})["task"],
	}
	snapshot, err := canonicalJSON(value)
	if err != nil {
		return nil, "", err
	}
	hash, err := canonicalJSONHash(value)
	if err != nil {
		return nil, "", err
	}
	return snapshot, hash, nil
}

func validateCreateProposalRequest(request CreateProposalRequest) error {
	if err := validateIdentifier(request.ProjectID, "project id"); err != nil {
		return err
	}
	if err := validateIdentifier(request.WorkstreamID, "workstream id"); err != nil {
		return err
	}
	if err := validateIdempotencyKey(request.IdempotencyKey); err != nil {
		return err
	}
	return validateRevisionInput(request.RevisionInput)
}

func validateRevisionInput(input RevisionInput) error {
	if err := validateText(input.Task.Title, 160, "task title"); err != nil {
		return err
	}
	if err := validateText(input.Task.Instructions, 8000, "task instructions"); err != nil {
		return err
	}
	if len(input.AcceptanceCriteria) < 1 || len(input.AcceptanceCriteria) > 32 {
		return fmt.Errorf("acceptance criteria count must be 1 through 32")
	}
	for index := range input.AcceptanceCriteria {
		if err := validateText(input.AcceptanceCriteria[index], 1000, fmt.Sprintf("acceptance criterion %d", index)); err != nil {
			return err
		}
	}
	policy := input.Policy
	if policy.Adapter.Access != "read_only" || policy.Adapter.Kind != "omp_audit" || policy.Adapter.Status != "future" ||
		policy.Authority != "deterministic" || policy.ModelRole != "advisory_only" || policy.Budget.Status != "future" ||
		len(policy.Budget.Dimensions) != 2 || policy.Budget.Dimensions[0] != "deadline" || policy.Budget.Dimensions[1] != "attempt_cap" {
		return fmt.Errorf("revision policy must be the fixed P1a policy")
	}
	return nil
}

func validateIdentifier(value, name string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s must be a P1a identifier", name)
	}
	return nil
}

func validateIdempotencyKey(value string) error {
	if !idempotencyKeyPattern.MatchString(value) {
		return fmt.Errorf("idempotency key must be a P1a idempotency key")
	}
	return nil
}

func validateText(value string, maximumBytes int, name string) error {
	if !utf8.ValidString(value) || len(value) == 0 || len(value) > maximumBytes {
		return fmt.Errorf("%s must be valid UTF-8 text from 1 through %d bytes", name, maximumBytes)
	}
	return nil
}

func newProposalIdentifier(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate %s identifier: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(bytes), nil
}

func lookupProposalIdempotency(ctx context.Context, tx *sql.Tx, operation, resource, key, bodyHash string) (ProposalMutation, bool, error) {
	var (
		storedHash string
		result     ProposalMutation
	)
	err := tx.QueryRowContext(ctx, `SELECT request_body_hash, proposal_id, revision, revision_hash, approval_id
		FROM task_proposal_idempotency
		WHERE actor = ? AND operation = ? AND resource = ? AND idempotency_key = ?`,
		localGUIOperator, operation, resource, key).
		Scan(&storedHash, &result.ProposalID, &result.Revision, &result.RevisionHash, &result.ApprovalID)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalMutation{}, false, nil
	}
	if err != nil {
		return ProposalMutation{}, false, fmt.Errorf("lookup proposal idempotency: %w", err)
	}
	if err := validateProposalMutationIdentity(ctx, tx, result); err != nil {
		return ProposalMutation{}, false, err
	}
	if storedHash != bodyHash {
		return ProposalMutation{}, false, ErrIdempotencyConflict
	}
	return result, true, nil
}

func insertProposalIdempotency(ctx context.Context, tx *sql.Tx, operation, resource, key, bodyHash string, result ProposalMutation) error {
	if err := validateProposalMutationIdentity(ctx, tx, result); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_idempotency
		(actor, operation, resource, idempotency_key, request_body_hash, proposal_id, revision, revision_hash, approval_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		localGUIOperator, operation, resource, key, bodyHash,
		result.ProposalID, result.Revision, result.RevisionHash, result.ApprovalID); err != nil {
		return fmt.Errorf("insert proposal idempotency: %w", err)
	}
	return nil
}

type proposalIdentityQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validateRevisionIdentity(ctx context.Context, queryer proposalIdentityQueryer, proposalID string, revision int, revisionHash string) error {
	var exists bool
	err := queryer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_proposal_revisions
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ?)`, proposalID, revision, revisionHash).Scan(&exists)
	if err != nil {
		return fmt.Errorf("validate revision identity: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: revision identity does not match its proposal", ErrProposalRecordCorrupt)
	}
	return nil
}

func validateProposalMutationIdentity(ctx context.Context, queryer proposalIdentityQueryer, result ProposalMutation) error {
	var exists bool
	err := queryer.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1
		FROM task_proposal_revision_lifecycles lifecycle
		JOIN task_proposal_revisions revision
			ON revision.proposal_id = lifecycle.proposal_id
			AND revision.revision = lifecycle.revision
			AND revision.revision_hash = lifecycle.revision_hash
		JOIN task_proposal_approvals approval
			ON approval.proposal_id = lifecycle.proposal_id
			AND approval.revision = lifecycle.revision
			AND approval.revision_hash = lifecycle.revision_hash
			AND approval.approval_id = lifecycle.approval_id
		WHERE lifecycle.proposal_id = ? AND lifecycle.revision = ?
			AND lifecycle.revision_hash = ? AND lifecycle.approval_id = ?
	)`, result.ProposalID, result.Revision, result.RevisionHash, result.ApprovalID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("validate proposal mutation identity: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: mutation identity does not name one revision lifecycle pair", ErrProposalRecordCorrupt)
	}
	return nil
}

func appendProposalActivity(ctx context.Context, tx *sql.Tx, result ProposalMutation, operation, now string) error {
	var maximum sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(sequence) FROM task_proposal_activity WHERE proposal_id = ?`, result.ProposalID).Scan(&maximum); err != nil {
		return fmt.Errorf("select proposal activity sequence: %w", err)
	}
	sequence := 1
	if maximum.Valid {
		sequence = int(maximum.Int64) + 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_activity
		(proposal_id, sequence, operation, revision, revision_hash, approval_id, written_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		result.ProposalID, sequence, operation, result.Revision, result.RevisionHash, result.ApprovalID, now); err != nil {
		return fmt.Errorf("insert proposal activity: %w", err)
	}
	return nil
}

// GetProposal returns the durable Proposal record, including terminal records.
func (s *Store) GetProposal(ctx context.Context, proposalID string) (Proposal, error) {
	var (
		proposal Proposal
		created  string
	)
	err := s.db.QueryRowContext(ctx, `SELECT proposal_id, project_id, workstream_id, created_at, created_by,
		state, current_revision, current_revision_hash
		FROM task_proposals WHERE proposal_id = ?`, proposalID).
		Scan(&proposal.ProposalID, &proposal.ProjectID, &proposal.WorkstreamID, &created, &proposal.CreatedBy,
			&proposal.State, &proposal.CurrentRevision, &proposal.CurrentRevisionHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, ErrProposalNotFound
	}
	if err != nil {
		return Proposal{}, err
	}
	proposal.CreatedAt, err = parseStamp(created)
	if err != nil {
		return Proposal{}, fmt.Errorf("parse proposal created_at: %w", err)
	}
	if err := validateRevisionIdentity(ctx, s.db, proposal.ProposalID, proposal.CurrentRevision, proposal.CurrentRevisionHash); err != nil {
		return Proposal{}, err
	}
	return proposal, nil
}

// GetRevision returns an immutable revision snapshot after proving its stored
// bytes remain canonical and match the durable revision hash.
func (s *Store) GetRevision(ctx context.Context, proposalID string, revisionNumber int) (Revision, error) {
	var snapshot, storedHash string
	err := s.db.QueryRowContext(ctx, `SELECT snapshot_json, revision_hash FROM task_proposal_revisions
		WHERE proposal_id = ? AND revision = ?`, proposalID, revisionNumber).Scan(&snapshot, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrRevisionNotFound
	}
	if err != nil {
		return Revision{}, err
	}
	var revision Revision
	if err := json.Unmarshal([]byte(snapshot), &revision); err != nil {
		return Revision{}, fmt.Errorf("decode revision snapshot: %w", err)
	}
	canonical, computedHash, err := canonicalRevisionSnapshot(revision)
	if err != nil || string(canonical) != snapshot || computedHash != storedHash {
		return Revision{}, fmt.Errorf("%w: proposal=%q revision=%d", ErrProposalRecordCorrupt, proposalID, revisionNumber)
	}
	return revision, nil
}

// GetRevisionLifecycle returns the mutable lifecycle paired with a Revision.
func (s *Store) GetRevisionLifecycle(ctx context.Context, proposalID string, revisionNumber int) (RevisionLifecycle, error) {
	var (
		lifecycle            RevisionLifecycle
		createdAt, updatedAt string
	)
	err := s.db.QueryRowContext(ctx, `SELECT proposal_id, revision, revision_hash, approval_id, state,
		created_at, updated_at, version FROM task_proposal_revision_lifecycles
		WHERE proposal_id = ? AND revision = ?`, proposalID, revisionNumber).
		Scan(&lifecycle.ProposalID, &lifecycle.Revision, &lifecycle.RevisionHash, &lifecycle.ApprovalID,
			&lifecycle.State, &createdAt, &updatedAt, &lifecycle.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return RevisionLifecycle{}, ErrRevisionNotFound
	}
	if err != nil {
		return RevisionLifecycle{}, err
	}
	if err := validateProposalMutationIdentity(ctx, s.db, ProposalMutation{
		ProposalID: lifecycle.ProposalID, Revision: lifecycle.Revision, RevisionHash: lifecycle.RevisionHash, ApprovalID: lifecycle.ApprovalID,
	}); err != nil {
		return RevisionLifecycle{}, err
	}
	var parseErr error
	if lifecycle.CreatedAt, parseErr = parseStamp(createdAt); parseErr != nil {
		return RevisionLifecycle{}, fmt.Errorf("parse revision lifecycle created_at: %w", parseErr)
	}
	if lifecycle.UpdatedAt, parseErr = parseStamp(updatedAt); parseErr != nil {
		return RevisionLifecycle{}, fmt.Errorf("parse revision lifecycle updated_at: %w", parseErr)
	}
	return lifecycle, nil
}

// GetApproval returns the durable Approval record, including terminal records.
func (s *Store) GetApproval(ctx context.Context, approvalID string) (Approval, error) {
	var (
		approval                  Approval
		createdAt, decidedAt      string
		decidedAtValue, decidedBy sql.NullString
		decisionKey, reason       sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT approval_id, proposal_id, revision, revision_hash, created_at,
		created_by, state, decided_at, decided_by, decision_idempotency_key, reason
		FROM task_proposal_approvals WHERE approval_id = ?`, approvalID).
		Scan(&approval.ApprovalID, &approval.ProposalID, &approval.Revision, &approval.RevisionHash, &createdAt,
			&approval.CreatedBy, &approval.State, &decidedAtValue, &decidedBy, &decisionKey, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, ErrApprovalNotFound
	}
	if err != nil {
		return Approval{}, err
	}
	if err := validateProposalMutationIdentity(ctx, s.db, ProposalMutation{
		ProposalID: approval.ProposalID, Revision: approval.Revision, RevisionHash: approval.RevisionHash, ApprovalID: approval.ApprovalID,
	}); err != nil {
		return Approval{}, err
	}
	var parseErr error
	if approval.CreatedAt, parseErr = parseStamp(createdAt); parseErr != nil {
		return Approval{}, fmt.Errorf("parse approval created_at: %w", parseErr)
	}
	if decidedAtValue.Valid {
		decidedAt = decidedAtValue.String
		parsed, err := parseStamp(decidedAt)
		if err != nil {
			return Approval{}, fmt.Errorf("parse approval decided_at: %w", err)
		}
		approval.DecidedAt = &parsed
	}
	if decidedBy.Valid {
		value := decidedBy.String
		approval.DecidedBy = &value
	}
	if decisionKey.Valid {
		value := decisionKey.String
		approval.DecisionIdempotencyKey = &value
	}
	if reason.Valid {
		value := reason.String
		approval.Reason = &value
	}
	return approval, nil
}

// ListProposalActivity returns the proposal's append-only mutation history.
func (s *Store) ListProposalActivity(ctx context.Context, proposalID string) ([]ProposalActivity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT proposal_id, sequence, operation, revision, revision_hash, approval_id, written_at
		FROM task_proposal_activity WHERE proposal_id = ? ORDER BY sequence ASC`, proposalID)
	if err != nil {
		return nil, err
	}
	var activity []ProposalActivity
	for rows.Next() {
		var (
			record  ProposalActivity
			written string
		)
		if err := rows.Scan(&record.ProposalID, &record.Sequence, &record.Operation, &record.Revision,
			&record.RevisionHash, &record.ApprovalID, &written); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if record.WrittenAt, err = parseStamp(written); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("parse proposal activity written_at: %w", err)
		}
		activity = append(activity, record)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, record := range activity {
		if err := validateProposalMutationIdentity(ctx, s.db, ProposalMutation{
			ProposalID: record.ProposalID, Revision: record.Revision, RevisionHash: record.RevisionHash, ApprovalID: record.ApprovalID,
		}); err != nil {
			return nil, err
		}
	}
	return activity, nil
}

const (
	appendRevisionRequestSchemaVersion = "ananke.proposal.append-request.v1"
	ProposalActivityAppend             = "append_revision"
)

// AppendProposalRevisionRequest is the P1a append body plus its caller key.
type AppendProposalRevisionRequest struct {
	IdempotencyKey              string
	ProposalID                  string
	ExpectedCurrentRevision     int
	ExpectedCurrentRevisionHash string
	RevisionInput               RevisionInput
}

// AppendProposalRevision atomically adds the next immutable snapshot and
// pending Approval pair, moves the Proposal current pointer, and supersedes a
// pending predecessor. A rejected predecessor deliberately remains rejected.
func (s *Store) AppendProposalRevision(ctx context.Context, request AppendProposalRevisionRequest) (ProposalMutation, error) {
	bodyHash, err := canonicalJSONHash(appendRevisionRequestBody(request))
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("canonical append body: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProposalMutation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if replay, found, err := lookupProposalIdempotency(ctx, tx, "append_revision", request.ProposalID, request.IdempotencyKey, bodyHash); err != nil {
		return ProposalMutation{}, err
	} else if found {
		return replay, nil
	}
	if err := validateAppendProposalRevisionRequest(request); err != nil {
		return ProposalMutation{}, err
	}

	var (
		proposalState   ProposalState
		currentRevision int
		currentHash     string
	)
	err = tx.QueryRowContext(ctx, `SELECT state, current_revision, current_revision_hash
		FROM task_proposals WHERE proposal_id = ?`, request.ProposalID).
		Scan(&proposalState, &currentRevision, &currentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalMutation{}, ErrProposalNotFound
	}
	if err != nil {
		return ProposalMutation{}, err
	}
	if proposalState != ProposalStateOpen || currentRevision != request.ExpectedCurrentRevision || currentHash != request.ExpectedCurrentRevisionHash {
		return ProposalMutation{}, ErrRevisionConflict
	}

	var (
		formerLifecycleState RevisionLifecycleState
		formerApprovalState  ApprovalState
	)
	err = tx.QueryRowContext(ctx, `SELECT lifecycle.state, approval.state
		FROM task_proposal_revision_lifecycles lifecycle
		JOIN task_proposal_approvals approval
			ON approval.approval_id = lifecycle.approval_id
			AND approval.proposal_id = lifecycle.proposal_id
			AND approval.revision = lifecycle.revision
			AND approval.revision_hash = lifecycle.revision_hash
		WHERE lifecycle.proposal_id = ? AND lifecycle.revision = ? AND lifecycle.revision_hash = ?`, request.ProposalID, currentRevision, currentHash).
		Scan(&formerLifecycleState, &formerApprovalState)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalMutation{}, fmt.Errorf("%w: current lifecycle missing", ErrProposalRecordCorrupt)
	}
	if err != nil {
		return ProposalMutation{}, err
	}
	if formerLifecycleState != RevisionLifecycleState(formerApprovalState) {
		return ProposalMutation{}, fmt.Errorf("%w: current lifecycle and approval diverged", ErrProposalRecordCorrupt)
	}
	if formerLifecycleState != RevisionLifecycleStatePending && formerLifecycleState != RevisionLifecycleStateRejected {
		return ProposalMutation{}, ErrRevisionConflict
	}

	approvalID, err := newProposalIdentifier("approval")
	if err != nil {
		return ProposalMutation{}, err
	}
	nowText := nowStamp()
	now, err := parseStamp(nowText)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("parse append timestamp: %w", err)
	}
	parentRevision := currentRevision
	parentHash := currentHash
	nextRevision := currentRevision + 1
	revision := Revision{
		SchemaVersion:      proposalRevisionSchemaVersion,
		ProposalID:         request.ProposalID,
		Revision:           nextRevision,
		ParentRevision:     &parentRevision,
		ParentRevisionHash: &parentHash,
		CreatedAt:          now,
		CreatedBy:          localGUIOperator,
		IdempotencyKey:     request.IdempotencyKey,
		Task:               request.RevisionInput.Task,
		AcceptanceCriteria: append([]string(nil), request.RevisionInput.AcceptanceCriteria...),
		Policy:             request.RevisionInput.Policy,
	}
	snapshot, revisionHash, err := canonicalRevisionSnapshot(revision)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("canonical appended revision snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revisions
		(proposal_id, revision, revision_hash, snapshot_json) VALUES (?, ?, ?, ?)`,
		request.ProposalID, nextRevision, revisionHash, string(snapshot)); err != nil {
		return ProposalMutation{}, fmt.Errorf("insert appended revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_approvals
		(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state,
		 decided_at, decided_by, decision_idempotency_key, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL)`,
		approvalID, request.ProposalID, nextRevision, revisionHash, nowText, localGUIOperator, ApprovalStatePending); err != nil {
		return ProposalMutation{}, fmt.Errorf("insert appended approval: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revision_lifecycles
		(proposal_id, revision, revision_hash, approval_id, state, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, request.ProposalID, nextRevision, revisionHash,
		approvalID, RevisionLifecycleStatePending, nowText, nowText, 1); err != nil {
		return ProposalMutation{}, fmt.Errorf("insert appended lifecycle: %w", err)
	}
	if formerLifecycleState == RevisionLifecycleStatePending {
		if err := supersedePendingRevisionPair(ctx, tx, request.ProposalID, currentRevision, nowText); err != nil {
			return ProposalMutation{}, err
		}
	}
	updated, err := tx.ExecContext(ctx, `UPDATE task_proposals
		SET current_revision = ?, current_revision_hash = ?
		WHERE proposal_id = ? AND state = ? AND current_revision = ? AND current_revision_hash = ?`,
		nextRevision, revisionHash, request.ProposalID, ProposalStateOpen, currentRevision, currentHash)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("update proposal current revision: %w", err)
	}
	if count, _ := updated.RowsAffected(); count != 1 {
		return ProposalMutation{}, ErrRevisionConflict
	}
	result := ProposalMutation{ProposalID: request.ProposalID, Revision: nextRevision, RevisionHash: revisionHash, ApprovalID: approvalID}
	if err := insertProposalIdempotency(ctx, tx, "append_revision", request.ProposalID, request.IdempotencyKey, bodyHash, result); err != nil {
		return ProposalMutation{}, err
	}
	if err := appendProposalActivity(ctx, tx, result, ProposalActivityAppend, nowText); err != nil {
		return ProposalMutation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProposalMutation{}, err
	}
	return result, nil
}

func appendRevisionRequestBody(request AppendProposalRevisionRequest) map[string]any {
	return map[string]any{
		"expected_current_revision":      request.ExpectedCurrentRevision,
		"expected_current_revision_hash": request.ExpectedCurrentRevisionHash,
		"proposal_id":                    request.ProposalID,
		"revision_input":                 revisionInputCanonicalValue(request.RevisionInput),
		"schema_version":                 appendRevisionRequestSchemaVersion,
	}
}

func validateAppendProposalRevisionRequest(request AppendProposalRevisionRequest) error {
	if err := validateIdentifier(request.ProposalID, "proposal id"); err != nil {
		return err
	}
	if err := validateIdempotencyKey(request.IdempotencyKey); err != nil {
		return err
	}
	if request.ExpectedCurrentRevision < 1 {
		return fmt.Errorf("expected current revision must be positive")
	}
	if !strings.HasPrefix(request.ExpectedCurrentRevisionHash, "sha256:") || len(request.ExpectedCurrentRevisionHash) != len("sha256:")+64 {
		return fmt.Errorf("expected current revision hash must be a SHA-256 hash")
	}
	for _, character := range request.ExpectedCurrentRevisionHash[len("sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("expected current revision hash must be a lowercase SHA-256 hash")
		}
	}
	return validateRevisionInput(request.RevisionInput)
}

func supersedePendingRevisionPair(ctx context.Context, tx *sql.Tx, proposalID string, revision int, now string) error {
	updatedLifecycle, err := tx.ExecContext(ctx, `UPDATE task_proposal_revision_lifecycles
		SET state = ?, updated_at = ?, version = version + 1
		WHERE proposal_id = ? AND revision = ? AND state = ?`,
		RevisionLifecycleStateSuperseded, now, proposalID, revision, RevisionLifecycleStatePending)
	if err != nil {
		return fmt.Errorf("supersede former lifecycle: %w", err)
	}
	if count, _ := updatedLifecycle.RowsAffected(); count != 1 {
		return ErrRevisionConflict
	}
	updatedApproval, err := tx.ExecContext(ctx, `UPDATE task_proposal_approvals
		SET state = ? WHERE proposal_id = ? AND revision = ? AND state = ?`,
		ApprovalStateSuperseded, proposalID, revision, ApprovalStatePending)
	if err != nil {
		return fmt.Errorf("supersede former approval: %w", err)
	}
	if count, _ := updatedApproval.RowsAffected(); count != 1 {
		return ErrRevisionConflict
	}
	return nil
}

const (
	decisionRequestSchemaVersion = "ananke.proposal.decision-request.v1"
	ProposalActivityDecision     = "decide_approval"
)

// DecideProposalApprovalRequest is the P1a approval-decision body plus its
// caller key. Decision is limited to approved or rejected.
type DecideProposalApprovalRequest struct {
	IdempotencyKey string
	ApprovalID     string
	ProposalID     string
	Revision       int
	RevisionHash   string
	Decision       ApprovalState
	Reason         string
}

// DecideProposalApproval atomically applies a terminal decision to a pending
// Approval and its paired lifecycle. Approval additionally approves the
// Proposal; rejection deliberately leaves the Proposal open for a new revision.
func (s *Store) DecideProposalApproval(ctx context.Context, request DecideProposalApprovalRequest) (ProposalMutation, error) {
	bodyHash, err := canonicalJSONHash(decisionRequestBody(request))
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("canonical decision body: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProposalMutation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if replay, found, err := lookupProposalIdempotency(ctx, tx, "decide_approval", request.ApprovalID, request.IdempotencyKey, bodyHash); err != nil {
		return ProposalMutation{}, err
	} else if found {
		return replay, nil
	}
	if err := validateDecideProposalApprovalRequest(request); err != nil {
		return ProposalMutation{}, err
	}

	var (
		approvalProposalID string
		approvalRevision   int
		approvalHash       string
		approvalState      ApprovalState
	)
	err = tx.QueryRowContext(ctx, `SELECT proposal_id, revision, revision_hash, state
		FROM task_proposal_approvals WHERE approval_id = ?`, request.ApprovalID).
		Scan(&approvalProposalID, &approvalRevision, &approvalHash, &approvalState)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalMutation{}, ErrApprovalNotFound
	}
	if err != nil {
		return ProposalMutation{}, err
	}
	if approvalProposalID != request.ProposalID || approvalRevision != request.Revision || approvalHash != request.RevisionHash || approvalState != ApprovalStatePending {
		return ProposalMutation{}, ErrApprovalConflict
	}

	var (
		proposalState   ProposalState
		currentRevision int
		currentHash     string
		lifecycleState  RevisionLifecycleState
	)
	err = tx.QueryRowContext(ctx, `SELECT proposal.state, proposal.current_revision, proposal.current_revision_hash, lifecycle.state
		FROM task_proposals proposal
		JOIN task_proposal_revision_lifecycles lifecycle
			ON lifecycle.proposal_id = proposal.proposal_id
			AND lifecycle.revision = ?
			AND lifecycle.revision_hash = ?
			AND lifecycle.approval_id = ?
		WHERE proposal.proposal_id = ?`, request.Revision, request.RevisionHash, request.ApprovalID, request.ProposalID).
		Scan(&proposalState, &currentRevision, &currentHash, &lifecycleState)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalMutation{}, fmt.Errorf("%w: proposal or lifecycle missing", ErrProposalRecordCorrupt)
	}
	if err != nil {
		return ProposalMutation{}, err
	}
	if proposalState != ProposalStateOpen || currentRevision != request.Revision || currentHash != request.RevisionHash || lifecycleState != RevisionLifecycleStatePending {
		return ProposalMutation{}, ErrApprovalConflict
	}

	nowText := nowStamp()
	updatedLifecycle, err := tx.ExecContext(ctx, `UPDATE task_proposal_revision_lifecycles
		SET state = ?, updated_at = ?, version = version + 1
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ? AND approval_id = ? AND state = ?`,
		RevisionLifecycleState(request.Decision), nowText, request.ProposalID, request.Revision,
		request.RevisionHash, request.ApprovalID, RevisionLifecycleStatePending)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("update approval lifecycle: %w", err)
	}
	if count, _ := updatedLifecycle.RowsAffected(); count != 1 {
		return ProposalMutation{}, ErrApprovalConflict
	}
	updatedApproval, err := tx.ExecContext(ctx, `UPDATE task_proposal_approvals
		SET state = ?, decided_at = ?, decided_by = ?, decision_idempotency_key = ?, reason = ?
		WHERE approval_id = ? AND state = ?`,
		request.Decision, nowText, localGUIOperator, request.IdempotencyKey, request.Reason,
		request.ApprovalID, ApprovalStatePending)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("update approval decision: %w", err)
	}
	if count, _ := updatedApproval.RowsAffected(); count != 1 {
		return ProposalMutation{}, ErrApprovalConflict
	}
	if request.Decision == ApprovalStateApproved {
		updatedProposal, err := tx.ExecContext(ctx, `UPDATE task_proposals SET state = ?
			WHERE proposal_id = ? AND state = ? AND current_revision = ? AND current_revision_hash = ?`,
			ProposalStateApproved, request.ProposalID, ProposalStateOpen, request.Revision, request.RevisionHash)
		if err != nil {
			return ProposalMutation{}, fmt.Errorf("approve proposal: %w", err)
		}
		if count, _ := updatedProposal.RowsAffected(); count != 1 {
			return ProposalMutation{}, ErrApprovalConflict
		}
	}
	result := ProposalMutation{ProposalID: request.ProposalID, Revision: request.Revision, RevisionHash: request.RevisionHash, ApprovalID: request.ApprovalID}
	if err := insertProposalIdempotency(ctx, tx, "decide_approval", request.ApprovalID, request.IdempotencyKey, bodyHash, result); err != nil {
		return ProposalMutation{}, err
	}
	if err := appendProposalActivity(ctx, tx, result, ProposalActivityDecision, nowText); err != nil {
		return ProposalMutation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProposalMutation{}, err
	}
	return result, nil
}

func decisionRequestBody(request DecideProposalApprovalRequest) map[string]any {
	return map[string]any{
		"approval_id":    request.ApprovalID,
		"decision":       string(request.Decision),
		"proposal_id":    request.ProposalID,
		"reason":         request.Reason,
		"revision":       request.Revision,
		"revision_hash":  request.RevisionHash,
		"schema_version": decisionRequestSchemaVersion,
	}
}

func validateDecideProposalApprovalRequest(request DecideProposalApprovalRequest) error {
	if err := validateIdentifier(request.ApprovalID, "approval id"); err != nil {
		return err
	}
	if err := validateIdentifier(request.ProposalID, "proposal id"); err != nil {
		return err
	}
	if err := validateIdempotencyKey(request.IdempotencyKey); err != nil {
		return err
	}
	if request.Revision < 1 {
		return fmt.Errorf("revision must be positive")
	}
	if err := validateRevisionHash(request.RevisionHash, "revision hash"); err != nil {
		return err
	}
	if request.Decision != ApprovalStateApproved && request.Decision != ApprovalStateRejected {
		return fmt.Errorf("decision must be approved or rejected")
	}
	return validateText(request.Reason, 1000, "decision reason")
}

func validateRevisionHash(value, name string) error {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return fmt.Errorf("%s must be a SHA-256 hash", name)
	}
	for _, character := range value[len("sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("%s must be a lowercase SHA-256 hash", name)
		}
	}
	return nil
}

const (
	withdrawRequestSchemaVersion = "ananke.proposal.withdraw-request.v1"
	ProposalActivityWithdraw     = "withdraw_proposal"
)

// WithdrawProposalRequest is the P1a withdrawal body plus its caller key.
type WithdrawProposalRequest struct {
	IdempotencyKey string
	ProposalID     string
}

// WithdrawProposal atomically closes an open Proposal. Its pending current pair
// becomes withdrawn; a rejected current pair remains rejected exactly as P1a
// requires.
func (s *Store) WithdrawProposal(ctx context.Context, request WithdrawProposalRequest) (ProposalMutation, error) {
	bodyHash, err := canonicalJSONHash(withdrawProposalRequestBody(request))
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("canonical withdrawal body: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProposalMutation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if replay, found, err := lookupProposalIdempotency(ctx, tx, "withdraw_proposal", request.ProposalID, request.IdempotencyKey, bodyHash); err != nil {
		return ProposalMutation{}, err
	} else if found {
		return replay, nil
	}
	if err := validateWithdrawProposalRequest(request); err != nil {
		return ProposalMutation{}, err
	}

	var (
		proposalState   ProposalState
		currentRevision int
		currentHash     string
	)
	err = tx.QueryRowContext(ctx, `SELECT state, current_revision, current_revision_hash
		FROM task_proposals WHERE proposal_id = ?`, request.ProposalID).
		Scan(&proposalState, &currentRevision, &currentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalMutation{}, ErrProposalNotFound
	}
	if err != nil {
		return ProposalMutation{}, err
	}
	if proposalState != ProposalStateOpen {
		return ProposalMutation{}, ErrProposalConflict
	}

	var (
		approvalID     string
		lifecycleState RevisionLifecycleState
		approvalState  ApprovalState
	)
	err = tx.QueryRowContext(ctx, `SELECT lifecycle.approval_id, lifecycle.state, approval.state
		FROM task_proposal_revision_lifecycles lifecycle
		JOIN task_proposal_approvals approval
			ON approval.approval_id = lifecycle.approval_id
			AND approval.proposal_id = lifecycle.proposal_id
			AND approval.revision = lifecycle.revision
			AND approval.revision_hash = lifecycle.revision_hash
		WHERE lifecycle.proposal_id = ? AND lifecycle.revision = ? AND lifecycle.revision_hash = ?`,
		request.ProposalID, currentRevision, currentHash).
		Scan(&approvalID, &lifecycleState, &approvalState)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalMutation{}, fmt.Errorf("%w: current approval pair missing", ErrProposalRecordCorrupt)
	}
	if err != nil {
		return ProposalMutation{}, err
	}
	if lifecycleState != RevisionLifecycleState(approvalState) {
		return ProposalMutation{}, fmt.Errorf("%w: current lifecycle and approval diverged", ErrProposalRecordCorrupt)
	}
	if lifecycleState != RevisionLifecycleStatePending && lifecycleState != RevisionLifecycleStateRejected {
		return ProposalMutation{}, ErrProposalConflict
	}

	nowText := nowStamp()
	if lifecycleState == RevisionLifecycleStatePending {
		updatedLifecycle, err := tx.ExecContext(ctx, `UPDATE task_proposal_revision_lifecycles
			SET state = ?, updated_at = ?, version = version + 1
			WHERE proposal_id = ? AND revision = ? AND revision_hash = ? AND approval_id = ? AND state = ?`,
			RevisionLifecycleStateWithdrawn, nowText, request.ProposalID, currentRevision, currentHash, approvalID, RevisionLifecycleStatePending)
		if err != nil {
			return ProposalMutation{}, fmt.Errorf("withdraw lifecycle: %w", err)
		}
		if count, _ := updatedLifecycle.RowsAffected(); count != 1 {
			return ProposalMutation{}, ErrProposalConflict
		}
		updatedApproval, err := tx.ExecContext(ctx, `UPDATE task_proposal_approvals SET state = ?
			WHERE approval_id = ? AND state = ?`, ApprovalStateWithdrawn, approvalID, ApprovalStatePending)
		if err != nil {
			return ProposalMutation{}, fmt.Errorf("withdraw approval: %w", err)
		}
		if count, _ := updatedApproval.RowsAffected(); count != 1 {
			return ProposalMutation{}, ErrProposalConflict
		}
	}
	updatedProposal, err := tx.ExecContext(ctx, `UPDATE task_proposals SET state = ?
		WHERE proposal_id = ? AND state = ? AND current_revision = ? AND current_revision_hash = ?`,
		ProposalStateWithdrawn, request.ProposalID, ProposalStateOpen, currentRevision, currentHash)
	if err != nil {
		return ProposalMutation{}, fmt.Errorf("withdraw proposal: %w", err)
	}
	if count, _ := updatedProposal.RowsAffected(); count != 1 {
		return ProposalMutation{}, ErrProposalConflict
	}
	result := ProposalMutation{ProposalID: request.ProposalID, Revision: currentRevision, RevisionHash: currentHash, ApprovalID: approvalID}
	if err := insertProposalIdempotency(ctx, tx, "withdraw_proposal", request.ProposalID, request.IdempotencyKey, bodyHash, result); err != nil {
		return ProposalMutation{}, err
	}
	if err := appendProposalActivity(ctx, tx, result, ProposalActivityWithdraw, nowText); err != nil {
		return ProposalMutation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProposalMutation{}, err
	}
	return result, nil
}

func withdrawProposalRequestBody(request WithdrawProposalRequest) map[string]any {
	return map[string]any{
		"proposal_id":    request.ProposalID,
		"schema_version": withdrawRequestSchemaVersion,
	}
}

func validateWithdrawProposalRequest(request WithdrawProposalRequest) error {
	if err := validateIdentifier(request.ProposalID, "proposal id"); err != nil {
		return err
	}
	return validateIdempotencyKey(request.IdempotencyKey)
}
