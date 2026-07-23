# P2a deterministic Grill contract

**Status:** frozen contract and fixture oracle.
**Scope:** deterministic, review-only questions bound to an exact P1a Proposal
Revision. This slice adds no store migration or runtime path.

## Decision

P2a freezes `ananke.grill.rules.v1`. It is a closed, deterministic evaluator;
it receives a P1a Revision *identity* plus a closed declaration object, not a
Revision snapshot, task prose, model result, command, approval, claim, worker,
or adapter result.

The identity is exactly `(proposal_id, revision, revision_hash)`. The canonical
fixture binds every P2a record to P1a root Revision
`proposal_p1a_001` / `1` /
`sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263`.
`revision_hash` is never recomputed or replaced by a Grill hash.

A Grill `clear` result only means no Grill question remains for the declared
input. It never approves a Proposal, authorizes an external action, creates an
execution command, or starts an attempt. P1a Approval remains a separate,
local-operator state machine.

## Closed evaluation input

`ananke.grill.input.v1` has only the exact Revision identity and these closed
`declarations`:

| Field | Allowed values / invariant |
| --- | --- |
| `observable_outcome`, `scope_compatibility`, `acceptance_evidence` | `absent` or `declared` |
| `destructive_external` | `none` or `declared` |
| `local_authorization` | `not_required`, `recorded`, or `unrecorded`; it is a review declaration, not P1 Approval |
| `adapter_mode` | `none` or `read_only` |
| `worktree_isolation` | `not_applicable`, `isolated`, or `not_isolated`; it is `not_applicable` when adapter mode is `none` |
| `autonomy.deadline` | `null` or a semantic UTC RFC 3339 timestamp |
| `autonomy.attempt_cap` | `null` or integer 1 through 100 |

No current-clock comparison occurs in v1, so the result is repeatable. A
missing deadline or attempt cap raises a question; it does not create a retry
loop. Raw Revision `task` prose and all model content are deliberately absent
from the input schema.

## Exactly six rule classes

Each rule may yield at most one question. `priority` is ascending and is the
only display-order tie breaker; the table is part of the frozen rule version.

| Priority | Rule class | Trigger | Risk | Blocking | Waivable | Default | Remedial step |
| ---: | --- | --- | --- | --- | --- | --- | --- |
| 10 | `observable_outcome` | outcome is not declared | `high` | yes | no | `needs_rewrite` | `declare_observable_outcome` |
| 20 | `scope_compatibility` | scope/compatibility is not declared | `medium` | yes | yes | `needs_rewrite` | `declare_scope_compatibility` |
| 30 | `acceptance_evidence` | evidence is not declared | `high` | yes | no | `needs_rewrite` | `declare_acceptance_evidence` |
| 40 | `destructive_external_authorization` | destructive/external work is declared without a recorded declaration | `critical` | yes | no | `deny` | `record_local_authorization` |
| 50 | `adapter_worktree_isolation` | a read-only adapter lacks isolated-worktree declaration | `high` | yes | no | `needs_rewrite` | `require_isolated_worktree` |
| 60 | `autonomy_budget` | deadline or attempt cap is missing | `high` | yes | no | `needs_rewrite` | `set_deadline_attempt_cap` |

An override is permitted only for the waivable scope/compatibility question.
It changes only Grill classification. It does not alter the immutable Revision,
create a P1 Approval, authorize a command, or permit destructive/external
work.

## Evaluation bounds and idempotency

An evaluation is keyed by the exact Revision identity, `rule_version`, and the
SHA-256 hash of the canonical Grill input. With the same input and the same
append-only record history, re-evaluation returns the same shown questions and
writes zero records.

1. Sort active questions by the fixed priority table.
2. Show at most five. Remaining active rule classes are deferred, not silently
   dropped.
3. Append at most `min(5, 10 - priorQuestionCount)` new Questions; this bound applies before an append and is independent of the display bound.
4. A waivable override can remove only its target question from the active
   display set; the next priority question may then be shown.
5. If the Revision already has ten Question records, append none and return
   `needs_rewrite`.
6. With no active questions return `clear`; `clear` is review-only.

The canonical vector triggers all six classes, persists the first five,
records a default, an answer, and the one permitted override, then shows the
sixth class. Its unchanged-input replay has zero new records. The canonical
acceptance sequence then starts at nine prior Questions, appends only one to
reach ten, and returns `needs_rewrite` with no append on the next evaluation.

## Append-only records

Every Question, Answer, Default, and Override record carries the exact Revision
identity, `rule_version`, a monotonic contiguous `record_sequence`, UTC write
timestamp, and writer. Question records additionally carry their deterministic
`question_id`, contiguous `question_sequence`, and the immutable rule fields.

| Record version | Writer | Closed mutable meaning |
| --- | --- | --- |
| `ananke.grill.question.v1` | `deterministic_grill` | one generated rule question |
| `ananke.grill.default.v1` | `deterministic_grill` | materialized rule default |
| `ananke.grill.answer.v1` | `local_gui_operator` | acknowledgement only; it does not approve or execute |
| `ananke.grill.override.v1` | `local_gui_operator` | `waived`, only for `scope_compatibility` |

A future store implementation MUST make these rows insert-only, reject a
mismatched Revision tuple/hash, retain records after a later Proposal revision,
and enforce record and question sequence uniqueness atomically.

## Adversarial boundary

`adversarial-v1.canonical.json` proves that raw Revision prose, model content,
commands, approval state, and unbounded retry policy are rejected as unknown
Grill-input properties. It also rejects `attempt_cap: 101`; a missing budget
returns the single blocking autonomy question and a review-only output shape.
The verifier has no API client, database, GUI, daemon, model, network, claim,
worker, adapter, or shell-execution dependency.

## Artifacts and gates

- `contracts/p2a/fixtures/grill-v1.canonical.json` freezes rules, identity,
  input hashing, bounded evaluation vectors, and append-only records.
- `contracts/p2a/fixtures/adversarial-v1.canonical.json` freezes injection and
  bounded-autonomy vectors.
- `contracts/p2a/fixtures/acceptance-v1.canonical.json` freezes clear, five of
  six, idempotent replay, waiver-slot, nine-to-ten capacity, and ten-question
  rewrite outcomes.
- `contracts/p2a/fixtures/fixtures.sha256` and hard-coded verifier digests
  detect byte drift.
- `contracts/p2a/verify.mjs` validates JCS bytes, fixture hashes, exact closed
  shapes, P1a Revision binding, all six rules, append-only records, bounds,
  replay, and adversarial cases.

```sh
node contracts/p2a/verify.mjs
node contracts/p2a/verify.mjs --self-test
```

## Explicit non-goals

No GUI, Tauri command, daemon handler, private protocol, store migration,
claim, worker, adapter runtime, worktree creation, model call, approval
mutation, command construction, execution, commit, or push is authorized by
P2a.
