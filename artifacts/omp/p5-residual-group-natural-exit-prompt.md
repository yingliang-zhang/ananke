Implement a conservative P5 residual-process-group exit proof in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` with strict TDD. No commit. Do not edit P6 repaircontract files. No real provider call.

Observed fifth real canary:
- DNS preflight: transparent fake-IP accepted.
- signed timeline: prepared -> running -> waiting_for_human(group_exit_unconfirmed).
- running at 17:31:38.480590Z; waiting at 17:31:40.739783Z; test ended 12.20s with executor_close_error.
- no sandbox/OMP/test process remained when checked after completion.

Root mechanism:
- the verified process-group leader exits and the `auditProcessWaiter` reaps it;
- `confirmAuditProcessExit` then sees leader absent but a short-lived same-PGID descendant remains beyond the current 750ms KillGrace;
- current code fails closed before collecting wrapper exit code/stdout/stderr, so the actual early wrapper failure stays hidden;
- the residual group disappears naturally shortly afterward.

Safety decision:
- DO NOT signal a process group after its leader has already been reaped. There is a PGID reuse TOCTOU between a liveness check and group signal.
- Preserve existing TERM->KILL behavior only while the exact leader start identity is still inspectable and matches.
- Add a separately named bounded natural residual-group observation grace for the verified-leader-exited path. It performs GroupExists polling only, emits no signal, and confirms exit only when the group is actually absent.

Required implementation:
1. Extend `auditTerminationBounds` with `ResidualExitGrace` (or equivalent), validated as positive and bounded. Set a practical production default of 5 seconds, separate from TermGrace/KillGrace.
2. After `waiter.await` proves the exact originally launched leader has exited (normal or `*exec.ExitError`) and `Inspect(expected.PID)` returns absent:
   - if group absent: confirm immediately;
   - if group present: poll `GroupExists(expected.PGID)` only until residual grace;
   - if it disappears: confirmed exit;
   - if inspection errors/context ends/deadline expires: retain typed failure (`group_inspection_failed` or `group_exit_unconfirmed`).
   - never call SignalGroup from this path.
3. Update `confirmAuditProcessExit`, `joinUnconfirmedAuditInvocation`, executor Close bounds, and all bound validation/copying to account for the new grace without making cancellation or shutdown unbounded.
4. Keep wrong/reused PID rejection before any signal. Keep live verified-leader cancellation TERM->KILL unchanged.
5. Add RED/GREEN tests with deterministic fake operations and at least one Darwin real-process test:
   - reaped leader + group naturally disappears within residual grace -> confirmed, zero signals;
   - reaped leader + group remains -> bounded group_exit_unconfirmed, zero signals;
   - group inspection failure -> typed failure, zero signals;
   - waiter incomplete/error -> no residual proof and no signal;
   - live matching leader still TERM->KILL as before;
   - wrong/reused leader still no signal;
   - Close budget includes residual grace and concurrent Close callers remain deterministic;
   - real process group: leader starts a short-lived child then exits; confirmation waits for child and succeeds without signaling.
6. Do not expose PID/path/stderr/raw output in canary safe timeline. This change should only let normal result processing proceed after real group exit; it must not log raw wrapper output.
7. Run focused termination/residual/Close tests count=10, race count=3, full trustedsupervisor single, vet, gofmt, diff-check.

Return actual RED/GREEN evidence and changed files. Do not create cron jobs.
