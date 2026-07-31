Working...
Implemented P4 as a **design-only** fixture/verifier contract.

### Added
- `contracts/p4/fixtures/evidence-repair-admission-v1.canonical.json`
  - Immutable self-hashed proposal, revision, approval, fence, envelope, receipt, callback, source, artifact, route, test, and evaluation evidence.
  - P1–P3f bindings, including P3f adapter fixture and exact 37-denial oracle.
  - Independent verifier trust/release identities, canonical input/output, and zero-new-facts replay.
  - Bounded repair admission: cap `2`, one allowed role/route, exact 12-hash evidence set, fresh approval/fence, and typed MoA grant.
- `contracts/p4/fixtures/repair-admission-red-flags-v1.canonical.json`
  - 38 ordered denial vectors, all projecting only `waiting_for_human`.
- `contracts/p4/fixtures/fixtures.sha256`
- `contracts/p4/verify.mjs`
  - Independently authenticates P3f fixture bytes and its 37 red flags before reading P4 fixtures.
  - Verifies JCS bytes, hard digests, self-hashes, evidence relations, verifier identity, replay behavior, freshness, cap/role/route/MoA bindings, and no-success inference.
- `docs/experiments/p4-evidence-verifier-bounded-repair-admission-design-contract.md`
- `docs/plans/2026-07-25-p4-evidence-verifier-bounded-repair-admission-design-contract.md`
- P4 evidence entry appended to `docs/experiment-ledger.md`.

### Frozen fixture digests
- Canonical bundle: `sha256:aa7d94f96b123ff200bf4f84ec55d7b5edbd157f4578ba99ed3b4fdbc93ee36c`
- P4 denials: `sha256:91c900ce7cc2c53ce360775be0909b3e679a971756075d643f3b0d0e3eb4ce0f`
- P3f adapter link: `sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44`
- P3f denial link: `sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525` with exactly 37 cases.

### Verified
```sh
node --check contracts/p4/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p4/verify.mjs
node contracts/p4/verify.mjs --self-test
```

The self-test proved P3f authentication precedes every P4 read and rejected evidence, trust/release, replay, cap, role, route, exact-evidence, fresh approval/fence, typed-MoA, inferred-success, failure, and review-finding drift. No supervisor, network, OMP, repair execution, source/artifact operation, commit, or push was added.
