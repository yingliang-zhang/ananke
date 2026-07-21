# P0b.2 renderer-public Event migration contract

**Status:** approved incremental P0b contract.
**Base:** uncommitted accepted P0b bootstrap + P0b.1 Run worktree.
**Scope:** one real renderer command: Tauri `list_events` and its TypeScript
consumer.

## Goal

Move the public Event projection to generated JSON Schema + Quicktype Rust and
TypeScript types while preserving all current event payload JSON semantics.

## Frozen public Event payloads

Every Event contains `seq`, wire key `type`, and present `payload`. Payload may
be a JSON object, array, string, number, or boolean; it must remain arbitrary
JSON and must not be narrowed to an application-specific schema.

## In scope

1. Add a canonical renderer-public Event schema with arbitrary JSON `payload`.
2. Extend the existing generator/check flow to produce generated Rust and TS
   Event models and include them in all-target drift/privacy checks.
3. Replace the handwritten `EventDto` public return type for only `list_events`
   with the generated Rust Event model.
4. Replace only `main.ts` local Event annotation with generated TS Event.
5. Add a TDD test through the real bridge fixture path that asserts exact public
   events for object, array, string, number, and boolean payloads, plus wire
   key `type` rather than `event_type`.

## Out of scope

- daemon Go wire/event structs or newline JSON transport;
- run/bootstrap/cancel/health/launch/get commands;
- private/internal fields and all auth/token behavior;
- replacing non-public adapter logic; commits/pushes.

## Acceptance gates

1. Focused public list-events bridge test is observed RED before generated Event
   integration and GREEN after minimal integration.
2. Rust serialization and TS types preserve `type` and arbitrary payload values
   without payload defaulting/omission or type narrowing.
3. Generated check rejects an Event artifact content mutation; privacy scan
   covers every generated public artifact and excludes `token`/raw `error`.
4. Focused Rust test, generator check/privacy, cargo fmt, TS typecheck, web
   build, diff check, and combined P0b scope whitelist pass.
5. Ledger records only verified results; no commit/push.
