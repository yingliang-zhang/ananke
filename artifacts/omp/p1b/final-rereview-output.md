Working...
## CHANGES REQUESTED

One migration-integrity blocker remains.

- `migrateV7` calls `createTaskProposalSchema` (`internal/store/store.go:325–326`).
- That schema is the repaired **v8** shape: it contains composite Revision identity FKs and lifecycle→Approval identity FKs (`store.go:441–518`).
- `migrateV8` calls that same function (`store.go:349`). Therefore applying `migrations[:7]` no longer produces the historical v7 schema that v8 is supposed to repair. This contradicts the source’s “ordered, immutable” migration-history model and makes v7 misversioned.

`TestProposalV7DataUpgradesToCompositeIdentityForeignKeys` is materially better than the prior fake fixture: it reconstructs historical pre-v8 DDL (`task_proposal_store_test.go:451–545`), seeds a valid chain, then verifies replay, v8/v9 advancement, FK enforcement, and clean `foreign_key_check`. It proves the **reconstructed historical DDL → head** upgrade works. It does not prove migration v7 itself is historically preserved, because the test bypasses `migrateV7` and calls its duplicate helper directly.

**Required repair:** split the schema constructors by version. Restore `migrateV7` to an immutable historical-v7 DDL constructor; have `migrateV8` use a distinct v8 constructor; retain the distinct v9 constructor. Make the historical-upgrade test exercise the preserved v7 migration definition or assert byte/DDL equivalence, eliminating the independent duplicate as a drift source.

### Verified repairs

- Original cross-handle blocker: fixed. `_txlock=immediate`, WAL, timeout, and one connection per handle are configured; real two-`Open` barrier tests returned only P1a replay/conflict outcomes.
- Original identity-FK blocker: fixed. Proposal pointers, Approval→Revision, Lifecycle→Revision and Lifecycle→Approval, activity, and idempotency identities use full composite FK targets.
- v9 reciprocal one-to-one: fixed. Approval now has deferred full-tuple FK to RevisionLifecycle; lifecycle has the reciprocal FK; unique keys prevent multiplicity. Raw orphan Approval insertion is rejected.
- `GetApproval` now calls full revision/lifecycle/approval identity validation and rejects the disabled-FK lifecycle reassignment as `ErrProposalRecordCorrupt`.
- Disabled-FK structural-corruption coverage passes for lifecycle, approval, lifecycle reassignment, proposal pointer, activity, and idempotency replay.
- P1a canonical hashes, fixed request hashes/scopes, replay-before-state-check behavior, transition semantics, accepted append-vs-reject linearizations, and no-partial-write assertions are covered by the current tests.
- No GUI, IPC, worker, claim, execution, adapter, model-output, or private-field scope leakage found in the reviewed implementation surface.

### Executed evidence

Passed:

```text
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test

go test -v ./internal/store -run '^(TestProposalV7DataUpgradesToCompositeIdentityForeignKeys|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled)$' -count=1 -timeout=90s

go test ./internal/store -run '^(TestProposalV7DataUpgradesToCompositeIdentityForeignKeys|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled|TestCrossStoreProposalMutationsPreserveP1ASemantics)$' -count=25 -timeout=180s

go test ./internal/store -count=1 -timeout=180s
go test -race ./internal/store -count=1 -timeout=180s
go test ./... -count=1 -timeout=240s
```

No files were edited or committed.
