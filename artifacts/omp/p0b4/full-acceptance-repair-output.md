Working...
Implemented P0b acceptance repairs. No commit/push.

- Generated TypeScript now has Quicktype runtime `Convert` decoders. Shared golden fixture: `gui/contracts/fixtures/renderer-public-golden.json`.
  - Rust and TypeScript both decode/re-serialize Bootstrap, Run, all five Event payload kinds, Cancel, and Health.
  - Renderer validates every Tauri command response at runtime; `launch_fixture` and `get_run` decode as generated `Run`.

- Removed obsolete `RunDto` / `RunDiagnosticsDto`.
  - `EventDto` is private and `Deserialize`-only; generated `Event` remains the public boundary.

- Expanded schema-enforced privacy denylist and regression coverage for:
  - `token`, `error`, `worker_env`, socket paths, identity files, adapter secrets.

- Added CI workflow: `.github/workflows/p0b-renderer-public.yml`.
  - Regeneration + drift rejection, privacy checks/tests, Rust format/tests, TypeScript typecheck, state test, and web build.

- Updated `docs/experiment-ledger.md` with only observed RED/GREEN evidence and gate results.

**Verification passed**
- `npm --prefix gui run build:go`
- `check:renderer-public`
- `check:renderer-public-privacy`
- `test:renderer-public`
- `test:renderer-public-privacy`
- `test:state`
- `typecheck`
- `cargo fmt --manifest-path gui/src-tauri/Cargo.toml --check`
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --lib` — 18 passed
- `web:build` — Vite built 12 modules
- `git diff --quiet 73fc6de -- internal cmd` — passed; Go daemon transport/source scope unchanged.

**GUI E2E blocker**
- Installed `tauri-driver v2.0.6` to attempt real Tauri WebDriver coverage.
- On this Darwin arm64 host, `tauri-driver --help` exits with: `tauri-driver is not supported on this platform`.
- `safaridriver` exists but is not a Tauri app WebDriver bridge. The GUI lifecycle E2E was therefore not falsely claimed; the concrete host blocker is recorded in the ledger.
