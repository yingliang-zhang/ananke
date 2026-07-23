package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

const (
	// GrillInputSchemaVersion is the sole closed declaration input accepted by
	// the P2a rule table.
	GrillInputSchemaVersion = "ananke.grill.input.v1"
	// GrillRuleVersion identifies the frozen P2a deterministic rule table.
	GrillRuleVersion = "ananke.grill.rules.v1"

	GrillQuestionSchemaVersion = "ananke.grill.question.v1"
	GrillDefaultSchemaVersion  = "ananke.grill.default.v1"
	GrillAnswerSchemaVersion   = "ananke.grill.answer.v1"
	GrillOverrideSchemaVersion = "ananke.grill.override.v1"

	deterministicGrillWriter = "deterministic_grill"
)

var (
	ErrGrillInvalidInput         = errors.New("invalid Grill input")
	ErrGrillRevisionMismatch     = errors.New("Grill revision identity does not exist")
	ErrGrillInputHashMismatch    = errors.New("Grill input hash does not match canonical input")
	ErrGrillRuleVersion          = errors.New("unsupported Grill rule version")
	ErrGrillQuestionNotFound     = errors.New("Grill question not found")
	ErrGrillOverrideNotPermitted = errors.New("Grill override is not permitted")

	grillHashPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	grillTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$`)
)

// GrillRevisionIdentity binds every Grill row to exactly one immutable P1a
// Proposal revision. RevisionHash is the P1 hash verbatim, never a Grill hash.
type GrillRevisionIdentity struct {
	ProposalID   string `json:"proposal_id"`
	Revision     int    `json:"revision"`
	RevisionHash string `json:"revision_hash"`
}

// GrillInput is the closed P2a review declaration. It intentionally has no
// Revision prose, approval, command, model, worker, claim, or execution field.
type GrillInput struct {
	SchemaVersion string            `json:"schema_version"`
	ProposalID    string            `json:"proposal_id"`
	Revision      int               `json:"revision"`
	RevisionHash  string            `json:"revision_hash"`
	Declarations  GrillDeclarations `json:"declarations"`
}

// GrillDeclarations are the only inputs to the frozen deterministic evaluator.
type GrillDeclarations struct {
	ObservableOutcome   string        `json:"observable_outcome"`
	ScopeCompatibility  string        `json:"scope_compatibility"`
	AcceptanceEvidence  string        `json:"acceptance_evidence"`
	DestructiveExternal string        `json:"destructive_external"`
	LocalAuthorization  string        `json:"local_authorization"`
	AdapterMode         string        `json:"adapter_mode"`
	WorktreeIsolation   string        `json:"worktree_isolation"`
	Autonomy            GrillAutonomy `json:"autonomy"`
}

// GrillAutonomy carries declarations only. The evaluator never reads a clock.
type GrillAutonomy struct {
	Deadline   *string `json:"deadline"`
	AttemptCap *int    `json:"attempt_cap"`
}

// GrillEvaluationRequest binds a closed input to its independently supplied
// JCS SHA-256 hash and the frozen rule version.
type GrillEvaluationRequest struct {
	Input       GrillInput `json:"input"`
	InputHash   string     `json:"input_hash"`
	RuleVersion string     `json:"rule_version"`
}

// GrillStatus is review-only. Clear never creates approval or execution state.
type GrillStatus string

const (
	GrillStatusClear        GrillStatus = "clear"
	GrillStatusBlocked      GrillStatus = "blocked"
	GrillStatusNeedsRewrite GrillStatus = "needs_rewrite"
)

// GrillEvaluation is the bounded result of evaluating one exact input/history.
type GrillEvaluation struct {
	ProposalID          string      `json:"proposal_id"`
	Revision            int         `json:"revision"`
	RevisionHash        string      `json:"revision_hash"`
	RuleVersion         string      `json:"rule_version"`
	InputHash           string      `json:"input_hash"`
	NewQuestionIDs      []string    `json:"new_question_ids"`
	ShownQuestionIDs    []string    `json:"shown_question_ids"`
	DeferredRuleClasses []string    `json:"deferred_rule_classes"`
	Status              GrillStatus `json:"status"`
	NewRecords          int         `json:"new_records"`
}

// GrillRecord is one immutable append-only review row. Fields not applicable to
// a record version are zero values when read back from SQLite.
type GrillRecord struct {
	GrillRevisionIdentity
	RuleVersion      string    `json:"rule_version"`
	RecordSequence   int       `json:"record_sequence"`
	SchemaVersion    string    `json:"schema_version"`
	QuestionID       string    `json:"question_id,omitempty"`
	QuestionSequence int       `json:"question_sequence,omitempty"`
	RuleClass        string    `json:"rule_class,omitempty"`
	Risk             string    `json:"risk,omitempty"`
	Blocking         bool      `json:"blocking,omitempty"`
	Waivable         bool      `json:"waivable,omitempty"`
	Default          string    `json:"default,omitempty"`
	RemedialStep     string    `json:"remedial_step,omitempty"`
	Answer           string    `json:"answer,omitempty"`
	Override         string    `json:"override,omitempty"`
	WrittenAt        time.Time `json:"written_at"`
	WrittenBy        string    `json:"written_by"`
}

type grillRule struct {
	Priority     int
	Class        string
	Risk         string
	Blocking     bool
	Waivable     bool
	Default      string
	RemedialStep string
}

var grillRules = []grillRule{
	{Priority: 10, Class: "observable_outcome", Risk: "high", Blocking: true, Waivable: false, Default: "needs_rewrite", RemedialStep: "declare_observable_outcome"},
	{Priority: 20, Class: "scope_compatibility", Risk: "medium", Blocking: true, Waivable: true, Default: "needs_rewrite", RemedialStep: "declare_scope_compatibility"},
	{Priority: 30, Class: "acceptance_evidence", Risk: "high", Blocking: true, Waivable: false, Default: "needs_rewrite", RemedialStep: "declare_acceptance_evidence"},
	{Priority: 40, Class: "destructive_external_authorization", Risk: "critical", Blocking: true, Waivable: false, Default: "deny", RemedialStep: "record_local_authorization"},
	{Priority: 50, Class: "adapter_worktree_isolation", Risk: "high", Blocking: true, Waivable: false, Default: "needs_rewrite", RemedialStep: "require_isolated_worktree"},
	{Priority: 60, Class: "autonomy_budget", Risk: "high", Blocking: true, Waivable: false, Default: "needs_rewrite", RemedialStep: "set_deadline_attempt_cap"},
}

// HashGrillInput returns the P2a JCS SHA-256 input hash after validating the
// closed declaration shape and semantic bounds.
func HashGrillInput(input GrillInput) (string, error) {
	if err := validateGrillInput(input); err != nil {
		return "", err
	}
	return canonicalJSONHash(grillInputCanonicalValue(input))
}

func grillInputCanonicalValue(input GrillInput) map[string]any {
	return map[string]any{
		"schema_version": input.SchemaVersion,
		"proposal_id":    input.ProposalID,
		"revision":       input.Revision,
		"revision_hash":  input.RevisionHash,
		"declarations": map[string]any{
			"observable_outcome":   input.Declarations.ObservableOutcome,
			"scope_compatibility":  input.Declarations.ScopeCompatibility,
			"acceptance_evidence":  input.Declarations.AcceptanceEvidence,
			"destructive_external": input.Declarations.DestructiveExternal,
			"local_authorization":  input.Declarations.LocalAuthorization,
			"adapter_mode":         input.Declarations.AdapterMode,
			"worktree_isolation":   input.Declarations.WorktreeIsolation,
			"autonomy": map[string]any{
				"deadline":    input.Declarations.Autonomy.Deadline,
				"attempt_cap": input.Declarations.Autonomy.AttemptCap,
			},
		},
	}
}

func validateGrillInput(input GrillInput) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrGrillInvalidInput, fmt.Sprintf(format, args...))
	}
	if input.SchemaVersion != GrillInputSchemaVersion {
		return invalid("schema_version must be %q", GrillInputSchemaVersion)
	}
	if err := validateIdentifier(input.ProposalID, "proposal id"); err != nil {
		return invalid("%v", err)
	}
	if input.Revision <= 0 {
		return invalid("revision must be positive")
	}
	if !grillHashPattern.MatchString(input.RevisionHash) {
		return invalid("revision_hash must be a SHA-256 hash")
	}
	declaration := input.Declarations
	if !grillOneOf(declaration.ObservableOutcome, "absent", "declared") {
		return invalid("observable_outcome must be absent or declared")
	}
	if !grillOneOf(declaration.ScopeCompatibility, "absent", "declared") {
		return invalid("scope_compatibility must be absent or declared")
	}
	if !grillOneOf(declaration.AcceptanceEvidence, "absent", "declared") {
		return invalid("acceptance_evidence must be absent or declared")
	}
	if !grillOneOf(declaration.DestructiveExternal, "none", "declared") {
		return invalid("destructive_external must be none or declared")
	}
	if !grillOneOf(declaration.LocalAuthorization, "not_required", "recorded", "unrecorded") {
		return invalid("local_authorization must be not_required, recorded, or unrecorded")
	}
	if !grillOneOf(declaration.AdapterMode, "none", "read_only") {
		return invalid("adapter_mode must be none or read_only")
	}
	if !grillOneOf(declaration.WorktreeIsolation, "not_applicable", "isolated", "not_isolated") {
		return invalid("worktree_isolation must be not_applicable, isolated, or not_isolated")
	}
	if declaration.AdapterMode == "none" && declaration.WorktreeIsolation != "not_applicable" {
		return invalid("worktree_isolation must be not_applicable without an adapter")
	}
	if declaration.DestructiveExternal == "none" && declaration.LocalAuthorization != "not_required" {
		return invalid("local_authorization must be not_required without destructive or external work")
	}
	if deadline := declaration.Autonomy.Deadline; deadline != nil {
		if !grillTimestampPattern.MatchString(*deadline) {
			return invalid("deadline must be a semantic UTC RFC 3339 timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, *deadline); err != nil {
			return invalid("deadline must be a semantic UTC RFC 3339 timestamp")
		}
	}
	if attemptCap := declaration.Autonomy.AttemptCap; attemptCap != nil && (*attemptCap < 1 || *attemptCap > 100) {
		return invalid("attempt_cap must be 1 through 100")
	}
	return nil
}

func grillOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func grillQuestionID(ruleClass string) string { return "grill_question_" + ruleClass }

func grillRuleByClass(ruleClass string) (grillRule, bool) {
	for _, rule := range grillRules {
		if rule.Class == ruleClass {
			return rule, true
		}
	}
	return grillRule{}, false
}

func grillRuleClass(value string) (string, bool) {
	for _, rule := range grillRules {
		if value == rule.Class || value == grillQuestionID(rule.Class) {
			return rule.Class, true
		}
	}
	return "", false
}

func grillRuleTriggered(input GrillInput, ruleClass string) bool {
	declaration := input.Declarations
	switch ruleClass {
	case "observable_outcome":
		return declaration.ObservableOutcome != "declared"
	case "scope_compatibility":
		return declaration.ScopeCompatibility != "declared"
	case "acceptance_evidence":
		return declaration.AcceptanceEvidence != "declared"
	case "destructive_external_authorization":
		return declaration.DestructiveExternal == "declared" && declaration.LocalAuthorization != "recorded"
	case "adapter_worktree_isolation":
		return declaration.AdapterMode != "none" && declaration.WorktreeIsolation != "isolated"
	case "autonomy_budget":
		return declaration.Autonomy.Deadline == nil || declaration.Autonomy.AttemptCap == nil
	default:
		return false
	}
}

// evaluateGrillPure implements the frozen P2a bounds without I/O. Existing
// entries may be rule classes or the deterministic question IDs used by the
// persisted API; accepting both keeps the internal representation private.
func evaluateGrillPure(input GrillInput, existingQuestionIDs []string, priorQuestionCount int, waivedRuleClasses []string) (GrillEvaluation, error) {
	if err := validateGrillInput(input); err != nil {
		return GrillEvaluation{}, err
	}
	if priorQuestionCount < 0 {
		return GrillEvaluation{}, fmt.Errorf("%w: prior question count must be nonnegative", ErrGrillInvalidInput)
	}
	existing := make(map[string]struct{}, len(existingQuestionIDs))
	for _, value := range existingQuestionIDs {
		ruleClass, ok := grillRuleClass(value)
		if !ok {
			return GrillEvaluation{}, fmt.Errorf("%w: unknown existing Grill rule %q", ErrGrillInvalidInput, value)
		}
		existing[ruleClass] = struct{}{}
	}
	waived := make(map[string]struct{}, len(waivedRuleClasses))
	for _, ruleClass := range waivedRuleClasses {
		rule, ok := grillRuleByClass(ruleClass)
		if !ok || !rule.Waivable {
			return GrillEvaluation{}, fmt.Errorf("%w: %s", ErrGrillOverrideNotPermitted, ruleClass)
		}
		waived[ruleClass] = struct{}{}
	}

	result := GrillEvaluation{
		ProposalID: input.ProposalID, Revision: input.Revision, RevisionHash: input.RevisionHash,
		RuleVersion: GrillRuleVersion,
	}
	if priorQuestionCount >= 10 {
		result.Status = GrillStatusNeedsRewrite
		return result, nil
	}

	active := make([]grillRule, 0, len(grillRules))
	for _, rule := range grillRules {
		if grillRuleTriggered(input, rule.Class) {
			if _, isWaived := waived[rule.Class]; !isWaived {
				active = append(active, rule)
			}
		}
	}
	if len(active) > 0 {
		result.DeferredRuleClasses = make([]string, 0)
	}
	alreadyShown := make([]grillRule, 0, len(active))
	available := make([]grillRule, 0, len(active))
	for _, rule := range active {
		if _, found := existing[rule.Class]; found {
			alreadyShown = append(alreadyShown, rule)
		} else {
			available = append(available, rule)
		}
	}
	newLimit := minGrill(5, 10-priorQuestionCount, 5-len(alreadyShown), len(available))
	newRules := available[:newLimit]
	shown := append(append([]grillRule(nil), alreadyShown...), newRules...)
	sort.Slice(shown, func(left, right int) bool { return shown[left].Priority < shown[right].Priority })
	shownClasses := make(map[string]struct{}, len(shown))
	for _, rule := range shown {
		shownClasses[rule.Class] = struct{}{}
		result.ShownQuestionIDs = append(result.ShownQuestionIDs, grillQuestionID(rule.Class))
	}
	for _, rule := range newRules {
		result.NewQuestionIDs = append(result.NewQuestionIDs, grillQuestionID(rule.Class))
	}
	for _, rule := range active {
		if _, shown := shownClasses[rule.Class]; !shown {
			result.DeferredRuleClasses = append(result.DeferredRuleClasses, rule.Class)
		}
	}
	if len(active) == 0 {
		result.Status = GrillStatusClear
	} else {
		result.Status = GrillStatusBlocked
	}
	return result, nil
}

func minGrill(values ...int) int {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	if minimum < 0 {
		return 0
	}
	return minimum
}

// EvaluateGrill validates the exact stored Revision identity, verifies the
// supplied hash, then appends only bounded Question rows. The immediate SQLite
// transaction makes concurrent evaluation observe one append-only stream.
func (s *Store) EvaluateGrill(ctx context.Context, request GrillEvaluationRequest) (GrillEvaluation, error) {
	if err := validateGrillInput(request.Input); err != nil {
		return GrillEvaluation{}, err
	}
	if request.RuleVersion != GrillRuleVersion {
		return GrillEvaluation{}, ErrGrillRuleVersion
	}
	computedHash, err := HashGrillInput(request.Input)
	if err != nil {
		return GrillEvaluation{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return GrillEvaluation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	identity := GrillRevisionIdentity{ProposalID: request.Input.ProposalID, Revision: request.Input.Revision, RevisionHash: request.Input.RevisionHash}
	if err := validateGrillRevisionIdentity(ctx, tx, identity); err != nil {
		return GrillEvaluation{}, err
	}
	if request.InputHash != computedHash {
		return GrillEvaluation{}, ErrGrillInputHashMismatch
	}

	priorQuestionCount, existingRules, waivedRules, err := loadGrillHistory(ctx, tx, identity)
	if err != nil {
		return GrillEvaluation{}, err
	}
	result, err := evaluateGrillPure(request.Input, existingRules, priorQuestionCount, waivedRules)
	if err != nil {
		return GrillEvaluation{}, err
	}
	result.InputHash = computedHash

	canonicalInput, err := canonicalJSON(grillInputCanonicalValue(request.Input))
	if err != nil {
		return GrillEvaluation{}, fmt.Errorf("canonical Grill input: %w", err)
	}
	created, err := insertGrillEvaluation(ctx, tx, identity, request.RuleVersion, computedHash, string(canonicalInput))
	if err != nil {
		return GrillEvaluation{}, err
	}
	if created {
		result.NewRecords++
	}
	for _, questionID := range result.NewQuestionIDs {
		ruleClass, _ := grillRuleClass(questionID)
		rule, _ := grillRuleByClass(ruleClass)
		if err := insertGrillQuestion(ctx, tx, identity, request.RuleVersion, priorQuestionCount, rule); err != nil {
			return GrillEvaluation{}, err
		}
		priorQuestionCount++
		result.NewRecords++
	}
	if err := tx.Commit(); err != nil {
		return GrillEvaluation{}, err
	}
	return result, nil
}

func validateGrillRevisionIdentity(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, identity GrillRevisionIdentity) error {
	var exists int
	err := queryer.QueryRowContext(ctx, `SELECT 1 FROM task_proposal_revisions
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ?`, identity.ProposalID, identity.Revision, identity.RevisionHash).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrGrillRevisionMismatch
	}
	if err != nil {
		return err
	}
	return nil
}

func loadGrillHistory(ctx context.Context, tx *sql.Tx, identity GrillRevisionIdentity) (int, []string, []string, error) {
	var questionCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM grill_records
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ? AND schema_version = ?`,
		identity.ProposalID, identity.Revision, identity.RevisionHash, GrillQuestionSchemaVersion).Scan(&questionCount); err != nil {
		return 0, nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT rule_class FROM grill_records
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ? AND rule_version = ? AND schema_version = ?
		ORDER BY question_sequence ASC`, identity.ProposalID, identity.Revision, identity.RevisionHash, GrillRuleVersion, GrillQuestionSchemaVersion)
	if err != nil {
		return 0, nil, nil, err
	}
	existing := make([]string, 0)
	for rows.Next() {
		var ruleClass string
		if err := rows.Scan(&ruleClass); err != nil {
			_ = rows.Close()
			return 0, nil, nil, err
		}
		existing = append(existing, ruleClass)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return 0, nil, nil, err
	}

	overrides, err := tx.QueryContext(ctx, `SELECT question.rule_class FROM grill_records AS override
		JOIN grill_records AS question
			ON question.proposal_id = override.proposal_id
			AND question.revision = override.revision
			AND question.revision_hash = override.revision_hash
			AND question.rule_version = override.rule_version
			AND question.question_id = override.question_id
		WHERE override.proposal_id = ? AND override.revision = ? AND override.revision_hash = ?
			AND override.rule_version = ? AND override.schema_version = ?
			AND question.schema_version = ?
		ORDER BY override.record_sequence ASC`, identity.ProposalID, identity.Revision, identity.RevisionHash,
		GrillRuleVersion, GrillOverrideSchemaVersion, GrillQuestionSchemaVersion)
	if err != nil {
		return 0, nil, nil, err
	}
	waived := make([]string, 0)
	for overrides.Next() {
		var ruleClass string
		if err := overrides.Scan(&ruleClass); err != nil {
			_ = overrides.Close()
			return 0, nil, nil, err
		}
		waived = append(waived, ruleClass)
	}
	if err := overrides.Err(); err != nil {
		_ = overrides.Close()
		return 0, nil, nil, err
	}
	if err := overrides.Close(); err != nil {
		return 0, nil, nil, err
	}
	return questionCount, existing, waived, nil
}

func insertGrillEvaluation(ctx context.Context, tx *sql.Tx, identity GrillRevisionIdentity, ruleVersion, inputHash, inputJSON string) (bool, error) {
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO grill_evaluations
		(proposal_id, revision, revision_hash, rule_version, input_hash, input_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, identity.ProposalID, identity.Revision, identity.RevisionHash,
		ruleVersion, inputHash, inputJSON, nowStamp())
	if err != nil {
		return false, fmt.Errorf("insert Grill evaluation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func insertGrillQuestion(ctx context.Context, tx *sql.Tx, identity GrillRevisionIdentity, ruleVersion string, priorQuestionCount int, rule grillRule) error {
	var maxRecord sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(record_sequence) FROM grill_records
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ?`, identity.ProposalID, identity.Revision, identity.RevisionHash).Scan(&maxRecord); err != nil {
		return fmt.Errorf("select Grill record sequence: %w", err)
	}
	recordSequence := 1
	if maxRecord.Valid {
		recordSequence = int(maxRecord.Int64) + 1
	}
	questionSequence := priorQuestionCount + 1
	_, err := tx.ExecContext(ctx, `INSERT INTO grill_records
		(proposal_id, revision, revision_hash, rule_version, record_sequence, schema_version,
		 question_id, question_sequence, rule_class, risk, blocking, waivable, default_value,
		 remedial_step, answer_value, override_value, written_at, written_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
		identity.ProposalID, identity.Revision, identity.RevisionHash, ruleVersion, recordSequence,
		GrillQuestionSchemaVersion, grillQuestionID(rule.Class), questionSequence, rule.Class, rule.Risk,
		rule.Blocking, rule.Waivable, rule.Default, rule.RemedialStep, nowStamp(), deterministicGrillWriter)
	if err != nil {
		return fmt.Errorf("insert Grill question: %w", err)
	}
	return nil
}

// RecordGrillDefault materializes the fixed rule default once. Replays are
// idempotent and do not add a second append-only row.
func (s *Store) RecordGrillDefault(ctx context.Context, identity GrillRevisionIdentity, questionID string) (bool, error) {
	return s.recordGrillOperatorAction(ctx, identity, questionID, GrillDefaultSchemaVersion)
}

// RecordGrillAnswer acknowledges one persisted question. It is review-only and
// never changes a Proposal, Approval, or execution state.
func (s *Store) RecordGrillAnswer(ctx context.Context, identity GrillRevisionIdentity, questionID string) (bool, error) {
	return s.recordGrillOperatorAction(ctx, identity, questionID, GrillAnswerSchemaVersion)
}

// RecordGrillOverride records the one allowed scope/compatibility waiver.
func (s *Store) RecordGrillOverride(ctx context.Context, identity GrillRevisionIdentity, questionID string) (bool, error) {
	return s.recordGrillOperatorAction(ctx, identity, questionID, GrillOverrideSchemaVersion)
}

func (s *Store) recordGrillOperatorAction(ctx context.Context, identity GrillRevisionIdentity, questionID, schemaVersion string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateGrillRevisionIdentity(ctx, tx, identity); err != nil {
		return false, err
	}
	var (
		ruleVersion  string
		ruleClass    string
		waivable     bool
		defaultValue string
	)
	err = tx.QueryRowContext(ctx, `SELECT rule_version, rule_class, waivable, default_value
		FROM grill_records WHERE proposal_id = ? AND revision = ? AND revision_hash = ?
			AND schema_version = ? AND question_id = ?`, identity.ProposalID, identity.Revision,
		identity.RevisionHash, GrillQuestionSchemaVersion, questionID).
		Scan(&ruleVersion, &ruleClass, &waivable, &defaultValue)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrGrillQuestionNotFound
	}
	if err != nil {
		return false, err
	}
	if schemaVersion == GrillOverrideSchemaVersion && !waivable {
		return false, ErrGrillOverrideNotPermitted
	}
	if schemaVersion != GrillDefaultSchemaVersion && schemaVersion != GrillAnswerSchemaVersion && schemaVersion != GrillOverrideSchemaVersion {
		return false, fmt.Errorf("unsupported Grill record schema %q", schemaVersion)
	}
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM grill_records WHERE proposal_id = ? AND revision = ? AND revision_hash = ?
		AND rule_version = ? AND schema_version = ? AND question_id = ?`, identity.ProposalID, identity.Revision,
		identity.RevisionHash, ruleVersion, schemaVersion, questionID).Scan(&exists)
	if err == nil {
		return false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var maxRecord sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(record_sequence) FROM grill_records
		WHERE proposal_id = ? AND revision = ? AND revision_hash = ?`, identity.ProposalID, identity.Revision, identity.RevisionHash).Scan(&maxRecord); err != nil {
		return false, err
	}
	recordSequence := 1
	if maxRecord.Valid {
		recordSequence = int(maxRecord.Int64) + 1
	}
	writer := localGUIOperator
	if schemaVersion == GrillDefaultSchemaVersion {
		writer = deterministicGrillWriter
	}
	var answer, override any
	if schemaVersion == GrillAnswerSchemaVersion {
		answer = "acknowledged"
	}
	if schemaVersion == GrillOverrideSchemaVersion {
		override = "waived"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO grill_records
		(proposal_id, revision, revision_hash, rule_version, record_sequence, schema_version,
		 question_id, question_sequence, rule_class, risk, blocking, waivable, default_value,
		 remedial_step, answer_value, override_value, written_at, written_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, ?, NULL, ?, ?, ?, ?)`,
		identity.ProposalID, identity.Revision, identity.RevisionHash, ruleVersion, recordSequence, schemaVersion,
		questionID, nullableGrillDefault(schemaVersion, defaultValue), answer, override, nowStamp(), writer)
	if err != nil {
		return false, fmt.Errorf("insert Grill review record: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func nullableGrillDefault(schemaVersion, value string) any {
	if schemaVersion == GrillDefaultSchemaVersion {
		return value
	}
	return nil
}

// ListGrillRecords returns the exact append-only stream in record-sequence
// order. It never projects review rows into Proposal or Approval state.
func (s *Store) ListGrillRecords(ctx context.Context, identity GrillRevisionIdentity) ([]GrillRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT proposal_id, revision, revision_hash, rule_version, record_sequence,
		schema_version, question_id, question_sequence, rule_class, risk, blocking, waivable,
		default_value, remedial_step, answer_value, override_value, written_at, written_by
		FROM grill_records WHERE proposal_id = ? AND revision = ? AND revision_hash = ?
		ORDER BY record_sequence ASC`, identity.ProposalID, identity.Revision, identity.RevisionHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]GrillRecord, 0)
	for rows.Next() {
		var (
			record                                                                    GrillRecord
			questionID, ruleClass, risk, defaultValue, remedialStep, answer, override sql.NullString
			questionSequence                                                          sql.NullInt64
			blocking, waivable                                                        sql.NullBool
			writtenAt                                                                 string
		)
		if err := rows.Scan(&record.ProposalID, &record.Revision, &record.RevisionHash, &record.RuleVersion,
			&record.RecordSequence, &record.SchemaVersion, &questionID, &questionSequence, &ruleClass, &risk,
			&blocking, &waivable, &defaultValue, &remedialStep, &answer, &override, &writtenAt, &record.WrittenBy); err != nil {
			return nil, err
		}
		record.QuestionID = questionID.String
		if questionSequence.Valid {
			record.QuestionSequence = int(questionSequence.Int64)
		}
		record.RuleClass = ruleClass.String
		record.Risk = risk.String
		record.Blocking = blocking.Bool
		record.Waivable = waivable.Bool
		record.Default = defaultValue.String
		record.RemedialStep = remedialStep.String
		record.Answer = answer.String
		record.Override = override.String
		var err error
		record.WrittenAt, err = parseStamp(writtenAt)
		if err != nil {
			return nil, fmt.Errorf("parse Grill record written_at: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
