Working...
Implemented P3c fenced launch recovery orchestration.

- Added private `internal/lifecycle/fenced_launch.go`.
  - Exact launch-spec + claim admission with idempotent reconnect only for the same full claim identity.
  - Trusted materialization-ready and modeled Run-intent stages advance through P3b’s durable outbox.
  - Process admission is returned as an obligation; no process action occurs.
  - Full fence checks fail closed on stale authority.
  - Unknown/corrupt boundaries return `waiting_for_human` while preserving the original private store error.
  - Terminal/evidence intent presence is treated as unknown outcome, never inferred.

- Added strict-TDD lifecycle coverage in `internal/lifecycle/fenced_launch_test.go`.
  - P3a fixture hashes, P1 approval tuple, materialization identity, token, and Run ID.
  - Claim → materialization, materialization → Run, and Run → process recovery/reconnect boundaries.
  - Concurrent identical admission/readiness/Run-intent calls.
  - Reclaimed stale-fence denial.
  - Unknown, corrupt, and unexpected terminal-intent fail-closed paths.
  - Asserts no real rows are created in `runs`.

- Added P3c RED/GREEN/scope/verification evidence to `docs/experiment-ledger.md`.

Verification passed:

```sh
go test ./internal/lifecycle -run '^TestP3C' -count=1
go test -race ./internal/lifecycle -run '^TestP3C' -count=1
node --check contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs
```

The P3a spawning `--self-test` was intentionally not run to preserve the requested no-subprocess scope.
