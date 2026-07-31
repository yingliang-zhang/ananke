Working...
## CHANGES REQUESTED

### P1 — Cross-handle proof is not deterministic enough

`TestP3ECrossHandleReclaimWaitsForFinalFenceAdmission` opens a real second `Store` handle and starts `ReclaimLaunchClaim`, but its `reclaimEntered` channel closes **before** that goroutine calls `ReclaimLaunchClaim` (`omp_adapter_test.go:547-561`). The only evidence that the second handle reached SQLite before fake execution is a `25ms` absence-of-result check (`568-572`).

A scheduler-delayed reclaim goroutine can therefore pass the test without ever attempting `BEGIN IMMEDIATE` until after `releaseAdapterReturn`; reclaim would then succeed legally and the test would still pass. The test proves the intended path under normal scheduling, not the claimed deterministic cross-handle lock contention.

**Required repair:** make the test observe a real second-handle SQLite admission attempt while the first transaction is held—e.g. instrument the actual `BEGIN IMMEDIATE` operation, or use a zero-busy-timeout second connection and assert a real `SQLITE_BUSY` before allowing fake start, followed by successful reclaim only after rollback.

### Source review: linearization implementation is sound

- `sqliteDSN` configures `_txlock=immediate`, WAL, and one connection per `Store` handle (`internal/store/store.go:27-33,46-50`). The pinned `modernc.org/sqlite v1.54.0` emits `begin immediate` for non-read-only `BeginTx` when that setting is present (`tx.go:19-31`).
- `WithLaunchFenceAdmission` starts that transaction, loads the active recovery boundary, compares the complete `LaunchFence`, invokes the callback, then rolls back only after the callback returns (`internal/store/launch_admission.go:1260-1278`).
- Boundary loading checks the current active claim and requires the latest outbox to carry the exact full active fence (`1287-1367`); runtime validation additionally requires `retry_process_admission`, exact request/claim/outbox fence equality, materialization, and run intent (`omp_adapter.go:612-624`).
- The callback revalidates descriptor binding and the full fence/outbox boundary immediately before `adapter.Start`; `cmd.Start()` runs inside the admission transaction (`omp_adapter.go:554-566`, `77-97`).
- No process-local mutex supplies the authority: `r.mu` is released before materialization/launch and reacquired only after admission for session registration. The SQLite transaction is the only lock spanning `Start`.

The pre-admission case is covered correctly: a separate-handle reclaim occurs after prior final-boundary validation but before transaction admission; the subsequent admission sees the stale fence and the fake is not invoked (`TestP3EPreAdmissionReclaimFromSecondHandlePreventsFakeExecution`).

### Prior P3e repairs

Confirmed in source and focused tests:

- Complete sealed tuple plus independently established source seal; recomputed caller bytes/tuple are denied.
- Descriptor/inode-bound materialization; post-seal namespace replacement is denied before fake execution.
- Partial-created tree cleanup and foreign duplicate-path preservation.
- `cancel_requested` recovery returns exactly `retry_bounded_cancellation`.
- Duplicate JSON object members fail closed.
- `p3eExecAdapter` is private; its sole construction is test-only re-exec of `TestP3EFakeAdapterExecutable` using `os.Args[0]` and a temporary synthetic root. No `cmd/`, GUI, or public lifecycle construction callsite exists.

### Verification passed

- Final-fence pair, 50 repetitions: PASS.
- Focused P3e suite, 20 repetitions: PASS.
- `go test ./... -count=1 -timeout 300s`: PASS.
- `go test -race ./... -count=1 -timeout 360s`: PASS.
- `go vet ./...`: PASS.
- P3d syntax, fixture, and self-test contract gates: PASS.

No files were edited or committed.
