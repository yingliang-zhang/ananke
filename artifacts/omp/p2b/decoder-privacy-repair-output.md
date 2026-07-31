Working...
Repaired P2b M1.

- `internal/lifecycle/engine.go`
  - Retained `DisallowUnknownFields`.
  - All Grill JSON decode failures—including trailing/multiple JSON values—now return exactly `invalid grill request`.
  - Removed every `err.Error()` concatenation from Grill payload decoding.

- `internal/lifecycle/grill_api_test.go`
  - Live daemon-socket regression injects nested `raw_revision_prose_secret` and a sentinel value.
  - Asserts rejection, exact stable error, absent `grill_evaluation`, and no field name, value, or `json: unknown field` diagnostic in the serialized daemon response.

- Strict TDD evidence:
  - RED: focused lifecycle test failed with the previously leaked `json: unknown field "raw_revision_prose_secret"` response.
  - GREEN: same test passed after the decoder repair.

- Ledger: recorded actual RED/GREEN and gate evidence in `docs/experiment-ledger.md`.

Verification passed:
- `go test ./internal/store ./internal/lifecycle -run Grill -count=1 -timeout 120s`
- `go test ./... -count=1 -timeout 300s`
- `cargo test --manifest-path gui/src-tauri/Cargo.toml` — 21 tests
- All GUI public gates
- P2a syntax/verifier/self-test; P1a verifier/self-test; P1c verifier/self-test
- `gofmt -d internal/lifecycle/engine.go internal/lifecycle/grill_api_test.go` — clean

No UI, model, worker, claim, or execution changes. No commit or push run.
