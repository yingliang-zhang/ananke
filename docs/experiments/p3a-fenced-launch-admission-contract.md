# P3a fenced launch-admission contract

**Status:** frozen contract and fixture oracle.
**Scope:** immutable admission and recovery facts only. This slice implements no
store, daemon, claim/lease, adapter, worktree, OMP, or process behavior.

## Decision

P3a introduces an immutable `ananke.launch-spec.v1` derived from a P1 Revision
identity, not from P1 Revision prose. It binds a proposed bounded read-only
launch to an independently verified local approval eligibility fact. Approval,
claim ownership, materialization readiness, launch outbox state, Run state, and
evidence are deliberately different facts. A valid approval does not authorize
an input outside the frozen launch envelope.

The canonical vector binds the exact P1 root identity:

```text
proposal_id:   proposal_p1a_001
revision:      1
revision_hash: sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263
```

It retains P1’s `local_gui_operator` as the only eligibility actor and P2a’s
`1..100` attempt-cap bound. Grill `clear`, a question answer/default/override,
model content, and Revision task prose are not approval eligibility and do not
appear in the launch spec.

## Immutable launch spec

The canonical `launch_spec` is closed and canonically SHA-256 hashed. The
admission record stores that `launch_spec_hash`; every later P3a fact binds it
verbatim.

| Closed field | Frozen invariant |
| --- | --- |
| `revision` | Exact P1 tuple and hash above. |
| `model.provider`, `model.model` | Explicit opaque identifiers; no model prompt, output, or policy authority. |
| `deadline`, `attempt_cap` | Semantic UTC deadline and integer cap `1..100`; no wall-clock check or retry loop occurs in this contract. |
| `read_only_scope` | `access: "read_only"`, `retrieval: "sealed_contract_only"`, and `writes: "forbidden"`, with a scope fingerprint. |
| `sealed_contract` | Opaque materialization SHA-256 hash plus nonce only; no contract bytes, task prose, or mutable memory tool. |
| `host_spec` | Opaque fingerprints for HostSpec, executable route, transcript source, required files, and worktree layout; `host_spec_fingerprint` is the SHA-256 JCS hash of that closed HostSpec excluding itself. Fixed capabilities cover bounded cancellation, reconnect recovery, read-only retrieval, shape-only transcripts, and verification. No raw path is carried. |
| `transcript` | `omp_shape_v1`, its fingerprint, and `parse: "shape_only"`; role labels have no authority. |
| `verification` | Read-only verification kind and a verification-command fingerprint. The fingerprint is a binding, not argv, a shell command, or permission to execute. |

`approval_eligibility` is outside the launch spec so it stays a separately
verified fact. It has approval ID, the exact P1 `proposal_id`, `revision`, and
`revision_hash`, UTC approval time, local actor, and `state: "approved"`. The
fixture does not copy a mutable Approval record or invent a second approval
state machine; a valid-format Approval for any other P1 tuple is rejected.

## Fenced projections and token ownership

P3a freezes distinct closed objects, each linked to the same canonical
launch-spec hash:

| Projection/fact | Fenced content |
| --- | --- |
| `task_claim` | Claim ID, opaque token hash, positive fence generation, owner, attempt, and active state. |
| `materialization` | Claim fence plus materialization ID/state and the exact sealed materialization hash/nonce. It carries no bytes. |
| `launch_outbox` | Claim fence plus the durable pending admission stage. It contains no executable command. |
| `Run` | Claim fence, materialization ID, attempt, and initial append-only `created` state fact owned by the current token. |

The opaque token is never fixture data. Only its SHA-256 hash is frozen. An
attempt is stale when **either** its token hash or fence generation differs
from the active `(claim_token_hash, fence_generation)` tuple. Each of
same-generation/different-token and lower-generation vectors for `create_run`,
`append_terminal_fact`, and `settle_evidence` deterministically produces
`rejected_stale_token`, `run_created: false`, `terminal_fact_written: false`,
and `evidence_written: false`.

This is a write-authority rule, not an implementation of a lease, process, or
evidence store. A current token is not proof that a process was launched, a
terminal state occurred, or evidence is valid.

## Fail-closed admission and transcript handling

The adversarial fixture requires a rejected admission and a bounded
`waiting_for_human` state fact for each of these inputs:

- unknown raw `command` and `prompt` field names;
- a materialization hash that differs from the sealed launch-spec hash;
- missing provider, model, deadline, or attempt cap;
- non-read-only scope access;
- unknown transcript dialect and unrecognized transcript event shape.

Every listed outcome has `process_started: false`, no terminal fact, no
evidence, and a frozen abstract intervention reference
`(run_id: run_p3a_001, tool_call_id: tool_call_p3a_001)`. The reference binds
the bounded `waiting_for_human` state fact to the canonical Run/tool call; it
does not define storage, a process, or a tool execution. Parsing is by the
frozen event shape/dialect declaration, never by role. An unknown dialect or
event cannot infer success, completion, or a terminal result.

## Recovery vectors

The recovery fixture describes only durable facts known at a crash boundary and
the sole safe next obligation. It never guesses a process state, terminal fact,
or evidence result.

| Boundary | Durable facts | Recovery action | Facts never inferred |
| --- | --- | --- | --- |
| claim → materialization | Active current claim; materialization and Run facts are explicitly absent with all identity fields `null`; outbox pending materialization; process not created | `retry_materialization` | materialization identity, terminal fact, evidence, Run, process start |
| materialization → Run | Exact ready materialization ID/hash/nonce; Run identity fields explicitly absent; outbox pending Run admission; process not created | `retry_run_admission` | terminal fact, evidence, Run creation result, process start |
| Run → process | Exact ready materialization ID/hash/nonce; exact current-token Run ID, materialization reference, and `created` fact; outbox pending process admission; process not started | `retry_process_admission` | terminal fact, evidence, process start/completion |

All vectors retain `terminal_fact: "absent"` and `evidence_state: "unsettled"`.
A future recovery implementation must retry or close that exact durable
obligation after authenticating ownership; it must not infer an outcome from a
PID, path, role, transcript, or stale token.

## Canonical artifacts and gate

- `contracts/p3a/fixtures/launch-admission-v1.canonical.json` freezes the
  immutable launch spec, approval eligibility, the four separated projections,
  and stale-token denial vectors.
- `contracts/p3a/fixtures/adversarial-v1.canonical.json` freezes reject plus
  `waiting_for_human` behavior for invalid envelope and transcript inputs.
- `contracts/p3a/fixtures/recovery-v1.canonical.json` freezes the three crash
  boundaries and no-guess recovery actions.
- `contracts/p3a/fixtures/fixtures.sha256` plus hard-pinned digests freeze the
  exact JCS bytes.
- `contracts/p3a/verify.mjs` is dependency-free Node code. It validates UTF-8,
  no BOM/unpaired surrogates, JCS bytes, manifest/hard digests, closed shapes,
  P1/P2 links, canonical launch-spec and HostSpec hashes, raw-authority
  exclusion, tuple-mismatch token fencing, run/tool-call intervention binding,
  fail-closed vectors, and exact-identity recovery vectors. Its self-test
  mutates copied fixtures, rehashes their manifest, and proves those bounds are
  rejected.

```sh
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test
```

The normal verifier opens no database or network connection and invokes no
runtime, daemon, adapter, OMP, shell, verification command, model, or process
launch. Only `--self-test` may spawn a copied Node verifier over isolated copied
fixtures; it never launches an adapter or a contract-defined process.

## Explicit non-goals

- SQLite/schema/migration/query, runtime state persistence, daemon protocols or
  handlers, Tauri commands, UI, public DTOs, and generated code;
- claim/lease implementation, worktree identity/opening/materialization,
  HostSpec configuration loading, adapter implementation, OMP invocation,
  transcript ingestion, process launch/monitor/cancel, or evidence settlement;
- raw prompt/command/prose carriage, model calls/output, mutable memory,
  command construction/execution, verification-command execution, policy
  execution, approval mutation, deadline timer, or attempt-counter enforcement;
- commit or push.
