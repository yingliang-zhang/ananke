Working...
## CHANGES REQUESTED

The canonical fixture/verifier layer is strong, but P1c is incomplete at the public DTO/codegen boundary and its schemas do not fully preserve P1a validation semantics.

| Audit area | Result | Evidence |
|---|---|---|
| Seven command mappings | **Change** | The declared pairs are correct: each snake_case Tauri name maps to its hyphenated daemon name in `contracts/p1c/verify.mjs:116-143`. But `daemon` is never consumed; search finds it only in those declarations. `verifyProtocol` validates fixture command keys, inputs, and results (`:590-601`), not the Tauri→daemon mappings. A typo in any daemon mapping would pass verifier and self-test. |
| Public/private boundary | **Pass, with codegen gap** | Contract explicitly keeps `cmd`, `token`, `ok`, raw errors, runtime data, and daemon transport private (`docs/experiments/p1c-task-proposal-public-protocol-contract.md:12-16, 67-80`). P1c verifier recursively rejects private field names and closed-schema violations (`verify.mjs:257-319, 398-456`); self-test proves private fixture/schema rejection. |
| Twelve DTO schema targets | **Change** | P1c verifier correctly inventories 11 documents plus embedded `ProposalActivity` as 12 closed targets (`verify.mjs:94-114, 438-456`). However, the renderer-public generator still inventories only legacy Bootstrap/Cancel/Run/Event/Health models (`gui/scripts/generate-renderer-public.mjs:8-84`); no P1c proposal schemas are code-generated. `gui/src/generated`, `gui/src-tauri/src/generated`, `mod.rs`, and `test-renderer-public.mjs` contain no Proposal DTO artifacts or decoder tests. Thus `check:renderer-public` passes while silently excluding every P1c target. This contradicts the P1c contract’s claim that the generator produces corresponding Rust and TypeScript types. |
| Canonical fixture and manifest | **Pass** | `protocol-v1.canonical.json` is one-line canonical JSON. Its SHA-256 manifest and pinned digest agree: `31a7f02ee79bf5bee66c546433a358bf3d25850e8ba8e9d017d32183d6c489ad`. The verifier checks manifest bytes before parsing, JCS round-trip, UTF-8/BOM, and pinned canonical digest (`verify.mjs:469-481`). |
| P1a policy, IDs, hashes, timestamps | **Change** | Fixture data correctly preserves P1a’s fixed policy, identifier/hash patterns, root revision hash, and semantic timestamp checks. In particular, P1c recomputes the embedded revision hash and requires P1a’s frozen root hash (`verify.mjs:672-676`). But the public schemas weaken P1a timestamps to unconstrained `{ "type": "string" }`: `ProposalList.created_at`, all relevant `ProposalDetail` timestamps, and `ProposalActivity.written_at`. P1a requires semantic UTC RFC3339/RFC3339Nano timestamps. Similarly, P1a’s UTF-8-byte limits are reduced to JSON Schema `maxLength` character limits. |
| Approval/revision P1a invariants | **Change** | `ProposalDetail` admits invalid states that P1a rejects. `decided_at`, `decided_by`, `decision_idempotency_key`, and `reason` are independently nullable/string, with no state coupling; `decided_by` accepts any string (`renderer-public-proposal-detail.schema.json:123-127`). P1a requires all four null for pending/superseded/withdrawn and `local_gui_operator` for terminal decisions. Likewise parent revision fields only have independent type/minimum constraints (`:30-37`), not P1a’s root/immediate-parent relation. The fixture verifier checks its one canonical sample (`verify.mjs:527-532, 558-567`); generated DTO schemas would not enforce these invariants. |
| Embedded revision hash cross-links | **Pass** | Detail Proposal/Revision/Lifecycle/Approval hashes are recomputed and linked; activity records are linked to proposal, revision, hash, approval ID, operation, sequence, and timestamps (`verify.mjs:670-730`). Self-test mutates the embedded revision and proves rejection. |
| Unknown fields | **Pass in P1c verifier** | Every object target is required and `additionalProperties: false`; verifier validates closure and self-test injects an unknown public field and rejects it. The generator privacy gate does **not** cover P1c schemas until the missing inventory is added. |
| Deferred daemon/Tauri/UI integration | **Pass** | Deferral is explicit in the contract and plan. `gui/src-tauri/src/lib.rs:687-746` registers only existing run/lifecycle commands; no P1c Tauri commands, daemon adapters, or UI integration were added. This boundary is correct for P1c. |

### Required corrections

1. Add all 11 P1c schema documents to `rendererPublicModels`; generate and commit Rust/TypeScript artifacts, update `mod.rs`, and add decoder/privacy coverage for every generated public DTO, including embedded `ProposalActivity`.
2. Make the schema contract preserve P1a timestamp and UTF-8-byte constraints, plus Approval state/nullability/actor and Revision parent invariants—or explicitly provide an equivalent generated-boundary validator with direct tests.
3. Make the seven Tauri→daemon mappings mechanically asserted by the verifier and covered by self-test; the current `daemon` values are dead metadata.

### Commands run

All executed successfully:

```text
node contracts/p1c/verify.mjs
node contracts/p1c/verify.mjs --self-test
npm --prefix gui run check:renderer-public
npm --prefix gui run check:renderer-public-privacy
npm --prefix gui run typecheck
```

The green generator/typecheck gates do not clear the missing-P1c-codegen finding because their model inventory excludes every Proposal schema. No files were edited or committed.
