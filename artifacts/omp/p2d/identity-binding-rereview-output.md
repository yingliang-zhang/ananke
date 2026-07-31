Working...
## CHANGES REQUESTED

**Blocker — evaluation identity binding is incomplete.**

`project_grill_evaluation` verifies only the outer evaluation’s `(proposal_id, revision, revision_hash)` and locally derived `input_hash` (`gui/src-tauri/src/lib.rs:1239-1249`). It then projects each returned `shown_question`’s own identity fields directly into the public DTO without requiring them to equal the submitted tuple (`:1253-1284`).

A schema-valid private response can therefore carry:

- correct outer evaluation tuple and `input_hash`;
- correct `shown_question_ids`;
- one or more `shown_questions` with a different valid proposal/revision/hash.

That response passes the current guard and crosses the boundary as a public `GrillEvaluation` containing foreign questions—the original identity-binding failure in nested form.

The new swapped-evaluation regression is incomplete: `replace_private_evaluation_identity` changes both the outer evaluation and every nested question (`:2734-2756`). It proves rejection of an outer tuple mismatch, but not the response above. Add a scripted-sidecar regression that leaves the outer tuple/hash and input hash correct while swapping one nested question’s tuple; assert the fixed sanitized error and no DTO/private detail leak. The projector must reject that case before constructing `shown_questions`.

**Confirmed**

- Outer evaluation tuple and locally derived canonical `input_hash` are checked.
- Generic record projection requires returned tuple plus submitted `question_id` for Default, Answer, and Override (`:1296-1303`).
- Identity mismatch errors map to the fixed public unavailable message; no raw daemon value is surfaced.
- Tauri registration exposes exactly the four Grill commands: `evaluate_grill`, `record_grill_default`, `record_grill_answer`, `record_grill_override`.
- No renderer Grill invocation was found. [INFERENCE] The repair itself did not add model/worker/claim/execution behavior; the uncommitted worktree also contains earlier P2a–P2c changes, so Git cannot isolate a separate committed P2d-repair baseline.

**Gates**

- Focused swapped-response test: passed.
- `npm --prefix gui run build:go`; Cargo format check; Rust `--all-targets`: passed, 24 tests.
- Renderer generator/privacy/decoder/typecheck/state gates: passed.
- P2a, P1a, P1c, and P2c verifier and self-test gates: passed.
- `go test ./... -count=1 -timeout=300s`: passed on clean rerun. An initial concurrent run had a `TestSupervisorAdoptCommand` temporary-directory cleanup failure; isolated `-count=5` and the full rerun passed.
