Implement P5 runtime authority v3: direct pinned OMP as the initial Seatbelt target, replacing execution of the Bash wrapper stream inside the sandbox. Work in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. No commit. Do not modify any `internal/repaircontract` or P6 Slice-4 files. Do not modify the external Hermes wrapper at `/Users/yingliangzhang/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh`. Do not print credentials or raw model output. Do not create cron jobs.

Verified real-Darwin blocker and evidence:
- Real canaries now pass prepared/running and process-group cleanup, but exit 1.
- A closed pre-cleanup classifier reports `wrapper_exit_nonzero_code_1_sandbox_denied`.
- A clean 30-second macOS sandbox log window had no forbidden-exec-sugid. The unique likely-fatal deny was Bash `file-write-create` for `sh-thd-<dynamic>` under the shared OS temp parent, outside invocation-owned roots.
- This is Bash 3.2 materializing the frozen wrapper's Python heredoc used by `snapshot_session_paths`.
- Do NOT allow writes to shared `/tmp` or per-user OS temp. That would weaken isolation and permit races/residue.
- Direct `/usr/bin/sandbox-exec ... /opt/homebrew/Cellar/omp/17.1.3/bin/omp --version` succeeds. Nested pinned OMP under Bash also succeeds under allow-default; the fatal issue is the wrapper heredoc temp file, not general nested exec.

Frozen architecture decision:
1. Production sandbox target must be exact pinned `entry.OMPExecutable.Path`, not Bash. The supervisor must natively own route mapping, argv, internal/hard deadline, output capture, session directory, process-group cleanup, cancellation, credential projection, and timeout evidence.
2. Keep verifying/freeze-binding the external wrapper and its exact SHA as a compatibility oracle for route semantics. Do not execute its bytes, bootstrap, stdin pipe, heredocs, `set -m`, wrapper watchdog, or shared-temp behavior in production.
3. Bump atomic runtime authority schema/policy to explicit v3 direct-exec. Bind launcher mode, exact OMP argv policy/model/thinking mapping, direct sandbox target, output transport, and timeout owner. Remove v2 watchdog FD3/bootstrap authority from the production launch path; do not leave a misleading “active” FD3 contract. Legacy artifacts may remain only as clearly named compatibility-oracle fields if still required by frozen release policy.
4. For the currently sealed P5 provider declaration, exact route mapping must match the wrapper oracle:
   - `custom:sudo / gpt-5.6-sol / normal` -> `--model sudo/gpt-5.6-sol --thinking xhigh`
   - invoke OMP print mode with exact prompt bytes, `--yolo`, `--max-time <InternalDeadlineSeconds>`, `--session-dir <isolated session dir>`;
   - exact resume adds `--resume <UUID>`; no `--continue`.
   Reject unsupported provider/model/tier combinations and duplicate/caller-injected route/deadline/session arguments.
5. Update command descriptor to v5 (or next explicit version): bind direct launcher mode, exact derived OMP argv policy, prompt hash, output transport, supervisor hard deadline, wrapper compatibility-oracle hash, pinned OMP/native identities, route, credential source-to-runtime projection, and resume fields. Remove false `private_pipe_stdin`/Bash-interpreter claims.
6. Output must remain a private authenticated `audit-output.json` under the invocation-owned output root, mode 0600/no symlink, with bounded capture. Preserve distinct stdout/stderr hashes and credential leak scanning. If both streams write the combined report file, use a synchronized bounded writer and ensure no race/partial-write authority. Do not place raw model output in logs/journal.
7. Supervisor hard deadline remains `InternalDeadlineSeconds + WrapperGraceSeconds` only as an outer safety bound. Native timeout classification/evidence must no longer require the Bash wrapper to author `[OMP_TIMEOUT]`; define a typed supervisor-authored timeout record bound to exact invocation/session/process identity, and preserve exact-session resume semantics. A natural OMP exit code 124 must not be confused with supervisor timeout without the typed observation.
8. Preserve production fail-closed cleanup, group-exit confirmation, PID start identity, runtime authority lease, broker-only network, readonly source snapshot, native-addon no-write/fallback-deny, provider credential projection (`SUDO_CODING_KEY` source -> child `SUDO_API_KEY`), and no credential/model output leaks.
9. Remove production dependencies that become unnecessary (Bash interpreter, wrapper executable helpers, frozen wrapper pipe, FD3 watchdog pipe), but do not weaken the compatibility-oracle verification or tests. Keep code KISS/DRY; avoid dual hidden launch modes. Test fixtures may use an explicit compile-time/test-only legacy harness if unavoidable, but production has exactly one direct launch path.
10. Update P5 docs/ledger only after tests prove behavior; do not overclaim real canary success.

Strict TDD and verification:
- RED tests for exact direct argv/descriptor, no Bash/stdin/ExtraFiles, direct sandbox target, unsupported route rejection, output mode/no-symlink/bounds, credential projection, supervisor-authored timeout + exact resume, cleanup/group exit, and compatibility-oracle drift rejection.
- Add a Darwin regression test demonstrating the production command target is pinned OMP and no `sh-thd-*` shared-temp authority is granted. Do not rely on raw log text.
- Migrate existing wrapper-oriented tests honestly; do not merely update assertions while leaving dead production machinery.
- Run focused tests repeatedly, full `internal/trustedsupervisor`, race focused, vet, gofmt, diff-check, and tagged canary compile. Do not run the real provider canary while editing; the orchestrator will run it in an exclusive quiet window after review.

If 1200 seconds is insufficient, leave compiling partial work, exact failing tests, and a concise continuation summary. Return changed files and exact RED/GREEN commands/results.
