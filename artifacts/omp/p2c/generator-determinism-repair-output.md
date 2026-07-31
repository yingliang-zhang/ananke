Working...
Repaired P2c generator determinism and dead-code warnings.

- `gui/scripts/generate-renderer-public.mjs`
  - Formats every generated Rust DTO and `generated/mod.rs` through `rustfmt --edition 2024` before write/drift comparison.
  - Emits only each DTO’s transitive P2c validator helper set.
  - Preserves custom `Deserialize`, `#[serde(deny_unknown_fields)]`, and semantic validation.
- Regenerated outputs exclusively through the generator; no generated file was hand-edited.
- Added factual RED/GREEN evidence to `docs/experiment-ledger.md`.

Verified:

- `npm --prefix gui run generate:renderer-public` then `check:renderer-public` — passed; repeated for regeneration stability.
- `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check` — passed.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` — 22 tests passed; no warnings emitted.
- `npm --prefix gui run typecheck` — passed.
- Renderer-public privacy check and privacy test — passed.
- Generated TypeScript decoder test — passed.
- `node contracts/p2c/verify.mjs` and `--self-test` — passed.

No Tauri/UI/daemon work; no commit or push.
