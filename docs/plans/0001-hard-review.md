Hard review of Ananke vertical slice 1: supervisor lifecycle proof.

You are an independent reviewer. Your job is to find BLOCKER, MAJOR, and MINOR issues in the Ananke supervisor lifecycle implementation. Do NOT modify any files. Read the ADRs and then audit the code.

## Authority documents

Read these completely:
- `docs/adr/0001-use-go-for-core-and-bootstrap.md`
- `docs/adr/0002-supervisor-lifecycle-identity-model.md`
- `docs/adr/0003-cleanup-state-machine-and-finalization-outbox.md`
- `docs/plans/0001-supervisor-lifecycle-proof.md`

## What to review

All Go source under `internal/` and `cmd/`, plus `scripts/`.

## Review checklist

For each item, cite the file:line and state PASS or FAIL with evidence:

### ADR-0002: Lifecycle identity
1. Does the supervisor call setpgid(0,0) at startup? Is it the group leader?
2. Does the worker inherit the supervisor's PGID (no self-setpgid)?
3. Is ReapWorker called AFTER cleanupGroup (deferred reap)?
4. Is there any kill(-pgid, ...) after the worker is reaped?
5. Are SIGKILL targets individual PIDs (not group)?
6. Does the supervisor install a SIGTERM handler that ignores SIGTERM?
7. Is the identity file written atomically before worker launch?
8. On daemon restart, is identity verified before reconnecting?
9. On supervisor crash, does the system enter recovery_unknown (not guess)?

### ADR-0003: Cleanup state machine + outbox
10. Are terminal states (completed/failed/cancelled) only reachable after authenticated quiescence?
11. Is cleanup_required nonterminal and does it stay in ListActiveRuns?
12. Is recovery_unknown nonterminal?
13. Is the finalization outbox row inserted in the same SQLite transaction as the terminal state?
14. Does startup reconcile pending outbox rows?
15. Is cancellation asynchronous (returns accepted immediately)?
16. Does transcript corruption while worker alive go to cleanup_required (NOT failed)?

### General
17. Are all tests bounded with deadlines (no fixed sleeps masking failures)?
18. Are mutation gates testing the right invariant (not just compile)?
19. Are there any race conditions in the concurrent paths?
20. Does the engine's tick loop handle all nonterminal states correctly?

## Output format

For each finding:
```
### [BLOCKER|MAJOR|MINOR] N: Title
File: path:line
Evidence: ...
Why it matters: ...
Suggested fix: ...
```

End with:
```
## Verdict
BLOCKER: N
MAJOR: N
MINOR: N
```

Be thorough. Read every file. Trace every code path. Do not skip anything.
