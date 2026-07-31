Working...
Implemented P3e controlled fake-only adapter runtime.

**Changed**
- `internal/lifecycle/omp_adapter.go`
  - Private `ompReadOnlyAdapter` interface and private executable adapter.
  - Exact P3d HostSpec validation; bare/unknown route and writable capability deny.
  - P3b full-fence plus P3c `retry_process_admission` outbox validation before and after materialization.
  - Sealed source hashing, no-follow descriptor materialization, root/inode revalidation, read-only sealing, and descriptor-based cleanup.
  - Bounded transcript normalizer with only `audit_started`, `audit_finding`, `audit_completed`.
  - Fail-closed unknown/malformed/reordered transcript handling; no raw output in public IR.
  - Bounded cancellation, deadline timeout, autonomous fake crash handling, and recovery returning only safe obligations.

- `internal/lifecycle/omp_adapter_test.go`
  - Deterministic re-exec fake executable only: `os.Args[0] -test.run=^TestP3EFakeAdapterExecutable$`.
  - Lifecycle, reconnect, abrupt crash, cancellation, timeout, unknown transcript, stale-fence, HostSpec/materialization drift, traversal, and TOCTOU containment coverage.
  - Tests use only temporary synthetic roots and source bytes—no real OMP, wrapper, Ananke checkout, daemon, UI, or Tauri path.

- `docs/experiment-ledger.md`
  - Recorded the real-vs-fake execution boundary, TDD RED evidence, gate results, and residual non-activation boundary.

**Verification**
- RED established: missing P3e symbols caused the focused test compile failure.
- `go test ./internal/lifecycle -run '^TestP3E' -count=1 -timeout 60s` — PASS, 2.69s.
- `go test ./... -count=1 -timeout 300s` — PASS, 38.36s.
- `go test -race ./... -count=1 -timeout 360s` — PASS, 106.55s.
- `go vet ./...` — PASS.
- `node --check contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs --self-test` — PASS.

**Residual boundary**
- Runtime remains private and has no production callsite or real-OMP enablement.
- Recovery intentionally emits only `retry_adapter_admission` or `reconnect_transcript_source`; it infers no result, terminal state, verification outcome, or transcript event.
