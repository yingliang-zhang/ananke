Working...
## Verdict

**PASS — P0b.2 complete.** No commit or push performed.

`list_events` now returns generated Rust `Event`; `main.ts` imports generated TypeScript `Event`. The public wire remains `type`; payloads map from the raw daemon `serde_json::Value` to `Some(Value)` and serialize exactly for the required fixture variants.

## Changed files — P0b.2

- `docs/experiment-ledger.md`
- `gui/contracts/renderer-public-event.schema.json`
- `gui/scripts/generate-renderer-public.mjs`
- `gui/src-tauri/src/generated/mod.rs`
- `gui/src-tauri/src/generated/renderer_public_event.rs`
- `gui/src/generated/renderer-public-event.ts`
- `gui/src-tauri/src/lib.rs`
- `gui/src/main.ts`

No Go daemon event structs/transport, token logic, Run/bootstrap/cancel/health/launch/get command behavior, or non-`list_events` semantic adapter changed.

## TDD evidence

**RED** — before Event schema/generated integration:

```text
error[E0433]: cannot find `renderer_public_event` in `generated`
...
could not compile `ananke-gui` (lib test) due to 1 previous error
```

Command exited **101**:

```sh
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run build:go &&
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH \
  cargo test list_events_public_wire_json_preserves_arbitrary_payloads \
  --lib --manifest-path gui/src-tauri/Cargo.toml
```

**GREEN** — same real bridge fixture after integration:

```text
cargo test: 1 passed (1 suite, 11 filtered, 1.44s)
```

Post-format rerun:

```text
cargo test: 1 passed (1 suite, 11 filtered, 1.43s)
```

The fixture launches through the real daemon bridge and proves exact serialized public events for object, array, string, number (`42.5`), and boolean payloads, with `type` rather than `event_type`.

## Verification

- Controlled Event generated-artifact mutation: `check:renderer-public` exited **1** and named `renderer_public_event.rs`; regeneration restored it.
- `npm --prefix gui run check:renderer-public` — passed.
- `npm --prefix gui run check:renderer-public-privacy` — passed:
  ```text
  Renderer-public generated models expose no token or error fields.
  ```
- `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check` — passed.
- `npm --prefix gui run typecheck` — passed.
- `npm --prefix gui run web:build` — passed: Vite 7.3.6, 7 modules, 52 ms.
- `git diff --check` — passed with no output.
- Combined P0b/P0b.1/P0b.2 whitelist — passed: **32** changed worktree paths; none outside the exact approved set.
- Evidence recorded in `docs/experiment-ledger.md`.

## Verified limitations

- Coverage proves the contract’s five non-null JSON payload categories. Null is rejected upstream and was not asserted as a public Event fixture.
- Numeric fidelity is proven for the frozen `42.5` fixture only; arbitrary-precision numeric fidelity is not claimed.
