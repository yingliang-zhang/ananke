Working...
Implemented the P1c runtime-blocker repair. No commit or push.

- `internal/store/proposals.go`
  - `ListProposalActivity` now rejects invalid proposal IDs and absent `task_proposals` rows with `store.ErrProposalNotFound` before querying activity.
  - Missing IDs can no longer be represented as an empty activity history.

- RED→GREEN coverage added:
  - Store: invalid and unknown IDs return `ErrProposalNotFound`.
  - Lifecycle/daemon: `proposal_missing` returns private `error: "proposal not found"` and omits `proposal_activity`.
  - Real bridge: missing activity returns private `BridgeError::DaemonRejected("proposal not found")`; it cannot expose `ProposalActivityList { activity: [] }`; its public message remains the existing sanitized `The daemon rejected this request.` with no raw details.

- Ledger updated with factual RED/GREEN and gate evidence:
  - `docs/experiment-ledger.md`

Verification passed:
- Focused store and lifecycle regressions.
- `go test ./internal/store ./internal/lifecycle -count=1`
- Rebuilt bridge daemon, then focused real bridge test.
- `cargo fmt --check`
- `go test ./... -count=1`
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` — 20 tests.
- P1c verifier and self-test.
- Renderer-public generation/check/privacy, TypeScript typecheck, run-state, decoder, and privacy tests.
