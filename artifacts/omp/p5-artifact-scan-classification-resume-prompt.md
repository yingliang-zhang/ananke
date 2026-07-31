Resume the exact timed-out session after the network outage. Preserve all edits. Do not rediscover or redesign, and do not run the real-provider canary, edit the ledger/wrapper, commit, push, reset, clean, or restore.

The session already completed RED, production implementation, focused GREEN, focused `-count=10`, an initial focused race, fixed the timeout-classification regression, reran the affected timeout test, final focused GREEN, and final focused `-count=10`.

Run only the remaining post-fix gates:
1. final focused race `-count=3` for the four classification/journal/canary tests;
2. `go test ./internal/trustedsupervisor`;
3. `go vet ./internal/trustedsupervisor`;
4. scoped `gofmt -d` for files you changed;
5. `git diff --check`.

If a gate fails, fix only the demonstrated regression and rerun that focused test plus the affected gate. Then return the concise final report with exact RED/GREEN/gate commands/results, changed files, exhaustive role/reason mapping, and residual risk. Do not call additional tools after the five gates are green; synthesize immediately.