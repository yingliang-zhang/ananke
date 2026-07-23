package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"time"
)

const launchSpecSchemaVersion = "ananke.launch-spec.v1"

var (
	ErrLaunchSpecInvalid             = errors.New("invalid launch spec")
	ErrLaunchSpecHashMismatch        = errors.New("launch spec hash does not match canonical launch spec")
	ErrLaunchSpecNotFound            = errors.New("launch spec not found")
	ErrLaunchSpecConflict            = errors.New("launch spec conflicts with durable authority")
	ErrLaunchApprovalIneligible      = errors.New("launch approval is not eligible")
	ErrLaunchClaimNotFound           = errors.New("active launch claim not found")
	ErrLaunchClaimAlreadyActive      = errors.New("active launch claim already exists")
	ErrLaunchStaleFence              = errors.New("launch claim fence is stale")
	ErrLaunchMaterializationMismatch = errors.New("materialization does not match sealed launch spec")
	ErrLaunchMaterializationNotReady = errors.New("launch materialization is not ready")
	ErrLaunchMaterializationConflict = errors.New("launch materialization conflicts with durable authority")
	ErrLaunchRunIntentNotFound       = errors.New("launch run intent not found")
	ErrLaunchRunIntentConflict       = errors.New("launch run intent conflicts with durable authority")
	ErrLaunchTerminalIntentConflict  = errors.New("launch terminal intent conflicts with durable authority")
	ErrLaunchEvidenceIntentConflict  = errors.New("launch evidence intent conflicts with durable authority")
	ErrLaunchRecordCorrupt           = errors.New("launch admission record is corrupt")

	launchHashPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	launchNoncePattern     = regexp.MustCompile(`^nonce:[0-9a-f]{64}$`)
	launchTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$`)
)

// LaunchRevisionIdentity is the immutable P1 revision tuple bound into a P3
// launch spec. The task prose is deliberately not represented here.
type LaunchRevisionIdentity struct {
	ProposalID   string `json:"proposal_id"`
	Revision     int    `json:"revision"`
	RevisionHash string `json:"revision_hash"`
}

// LaunchModelSpec names the opaque model route. It carries no prompt, output,
// model policy, or permission to call a model.
type LaunchModelSpec struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// LaunchReadOnlyScope is the closed P3 read-only retrieval declaration.
type LaunchReadOnlyScope struct {
	Access           string `json:"access"`
	Retrieval        string `json:"retrieval"`
	ScopeFingerprint string `json:"scope_fingerprint"`
	Writes           string `json:"writes"`
}

// LaunchSealedContract is the opaque materialization identity. It contains no
// materialized bytes or filesystem path.
type LaunchSealedContract struct {
	MaterializationHash string `json:"materialization_hash"`
	Nonce               string `json:"nonce"`
}

// LaunchHostSpec contains only frozen fingerprints and capability labels.
type LaunchHostSpec struct {
	Capabilities                []string `json:"capabilities"`
	ExecutableRouteFingerprint  string   `json:"executable_route_fingerprint"`
	HostSpecFingerprint         string   `json:"host_spec_fingerprint"`
	RequiredFilesFingerprint    string   `json:"required_files_fingerprint"`
	TranscriptSourceFingerprint string   `json:"transcript_source_fingerprint"`
	WorktreeLayoutFingerprint   string   `json:"worktree_layout_fingerprint"`
}

// LaunchTranscriptSpec freezes shape-only transcript recognition.
type LaunchTranscriptSpec struct {
	Dialect            string `json:"dialect"`
	DialectFingerprint string `json:"dialect_fingerprint"`
	Parse              string `json:"parse"`
}

// LaunchVerificationSpec binds a verification fingerprint, not an executable
// command or request to execute one.
type LaunchVerificationSpec struct {
	Kind                           string `json:"kind"`
	VerificationCommandFingerprint string `json:"verification_command_fingerprint"`
}

// LaunchSpec is the closed P3a immutable admission declaration.
type LaunchSpec struct {
	SchemaVersion  string                 `json:"schema_version"`
	Revision       LaunchRevisionIdentity `json:"revision"`
	Model          LaunchModelSpec        `json:"model"`
	Deadline       string                 `json:"deadline"`
	AttemptCap     int                    `json:"attempt_cap"`
	ReadOnlyScope  LaunchReadOnlyScope    `json:"read_only_scope"`
	SealedContract LaunchSealedContract   `json:"sealed_contract"`
	HostSpec       LaunchHostSpec         `json:"host_spec"`
	Transcript     LaunchTranscriptSpec   `json:"transcript"`
	Verification   LaunchVerificationSpec `json:"verification"`
}

// LaunchApprovalEligibility is the immutable P1 approval fact read while a
// LaunchSpec is admitted. It is not a mutable Approval projection.
type LaunchApprovalEligibility struct {
	ApprovalID   string        `json:"approval_id"`
	ProposalID   string        `json:"proposal_id"`
	Revision     int           `json:"revision"`
	RevisionHash string        `json:"revision_hash"`
	ApprovedAt   time.Time     `json:"approved_at"`
	ApprovedBy   string        `json:"approved_by"`
	State        ApprovalState `json:"state"`
}

// LaunchAdmissionRequest stores one immutable spec only after deriving an
// exact approved P1 eligibility fact.
type LaunchAdmissionRequest struct {
	Spec           LaunchSpec
	LaunchSpecHash string
	ApprovalID     string
}

// StoredLaunchSpec is the immutable admission record durable in SQLite.
type StoredLaunchSpec struct {
	Spec           LaunchSpec
	LaunchSpecHash string
	Approval       LaunchApprovalEligibility
	CreatedAt      time.Time
}

// LaunchFence is the complete authority tuple required for every mutable P3b
// projection write. Matching only a claim ID or only a generation is unsafe.
type LaunchFence struct {
	ClaimID         string
	ClaimTokenHash  string
	FenceGeneration int
}

type TaskClaimState string

const TaskClaimStateActive TaskClaimState = "active"

// TaskClaim is an immutable claim generation. Its State is projected from the
// durable active-head row; reclaimed generations remain durable facts.
type TaskClaim struct {
	LaunchFence
	Fence          LaunchFence
	LaunchSpecHash string
	OwnerID        string
	Attempt        int
	State          TaskClaimState
	CreatedAt      time.Time
}

// LaunchClaimRequest supplies an initial or reclaimed immutable claim fact.
type LaunchClaimRequest struct {
	LaunchSpecHash string
	ClaimID        string
	ClaimTokenHash string
	OwnerID        string
	Attempt        int
}

// LaunchClaimReclaimRequest requires authenticating the active fence before a
// new immutable generation replaces the active head atomically.
type LaunchClaimReclaimRequest struct {
	ExpectedFence LaunchFence
	Claim         LaunchClaimRequest
}

type LaunchMaterializationState string

const LaunchMaterializationStateReady LaunchMaterializationState = "ready"

// LaunchMaterialization is only a trusted identity/readiness fact. It neither
// opens nor creates a worktree and carries no filesystem path or bytes.
type LaunchMaterialization struct {
	LaunchFence
	LaunchSpecHash      string
	MaterializationID   string
	MaterializationHash string
	Nonce               string
	State               LaunchMaterializationState
	CreatedAt           time.Time
}

type LaunchMaterializationRequest struct {
	Fence               LaunchFence
	MaterializationID   string
	MaterializationHash string
	Nonce               string
}

type LaunchStateFactKind string

const LaunchStateFactCreated LaunchStateFactKind = "created"

// LaunchStateFact is an append-only model fact. It is not a Run lifecycle
// state and does not imply a process was created or started.
type LaunchStateFact struct {
	Kind      LaunchStateFactKind
	Sequence  int
	TokenHash string
	WrittenAt time.Time
}

// LaunchRunIntent models P3a's initial created fact without touching the
// existing runs journal or constructing a real Run.
type LaunchRunIntent struct {
	LaunchFence
	LaunchSpecHash    string
	MaterializationID string
	RunID             string
	Attempt           int
	StateFact         LaunchStateFact
	CreatedAt         time.Time
}

type LaunchRunIntentRequest struct {
	Fence             LaunchFence
	MaterializationID string
	RunID             string
	Attempt           int
}

// LaunchTerminalIntent is an append-only request fact, not a terminal process
// result. No terminal state is inferred from its presence.
type LaunchTerminalIntent struct {
	LaunchFence
	RunID     string
	IntentID  string
	CreatedAt time.Time
}

type LaunchTerminalIntentRequest struct {
	Fence    LaunchFence
	RunID    string
	IntentID string
}

// LaunchEvidenceIntent records only that an evidence-settlement intent was
// authorized. It contains no evidence result, transcript, or process outcome.
type LaunchEvidenceIntent struct {
	LaunchFence
	RunID     string
	IntentID  string
	State     string
	CreatedAt time.Time
}

type LaunchEvidenceIntentRequest struct {
	Fence    LaunchFence
	RunID    string
	IntentID string
}

type LaunchOutboxState string

const (
	LaunchOutboxPendingMaterialization  LaunchOutboxState = "pending_materialization"
	LaunchOutboxPendingRunAdmission     LaunchOutboxState = "pending_run_admission"
	LaunchOutboxPendingProcessAdmission LaunchOutboxState = "pending_process_admission"
)

// LaunchOutbox is an immutable durable admission-stage obligation. A later
// stage is a new row; earlier facts are never rewritten or deleted.
type LaunchOutbox struct {
	LaunchFence
	LaunchSpecHash string
	OutboxID       string
	Sequence       int
	State          LaunchOutboxState
	CreatedAt      time.Time
}

type LaunchRecoveryAction string

const (
	LaunchRecoveryRetryMaterialization  LaunchRecoveryAction = "retry_materialization"
	LaunchRecoveryRetryRunAdmission     LaunchRecoveryAction = "retry_run_admission"
	LaunchRecoveryRetryProcessAdmission LaunchRecoveryAction = "retry_process_admission"
)

// LaunchRecoveryBoundary returns the exact durable facts for the active
// generation. Nil fields mean absent facts, never inferred states.
type LaunchRecoveryBoundary struct {
	LaunchSpecHash  string
	Claim           TaskClaim
	Outbox          LaunchOutbox
	Materialization *LaunchMaterialization
	RunIntent       *LaunchRunIntent
	TerminalIntent  *LaunchTerminalIntent
	EvidenceIntent  *LaunchEvidenceIntent
	Action          LaunchRecoveryAction
}

// HashLaunchSpec validates and hashes the closed P3a declaration using the
// existing RFC 8785 JCS encoder shared with P1/P2.
func HashLaunchSpec(spec LaunchSpec) (string, error) {
	if err := validateLaunchSpec(spec); err != nil {
		return "", err
	}
	return canonicalJSONHash(launchSpecCanonicalValue(spec))
}

func launchSpecCanonicalValue(spec LaunchSpec) map[string]any {
	return map[string]any{
		"schema_version": spec.SchemaVersion,
		"revision": map[string]any{
			"proposal_id":   spec.Revision.ProposalID,
			"revision":      spec.Revision.Revision,
			"revision_hash": spec.Revision.RevisionHash,
		},
		"model": map[string]any{
			"provider": spec.Model.Provider,
			"model":    spec.Model.Model,
		},
		"deadline":    spec.Deadline,
		"attempt_cap": spec.AttemptCap,
		"read_only_scope": map[string]any{
			"access":            spec.ReadOnlyScope.Access,
			"retrieval":         spec.ReadOnlyScope.Retrieval,
			"scope_fingerprint": spec.ReadOnlyScope.ScopeFingerprint,
			"writes":            spec.ReadOnlyScope.Writes,
		},
		"sealed_contract": map[string]any{
			"materialization_hash": spec.SealedContract.MaterializationHash,
			"nonce":                spec.SealedContract.Nonce,
		},
		"host_spec": launchHostSpecCanonicalValue(spec.HostSpec, true),
		"transcript": map[string]any{
			"dialect":             spec.Transcript.Dialect,
			"dialect_fingerprint": spec.Transcript.DialectFingerprint,
			"parse":               spec.Transcript.Parse,
		},
		"verification": map[string]any{
			"kind":                             spec.Verification.Kind,
			"verification_command_fingerprint": spec.Verification.VerificationCommandFingerprint,
		},
	}
}

func launchHostSpecCanonicalValue(spec LaunchHostSpec, includeFingerprint bool) map[string]any {
	value := map[string]any{
		"capabilities":                  append([]string(nil), spec.Capabilities...),
		"executable_route_fingerprint":  spec.ExecutableRouteFingerprint,
		"required_files_fingerprint":    spec.RequiredFilesFingerprint,
		"transcript_source_fingerprint": spec.TranscriptSourceFingerprint,
		"worktree_layout_fingerprint":   spec.WorktreeLayoutFingerprint,
	}
	if includeFingerprint {
		value["host_spec_fingerprint"] = spec.HostSpecFingerprint
	}
	return value
}

func validateLaunchSpec(spec LaunchSpec) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrLaunchSpecInvalid, fmt.Sprintf(format, args...))
	}
	if spec.SchemaVersion != launchSpecSchemaVersion {
		return invalid("schema_version must be %q", launchSpecSchemaVersion)
	}
	if err := validateIdentifier(spec.Revision.ProposalID, "proposal id"); err != nil {
		return invalid("%v", err)
	}
	if spec.Revision.Revision < 1 {
		return invalid("revision must be positive")
	}
	if !launchHashPattern.MatchString(spec.Revision.RevisionHash) {
		return invalid("revision_hash must be a SHA-256 hash")
	}
	if err := validateIdentifier(spec.Model.Provider, "model provider"); err != nil {
		return invalid("%v", err)
	}
	if err := validateIdentifier(spec.Model.Model, "model"); err != nil {
		return invalid("%v", err)
	}
	if !launchTimestampPattern.MatchString(spec.Deadline) {
		return invalid("deadline must be a semantic UTC RFC 3339 timestamp")
	}
	if _, err := time.Parse(time.RFC3339Nano, spec.Deadline); err != nil {
		return invalid("deadline must be a semantic UTC RFC 3339 timestamp")
	}
	if spec.AttemptCap < 1 || spec.AttemptCap > 100 {
		return invalid("attempt_cap must be 1 through 100")
	}
	if spec.ReadOnlyScope.Access != "read_only" || spec.ReadOnlyScope.Retrieval != "sealed_contract_only" || spec.ReadOnlyScope.Writes != "forbidden" || !launchHashPattern.MatchString(spec.ReadOnlyScope.ScopeFingerprint) {
		return invalid("read_only_scope must be sealed read-only authority")
	}
	if !launchHashPattern.MatchString(spec.SealedContract.MaterializationHash) || !launchNoncePattern.MatchString(spec.SealedContract.Nonce) {
		return invalid("sealed_contract must contain a SHA-256 materialization hash and nonce")
	}
	wantCapabilities := []string{"bounded_cancellation", "read_only_retrieval", "reconnect_recovery", "shape_only_transcript", "verification"}
	if !reflect.DeepEqual(spec.HostSpec.Capabilities, wantCapabilities) {
		return invalid("host_spec capabilities are not the frozen inventory")
	}
	for name, value := range map[string]string{
		"executable_route_fingerprint":  spec.HostSpec.ExecutableRouteFingerprint,
		"required_files_fingerprint":    spec.HostSpec.RequiredFilesFingerprint,
		"transcript_source_fingerprint": spec.HostSpec.TranscriptSourceFingerprint,
		"worktree_layout_fingerprint":   spec.HostSpec.WorktreeLayoutFingerprint,
	} {
		if !launchHashPattern.MatchString(value) {
			return invalid("host_spec %s must be a SHA-256 hash", name)
		}
	}
	hostHash, err := canonicalJSONHash(launchHostSpecCanonicalValue(spec.HostSpec, false))
	if err != nil || !launchHashPattern.MatchString(spec.HostSpec.HostSpecFingerprint) || spec.HostSpec.HostSpecFingerprint != hostHash {
		return invalid("host_spec_fingerprint does not bind the closed HostSpec")
	}
	if spec.Transcript.Dialect != "omp_shape_v1" || spec.Transcript.Parse != "shape_only" || !launchHashPattern.MatchString(spec.Transcript.DialectFingerprint) {
		return invalid("transcript must be frozen shape-only omp_shape_v1")
	}
	if spec.Verification.Kind != "read_only" || !launchHashPattern.MatchString(spec.Verification.VerificationCommandFingerprint) {
		return invalid("verification must be a read-only fingerprint")
	}
	return nil
}

// StoreLaunchSpec atomically derives P1 approval eligibility and records the
// canonical immutable launch spec. It does not acquire a claim or create a Run.
func (s *Store) StoreLaunchSpec(ctx context.Context, request LaunchAdmissionRequest) (StoredLaunchSpec, error) {
	computedHash, err := HashLaunchSpec(request.Spec)
	if err != nil {
		return StoredLaunchSpec{}, err
	}
	if request.LaunchSpecHash != computedHash {
		return StoredLaunchSpec{}, ErrLaunchSpecHashMismatch
	}
	if err := validateIdentifier(request.ApprovalID, "approval id"); err != nil {
		return StoredLaunchSpec{}, fmt.Errorf("%w: %v", ErrLaunchApprovalIneligible, err)
	}
	canonical, err := canonicalJSON(launchSpecCanonicalValue(request.Spec))
	if err != nil {
		return StoredLaunchSpec{}, fmt.Errorf("canonical launch spec: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredLaunchSpec{}, err
	}
	defer func() { _ = tx.Rollback() }()

	existing, found, err := loadLaunchSpec(ctx, tx, request.LaunchSpecHash)
	if err != nil {
		return StoredLaunchSpec{}, err
	}
	if found {
		if existing.Approval.ApprovalID != request.ApprovalID || !reflect.DeepEqual(existing.Spec, request.Spec) {
			return StoredLaunchSpec{}, ErrLaunchSpecConflict
		}
		return existing, nil
	}

	eligibility, err := validateLaunchApprovalEligibility(ctx, tx, request.Spec.Revision, request.ApprovalID)
	if err != nil {
		return StoredLaunchSpec{}, err
	}
	createdText := nowStamp()
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return StoredLaunchSpec{}, fmt.Errorf("parse launch spec timestamp: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_specs
		(launch_spec_hash, proposal_id, revision, revision_hash, approval_id, approved_at, approved_by, approval_state, spec_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		request.LaunchSpecHash, request.Spec.Revision.ProposalID, request.Spec.Revision.Revision, request.Spec.Revision.RevisionHash,
		eligibility.ApprovalID, eligibility.ApprovedAt.UTC().Format(time.RFC3339Nano), eligibility.ApprovedBy, eligibility.State, string(canonical), createdText); err != nil {
		return StoredLaunchSpec{}, fmt.Errorf("insert launch spec: %w", err)
	}
	stored := StoredLaunchSpec{Spec: request.Spec, LaunchSpecHash: request.LaunchSpecHash, Approval: eligibility, CreatedAt: createdAt}
	if err := tx.Commit(); err != nil {
		return StoredLaunchSpec{}, err
	}
	return stored, nil
}

// GetLaunchSpec returns an immutable launch spec after revalidating its stored
// canonical bytes, hash, and eligibility-to-spec identity binding.
func (s *Store) GetLaunchSpec(ctx context.Context, launchSpecHash string) (StoredLaunchSpec, error) {
	stored, found, err := loadLaunchSpec(ctx, s.db, launchSpecHash)
	if err != nil {
		return StoredLaunchSpec{}, err
	}
	if !found {
		return StoredLaunchSpec{}, ErrLaunchSpecNotFound
	}
	return stored, nil
}

type launchQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadLaunchSpec(ctx context.Context, queryer launchQueryer, launchSpecHash string) (StoredLaunchSpec, bool, error) {
	var (
		stored                        StoredLaunchSpec
		approvedAtText, createdAtText string
		storedProposalID, storedHash  string
		storedRevision                int
		specJSON                      string
	)
	err := queryer.QueryRowContext(ctx, `SELECT launch_spec_hash, proposal_id, revision, revision_hash, approval_id,
		approved_at, approved_by, approval_state, spec_json, created_at
		FROM launch_specs WHERE launch_spec_hash = ?`, launchSpecHash).Scan(
		&stored.LaunchSpecHash, &storedProposalID, &storedRevision, &storedHash, &stored.Approval.ApprovalID,
		&approvedAtText, &stored.Approval.ApprovedBy, &stored.Approval.State, &specJSON, &createdAtText,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredLaunchSpec{}, false, nil
	}
	if err != nil {
		return StoredLaunchSpec{}, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(specJSON)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored.Spec); err != nil {
		return StoredLaunchSpec{}, false, fmt.Errorf("%w: decode launch spec: %v", ErrLaunchRecordCorrupt, err)
	}
	canonical, err := canonicalJSON(launchSpecCanonicalValue(stored.Spec))
	if err != nil || string(canonical) != specJSON {
		return StoredLaunchSpec{}, false, fmt.Errorf("%w: launch spec bytes are not canonical", ErrLaunchRecordCorrupt)
	}
	computedHash, err := HashLaunchSpec(stored.Spec)
	if err != nil || computedHash != stored.LaunchSpecHash || stored.Spec.Revision != (LaunchRevisionIdentity{ProposalID: storedProposalID, Revision: storedRevision, RevisionHash: storedHash}) {
		return StoredLaunchSpec{}, false, fmt.Errorf("%w: launch spec identity mismatch", ErrLaunchRecordCorrupt)
	}
	stored.Approval.ProposalID = storedProposalID
	stored.Approval.Revision = storedRevision
	stored.Approval.RevisionHash = storedHash
	if stored.Approval.ApprovedAt, err = parseStamp(approvedAtText); err != nil {
		return StoredLaunchSpec{}, false, fmt.Errorf("%w: invalid approval timestamp", ErrLaunchRecordCorrupt)
	}
	if stored.CreatedAt, err = parseStamp(createdAtText); err != nil {
		return StoredLaunchSpec{}, false, fmt.Errorf("%w: invalid launch spec timestamp", ErrLaunchRecordCorrupt)
	}
	if stored.Approval.State != ApprovalStateApproved || stored.Approval.ApprovedBy != localGUIOperator || stored.Approval.ApprovedAt.IsZero() {
		return StoredLaunchSpec{}, false, fmt.Errorf("%w: stored approval eligibility is invalid", ErrLaunchRecordCorrupt)
	}
	return stored, true, nil
}

func validateLaunchApprovalEligibility(ctx context.Context, tx *sql.Tx, revision LaunchRevisionIdentity, approvalID string) (LaunchApprovalEligibility, error) {
	var snapshot, storedHash string
	err := tx.QueryRowContext(ctx, `SELECT snapshot_json, revision_hash FROM task_proposal_revisions
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ?`, revision.ProposalID, revision.Revision, revision.RevisionHash).Scan(&snapshot, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return LaunchApprovalEligibility{}, ErrLaunchApprovalIneligible
	}
	if err != nil {
		return LaunchApprovalEligibility{}, err
	}
	var storedRevision Revision
	if err := json.Unmarshal([]byte(snapshot), &storedRevision); err != nil {
		return LaunchApprovalEligibility{}, fmt.Errorf("%w: decode P1 revision: %v", ErrLaunchRecordCorrupt, err)
	}
	canonical, computedHash, err := canonicalRevisionSnapshot(storedRevision)
	if err != nil || string(canonical) != snapshot || computedHash != storedHash || storedRevision.ProposalID != revision.ProposalID || storedRevision.Revision != revision.Revision {
		return LaunchApprovalEligibility{}, fmt.Errorf("%w: P1 revision snapshot does not match identity", ErrLaunchRecordCorrupt)
	}

	var (
		eligibility    LaunchApprovalEligibility
		approvedAtText sql.NullString
		proposalState  ProposalState
		lifecycleState RevisionLifecycleState
	)
	err = tx.QueryRowContext(ctx, `SELECT approval.approval_id, approval.proposal_id, approval.revision, approval.revision_hash,
		approval.decided_at, approval.decided_by, approval.state, proposal.state, lifecycle.state
		FROM task_proposals proposal
		JOIN task_proposal_revisions revision_row
			ON revision_row.proposal_id = proposal.proposal_id
			AND revision_row.revision = proposal.current_revision
			AND revision_row.revision_hash = proposal.current_revision_hash
		JOIN task_proposal_revision_lifecycles lifecycle
			ON lifecycle.proposal_id = revision_row.proposal_id
			AND lifecycle.revision = revision_row.revision
			AND lifecycle.revision_hash = revision_row.revision_hash
		JOIN task_proposal_approvals approval
			ON approval.approval_id = lifecycle.approval_id
			AND approval.proposal_id = lifecycle.proposal_id
			AND approval.revision = lifecycle.revision
			AND approval.revision_hash = lifecycle.revision_hash
		WHERE proposal.proposal_id = ? AND revision_row.revision = ? AND revision_row.revision_hash = ? AND approval.approval_id = ?`,
		revision.ProposalID, revision.Revision, revision.RevisionHash, approvalID).Scan(
		&eligibility.ApprovalID, &eligibility.ProposalID, &eligibility.Revision, &eligibility.RevisionHash,
		&approvedAtText, &eligibility.ApprovedBy, &eligibility.State, &proposalState, &lifecycleState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return LaunchApprovalEligibility{}, ErrLaunchApprovalIneligible
	}
	if err != nil {
		return LaunchApprovalEligibility{}, err
	}
	if !approvedAtText.Valid {
		return LaunchApprovalEligibility{}, ErrLaunchApprovalIneligible
	}
	if eligibility.ApprovedAt, err = parseStamp(approvedAtText.String); err != nil {
		return LaunchApprovalEligibility{}, fmt.Errorf("%w: invalid P1 approval timestamp", ErrLaunchRecordCorrupt)
	}
	if eligibility.ProposalID != revision.ProposalID || eligibility.Revision != revision.Revision || eligibility.RevisionHash != revision.RevisionHash || eligibility.ApprovalID != approvalID || eligibility.State != ApprovalStateApproved || eligibility.ApprovedBy != localGUIOperator || proposalState != ProposalStateApproved || lifecycleState != RevisionLifecycleStateApproved {
		return LaunchApprovalEligibility{}, ErrLaunchApprovalIneligible
	}
	return eligibility, nil
}

// AcquireLaunchClaim atomically creates the initial active claim and the first
// durable materialization obligation. It performs no lease, process, or I/O.
func (s *Store) AcquireLaunchClaim(ctx context.Context, request LaunchClaimRequest) (TaskClaim, error) {
	if err := validateLaunchClaimRequest(request); err != nil {
		return TaskClaim{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskClaim{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, found, err := loadLaunchSpec(ctx, tx, request.LaunchSpecHash); err != nil {
		return TaskClaim{}, err
	} else if !found {
		return TaskClaim{}, ErrLaunchSpecNotFound
	}
	if _, found, err := loadActiveLaunchClaim(ctx, tx, request.LaunchSpecHash); err != nil {
		return TaskClaim{}, err
	} else if found {
		return TaskClaim{}, ErrLaunchClaimAlreadyActive
	}
	claim, err := insertLaunchClaim(ctx, tx, request, 1)
	if err != nil {
		return TaskClaim{}, err
	}
	if err := insertLaunchOutbox(ctx, tx, claim, 1, LaunchOutboxPendingMaterialization); err != nil {
		return TaskClaim{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskClaim{}, err
	}
	return claim, nil
}

// ReclaimLaunchClaim atomically authenticates an active complete fence, then
// writes a new immutable claim generation and initial outbox obligation.
func (s *Store) ReclaimLaunchClaim(ctx context.Context, request LaunchClaimReclaimRequest) (TaskClaim, error) {
	if err := validateLaunchClaimRequest(request.Claim); err != nil {
		return TaskClaim{}, err
	}
	if request.Claim.LaunchSpecHash == "" || request.ExpectedFence.ClaimID == "" || request.ExpectedFence.ClaimTokenHash == "" || request.ExpectedFence.FenceGeneration < 1 {
		return TaskClaim{}, fmt.Errorf("%w: invalid reclaim fence", ErrLaunchSpecInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskClaim{}, err
	}
	defer func() { _ = tx.Rollback() }()
	active, err := requireActiveLaunchFence(ctx, tx, request.Claim.LaunchSpecHash, request.ExpectedFence)
	if err != nil {
		return TaskClaim{}, err
	}
	claim, err := insertLaunchClaim(ctx, tx, request.Claim, active.FenceGeneration+1)
	if err != nil {
		return TaskClaim{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE launch_claim_heads SET claim_id = ?, claim_token_hash = ?, fence_generation = ? WHERE launch_spec_hash = ?`,
		claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration, claim.LaunchSpecHash); err != nil {
		return TaskClaim{}, fmt.Errorf("update active launch claim: %w", err)
	}
	if err := insertLaunchOutbox(ctx, tx, claim, 1, LaunchOutboxPendingMaterialization); err != nil {
		return TaskClaim{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskClaim{}, err
	}
	return claim, nil
}

func validateLaunchClaimRequest(request LaunchClaimRequest) error {
	if !launchHashPattern.MatchString(request.LaunchSpecHash) || !launchHashPattern.MatchString(request.ClaimTokenHash) || request.Attempt < 1 {
		return fmt.Errorf("%w: invalid claim hash or attempt", ErrLaunchSpecInvalid)
	}
	if err := validateIdentifier(request.ClaimID, "claim id"); err != nil {
		return fmt.Errorf("%w: %v", ErrLaunchSpecInvalid, err)
	}
	if err := validateIdentifier(request.OwnerID, "claim owner id"); err != nil {
		return fmt.Errorf("%w: %v", ErrLaunchSpecInvalid, err)
	}
	return nil
}

func insertLaunchClaim(ctx context.Context, tx *sql.Tx, request LaunchClaimRequest, generation int) (TaskClaim, error) {
	createdText := nowStamp()
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return TaskClaim{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_claims
		(claim_id, launch_spec_hash, claim_token_hash, fence_generation, owner_id, attempt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, request.ClaimID, request.LaunchSpecHash, request.ClaimTokenHash, generation, request.OwnerID, request.Attempt, createdText); err != nil {
		return TaskClaim{}, fmt.Errorf("insert launch claim: %w", err)
	}
	fence := LaunchFence{ClaimID: request.ClaimID, ClaimTokenHash: request.ClaimTokenHash, FenceGeneration: generation}
	claim := TaskClaim{LaunchFence: fence, Fence: fence, LaunchSpecHash: request.LaunchSpecHash, OwnerID: request.OwnerID, Attempt: request.Attempt, State: TaskClaimStateActive, CreatedAt: createdAt}
	if generation == 1 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO launch_claim_heads
			(launch_spec_hash, claim_id, claim_token_hash, fence_generation) VALUES (?, ?, ?, ?)`, claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration); err != nil {
			return TaskClaim{}, fmt.Errorf("insert active launch claim: %w", err)
		}
	}
	return claim, nil
}

// GetLaunchClaim projects the active immutable claim generation only after
// proving the mutable head agrees with its complete immutable identity.
func (s *Store) GetLaunchClaim(ctx context.Context, launchSpecHash string) (TaskClaim, error) {
	claim, found, err := loadActiveLaunchClaim(ctx, s.db, launchSpecHash)
	if err != nil {
		return TaskClaim{}, err
	}
	if !found {
		return TaskClaim{}, ErrLaunchClaimNotFound
	}
	return claim, nil
}

func loadActiveLaunchClaim(ctx context.Context, queryer launchQueryer, launchSpecHash string) (TaskClaim, bool, error) {
	var (
		claim       TaskClaim
		createdText string
		headToken   string
		headGen     int
	)
	err := queryer.QueryRowContext(ctx, `SELECT claim.claim_id, claim.launch_spec_hash, claim.claim_token_hash, claim.fence_generation,
		claim.owner_id, claim.attempt, claim.created_at, head.claim_token_hash, head.fence_generation
		FROM launch_claim_heads head
		JOIN task_claims claim
			ON claim.launch_spec_hash = head.launch_spec_hash
			AND claim.claim_id = head.claim_id
			AND claim.claim_token_hash = head.claim_token_hash
			AND claim.fence_generation = head.fence_generation
		WHERE head.launch_spec_hash = ?`, launchSpecHash).Scan(
		&claim.ClaimID, &claim.LaunchSpecHash, &claim.ClaimTokenHash, &claim.FenceGeneration,
		&claim.OwnerID, &claim.Attempt, &createdText, &headToken, &headGen,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var headExists bool
		if err := queryer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM launch_claim_heads WHERE launch_spec_hash = ?)`, launchSpecHash).Scan(&headExists); err != nil {
			return TaskClaim{}, false, err
		}
		if headExists {
			return TaskClaim{}, false, fmt.Errorf("%w: active claim head does not name its immutable claim", ErrLaunchRecordCorrupt)
		}
		return TaskClaim{}, false, nil
	}
	if err != nil {
		return TaskClaim{}, false, err
	}
	if claim.ClaimTokenHash != headToken || claim.FenceGeneration != headGen || claim.FenceGeneration < 1 || claim.Attempt < 1 {
		return TaskClaim{}, false, fmt.Errorf("%w: active claim head mismatch", ErrLaunchRecordCorrupt)
	}
	if claim.CreatedAt, err = parseStamp(createdText); err != nil {
		return TaskClaim{}, false, fmt.Errorf("%w: invalid claim timestamp", ErrLaunchRecordCorrupt)
	}
	claim.State = TaskClaimStateActive
	claim.Fence = claim.LaunchFence
	return claim, true, nil
}

func requireActiveLaunchFence(ctx context.Context, queryer launchQueryer, launchSpecHash string, fence LaunchFence) (TaskClaim, error) {
	if launchSpecHash == "" {
		var err error
		launchSpecHash, err = launchSpecHashForClaim(ctx, queryer, fence.ClaimID)
		if err != nil {
			return TaskClaim{}, err
		}
	}
	active, found, err := loadActiveLaunchClaim(ctx, queryer, launchSpecHash)
	if err != nil {
		return TaskClaim{}, err
	}
	if !found {
		return TaskClaim{}, ErrLaunchClaimNotFound
	}
	if active.LaunchFence != fence {
		return TaskClaim{}, ErrLaunchStaleFence
	}
	return active, nil
}

func launchSpecHashForClaim(ctx context.Context, queryer launchQueryer, claimID string) (string, error) {
	var launchSpecHash string
	err := queryer.QueryRowContext(ctx, `SELECT launch_spec_hash FROM task_claims WHERE claim_id = ?`, claimID).Scan(&launchSpecHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrLaunchClaimNotFound
	}
	if err != nil {
		return "", err
	}
	return launchSpecHash, nil
}

// RecordLaunchMaterializationReady records a verified opaque identity only. It
// verifies sealed hash/nonce equality but never opens, creates, or materializes
// a filesystem worktree.
func (s *Store) RecordLaunchMaterializationReady(ctx context.Context, request LaunchMaterializationRequest) (LaunchMaterialization, error) {
	if err := validateLaunchMaterializationRequest(request); err != nil {
		return LaunchMaterialization{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LaunchMaterialization{}, err
	}
	defer func() { _ = tx.Rollback() }()
	active, err := requireActiveLaunchFence(ctx, tx, "", request.Fence)
	if err != nil {
		return LaunchMaterialization{}, err
	}
	storedSpec, found, err := loadLaunchSpec(ctx, tx, active.LaunchSpecHash)
	if err != nil {
		return LaunchMaterialization{}, err
	}
	if !found {
		return LaunchMaterialization{}, ErrLaunchSpecNotFound
	}
	if request.MaterializationHash != storedSpec.Spec.SealedContract.MaterializationHash || request.Nonce != storedSpec.Spec.SealedContract.Nonce {
		return LaunchMaterialization{}, ErrLaunchMaterializationMismatch
	}
	if existing, found, err := loadLaunchMaterialization(ctx, tx, active.LaunchSpecHash, active.FenceGeneration); err != nil {
		return LaunchMaterialization{}, err
	} else if found {
		if err := validatePersistedLaunchMaterialization(storedSpec, existing); err != nil {
			return LaunchMaterialization{}, err
		}
		if existing.LaunchFence == request.Fence && existing.MaterializationID == request.MaterializationID && existing.MaterializationHash == request.MaterializationHash && existing.Nonce == request.Nonce {
			return existing, nil
		}
		return LaunchMaterialization{}, ErrLaunchMaterializationConflict
	}
	createdText := nowStamp()
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return LaunchMaterialization{}, err
	}
	materialization := LaunchMaterialization{LaunchFence: request.Fence, LaunchSpecHash: active.LaunchSpecHash, MaterializationID: request.MaterializationID, MaterializationHash: request.MaterializationHash, Nonce: request.Nonce, State: LaunchMaterializationStateReady, CreatedAt: createdAt}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_materializations
		(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, materialization.MaterializationID, materialization.LaunchSpecHash,
		materialization.ClaimID, materialization.ClaimTokenHash, materialization.FenceGeneration, materialization.MaterializationHash,
		materialization.Nonce, materialization.State, createdText); err != nil {
		return LaunchMaterialization{}, fmt.Errorf("insert launch materialization: %w", err)
	}
	if err := insertLaunchOutbox(ctx, tx, active, 2, LaunchOutboxPendingRunAdmission); err != nil {
		return LaunchMaterialization{}, err
	}
	if err := tx.Commit(); err != nil {
		return LaunchMaterialization{}, err
	}
	return materialization, nil
}

func validateLaunchMaterializationRequest(request LaunchMaterializationRequest) error {
	if request.Fence.ClaimID == "" || !launchHashPattern.MatchString(request.Fence.ClaimTokenHash) || request.Fence.FenceGeneration < 1 {
		return fmt.Errorf("%w: invalid materialization fence", ErrLaunchSpecInvalid)
	}
	if err := validateIdentifier(request.MaterializationID, "materialization id"); err != nil {
		return fmt.Errorf("%w: %v", ErrLaunchSpecInvalid, err)
	}
	if !launchHashPattern.MatchString(request.MaterializationHash) || !launchNoncePattern.MatchString(request.Nonce) {
		return fmt.Errorf("%w: invalid materialization identity", ErrLaunchSpecInvalid)
	}
	return nil
}

func loadLaunchMaterialization(ctx context.Context, queryer launchQueryer, launchSpecHash string, generation int) (LaunchMaterialization, bool, error) {
	var (
		materialization LaunchMaterialization
		createdText     string
	)
	err := queryer.QueryRowContext(ctx, `SELECT materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation,
		materialization_hash, nonce, state, created_at FROM launch_materializations
		WHERE launch_spec_hash = ? AND fence_generation = ?`, launchSpecHash, generation).Scan(
		&materialization.MaterializationID, &materialization.LaunchSpecHash, &materialization.ClaimID, &materialization.ClaimTokenHash,
		&materialization.FenceGeneration, &materialization.MaterializationHash, &materialization.Nonce, &materialization.State, &createdText,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return LaunchMaterialization{}, false, nil
	}
	if err != nil {
		return LaunchMaterialization{}, false, err
	}
	if materialization.State != LaunchMaterializationStateReady || !launchHashPattern.MatchString(materialization.MaterializationHash) || !launchNoncePattern.MatchString(materialization.Nonce) {
		return LaunchMaterialization{}, false, fmt.Errorf("%w: invalid materialization record", ErrLaunchRecordCorrupt)
	}
	if materialization.CreatedAt, err = parseStamp(createdText); err != nil {
		return LaunchMaterialization{}, false, fmt.Errorf("%w: invalid materialization timestamp", ErrLaunchRecordCorrupt)
	}
	return materialization, true, nil
}

func validatePersistedLaunchMaterialization(storedSpec StoredLaunchSpec, materialization LaunchMaterialization) error {
	sealed := storedSpec.Spec.SealedContract
	if materialization.MaterializationHash != sealed.MaterializationHash || materialization.Nonce != sealed.Nonce {
		return fmt.Errorf("%w: materialization does not match sealed launch spec", ErrLaunchRecordCorrupt)
	}
	return nil
}

// CreateLaunchRunIntent writes only a modeled initial created fact. It never
// invokes CreateRun and never creates a process or an existing runs row.
func (s *Store) CreateLaunchRunIntent(ctx context.Context, request LaunchRunIntentRequest) (LaunchRunIntent, error) {
	if err := validateLaunchRunIntentRequest(request); err != nil {
		return LaunchRunIntent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LaunchRunIntent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	active, err := requireActiveLaunchFence(ctx, tx, "", request.Fence)
	if err != nil {
		return LaunchRunIntent{}, err
	}
	storedSpec, found, err := loadLaunchSpec(ctx, tx, active.LaunchSpecHash)
	if err != nil {
		return LaunchRunIntent{}, err
	}
	if !found {
		return LaunchRunIntent{}, ErrLaunchSpecNotFound
	}
	if request.Attempt != active.Attempt {
		return LaunchRunIntent{}, ErrLaunchRunIntentConflict
	}
	materialization, found, err := loadLaunchMaterialization(ctx, tx, active.LaunchSpecHash, active.FenceGeneration)
	if err != nil {
		return LaunchRunIntent{}, err
	}
	if found {
		if err := validatePersistedLaunchMaterialization(storedSpec, materialization); err != nil {
			return LaunchRunIntent{}, err
		}
	}
	if !found || materialization.LaunchFence != active.LaunchFence || materialization.MaterializationID != request.MaterializationID {
		return LaunchRunIntent{}, ErrLaunchMaterializationNotReady
	}
	if existing, found, err := loadLaunchRunIntent(ctx, tx, request.RunID); err != nil {
		return LaunchRunIntent{}, err
	} else if found {
		if existing.LaunchFence == request.Fence && existing.MaterializationID == request.MaterializationID && existing.Attempt == request.Attempt {
			return existing, nil
		}
		return LaunchRunIntent{}, ErrLaunchRunIntentConflict
	}
	if existing, found, err := loadLaunchRunIntentByGeneration(ctx, tx, active.LaunchSpecHash, active.FenceGeneration); err != nil {
		return LaunchRunIntent{}, err
	} else if found {
		if existing.RunID == request.RunID {
			return existing, nil
		}
		return LaunchRunIntent{}, ErrLaunchRunIntentConflict
	}
	createdText := nowStamp()
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return LaunchRunIntent{}, err
	}
	run := LaunchRunIntent{LaunchFence: active.LaunchFence, LaunchSpecHash: active.LaunchSpecHash, MaterializationID: request.MaterializationID, RunID: request.RunID, Attempt: request.Attempt, StateFact: LaunchStateFact{Kind: LaunchStateFactCreated, Sequence: 1, TokenHash: active.ClaimTokenHash, WrittenAt: createdAt}, CreatedAt: createdAt}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_run_intents
		(run_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_id, attempt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, run.RunID, run.LaunchSpecHash, run.ClaimID, run.ClaimTokenHash,
		run.FenceGeneration, run.MaterializationID, run.Attempt, createdText); err != nil {
		return LaunchRunIntent{}, fmt.Errorf("insert launch run intent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_run_state_facts
		(run_id, sequence, kind, token_hash, written_at) VALUES (?, ?, ?, ?, ?)`, run.RunID, 1, run.StateFact.Kind, run.StateFact.TokenHash, createdText); err != nil {
		return LaunchRunIntent{}, fmt.Errorf("insert launch created fact: %w", err)
	}
	if err := insertLaunchOutbox(ctx, tx, active, 3, LaunchOutboxPendingProcessAdmission); err != nil {
		return LaunchRunIntent{}, err
	}
	if err := tx.Commit(); err != nil {
		return LaunchRunIntent{}, err
	}
	return run, nil
}

func validateLaunchRunIntentRequest(request LaunchRunIntentRequest) error {
	if request.Fence.ClaimID == "" || !launchHashPattern.MatchString(request.Fence.ClaimTokenHash) || request.Fence.FenceGeneration < 1 || request.Attempt < 1 {
		return fmt.Errorf("%w: invalid run intent fence or attempt", ErrLaunchSpecInvalid)
	}
	if err := validateIdentifier(request.RunID, "run intent id"); err != nil {
		return fmt.Errorf("%w: %v", ErrLaunchSpecInvalid, err)
	}
	if err := validateIdentifier(request.MaterializationID, "materialization id"); err != nil {
		return fmt.Errorf("%w: %v", ErrLaunchSpecInvalid, err)
	}
	return nil
}

func loadLaunchRunIntent(ctx context.Context, queryer launchQueryer, runID string) (LaunchRunIntent, bool, error) {
	var (
		run                 LaunchRunIntent
		createdText, factAt string
	)
	err := queryer.QueryRowContext(ctx, `SELECT intent.run_id, intent.launch_spec_hash, intent.claim_id, intent.claim_token_hash,
		intent.fence_generation, intent.materialization_id, intent.attempt, intent.created_at,
		fact.kind, fact.sequence, fact.token_hash, fact.written_at
		FROM launch_run_intents intent JOIN launch_run_state_facts fact ON fact.run_id = intent.run_id
		WHERE intent.run_id = ? AND fact.sequence = 1`, runID).Scan(
		&run.RunID, &run.LaunchSpecHash, &run.ClaimID, &run.ClaimTokenHash, &run.FenceGeneration,
		&run.MaterializationID, &run.Attempt, &createdText, &run.StateFact.Kind, &run.StateFact.Sequence, &run.StateFact.TokenHash, &factAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if err := queryer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM launch_run_intents WHERE run_id = ?)`, runID).Scan(&exists); err != nil {
			return LaunchRunIntent{}, false, err
		}
		if exists {
			return LaunchRunIntent{}, false, fmt.Errorf("%w: run intent lacks its created fact", ErrLaunchRecordCorrupt)
		}
		return LaunchRunIntent{}, false, nil
	}
	if err != nil {
		return LaunchRunIntent{}, false, err
	}
	if run.StateFact.Kind != LaunchStateFactCreated || run.StateFact.Sequence != 1 || run.StateFact.TokenHash != run.ClaimTokenHash || run.Attempt < 1 {
		return LaunchRunIntent{}, false, fmt.Errorf("%w: invalid run created fact", ErrLaunchRecordCorrupt)
	}
	if run.CreatedAt, err = parseStamp(createdText); err != nil {
		return LaunchRunIntent{}, false, fmt.Errorf("%w: invalid run intent timestamp", ErrLaunchRecordCorrupt)
	}
	if run.StateFact.WrittenAt, err = parseStamp(factAt); err != nil {
		return LaunchRunIntent{}, false, fmt.Errorf("%w: invalid run fact timestamp", ErrLaunchRecordCorrupt)
	}
	return run, true, nil
}

func loadLaunchRunIntentByGeneration(ctx context.Context, queryer launchQueryer, launchSpecHash string, generation int) (LaunchRunIntent, bool, error) {
	var runID string
	err := queryer.QueryRowContext(ctx, `SELECT run_id FROM launch_run_intents WHERE launch_spec_hash = ? AND fence_generation = ?`, launchSpecHash, generation).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return LaunchRunIntent{}, false, nil
	}
	if err != nil {
		return LaunchRunIntent{}, false, err
	}
	return loadLaunchRunIntent(ctx, queryer, runID)
}

// AppendLaunchTerminalIntent appends only a terminal intent fact. It does not
// write a terminal Run state or infer an adapter/process outcome.
func (s *Store) AppendLaunchTerminalIntent(ctx context.Context, request LaunchTerminalIntentRequest) (LaunchTerminalIntent, error) {
	if err := validateLaunchIntentRequest(request.Fence, request.RunID, request.IntentID); err != nil {
		return LaunchTerminalIntent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LaunchTerminalIntent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	active, err := requireActiveLaunchFence(ctx, tx, "", request.Fence)
	if err != nil {
		return LaunchTerminalIntent{}, err
	}
	if err := requireLaunchRunIntentFence(ctx, tx, request.RunID, active); err != nil {
		return LaunchTerminalIntent{}, err
	}
	if existing, found, err := loadLaunchTerminalIntent(ctx, tx, request.RunID); err != nil {
		return LaunchTerminalIntent{}, err
	} else if found {
		if existing.LaunchFence == request.Fence && existing.IntentID == request.IntentID {
			return existing, nil
		}
		return LaunchTerminalIntent{}, ErrLaunchTerminalIntentConflict
	}
	createdText := nowStamp()
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return LaunchTerminalIntent{}, err
	}
	intent := LaunchTerminalIntent{LaunchFence: active.LaunchFence, RunID: request.RunID, IntentID: request.IntentID, CreatedAt: createdAt}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_terminal_intents
		(intent_id, run_id, claim_id, claim_token_hash, fence_generation, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		intent.IntentID, intent.RunID, intent.ClaimID, intent.ClaimTokenHash, intent.FenceGeneration, createdText); err != nil {
		return LaunchTerminalIntent{}, fmt.Errorf("insert terminal intent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return LaunchTerminalIntent{}, err
	}
	return intent, nil
}

// SettleLaunchEvidenceIntent records only a fenced settlement intent. It has no
// evidence payload and is not proof of a completed process or valid evidence.
func (s *Store) SettleLaunchEvidenceIntent(ctx context.Context, request LaunchEvidenceIntentRequest) (LaunchEvidenceIntent, error) {
	if err := validateLaunchIntentRequest(request.Fence, request.RunID, request.IntentID); err != nil {
		return LaunchEvidenceIntent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LaunchEvidenceIntent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	active, err := requireActiveLaunchFence(ctx, tx, "", request.Fence)
	if err != nil {
		return LaunchEvidenceIntent{}, err
	}
	if err := requireLaunchRunIntentFence(ctx, tx, request.RunID, active); err != nil {
		return LaunchEvidenceIntent{}, err
	}
	if existing, found, err := loadLaunchEvidenceIntent(ctx, tx, request.RunID); err != nil {
		return LaunchEvidenceIntent{}, err
	} else if found {
		if existing.LaunchFence == request.Fence && existing.IntentID == request.IntentID {
			return existing, nil
		}
		return LaunchEvidenceIntent{}, ErrLaunchEvidenceIntentConflict
	}
	createdText := nowStamp()
	createdAt, err := parseStamp(createdText)
	if err != nil {
		return LaunchEvidenceIntent{}, err
	}
	intent := LaunchEvidenceIntent{LaunchFence: active.LaunchFence, RunID: request.RunID, IntentID: request.IntentID, State: "settled", CreatedAt: createdAt}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_evidence_intents
		(intent_id, run_id, claim_id, claim_token_hash, fence_generation, state, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		intent.IntentID, intent.RunID, intent.ClaimID, intent.ClaimTokenHash, intent.FenceGeneration, intent.State, createdText); err != nil {
		return LaunchEvidenceIntent{}, fmt.Errorf("insert evidence intent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return LaunchEvidenceIntent{}, err
	}
	return intent, nil
}

func validateLaunchIntentRequest(fence LaunchFence, runID, intentID string) error {
	if fence.ClaimID == "" || !launchHashPattern.MatchString(fence.ClaimTokenHash) || fence.FenceGeneration < 1 {
		return fmt.Errorf("%w: invalid intent fence", ErrLaunchSpecInvalid)
	}
	if err := validateIdentifier(runID, "run intent id"); err != nil {
		return fmt.Errorf("%w: %v", ErrLaunchSpecInvalid, err)
	}
	if err := validateIdentifier(intentID, "intent id"); err != nil {
		return fmt.Errorf("%w: %v", ErrLaunchSpecInvalid, err)
	}
	return nil
}

func requireLaunchRunIntentFence(ctx context.Context, queryer launchQueryer, runID string, active TaskClaim) error {
	run, found, err := loadLaunchRunIntent(ctx, queryer, runID)
	if err != nil {
		return err
	}
	if !found {
		return ErrLaunchRunIntentNotFound
	}
	if run.LaunchFence != active.LaunchFence || run.LaunchSpecHash != active.LaunchSpecHash {
		return ErrLaunchStaleFence
	}
	return nil
}

func loadLaunchTerminalIntent(ctx context.Context, queryer launchQueryer, runID string) (LaunchTerminalIntent, bool, error) {
	var intent LaunchTerminalIntent
	var createdText string
	err := queryer.QueryRowContext(ctx, `SELECT intent_id, run_id, claim_id, claim_token_hash, fence_generation, created_at
		FROM launch_terminal_intents WHERE run_id = ?`, runID).Scan(&intent.IntentID, &intent.RunID, &intent.ClaimID, &intent.ClaimTokenHash, &intent.FenceGeneration, &createdText)
	if errors.Is(err, sql.ErrNoRows) {
		return LaunchTerminalIntent{}, false, nil
	}
	if err != nil {
		return LaunchTerminalIntent{}, false, err
	}
	if intent.CreatedAt, err = parseStamp(createdText); err != nil {
		return LaunchTerminalIntent{}, false, fmt.Errorf("%w: invalid terminal intent timestamp", ErrLaunchRecordCorrupt)
	}
	return intent, true, nil
}

func loadLaunchEvidenceIntent(ctx context.Context, queryer launchQueryer, runID string) (LaunchEvidenceIntent, bool, error) {
	var intent LaunchEvidenceIntent
	var createdText string
	err := queryer.QueryRowContext(ctx, `SELECT intent_id, run_id, claim_id, claim_token_hash, fence_generation, state, created_at
		FROM launch_evidence_intents WHERE run_id = ?`, runID).Scan(&intent.IntentID, &intent.RunID, &intent.ClaimID, &intent.ClaimTokenHash, &intent.FenceGeneration, &intent.State, &createdText)
	if errors.Is(err, sql.ErrNoRows) {
		return LaunchEvidenceIntent{}, false, nil
	}
	if err != nil {
		return LaunchEvidenceIntent{}, false, err
	}
	if intent.State != "settled" {
		return LaunchEvidenceIntent{}, false, fmt.Errorf("%w: invalid evidence intent state", ErrLaunchRecordCorrupt)
	}
	if intent.CreatedAt, err = parseStamp(createdText); err != nil {
		return LaunchEvidenceIntent{}, false, fmt.Errorf("%w: invalid evidence intent timestamp", ErrLaunchRecordCorrupt)
	}
	return intent, true, nil
}

func insertLaunchOutbox(ctx context.Context, tx *sql.Tx, claim TaskClaim, sequence int, state LaunchOutboxState) error {
	outboxID, err := newProposalIdentifier("launch_outbox")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO launch_admission_outbox
		(outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, outboxID, claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration, sequence, state, nowStamp()); err != nil {
		return fmt.Errorf("insert launch outbox: %w", err)
	}
	return nil
}

func loadLatestLaunchOutbox(ctx context.Context, queryer launchQueryer, launchSpecHash string, generation int) (LaunchOutbox, error) {
	var outbox LaunchOutbox
	var createdText string
	err := queryer.QueryRowContext(ctx, `SELECT outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at
		FROM launch_admission_outbox WHERE launch_spec_hash = ? AND fence_generation = ?
		ORDER BY sequence DESC LIMIT 1`, launchSpecHash, generation).Scan(&outbox.OutboxID, &outbox.LaunchSpecHash, &outbox.ClaimID, &outbox.ClaimTokenHash, &outbox.FenceGeneration, &outbox.Sequence, &outbox.State, &createdText)
	if errors.Is(err, sql.ErrNoRows) {
		return LaunchOutbox{}, fmt.Errorf("%w: active claim lacks a launch outbox", ErrLaunchRecordCorrupt)
	}
	if err != nil {
		return LaunchOutbox{}, err
	}
	if !validLaunchOutboxStage(outbox.Sequence, outbox.State) {
		return LaunchOutbox{}, fmt.Errorf("%w: invalid launch outbox stage", ErrLaunchRecordCorrupt)
	}
	if outbox.CreatedAt, err = parseStamp(createdText); err != nil {
		return LaunchOutbox{}, fmt.Errorf("%w: invalid launch outbox timestamp", ErrLaunchRecordCorrupt)
	}
	return outbox, nil
}

func validLaunchOutboxStage(sequence int, state LaunchOutboxState) bool {
	return (sequence == 1 && state == LaunchOutboxPendingMaterialization) ||
		(sequence == 2 && state == LaunchOutboxPendingRunAdmission) ||
		(sequence == 3 && state == LaunchOutboxPendingProcessAdmission)
}

// GetLaunchRecoveryBoundary returns the single safe next modeled obligation for
// an active claim. It returns only durable identities and never guesses a
// terminal fact, evidence result, process, or filesystem/materialization state.
func (s *Store) GetLaunchRecoveryBoundary(ctx context.Context, launchSpecHash string) (LaunchRecoveryBoundary, error) {
	claim, err := s.GetLaunchClaim(ctx, launchSpecHash)
	if err != nil {
		return LaunchRecoveryBoundary{}, err
	}
	storedSpec, err := s.GetLaunchSpec(ctx, launchSpecHash)
	if err != nil {
		return LaunchRecoveryBoundary{}, err
	}
	outbox, err := loadLatestLaunchOutbox(ctx, s.db, launchSpecHash, claim.FenceGeneration)
	if err != nil {
		return LaunchRecoveryBoundary{}, err
	}
	if outbox.LaunchFence != claim.LaunchFence {
		return LaunchRecoveryBoundary{}, fmt.Errorf("%w: outbox does not match active claim", ErrLaunchRecordCorrupt)
	}
	boundary := LaunchRecoveryBoundary{LaunchSpecHash: launchSpecHash, Claim: claim, Outbox: outbox}
	if materialization, found, err := loadLaunchMaterialization(ctx, s.db, launchSpecHash, claim.FenceGeneration); err != nil {
		return LaunchRecoveryBoundary{}, err
	} else if found {
		if err := validatePersistedLaunchMaterialization(storedSpec, materialization); err != nil {
			return LaunchRecoveryBoundary{}, err
		}
		if materialization.LaunchFence != claim.LaunchFence {
			return LaunchRecoveryBoundary{}, fmt.Errorf("%w: materialization does not match active claim", ErrLaunchRecordCorrupt)
		}
		boundary.Materialization = &materialization
	}
	if run, found, err := loadLaunchRunIntentByGeneration(ctx, s.db, launchSpecHash, claim.FenceGeneration); err != nil {
		return LaunchRecoveryBoundary{}, err
	} else if found {
		if run.LaunchFence != claim.LaunchFence {
			return LaunchRecoveryBoundary{}, fmt.Errorf("%w: run intent does not match active claim", ErrLaunchRecordCorrupt)
		}
		if boundary.Materialization == nil || run.MaterializationID != boundary.Materialization.MaterializationID {
			return LaunchRecoveryBoundary{}, fmt.Errorf("%w: run intent does not name the recovered materialization", ErrLaunchRecordCorrupt)
		}
		boundary.RunIntent = &run
		if terminal, found, err := loadLaunchTerminalIntent(ctx, s.db, run.RunID); err != nil {
			return LaunchRecoveryBoundary{}, err
		} else if found {
			if terminal.LaunchFence != claim.LaunchFence {
				return LaunchRecoveryBoundary{}, fmt.Errorf("%w: terminal intent does not match active claim", ErrLaunchRecordCorrupt)
			}
			boundary.TerminalIntent = &terminal
		}
		if evidence, found, err := loadLaunchEvidenceIntent(ctx, s.db, run.RunID); err != nil {
			return LaunchRecoveryBoundary{}, err
		} else if found {
			if evidence.LaunchFence != claim.LaunchFence {
				return LaunchRecoveryBoundary{}, fmt.Errorf("%w: evidence intent does not match active claim", ErrLaunchRecordCorrupt)
			}
			boundary.EvidenceIntent = &evidence
		}
	}
	switch outbox.State {
	case LaunchOutboxPendingMaterialization:
		if boundary.Materialization != nil || boundary.RunIntent != nil {
			return LaunchRecoveryBoundary{}, fmt.Errorf("%w: materialization boundary contains later facts", ErrLaunchRecordCorrupt)
		}
		boundary.Action = LaunchRecoveryRetryMaterialization
	case LaunchOutboxPendingRunAdmission:
		if boundary.Materialization == nil || boundary.RunIntent != nil {
			return LaunchRecoveryBoundary{}, fmt.Errorf("%w: run-admission boundary facts mismatch", ErrLaunchRecordCorrupt)
		}
		boundary.Action = LaunchRecoveryRetryRunAdmission
	case LaunchOutboxPendingProcessAdmission:
		if boundary.Materialization == nil || boundary.RunIntent == nil {
			return LaunchRecoveryBoundary{}, fmt.Errorf("%w: process-admission boundary facts mismatch", ErrLaunchRecordCorrupt)
		}
		boundary.Action = LaunchRecoveryRetryProcessAdmission
	default:
		return LaunchRecoveryBoundary{}, fmt.Errorf("%w: unknown outbox state", ErrLaunchRecordCorrupt)
	}
	return boundary, nil
}

// ListLaunchRecoveryBoundaries returns every active durable launch boundary in
// stable claim creation order. It does not retry, materialize, or launch.
func (s *Store) ListLaunchRecoveryBoundaries(ctx context.Context) ([]LaunchRecoveryBoundary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT head.launch_spec_hash FROM launch_claim_heads head
		LEFT JOIN task_claims claim ON claim.launch_spec_hash = head.launch_spec_hash
			AND claim.claim_id = head.claim_id AND claim.claim_token_hash = head.claim_token_hash
			AND claim.fence_generation = head.fence_generation
		ORDER BY claim.created_at ASC, head.launch_spec_hash ASC`)
	if err != nil {
		return nil, err
	}
	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			_ = rows.Close()
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	boundaries := make([]LaunchRecoveryBoundary, 0, len(hashes))
	for _, hash := range hashes {
		boundary, err := s.GetLaunchRecoveryBoundary(ctx, hash)
		if err != nil {
			return nil, err
		}
		boundaries = append(boundaries, boundary)
	}
	return boundaries, nil
}
