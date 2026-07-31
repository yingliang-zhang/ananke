Working...
Implemented the P4 review-blocker repair.

- `contracts/p4/verify.mjs`
  - Added a one-to-one `Map` for all exact 38 P4 denial kinds.
  - Each kind has a targeted invalid evidence/admission mutator and expected invariant-specific rejection.
  - Rehashes bundle, grant, admission, verifier input/output, and replay dependents where required; intentional self-hash drift remains un-rehashed.
  - Asserts map cardinality and ordered keys exactly match the red-flag fixture inventory.
  - For every denial class, verifies its declared projection exactly equals the canonical closed `waiting_for_human` projection and exercises the target verifier/validator rejection.
  - Preserved P3f-first authentication proof and design-only/no-repair boundary.

- Updated P4 plan and experiment ledger with the exhaustive mapping and verification evidence.

Verification passed:
```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
node --check contracts/p4/verify.mjs
node contracts/p4/verify.mjs
node contracts/p4/verify.mjs --self-test
```

P4 self-test confirms the rehashed 38-case map rejects every intended invariant while retaining the identical closed `waiting_for_human` denial projection.
