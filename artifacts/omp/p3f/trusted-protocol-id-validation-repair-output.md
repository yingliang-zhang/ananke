Completed PImplemented P3f trusted-protocol review repair.

- Exact **37-case** authority now binds verifier, manifest, plan, experiment, ledger, and recorded P3f evidence. New red-flag fixture digest:  
  `sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`
- Added strict safe opaque-ID validation in `contracts/p3f/verify.mjs` for:
  - `release_approval.approval_id`
  - `moa_typed_role_grant.grant_id`  
  Grammar: `[a-z][a-z0-9_]{2,63}`; rejects URL syntax, whitespace, secret/key markers, and authority/key/identity-style fragments.
- Replaced generic plaintext-secret/endpoint vectors with two canonical identifier vectors covering **both IDs**:
  - secret/key-marker payloads
  - endpoint/URL authority payloads  
  Both project exactly `waiting_for_human`.
- Added rehashed self-test mutations for endpoint and secret/key attacks separately in approval and grant IDs, plus whitespace and raw-authority variants.
- Removed stale 27-case wording from recorded review artifacts; retained Darwin macOS 27 references. Targeted stale-claim scan returned **no matches**.
- Verified canonical fixture directly: **37** cases; every case has empty events, `result: null`, `verification_state: not_run`, and `state: waiting_for_human`.

Verification passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

No network operation, runtime protocol adapter, commit, or push was performed.
