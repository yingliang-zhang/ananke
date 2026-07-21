# P0b.1 Renderer-Public Run Implementation Plan

> **For Hermes:** Execute with strict TDD in one isolated coding-agent session.

**Goal:** Move the actual `list_runs` Tauri response and its TypeScript consumer
to generated public Run types while retaining the needed internal-to-public
semantic adapter.

**Architecture:** Extend the existing GUI JSON Schema + Quicktype generator with
one public Run schema. The generated Rust Run model becomes `list_runs`' public
return type; generated TS Run replaces `main.ts` local declaration. The existing
flat `JsonRun` remains internal and maps into the generated nested Run.

---

### Task 1: Write and observe a failing list-runs wire test

- Modify `gui/src-tauri/src/lib.rs` test module only.
- Test a real backend path that returns one known public Run and asserts exact
  nested JSON including diagnostics fields.
- Run the focused test; it must fail because generated Run integration is absent.

### Task 2: Extend the canonical generator

- Create a public Run schema under `gui/contracts/`.
- Extend `gui/scripts/generate-renderer-public.mjs` and generated module exports
  to create/check Rust + TS Run models.
- Ensure content check includes all targets and privacy scans every generated
  public Rust/TS output.

### Task 3: Make the smallest Rust/TS integration

- Modify `gui/src-tauri/src/lib.rs`: map internal `JsonRun` to generated public
  Run only in `list_runs`; do not alter launch/get paths.
- Modify `gui/src/main.ts`: import generated Run type and remove its local
  handwritten Run annotation.
- Re-run the focused test until it is GREEN.

### Task 4: Verify and record

- Prove a controlled generated Run mutation causes check failure before restore.
- Run focused Rust test, formatter, renderer-public check/privacy, typecheck,
  web build, diff/scope guard.
- Update the ledger with actual commands/results; no commit.
