Working...
VERDICT: **CHANGES REQUESTED**

### MAJOR — B1 stale-socket safety remains incomplete

The private runtime layout itself is repaired:

- `gui/src-tauri/src/lib.rs:161-193` creates `/tmp/ananke-gui-<euid>`, rejects symlinks/non-directories/wrong owners, and enforces mode `0700`.
- `lib.rs:128-139` places both `daemon.sock` and the short `data` alias inside that directory.
- `lib.rs:60-84,664-672` maps bridge failures to generic public messages.

But the bridge still removes the endpoint after **any** failed ping:

- `gui/src-tauri/src/lib.rs:453-464`: a connected daemon returning `ok:false` becomes `BridgeError::DaemonRejected`.
- `lib.rs:474-479`: every non-successful `ping()` immediately calls `remove_known_stale_socket()` and spawns another daemon.

A successfully connected daemon rejecting authentication is not stale. `[INFERENCE]` If the persisted token is recreated/rotated while an existing daemon remains live, this unlinks that live daemon’s socket and attempts a second daemon against its store. The location/type checks in `remove_known_stale_socket` (`lib.rs:640-653`) do not establish liveness or ownership by this `Backend`.

The Go daemon independently retains arbitrary-path deletion:

- `cmd/ananke/main.go:20-31` accepts caller-controlled `-socket`.
- `internal/lifecycle/engine.go:330` unconditionally executes `os.Remove(e.cfg.SocketPath)` before binding.

That can unlink a regular file or empty directory selected by `-socket`, not just a verified stale Unix socket. This violates the stated requirement that stale removal cannot unlink an arbitrary path.

**Required repair:** classify connection failures. Only remove the fixed endpoint after a credible stale condition and endpoint validation; return the generic bridge error for a live/auth-rejecting/protocol-failing peer. Also make `Engine.Run` reject non-socket endpoints rather than blindly removing its configured path.

### Findings confirmed closed

- **M1 — error containment:** `GoResponse.error` is deserialized internally only (`gui/src-tauri/src/lib.rs:334-349`); it is not part of any serialized Tauri DTO. Bootstrap accepts only error text matching the two SQLite unique constraints (`lib.rs:627-638`), while other storage errors propagate to the generic public daemon-rejection message (`lib.rs:60-84`). The regression test covers both duplicate acceptance and `"database is locked"` rejection (`lib.rs:839-858`).

- **M2 — Backend E2E:** `bridge_bootstrap_launches_lists_events_cancels_and_reconnects` invokes `Backend`, not hand-built wire requests (`lib.rs:969-1031`). It requires all three sidecars (`lib.rs:907-928`), bootstraps, lists, launches, observes events, cancels to terminal state, reconnects with the persisted token, and verifies persisted results. `TestBackend::Drop` kills the spawned daemon and removes the temporary root (`lib.rs:895-905`).

- **M3 — release root:** `build.rs` no longer embeds a checkout path. Release mode selects `app_data/project-root` and enforces private mode (`lib.rs:196-228`); the release-root test checks that it differs from a simulated builder checkout and is `0700` (`lib.rs:861-872`).

- **m1 — frontend state:** `created` and `recovery_unknown` are active (`gui/src/run-state.ts:1-9`), and the state test asserts both are cancellable (`gui/scripts/test-run-state.mjs:16-20`). `main.ts:22,26` uses that predicate for active counts and Cancel enablement.

- **Tauri resources/API/secrets:** `gui/src-tauri/tauri.conf.json:28-35` bundles all three Go executables; `capabilities/default.json:1-7` grants only `core:default`. Registered command names and parameter signatures match the renderer’s invocations (`lib.rs:675-735`; `gui/src/main.ts:13-20`). Public DTOs expose run diagnostics and event data, not the daemon token, socket path, or runtime directory.

Per instruction, I did not start GUI/daemon processes or rerun process-spawning suites. The pass results listed in the request are treated as requester-provided evidence, not independently rerun here.
