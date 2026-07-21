Resume this exact Arm A session. You may use tools now and must finish the remaining implementation and verification work. Do not read or list Arm B or any other P0a arm; remain limited to this worktree plus the explicitly authorized shared contract/fixture paths.

The synthesis evidence established this is not complete: after changing `additionalProperties` and the TypeScript unknown-property setting, final multi-language verification was not rerun; `scripts/verify.mjs` only inventories artifacts and does not satisfy the frozen contract.

Finish requirements:
1. Keep all changes inside experiments/p0a/jsonschema-quicktype/. Do not change production DTOs, transport, shared contract/fixtures, root manifests, or commit/push.
2. Make one self-contained reproducible verify command that performs generation, Go/Rust/TS fixture behavior checks, preserves all payload variants, absent optional behavior, tolerance of the unknown response field, and proves the generated TypeScript public tree has no `token` field.
3. Add real drift rejection: alter a generated artifact in a controlled temporary copy or then restore it; the check must fail because generated output differs, not because a file is missing.
4. Prove clean regeneration: remove/recreate generated output and demonstrate a clean generated tree under the declared generator.
5. Run an optional-field schema-evolution probe and record its exact compatibility outcome.
6. Record direct command output, exact Quicktype/TypeScript versions, timing, file/LOC counts, dependency footprint, original RED evidence, final GREEN evidence, and terminal verdict in README.md.
7. Ensure Cargo build output is not part of source artifacts (use a target directory under ignored `.tools` or clean it). Run git diff --check.

Keep stdout compact. Do not claim VALIDATED unless every required check actually passes; otherwise report BOUNDED — CHANGES REQUESTED or REJECTED with the exact missing requirement.
