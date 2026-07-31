Working...
Repaired P3b review blockers.

- `internal/store/launch_admission.go`
  - Added sealed-contract validation for every persisted materialization read used by:
    - idempotent `RecordLaunchMaterializationReady` reload,
    - `CreateLaunchRunIntent`,
    - `GetLaunchRecoveryBoundary`.
  - Hash/nonce divergence now returns `ErrLaunchRecordCorrupt` before a Run intent or recovery action can be projected.
  - Recovery verifies a durable Run intent names the exact recovered materialization ID.

- `internal/store/launch_admission_test.go`
  - TDD RED observed the pre-fix idempotent reload returning `ErrLaunchMaterializationConflict` for an FK-valid corrupt row.
  - Added active-fence, FK-valid corrupted materialization + sequence-2 outbox regression; readiness reload, recovery, and Run-intent creation now fail closed.
  - Extended same-generation wrong-token and lower-generation stale vectors to cover readiness and reclaim, asserting no materialization/claim/outbox/head mutation.
  - Added direct raw-SQL FK and uniqueness enforcement tests for forged child fences and duplicate claim generation, outbox stage, materialization, and Run-intent identities.

- `docs/experiment-ledger.md`
  - Recorded factual RED/GREEN, verification, and scope evidence.

Verification passed:

```sh
go test ./internal/store -run '^TestP3B' -count=1
go test ./internal/store -count=1 -timeout=180s
go test ./... -count=1 -timeout=180s
go test -race ./internal/store -count=1 -timeout=180s
go test ./internal/store -run '^TestP3BClaimReclamationIsAtomicAcrossStoreHandles$' -count=100
```

Normal and `--self-test` contract gates also passed for `p1a`, `p1c`, `p2a`, `p2c`, and `p3a`.
