# P3a fenced launch-admission contract / fixture TDD plan

**Goal:** freeze a P1/P2-bound launch-admission boundary before any adapter,
claim, process, or persistence implementation exists.

**Authorized now:** canonical P3a fixtures and manifest, a dependency-free Node
verifier and self-test, this TDD plan, the P3a contract document, and a factual
ledger entry. **Not authorized:** SQLite or any schema/migration work; runtime,
daemon, Tauri, UI, claim/lease, worktree, adapter, OMP, process, command,
prompt, transcript, evidence, or verification-command execution; commit or
push.

## Task 1: RED/GREEN — immutable admission spec

**Files:** `contracts/p3a/fixtures/launch-admission-v1.canonical.json`,
`contracts/p3a/verify.mjs`.

1. Bind the spec to exactly P1 root tuple
   `(proposal_p1a_001, 1,
   sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263)`.
2. Require a separate approval-eligibility fact with `state: "approved"`,
   `approved_by: "local_gui_operator"`, and the same exact P1 root tuple/hash;
   a valid-format Approval ID or tuple for another Revision must not authorize
   this launch. Do not treat a Grill result or model output as approval.
3. Freeze provider and model, an RFC 3339 deadline, attempt cap `1..100`, and a
   read-only, sealed-contract-only retrieval scope. `writes` is exactly
   `"forbidden"`.
4. Freeze only sealed-contract `materialization_hash` and opaque nonce, never
   its task text. Hash the canonical launch-spec object and bind all later facts
   to that hash.
5. Freeze `HostSpec` fingerprints for executable route, transcript source,
   required files, worktree layout, and the closed host declaration plus a
   capability list. Define `host_spec_fingerprint` as the JCS SHA-256 of the
   HostSpec excluding itself. Never carry a raw path. Freeze a shape-only
   transcript dialect/fingerprint and a read-only verification **command
   fingerprint**, never raw argv, shell text, or an execution request.
6. RED: consistently rehash a copied launch fixture after changing the cap; the
   normal verifier must reject the hard digest. GREEN: the fixture validates
   only when all closed launch-spec fields and its canonical hash agree.

**Acceptance:** the immutable spec contains every P3a admission input and no
raw prompt, command, argv, task prose, instructions, shell, script, or
execution field.

## Task 2: RED/GREEN — independent fenced projections

**Files:** `contracts/p3a/fixtures/launch-admission-v1.canonical.json`,
`contracts/p3a/verify.mjs`.

1. Freeze distinct `task_claim`, `materialization`, `launch_outbox`, and `Run`
   objects. Each independently records the exact launch-spec hash, claim ID,
   claim-token hash, and monotonic fence generation; no object derives mutable
   approval or process state from another.
2. Make materialization repeat the sealed hash and nonce, the outbox express
   only its durable admission stage, and the Run contain an append-only initial
   `created` state fact owned by the current token.
3. Define a stale attempt as **any** mismatch from the active
   `(claim_token_hash, fence_generation)` tuple. Freeze both a
   same-generation/different-token and a lower-generation vector for each of
   `create_run`, `append_terminal_fact`, and `settle_evidence`. All six return
   `rejected_stale_token`, create no Run, and write neither a terminal fact nor
   evidence.
4. RED: replace either stale vector's values with the active tuple. GREEN: the
   verifier rejects the attempted stale-authority impersonation.

**Acceptance:** token ownership is explicit and stale ownership cannot become a
new Run, a terminal state fact, or settled evidence in the fixture oracle.

## Task 3: RED/GREEN — fail-closed input and recovery boundaries

**Files:** `contracts/p3a/fixtures/adversarial-v1.canonical.json`,
`contracts/p3a/fixtures/recovery-v1.canonical.json`,
`contracts/p3a/verify.mjs`.

1. Freeze reject-plus-`waiting_for_human` outcomes for unknown raw `command` or
   `prompt` inputs, an unverified materialization hash, missing provider/model/
   deadline/attempt cap, a non-read-only scope, an unknown transcript dialect,
   and an unknown transcript event shape. Every result has no started process,
   terminal fact, or evidence and an abstract canonical Run/tool-call
   intervention reference.
2. Require `shape_only` transcript parsing. Do not add a role field or invent a
   successful event from an unknown dialect or event shape.
3. Freeze three recovery vectors: claim before materialization records exact
   absent identity fields; materialization before Run records exact
   materialization ID/hash/nonce and absent Run identity fields; Run before
   process start records that materialization plus exact Run ID,
   materialization reference, and active-token `created` fact. Every vector
   preserves `terminal_fact: "absent"` and `evidence_state: "unsettled"`.
4. RED: swap materialization or Run identities, intervention references, or a
   current-token `created` fact; then guess terminal, evidence, or process
   state. GREEN: the verifier rejects every rehashed copied fixture.

**Acceptance:** recovery has one bounded action per durable boundary and never
creates terminal evidence or process facts by inference.

## Task 4: Future runtime RED tests — document only

A separately authorized implementation must write and demonstrate these focused
RED tests before adding a store schema, daemon endpoint, adapter, worktree, or
process code:

1. Valid exact P1 tuple, exact local approved eligibility tuple, sealed
   hash/nonce, provider/model, deadline, cap, scope, HostSpec (including
   transcript-source fingerprint), transcript dialect, and verification
   fingerprint atomically create a fenced claim/outbox but execute nothing.
2. Any mismatch in P1 hash, approval eligibility tuple, provider/model,
   deadline, cap, read-only scope, HostSpec fingerprint/capability/transcript
   source, dialect, verification fingerprint, materialization hash, or nonce
   leaves all projection facts unchanged and writes the canonical bounded
   `waiting_for_human` Run/tool-call intervention reference.
3. Materialization verifies the sealed hash and nonce after every rename/open
   boundary against a trusted canonical worktree identity. It accepts no lexical
   prefix check and no mutable memory tool.
4. Concurrent claimholders with any non-active token/fence tuple cannot create
   a Run, append a terminal fact, or settle evidence. A current tuple can write
   each fenced fact once according to its durable generation.
5. Restart at claim→materialization, materialization→Run, and Run→process
   produces exactly the fixture recovery action and durable identities. It does
   not infer materialization/Run identity, process start, completion, evidence,
   or a new token.
6. Unknown transcript dialect/event or an envelope outside the launch scope
   yields `waiting_for_human`, retains the frozen run/tool-call intervention,
   and cannot be authorized by the launch approval.
7. Adapter launch/monitor/cancel/recover behavior is separately proved against
   a controlled OMP read-only audit, including timeout, cancellation, reconnect,
   and cleanup. This P3a fixture contract is not that proof.

## Contract-only gate

Run no build, runtime, database, GUI, daemon, adapter, browser, or OMP command:

```sh
node --check contracts/p3a/verify.mjs
node contracts/p1a/verify.mjs
node contracts/p1c/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2c/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test
```

Only `--self-test` may spawn a copied verifier over copied fixtures. It never
launches an adapter or a contract-defined process.

## Explicit non-goals

- SQLite/schema/migration/query work, any storage or restart implementation;
- daemon protocol/handler, Tauri command, UI, renderer DTO, or generated code;
- a claim/lease service, worktree creation/opening, adapter interface, HostSpec
  loader, OMP invocation, transcript ingestion, process launch/monitor/cancel,
  verification-command execution, or evidence settlement;
- policy execution, prompt or command construction, model calls, mutable memory
  access, approval mutation, timeout or attempt-counter enforcement;
- commit or push.
