Resume this exact P5 scanner-classification session. Do not rediscover or redesign broadly. Preserve all dirty work; no real-provider canary, ledger/wrapper edit, commit/push, reset/clean/restore.

New verified live evidence after the prior gates:
- globally quiet preflight PASS
- canary failed closed as exactly `artifact_scan_temporary_authority`
- lookups=1, dial_attempts=0, dial_successes=0
- raw trees were correctly scrubbed

Narrow TDD task: safely refine only the generic authority branch so the next terminal class identifies the closed temporary sublocation and closed authority kind without exposing raw values.

Requirements:
1. Keep `auditBytesLeakAuthority` behavior unchanged for existing callers, but factor a closed classifier returning no raw bytes.
2. Closed authority kinds must exhaust the current match set and precedence: secret, repository, wrapper, prompt_root, output_root, session_root, work_root, prompt_path, output_path, session_path, work_path.
3. For artifacts under `TemporaryDir`, classify closed location from the current walked path as exactly agent-owned (`AgentDir`), home-owned (`HomeDir`), temporary-root, or other-temporary. Never include basename/relative path.
4. Persist a protocol identifier such as `artifact_scan_temporary_authority_home_work_path` (choose one fixed ordering and test it). For non-authority reasons/classes preserve exact prior strings. Invalid/open kind/location must fail closed with no raw cause in `Error()`.
5. Add exhaustive vocabulary/precedence tests and one fake-OMP journal integration test that writes only a temporary authority artifact and proves the exact terminal class. Do not weaken session/fresh/secret/protected checks.
6. Keep timeout/resume semantics unchanged. Do not alter CLT Git v7, schemas, sandbox authorities, or accepted artifact policy.
7. Run strict RED, focused GREEN, `-count=10`, focused race `-count=3`, full `internal/trustedsupervisor`, vet, scoped gofmt-diff, and git diff check. Do not run canary.

If implementation and evidence are already complete, synthesize immediately without extra tool calls. Report exact commands/results and changed files.