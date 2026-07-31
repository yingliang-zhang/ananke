# P5 bounded artifact-scan failure classification

Work in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` on `feat/task-proposal-core`.

Workspace is intentionally dirty. Preserve all changes. Do not reset/clean/restore, edit the ledger or external wrapper, commit/push, or run a real-provider canary.

## Verified live failure

After the authenticated fresh-session repair and all provider-free gates passed, a globally quiet canary still crossed `prepared -> running` and failed before any provider dial:
- `direct_omp_or_capture_verification_failed_working_only`
- lookups=1, dial_attempts=0, dial_successes=0
- 14.30s test / 18.24s wall

The current generic failure hides whether the rejected artifact was prompt/output/session/temporary and why. Raw artifacts are correctly scrubbed before the test hook can inspect them. Do not preserve or log raw content/path/PID/session/credential data.

## Narrow task — TDD

1. Read only `scanAuditInvocationWritableTreesExcept`, its direct call in `runAuditInvocation`, `classifyAuditRunFailure`, scanner tests, and canary safe-timeline validation.
2. Add strict RED tests for a typed, bounded scanner failure classification. Required output is a protocol identifier containing only closed role + reason, for example `artifact_scan_session_fresh_authentication`; never a path, filename, content, PID, UUID, hash, secret, or unbounded error string.
3. Cover every scanner rejection branch with a closed role (`prompt`, `output`, `session`, `temporary`, or `unclassified`) and closed reason (`walk`, `symlink`, `special`, `limit`, `read`, `timeout_secret`, `fresh_authentication`, `fresh_authority`, `authority`, `protected`, or equivalent exhaustive enum). Preserve `errors.Is(..., ErrAuthentication/ErrLimit)` through wrapping.
4. At the `runAuditInvocation` writable-tree scan boundary, convert only this typed scanner error into `auditInvocationStageError` with the closed class. `classifyAuditRunFailure` must then persist the specific class instead of `direct_omp_or_capture_verification_failed`. Unrelated errors and timeout/resume semantics stay unchanged.
5. Extend the canary diagnostic allowed-class contract only as needed for the closed persisted class; do not expose raw data.
6. Add one integration test showing a leaking fake fresh session becomes the exact closed scanner class in the terminal journal event, while authenticated fresh session/nonzero exit remains `direct_omp_exit_nonzero`.
7. Do not alter the accepted CLT Git v7 contract, sandbox authorities, fresh-session acceptance logic, policy schema, or evidence schema.
8. Run exact RED before production change; after GREEN run focused once, `-count=10`, focused race `-count=3`, full `internal/trustedsupervisor`, vet, gofmt-diff, and git diff check. Do not run the real canary.

Return exact commands/results, changed files, exhaustive class mapping, and residual risk. Keep the implementation small; this is diagnostic classification, not another artifact-policy redesign.