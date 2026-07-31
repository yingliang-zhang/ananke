Implement Tasks 1 and 2 only from `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md` in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. Strict TDD. Do not edit trusted-supervisor, repairrunner, docs/ledger, CLI, or artifacts. No commit.

Scope: durable P6 schema/migration, canonical authorization validation, and atomic admission-before-effect in `internal/store`.

Critical requirements:
- P4 remains design-only and cannot authorize execution alone.
- Require an exact already-persisted P4 admission/bundle/full fence and a fresh P6 local GUI operator approval.
- Bind exact repo identity/path, base commit/tree, worktree parent/name, ordered allowlisted relative paths, ordered policy-owned test declarations, exact route/role identity, attempt 1..2 and cap exactly 2.
- Canonical self-hashed insert-only authorization. Atomic transaction inserts authorization + attempt + sequence-1 prepared event before any later effect.
- Exact replay returns byte-identical durable fact without new rows. Same ID/hash with any conflict rejects. Missing/stale/projected P4 rejects.
- Event append must compare exact prior sequence/hash/state. Closed states exactly prepared,running,waiting_for_review,failed,waiting_for_human; no completed/success.
- No prompt, raw command/shell, credential, endpoint, arbitrary environment, source bytes, raw output, branch, commit message, or push/merge authority in structs/tables.
- Validate local_gui_operator approved decision, exact hash grammar, clean absolute repository/worktree parent paths, clean relative paths; reject `.git`, traversal, backslashes, duplicates, unsorted lists, ambiguous path prefix overlap. Test declarations carry ID, pinned executable identity/hash, fixed argv list, timeout, command hash; no shell string.
- Use existing store migration, canonical JSON/hash, transaction, and P4 persistence conventions rather than inventing a second DB layer.
- Table-driven RED tests must cover all listed denials, migration idempotence, rollback, replay, conflict, and concurrent duplicate admission.
- Run `go test ./internal/store -run 'P6' -count=1`, then count=10, race count=3, full store package, gofmt, vet, diff-check.
Return changed files and exact results, with any remaining Task 1/2 gap stated explicitly.