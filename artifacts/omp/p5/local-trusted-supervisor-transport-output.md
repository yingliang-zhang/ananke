Working...
Implemented and verified the P5 local trusted-supervisor transport slice. No commit or push.

## Changed files

Production:

- `internal/trustedsupervisor/protocol.go`
- `internal/trustedsupervisor/canonical.go`
- `internal/trustedsupervisor/frame.go`
- `internal/trustedsupervisor/client.go`
- `internal/trustedsupervisor/peer_darwin.go`
- `internal/trustedsupervisor/peer_unsupported.go`
- `internal/lifecycle/external_supervisor_handoff.go`
- `cmd/ananke-trusted-supervisor-transport/main.go`

Tests:

- `internal/trustedsupervisor/client_test.go`
- `internal/lifecycle/external_supervisor_unix_transport_test.go`
- `cmd/ananke-trusted-supervisor-transport/main_test.go`

Operations/evidence:

- `docs/local-trusted-supervisor-transport-runbook.md`
- `docs/experiment-ledger.md`

## Delivered behavior

- Production Unix-domain-socket client injected through the existing external-supervisor transport/authenticator seam.
- Separate one-shot `ananke-trusted-supervisor-transport` binary.
- Four-byte big-endian framed RFC 8785 canonical JSON.
- Closed delivery, reconcile, and cancel schemas.
- 1,024–65,536-byte frame limits; hard 64 KiB maximum.
- Per-exchange deadlines capped at 10 seconds.
- Darwin socket ownership/mode validation and `LOCAL_PEERCRED` UID verification.
- Request-specific channel binding over peer credentials, nonce, canonical payload, and pinned release/trust identities.
- Bounded nonce, replay, and authenticated-record tables.
- Exact replay idempotency; conflicting receipt/callback/cancellation replay fails closed.
- Receipt/callback authentication hooks with a minimum local credential-plus-pin implementation.
- Noncanonical JSON, unknown fields, trailing bytes, oversized frames, timeout, peer drift, identity drift, and hook failure are rejected.

## Security boundaries

- Operator socket path remains only in command flags and `trustedsupervisor.Config`.
- Tests capture raw framed requests in a separate fake Unix server process and prove socket/path, credential, raw source, argv, and environment markers do not cross the protocol.
- No endpoint, path, command, argv, environment, credential, raw source, artifact contents, or raw evidence field exists in the wire schema.
- Existing store admission still performs full current-fence, deadline, attempt-cap, envelope, receipt, and callback checks.
- P3f/P4 fixtures and exact identities were not modified.
- Public lifecycle output remains exactly:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

`PinnedLocalAuthentication` does not claim detached-signature or revocation verification; deployments can inject stronger `AuthenticationHooks`.

## Gates

Strict TDD RED was observed: focused tests initially failed to compile because the transport package/types and command binary did not exist.

PASS:

```sh
go test ./internal/trustedsupervisor ./internal/lifecycle ./cmd/ananke-trusted-supervisor-transport \
  -run '^(TestUnixClient|TestP3FExternalSupervisor|TestP3FUnixTransportInjection|TestTrustedSupervisorTransportBinary)' \
  -count=1 -timeout=120s
```

Result: 3 packages passed.

PASS:

```sh
go vet ./internal/trustedsupervisor ./internal/lifecycle ./cmd/ananke-trusted-supervisor-transport
```

PASS: syntax, normal verification, and self-test for:

- `contracts/p3d/verify.mjs`
- `contracts/p3f/verify.mjs`
- `contracts/p4/verify.mjs`

P3f retained:

- Adapter fixture: `sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44`
- Exact 37-case denial fixture: `sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`

Per the resumed scope, verification was limited to focused tests, focused vet, and P3d/P3f/P4 contracts; full-suite and race runs were not rerun.

## Remaining state

Still intentionally absent:

- trusted-supervisor server
- real read-only project audit execution
- OMP execution
- child creation
- source/artifact/evidence access
- repair execution or authorization
- Run creation
- inferred success, failure, cancellation, or completion

The slice transports sealed identities toward a future local audit authority; it does not create that authority.
