# P1a Proposal / Revision / Approval contract

**Status:** frozen P1a contract and fixture oracle; no runtime implementation.
**Scope:** logical durable-record contract only. It authorizes neither a SQLite
migration nor a GUI, daemon, worker, adapter, claim, commit, or push.

## Decision

P1a freezes a small local-operator approval boundary before any task can become
executable:

- Only `local_gui_operator` creates a proposal, creates a revision, or makes an
  approval decision.
- Deterministic policy is authoritative. Model output is advisory-only and is
  neither an actor nor an input to a state transition.
- The first future budget shape is **deadline + attempt cap**. Cost, token,
  time-per-tool, and concurrency budgets are not P1a fields.
- The first future adapter shape is a **read-only OMP audit**. It cannot claim,
  mutate, launch, or approve.

Those future shapes are frozen in `Revision.policy` below so a later feature
cannot silently substitute a more permissive default. `status: "future"` means
P1a evaluates none of them.

## Closed record shapes

Every listed field is required. Unknown fields are rejected at every nesting
level. Identifiers match `^[a-z][a-z0-9_]{2,63}$`; idempotency keys match
`^[a-z][a-z0-9_]{2,127}$`; hashes are `sha256:` plus 64 lowercase hex digits.
Timestamps are semantic UTC RFC 3339/RFC3339Nano values in
`YYYY-MM-DDTHH:MM:SS[.fffffffff]Z` form: uppercase `T`/`Z`, optional one-to-nine
fractional digits, and a valid Gregorian calendar date and `00:00:00`–`23:59:59` time.

### Proposal

| Field | Type / invariant |
| --- | --- |
| `proposal_id` | Immutable proposal identifier. |
| `project_id`, `workstream_id` | Immutable logical target identifiers; never paths. |
| `created_at` | Proposal creation timestamp. |
| `created_by` | Exactly `local_gui_operator`. |
| `state` | `open`, `approved`, or `withdrawn`. |
| `current_revision` | Positive integer. |
| `current_revision_hash` | Hash of that immutable revision snapshot. |

A Proposal does not embed revisions, approvals, policy output, model output, or
execution data. A record remains addressable after it becomes terminal.

### Revision snapshot

A Revision snapshot is the exact canonical JSON object that is hashed. Its hash
is a derived durable value named `revision_hash`; it is deliberately **not**
embedded in the object being hashed. It has no mutable state field: all
lifecycle state is held by the separate `RevisionLifecycle` record below.

| Field | Type / invariant |
| --- | --- |
| `schema_version` | Exactly `ananke.proposal-revision.v1`. |
| `proposal_id` | References the Proposal. |
| `revision` | Positive, contiguous integer starting at `1`. |
| `parent_revision`, `parent_revision_hash` | Both `null` for revision 1; otherwise immediate predecessor number and hash. |
| `created_at`, `created_by` | Creation timestamp; actor exactly `local_gui_operator`. |
| `idempotency_key` | The proposal-create or append-revision mutation key. |
| `task.title` | Secret-free task label, 1–160 UTF-8 bytes. |
| `task.instructions` | Secret-free operator task text, 1–8,000 UTF-8 bytes. |
| `acceptance_criteria` | Ordered 1–32 secret-free strings, each 1–1,000 UTF-8 bytes. |
| `policy` | Exact object defined below. |

`policy` has exactly this shape and values:

```json
{
  "adapter": { "access": "read_only", "kind": "omp_audit", "status": "future" },
  "authority": "deterministic",
  "budget": { "dimensions": ["deadline", "attempt_cap"], "status": "future" },
  "model_role": "advisory_only"
}
```

There are no budget amounts, deadline values, adapter parameters, audit results,
model/provider names, prompts, completions, or execution commands in v1.

### Revision lifecycle

`RevisionLifecycle` is the sole mutable state record for a Revision snapshot.
Its durable primary key is the ordered tuple `(proposal_id, revision)`; the
tuple is unique, and `revision_hash` and `approval_id` each identify exactly
that tuple. It has exactly these fields:

| Field | Type / invariant |
| --- | --- |
| `proposal_id`, `revision` | Composite key; references the immutable snapshot. |
| `revision_hash` | Hash of that snapshot; immutable after creation. |
| `approval_id` | The one Approval paired with this Revision; immutable after creation. |
| `state` | `pending`, `approved`, `rejected`, `superseded`, or `withdrawn`. |
| `created_at`, `updated_at` | Canonical UTC creation and latest-transition timestamps. |
| `version` | Positive monotonic transition version, starting at `1`. |

The lifecycle record and its paired Approval always have the same state at the
end of an atomic mutation. The snapshot remains byte- and hash-stable while
the lifecycle record changes.


### Approval

| Field | Type / invariant |
| --- | --- |
| `approval_id` | Immutable approval identifier; exactly one Approval is created for each Revision. |
| `proposal_id`, `revision`, `revision_hash` | Immutable reference to the approved snapshot and its lifecycle record. |
| `created_at`, `created_by` | Approval-request timestamp; actor exactly `local_gui_operator`. |
| `state` | `pending`, `approved`, `rejected`, `superseded`, or `withdrawn`. |
| `decided_at`, `decided_by`, `decision_idempotency_key`, `reason` | Nonempty canonical decision values only for `approved` or `rejected`; all are `null` for `pending`, `superseded`, or `withdrawn`. |

`reason` is secret-free operator text limited to 1–1,000 UTF-8 bytes. An
Approval never points at a mutable revision or a model recommendation.

## State and atomicity rules

The machine-readable transition table is
[`state-machine-v1.canonical.json`](../../contracts/p1a/fixtures/state-machine-v1.canonical.json).
It is normative:

| Record | Legal transitions |
| --- | --- |
| Proposal | `open → approved` or `open → withdrawn`; both terminal states have no exits. |
| Revision snapshot | None. It is immutable. |
| Revision lifecycle | `pending → approved`, `rejected`, `superseded`, or `withdrawn`; all four terminal states have no exits. |
| Approval | Identical to Revision lifecycle. |

An append is permitted only for an `open` Proposal and only when its expected
current revision and hash match. In one atomic mutation it creates the next
immutable Revision snapshot with an immediate parent link, its `pending`
RevisionLifecycle, its `pending` Approval, and the Proposal's new current
pointer. A pending former lifecycle/Approval pair becomes `superseded` in that
same mutation. A rejected former pair remains `rejected`.

The append request deliberately has no expected lifecycle state or version. A
rejected current pair therefore remains an allowed append base while its
Proposal is `open`; that later append creates a new pending pair and leaves the
rejected predecessor untouched.

Approving a pending Approval atomically approves its paired RevisionLifecycle
and Proposal. Rejecting it atomically rejects only that Approval and
RevisionLifecycle; the Proposal remains `open`. Withdrawing an open Proposal
with a pending current pair atomically withdraws the Proposal, Approval, and
RevisionLifecycle. Withdrawing an open Proposal whose current pair is rejected
atomically withdraws only the Proposal; that rejected Approval and lifecycle
record remain rejected. Any unsupported transition, stale expected base,
revision/hash mismatch, noncontiguous revision, or second Approval is a
conflict with no partial write.

The frozen one-to-one relation, transition version, and approved example are
in [`revision-lifecycle-v1.canonical.json`](../../contracts/p1a/fixtures/revision-lifecycle-v1.canonical.json).

## Idempotency, restart, and concurrency

Every mutation carries a canonical versioned request envelope with exactly
`schema_version`, `operation`, `scope`, `idempotency_key`, `body`, and
`body_hash`. `body_hash` is SHA-256 of the exact RFC 8785 bytes of `body`; it
is never a Revision snapshot hash. The v1 envelopes freeze these body shapes:

- create: `project_id`, `workstream_id`, and `revision_input`;
- append: `proposal_id`, `expected_current_revision`,
  `expected_current_revision_hash`, and `revision_input`;
- decision: `approval_id`, `proposal_id`, `revision`, `revision_hash`,
  `decision`, and `reason`;
- withdrawal: `proposal_id`.

The canonical envelopes are cross-checked against the golden records: create
targets and `revision_input` match the Proposal and immutable Revision; append
targets, revision number, and hash match the Proposal, Revision,
RevisionLifecycle, and Approval; both decision requests target that same tuple;
the approved decision and its idempotency key/reason match the Approval; and
withdraw targets the Proposal. The rejected decision is the frozen competing
candidate, so its exact `rejected` value and reason are independently fixed;
the golden Approval records the approved winner.

The durable idempotency key is the ordered operation-scope tuple followed by
the caller key: `(actor, operation, resource, idempotency_key)`. Its exact v1
scope values are `("local_gui_operator", "create_proposal",
"proposal_collection")`, `("local_gui_operator", "append_revision",
proposal_id)`, `("local_gui_operator", "decide_approval", approval_id)`,
and `("local_gui_operator", "withdraw_proposal", proposal_id)`. The durable
record stores the request body hash and exact response identity.

For every mutation, the transaction first looks up that durable scope/key
record and compares its request hash **before** checking mutable Proposal,
Approval, or expected-base state. The same scope/key/hash replays the original
identity with no new write, including after restart and after later state
changes. The same scope/key with a different hash returns
`idempotency_conflict` with no write. Only a request without a durable match
may run mutable checks: a stale append returns `revision_conflict`, and a
competing terminal decision returns `approval_conflict`, both with no partial
write.

Concurrent mutations are linearizable per Proposal. Two appends from the same
base, two terminal decisions, and append versus approval each allow exactly one
commit; their losing request has no durable or partial write. Append versus
rejection is intentionally different because rejection leaves the Proposal
open: if append linearizes first, rejection returns `approval_conflict` with no
write; if rejection linearizes first, both commits, with the rejected
predecessor retained and the new current pair pending. The latter exact
two-commit state and the former one-commit state are frozen in the acceptance
matrix, each with `partial_writes: 0`. Same-key/same-body callers replay the
original identity. The storage technology and lock primitive are not selected
by P1a. The canonical envelopes and the normative replay, restart, append,
withdrawal, and concurrency examples are
[`request-envelopes-v1.canonical.json`](../../contracts/p1a/fixtures/request-envelopes-v1.canonical.json)
and [`acceptance-v1.canonical.json`](../../contracts/p1a/fixtures/acceptance-v1.canonical.json).
They specify outcomes, not SQLite statements or a GUI API.

## Canonical bytes and hash

`ananke-proposal-canonical-json-v1` means RFC 8785 JSON Canonicalization Scheme
(JCS): one UTF-8 JSON object, no BOM, no trailing newline or whitespace,
recursively sorted object keys by ECMAScript UTF-16 code-unit order, JCS string
escaping, and ECMAScript/JCS number formatting. Before canonicalization, v1
rejects every unpaired high or low Unicode surrogate in an object key or string
value. Parsing then reserializing an arbitrary JSON document is not permission
to normalize an invalid record.

`revision_hash` is `sha256:` followed by the lowercase SHA-256 digest of those
exact canonical UTF-8 Revision bytes. Hash and schema versions are coupled:
a new semantic shape, canonicalization rule, or digest algorithm requires a new
version; v1 values never get rehashed in place.

The frozen golden bytes are under [`contracts/p1a/fixtures`](../../contracts/p1a/fixtures):

- `revision-v1.canonical.json` — hash
  `sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263`;
- `proposal-v1.canonical.json`, `approval-v1.canonical.json`, and
  `revision-lifecycle-v1.canonical.json` — referential approved-state example;
- `request-envelopes-v1.canonical.json` — canonical request body/hash and
  operation-scope tuples;
- `state-machine-v1.canonical.json` — legal Proposal, lifecycle, and Approval
  transition matrix;
- `acceptance-v1.canonical.json` — replay, conflict, successful append,
  rejected withdrawal, restart, concurrency, and zero-partial-write matrix;
- `fixtures.sha256` — versioned SHA-256 manifest over all seven canonical fixtures.

Run `node contracts/p1a/verify.mjs` to validate canonical bytes, manifest
hashes, record links, cross-record envelope targets and identities, request
body hashes and scopes, policy defaults, privacy fields, closed object schemas,
state transitions, and acceptance-case inventory. Run
`node contracts/p1a/verify.mjs --self-test` to prove rejection of canonical
content drift, private fields, unpaired surrogates in values and keys,
request-hash conflation, consistently rehashed invalid calendar/time values,
rehashed create-target and decision-hash tampering, and missing vectors. The
verifier has no database, GUI, model, adapter, or network dependency.

## Privacy allowlist

The record and request-envelope fields enumerated above are the complete v1
canonical surface. The verifier uses context-specific closed schemas for every
fixture object; an unknown field is rejected even if it is not named below.
The explicit forbidden-name coverage includes repository and workspace roots or
paths (`repository_root`, `repository_path`, `repo_root`, `root_path`,
`file_path`, `workspace_path`, `worktree_path`, `path`), credentials and
headers (`token`, `access_token`, `refresh_token`, `api_key`, `api_token`,
`credential`, `credentials`, `password`, `authorization`,
`authorization_header`, `cookie`, `cookies`), sockets/process IDs, transcript
and identity paths, worker commands/arguments/environments, model
prompts/outputs/completions, and OMP audit output. The denylist is defense in
depth; closed shapes are the privacy boundary.

`task.*`, `acceptance_criteria`, and decision `reason` are the sole free-text
fields. They are operator-entered, secret-free task text; this contract does
not claim semantic secret detection. The local GUI input boundary must not
solicit or store secrets. Raw model output is excluded rather than redacted.

## Explicit non-goals

- SQLite schemas, migrations, queries, proposal storage, and restart code;
- GUI screens, IPC, renderer DTOs, or local-operator authentication;
- claims, leases, workers, policy execution, adapters, OMP calls, or audit
  persistence;
- budget enforcement, deadline timers, attempt counters, model calls, or
  advisory-model persistence;
- commits, pushes, code modifications derived from a Proposal, and any change
  to current run/lifecycle semantics.
