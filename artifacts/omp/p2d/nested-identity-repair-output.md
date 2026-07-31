Working...
Implemented P2d nested shown-question identity binding.

- `gui/src-tauri/src/lib.rs`
  - `project_grill_evaluation` now rejects every nested `shown_question` whose `proposal_id`, `revision`, or `revision_hash` differs from the submitted identity.
  - Guard executes before projected public Question JSON / `GrillEvaluation` construction.
  - Retains outer tuple, local `input_hash`, rule-version, and `shown_question_ids` checks.
  - Added scripted Unix-sidecar regression: correct outer tuple/hash/IDs, one individually valid foreign nested tuple; asserts fixed sanitized unavailable error, no returned DTO, and no swapped/private detail leak.

- `docs/experiment-ledger.md`
  - Added factual repair and verification evidence.
  - Note: the new sidecar regression was initially green because the generated P2c decoder already rejected the mismatch *after* public JSON assembly. The explicit bridge guard removes reliance on that late defense.

Verification passed:

- Focused nested-sidecar regression: passed.
- `npm --prefix gui run build:go`
- `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check`
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` — 25 tests passed.
- `go test ./... -count=1 -timeout=300s`
- Renderer generation/privacy/decoder/typecheck/state gates.
- P2a/P2c and P1a/P1c verifier plus self-test gates.

No UI, model, worker, claim, execution, protocol, or command-registration changes.
