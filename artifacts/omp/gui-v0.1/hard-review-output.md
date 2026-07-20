Working...
VERDICT: CHANGES REQUESTED

### BLOCKER — predictable shared socket leaks the daemon credential and is not safe stale-socket handling

- `gui/src-tauri/src/lib.rs:18` fixes the daemon endpoint at `/tmp/ananke-gui-v01.sock`.
- `lib.rs:348-364` connects and serializes a request containing the long-lived daemon token **before** authenticating the peer.
- `lib.rs:366-371` treats any failed ping as a stale endpoint; `lib.rs:508-517` unlinks any socket at that pathname.
- `internal/lifecycle/engine.go:330` then unconditionally removes the configured socket again before binding.
- Verified `/private/tmp` is `drwxrwxrwt root:wheel`.

[INFERENCE] A pre-created Unix socket in that shared, predictable directory receives the `ping` JSON and its token. The bridge has neither a private parent directory nor peer credential validation. It also has no proof that a socket is stale or owned before unlinking it.

**Required repair:** use a per-user, mode-`0700` directory for both the daemon socket and short data alias, with a stable path persisted under app data if reconnection is required. Do not remove an endpoint merely because authentication or connection failed; remove only an endpoint created/owned by this runtime under that private directory.

### MAJOR — bootstrap converts every daemon-side creation failure into success

- `gui/src-tauri/src/lib.rs:435-439` accepts every `DaemonRejected` as “already exists.”
- The daemon returns `ok:false` for all project/workstream storage failures (`internal/lifecycle/engine.go:1620-1628`), not just uniqueness conflicts.
- `GoResponse` has no error/code field (`gui/src-tauri/src/lib.rs:236-249`), so the bridge cannot distinguish a duplicate from a locked/corrupt/unavailable journal.

This can return a `BootstrapDto` although neither durable project nor workstream was established. The renderer then proceeds as if bootstrap succeeded.

**Required repair:** make bootstrap idempotency explicit—e.g. a typed duplicate response or authenticated read/verify of the existing project/workstream—and surface every other daemon rejection as a bridge failure.

### MAJOR — claimed Rust bridge E2E bypasses the bridge and may silently skip

- The integration test manually starts `ananke` and hand-builds raw `GoRequest` messages (`gui/src-tauri/src/lib.rs:658-754`).
- It never creates `Backend`, calls `ensure_daemon`, invokes `bootstrap`, validates token-file behavior, exercises resource resolution, or covers Rust DTO/command paths.
- `lib.rs:714-717` returns success when the three `.ananke/bin` files are absent.

The current test proves a raw Go daemon/fakeworker scenario, not the required Rust bridge path `start/connect → bootstrap → list/launch → events → durable cancel`.

**Required repair:** test public `Backend` operations against a temporary app-data directory and a controlled packaged-sidecar directory; make sidecar availability a test setup prerequisite, not a passing skip. Cover reconnect to the persisted token/socket and cancellation through `Backend::cancel_run`.

### MAJOR — release bootstrap embeds the build-machine checkout as durable project root

- `gui/src-tauri/build.rs:4-9` compiles `ANANKE_REPOSITORY_ROOT` from `CARGO_MANIFEST_DIR`.
- `gui/src-tauri/src/lib.rs:87-97` uses that value in release builds.
- `lib.rs:406-426` writes it into the durable project and returns it in the bootstrap DTO.

[INFERENCE] An `.app` moved to a Mac without `/Users/yingliangzhang/Projects/ananke` creates a project whose root is the builder’s nonexistent source checkout. The sidecar resource mapping itself is correct; this is a separate release-path defect.

**Required repair:** make the project root a runtime-selected/configured path, or use a valid app-data/runtime path for the fixed fixture project. Never embed a build-machine checkout in a release artifact’s durable state.

### MINOR — UI misclassifies cancellable nonterminal states as settled

- `gui/src/main.ts:9` treats only `running`, `cancelling`, and `cleanup_required` as active.
- `main.ts:26` disables Cancel whenever that predicate is false.
- `internal/store/state.go:19-24` defines `created` and `recovery_unknown` as nonterminal.
- `internal/store/cancellation.go:18-21` explicitly supports durable cancellation intent for both.

A `created` or `recovery_unknown` run is counted as done and cannot be cancelled from the GUI.

### LOW — Rust integration cleanup leaves temporary data directories

- The test creates `data_dir` at `gui/src-tauri/src/lib.rs:719-724`.
- `TestDaemon::drop` only removes the daemon socket and `root` (`lib.rs:643-655`).
- Observed after the focused Rust test: `/tmp/ananke-gui-data-72209-0/` remained.

### Verified passes

- `list-runs` is authenticated before command dispatch: `internal/lifecycle/engine.go:1598-1613`.
- It requires `project_id`, preserves store ordering, applies optional `workstream_id`, and emits typed `jsonRun` fields only: `engine.go:1582-1590,1740-1755`.
- Rust token generation uses 32 bytes from `/dev/urandom`, hex encoding, `create_new`, and `0600`; app-data and run directories are forced to `0700`.
- Bridge error mapping is generic and does not interpolate socket paths or tokens.
- Tauri resource mapping includes all three Go executables, and the capability grants only `core:default`; no filesystem/shell plugin capability was found.
- Frontend invokes registered commands with matching names and does not contain or persist the daemon token.

### Commands run

```sh
# repository root
go test ./internal/lifecycle -run '^TestEngineListRunsByProject$' -count=1 -timeout 60s
cargo test --manifest-path gui/src-tauri/Cargo.toml --lib
stat -f '%Sp %Su:%Sg %N' /private/tmp

# gui/
npm run typecheck
npm run web:build
```

Results:

- Focused Go API test: PASS.
- Rust crate tests: PASS, 3 tests; emitted three dead-code warnings.
- TypeScript typecheck: PASS.
- Vite production build: PASS.

Constraint note: `npm run web:build` regenerated ignored `gui/dist` output once. No source edits, GUI launches, app-bundle builds, destructive commands, commits, or pushes were performed.
