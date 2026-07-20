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

### 1. Worker-led group with a paused pre-exec barrier

The supervisor remains outside the owned worker process group. It launches a
small trampoline as a new process-group leader, so `PGID == worker PID`, while
retaining the only exact child wait authority.

```
  daemon
    │
    ▼
  supervisor (outside owned group)
    │
    └── worker trampoline (PID = owned PGID)
          ├── child A (inherits owned PGID)
          └── child B (inherits owned PGID)
```

Worker path, arguments, and environment are transferred through an inherited
configuration descriptor. A second inherited descriptor is a release barrier.
After `Start` returns, the supervisor installs exact exit observation, records
the positive PID as both worker PID and owned PGID, and publishes identity JSON,
SQLite authority, the authenticated socket, and running state. Only then does
it release the trampoline. The trampoline `exec`s the real worker in place, so
PID and PGID do not change.

The persisted `supervisor_pgid` / Go `SupervisorPGID` names are retained for
pre-v0.1 compatibility. Their value is the owned worker-group PGID, not the
supervisor process's current group.

### 2. Deferred reap: never reap the worker leader before group cleanup

The supervisor does not consume the worker leader's wait status when exit is
observed. The unreaped leader pins the numeric PGID until cleanup is proven:

1. `GroupMembers(pgid)` enumerates live, non-zombie members only as a
   fail-closed quiescence oracle.
2. If the group is nonempty, send one atomic `kill(-pgid, SIGTERM)` and wait the
   grace period.
3. If members remain, send one atomic `kill(-pgid, SIGKILL)` and poll until
   enumeration proves the group empty.
4. Reap the exact worker leader and preserve its real exit status.
5. Never signal the group after reap.

The supervisor is outside the target group, so atomic TERM and KILL cannot kill
the cleanup authority. A durable cancellation observed before release follows
the same cleanup path while the trampoline is still blocked; the real worker
never executes.

### 3. Crash recovery with durable identity

Transcript file identity is published before trampoline launch. Complete
process authority is published after the paused trampoline returns a positive
PID and before release:

```
identity.json:
  supervisor_pid:  <supervisor PID>
  supervisor_pgid: <owned worker PGID; compatibility field name>
  worker_pid:       <worker/trampoline PID, equal to owned PGID>
  worker_args:      [...]
  socket_path:      <Unix socket>
  token:            <random secret>
  transcript_path:  <NDJSON file>
  launch_time:      <wall clock>
```

On daemon restart:

1. Read and cross-check the identity file and SQLite authority.
2. Check whether the supervisor process is alive.
3. If alive, authenticate to its Unix socket and adopt status/finalization. The
   supervisor retains exact wait authority and does not reap before cleanup.
4. If dead, the stored PGID is no longer safe signalling authority. If either
   the worker PID or group occupancy remains, enter `recovery_unknown`; if all
   processes are gone, use only durable transcript/finalization evidence.

No restart path signals the numeric PGID after supervisor authority is lost.

### 4. Group member enumeration

Enumeration is evidence only; enumerated numeric PIDs are never signalled
individually:

- **Darwin**: parse `ps -A -o pid,pgid,stat`, rejecting malformed output and
  excluding zombies. An unavailable or malformed enumeration is not empty-group
  proof.
- **Linux**: read `/proc/*/stat` and filter by process group.
- **Windows**: Job Object provides implicit group membership and atomic
  termination.

### 5. Platform abstraction

```go
type LifecycleBackend interface {
    // LaunchWorker starts a paused group-leading trampoline and installs exact
    // exit observation. The positive PID is also the owned PGID.
    LaunchWorker(path string, args []string, env []string) (pid int, err error)

    // ReleaseWorker lets the trampoline exec the real worker in place.
    ReleaseWorker(pid int) error

    // WorkerExited observes exact exit without reaping.
    WorkerExited(pid int) (<-chan struct{}, error)

    // ReapWorker consumes wait status only after group cleanup.
    ReapWorker(pid int) (exitCode int, err error)

    // GroupMembers is a fail-closed quiescence oracle.
    GroupMembers(pgid int) ([]int, error)

    // SignalGroup atomically signals the positive owned PGID.
    SignalGroup(pgid int, sig Signal) error

    // ProcessAlive checks whether a PID exists without delivering a signal.
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

- The supervisor is never a member of the worker group it must terminate.
- The worker PID equals the owned PGID before any real worker code executes.
- Complete durable and socket authority exists before barrier release.
- The unreaped worker leader pins PGID identity through atomic group TERM/KILL
  and fail-closed empty-group proof.
- No group signal occurs after exact leader reap.
- Daemon restart can authenticate to the still-live supervisor without changing
  worker ownership.
- Loss of supervisor authority enters `recovery_unknown` rather than risking a
  reused numeric PGID.

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
