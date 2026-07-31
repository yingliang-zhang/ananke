Working...
Implemented production server P1 #3 only. No commit or push.

### Changes

- Added request v4 / envelope-reference v2 with a closed, canonical, self-hashed predecessor projection.
  - Contains every envelope binding.
  - Replaces raw `repository_identity` with `repository_identity_hash`.
  - Projection hash binds into the request payload hash, Unix channel binding, delivery channel, and delivery self-hash.
- Added operator-owned repository policy support:
  - New required `--repository-policy` production flag.
  - Generic canonical list mapping identity hash to raw repository identity.
  - Exact hash verification and unique mapping enforcement.
  - Owner, regular-file, mode `0600`, symlink, open-time replacement, and runtime path-replacement checks.
- Server now:
  - Resolves exactly one repository mapping.
  - Reconstructs the complete `ExternalSupervisorEnvelope`.
  - Seals it and requires exact durable envelope hash and exact regenerated projection.
  - Calls the same `verifyAuthorizationAt` path used by `Client`, including frozen predecessor pins and signed attestation/approval/MoA validation.
- Reconcile/cancel now require exact reconstructed envelope and durable receipt bindings:
  - Handoff ID, attempt, receipt identity, envelope hash.
  - Closed operation shapes.
  - Receipt-scoped cancellation identity.
  - Atomic reconcile/cancel exclusivity through an existing journal nonce uniqueness boundary.
  - Differently sealed handoff IDs for one receipt fail closed.
- Raw repository identity occurs only in `server_repository_policy.go`; it is absent from wire requests recursively.
- Final code does not modify journal URI/schema logic, `Server.Close`, admission, key buffers, docs, or OMP.

### Verification

Passed:

- Focused server/command tests, `-count=3`
- Focused security tests under `-race`
- Final focused tests, `-count=2`
- `go test ./... -count=1 -timeout 300s`
  - 6 packages passed; 3 packages had no tests
- `go vet ./...`
- `CGO_ENABLED=0 go build ./cmd/...`
- `gofmt -d` — clean
- Contract verifiers: P1a, P1c, P2a, P2c, P3a, P3d, P3f, P4
- Final P3f contract recheck passed
