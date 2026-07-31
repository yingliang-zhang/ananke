Working...
## CHANGES REQUESTED

### Blocker — the ten-question cap is not enforced before appending

`evaluate()` returns `needs_rewrite` only when `priorQuestionCount >= 10`, then selects up to five new questions without remaining-capacity logic:

- Contract: `docs/experiments/p2a-deterministic-grill-contract.md:71-78`
- Implementation: `contracts/p2a/verify.mjs:211-220`

I executed the existing evaluator through a temporary, non-repository probe:

```json
{
  "prior_question_count": 9,
  "new_question_count": 5,
  "resulting_question_count": 14,
  "shown_question_count": 5,
  "status": "blocked"
}
```

This violates the requested invariant: **maximum 10 Question records per Revision, then `needs_rewrite`**. The acceptance fixture covers counts `0`, `5`, and `10`, but not the boundary at `9`.

**Required remediation**

1. Limit generated questions by remaining record capacity, e.g. also bound selection by `10 - priorQuestionCount`.
2. Add a canonical acceptance case at `prior_question_count: 9` that proves no more than one Question is appended; the next evaluation at ten returns `needs_rewrite`.
3. Extend the self-test to reject a fixture/evaluator outcome that exceeds ten Question records.

### Confirmed compliant

- Exactly six frozen rule classes with stable priorities, risks, blocking flags, defaults, waiver policy, and remediation steps.
- All fixture evaluation and record identities bind the exact P1a tuple, including the frozen root hash `sha256:114a…70263`; `node contracts/p1a/verify.mjs` passed.
- Record vector enforces contiguous append-only `record_sequence`, contiguous question sequence, fixed question IDs/rule fields, and only the scope/compatibility waiver.
- Five-question display bound, deferred sixth question, deterministic same-input replay (`new_records: 0`), and waiver slot release are covered.
- Deadline and attempt cap are bounded (`1..100`); missing budget returns only the blocking review question.
- Closed input shapes reject raw Revision prose, model output, command, approval state, and retry policy. `clear` remains review-only.
- P1b’s documented authority remains SQLite/store only; P1c remains public DTO/codegen only. No Grill references exist under `cmd`, `internal`, `gui/src`, or `gui/src-tauri`.
- No repository files were edited and no commit was made.

### Verification run

Passed:

```sh
node --check contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs --self-test
node contracts/p1a/verify.mjs
node contracts/p1c/verify.mjs
```

The P2a self-test correctly rejected frozen-rule drift, command/approval injection, unbounded attempt caps, and question-sequence tampering; it does not exercise the nine-to-ten cap boundary.
