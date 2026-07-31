Working...
Implemented P3c aggregate recovery error propagation.

- `internal/lifecycle/fenced_launch.go`
  - `recoverAll` now gates per-hash causes through `isFencedLaunchHumanIntervention`.
  - Only `ErrLaunchClaimNotFound`, `ErrLaunchSpecNotFound`, and `ErrLaunchRecordCorrupt` produce exact-hash `WaitingForHuman`.
  - `context.Canceled` and other non-authority/read failures return unchanged with no aggregate actions.
  - Added a private lifecycle store contract to allow the regression’s delegating fault store; no public API change.

- `internal/lifecycle/fenced_launch_test.go`
  - Added after-hash-discovery aggregate regressions for `context.Canceled` and an injected non-authority read failure.
  - Both assert exact error identity (`==`), no `WaitingForHuman` actions, confirmed prior hash discovery, and zero real `runs`.
  - Existing valid-plus-corrupt isolation test remains passing.

- `docs/experiment-ledger.md`
  - Recorded TDD red/green evidence, scope, verification commands, and terminal verdict.

Verification passed:

```sh
go test ./internal/lifecycle -run '^TestP3CFencedLaunchAggregateRecoveryPropagatesOperationalErrors$' -count=1
go test ./internal/lifecycle -run '^TestP3CFencedLaunchAggregateRecovery(IsolatesCorruptBoundary|PropagatesOperationalErrors)$' -count=1
go test ./internal/store -run '^TestP3B' -count=1
go test ./... -count=1 -timeout=180s
go test -race ./... -count=1 -timeout=180s
node --check contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test
git diff --check
```
