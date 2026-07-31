Working...
**PASS — P0b.3 complete.**

- `gui/src-tauri/src/lib.rs`
  - Added real bridge wire tests:
    - `launch_fixture_public_run_wire_json_is_frozen`
    - `get_run_public_run_wire_json_is_frozen`
  - Migrated only launch/get bridge and Tauri command results from `RunDto` to generated `Run`.
  - Both paths reuse existing `JsonRun -> Run` via `Run::from`.
  - Kept handwritten/private wire types intact; no schema, generator, daemon transport, events, list-runs, bootstrap, cancel, or health changes.
  - `main.ts` already imported generated `Run`; no renderer edit was needed.

- `docs/experiment-ledger.md`
  - Appended P0b.3 evidence with actual RED/GREEN and verification results.

**TDD evidence**
- RED: focused test command exited `101`; both tests reported `expected Run, found RunDto`.
- GREEN: after Go sidecar build, focused tests passed: `2 passed`, `13 filtered`, `1.57s`.
- Post-`cargo fmt` focused rerun passed: `2 passed`, `13 filtered`, `0.40s`.

**Verification**
- Node `v22.22.3` generator drift check: passed.
- Generated-public privacy check: passed; no `token` or `error` fields.
- TypeScript typecheck: passed.
- Vite web build: passed; 7 modules, `51ms`.
- Final `git diff --check`: passed.
- Final combined P0b/P0b.1/P0b.2/P0b.3 scope guard: passed for 41 changed worktree paths.

No commit or push performed.
