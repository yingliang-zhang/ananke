Working...
Implemented P3f fake-only enforcement.

- Moved the entire P3f runtime from production source to `internal/lifecycle/p3f_fake_runtime_test.go`. `go list` now excludes every P3f source from production `GoFiles`.
- Removed configurable fake-child program and Seatbelt executable fields. Test-only execution is fixed to:
  - `/usr/bin/sandbox-exec`
  - `os.Args[0]` test binary
  - `TestP3FFakeChild`
  - fixed fake-child marker
- Added `TestP3FProductionBuildExcludesFakeExecution`, which mechanically:
  - inspects `go list -json` production vs test file selection;
  - rejects any production P3f Go source;
  - requires the fake runtime only in `TestGoFiles`;
  - parses the fake-runtime AST to require zero-field launcher/sandbox types, no string program parameter, and `exec.CommandContext` bound to `os.Args[0]`.
- Preserved P3f sandbox, archive, fence, FD-only, credential, and cleanup coverage.
- Added factual verification evidence to `docs/experiment-ledger.md`.

Strict TDD:
- RED observed: production test failed because `p3f_runtime.go` was compiled into lifecycle.
- GREEN: static exclusion regression passed after the move/refactor.

Verified:
- `go test -v ./internal/lifecycle -run '^TestP3F' -count=1 -timeout 120s` — PASS; Darwin Seatbelt read-only/write-denied proof exercised.
- `go build ./internal/lifecycle && go list -json ./internal/lifecycle` — PASS; P3f fake runtime appears only in `TestGoFiles`.
- `go test ./... -count=1 -timeout 300s` — PASS.
- `go test -race ./... -count=1 -timeout 360s` — PASS.
- `go vet ./...` — PASS.
- P3d and P3f normal/self-test contract gates — PASS.

No real OMP, wrapper, Ananke target, repository commit, or push was invoked.
