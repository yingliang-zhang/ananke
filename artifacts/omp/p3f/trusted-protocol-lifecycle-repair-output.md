Working...
Repaired P3f protocol-adapter authority contract.

- `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-v1.canonical.json`
  - Defines three lifecycle sets: release, approval, and typed-MoA roots.
  - Freezes verification at `delivery.issued_at`, `receipt.issued_at`, and `callback.issued_at`.
  - Requires `record.issued_at <= verification_time < record.not_after` at every boundary.
  - Enforces active-root-before-successor, successor-only-on/after activation, strict overlap, revocation at `effective_at <= verification_time`, and downgrade rejection.

- `contracts/p3f/fixtures/independent-supervisor-protocol-adapter-red-flags-v1.canonical.json`
  - Adds approval expiry vectors for delivery, receipt, callback.
  - Adds root revocation-before-delivery/receipt/callback vectors.
  - Adds expired/wrong-root/revoked MoA-grant vectors.
  - Adds MoA rotation-overlap and successor-binding drift vectors.
  - Every vector retains the exact closed `waiting_for_human` projection.

- `contracts/p3f/verify.mjs`
  - Revalidates attestation, approval, MoA grant, and all corresponding root states at each frozen boundary.
  - Adds self-tests that coherently rehash dependent wire records, then prove expiry, revocation, wrong-root, overlap, and successor-binding mutations reject.
  - Updates fixture hard digests and manifest bindings.

- Updated the P3f plan, design contract, and experiment ledger with the temporal authorization and root-lifecycle rules.

Verification passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

No network, runtime adapter, supervisor, child process, commit, or push was added.
