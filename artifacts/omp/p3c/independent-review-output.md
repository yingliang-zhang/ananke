Working...
## CHANGES REQUESTED

**Blocking: aggregate recovery drops fail-closed actions on store corruption.**

- `recoverAll` promises one safe obligation per independently durable active claim, but delegates to `Store.ListLaunchRecoveryBoundaries` and returns `nil, err` unchanged on any list failure.  
  - `internal/lifecycle/fenced_launch.go:108-124`
  - `internal/store/launch_admission.go:1323-1358`
- `ListLaunchRecoveryBoundaries` gathers hashes, then aborts with `return nil, err` when one `GetLaunchRecoveryBoundary` detects corruption (`:1350-1354`).
- Result: one corrupt active launch produces **no** `WaitingForHuman` action for that launch and suppresses safe recovery actions for every unrelated valid launch. This violates the fail-closed aggregate-recovery requirement; it is neither a `waiting_for_human` result nor an actionable safe boundary.
- Existing coverage misses this: `recoverAll` is tested only on the healthy path (`fenced_launch_test.go:81-88`); corrupt-state coverage exercises only single-launch `recover` (`:182-213`). There is also no direct evidence-intent recovery test; terminal and evidence share the production condition, but evidence behavior is unproven.

**Required correction**

1. Preserve the launch-spec identity and cause for each malformed active record during aggregate recovery.
2. Make `recoverAll` continue valid boundaries and emit `WaitingForHuman: true` for corrupt/unknown ones—never return a nil action set that hides the affected launch.
3. Add aggregate tests with at least one valid active launch plus one corrupt active launch, asserting:
   - valid launch retains its exact retry action;
   - corrupt launch has the exact hash, `WaitingForHuman`, and `ErrLaunchRecordCorrupt`;
   - no modeled terminal/evidence/process outcome is inferred;
   - `runs` remains empty.
4. Add the corresponding evidence-intent fail-closed test.

**Verified strengths**

- P3b persists the three intended atomic boundaries: claim → `pending_materialization`; trusted materialization → `pending_run_admission`; modeled created Run intent → `pending_process_admission`.
- Full active fence validation is enforced in P3b writes; same-generation/wrong-token and lower-generation stale writes are rejected.
- P3c validates active claim identity for admission reconnects and validates materialization/Run identity before exposing the next action.
- Single-launch unknown/corrupt recovery becomes `WaitingForHuman`; terminal/evidence presence is rejected without inferring an outcome.
- Reviewed candidate contains no process, worktree, adapter, OMP, daemon, Tauri, or UI invocation. P3c/P3b tests assert the real `runs` table remains empty.

**Verification passed**

- `go test ./internal/lifecycle -run '^TestP3C' -count=1`
- `go test ./internal/store -run '^TestP3B' -count=1`
- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `go test -race ./internal/lifecycle -run '^TestP3CFencedLaunchReconnectsConcurrentStagesAndRejectsStaleFence$' -count=50`
- P1a → P1c → P2a → P2c → P3a contract verifier chain.
