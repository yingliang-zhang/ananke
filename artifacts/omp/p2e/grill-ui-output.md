Working...
Implemented P2e deterministic Grill review UI.

- Added `gui/src/grill-review.ts`:
  - Guarded current-Revision state.
  - Immutable Proposal ID / Revision / Revision-hash display.
  - Bounded, sequence-ordered five-question rendering.
  - Risk, default, remedial-step, and waiver context.
  - Default, acknowledge, and scope-compatibility-only waiver actions.
  - Pending-state action disablement and post-record deterministic re-evaluation.
  - Fixed sanitized renderer error; raw bridge errors never render.

- Integrated generated DTO codecs in `gui/src/main.ts`.
  - Existing public Proposal list/detail DTOs establish a coherent current Revision tuple.
  - Grill actions invoke only `evaluate_grill`, `record_grill_default`, `record_grill_answer`, and `record_grill_override`.
  - No backend commands, private bridge/wire fields, model, worker, claim, approval, or execution paths added.

- Added renderer state/DOM contract coverage: `gui/scripts/test-grill-review.mjs`.
  - Includes a RED regression for disabled DOM controls dispatching actions; handlers now reject disabled controls.
  - Added `npm run test:grill-review`.

- Added persistent Mac accessibility selector `ananke-grill-review`; Mac preflight harness now asserts it.

- Added UI styling and recorded scope/evidence in `docs/experiment-ledger.md`.

Verification passed:

- `npm --prefix gui run typecheck`
- `npm --prefix gui run web:build`
- Renderer state, Grill state/DOM, generated DTO, privacy, and codegen checks.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml` — 25 passed.
- `go test ./...` — 3 packages passed; 3 without tests.
- P1a, P1c, P2a, and P2c contract verifiers.
- `npm --prefix tests/mac2 test` — 7 passed.
- Browser smoke: Vite visibly rendered the guarded `ananke-grill-review` region with a disabled refresh control when no Tauri-backed Revision was available.

No commit or push performed.
