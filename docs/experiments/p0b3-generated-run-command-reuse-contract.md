# P0b.3 generated Run command reuse contract

**Scope:** migrate only Tauri `launch_fixture` and `get_run` public return types
to the already accepted generated Rust/TS public Run model.

## In scope

- Replace handwritten `RunDto` returns on only those two commands with generated
  `Run`; reuse the existing explicit internal `JsonRun -> Run` semantic adapter.
- Add TDD bridge tests for exact nested Run JSON from launch and get paths.
- Remove any remaining `main.ts` dependence on handwritten Run types (it should
  already import generated Run).

## Out of scope

No new schemas/generator changes; no daemon transport/Go changes; no list-runs,
events, bootstrap, cancel, health behavior change; no commit/push.

## Gates

RED then GREEN for launch/get exact public Run wire; focused Rust tests,
generator check/privacy, formatter, typecheck, web build, diff/scope guard.
