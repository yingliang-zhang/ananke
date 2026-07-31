Working...
**Terminal verdict: PASS — P0b.2 Event payload is now required and non-null.**

- Canonical schema: `gui/contracts/renderer-public-event.schema.json`
  - `payload.type` explicitly permits only `object`, `array`, `string`, `number`, or `boolean`.
  - `payload` remains required.

- Regenerated outputs:
  - Rust: `Event.payload: Payload`, not `Option<_>`.
  - TypeScript:  
    `unknown[] | boolean | number | { [key: string]: unknown } | string`  
    The required field excludes top-level `null` and `undefined`.

- Bridge conversion now uses `TryFrom<EventDto>` and generated `Payload` deserialization; invalid top-level payloads return a bridge JSON error rather than fabricating an event.

- Added `generated_event_requires_present_non_null_payload` in `gui/src-tauri/src/lib.rs`.
  - **RED before repair:** exited 101 because a missing payload deserialized through the old `Option<serde_json::Value>` model.
  - **GREEN after repair:** passes; exact round-trips prove object, array, string, number (`42.5`), and boolean. Missing and explicit `null` payload deserialization both fail.
  - Existing real bridge fixture remains green and proves the same five kinds through `list_events`.

**Verified**
- `npm --prefix gui run generate:renderer-public`
- `npm --prefix gui run check:renderer-public`
- Controlled generated-Event drift rejected with exit 1; regeneration restored it.
- `npm --prefix gui run check:renderer-public-privacy`
- Formatter/generator order-independence in both required sequences.
- `cargo test generated_event_requires_present_non_null_payload --lib --manifest-path gui/src-tauri/Cargo.toml`
- `npm --prefix gui run build:go && cargo test list_events_public_wire_json_preserves_arbitrary_payloads --lib --manifest-path gui/src-tauri/Cargo.toml`
- `npm --prefix gui run typecheck`
- `npm --prefix gui run web:build` — Vite built successfully.

**Generator limitations**
- Quicktype emits an untagged Rust `Payload` enum, not `serde_json::Value`.
- Nested array/object values use `Option<serde_json::Value>` so nested JSON `null` remains valid; the top-level `Event.payload` itself is non-optional and has no null variant.
- Top-level numbers map to `f64`; only frozen `42.5` fidelity is proven.
- TypeScript’s nested array/object members remain `unknown`; the non-null guarantee is for the top-level payload and is static-only.

Recorded the correction and real gate results in `docs/experiment-ledger.md`. I did not commit or push.
