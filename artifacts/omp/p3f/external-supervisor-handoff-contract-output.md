Working...
Implemented P3f external independently trusted supervisor handoff as a **design-only contract**.

### Added
- `contracts/p3f/fixtures/external-supervisor-handoff-v1.canonical.json`
  - P3d → P3f activation → Darwin `none_fail_closed` binding.
  - Sealed JCS launch envelope with source/artifact/evidence identities, route-map hash, opaque fence binding, deadline, and attempt cap.
  - Independent supervisor release provenance, approval, and trust-root rotation policy.
  - Authenticated callback/result declaration, no-outcome-inference recovery, cancellation, replay/idempotency, and typed MoA role-grant boundary.
  - Explicitly forbids secrets, raw paths, source/evidence bytes, prompt/prose authority, argv/environment, local path execution, fallback, and runtime integration.

- `contracts/p3f/fixtures/external-supervisor-red-flags-v1.canonical.json`
  - 23 fail-closed vectors covering local launch fallbacks, release/route/source/artifact/evidence drift, fence/deadline/cap drift, callback/result failures, no response, cancellation/recovery inference, replay conflict, trust-root downgrade, MoA grants/fallback, and prompt-authority transmission.

- `contracts/p3f/verify.mjs`
  - Validates the full P3d/P3f canonical-byte chain.
  - Validates envelope self-hash, exact route-aware mapping, provenance, root rotation, receipt-only authority, callback/result policy, cancellation/recovery, replay, MoA grants, and the closed failure projection.
  - Extends in-memory self-tests for every added policy class.

- Documentation and evidence:
  - `docs/plans/2026-07-24-p3f-external-supervisor-handoff-design-contract.md`
  - `docs/experiments/p3f-external-supervisor-handoff-design-contract.md`
  - `docs/experiment-ledger.md`
  - Updated `contracts/p3f/fixtures/fixtures.sha256`.

### Verification
Passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

No OMP, supervisor, network delivery/request, callback, child, local execution, source/artifact/evidence I/O, commit, or push was performed.
