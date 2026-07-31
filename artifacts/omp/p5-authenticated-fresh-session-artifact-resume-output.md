Working...
Implemented the provider-free authenticated fresh-session exception. No real-provider canary, ledger edit, Hermes wrapper edit, commit, push, reset, clean, or restore was performed.

## Behavior

- Allows at most one fresh OMP session JSONL.
- Requires:
  - regular, non-symlink `.jsonl`;
  - direct child of the exact invocation `SessionDir`;
  - fresh invocation only—timeout and resume paths remain separate;
  - valid fresh UUID;
  - exact physical snapshot `WorkDir` as CWD;
  - exact current prompt;
  - authenticated session root;
  - only exact invocation-owned ephemeral path values.
- Rejects:
  - malformed/non-JSONL, nested, symlink, and special artifacts;
  - wrong UUID, CWD, or prompt;
  - credentials;
  - repository, wrapper, protected, stale, foreign, or otherwise unknown absolute paths;
  - multiple authenticated sessions;
  - file-count and byte-limit overflow.
- Fake OMP now supports authenticated and leaking fresh-session modes.
- Authenticated session plus exit 9 returns `runErr == nil`, `ExitCode == 9`, and reaches `direct_omp_exit_nonzero`.
- Leaking session returns `ErrAuthentication` and classifies as `direct_omp_or_capture_verification_failed`.

## Files changed

- `internal/trustedsupervisor/audit_executor.go`
- `internal/trustedsupervisor/audit_evidence.go`
- `internal/trustedsupervisor/audit_executor_test.go`
- `internal/trustedsupervisor/audit_evidence_test.go`
- `internal/trustedsupervisor/testdata/fakeomp/main.go`

No sandbox profile, process authority, CLT Git v7 policy, or descriptor-contract changes.

## Strict RED

```sh
go test ./internal/trustedsupervisor -run '^TestAuditFreshSessionArtifactAllowsAuthenticatedInvocationOwnedPaths$' -count=1
```

Result before production implementation:

```text
--- FAIL: TestAuditFreshSessionArtifactAllowsAuthenticatedInvocationOwnedPaths (0.29s)
    audit_executor_test.go:522: authenticated fresh session artifact rejected: local trusted supervisor authentication failed: audit invocation artifact authority leakage
FAIL
FAIL	github.com/yingliang-zhang/ananke/internal/trustedsupervisor	1.295s
FAIL
```

Exit code: `1`.

## Scanner GREEN

```sh
gofmt -w internal/trustedsupervisor/audit_executor.go internal/trustedsupervisor/audit_evidence.go internal/trustedsupervisor/audit_executor_test.go && go test ./internal/trustedsupervisor -run '^TestAuditFreshSessionArtifact(AllowsAuthenticatedInvocationOwnedPaths|DenialVectorsFailClosed)$' -count=1
```

```text
ok  	github.com/yingliang-zhang/ananke/internal/trustedsupervisor	3.565s
```

## Runtime smoke proof

```sh
gofmt -w internal/trustedsupervisor/testdata/fakeomp/main.go internal/trustedsupervisor/audit_executor_test.go internal/trustedsupervisor/audit_evidence_test.go && go test ./internal/trustedsupervisor -run '^(TestAuditFreshSessionNonzeroExitSeparatesExitFromCaptureFailure|TestAuditFailureAndTimeoutScrubCredentialBearingTrees)$' -count=1
```

```text
ok  	github.com/yingliang-zhang/ananke/internal/trustedsupervisor	6.162s
```

## Requested verification matrix

Focused scanner/session/runtime tests:

```sh
go test ./internal/trustedsupervisor -run '^TestAudit(FreshSessionArtifactAllowsAuthenticatedInvocationOwnedPaths|FreshSessionArtifactDenialVectorsFailClosed|FreshSessionNonzeroExitSeparatesExitFromCaptureFailure|FailureAndTimeoutScrubCredentialBearingTrees|SupervisorTimeoutObservationBindsProcessSessionAndExactResume|ResumeKeepsImmutableSessionRootAndTrustedPromptState)$' -count=1
```

```text
ok  	github.com/yingliang-zhang/ananke/internal/trustedsupervisor	8.678s
```

Repeated focused tests:

```sh
go test ./internal/trustedsupervisor -run '^TestAudit(FreshSessionArtifactAllowsAuthenticatedInvocationOwnedPaths|FreshSessionArtifactDenialVectorsFailClosed|FreshSessionNonzeroExitSeparatesExitFromCaptureFailure|FailureAndTimeoutScrubCredentialBearingTrees|SupervisorTimeoutObservationBindsProcessSessionAndExactResume|ResumeKeepsImmutableSessionRootAndTrustedPromptState)$' -count=10
```

```text
ok  	github.com/yingliang-zhang/ananke/internal/trustedsupervisor	90.491s
```

Focused race tests:

```sh
go test -race ./internal/trustedsupervisor -run '^TestAudit(FreshSessionArtifactAllowsAuthenticatedInvocationOwnedPaths|FreshSessionArtifactDenialVectorsFailClosed|FreshSessionNonzeroExitSeparatesExitFromCaptureFailure|FailureAndTimeoutScrubCredentialBearingTrees|SupervisorTimeoutObservationBindsProcessSessionAndExactResume|ResumeKeepsImmutableSessionRootAndTrustedPromptState)$' -count=3
```

```text
ok  	github.com/yingliang-zhang/ananke/internal/trustedsupervisor	59.785s
```

Full package:

```sh
go test ./internal/trustedsupervisor
```

```text
ok  	github.com/yingliang-zhang/ananke/internal/trustedsupervisor	66.020s
```

Vet:

```sh
go vet ./internal/trustedsupervisor
```

Result: exit `0`, no output.

Scoped formatting diff:

```sh
gofmt -d internal/trustedsupervisor/audit_executor.go internal/trustedsupervisor/audit_evidence.go internal/trustedsupervisor/audit_executor_test.go internal/trustedsupervisor/audit_evidence_test.go internal/trustedsupervisor/testdata/fakeomp/main.go
```

Result: exit `0`, no output.

Whitespace check:

```sh
git diff --check
```

Result: exit `0`, no output.

## Residual risks

- The real provider canary was intentionally not run. Exact compatibility with additional live OMP JSONL record fields remains unverified.
- Unknown absolute paths are deliberately fail-closed. A future OMP release adding new absolute runtime metadata may require another narrowly authenticated allowance.
- Existing `parseAuditSessionHeader` bounds remain unchanged: first 50 records and a 1 MiB scanner token limit. The new validator still validates every JSONL record within the existing total tree-byte limit.
