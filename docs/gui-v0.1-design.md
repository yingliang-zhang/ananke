# Ananke GUI v0.1 Design Contract

## Purpose

GUI v0.1 is the first operator-facing proof of Ananke's real lifecycle path. It
is not a generic chat client and it is not yet the daily-driver milestone. It
must prove this vertical slice with durable Go state as the sole authority:

`Tauri window → Rust IPC bridge → Go daemon sidecar → supervisor → worker → SQLite journal → activity UI`

The Rust layer may locate and start sidecars, send local IPC requests, and map
responses into Tauri commands. It must not own durable project, workstream,
run, event, cancellation, or recovery state.

## Primary Information Architecture

The user-facing hierarchy is `Project → Workstream → Run`. Worker-specific
concepts such as Codex threads or OMP sessions remain adapter details.

| Region | v0.1 content | Later evolution |
|---|---|---|
| Left rail | Projects and workstreams; selected repository root | Search, archived workstreams, project health |
| Run list | Run state, elapsed time, current activity, attention badge | Retry/backoff, budget, adapter/model, filters |
| Main detail | Activity, transcript payloads, diagnostics | Diff, artifacts, tests, approvals, memory |
| Action bar | Create project/workstream, launch, refresh, cancel | Steer, pause, resume, retry, approve |
| Status strip | Daemon online/offline and running/failed/cancelled counts | Rate limits, token/cost budget, update status |

Approximate desktop layout:

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Ananke   daemon ● online       Project: ananke          1 running        │
├──────────────┬──────────────────────┬────────────────────────────────────┤
│ Projects     │ Runs                 │ Run: repair-d                      │
│              │                      │                                    │
│ ananke       │ ● running  repair-d  │ Activity | Transcript | Diagnostics│
│  └ main      │ ✓ done     gate-v4   │                                    │
│              │ ✕ failed   probe-17  │ 02:41  worker started              │
│              │                      │ 02:41  event 1                      │
│              │                      │ 02:42  cancellation requested       │
│              │                      │                                    │
├──────────────┴──────────────────────┴────────────────────────────────────┤
│ [Launch run] [Cancel] [Refresh]                         backend: Go core  │
└──────────────────────────────────────────────────────────────────────────┘
```

## v0.1 Behaviors

1. Start or connect to the packaged Go daemon sidecar.
2. Show a fail-closed offline state if the daemon cannot be authenticated.
3. Create a real project and workstream through the Go API.
4. Launch a real supervised run, initially using the bundled fake worker as a
   deterministic lifecycle fixture.
5. Poll or subscribe to state and event changes and render canonical payloads.
6. Cancel a running job through durable cancellation, not by killing from the
   frontend.
7. Preserve terminal truth across closing and reopening the Tauri window.
8. Expose diagnostics when lifecycle state is `cleanup_required`, failed, or
   otherwise needs operator attention.

## Visual Direction

- Native-feeling macOS dark/light surfaces, restrained neutral palette, one
  accent color, semantic status colors only for state.
- Medium information density: denser than a chat client, less noisy than a
  terminal process table.
- Human labels first. Raw PID, PGID, socket, token, and opaque session IDs stay
  in a collapsible diagnostics surface.
- Attention-first ordering: blocked, cleanup-required, and failed runs are
  visually prominent; healthy completed runs recede.
- No decorative dashboard charts in v0.1. Every visible metric must support an
  operator decision.

## Reference Projects

Source review snapshot: 2026-07-20.

The detailed, source-backed matrix is in [UI Reference Audit](ui-reference-audit.md).
The reference set includes OpenCode, Codex, Multica, OpenHands, Orca, upstream
Hermes Agent, Typora, Otty, otty-shell, Symphony, and Hermes Studio. Star count
is treated as a maturity signal, not design authority; fit with Ananke's durable
local control plane is the primary weight.

The intended v0.1 synthesis is:

- **Codex/OpenCode protocol discipline:** typed events, bounded transport,
  durable interruption, health and capability boundaries;
- **Hermes/Multica projection discipline:** backend truth is cached, never
  re-owned by the renderer;
- **Orca attention discipline:** unvisited completion/failure remains visible
  until the Run is viewed;
- **Otty surrounding chrome:** compact vertical Run rows and semantic status
  glyphs, without adopting its terminal core;
- **Typora readable-surface discipline:** quiet main content, progressive raw
  diagnostics, and restrained visual tokens.

Hermes Studio is retained only as a low-weight Hermes-specific interaction
sample. Its popularity, BSL code and chat/session authority do not justify using
it as Ananke's primary UI or architecture reference. The Go journal and
lifecycle supervisor remain independently authoritative.

## Explicit Non-Goals for v0.1

- Full coding-agent chat UX
- OMP/Codex production adapters
- Diff editor or integrated IDE
- Memory promotion UI
- Kanban, issue tracker, cron, messaging, voice, or multi-user administration
- Cross-platform packaging beyond the current macOS proof
- Mobile control

## Acceptance Contract

GUI v0.1 is accepted only when all of the following are executed successfully:

1. Tauri development app and production build both compile.
2. The app launches the real Go daemon and proves authenticated `ping`.
3. An end-to-end test creates project/workstream, launches a run, displays
   non-null canonical event payloads, and reaches the correct terminal state.
4. A cancellation test reaches durable `cancelled`, with transcript fully
   consumed before terminal publication.
5. Closing/reopening the UI does not alter or lose Go-owned state.
6. Closing/reopening the window leaves the independent Go daemon and active
   supervisors intact; the new window reconnects with the persisted local
   credential. A separate explicit backend-shutdown test, run only after all
   work is terminal, leaves no processes or stale sockets.
7. An independent review returns `VERDICT: ACCEPT` against a frozen candidate.