# P0b renderer-public bootstrap integration contract

**Status:** approved implementation contract.
**Base:** `49c419c` (P0a evidence decision).
**Scope:** one production vertical slice: Tauri `bootstrap` command and its
TypeScript consumer only.

## Goal

Adopt the P0a-selected JSON Schema + Quicktype pattern for the existing public
bootstrap payload without changing daemon transport, private daemon types, or
semantic adapters.

## In scope

1. Add one canonical public bootstrap schema under `gui/contracts/`.
2. Add a reproducible generator/check flow under `gui/scripts/`, pinned in the
   existing GUI Node toolchain.
3. Generate a Rust public bootstrap type and a TypeScript public bootstrap type.
4. Change the Rust `bootstrap` Tauri command to return the generated Rust public
   type, preserving the current wire JSON including `project.root`.
5. Change `gui/src/main.ts` to import the generated TypeScript bootstrap type
   instead of maintaining its local handwritten `Bootstrap` type.
6. Add test-first proof for the exact public bootstrap JSON shape and the
   generated TypeScript import/typecheck path.

## Explicitly out of scope

- Go daemon request/response schemas or newline JSON socket transport;
- `token`, raw daemon `error`, private `root` requests, run/event DTOs;
- `JsonRun -> RunDto` and all other semantic cross-boundary adapters;
- gRPC, Proto3, Buf, or protobuf binary wire format;
- changes outside `gui/` except this contract/ledger; commits or pushes.

## Frozen public payload

```json
{
  "project": {"id": "project-a", "name": "Ananke", "root": "/workspace/ananke"},
  "workstream": {"id": "main", "project_id": "project-a", "name": "Main"}
}
```

`project.root` is intentionally public because the existing Rust bridge sends
it. The TypeScript generated type must include it even though current rendering
does not read it.

## Acceptance gates

1. **TDD RED/GREEN:** a focused Rust bootstrap serialization test first fails
   because generated integration is missing, then passes after the minimal
   implementation; TypeScript generated import participates in `npm run typecheck`.
2. **Generation:** one documented Node 22 command regenerates Rust and TypeScript
   artifacts; a check mode rejects a hand mutation by content difference.
3. **Wire compatibility:** Rust bootstrap serialization equals the frozen JSON
   exactly; no omitted `project.root` and no renamed `project_id`.
4. **Scope/security:** generated public Rust/TS sources contain no `token` or raw
   daemon `error`; `git diff --exit-code` is clean outside the allowed P0b paths.
5. **Regression:** focused Rust test and `npm --prefix gui run typecheck` pass;
   run `npm --prefix gui run web:build` if environment supports it.
6. **Evidence:** record exact commands, tool versions, generated file/LOC counts,
   RED/GREEN logs, and limitations in `docs/experiment-ledger.md` only after
   results are verified.

## Allowed production paths

- `gui/contracts/renderer-public-bootstrap.schema.json`
- `gui/scripts/*renderer*public*`
- `gui/package.json`, `gui/package-lock.json`
- `gui/src/generated/**`
- `gui/src-tauri/src/generated/**`
- `gui/src-tauri/src/lib.rs` only for bootstrap DTO/command integration and test
- `gui/src/main.ts` only for generated Bootstrap type import

All other production paths require a separate contract revision.
