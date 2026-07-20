# Handoff — Ananke GUI v0.1 accepted

## Goal

Deliver the first operator-facing desktop proof over the real Go durable lifecycle path.

## Completed

- Slice-001 durable lifecycle is on `main` as `df72fe9`.
- GUI v0.1 is implemented on `feat/gui-v0.1`: Tauri 2 + Vanilla TypeScript → Rust bridge → Go daemon sidecar → supervisor/worker → SQLite journal → activity UI.
- Go daemon added authenticated `list-runs` with project/workstream filtering; no secret fields are projected.
- Bridge uses a private owned `0700` per-user `/tmp` runtime directory, a `0600` app-data token, narrow stale-socket classification, safe Go socket removal, release-safe project root, and no renderer token/socket path.
- Rust public `Backend` E2E covers bootstrap → list → launch → events → durable cancel → reconnect.
- Independent final B1 review: `artifacts/omp/gui-v0.1/b1-final-review-output.md` — `VERDICT: ACCEPT`.

## Verification

- `cargo test --manifest-path gui/src-tauri/Cargo.toml`: 9 tests PASS.
- `npm --prefix gui run test:state`, typecheck, Vite build: PASS.
- `go test ./internal/store ./internal/supervisor ./internal/lifecycle -count=1 -timeout 300s`: PASS.
- `CI=true npm run tauri:build` from `gui/`: `Ananke.app` PASS, with `ananke`, `ananke-supervisor`, `ananke-fakeworker` resources.

## Current state / known boundary

- The deliverable is a macOS `.app` proof. Tauri DMG creation is excluded because macOS 27 rejects the version of `hdiutil create` used by Tauri's create-dmg script. This is packaging compatibility work, not a GUI authority/runtime defect.
- Native screenshot capture is unavailable in this no-display agent session; no visual screenshot claim was made.
- The GUI source must now be staged/committed/pushed as a feature-branch commit, then manually reviewed/launched by the user.

## Changed paths

- `.gitignore`, `internal/lifecycle/engine.go`, `internal/lifecycle/engine_test.go`, `internal/lifecycle/engine_shutdown_test.go`
- `gui/` (Tauri/Vite/Rust bridge/UI/tests/config/icon)
- `docs/experiment-ledger.md`, `docs/handoff.md`

## Next step

Commit and push `feat/gui-v0.1`; optionally open/merge after user acceptance of the native UI.
