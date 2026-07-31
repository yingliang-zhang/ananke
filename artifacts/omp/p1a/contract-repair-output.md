Working...
Completed P1a contract/fixture-only repair. No SQLite, GUI/IPC, claims, workers, adapters, commits, or pushes.

### Changed paths

- `contracts/p1a/verify.mjs`
  - Rejects unpaired high/low Unicode surrogates in all object keys and string values before JCS canonicalization.
  - Enforces closed, context-specific shapes for every fixture object.
  - Adds full private-field denylist coverage, including `repository_root` variants.
  - Validates immutable Revision snapshots separately from composite-key mutable `RevisionLifecycle` records.
  - Validates canonical versioned request envelopes, body hashes, exact ordered scopes, lifecycle links, and all acceptance vectors.
  - Expands self-test to cover drift, privacy fields, surrogate keys/values, request-hash conflation, and missing vectors.

- `contracts/p1a/fixtures/acceptance-v1.canonical.json`
  - Expanded from 6 to 13 vectors, including successful append, append-after-rejection, withdrawal-after-rejection, create/append/decision restart replay after state changes, append-vs-decision, same-key concurrent replay, and zero-partial-write losers.

- `contracts/p1a/fixtures/state-machine-v1.canonical.json`
  - Replaced mutable `revision` state with `revision_lifecycle`; Revision snapshots now have no transitions.

- `contracts/p1a/fixtures/request-envelopes-v1.canonical.json` — new
  - Canonical create, append, approve, reject, and withdrawal envelopes with separate body hashes and precise scope tuples.

- `contracts/p1a/fixtures/revision-lifecycle-v1.canonical.json` — new
  - One-to-one mutable lifecycle record with key `(proposal_id, revision)`, Revision hash, Approval relation, transition version, and timestamps.

- `contracts/p1a/fixtures/fixtures.sha256`
  - Updated manifest for all seven canonical fixtures.

- `docs/experiments/p1a-task-proposal-contract.md`
  - Defines lifecycle record fields/key/relation, atomic transitions, rejected-current withdrawal semantics, versioned envelope bodies/scopes, replay ordering, surrogate rejection, and closed privacy schemas.

- `docs/plans/2026-07-22-p1a-task-proposal-contract.md`
  - Aligns fixture count, verifier obligations, future storage RED cases, and concurrency vectors.

- `docs/experiment-ledger.md`
  - Records only observed RED/GREEN evidence and targeted-probe outcomes.

### Evidence

**RED observed**
- Pre-repair verifier accepted a consistently rehashed `repository_root` injection.
- Pre-repair verifier accepted a consistently rehashed lone surrogate.
- Original create `body_hash` equaled the Revision snapshot hash.
- The initial strict verifier rejected the prior five-file manifest because request-envelope and lifecycle fixtures were absent.

**GREEN**
```text
node --check contracts/p1a/verify.mjs
node contracts/p1a/verify.mjs
# P1a proposal contract fixtures verified.

node contracts/p1a/verify.mjs --self-test
# P1a fixture verifier self-test rejected drift, private fields,
# unpaired Unicode surrogates, request-hash conflation, and missing vectors.
```

**Targeted consistently-rehashed probes rejected**
- Lifecycle/Approval state divergence.
- Reordered operation scope.
- Request-hash conflation.
- Unpaired-surrogate object key.
- `repository_root` injection.
- Missing `concurrent_same_key_replay` vector.
