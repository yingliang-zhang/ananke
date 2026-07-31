# P5 Option A — root-owned Command Line Tools Git authority

Work in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` on branch `feat/task-proposal-core`.

The workspace is intentionally very dirty and mixes P5/P6 slices. Preserve every existing change. Do not reset, clean, restore, delete unknown files, commit, push, edit P6 files, edit the experiment ledger, run a real-provider canary, or modify the external Hermes wrapper.

Operator direction is now explicit: implement Option A. Replace the xcselect forwarding identity `/usr/bin/git` with the exact root-owned concrete Git executable `/Library/Developer/CommandLineTools/usr/bin/git`. Keep one exact Git executable authority; do not model an xcselect launcher chain.

Read the current files and current uncommitted diff first, especially:
- `docs/experiment-ledger.md:2497-2515`
- `internal/trustedsupervisor/execution_policy.go`
- `internal/trustedsupervisor/execution_policy_test.go`
- `internal/trustedsupervisor/audit_executor.go`
- `internal/trustedsupervisor/audit_executor_test.go`
- `internal/trustedsupervisor/audit_direct_runtime_test.go`
- `internal/trustedsupervisor/audit_wrapper_compatibility_test.go`
- `internal/trustedsupervisor/audit_snapshot_test.go`
- `internal/trustedsupervisor/audit_real_provider_canary_test.go`
- `internal/trustedsupervisor/testdata/fakeomp/main.go`
- `docs/local-trusted-supervisor-transport-runbook.md`

Verified root cause and RED:
- The current focused test `TestDarwinDirectOMPSandboxExecutesPinnedGitWithoutParentRepositoryDiscovery` is RED at closed stage `05PE`.
- Exact `/usr/bin/git` starts but `rev-parse` exits 1/EPERM because `/usr/bin/git` is an xcselect forwarding binary.
- `/Library/Developer/CommandLineTools/usr/bin/git` exists, is a real 3,837,392-byte arm64 Mach-O, UID/GID 0/0, mode 0755.
- Active Xcode-beta Git is UID 501 and must NOT be admitted.

Follow strict TDD. Do not weaken or delete the existing RED.

Required implementation:
1. First add/run focused RED policy tests that require only the exact root-owned CLT Git identity and reject `/usr/bin/git`, active Xcode/Xcode-beta Git, mutable/user-owned Git, symlinks, hash/inode/device/owner/mode drift, and missing CLT Git. Record the exact RED result in your final report.
2. Change the production Git identity contract to `/Library/Developer/CommandLineTools/usr/bin/git`. Reuse existing root-owner/device/inode/hash/mode validation and every-effect revalidation; do not add a caller-selected path or environment override.
3. Update child executable lookup so real OMP resolving command name `git` selects the pinned CLT Git first. Use an exact fixed PATH such as `/Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin`, bind this value into the command descriptor/environment contract, and assert ambient PATH cannot override it. Do not add `DEVELOPER_DIR`, `GIT_EXEC_PATH`, Xcode paths, `/Library/Developer/CommandLineTools` subpath authority, or any broad read/exec rule.
4. Keep sandbox authority to only exact literals `entry.OMPExecutable.Path` and `entry.GitExecutable.Path` for file-read/file-map/process-exec. Explicitly prove `/usr/bin/git`, `/usr/bin`, `/bin/sh`, `/bin/bash`, `/usr/bin/env`, an arbitrary test executable, active Xcode Git, and another CLT executable are not executable/read-map authorities.
5. Make the fake OMP Git probe use the policy-selected Git path (prefer command-name `git` plus the fixed PATH and an embedded expected exact path), not hardcoded `/usr/bin/git`. Bind the expected path into the compiled fixture from `entry.GitExecutable.Path`; reject caller mismatch.
6. Bump every affected unaccepted contract consistently: execution-policy/document schema and command descriptor schema. Update runbook/schema/hash/environment tests and exact fixtures. Do not invent backward migration for this uncommitted candidate; stale schemas must fail closed.
7. Make the original tight test GREEN and ensure it executes all blocked probes: parent repo non-discovery, global/system/ambient command config ignored, source and parent HEAD immutable, shell/arbitrary/active-Xcode/other-CLT executable denied.
8. Update all hardcoded `/usr/bin/git` usages in active P5 policy/canary/test fixtures to the production constant where they represent the trusted Git. Do not rewrite historical artifacts or rejected docs.
9. Keep P5 read-only and retain all existing OMP/native/network/credential/timeout/recovery boundaries.

Verification scope for this delegation only:
- Run the exact policy RED, then GREEN.
- Run the original tight Git startup test GREEN.
- Run the focused direct invocation/environment/profile/descriptor/policy/snapshot/canary-build fixture tests once.
- Run that focused set `-count=10` only after single-pass GREEN.
- Run scoped gofmt verification and `git diff --check`.
- Do NOT run race/full package/vet/tagged real-canary execution; the orchestrator will run final gates.

Return exact commands/results, files changed, schema versions, and residual risks. Never claim canary/provider/audit success.