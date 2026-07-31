Working...
Implemented P3a as a contract/fixture-only slice.

- Added `contracts/p3a/fixtures/`:
  - Immutable P1-bound launch admission, local approval eligibility, sealed materialization, HostSpec/transcript/verification fingerprints.
  - Separate fenced `task_claim`, materialization, launch outbox, and Run facts.
  - Stale-token denial vectors for Run creation, terminal facts, and evidence.
  - Fail-closed `waiting_for_human` vectors for unknown prompt/command inputs, invalid launch fields, unknown dialects, and unknown event shapes.
  - Recovery vectors for claim→materialization, materialization→Run, and Run→process boundaries.
  - SHA-256 manifest with hard-pinned fixture bytes.

- Added `contracts/p3a/verify.mjs`:
  - Dependency-free Node verifier and copied-fixture self-test.
  - Validates JCS, hashes, exact P1 tuple, local approval fact, closed shapes, scope/cap bounds, sealing, token fences, fail-closed behavior, and no-guess recovery.
  - Rejects raw command/prompt authority, unverified materialization, writable scope, stale-token impersonation, unknown-dialect success inference, and invented terminal recovery facts.

- Added documentation:
  - `docs/plans/2026-07-23-p3a-fenced-launch-admission-contract.md`
  - `docs/experiments/p3a-fenced-launch-admission-contract.md`
  - P3a evidence entry in `docs/experiment-ledger.md`

Verification completed — contract checks only:

```sh
node --check contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test

node contracts/p1a/verify.mjs
node contracts/p1c/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2c/verify.mjs
node contracts/p3a/verify.mjs
```

All exited `0`. No production schema, SQLite, runtime, daemon, Tauri, UI, claim/lease, worktree, adapter, OMP/process, command execution, commit, or push was added or run.
