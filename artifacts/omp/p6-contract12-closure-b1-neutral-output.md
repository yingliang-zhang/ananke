Working...
Implemented P6 B1 GREEN. No commit.

### Changes

- `authorization.go`
  - `VerifyAuthorization` now validates the compiled release pins, trust bundle, and rotation at `now`.
  - Both authority and authorization-record paths validate:
    - exact `FullFenceSchemaVersion`
    - fence and claim-token hashes
    - bounded closed ClaimID
    - positive fence generation
    - bounded repository identity, route ID, supervisor profile ID, and peer role

- `dispatch.go`
  - Freshness-enforcing admission/effect validation now revalidates compiled release trust at the supplied `now`.
  - Existing authorization freshness revalidation remains intact.
  - Static replay remains clock-independent and non-authoritative for effects.

- `contract.go`
  - Added documented ASCII grammars and limits:
    - Claim/route/profile/role IDs: `^[a-z0-9]+(?:_[a-z0-9]+)*$`, 1–128 bytes
    - Repository identity: `^[a-z0-9]+(?:[.-][a-z0-9]+)*(?:/[a-z0-9]+(?:[-._][a-z0-9]+)*)+$`, maximum 256 bytes

- `acceptance_registry_test.go`
  - Registry expanded from 66 to 79 ordered vectors.
  - Added foreign fence schema, empty/invalid claim ID, zero/negative generation, and empty/invalid dynamic identifier cases.
  - Each new probe recomputes linked hashes and validates both matched authority and authorization-record rejection.

- `docs/experiments/p6-controlled-repair-supervisor-contract.md`
  - Documented release revalidation, identifier grammars, replay semantics, and the 79-vector inventory.

### Commands and results

Initial RED:

```text
$ go test ./internal/repaircontract
--- FAIL: TestP6A2ReleaseTrustMandatoryAtAuthorizationAndEffect
```

Failures reproduced at certificate `N`, `N+1ns`, retained-capability effect, and fresh 2029 authorization. Exit code 1.

Focused GREEN:

```text
$ go test ./internal/repaircontract -run '^TestP6A2RetainedAuthorizationEffectFreshness$' -count=1 -v
PASS
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	0.360s
```

```text
$ go test ./internal/repaircontract -run '^TestP6A2ReleaseTrustMandatoryAtAuthorizationAndEffect$' -count=1 -v
PASS
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	0.712s
```

```text
$ go test ./internal/repaircontract -run '^TestP6A2ExecutableAcceptanceVectorRegistry$' -count=1 -v
PASS
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	0.968s
```

All 79 registry vectors executed, including every new fence and identifier case.

Compatibility check:

```text
$ go test ./internal/repaircontract -run '^TestP6A2ExternalDynamicAuthorityAcceptsIndependentlyTrustedValues$' -count=1 -v
PASS
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	0.301s
```

Package verification:

```text
$ go test ./internal/repaircontract -count=1 -timeout 300s
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	0.786s
```

```text
$ go test ./internal/repaircontract -count=10 -timeout 300s
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	6.256s
```

```text
$ go test -race ./internal/repaircontract -count=3 -timeout 300s
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	16.560s
```

Static checks:

```text
$ go vet ./internal/repaircontract
```

No output; exit 0.

```text
$ gofmt -w internal/repaircontract/contract.go internal/repaircontract/authorization.go internal/repaircontract/dispatch.go internal/repaircontract/acceptance_registry_test.go
```

No output; exit 0.

```text
$ gofmt -d internal/repaircontract/contract.go internal/repaircontract/authorization.go internal/repaircontract/dispatch.go internal/repaircontract/acceptance_registry_test.go
```

No output; formatting clean.

```text
$ git diff --check -- internal/repaircontract docs/experiments/p6-controlled-repair-supervisor-contract.md
```

No output; exit 0.

Remaining failures: none. Rotation-approver DER files were not modified.
