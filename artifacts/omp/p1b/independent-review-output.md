Working...
## Verdict — **CHANGES REQUESTED**

Two contract blockers.

### 1. Cross-handle concurrency returns SQLite errors, not contract outcomes

`Store.Open` limits connections **per `Store` instance** (`internal/store/store.go:32-33`), while each mutation uses a normal `BeginTx` (`proposals.go:200`, `624`, `833`, `1001`). Two independent `Open` calls against the same database are therefore not serialized by that pool limit.

Isolated probes against the actual source showed:

- Same-base append from two `Store` handles: one succeeds; the other returns  
  `insert appended revision: database is locked (5) (SQLITE_BUSY)`  
  instead of required `revision_conflict`.
- Same-key/same-body create from two handles: one succeeds; the other returns  
  `insert proposal: database is locked (5) (SQLITE_BUSY)`  
  instead of replaying the winner’s exact identity.

This violates frozen P1a requirements for per-Proposal linearizability and same-key replay. `busy_timeout` does not make the API return the required semantic result.

A companion probe confirmed the busy loser rolled back: exactly the winner’s rows remained—no partial revision, approval, lifecycle, idempotency, or activity rows. The exposed error is still nonconformant.

**Required repair:** serialize mutation transactions across independent `Store` handles/processes, or retry/restart a conflicted transaction until it can perform the durable idempotency lookup/current-state check and return the specified replay/conflict result. Do not translate `SQLITE_BUSY` directly to a conflict: same-key callers must replay. Add two-`Open`-handle tests for same-base append, competing decision, append-vs-rejection, and same-key create.

### 2. v7 foreign keys do not enforce the durable Revision/Lifecycle/Approval identity

`migrateV7` permits invalid cross-record combinations:

- `task_proposal_approvals` references the Revision `(proposal_id, revision)` but does not bind its `revision_hash` to that Revision.
- `task_proposal_revision_lifecycles` references its Revision tuple and `approval_id` independently, but does not require the Approval to belong to that same tuple/hash.
- Proposal current pointers, activity identities, and idempotency response identities are also not constrained to a specific Revision/Approval tuple.
- `GetRevisionLifecycle` returns the cross-linked row without detecting that mismatch.

An isolated FK probe created two proposals, added an unattached revision/approval for proposal B, then changed proposal A’s lifecycle to reference B’s approval. `PRAGMA foreign_key_check` returned no violations, and `GetRevisionLifecycle(A, 1)` returned the foreign approval ID.

That breaks P1a’s durable one-to-one requirement: `revision_hash` and `approval_id` must each identify exactly the lifecycle `(proposal_id, revision)` tuple.

**Required repair:** encode the full tuple/hash/approval relationship with composite unique keys and composite/deferred FKs, or equivalent database-enforced constraints; ensure Proposal current pointers and durable idempotency/activity response identities cannot point at a different Revision/Approval. Add adversarial FK tests including `foreign_key_check`.

### Minor: redundant v7 indexes

These indexes duplicate SQLite indexes already created by constraints:

- `idx_task_proposal_revisions_hash` duplicates `UNIQUE(revision_hash)`.
- `idx_task_proposal_approvals_revision` duplicates `UNIQUE(proposal_id, revision)`.
- `idx_task_proposal_activity_proposal` duplicates `PRIMARY KEY(proposal_id, sequence)`.

Remove them unless an intentionally different index order is needed.

## Verified behavior

No defect found in the following within a single `Store` handle:

- Exact P1a request-body hashes and fixed operation/resource scope tuples.
- Durable idempotency lookup before validation or mutable-state reads for all four mutations.
- Restart replay for create, append, and decision.
- Create/append/approve atomic transitions and rollback guards.
- Both append-vs-reject linearizations.
- Rejected-predecessor append and rejected-current withdrawal.
- UTF-16 key ordering, control escapes, checked ECMAScript number thresholds (`-0`, `1e-6`, `1e-7`, `1e20`, `1e21`), rounded IEEE-754 decimal, and invalid UTF-8 surrogate-byte rejection in the canonicalizer probe.

No GUI, IPC, Grill/policy execution, claims, worker, process-launch, adapter execution, model execution, or private-field leakage was found. The P1b worktree surface is confined to `internal/store` plus the ledger.

## Executed evidence

Passed:

```text
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test
go test ./internal/store -count=1 -timeout 180s
go test -race ./internal/store -count=1 -timeout 180s
```

Also passed focused canonical-hash, replay/conflict, transition, rejection/withdrawal, and ten-iteration same-handle concurrency tests.

The FK and cross-`Store` probes were isolated temporary compiler overlays against the actual source; they were removed afterward. No repository files were edited or committed.
