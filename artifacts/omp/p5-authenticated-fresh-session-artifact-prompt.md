# P5 post-Option-A provider-free repair — authenticated fresh OMP session artifact

Work in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` on `feat/task-proposal-core`.

The workspace is intentionally very dirty and contains accepted P5/P6 candidates. Preserve every existing change. Do not reset, clean, restore, commit, push, run a real provider canary, edit the ledger, or edit the external Hermes wrapper.

## Verified failure

After Option A and all focused/repeat/race/full/vet/format/tagged gates passed, one exclusive real-provider canary crossed `prepared -> running` but failed before any provider dial:
- terminal class: `direct_omp_or_capture_verification_failed_working_only`
- transport: lookups=1, dial_attempts=0, dial_successes=0
- test 16.80s / wall 19.20s

Source tracing shows `runAuditInvocation` writes captured stdout and then calls `scanAuditInvocationWritableTrees`. The scan rejects any non-timeout session JSONL containing `invocation.WorkDir` via `auditBytesLeakAuthority`. Real OMP session JSONL necessarily binds its exact CWD and user prompt. Fake OMP tests do not currently create a standard session JSONL, so they missed this boundary. The canary cleanup scrubbed the private tree before the diagnostic hook could inspect it; do not preserve raw canary artifacts.

## Task — strict RED→GREEN

1. Read only the relevant scanner/session parser/runtime/fake-OMP regions and existing tests first.
2. Add a provider-free RED test that creates a fresh OMP-style session JSONL directly under the exact invocation SessionDir with:
   - valid fresh session UUID;
   - exact physical snapshot WorkDir as CWD;
   - exact current prompt as the user message;
   - expected private invocation-owned path references.
   The existing scanner must reject this before the production fix, proving the gap.
3. Add denial vectors proving the exception remains closed: malformed/non-JSONL/symlink/special or nested artifacts, wrong UUID/CWD/prompt, credential bytes, original repository path, wrapper path, protected path, stale/foreign paths, too many/oversized artifacts all remain rejected.
4. Implement the narrowest authenticated fresh-session exception. Reuse `parseAuditSessionHeader`; do not accept arbitrary `.jsonl`. Permit only exact invocation-owned ephemeral path references after authenticating the session header. Continue rejecting credentials, original repository/wrapper authority, protected paths, unknown paths, special files, symlinks, file/byte limit overflow. Timeout/resume session behavior must remain unchanged.
5. Add/extend a fake-OMP execution test proving an authenticated fresh session plus nonzero OMP exit reaches the existing `direct_omp_exit_nonzero` handling rather than `direct_omp_or_capture_verification_failed`; malformed/leaking sessions must fail closed before that state.
6. Do not broaden sandbox read/write/process authority and do not change the accepted CLT Git v7 policy/descriptor contract.
7. Run the exact RED before implementation and preserve its command/result. After GREEN run focused scanner/session/runtime tests once, then `-count=10`, focused `-race -count=3`, full `internal/trustedsupervisor`, `go vet ./internal/trustedsupervisor`, scoped `gofmt -d`, and `git diff --check`. Do not run the real provider canary.

Return exact commands/results, files changed, and residual risks. If source tracing disproves the stated root cause, stop after a bounded diagnostic and report the corrected cause instead of forcing this design.