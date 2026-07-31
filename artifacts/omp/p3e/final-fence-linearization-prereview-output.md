Working...
## CHANGES REQUESTED

One verification blocker; no code was edited or committed.

### Final-fence implementation

**Implementation appears linearizable.**

- `Store.Open` configures every SQLite handle with `_txlock=immediate` (`internal/store/store.go:31-49`).
- The pinned `modernc.org/sqlite v1.54.0` driver turns a default writable `BeginTx` into `BEGIN IMMEDIATE` when `_txlock=immediate` is configured.
- `WithLaunchFenceAdmission` begins that transaction, loads the durable recovery boundary, checks the full active `LaunchFence`, invokes the runtime callback, then rolls back only after the callback returns (`internal/store/launch_admission.go:1252-1278`).
- Boundary loading proves the latest outbox has the exact active fence and derives the only legal action from its validated stage (`launch_admission.go:1302-1367`).
- The runtime’s callback revalidates the sealed descriptor and exact `retry_process_admission` boundary immediately before `adapter.Start`; `cmd.Start()` occurs while the immediate transaction remains open (`internal/lifecycle/omp_adapter.go:553-563`).
- No process-lifetime lock is retained: the transaction rolls back immediately after `Start` returns. The only runtime mutex protects in-memory sessions/materialization bookkeeping.

Therefore a reclaim cannot **commit** between the in-transaction fence/outbox validation and `cmd.Start`. It either commits before admission begins—causing fail-closed stale-fence rejection—or waits until after the fake has been started.

### Required repair: deterministic cross-handle proof is missing

`TestP3EReclaimAfterFinalValidationPreventsFakeExecution` does **not** test the claimed final-fence linearization.

- Its hook runs **before** `WithLaunchFenceAdmission` begins (`omp_adapter.go:549-553`).
- The hook synchronously calls `runtime.fence.ReclaimLaunchClaim` on the **same `Store` handle** (`omp_adapter_test.go:424-455`).
- The reclaim completes before the immediate transaction and its final callback validation exist. The test proves only: reclaim-before-admission makes the subsequent admission stale.
- There is no second `store.Open` handle, no concurrent reclaim goroutine, and no barrier inside the final transaction between callback validation and `adapter.Start`.

The test passed 20 repetitions, but its topology cannot establish cross-handle lock serialization through `cmd.Start`.

**Required test shape:**

1. Open a second `Store` on the same SQLite journal.
2. Place a deterministic barrier after the callback’s final descriptor/fence/outbox validation and before the fake’s actual `Start`.
3. Start `ReclaimLaunchClaim` through the second handle.
4. Assert reclaim cannot complete while the admission transaction is held.
5. Release fake start; assert the fake invocation occurred.
6. Assert reclaim completes only after `WithLaunchFenceAdmission` releases its transaction.
7. Retain/convert the existing pre-transaction case to use the second handle, asserting that if reclaim commits before admission acquires the immediate transaction, the fake is not invoked.

This proves both allowed linearizations rather than merely exercising the stale path.

### Prior P3e repairs

Confirmed in source and targeted tests:

- Complete sealed tuple and private source binding; caller-recomputed source bytes are rejected.
- Descriptor/inode-bound materialization; child receives a directory descriptor, not a mutable materialization pathname.
- Descriptor-owned partial-tree cleanup and foreign duplicate preservation.
- Explicit `cancel_requested` recovery returning `retry_bounded_cancellation`.
- Duplicate JSON transcript members rejected before normalization.
- Fake-only isolation: the sole `p3eExecAdapter` construction is test code re-executing `TestP3EFakeAdapterExecutable` with a temporary synthetic root. No production constructor/callsite, real OMP executable, real repository target, daemon, GUI, or CLI path exists.

### Verification run

All green, but none closes the missing interleaving proof:

- `go test ./internal/lifecycle -run '^TestP3E' -count=1 -timeout 60s`
- `go test ./internal/lifecycle -run '^TestP3EReclaimAfterFinalValidationPreventsFakeExecution$' -count=20 -timeout 60s`
- `go test ./internal/store -run '^TestP3BClaimReclamationIsAtomicAcrossStoreHandles$' -count=20 -timeout 60s`
- `go test ./... -count=1 -timeout 300s`
- `go test -race ./... -count=1 -timeout 360s`
- `go vet ./...`
- Normal and `--self-test` contract gates for `p1a`, `p1c`, `p2a`, `p2c`, `p3a`, and `p3d`.

The implementation has the intended transaction boundary; acceptance requires the missing deterministic **separate-handle, in-transaction** regression.
