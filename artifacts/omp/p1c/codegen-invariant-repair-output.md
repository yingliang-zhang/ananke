Working...
Implemented P1c repair; scope remained contract/codegen/test only. No daemon, Tauri command, UI, or store integration. No commit/push.

- Added all 11 P1c schema documents plus embedded `ProposalActivity` to `gui/scripts/generate-renderer-public.mjs`.
  - Embedded activity is materialized from its schema fragment into a temporary Quicktype input.
  - Generated 12 TypeScript modules, 12 Rust modules, and regenerated `gui/src-tauri/src/generated/mod.rs`.

- Strengthened P1c schema semantics:
  - Semantic UTC timestamp markers.
  - UTF-8 byte-limit markers for P1a text fields.
  - Approval terminal/non-terminal state, nullability, actor, reason, and idempotency coupling.
  - Revision root-parent and immediate-parent invariants.
  - Updated pinned schema digests.

- Extended `contracts/p1c/verify.mjs`:
  - Validates the new semantic schema markers and conditional branches.
  - Direct self-tests reject invalid timestamp, UTF-8 byte overflow, invalid Approval coupling, invalid root/non-immediate Revision parents.
  - Executes and self-tests all seven Tauri→daemon command mappings; a daemon command typo is rejected.

- Expanded renderer coverage:
  - Decoder test covers every P1c DTO, including embedded activity.
  - Privacy test injects a prohibited field into every P1c schema target, including embedded activity.
  - Generator drift check includes every generated target.

- Recorded only observed RED/GREEN results in `docs/experiment-ledger.md`.

Verification passed:

```text
node contracts/p1c/verify.mjs
node contracts/p1c/verify.mjs --self-test
npm --prefix gui run generate:renderer-public
npm --prefix gui run check:renderer-public
npm --prefix gui run check:renderer-public-privacy
npm --prefix gui run test:renderer-public
npm --prefix gui run test:renderer-public-privacy
npm --prefix gui run typecheck
cargo test                 # gui/src-tauri: 19 tests, 3 suites
```
