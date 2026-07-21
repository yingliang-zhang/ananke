# P0b Renderer-Public Bootstrap Implementation Plan

> **For Hermes:** Execute with strict TDD and one isolated coding-agent session.

**Goal:** Replace the handwritten public bootstrap DTO boundary with JSON
Schema + Quicktype generated Rust/TypeScript types while preserving the current
Tauri JSON payload.

**Architecture:** A GUI-owned JSON Schema is canonical. A Node 22 Quicktype
script produces one Rust public model and one TypeScript public model. The Rust
bridge constructs the generated bootstrap model; `main.ts` imports the generated
TS type. Existing daemon/internal adapters remain untouched.

**Tech stack:** JSON Schema 2020-12, Quicktype, Node 22, Rust Serde, TypeScript,
Tauri 2.

---

### Task 1: Establish a failing bootstrap wire-contract test

**Files:** modify `gui/src-tauri/src/lib.rs`; test in its existing test module.

1. Write a focused test that serializes the public bootstrap command/result and
   asserts exact JSON including `project.root` and `workstream.project_id`.
2. Run only that test. It must fail because the generated bootstrap integration
   does not exist yet.
3. Preserve the failing command/output as P0b RED evidence.

### Task 2: Add canonical schema and deterministic generator

**Files:** create `gui/contracts/renderer-public-bootstrap.schema.json`,
`gui/scripts/generate-renderer-public.mjs`; modify `gui/package.json` and lock.

1. Describe exactly the frozen bootstrap payload.
2. Pin Quicktype in the GUI dev toolchain; Node 22 must be required.
3. Generate Rust and TypeScript under `gui/src-tauri/src/generated/` and
   `gui/src/generated/`.
4. Add a content-difference check mode; do not hand-edit generated files.

### Task 3: Integrate only the bootstrap public types

**Files:** modify `gui/src-tauri/src/lib.rs`, `gui/src/main.ts`.

1. Use generated Rust public types for the Tauri bootstrap return path.
2. Import generated TypeScript Bootstrap type into `main.ts`.
3. Do not change daemon transport, request/response JSON types, Run/Event DTOs,
   or semantic adapters.
4. Re-run the focused Rust test: it must pass (GREEN).

### Task 4: Add production-facing verification

**Files:** generator/check scripts and focused tests only as needed.

1. Prove TypeScript and Rust generated public fields exclude `token`/`error`.
2. Prove check mode rejects a content mutation before restoring generated output.
3. Run focused Rust test, `npm --prefix gui run typecheck`, and `npm --prefix gui run web:build` when available.
4. Run `git diff --check` and a whitelist-based production-scope check.
5. Update ledger only with actual outputs; do not commit.
