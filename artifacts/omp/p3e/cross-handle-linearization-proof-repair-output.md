Working...
Implemented P3e proof repair only.

- `internal/lifecycle/omp_adapter.go`
  - Added an unexported test synchronization hook inside `WithLaunchFenceAdmission`’s callback.
  - Hook runs after sealed-descriptor and complete fence/outbox boundary validation, immediately before `p3eExecAdapter.Start`.
  - SQLite immediate-transaction linearization remains unchanged.

- `internal/lifecycle/omp_adapter_test.go`
  - Added file-backed synthetic SQLite fixture support for two independent `Store` handles.
  - Converted pre-admission reclaim coverage to use the second handle: reclaim commits before admission acquisition, admission denies, fake is never invoked.
  - Added deterministic cross-handle proof: pause the admitted callback, start second-handle reclaim, release fake start, observe fake invocation while the transaction is still held, then release adapter return and observe reclaim commit at generation +1.
  - All execution remains the deterministic test re-exec fake; no real OMP or target opened.

- `docs/experiment-ledger.md`
  - Recorded scope, RED/GREEN evidence, cross-handle proof, and verification results.

Verification:

- RED: focused proof tests initially failed because `afterFenceAdmissionValidation` was absent.
- `go test ./internal/lifecycle -run '^TestP3E' -count=20 -timeout 180s` — PASS.
- Final focused two-proof run — PASS.
- `go test ./... -count=1 -timeout 300s` — PASS.
- `go test -race ./... -count=1 -timeout 360s` — PASS.
- `go vet ./...` — PASS.
- P3d contract verifier plus self-test — PASS.
