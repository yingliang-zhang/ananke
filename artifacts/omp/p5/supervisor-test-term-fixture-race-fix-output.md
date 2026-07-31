Working...
Implemented test-only fix in `internal/trustedsupervisor/supervisor_test_termination_test.go`.

Changes:
- TERM handler exits immediately.
- Readiness is atomically published via staging file + rename after handler installation.
- Fixture uses a shell-builtin loop, eliminating the child `sleep` termination race.
- Timeout remains gated on observed readiness before `AfterStart` returns and releases execution.
- Assertion now requires:
  - `ErrDeadline`
  - exact process identity
  - completed/reaped process result
  - exit code `0`
  - no owned-process failure
  - exactly one signal: `SIGTERM`
  - leader and process group gone
- Separate ignored-TERM → `SIGKILL` test remains intact.

Verification passed:

```text
go test ./internal/trustedsupervisor \
  -run '^TestSupervisorAuditTestTimeoutConfirmsTERMExit$' \
  -count=100
```

```text
go test -race ./internal/trustedsupervisor \
  -run '^TestSupervisorAuditTestTimeoutConfirmsTERMExit$' \
  -count=30
```

```text
go test ./internal/trustedsupervisor \
  -run '^(TestSupervisorAuditTest|TestUnconfirmedAuditInvocationJoinIsBoundedReplayableAndRecoverable$|TestProductionServerSupervisorTestTerminationFailureRetainsResourcesUntilCloseRetry$)' \
  -count=10
```

No production changes. No commit created.
