Working...
Implemented **P3f contract-only production self-hosted OMP activation**.

- Added frozen P3f fixtures and SHA-256 manifest:
  - `contracts/p3f/fixtures/production-activation-v1.canonical.json`
  - `contracts/p3f/fixtures/preflight-red-flags-v1.canonical.json`
  - `contracts/p3f/fixtures/fixtures.sha256`
- Added `contracts/p3f/verify.mjs`:
  - Hard-pins and validates the P3d fixture manifest/canonical bytes and P3d route, deadline, source snapshot, HostSpec, and P3c action.
  - Enforces tracked Git commit → RFC 8785 JCS source-manifest hash derivation.
  - Freezes approved wrapper SHA-256 plus exact P3d wrapper-kind/route pair.
  - Declares inherited FD-only manifest/source/evidence, OS-enforced read-only/write-denied sandbox, owned descriptor/device-inode cleanup, and argv/environment credential denial.
  - Enforces launch-time deadline, full private-fence, P3c, source, wrapper, and route checks.
  - Requires normalized `waiting_for_human` output for every preflight red flag.
- Added contract documents:
  - `docs/plans/2026-07-24-p3f-production-self-hosted-omp-activation-contract.md`
  - `docs/experiments/p3f-production-self-hosted-omp-activation-contract.md`
- Added P3f scope, evidence, and explicit activation prohibition to `docs/experiment-ledger.md`.

Verification passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

No real wrapper, sandbox, source/worktree, OMP session, production child, activation command, frozen verification command, commit, or push was created or launched.

**A real child cannot be launched until both the sandbox and production wrapper implementation are accepted.**
