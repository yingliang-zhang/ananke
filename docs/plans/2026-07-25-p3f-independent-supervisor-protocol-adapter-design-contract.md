# P3f independently trusted supervisor protocol-adapter / fixture TDD plan

**Goal:** freeze a future independently trusted supervisor protocol-adapter
wire boundary after P3f's external-handoff design. This protocol-adapter slice
does not produce a network client/server, mTLS connection, supervisor/OMP
process, or executable path.

**Authorized now:** two canonical P3f fixtures, two manifest entries, verifier
chain checks and in-memory self-tests, this plan, the design contract, and a
ledger entry. **Not authorized in this slice:** a protocol adapter implementation,
transport, listener, RPC, endpoint, TLS handshake, signature/key/root
implementation, supervisor/OMP process, child, source/artifact/evidence I/O,
persistence, commit, or push. This is not a repository-wide claim: predecessor
P3e and P3f runtime paths are out of this protocol-adapter slice.

## Task 1: pin the P3d → P3f → adapter chain

**Files:** `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-v1.canonical.json`, `contracts/p3f/verify.mjs`.

1. Authenticate the P3d manifest and canonical fixture, then derive its anchor before reading any P3f manifest or fixture; self-test both the read trace and the rejected-P3d no-P3f-read boundary.
2. Bind P3d fixture/HostSpec/source snapshot, P3f activation fixture,
   exec-by-FD design fixture, external-handoff design fixture, P3f source
   manifest/wrapper, predecessor sealed-envelope hash, and predecessor route-map
   hash.
3. Retain P3f Darwin `none_fail_closed`: the protocol adapter is a future
   identity boundary, never a local launcher workaround.

**Acceptance:** byte-pinned verifier rejects P3d/P3f/handoff chain drift.

## Task 2: freeze closed canonical wire records

1. Define JCS UTF-8 canonical record schemas and self-hashes for detached
   attestation, approval, typed MoA grant, sealed delivery, acceptance receipt,
   and completion callback.
2. Bind delivery to the predecessor envelope/idempotency/route/deadline/cap,
   current release material, trust bundle, nonce, timestamp, expiry, and channel
   binding; bind receipt and callback transitively and exactly.
3. Treat all values as non-authorizing wire-format vectors. No keys, signatures,
   releases, receipts, callbacks, or durable facts are implemented.

**Acceptance:** canonical self-hash mismatch and out-of-order/binding drift are
rejected in the in-memory oracle.

## Task 3: define temporal authorization, roots, mTLS, replay, and payload constraints

1. Freeze three independent root lifecycles: detached-release, release-approval,
   and typed-MoA-grant. Each names active and successor roots, binds the
   successor by a cross-signed rotation, gives an explicit revocation, and has
   a strict validity overlap.
2. Freeze the only verification times: delivery at
   `sealed_handoff_delivery.issued_at`, receipt at
   `acceptance_receipt.issued_at`, and callback at
   `completion_callback.issued_at`. At every boundary revalidate detached
   attestation, approval, MoA grant, and the corresponding root. Require
   `issued_at <= verification_time < not_after` for each authorization record.
3. Before successor activation accept only the named active root; at or after
   successor activation accept only the cross-signed bound successor. Reject a
   revoked root at `effective_at <= verification_time`, an absent/invalid
   overlap, successor-binding drift, and every predecessor downgrade—even
   during the overlap.
4. Define an interface-only TLS 1.3 mTLS/SPKI/channel-binding contract with no
   serializable URI, host, port, or endpoint authority.
5. Require nonce uniqueness and ordered bounded timestamps; reject replay,
   stale/future/expired messages, receipt-before-delivery, and
   callback-before-receipt.
6. Permit only hashes/enums/timestamps/nonce hashes/reference hashes and safe opaque identifiers matching `[a-z][a-z0-9_]{2,63}`. `release_approval.approval_id` and `moa_typed_role_grant.grant_id` reject URLs, whitespace, secret/key markers, and raw authority content. Reject secrets, including encrypted secrets, and all raw authority content.
7. Bind `remote_supervisor_runner` to a typed current-root MoA grant,
   route-map, attestation, and approval. Labels alone grant nothing.

**Acceptance:** denial vectors for approval expiry at delivery/receipt/callback,
root revocation before each boundary, expired or wrong-root MoA grants,
rotation-overlap/successor-binding drift, mTLS downgrade/identity drift,
endpoint authority, replay, secret/encrypted-secret content all project only
`waiting_for_human`.

## Task 4: gate and evidence

1. Add manifest and hard-digest entries for both new fixtures.
2. Extend the P3f normal verifier to authenticate the full chain and vectors.
3. Prove that P3d manifest/canonical-fixture authentication and anchor derivation are the first dependency, before every P3f manifest or fixture read.
4. Extend its self-test with in-memory mutations of chain, canonical wire hash,
   attestation/approval, rehashed endpoint/secret/key-marker payloads in both safe opaque IDs, mTLS/endpoint, root revocation, replay, and failure projection classes.
5. Freeze the authoritative current denial inventory at exactly **37** cases:
   these are the complete ordered protocol-adapter denial classes, so the exact
   count prevents omission or reclassification; it is not a repository-wide
   test inventory. `independent-supervisor-protocol-adapter-red-flags-v1.canonical.json` is
   `sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`;
   its companion canonical wire fixture is
   `sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44`.
   `fixtures.sha256` and the verifier hard digest must bind those exact bytes.
6. Record exact normal and self-test commands in the ledger.

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

For this protocol-adapter slice, the normal gate reads fixture bytes and the
self-test mutates copies only. Neither command exercises a network, mTLS, OMP,
supervisor, child, runtime adapter, source/artifact/evidence I/O, commit, or
push. Predecessor P3e/P3f runtime paths are explicitly out of slice; these
commands make no repository-wide implementation or execution claim.
