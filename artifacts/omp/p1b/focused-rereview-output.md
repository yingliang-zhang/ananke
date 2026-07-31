Working...
## CHANGES REQUESTED

The two original blockers are substantially repaired, but the v8 schema still does **not** database-enforce the complete Approval ↔ RevisionLifecycle one-to-one relation.

### Blocker — an Approval can exist without its lifecycle pair

`internal/store/store.go` v8 schema has:

- Approval → Revision composite deferred FK (`400–416`).
- RevisionLifecycle → Revision and full Approval tuple composite deferred FKs (`418–434`).

It lacks the reciprocal deferred composite FK:

```sql
task_proposal_approvals
  (proposal_id, revision, revision_hash, approval_id)
→ task_proposal_revision_lifecycles
  (proposal_id, revision, revision_hash, approval_id)
```

Consequences:

- Raw SQL can insert an Approval bound to a valid Revision without a `RevisionLifecycle`.
- `PRAGMA foreign_key_check` remains clean.
- This violates P1a’s “exactly one Approval … for each Revision” and the durable paired-record model.
- With foreign keys disabled, an existing lifecycle can be changed to another Approval; `GetRevisionLifecycle` rejects it, but `GetApproval` of the orphaned original still succeeds because it only calls `validateRevisionIdentity` (`proposals.go:606`), not the full lifecycle-pair validation.

The current adversarial test demonstrates the hole rather than closing it: it inserts an unattached Revision and then an unattached Approval (`task_proposal_store_test.go:902–907`), and later calls `foreign_key_check` successfully.

**Required repair**

1. Add the reciprocal full-tuple, `DEFERRABLE INITIALLY DEFERRED` Approval → Lifecycle FK in the v8 schema.
2. Rebuild existing v8 databases through a new migration if v8 is already released; do not alter an applied migration.
3. Make `GetApproval` reject an Approval that does not name exactly one matching lifecycle pair, using the existing full-identity validation or equivalent.
4. Add raw-SQL tests proving:
   - orphan Approval insertion fails with FKs enabled;
   - an FK-disabled lifecycle reassignment/orphan causes `GetApproval` to return `ErrProposalRecordCorrupt`;
   - `foreign_key_check` is clean after rejected writes and reports the intended corruption when deliberately bypassed.

### Evidence gap — the claimed v7→v8 test is not a historical v7 fixture

`TestProposalV7DataUpgradesToCompositeIdentityForeignKeys` starts with `Open(dbPath)` at `task_proposal_store_test.go:354`, which migrates the database to v8. It then deletes only the v8 `schema_version` row (`372`) and reruns v8. The table definitions remain v8 throughout.

That exercises the table rebuild/copy path, but it does **not** prove migration from the actual v7 FK definitions that permitted the original cross-link. Build the fixture using historical v7 DDL (or a preserved v7 migration fixture), insert a valid v7 proposal chain, then reopen through `Open` and assert v8 constraints, replay, and `foreign_key_check`.

### Verified repair portions

- `sqliteDSN` sets `_txlock=immediate`, WAL, timeout, and per-connection foreign keys; the cross-`Open` two-handle tests cover same-key create replay, same-base append conflict, competing decisions, and append-vs-rejection.
- Mutation transactions perform durable idempotency lookup before request validation and mutable-state reads.
- Lifecycle → Approval, Proposal current-pointer → Revision, activity → lifecycle, and idempotency response → lifecycle are now full-tuple deferred FKs.
- Disabled-FK corruption tests reject the tested lifecycle, approval-hash, proposal-pointer, activity, and replay corruptions.
- Canonical fixture hashes and fixed policy surface pass; proposal types contain no GUI/IPC, worker, execution, claims, credentials, paths, or model-output fields.

### Executed evidence

Passed:

```text
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test
go test ./internal/store -count=20 -run <canonical,cross-store,v8,FKs,corruption,partial-write suite>
go test ./internal/store -count=1 -timeout 180s
go test -race ./internal/store -count=1 -timeout 180s
go test -v ./internal/store -run '^TestProposalIdentityForeignKeysRejectCrossLinks$'
go test -v ./internal/store -run '^TestProposalV7DataUpgradesToCompositeIdentityForeignKeys$'
```

No files were edited or committed.
