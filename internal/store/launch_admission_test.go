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
	"strings"
	"sync"
	"testing"
)

type p3aLaunchAdmissionFixture struct {
	Admission struct {
		ApprovalEligibility LaunchApprovalEligibility `json:"approval_eligibility"`
		LaunchSpec          LaunchSpec                `json:"launch_spec"`
		LaunchSpecHash      string                    `json:"launch_spec_hash"`
	} `json:"admission"`
	LaunchOutbox    json.RawMessage `json:"launch_outbox"`
	Materialization json.RawMessage `json:"materialization"`
	Run             json.RawMessage `json:"run"`
	SchemaVersion   string          `json:"schema_version"`
	TaskClaim       json.RawMessage `json:"task_claim"`
	TokenFenceCases json.RawMessage `json:"token_fence_cases"`
}

func TestP3BLaunchSpecHashPreservesP3AFixtureParity(t *testing.T) {
	fixture := readP3ALaunchAdmissionFixture(t)
	got, err := HashLaunchSpec(fixture.Admission.LaunchSpec)
	if err != nil {
		t.Fatalf("HashLaunchSpec: %v", err)
	}
	if got != fixture.Admission.LaunchSpecHash {
		t.Fatalf("P3a launch spec hash = %q, want %q", got, fixture.Admission.LaunchSpecHash)
	}
}
func TestP3BMigratesP2HeadToFencedLaunchAuthority(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "p2-head.sqlite")
	rawDB, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open P2 head fixture: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	if _, err := rawDB.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema history: %v", err)
	}
	for _, migration := range migrations[:10] {
		tx, err := rawDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin P2 migration v%d: %v", migration.version, err)
		}
		if err := migration.up(ctx, tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply P2 migration v%d: %v", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`, migration.version, nowStamp()); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record P2 migration v%d: %v", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit P2 migration v%d: %v", migration.version, err)
		}
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close P2 head fixture: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open P2 head fixture: %v", err)
	}
	defer s.Close()
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("schema version = %d, want %d", version, len(migrations))
	}
	for _, table := range []string{
		"launch_specs", "task_claims", "launch_claim_heads", "launch_materializations",
		"launch_admission_outbox", "launch_run_intents", "launch_run_state_facts",
		"launch_terminal_intents", "launch_evidence_intents",
	} {
		var count int
		if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("look up %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("P3b table %s missing after migration", table)
		}
	}
	rows, err := s.DB().QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign key check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("P3b migration foreign_key_check reported a violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign key check rows: %v", err)
	}
}

func TestP3BStoresImmutableEligibleLaunchSpec(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	request, eligibility := approvedLaunchAdmissionRequest(t, s)

	stored, err := s.StoreLaunchSpec(ctx, request)
	if err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}
	if stored.LaunchSpecHash != request.LaunchSpecHash || stored.Approval != eligibility {
		t.Fatalf("stored launch admission = %+v, want hash %q and eligibility %+v", stored, request.LaunchSpecHash, eligibility)
	}
	if !reflect.DeepEqual(stored.Spec, request.Spec) {
		t.Fatalf("stored launch spec differs from immutable request")
	}

	replayed, err := s.StoreLaunchSpec(ctx, request)
	if err != nil {
		t.Fatalf("StoreLaunchSpec replay: %v", err)
	}
	if !reflect.DeepEqual(replayed, stored) {
		t.Fatalf("StoreLaunchSpec replay = %+v, want %+v", replayed, stored)
	}

	loaded, err := s.GetLaunchSpec(ctx, request.LaunchSpecHash)
	if err != nil {
		t.Fatalf("GetLaunchSpec: %v", err)
	}
	if !reflect.DeepEqual(loaded, stored) {
		t.Fatalf("GetLaunchSpec = %+v, want %+v", loaded, stored)
	}

	if _, err := s.DB().ExecContext(ctx, `UPDATE launch_specs SET created_at = ? WHERE launch_spec_hash = ?`, nowStamp(), request.LaunchSpecHash); err == nil {
		t.Fatal("immutable launch spec update unexpectedly succeeded")
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM launch_specs WHERE launch_spec_hash = ?`, request.LaunchSpecHash); err == nil {
		t.Fatal("immutable launch spec delete unexpectedly succeeded")
	}
}

func TestP3BRejectsIneligibleP1ApprovalWithoutLaunchRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	request, _ := approvedLaunchAdmissionRequest(t, s)

	for _, mutate := range []struct {
		name  string
		apply func(*LaunchAdmissionRequest)
	}{
		{
			name: "approval identity",
			apply: func(request *LaunchAdmissionRequest) {
				request.ApprovalID = "approval_other_001"
			},
		},
		{
			name: "revision hash",
			apply: func(request *LaunchAdmissionRequest) {
				request.Spec.Revision.RevisionHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				request.LaunchSpecHash = mustHashLaunchSpec(t, request.Spec)
			},
		},
		{
			name: "hash binding",
			apply: func(request *LaunchAdmissionRequest) {
				request.LaunchSpecHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			},
		},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			candidate := request
			mutate.apply(&candidate)
			if _, err := s.StoreLaunchSpec(ctx, candidate); !errors.Is(err, ErrLaunchApprovalIneligible) && !errors.Is(err, ErrLaunchSpecHashMismatch) {
				t.Fatalf("StoreLaunchSpec(%s) error = %v, want ineligible approval or hash mismatch", mutate.name, err)
			}
			assertP3BLaunchTableCount(t, s, "launch_specs", 0)
			assertP3BLaunchTableCount(t, s, "task_claims", 0)
			assertP3BLaunchTableCount(t, s, "launch_admission_outbox", 0)
		})
	}
}

func TestP3BClaimMaterializationRunIntentAndRecoveryBoundariesSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "p3b.sqlite")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	request, _ := approvedLaunchAdmissionRequest(t, s)
	if _, err := s.StoreLaunchSpec(ctx, request); err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}

	claim, err := s.AcquireLaunchClaim(ctx, LaunchClaimRequest{
		LaunchSpecHash: request.LaunchSpecHash,
		ClaimID:        "claim_p3b_001",
		ClaimTokenHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		OwnerID:        "launch_admitter",
		Attempt:        1,
	})
	if err != nil {
		t.Fatalf("AcquireLaunchClaim: %v", err)
	}
	if claim.FenceGeneration != 1 || claim.State != TaskClaimStateActive {
		t.Fatalf("claim = %+v, want active generation 1", claim)
	}
	boundary, err := s.GetLaunchRecoveryBoundary(ctx, request.LaunchSpecHash)
	if err != nil {
		t.Fatalf("claim recovery boundary: %v", err)
	}
	assertP3BRecoveryBoundary(t, boundary, LaunchOutboxPendingMaterialization, LaunchRecoveryRetryMaterialization, nil, nil)

	if err := s.Close(); err != nil {
		t.Fatalf("close before materialization: %v", err)
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatalf("reopen before materialization: %v", err)
	}
	defer s.Close()

	materialization, err := s.RecordLaunchMaterializationReady(ctx, LaunchMaterializationRequest{
		Fence:               claim.Fence,
		MaterializationID:   "materialization_p3b_001",
		MaterializationHash: request.Spec.SealedContract.MaterializationHash,
		Nonce:               request.Spec.SealedContract.Nonce,
	})
	if err != nil {
		t.Fatalf("RecordLaunchMaterializationReady: %v", err)
	}
	boundary, err = s.GetLaunchRecoveryBoundary(ctx, request.LaunchSpecHash)
	if err != nil {
		t.Fatalf("materialization recovery boundary: %v", err)
	}
	assertP3BRecoveryBoundary(t, boundary, LaunchOutboxPendingRunAdmission, LaunchRecoveryRetryRunAdmission, &materialization, nil)

	run, err := s.CreateLaunchRunIntent(ctx, LaunchRunIntentRequest{
		Fence:             claim.Fence,
		RunID:             "run_p3b_001",
		MaterializationID: materialization.MaterializationID,
		Attempt:           claim.Attempt,
	})
	if err != nil {
		t.Fatalf("CreateLaunchRunIntent: %v", err)
	}
	if run.StateFact.Kind != LaunchStateFactCreated || run.StateFact.Sequence != 1 || run.StateFact.TokenHash != claim.ClaimTokenHash {
		t.Fatalf("run intent state fact = %+v, want current-token created fact", run.StateFact)
	}
	boundary, err = s.GetLaunchRecoveryBoundary(ctx, request.LaunchSpecHash)
	if err != nil {
		t.Fatalf("run recovery boundary: %v", err)
	}
	assertP3BRecoveryBoundary(t, boundary, LaunchOutboxPendingProcessAdmission, LaunchRecoveryRetryProcessAdmission, &materialization, &run)
	if boundary.TerminalIntent != nil || boundary.EvidenceIntent != nil {
		t.Fatalf("recovery inferred terminal/evidence intent: %+v", boundary)
	}
	assertP3BLaunchTableCount(t, s, "runs", 0)
}

func TestP3BFenceRejectsSameGenerationWrongTokenAndLowerGeneration(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	request, _ := approvedLaunchAdmissionRequest(t, s)
	if _, err := s.StoreLaunchSpec(ctx, request); err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}
	claim := mustAcquireP3BClaim(t, s, request.LaunchSpecHash, "claim_p3b_001", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	materialization, err := s.RecordLaunchMaterializationReady(ctx, LaunchMaterializationRequest{
		Fence:               claim.Fence,
		MaterializationID:   "materialization_p3b_001",
		MaterializationHash: request.Spec.SealedContract.MaterializationHash,
		Nonce:               request.Spec.SealedContract.Nonce,
	})
	if err != nil {
		t.Fatalf("RecordLaunchMaterializationReady: %v", err)
	}
	active, err := s.ReclaimLaunchClaim(ctx, LaunchClaimReclaimRequest{
		ExpectedFence: claim.Fence,
		Claim: LaunchClaimRequest{
			LaunchSpecHash: request.LaunchSpecHash,
			ClaimID:        "claim_p3b_002",
			ClaimTokenHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			OwnerID:        "launch_admitter",
			Attempt:        2,
		},
	})
	if err != nil {
		t.Fatalf("ReclaimLaunchClaim: %v", err)
	}
	if active.FenceGeneration != 2 {
		t.Fatalf("reclaimed generation = %d, want 2", active.FenceGeneration)
	}

	sameGenerationWrongToken := active.Fence
	sameGenerationWrongToken.ClaimTokenHash = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	lowerGeneration := active.Fence
	lowerGeneration.FenceGeneration--
	for _, stale := range []struct {
		name  string
		fence LaunchFence
	}{
		{name: "same generation wrong token", fence: sameGenerationWrongToken},
		{name: "lower generation", fence: lowerGeneration},
	} {
		t.Run(stale.name, func(t *testing.T) {
			if _, err := s.CreateLaunchRunIntent(ctx, LaunchRunIntentRequest{
				Fence: stale.fence, RunID: "run_stale_" + compactP3BTestName(stale.name), MaterializationID: materialization.MaterializationID, Attempt: active.Attempt,
			}); !errors.Is(err, ErrLaunchStaleFence) {
				t.Fatalf("CreateLaunchRunIntent stale error = %v, want %v", err, ErrLaunchStaleFence)
			}
			if _, err := s.AppendLaunchTerminalIntent(ctx, LaunchTerminalIntentRequest{
				Fence: stale.fence, RunID: "run_p3b_001", IntentID: "terminal_" + compactP3BTestName(stale.name),
			}); !errors.Is(err, ErrLaunchStaleFence) {
				t.Fatalf("AppendLaunchTerminalIntent stale error = %v, want %v", err, ErrLaunchStaleFence)
			}
			if _, err := s.SettleLaunchEvidenceIntent(ctx, LaunchEvidenceIntentRequest{
				Fence: stale.fence, RunID: "run_p3b_001", IntentID: "evidence_" + compactP3BTestName(stale.name),
			}); !errors.Is(err, ErrLaunchStaleFence) {
				t.Fatalf("SettleLaunchEvidenceIntent stale error = %v, want %v", err, ErrLaunchStaleFence)
			}
			if _, err := s.RecordLaunchMaterializationReady(ctx, LaunchMaterializationRequest{
				Fence:               stale.fence,
				MaterializationID:   "materialization_stale_" + compactP3BTestName(stale.name),
				MaterializationHash: request.Spec.SealedContract.MaterializationHash,
				Nonce:               request.Spec.SealedContract.Nonce,
			}); !errors.Is(err, ErrLaunchStaleFence) {
				t.Fatalf("RecordLaunchMaterializationReady stale error = %v, want %v", err, ErrLaunchStaleFence)
			}
			if _, err := s.ReclaimLaunchClaim(ctx, LaunchClaimReclaimRequest{
				ExpectedFence: stale.fence,
				Claim: LaunchClaimRequest{
					LaunchSpecHash: request.LaunchSpecHash,
					ClaimID:        "claim_reclaim_" + compactP3BTestName(stale.name),
					ClaimTokenHash: "sha256:4444444444444444444444444444444444444444444444444444444444444444",
					OwnerID:        "launch_admitter",
					Attempt:        3,
				},
			}); !errors.Is(err, ErrLaunchStaleFence) {
				t.Fatalf("ReclaimLaunchClaim stale error = %v, want %v", err, ErrLaunchStaleFence)
			}
			gotActive, err := s.GetLaunchClaim(ctx, request.LaunchSpecHash)
			if err != nil {
				t.Fatalf("GetLaunchClaim after stale writes: %v", err)
			}
			if !reflect.DeepEqual(gotActive, active) {
				t.Fatalf("active claim mutated by stale writes = %+v, want %+v", gotActive, active)
			}
			assertP3BLaunchTableCount(t, s, "launch_materializations", 1)
			assertP3BLaunchTableCount(t, s, "task_claims", 2)
			assertP3BLaunchTableCount(t, s, "launch_admission_outbox", 3)
			assertP3BLaunchTableCount(t, s, "launch_claim_heads", 1)
			assertP3BLaunchTableCount(t, s, "launch_run_intents", 0)
			assertP3BLaunchTableCount(t, s, "launch_terminal_intents", 0)
			assertP3BLaunchTableCount(t, s, "launch_evidence_intents", 0)
		})
	}
}

func TestP3BClaimReclamationIsAtomicAcrossStoreHandles(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "concurrent.sqlite")
	first, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	defer first.Close()
	request, _ := approvedLaunchAdmissionRequest(t, first)
	if _, err := first.StoreLaunchSpec(ctx, request); err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}
	initial := mustAcquireP3BClaim(t, first, request.LaunchSpecHash, "claim_p3b_001", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	second, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	defer second.Close()

	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for index, store := range []*Store{first, second} {
		group.Add(1)
		go func(index int, store *Store) {
			defer group.Done()
			<-start
			_, err := store.ReclaimLaunchClaim(ctx, LaunchClaimReclaimRequest{
				ExpectedFence: initial.Fence,
				Claim: LaunchClaimRequest{
					LaunchSpecHash: request.LaunchSpecHash,
					ClaimID:        "claim_p3b_00" + string(rune('2'+index)),
					ClaimTokenHash: "sha256:" + string(bytes.Repeat([]byte{byte('2' + index)}, 64)),
					OwnerID:        "launch_admitter",
					Attempt:        2,
				},
			})
			results <- err
		}(index, store)
	}
	close(start)
	group.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrLaunchStaleFence) {
			t.Fatalf("concurrent reclaim error = %v, want stale fence", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent reclaims succeeded %d times, want 1", successes)
	}
	current, err := first.GetLaunchClaim(ctx, request.LaunchSpecHash)
	if err != nil {
		t.Fatalf("GetLaunchClaim: %v", err)
	}
	if current.FenceGeneration != 2 {
		t.Fatalf("current generation = %d, want 2", current.FenceGeneration)
	}
	assertP3BLaunchTableCount(t, first, "task_claims", 2)
}

func TestP3BRejectsCorruptP1AndLaunchAuthorityVectors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	request, eligibility := approvedLaunchAdmissionRequest(t, s)

	if _, err := s.DB().ExecContext(ctx, `UPDATE task_proposal_approvals SET decided_by = 'other_actor' WHERE approval_id = ?`, eligibility.ApprovalID); err != nil {
		t.Fatalf("corrupt approval eligibility: %v", err)
	}
	if _, err := s.StoreLaunchSpec(ctx, request); !errors.Is(err, ErrLaunchApprovalIneligible) {
		t.Fatalf("StoreLaunchSpec corrupt P1 approval error = %v, want %v", err, ErrLaunchApprovalIneligible)
	}
	assertP3BLaunchTableCount(t, s, "launch_specs", 0)

	if _, err := s.DB().ExecContext(ctx, `UPDATE task_proposal_approvals SET decided_by = ? WHERE approval_id = ?`, localGUIOperator, eligibility.ApprovalID); err != nil {
		t.Fatalf("repair approval eligibility: %v", err)
	}
	if _, err := s.StoreLaunchSpec(ctx, request); err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}
	claim := mustAcquireP3BClaim(t, s, request.LaunchSpecHash, "claim_p3b_001", "sha256:1111111111111111111111111111111111111111111111111111111111111111")

	if _, err := s.DB().ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE launch_claim_heads SET claim_token_hash = ? WHERE launch_spec_hash = ?`, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", request.LaunchSpecHash); err != nil {
		t.Fatalf("corrupt active claim head: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("re-enable foreign keys: %v", err)
	}
	if _, err := s.GetLaunchClaim(ctx, request.LaunchSpecHash); !errors.Is(err, ErrLaunchRecordCorrupt) {
		t.Fatalf("GetLaunchClaim corrupt head error = %v, want %v", err, ErrLaunchRecordCorrupt)
	}
	results, err := s.ListLaunchRecoveryBoundaries(ctx)
	if err != nil {
		t.Fatalf("ListLaunchRecoveryBoundaries corrupt head error = %v, want isolated result", err)
	}
	if len(results) != 1 || results[0].LaunchSpecHash != request.LaunchSpecHash || results[0].Boundary != nil || !errors.Is(results[0].Cause, ErrLaunchRecordCorrupt) {
		t.Fatalf("ListLaunchRecoveryBoundaries corrupt head result = %+v, want exact corrupt result", results)
	}
	if _, err := s.RecordLaunchMaterializationReady(ctx, LaunchMaterializationRequest{
		Fence:               claim.Fence,
		MaterializationID:   "materialization_p3b_001",
		MaterializationHash: request.Spec.SealedContract.MaterializationHash,
		Nonce:               request.Spec.SealedContract.Nonce,
	}); !errors.Is(err, ErrLaunchRecordCorrupt) {
		t.Fatalf("RecordLaunchMaterializationReady corrupt head error = %v, want %v", err, ErrLaunchRecordCorrupt)
	}
}

func TestP3BAggregateRecoveryPreservesValidAndCorruptActiveBoundaries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	validRequest, _ := approvedLaunchAdmissionRequest(t, s)
	corruptRequest := approvedP3BAggregateAdmissionRequest(t, s)
	if _, err := s.StoreLaunchSpec(ctx, validRequest); err != nil {
		t.Fatalf("store valid launch spec: %v", err)
	}
	validClaim := mustAcquireP3BClaim(t, s, validRequest.LaunchSpecHash, "claim_p3b_valid", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	if _, err := s.StoreLaunchSpec(ctx, corruptRequest); err != nil {
		t.Fatalf("store corrupt launch spec: %v", err)
	}
	corruptClaim := mustAcquireP3BClaim(t, s, corruptRequest.LaunchSpecHash, "claim_p3b_corrupt", "sha256:2222222222222222222222222222222222222222222222222222222222222222")
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO launch_materializations
		(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "materialization_p3b_aggregate_corrupt", corruptClaim.LaunchSpecHash, corruptClaim.ClaimID, corruptClaim.ClaimTokenHash, corruptClaim.FenceGeneration,
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "nonce:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", LaunchMaterializationStateReady, nowStamp()); err != nil {
		t.Fatalf("seed corrupt materialization: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO launch_admission_outbox
		(outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "outbox_p3b_aggregate_corrupt", corruptClaim.LaunchSpecHash, corruptClaim.ClaimID, corruptClaim.ClaimTokenHash, corruptClaim.FenceGeneration, 2, LaunchOutboxPendingRunAdmission, nowStamp()); err != nil {
		t.Fatalf("seed corrupt outbox: %v", err)
	}

	results, err := s.ListLaunchRecoveryBoundaries(ctx)
	if err != nil {
		t.Fatalf("aggregate recovery boundaries: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("aggregate recovery result count = %d, want 2", len(results))
	}
	byHash := make(map[string]LaunchRecoveryResult, len(results))
	for _, result := range results {
		byHash[result.LaunchSpecHash] = result
	}
	valid, found := byHash[validRequest.LaunchSpecHash]
	if !found || valid.Cause != nil || valid.Boundary == nil || valid.Boundary.LaunchSpecHash != validRequest.LaunchSpecHash || valid.Boundary.Claim != validClaim || valid.Boundary.Action != LaunchRecoveryRetryMaterialization {
		t.Fatalf("valid aggregate recovery result = %+v, want exact materialization retry", valid)
	}
	corrupt, found := byHash[corruptRequest.LaunchSpecHash]
	if !found || corrupt.Boundary != nil || !errors.Is(corrupt.Cause, ErrLaunchRecordCorrupt) {
		t.Fatalf("corrupt aggregate recovery result = %+v, want exact hash and corrupt cause", corrupt)
	}
	assertP3BLaunchTableCount(t, s, "runs", 0)
}
func TestP3BFailsClosedOnFKValidSealedMaterializationCorruption(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	request, _ := approvedLaunchAdmissionRequest(t, s)
	if _, err := s.StoreLaunchSpec(ctx, request); err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}
	claim := mustAcquireP3BClaim(t, s, request.LaunchSpecHash, "claim_p3b_001", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	corruptMaterializationID := "materialization_p3b_corrupt"
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO launch_materializations
		(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, corruptMaterializationID, claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration,
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "nonce:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", LaunchMaterializationStateReady, nowStamp()); err != nil {
		t.Fatalf("insert FK-valid corrupt materialization: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO launch_admission_outbox
		(outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "outbox_p3b_corrupt", claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration, 2, LaunchOutboxPendingRunAdmission, nowStamp()); err != nil {
		t.Fatalf("insert FK-valid corrupt outbox: %v", err)
	}

	if _, err := s.RecordLaunchMaterializationReady(ctx, LaunchMaterializationRequest{
		Fence:               claim.Fence,
		MaterializationID:   corruptMaterializationID,
		MaterializationHash: request.Spec.SealedContract.MaterializationHash,
		Nonce:               request.Spec.SealedContract.Nonce,
	}); !errors.Is(err, ErrLaunchRecordCorrupt) {
		t.Fatalf("RecordLaunchMaterializationReady corrupt reload error = %v, want %v", err, ErrLaunchRecordCorrupt)
	}
	if _, err := s.GetLaunchRecoveryBoundary(ctx, request.LaunchSpecHash); !errors.Is(err, ErrLaunchRecordCorrupt) {
		t.Fatalf("GetLaunchRecoveryBoundary corrupt materialization error = %v, want %v", err, ErrLaunchRecordCorrupt)
	}
	if _, err := s.CreateLaunchRunIntent(ctx, LaunchRunIntentRequest{
		Fence:             claim.Fence,
		RunID:             "run_p3b_corrupt",
		MaterializationID: corruptMaterializationID,
		Attempt:           claim.Attempt,
	}); !errors.Is(err, ErrLaunchRecordCorrupt) {
		t.Fatalf("CreateLaunchRunIntent corrupt materialization error = %v, want %v", err, ErrLaunchRecordCorrupt)
	}
	assertP3BLaunchTableCount(t, s, "launch_run_intents", 0)
}

func TestP3BMigrationEnforcesFencedForeignKeysAndUniqueIdentities(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	request, _ := approvedLaunchAdmissionRequest(t, s)
	if _, err := s.StoreLaunchSpec(ctx, request); err != nil {
		t.Fatalf("StoreLaunchSpec: %v", err)
	}
	claim := mustAcquireP3BClaim(t, s, request.LaunchSpecHash, "claim_p3b_001", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	now := nowStamp()
	forgedToken := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	execForeignKeyFailure(t, s, `INSERT INTO launch_materializations
		(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "materialization_p3b_forged", claim.LaunchSpecHash, claim.ClaimID, forgedToken, claim.FenceGeneration,
		request.Spec.SealedContract.MaterializationHash, request.Spec.SealedContract.Nonce, LaunchMaterializationStateReady, now)
	execForeignKeyFailure(t, s, `INSERT INTO launch_admission_outbox
		(outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "outbox_p3b_forged", claim.LaunchSpecHash, claim.ClaimID, forgedToken, claim.FenceGeneration, 2, LaunchOutboxPendingRunAdmission, now)
	execForeignKeyFailure(t, s, `INSERT INTO launch_run_intents
		(run_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_id, attempt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "run_p3b_forged", claim.LaunchSpecHash, claim.ClaimID, forgedToken, claim.FenceGeneration, "materialization_p3b_missing", claim.Attempt, now)

	assertP3BUniqueFailure(t, execP3BStatement(ctx, s, `INSERT INTO task_claims
		(claim_id, launch_spec_hash, claim_token_hash, fence_generation, owner_id, attempt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "claim_p3b_duplicate", claim.LaunchSpecHash,
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", claim.FenceGeneration, claim.OwnerID, claim.Attempt, now), "duplicate claim generation")
	assertP3BUniqueFailure(t, execP3BStatement(ctx, s, `INSERT INTO launch_admission_outbox
		(outbox_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, sequence, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "outbox_p3b_duplicate", claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration, 1, LaunchOutboxPendingMaterialization, now), "duplicate outbox stage")

	materialization, err := s.RecordLaunchMaterializationReady(ctx, LaunchMaterializationRequest{
		Fence:               claim.Fence,
		MaterializationID:   "materialization_p3b_001",
		MaterializationHash: request.Spec.SealedContract.MaterializationHash,
		Nonce:               request.Spec.SealedContract.Nonce,
	})
	if err != nil {
		t.Fatalf("RecordLaunchMaterializationReady: %v", err)
	}
	assertP3BUniqueFailure(t, execP3BStatement(ctx, s, `INSERT INTO launch_materializations
		(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, materialization.MaterializationID, claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration,
		materialization.MaterializationHash, materialization.Nonce, materialization.State, now), "duplicate materialization identity")
	assertP3BUniqueFailure(t, execP3BStatement(ctx, s, `INSERT INTO launch_materializations
		(materialization_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_hash, nonce, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "materialization_p3b_duplicate", claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration,
		materialization.MaterializationHash, materialization.Nonce, materialization.State, now), "duplicate materialization generation")

	run, err := s.CreateLaunchRunIntent(ctx, LaunchRunIntentRequest{
		Fence:             claim.Fence,
		RunID:             "run_p3b_001",
		MaterializationID: materialization.MaterializationID,
		Attempt:           claim.Attempt,
	})
	if err != nil {
		t.Fatalf("CreateLaunchRunIntent: %v", err)
	}
	assertP3BUniqueFailure(t, execP3BStatement(ctx, s, `INSERT INTO launch_run_intents
		(run_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_id, attempt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, run.RunID, claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration, materialization.MaterializationID, claim.Attempt, now), "duplicate Run-intent ID")
	assertP3BUniqueFailure(t, execP3BStatement(ctx, s, `INSERT INTO launch_run_intents
		(run_id, launch_spec_hash, claim_id, claim_token_hash, fence_generation, materialization_id, attempt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "run_p3b_duplicate", claim.LaunchSpecHash, claim.ClaimID, claim.ClaimTokenHash, claim.FenceGeneration, materialization.MaterializationID, claim.Attempt, now), "duplicate Run-intent generation")
	assertForeignKeyCheckClean(t, s)
}

func approvedLaunchAdmissionRequest(t *testing.T, s *Store) (LaunchAdmissionRequest, LaunchApprovalEligibility) {
	t.Helper()
	ctx := context.Background()
	created, err := s.CreateProposal(ctx, createProposalRequestFromFixture(t))
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	decision := decisionProposalRequestFromFixture(t, created, ApprovalStateApproved)
	if _, err := s.DecideProposalApproval(ctx, decision); err != nil {
		t.Fatalf("DecideProposalApproval: %v", err)
	}
	approval, err := s.GetApproval(ctx, created.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if approval.DecidedAt == nil || approval.DecidedBy == nil {
		t.Fatal("approved P1 approval is missing decision identity")
	}
	fixture := readP3ALaunchAdmissionFixture(t)
	spec := fixture.Admission.LaunchSpec
	spec.Revision = LaunchRevisionIdentity{ProposalID: created.ProposalID, Revision: created.Revision, RevisionHash: created.RevisionHash}
	request := LaunchAdmissionRequest{
		Spec:           spec,
		LaunchSpecHash: mustHashLaunchSpec(t, spec),
		ApprovalID:     created.ApprovalID,
	}
	return request, LaunchApprovalEligibility{
		ApprovalID:   created.ApprovalID,
		ProposalID:   created.ProposalID,
		Revision:     created.Revision,
		RevisionHash: created.RevisionHash,
		ApprovedAt:   *approval.DecidedAt,
		ApprovedBy:   *approval.DecidedBy,
		State:        ApprovalStateApproved,
	}
}

func approvedP3BAggregateAdmissionRequest(t *testing.T, s *Store) LaunchAdmissionRequest {
	t.Helper()
	ctx := context.Background()
	create := createProposalRequestFromFixture(t)
	create.IdempotencyKey = "create_p3b_aggregate_002"
	created, err := s.CreateProposal(ctx, create)
	if err != nil {
		t.Fatalf("create alternate P1 proposal: %v", err)
	}
	decision := decisionProposalRequestFromFixture(t, created, ApprovalStateApproved)
	decision.IdempotencyKey = "approve_p3b_aggregate_002"
	if _, err := s.DecideProposalApproval(ctx, decision); err != nil {
		t.Fatalf("approve alternate P1 proposal: %v", err)
	}
	fixture := readP3ALaunchAdmissionFixture(t)
	spec := fixture.Admission.LaunchSpec
	spec.Revision = LaunchRevisionIdentity{ProposalID: created.ProposalID, Revision: created.Revision, RevisionHash: created.RevisionHash}
	return LaunchAdmissionRequest{
		Spec:           spec,
		LaunchSpecHash: mustHashLaunchSpec(t, spec),
		ApprovalID:     created.ApprovalID,
	}
}

func mustAcquireP3BClaim(t *testing.T, s *Store, launchSpecHash, claimID, tokenHash string) TaskClaim {
	t.Helper()
	claim, err := s.AcquireLaunchClaim(context.Background(), LaunchClaimRequest{
		LaunchSpecHash: launchSpecHash,
		ClaimID:        claimID,
		ClaimTokenHash: tokenHash,
		OwnerID:        "launch_admitter",
		Attempt:        1,
	})
	if err != nil {
		t.Fatalf("AcquireLaunchClaim: %v", err)
	}
	return claim
}

func assertP3BRecoveryBoundary(t *testing.T, boundary LaunchRecoveryBoundary, outboxState LaunchOutboxState, action LaunchRecoveryAction, materialization *LaunchMaterialization, run *LaunchRunIntent) {
	t.Helper()
	if boundary.Outbox.State != outboxState || boundary.Action != action {
		t.Fatalf("recovery boundary state/action = %q/%q, want %q/%q", boundary.Outbox.State, boundary.Action, outboxState, action)
	}
	if materialization == nil {
		if boundary.Materialization != nil {
			t.Fatalf("recovery materialization = %+v, want absent", boundary.Materialization)
		}
	} else if boundary.Materialization == nil || *boundary.Materialization != *materialization {
		t.Fatalf("recovery materialization = %+v, want %+v", boundary.Materialization, materialization)
	}
	if run == nil {
		if boundary.RunIntent != nil {
			t.Fatalf("recovery run intent = %+v, want absent", boundary.RunIntent)
		}
	} else if boundary.RunIntent == nil || *boundary.RunIntent != *run {
		t.Fatalf("recovery run intent = %+v, want %+v", boundary.RunIntent, run)
	}
}

func assertP3BLaunchTableCount(t *testing.T, s *Store, table string, want int) {
	t.Helper()
	var got int
	if err := s.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}

func execP3BStatement(ctx context.Context, s *Store, statement string, args ...any) error {
	_, err := s.DB().ExecContext(ctx, statement, args...)
	return err
}

func assertP3BUniqueFailure(t *testing.T, err error, identity string) {
	t.Helper()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
		t.Fatalf("%s error = %v, want unique constraint failure", identity, err)
	}
}
func mustHashLaunchSpec(t *testing.T, spec LaunchSpec) string {
	t.Helper()
	hash, err := HashLaunchSpec(spec)
	if err != nil {
		t.Fatalf("HashLaunchSpec: %v", err)
	}
	return hash
}

func compactP3BTestName(value string) string {
	if value == "same generation wrong token" {
		return "same_token"
	}
	return "lower_generation"
}

func readP3ALaunchAdmissionFixture(t *testing.T) p3aLaunchAdmissionFixture {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "contracts", "p3a", "fixtures", "launch-admission-v1.canonical.json"))
	if err != nil {
		t.Fatalf("read P3a fixture: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()

	var fixture p3aLaunchAdmissionFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode P3a fixture: %v", err)
	}
	return fixture
}
