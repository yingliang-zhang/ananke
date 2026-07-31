Working...
Implemented the P3f final-review repairs.

- `contracts/p3f/verify.mjs`
  - Refactored loading so P3d manifest + canonical fixture authentication and anchor derivation complete before **any** P3f manifest or fixture read.
  - Added self-test proof:
    - traced reads require P3d manifest and canonical fixture as the first dependencies;
    - corrupt P3d canonical bytes reject before any P3f read occurs.
  - Normal/self-test output now states the ordering and limits claims to the protocol-adapter slice.

- Scoped authoritative protocol-adapter claims in:
  - `docs/plans/2026-07-25-p3f-independent-supervisor-protocol-adapter-design-contract.md`
  - `docs/experiments/p3f-independent-supervisor-protocol-adapter-design-contract.md`
  - `docs/experiment-ledger.md`
  - Each now explicitly states predecessor P3e/P3f runtime paths are out of slice and makes no repository-wide absence/implementation claim.

- Retained exactly **37** authoritative protocol-adapter denial cases. The plan, design contract, and ledger now state that 37 is the closed, ordered adapter-denial inventory; the exact count prevents omission or reclassification and is not a repository-wide test count.
  - Targeted authoritative-text scan found no obsolete `all 27` / 27-case acceptance wording.

Verification passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

No network or commit command was invoked.
