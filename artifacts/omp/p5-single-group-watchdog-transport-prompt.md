Implement a P5 trusted-supervisor single-process-group and fork-free wrapper-watchdog transport repair in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` with strict TDD. No commit. Do not edit P6 repaircontract files. No provider call.

Verified root cause from production source:
- the exact pinned `omp_with_timeout.sh` enables `set -m` before launching OMP, so OMP enters a different PGID from the verified `sandbox-exec` leader;
- the supervisor currently proves only the sandbox leader PGID gone, so that is not complete descendant containment;
- the wrapper also starts `( sleep "$HARD_SECONDS"; ... ) &`; killing the watchdog subshell may leave its external `/bin/sleep` child alive in the sandbox PGID;
- fifth/sixth canaries reached running but failed `group_exit_unconfirmed`; increasing natural residual grace from 750ms to 5s correctly did not solve the structural issue;
- do not modify the external Hermes wrapper and do not signal an unverified/reused PGID after its leader is reaped.

Verified Bash 3.2 facts on this Mac:
- a trusted shell function named `set` can intercept `set -m` and `set +m`; ignoring only those exact one-argument forms keeps a background child in the parent PGID;
- macOS `/bin/bash` 3.2 has no usable `BASHPID`; use numeric `BASH_SUBSHELL > 0` to identify the watchdog subshell;
- Bash builtin `read -t N -u 3` can supply the watchdog delay without creating a child when FD 3 is the read side of a pipe whose writer remains open in the trusted parent;
- killing the read-subshell with SIGKILL returns immediately and leaves no sleep child. The exact wrapper already calls `wait "$WATCHDOG_PID" ... || true`, so a trusted `kill` function can translate only the exact one-argument positive `kill "$WATCHDOG_PID"` call to `builtin kill -KILL "$WATCHDOG_PID"`; all `kill -0`, group TERM/KILL, and other forms must forward unchanged.

Required design:
1. Bump `ananke.atomic-omp-runtime-authority.v1` and its policy version to explicit v2 values because this changes the trusted bootstrap/FD/process-group contract. Add closed fields such as:
   - `process_group_policy = trusted_supervisor_single_group_v1`
   - `watchdog_wait_fd_policy = inherited_read_fd_3_parent_writer_retained_v1`
   Keep artifact descriptors parent-retained CLOEXEC and separate from the watchdog FD. Unknown/old/mixed policies reject.
2. Extend `auditOMPBootstrap` so its sealed bytes prepend three readonly functions before the unchanged frozen wrapper:
   - `set`: ignore only exact `-m`/`+m`; forward every other form to `builtin set`;
   - `sleep`: only when `BASH_SUBSHELL > 0`, exactly one argument equals the wrapper's validated integer `HARD_SECONDS`, and FD 3 is readable, use `IFS= read -r -t "$1" -u 3 ...`; a timeout status (>128) means successful elapsed sleep; every other call uses exact `/bin/sleep`;
   - `kill`: only exact one-argument positive target equal to nonempty numeric `WATCHDOG_PID` becomes `builtin kill -KILL`; all other forms use `builtin kill` unchanged;
   - preserve the existing readonly `omp()` absolute-executable function.
   Use Bash-3.2-compatible syntax. No `BASHPID`, process substitution, eval, source, caller script, environment-selected executable, or raw wrapper mutation.
3. In `executeAuditInvocation`, create a dedicated `os.Pipe` for the watchdog wait before start. Pass only its reader as child FD 3 through `exec.Cmd.ExtraFiles`. Close the parent's reader immediately after successful Start; keep the writer open until the verified command group is gone, then close it. Close both ends on every prestart/error/cancel/Close path. Do not write data to this pipe. No credential or model data crosses it.
4. Bind exact FD number, process-group policy, watchdog transport, bootstrap hash, and framed bootstrap+wrapper hash into the v2 runtime authority and command descriptor. Validate them at policy load, admission, effect boundary, and execution. Update canonical test fixtures and canary policy construction; reject v1, missing/mixed fields, wrong FD/policy/hash.
5. Keep `command.SysProcAttr.Setpgid=true` on the sandbox leader. With job-control interception, wrapper, OMP, watchdog read-subshell, and normal descendants inherit that exact group unless the untrusted program explicitly calls setsid (future exclusive UID-wide containment remains a separate P6 production gate; document that this repair closes the known wrapper-induced split but does not claim UID-wide setsid containment).
6. Preserve existing 5s residual observation as fail-closed defense; this repair must make normal early wrapper exit prove group absence promptly rather than merely extending grace.
7. Add RED/GREEN tests:
   - v1/unknown/missing/mixed runtime authority rejects;
   - bootstrap exact bytes/hash/framing and Bash 3.2 syntax;
   - `set -m/+m` interception keeps fake OMP child in exact leader PGID;
   - all other `set` forms preserve behavior;
   - watchdog delay uses FD3 with zero child processes;
   - wrapper exact positive watchdog kill terminates/reaps promptly; `kill -0`, unrelated PID, and negative group signals forward unchanged;
   - FD3 missing/closed/wrong policy fails closed or remains bounded without weakening supervisor deadline;
   - reader/writer lifecycle and no descriptor leaks across success, start failure, cancellation, timeout, concurrent Close;
   - integration fixture modeled on the pinned wrapper: fake OMP exits early, watchdog cleanup leaves no residual process and `ProcessGroupGone=true` promptly;
   - live verified leader cancellation TERM->KILL unchanged; reaped leader still never receives a group signal;
   - command descriptor and policy hashes change on every new field/bootstrap/FD drift;
   - production source does not mutate external wrapper bytes or enable caller-selected bootstrap.
8. Update relevant P5 runbook/contract docs accurately. Explicitly state residual limitation: arbitrary untrusted `setsid` requires future exclusive dedicated-UID lease + UID-empty terminal proof and is not claimed closed here.
9. Run focused bootstrap/authority/process-group/watchdog/termination/Close tests count=10, race count=3, full trustedsupervisor single, vet, gofmt, diff-check. No real provider or external wrapper edit.

Return actual RED/GREEN evidence and changed files. Do not create cron jobs.
