# Ananke Vertical Slice 1: Supervisor Lifecycle Proof

## Contract

This is the first production implementation slice for Ananke. It proves
the supervisor lifecycle identity model (ADR-0002) and the cleanup state
machine with finalization outbox (ADR-0003) on Darwin/arm64.

The implementation is a fresh clean-room Go project. It does not inherit
or read any spike code. The ADRs are the authority.

## Scope

### In

1. **Lifecycle backend interface + Darwin implementation**
   - `BecomeGroupLeader()`: supervisor calls `setpgid(0, 0)`.
   - `LaunchWorker(path, args, env)`: fork+exec worker inheriting supervisor PGID.
   - `WorkerExited(pid)`: non-blocking exit detection (pipe or `waitpid(WNOHANG)`).
   - `ReapWorker(pid)`: blocking `waitpid` for exit status. Only called after group cleanup.
   - `GroupMembers(pgid)`: enumerate PIDs in group, excluding caller. Darwin: shell out to `ps` or use `libproc`.
   - `SignalProcess(pid, sig)`: signal a specific PID (not group).
   - `ProcessAlive(pid)`: `kill(pid, 0)` check without signal.

2. **Supervisor binary** (`cmd/ananke-supervisor`)
   - Calls `setpgid(0, 0)` at startup → becomes group leader.
   - Listens on a Unix socket for authenticated commands.
   - Launches worker into its own process group (worker inherits supervisor PGID).
   - Installs SIGTERM handler that ignores SIGTERM (survives group-wide TERM).
   - Monitors worker via pipe/stderr for exit detection (does NOT reap immediately).
   - On worker exit: checks group for survivors.
     - No survivors: reap worker, report exit status + transcript receipt.
     - Survivors: TERM group (supervisor survives), wait grace period, SIGKILL individual survivors, wait, reap worker.
   - After reap + cleanup: writes finalization acknowledgement.
   - Identity file: written atomically before worker launch.

3. **Cleanup state machine** (`internal/lifecycle/cleanup.go`)
   - States: `created`, `running`, `cancelling`, `cleanup_required`, `recovery_unknown`, `completed`, `failed`, `cancelled`.
   - Terminal states require authenticated group quiescence evidence.
   - `cleanup_required` and `recovery_unknown` are nonterminal and stay in active runs.
   - Transcript error while worker alive → `cleanup_required`, NOT `failed`.
   - Supervisor unreachable → `recovery_unknown`, NOT terminal.

4. **SQLite store with finalization outbox** (`internal/store/`)
   - Append-only journal: runs, events, state transitions.
   - `finalization_outbox` table: terminal state + supervisor identity + acknowledged flag.
   - Terminal transition transaction inserts outbox row atomically.
   - `ListActiveRuns` includes terminal runs with `acknowledged=0`.
   - Pure-Go `modernc.org/sqlite` driver; `CGO_ENABLED=0` compatible.
   - Schema versioning with immutable fixtures for migration testing.

5. **Fake worker fixture** (`cmd/ananke-fakeworker`)
   - Writes NDJSON transcript events to a file.
   - Can split a JSON write across two writes (partial line test).
   - Can omit final newline.
   - Can spawn normal or SIGTERM-resistant children.
   - Configurable exit code, event count, delay.
   - Does NOT call `setpgid` (inherits supervisor group).

6. **Daemon + local API** (`cmd/ananke`)
   - Launches supervisors, monitors workers, recovers on restart.
   - Unix socket JSON API: create-project, create-workstream, launch-run, get-run, list-events, cancel-run, ping.
   - Asynchronous cancellation: returns `accepted` immediately; cleanup runs on state machine.
   - Startup reconciliation: processes pending finalization outbox rows.

7. **Mutation gates** (build-tag controlled)
   - M1: Reap worker before group cleanup (must be detected).
   - M2: Commit terminal state without outbox row (must be detected).
   - M3: Signal numeric PGID after identity lost (must be detected).
   - M4: Enter terminal `failed` while worker group alive (must be detected).
   - M5: Reset transcript offset on restart without dedup (must be detected).
   - M6: Cancel only the parent PID, not the group (must be detected).

### Out

- UI / Tauri / React (deferred to a later slice).
- Linux/Windows lifecycle backends (interface only; Darwin implementation now).
- Real worker adapters (OMP, Claude) — fake worker only.
- Remote/mobile access.
- Authentication beyond local Unix socket + random token.
- Branding, installers, self-update.

## Verification Gates

1. `gofmt -d` on all Go files: no diff.
2. `go vet ./...`: clean.
3. `go test ./... -count=1`: all pass.
4. `go test -race ./... -count=1`: all pass.
5. `CGO_ENABLED=0 go test ./... -count=1`: all pass.
6. `CGO_ENABLED=0 go build ./cmd/...`: all binaries build.
7. Mutation proof: all six mutations produce named test failures at the intended assertion, not at setup/compile.
8. Crash/restart: 20 consecutive passes, stop on first failure.
9. Group cancellation: 20 consecutive passes, stop on first failure.
10. Production-binary black-box: cancellation and successful completion both leave no survivors.
11. No post-reap signal: a test observes the signal state machine and proves no group signal occurs after reap.
12. Resistant descendant cleanup: SIGTERM-resistant child receives SIGKILL and exits before worker reap.
13. Supervisor crash detection: SIGKILL supervisor → daemon enters `recovery_unknown`.
14. Terminal + outbox atomicity: crash between commit and finalize → outbox row present on restart.
15. Transcript failure: corrupt live transcript → `cleanup_required` while group alive → `failed` only after quiescence.

## Architecture Invariants (from ADRs)

- The supervisor is the process-group anchor. It calls `setpgid(0, 0)` at startup.
- The worker does NOT call `setpgid`. It inherits the supervisor's group.
- No `kill(-pgid, …)` after `waitpid` has reaped the worker.
- SIGKILL escalation targets individual PIDs, not the group.
- Terminal states require outbox row in the same SQLite transaction.
- `cleanup_required` and `recovery_unknown` are nonterminal.
- Cancellation is asynchronous: API returns `accepted`, client polls status.
- Identity file is written atomically (temp + rename) before worker launch.

## Key Dependencies

- `golang.org/x/sys`: OS-specific syscalls (setpgid, waitpid, signals).
- `modernc.org/sqlite`: pure-Go SQLite (CGO-free).

## Build

```sh
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/release/ananke ./cmd/ananke
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/release/ananke-supervisor ./cmd/ananke-supervisor
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/release/ananke-fakeworker ./cmd/ananke-fakeworker
```
