Working...
Implemented the final P5 test-matrix blocker. Production code unchanged.

### Test changes

- `internal/trustedsupervisor/authentication_test.go`
  - Added a shared table covering:
    - raw authority
    - credential
    - secret
    - key
    - private key
    - key marker
    - command
    - argv
    - environment
    - source
    - artifact
    - evidence
    - path
    - URL
    - whitespace
  - Exercises every value in both:
    - `ReleaseApproval.ApprovalID`
    - `MoARoleGrant.GrantID`
  - Each forged authorization record is canonically self-hashed and Ed25519-signed.
  - Added explicit recomputation and signature verification assertions before testing rejection.

- `internal/trustedsupervisor/client_test.go`
  - Reuses one valid fixture, receipt, and denial socket.
  - For every category × identifier field, proves `Deliver`, `Reconcile`, and `Cancel` return `ErrAuthentication`.
  - Asserts cumulative dial-call count remains exactly zero.
  - Retained the recursive wire scanner as defense-in-depth.

### Verification

Passed:

```text
go test ./internal/store ./internal/trustedsupervisor \
  -run '^(TestExternalSupervisorAuthorizationIdentifiersUseFrozenP3FSafeOpaqueSemantics|TestEd25519AuthorizationRejectsSignedForbiddenOpaqueIdentifierValues|TestUnixClientNeverTransmitsSignedForbiddenAuthorizationIdentifierValues)$' \
  -count=3 -timeout 120s
```

```text
ok internal/store
ok internal/trustedsupervisor
```

Focused race passed:

```text
go test -race ./internal/store ./internal/trustedsupervisor \
  -run '^(TestExternalSupervisorAuthorizationIdentifiersUseFrozenP3FSafeOpaqueSemantics|TestEd25519AuthorizationRejectsSignedForbiddenOpaqueIdentifierValues|TestUnixClientNeverTransmitsSignedForbiddenAuthorizationIdentifierValues)$' \
  -count=1 -timeout 180s
```

Signed separate-process E2E passed:

```text
go test ./internal/trustedsupervisor \
  -run '^Test(ProductionCommandStoreLifecycleSignedPeerEndToEnd|SeparateProcessSignedSupervisorIsTestOnly)$' \
  -count=1 -timeout 180s
```

Recursive scanner test passed 3×. Full `go test ./... -run '^$'` compile and `go vet ./...` passed.

No production server or OMP started. No commit or push.
