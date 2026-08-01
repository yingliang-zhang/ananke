# Ananke GUI Repair Interaction Design Contract

## Status

Draft — 2026-08-01. Supersedes the repair-tab form interface in `gui/src/main.ts`.
This contract must be accepted by the user before implementation begins.

## Context

The v0.1 GUI design contract (`docs/gui-v0.1-design.md`) states the GUI "is not a
generic chat client." That remains true for the lifecycle monitoring surface
(runs, events, diagnostics). However, the **repair interaction** — directing an
AI coding agent to modify code, reviewing its output, and accepting/rejecting —
requires a **conversational** interaction model. This is not a contradiction:
the lifecycle surface shows structured, durable state; the repair surface shows
the agent's reasoning, actions, and evidence as a conversation.

This contract defines how repair interaction appears in the Tauri 2 native GUI,
within the existing `Project → Workstream → Run` information architecture.

## Design Principles

1. **Repair lives inside a Run, not as a separate tab.** A repair is a kind of
   run activity. The user launches a repair from within a Run's detail view, and
   the repair conversation appears alongside activity and transcript.

2. **The conversation is the primary interface.** The user types a natural-language
   request ("fix the deadlock in ananke-repair-gui") and the agent responds with
   its plan, actions, diff, and evidence — all in a chat-like message thread.

3. **Structured evidence is embedded in the conversation.** Diff patches,
   attestation hashes, test results, and review actions appear as rich messages
   within the thread — not as separate form fields or tabs.

4. **The Go daemon owns all state.** The Tauri Rust layer is a thin IPC bridge.
   Repair jobs are journaled Runs with durable cancellation and recovery. The
   Rust layer never spawns processes or holds job state.

5. **Terminal truth survives window close.** Closing and reopening the Tauri
   window restores the repair conversation from the SQLite journal — including
   the agent's messages, the diff, and the attestation state.

## Interaction Model

### Starting a Repair

The user is in a Run's detail view (Activity tab). They click "Request Repair"
(not a separate tab — an action button). A conversation thread replaces the
activity feed, with an input box at the bottom.

```
┌──────────────────────────────────────────────────────────────────┐
│ Run: repair-20260801    │ state: running                         │
│ [Activity] [Transcript] [Repair]                                 │
│──────────────────────────────────────────────────────────────────│
│                                                                  │
│  user:    fix the deadlock in ananke-repair-gui                  │
│                                                                  │
│  agent:   Looking at the code... The issue is `select{}` on      │
│           line 59 of main.go. Go's deadlock detector fires      │
│           because all goroutines are asleep.                     │
│                                                                  │
│  agent:   I'll replace it with `signal.Notify` on SIGINT/SIGTERM.│
│           [view diff]                                            │
│                                                                  │
│  agent:   Tests passed. Attestation signed.                      │
│           sha256:a1b2c3d4...                                     │
│           [Accept] [Reject] [Ask for changes]                   │
│                                                                  │
│  user:    looks good                                             │
│                                                                  │
│  agent:   Repair accepted. Outbox delivered.                    │
│                                                                  │
├──────────────────────────────────────────────────────────────────│
│ [input: type your message...]                          [Send]    │
└──────────────────────────────────────────────────────────────────┘
```

### Message Types

| Type | Direction | Content |
|------|-----------|---------|
| `user_request` | user → agent | Natural language repair request |
| `agent_reasoning` | agent → user | Agent's analysis of the problem |
| `agent_action` | agent → user | What the agent did (files changed, tests run) |
| `agent_diff` | agent → user | Git diff patch (inline, collapsible) |
| `agent_evidence` | agent → user | Attestation hash, test result summary |
| `review_action` | user → agent | Accept / Reject / Ask-for-changes |
| `agent_response` | agent → user | Response to review (e.g., "accepted, outbox delivered") |
| `error` | system → user | Error message (failed adapter, timeout, etc.) |

### Multi-turn Iteration

The user can "Ask for changes" instead of Accept/Reject. This starts a new
repair attempt in the same conversation, with the agent retaining context from
previous messages. Each attempt produces a new attestation; the conversation
shows all attempts in order.

```
user:    fix the deadlock
agent:   [diff] [evidence] [Accept] [Reject] [Ask for changes]
user:    can you also add a timeout?
agent:   [new diff] [new evidence] [Accept] [Reject] [Ask for changes]
user:    [Accept]
agent:   Repair accepted.
```

## Architecture

```
Tauri 2 TS frontend (chat UI)
    ↓ invoke("repair_request", { runId, message })
Tauri 2 Rust (thin IPC bridge)
    ↓ Unix socket (authenticated)
Go daemon (repair service)
    ↓ repairrunner.RunControlledRepair()
OMP adapter (K3) → worktree → test → attestation → store
    ↓ events stream back via Unix socket
Tauri 2 TS frontend (chat messages update)
```

### Go daemon repair IPC

The Go daemon gains these Unix-socket commands (matching existing pattern):

| Command | Direction | Description |
|---------|-----------|-------------|
| `repair_request` | TS → Go | Start a repair (runId, requestText, adapterType) |
| `repair_poll` | TS → Go | Poll repair job state (runId) |
| `repair_review` | TS → Go | Accept/reject repair attestation (runId, action) |
| `repair_messages` | TS → Go | Get conversation messages for a run (runId) |

The Go daemon salvages `internal/gui/api.go`'s `runRepair` logic — it already
correctly calls repairrunner in-process with proper attestation signing.

### Tauri Rust layer

The Rust layer deletes all repair-specific logic:
- `PENDING_REPAIRS`, `PendingRepair`, `chrono_now`, `libc_kill`
- `submit_repair`, `poll_repair_job`, `get_repair_status`, `accept_repair`,
  `reject_repair`, `read_repair_diff`

And replaces them with thin IPC pass-throughs matching the existing
`use_backend` pattern used by lifecycle commands:

```rust
#[tauri::command]
fn repair_request(state: State<'_, BridgeState>, run_id: String, message: String) -> Result<...> {
    use_backend(state, |backend| backend.repair_request(run_id, message))
}
```

### Frontend (TypeScript)

The repair tab becomes a chat thread:
- Message list (rendered from `repair_messages` IPC response)
- Input box + Send button
- Inline diff viewer (collapsible `<pre>` block)
- Accept/Reject/Ask-for-changes buttons in agent evidence messages
- Auto-poll via `repair_poll` every 3s while repair is running

## Relationship to v0.1 Design Contract

This contract is an **addendum** to `docs/gui-v0.1-design.md`. It does not
replace the lifecycle surface — it adds the repair interaction model that the
original contract deferred. The v0.1 statement "is not a generic chat client"
remains true: the lifecycle surface (runs, events, diagnostics) is structured.
Only the repair interaction within a run is conversational.

## Non-Goals

- Not a general-purpose chat client (no free-form conversation outside repair context)
- Not a multi-agent chat (one repair conversation per run at a time)
- Not a streaming UI in v0.1 (polling, not WebSocket SSE — matching the existing
  lifecycle polling pattern)
- Not a code editor (diffs are view-only; the user applies them with `git apply`)

## Open Questions

1. Should repair conversations persist across sessions (in the SQLite journal)?
   → **Yes** — per principle 4, terminal truth survives window close.

2. Should the agent's reasoning be stored in the attestation or in a separate
   message table?
   → **Separate message table** — the attestation is cryptographic evidence of
   what happened; the conversation is the human-readable record.

3. Should multiple repair attempts in the same conversation produce separate
   attestations?
   → **Yes** — each attempt is a distinct controlled repair with its own
   attestation. The conversation links them by order.
