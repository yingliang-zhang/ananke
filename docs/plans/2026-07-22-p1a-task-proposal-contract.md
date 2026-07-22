# P1a Proposal Contract / Fixture TDD Plan

**Goal:** freeze and mechanically verify the Proposal, Revision, and Approval
boundary before storage or execution exists.

**Authorized now:** docs, machine-readable golden fixtures, and a dependency-free
Node verifier under `contracts/p1a/`. **Not authorized:** SQLite proposal
storage, GUI/IPC, claims, workers, adapters, policy execution, commits, pushes.

## Task 1: Freeze canonical fixture inputs

**Files:** `contracts/p1a/fixtures/*.canonical.json`,
`contracts/p1a/fixtures/fixtures.sha256`.

1. Encode exactly one approved Proposal/immutable-Revision/RevisionLifecycle/
   Approval chain, one state transition table, canonical versioned request
   envelopes, and replay/conflict/restart/concurrency acceptance cases.
2. Keep every `.canonical.json` as RFC 8785 JCS bytes: no formatting newline,
   sorted keys, no unpaired Unicode surrogates, and no private or unknown
   fields.
3. Write a versioned SHA-256 manifest over all seven canonical fixture files.

**Acceptance:** a byte change to any golden requires an explicit contract
revision and manifest update; it cannot be silently reformatted.

## Task 2: TDD the scoped verifier

**Files:** `contracts/p1a/verify.mjs`.

1. RED: copy the fixture directory, change only the Revision title while keeping
   valid canonical JSON, then run the verifier against the copy. It must fail
   with `fixture digest mismatch`.
2. GREEN: validate UTF-8/no-BOM/JCS bytes, unpaired-surrogate rejection,
   manifest hashes, snapshot/lifecycle/Approval links, canonical envelope
   hashes and scopes, context-specific closed schemas, privacy fields, fixed
   policy defaults, state table, and acceptance-case inventory.
3. Keep the verifier fixture-only: no package install, database, GUI, daemon,
   model, or network call.

**Commands:**

```sh
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test
```

## Task 3: Specify the first storage RED tests, but do not add storage

When storage is separately authorized, first add focused tests in
`internal/store` that consume these fixtures rather than copying their values:

1. a raw historical-schema fixture built by applying `migrations[:n]`, then
   reopening through `Open`, following `migration_test.go` and
   `migration_v5_test.go` conventions;
2. restart replay tests for create, append, and decision proving the stored
   idempotency response is returned before mutable checks with zero new rows;
3. a successful append from pending proving the parent link, Proposal current
   pointer, former pending lifecycle/Approval supersession, and new pending
   pair are one transaction;
4. a rejected-predecessor append and rejected-current withdrawal proving those
   rejected records remain rejected;
5. a stale expected revision/hash test proving `revision_conflict` with no
   partial row;
6. a same-key/different-body test proving `idempotency_conflict` with no
   partial row.

The RED tests must exist before any migration number, table, or query is added.
P1a does not choose the schema version or create those files.

## Task 4: Specify transactional concurrency RED tests, but do not add runtime

After the storage contract is green, add deterministic concurrent callers for:

- two appends from one expected revision/hash: exactly one commits, one receives
  `revision_conflict`, and the loser creates no partial row;
- simultaneous approve/reject on one pending Approval: exactly one terminal
  decision commits, the other receives `approval_conflict` with no partial row;
- an append racing approval on the same pending Revision: exactly one commits
  and the loser creates no partial row;
- an append racing rejection on that Revision: preserve both frozen
  linearizations—append-first commits once and rejects the decision with no
  write; reject-first commits twice, retains the rejected predecessor, and
  creates a pending successor—with no partial writes in either outcome;
- same-key/same-body retries: every caller receives the original identity,
  never a duplicate proposal/revision/approval or partial row.

Use synchronization barriers; do not use sleeps as a concurrency oracle. The
future implementation must preserve these outcomes across a close/reopen.

## Task 5: Defer all execution capabilities

A later, separately authorized slice may add only the frozen first extensions:
`deadline` plus `attempt_cap` budgets, then a read-only OMP audit adapter.
Neither can make the advisory model authoritative or bypass a local-operator
Approval. Add contract fixtures and RED tests before adding fields or behavior.

## Final P1a gate

Run both Node commands above, check the canonical fixture files are valid JSON,
and update the experiment ledger only with those observed outputs. Do not claim
storage, GUI, model, adapter, restart, or concurrency execution coverage from
the fixture verifier.