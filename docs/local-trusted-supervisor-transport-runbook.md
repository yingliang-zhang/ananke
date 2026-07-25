# Local trusted-supervisor transport runbook

## Scope and current state

`ananke-trusted-supervisor-transport` is the one-shot local Unix-domain-socket client for the existing P3f external-supervisor handoff runtime. It can submit an already sealed handoff, reconcile an already durable receipt, or transport an already admitted cancellation. `ananke-trusted-supervisor` is the production signed Unix server for that protocol.

The server authenticates the predecessor projection, repository policy, authorization chain, local client UID, replay state, and request bindings before signing protocol receipts, callbacks, or cancellation acknowledgements. Reconciliation remains an explicit `audit_not_run` / `waiting_for_human` callback; the lifecycle projection remains exactly:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

Neither binary implements OMP, an executor, a project audit, a child process, source/artifact/evidence access, repair execution, or Run creation. The P3f/P4 canonical fixtures and identities are unchanged. The runtime still requires the complete current private fence and frozen P3f envelope policy before any delivery, cancellation, or reconciliation reaches the transport.

## Build

```sh
go build -o ./bin/ananke-trusted-supervisor ./cmd/ananke-trusted-supervisor
go build -o ./bin/ananke-trusted-supervisor-transport ./cmd/ananke-trusted-supervisor-transport
```

The server and one-shot client are production composition roots. Deployment requires operator-owned trust, private-key, repository-policy, journal, store, and socket paths; the repository does not supply deployment credentials or private signing material.

## Socket requirements

The `--socket` value is operator-only configuration. It is never copied into the sealed envelope, framed request, receipt, callback, evidence, or lifecycle output.

Before each exchange the client requires:

- an absolute Unix socket path;
- a socket owned by `--peer-uid`;
- socket permissions with no group or other bits (normally `0600`);
- Darwin `LOCAL_PEERCRED` reporting the same peer UID;
- a per-exchange deadline no longer than 10 seconds;
- a frame limit from 1,024 through 65,536 bytes.

Use a private directory owned by the supervisor account. Do not place the socket in a group/world-writable directory.

## Required trust inputs

Every invocation requires these operator-provided inputs in addition to the store and socket paths:

- `--trust-bundle`: path to a bounded, canonical public JSON trust bundle;
- `--peer-uid`: expected Unix peer user ID;
- `--peer-pid`: expected Unix peer process ID.

The trust bundle contains only public material: active and successor Ed25519 roots for release, approval, and MoA authority; signed root rotations and revocations; signed leaf certificates for the release attestor, release approver, MoA grantor, and supervisor peer; and the signed release-attestation/approval/MoA authorization chain. It contains no private key.

The client always performs the mandatory verification. It validates bundle and record self-hashes, public-key/SPKI bindings, overlapping active/successor root lifetimes, cross-signed rotations, successor-signed revocations, root selection and revocation at each authoritative timestamp, root-signed leaf certificates, detached Ed25519 signatures on the authorization records, and Ed25519 peer-possession signatures over the exact request/channel/message bindings. Optional `AuthenticationHooks` run only after mandatory verification and cannot replace it.

Delivery additionally requires the artifact, build, route, attestation, approval, and MoA bindings to match the sealed envelope and signed authorization chain exactly. Durable receipts, callbacks, and cancellation acknowledgements are reverified against their own authoritative timestamps and the current bundle lifecycle before persistence. The response must retain the exact peer signing identity, selected release root, request hash, nonce, and Unix channel-binding hash.

The production server requires:

- `--socket`: absolute operator-owned Unix socket path;
- `--repository-policy`: owner-only canonical repository identity policy;
- `--trust-bundle`: owner-only canonical public trust bundle;
- `--private-key-bundle`: owner-only canonical private signing-key bundle path; private key bytes never enter argv;
- `--journal`: absolute durable server SQLite journal path;
- `--expected-client-uid`: expected local client Unix user ID.

The server checks owner-only files, pinned directory/file identity, socket mode and ownership, Darwin peer credentials, canonical bounded frames, authorization and predecessor bindings, and durable replay/conflict state. `--timeout` and `--max-frame-bytes` are optional and default to `2s` and `65536`.

## Invocation

### Server

```sh
ananke-trusted-supervisor \
  --socket /private/operator/trusted-supervisor.sock \
  --repository-policy /private/operator/trusted-supervisor-repositories.json \
  --trust-bundle /private/operator/trusted-supervisor-trust-bundle.json \
  --private-key-bundle /private/operator/trusted-supervisor-private-key.json \
  --journal /private/operator/trusted-supervisor-journal.sqlite \
  --expected-client-uid 501 \
  --timeout 2s \
  --max-frame-bytes 65536
```

The server owns the socket, journal, pinned directory handles, and in-memory signing key until shutdown. Shutdown stops admission, closes accepted connections, waits for workers, removes only its own socket inode, then closes and zeroizes resources. A replacement socket is never removed.

### Client

The transport binary reads one bounded JSON object from standard input. Unknown or trailing fields are rejected.

Submit an envelope that was already sealed and admitted under the full current fence:

```json
{"operation":"submit","envelope":{...},"fence":{...}}
```

Reconcile a durable handoff:

```json
{"operation":"recover","handoff_id":"remote_handoff_p3f_001"}
```

Transport an already receipt-bound cancellation under the full current fence:

```json
{"operation":"cancel","cancellation":{...},"fence":{...}}
```

Example command shape:

```sh
ananke-trusted-supervisor-transport \
  --store /private/operator/ananke.sqlite \
  --socket /private/operator/trusted-supervisor.sock \
  --trust-bundle /private/operator/trusted-supervisor-trust-bundle.json \
  --peer-uid 501 \
  --peer-pid 4242 \
  --timeout 2s \
  --max-frame-bytes 65536 \
  < invocation.json
```

`--timeout` and `--max-frame-bytes` are optional; their defaults are `2s` and `65536`. The store, socket, and trust-bundle paths and the expected UID/PID remain local process configuration. Do not put endpoints, credentials, raw source, artifact bytes, evidence bytes, commands, argv, or environment data in an invocation or supervisor response.

## Wire and failure behavior

Each operation uses one Unix connection and one request/response exchange:

1. Validate the closed store record and release pins.
2. Generate or reuse a bounded per-operation nonce hash.
3. Verify socket-file ownership/mode and Darwin `LOCAL_PEERCRED` user, primary-group, and process credentials against `--peer-uid` and `--peer-pid`.
4. Derive a channel-binding hash over peer credentials, peer pins, nonce hash, and canonical request payload hash.
5. Send RFC 8785 canonical JSON behind a four-byte big-endian length prefix.
6. Require one bounded canonical response, exact request/channel/peer bindings, a valid root-authorized Ed25519 peer signature, then EOF.
7. Revalidate the signed authorization chain, root lifecycle, and replay state for the receipt, callback, or cancellation acknowledgement before returning it to the store boundary.

Exact replays are idempotent. A conflicting receipt, callback, or cancellation acknowledgement fails closed. Noncanonical JSON, unknown fields, extra bytes, oversized/empty frames, deadline expiry, credential drift, release/trust drift, binding drift, and authentication-hook failure all return errors to the lifecycle seam; the public projection still remains `waiting_for_human` with no inferred outcome.

## Operational gate

```sh
go test ./internal/trustedsupervisor ./internal/lifecycle ./cmd/ananke-trusted-supervisor ./cmd/ananke-trusted-supervisor-transport \
  -run '^(TestProductionServer|TestUnixClient|TestP3FExternalSupervisor|TestP3FUnixTransportInjection|TestTrustedSupervisorTransportBinary)' \
  -count=1 -timeout=120s
go vet ./internal/trustedsupervisor ./internal/lifecycle ./cmd/ananke-trusted-supervisor ./cmd/ananke-trusted-supervisor-transport
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
node contracts/p4/verify.mjs
node contracts/p4/verify.mjs --self-test
```

The process E2E coverage builds and launches the production `cmd/ananke-trusted-supervisor` command for submit, crash/restart, reconcile, shutdown, socket cleanup, journal secrecy, and binary-secrecy checks. Separate test-only signed peers remain adversarial client fixtures; they are not installation templates. Production still stops at the signed identity protocol boundary: there is no OMP, executor, audit runner, source/artifact/evidence reader, repair path, or Run creator.
