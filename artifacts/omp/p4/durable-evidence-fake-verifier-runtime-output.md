Working...
Implemented P4 durable evidence/verifier + bounded repair-admission runtime.

**Changed files**
- `internal/store/p4_evidence_admission.go`
  - SQLite migration v13.
  - Immutable transactional records: evidence bundle, repair admission, verifier request/output/replay.
  - Exact frozen P1–P3f bindings, including P3f fixture hashes and 37-denial count.
  - Exact verifier trust/release identities, 12 evidence hashes, cap 2, allowed role/route, fresh approval/full fence, typed MoA, and zero-fact replay.
  - Immutable DB triggers and idempotent concurrent replay behavior.
- `internal/store/store.go`
  - Registered migration v13.
- `internal/store/p4_evidence_admission_test.go`
  - RED→GREEN transactional rollback, immutability, invariant rejection, and concurrent replay tests.
- `internal/lifecycle/p4_evidence_admission.go`
  - Fail-closed runtime. Both valid and failure paths return `waiting_for_human`; no local repair/run path exists.
- `internal/lifecycle/p4_evidence_admission_test.go`
  - Test-only `p4FakeVerifier`, replay-once/concurrency proof, malformed/failing verifier rejection, zero local runs, production build exclusion.
- `docs/experiment-ledger.md`
  - Recorded TDD evidence, durable policy bindings, no-repair boundary, and gate results.

**Verification**
- Focused: `go test ./internal/store ./internal/lifecycle -run '^TestP4' -count=1 -timeout=120s` — PASS.
- Full: `go test ./... -count=1 -timeout=300s` — PASS.
- Race: `go test -race ./... -count=1 -timeout=300s` — PASS.
- Vet: `go vet ./...` — PASS.
- Contracts: P3d, P3f, and P4 normal + self-test verifier chain — PASS.
  - P4 self-test validated all 38 ordered denial mutators with the closed `waiting_for_human` projection.

**No-repair boundary retained**
- No concrete production verifier; only `p4FakeVerifier` in `_test.go`.
- No network, OMP, process, child, source, artifact, repair, or local-run operation.
- Verified output remains `waiting_for_human` with `repair_execution: not_authorized_by_verifier`.
- Failure output remains `waiting_for_human` with `repair_execution: not_authorized`.
- No commit or push performed.
