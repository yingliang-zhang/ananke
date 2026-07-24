# P3f external independently trusted supervisor / remote execution handoff design contract

**Status:** frozen design-only successor contract and fixture oracle.

**Scope:** this documents a future independently trusted remote-supervisor handoff. It does not open a connection, construct a request, start a supervisor or OMP, read a source/artifact/evidence object, create a child, or modify runtime authority.

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

The local durable record permitted by this design is only the sealed-envelope hash plus the future authenticated receipt identity. The receipt must be durable before it is reported, signed under the current trusted supervisor root, and bound to the envelope, route, and full private fence. A caller-supplied digest, self-consistent artifact/digest pair, dynamic build, or test fixture cannot create authority.

The handoff transmits only sealed identity hashes, fixed enums, and attestation references. It never transmits secrets, raw paths, source/evidence bytes, prompt or prose authority, commands, argv, environment, or an endpoint capable of becoming executable authority.

## Independent supervisor release and root rotation

A future supervisor release requires a detached `ananke.remote-supervisor-release-attestation.v1`, independent release approval, and identities for artifact, build, attestation, and approval. Its release authority is explicitly distinct from Ananke, a builder, launch machinery, and the supervisor runtime.

The frozen trust policy has an active root, successor root, and trust-bundle hash. Rotation is cross-signed by active and successor roots with a validity overlap. Root downgrade is forbidden. Unknown, invalid, expired, revoked, or unproven roots must produce `waiting_for_human`; no stale root is silently retained and no successor is accepted merely because it is locally named.

These identities are fixture declarations. No artifact, signature, root, or rotation event exists or is verified in this slice.

## Callback/result, cancellation, and recovery

The future callback uses `ananke.remote-supervisor-callback.v1`; the typed result uses `ananke.remote-supervisor-result.v1`; evidence uses `ananke.remote-supervisor-evidence.v1`. A terminal result is authoritative only when a callback is authenticated by the current trust root, bound to the sealed envelope, and carries attested typed evidence identities. The result schema carries typed terminal state plus identities only, never raw transcript, source, prompt, path, or error material.

Ananke must not infer execution from elapsed time, a missing callback, a process/connection handle, an acknowledgement, or an absent receipt. An unknown schema, invalid signature, no response, unverified receipt, unavailable reconciliation backend, or any other incomplete state returns exactly:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

Cancellation first authenticates the complete private fence and durable receipt and binds only one handoff/attempt. Before an attested callback, its effect is unknown. Recovery reconciles only through an authenticated current root and durable receipt; it never guesses completion, cancellation, failure, evidence, or cleanup.

## Replay and idempotency

- Repeated submission for the same envelope hash and idempotency-key hash returns the same receipt only.
- Callback de-duplication keys on handoff, attempt, and callback identity hashes.
- Attempts increase strictly within P3d's cap.
- The same identity with a different envelope, route, evidence, artifact, or fence binding is a conflict and projects `waiting_for_human`.

No replay behavior is implemented or exercised here.

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

No local exec, remote supervisor, OMP, network, RPC/request, callback receiver, release/signature/root implementation, source/artifact/evidence I/O, child, lifecycle/store/UI/API change, prompt/command/transcript/verification execution, commit, or push is authorized by this contract.
