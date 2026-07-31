Working...
Implemented the P0b.2 formatter-stability repair.

- `gui/scripts/generate-renderer-public.mjs`: `rustModuleSource` now emits Rust modules in rustfmt’s canonical order:
  `bootstrap`, `event`, `run`.
- Regenerated `gui/src-tauri/src/generated/mod.rs`.
- `docs/experiment-ledger.md`: added a correction explaining the prior separate checks did not prove formatter/generator compatibility, plus current verified repair evidence.

Verification passed:

```sh
npm --prefix gui run check:renderer-public \
  && cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check
```

```sh
cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check \
  && npm --prefix gui run check:renderer-public
```

Both sequences passed; each generator check reported canonical-schema agreement and each formatter check exited 0.

Additional gates:

```sh
npm --prefix gui run build:go \
  && cargo test list_events_public_wire_json_preserves_arbitrary_payloads \
       --lib --manifest-path gui/src-tauri/Cargo.toml
# PASS: 1 passed, 11 filtered, 1.42s

npm --prefix gui run check:renderer-public-privacy
# PASS

npm --prefix gui run typecheck
npm --prefix gui run web:build
# PASS — Vite 7.3.6, 7 modules, 56ms

git diff --check
# PASS — no output
```

Full scope audit (`git status --short --untracked-files=all`) found five modified and 29 untracked paths, all within the established P0b GUI renderer-public, `docs/`, or `artifacts/omp/p0b*` scope.

**Terminal verdict: PASS — generator output is formatter-stable and the required P0b.2 gates are order-independent.** No commit or push performed.
