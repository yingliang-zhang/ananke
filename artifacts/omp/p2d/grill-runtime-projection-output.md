Working...
Implemented P2d Grill runtime public projection.

- Added only contract-approved Tauri commands:
  - `evaluate_grill`
  - `record_grill_default`
  - `record_grill_answer`
  - `record_grill_override`
- Public commands accept/return generated P2c Rust DTOs. Private daemon envelope, hashes, rule/schema versions, and raw errors are allowlisted out before renderer projection.
- The bridge creates a canonical conservative P2a review input from the immutable Revision tuple, hashes it locally, and calls the existing authenticated private Grill commands.
- Extended private daemon responses with persisted shown Question records and durable action records; no new daemon command was registered.
- Fixed P2b Default records to persist `written_by: "deterministic_grill"`, matching `GrillDefaultRecord`.
- Corrected generated P2c `new_records` validation for a deferred Question appended after an already-existing Evaluation row.
- Added real-sidecar Rust coverage using `contracts/p2c/fixtures/protocol-v1.canonical.json` as the public wire oracle:
  - eval → idempotent default → answer → override → re-eval
  - unchanged replay and reconnect
  - missing Revision identity sanitization
  - private six-question cap rejection
  - no raw daemon/private transport leakage.
- Recorded RED/GREEN and gate evidence in `docs/experiment-ledger.md`.

Verification observed:

- `go test ./... -count=1 -timeout=300s` — passed.
- `cargo fmt --check`, `npm --prefix gui run build:go`, `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` — passed; 23 tests.
- Renderer-public generation, privacy, decoder, TypeScript, state, and production web-build gates — passed.
- P1a, P1c, P2a, and P2c verifier/self-test gates — passed.
- No renderer UI, model, worker, claim, approval, execution, commit, or push added/run.
