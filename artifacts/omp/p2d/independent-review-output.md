Working...
## CHANGES REQUESTED

**Major — private-result identity is not bound to the public request.**

- `Backend::evaluate_grill` derives the conservative private input and local hash, but then calls `project_grill_evaluation` with only the daemon payload. The projector validates a payload’s internal P2c shape, yet never requires its `proposal_id`, `revision`, `revision_hash`, or private `input_hash` to equal the request/local derivation.  
  `gui/src-tauri/src/lib.rs:1002-1015,1207-1256`
- All three record paths similarly project only against expected schema version; they do not require returned revision tuple or `question_id` to match the submitted public input.  
  `gui/src-tauri/src/lib.rs:1018-1110,1258-1290`

A schema-valid stale/swapped daemon response for a different persisted revision—or a different allowed question for a record command—would cross the public boundary. It would be sanitized and P2c-valid, but would not be the result for the renderer’s immutable tuple. This violates the requested exact identity behavior.

**Required repair**

1. Pass expected revision identity and locally derived evaluation hash into the projection functions.
2. Reject/sanitize any private evaluation whose tuple or hash differs.
3. Reject/sanitize any private record whose tuple or question ID differs.
4. Add conversion-boundary regressions for swapped evaluation tuple, mismatched evaluation hash, and mismatched record tuple/question. The current sidecar test covers missing identity and six-question overflow, not valid-but-wrong private result identity.

**Audit results otherwise**

- Exactly four Grill Tauri commands are registered: `evaluate_grill`, `record_grill_default`, `record_grill_answer`, `record_grill_override`. No Grill renderer invocation/UI was added.
- Generated Rust/TypeScript P2c DTOs are closed and reject declaration, input-hash, rule/schema, model, execution, and private-transport injection.
- Bridge derives fixed conservative declarations locally; lifecycle keeps private Grill transport closed; store preserves append-only review rows and writes Defaults as `deterministic_grill`.
- Tauri maps bridge failures through fixed public messages; raw daemon errors do not reach the public error string.
- Ordering, bounds, replay, reconnect, cap rejection, and public-field allowlisting are covered by the real-sidecar test, but the missing response-to-request binding remains.

**Gates run**

- `go test ./... -count=1 -timeout=300s` — PASS.
- `cargo fmt --check` — PASS.
- `npm --prefix gui run build:go` — PASS.
- `cargo test --all-targets` — PASS, 23 tests.
- Exact real-sidecar test `tests::bridge_grill_projects_p2c_oracle_through_sidecar_and_sanitizes_failures` — PASS.
- Renderer generation, privacy, DTO, typecheck, and state gates — PASS.
- P2a (`--check`, verify, self-test), P1a, P1c, and P2c verifier/self-test gates — PASS.

No files were edited or committed.
