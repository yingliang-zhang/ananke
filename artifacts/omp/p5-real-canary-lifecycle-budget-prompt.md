Fix only the real-provider canary lifecycle-budget bug in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/internal/trustedsupervisor/audit_real_provider_canary_test.go` using TDD. No commit. Do not edit production files or the accepted P6 contract.

Observed failure: canary returned `lifecycle_deadline_or_exit` at about 570.46s. Current constants are overall context 570s, OMP internal 540s, wrapper grace 5s. The 570s context starts before repository capture, runtime copying, policy/signing/journal/executor setup. The executor hard deadline starts only after process spawn at 545s. Therefore setup/queue time consumes the 25s margin and the harness context can cancel before the executor's own hard timeout/terminal persistence.

Required correction:
1. Do not merely increase one global constant.
2. Split bounded phases:
   - a dedicated setup context/budget for repository baseline and policy/runtime/signing/journal construction;
   - cancel setup context after intent persistence is complete;
   - create the execution/lifecycle context immediately before `executor.Notify`/wait;
   - execution budget must be derived from `realProviderCanaryOMPDeadline + realProviderCanaryWrapperGrace` plus an explicit bounded queue/preparation/terminal-persistence margin (recommend 120s unless tests justify another value);
   - keep the existing separate 20s post-state context.
3. Ensure any setup timeout, lifecycle timeout, executor close error, and terminal state remain distinguishable without printing credentials or raw provider output.
4. Add a small testable budget invariant under the canary build tag: execution budget must exceed OMP+grace by the exact documented margin, setup budget positive/bounded, and no overflow/implicit magic 570 remains.
5. Preserve fail-closed behavior, repository immutability, cleanup, build tags, credential handling, and all policy values. Do not lower OMP deadline or wrapper grace.
6. Do not run the real provider call. Verification is compile/tests only:
   - `gofmt`;
   - build-tagged focused invariant test;
   - build-tagged compile with `-run '^$'` and tags `ananke_real_provider_canary ananke_test_runtime_authority`;
   - ordinary trustedsupervisor focused tests sufficient to ensure no compile regression;
   - `go vet` where tag-compatible and `git diff --check`.

Return exact changes/results and state clearly that real-provider runtime is still unverified until a later canary execution. Do not create cron jobs.
