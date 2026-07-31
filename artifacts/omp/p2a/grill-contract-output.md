Working...
Implemented P2a as fixture/contract-only.

- Added `contracts/p2a/fixtures/`:
  - Frozen `ananke.grill.rules.v1` with exactly six deterministic rule classes.
  - Exact P1a Revision tuple binding on all evaluation and Question/Answer/Default/Override records.
  - Canonical input hash, five-question display bound, ten-question `needs_rewrite` cap, and idempotent replay vector.
  - Adversarial vectors rejecting Revision prose, model content, commands, approval state, retry loops, and `attempt_cap > 100`.
  - SHA-256 manifest plus hard-pinned fixture digests.

- Added `contracts/p2a/verify.mjs`:
  - JCS/UTF-8/manifest/digest validation.
  - Closed input and record schemas.
  - Exact six-rule table and fixed priority/risk/blocking/waivable/default/remedial fields.
  - Append-only contiguous record/question sequence checks.
  - Revision binding, bounded evaluation, waiver-slot, replay, and adversarial checks.
  - Self-test mutates copied fixtures and proves rejection of rule drift, command/approval injection, unbounded attempts, and sequence tampering.

- Added contract and implementation TDD plan:
  - `docs/experiments/p2a-deterministic-grill-contract.md`
  - `docs/plans/2026-07-23-p2a-deterministic-grill-contract.md`
  - Factual ledger entry in `docs/experiment-ledger.md`.

Verification passed:

```sh
node --check contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs --self-test
```

No GUI, daemon, Tauri, store migration, claims, workers, adapters, model calls, approval mutation, command execution, commit, or push was added or run.
