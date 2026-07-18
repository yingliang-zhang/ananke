# ADR-0002: Supervisor lifecycle identity model

## Status

Draft — 2026-07-17

## Context

The RCOS language spike proved that both Rust and Go implementations
shared the same core BLOCKER: after the process-group leader (the
worker) is reaped by `waitpid`, the numeric PGID can be reused by the
kernel for an unrelated process group. Subsequent `kill(-pgid, …)`
calls may then signal the wrong group.

The spike's Go hard review (B1) states:

> `cleanupGroup` then relies only on the stored integer PGID:
> existence check, TERM, subsequent checks, KILL.
> There is no retained leader/anchor, start-time/session verification,
> or stable kernel process handle between check, TERM, wait, and KILL.

This is not a language defect — it is a design defect in the
supervisor boundary. Both candidates must fix it before either could
be accepted.

### Root cause

In the spike design:

1. The worker process calls `setpgid(0, 0)` and becomes the
   process-group leader. PGID = worker PID.
2. The supervisor calls `worker.Wait()` to collect the exit status.
   `Wait()` calls `waitpid`, which reaps the worker.
3. After reaping, the worker's PID is freed. The kernel may assign it
   to a new process. If that process also calls `setpgid(0, 0)`, the
   numeric PGID now identifies a different group.
4. The supervisor then calls `kill(-pgid, SIGTERM)` or
   `kill(-pgid, SIGKILL)` to clean up resistant descendants. But the
   PGID may no longer refer to the original group.

The window between step 2 (reap) and step 4 (signal) is the
identity-reuse vulnerability.

### Additional constraints

- **Daemon crash**: the daemon (ananke-core) may crash and restart.
  The supervisor and worker continue running. The restarted daemon
  must be able to safely reconnect and verify the process group.
- **Supervisor crash**: if the supervisor itself dies, the worker is
  reparented to PID 1 and may be reaped, losing the exit status. The
  system must detect this and enter `recovery_unknown` rather than
  guessing.
- **Resistant descendants**: workers may spawn children that ignore
  SIGTERM. The cleanup protocol must escalate to SIGKILL while
  identity is still provably stable.
- **Cross-platform**: the identity model must be expressible on
  Darwin, Linux, and Windows, even though the initial implementation
  targets Darwin only.

## Decision

### 1. Supervisor is the process-group anchor

The supervisor — not the worker — is the process-group leader. The
supervisor launches the worker into its own process group, and the
worker plus all descendants inherit the supervisor's PGID.

```
  daemon
    │
    ▼
  supervisor (PGID = supervisor PID)
    ├── worker (inherits supervisor PGID)
    │     ├── child A (inherits PGID)
    │     └── child B (inherits PGID)
    └── (supervisor itself, alive until cleanup done)
```

The supervisor:

1. Calls `setpgid(0, 0)` at startup → becomes group leader.
2. Forks the worker. The worker inherits the supervisor's PGID.
3. Owns `waitpid(workerPID)` for exit status.
4. Ignores SIGTERM — group-wide TERM must not kill the supervisor
   before it can manage escalation.
5. Exits only after all cleanup and finalization obligations are
   complete.

### 2. Deferred reap: never reap the worker before group cleanup

The supervisor does NOT call `worker.Wait()` immediately. Instead:

1. Monitor the worker via a pipe (stdout/stderr) or
   `waitpid(WNOHANG)` for non-blocking exit detection.
2. When the worker has exited (pipe closed or `WNOHANG` returns):
   a. Check whether the process group has any remaining members
      besides the supervisor.
   b. If no survivors: reap the worker (`waitpid` blocking) →
      safe, no group signaling needed.
   c. If survivors exist (resistant descendants):
      - Send `kill(-pgid, SIGTERM)` — supervisor is still alive,
        PGID is stable, supervisor ignores SIGTERM.
      - Wait for the grace period.
      - If survivors remain: enumerate group members and send
        `SIGKILL` to each member individually (NOT `kill(-pgid, …)`,
        to avoid killing the supervisor itself).
      - Wait for all members to exit.
   d. Reap the worker.
3. After reaping: the supervisor is still alive. No further group
   signaling is needed. The supervisor exits when finalization is
   acknowledged.

### 3. Crash recovery with durable identity

The supervisor writes a durable identity file before launching the
worker:

```
identity.json:
  supervisor_pid:  <PID>
  supervisor_pgid: <PGID>
  worker_pid:       <PID>
  worker_args:      [...]
  socket_path:      <Unix socket>
  token:            <random secret>
  transcript_path:  <NDJSON file>
  launch_time:      <monotonic+wall clock>
```

On daemon restart:

1. Read the identity file.
2. Check if the supervisor process is alive (`kill(supervisorPID, 0)`).
3. If alive: reconnect via the Unix socket. The supervisor still pins
   the PGID. Query worker status, continue monitoring.
4. If dead: the supervisor's PID is freed. The PGID may be reused.
   - Check if any process with `worker_pid` is alive.
   - If the worker is still alive (reparented to PID 1): it may be in
     a now-ambiguous group. Enter `recovery_unknown`.
   - If the worker is also dead: exit status is lost (reaped by PID 1).
     Use transcript evidence if available; otherwise `recovery_unknown`.
   - Do NOT signal the numeric PGID after the supervisor is confirmed
     dead. The identity is no longer provably stable.

### 4. Group member enumeration

For targeted SIGKILL (step 2c), the supervisor must enumerate process
group members:

- **Darwin**: use `libproc` or `proc_listallpids` + `proc_pgrp`
  filtering. A pure-Go wrapper via `golang.org/x/sys/unix` or cgo
  (guarded by build tag, not in the CGO-free path) may be needed.
  Alternative: shell out to `ps -A -o pid,pgid` and parse output.
  This is acceptable because SIGKILL escalation is rare and not in
  the hot path.
- **Linux**: read `/proc/*/stat` and filter by process group.
- **Windows**: Job Object provides implicit group membership. No
  enumeration needed — `TerminateJobObject` kills all members.

### 5. Platform abstraction

```go
type LifecycleBackend interface {
    // BecomeGroupLeader makes the calling process the leader of a
    // new process group. Returns the PGID.
    BecomeGroupLeader() (pgid int, err error)

    // LaunchWorker forks/execs the worker into the caller's process
    // group. Returns the worker PID.
    LaunchWorker(path string, args []string, env []string) (pid int, err error)

    // WorkerExited returns a channel that closes when the worker
    // exits. Does NOT reap the worker.
    WorkerExited(pid int) (<-chan struct{}, error)

    // ReapWorker calls waitpid and returns the exit status. Only
    // call after group cleanup is complete.
    ReapWorker(pid int) (exitCode int, err error)

    // GroupMembers returns PIDs of all processes in the given
    // process group, excluding the caller.
    GroupMembers(pgid int) ([]int, error)

    // SignalProcess sends a signal to a specific PID.
    SignalProcess(pid int, sig Signal) error

    // ProcessAlive checks if a PID is alive without signalling.
    ProcessAlive(pid int) bool
}
```

Initial implementation: `darwinLifecycleBackend`. Future:
`linuxLifecycleBackend` (cgroup/pidfd), `windowsLifecycleBackend`
(Job Object).

## Alternatives Considered

### A. Separate anchor process (rejected for complexity)

Launch a trivial anchor process that becomes group leader, then launch
the worker into the anchor's group. The anchor stays alive until
cleanup, pinning the PGID even if the supervisor crashes.

**Rejected because:**

- On Darwin, `exec.Cmd.SysProcAttr` has no `Pgid` field. Putting the
  worker into the anchor's group requires either a custom fork wrapper
  or making the anchor fork the worker (which means only the anchor
  can `waitpid` the worker, complicating exit-status ownership).
- The anchor is a new process to manage, monitor, and clean up —
  adding failure modes rather than removing them.
- The supervisor-as-anchor achieves the same identity stability for
  the common case (daemon crash) with simpler code.

If supervisor-crash resilience becomes a hard requirement later, a
separate anchor can be added as an enhancement without changing the
deferred-reap protocol.

### B. pidfd (Linux only, deferred)

Linux 5.3+ provides `pidfd_open` and `pidfd_send_signal` which give a
stable handle to a process that is immune to PID reuse. This is the
ideal mechanism on Linux.

**Deferred because:**

- Not available on Darwin (the initial target platform).
- The Darwin backend uses deferred-reap + group enumeration instead.
- When the Linux backend is implemented, it should use pidfd for both
  the worker and each group member, eliminating the enumeration +
  individual-signal approach.

### C. Process re-verification before each signal (insufficient)

Before each `kill(-pgid, …)`, verify that the process group still
contains the expected members by checking start time or process name.

**Rejected because:**

- Race-prone: the group can change between verification and signal.
- Does not solve the fundamental problem: the PGID number itself may
  refer to a different group.
- Adds complexity without providing the hard guarantee the contract
  requires.

## Consequences

### What this design guarantees

- The PGID is stable for the entire duration of cleanup because the
  group leader (supervisor) is alive and unreaped.
- No `kill(-pgid, …)` is ever issued after the group leader has been
  reaped.
- SIGKILL escalation targets individual PIDs, not the group, avoiding
  self-kill of the supervisor.
- Daemon crash is recoverable: the supervisor is still alive, still
  pinning the PGID, and can be reconnected.
- Supervisor crash is detected and enters `recovery_unknown` rather
  than risking identity-reuse signalling.

### What this design does NOT guarantee

- If the supervisor itself crashes, the worker's exit status may be
  lost (reaped by PID 1). The system enters `recovery_unknown` and
  relies on transcript evidence.
- If the supervisor crashes AND the worker has resistant descendants,
  those descendants may continue running with a PGID that is no
  longer safely signalable. This is an accepted residual risk;
  mitigation requires a separate anchor (Alternative A) or platform-
  specific handles (pidfd on Linux).

### Implementation impact

- The supervisor binary (`ananke-bootstrap` or a child of it) must
  call `setpgid(0, 0)` at startup.
- The worker must NOT call `setpgid` itself; it inherits the
  supervisor's group.
- `exec.Cmd` must use `Setpgid: false` (default) so the worker
  inherits the parent's group.
- The supervisor must install a SIGTERM handler that ignores the
  signal (or handles it as a no-op) to survive group-wide TERM.
- Group member enumeration on Darwin requires either `libproc` via
  cgo or shelling out to `ps`. The cgo path must be guarded by a
  build tag and not required for the CGO-free build.
- The identity file must be written atomically (temp + rename) before
  the worker is launched.

### Verification requirements

The implementation must prove:

1. **No post-reap signal**: no `kill(-pgid, …)` call occurs after
   `waitpid` has reaped the worker. Enforced by code structure and
   verified by a test that observes the signal state machine.
2. **Resistant descendant cleanup**: a worker that spawns a
   SIGTERM-resistant child is fully cleaned up (child receives
   SIGKILL and exits) before the worker is reaped.
3. **Daemon crash recovery**: SIGKILL the daemon, restart, and
   verify the supervisor is still reachable, the PGID is still
   valid, and the run can be finalized.
4. **Supervisor crash detection**: SIGKILL the supervisor, restart
   the daemon, and verify the run enters `recovery_unknown` without
   issuing any group signal.
5. **Mutation gate**: a mutation that reaps the worker before group
   cleanup must be detected and rejected by the test suite.

## References

- Spike B1 finding: `docs/go-hard-review-001.md` and
  `artifacts/omp/go-review-001/output.md`
- Spike Rust B1 finding: `docs/rust-arm-verdict.md`
- Final language decision: `docs/language-decision-final.md`
- Deferred to ADR-0003: cleanup state machine, finalization outbox,
  and the `recovery_unknown` / `cleanup_required` state transitions.
