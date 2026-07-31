Working...
Implemented deterministic Mac2 cancellable fixture behavior.

- `gui/src-tauri/src/lib.rs`
  - Added build-scoped `fixture_worker_env()`.
  - **Debug/test builds:** emit the normal six fakeworker events, then use existing `ANANKE_FW_EXIT_DELAY_MS=30000` fixture control to keep the run active for the harness observation window.
  - **Release builds:** retain the prior `6 events / 250 ms cadence / 750 ms pre-exit` behavior unchanged.
  - Added coverage asserting the build-scoped configuration. Initial RED failed because the helper did not exist; GREEN passed after implementation.
  - Corrected the first debug configuration attempt, which suppressed events and broke bridge event coverage; debug fixtures now preserve the canonical event stream.

- Documentation updated:
  - `docs/experiments/mac-native-e2e-contract.md`
  - `docs/experiment-ledger.md`

**Real Mac proof — no cancellation issued**

Evidence:  
`/var/folders/fh/7dlfvrsn5938lw_4z6_pg_th0000gn/T/ananke-mac2-running-proof-final.YppDc2/result.json`

Observed against the rebuilt debug `Ananke.app` and caller-provided WDA:

- 5 static accessibility identifiers verified.
- Health: `● daemon online`.
- Launch fixture.
- Selected state: `● running` at `2026-07-22T09:24:06.271Z`.
- Captured `running.png`.
- Closed only the WDA session; did not cancel the fixture.

**Verification**

- `cargo test --lib --manifest-path gui/src-tauri/Cargo.toml` — 19 passed.
- `cargo fmt --check --manifest-path gui/src-tauri/Cargo.toml` — passed.
- `cargo check --release --lib --manifest-path gui/src-tauri/Cargo.toml` — passed.
- Rebuilt debug `.app` with `npm --prefix gui run tauri:build -- --debug` — passed.
- `npm --prefix tests/mac2 test` — 5 passed.

No commit or push performed.
