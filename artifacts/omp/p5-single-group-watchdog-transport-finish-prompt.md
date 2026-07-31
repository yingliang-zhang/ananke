Continue the P5 v2 single-group/watchdog-FD repair from exact session `019f9f9f-7bd6-7000-81ef-5bbd5494420e` in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`.

Verified current state:
- bootstrap v2 authority and `audit_watchdog_transport_test.go` landed;
- focused `AtomicRuntime|Bootstrap|Watchdog|ProcessGroup|Residual` passes once (`6.400s`);
- production `audit_executor.go` has NO `ExtraFiles`, watchdog wait pipe, or FD3 lifecycle (`search ExtraFiles/watchdogWait` found zero);
- therefore the real canary would run without FD3, the bootstrap would fall back to external `/bin/sleep`, and the structural residual would remain;
- gofmt has 1189 bytes drift.

This is not done. Implement the missing production transport, not just formatting:
1. Before sandbox start create a second dedicated `os.Pipe` for the watchdog delay. It is separate from frozen-wrapper stdin.
2. Set `command.ExtraFiles` to exactly the watchdog reader so the child receives exact FD 3. No other inherited files.
3. Close the parent copy of the watchdog reader immediately after successful `StartCommand`; on start failure close reader+writer before return.
4. Keep the watchdog writer open and unwritten until the verified process group is gone / invocation returns, then close it. Close both ends exactly once on every prestart, credential, hook, start, identity, cancellation, timeout, cleanup, concurrent Close, and normal path. Do not allow a close-before-group-gone race.
5. Add production-integration tests that fail RED without this wiring:
   - StartCommand hook observes exact one `ExtraFiles` entry mapped to child FD3 and returns an injected error; all pipe FDs close and no leak;
   - real modeled invocation through production `executeAuditInvocation` with early fake OMP exit confirms prompt group absence promptly and no watchdog sleep child;
   - success, start failure, prestart failure, cancellation/timeout, and concurrent Close descriptor accounting;
   - command descriptor/runtime authority field/hash drift and old v1 rejection remain.
6. If the existing `StartCommand func(*exec.Cmd,*os.File,*os.File)` seam remains, use `command.ExtraFiles` for inspection; do not add an unbound caller-selected FD seam. A test hook that returns nil without starting is invalid; existing injected error hook must remain supported.
7. Run gofmt on every touched P5 file, focused count=1 and count=10, race count=3, full trustedsupervisor single, vet, diff-check. No provider call, external wrapper edit, P6 edit, or commit.

Return exact changed files and gate output. Do not create cron jobs.
