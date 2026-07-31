Working...
Repaired all P2c review blockers, generator-managed.

- Generated TS validators now enforce each Grill schema’s closed fields, regex/min/max/const/timestamp rules, plus P2b identity, fixed rule, priority/order, deferred, and `new_records` invariants.
- Generated Rust DTOs now deserialize through `#[serde(deny_unknown_fields)]` wire types and validate equivalent field/cross-field invariants.
- Added generated Rust and TS adversarial decoder coverage for all nine Grill DTO targets.
- Expanded P2c privacy mutations across every declared private-field fragment and every Grill target.
- Appended factual TDD RED/GREEN evidence to `docs/experiment-ledger.md`.
- No Tauri command, daemon bridge, renderer UI, or commit added.

Verified GREEN:

- `node contracts/p2c/verify.mjs`
- `node contracts/p2c/verify.mjs --self-test`
- `npm --prefix gui run check:renderer-public`
- `npm --prefix gui run check:renderer-public-privacy`
- `npm --prefix gui run test:renderer-public`
- `npm --prefix gui run test:renderer-public-privacy`
- `npm --prefix gui run typecheck`
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --lib` — 22 passed; tool reported 18 warnings.
