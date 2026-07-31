Working...
## CHANGES REQUESTED

The frozen canonical vector is sound, but generated DTO decoders do **not** preserve the required P2b semantic invariants at the renderer boundary.

### Blocking findings

1. **Generated TS decoders discard schema constraints and relational invariants.**

   `quicktype` generated TS accepts values that violate the nine DTO schemas and P2b contract:

   - `EvaluateGrillInput` accepts `revision: 0` and a non-SHA-256 `revision_hash`.
   - `GrillEvaluation` accepts six shown questions/new IDs, `new_records: 7`, reordered questions/IDs, and a shown question with mismatched revision, arbitrary `question_id`, and `blocking: false`.
   - This defeats exact identity, fixed order, and display/append bounds despite the JSON schemas declaring them.

   Direct decoder probe observed:

   ```text
   acceptsRevisionZero: true
   acceptsInvalidHash: true
   acceptsSixQuestions: true
   acceptsMismatchedQuestionIdentity: true
   acceptsReorderedQuestions: true
   ```

   `renderer-public-grill-evaluation.schema.json` constrains individual fields, but neither it nor generated code enforces root/question identity equality or `new_question_ids` ↔ `shown_questions` order.

   **Required fix:** provide contract-aware runtime validation for the generated Rust and TS boundary types. It must enforce regex/min/max/const constraints plus the P2b cross-field identity and ordering invariants. Add adversarial decoder tests for every case above.

2. **Generated Rust DTOs are open to unknown fields.**

   All nine Rust modules derive ordinary `Deserialize`; none uses `#[serde(deny_unknown_fields)]`. For example:

   - `gui/src-tauri/src/generated/renderer_public_grill_evaluate_input.rs`
   - `gui/src-tauri/src/generated/renderer_public_grill_evaluation.rs`
   - the seven remaining Grill DTO modules

   Therefore, a future Tauri handler deserializing these structs would not reject renderer-supplied `command`, `model`, `prose`, `approval`, `execution`, transport, or raw-error fields. They are silently ignored by standard Serde struct deserialization.

   **Required fix:** make generated Rust public DTO deserialization closed, then test all nine Rust DTOs against unknown/private-field injection. This alone is insufficient; retain the semantic validation from finding 1.

3. **The generic generator privacy gate is weaker than P2c’s declared privacy contract.**

   `generate-renderer-public.mjs` denies token/error/socket/command/prose/etc., but omits `model`, `approval`, `execution`, `raw`, `prompt`, and `runtime`. `test-renderer-public-privacy.mjs` injects only `token` into P2c targets.

   `contracts/p2c/verify.mjs` correctly protects the current schemas using a broader list, so the frozen P2c artifact passes today. The generator-level privacy gate can still regress on the explicit P2c field classes without failing.

   **Required fix:** align the generator denylist and privacy mutation tests with P2c’s `privateFieldFragments` policy.

### Confirmed

- **Inventory:** eight schema documents yield the required **nine DTO targets**, including `GrillQuestion` selected from `GrillEvaluation`; generator inventory, Rust `mod.rs`, and generated Rust/TS modules agree.
- **Canonical P2b parity:** the P2c fixture retains the frozen P1a identity, `blocked` status, first-five question ordering, `autonomy_budget` deferral, and bounds of `5/5/1/6`. Its ordered IDs match the P2a frozen initial vector; P2b’s lifecycle test asserts the matching `new_records: 6`.
- **Private-field rejection in the existing TS structural decoder:** `cmd`, `command`, `model`, `prose`, `approval`, `execution`, `input_hash`, `rule_version`, `socket_path`, and `error` are rejected as unknown fields.
- **No P2c runtime surface added:** candidate changes are schemas, generated modules, generator/tests, and `contracts/p2c`. No Grill Tauri command is registered, no renderer call site/UI was added, and no P2c daemon bridge was added. Existing P2b private bridge methods remain private and predate this candidate.

### Gates run — all passed

```text
node contracts/p2c/verify.mjs
node contracts/p2c/verify.mjs --self-test
npm --prefix gui run check:renderer-public
npm --prefix gui run test:renderer-public-privacy
npm --prefix gui run test:renderer-public
npm --prefix gui run typecheck
cargo test --manifest-path gui/src-tauri/Cargo.toml --lib
```

Result: all passed; Rust suite: **21 passed**. Green gates do not cover the semantic and Rust-closure failures above.
