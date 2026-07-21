# P0b.2 Renderer-Public Event Implementation Plan

> **For Hermes:** Execute with strict TDD in one isolated coding-agent session.

**Goal:** Replace the actual `list_events` public return type and renderer Event
annotation with generated JSON Schema + Quicktype models while preserving any
valid JSON payload.

**Architecture:** Add one renderer-public Event schema. Extend the current
multi-target generator. Rust bridge uses generated Event only for `list_events`;
TypeScript renderer imports the generated Event type. Existing daemon event
wire/JSON remains authoritative and unchanged.

---

### Task 1: RED event wire test

Write a focused bridge test that launches the fixture, calls `list_events`, and
asserts `seq`, `type`, plus object/array/string/number/boolean payload values.
Run it before generated Event integration and capture expected RED.

### Task 2: Schema/generator outputs

Create Event schema with arbitrary JSON payload. Extend generated Rust module,
TS output, full target drift check, and privacy check.

### Task 3: Minimal command migration

Change only `list_events` return type/mapping to generated Event and only
`main.ts` Event import. Preserve the public wire `type` key via generated serde
mapping; do not alter daemon event structs or other commands.

### Task 4: GREEN and evidence

Run focused test GREEN, controlled Event drift proof, all generator checks,
cargo fmt, TS typecheck/web build, diff/scope guard, and ledger update. No
commit.
