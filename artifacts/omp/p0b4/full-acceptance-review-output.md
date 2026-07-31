Working...
## CHANGES REQUESTED

The Rust-side P0b migration is functional, but the roadmap’s full P0b acceptance gates are not met.

### Verified passing

| Area | Evidence |
|---|---|
| Rust public command matrix | `gui/src-tauri/src/lib.rs` returns generated types for all seven commands: `Bootstrap`, `Vec<Run>`, `Run`, `Run`, `Vec<Event>`, `Cancel`, `Health`. |
| Exact public Rust wire fixtures | Dedicated tests cover bootstrap, list-runs, launch-fixture, get-run, list-events, cancel-run, and daemon-health. `cargo test --manifest-path gui/src-tauri/Cargo.toml --lib` passed **17/17**. |
| Public event boundary | The live bridge fixture verifies object, array, string, number, and boolean payloads; `type` is preserved and missing/null payloads are rejected by generated Rust `Event`. |
| Lifecycle smoke | `bridge_bootstrap_launches_lists_events_cancels_and_reconnects` passed: bootstrap → launch → events → cancel → reconnect through the real daemon bridge. This is backend-bridge coverage, not GUI E2E. |
| Generator/drift/current privacy/format stability | `check:renderer-public`, `cargo fmt --check`, then `check:renderer-public` again passed; all generated models are current. The configured privacy scan passed. Generated outputs contain none of `token`, `worker_env`, socket-path, identity-file, adapter-secret, or secret field names. |
| Renderer | `npm --prefix gui run typecheck`, `web:build`, and `test:state` passed. Vite built seven modules. |
| Scope | `git diff --quiet 73fc6de -- internal cmd` returned `0`: no Go daemon or `cmd/` source delta. The tracked delta is limited to the ledger and GUI generator/module/bridge/renderer files; new public Cancel/Health artifacts are under `gui/`. |

### Blocking acceptance gaps

1. **No generated TypeScript JSON decoding proof.**  
   The generator requests TypeScript with `--just-types`; all five generated TS artifacts are interfaces only. There is no TypeScript decoder, golden JSON parsing fixture, or test proving Go-emitted public JSON is accepted through generated TS types. `main.ts` uses `invoke<T>`, which supplies a compile-time type argument but no runtime decode/validation.

   This leaves roadmap acceptance unmet: *“Golden JSON emitted by Go decodes through generated Rust and TypeScript.”*

2. **No GUI E2E.**  
   The passing reconnect test operates directly on `Backend`; it does not launch Tauri, invoke commands from the renderer, or inspect the UI. The repository has no `.github` workflow, Playwright/WebDriver/Tauri-driver setup, or renderer E2E test. `gui/scripts/test-run-state.mjs` only tests `isActiveRunState`.

   This leaves roadmap acceptance unmet: *“Existing GUI E2E still proves bootstrap → launch → events → cancel → reconnect.”*

3. **No CI enforcement for regeneration/drift.**  
   Local package scripts exist, but no repository CI configuration was found (`.github`, GitLab, CircleCI, Azure, Jenkins, Makefile, Taskfile). Therefore the requirement *“CI regenerates and fails on any checked-in drift”* is not established.

4. **Remaining handwritten public DTOs.**  
   `gui/src-tauri/src/lib.rs` still contains:
   - `pub struct RunDto`
   - `pub struct RunDiagnosticsDto`
   - `impl From<JsonRun> for RunDto`

   Repository search found no remaining use of those types. They duplicate the generated public `Run`/`RunDiagnostics` projection and should be removed.

   `EventDto` remains a manually synchronized `pub` + `Serialize` DTO. It is currently used as the raw daemon-response adapter before conversion to generated `Event`, so an internal decoder is justified; its public visibility and serialization are not.

5. **Renderer command-matrix gap.**  
   Rust returns generated `Run` for both `launch_fixture` and `get_run`, but:
   - `main.ts` calls `launch_fixture` without `invoke<Run>`, discarding the result.
   - `main.ts` has no `get_run` consumer.

   This does not break the Rust public boundary, but it does not demonstrate generated TS response handling for those command results.

6. **Privacy enforcement is narrower than the roadmap requirement.**  
   The generator’s enforced scan only rejects `token` and `error`. Current generated fields are safe, but the check would not reject a future schema field such as `worker_env`, socket path, identity-file path, or adapter secret. The roadmap requires that public projections **cannot** expose those classes of data.

### Required before acceptance

- Add a real generated-TypeScript golden-payload decode/validation path and test it against the same public fixtures.
- Add or restore a GUI-level Tauri E2E covering bootstrap → launch → events → cancel → reconnect.
- Add CI that runs generation/drift, privacy, formatter, Rust tests, typecheck, and web build.
- Delete `RunDto` and `RunDiagnosticsDto`; make `EventDto` private/deserialization-only if it remains the raw bridge adapter.
- Either type `launch_fixture` as `invoke<Run>` or document/remove its public renderer result; add a typed `get_run` consumer if it is part of the renderer contract.
- Expand the enforced privacy denylist to the roadmap’s sensitive-field classes.

No files were edited or committed.
