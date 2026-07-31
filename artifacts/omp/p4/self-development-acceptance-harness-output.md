Working...
Implemented P4 self-development acceptance harness. No commit or push.

### Added
- `internal/lifecycle/p4_self_development_acceptance_test.go`
  - Deterministic end-to-end P1→P4 integration test.
  - Loads frozen P1 proposal/revision/approval identity.
  - Runs deterministic P2 Grill clear evaluation and exact zero-write replay.
  - Stages P3 fenced external handoff through the existing test-only in-process fake supervisor; persists receipt and typed callback while public output remains `waiting_for_human`.
  - Verifies P3f 37-case and P4 38-case fixture-oracle bindings/digests.
  - Submits P4 through a new test-only verifier, persists exactly one durable fact set, and proves replay makes zero additional verifier calls/facts.
  - Proves cap overflow, stale/fresh-fence failure, and typed-MoA role drift fail before reaching the verifier and project only to `waiting_for_human`.
  - Asserts zero local `runs`, repair-run tables, and OMP session tables.
  - Audits the exercised P3/P4 runtime and fake sources for forbidden `net`, `os`, `syscall`, and `unix` imports.

- `internal/lifecycle/p4_self_development_acceptance_fake_test.go`
  - Test-only, in-process P4 verifier fake.
  - Accepts only the exact immutable P4 verifier request and returns the frozen design-only output/replay.
  - No transport, process, OMP, repair, source, or artifact capability.

- `contracts/p4/self-development-acceptance-v1.canonical.json`
  - Canonical machine-readable acceptance report/runbook.
  - Records immutable P1–P3f chain, P3f/P4 oracle counts, receipt/callback evidence identities, P4 cap/fresh-fence/typed-MoA boundaries, observed zero-execution effects, and required operator prerequisites before connecting any real trusted external supervisor.

### Strict TDD evidence
- RED: `go test ./internal/lifecycle -run '^TestP4SelfDevelopmentAcceptance$' -count=1` initially failed on the intentionally undefined acceptance-harness symbols.
- GREEN: the same targeted test now passes and byte-compares its deterministic runtime report to the canonical runbook artifact.

### Verification
Passed:
```sh
node --check contracts/p4/verify.mjs
node contracts/p4/verify.mjs
node contracts/p4/verify.mjs --self-test
go test ./internal/store -run '^TestP4' -count=1
go test ./internal/lifecycle -count=1
go vet ./internal/lifecycle
go test ./internal/lifecycle -run '^TestP4SelfDevelopmentAcceptance$' -count=1
```

The P4 self-test independently authenticated the P3f 37-case oracle before P4 reads and exercised all 38 P4 denial mutators. Go LSP diagnostics were unavailable because no Go language server is configured; compile, package tests, targeted acceptance test, and `go vet` passed.
