# P3f external independently trusted supervisor handoff / fixture TDD plan

**Goal:** define the only future remote-execution handoff that may follow P3f's accepted Darwin `none_fail_closed` result, without adding local path execution or any working remote path.

**Authorized now:** two canonical P3f fixtures, their SHA-256 manifest entries, dependency-free verifier checks and in-memory mutation self-tests, this plan, the contract document, and a factual ledger entry. **Not authorized:** OMP, a supervisor, a remote service, network delivery, an RPC/request, a child, a local launcher, artifact/source/evidence I/O, runtime integration, commit, or push.

## Task 1: closed P3d → P3f → remote-handoff chain

**Files:** `contracts/p3f/fixtures/external-supervisor-handoff-v1.canonical.json`, `contracts/p3f/verify.mjs`.

1. Revalidate P3d's pinned canonical bytes and manifest before accepting any P3f successor.
2. Bind the activation fixture digest, exec-by-FD design fixture digest, P3d fixture and HostSpec digests, P3d source snapshot, P3f source-manifest hash, and P3f wrapper hash.
3. Bind only P3d `ananke_omp_readonly_wrapper_v1` plus `ananke_omp_read_only_audit_v1` to one remote-supervisor wrapper and route. No bare OMP, alternate wrapper, provider route, local exec, `/dev/fd`, pathname, spawn, or fallback is admitted.
4. Preserve Darwin's `none_fail_closed` decision: the remote handoff is a distinct future authority path, never a local-image-selector workaround.

**Acceptance:** a canonical verifier checks every predecessor identity from bytes; neither the binding nor its verifier creates authority to run locally or remotely.

## Task 2: sealed envelope and durable authority

1. Freeze `ananke.remote-supervisor-sealed-launch-envelope.v1`, self-hashed as RFC 8785 JCS bytes excluding only `envelope_hash`.
2. Include only an opaque handoff ID, hashed idempotency key, exact route-map hash, source/artifact/evidence identities, opaque full-fence binding, deadline, initial attempt, and P3d-bound attempt cap.
3. Define durable authority as an independently trusted supervisor's authenticated, durable acceptance receipt bound to that envelope. Persist only the envelope hash and receipt identity.
4. Require private full-fence authentication at delivery/cancellation/reconciliation boundaries. A fence hash is a binding, never a credential.

**Acceptance:** no envelope field carries a secret, raw filesystem location, prompt/prose authority, source/evidence bytes, command, argv, environment, or network endpoint.

## Task 3: independent supervisor release and trust roots

1. Freeze an independently released supervisor artifact SHA-256, build identity, detached-attestation hash, and independent release-approval hash.
2. Require a release authority distinct from Ananke, the builder, launch machinery, and supervisor runtime; reject caller artifacts, self-consistency, dynamic builds, and test fixtures.
3. Freeze an active root, successor root, trust-bundle hash, cross-signed validity-overlap rotation, no downgrade, and `waiting_for_human` for unknown or invalid roots.
4. Require the remote route-aware wrapper mapping and release artifact identity to match the sealed envelope exactly.

**Acceptance:** fixture labels and hashes are declarations only; no release, trust root, key, signature, or artifact is created or verified at runtime.

## Task 4: callbacks, cancellation, recovery, replay, and MoA roles

1. Freeze callback and result schema versions. Only a callback authenticated by the current trust root, bound to the envelope, and accompanied by attested typed evidence may establish a terminal result.
2. No callback, invalid callback, unverified receipt, error, or missing reconciliation response projects only the closed `waiting_for_human` shape. Ananke never derives completion from timing, a handle, or absence of data.
3. Bind cancellation to the full private fence, durable receipt, handoff, and attempt; before an attested callback the outcome is unknown. Recovery admits only current-root-and-receipt reconciliation and never guesses an outcome.
4. Require idempotent submission by envelope/idempotency hash, callback de-duplication by handoff/attempt/callback identity hash, strict bounded attempt progression, and `waiting_for_human` for binding conflicts.
5. Freeze `ananke.moa-typed-role-grant-boundary.v1`: role labels confer nothing; route selection/provider delegation require a typed signed grant plus exact route/release bindings. Runtime integration and fallback remain absent/forbidden.

**Acceptance:** all contract-only failures use exactly:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

## Task 5: denial oracle and contract gate

**Files:** `contracts/p3f/fixtures/external-supervisor-red-flags-v1.canonical.json`, `contracts/p3f/verify.mjs`.

1. Add red flags for local path/FD fallbacks, untrusted release, route/source/artifact/evidence drift, fence/deadline/cap drift, unsigned or unknown callbacks/results, outcome inference, no response, cancellation/recovery inference, replay conflict, trust-root rotation/downgrade, ungranted MoA roles/fallback, and prompt authority transmission.
2. Make each vector emit only the closed failure projection, with no reason, event, result, verification, child, or execution fact.
3. Self-test in memory must mutate each authority and policy class and reject it.

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

The normal gate reads fixtures only. The self-test mutates copies in memory only. Neither invokes an OMP, supervisor, network transport, request, child, sandbox, source/artifact/evidence descriptor, command, commit, or push.

## Explicit non-goals

- any supervisor/remote-service implementation, release verification implementation, trust-root/key storage, callback receiver, RPC/request, network, OMP, model, child, local path exec, FD/sandbox action, source/artifact/evidence materialization, or persistence/runtime integration;
- any daemon, Tauri/UI, renderer, public API, store/lifecycle change, prompt, command, transcript, verification execution, commit, or push.
