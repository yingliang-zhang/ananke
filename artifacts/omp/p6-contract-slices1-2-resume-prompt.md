Resume exact session `019f9e80-9d08-7000-9fb2-ff5404cc80f5` in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`.

Do not repeat research or rewrite the RED tests. The required RED was already captured at 13:08:27: `CanonicalBytes`, `ContractFixture`, `ImmutableDispatch`, and related contract symbols are undefined. Continue from the implementation design already reasoned about in the session.

Now:
1. Implement `internal/repaircontract/contract.go` and `canonical.go` minimally to satisfy the existing 718-line RED contract suite while preserving every closed-schema/semantic requirement.
2. Generate canonical `internal/repaircontract/testdata/p6-contract-v1.json` with exact acceptance-vector inventory.
3. Write `docs/experiments/p6-controlled-repair-supervisor-contract.md`, accurately limited to pure Slices 1–2 contract/fixture/verifier with no runtime/storage claim.
4. Run focused package test count=1, then count=10, race count=3, gofmt, package vet, and git diff --check.
5. Do not edit tests except to fix a proven test bug; do not edit any other package, store, runtime, command, migration, go.mod, or plan.
6. Return exact RED evidence, files, GREEN results, and any gap. No commit.
