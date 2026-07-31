Implement a focused TDD credential-name compatibility fix in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. No commit/docs/ledger. Scope only `internal/trustedsupervisor/execution_policy.go`, relevant trustedsupervisor tests, and `audit_real_provider_canary_test.go`; do not edit store/repairrunner.

Observed real evidence: route-aware OMP succeeds when `SUDO_CODING_KEY` from ~/.zshrc remains set, and returns 401 when only `SUDO_API_KEY` is set or both are absent. Ananke currently allows only SUDO_API_KEY for custom:sudo.

Requirements:
- Add `SUDO_CODING_KEY` to the closed known credential names and leak/authority scanning.
- For custom:sudo, allow exactly one of two alternative exact credential declarations: `["SUDO_CODING_KEY"]` (preferred current contract) OR legacy `["SUDO_API_KEY"]`; reject empty, both together, mixed, duplicates, unknown names, or wrong provider.
- Do not read or persist values. Existing environment shaping passes only the exact declared name.
- Update production contract tests for both valid alternatives and all ambiguity denials; add evidence leak tests covering SUDO_CODING_KEY exactly like SUDO_API_KEY.
- Change the build-tagged real provider canary to require/use only `SUDO_CODING_KEY`, including policy declaration and secret leak checks. No mapping to SUDO_API_KEY in code.
- Keep generated broker models config static local credential behavior unchanged.
- Run focused count=10, race count=3, full trustedsupervisor, gofmt, vet, diff-check. No real provider call.
Return files and exact results.