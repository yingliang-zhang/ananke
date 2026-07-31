Working...
Implemented the P3e final cross-handle proof repair only.

- `internal/lifecycle/omp_adapter_test.go`
  - Final admission-window test now invokes the second store handle’s real `ReclaimLaunchClaim` with `PRAGMA busy_timeout = 0`.
  - Requires returned `SQLITE_BUSY` or `database is locked` semantics while `WithLaunchFenceAdmission` is held.
  - Confirms fake execution has not occurred before releasing the gate.
  - After fake start returns and admission rolls back, restores `busy_timeout = 5000`, retries the **same** reclaim request, and verifies generation `+1` plus active-fence equality.
  - Retained the pre-admission second-handle reclaim/no-fake regression.

- `docs/experiment-ledger.md`
  - Added the actual-reclaim proof record and explicit isolation statement: focused repetitions completed before full/race gates; no determinism claim relies on concurrent broader suites.

Verification passed, sequentially:

```sh
go test ./internal/lifecycle -run '^TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims$' -count=50 -timeout 180s
go test ./internal/lifecycle -run '^TestP3E' -count=20 -timeout 180s
go test ./... -count=1 -timeout 300s
go test -race ./... -count=1 -timeout 360s
go vet ./...
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
```

All passed. No real OMP, wrapper, target, commit, or push was invoked.
