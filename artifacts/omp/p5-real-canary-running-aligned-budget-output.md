Working...
Implemented in `internal/trustedsupervisor/audit_real_provider_canary_test.go` only.

### Changes

- Preserved the existing bounded setup context:
  - setup budget: `2m`
  - documented maximum: `5m`
- Replaced the obsolete Notify-based `665s` lifecycle budget with:
  - prelaunch: `180s`, maximum `180s`
  - running: `540s + 5s + 60s = 605s`
  - terminal-persistence margin: `60s`, maximum `60s`
- After `Notify`, prelaunch now polls the signed journal until:
  - durable `running`, or
  - `completed`, `failed`, `cancelled`, or `waiting_for_human`.
- Starts the `605s` running budget only after durable `running` is observed.
- Handles terminal-before-running and running-plus-terminal in one journal load.
- Scans the complete loaded history for `timed_out`, preserves `sawTimeout`, and does not treat `timed_out` as terminal.
- Added closed lifecycle failure codes:
  - `prelaunch_timeout`
  - `running_terminal_timeout`
  - `journal_wait_error`
  - `executor_close_error`
  - combined executor-close variants
  - existing setup failures remain `setup_timeout_<operation>`.
- Added failure-only safe timeline logging:
  - maximum 32 events
  - maximum 4096 bytes
  - only `sequence`, `state`, `attempt`, `occurred_at`, and `failure_class`
  - captured before executor close and ephemeral scrub
  - fatal output remains only the closed failure code.
- Added deterministic tests for budgets, state transitions, races, timeout handling, deadline classification, close/journal failures, and timeline redaction/bounds.
- Removed the old lifecycle margin/execution-budget identifiers and the ambiguous `665s` contract.

### TDD evidence

Initial focused run failed to compile because the new staged wait symbols were intentionally absent. After implementation, all focused tests passed.

### Verification

Passed:

```text
go test -tags 'ananke_real_provider_canary ananke_test_runtime_authority' \
  ./internal/trustedsupervisor \
  -run '^TestRealProviderCanary(BudgetInvariant|StageWaitStateMachine|TerminalBeforeRunning|RunningAndTerminalInSameLoad|TimedOutWaitsForAttemptTerminal|StageDeadlineClassification|JournalAndCloseFailureClassification|SafeTimelineRedactsSensitiveEventFields|SafeTimelineBounds)$' \
  -count=1
```

```text
go test -tags 'ananke_real_provider_canary ananke_test_runtime_authority' \
  ./internal/trustedsupervisor -run '^$' -count=1
```

```text
go test ./internal/trustedsupervisor -count=1
```

```text
go vet -tags 'ananke_real_provider_canary ananke_test_runtime_authority' \
  ./internal/trustedsupervisor
```

- `gofmt -d`: no output.
- Untracked-file equivalent of `git diff --check`: zero whitespace diagnostics.
- Obsolete budget-name/`665` search: no matches.
- Target remains untracked (`?? internal/trustedsupervisor/audit_real_provider_canary_test.go`).
- No production executor/runtime files changed.
- No commit created.
- No cron jobs created.
- No provider call was made.

Real-provider execution remains unverified pending the requested canary rerun.
