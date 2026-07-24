# P3f independently trusted supervisor protocol-adapter design contract

**Status:** frozen design contract and canonical fixture oracle.
**Scope:** two canonical wire-schema/vector fixtures, the dependency-free fixture
verifier, this document, its plan, and an experiment-ledger entry. The
protocol-adapter slice creates **no** protocol adapter implementation, network
client or server, mTLS connection, listener, endpoint configuration, OMP or
supervisor process, persistence, callback receiver, release verifier, key,
certificate, artifact, source/evidence operation, child, commit, or push.
This is not a repository-wide claim: predecessor P3e and P3f runtime paths are
outside this slice and are neither implemented nor assessed here.

## Decision

P3f remains `none_fail_closed` on Darwin before any child. The separately frozen
external-supervisor handoff establishes only a future authority boundary. This
successor freezes the **protocol-adapter wire boundary** which a future,
independently trusted supervisor must satisfy; it does not activate that
boundary or connect to it.

The sole predecessor chain is:

```text
P3d canonical read-only OMP fixture
  → P3f production-activation fixture
    → P3f Darwin exec-by-FD design (`none_fail_closed`)
      → P3f external-supervisor handoff design
        → P3f independently trusted supervisor protocol-adapter design
```

`independent-supervisor-protocol-adapter-v1.canonical.json` pins every
predecessor fixture digest, P3d HostSpec, source snapshot, P3f source manifest,
P3f wrapper hash, predecessor sealed-envelope hash, and predecessor route-map
hash. The verifier authenticates the P3d manifest and canonical fixture and
derives the anchor before reading any P3f manifest or fixture. A substitute
route, P3d fixture, P3f handoff fixture, source identity, wrapper identity,
envelope, or route map is rejected.

## Canonical wire format

All wire records are RFC 8785 JCS UTF-8 without a BOM. Each vector's named
self-hash is SHA-256 over its complete canonical object excluding only that
self-hash field. Hashes are required to have the `sha256:<64-lowercase-hex>`
form. The verifier proves canonical bytes and those self-hash derivations; it
does **not** implement or invoke a signature scheme.

The fixture freezes these closed record schemas:

| Record | Schema version | Binding / self-hash |
| --- | --- | --- |
| Detached release attestation | `ananke.independent-supervisor-release-attestation.v1` | `attestation_hash`; artifact, build, route-map, current release root, attestor identity, and validity interval. |
| Independent release approval | `ananke.independent-supervisor-release-approval.v1` | `approval_hash`; exact attestation and route-map, distinct approval root/identity, `approved`, and validity interval. |
| Typed MoA role grant | `ananke.moa-typed-role-grant.v1` | `grant_hash`; `remote_supervisor_runner`, exact route-map, attestation, approval, grant root/identity, and validity interval. |
| Sealed handoff delivery | `ananke.independent-supervisor-sealed-handoff-delivery.v1` | `delivery_hash`; predecessor envelope/idempotency identities, exact route-map, attestation/approval/grant hashes, trust bundle, attempt `1/3`, deadline, channel-binding hash, nonce hash, and expiry. |
| Acceptance receipt | `ananke.independent-supervisor-acceptance-receipt.v1` | `receipt_hash`; exact delivery/envelope/route/approval/attempt/channel binding, peer signing identity, current root, nonce hash, and timestamp. |
| Completion callback | `ananke.independent-supervisor-callback.v1` | `callback_hash`; exact delivery/envelope/receipt/route/attempt, peer identity, current root, separate callback channel binding/nonce, evidence hash, result schema, and typed terminal enum. |

`release_approval.approval_id` and `moa_typed_role_grant.grant_id` are safe opaque identifiers only: `[a-z][a-z0-9_]{2,63}` with no URL, whitespace, secret/key marker, or raw authority fragment. They are record identities, never endpoints, credentials, keys, or authority-bearing payloads.

The fixture values are **format and binding test vectors**, not releases,
certificates, signatures, keys, accepted receipts, or execution authority. A
future implementation must perform its real detached-signature, certificate,
revocation, and durable-state checks independently; fixture hashes and
self-consistent vectors never substitute for any of them.

### Sealed delivery, receipt, callback, and authorization time

The authoritative verification time is the `issued_at` timestamp of the record
being evaluated, never a later message's time, elapsed time, a process handle,
or the verifier's inference. A future implementation must authenticate each
record and apply all authorization checks at these exact points:

| Boundary | Verification time | Required revalidation |
| --- | --- | --- |
| Delivery | `sealed_handoff_delivery.issued_at` | detached attestation, release approval, typed MoA grant, and their release/approval/MoA roots. |
| Receipt | `acceptance_receipt.issued_at` | the same three authorization records and root states again, before a durable receipt can be accepted. |
| Callback | `completion_callback.issued_at` | the same three authorization records and root states again, before a callback can be terminal. |

For each record and each boundary, `record.issued_at <= verification_time <
record.not_after` is required. The delivery and receipt remain bound to and are
also checked at their own `issued_at` values; a later receipt or callback cannot
repair an earlier authorization failure. Receipt and callback still bind the
supervisor peer SPKI identity, exact delivery/envelope/route/attempt, and the
known prior durable record. A timestamp outside its allowed ordering, validity,
expiry, or P3d-deadline interval is a denial, not a retry or an inferred state.

The frozen replay key policy is:

- delivery: `delivery_id`, nonce hash, and predecessor envelope hash;
- receipt: `receipt_id`, nonce hash, and delivery hash;
- callback: `callback_id`, nonce hash, and receipt hash.

A nonce is public freshness data, not a credential. It is single-use within its
message type and issuance window even if every other field agrees. A receipt or
callback with a different binding is a conflict, never a new fact.

## Root lifecycle, release approval, and typed MoA

A release requires both a detached release attestation and a distinct,
independent approval. Both bind the same artifact/build and exact route mapping.
Neither Ananke, a builder, launch machinery, test code, nor the supervisor
runtime is that release authority.

The fixture carries independent release, approval, and typed-MoA root sets. Each
has a named active root, one cross-signed named successor, a self-hashed
revocation record, and a strict overlap: `successor_valid_from <
active_root_not_after`. At a boundary before `successor_valid_from`, only the
named active root is accepted. At or after that instant, only the bound
successor is accepted; the former root is a downgrade even inside the nominal
validity overlap. A root with `revocation.effective_at <= verification_time` is
always rejected. Unknown roots, a root not valid at the boundary, a missing or
mismatched successor binding, a non-overlap rotation, or any revoked root
therefore cannot be accepted.

The canonical vector is evaluated before any successor activates: the detached
attestation and receipt/callback use release root `v2`, the approval uses
approval root `v2`, and the typed `remote_supervisor_runner` grant uses MoA root
`v1`. It freezes successor roots and future revocations only as declarative
inputs. It neither creates nor verifies a key, signature, root, release,
approval, grant, receipt, callback, or durable authority.

`ananke.moa-typed-role-grant.v1` is necessary but insufficient. Its exact route,
detached attestation, release approval, typed role, validity interval, and
active-or-bound-successor nonrevoked MoA root must all revalidate at delivery,
receipt, and callback. `moa_route_selector` and `moa_provider_delegate` remain
ungranted here. Role labels are explicitly not authorization; runtime
integration and fallback are absent/forbidden.

## mTLS identity and endpoint boundary

`ananke.independent-supervisor-mtls-channel-binding.v1` defines an interface,
not a transport implementation:

- TLS 1.3 mutual authentication is required for a future connection.
- The sender and supervisor peer are identified only by pinned SPKI hashes and
  typed roles.
- A future TLS-exporter SHA-256 channel binding is required separately for
  delivery and callback messages.
- The interface serializes no URI, host, port, resolver value, endpoint, or
  endpoint-selection authority. `endpoint_serialization` is `forbidden`.
- Its state is `future_interface_only_no_connection_or_listener`.

The protocol adapter cannot accept an endpoint supplied by a renderer, config,
release object, route, or payload. TLS downgrade, absent channel binding, peer
identity drift, or endpoint authority is a denial vector, not a transport
fallback.

## Payload confidentiality is not secret authority

A future transport may provide mTLS confidentiality and a future application
layer may encrypt canonical bytes, but encryption must never create a secret
payload channel. Permitted wire content is only identity hashes, fixed enums,
timestamps, nonce hashes, and detached-reference hashes. It forbids credentials,
secrets, raw paths, prompt authority, commands, argv, environment, raw source,
raw evidence, and network endpoints — in plaintext **and** encrypted form.

Within the protocol-adapter wire schema, there is no field from which a process,
OMP session, target, command, source, evidence object, endpoint, or credential
can be constructed.

## No-inference projection

Any malformed/noncanonical record, self-hash drift, chain drift, replay, nonce
reuse, timestamp failure, delivery/receipt/callback ordering failure, channel
binding failure, peer identity drift, TLS downgrade, endpoint authority,
approval or MoA expiry at any verification boundary, an unknown/revoked/
downgraded root, invalid root overlap, successor-binding drift, attestation or
approval mismatch, untyped or mismatched MoA grant, unknown schema, or
timeout/missing response projects exactly:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

It exposes no error class, event, result, verification result, receipt,
execution outcome, or inferred cleanup/completion fact. A delivery/receipt/
callback test vector demonstrates schema binding only; it cannot establish
runtime authority.

## Fixture oracle and gate

- `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-v1.canonical.json`
  — closed schema inventory and release/approval/grant/delivery/receipt/callback
  wire vectors, exact delivery/receipt/callback authorization-time policy,
  independently active/successor/revoked release, approval, and MoA root sets,
  replay/timestamp policy, encrypted no-secret boundary, predecessor chain, and
  closed failure projection.
- `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-red-flags-v1.canonical.json`
  — exactly **37** current red flags: the closed, ordered protocol-adapter
  denial inventory. Its exact count prevents accidental omission or
  reclassification and is not a repository-wide test count. It includes endpoint
  and secret/key-marker payloads in both safe opaque `approval_id` and `grant_id`,
  canonical-encoding/hash, P3d/P3f/handoff chain, secret/encrypted-secret,
  endpoint, mTLS, nonce/timestamp, receipt/callback, approval expiry at every
  boundary, root revocation before delivery/receipt/callback, expired/wrong-root
  MoA, rotation overlap/successor binding, inference, and schema denials;
  canonical SHA-256 `sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`.
- `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-v1.canonical.json`
  — canonical wire fixture SHA-256
  `sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44`.
- `contracts/p3f/fixtures/fixtures.sha256` and verifier hard digests bind those
  exact bytes.
- `contracts/p3f/verify.mjs` authenticates the P3d anchor before any P3f read,
  then validates the P3d→P3f chain and mutates in-memory copies of each
  protocol policy class in self-test mode.

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

For this protocol-adapter slice, the normal gates read fixture bytes and the
self-test changes copies in memory. Neither command opens a network connection,
performs mTLS, starts an OMP or supervisor process, opens
source/artifact/evidence data, creates a child, or calls a protocol adapter.
These fixture-oracle commands do not make repository-wide claims about those
paths; predecessor P3e/P3f runtime paths remain out of slice.

## Explicit non-goals

- protocol-adapter, transport, mTLS, certificate, signature, key, root-store,
  revocation-store, release-verifier, endpoint-selection, client, server,
  listener, RPC, callback, or persistence implementation in this slice;
- OMP, model, supervisor, remote execution, process, child, local execution,
  source/artifact/evidence I/O, sandbox, cancellation/recovery action, or public
  lifecycle/API/UI integration in this slice;
- predecessor P3e and P3f runtime paths: they are out of slice, with no claim
  that they are absent, unimplemented, or unexercised elsewhere in the repository;
- commands, prompts, transcripts, renderer authority, verification execution,
  commits, and pushes in this slice.
