# P0b.1 renderer-public Run migration contract

**Status:** approved incremental P0b contract.
**Base:** uncommitted P0b bootstrap accepted worktree.
**Scope:** one real renderer command: Tauri `list_runs` and its TypeScript
consumer.

## Goal

Extend the selected JSON Schema + Quicktype public protocol migration from
bootstrap to the public nested Run projection, without changing the Go daemon
wire protocol or removing the intentional semantic adapter.

## Frozen public Run payload

```json
{
  "id": "run-001",
  "state": "running",
  "diagnostics": {
    "project_id": "project-a",
    "workstream_id": "main",
    "worker_pid": 1234,
    "supervisor_pid": 1235,
    "committed_offset": 4096
  }
}
```

## In scope

1. Add a canonical public Run schema to the same GUI contract source family.
2. Extend the existing deterministic generator/check flow to produce Rust and
   TypeScript public Run types, with shared public-field privacy and content
   drift checks.
3. Replace the handwritten public `RunDto` return type for `list_runs` with the
   generated Rust Run type.
4. Keep the current flat internal `JsonRun -> nested public Run` mapping as an
   explicit handwritten semantic adapter; it maps data, not duplicate type
   definitions.
5. Replace only `main.ts` local Run annotation with the generated TypeScript
   Run type.
6. Add test-first proof that a real list-runs path yields exactly the frozen
   nested public shape, including `worker_pid`, `supervisor_pid`, and
   `committed_offset`.

## Out of scope

- `launch_fixture`, `get_run`, events, cancellation, health, bootstrap
  behavior, or any other Tauri command;
- Go daemon API structs, token/private request fields, daemon socket transport;
- changing the public nested diagnostics shape or removing the semantic adapter;
- commits/pushes.

## Acceptance gates

1. A focused Rust list-runs wire test must be observed RED before generated Run
   integration, then GREEN after the minimum implementation.
2. Generated Rust and TS Run types must preserve snake_case fields and nested
   diagnostics exactly.
3. Generator check must reject a mutation to any public generated output,
   including Run, by content difference; privacy check covers all generated
   public Rust/TS files and excludes `token`/raw `error` fields.
4. `list_runs` returns generated public Run values; no use of handwritten
   `RunDto` remains on that command path. `JsonRun` may remain internal-only.
5. Focused Rust test, generation check, privacy check, GUI typecheck, and web
   build pass. No changes outside the P0b allowed GUI/docs/artifact paths.
6. Ledger records only verified RED/GREEN and independent review evidence.
