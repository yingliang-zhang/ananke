Working...
Implemented P1c contract-only repair.

- `contracts/p1c/verify.mjs`
  - Computes `hashCanonical(get_proposal.result.revision)` once.
  - Directly requires it to equal:
    - Proposal `current_revision_hash`
    - RevisionLifecycle `revision_hash`
    - Approval `revision_hash`
    - frozen P1a root Revision hash
  - Adds a self-test probe that preserves the immutable create-input/Revision link, recanonicalizes the copied fixture, rewrites its copied manifest, and confirms the embedded Revision/hash mismatch is rejected.

- Fixture and manifest unchanged: the embedded P1c Revision canonical bytes already exactly equal P1a’s Revision fixture and hash:
  `sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263`.

- `docs/experiment-ledger.md`
  - Appended real RED/GREEN evidence and scope verdict.

Verification passed:

```sh
node --check contracts/p1c/verify.mjs \
  && node contracts/p1c/verify.mjs \
  && node contracts/p1c/verify.mjs --self-test
```

Observed: exit `0` in `0.42s`; self-test reported rejection of the consistently rehashed embedded Revision/hash mismatch.
