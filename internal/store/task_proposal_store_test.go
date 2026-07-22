package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateProposalCreatesDurablePendingPair(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	request := createProposalRequestFromFixture(t)

	result, err := s.CreateProposal(ctx, request)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if result.ProposalID == "" || result.Revision != 1 || result.RevisionHash == "" || result.ApprovalID == "" {
		t.Fatalf("CreateProposal identity = %+v, want proposal/revision/hash/approval", result)
	}

	proposal, err := s.GetProposal(ctx, result.ProposalID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if proposal.ProjectID != request.ProjectID || proposal.WorkstreamID != request.WorkstreamID {
		t.Fatalf("proposal target = %q/%q, want %q/%q", proposal.ProjectID, proposal.WorkstreamID, request.ProjectID, request.WorkstreamID)
	}
	if proposal.State != ProposalStateOpen || proposal.CurrentRevision != result.Revision || proposal.CurrentRevisionHash != result.RevisionHash {
		t.Fatalf("proposal = %+v, want open current revision identity", proposal)
	}

	revision, err := s.GetRevision(ctx, result.ProposalID, 1)
	if err != nil {
		t.Fatalf("GetRevision: %v", err)
	}
	if revision.ProposalID != result.ProposalID || revision.ParentRevision != nil || revision.ParentRevisionHash != nil {
		t.Fatalf("initial revision = %+v, want immutable root snapshot", revision)
	}
	if revision.IdempotencyKey != request.IdempotencyKey || revision.Task != request.RevisionInput.Task || len(revision.AcceptanceCriteria) != len(request.RevisionInput.AcceptanceCriteria) {
		t.Fatalf("revision did not preserve fixture input: %+v", revision)
	}

	lifecycle, err := s.GetRevisionLifecycle(ctx, result.ProposalID, 1)
	if err != nil {
		t.Fatalf("GetRevisionLifecycle: %v", err)
	}
	if lifecycle.RevisionHash != result.RevisionHash || lifecycle.ApprovalID != result.ApprovalID || lifecycle.State != RevisionLifecycleStatePending || lifecycle.Version != 1 {
		t.Fatalf("lifecycle = %+v, want initial pending pair", lifecycle)
	}

	approval, err := s.GetApproval(ctx, result.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if approval.ProposalID != result.ProposalID || approval.Revision != 1 || approval.RevisionHash != result.RevisionHash || approval.State != ApprovalStatePending {
		t.Fatalf("approval = %+v, want pending revision reference", approval)
	}
	if approval.DecidedAt != nil || approval.DecidedBy != nil || approval.DecisionIdempotencyKey != nil || approval.Reason != nil {
		t.Fatalf("pending approval has decision values: %+v", approval)
	}

	activity, err := s.ListProposalActivity(ctx, result.ProposalID)
	if err != nil {
		t.Fatalf("ListProposalActivity: %v", err)
	}
	if len(activity) != 1 || activity[0].Operation != ProposalActivityCreate || activity[0].Revision != 1 || activity[0].ApprovalID != result.ApprovalID {
		t.Fatalf("activity = %+v, want one create activity record", activity)
	}
}

func TestAppendProposalRevisionSupersedesPendingPairAtomically(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	appended, err := s.AppendProposalRevision(ctx, appendProposalRequestFromFixture(t, created))
	if err != nil {
		t.Fatalf("AppendProposalRevision: %v", err)
	}
	if appended.ProposalID != created.ProposalID || appended.Revision != 2 || appended.RevisionHash == "" || appended.ApprovalID == "" {
		t.Fatalf("append identity = %+v, want revision two of the created proposal", appended)
	}

	proposal, err := s.GetProposal(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if proposal.State != ProposalStateOpen || proposal.CurrentRevision != 2 || proposal.CurrentRevisionHash != appended.RevisionHash {
		t.Fatalf("proposal after append = %+v, want open revision two", proposal)
	}
	revision, err := s.GetRevision(ctx, created.ProposalID, 2)
	if err != nil {
		t.Fatalf("GetRevision two: %v", err)
	}
	if revision.ParentRevision == nil || *revision.ParentRevision != 1 || revision.ParentRevisionHash == nil || *revision.ParentRevisionHash != created.RevisionHash {
		t.Fatalf("appended parent = %v/%v, want 1/%q", revision.ParentRevision, revision.ParentRevisionHash, created.RevisionHash)
	}

	formerLifecycle, err := s.GetRevisionLifecycle(ctx, created.ProposalID, 1)
	if err != nil {
		t.Fatalf("GetRevisionLifecycle one: %v", err)
	}
	formerApproval, err := s.GetApproval(ctx, created.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval one: %v", err)
	}
	if formerLifecycle.State != RevisionLifecycleStateSuperseded || formerLifecycle.Version != 2 || formerApproval.State != ApprovalStateSuperseded {
		t.Fatalf("former pair after append = lifecycle %+v approval %+v, want superseded", formerLifecycle, formerApproval)
	}

	currentLifecycle, err := s.GetRevisionLifecycle(ctx, created.ProposalID, 2)
	if err != nil {
		t.Fatalf("GetRevisionLifecycle two: %v", err)
	}
	currentApproval, err := s.GetApproval(ctx, appended.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval two: %v", err)
	}
	if currentLifecycle.State != RevisionLifecycleStatePending || currentLifecycle.Version != 1 || currentLifecycle.ApprovalID != appended.ApprovalID || currentApproval.State != ApprovalStatePending {
		t.Fatalf("new pair after append = lifecycle %+v approval %+v, want pending", currentLifecycle, currentApproval)
	}

	activity, err := s.ListProposalActivity(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("ListProposalActivity: %v", err)
	}
	if len(activity) != 2 || activity[1].Operation != ProposalActivityAppend || activity[1].Revision != 2 || activity[1].ApprovalID != appended.ApprovalID {
		t.Fatalf("activity after append = %+v, want create then append", activity)
	}
}

func appendProposalRequestFromFixture(t *testing.T, base ProposalMutation) AppendProposalRevisionRequest {
	t.Helper()
	var envelopes struct {
		Append struct {
			Body           json.RawMessage `json:"body"`
			IdempotencyKey string          `json:"idempotency_key"`
		} `json:"append"`
	}
	readP1AJSONFixture(t, "request-envelopes-v1.canonical.json", &envelopes)
	var body struct {
		RevisionInput RevisionInput `json:"revision_input"`
	}
	if err := json.Unmarshal(envelopes.Append.Body, &body); err != nil {
		t.Fatalf("decode append request body: %v", err)
	}
	return AppendProposalRevisionRequest{
		IdempotencyKey:              envelopes.Append.IdempotencyKey,
		ProposalID:                  base.ProposalID,
		ExpectedCurrentRevision:     base.Revision,
		ExpectedCurrentRevisionHash: base.RevisionHash,
		RevisionInput:               body.RevisionInput,
	}
}

func TestDecideProposalApprovalApprovesPairAndProposalAtomically(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	request := decisionProposalRequestFromFixture(t, created, ApprovalStateApproved)

	result, err := s.DecideProposalApproval(ctx, request)
	if err != nil {
		t.Fatalf("DecideProposalApproval: %v", err)
	}
	if result != created {
		t.Fatalf("decision identity = %+v, want %+v", result, created)
	}
	proposal, err := s.GetProposal(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if proposal.State != ProposalStateApproved {
		t.Fatalf("proposal state = %q, want approved", proposal.State)
	}
	lifecycle, err := s.GetRevisionLifecycle(ctx, created.ProposalID, created.Revision)
	if err != nil {
		t.Fatalf("GetRevisionLifecycle: %v", err)
	}
	approval, err := s.GetApproval(ctx, created.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if lifecycle.State != RevisionLifecycleStateApproved || lifecycle.Version != 2 || approval.State != ApprovalStateApproved {
		t.Fatalf("decision records = lifecycle %+v approval %+v, want approved", lifecycle, approval)
	}
	if approval.DecidedAt == nil || approval.DecidedBy == nil || *approval.DecidedBy != localGUIOperator ||
		approval.DecisionIdempotencyKey == nil || *approval.DecisionIdempotencyKey != request.IdempotencyKey ||
		approval.Reason == nil || *approval.Reason != request.Reason {
		t.Fatalf("approval decision values = %+v, want durable fixture decision", approval)
	}
	activity, err := s.ListProposalActivity(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("ListProposalActivity: %v", err)
	}
	if len(activity) != 2 || activity[1].Operation != ProposalActivityDecision || activity[1].Revision != 1 {
		t.Fatalf("activity after decision = %+v, want create then decision", activity)
	}
}

func TestRejectedProposalCanAppendWithoutChangingRejectedPredecessor(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if _, err := s.DecideProposalApproval(ctx, decisionProposalRequestFromFixture(t, created, ApprovalStateRejected)); err != nil {
		t.Fatalf("reject approval: %v", err)
	}
	appended, err := s.AppendProposalRevision(ctx, appendProposalRequestFromFixture(t, created))
	if err != nil {
		t.Fatalf("append after rejection: %v", err)
	}
	formerLifecycle, err := s.GetRevisionLifecycle(ctx, created.ProposalID, 1)
	if err != nil {
		t.Fatalf("GetRevisionLifecycle one: %v", err)
	}
	formerApproval, err := s.GetApproval(ctx, created.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval one: %v", err)
	}
	if formerLifecycle.State != RevisionLifecycleStateRejected || formerLifecycle.Version != 2 || formerApproval.State != ApprovalStateRejected {
		t.Fatalf("rejected predecessor changed during append: lifecycle %+v approval %+v", formerLifecycle, formerApproval)
	}
	currentLifecycle, err := s.GetRevisionLifecycle(ctx, created.ProposalID, appended.Revision)
	if err != nil {
		t.Fatalf("GetRevisionLifecycle appended: %v", err)
	}
	if currentLifecycle.State != RevisionLifecycleStatePending {
		t.Fatalf("new lifecycle state = %q, want pending", currentLifecycle.State)
	}
}

func TestWithdrawProposalAfterRejectionPreservesRejectedPair(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if _, err := s.DecideProposalApproval(ctx, decisionProposalRequestFromFixture(t, created, ApprovalStateRejected)); err != nil {
		t.Fatalf("reject approval: %v", err)
	}
	result, err := s.WithdrawProposal(ctx, withdrawProposalRequestFromFixture(t, created.ProposalID))
	if err != nil {
		t.Fatalf("WithdrawProposal: %v", err)
	}
	if result != created {
		t.Fatalf("withdraw identity = %+v, want %+v", result, created)
	}
	proposal, err := s.GetProposal(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if proposal.State != ProposalStateWithdrawn {
		t.Fatalf("proposal state = %q, want withdrawn", proposal.State)
	}
	lifecycle, err := s.GetRevisionLifecycle(ctx, created.ProposalID, 1)
	if err != nil {
		t.Fatalf("GetRevisionLifecycle: %v", err)
	}
	approval, err := s.GetApproval(ctx, created.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if lifecycle.State != RevisionLifecycleStateRejected || lifecycle.Version != 2 || approval.State != ApprovalStateRejected {
		t.Fatalf("withdraw changed rejected pair: lifecycle %+v approval %+v", lifecycle, approval)
	}
	activity, err := s.ListProposalActivity(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("ListProposalActivity: %v", err)
	}
	if len(activity) != 3 || activity[2].Operation != ProposalActivityWithdraw {
		t.Fatalf("activity after withdrawal = %+v, want create, reject, withdraw", activity)
	}
}

func withdrawProposalRequestFromFixture(t *testing.T, proposalID string) WithdrawProposalRequest {
	t.Helper()
	var envelopes struct {
		Withdraw struct {
			IdempotencyKey string `json:"idempotency_key"`
		} `json:"withdraw"`
	}
	readP1AJSONFixture(t, "request-envelopes-v1.canonical.json", &envelopes)
	return WithdrawProposalRequest{IdempotencyKey: envelopes.Withdraw.IdempotencyKey, ProposalID: proposalID}
}

func TestProposalSchemaMigrationFromV6Fixture(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "v6fixture.sqlite")
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema version: %v", err)
	}
	for _, migration := range migrations[:6] {
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
		t.Fatalf("close v6 fixture: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open migrated v6 fixture: %v", err)
	}
	defer s.Close()
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("SchemaVersion = %d, want %d", version, len(migrations))
	}
	if _, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t)); err != nil {
		t.Fatalf("CreateProposal after migration: %v", err)
	}
}

func TestProposalV7DataUpgradesToCompositeIdentityForeignKeys(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "v7proposal.sqlite")
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open v7 fixture: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		_ = rawDB.Close()
		t.Fatalf("create schema version: %v", err)
	}
	for _, migration := range migrations[:7] {
		tx, err := rawDB.BeginTx(ctx, nil)
		if err != nil {
			_ = rawDB.Close()
			t.Fatalf("begin migration v%d: %v", migration.version, err)
		}
		if err := migration.up(ctx, tx); err != nil {
			_ = tx.Rollback()
			_ = rawDB.Close()
			t.Fatalf("apply migration v%d: %v", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`, migration.version, nowStamp()); err != nil {
			_ = tx.Rollback()
			_ = rawDB.Close()
			t.Fatalf("record migration v%d: %v", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			_ = rawDB.Close()
			t.Fatalf("commit migration v%d: %v", migration.version, err)
		}
	}
	assertHistoricalV7TaskProposalForeignKeys(t, rawDB)
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		_ = rawDB.Close()
		t.Fatalf("begin v7 fixture: %v", err)
	}
	created := seedHistoricalV7ProposalChain(t, ctx, tx)
	if err := tx.Commit(); err != nil {
		_ = rawDB.Close()
		t.Fatalf("commit v7 fixture: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close v7 fixture: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("upgrade v7 fixture: %v", err)
	}
	defer s.Close()
	if version, err := s.SchemaVersion(ctx); err != nil || version != 9 {
		t.Fatalf("SchemaVersion after v7 upgrade = %d, %v; want 9, nil", version, err)
	}
	for _, version := range []int{8, 9} {
		var count int
		if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_version WHERE version = ?`, version).Scan(&count); err != nil {
			t.Fatalf("count schema version %d: %v", version, err)
		}
		if count != 1 {
			t.Fatalf("schema version %d rows = %d, want 1", version, count)
		}
	}
	if replay, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t)); err != nil || replay != created {
		t.Fatalf("create replay after v7 upgrade = %+v, %v; want %+v, nil", replay, err, created)
	}
	assertProposalJournalCounts(t, s, created.ProposalID, 1, 1, 1, 1, 1, 1)
	assertApprovalLifecycleForeignKey(t, s)
	foreignHash := insertUnattachedRevision(t, s, created.ProposalID, 2)
	execForeignKeyFailure(t, s, `INSERT INTO task_proposal_approvals
		(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "approval_orphan_p1a", created.ProposalID, 2, foreignHash, nowStamp(), localGUIOperator, ApprovalStatePending)
	assertForeignKeyCheckClean(t, s)
	for _, index := range []string{
		"idx_task_proposal_revisions_hash",
		"idx_task_proposal_approvals_revision",
		"idx_task_proposal_activity_proposal",
	} {
		var count int
		if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatalf("look up index %s: %v", index, err)
		}
		if count != 0 {
			t.Fatalf("redundant v7 index %s remains after upgrade", index)
		}
	}
}

func assertHistoricalV7TaskProposalForeignKeys(t *testing.T, db *sql.DB) {
	t.Helper()
	expected := map[string][]string{
		"task_proposals": {},
		"task_proposal_revisions": {
			"proposal_id->task_proposals(proposal_id)",
		},
		"task_proposal_approvals": {
			"proposal_id,revision->task_proposal_revisions(proposal_id,revision)",
		},
		"task_proposal_revision_lifecycles": {
			"approval_id->task_proposal_approvals(approval_id)",
			"proposal_id,revision->task_proposal_revisions(proposal_id,revision)",
		},
		"task_proposal_activity": {
			"proposal_id,revision->task_proposal_revisions(proposal_id,revision)",
		},
		"task_proposal_idempotency": {
			"proposal_id,revision->task_proposal_revisions(proposal_id,revision)",
		},
	}
	for table, want := range expected {
		rows, err := db.Query(`PRAGMA foreign_key_list(` + table + `)`)
		if err != nil {
			t.Fatalf("list v7 foreign keys for %s: %v", table, err)
		}
		actual := make(map[string]struct{})
		columns := make(map[int][]string)
		targets := make(map[int][]string)
		references := make(map[int]string)
		for rows.Next() {
			var id, sequence int
			var referencedTable, from, to, onUpdate, onDelete, match string
			if err := rows.Scan(&id, &sequence, &referencedTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				_ = rows.Close()
				t.Fatalf("scan v7 foreign key for %s: %v", table, err)
			}
			columns[id] = append(columns[id], from)
			targets[id] = append(targets[id], to)
			references[id] = referencedTable
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("iterate v7 foreign keys for %s: %v", table, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close v7 foreign keys for %s: %v", table, err)
		}
		for id, from := range columns {
			actual[strings.Join(from, ",")+"->"+references[id]+"("+strings.Join(targets[id], ",")+")"] = struct{}{}
		}
		if len(actual) != len(want) {
			t.Fatalf("v7 foreign keys for %s = %v, want %v", table, actual, want)
		}
		for _, signature := range want {
			if _, ok := actual[signature]; !ok {
				t.Fatalf("v7 foreign keys for %s = %v, want %v", table, actual, want)
			}
		}
	}
}

func seedHistoricalV7ProposalChain(t *testing.T, ctx context.Context, tx *sql.Tx) ProposalMutation {
	t.Helper()
	request := createProposalRequestFromFixture(t)
	createdAt, err := parseStamp("2026-07-22T09:00:00Z")
	if err != nil {
		t.Fatalf("parse fixture timestamp: %v", err)
	}
	result := ProposalMutation{ProposalID: "proposal_p1a_001", Revision: 1, ApprovalID: "approval_p1a_001"}
	revision := Revision{
		SchemaVersion:      proposalRevisionSchemaVersion,
		ProposalID:         result.ProposalID,
		Revision:           result.Revision,
		CreatedAt:          createdAt,
		CreatedBy:          localGUIOperator,
		IdempotencyKey:     request.IdempotencyKey,
		Task:               request.RevisionInput.Task,
		AcceptanceCriteria: request.RevisionInput.AcceptanceCriteria,
		Policy:             request.RevisionInput.Policy,
	}
	snapshot, revisionHash, err := canonicalRevisionSnapshot(revision)
	if err != nil {
		t.Fatalf("canonical historical revision: %v", err)
	}
	result.RevisionHash = revisionHash
	bodyHash, err := canonicalJSONHash(createRequestBody(request))
	if err != nil {
		t.Fatalf("canonical historical create request: %v", err)
	}
	createdAtText := createdAt.Format(time.RFC3339Nano)
	for _, statement := range []struct {
		statement string
		arguments []any
	}{
		{`INSERT INTO task_proposals
			(proposal_id, project_id, workstream_id, created_at, created_by, state, current_revision, current_revision_hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, []any{result.ProposalID, request.ProjectID, request.WorkstreamID, createdAtText, localGUIOperator, ProposalStateOpen, result.Revision, result.RevisionHash}},
		{`INSERT INTO task_proposal_revisions
			(proposal_id, revision, revision_hash, snapshot_json) VALUES (?, ?, ?, ?)`, []any{result.ProposalID, result.Revision, result.RevisionHash, string(snapshot)}},
		{`INSERT INTO task_proposal_approvals
			(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, []any{result.ApprovalID, result.ProposalID, result.Revision, result.RevisionHash, createdAtText, localGUIOperator, ApprovalStatePending}},
		{`INSERT INTO task_proposal_revision_lifecycles
			(proposal_id, revision, revision_hash, approval_id, state, created_at, updated_at, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, []any{result.ProposalID, result.Revision, result.RevisionHash, result.ApprovalID, RevisionLifecycleStatePending, createdAtText, createdAtText, 1}},
		{`INSERT INTO task_proposal_activity
			(proposal_id, sequence, operation, revision, revision_hash, approval_id, written_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, []any{result.ProposalID, 1, ProposalActivityCreate, result.Revision, result.RevisionHash, result.ApprovalID, createdAtText}},
		{`INSERT INTO task_proposal_idempotency
			(actor, operation, resource, idempotency_key, request_body_hash, proposal_id, revision, revision_hash, approval_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{localGUIOperator, "create_proposal", "proposal_collection", request.IdempotencyKey, bodyHash, result.ProposalID, result.Revision, result.RevisionHash, result.ApprovalID}},
	} {
		if _, err := tx.ExecContext(ctx, statement.statement, statement.arguments...); err != nil {
			t.Fatalf("seed historical v7 proposal chain: %v", err)
		}
	}
	return result
}

func TestProposalReplaysBeforeMutableChecksAfterRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "proposal-replay.sqlite")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	createRequest := createProposalRequestFromFixture(t)
	created, err := s.CreateProposal(ctx, createRequest)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	appendRequest := appendProposalRequestFromFixture(t, created)
	appended, err := s.AppendProposalRevision(ctx, appendRequest)
	if err != nil {
		t.Fatalf("AppendProposalRevision: %v", err)
	}
	decisionRequest := decisionProposalRequestFromFixture(t, appended, ApprovalStateRejected)
	if _, err := s.DecideProposalApproval(ctx, decisionRequest); err != nil {
		t.Fatalf("reject appended approval: %v", err)
	}
	nextAppend := appendProposalRequestFromFixture(t, appended)
	nextAppend.IdempotencyKey = "revise_p1a_003"
	if _, err := s.AppendProposalRevision(ctx, nextAppend); err != nil {
		t.Fatalf("append successor: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close before replay: %v", err)
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	if replay, err := s.CreateProposal(ctx, createRequest); err != nil || replay != created {
		t.Fatalf("create replay = %+v, %v; want %+v, nil", replay, err, created)
	}
	if replay, err := s.AppendProposalRevision(ctx, appendRequest); err != nil || replay != appended {
		t.Fatalf("append replay = %+v, %v; want %+v, nil", replay, err, appended)
	}
	if replay, err := s.DecideProposalApproval(ctx, decisionRequest); err != nil || replay != appended {
		t.Fatalf("decision replay = %+v, %v; want %+v, nil", replay, err, appended)
	}
	activity, err := s.ListProposalActivity(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("ListProposalActivity: %v", err)
	}
	if len(activity) != 4 {
		t.Fatalf("activity after restart replays = %+v, want four original mutations", activity)
	}
}

func TestProposalConflictsLeaveNoPartialRecords(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createRequest := createProposalRequestFromFixture(t)
	created, err := s.CreateProposal(ctx, createRequest)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	conflictingCreate := createRequest
	conflictingCreate.WorkstreamID = "workstream_other"
	if _, err := s.CreateProposal(ctx, conflictingCreate); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same create key different body error = %v, want %v", err, ErrIdempotencyConflict)
	}
	if count := proposalRowCount(t, s, "task_proposals"); count != 1 {
		t.Fatalf("proposal rows after idempotency conflict = %d, want 1", count)
	}

	staleAppend := appendProposalRequestFromFixture(t, created)
	staleAppend.ExpectedCurrentRevisionHash = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err := s.AppendProposalRevision(ctx, staleAppend); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale append error = %v, want %v", err, ErrRevisionConflict)
	}
	if _, err := s.GetRevision(ctx, created.ProposalID, 2); !errors.Is(err, ErrRevisionNotFound) {
		t.Fatalf("revision two after stale append error = %v, want %v", err, ErrRevisionNotFound)
	}

	if _, err := s.DecideProposalApproval(ctx, decisionProposalRequestFromFixture(t, created, ApprovalStateApproved)); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := s.DecideProposalApproval(ctx, decisionProposalRequestFromFixture(t, created, ApprovalStateRejected)); !errors.Is(err, ErrApprovalConflict) {
		t.Fatalf("competing decision error = %v, want %v", err, ErrApprovalConflict)
	}
	activity, err := s.ListProposalActivity(ctx, created.ProposalID)
	if err != nil {
		t.Fatalf("ListProposalActivity: %v", err)
	}
	if len(activity) != 2 {
		t.Fatalf("activity after conflicts = %+v, want create and approved decision only", activity)
	}
}

func TestConcurrentProposalMutationsAreLinearizable(t *testing.T) {
	t.Run("append", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
		if err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		first := appendProposalRequestFromFixture(t, created)
		second := first
		second.IdempotencyKey = "revise_p1a_002_b"
		start := make(chan struct{})
		outcomes := make(chan error, 2)
		go func() { <-start; _, err := s.AppendProposalRevision(ctx, first); outcomes <- err }()
		go func() { <-start; _, err := s.AppendProposalRevision(ctx, second); outcomes <- err }()
		close(start)
		var successes, conflicts int
		for range 2 {
			if err := <-outcomes; err == nil {
				successes++
			} else if errors.Is(err, ErrRevisionConflict) {
				conflicts++
			} else {
				t.Fatalf("concurrent append error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent append outcomes = %d success, %d conflicts", successes, conflicts)
		}
		assertProposalActivityCount(t, s, created.ProposalID, 2)
	})

	t.Run("decision", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
		if err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		approve := decisionProposalRequestFromFixture(t, created, ApprovalStateApproved)
		reject := decisionProposalRequestFromFixture(t, created, ApprovalStateRejected)
		start := make(chan struct{})
		outcomes := make(chan error, 2)
		go func() { <-start; _, err := s.DecideProposalApproval(ctx, approve); outcomes <- err }()
		go func() { <-start; _, err := s.DecideProposalApproval(ctx, reject); outcomes <- err }()
		close(start)
		var successes, conflicts int
		for range 2 {
			if err := <-outcomes; err == nil {
				successes++
			} else if errors.Is(err, ErrApprovalConflict) {
				conflicts++
			} else {
				t.Fatalf("concurrent decision error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent decision outcomes = %d success, %d conflicts", successes, conflicts)
		}
		assertProposalActivityCount(t, s, created.ProposalID, 2)
	})

	t.Run("append versus approval", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
		if err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		appendRequest := appendProposalRequestFromFixture(t, created)
		approve := decisionProposalRequestFromFixture(t, created, ApprovalStateApproved)
		start := make(chan struct{})
		outcomes := make(chan error, 2)
		go func() { <-start; _, err := s.AppendProposalRevision(ctx, appendRequest); outcomes <- err }()
		go func() { <-start; _, err := s.DecideProposalApproval(ctx, approve); outcomes <- err }()
		close(start)
		var successes, conflicts int
		for range 2 {
			if err := <-outcomes; err == nil {
				successes++
			} else if errors.Is(err, ErrRevisionConflict) || errors.Is(err, ErrApprovalConflict) {
				conflicts++
			} else {
				t.Fatalf("append versus approval error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("append versus approval outcomes = %d success, %d conflicts", successes, conflicts)
		}
		assertProposalActivityCount(t, s, created.ProposalID, 2)
	})

	t.Run("append versus rejection", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
		if err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		appendRequest := appendProposalRequestFromFixture(t, created)
		reject := decisionProposalRequestFromFixture(t, created, ApprovalStateRejected)
		start := make(chan struct{})
		outcomes := make(chan error, 2)
		go func() { <-start; _, err := s.AppendProposalRevision(ctx, appendRequest); outcomes <- err }()
		go func() { <-start; _, err := s.DecideProposalApproval(ctx, reject); outcomes <- err }()
		close(start)
		var successes, approvalConflicts int
		for range 2 {
			if err := <-outcomes; err == nil {
				successes++
			} else if errors.Is(err, ErrApprovalConflict) {
				approvalConflicts++
			} else {
				t.Fatalf("append versus rejection error = %v", err)
			}
		}
		if (successes != 1 || approvalConflicts != 1) && successes != 2 {
			t.Fatalf("append versus rejection outcomes = %d success, %d approval conflicts", successes, approvalConflicts)
		}
		if successes == 1 {
			assertProposalActivityCount(t, s, created.ProposalID, 2)
			return
		}
		proposal, err := s.GetProposal(ctx, created.ProposalID)
		if err != nil {
			t.Fatalf("GetProposal: %v", err)
		}
		former, err := s.GetRevisionLifecycle(ctx, created.ProposalID, 1)
		if err != nil {
			t.Fatalf("GetRevisionLifecycle: %v", err)
		}
		current, err := s.GetRevisionLifecycle(ctx, created.ProposalID, 2)
		if err != nil {
			t.Fatalf("GetRevisionLifecycle two: %v", err)
		}
		if proposal.State != ProposalStateOpen || proposal.CurrentRevision != 2 || former.State != RevisionLifecycleStateRejected || current.State != RevisionLifecycleStatePending {
			t.Fatalf("reject-first final state = proposal %+v former %+v current %+v", proposal, former, current)
		}
		assertProposalActivityCount(t, s, created.ProposalID, 3)
	})

	t.Run("same key replay", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		request := createProposalRequestFromFixture(t)
		start := make(chan struct{})
		type outcome struct {
			result ProposalMutation
			err    error
		}
		outcomes := make(chan outcome, 2)
		go func() { <-start; result, err := s.CreateProposal(ctx, request); outcomes <- outcome{result, err} }()
		go func() { <-start; result, err := s.CreateProposal(ctx, request); outcomes <- outcome{result, err} }()
		close(start)
		first, second := <-outcomes, <-outcomes
		if first.err != nil || second.err != nil || first.result != second.result {
			t.Fatalf("same-key concurrent replays = %+v / %+v, want same successful identity", first, second)
		}
		assertProposalActivityCount(t, s, first.result.ProposalID, 1)
		if count := proposalRowCount(t, s, "task_proposals"); count != 1 {
			t.Fatalf("proposal rows after same-key race = %d, want 1", count)
		}
	})
}

func TestCrossStoreProposalMutationsPreserveP1ASemantics(t *testing.T) {
	t.Run("same key create replays winner identity", func(t *testing.T) {
		firstStore, secondStore := openConcurrentProposalStores(t)
		request := createProposalRequestFromFixture(t)
		start := make(chan struct{})
		outcomes := make(chan proposalMutationOutcome, 2)
		go func() {
			<-start
			result, err := firstStore.CreateProposal(context.Background(), request)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		go func() {
			<-start
			result, err := secondStore.CreateProposal(context.Background(), request)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		close(start)
		first, second := <-outcomes, <-outcomes
		if first.err != nil || second.err != nil || first.result != second.result {
			t.Fatalf("concurrent same-key create = %+v / %+v, want identical successful winner replay", first, second)
		}
		assertProposalJournalCounts(t, firstStore, first.result.ProposalID, 1, 1, 1, 1, 1, 1)
		assertForeignKeyCheckClean(t, firstStore)
	})

	t.Run("same base append returns revision conflict", func(t *testing.T) {
		firstStore, secondStore := openConcurrentProposalStores(t)
		created, err := firstStore.CreateProposal(context.Background(), createProposalRequestFromFixture(t))
		if err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		firstRequest := appendProposalRequestFromFixture(t, created)
		secondRequest := firstRequest
		secondRequest.IdempotencyKey = "revise_p1a_002_b"
		start := make(chan struct{})
		outcomes := make(chan proposalMutationOutcome, 2)
		go func() {
			<-start
			result, err := firstStore.AppendProposalRevision(context.Background(), firstRequest)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		go func() {
			<-start
			result, err := secondStore.AppendProposalRevision(context.Background(), secondRequest)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		close(start)
		assertConcurrentOutcomes(t, outcomes, ErrRevisionConflict, 1, 1)
		assertProposalJournalCounts(t, firstStore, created.ProposalID, 1, 2, 2, 2, 2, 2)
		assertForeignKeyCheckClean(t, firstStore)
	})

	t.Run("competing decisions return approval conflict", func(t *testing.T) {
		firstStore, secondStore := openConcurrentProposalStores(t)
		created, err := firstStore.CreateProposal(context.Background(), createProposalRequestFromFixture(t))
		if err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		approve := decisionProposalRequestFromFixture(t, created, ApprovalStateApproved)
		reject := decisionProposalRequestFromFixture(t, created, ApprovalStateRejected)
		start := make(chan struct{})
		outcomes := make(chan proposalMutationOutcome, 2)
		go func() {
			<-start
			result, err := firstStore.DecideProposalApproval(context.Background(), approve)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		go func() {
			<-start
			result, err := secondStore.DecideProposalApproval(context.Background(), reject)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		close(start)
		assertConcurrentOutcomes(t, outcomes, ErrApprovalConflict, 1, 1)
		assertProposalJournalCounts(t, firstStore, created.ProposalID, 1, 1, 1, 1, 2, 2)
		assertForeignKeyCheckClean(t, firstStore)
	})

	t.Run("append versus rejection preserves an allowed linearization", func(t *testing.T) {
		firstStore, secondStore := openConcurrentProposalStores(t)
		created, err := firstStore.CreateProposal(context.Background(), createProposalRequestFromFixture(t))
		if err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}
		appendRequest := appendProposalRequestFromFixture(t, created)
		reject := decisionProposalRequestFromFixture(t, created, ApprovalStateRejected)
		start := make(chan struct{})
		outcomes := make(chan proposalMutationOutcome, 2)
		go func() {
			<-start
			result, err := firstStore.AppendProposalRevision(context.Background(), appendRequest)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		go func() {
			<-start
			result, err := secondStore.DecideProposalApproval(context.Background(), reject)
			outcomes <- proposalMutationOutcome{result, err}
		}()
		close(start)

		var successes, conflicts int
		for range 2 {
			outcome := <-outcomes
			if outcome.err == nil {
				successes++
				continue
			}
			if errors.Is(outcome.err, ErrApprovalConflict) {
				conflicts++
				continue
			}
			t.Fatalf("append versus rejection error = %v, want P1a success or approval_conflict", outcome.err)
		}
		if (successes != 1 || conflicts != 1) && successes != 2 {
			t.Fatalf("append versus rejection outcomes = %d successes, %d conflicts", successes, conflicts)
		}
		if successes == 1 {
			assertProposalJournalCounts(t, firstStore, created.ProposalID, 1, 2, 2, 2, 2, 2)
		} else {
			assertProposalJournalCounts(t, firstStore, created.ProposalID, 1, 2, 2, 2, 3, 3)
			former, err := firstStore.GetRevisionLifecycle(context.Background(), created.ProposalID, 1)
			if err != nil {
				t.Fatalf("GetRevisionLifecycle former: %v", err)
			}
			current, err := firstStore.GetRevisionLifecycle(context.Background(), created.ProposalID, 2)
			if err != nil {
				t.Fatalf("GetRevisionLifecycle current: %v", err)
			}
			if former.State != RevisionLifecycleStateRejected || current.State != RevisionLifecycleStatePending {
				t.Fatalf("rejection-first lifecycle states = %q/%q, want rejected/pending", former.State, current.State)
			}
		}
		assertForeignKeyCheckClean(t, firstStore)
	})
}

type proposalMutationOutcome struct {
	result ProposalMutation
	err    error
}

func openConcurrentProposalStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cross-store.sqlite")
	first, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	second, err := Open(dbPath)
	if err != nil {
		_ = first.Close()
		t.Fatalf("open second store: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second store: %v", err)
		}
		if err := first.Close(); err != nil {
			t.Errorf("close first store: %v", err)
		}
	})
	return first, second
}

func assertConcurrentOutcomes(t *testing.T, outcomes <-chan proposalMutationOutcome, expectedConflict error, wantSuccesses, wantConflicts int) {
	t.Helper()
	var successes, conflicts int
	for range 2 {
		outcome := <-outcomes
		if outcome.err == nil {
			successes++
			continue
		}
		if errors.Is(outcome.err, expectedConflict) {
			conflicts++
			continue
		}
		t.Fatalf("concurrent mutation error = %v, want success or %v", outcome.err, expectedConflict)
	}
	if successes != wantSuccesses || conflicts != wantConflicts {
		t.Fatalf("concurrent mutation outcomes = %d successes, %d conflicts; want %d/%d", successes, conflicts, wantSuccesses, wantConflicts)
	}
}

func assertProposalJournalCounts(t *testing.T, s *Store, proposalID string, wantProposals, wantRevisions, wantApprovals, wantLifecycles, wantActivity, wantIdempotency int) {
	t.Helper()
	for _, check := range []struct {
		table string
		want  int
	}{
		{"task_proposals", wantProposals},
		{"task_proposal_revisions", wantRevisions},
		{"task_proposal_approvals", wantApprovals},
		{"task_proposal_revision_lifecycles", wantLifecycles},
		{"task_proposal_activity", wantActivity},
		{"task_proposal_idempotency", wantIdempotency},
	} {
		var got int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM `+check.table+` WHERE proposal_id = ?`, proposalID).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", check.table, err)
		}
		if got != check.want {
			t.Fatalf("%s rows = %d, want %d", check.table, got, check.want)
		}
	}
}

func TestProposalIdentityForeignKeysRejectCrossLinks(t *testing.T) {
	t.Run("lifecycle hash must belong to its revision", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		foreignHash := insertUnattachedRevision(t, s, other.ProposalID, 2)
		execForeignKeyFailure(t, s, `UPDATE task_proposal_revision_lifecycles
			SET revision_hash = ? WHERE proposal_id = ? AND revision = ?`, foreignHash, created.ProposalID, created.Revision)
		assertForeignKeyCheckClean(t, s)
	})

	t.Run("approval hash must belong to its revision", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		foreignHash := insertUnattachedRevision(t, s, other.ProposalID, 2)
		execForeignKeyFailure(t, s, `UPDATE task_proposal_approvals
			SET revision_hash = ? WHERE approval_id = ?`, foreignHash, created.ApprovalID)
		assertForeignKeyCheckClean(t, s)
	})

	t.Run("approval must name its lifecycle pair", func(t *testing.T) {
		s, _, other := newProposalIdentityFixture(t)
		foreignHash := insertUnattachedRevision(t, s, other.ProposalID, 2)
		execForeignKeyFailure(t, s, `INSERT INTO task_proposal_approvals
			(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, "approval_foreign_p1a", other.ProposalID, 2, foreignHash, nowStamp(), localGUIOperator, ApprovalStatePending)
		assertForeignKeyCheckClean(t, s)
	})

	t.Run("proposal pointer must belong to its proposal", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		execForeignKeyFailure(t, s, `UPDATE task_proposals
			SET current_revision_hash = ? WHERE proposal_id = ?`, other.RevisionHash, created.ProposalID)
		assertForeignKeyCheckClean(t, s)
	})

	t.Run("activity identity must name its lifecycle pair", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		execForeignKeyFailure(t, s, `INSERT INTO task_proposal_activity
			(proposal_id, sequence, operation, revision, revision_hash, approval_id, written_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, created.ProposalID, 2, "adversarial", other.Revision, other.RevisionHash, other.ApprovalID, nowStamp())
		assertForeignKeyCheckClean(t, s)
	})

	t.Run("idempotency identity must name its lifecycle pair", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		execForeignKeyFailure(t, s, `INSERT INTO task_proposal_idempotency
			(actor, operation, resource, idempotency_key, request_body_hash, proposal_id, revision, revision_hash, approval_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, localGUIOperator, "adversarial", "adversarial", "adversarial_p1a_key", "sha256:adversarial", created.ProposalID, other.Revision, other.RevisionHash, other.ApprovalID)
		assertForeignKeyCheckClean(t, s)
	})
}

func newProposalIdentityFixture(t *testing.T) (*Store, ProposalMutation, ProposalMutation) {
	t.Helper()
	s := newTestStore(t)
	firstRequest := createProposalRequestFromFixture(t)
	created, err := s.CreateProposal(context.Background(), firstRequest)
	if err != nil {
		t.Fatalf("create first proposal: %v", err)
	}
	secondRequest := firstRequest
	secondRequest.IdempotencyKey = "create_p1a_second"
	other, err := s.CreateProposal(context.Background(), secondRequest)
	if err != nil {
		t.Fatalf("create second proposal: %v", err)
	}
	return s, created, other
}

func insertUnattachedRevision(t *testing.T, s *Store, proposalID string, revision int) string {
	t.Helper()
	foreignHash := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := s.DB().ExecContext(context.Background(), `INSERT INTO task_proposal_revisions
		(proposal_id, revision, revision_hash, snapshot_json) VALUES (?, ?, ?, ?)`, proposalID, revision, foreignHash, "{}"); err != nil {
		t.Fatalf("insert unattached revision: %v", err)
	}
	return foreignHash
}

func execForeignKeyFailure(t *testing.T, s *Store, statement string, args ...any) {
	t.Helper()
	_, err := s.DB().ExecContext(context.Background(), statement, args...)
	assertForeignKeyFailure(t, err)
}

func assertForeignKeyFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("raw SQL error = %v, want foreign key constraint failure", err)
	}
}

func assertForeignKeyCheckClean(t *testing.T, s *Store) {
	t.Helper()
	rows, err := s.DB().QueryContext(context.Background(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var foreignKeyIndex int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyIndex); err != nil {
			t.Fatalf("scan foreign_key_check: %v", err)
		}
		t.Fatalf("foreign_key_check violation: table=%s rowid=%v parent=%s fk=%d", table, rowID, parent, foreignKeyIndex)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check rows: %v", err)
	}
}

func assertApprovalLifecycleForeignKey(t *testing.T, s *Store) {
	t.Helper()
	rows, err := s.DB().QueryContext(context.Background(), `PRAGMA foreign_key_list(task_proposal_approvals)`)
	if err != nil {
		t.Fatalf("list approval foreign keys: %v", err)
	}
	defer rows.Close()
	columns := map[int]map[int]string{}
	for rows.Next() {
		var (
			foreignKeyID, sequence int
			table, from, to        string
			onUpdate, onDelete     string
			match                  string
		)
		if err := rows.Scan(&foreignKeyID, &sequence, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan approval foreign key: %v", err)
		}
		if table != "task_proposal_revision_lifecycles" {
			continue
		}
		if columns[foreignKeyID] == nil {
			columns[foreignKeyID] = map[int]string{}
		}
		columns[foreignKeyID][sequence] = from + "->" + to
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list approval foreign keys: %v", err)
	}
	want := []string{
		"proposal_id->proposal_id",
		"revision->revision",
		"revision_hash->revision_hash",
		"approval_id->approval_id",
	}
	for _, foreignKeyColumns := range columns {
		if len(foreignKeyColumns) != len(want) {
			continue
		}
		matches := true
		for sequence, column := range want {
			if foreignKeyColumns[sequence] != column {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("approval schema has no full-tuple lifecycle foreign key")
}

func assertForeignKeyCheckDetects(t *testing.T, s *Store, wantTable string) {
	t.Helper()
	rows, err := s.DB().QueryContext(context.Background(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var foreignKeyIndex int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyIndex); err != nil {
			t.Fatalf("scan foreign_key_check: %v", err)
		}
		if table == wantTable {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check rows: %v", err)
	}
	t.Fatalf("foreign_key_check did not report %s corruption", wantTable)
}

func TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled(t *testing.T) {
	t.Run("lifecycle", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		foreignHash := insertUnattachedRevision(t, s, other.ProposalID, 2)
		disableProposalForeignKeys(t, s)
		if _, err := s.DB().ExecContext(context.Background(), `UPDATE task_proposal_revision_lifecycles
			SET revision_hash = ? WHERE proposal_id = ? AND revision = ?`, foreignHash, created.ProposalID, created.Revision); err != nil {
			t.Fatalf("corrupt lifecycle: %v", err)
		}
		if _, err := s.GetRevisionLifecycle(context.Background(), created.ProposalID, created.Revision); !errors.Is(err, ErrProposalRecordCorrupt) {
			t.Fatalf("GetRevisionLifecycle error = %v, want %v", err, ErrProposalRecordCorrupt)
		}
	})

	t.Run("approval", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		foreignHash := insertUnattachedRevision(t, s, other.ProposalID, 2)
		disableProposalForeignKeys(t, s)
		if _, err := s.DB().ExecContext(context.Background(), `UPDATE task_proposal_approvals
			SET revision_hash = ? WHERE approval_id = ?`, foreignHash, created.ApprovalID); err != nil {
			t.Fatalf("corrupt approval: %v", err)
		}
		if _, err := s.GetApproval(context.Background(), created.ApprovalID); !errors.Is(err, ErrProposalRecordCorrupt) {
			t.Fatalf("GetApproval error = %v, want %v", err, ErrProposalRecordCorrupt)
		}
	})

	t.Run("approval rejects lifecycle reassignment", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		foreignHash := insertUnattachedRevision(t, s, other.ProposalID, 2)
		disableProposalForeignKeys(t, s)
		if _, err := s.DB().ExecContext(context.Background(), `INSERT INTO task_proposal_approvals
			(approval_id, proposal_id, revision, revision_hash, created_at, created_by, state)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, "approval_foreign_p1a", other.ProposalID, 2, foreignHash, nowStamp(), localGUIOperator, ApprovalStatePending); err != nil {
			t.Fatalf("insert foreign approval: %v", err)
		}
		if _, err := s.DB().ExecContext(context.Background(), `UPDATE task_proposal_revision_lifecycles
			SET approval_id = ? WHERE proposal_id = ? AND revision = ?`, "approval_foreign_p1a", created.ProposalID, created.Revision); err != nil {
			t.Fatalf("reassign lifecycle approval: %v", err)
		}
		if _, err := s.GetApproval(context.Background(), created.ApprovalID); !errors.Is(err, ErrProposalRecordCorrupt) {
			t.Fatalf("GetApproval after lifecycle reassignment error = %v, want %v", err, ErrProposalRecordCorrupt)
		}
		assertForeignKeyCheckDetects(t, s, "task_proposal_approvals")
	})

	t.Run("proposal pointer", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		disableProposalForeignKeys(t, s)
		if _, err := s.DB().ExecContext(context.Background(), `UPDATE task_proposals
			SET current_revision_hash = ? WHERE proposal_id = ?`, other.RevisionHash, created.ProposalID); err != nil {
			t.Fatalf("corrupt proposal pointer: %v", err)
		}
		if _, err := s.GetProposal(context.Background(), created.ProposalID); !errors.Is(err, ErrProposalRecordCorrupt) {
			t.Fatalf("GetProposal error = %v, want %v", err, ErrProposalRecordCorrupt)
		}
	})

	t.Run("activity", func(t *testing.T) {
		s, created, other := newProposalIdentityFixture(t)
		disableProposalForeignKeys(t, s)
		if _, err := s.DB().ExecContext(context.Background(), `UPDATE task_proposal_activity
			SET revision = ?, revision_hash = ?, approval_id = ? WHERE proposal_id = ? AND sequence = 1`, other.Revision, other.RevisionHash, other.ApprovalID, created.ProposalID); err != nil {
			t.Fatalf("corrupt activity: %v", err)
		}
		if _, err := s.ListProposalActivity(context.Background(), created.ProposalID); !errors.Is(err, ErrProposalRecordCorrupt) {
			t.Fatalf("ListProposalActivity error = %v, want %v", err, ErrProposalRecordCorrupt)
		}
	})

	t.Run("idempotency response", func(t *testing.T) {
		s, _, other := newProposalIdentityFixture(t)
		disableProposalForeignKeys(t, s)
		if _, err := s.DB().ExecContext(context.Background(), `UPDATE task_proposal_idempotency
			SET proposal_id = ?
			WHERE actor = ? AND operation = ? AND resource = ? AND idempotency_key = ?`,
			other.ProposalID, localGUIOperator, "create_proposal", "proposal_collection", "create_p1a_001"); err != nil {
			t.Fatalf("corrupt idempotency response: %v", err)
		}
		if _, err := s.CreateProposal(context.Background(), createProposalRequestFromFixture(t)); !errors.Is(err, ErrProposalRecordCorrupt) {
			t.Fatalf("CreateProposal replay error = %v, want %v", err, ErrProposalRecordCorrupt)
		}
	})
}

func disableProposalForeignKeys(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.DB().ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	t.Cleanup(func() {
		if _, err := s.DB().ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
			t.Errorf("restore foreign keys: %v", err)
		}
	})
}

func assertProposalActivityCount(t *testing.T, s *Store, proposalID string, want int) {
	t.Helper()
	activity, err := s.ListProposalActivity(context.Background(), proposalID)
	if err != nil {
		t.Fatalf("ListProposalActivity: %v", err)
	}
	if len(activity) != want {
		t.Fatalf("activity count = %d, want %d: %+v", len(activity), want, activity)
	}
}

func proposalRowCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestCreateProposalAcceptsCanonicalUTF8ControlText(t *testing.T) {
	s := newTestStore(t)
	request := createProposalRequestFromFixture(t)
	request.RevisionInput.Task.Title = "P1a\x00control"
	if _, err := s.CreateProposal(context.Background(), request); err != nil {
		t.Fatalf("CreateProposal rejects valid UTF-8 control text: %v", err)
	}
}

func decisionProposalRequestFromFixture(t *testing.T, target ProposalMutation, decision ApprovalState) DecideProposalApprovalRequest {
	t.Helper()
	var envelopes struct {
		DecisionApprove struct {
			Body           json.RawMessage `json:"body"`
			IdempotencyKey string          `json:"idempotency_key"`
		} `json:"decision_approve"`
		DecisionReject struct {
			Body           json.RawMessage `json:"body"`
			IdempotencyKey string          `json:"idempotency_key"`
		} `json:"decision_reject"`
	}
	readP1AJSONFixture(t, "request-envelopes-v1.canonical.json", &envelopes)
	envelope := envelopes.DecisionApprove
	if decision == ApprovalStateRejected {
		envelope = envelopes.DecisionReject
	}
	var body struct {
		Decision ApprovalState `json:"decision"`
		Reason   string        `json:"reason"`
	}
	if err := json.Unmarshal(envelope.Body, &body); err != nil {
		t.Fatalf("decode decision request body: %v", err)
	}
	return DecideProposalApprovalRequest{
		IdempotencyKey: envelope.IdempotencyKey,
		ApprovalID:     target.ApprovalID,
		ProposalID:     target.ProposalID,
		Revision:       target.Revision,
		RevisionHash:   target.RevisionHash,
		Decision:       body.Decision,
		Reason:         body.Reason,
	}
}

func createProposalRequestFromFixture(t *testing.T) CreateProposalRequest {
	t.Helper()
	var envelopes struct {
		Create struct {
			Body           json.RawMessage `json:"body"`
			IdempotencyKey string          `json:"idempotency_key"`
		} `json:"create"`
	}
	readP1AJSONFixture(t, "request-envelopes-v1.canonical.json", &envelopes)
	var body struct {
		ProjectID     string        `json:"project_id"`
		WorkstreamID  string        `json:"workstream_id"`
		RevisionInput RevisionInput `json:"revision_input"`
	}
	if err := json.Unmarshal(envelopes.Create.Body, &body); err != nil {
		t.Fatalf("decode create request body: %v", err)
	}
	return CreateProposalRequest{
		IdempotencyKey: envelopes.Create.IdempotencyKey,
		ProjectID:      body.ProjectID,
		WorkstreamID:   body.WorkstreamID,
		RevisionInput:  body.RevisionInput,
	}
}

func readP1AJSONFixture(t *testing.T, name string, target any) {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join("..", "..", "contracts", "p1a", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(bytes, target); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
}
