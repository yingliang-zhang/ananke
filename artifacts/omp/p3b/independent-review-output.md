Working...
# CHANGES REQUESTED

## Major correctness issue

`launch_materializations` is checked against the sealed spec **only at API insertion time**. Later durable reads trust any syntactically valid materialization row.

- `RecordLaunchMaterializationReady` compares request hash/nonce to the sealed spec at `internal/store/launch_admission.go:829`.
- `loadLaunchMaterialization` validates only record shape/state/timestamp, not sealed-spec equality (`:875-898`).
- `CreateLaunchRunIntent` then accepts the loaded materialization based on active fence and ID only (`:919-925`).
- `GetLaunchRecoveryBoundary` likewise checks only the materialization fence (`:1239-1245`).

A corrupted but FK-valid database can therefore contain a matching-fence `ready` materialization with a different valid-format hash/nonce plus sequence-2 outbox record. Recovery would return `retry_run_admission`, and Run-intent creation could proceed. This violates the required exact sealed materialization ID/hash/nonce readiness and fail-closed corruption behavior.

**Required fix:** validate persisted materialization hash and nonce against `StoredLaunchSpec.Spec.SealedContract` before returning a recovery boundary or creating a Run intent; reject with `ErrLaunchRecordCorrupt`. Validate an idempotently reloaded materialization too.

**Required regression:** insert a fully FK-valid, active-fence materialization/outbox pair with a different valid hash or nonce; assert both recovery and Run-intent creation fail closed.

## Required coverage gaps

1. `TestP3BFenceRejectsSameGenerationWrongTokenAndLowerGeneration` exercises stale denial only for Run, terminal, and evidence writes (`launch_admission_test.go:315-334`). It does not prove same-generation wrong-token and lower-generation denial for:
   - `RecordLaunchMaterializationReady`
   - `ReclaimLaunchClaim`

   Add both vectors, asserting no materialization/claim/outbox/head mutation.

2. The migration test confirms table existence and an empty `foreign_key_check` (`:84-107`), but does not exercise P3b FK and uniqueness enforcement. Add direct constraint tests for forged child fences and duplicate generation/stage/materialization/Run identities. An empty FK check alone cannot prove the constraints reject invalid writes.

## Passed review points

- v11 migration is registered; P2-head migration reaches schema version 11.
- Launch specs are canonical JCS-hashed, stored immutable, and bound at admission to the current exact P1 revision plus approved reciprocal lifecycle/approval tuple.
- Claim acquire/reclaim are transactionally serialized through `_txlock=immediate`; reclaim requires the complete active fence.
- All inspected modeled write APIs compare the full active `LaunchFence`; stale Run/terminal/evidence cases are covered.
- Append-only triggers and composite foreign keys bind claim, materialization, outbox, and Run-intent records to full fenced identities.
- Candidate code does not invoke `CreateRun`, create a `runs` row, start a process, materialize/open a worktree, invoke adapter/OMP/daemon/Tauri/UI, or execute commands. The existing P3b test asserts `runs` remains empty.
- P3a fixtures/verifier are unchanged by the tracked candidate diff.

## Verification run

Passed:

```text
go test ./internal/store -run '^TestP3B' -count=1
go test ./internal/store -count=1 -timeout=180s
go test ./... -count=1 -timeout=180s
go test -race ./internal/store -count=1 -timeout=180s
go test ./internal/store -run '^TestP3BClaimReclamationIsAtomicAcrossStoreHandles$' -count=100
```

Passed normal and `--self-test` gates for `contracts/p1a`, `p1c`, `p2a`, `p2c`, and `p3a`.

`git diff --check` passed. I made no edits or commits.
