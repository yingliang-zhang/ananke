Working...
## CHANGES REQUESTED

### Blocking finding

1. **`list_proposal_activity` treats a missing proposal as an empty history, not a sanitized not-found error.**

   - `internal/store/proposals.go:687` queries only `task_proposal_activity`; an unknown `proposal_id` produces zero rows and `nil` error. It neither validates the identifier nor establishes that the proposal exists.
   - `internal/lifecycle/engine.go:1957` forwards that successful empty slice.
   - `gui/src-tauri/src/lib.rs:828` converts it to `ProposalActivityList { activity: [] }`, bypassing the existing sanitized error path.
   - This conflicts with the runtime requirement to map store conflict/not-found conditions through the public sanitized bridge behavior (`docs/plans/2026-07-22-p1c-task-proposal-public-protocol.md:60`).

   **Required fix:** make the activity query reject unknown proposals with `store.ErrProposalNotFound` (and invalid IDs), retain raw details only within the private daemon bridge, and add store → daemon → real-bridge coverage proving `proposal_missing` returns the generic sanitized error. Existing bridge coverage tests missing `get_proposal` only; it does not test missing activity.

### Accepted portions

- All seven private hyphenated daemon commands are dispatched in `internal/lifecycle/engine.go`.
- All seven generated DTO-based Tauri commands are registered in `gui/src-tauri/src/lib.rs`; public handlers return only generated proposal DTOs.
- `cmd`, `token`, daemon `ok`, and raw `error` remain private `GoRequest`/`GoResponse` transport fields. `use_backend` maps daemon failures through `BridgeError::public_message()`.
- Mutation identity remains the exact four-field public shape: `proposal_id`, `revision`, `revision_hash`, `approval_id`.
- List ordering is deterministic by `created_at ASC, proposal_id ASC`; activity ordering is `sequence ASC`.
- Real bridge coverage exercises create replay, conflict sanitization, detail/list/activity serialization, decision/withdraw state, and reconnect list consistency.
- Scope is contained: tracked diffs modify only the Tauri bridge, lifecycle engine, proposal store, and store test; the untracked `internal/lifecycle/proposal_api_test.go` is the corresponding integration test. No renderer UI, Grill, claims, worker, or supervisor changes.

### Verification run

Passed:

- `node contracts/p1c/verify.mjs`
- `node contracts/p1c/verify.mjs --self-test`
- `npm --prefix gui run check:renderer-public`
- `npm --prefix gui run check:renderer-public-privacy`
- `npm --prefix gui run typecheck`
- Focused store and lifecycle proposal tests
- Focused real bridge test: `bridge_proposals_serialize_public_wire_replay_conflicts_and_reconnect`
- `npm --prefix gui run test:state`
- `npm --prefix gui run test:renderer-public`
- `npm --prefix gui run test:renderer-public-privacy`
- `go test ./...` — lifecycle, store, and supervisor passed
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` — 20 tests passed
- `git diff --check`

No files were edited or committed.
