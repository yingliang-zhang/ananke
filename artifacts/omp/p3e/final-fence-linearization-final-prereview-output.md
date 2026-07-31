Working...
# CHANGES REQUESTED

No edits or commits made.

## Blocking test gap

`TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims` does **not** interleave an actual reclaim during the final admission window.

- While the first handle is paused after in-transaction validation, the second handle calls raw `reclaimer.DB().BeginTx(...)` and requires `SQLITE_BUSY` (`internal/lifecycle/omp_adapter_test.go:547-570`).
- `ReclaimLaunchClaim(...)` is called only after the gated fake returns and `runtime.start` has returned—therefore after `WithLaunchFenceAdmission` has rolled back (`:594-615`).
- The real reclaim path validates its request, starts its own transaction, validates the exact active fence, writes the new immutable generation/head/outbox, then commits (`internal/store/launch_admission.go:658-688`). The current contention probe never exercises that path under the held admission lock.

Required repair: during the held callback, invoke `reclaimer.ReclaimLaunchClaim` with `busy_timeout=0` and require `SQLITE_BUSY` before releasing fake start. Then, after admission rollback, retry the actual reclaim and require the existing exact generation-plus-one and active-fence assertions. This proves the claimed operation—not merely an equivalent raw write admission—was linearized.

## Runtime review: accepted

- `WithLaunchFenceAdmission` starts a transaction, reads the recovery boundary inside it, compares the complete `LaunchFence`, invokes the callback, and only then rolls back (`internal/store/launch_admission.go:1252-1278`).
- The configured DSN sets `_txlock=immediate`, WAL mode, and one connection per Store handle (`internal/store/store.go:27-50`). The pinned `modernc.org/sqlite v1.54.0` confirms `BeginTx` under `immediate` is a write transaction, including in WAL mode.
- Boundary loading requires the latest outbox to exactly match the active full fence (`launch_admission.go:1302-1308`). Runtime validation additionally requires `retry_process_admission`, request/claim/outbox full-fence equality, matching materialization, and matching run intent (`omp_adapter.go:612-624`).
- The callback revalidates sealed-root binding and the complete durable boundary immediately before `adapter.Start`; `cmd.Start()` remains inside the SQLite admission transaction (`omp_adapter.go:554-567`, `77-97`).
- Therefore a reclaim that commits after the earlier validation but before admission starts is rejected before fake execution by the second-handle pre-admission test. Once admission has begun, a competing writer cannot replace the active fence before `Start` returns.
- No process-lifetime mutex is claimed as authority. `r.mu` protects runtime maps only; it is not held through materialization/admission/start. The SQLite transaction ends immediately after `Start` returns.

## Prior P3e repairs: accepted

- Complete sealed tuple plus independently established source seal rejects caller-recomputed source bytes.
- Descriptor/inode binding is revalidated at launch; the child receives a directory descriptor through `ExtraFiles`, not a mutable worktree pathname.
- Partial materialization cleanup and foreign duplicate-path preservation are covered.
- `cancel_requested` recovery returns `retry_bounded_cancellation` with empty fail-closed state.
- Duplicate JSON object members fail closed before normalization.
- `p3eExecAdapter` and `newOMPReadOnlyRuntime` have only the test construction callsite. It re-execs `os.Args[0]` for `TestP3EFakeAdapterExecutable` against temporary roots and journals; no command, daemon, GUI, real OMP, wrapper, or real target path constructs it.

## Verification

Passed:

- Exact fence-proof pair, isolated: `-count=50`
- Focused P3e suite: `-count=20`
- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `go vet ./...`
- `node --check contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs --self-test`

Caveat: the first `-count=50` proof run, launched concurrently with full and race suites, failed once with admission denial before fake invocation; rerunning the identical command in isolation passed 50/50. [INFERENCE] This was host-resource contention, but it further argues against calling the current test fully deterministic.
