Working...
## ACCEPT

- `project_grill_evaluation` derives the expected submitted tuple and local canonical-input hash in `Backend::evaluate_grill`, then rejects before projection unless the outer `(proposal_id, revision, revision_hash)`, rule version, and `input_hash` match. `gui/src-tauri/src/lib.rs:1019-1038`, `1232-1257`.
- Before constructing the public `shown_questions` JSON, it checks **every** nested question tuple against that same expected tuple. `lib.rs:1244-1248`.
- `project_grill_record` rejects mismatched record tuple or requested `question_id` before its public-field allowlist projection; all three record commands share that path. `lib.rs:1041-1135`, `1292-1331`.
- Regression is adequate: `bridge_grill_rejects_schema_valid_nested_shown_question_identity_swap` keeps the outer tuple/hash correct, alters only one nested question to a valid foreign tuple, and requires an `Err` rather than any public DTO. The scripted-sidecar helper asserts the fixed sanitized public error and rejects leaked tuple/hash details. `lib.rs:2611-2660`, `2744-2775`.
- Record regression covers an incorrect default-record tuple and incorrect answer/override question bindings through the shared projection path. `lib.rs:2503-2608`.

Scope:
- Exactly four Grill Tauri commands are exposed and registered: `evaluate_grill`, `record_grill_default`, `record_grill_answer`, `record_grill_override`. `lib.rs:1505-1565`.
- No renderer UI source change (`gui/src/main.ts` is absent from the patch); no model, worker, claim, or execution implementation was added. The lifecycle diff is confined to private Grill response/record projection and those four handlers.
- `git diff --check HEAD` passed. No edits or commits performed.

Gates passed:
- `node contracts/p2c/verify.mjs`
- `node contracts/p2c/verify.mjs --self-test`
- `npm --prefix gui run check:renderer-public`
- `npm --prefix gui run check:renderer-public-privacy`
- `npm --prefix gui run test:renderer-public`
- `npm --prefix gui run typecheck`
- `npm --prefix gui run build:go`
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --lib bridge_grill_rejects_schema_valid -- --nocapture` — 2 passed
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --lib` — 25 passed
- Focused Go Grill lifecycle/store tests — passed.
