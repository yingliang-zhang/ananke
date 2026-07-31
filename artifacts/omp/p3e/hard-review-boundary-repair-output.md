Working...
Repaired all P3e hard-review findings.

- `internal/lifecycle/omp_adapter.go`
  - Added and validated full P3d sealed tuple: materialization hash, nonce, `payload_hash`, `seal_fingerprint`.
  - Replaced caller-controlled `SourceHash` with an independently established private source seal; source bytes are copied before materialization.
  - Made materialization descriptor-owned; pre-exec boundary revalidates descriptor/device/inode, namespace binding, and fence. Fake receives only the directory descriptor.
  - Transactional cleanup preserves foreign duplicate paths and removes partial created trees.
  - Added `cancel_requested` and exact `retry_bounded_cancellation` recovery with empty fail-closed state.
  - Replaced permissive JSON unmarshalling with duplicate-member-rejecting decoding.
  - Added deterministic stale-fence revalidation immediately before fake start.

- `internal/lifecycle/omp_adapter_test.go`
  - Added recomputed-source bypass, post-seal replacement, duplicate/partial cleanup, pre-terminal cancellation recovery, duplicate JSON-member, and late stale-fence tests.
  - All execution remained the controlled test re-exec fake.

- `docs/experiment-ledger.md`
  - Recorded scope, repairs, RED evidence, verification, and residual fake-only boundary.

Verification passed:

```text
go test ./internal/lifecycle -run '^TestP3E' -count=1 -timeout 60s
go test ./... -count=1 -timeout 300s
go test -race ./... -count=1 -timeout 360s
go vet ./...
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
```

No real OMP, wrapper, Ananke target, daemon, Tauri/UI, commit, or push was invoked.
