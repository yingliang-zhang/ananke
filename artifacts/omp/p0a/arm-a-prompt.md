You are implementing Arm A of Ananke P0a, a schema/codegen binary spike.

WORKTREE: /Users/yingliangzhang/Projects/ananke-p0a-jsonschema-quicktype
BASE: eedba6d
SHARED CONTRACT (read-only): /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/docs/experiments/p0a-schema-codegen-contract.md
SHARED FIXTURES (read-only): /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/contracts/p0a/fixtures/

This is an isolated experiment. Do NOT read or list the Proto/Buf arm directory, any other p0a arm directory, their artifacts, prompts, outputs, manifests, git diffs, or review results. You may read only this worktree plus the two explicitly authorized shared-contract paths above.

Implement JSON Schema 2020-12 + Quicktype in experiments/p0a/jsonschema-quicktype/ only. Do not edit production DTOs or transport files: internal/lifecycle/engine.go, gui/src-tauri/src/lib.rs, gui/src/main.ts, go.mod, gui/package.json, gui/src-tauri/Cargo.toml. Do not commit, push, or install global/brew dependencies.

Use strict TDD:
1. Create a focused experiment test command/runner first and run it to confirm it fails because schemas/generated artifacts are missing.
2. Add the minimum schema/config/generation runner to make it pass.
3. Add one behavior at a time: fixture decode/round-trip (including payload null/object/array/string/number/bool), absent optionals, unknown inbound field tolerance, TS private-token exclusion, generated-file drift rejection, clean regeneration, optional-field evolution probe.
4. Run focused verification after each change.

Requirements:
- Pin and record exact Quicktype version locally to the experiment. Use Node 22 from /Users/yingliangzhang/.hermes/node/bin; do not use system Node 26.
- Rust is available at /Users/yingliangzhang/.cargo/bin/cargo (not on the default PATH); prepend that directory when compiling generated Rust. Do not install Rust globally.
- Generate Go, Rust/Serde, and TypeScript artifacts from JSON Schema. Generated files cannot be hand edited.
- Keep daemon-private and renderer-public schemas/artifacts separate. The generated TypeScript tree must contain no field named token.
- Preserve existing newline JSON wire names; do not alter production transport.
- If Rust tooling is unavailable, distinguish environmental prerequisite failure from an Arm A semantic failure. Attempt only a local/project-scoped solution; never install a global toolchain.
- Record commands, exact versions, elapsed seconds, generated file/LOC counts, dependency changes, TDD RED/GREEN evidence, and current arm verdict in experiments/p0a/jsonschema-quicktype/README.md.
- Keep stdout compact; redirect verbose logs to experiment artifacts and print only PASS/FAIL plus paths.

Before final response, run the arm's complete reproducible verification command and git diff --check. Report changed files, test commands/results, unresolved limitations, and one of VALIDATED / BOUNDED — CHANGES REQUESTED / REJECTED. Do not claim a winner.
