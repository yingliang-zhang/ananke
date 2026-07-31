Working...
Implemented the remaining P5 command integration compile fix.

### Exact change

Modified only `cmd/ananke-trusted-supervisor-transport/main.go`:

- Added a private `commandRuntime` interface covering `Submit`, `Recover`, and `Cancel`.
- Extracted existing operation validation and dispatch into:
  ```go
  executeInvocation(ctx context.Context, runtime commandRuntime, request invocation)
  ```
- Updated `run` to call `executeInvocation`.
- Preserved all fail-closed payload validation.
- Did not modify or weaken `main_test.go` assertions.
- No changes to internal packages, contracts, or architecture.

### Gates

All passed:

```text
go test ./cmd/ananke-trusted-supervisor-transport
go test ./cmd/ananke-trusted-supervisor-transport ./internal/store ./internal/lifecycle ./internal/trustedsupervisor
go vet ./cmd/ananke-trusted-supervisor-transport
go test ./cmd/ananke-trusted-supervisor-transport \
  -run TestTrustedSupervisorCommandDispatchesSubmitRecoverAndCancelExactly \
  -count=1
```

No commit or push performed.
