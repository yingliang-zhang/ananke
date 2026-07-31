Continue P6 Contract Slice 4 in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` from exact session. The first 900s run correctly completed the RED test/registry/document-test surface but timed out before production implementation.

Current verified RED:
- new files: `repository_worktree_test.go`, `repository_worktree_registry_test.go`, `repository_worktree_document_test.go`;
- `repository_worktree.go` does not exist;
- `go test ./internal/repaircontract -run '^TestP6Slice4'` fails to compile on undefined Slice-4 closed types (`RepositoryWorktreeAction`, `RepositoryWorktreeDisposition`, `RepositoryWorktreeRequirement`, `CommonGitProtectedDomainID`, `CommonGitMemberID`, `RepositoryWorktreeInstallationAuthority`, `RepositoryWorktreeMaterializationObservation`, `VerifiedRepositoryWorktreeSnapshot`, etc.);
- do not delete, weaken, skip, or bulk-rewrite RED vectors merely to compile.

Required continuation:
1. Read all three landed Slice-4 test files and the accepted Slices 1–3 production helpers they reference.
2. Implement `internal/repaircontract/repository_worktree.go` as the pure contract specified in the original prompt. No filesystem/Git/process/network/store effects and no production capability minter from caller bytes.
3. Create/sync `docs/experiments/p6-controlled-repair-repository-authority.md` to the exact machine manifest expected by the document parser. Status must remain candidate pending independent frozen-source review.
4. Ensure exact six-member admin closure, protected common-git domains, config/attributes/filter closure, path alias/symlink/hardlink closure, retained/ambiguous no-cleanup semantics, explicit freshness, and all-success `EffectAllowed=false`.
5. Run Slice-4 focused, registry, and document gates; package single; package count=10; race count=3; vet; gofmt; diff-check. If time is constrained, prioritize compiling production, focused all-Slice4, document sync, package single, vet, and return exact omitted repetition for orchestrator.
6. No commit, provider call, runtime implementation, test weakening, or next-slice work.

Return actual RED/GREEN evidence and changed files. Do not create cron jobs.
