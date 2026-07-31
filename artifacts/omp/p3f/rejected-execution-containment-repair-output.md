Working...
Implemented P3f runtime containment repair.

- Deleted production launcher: `internal/lifecycle/omp_production_execution.go`.
- Moved fake executor, sandbox, descriptor duplication, artifact staging, and fake child execution into `internal/lifecycle/omp_production_fake_execution_test.go`.
  - Fake execution re-execs only `os.Args[0]` as `TestOMPProductionFakeWrapperChild`.
  - Fixed testdata artifact is checked against a frozen SHA-256.
  - Removed dynamic `go build`, caller artifact input, caller approval digest, and self-consistent artifact acceptance.
  - Removed the FD-to-path wrapper launch path; sandbox receives only source/manifest/evidence FDs.
- Production preparation now accepts only the exact frozen P3f wrapper identity, not any structurally valid SHA-256 identity.
- Added compiler `GoFiles` containment guard: production compilation must exclude both P3f fake runtimes and the removed execution source.
- Fixed owned-wrapper cleanup: it now closes wrapper and parent descriptors even when inode/path replacement prevents safe deletion; foreign replacement is preserved.
- Preserved fake sandbox, fence, FD-only, identity drift, artifact drift, and fail-closed output coverage.
- Documented that production execution remains blocked pending separately approved exec-by-FD plus independently trusted artifact identity:
  - `docs/experiments/p3f-production-wrapper-identity-activation-core.md`
  - `docs/experiments/p3f-production-self-hosted-omp-activation-contract.md`

Strict TDD evidence: new containment and replacement-cleanup tests failed before implementation because production still compiled `omp_production_execution.go` and replacement cleanup left descriptors open.

Verification passed:

```sh
go test ./internal/lifecycle -count=1
go build ./...
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

No real OMP, target, or production commit path was introduced or invoked.
