ImplementedRepaired P3e cross-handle proof quality only.

- `internal/lifecycle/omp_adapter_test.go`
  - Replaced the scheduler-sensitive 25 ms noncompletion assertion.
  - New `TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims` holds the first `WithLaunchFenceAdmission` transaction, sets the independent handle’s SQLite `busy_timeout` to zero, and performs its real writable `BeginTx`.
  - Requires observable `SQLITE_BUSY` before releasing fake start; confirms no fake invocation beforehand.
  - After gated fake completion and admission rollback, second-handle reclaim must succeed at generation `+1` and match the active fence.
  - Kept the pre-admission second-handle reclaim/stale-fence/no-fake case.

- Evidence recorded:
  - `docs/experiment-ledger.md`
  - `artifacts/omp/p3e/deterministic-sqlite-contention-proof-output.md`

Verification passed:

```text
go test ./internal/lifecycle -run '^TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims$' -count=50 -timeout 180s
go test ./internal/lifecycle -run '^TestP3E' -count=20 -timeout 180s
go test ./... -count=1 -timeout 300s
```

No production SQLite isolation, runtime authority, real OMP/target access, commits, or pushes were changed or exercised.

- `go test ./internal/lifecycle -run '^TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims$' -count=1 -timeout 30s` — PASS.
- `go test ./internal/lifecycle -run '^TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims$' -count=50 -timeout 180s` — PASS.
- `go test ./internal/lifecycle -run '^TestP3E' -count=20 -timeout 180s` — PASS.
- `go test ./... -count=1 -timeout 300s` — PASS (3 packages; 3 packages without tests).
