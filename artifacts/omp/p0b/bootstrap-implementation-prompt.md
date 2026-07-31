Implement the approved P0b renderer-public bootstrap vertical slice in this worktree: /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen.

Read and obey docs/experiments/p0b-renderer-public-bootstrap-contract.md and docs/plans/2026-07-21-p0b-renderer-public-bootstrap.md. Do not read or reuse Arm B. You may use P0a only as historical evidence, not as production source.

Scope: JSON Schema + Quicktype generation for the real Tauri `bootstrap` command only. Add canonical GUI bootstrap schema and deterministic Node 22 generator/check; generate one Rust public model and one TypeScript public model; use those types in `gui/src-tauri/src/lib.rs` bootstrap path and `gui/src/main.ts` only. Preserve `project.root` and `workstream.project_id` exactly.

Strict TDD required:
1. Add focused Rust bootstrap wire-contract test first and run it. Capture expected RED due to absent generated integration.
2. Implement minimal generation/integration.
3. Rerun focused test for GREEN.
4. Add/rerun generator content-drift check, public field privacy check, GUI typecheck, and web build if available.

Constraints: do NOT modify daemon Go transport, private/internal request-response types, `JsonRun -> RunDto`, run/event/cancel/health behavior, or files outside allowed P0b scope. Do not commit/push. Use Node 22 commands with `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH`; do not rely on a user-specific executable path inside committed scripts. Pin required GUI dev dependency and lockfile. Record only real verified P0b evidence in the ledger at the end. Run `git diff --check` and whitelist production scope validation before final response.

Report files changed, RED/Green commands/results, generator/tool versions, verification output, limitations, and a truthful terminal verdict.