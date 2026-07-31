Working...
## ACCEPT

The migration-history blocker is repaired.

- `migrations` is ordered v1–v9. `migrateV7` invokes immutable `createTaskProposalSchemaV7`; v8 rebuilds to distinct composite-identity DDL; v9 rebuilds to the reciprocal Approval → RevisionLifecycle full-tuple FK.
- `TestProposalV7DataUpgradesToCompositeIdentityForeignKeys` now applies `migrations[:7]`, asserts the historical v7 FK shapes, seeds a valid v7 Proposal/Revision/Approval/Lifecycle/activity/idempotency chain, closes it, and `Open`s to head. It verifies v8/v9 history, exact create replay, orphan-Approval rejection, and clean `PRAGMA foreign_key_check`.
- No duplicate historical DDL remains in `task_proposal_store_test.go`: neither `CREATE TABLE task_proposal` nor the removed historical-DDL helper is present. The test exercises the real v7 migration instead.

Durable-authority invariants hold.

- Independent handles use one connection each with WAL, foreign keys, busy timeout, and `_txlock=immediate`. Cross-handle tests cover same-key replay, append conflict, decision conflict, and both permitted append-vs-reject linearizations.
- v9 database constraints enforce the full identities: Proposal current pointer → Revision, Approval → Revision and reciprocal Lifecycle, Lifecycle → Revision and Approval, Activity → Lifecycle, and idempotency response → Lifecycle.
- `GetApproval` validates the complete revision/lifecycle/approval identity. Disabled-FK corruption tests reject lifecycle, approval, lifecycle reassignment, Proposal pointer, activity, and idempotency corruption as `ErrProposalRecordCorrupt`.

P1a boundary remains scoped and verified.

- `contracts/p1a/verify.mjs` remains fixture-only, using Node core modules; it validates manifest/JCS bytes, privacy, closed schemas, identity links, hashes, fixed scopes, state transitions, and acceptance inventory.
- Store contract tests consume frozen fixtures and validate canonical revision and request-body hashes. Proposal types retain fixed advisory policy fields and expose no execution, credential, process, worker, IPC, or private runtime fields.

Verification passed:

```text
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test

go test -v ./internal/store \
  -run '^(TestProposalV7DataUpgradesToCompositeIdentityForeignKeys|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled|TestCrossStoreProposalMutationsPreserveP1ASemantics)$' \
  -count=25 -timeout=180s

go test -race ./internal/store -count=1 -timeout=180s
go test ./... -count=1 -timeout=240s
```

No files were edited or committed.
