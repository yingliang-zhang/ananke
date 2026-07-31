# P1b durabP1b implementation complete. Report written to:

- `artifacts/omp/p1b/store-implementation-output.md`

Implemented:

- SQLite migration v7 for Proposal, immutable Revision, Lifecycle, Approval, Activity, and Idempotency records.
- RFC-8785/JCS-oriented revision and request hashing matched to P1a fixtures.
- `CreateProposal`, `AppendProposalRevision`, `DecideProposalApproval`, `WithdrawProposal`, and durable read/activity APIs.
- Atomic lifecycle/Approval/Proposal transitions, replay-before-mutable-check idempotency, conflict rollback, restart preservation, and concurrent linearization.
- Updated the existing v5 migration test to assert the dynamic migration head.

Changed paths:

- `internal/store/store.go`
- `internal/store/proposal_canonical.go`
- `internal/store/proposals.go`
- `internal/store/task_proposal_canonical_test.go`
- `internal/store/task_proposal_store_test.go`
- `internal/store/transcript_identity_test.go`
- `docs/experiment-ledger.md`
- `artifacts/omp/p1b/store-implementation-output.md`

Final verification passed:

```text
go test ./internal/store -run <focused P1b suite>
go test ./internal/store -count=1
go test -race ./internal/store -count=1
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test
```

`gofmt -d` produced no diff. No GUI/IPC, Grill/policy evaluation, claims, workers, adapters, process launch, commit, or push was added or run.
 supersedes only a pending predecessor pair; a rejected predecessor remains rejected. Approval approves the Proposal and paired lifecycle; rejection leaves the Proposal open. Withdrawal withdraws a pending pair but preserves a rejected pair. Existing `Store` single-writer configuration serializes competing proposal mutations.

## TDD evidence

Observed RED before implementation:

- canonical fixture test failed because `canonicalJSONHash` was undefined;
- create, append, decision, and withdrawal tests failed because each corresponding API was absent;
- valid UTF-8 control text failed validation, then passed after removing the non-contractual NUL restriction.

Observed GREEN:

- canonical Revision hash and all create/append/decision/withdraw body hashes match P1a fixtures;
- durable create/pending pair, atomic append, approval, rejected append, and rejected withdrawal tests pass;
- v6-to-head migration, restart replay, idempotency conflict, stale append, competing decision, and barrier-synchronized append/decision/replay tests pass.

## Verification

All observed successful after repairing the existing hard-coded migration-head assertion:

```text
go test ./internal/store -count=1
go test -race ./internal/store -count=1
node contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs --self-test
```

`gofmt` completed over every touched Go file without a remaining diff.

## Remaining risks

No known failing gate. Independent review should scrutinize RFC 8785 number and Unicode edge cases beyond the frozen P1a corpus, plus the migration-v7 foreign-key and idempotency invariants. No GUI/IPC, Grill/policy evaluation, claims, workers, adapters, or process launch behavior was added.
