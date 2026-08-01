# ADR-0006: Reframe from "Controlled Repair" to "Controlled Coding"

## Status

Accepted — 2026-08-01

## Context

Ananke was originally conceived as a "controlled repair" system — a tool
for managing AI agent repairs to code with cryptographic trust verification.
During development, three direction divergences revealed that the "repair"
framing is too narrow and misleading:

1. A web GUI (`cmd/ananke-repair-gui`) was built when the architecture
   specified Tauri 2 as the only operator surface. (This also produced a
   standalone `ananke-repair` CLI binary as a side effect.)
2. A form-based "Repair" tab was built inside the Run detail view when the
   user wanted a chat-first conversational interface.
3. The Rust layer spawned CLI processes when the architecture specified
   Go daemon IPC.

The root cause of all three divergences was the same: the "repair" framing
led implementers to build a narrow patching tool instead of a general
development environment. See `docs/first-principles-redesign.md` for the
full analysis and `README.md` §7 (Principle 7: Contract traceability) for
the governance principle added in response.

## Decision

Reframe Ananke from "controlled repair" to "controlled coding":

1. **Ananke is a personal Research Coding OS** — a system for a single
   researcher using AI coding agents to develop code, not just repair it.

2. **The trust layer applies to all agent code changes** — attestation,
   verifier, and human review are not specific to "repair"; they validate
   any agent-made modification (features, refactors, bug fixes, reviews).

3. **The primary user surface is chat-first** — the user types a
   natural-language request and the agent responds, like Hermes Desktop.
   There is no "repair mode" or "repair tab."

4. **`cmd/ananke-repair/` is deleted** — the standalone CLI binary is
   removed. If headless execution is needed later, it will be a flag on
   the main `ananke` binary, not a separate binary.

5. **User-facing "repair" terminology is removed** — from GUI strings,
   docs, and future ADRs. Internal package names (`repairrunner`,
   `repaircontract`, `repairverifier`) and IPC command names
   (`repair-request`, `repair-poll`) are kept unchanged because:
   - They have frozen hash preimage bindings (the `RepairContext` struct
     fields like `"ananke-repair-auth"`, `"ananke-repair-dispatch"` are
     the binding locus, not package paths)
   - They are implementation details the user never sees
   - Renaming would require re-deriving P6 contract hashes, which is
     out of scope

## Relationship to ADR-0005

This ADR does **NOT** supersede ADR-0005. ADR-0005 (Multi-model
review/audit MoA) describes the review methodology, which is orthogonal
to the "repair" vs "coding" framing. The MoA pattern applies equally
to reviewing any agent code change, not just "repairs." ADR-0005's
title ("for controlled repair") is historical and immutable; this ADR
reframes the scope without invalidating the methodology.

## Consequences

- `cmd/ananke-repair/` deleted; no headless CLI for now
- GUI redesigned from tab-based to chat-first (see
  `docs/first-principles-redesign.md`)
- `README.md` §4 P4 table updated: conversational messages are now
  journaled run events in Ananke SQLite (authority shift)
- `README.md` §6 references to "repair flow" should be updated to
  "agent coding flow"
- Future ADRs use "controlled coding" framing, not "controlled repair"
- Internal package names and IPC commands unchanged (frozen hash contracts)
