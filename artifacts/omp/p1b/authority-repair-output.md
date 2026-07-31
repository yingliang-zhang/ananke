Working...
Repaired P1b blockers. No commit or push.

**RED**
- Two independent `Open` handles returned `SQLITE_BUSY` for concurrent create, append, decisions, and append-vs-reject.
- Raw SQL could cross-link lifecycle hashes/approvals, Proposal pointers, activity, and idempotency response identities.
- Read/replay paths accepted deliberately corrupted cross-linked rows when FK enforcement was disabled.

**GREEN**
- `internal/store/store.go`
  - Added SQLite `_txlock=immediate`; mutation transactions now serialize across independent handles/processes.
  - Strengthened fresh v7 proposal schema.
  - Added v8 migration rebuilding historical v7 proposal tables with composite deferred FKs.
  - Removed reviewed redundant v7 indexes.

- `internal/store/proposals.go`
  - Composite identity enforcement binds:
    - Proposal current pointers → own revision tuple.
    - Approval hash → own revision tuple.
    - Lifecycle hash/approval → own revision/approval tuple.
    - Activity/idempotency mutation identities → own lifecycle tuple.
  - Added read/replay validation returning `ErrProposalRecordCorrupt` for corrupted pointer, approval, lifecycle, activity, or idempotency identities.
  - Mutation queries now validate full lifecycle/approval tuples.

- `internal/store/task_proposal_store_test.go`
  - Added real two-`Open` concurrent race tests with exact P1a outcomes and complete journal-count checks for zero partial writes.
  - Added adversarial raw-SQL FK tests plus `PRAGMA foreign_key_check`.
  - Added disabled-FK corruption tests for all read/replay validation paths.
  - Added populated v7→v8 migration regression and redundant-index assertions.

- `docs/experiment-ledger.md`
  - Recorded actual RED/GREEN evidence and verification.

**Verification passed**
```text
go test ./internal/store -run 'TestCrossStoreProposalMutationsPreserveP1ASemantics|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled|TestProposalV7DataUpgradesToCompositeIdentityForeignKeys' -count=1 -timeout=90s
go test ./internal/store -count=1 -timeout=180s
go test -race ./internal/store -count=1 -timeout=180s
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test
```

No GUI, IPC, P2, or P3 surface changed.
