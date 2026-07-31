Working...
Implemented and verified. No commit, real provider call, or cron job. No P6 repaircontract files changed.

## RED

```sh
go test ./internal/trustedsupervisor \
  -run '^TestAuditHTTPGatewayAcceptsExactTransparentFakeIPRouteAndPinsDial$' \
  -count=1
```

Failed as expected:

```text
exact transparent fake-IP route rejected:
local trusted supervisor authentication failed:
non-public audit provider resolution
FAIL ... 0.760s
```

## GREEN / final verification

Formatted only the requested implementation file:

```sh
gofmt -w internal/trustedsupervisor/audit_connect_broker.go
```

Exit 0; no output.

Focused broker/fake-IP/TLS tests, ten repetitions:

```sh
go test ./internal/trustedsupervisor \
  -run '^(TestAuditProviderResolution.*|TestAuditHTTPGateway.*)$' \
  -count=10
```

```text
ok  github.com/yingliang-zhang/ananke/internal/trustedsupervisor  5.957s
```

Focused race tests, three repetitions:

```sh
go test -race ./internal/trustedsupervisor \
  -run '^(TestAuditProviderResolution.*|TestAuditHTTPGateway.*)$' \
  -count=3
```

```text
ok  github.com/yingliang-zhang/ananke/internal/trustedsupervisor  3.916s
```

Full package:

```sh
go test ./internal/trustedsupervisor -count=1
```

```text
ok  github.com/yingliang-zhang/ananke/internal/trustedsupervisor  53.796s
```

Vet:

```sh
go vet ./internal/trustedsupervisor
```

Exit 0; no output.

Touched-file formatting check:

```sh
gofmt -d \
  internal/trustedsupervisor/audit_connect_broker.go \
  internal/trustedsupervisor/audit_connect_broker_test.go \
  internal/trustedsupervisor/audit_wrapper_compatibility_test.go \
  internal/trustedsupervisor/audit_real_provider_canary_test.go
```

Exit 0; no output.

Diff check:

```sh
git diff --check
```

Exit 0; no output.

Build-tagged canary compile proof without invoking the real-provider test:

```sh
go test -tags ananke_real_provider_canary \
  ./internal/trustedsupervisor \
  -run '^TestRealProviderCanaryBudgetInvariant$' \
  -count=1
```

```text
ok  github.com/yingliang-zhang/ananke/internal/trustedsupervisor  0.490s
```

## Changed files

- `internal/trustedsupervisor/audit_connect_broker.go`
- `internal/trustedsupervisor/audit_connect_broker_test.go`
- `internal/trustedsupervisor/audit_wrapper_compatibility_test.go`
- `internal/trustedsupervisor/audit_real_provider_canary_test.go`

The public-address predicate remains strict; `198.18.0.0/15` is admitted only through the separate exact-route, Darwin-bound transparent fake-IP classification. No policy schema or policy-hash files changed.
