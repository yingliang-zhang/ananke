Working...
Implemented P3d contract-only artifacts.

- Added `contracts/p3d/`:
  - Canonical HostSpec/request/result/event fixture.
  - Adversarial fail-closed vectors.
  - Crash/recovery vectors.
  - SHA-256 manifest and hard-pinned dependency-free verifier.
- HostSpec freezes:
  - `ananke_omp_readonly_wrapper_v1` + exact `ananke_omp_read_only_audit_v1` route; rejects bare `omp`.
  - Provider `omp`, model `omp_audit_model_v1`, P3a deadline/cap, sealed materialization payload/hash/nonce, canonical Ananke repository identity/source snapshot, transcript normalization, bounded cancellation/reconnect, and read-only verification fingerprint.
  - P3a/P3b/P3c binding, including only opaque fence correlation—no token.
- Public IR is closed and bounded. Unknown transcript sources/dialects/events and renderer command/prompt/prose/private-field attempts return only less-informative `waiting_for_human`; no raw errors, paths, sockets, tokens, commands, or transcript content.
- Crash vectors forbid guessed events, results, terminal state, cancellation completion, or process outcome.
- Added:
  - `docs/experiments/p3d-controlled-readonly-omp-adapter-contract.md`
  - `docs/plans/2026-07-24-p3d-controlled-readonly-omp-adapter-contract.md`
  - P3d ledger entry in `docs/experiment-ledger.md`.

Verification passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
```

The self-test reads fixture bytes and mutates cloned values only in memory. `contracts/p3d/verify.mjs` contains no child-process, spawn, temp-directory, fixture-copy, or file-write primitives. No adapter/runtime/UI/daemon/worktree/source-materialization implementation was added.
