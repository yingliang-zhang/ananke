Working...
Completed P3f trusted protocol inventory repair.

- `contracts/p3f/verify.mjs`
  - Defines the authoritative protocol-adapter inventory: exactly **37** cases.
  - Pins red-flag digest `sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`.
  - Asserts the manifest binding explicitly.
  - Self-test rejects both a 36-case inventory and a substituted manifest digest.
  - Normal/self-test output records the exact 37-case count, current fixture digests, and strict opaque identifier denial coverage.

- Updated authoritative protocol contract evidence:
  - `docs/plans/2026-07-25-p3f-independent-supervisor-protocol-adapter-design-contract.md`
  - `docs/experiments/p3f-independent-supervisor-protocol-adapter-design-contract.md`
  - `docs/experiment-ledger.md`

  They now record:
  - 37 current red flags;
  - red-flag fixture: `sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`;
  - canonical wire fixture: `sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44`;
  - exact retained failure projection:
    ```json
    {"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
    ```

- The exact **37**-case inventory is authoritative across the protocol plan, experiment, ledger, and recorded evidence. The separate Darwin macOS 27 platform lifecycle case remains unchanged.

Verification passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

Direct SHA-256 confirmation matched both fixture bytes. No network access or commit performed.
