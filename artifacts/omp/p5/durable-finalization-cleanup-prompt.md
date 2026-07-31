Repair fourth-review blocker 5 only. Read report lines 181-211 and current audit_runtime/audit_journal/audit_evidence recovery paths. Strict TDD, no provider/model, no commit; preserve blocker-4 authority.

Implement a durable signed nonterminal finalization state (prefer state name `finalizing`) rather than losing verified evidence across crash:
1. Child and supervisor tests finish; collect and validate evidence; reverify output.
2. Append authenticated `finalizing` event containing the immutable evidence fields/hash and all authority needed for restart. Do not expose callback.
3. Cleanup every owned root: prompt, output, session, temporary, work/source snapshot, wrapper/transport state and all attempts. Verify absence using descriptor-safe cleanup.
4. Only after successful verified absence append authenticated `completed` event preserving exact evidence binding. A completed callback is impossible while any owned root remains.
5. On restart, a valid finalizing history resumes cleanup idempotently, verifies absence, then appends completed. Do not rerun OMP/provider/test after finalizing. Tampered/unsigned finalizing history fails closed.
6. Cleanup failure remains nonterminal/durable and restart-retryable; never append completed or expose signed success. Avoid a hot retry loop.

Update closed state/transition/history validation and event signing. Completed callback validation must require all owned roots absent, not only output mismatch/dir absent. Include multi-attempt shared session/root cleanup.

Deterministic tests:
- pause after finalizing persistence before cleanup: reconcile remains nonterminal even with output unchanged;
- inject cleanup failure independently for each owned root: no completed callback/event;
- crash/stop after finalizing persistence, restart: cleanup resumes then completed; no child/provider rerun;
- crash after cleanup before completed: restart verifies absence then completes;
- signed finalizing tamper, invalid transition, replay fail closed;
- count=10, race=3, full package.

Do not implement blocker 6. Report exact schema/state changes and gates.