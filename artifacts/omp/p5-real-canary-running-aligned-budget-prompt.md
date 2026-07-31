Repair the real-provider canary lifecycle alignment in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/internal/trustedsupervisor/audit_real_provider_canary_test.go` with TDD. No commit and no real provider run.

Observed after first context split:
- setup context completed and lifecycle began immediately before `executor.Notify`;
- lifecycle budget was 540s OMP + 5s wrapper grace + 120s margin = 665s;
- canary still failed `lifecycle_timeout` at 667.09s.

Root architectural issue: after Notify, the executor still performs snapshot materialization and invocation preparation before it appends the durable `running` event and starts `runAuditInvocation`'s 545s process hard deadline. A Notify-based budget remains misaligned with the process deadline; a larger single margin only hides this.

Required design:
1. Keep the existing bounded pre-Notify setup context.
2. Replace the single Notify-based execution context with two explicit stages:
   - **prelaunch stage**: after `Notify`, wait with a closed positive budget (e.g. 180s, bounded by a documented maximum) until the signed journal has either a `running` event or a terminal event. If terminal appears first, return it. If no running/terminal appears, fail as `prelaunch_timeout`.
   - **running stage**: only after observing the durable `running` event, start a new context equal to `InternalDeadlineSeconds + WrapperGraceSeconds + a bounded terminal-persistence margin` (e.g. 60s). Wait for completed/failed/cancelled/waiting_for_human. If it expires, fail as `running_terminal_timeout`.
3. Do not treat `timed_out` as final; continue waiting for the required attempt-cap terminal state. Preserve `sawTimeout`.
4. Handle the race where running and terminal events are already both present in one journal load.
5. Add a bounded safe timeline used only on failure. It may include only event sequence, state, attempt, occurred_at, and failure_class. It must never include PID/paths/session IDs, evidence, stdout/stderr, model output, hashes, repository/wrapper paths, or credentials. Limit count and bytes. Add tests proving sensitive fixture fields do not appear.
6. Distinguish setup timeout, prelaunch timeout, running-terminal timeout, journal wait error, and executor close error. Capture the safe timeline before close/scrub and emit it with `t.Logf`, while the fatal message remains a closed failure code.
7. Budget invariant tests must assert exact derivations and bounds. Remove the old Notify-based execution budget/margin so no ambiguous 665s contract remains.
8. Preserve repository immutability, cleanup, credential handling, policy deadlines, build tags, and fail-closed behavior. Do not edit production executor/runtime code in this repair.

TDD/verification without provider call:
- focused tests for budget derivation, stage-wait state machine using a temporary signed journal or pure deterministic helper, terminal-before-running, running+terminal same load, `timed_out` followed by waiting-for-human, prelaunch/running deadline classification, and timeline redaction/bounds;
- build-tagged compile with tags `ananke_real_provider_canary ananke_test_runtime_authority` and `-run '^$'`;
- ordinary focused trustedsupervisor tests;
- tag-compatible vet, gofmt, diff-check.

Return exact changes/results and state that real-provider execution remains unverified pending rerun. Do not create cron jobs.
