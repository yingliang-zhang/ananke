Working...
## ACCEPT

- **Durable sealed binding:** `RecordLaunchMaterializationReady` loads the immutable stored launch spec, validates request hash/nonce, then validates an existing persisted materialization against the same sealed contract before idempotent replay. `CreateLaunchRunIntent` performs that same persisted-materialization validation before admission. Recovery validates it before selecting an action and requires the recovered Run intent to name that exact materialization ID.
- **Corruption failure:** `TestP3BFailsClosedOnFKValidSealedMaterializationCorruption` inserts a materialization bound to the real active fence but with valid, wrong sealed hash/nonce. Readiness replay, recovery, and Run-intent admission each return `ErrLaunchRecordCorrupt`; Run-intents remain empty. Mutation paths return before inserts and roll back their transaction.
- **Fence denial / non-mutation:** Same-generation wrong-token and lower-generation vectors reject both readiness and reclaim with `ErrLaunchStaleFence`. Assertions retain the prior active claim and exact counts for claims, head, outbox, materializations, Run intents, terminal intents, and evidence intents.
- **Relational enforcement:** v11 schema uses composite foreign keys for each fenced projection and uniqueness for claim generation, outbox stage, materialization identity/generation, and Run-intent ID/generation. Direct-SQL adversarial tests verify each FK/unique rejection and finish with clean `PRAGMA foreign_key_check`.
- **Scope:** P3b stores modeled facts only. `CreateLaunchRunIntent` does not call `CreateRun`; materialization carries no path/bytes and does not touch a worktree; no process, adapter, or OMP invocation is present. The restart flow asserts the real `runs` table remains empty.

### Verification passed

```text
go test ./internal/store -run '^TestP3B' -count=1 -timeout=90s
go test ./internal/store -count=1 -timeout=180s
go test ./... -count=1 -timeout=180s
go test -race ./internal/store -count=1 -timeout=180s
go test ./internal/store -run '^TestP3BClaimReclamationIsAtomicAcrossStoreHandles$' -count=100 -timeout=180s
```

Contract verifiers and their `--self-test` modes passed for `p1a`, `p1c`, `p2a`, `p2c`, and `p3a`.

No files edited or commits made.
