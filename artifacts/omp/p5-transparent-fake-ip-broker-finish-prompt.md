Finish the transparent fake-IP broker repair in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. The implementation and focused tests landed before timeout. No design expansion and no real provider call.

Verified current state:
- focused fake-IP/address/pinning/TLS tests PASS in 1.600s;
- `go vet ./internal/trustedsupervisor` passes;
- exact provider route, all-fake class, mixed-class rejection, reserved denials, exact dial pinning, Proxy:nil, original Host/SNI, valid-cert success, wrong-host cert rejection, redirect rejection, request semantics, and non-secret preflight are implemented and reviewed;
- `git diff --check` passes;
- only mechanical failure: `gofmt -d internal/trustedsupervisor/audit_connect_broker.go` emits 1109 bytes; all other touched files emit zero.

Required finish:
1. Run gofmt on `internal/trustedsupervisor/audit_connect_broker.go` only; do not reformat unrelated files.
2. Run focused fake-IP/TLS/broker tests count=10 and race count=3, full `trustedsupervisor` single, vet, gofmt-d on all touched P5 files, and diff-check.
3. Return actual commands/results. No commit, provider call, or cron job.
