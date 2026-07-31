Working...
Implemented P1b focused-review repairs.

- Added immutable **migration v9** in `internal/store/store.go`.
  - Rebuilds Proposal journal tables from v8.
  - Adds reciprocal deferred full-tuple FK:
    `Approval(proposal_id, revision, revision_hash, approval_id) → RevisionLifecycle(...)`.
  - Leaves v8 migration definition unchanged.

- Updated `GetApproval` in `internal/store/proposals.go` to validate the complete lifecycle/revision/approval identity pair and return `ErrProposalRecordCorrupt` for reassigned or orphaned records.

- Replaced the fake v7→v8 fixture with historical pre-v8 table DDL in `internal/store/task_proposal_store_test.go`.
  - Seeds a valid v7 chain.
  - Verifies `Open` applies both v8 and v9, retains replay identity, preserves data, exposes the reciprocal FK, and leaves `foreign_key_check` clean.
  - Covers FK-enabled orphan-Approval rejection.
  - Covers FK-disabled lifecycle reassignment causing `GetApproval` corruption rejection and `foreign_key_check` detection.

- Recorded factual RED/GREEN and verification evidence in `docs/experiment-ledger.md`.

Verification observed:

```text
RED:
go test ./internal/store -run 'TestProposalV7DataUpgradesToCompositeIdentityForeignKeys|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled' -count=1
# exited 1 before the repair: v8 head, orphan insert accepted, GetApproval accepted reassignment

GREEN:
go test ./internal/store -run 'TestProposalV7DataUpgradesToCompositeIdentityForeignKeys|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled' -count=1
go test ./internal/store -count=1 -timeout=180s
go test -race ./internal/store -count=1 -timeout=180s
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test
```

All final commands passed. No P2/P3/UI work, commits, or pushes performed.
