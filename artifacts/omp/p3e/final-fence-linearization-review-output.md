Working...
## ACCEPT

No files edited or committed.

**Fence linearization**
- `WithLaunchFenceAdmission` opens the SQLite transaction before loading the active recovery boundary, verifies the complete `LaunchFence`, invokes the final callback while the transaction remains open, then rolls back on return.
- Store DSN sets `_txlock=immediate`; the installed `modernc.org/sqlite` driver maps that to `BEGIN IMMEDIATE`. The cross-handle write lock therefore begins before active-fence validation and persists through `adapter.Start`.
- Boundary loading derives the active claim from `launch_claim_heads`, loads the matching-generation latest outbox, and requires the entire fence tuple to match. Runtime then requires `retry_process_admission`, exact request/claim/outbox fence equality, matching sealed materialization and run intent, and no terminal/evidence intents.
- No process-wide fence mechanism exists: no file/process lock or in-memory admission claim. `Store.mu` only protects close/migration state; runtime mutex protects session/materialization bookkeeping.

**Reclaim / fake-execution proofs**
- `TestP3EPreAdmissionReclaimFromSecondHandlePreventsFakeExecution` performs reclaim from a separately opened Store after the outer final validation and before admission transaction acquisition. Admission observes the stale fence; fake invocation is absent.
- `TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims` pauses inside the admission callback after descriptor and complete fence/outbox validation. It uses a separate SQLite handle, pins its sole connection, sets `busy_timeout = 0`, and launches a real `ReclaimLaunchClaim`.
  - Reclaim returns `SQLITE_BUSY` / locked while admission is held.
  - Fake invocation remains absent until the start gate is released.
  - Fake invocation then occurs before the admission transaction returns.
  - Retrying the same reclaim after rollback succeeds at generation `+1`, and the active fence equals the reclaimed fence.
- `TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims -count=50` passed.

**Prior P3e repairs rechecked**
- Complete sealed tuple and independently established source seal reject recomputed caller-source bypasses.
- Launch uses a sealed directory descriptor via `ExtraFiles`, not a mutable worktree pathname; post-seal replacement is rejected before fake execution.
- Partial materialization cleanup and duplicate-path preservation are covered.
- Cancellation recovery returns `retry_bounded_cancellation`.
- Duplicate JSON transcript members fail closed.
- `p3eExecAdapter` and `newOMPReadOnlyRuntime` have only the test construction path: re-exec of `TestP3EFakeAdapterExecutable` against temporary roots/journals. No production command, daemon, GUI, real OMP wrapper, or real repository target constructs it.

**Verification passed**
- `go test ./internal/lifecycle -run '^TestP3E' -count=3 -timeout 120s`
- `go test ./... -count=1 -timeout 300s`
- `go test -race ./... -count=1 -timeout 360s`
- `go vet ./...`
- Syntax, normal verification, and self-tests for `contracts/{p1a,p1c,p2a,p2c,p3a,p3d}/verify.mjs`.
