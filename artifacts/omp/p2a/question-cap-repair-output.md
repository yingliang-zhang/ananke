Working...
Repaired P2a ten-question cap within contract artifacts only.

- `evaluate()` now bounds new questions with `min(5, 10 - priorQuestionCount)` before the display-slot bound; `priorQuestionCount >= 10` returns `needs_rewrite` with no append.
- Added canonical acceptance boundary:
  - `prior_question_count: 9` appends only `observable_outcome`, reaching total 10.
  - next `prior_question_count: 10` evaluation returns `needs_rewrite` with no new/shown/deferred questions.
- Rehashed `acceptance-v1.canonical.json`; manifest and hard-coded digest now pin:
  `25ca379f62be62ca90928d7f211ce8678316eb7b84640aa931858b8623207000`.
- Extended self-test: a consistently rehashed copied acceptance fixture that requests five new questions from nine prior Questions (14 total) is rejected.
- Aligned contract documentation, TDD plan, and experiment ledger with the review RED and repair GREEN evidence.

Verification passed:

```sh
node --check contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs --self-test
node contracts/p1a/verify.mjs
node contracts/p1c/verify.mjs
```

The P2a self-test reports rejection of the rehashed ten-question-cap overrun. No runtime, UI, daemon, store, model, or commit artifact was changed.
