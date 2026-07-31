Working...
# P6 Slice 4 Repair P1+P2 — Final Report

## Gate evidence (exact outputs)

| Gate | Command | Result |
|---|---|---|
| 1 | `go build ./... && go vet ./...` | **PASS** (no output) |
| 1a | `go vet ./internal/...` | **PASS** (no output) |
| 2 | `go test ./internal/repaircontract -run '^TestP6Slice4' -count=1` | **PASS** `ok github.com/yingliang-zhang/ananke/internal/repaircontract 13.160s` |
| 3 | `go test ./internal/repaircontract -count=1` | **PASS** `ok .../repaircontract 17.730s` |
| 4 | focused `-count=10` | **skipped** per orchestrator instruction in resume prompt (runs independently) |
| 5 | `go test -race ./internal/repaircontract -run '^TestP6Slice4' -count=3` | **PASS** `ok .../repaircontract 383.841s` (no race reports) |
| 6 | `go build -tags 'ananke_real_provider_canary ananke_test_runtime_authority' ./...` + `go vet -tags ... ./...` + `go test -tags ... -run '^$' ./internal/repaircontract` | **PASS** all three (test compile reports `1 packages ok`) |
| 7 | `gofmt -d` on all five Slice-4 Go files | **empty** |
| 7 | `git diff --no-index --check /dev/null <file>` ×6 untracked files | exit=1, **0 diagnostic bytes each** (expected content-diff exit) |
| 7 | scoped tracked `git diff --check` | **PASS, clean** |

`gofmt -l ./internal/repaircontract ./cmd` flags only `cmd/ananke-trusted-supervisor-transport/main_test.go` — a tracked P5 file, unmodified by this work (`git diff HEAD -- cmd/` is empty); pre-existing on HEAD. Do NOT run full `./...` suite per prompt (pre-existing `trustedsupervisor` environmental cluster on pristine HEAD).

## Final Slice-4 vector count

258 (226 prior + 32 new P1/P2 vectors), executed in order by `TestP6Slice4ExecutableVectorRegistry`; prior 91 and Slice-3 99 inventories untouched.

## Changed files (6, all pre-existing untracked Slice-4 files; no commit)

- `internal/repaircontract/repository_worktree.go`
- `internal/repaircontract/repository_worktree_test.go`
- `internal/repaircontract/repository_worktree_registry_test.go`
- `internal/repaircontract/repository_worktree_document_test.go`
- `internal/repaircontract/repository_worktree_repair_c_test.go`
- `docs/experiments/p6-controlled-repair-repository-authority.md`

A scratch docgen test file was created transiently and deleted; workspace contains exactly the six files above.

## Frozen computed values

- verifier authority hash: `sha256:24538f3fd98a3df4208aca485cdfb398e018a649c48e72d7a0fec2aa865bab2e`
  (verifier `controlled_repair_repository_worktree_verifier_v1`, release pins `sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a`, 7 kinds)
- materializer profile hash: `sha256:93b63bc17d7dc891ca6a9ed99009d3161a2b2b494a4126e1bc984621689e3180`
  (ID `installed_git_2_54_detached_worktree_materializer_v1`, `git_version=2.54`)
- new canonical fixture `snapshot_integrity_hash`: `sha256:6ca353f8a8091de3d84bddb8fd69601cd78ad7fd21bde2e8034c40eb034be39b`
- new canonical fixture `worktree_slot_path_hash`: `sha256:6bf066f93836c8ccf72560b69f159531f8af5f923f285393e42d0c1ffa4b4e22` (= `sha256("worktrees/attempt_1_materialization_worktree_001")`)
- (fixture also changed: `observation_hash` `sha256:be625f…346b9e`, `canonical_sha256` `sha256:3816a2…731c63`, `common_git_delta_hash` `sha256:d16e53…42f17c`, `config_closure_hash` `sha256:4a35bf…956c83`; all live in the machine-contract block.)

## What was implemented (frozen design A–F)

A: compiled self-hashed `RepositoryWorktreeMaterializerProfile` + `RepositoryWorktreeVerifierAuthority` derived once at init via `mustDerive…` panic-on-mismatch; `FrozenRepositoryWorktreeMaterializerProfile()`/`FrozenRepositoryWorktreeVerifierAuthority()` accessors; `EvaluateRepositoryWorktree` and `VerifyRepositoryWorktreeInstallation` first re-establish fresh `VerifyReleaseTrust(...)` and derived-equals-frozen authority. B: `RepositoryWorktreeVerificationSeal` replaces all seven `fixedHash` labels; snapshot carries `verifierAuthorityHash` + seven seals; intact recomputes every seal from decoded observation + booleans + evaluator-derived authority. C: `mintVerifiedRepositoryWorktree` deleted; only inline mint in `new_exact`; capability carries `verifierAuthorityHash` + `verificationSealsHash`; intact re-establishes provenance under the frozen authority. D: `HEAD`/`ORIG_HEAD`/`commondir` content hashes must equal authorization-derivable digests; three forged-content regression vectors (fully self-consistent seals+integrity) rejected at closure. E: `VerifyRepositoryWorktreeInstallation` mints opaque `VerifiedRepositoryWorktreeInstallation` requiring fresh release trust, frozen profile equality, slot grammar `attempt_<n>_materialization_worktree_001`, and `sha256("worktrees/"+slot)`; evaluator consumes the opaque capability; installed-root anti-alias in exact closure. F: test minters use production seal derivation; 32 new vectors (name list in machine block); document rewritten (verifier authority, seals + explicit non-claim, Git 2.54 freeze, slot grammar, anti-alias) with status still candidate pending frozen-source review.

## Deviations (all fail-closed strengthenings or explicit instructions)

1. `verifiedRepositoryWorktreeInstallationIntact` additionally re-checks the frozen profile and slot-grammar/path-hash derivations beyond the enumerated integrity/verifier/authorization/attempt match — matches package intact-check idiom; can only reject more forgeries.
2. `verifiedRepositoryWorktreeSnapshotIntact` also requires the evaluator-supplied authority's self-hash to recomputE — rejects a zero/invalid authority argument.
3. Capability seal recomputation uses the fixed `new_exact` mint invariants (all four booleans true), since the capability type stores no snapshot booleans and exists only for `new_exact` where they are definitionally true (explicit reading of C.3).
4. Stale-seal vectors graft another kind's valid same-snapshot seal (misplaced, genuinely stale) instead of synthetic hashes.
5. `-count=10` gate skipped on explicit orchestrator instruction (resume prompt); all other gates run exactly as specified.
6. Doc `worktree_slot_grammar` carries the literal string `attempt_<attempt_number>_materialization_worktree_001`; the document test re-derives it from compiled slot prefix/suffix constants (the derivation itself stays in code).

No commit made. No cron jobs created. No files outside the six Slice-4 files touched (only read elsewhere).
