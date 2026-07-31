Working...
# ACCEPT

No files changed.

**Final-fence linearization**
- `Store.Open` configures `_txlock=immediate`, WAL, and one connection per handle (`internal/store/store.go:27-50`). Pinned `modernc.org/sqlite v1.54.0` translates writable `BeginTx` to `BEGIN IMMEDIATE` (`tx.go:19-31`).
- `WithLaunchFenceAdmission` begins that transaction, loads the recovery boundary inside it, compares the complete `LaunchFence`, invokes the callback, then rolls back only after callback return (`internal/store/launch_admission.go:1252-1278`).
- Boundary loading authenticates the active claim and requires the latest outbox to carry the identical full fence (`:1287-1367`). Runtime validation additionally requires `retry_process_admission`, exact request/claim/outbox fence equality, matching materialization, and matching run intent (`internal/lifecycle/omp_adapter.go:612-624`; `fenced_launch.go:173-206`).
- The runtime revalidates descriptor/root binding and the complete durable boundary inside the admission callback immediately before `adapter.Start`; `cmd.Start()` is inside that callback and therefore inside the immediate transaction (`omp_adapter.go:554-567`, `77-97`).
- No process-lifetime admission lock is claimed or retained. The transaction is deliberately rolled back after `Start` returns; the runtime mutex only protects in-memory session/materialization bookkeeping.

**Reclaim interleaving proof**
- `TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims` opens an independent second `Store` handle to the same SQLite file (`omp_adapter_test.go:508-612`, fixture `:648-663`).
- The first handle pauses after in-transaction descriptor/fence/outbox validation and before `Start`.
- The second handle sets `busy_timeout = 0` and invokes the real `ReclaimLaunchClaim`, not raw SQL or a synthetic lock probe. It must return `SQLITE_BUSY`/locked while the first handle is held; fake execution is asserted absent at that point.
- Only after releasing the admission gate does the fake invoke. The test then releases the adapter-return gate, allowing admission rollback, retries the **same** reclaim request, and verifies generation `+1` plus exact active-fence equality.
- This deterministically demonstrates that a reclaim attempted after final validation cannot commit and cannot cause the stale fence to invoke the fake. The separate pre-admission reclaim test confirms the complementary ordering: a cross-handle reclaim that commits before admission causes fail-closed/no fake execution.

**Prior P3e repairs rechecked**
- Complete sealed tuple and independently established source seal reject caller-recomputed source bytes.
- Descriptor/inode binding is revalidated at launch; the child receives only the sealed directory descriptor via `ExtraFiles`, never a mutable materialization pathname.
- Duplicate/partial materialization cleanup is guarded and covered.
- `cancel_requested` recovery returns exact `retry_bounded_cancellation` with empty fail-closed state.
- Duplicate JSON object members are rejected before transcript normalization.
- `p3eExecAdapter` and `newOMPReadOnlyRuntime` have one construction path: test-only `os.Args[0] -test.run=^TestP3EFakeAdapterExecutable$` with fixed fake environment, synthetic temporary root, and temporary SQLite journal. No `cmd/`, daemon, GUI, Tauri, wrapper, real OMP, or real-target construction callsite was found.

**Verification passed**
- `go test ./internal/lifecycle -run '^TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims$' -count=50 -timeout 180s`
- `go test ./internal/lifecycle -run '^TestP3E' -count=20 -timeout 180s`
- `go test ./... -count=1 -timeout 300s`
- `go test -race ./... -count=1 -timeout 360s`
- `go vet ./...`
- P3d verifier syntax, normal verification, and self-test.
- Normal and self-test gates for P1a, P1c, P2a, P2c, and P3a contracts.
