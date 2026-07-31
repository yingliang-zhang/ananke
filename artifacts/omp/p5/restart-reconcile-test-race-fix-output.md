Working...
Updated `internal/trustedsupervisor/audit_server_test.go` only.

- Accepts exactly:
  1. Initial `nil` pending, then bounded polling to callback.
  2. Immediate authenticated callback.
- Requires callback state `waiting_for_human`.
- Requires `ananke.independent-supervisor-audit-not-run.v1`.
- Verifies evidence hash binding to the journaled audit state and failure class.
- Verifies callback authentication bindings and non-empty signature.
- Rejects `completed`, success, or any other terminal state.
- Preserves crash, restart, running-state recovery, and no-guessed-success assertions.

Verification passed:

```text
go test ./internal/trustedsupervisor \
  -run '^TestProductionServerCrashRestartReconcilesRunningWithoutGuessingSuccess$' \
  -count=50
```

```text
go test -race ./internal/trustedsupervisor \
  -run '^TestProductionServerCrashRestartReconcilesRunningWithoutGuessingSuccess$' \
  -count=20
```

```text
go test -race ./internal/trustedsupervisor \
  -run '(Restart|Term)' \
  -count=1
```

No production changes. No commit created.
