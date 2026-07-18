# Ananke Architecture

## Component Boundary

```
┌─────────────────────────────────────────────┐
│              ananke-desktop                  │
│  Tauri 2 thin shell + React/TS/Vite UI      │
│  (Rust = shell only, not durable authority)  │
├─────────────────────────────────────────────┤
│                local IPC                     │
├─────────────────────────────────────────────┤
│              ananke-core (Go)                 │
│  ┌──────────┐ ┌──────────┐ ┌──────────────┐ │
│  │ Projects  │ │ Engine   │ │  Memory      │ │
│  │ Workstreams│ │ Lifecycle│ │  (layered)   │ │
│  │ Sessions  │ │ Recovery │ │              │ │
│  └──────────┘ └────┬─────┘ └──────────────┘ │
│                   │                          │
│  ┌────────────────┴──────────────────────┐  │
│  │       Supervisor Protocol              │  │
│  │  ┌─────────┐ ┌────────┐ ┌──────────┐ │  │
│  │  │ Darwin  │ │ Linux  │ │ Windows  │ │  │
│  │  │ backend │ │ backend│ │ backend  │ │  │
│  │  └─────────┘ └────────┘ └──────────┘ │  │
│  └───────────────────────────────────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌─────────────┐ │
│  │  SQLite   │ │ Worker   │ │  Finaliz.   │ │
│  │  Journal  │ │ Adapters │ │  Outbox     │ │
│  └──────────┘ └──────────┘ └─────────────┘ │
├─────────────────────────────────────────────┤
│           ananke-bootstrap (Go)              │
│  Host supervisor: launch, monitor, recover   │
│  Platform-specific lifecycle, not embedded     │
└─────────────────────────────────────────────┘
        │
        ▼
  ┌──────────┐  ┌──────────┐  ┌──────────┐
  │  OMP     │  │ Claude   │  │ Custom   │
  │ adapter  │  │ adapter  │  │ adapter  │
  └──────────┘  └──────────┘  └──────────┘
  (out-of-process capability packs, polyglot)
```

## Design Invariants

1. **One durable Go authority owns all state.** Tauri's Rust layer is a
   thin shell; it never holds durable truth.

2. **Stable process identity.** A retained group anchor pins PGID until
   cleanup completes. No signal is issued after identity is lost.

3. **Explicit cleanup state machine.** Errors enter nonterminal
   cleanup-required states. Terminal states require authenticated
   quiescence evidence. Identity loss stays `recovery_unknown`.

4. **Durable finalization outbox.** Terminal transactions record pending
   supervisor obligations. Startup reconciles them. Terminal rows with
   pending cleanup remain recoverable.

5. **Schema-first protocol.** All IPC types are generated from a single
   schema source. No hand-synced Go/TS type definitions.

6. **Append-only SQLite journal.** Typed queries, no ORM. Every state
   transition is an append to the journal, not an in-place update.

7. **Capability packs are out-of-process.** Worker adapters (OMP, Claude,
   custom) run as separate processes. The supervisor owns their lifecycle.

## Language Assignment

| Component | Language | Rationale |
|---|---|---|
| `ananke-core` | Go | Durable authority; small agent-edit surface |
| `ananke-bootstrap` | Go | Host supervisor; OS-specific lifecycle backends |
| `ananke-desktop` | Tauri 2 (Rust shell) + React/TS | WebView-first UI; Rust = shell glue only |
| Capability packs | Polyglot | Each worker adapter uses its native toolchain |

## References

- Language spike evidence: `../rcos-agent-maintained-spike/`
- Spike final decision: `../rcos-agent-maintained-spike/docs/language-decision-final.md`
- Supervisor redesign contract: `docs/adr/` (to be written)
