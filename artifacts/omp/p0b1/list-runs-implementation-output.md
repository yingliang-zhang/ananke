Working...
## Terminal verdict

**PASS — P0b.1 complete.** No commit or push performed.

## Changed paths — P0b.1 slice

- `gui/contracts/renderer-public-run.schema.json`
- `gui/scripts/generate-renderer-public.mjs`
- `gui/src-tauri/src/generated/mod.rs`
- `gui/src-tauri/src/generated/renderer_public_run.rs`
- `gui/src/generated/renderer-public-run.ts`
- `gui/src-tauri/src/lib.rs`
- `gui/src/main.ts`
- `docs/experiment-ledger.md`

Implementation:
- `list_runs` now returns generated Rust `Run`.
- `JsonRun -> generated::Run` remains an explicit handwritten semantic adapter; diagnostics remain nested and snake_case.
- `launch_fixture` and `get_run` retain `RunDto`.
- `main.ts` imports generated TypeScript `Run`; its local Run annotation is removed.
- Generator content and privacy checks iterate every generated public target, including Run.

## RED / GREEN evidence

**RED — before generated Run integration**

```sh
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  cargo test list_runs_public_wire_json_is_frozen --lib
```

Result: exit **101**.

```text
error[E0433]: cannot find `renderer_public_run` in `generated`
--> src/lib.rs:976:30
could not compile `ananke-gui` (lib test)
```

**GREEN — after schema, generator, generated types, and adapter**

```sh
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  npm --prefix gui run build:go && \
  cargo test list_runs_public_wire_json_is_frozen --lib \
    --manifest-path gui/src-tauri/Cargo.toml
```

Result: exit **0**.

```text
cargo test: 1 passed (1 suite, 10 filtered, 0.45s)
```

Post-format focused rerun:

```sh
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  cargo test list_runs_public_wire_json_is_frozen --lib \
    --manifest-path gui/src-tauri/Cargo.toml
```

Result: exit **0**.

```text
cargo test: 1 passed (1 suite, 10 filtered, 0.65s)
```

The focused test launches a real fixture through the bridge, lists it through `list_runs`, and compares the exact nested JSON projection, including `worker_pid`, `supervisor_pid`, and `committed_offset`.

## Generator drift / privacy proof

Controlled mutation: inserted `// Controlled drift proof.` into generated `renderer_public_run.rs`.

```sh
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  npm --prefix gui run check:renderer-public
```

Result: exit **1**.

```text
Generated renderer-public models drifted:
.../gui/src-tauri/src/generated/renderer_public_run.rs.
```

Restored strictly through generation, then verified:

```sh
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  npm --prefix gui run generate:renderer-public && \
  npm --prefix gui run check:renderer-public && \
  npm --prefix gui run check:renderer-public-privacy
```

Result: exit **0**.

```text
Generated renderer-public models from the canonical schemas.
Renderer-public generated models match the canonical schema.
Renderer-public generated models expose no token or error fields.
```

## Final validation

```sh
cargo fmt --manifest-path gui/src-tauri/Cargo.toml && \
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  npm --prefix gui run check:renderer-public && \
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  npm --prefix gui run check:renderer-public-privacy && \
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  npm --prefix gui run typecheck && \
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  npm --prefix gui run web:build
```

Result: exit **0**.

```text
Renderer-public generated models match the canonical schema.
Renderer-public generated models expose no token or error fields.
tsc -b --pretty false
vite v7.3.6 ... 7 modules transformed ... built in 70ms
```

## Scope proof

```sh
git diff --check
```

Result: exit **0**, no output.

```sh
git status --short
```

Reported 15 changed worktree paths, all within approved `gui/`, `docs/`, or `artifacts/omp/p0b*` locations. A whitelist evaluation passed:

```text
Scope whitelist passed: 15 changed paths; no path outside approved GUI/docs/artifact locations.
```

`docs/experiment-ledger.md` records only observed RED/GREEN, drift, validation, and scope results.

## Limitations

- The worktree includes inherited uncommitted P0b bootstrap artifacts; scope proof covers the combined P0b/P0b.1 worktree locations.
- No Go daemon transport, Go structs, bootstrap behavior, token/internal fields, or non-`list_runs` Tauri command behavior changed.
