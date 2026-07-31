Working...
Implemented P1a contract/fixture slice only. No SQLite proposal storage, GUI/IPC, claims, workers, adapters, execution, commit, or push.

**Frozen contract**
- Local GUI operator is the only actor.
- Deterministic policy is authoritative; models are advisory-only.
- Future budget shape: `deadline` + `attempt_cap`.
- Future adapter shape: read-only `omp_audit`.
- Exact Proposal / immutable Revision / Approval fields, state machines, canonical RFC 8785 JSON bytes, SHA-256 versioning, privacy allowlist, idempotent replay/conflict rules, restart and concurrency outcomes.
- Explicit non-goals and future storage TDD sequence follow existing `internal/store` migration-fixture conventions.

**Changed paths**
```text
contracts/p1a/verify.mjs
contracts/p1a/fixtures/acceptance-v1.canonical.json
contracts/p1a/fixtures/approval-v1.canonical.json
contracts/p1a/fixtures/fixtures.sha256
contracts/p1a/fixtures/proposal-v1.canonical.json
contracts/p1a/fixtures/revision-v1.canonical.json
contracts/p1a/fixtures/state-machine-v1.canonical.json
docs/experiments/p1a-task-proposal-contract.md
docs/plans/2026-07-22-p1a-task-proposal-contract.md
docs/experiment-ledger.md
```

**Gates**
- `node --check contracts/p1a/verify.mjs` — passed.
- `node contracts/p1a/verify.mjs` — passed.
- `node contracts/p1a/verify.mjs --self-test` — passed; a valid canonical Revision content mutation is rejected by the digest gate.
- Local Markdown links in the new contract and plan resolve.

The requested `docs/plans/2026-07-21-task-intake-grill-and-loop-roadmap.md` path is absent from this checkout and available Git history; the implementation used the stated P1a requirements and existing store migration/test conventions. No commit or push was run.
