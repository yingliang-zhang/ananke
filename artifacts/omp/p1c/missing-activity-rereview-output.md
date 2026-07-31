Working...
## ACCEPT

Prior blocker is repaired end-to-end.

- **Store:** `ListProposalActivity` rejects both invalid (`proposal-missing`) and valid-but-unknown (`proposal_missing`) IDs with `ErrProposalNotFound`; no zero-length history is returned. Focused and proposal-store regression tests pass.
- **Daemon:** `handleListProposalActivity` converts that sentinel into private `{"ok":false,"error":"proposal not found"}` and omits `proposal_activity`. Lifecycle test passes.
- **Bridge:** daemon raw errors remain internal as `BridgeError::DaemonRejected`; every Tauri command routes through `use_backend`, which emits only **“The daemon rejected this request.”** The real sidecar-backed bridge test verifies the missing-activity request fails rather than returning an empty public list and checks the sanitized public message has no raw daemon detail.
- **Public boundary/scope:** proposal outputs are closed generated DTOs; contract/privacy checks reject private fields. The proposal runtime surface is the seven specified operations plus the required target-scoped list query; no proposal UI invocation was found in `gui/src`.

Verification passed:

```text
go test ./internal/store -run 'Proposal|ListProposals' -count=1
go test ./internal/lifecycle -run 'Proposal|ListProposal' -count=1
cargo test --manifest-path gui/src-tauri/Cargo.toml \
  bridge_proposals_serialize_public_wire_replay_conflicts_and_reconnect --lib
cargo test --manifest-path gui/src-tauri/Cargo.toml --lib
node contracts/p1c/verify.mjs
node contracts/p1c/verify.mjs --self-test
npm --prefix gui run check:renderer-public
npm --prefix gui run check:renderer-public-privacy
npm --prefix gui run test:renderer-public
npm --prefix gui run test:renderer-public-privacy
npm --prefix gui run typecheck
```

The complete bridge library suite passed: **20/20**. No source files were edited or committed.
