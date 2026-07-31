Working...
Implemented. No commit.

Changes:
- `internal/trustedsupervisor/audit_evidence.go`
  - Validates reported and expected cwd paths are non-empty, absolute, and already `filepath.Clean`.
  - Canonicalizes both with `filepath.EvalSymlinks`; any failure rejects evidence.
  - Compares canonical physical paths, handling Darwin `/var` ↔ `/private/var`.
  - Applies the same fail-closed canonical validation to `session_path`, while retaining exact textual equality with `invocation.SessionDir`.
  - Preserves exact 8-line structure, UUID, deadline, marker, source, and recovery requirements.
  - Removed `AUDIT_TIMEOUT_PARSE` instrumentation and unused `fmt` import.
- `internal/trustedsupervisor/audit_runtime.go`
  - Removed all temporary `AUDIT_TIMEOUT_RESULT` and `AUDIT_TIMEOUT_CAPTURE` instrumentation.
- `internal/trustedsupervisor/audit_hard_review_test.go`
  - Uses existing temporary directories required by fail-closed canonicalization.
  - Covers physical cwd canonicalization and cwd/session traversal rejection.

Verification passed:

```text
go test ./internal/trustedsupervisor \
  -run '^(TestProductionServerTimeoutRetriesExactSessionThenCompletes|TestProductionServerTimeoutAttemptCapWaitsForHuman)$' \
  -count=10
```

```text
go test -race ./internal/trustedsupervisor \
  -run '^(TestProductionServerTimeoutRetriesExactSessionThenCompletes|TestProductionServerTimeoutAttemptCapWaitsForHuman)$' \
  -count=3
```

```text
go test ./internal/trustedsupervisor \
  -run '^TestAuditTimeoutEvidenceRequiresOneExactBoundedSuffix$' \
  -count=1
```

All passed. `AUDIT_TIMEOUT_` instrumentation search returned no matches. Only the three files above were edited.
