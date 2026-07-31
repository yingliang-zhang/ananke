Working...
ACCEPT

- `Backend::launch_fixture`, `Backend::get_run`, and both Tauri commands return generated `Run`; `JsonRun -> Run` preserves `id`, `state`, and nested diagnostics, with lossless PID widening.
- Both focused tests use the real daemon-backed fixture path, require `Run` at compile time, and assert the complete serialized nested public JSON. `get_run` is cross-checked against the launched run.
- P0b.3-specific migration is confined to launch/get return conversions and those tests. No Go daemon files changed; schema/generator/bootstrap/event work in the uncommitted tree belongs to the preceding P0b/P0b.1/P0b.2 layers.
- Verification passed: `cargo test public_run_wire_json_is_frozen` (2 passed, 13 filtered), `cargo fmt -- --check`, generator drift check, generated-public privacy check, TypeScript typecheck, and `git diff --check`.
