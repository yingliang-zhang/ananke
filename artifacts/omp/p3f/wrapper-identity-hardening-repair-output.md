Working...
Implemented and committed P3f identity-core repair.

- Pinned canonical P3d source snapshot:
  `sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4`
- `validInput` now requires a valid lower-case SHA-256 before exact source-snapshot pin matching.
- Positive test uses an independent canonical P3d value. Malformed uppercase, bad-length, and different valid SHA-256 values fail closed.
- Expanded production-core AST guard rejects:
  - `os/exec`, `os.StartProcess`, `syscall.Exec`, `os.Args`, `os.Environ`
  - normalized struct/field names: `argv`, `env`, `path`, `program`, `command`, `executable`
- Preserved inert FD-only behavior; no production wrapper/OMP artifact executed.
- Recorded RED/GREEN evidence and boundary in `docs/experiment-ledger.md`.

Verification:
- RED focused test failed before repair on the malformed pin and denied canonical input.
- GREEN focused lifecycle tests passed.
- `go test ./... -count=1 -timeout 300s` — PASS.
- `go test -race ./... -count=1 -timeout 360s` — PASS.
- `go vet ./...` — PASS.
- Syntax, normal verification, and self-tests passed for P1a, P1c, P2a, P2c, P3a, P3d, and P3f contracts.

Commit: `7feb4a9 fix: pin P3f production wrapper identity`

Worktree: no staged or unstaged changes; pre-existing unrelated artifact directories remain untracked.
