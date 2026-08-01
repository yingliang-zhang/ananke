# Ananke — First-Principles Redesign: From "Controlled Repair" to "Controlled Coding"

## Status

Draft — 2026-08-01. This document supersedes the "repair" framing in
`docs/gui-repair-interaction-design.md`. It is a first-principles redesign
of Ananke's purpose, interaction model, and user-facing terminology.

## The Problem

Ananke was conceived as a "controlled repair" system. This framing is wrong:

1. **"Repair" is too narrow.** Users don't just fix bugs — they write features,
   refactor, review, explore. Ananke's attestation/verifier mechanism is not
   specific to repair; it validates any agent-made code change.

2. **"Repair" creates artificial barriers.** A separate `ananke-repair` CLI,
   a "Repair" GUI tab, "New Repair" buttons — these make users think they're
   using a patching tool, not a development environment.

3. **"Repair" contradicts the chat-first model.** In Hermes Desktop, Orca, and
   OpenCode, the user just types a request. There is no "repair mode" — coding,
   debugging, refactoring are all the same flow. Ananke's "repair" framing
   forces a mode switch that doesn't exist in the user's mental model.

## First-Principles Analysis

### What is Ananke, fundamentally?

Ananke is a **personal Research Coding OS** — a system for a single researcher
who uses AI coding agents to develop code. Its core value proposition is:

1. **Agent does the coding** — the AI agent (via OMP/K3/etc.) modifies code
2. **System verifies what happened** — attestation + verifier provides
   cryptographic evidence of the agent's actions
3. **Human decides** — accept/reject/iterate on the agent's work
4. **State persists** — durable journal survives crashes, restarts, context resets

This is not "repair." This is **controlled coding** — AI agent development with
trust verification. The word "repair" appears nowhere in this value proposition.

### What should the user experience be?

From first principles, the user wants to:

1. **Open Ananke** → see their project and recent work
2. **Type a request** → "add a hello world function", "fix the deadlock",
   "refactor the auth module" — all the same
3. **Agent works** → modifies code, runs tests, produces evidence
4. **Review** → see the diff, accept/reject/ask-for-changes
5. **Continue** → the conversation continues, context preserved

This is exactly Hermes Desktop's model — except with Ananke's trust layer
(attestation, verifier, durable journal) underneath.

### What about memory, skills, session rollover?

Ananke's architecture already has the right bones:

- **Memory** (README §4): Ananke SQLite is authority for task/run/evidence.
  Hermes memory (Session DB, MEMORY/USER, Hindsight, Basic Memory) provides
  context. This boundary is correct and doesn't change.

- **Skills**: Ananke doesn't have its own skill system yet, but the agent
  (OMP/K3) has skills. The GUI should expose what skills the agent is using
  (like Hermes Desktop's tool ticker), not hide them behind a "repair" tab.

- **Session rollover**: Ananke's durable journal already persists runs and
  events. The GUI should restore the last conversation on startup (like
  Hermes Desktop restoring the last session). This is already partially
  implemented (`restoreRepairMessages`).

### What changes?

| Before | After |
|--------|-------|
| "Repair" tab in Run detail | Chat is the primary surface; no "repair" tab |
| `ananke-repair` CLI binary | Deleted; repair logic stays in `internal/` as implementation |
| "New Repair" / "Repair Run" | No special term; user just types a request |
| `Repair` in all user-facing strings | Removed; the user sees "chat", "conversation", "request" |
| Project → Workstream → Run → Repair | Project → Workstream → (auto-creates Run) → Chat |
| 7 steps to start | 2 steps (warm) / 4 steps (cold) |

### What stays the same?

- **Internal package names**: `repairrunner`, `repaircontract`, `repairverifier` —
  these have frozen hash bindings from P6 contracts. Renaming would break the
  contract layer. They are implementation details the user never sees.

- **IPC command names**: `repair-request`, `repair-poll`, etc. — these are
  internal daemon commands. The user never sees them.

- **Attestation/verifier flow**: The cryptographic trust layer is unchanged.
  The agent signs what it did; the verifier checks; the human decides.

- **Architecture invariants**: All 7 invariants in ARCHITECTURE.md hold.
  Go owns state; Tauri is thin shell; schema-first; append-only journal.

### What gets deleted?

- `cmd/ananke-repair/` — the standalone CLI binary. Users don't need a CLI
  for coding; the GUI is the primary surface. If headless execution is needed
  later, it can be a flag on the main `ananke` binary.

- All user-facing "repair" terminology in GUI strings, docs, and ADRs.

## Proposed Interaction Model

### Primary surface: Chat

The main window is a chat interface — like Hermes Desktop. The user types a
natural-language request and the agent responds.

```
┌──────────────────────────────────────────────────────────────────┐
│ Ananke   daemon ● online       Project: ananke                    │
├──────────────┬───────────────────────────────────────────────────┤
│ Projects     │                                                   │
│              │  user:    add a hello world function in main.go   │
│ ananke       │                                                   │
│  └ main      │  agent:   I'll add a hello world function.        │
│              │           [view diff]                             │
│              │                                                   │
│              │  agent:   Tests passed. Attestation signed.       │
│              │           sha256:a1b2c3d4...                      │
│              │           [Accept] [Reject] [Ask for changes]    │
│              │                                                   │
│              │  user:    looks good                              │
│              │                                                   │
│              │  agent:   Accepted. Outbox delivered.            │
│              │                                                   │
├──────────────┴───────────────────────────────────────────────────┤
│ [input: type your message...]                          [Send]    │
└──────────────────────────────────────────────────────────────────┘
```

### Navigation: minimal

- **Left rail**: Projects and Workstreams (same as v0.1)
- **Main area**: Chat conversation (replaces Activity/Transcript/Repair tabs)
- **No tabs**: The chat IS the run detail. Activity events appear as system
  messages in the conversation. Diagnostics stay in a collapsible section.

### Startup: state restoration

1. App opens → restores last Project + Workstream from journal
2. Chat thread restored from last conversation
3. Input box focused → type → Enter → agent works

**Warm path: 2 actions** (type, Enter).
**Cold path: 4 actions** (select Project, select Workstream, type, Enter).

### Run lifecycle: implicit

The user never explicitly "creates a Run." When they type a request and press
Enter, the Go daemon auto-creates a Run bound to the current Project+Workstream.
The Run appears in the left rail as a conversation entry (like Hermes sessions).

### Memory and context

- **Ananke SQLite**: authoritative for runs, events, attestations (unchanged)
- **Conversation history**: stored in Ananke's journal as run events, not in
  Hermes memory. Survives window close/reopen.
- **Agent skills**: the agent's skill usage appears as system messages in the
  conversation (e.g., "Using skill: systematic-debugging")
- **Session rollover**: when context window fills, older messages compress to
  summaries (like Hermes Desktop's progressive collapse). The journal retains
  the full record.

## Implementation Impact

### Delete
- `cmd/ananke-repair/` (CLI binary)
- "Repair" tab in `gui/src/main.ts`
- All user-facing "repair" strings in GUI

### Rename (user-facing only)
- "Repair panel" → "Chat" / "Conversation"
- "Repair request" → "message" / "request"
- "New Repair" → removed (just type and Enter)

### Keep (internal, user never sees)
- `internal/repairrunner/`, `internal/repaircontract/`, `internal/repairverifier/`
- `repair-request` IPC command names
- Attestation/verifier flow

### Add
- Chat as primary surface (replacing tab-based layout)
- Startup state restoration (restore last Project/Workstream/conversation)
- Auto-run-creation on first message in a new conversation
- Progressive collapse of agent tool activity

## Resolved Decisions

1. **Left rail shows "Conversations"** (not "Runs"). Internally a
   Conversation is a journaled Run — same durable, recoverable entity.
   The label change is user-facing only.

2. **Skills remain agent-owned.** Ananke delegates skill execution to
   the agent adapter (OMP/K3). Ananke does not have its own skill system.
   Skill usage appears as system messages in the conversation.

3. **Conversations persist across sessions.** This is Ananke's core
   value — durable journal survives window close/reopen. The conversation
   is restored from the journal on app launch.

4. **Activity/Transcript become collapsible sections within the chat.**
   No separate tabs. System events, lifecycle diagnostics, and raw
   payloads are in a collapsible "Diagnostics" section at the bottom of
   the conversation — progressive disclosure, matching Hermes Desktop.

## Core Differentiation: Ananke vs Hermes/Orca/OpenCode

### Philosophy

| | Ananke | Hermes Desktop | Orca | OpenCode |
|--|--------|---------------|------|----------|
| **Core thesis** | Durable trust layer for AI coding agents — *what happened* is cryptographically verifiable, survives crashes | Best agent orchestration UX — agent does everything through one chat | Parallelism — same task across N agents in N worktrees, pick the best | Best TUI coding agent — plan-then-build with git-backed safety |
| **Authority** | Ananke SQLite journal (append-only, signed) | Session DB + compression + memory layers | Git worktrees (authority = branch state) | SQLite sessions + git undo/redo |
| **What survives** | Runs, attestations, diffs, test results — all in a durable journal that outlives any model switch or crash | Conversation history, memory, skills — across CLI/TUI/desktop/gateway | Worktree branches with attribution per agent | Session history, /undo /redo (git-backed) |
| **Trust model** | Agent signs what it did → verifier checks → human decides (attestation + release-pinned verifier) | None — user trusts the agent's tool output | Reviewer picks the winner diff, deletes losers | Per-action permission prompts (ask/allow/deny) |
| **Who owns state** | Go daemon (single durable authority); Tauri Rust = thin shell | Hermes orchestrator (Go/Python) | Electron main process + git | OpenCode runtime (Zig core) |

### Features

| Feature | Ananke | Hermes | Orca | OpenCode |
|---------|--------|--------|------|----------|
| **Primary interaction** | Chat-first (type → Enter → agent works) | Chat-first | Worktree-centric IDE (⌘N → agent → paste) | Chat-first (TUI + desktop tabs) |
| **Launch to working** | 2 steps (warm) / 4 (cold) | 0 clicks (type, Enter) | 2-5 steps | 1 command |
| **Code change evidence** | Inline in conversation: diff + attestation hash + Accept/Reject/Ask-changes | Inline diff, progressive collapse, "Changed files" card | Dedicated diff viewer with j/k nav + inline comments | Side-by-side diff, git-backed /undo /redo |
| **Parallel agents** | No (single agent per conversation) | Subagent delegation (inline, same stream) | Yes — core feature, N worktrees × N agents | Subagents (clickable threads) |
| **Durable journal** | Yes — append-only SQLite, survives crashes/restarts | Session DB (compression, not append-only) | Git branches (not a journal) | SQLite sessions (not append-only) |
| **Cryptographic attestation** | Yes — Ed25519 signed, release-pinned verifier | No | No | No |
| **Memory architecture** | Authority/context boundary: Ananke SQLite (authority) + Hermes memory (context, opt-in bridge) | 4-layer: Session DB + MEMORY/USER + Hindsight + Basic Memory | None (state lives in git) | Session persistence only |
| **Skills** | Agent-owned (OMP/K3); usage appears as system messages | Agent-owned; loaded before each turn | Agent-owned (wraps any CLI agent) | Agent-owned (custom markdown agents) |
| **Session rollover** | Conversation restored from journal on launch; older messages compress to summaries | Full session restore from Session DB; /new for fresh | Worktree persists; tabs restore | Session resume/fork/share; /compact for rollover |
| **Lifecycle management** | Supervisor protocol (spawn, track, cancel, recover); cleanup state machine | Agent tool lifecycle (delegate, background, notify) | Worktree lifecycle (create, diff, ship, delete) | Plan/Build mode + permission gates |
| **Recovery on crash** | Durable finalization outbox reconciles pending obligations on restart | Session DB replay | Git worktree still exists | Session DB replay + git /undo |
| **Platform** | Tauri 2 (native desktop, thin shell) | Electron (desktop) + CLI + TUI + gateway | Electron (desktop) + CLI + mobile | TUI (Zig core) + Electron (desktop, beta) + IDE extensions |
| **Provider lock-in** | None (OMP adapter supports any provider) | None (multi-provider) | None (wraps any CLI agent) | None (multi-provider) |

### What Ananke uniquely offers

1. **Cryptographic trust**: the only system where agent code changes are
   attested (Ed25519 signed), verified (release-pinned), and decided by the
   human. Hermes/Orca/OpenCode all trust the agent's output directly.

2. **Durable journal**: the only system with an append-only SQLite journal
   that survives crashes with a reconciliation outbox. Others use git
   (Orca) or session DBs (Hermes/OpenCode) which don't have cleanup state
   machines or finalization outboxes.

3. **Authority/context boundary**: Ananke explicitly separates what it
   owns (task state, evidence, attestations) from what the agent's memory
   owns (conversation, skills, preferences). No other system makes this
   boundary explicit.

### What Ananke borrows from each

| From | Pattern | Why |
|------|---------|-----|
| Hermes | Chat-first + progressive collapse + inline approvals | Zero-friction launch, readable long sessions |
| Orca | Worktree isolation (internal, not user-facing) | Agent changes are isolated, diffable, discardable |
| OpenCode | Plan/Build → Accept/Reject mapping | Attestation review loop = implicit plan/build |
