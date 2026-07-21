Working...
**VALIDATED** — Arm A satisfies the frozen revision-3 contract. No cross-arm winner claim.

### Revision-3 correction
- Split the prior two-schema model into:
  - `daemon-request-private` → Go + Rust
  - `daemon-response-internal` → Go + Rust
  - `renderer-public` → Rust + TypeScript
- Moved all daemon response fixtures, `ok`, raw `error`, runs/events, and unknown-response tolerance to `daemon-response-internal`.
- Kept public bootstrap/activity/cancel/health in `renderer-public`.
- Modelled public activity’s nested `diagnostics` shape from the authoritative `renderer-activity.json`.
- Generated TypeScript contains no structural `token` or raw daemon `error` field.

### Changed experiment artifacts
- Schemas:
  - `schema/daemon-request-private.schema.json`
  - `schema/daemon-response-internal.schema.json`
  - `schema/renderer-public.schema.json`
  - Removed obsolete `schema/daemon-private.schema.json`
- Generated output:
  - `generated/go/daemon_request_private.go`
  - `generated/go/daemon_response_internal.go`
  - `generated/rust/daemon_request_private.rs`
  - `generated/rust/daemon_response_internal.rs`
  - `generated/rust/renderer_public.rs`
  - `generated/typescript/renderer_public.ts`
- Tests and runners:
  - `tests/go/generated_test.go`
  - `tests/rust/src/lib.rs`
  - `tests/typescript/fixture-roundtrip.mjs`
  - `scripts/generate.mjs`
  - `scripts/verify.mjs`
  - `scripts/check-typescript-privacy.mjs`
  - `scripts/check-generated.mjs`
  - `scripts/drift-probe.mjs`
  - `scripts/clean-regeneration.mjs`
  - `scripts/evolution-probe.mjs`
- Experiment support:
  - nine copied fixture files under `fixtures/`
  - `.gitignore`
  - `README.md`
  - pinned local `package.json` / `package-lock.json`
  - Rust test manifest/lock.

### Verification
Final reproducible command:

```sh
/Users/yingliangzhang/.hermes/node/bin/node scripts/verify.mjs
```

Result: **PASS**
- Full verifier: `12.175s`
- Fixture phase: `0.974s`
- Generation: `0.534s`
- Generated output: 6 files / 677 LOC
  - Go: 2 / 212
  - Rust: 3 / 209
  - TypeScript: 1 / 256

The final verifier passed:
- all nine fixtures in relevant generated runtimes;
- payload `null`, object, array, string, number, boolean preservation;
- absent optionals;
- wire-present `ok:false` plus daemon error;
- internal unknown-field tolerance;
- TypeScript `token` and raw daemon `error` field exclusion;
- staged content-drift rejection followed by restoration;
- clean regeneration with stale-output removal;
- bidirectional optional public-field evolution.

Additional checks:
- Fixture fidelity: `diff -r fixtures /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/contracts/p0a/fixtures` — pass, no output.
- `git diff --check -- experiments/p0a/jsonschema-quicktype` — pass, no output.

### Toolchain
- Node: `v22.22.3`
- Quicktype: `26.0.0`
- TypeScript: `5.9.3`
- Cargo: `1.97.1`

Evidence, original RED, revision-2 boundary defect, revision-3 RED/GREEN recovery, measurements, dependencies, and verdict are recorded in:

```text
experiments/p0a/jsonschema-quicktype/README.md
```

No unresolved Arm A contract limitation observed.
