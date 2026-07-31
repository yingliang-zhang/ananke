# P5 exact Git startup boundary — narrow TDD implementation

Work in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` on branch `feat/task-proposal-core`.

The workspace is intentionally very dirty and mixes accepted/in-progress P5 and P6 work. Preserve every existing change. Do not reset, clean, restore, delete unknown artifacts, commit, push, or edit P6 files. Do not run a real-provider canary. Do not modify the external Hermes wrapper.

Read first:
- `docs/experiment-ledger.md` around lines 2274–2324
- `internal/trustedsupervisor/audit_executor.go`
- `internal/trustedsupervisor/audit_executor_test.go`
- `internal/trustedsupervisor/audit_direct_runtime_test.go`
- `internal/trustedsupervisor/audit_wrapper_compatibility_test.go`
- `internal/trustedsupervisor/execution_policy.go`
- `internal/trustedsupervisor/testdata/fakeomp/main.go`

Current evidence: real direct pinned OMP reaches `prepared -> running` but stops before first trusted-gateway dial. Closed telemetry is `lookups=1`, `dial_attempts=0`, `dial_successes=0`. Sandbox evidence shows denied exact `/usr/bin/git` startup and snapshot-parent repository discovery. `executionPolicyEntry.GitExecutable` is already identity/hash/owner/mode pinned and revalidated.

Implement only this hypothesis using strict RED -> GREEN. Do not stack a second speculative permission.

Required behavior:
1. First add and run a focused RED test that proves the current audit sandbox cannot execute the policy-pinned Git startup/repository-discovery path.
2. In `auditSandboxProfile`, add only `entry.GitExecutable.Path` beside the pinned OMP path for exact literal `file-read*`, `file-map-executable`, and `process-exec` authority. Do not add `/usr/bin` subpath authority. Do not allow `/bin/bash`, `/bin/sh`, arbitrary test commands, or caller-selected executables.
3. Add fixed child environment boundaries:
   - `GIT_CEILING_DIRECTORIES=<invocation.WorkDir>`
   - `GIT_CONFIG_GLOBAL=/dev/null`
   - `GIT_CONFIG_NOSYSTEM=1`
4. Add executable coverage proving Git cannot discover a repository above the isolated snapshot, cannot use user/global/system config, cannot mutate refs/source, and shell/arbitrary-executable authority remains denied. Prefer the existing native fake OMP fixture and real `/usr/bin/git` path rather than mocks.
5. Update exact environment-name/value tests, command descriptor/hash tests, sandbox profile tests, mutation tests, and runtime/release authority fixtures only where the fixed Git boundary contract requires it. If the command descriptor contract changes, bump its schema version and bind the fixed Git executable/environment-discovery policy explicitly.
6. Preserve exact runtime ancestor directory read-data rules, native write/fallback denials, timeout/recovery/cancellation behavior, credential projection, and the direct pinned-OMP initial Seatbelt target.
7. Keep code/comments/tests in English and gofmt touched Go files.

Run and report:
- the exact RED command and expected failure;
- focused GREEN tests covering Git sandbox authority + env + direct runtime + credential projection, normally `-count=1` first;
- a focused repeat (`-count=10`) if the first GREEN passes;
- `git diff --check` and scoped gofmt check.

Do not run the full package, race suite, vet, or canary; the orchestrator will run final gates after reviewing your narrow patch. Return changed files and exact command results, including any residual risk.