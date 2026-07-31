Working...
Implemented mechanical fixture migration in `internal/trustedsupervisor/audit_evidence_test.go`:

- Replaced six wrapper-script reads of `ANTHROPIC_API_KEY` with `SUDO_API_KEY`.
- Replaced three corresponding `t.Setenv` calls.
- Preserved secret strings and expected failure classes.
- Did not modify `audit_hard_review_test.go` or production code.
- No commit created.

Verification:

- Credential leak and failure/timeout scrub tests, `-count=10`: **passed**
- Same tests, `-race -count=3`: **passed**
- Native-alias, cleanup, timeout-cap, and sandbox focused tests: **passed**
- Full `internal/trustedsupervisor` package: **failed on two unrelated existing checks**
  - `TestExecutionPolicyLoadsCanonicalPinnedLaunchEntry`: expects `api.anthropic.com`, while resolved `custom:sudo` authority is `coding.sudoai.cc`.
  - `TestProductionServerTimeoutRetriesExactSessionThenCompletes`: cleanup assertion retains one entry in each of `output`, `prompt`, and `tmp`.

Both full-package failures reproduced individually.
