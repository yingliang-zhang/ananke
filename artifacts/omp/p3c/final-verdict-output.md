Working...
ACCEPT

- `recoverAll` applies `isFencedLaunchHumanIntervention`: only missing claim, missing launch spec, and corrupt authority records become exact-hash `WaitingForHuman`; `context.Canceled` and injected non-authority read failures return unchanged with no action list.
- Aggregate storage returns one `LaunchRecoveryResult` per active hash. Mixed valid/corrupt recovery retains the valid materialization-retry action and isolates the corrupt hash.
- Evidence intent remains fail-closed: validation returns `errFencedLaunchOutcomeUnknown`; no terminal/evidence/process outcome is inferred.
- Recovery path performs storage reads only (`List…` → query/get/load); no Run/process/worktree path is invoked. Targeted tests assert zero `runs`.
- Passed: targeted `go test ./internal/lifecycle ./internal/store … -count=1`.
- Passed: `git diff --check` (no output).
