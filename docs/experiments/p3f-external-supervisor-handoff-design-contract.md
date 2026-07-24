# P3f external independently trusted supervisor / remote execution handoff design contract

**Status:** accepted design contract with a private Ananke-side durable identity runtime and an in-process test-only fake supervisor.
**Scope:** the runtime seals and validates identity-only envelopes, persists immutable envelope/outbox/receipt/callback/cancellation facts, and exercises only the fake supervisor. It opens no connection, creates no RPC/request, starts no supervisor or OMP, reads no source/artifact/evidence object, creates no child, and modifies no Run or execution authority.

## Decision

P3f's Darwin macOS 27 outcome remains `none_fail_closed`: no local path, `/dev/fd`, spawn, fileport, test-binary, or other local image-selection workaround may run a child. The successor is therefore not a local launch design. It freezes only the declarative handoff required for a future independently trusted supervisor to acquire authority outside Ananke.

Ananke has no remote execution authority merely by constructing a fixture or sealed envelope. Durable authority exists only after a future supervisor produces an authenticated, durable acceptance receipt bound to the exact sealed envelope and complete private fence. Until then, and whenever validation is incomplete, the only projection is `waiting_for_human`.

## Canonical chain and exact route mapping

```text
P3d canonical fixture
  → P3f activation fixture
    → P3f Darwin exec-by-FD design (`none_fail_closed`)
      → external-supervisor handoff design
        → external-supervisor denial vectors
```

The verifier reads canonical P3d bytes and its manifest, then authenticates every P3f fixture byte and hash. The handoff fixture directly binds P3d's fixture and HostSpec identities plus the P3f activation and Darwin-design fixture digests, source snapshot, source manifest, and wrapper identity.

One mapping exists:

| P3d wrapper kind | P3d route | Supervisor wrapper kind | Supervisor route | Protocol |
| --- | --- | --- | --- | --- |
| `ananke_omp_readonly_wrapper_v1` | `ananke_omp_read_only_audit_v1` | `ananke_remote_supervisor_omp_readonly_wrapper_v1` | `ananke_remote_supervisor_omp_readonly_audit_v1` | `ananke.remote-supervisor-handoff.v1` |

No bare OMP route, other wrapper, route fallback, provider selection, or local launcher is admitted.

## Sealed envelope and durable authority

`ananke.remote-supervisor-sealed-launch-envelope.v1` is canonical JCS with an `envelope_hash` over every field except itself. It binds:

- opaque handoff and idempotency identities;
- exact route-mapping hash;
- P3d's deadline, attempt cap, and initial bounded attempt;
- opaque complete-fence binding hash; the full fence must be authenticated privately and the hash is never a credential;
- P3d snapshot/P3f manifest/repository identity;
- supervisor artifact, build, attestation, release-approval, evidence-contract, and evidence-schema identities.

The runtime durably stores canonical immutable envelope bytes and hash, its immutable delivery outbox obligation, and receipt/callback/cancellation identity facts in their own transactions. A receipt is durable only after the configured verifier authenticates it under the current trust-root identity and binds it to the exact envelope, route, and full private fence. A caller-supplied digest, self-consistent artifact/digest pair, dynamic build, or test fixture cannot create authority.

The handoff transmits only sealed identity hashes, fixed enums, and attestation references. It never transmits secrets, raw paths, source/evidence bytes, prompt or prose authority, commands, argv, environment, or an endpoint capable of becoming executable authority.

## Independent supervisor release and root rotation

A future supervisor release requires a detached `ananke.remote-supervisor-release-attestation.v1`, independent release approval, and identities for artifact, build, attestation, and approval. Its release authority is explicitly distinct from Ananke, a builder, launch machinery, and the supervisor runtime.

The production core contains no release verifier, root key, signature implementation, target, or transport. Its only configured target/verifier is the in-process fake compiled into package tests; that fake is an identity-flow oracle, not a release, artifact, signature, or root implementation.

## Callback/result, cancellation, and recovery

The runtime accepts `ananke.remote-supervisor-callback.v1` only after an authenticated durable `ananke.remote-supervisor-acceptance-receipt.v1`. Its typed `ananke.remote-supervisor-result.v1` is identity hashes plus one terminal enum; it never changes a local Run state or exposes the result publicly. Before persistence, the callback verifier must bind the current trust root, envelope hash, receipt identity, handoff, attempt, and typed evidence identity.

No response, unverified receipt, unknown schema, failed verifier, stale root, stale full fence, expired deadline, attempt-cap mismatch, delivery error, reconciliation error, cancellation, or missing callback creates an outcome. Every public return is exactly:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

Cancellation first authenticates the complete private fence and durable receipt and persists only one handoff/attempt-bound cancellation identity. Recovery retries the immutable delivery obligation or asks the fake target for a callback only while the full private fence, deadline, and attempt remain current. It never guesses completion, cancellation, failure, evidence, or cleanup.

- Staging the same sealed envelope and idempotency-key hash returns the same durable handoff; any shared handoff, envelope, or idempotency identity with different bindings is a conflict.
- Delivery is receipt-idempotent: a durable receipt suppresses another fake delivery.
- Receipt, callback, and cancellation identity replays are idempotent only when every typed field agrees; a different binding is a conflict and projects `waiting_for_human`.
- Attempts equal the active P3b/P3c claim attempt and remain strictly within P3d's cap.

## MoA typed-role grant boundary

`ananke.moa-typed-role-grant-boundary.v1` names only role types: Ananke handoff authority, MoA route selector, MoA provider delegate, remote-supervisor runner, and remote evidence recorder. A role label is not authorization. Route selection and provider delegation require a future typed signed grant, exact route mapping, and the independently released supervisor binding. Runtime integration is absent and fallback is forbidden.

## Fixture oracle

- `external-supervisor-handoff-v1.canonical.json` freezes all identities, policy shapes, and no-authority state.
- `external-supervisor-red-flags-v1.canonical.json` covers local fallback, release/route/identity/fence/deadline/cap drift, callback/result failures, no response, cancellation/recovery inference, replay, root rotation, MoA grants, and prompt authority transmission.
- `contracts/p3f/verify.mjs` validates the complete P3d → P3f chain and rejects in-memory mutations of each policy class.

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

All commands inspect fixture bytes or mutate in-memory copies only.

## Explicit non-goals

No local exec, remote supervisor, network/RPC/request, OMP, release/signature/root implementation, source/artifact/evidence I/O, child, public lifecycle/API/UI integration, prompt/command/transcript/verification execution, commit, or push is authorized by this implementation.
