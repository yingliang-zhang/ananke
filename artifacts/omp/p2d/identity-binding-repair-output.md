Working...
Implemented P2d identity binding repair.

- `gui/src-tauri/src/lib.rs`
  - Evaluation projection now requires exact submitted `proposal_id` / `revision` / `revision_hash` and the locally derived private input hash.
  - Default, Answer, and Override record projections now require the submitted Revision tuple and `question_id`.
  - Mismatches return `BridgeError::Protocol`, yielding the fixed sanitized public bridge error before any DTO is exposed.
  - Added scripted Unix-sidecar regression coverage for:
    - swapped evaluation tuple;
    - mismatched evaluation input hash;
    - swapped Default record tuple;
    - swapped allowed Answer and Override question IDs.
  - Regression asserts no wrong DTO or private response detail reaches the public error.

- `docs/experiment-ledger.md`
  - Recorded RED/GREEN TDD evidence, scope preservation, and gate outcomes.

Verification:
- RED: focused swapped-response regression exited `101` before the repair.
- GREEN: focused regression passed.
- `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check`
- `npm --prefix gui run build:go`
- Real-sidecar P2d oracle test passed.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` — 24 passed.
- `node contracts/p2c/verify.mjs`
- `node contracts/p2c/verify.mjs --self-test`

No UI, generated models, workers, claims, execution paths, commits, or pushes changed.
