Working...
Repaired P3e final-fence flake and diagnostics only.

- **Root cause:** test-only `PRAGMA busy_timeout = 0` configuration and `ReclaimLaunchClaim` used separate `*sql.DB` pool acquisitions. The test did not coordinate the configured SQLite connection with the actual reclaim.
- **Deterministic contention:** `TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims` now:
  - pins the second handle’s sole `*sql.Conn`;
  - configures its zero busy timeout;
  - proves the real reclaim is queued via `DBStats.WaitCount`;
  - releases the connection, requires `SQLITE_BUSY`/locked before fake start;
  - retries after admission rollback and verifies fence generation `+1` and active-fence equality;
  - releases/cancels gates on assertion failure to avoid stranded fake processes or sealed directories.
- **Opaque runtime preserved:** `p3eStartFailure` retains private test-visible `stage` and `cause`, while `Error()` remains exactly the sanitized denial and `errors.Is(err, errP3eDenied)` remains true.
  - Stages covered: descriptor validation, fence/boundary validation, SQLite admission, fake start.
  - Added explicit assertions for each; public state remains fail-closed.
- **Fake-only boundary retained:** construction remains test re-exec of `TestP3EFakeAdapterExecutable` over temporary synthetic roots and SQLite journals. No real OMP, target, commit, or push.

Updated:
- `internal/lifecycle/omp_adapter.go`
- `internal/lifecycle/omp_adapter_test.go`
- `docs/experiment-ledger.md`

Verification:
- `go test ./internal/lifecycle -run '^TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims$' -count=100 -timeout 300s` — PASS, 3.908s.
- `go test ./internal/lifecycle -run '^TestP3E' -count=20 -timeout 180s` — PASS, 12.708s.
- `go test ./... -count=1 -timeout 300s` — PASS.
- `go test -race ./... -count=1 -timeout 360s` — PASS.
- `go vet ./...` — PASS.
- Contract check, verification, and self-test — PASS for P1a, P1c, P2a, P2c, P3a, and P3d.
