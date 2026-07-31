Working...
Implemented P3c aggregate recovery isolation.

- `internal/store/launch_admission.go`
  - Added `LaunchRecoveryResult`: exact active `LaunchSpecHash` plus either a boundary or original private cause.
  - `ListLaunchRecoveryBoundaries` now retains corrupt active records instead of aborting the aggregate.

- `internal/lifecycle/fenced_launch.go`
  - `recoverAll` continues valid boundaries.
  - Per-boundary causes produce exact-hash `WaitingForHuman` actions.
  - Boundary validation failures remain local; no process, terminal, evidence, filesystem, or materialization outcome is inferred.
  - Single-launch `recover` semantics remain unchanged.

- Tests:
  - Valid + corrupt active aggregate retains the valid exact retry, emits corrupt `WaitingForHuman` with private `ErrLaunchRecordCorrupt`, and asserts `runs` remains empty.
  - Added explicit evidence-intent recovery fail-closed coverage.
  - Updated corrupt aggregate-store expectation to preserve hash/cause.

- Ledger evidence: `docs/experiment-ledger.md`.

Verification passed:

```sh
go test ./internal/store -run '^TestP3B' -count=1
go test ./internal/lifecycle -run '^TestP3C' -count=1
go test ./... -count=1 -timeout=180s
go test -race ./... -count=1 -timeout=180s
node --check contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test
```

No product Run, worktree/materialization filesystem action, adapter/OMP, daemon, Tauri/UI, or commit path was invoked.
