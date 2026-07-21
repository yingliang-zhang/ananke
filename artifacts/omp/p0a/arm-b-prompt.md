You are implementing Arm B of Ananke P0a, a schema/codegen binary spike.

WORKTREE: /Users/yingliangzhang/Projects/ananke-p0a-proto-buf
BASE: eedba6d
SHARED CONTRACT (read-only): /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/docs/experiments/p0a-schema-codegen-contract.md
SHARED FIXTURES (read-only): /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/contracts/p0a/fixtures/

This is an isolated experiment. Do NOT read or list the JSON Schema/Quicktype arm directory, any other p0a arm directory, their artifacts, prompts, outputs, manifests, git diffs, or review results. You may read only this worktree plus the two explicitly authorized shared-contract paths above.

Implement Proto3 + Buf experiment in experiments/p0a/proto-buf/ only. Use Buf plus pinned Go/Rust/TypeScript generators appropriate for the contract. The current newline-delimited JSON socket remains the transport: use ProtoJSON mapping for this experiment; do not switch to binary Protobuf or gRPC. Do not edit production DTOs or transport files: internal/lifecycle/engine.go, gui/src-tauri/src/lib.rs, gui/src/main.ts, go.mod, gui/package.json, gui/src-tauri/Cargo.toml. Do not commit, push, or install global/brew dependencies.

Use strict TDD:
1. Create a focused experiment test command/runner first and run it to confirm it fails because schemas/generated artifacts are missing.
2. Add the minimum proto/config/generation runner to make it pass.
3. Add one behavior at a time: exact snake_case ProtoJSON fixture mapping; payload null/object/array/string/number/bool behavior; absent optionals; unknown inbound field tolerance; TypeScript private-token exclusion; generated-file drift rejection; clean regeneration; optional-field evolution plus Buf breaking probe.
4. Run focused verification after each change.

Requirements:
- Pin and record exact Buf/protoc/plugin versions locally to the experiment. Use Node 22 from /Users/yingliangzhang/.hermes/node/bin; do not use system Node 26.
- Rust is available at /Users/yingliangzhang/.cargo/bin/cargo (not on the default PATH); prepend that directory when compiling generated Rust. Do not install Rust globally.
- Generate Go, Rust/Serde/ProtoJSON, and TypeScript artifacts from proto source. Generated files cannot be hand edited.
- Keep daemon-private and renderer-public artifacts separate. The generated TypeScript tree must contain no field named token.
- Treat ProtoJSON incompatibility with frozen fixture semantics as an Arm B failure, not a reason to edit the shared fixtures or production transport.
- If Rust/protoc tooling is unavailable, distinguish environmental prerequisite failure from an Arm B semantic failure. Attempt only a local/project-scoped solution; never install a global toolchain.
- Record commands, exact versions, elapsed seconds, generated file/LOC counts, dependency changes, TDD RED/GREEN evidence, and current arm verdict in experiments/p0a/proto-buf/README.md.
- Keep stdout compact; redirect verbose logs to experiment artifacts and print only PASS/FAIL plus paths.

Before final response, run the arm's complete reproducible verification command and git diff --check. Report changed files, test commands/results, unresolved limitations, and one of VALIDATED / BOUNDED — CHANGES REQUESTED / REJECTED. Do not claim a winner.
