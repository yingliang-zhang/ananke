package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

type p2aGrillFixture struct {
	Input      GrillInput `json:"input"`
	ValidInput GrillInput `json:"valid_input"`
	Evaluation struct {
		Initial struct {
			EvaluationID        string   `json:"evaluation_id"`
			InputHash           string   `json:"input_hash"`
			ProposalID          string   `json:"proposal_id"`
			Revision            int      `json:"revision"`
			RevisionHash        string   `json:"revision_hash"`
			RuleVersion         string   `json:"rule_version"`
			NewQuestionIDs      []string `json:"new_question_ids"`
			ShownQuestionIDs    []string `json:"shown_question_ids"`
			DeferredRuleClasses []string `json:"deferred_rule_classes"`
			Status              string   `json:"status"`
		} `json:"initial"`
		AfterScopeOverride struct {
			EvaluationID        string   `json:"evaluation_id"`
			InputHash           string   `json:"input_hash"`
			ProposalID          string   `json:"proposal_id"`
			Revision            int      `json:"revision"`
			RevisionHash        string   `json:"revision_hash"`
			RuleVersion         string   `json:"rule_version"`
			NewQuestionIDs      []string `json:"new_question_ids"`
			ShownQuestionIDs    []string `json:"shown_question_ids"`
			DeferredRuleClasses []string `json:"deferred_rule_classes"`
			Status              string   `json:"status"`
		} `json:"after_scope_override"`
		SameInputReplay struct {
			EvaluationID        string   `json:"evaluation_id"`
			InputHash           string   `json:"input_hash"`
			ProposalID          string   `json:"proposal_id"`
			Revision            int      `json:"revision"`
			RevisionHash        string   `json:"revision_hash"`
			RuleVersion         string   `json:"rule_version"`
			NewQuestionIDs      []string `json:"new_question_ids"`
			NewRecords          int      `json:"new_records"`
			ShownQuestionIDs    []string `json:"shown_question_ids"`
			DeferredRuleClasses []string `json:"deferred_rule_classes"`
			Status              string   `json:"status"`
		} `json:"same_input_replay"`
	} `json:"evaluation"`
	Records       []json.RawMessage `json:"records"`
	Rules         []json.RawMessage `json:"rules"`
	SchemaVersion string            `json:"schema_version"`
}

func TestP2AGrillFrozenInputHashAndPureEvaluator(t *testing.T) {
	fixture := readP2AGrillFixture(t)
	if got, err := HashGrillInput(fixture.Input); err != nil {
		t.Fatalf("HashGrillInput: %v", err)
	} else if got != fixture.Evaluation.Initial.InputHash {
		t.Fatalf("input hash = %q, want %q", got, fixture.Evaluation.Initial.InputHash)
	}

	clear, err := evaluateGrillPure(fixture.ValidInput, nil, 0, nil)
	if err != nil {
		t.Fatalf("evaluate complete declarations: %v", err)
	}
	assertGrillEvaluationShape(t, clear, nil, nil, nil, GrillStatusClear)

	initial, err := evaluateGrillPure(fixture.Input, nil, 0, nil)
	if err != nil {
		t.Fatalf("evaluate canonical missing declarations: %v", err)
	}
	assertGrillEvaluationShape(t, initial, fixture.Evaluation.Initial.NewQuestionIDs, fixture.Evaluation.Initial.ShownQuestionIDs, fixture.Evaluation.Initial.DeferredRuleClasses, GrillStatusBlocked)

	replayed, err := evaluateGrillPure(fixture.Input, fixture.Evaluation.Initial.NewQuestionIDs, 5, nil)
	if err != nil {
		t.Fatalf("evaluate same input replay: %v", err)
	}
	assertGrillEvaluationShape(t, replayed, nil, fixture.Evaluation.Initial.ShownQuestionIDs, fixture.Evaluation.Initial.DeferredRuleClasses, GrillStatusBlocked)

	waived, err := evaluateGrillPure(fixture.Input, fixture.Evaluation.Initial.NewQuestionIDs, 5, []string{"scope_compatibility"})
	if err != nil {
		t.Fatalf("evaluate scope waiver: %v", err)
	}
	assertGrillEvaluationShape(t, waived, fixture.Evaluation.AfterScopeOverride.NewQuestionIDs, fixture.Evaluation.AfterScopeOverride.ShownQuestionIDs, fixture.Evaluation.AfterScopeOverride.DeferredRuleClasses, GrillStatusBlocked)

	atCapacity, err := evaluateGrillPure(fixture.Input, nil, 10, nil)
	if err != nil {
		t.Fatalf("evaluate capacity: %v", err)
	}
	assertGrillEvaluationShape(t, atCapacity, nil, nil, nil, GrillStatusNeedsRewrite)
}

func TestEvaluateGrillPersistsFrozenVectorAndReplayAcrossRestart(t *testing.T) {
	ctx := context.Background()
	fixture := readP2AGrillFixture(t)
	dbPath := filepath.Join(t.TempDir(), "grill.sqlite")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	identity := grillIdentity(fixture.Input)
	seedGrillRevision(t, s, identity)
	beforeProposal, beforeApproval := grillProposalStates(t, s, identity)

	initial, err := s.EvaluateGrill(ctx, GrillEvaluationRequest{
		Input:       fixture.Input,
		InputHash:   fixture.Evaluation.Initial.InputHash,
		RuleVersion: GrillRuleVersion,
	})
	if err != nil {
		t.Fatalf("EvaluateGrill initial: %v", err)
	}
	assertGrillEvaluationShape(t, initial, fixture.Evaluation.Initial.NewQuestionIDs, fixture.Evaluation.Initial.ShownQuestionIDs, fixture.Evaluation.Initial.DeferredRuleClasses, GrillStatusBlocked)
	if initial.NewRecords != 6 {
		t.Fatalf("initial new records = %d, want one evaluation and five questions", initial.NewRecords)
	}
	assertGrillRecordSequences(t, s, identity, 5, 5)
	if gotProposal, gotApproval := grillProposalStates(t, s, identity); gotProposal != beforeProposal || gotApproval != beforeApproval {
		t.Fatalf("Grill mutated Proposal/Approval state = %q/%q, want %q/%q", gotProposal, gotApproval, beforeProposal, beforeApproval)
	}

	if _, err := s.RecordGrillDefault(ctx, identity, "grill_question_scope_compatibility"); err != nil {
		t.Fatalf("RecordGrillDefault: %v", err)
	}
	if _, err := s.RecordGrillAnswer(ctx, identity, "grill_question_acceptance_evidence"); err != nil {
		t.Fatalf("RecordGrillAnswer: %v", err)
	}
	if _, err := s.RecordGrillOverride(ctx, identity, "grill_question_scope_compatibility"); err != nil {
		t.Fatalf("RecordGrillOverride: %v", err)
	}
	assertGrillRecordSequences(t, s, identity, 8, 5)

	afterWaiver, err := s.EvaluateGrill(ctx, GrillEvaluationRequest{
		Input:       fixture.Input,
		InputHash:   fixture.Evaluation.Initial.InputHash,
		RuleVersion: GrillRuleVersion,
	})
	if err != nil {
		t.Fatalf("EvaluateGrill after scope waiver: %v", err)
	}
	assertGrillEvaluationShape(t, afterWaiver, fixture.Evaluation.AfterScopeOverride.NewQuestionIDs, fixture.Evaluation.AfterScopeOverride.ShownQuestionIDs, fixture.Evaluation.AfterScopeOverride.DeferredRuleClasses, GrillStatusBlocked)
	if afterWaiver.NewRecords != 1 {
		t.Fatalf("post-waiver new records = %d, want autonomy question only", afterWaiver.NewRecords)
	}
	assertGrillRecordSequences(t, s, identity, 9, 6)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	replayed, err := s.EvaluateGrill(ctx, GrillEvaluationRequest{
		Input:       fixture.Input,
		InputHash:   fixture.Evaluation.Initial.InputHash,
		RuleVersion: GrillRuleVersion,
	})
	if err != nil {
		t.Fatalf("EvaluateGrill restart replay: %v", err)
	}
	assertGrillEvaluationShape(t, replayed, nil, fixture.Evaluation.AfterScopeOverride.ShownQuestionIDs, fixture.Evaluation.AfterScopeOverride.DeferredRuleClasses, GrillStatusBlocked)
	if replayed.NewRecords != 0 {
		t.Fatalf("restart replay new records = %d, want 0", replayed.NewRecords)
	}
	assertGrillRecordSequences(t, s, identity, 9, 6)
}

func TestGrillRejectsInvalidInputIdentityAndAuthorityWithoutRows(t *testing.T) {
	ctx := context.Background()
	fixture := readP2AGrillFixture(t)
	s := newTestStore(t)
	identity := grillIdentity(fixture.Input)
	seedGrillRevision(t, s, identity)
	request := GrillEvaluationRequest{Input: fixture.Input, InputHash: fixture.Evaluation.Initial.InputHash, RuleVersion: GrillRuleVersion}

	for _, check := range []struct {
		name string
		edit func(*GrillEvaluationRequest)
		want error
	}{
		{
			name: "mismatched proposal identity",
			edit: func(request *GrillEvaluationRequest) { request.Input.ProposalID = "proposal_other_001" },
			want: ErrGrillRevisionMismatch,
		},
		{
			name: "mismatched revision hash",
			edit: func(request *GrillEvaluationRequest) {
				request.Input.RevisionHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
			want: ErrGrillRevisionMismatch,
		},
		{
			name: "mismatched supplied input hash",
			edit: func(request *GrillEvaluationRequest) {
				request.InputHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
			want: ErrGrillInputHashMismatch,
		},
		{
			name: "unknown rule version",
			edit: func(request *GrillEvaluationRequest) { request.RuleVersion = "ananke.grill.rules.v2" },
			want: ErrGrillRuleVersion,
		},
		{
			name: "unbounded attempt cap",
			edit: func(request *GrillEvaluationRequest) {
				attemptCap := 101
				request.Input.Declarations.Autonomy.AttemptCap = &attemptCap
			},
			want: ErrGrillInvalidInput,
		},
	} {
		t.Run(check.name, func(t *testing.T) {
			candidate := request
			candidate.Input = cloneGrillInput(t, request.Input)
			check.edit(&candidate)
			if _, err := s.EvaluateGrill(ctx, candidate); !errors.Is(err, check.want) {
				t.Fatalf("EvaluateGrill error = %v, want %v", err, check.want)
			}
			assertGrillRecordSequences(t, s, identity, 0, 0)
		})
	}

	if _, err := s.EvaluateGrill(ctx, request); err != nil {
		t.Fatalf("seed questions: %v", err)
	}
	if _, err := s.RecordGrillOverride(ctx, identity, "grill_question_acceptance_evidence"); !errors.Is(err, ErrGrillOverrideNotPermitted) {
		t.Fatalf("non-waivable override error = %v, want %v", err, ErrGrillOverrideNotPermitted)
	}
	if _, err := s.RecordGrillDefault(ctx, identity, "grill_question_acceptance_evidence"); err != nil {
		t.Fatalf("record valid default: %v", err)
	}
	if _, err := s.RecordGrillDefault(ctx, identity, "grill_question_acceptance_evidence"); err != nil {
		t.Fatalf("idempotent default replay: %v", err)
	}
	assertGrillRecordSequences(t, s, identity, 6, 5)
}

func TestGrillConcurrentEvaluationAndRevisionStreamsRemainIndependent(t *testing.T) {
	ctx := context.Background()
	fixture := readP2AGrillFixture(t)
	dbPath := filepath.Join(t.TempDir(), "concurrent.sqlite")
	seed, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open seed store: %v", err)
	}
	identity := grillIdentity(fixture.Input)
	seedGrillRevision(t, seed, identity)
	if _, err := seed.EvaluateGrill(ctx, GrillEvaluationRequest{Input: fixture.Input, InputHash: fixture.Evaluation.Initial.InputHash, RuleVersion: GrillRuleVersion}); err != nil {
		t.Fatalf("initial evaluation: %v", err)
	}
	if _, err := seed.RecordGrillOverride(ctx, identity, "grill_question_scope_compatibility"); err != nil {
		t.Fatalf("scope override: %v", err)
	}
	secondIdentity := identity
	secondIdentity.Revision = 2
	secondIdentity.RevisionHash = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	seedGrillRevision(t, seed, secondIdentity)
	secondInput := cloneGrillInput(t, fixture.Input)
	secondInput.Revision = secondIdentity.Revision
	secondInput.RevisionHash = secondIdentity.RevisionHash
	secondHash, err := HashGrillInput(secondInput)
	if err != nil {
		t.Fatalf("hash second revision input: %v", err)
	}
	if _, err := seed.EvaluateGrill(ctx, GrillEvaluationRequest{Input: secondInput, InputHash: secondHash, RuleVersion: GrillRuleVersion}); err != nil {
		t.Fatalf("second revision evaluation: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	left, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open left store: %v", err)
	}
	defer left.Close()
	right, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open right store: %v", err)
	}
	defer right.Close()
	request := GrillEvaluationRequest{Input: fixture.Input, InputHash: fixture.Evaluation.Initial.InputHash, RuleVersion: GrillRuleVersion}
	start := make(chan struct{})
	results := make(chan GrillEvaluation, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, concurrentStore := range []*Store{left, right} {
		wait.Add(1)
		go func(concurrentStore *Store) {
			defer wait.Done()
			<-start
			result, err := concurrentStore.EvaluateGrill(ctx, request)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(concurrentStore)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent EvaluateGrill: %v", err)
	}
	newQuestionWrites := 0
	for result := range results {
		newQuestionWrites += len(result.NewQuestionIDs)
		assertGrillEvaluationShape(t, result, result.NewQuestionIDs, fixture.Evaluation.AfterScopeOverride.ShownQuestionIDs, fixture.Evaluation.AfterScopeOverride.DeferredRuleClasses, GrillStatusBlocked)
	}
	if newQuestionWrites != 1 {
		t.Fatalf("concurrent autonomy writes = %d, want 1", newQuestionWrites)
	}
	assertGrillRecordSequences(t, left, identity, 7, 6)
	assertGrillRecordSequences(t, left, secondIdentity, 5, 5)
}

func TestGrillQuestionCapStopsAtTenWithoutProposalMutation(t *testing.T) {
	ctx := context.Background()
	fixture := readP2AGrillFixture(t)
	s := newTestStore(t)
	identity := grillIdentity(fixture.Input)
	seedGrillRevision(t, s, identity)
	seedFutureGrillQuestions(t, s, identity, 9)
	beforeProposal, beforeApproval := grillProposalStates(t, s, identity)

	first, err := s.EvaluateGrill(ctx, GrillEvaluationRequest{Input: fixture.Input, InputHash: fixture.Evaluation.Initial.InputHash, RuleVersion: GrillRuleVersion})
	if err != nil {
		t.Fatalf("EvaluateGrill from nine questions: %v", err)
	}
	assertGrillEvaluationShape(t, first, []string{"grill_question_observable_outcome"}, []string{"grill_question_observable_outcome"}, []string{"scope_compatibility", "acceptance_evidence", "destructive_external_authorization", "adapter_worktree_isolation", "autonomy_budget"}, GrillStatusBlocked)
	assertGrillRecordSequences(t, s, identity, 10, 10)

	second, err := s.EvaluateGrill(ctx, GrillEvaluationRequest{Input: fixture.Input, InputHash: fixture.Evaluation.Initial.InputHash, RuleVersion: GrillRuleVersion})
	if err != nil {
		t.Fatalf("EvaluateGrill at cap: %v", err)
	}
	assertGrillEvaluationShape(t, second, nil, nil, nil, GrillStatusNeedsRewrite)
	if second.NewRecords != 0 {
		t.Fatalf("at-cap evaluation wrote %d records, want 0", second.NewRecords)
	}
	assertGrillRecordSequences(t, s, identity, 10, 10)
	if gotProposal, gotApproval := grillProposalStates(t, s, identity); gotProposal != beforeProposal || gotApproval != beforeApproval {
		t.Fatalf("Grill cap mutated Proposal/Approval state = %q/%q, want %q/%q", gotProposal, gotApproval, beforeProposal, beforeApproval)
	}
}

func TestGrillMigrationFromP1bHead(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "p1b-head.sqlite")
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema history: %v", err)
	}
	for _, migration := range migrations[:9] {
		tx, err := rawDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin migration v%d: %v", migration.version, err)
		}
		if err := migration.up(ctx, tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply migration v%d: %v", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`, migration.version, nowStamp()); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record migration v%d: %v", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration v%d: %v", migration.version, err)
		}
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close raw database: %v", err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer s.Close()
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("SchemaVersion = %d, want %d", version, len(migrations))
	}
	for _, name := range []string{"grill_evaluations", "grill_records"} {
		var count int
		if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("%s table count = %d, want 1", name, count)
		}
	}
}

func readP2AGrillFixture(t *testing.T) p2aGrillFixture {
	t.Helper()
	path := filepath.Join("..", "..", "contracts", "p2a", "fixtures", "grill-v1.canonical.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read P2a fixture: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var fixture p2aGrillFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode P2a fixture: %v", err)
	}
	return fixture
}

func grillIdentity(input GrillInput) GrillRevisionIdentity {
	return GrillRevisionIdentity{ProposalID: input.ProposalID, Revision: input.Revision, RevisionHash: input.RevisionHash}
}

func cloneGrillInput(t *testing.T, input GrillInput) GrillInput {
	t.Helper()
	contents, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal Grill input: %v", err)
	}
	var cloned GrillInput
	if err := json.Unmarshal(contents, &cloned); err != nil {
		t.Fatalf("unmarshal Grill input: %v", err)
	}
	return cloned
}

func assertGrillEvaluationShape(t *testing.T, evaluation GrillEvaluation, wantNew, wantShown, wantDeferred []string, wantStatus GrillStatus) {
	t.Helper()
	if !reflect.DeepEqual(evaluation.NewQuestionIDs, wantNew) || !reflect.DeepEqual(evaluation.ShownQuestionIDs, wantShown) || !reflect.DeepEqual(evaluation.DeferredRuleClasses, wantDeferred) || evaluation.Status != wantStatus {
		t.Fatalf("evaluation = %+v, want new=%v shown=%v deferred=%v status=%s", evaluation, wantNew, wantShown, wantDeferred, wantStatus)
	}
}

func seedGrillRevision(t *testing.T, s *Store, identity GrillRevisionIdentity) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin P1 revision seed: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		t.Fatalf("defer P1 revision keys: %v", err)
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_proposals WHERE proposal_id = ?`, identity.ProposalID).Scan(&exists); err != nil {
		t.Fatalf("check P1 proposal seed: %v", err)
	}
	if exists == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposals
			(proposal_id, project_id, workstream_id, created_at, created_by, state, current_revision, current_revision_hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, identity.ProposalID, "project_p2b", "workstream_p2b", nowStamp(), localGUIOperator, ProposalStateOpen, identity.Revision, identity.RevisionHash); err != nil {
			t.Fatalf("insert P1 proposal seed: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revisions
		(proposal_id, revision, revision_hash, snapshot_json) VALUES (?, ?, ?, ?)`, identity.ProposalID, identity.Revision, identity.RevisionHash, "{}"); err != nil {
		t.Fatalf("insert P1 revision seed: %v", err)
	}
	approvalID := "approval_grill_" + string(rune('a'+identity.Revision))
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_approvals
		(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state, decided_at, decided_by, decision_idempotency_key, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL)`, approvalID, identity.ProposalID, identity.Revision, identity.RevisionHash, nowStamp(), localGUIOperator, ApprovalStatePending); err != nil {
		t.Fatalf("insert P1 approval seed: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_proposal_revision_lifecycles
		(proposal_id, revision, revision_hash, approval_id, state, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, identity.ProposalID, identity.Revision, identity.RevisionHash, approvalID, RevisionLifecycleStatePending, nowStamp(), nowStamp(), 1); err != nil {
		t.Fatalf("insert P1 lifecycle seed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit P1 revision seed: %v", err)
	}
}

func grillProposalStates(t *testing.T, s *Store, identity GrillRevisionIdentity) (string, string) {
	t.Helper()
	ctx := context.Background()
	var proposalState, approvalState string
	if err := s.DB().QueryRowContext(ctx, `SELECT state FROM task_proposals WHERE proposal_id = ?`, identity.ProposalID).Scan(&proposalState); err != nil {
		t.Fatalf("read P1 proposal state: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT state FROM task_proposal_approvals WHERE proposal_id = ? AND revision = ? AND revision_hash = ?`, identity.ProposalID, identity.Revision, identity.RevisionHash).Scan(&approvalState); err != nil {
		t.Fatalf("read P1 approval state: %v", err)
	}
	return proposalState, approvalState
}

func assertGrillRecordSequences(t *testing.T, s *Store, identity GrillRevisionIdentity, wantRecords, wantQuestions int) {
	t.Helper()
	ctx := context.Background()
	records, err := s.ListGrillRecords(ctx, identity)
	if err != nil {
		t.Fatalf("ListGrillRecords: %v", err)
	}
	if len(records) != wantRecords {
		t.Fatalf("record count = %d, want %d (%+v)", len(records), wantRecords, records)
	}
	questionCount := 0
	for index, record := range records {
		if record.RecordSequence != index+1 {
			t.Fatalf("record %d sequence = %d, want %d", index, record.RecordSequence, index+1)
		}
		if record.SchemaVersion == GrillQuestionSchemaVersion {
			questionCount++
			if record.QuestionSequence != questionCount {
				t.Fatalf("question %d sequence = %d, want %d", questionCount, record.QuestionSequence, questionCount)
			}
		}
	}
	if questionCount != wantQuestions {
		t.Fatalf("question count = %d, want %d", questionCount, wantQuestions)
	}
}

func seedFutureGrillQuestions(t *testing.T, s *Store, identity GrillRevisionIdentity, count int) {
	t.Helper()
	ctx := context.Background()
	for sequence := 1; sequence <= count; sequence++ {
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO grill_records
			(proposal_id, revision, revision_hash, rule_version, record_sequence, schema_version, question_id, question_sequence, rule_class, risk, blocking, waivable, default_value, remedial_step, written_at, written_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			identity.ProposalID, identity.Revision, identity.RevisionHash, "ananke.grill.rules.future", sequence,
			GrillQuestionSchemaVersion, "grill_question_future_"+string(rune('a'+sequence)), sequence, "future_rule_"+string(rune('a'+sequence)), "high", true, false, "needs_rewrite", "future_step", nowStamp(), deterministicGrillWriter); err != nil {
			t.Fatalf("seed future question %d: %v", sequence, err)
		}
	}
}
