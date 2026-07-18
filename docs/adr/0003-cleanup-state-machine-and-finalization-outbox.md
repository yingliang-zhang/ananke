# ADR-0003: Cleanup state machine and finalization outbox

## Status

Draft — 2026-07-17

## Context

The spike proved two crash-consistency BLOCKERs that are independent of
language choice:

1. **Transcript failure before cleanup (Go B2):** a malformed or
   truncated transcript can commit terminal `failed` while the worker
   group is still alive. The engine stops monitoring the run; the
   supervisor waits indefinitely.

2. **Terminal commit / finalize crash window (Go B3, Rust implicit):**
   the terminal SQLite state (`completed`/`failed`/`cancelled`) is
   committed before supervisor finalization. If the daemon crashes
   between commit and finalize, the supervisor is leaked. Terminal
   rows are excluded from `ListActiveRuns`, so recovery never
   reconciles them.

Both defects share a root cause: **terminal state publication and
resource cleanup are not atomic, and there is no durable record of
pending cleanup obligations.**

## Decision

### 1. Cleanup state machine

Replace the flat state model with an explicit cleanup lifecycle:

```
                    ┌──────────┐
                    │ created  │
                    └────┬─────┘
                         │ launch
                    ┌────▼─────┐
                    │ running  │◄──────────────────┐
                    └────┬─────┘                    │
              ┌────────┼────────┐                  │
              │        │        │                  │
         ┌────▼───┐ ┌──▼───┐ ┌──▼──────────┐      │
         │cancelling│ │failed│ │cleanup_required│   │
         └────┬───┘ └──┬───┘ └──────┬──────┘      │
              │        │            │              │
              │        │     ┌──────▼──────┐       │
              │        │     │ cleanup_run  │───────┘
              │        │     │ (authenticated │   │ retry
              │        │     │  quiescence)   │   │
              │        │     └──────┬──────┘       │
              │        │            │              │
         ┌────▼──────┐ ┌─▼────────┐ │
         │ cancelled │ │ failed   │◄┘
         └───────────┘ └──────────┘
                            │
                    ┌───────▼──────┐
                    │ completed    │
                    │ (success only)│
                    └──────────────┘
```

State definitions:

| State | Meaning | Monitored? | Terminal? |
|---|---|---|---|
| `created` | Run record exists; worker not launched | yes | no |
| `running` | Worker is executing; transcript streaming | yes | no |
| `cancelling` | Cancel requested; awaiting group quiescence | yes | no |
| `cleanup_required` | Error detected (transcript, worker); cleanup pending | yes | **no** |
| `recovery_unknown` | Supervisor unreachable; identity ambiguous | yes | **no** |
| `completed` | Trusted zero exit + full transcript + group quiescent | no | yes |
| `failed` | Nonzero exit or transcript error after authenticated cleanup | no | yes |
| `cancelled` | Group quiesced after cancellation request | no | yes |

**Invariant:** terminal states (`completed`, `failed`, `cancelled`)
are only reachable after authenticated group quiescence evidence.
`cleanup_required` and `recovery_unknown` are nonterminal — they
remain in `ListActiveRuns` and are reconciled on every tick.

### 2. Finalization outbox

The SQLite journal stores a `finalization_outbox` table:

```sql
CREATE TABLE finalization_outbox (
    run_id        TEXT NOT NULL,
    terminal_state TEXT NOT NULL,  -- completed/failed/cancelled
    supervisor_pid  INTEGER,
    supervisor_pgid INTEGER,
    socket_path     TEXT,
    token           TEXT,
    acknowledged     INTEGER DEFAULT 0,
    created_at       TEXT NOT NULL,
    acknowledged_at  TEXT,
    PRIMARY KEY (run_id),
    FOREIGN KEY (run_id) REFERENCES runs(id)
);
```

Protocol:

1. **Terminal commit:** the SQLite transaction that writes
   `completed`/`failed`/`cancelled` ALSO inserts an outbox row in the
   same transaction. Either both commit or neither does.

2. **Finalization:** after the transaction commits, the engine calls
   `finalizeSupervisor`. If it succeeds, the outbox row is marked
   `acknowledged=1`. If it fails, the row remains
   `acknowledged=0`.

3. **Startup reconciliation:** on every daemon start, the engine
   queries `SELECT * FROM finalization_outbox WHERE acknowledged=0`.
   For each row:
   - If the supervisor is alive and reachable: finalize and
     acknowledge.
   - If the supervisor is dead: attempt identity-safe cleanup (per
     ADR-0002 §3). If identity is ambiguous, leave the row pending.
     The run is NOT removed from active processing.
   - If the supervisor was already dead before this daemon start and
     identity is truly lost: mark the outbox row as
     `acknowledged=-1` (abandoned) and log a diagnostic. The run
     stays terminal but the leak is recorded.

4. **No terminal row is excluded from recovery** until its outbox
   row is acknowledged. `ListActiveRuns` returns all non-terminal
   runs PLUS terminal runs with `acknowledged=0`.

### 3. Transcript failure handling

When the engine detects a malformed, truncated, or replaced
transcript while the worker group is alive:

1. Transition `running` → `cleanup_required` (NOT `failed`).
2. Authenticate the supervisor, request cancellation of the worker
   group.
3. Wait for authenticated group quiescence.
4. Only then transition `cleanup_required` → `failed` with the
   transcript error as the reason.
5. The terminal commit inserts the finalization outbox row.

If the supervisor is unreachable (crash), transition to
`recovery_unknown` instead of `cleanup_required`. The run stays
monitored. On reconnect, if the worker has exited and the group is
quiescent, proceed to `failed`. If the supervisor is confirmed dead,
proceed with identity-safe cleanup per ADR-0002.

### 4. Cancellation protocol

Cancellation is asynchronous:

1. Client sends `cancel-run` via the local API.
2. Engine transitions `running` → `cancelling` (nonterminal).
3. Engine authenticates the supervisor and requests group TERM.
4. Client receives an immediate `accepted` response with the run's
   current state.
5. The cleanup state machine runs independently:
   - TERM → grace period → per-member SIGKILL if needed.
   - On group quiescence: transition `cancelling` → `cancelled`
     (terminal commit + outbox row).
6. Client polls `get-run` to observe progress.

The request-decode deadline is short (≤500ms). The cleanup deadline
is derived from the supervisor's monotonic state machine, not from
the connection deadline. This fixes the spike's M1 defect where the
default 500ms connection deadline encompassed the entire TERM→KILL
escalation.

## Alternatives Considered

### A. Two-phase terminal commit without outbox (rejected)

Write terminal state, attempt cleanup, and if cleanup fails, rewrite
to `recovery_unknown`.

**Rejected because:**

- A crash between the first commit and the rewrite loses the cleanup
  obligation.
- `recovery_unknown` after a terminal commit is semantically
  contradictory.
- No durable record of what cleanup was attempted.

### B. Out-of-process cleanup daemon (rejected)

Run a separate cleanup daemon that polls for leaked supervisors.

**Rejected because:**

- Adds another process to manage and monitor.
- Polling latency may delay cleanup significantly.
- The cleanup daemon itself can crash, creating a recursive problem.
- The in-process outbox is simpler and sufficient for single-user.

### C. SQLite WAL checkpoint as finalization marker (rejected)

Use a WAL checkpoint event as the durability boundary for
finalization.

**Rejected because:**

- WAL checkpoints are internal SQLite mechanics, not application
  semantics. They do not guarantee supervisor cleanup.
- Conflating storage durability with process lifecycle is the kind
  of boundary confusion the spike identified.

## Consequences

### Guarantees

- Terminal state and cleanup obligation are committed atomically.
  Either both are durable or neither is.
- Startup always reconciles pending finalizations.
- No terminal run with pending cleanup is silently dropped from
  recovery.
- Transcript errors never expose terminal state while the worker
  group is alive.
- Cancellation is asynchronous and does not block on cleanup
  duration.

### Implementation impact

- `Store` must add the `finalization_outbox` table and include outbox
  insertion in every terminal transition transaction.
- `ListActiveRuns` must include terminal runs with `acknowledged=0`
  outbox rows.
- The engine's recovery loop must process pending outbox rows on
  every startup and on each tick.
- The local API must return `accepted` immediately for cancellation
  and provide status polling.
- State transitions must enforce: `cleanup_required` and
  `recovery_unknown` are only reachable from nonterminal states;
  terminal states require the outbox row in the same transaction.

### Verification requirements

1. **Terminal + outbox atomicity:** inject a crash between SQLite
   commit and supervisor finalize; on restart, the outbox row is
   present and the run is reconciled.
2. **No terminal before cleanup:** corrupt a live transcript; assert
   the run is `cleanup_required` (not `failed`) while any group
   member is alive.
3. **Cancellation asynchronicity:** cancel a resistant-child run;
   assert the API returns `accepted` immediately and the run reaches
   `cancelled` only after group quiescence.
4. **Startup reconciliation:** create a pending outbox row, restart
   the daemon, and verify the supervisor is finalized (or the
   outbox is marked abandoned with a diagnostic).
5. **Mutation gate:** a mutation that commits terminal state
   without an outbox row must be detected and rejected.

## References

- Spike Go B2: `docs/go-hard-review-001.md`
- Spike Go B3: `docs/go-hard-review-001.md`
- ADR-0002: `docs/adr/0002-supervisor-lifecycle-identity-model.md`
- Final language decision: `docs/language-decision-final.md`
