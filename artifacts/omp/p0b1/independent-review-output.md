Working...
ACCEPT

- `gui/src/main.ts:3,8,14` — relative to the accepted P0b bootstrap base, only the local `Run` annotation is migrated: generated `Run` is imported and used for `invoke<Run[]>("list_runs")`.
- `gui/contracts/renderer-public-run.schema.json:8-30`; `gui/src-tauri/src/generated/renderer_public_run.rs:18-36`; `gui/src/generated/renderer-public-run.ts:4-15` — canonical and generated Rust/TS models preserve the exact nested diagnostics keys: `project_id`, `workstream_id`, `worker_pid`, `supervisor_pid`, `committed_offset`.
- `gui/src-tauri/src/lib.rs:369-425,569-580,706-707` — private flat daemon `JsonRun` is explicitly adapted into nested generated `Run` only on `list_runs`; this is a necessary flat-internal-to-nested-public semantic conversion. `RunDto` remains only on unchanged `launch_fixture`/`get_run` paths (`:582-612`).
- `gui/scripts/generate-renderer-public.mjs:8-56,101-105,137-175` — deterministic check regenerates and compares bootstrap Rust/TS, Run Rust/TS, and generated Rust module; privacy scans every generated public target for `token`/`error`.
- Real bridge path: `gui/src-tauri/src/lib.rs:986-1018` bootstraps, launches a real fixture, calls `Backend::list_runs`, and serializes the generated result against the exact frozen nested JSON. Recorded RED: `docs/experiment-ledger.md:350`; independently observed GREEN:  
  `npm --prefix gui run build:go && cargo test list_runs_public_wire_json_is_frozen --lib --manifest-path gui/src-tauri/Cargo.toml` → `1 passed`, `10 filtered`.
- Focused checks passed:  
  `npm --prefix gui run check:renderer-public`;  
  `npm --prefix gui run check:renderer-public-privacy`;  
  `npm --prefix gui run typecheck`;  
  `npm --prefix gui run web:build`;  
  `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check`.
- Scope/transport: post-check `git status --porcelain=v1 -uall` contains only approved GUI/docs/artifact paths; `git diff --check` and `git diff --exit-code -- internal cmd` both exited 0. No daemon transport, Go/private types, or non-`list_runs` command changed.
- Portability: `gui/package.json:6-9,27-31` pins Quicktype `26.0.0` with Node 22 policy; `gui/scripts/generate-renderer-public.mjs:42-45` resolves the local package and selects `quicktype.cmd` on Windows. Current Node `v22.22.3` passed all generator and build checks.
