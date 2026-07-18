Implement the Ananke supervisor lifecycle proof — vertical slice 1.

This is a fresh clean-room Go implementation. Do NOT read, inspect, or reference any files outside `/Users/yingliangzhang/Projects/ananke/`. The ADRs and the implementation contract are your authority.

## Authority documents

Read these completely before starting:
- `docs/adr/0001-use-go-for-core-and-bootstrap.md`
- `docs/adr/0002-supervisor-lifecycle-identity-model.md`
- `docs/adr/0003-cleanup-state-machine-and-finalization-outbox.md`
- `docs/plans/0001-supervisor-lifecycle-proof.md`

## Toolchain

Go 1.26.5 is installed at `/opt/homebrew/bin/go`. The module is already initialized at `github.com/yingliang-zhang/ananke`. Use `CGO_ENABLED=0` for all builds and tests.

## Method

Strict TDD. For every production behavior:
1. Write a failing test (RED).
2. Run it to verify failure.
3. Write minimal implementation (GREEN).
4. Run to verify pass.
5. Only then move to the next behavior.

## What to build

### Phase 1: State and store

Build `internal/store/` with:
- State definitions: `created`, `running`, `cancelling`, `cleanup_required`, `recovery_unknown`, `completed`, `failed`, `cancelled`.
- Allowed transitions enforced by code. Terminal states require authenticated quiescence evidence.
- SQLite schema with `runs`, `events`, `state_transitions`, and `finalization_outbox` tables.
- Atomic terminal transition + outbox row insertion in one transaction.
- `ListActiveRuns` includes terminal runs with `acknowledged=0` outbox rows.
- Reconnectable event reads by sequence number.
- Schema versioning: `schema_version` table; `migrate` function that adds columns transactionally.
- Use `modernc.org/sqlite` (pure-Go, CGO-free). Add to `go.mod`.

TDD order:
1. State transition rules (allowed/rejected).
2. Store: create project, workstream, run.
3. Store: append event with monotonic sequence, atomic offset commit.
4. Store: terminal transition + outbox row atomicity.
5. Store: ListActiveRuns includes pending-outbox terminal runs.
6. Store: reconnectable event reads, high unsigned cursor.
7. Store: schema version + migration from v1 fixture.

### Phase 2: Lifecycle backend

Build `internal/lifecycle/` with:
- `LifecycleBackend` interface (see ADR-0002 §5).
- Darwin implementation using `golang.org/x/sys/unix`:
  - `BecomeGroupLeader`: `unix.Setpgid(0, 0)`.
  - `LaunchWorker`: `exec.Cmd` with `SysProcAttr{Setpgid: false}` (inherits parent group).
  - `WorkerExited`: pipe-based exit detection (worker stdout → pipe; EOF means exit).
  - `ReapWorker`: `unix.Wait4(pid, &status, 0, nil)`.
  - `GroupMembers`: shell out to `ps -A -o pid,pgid` and parse; exclude caller PID.
  - `SignalProcess`: `unix.Kill(pid, sig)`.
  - `ProcessAlive`: `unix.Kill(pid, 0)` with ESRCH check.
- Identity file struct and atomic write (temp + rename).

TDD order:
1. BecomeGroupLeader returns nonzero PGID.
2. LaunchWorker: worker inherits supervisor PGID.
3. WorkerExited: channel closes on worker exit.
4. ReapWorker: returns correct exit code.
5. GroupMembers: enumerates group members, excludes caller.
6. SignalProcess: sends signal to specific PID.
7. ProcessAlive: true for live, false for dead.
8. Identity file: atomic write, read back, verify fields.

### Phase 3: Supervisor binary

Build `cmd/ananke-supervisor/main.go`:
- Parse flags: `--socket`, `--token`, `--worker`, `--worker-args`, `--transcript`, `--identity`, `--term-grace`, `--adoption-timeout`.
- Call `BecomeGroupLeader()`.
- Write identity file atomically.
- Launch worker via `LaunchWorker`.
- Listen on Unix socket for authenticated JSON commands:
  - `status`: return worker exit status, group members, transcript digest.
  - `cancel`: request group cleanup (TERM → grace → SIGKILL individuals → reap).
  - `adopt`: acknowledge durable adoption by the daemon.
- Install SIGTERM handler: ignore (survive group-wide TERM).
- Install SIGCHLD handler: detect worker exit via pipe EOF or `waitpid(WNOHANG)`.
- On worker exit:
  - Check group for survivors (excluding self).
  - No survivors: reap worker, compute transcript receipt, signal done.
  - Survivors: group TERM, wait grace, enumerate survivors, SIGKILL each, wait, reap worker, compute receipt, signal done.
- After done: wait for finalization acknowledgement from daemon, then exit.
- Never call `kill(-pgid, …)` after reaping the worker.
- SIGKILL targets individual PIDs, not the group.

TDD order:
1. Supervisor becomes group leader (test via identity file PGID).
2. Worker launched into supervisor group (test via GroupMembers).
3. Worker exits, no survivors → supervisor reaps and reports status.
4. Worker exits, resistant child → supervisor escalates TERM→SIGKILL, reaps, reports.
5. No group signal issued after reap (observation test).
6. Cancel command triggers cleanup state machine.
7. Adopt command acknowledges.
8. Identity file written before worker launch.

### Phase 4: Fake worker

Build `cmd/ananke-fakeworker/main.go`:
- Flags: `--events N`, `--delay D`, `--exit CODE`, `--child MODE`, `--child-pid-file PATH`, `--transcript PATH`.
- Writes NDJSON to transcript file: `started`, `progress` (N times), `result`.
- Can split a JSON line across two writes (for partial-line test).
- Can omit final newline.
- Can spawn a child: `normal` (exits on SIGTERM) or `resistant` (ignores SIGTERM, exits on SIGKILL).
- Does NOT call `setpgid` (inherits supervisor group).
- Writes child PID to file if requested.

### Phase 5: Daemon + local API

Build `cmd/ananke/main.go` and `internal/lifecycle/engine.go`:
- Daemon launches supervisors, monitors identity files, reconnects after crash.
- Unix socket JSON API:
  - `ping`: health check.
  - `create-project`: create a project.
  - `create-workstream`: create a workstream under a project.
  - `launch-run`: launch a supervisor + worker for a workstream.
  - `get-run`: get run state, events, status.
  - `list-events`: reconnectable event reads by sequence.
  - `cancel-run`: asynchronous cancellation (returns `accepted` immediately).
- Engine recovery loop:
  - On startup: process pending finalization outbox rows.
  - On each tick: check active runs; handle supervisor reconnection.
  - Transcript error while worker alive → `cleanup_required` (NOT `failed`).
  - Supervisor unreachable → `recovery_unknown`.
  - Terminal commit + outbox row atomic.
  - Finalization: acknowledge outbox row after supervisor confirms.

TDD order:
1. Ping.
2. Create project + workstream.
3. Launch run → supervisor starts, worker runs, events stream.
4. Get run state.
5. List events by sequence.
6. Cancel run → `accepted` immediately → `cancelled` after quiescence.
7. Daemon SIGKILL + restart → recover same run.
8. Transcript corruption while worker alive → `cleanup_required` → `failed` after cleanup.
9. Supervisor SIGKILL → `recovery_unknown`.
10. Terminal commit + crash before finalize → outbox row on restart.

### Phase 6: Mutation gates

Create build-tag-controlled mutation files:
- `internal/lifecycle/mutation_reap_before_cleanup.go` (`-tags=mutation_reap_before_cleanup`)
- `internal/store/mutation_no_outbox.go` (`-tags=mutation_no_outbox`)
- `internal/lifecycle/mutation_signal_after_reap.go` (`-tags=mutation_signal_after_reap`)
- `internal/lifecycle/mutation_terminal_while_alive.go` (`-tags=mutation_terminal_while_alive`)
- `internal/store/mutation_reset_offset.go` (`-tags=mutation_reset_offset`)
- `internal/lifecycle/mutation_cancel_parent_only.go` (`-tags=mutation_cancel_parent_only`)

Each mutation alters production behavior in a specific way. Each must have a named test that fails only because of the mutation's behavioral change, not because of a compile/setup error.

Write `scripts/mutation_proof.py` that:
- Runs baseline (no tags): all named tests pass.
- For each mutation: build with the tag, run the named test, verify it fails.
- Classifies: `behavioral_test_rejection` (expected) vs `compile_failure` or `timeout` (unexpected).
- Outputs `reports/mutation-proof.json`.

### Phase 7: Verification scripts

Write:
- `scripts/verify.py`: gofmt, vet, standard test, race test, no-CGO test, no-CGO builds. Outputs `reports/verification.json`.
- `scripts/stress_lifecycle.py`: 20-pass crash/restart and cancellation. Stops on first failure. Outputs JSON reports.
- `scripts/blackbox_lifecycle.py`: builds production binaries, drives local API through CLI, verifies no survivors for cancellation and success.

## Rules

- All work inside `/Users/yingliangzhang/Projects/ananke/`.
- Do not modify ADRs or the implementation contract.
- Do not initialize or modify Git.
- `go fmt ./...` before stopping.
- Every test wait must be bounded with a deadline and diagnostic.
- No fixed sleeps masking failures — use event-driven waits.
- Report exact RED/GREEN evidence, changed files, and commands run.
