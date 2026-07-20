# Ananke UI Reference Audit

Date: 2026-07-20

## Scope and weighting

This audit broadens the GUI evidence base beyond Hermes Studio. Popularity is a
signal for product maturity, not a design authority. Patterns are weighted by
fit with Ananke's constraints: a local, daemon-authoritative Research Coding OS
with `Project → Workstream → Run` as the product model and Tauri as a thin
client.

No external source was copied. Multica's modified Apache license in particular
makes its frontend a pattern reference only.

| Reference snapshot | Role in the audit | License / evidence boundary |
|---|---|---|
| OpenCode `cd46f22d513d60b7a9bdca1111d25c50d2398355` | Desktop/sidecar health, status and review interaction | MIT; source inspected under `packages/desktop`, `packages/app`, `packages/session-ui` |
| OpenAI Codex `678157acaa819d5510adfe359abb5d0392cfe461` | Protocol, event decomposition, interruption, approvals and backpressure | Apache-2.0; `codex-rs/app-server-*` and `codex-rs/tui` |
| Multica `1483ce08256ad3bb3d179e4f94a390f4d326b78d` | Control-plane projections, cross-surface architecture and presence | Modified Apache-2.0; no verbatim frontend reuse |
| OpenHands `93c0871951f9247cc87a63940972ae7e25d46b6f` | Event-store projection and agent state/control surfaces | MIT outside `enterprise/`; enterprise code excluded |
| Orca `a02389cea809239c7d38409e62b957b2dcaf66e1` | Parallel-agent attention, worktree navigation and diff review | Source inspected; terminal-title parsing is explicitly rejected |
| Hermes Agent `e702a45b5d35aeae8793ea2ce11aa61005251470` | Backend-authoritative desktop seam, project tree and diagnostic panes | Upstream source inspected, not the local patched fork |
| Typora official product/docs | Readable desktop surface, progressive disclosure and visual restraint | Closed source; visual/docs reference only |
| Otty official product/docs | Vertical sessions, status glyphs, palette and keyboard-first navigation | Proprietary; visual/docs reference only |
| `otty-shell/otty` `d89c7d51351a9c916e452a8dde596da5596e8259` | Negative control for terminal-centric architecture | Apache-2.0, 192-star WIP; architecture does not transfer |
| Hermes Studio `44f1368` | Low-weight Hermes-specific interaction sample | BSL-1.1; not a runtime or architectural reference |

## Pattern decisions

| Pattern | Source evidence | Decision |
|---|---|---|
| Backend owns durable truth; renderer holds projections | Hermes `apps/desktop/AGENTS.md`; Multica `apps/desktop/src/renderer/src/platform/daemon-ipc-bridge.ts`; OpenHands `frontend/src/stores/use-event-store.ts` | **Adopt v0.1.** UI state is a cache of the Go journal. No renderer-owned run lifecycle. |
| Thin desktop shell with authenticated backend health | OpenCode `packages/desktop/src/main/server.ts:spawnLocalServer`; `packages/app/src/utils/server-health.ts:checkServerHealth` | **Adopt v0.1.** Map to Tauri IPC and Go daemon; do not adopt Electron/HTTP. |
| Typed event envelopes, bounded transport and per-run serialization | Codex `app-server-protocol/src/protocol/common.rs`; `app-server-transport/src/lib.rs` | **Adopt v0.1.** Preserve monotonic event sequence and typed payloads; adapter vocabulary stays private. |
| Durable interrupt with expected-current-unit precondition | Codex `protocol/v2/turn.rs:TurnInterruptParams/TurnSteerParams` | **Adopt cancel semantics in v0.1.** Steer remains later. Frontend never kills workers directly. |
| Attention-first ordering and acknowledge-by-visiting | Orca `useAutoAckViewedAgent.ts`, `useRetainedAgents.ts`; tray attention icon; Codex `InterruptManager` | **Adopt v0.1.** Failed/cleanup-required/unvisited completion remain prominent until viewed; bound retained snapshots. |
| Vertical run rows with compact status glyphs | Otty official `tab-badge` and `code-agents` screenshots | **Adopt v0.1.** One row per Run; color plus shape; no text-heavy state pill in every row. |
| Calm, centered readable detail surface | Typora official editor/outline visuals | **Adopt v0.1.** Render canonical payloads readably; raw JSON and process identity stay behind disclosure. |
| Project/workstream jump palette | Orca `WorktreeJumpPalette.tsx`; Otty command palette | **Defer to v0.2 unless nearly free.** v0.1 fixed navigation remains sufficient. |
| Rich tool cards and right-rail files/review/preview | Hermes `components/chat/expandable-block.tsx`, `app/right-sidebar`; OpenCode `BasicTool` | **Use a minimal collapsible diagnostic rail in v0.1; defer rich cards/files/review.** Avoid synchronous layout reads in hot paths. |
| Diff viewer, inline comments and approvals | Orca editor/diff-comment components; OpenCode session review; Codex server requests | **Defer v0.2+.** Not part of the lifecycle proof. |
| Board/Gantt/Issue/Squad/Inbox task management | Multica views and domain types | **Avoid for v0.1.** It creates a competing authority and implies manual workflow state. |
| Chat-first shell / provider Thread→Turn→Item as product vocabulary | OpenHands shell, Codex protocol, OpenCode session home | **Avoid.** These remain adapter internals; Ananke exposes Project→Workstream→Run. |
| Integrated terminal/editor/browser/simulator panes | Orca workspace; Otty and otty-shell terminal cores | **Avoid v0.1.** Ananke is an operator cockpit, not another IDE. |
| Terminal title/OSC parsing as agent state | Orca runtime | **Avoid.** Go emits structured Run status and events. |
| Multi-user billing/org/admin and enterprise surfaces | OpenHands enterprise; Multica team control plane | **Avoid.** Personal local-first scope. |

## v0.1 visual contract derived from the audit

- Three-region structure remains: collapsible Project/Workstream rail, vertical
  Run list, readable Run detail. No Kanban and no embedded terminal.
- Left navigation width: approximately 220–260 px. Run rows: approximately
  34–36 px. Active row: neutral rounded surface with a subtle border/shadow.
- Main activity/transcript reading column: maximum width about 860 px, roughly
  48 px horizontal padding, body around 16 px with generous line height.
- Neutral tokens: white content, soft gray rail, near-black primary text, muted
  secondary text, light dividers, one accent. Semantic state colors are the only
  extra colors.
- State glyphs combine shape and color: running spinner/dot, waiting amber,
  failed red triangle, complete green check. Text remains available to
  accessibility APIs and detail views.
- Human labels and rendered payloads come first. PID, PGID, socket paths, opaque
  IDs and raw payload JSON use a quick-source-style collapsible diagnostics
  surface.
- Completion can stay bold/unread until its Run is visited. A hard cap prevents
  retained attention snapshots from growing without bound.
- The renderer never latches a backend truth permanently. Reconnect, project
  switching and window reopen rebuild projections from the Go daemon.

## Deferred surfaces

The following are valid reference-derived candidates but are not v0.1 release
requirements: contextual command palette, retry/backoff countdowns, rich tool
cards, diff/review/approvals, worktree lineage graph, full artifact/file panes,
subagent trees, split panes, mobile steering, OS notifications and configurable
status lines.

## Verification notes

- Exact SHAs and representative source paths were re-read from the local
  read-only clones after the parallel audit.
- Otty and Typora visual claims are limited to official screenshots/docs. Their
  implementation architecture is not inferred.
- `otty-shell/otty` is a separate Rust/Iced WIP project, not the proprietary
  Otty app. It is not a Tauri architecture reference.
