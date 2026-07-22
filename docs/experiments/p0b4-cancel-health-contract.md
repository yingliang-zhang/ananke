# P0b.4 Cancel and Health public migration contract

Migrate only Tauri `cancel_run` and `daemon_health` public responses to generated
JSON Schema + Quicktype Rust/TypeScript types. Canonical `Cancel` requires
`accepted:boolean` and `state:string`; canonical `Health` requires
`online:boolean`. Add real bridge TDD RED/GREEN wire tests. Do not alter daemon
transport, token/private/internal types, Run/Event/bootstrap/launch/get behavior,
or semantics. Extend generator all-target drift/privacy checks; run focused Rust,
formatter, typecheck/web build, scope guard; no commit/push.
