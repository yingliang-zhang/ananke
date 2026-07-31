Working...
Completed the P3f independently trusted supervisor protocol-adapter **design contract only**.

**Artifacts**
- `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-v1.canonical.json`
  - Canonical sealed-delivery, receipt, callback, detached-attestation, approval, typed-MoA-grant, mTLS channel-binding, replay/timestamp, trust-root rotation/revocation vectors.
- `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-red-flags-v1.canonical.json`
  - Exactly **37** closed `waiting_for_human` denials: canonical/hash drift, P3d/P3f chain drift, encrypted/plaintext secrets, unsafe endpoint/secret/key-marker approval and grant identifiers, mTLS downgrade/identity drift, replay/nonces/timestamps, receipt/callback ordering, revocation/downgrade, release approval, MoA, and inference.
- `contracts/p3f/fixtures/fixtures.sha256`
  - Pinned current digests:
    - canonical wire fixture: `956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44`
    - exact 37-case red-flag fixture: `6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`
- `contracts/p3f/verify.mjs`
  - Authenticates P3d → activation → Darwin exec-by-FD → external-handoff → protocol-adapter chain.
  - Verifies closed field sets, canonical self-hashes, strict safe opaque `approval_id`/`grant_id` grammar, attestation/approval/grant/delivery/receipt/callback bindings, mTLS interface constraints, replay/timestamp policy, root rotation/revocation, endpoint rejection, and no-inference failures.
  - Self-test rehashes endpoint and secret/key-marker attacks in both identifiers, plus whitespace/raw-authority variants, and rejects each mutation.
- Documentation and evidence:
  - `docs/experiments/p3f-independent-supervisor-protocol-adapter-design-contract.md`
  - `docs/plans/2026-07-25-p3f-independent-supervisor-protocol-adapter-design-contract.md`
  - `docs/experiment-ledger.md`

**Verified**
```sh
node --check contracts/p3d/verify.mjs &&
node contracts/p3d/verify.mjs &&
node contracts/p3d/verify.mjs --self-test &&
node --check contracts/p3f/verify.mjs &&
node contracts/p3f/verify.mjs &&
node contracts/p3f/verify.mjs --self-test
```
All passed. Fixture byte digests also match the manifest.

**Boundary remains closed**
- No protocol-adapter implementation.
- No network client/server, listener, endpoint configuration, RPC, or mTLS connection.
- No key/certificate/signature/root implementation.
- No OMP or supervisor process, child, source/artifact/evidence I/O, persistence, commit, or push.
- mTLS is an interface-only SPKI/channel-binding contract; endpoint authority is non-representable.
- Every failure remains exactly the normalized `waiting_for_human` projection.
