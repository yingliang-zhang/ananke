Working...
Implemented the three focused termination/recovery fixes.

- `Server.Close` now:
  - keeps admissions fenced after shutdown starts;
  - returns the bounded `auditExecutor.Close` error without closing journal, signing material, policies, or directory anchors;
  - permits a later `Close` retry after workers join;
  - releases resources exactly once after successful retry;
  - preserves concurrent/idempotent close behavior.

- Server-context KILL-failure test now explicitly verifies:
  - `ErrDeadline` from bounded shutdown;
  - durable `waiting_for_human` with `kill_signal_failed`;
  - no `cancelled` or `timed_out` event;
  - journal, policies, and private signing key remain alive;
  - later shutdown retry joins and cleans resources.

- Wrong-PID restart test now expects the centralized `process_identity_mismatch` class and verifies:
  - zero signals to the unrelated process group;
  - no cleanup of unrelated work/output/session/prompt/tmp artifacts.

Verification:

- Exact three tests, `-count=10`: PASS
- Exact three tests under race detector, `-count=3`: PASS
- Termination/recovery suite: PASS
- Full `internal/trustedsupervisor` package: PASS
- `go vet ./internal/trustedsupervisor`: PASS

No real OMP/model invocation. No commit or push.
