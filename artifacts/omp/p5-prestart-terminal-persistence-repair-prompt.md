Fix the P5 audit executor stuck-prepared production bug in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` with strict TDD. No commit. Do not edit P6 repaircontract files.

Real evidence:
- Third canary safe timeline after 182.51s: exactly one event, `sequence=1,state=prepared,attempt=1`; no `running`.
- A 45s Go timeout goroutine dump showed the test polling and the DB opener only; the audit executor run goroutine had already exited.
- The preserved signed SQLite journal contains only prepared.
- `audit_runtime.go:357-376` handles a safe pre-start `runErr`, cleans artifacts, creates `auditStateFailed`, and discards `_ = executor.appendEvent(...)`.
- `audit_journal.go:475-482` forbids `prepared -> failed`; prepared allows only running/cancelled/waiting_for_human.
- Thus the invalid terminal append is silently discarded and the execution is stranded prepared. This is a production state-machine bug, not a canary budget issue.

Required correction:
1. For any `runAuditInvocation` error while the latest durable event is still `prepared` (no durable running identity), persist a legal terminal `waiting_for_human` with the precise closed failure class and zero effect. Do not weaken the transition table to allow an unauthenticated/identity-ambiguous failed state unless a contract test proves that is safer.
2. If latest state is `running` and termination/cleanup are confirmed safe, existing failed behavior may remain.
3. Fix the class, not only this site:
   - audit every `_ = executor.appendEvent` and void `appendWaiting` in `audit_runtime.go`;
   - no required terminal append may be silently ignored;
   - change helpers to return errors and propagate/record them through the executor close/recovery surface so a persistence failure is observable and cannot look like a successful completed run;
   - where a legal `waiting_for_human` fallback can be persisted, use a closed failure class; if journal persistence itself fails, surface the error through `executor.Close()` and leave recovery to signed-journal restart logic—never fabricate an in-memory terminal state.
4. Add tests for at least these post-prepared/pre-running failures: runtime-authority verification, provider gateway setup, transport binding, start gate/cancellation, and command start/capture failure. Each must end in a durable signed `waiting_for_human` (or cancelled for a real cancellation), never prepared-only, and must perform zero provider/process effect when failure is pre-start.
5. Add a regression test reproducing the exact old sequence: prepared persisted, pre-start runErr, illegal failed append would fail; new code persists waiting and executor goroutine/Close reports no hidden error.
6. Add a forced terminal journal append failure test proving the persistence error is observable via executor close or an equivalent existing signed recovery surface. Ensure server/recovery semantics remain fail-closed.
7. Preserve all accepted P5 contracts, cancellation semantics, process identity continuity, signed event validation, cleanup authority, and no raw error/credential/path leakage in failure classes.
8. The canary may retain the two-stage wait and safe timeline, but do not simply enlarge prelaunch budget. No real provider call in this repair.

Verification:
- focused new tests RED then GREEN;
- relevant audit runtime/journal/cancellation tests count=10 and race count=3;
- full trustedsupervisor single;
- go vet, gofmt, diff-check.
Return exact commands/results and unresolved obligations. Do not create cron jobs.
