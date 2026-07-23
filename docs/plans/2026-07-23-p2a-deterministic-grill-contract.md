# P2a deterministic Grill contract / fixture TDD plan

**Goal:** freeze a deterministic, revision-bound Grill review boundary before
any persistence or execution implementation exists.

**Authorized now:** P2a canonical fixtures, dependency-free Node verifier,
contract documentation, plan, and factual ledger entry. **Not authorized:**
SQLite migration/query, GUI, Tauri, daemon, private protocol, claim, worker,
adapter, worktree, model call, approval mutation, command execution, commit,
or push.

## Task 1: Freeze deterministic rule inputs and outcomes

**Files:** `contracts/p2a/fixtures/grill-v1.canonical.json`,
`contracts/p2a/fixtures/acceptance-v1.canonical.json`,
`contracts/p2a/fixtures/fixtures.sha256`.

1. Bind every evaluation and record to the exact P1a tuple
   `(proposal_id, revision, revision_hash)`.
2. Freeze `ananke.grill.rules.v1` with exactly six rules: observable outcome,
   scope/compatibility, acceptance evidence, destructive/external
   authorization, adapter/worktree isolation, and deadline-plus-attempt-cap
   autonomy.
3. Keep declarations closed and structural. Do not add raw Revision prose,
   model text, command, approval, claim, worker, or adapter-output fields.
4. Freeze a stable question priority/risk/blocking/waivable/default/remedial
   tuple for every rule. A rule produces at most one question.
5. Freeze deterministic outcomes: five visible questions per evaluation, at
   most `min(5, 10 - priorQuestionCount)` appended Questions per evaluation,
   ten Questions per Revision before `needs_rewrite`, and zero records on an
   unchanged input/history replay.

**Acceptance:** all fixture bytes are JCS without newline, manifest digests and
hard-coded digests match, and the P1a hash is the exact root revision hash.

## Task 2: Freeze append-only review records

**Files:** `contracts/p2a/fixtures/grill-v1.canonical.json`,
`contracts/p2a/verify.mjs`.

1. Define closed Question, Answer, Default, and Override record versions.
2. Require every record to contain the exact Revision tuple, rule version,
   contiguous append-only `record_sequence`, timestamp, and writer.
3. Require Question records to carry a unique deterministic rule question ID,
   contiguous question sequence, and the fixed rule fields.
4. Permit `waived` only for scope/compatibility. Treat Answer and Override as
   review records only: neither can create approval, authorization, or a
   command.
5. Demonstrate that a scope waiver frees one display slot for the deferred
   autonomy question without modifying the bound Revision.

**Acceptance:** the vector has five initial Questions, one Default, one Answer,
one allowed Override, and one subsequently displayed sixth Question; no record
can be reordered, retargeted, or rewritten.

## Task 3: TDD the canonical, adversarial verifier

**Files:** `contracts/p2a/verify.mjs`,
`contracts/p2a/fixtures/adversarial-v1.canonical.json`.

1. RED: alter a frozen priority and rehash the copied fixture and manifest; the
   verifier must reject hard-pinned digest drift. Waive only that pin in the
   self-test and require rejection by the fixed rule table.
2. GREEN: validate UTF-8/no-BOM/JCS bytes, no unpaired surrogates, manifest and
   hard-pinned digests, closed shapes, timestamps, P1a identity, input hash,
   exact rules, records, display/cap behavior, and idempotent replay.
3. RED/GREEN adversarial vectors must reject injected Revision prose, model
   output, command, approval state, retry loop, and an attempt cap above 100.
   A missing budget must produce only the autonomy question and review-only
   output fields.
4. Keep the verifier fixture-only. It must not install packages, open a
   database, invoke a runtime, call a model, or access the network.
5. RED: consistently rehash a copied acceptance case with nine prior Questions
   that expects five new Questions (fourteen total); with the hard pin waived,
   the evaluator/verifier must reject that overrun.
6. GREEN: the canonical acceptance sequence appends exactly one Question from
   nine to ten, then returns `needs_rewrite` with no append at ten.

**Commands:**

```sh
node --check contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs --self-test
```

## Task 4: Future persistence RED tests — do not implement in P2a

After a separately authorized storage slice begins, write these focused RED
store tests before adding a migration, table, or production API:

1. `EvaluateGrill` on a valid exact Revision tuple creates zero Questions and
   returns review-only `clear`; it must not read raw Revision task text or
   mutate Proposal/Approval state.
2. One all-missing declaration evaluation atomically appends the first five
   priority Questions only. It records the P1a hash verbatim and has no
   command, claim, worker, adapter, or execution side effect.
3. A close/reopen followed by the same canonical input replays the stored
   evaluation with zero rows and byte-identical question IDs/order.
4. A Default, Answer, and the sole allowed Override each append a new row;
   update/delete attempts and an Override against any non-waivable question
   fail without partial writes.
5. A scope waiver followed by reevaluation appends only the deferred autonomy
   Question. Concurrent reevaluations must produce it at most once.
6. At nine pre-existing Questions for one exact Revision, evaluation appends
   exactly one Question to reach ten; the next evaluation at ten returns
   `needs_rewrite`, writes no Question, and leaves all P1 Proposal/Approval
   rows unchanged.
7. Mismatched proposal ID, revision number, revision hash, rule version, or
   input hash fails closed with zero rows. A later Proposal revision has a
   separate record stream.
8. Bounds reject `attempt_cap` outside 1–100 and require both deadline and cap
   before no-question `clear`; no test may use a wall-clock race or sleep.

Follow the existing P1b `internal/store` transaction and canonical-fixture test
conventions. A future implementation may expose store types only after these
RED tests exist. It remains review-only until a separately authorized,
end-to-end approval/execution design exists.

## Final P2a gate

Run all three P2a Node commands above plus `node contracts/p1a/verify.mjs` and
`node contracts/p1c/verify.mjs`; record only their observed RED/GREEN outcomes
in the ledger. Do not claim storage, restart, concurrency, GUI, daemon, model,
adapter, worktree, approval, or execution coverage from a fixture verifier.
