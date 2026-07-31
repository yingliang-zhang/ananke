Working...
Implemented P2b deterministic Grill runtime.

**Runtime**
- `internal/store/grill.go`
  - Closed P2a input types, JCS SHA-256 hashing, six-rule pure evaluator, bounded display/append behavior.
  - Exact revision identity validation, input-hash/rule-version failure modes.
  - Append-only Question/Default/Answer/Override persistence; idempotent record replays; scope-only override.
  - Restart, concurrency, ten-question cap, and independent revision streams supported.
- `internal/store/store.go`
  - Migration v10: `grill_evaluations`, `grill_records`, sequence/uniqueness constraints, insert-only triggers.
- `internal/lifecycle/engine.go`
  - Private daemon commands: `evaluate-grill`, `record-grill-default`, `record-grill-answer`, `record-grill-override`.
  - Closed `grill` JSON decoding via `DisallowUnknownFields`; injected Revision prose is rejected before store evaluation.
- `gui/src-tauri/src/lib.rs`
  - Private native/daemon Grill wire types and bridge methods.
  - No generated renderer DTO, registered Tauri command, or renderer UI/model call.

**Test maintenance**
- Completed fixture decoder structs so `DisallowUnknownFields` accepts every frozen P2a fixture field.
- Corrected the independent second-revision record expectation from six to five: `ListGrillRecords` contains five initial Question rows; the sixth initial write is the separate evaluation row.
- Updated the historical migration assertion to track `len(migrations)` and verify v8–v10 history.

**Evidence**
- Focused: `go test ./internal/store ./internal/lifecycle -run Grill` — passed.
- Full Go: `go test ./...` — passed; 3 test packages, 3 packages without tests.
- Full Rust: `cargo test --manifest-path gui/src-tauri/Cargo.toml` — passed; 21 tests across 3 suites.
- TypeScript/public-contract gates — passed:
  `typecheck`, renderer-public generation/privacy checks, run-state, public decoder, and privacy tests.
- P2a/P1a/P1c gates — passed:
  `node --check contracts/p2a/verify.mjs`, both P2a verifier modes, P1a verifier, P1c verifier.
- Ledger: `docs/experiment-ledger.md` lines 758–805.
- No commit or push run.
