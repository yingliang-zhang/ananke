# P1c task-proposal public protocol implementation plan

**Goal:** freeze and mechanically generate the renderer-public Proposal,
Revision, Approval, Activity, command-input, and result DTO contract.

**Boundary:** P1c is contract/codegen only. The daemon socket `cmd`/`token` and
`ok`/raw-`error` envelope remain private. Do not add a Tauri handler, daemon
handler, GUI, store query, claim, worker, adapter, Grill feature, commit, or
push in this slice.

## Task 1: Freeze canonical public vectors

**Files:** `contracts/p1c/fixtures/protocol-v1.canonical.json`,
`contracts/p1c/fixtures/fixtures.sha256`, `contracts/p1c/verify.mjs`.

1. Define the seven minimum Tauri/daemon command-name pairs:
   create, list, get detail, list activity, append, decide, and withdraw.
2. Freeze closed public inputs and results. Mutation results carry durable
   P1b identity only; a separate detail read observes state.
3. Prove the current-detail links among Proposal, current Revision,
   RevisionLifecycle, and Approval. Keep activity ordered and linked to the
   same proposal/revision/hash/approval identities.
4. Require canonical one-line JCS bytes, a versioned SHA-256 manifest, semantic
   timestamps, P1a policy constants, and P1a identifiers/hashes.
5. TDD the dependency-free verifier. It must reject byte drift before semantic
   parsing, then reject private runtime data and unknown fields after a manifest
   update. It must not connect to SQLite, the daemon, Tauri, a model, or a
   network.

## Task 2: Generate renderer-public DTO types

**Files:** `gui/contracts/renderer-public-proposal-*.schema.json`,
`gui/scripts/generate-renderer-public.mjs`,
`gui/src-tauri/src/generated/**`, `gui/src/generated/**`.

1. Add closed JSON Schema 2020-12 models for seven command inputs and four
   result types: `ProposalMutation`, `ProposalList`, `ProposalDetail`, and
   `ProposalActivityList`.
2. Reuse exact P1a field names and fixed policy values. Use nullable decision
   fields for a pending/superseded/withdrawn Approval; do not make them absent.
3. Extend the existing public generator inventory and generated Rust module;
   do not hand-edit generated output.
4. Preserve existing all-target drift and privacy checks. The generated public
   tree must never contain `token`, raw `error`, path/root/socket/identity,
   worker/process/PID, credential, or model-output fields.

## Task 3: Defer runtime integration

A separately authorized P1d vertical slice must begin with focused RED tests,
then implement only the code needed to satisfy this frozen contract:

1. Add a P1b `ListProposalsByTarget` query returning Proposal summaries in the
   required deterministic order. This API does not currently exist.
2. Add private daemon request/response adapters and handlers for the seven
   hyphenated commands. Keep the daemon envelope (`cmd`, `token`, `ok`, raw
   `error`) private to Go/Rust.
3. Add bridge conversions from generated public input/result types, register
   the seven Tauri commands, and add serialization tests through the real
   bridge. Do not build renderer UI in that slice unless separately authorized.
4. Map store conflict/not-found errors to the existing sanitized public bridge
   error behavior; never serialize a raw daemon error.

## P1c gates

```sh
node contracts/p1c/verify.mjs
node contracts/p1c/verify.mjs --self-test
npm --prefix gui run generate:renderer-public
npm --prefix gui run check:renderer-public
npm --prefix gui run check:renderer-public-privacy
npm --prefix gui run typecheck
```

A controlled generated-file mutation must make `check:renderer-public` fail
before regeneration. No P1c command is considered live until the deferred
bridge/daemon/store integration tests exist and pass.
