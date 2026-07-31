Working...
## CHANGES REQUESTED

No files edited or commits made.

### Prior blocking findings

| Finding | Result | Evidence |
|---|---|---|
| Immutable Revision state ambiguity | **Closed** | The immutable snapshot has no state; [`RevisionLifecycle`](docs/experiments/p1a-task-proposal-contract.md#L81-L99) is the sole mutable record, keyed by `(proposal_id, revision)`, references both immutable snapshot hash and Approval, and must transition atomically with Approval. The rejected-current withdrawal outcome is explicitly specified at lines 135–143. |
| Request-hash envelope conflation / ordering | **Closed** | The contract defines versioned create/append/decision/withdrawal bodies, hashes the canonical `body` rather than the Revision, freezes ordered scope tuples, and requires idempotency lookup before mutable checks ([lines 148–187](docs/experiments/p1a-task-proposal-contract.md#L148-L187)). A recomputation changing only create `project_id` changed the body hash from `aca360…91c4d` to `34a40b…0ac9`. A consistently rehashed fixture with `create.body_hash = revision_hash` was rejected. |
| RFC 8785 lone-surrogate handling | **Closed** | The contract requires rejecting unpaired key/value surrogates before canonicalization ([lines 189–197](docs/experiments/p1a-task-proposal-contract.md#L189-L197)); `assertNoUnpairedSurrogates` walks values and keys in [`verify.mjs:78-107`](contracts/p1a/verify.mjs#L78-L107). Consistently rehashed high-surrogate-value and low-surrogate-key probes both exited `1` with the expected diagnostic. |
| Partial privacy fixture schemas | **Closed** | Closed object shapes are enforced with `expectKeys` and the exact acceptance matrix comparison; the denylist now includes `repository_root` and the documented field set ([`verify.mjs:21-64`](contracts/p1a/verify.mjs#L21-L64), [`:443-642`](contracts/p1a/verify.mjs#L443-L642)). A consistently rehashed `repository_root` injection in `acceptance.cases[0].given` was rejected. |
| Append / replay / restart / concurrency vectors | **Not closed** | Successful pending append, rejected-predecessor append, rejected withdrawal, create/append/decision restart replay, two-appends, approve-vs-reject, and same-key creation replay were added. But the only append-vs-decision concurrency vector is append vs **approve**: [`verify.mjs:610-626`](contracts/p1a/verify.mjs#L610-L626). There is no append-vs-**reject** vector. |

### Blocking defect: append racing rejection is unspecified and conflicts with the stated concurrency rule

The contract permits an append for an open Proposal with matching revision/hash ([lines 128–133](docs/experiments/p1a-task-proposal-contract.md#L128-L133)), and explicitly permits append after rejection. Rejection leaves the Proposal open ([lines 135–141](docs/experiments/p1a-task-proposal-contract.md#L135-L141)).

Therefore, for an append and rejection submitted against the same pending revision:

1. **Reject linearizes first** → Proposal remains open; append still matches its expected revision/hash and is permitted. Both mutations commit.
2. **Append linearizes first** → old pair is superseded; the later rejection conflicts.

That differs materially from append-vs-approval and is not covered by the single `concurrent_append_decision` vector, which uses `decision_approve`. It also leaves the blanket claim that “exactly one conflicting append or decision commits” ([line 180](docs/experiments/p1a-task-proposal-contract.md#L180)) underdefined for rejection.

Required repair: define the intended append-vs-reject result and add a frozen acceptance vector. If only one operation may commit, append needs an expected lifecycle state/version (or an equivalent conflict condition); the present expected revision/hash alone cannot distinguish a just-rejected current revision.

### New verifier defects

1. **Invalid RFC 3339 timestamps are accepted.**  
   The contract requires canonical UTC RFC 3339/RFC 3339Nano text ([line 30](docs/experiments/p1a-task-proposal-contract.md#L30)), but the verifier uses only a digit-shape regex ([`verify.mjs:68,159-161`](contracts/p1a/verify.mjs#L68-L68)). I consistently rehashed all affected fixture links after changing the Revision timestamp to:

   ```text
   2026-99-99T99:99:99Z
   ```

   Result: exit `0`, `P1a proposal contract fixtures verified.`  
   The verifier needs semantic calendar/time validation, not only lexical matching.

2. **Request-envelope identity is not verified against the resulting records.**  
   Both consistently rehashed probes below exited `0`:

   - Changed `create.body.project_id` to `project_other`, while the Proposal remained `project_p1a`.
   - Changed `decision_approve.body.revision_hash` to `sha256:` + 64 `d` characters, while Approval and RevisionLifecycle retained the actual Revision hash.

   This violates the intended create-target and decision identity relationships, including the contract’s `revision/hash mismatch` conflict requirement ([lines 141–143](docs/experiments/p1a-task-proposal-contract.md#L141-L143)). `verifyRequestEnvelope` validates field shapes and body digests but does not cross-check those bodies against Proposal/Approval/Revision records ([`verify.mjs:354-432`](contracts/p1a/verify.mjs#L354-L432)).

### Scope and authority audit

No new authority or P2/P3 leakage found:

- Only `local_gui_operator` mutates; policy is deterministic; model output is advisory; the adapter is frozen as future/read-only ([lines 9–23](docs/experiments/p1a-task-proposal-contract.md#L9-L23)).
- Explicit non-goals exclude storage, GUI/IPC, claims/workers/adapters, execution, and commits/pushes ([lines 246–255](docs/experiments/p1a-task-proposal-contract.md#L246-L255)).
- Runtime search across `cmd`, `internal`, and `gui` for P1a record/operation identifiers returned no matches.
- `contracts/p1a/` remains fixtures plus the dependency-free Node verifier.

### Verification run

```text
node --check contracts/p1a/verify.mjs
# exit 0

node contracts/p1a/verify.mjs
# P1a proposal contract fixtures verified.

node contracts/p1a/verify.mjs --self-test
# P1a fixture verifier self-test rejected drift, private fields,
# unpaired Unicode surrogates, request-hash conflation, and missing vectors.
```

The repaired contract closes four of the five original blockers, but the missing append-vs-reject semantics plus the accepted invalid timestamp and unlinked request identities prevent acceptance.
