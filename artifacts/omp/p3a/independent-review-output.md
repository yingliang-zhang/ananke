Working...
## CHANGES REQUESTED

The contract chain passes, but the P3a oracle does not yet freeze several required P3 safety bindings.

### Blocking contract gaps

1. **Approval eligibility is not bound to the P1 Revision.**  
   P1 Approval carries `proposal_id`, `revision`, and `revision_hash` (`docs/experiments/p1a-task-proposal-contract.md:104-115`). P3a’s eligibility fact permits only `approval_id`, time, actor, and state (`contracts/p3a/verify.mjs:155-161`).

   I changed only `approval_eligibility.approval_id` to valid-format `approval_p1a_999`, recomputed the copied manifest, and ran the verifier with its self-test drift mode. **It exited 0.** An approved local fact for a different/unproven Revision can therefore authorize the frozen root tuple.

   **Required:** bind eligibility to the exact P1 tuple/hash and add a negative verifier/self-test vector.

2. **HostSpec lacks a transcript-source binding.**  
   Roadmap P3 requires executable route, **transcript source/dialect**, required files, worktree layout, cancellation/recovery, and verification command (`…roadmap.md:200-212`). P3a closes route/files/layout hashes and capability list (`verify.mjs:163-174`) plus a separately declared dialect/fingerprint (`:203-206`), but no transcript-source fingerprint exists.

   **Required:** add an opaque `transcript_source_fingerprint` or equivalent explicit source binding; never add a raw path.

3. **Recovery vectors assert states, not the exact durable identities they claim.**  
   The contract prose requires “Exact materialization ready” and “Exact materialization and current-token Run created” (`docs/experiments/p3a-fenced-launch-admission-contract.md:98-105`). The fixture/verifier recovery facts contain only `materialization: "ready"` and `run: "created"` alongside the claim and launch-spec hash (`verify.mjs:368-398`).

   There is no `materialization_id`, materialization hash, nonce, `run_id`, Run materialization reference, or created-fact token binding at those boundaries.

   **Required:** model and verify those exact durable identities in the recovery vectors, retaining no-guess terminal/evidence/process outcomes.

4. **Stale-token coverage requires both a lower generation and a different token.**  
   `staleTokenOutcome` requires `fence_generation < current` **and** a non-current token (`verify.mjs:232-243`). Roadmap P3 says stale tokens cannot create a Run, append terminal facts, or settle evidence (`…roadmap.md:204-205`), without that conjunction.

   I changed only a stale vector’s generation to the active generation while retaining its different token. The semantic verifier rejected the fixture with `stale fencing generation`; it cannot express or prove same-generation/wrong-token rejection.

   **Required:** define staleness as any mismatch from the active `(claim_token_hash, fence_generation)` authority and cover both same-generation/different-token and lower-generation cases for all three writes.

5. **`waiting_for_human` outcomes omit the roadmap-required intervention binding.**  
   P3 requires an out-of-envelope request to emit `waiting_for_human` **plus a run/tool-call-bound intervention** (`…roadmap.md:213-215`). P3a’s fail-closed output only carries rejection, no process, no terminal/evidence, and `waiting_for_human` (`verify.mjs:36-42`); the document defers intervention to a later runtime (`docs/experiments/p3a-fenced-launch-admission-contract.md:84-88`).

   **Required:** freeze an abstract run/tool-call intervention reference in the fixture outcome. This need not introduce storage or runtime scope.

### Verified strengths

- Exact P1 root tuple/hash is canonically bound in the launch spec; P2’s `1..100` attempt-cap bound is retained.
- Provider/model, semantic deadline, sealed read-only scope, sealed materialization hash/nonce, HostSpec fingerprints, transcript dialect, and verification-command fingerprint are closed and hashed.
- Claim, materialization, outbox, and Run are distinct fenced objects.
- All ten required fail-closed command/prompt/materialization/budget/scope/dialect/event cases yield rejected `waiting_for_human` with no process, terminal fact, or evidence.
- All three named crash boundaries exist and retain absent terminal/unsettled evidence.
- No SQLite, daemon, Tauri, UI, adapter, OMP, raw command, prompt, or production process implementation was found in the P3a artifacts.

### Scope/documentation note

`contracts/p3a/verify.mjs` imports `node:child_process` and self-test invokes a copied Node verifier via `spawnSync` (`:2`, `:461-465`). That is test-only, not adapter/process scope, but the P3a document’s literal claim that the verifier “invokes no … process” is inaccurate (`docs/experiments/p3a-fenced-launch-admission-contract.md:129-130`). Correct the wording or remove the child-process self-test mechanism.

### Verification run

Passed exactly:

```sh
node --check contracts/p3a/verify.mjs &&
node contracts/p1a/verify.mjs &&
node contracts/p1c/verify.mjs &&
node contracts/p2a/verify.mjs &&
node contracts/p2c/verify.mjs &&
node contracts/p3a/verify.mjs &&
node contracts/p3a/verify.mjs --self-test
```

No files were edited or committed.
