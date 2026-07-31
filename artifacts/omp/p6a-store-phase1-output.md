Working...
Implemented Tasks 1 and 2 only. No commit.

### Changed files

- `internal/store/p6_repair.go` — new P6 authorization, policy, approval, attempt, event schemas; canonical hashes; validation; atomic admission; event CAS append; migration v15.
- `internal/store/p6_repair_test.go` — table-driven TDD coverage for migration, rollback, replay, conflicts, concurrency, P4 binding, validation denials, insert-only storage, and event sequencing.
- `internal/store/store.go` — registered migration v15.

### Implemented guarantees

- Exact persisted P4 bundle/admission/full-fence binding; P4 remains explicitly design-only.
- Fresh `local_gui_operator` `approved` decision bound to the exact policy hash.
- Exact repository identity/path, base commit/tree, worktree parent/name, ordered paths/tests, route/role, and attempt policy.
- Canonical self-hashes at every authority layer.
- Single transaction inserts authorization, attempt, and sequence-1 `prepared` event.
- Exact replay returns the stored fact without new rows; conflicting IDs or hashes reject.
- Concurrent duplicate admission creates exactly one fact.
- Event append compares exact prior sequence, event hash, and state.
- Closed states: `prepared`, `running`, `waiting_for_review`, `failed`, `waiting_for_human`.
- No execution-oriented forbidden fields or columns.
- Insert-only triggers protect all three P6 tables.

### TDD evidence

Initial RED run failed compilation because all P6 types and APIs were absent. A targeted repository/worktree-overlap test also failed before the overlap validation was added. Both became GREEN after implementation.

### Exact verification results

- `go test ./internal/store -run 'P6' -count=1` — PASS
- `go test ./internal/store -run 'P6' -count=10` — PASS
- `go test -race ./internal/store -run 'P6' -count=3` — PASS
- `go test ./internal/store -count=1` — PASS
- `gofmt -w internal/store/p6_repair.go internal/store/p6_repair_test.go internal/store/store.go` — completed, no output
- `go vet ./...` — PASS, no output
- `git diff --check` — PASS, no output

Remaining Task 1/2 gaps: none. No trusted-supervisor, repairrunner, CLI, documentation, ledger, or artifact files were edited.
