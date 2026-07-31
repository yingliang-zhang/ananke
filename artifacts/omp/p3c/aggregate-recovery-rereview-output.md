Working...
## CHANGES REQUESTED

**Major — `recoverAll` suppresses non-authority failures as `WaitingForHuman`.**

- `ListLaunchRecoveryBoundaries` records **every** `GetLaunchRecoveryBoundary` error as a per-hash `Cause` (`internal/store/launch_admission.go:1362-1368`).
- `recoverAll` converts every non-`nil` `Cause` into an exact-hash `WaitingForHuman` action and returns `nil` aggregate error (`internal/lifecycle/fenced_launch.go:117-124`).
- This differs from single-launch `recover`, which only maps `ErrLaunchClaimNotFound`, `ErrLaunchSpecNotFound`, and `ErrLaunchRecordCorrupt` to human intervention; other errors propagate (`fenced_launch.go:97-105,201-203`).

Therefore a context cancellation after hash discovery, or a database/read failure during one per-hash recovery, is silently recast as a human-repair condition. That loses the operational failure and violates the intended no-suppression behavior. `recoverAll` should apply the same human-intervention classifier, preserving exact-hash `WaitingForHuman` only for unknown/corrupt authority; propagate other errors.

### Confirmed correct

- Per-hash identity is retained before recovery; corrupt results retain their original private cause.
- Valid and corrupt active boundaries coexist: valid action remains `retry_materialization`; corrupt action is exact-hash `WaitingForHuman`.
- Explicit evidence-intent path remains fail-closed: no terminal/evidence outcome inferred.
- Relevant tests assert the real `runs` table remains empty.
- Single-launch `GetLaunchRecoveryBoundary` and `recover` semantics are unchanged.
- Reviewed diff scope is limited to:
  - `internal/store/launch_admission.go`
  - `internal/store/launch_admission_test.go`
  - `docs/experiment-ledger.md`
- No process, worktree, adapter, UI, or protocol changes in the diff.

### Verification passed

```text
go test ./internal/store -run '^(TestP3BRejectsCorruptP1AndLaunchAuthorityVectors|TestP3BAggregateRecoveryPreservesValidAndCorruptActiveBoundaries)$' -count=1
go test ./internal/lifecycle -run '^(TestP3CFencedLaunchAggregateRecoveryIsolatesCorruptBoundary|TestP3CFencedLaunchFailsClosedOnUnknownCorruptAndOutcomeFacts)$' -count=1
go test ./... -count=1 -timeout=180s
go test -race ./... -count=1 -timeout=180s
node --check contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test
git diff --check
```

All passed. No files edited or commits made.
