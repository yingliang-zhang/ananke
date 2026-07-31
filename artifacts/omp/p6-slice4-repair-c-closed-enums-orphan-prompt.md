Implement only P6 Slice 4 Repair C (closed enums plus explicit orphan semantics) in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. No commit. Do not touch trusted-supervisor/P5 files. Read the rejected review at `artifacts/omp/p6a/slice4-closure-review-rejected.md`, but address only its P3 finding. P1/P2 remain intentionally open and Slice 4 remains rejected after this task.

Verified P3 reproductions:
- unknown `RepositoryWorktreeAmbiguityReason`, `RepositoryDescriptorID`, `CommonGitMemberID`, `CommonGitMemberSemantic`, `AddedMemberIDs`, and `CommonGitProtectedDomainID` values decode successfully;
- on retain-for-human status paths they can return waiting-for-human with no capability;
- exact paths deny authority, but schema closure is false;
- no explicit orphan state/reason/vector exists.

Required strict TDD:
1. Add closed validators for every declared enum used in canonical observations. Decode must reject unknown ambiguity reasons, descriptor IDs, member IDs, member semantics, added member IDs, and protected domain IDs before any evaluator path. Preserve already closed state/delta/kind/head/status/action behavior.
2. Add explicit orphan semantics as closed constants, not a free-form identifier:
   - a distinct status-only orphan worktree state;
   - an exact orphan ambiguity reason;
   - `status_only` may classify it only as waiting-for-human/human-review with nil capability and `EffectAllowed=false`;
   - `admit_new_materialization` and every effect/cleanup action reject;
   - orphan can never mint `VerifiedRepositoryWorktree` and grants no cleanup/delete/prune/remove/ref/commit/push/merge/launch authority.
3. Define exact valid state/reason combinations:
   - exact new/retained require empty reason;
   - retain-for-human requires one of the existing closed non-orphan reasons;
   - orphan state requires exactly the orphan reason;
   - orphan reason on any other state rejects.
4. Add executable registry probes for each unknown enum at decode and evaluation boundaries, every state/reason cross-product, explicit orphan status, orphan attempted admit/effects, and orphan freshness N-1/N/N+1. No generic vector name without an executable probe.
5. Update machine document counts/inventory/status while still saying `candidate_pending_independent_frozen_source_review` and explicitly noting P1/P2 remain unresolved. Do not overclaim acceptance.
6. Preserve prior 91 and Slice-3 99 inventories exactly. Do not weaken canonical/JCS/unknown-member checks or mutate tests merely to fit implementation.
7. Run RED; focused Slice4; package single; focused count=10; race count=3 if time; vet/gofmt/diff-check. No provider, filesystem/Git runtime, signature design, production verifier, installation-authority redesign, cleanup, or next-slice work.

Return exact RED/GREEN evidence, new vector count, and changed files. Do not create cron jobs.
