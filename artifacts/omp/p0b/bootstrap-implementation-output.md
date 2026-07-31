Working...
Implemented P0b renderer-public bootstrap vertical slice. No commit or push.

### Changed files
- `gui/contracts/renderer-public-bootstrap.schema.json`
- `gui/scripts/generate-renderer-public.mjs`
- `gui/package.json`
- `gui/package-lock.json`
- `gui/src/generated/renderer-public-bootstrap.ts`
- `gui/src-tauri/src/generated/mod.rs`
- `gui/src-tauri/src/generated/renderer_public_bootstrap.rs`
- `gui/src-tauri/src/lib.rs`
- `gui/src/main.ts`
- `docs/experiment-ledger.md`

### TDD evidence
- Prerequisite: `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm run build:go` passed.
- **RED:**  
  `cargo test bootstrap_public_wire_json_is_frozen --lib`  
  exited `101` with `E0433: cannot find module or crate generated`.
- **GREEN:**  
  Same focused command passed: `1 passed`, `9 filtered`, `0.40s`.

The focused test exercises the real Rust bridge bootstrap result and verifies the full public JSON object, including `project.root` and `workstream.project_id`.

### Generation
- Pinned `quicktype@26.0.0` in GUI dev dependencies.
- Node 22-only generator:
  - `npm --prefix gui run generate:renderer-public`
  - `npm --prefix gui run check:renderer-public`
  - `npm --prefix gui run check:renderer-public-privacy`
- Generated:
  - Rust public model: 40 LOC plus 3-LOC module declaration.
  - TypeScript public model: 19 LOC.
- Drift proof: a temporary generated-Rust mutation made check mode fail with `Generated renderer-public models drifted`; regeneration restored the output and check mode passed.
- Privacy check passed: generated public models expose neither `token` nor raw daemon `error`.

### Tool versions
- Node: `v22.22.3`
- Quicktype: `26.0.0`
- Cargo: `1.97.1`

### Verification
Passed:
```text
cargo test bootstrap_public_wire_json_is_frozen --lib
cargo fmt -- --check
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run check:renderer-public
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run check:renderer-public-privacy
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run typecheck
PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run web:build
git diff --check
```

Web build passed with Vite `7.3.6`: 7 modules transformed, completed in 54 ms.

Production-path whitelist validation listed only the approved GUI paths plus `docs/experiment-ledger.md`. `git diff --check` produced no output.

### Limitations
- Intentionally unchanged: Go daemon transport, private/internal daemon types, `JsonRun -> RunDto`, and run/event/cancel/health behavior.
- Existing untracked `artifacts/omp/p0b/` and approved P0b contract/plan files remain in the worktree; they were not modified.

**Terminal verdict: PASS.**
