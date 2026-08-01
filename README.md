# Ananke

Ananke (Ἀνάγκη) — a personal Research Coding OS.

Named after the Greek goddess of necessity: the constraints that hold even
when the rest of the system fails. Ananke is built for a single researcher
who relies on AI coding agents — it provides durable project continuity,
trusted process lifecycle, and layered memory that survives crashes, model
switches, and context resets.

## Status

Early development. The language evaluation and supervisor-spike phases are
complete; the production implementation begins from a clean Go architecture.

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Development principles

Ananke is built by AI coding agents and maintained by AI coding agents.
These principles govern how agents — and humans directing them — should
approach every task in this repository. If you use Ananke, you should
understand and agree with these principles.

### 1. First-principles reasoning

Every conclusion must start from the thing itself, not from a pattern
that worked elsewhere. "The code looks like X therefore it does Y" is
pattern matching. "The code's semantics require A, and A implies B,
therefore B" is first-principles reasoning. The difference matters because
patterns transfer incorrectly — a binding that looks reasonable can be
semantically wrong, and only first-principles analysis catches it.

This applies to implementation, review, audit, and debugging. A model
that writes code must understand why the code is correct, not just that
it passes tests. A model that reviews code must trace semantics from
source, not rely on surface similarity. Tests can encode the same wrong
formula as the implementation — both agree on the wrong answer.

### 2. Implementation does not review its own output

A model that writes code cannot reliably review it. This is not a
comment on model quality — it is a structural property. The reasoning
path that produced a defect is the same reasoning path that fails to
detect it. An author builds assumptions; a fresh reader questions them.

Ananke's P6 contract was developed by one model (GLM-5.2) and audited
by an independent model (K3). In two rounds of audit-review-loop, K3
found defects the implementer missed — including a cross-slice binding
error where two fields with different semantics were compared as if
identical. Tests passed. The implementer's contiguous self-review
passed. Only a fresh-context independent reading of the code caught it.
In R2, a fresh-context GLM-5.2 audit instance independently confirmed
the first sub-finding — fresh context matters, not just model diversity.

This is the same principle as P6's trust boundary: the adapter (code-writing
agent) is untrusted; the supervisor (attestor/executor) signs what happened;
Ananke's release-pinned verifier validates signatures; the human (decision
maker) decides. Execution, attestation, verification, and decision are
distinct roles with distinct trust boundaries.

### 3. Multi-model review with coverage-union aggregation (MoA)

When multiple models review the same artifact, their findings are
aggregated by **union, not vote**. A finding that one model raises with
high confidence is presented to the human regardless of whether other
models agree. The rationale: a single high-confidence security finding
must not be suppressed by majority indifference.

Confidence (how many models independently found the same issue) is
**information for the human**, not a suppression threshold. "1 of 3
models found this P2" tells the human "look carefully here" — it does
not auto-dismiss the finding.

Models may also review **different lenses** (security, correctness,
contract consistency, regression) for orthogonal coverage, or the same
lens for redundant defense-in-depth. Both modes can coexist.

See [ADR-0005](docs/adr/0005-multi-model-review-audit-moa.md) for the
full design.

### 4. Memory: authority vs context boundary

Models forget. Context windows are finite. Ananke treats memory as a
first-class architectural concern, not an afterthought.

The fundamental question is: **who has final authority over what?**

Ananke uses a strict authority/context boundary:

| Layer | Authority | Stores | Does not store |
|---|---|---|---|
| **Ananke SQLite** | Ananke (authority) | task proposal, approval, claim, run event (incl. conversational messages as journaled events), evidence, attestation, diff patches | user preferences, agent knowledge, agent skill state |
| **Session DB** | Hermes (context) | agent-internal conversation history, tool output, compressed summaries | task state, evidence |
| **MEMORY/USER** | Hermes (context) | operational facts, user preferences (injected every turn) | task state, evidence |
| **Hindsight** | Hermes (context) | episodic recall, delta-based retain/recall | task state, evidence |
| **Basic Memory** | Hermes (context) | structured markdown notes, FTS5 + vector search | task state, evidence |

Note: The redesign (`docs/first-principles-redesign.md`) reclassifies
conversational messages as journaled run events stored in Ananke SQLite.
This is an authority shift from the previous design (which listed
"conversation history" under Ananke's "does not store"). The rationale:
conversational messages in Ananke's GUI are not free-form chat — they are
structured run events (user_request, agent_reasoning, agent_evidence,
review_action) that participate in the durable journal, attestation flow,
and recovery loop. Hermes Session DB may still hold agent-internal
conversation history as context for the agent adapter, but Ananke's
authoritative record is the journal. See
[First-Principles Redesign](docs/first-principles-redesign.md) §Memory
and context.

Key principles:

- **Single authority**: Ananke SQLite is the sole canonical authority for
  task/run/evidence. Hermes memory never becomes Ananke's task authority.
- **No write-back**: Ananke never writes to Hermes memory.
- **Optional bridge**: if Ananke needs context from Hermes memory, the
  bridge is opt-in, scoped, read-only, provenance-bearing, and fails
  closed (no context is better than stale or wrong context).
- **Reproducibility**: a task contract must be self-contained. If its
  context depends on a global memory bank it doesn't control, the same
  proposal may behave differently on re-run.

The first artifact to read when resuming work on any project is the
[experiment ledger](docs/experiment-ledger.md) — before progress notes,
before session history, before code. It records what was tried, what
worked, what was decided, and why. If a fact will be stale in a week,
it does not belong in memory; if a procedure will be needed again, it
belongs in a skill.

See [ADR-0005](docs/adr/0005-multi-model-review-audit-moa.md) for the
full design. The memory boundary decision (reject external memory
engines, reuse Hermes four-layer context) is documented in this README
§4 and was evaluated using the 8-dimension framework in the
`memory-boundary-design` skill.

### 5. Machine-verifiable contracts before runtime

Contracts are frozen before implementation. A contract defines the
exact types, hashes, schemas, and verification rules that the runtime
must satisfy. The contract is independently reviewed and accepted before
any storage, process, or runtime code is written.

This means: types first, tests second, implementation third, runtime
last. The contract is the source of truth; the implementation is a
candidate that must prove it satisfies the contract. Tests are evidence,
not authority — a test that passes against a wrong implementation
proves nothing.

### 6. At-most-once effects, never exactly-once

Ananke's agent coding flow guarantees at-most-once automatic phase launch: any
crash after a claim may mean zero or partial effects and becomes signed
`waiting_for_human`. It is never automatically resumed. This is a
deliberate trade-off: automatic resume requires exactly-once machinery
(deduplication, idempotent replay, distributed consensus) that adds
complexity without clear benefit for a single-researcher workflow. If
the MVP demonstrates a real need, it can be added later.

### 7. Contract traceability — no surface without an ADR

No component, IPC path, operator surface, or authority boundary ships
without citing its locus in [ARCHITECTURE.md](ARCHITECTURE.md) or a
design contract (e.g., `docs/gui-*.md`). Any new surface, authority,
or IPC path requires an ADR or design contract **before** implementation.
A design contract (like `docs/gui-repair-interaction-design.md`)
satisfies this gate; it does not need to be in the ADR table, but must
be linked from ARCHITECTURE.md or the relevant design doc.

This principle was added after three direction divergences during
development (a web GUI built when the architecture specified Tauri 2;
a form-based repair panel when the user wanted conversational; Rust-layer
process spawning when the architecture specified Go daemon IPC). All
three would have been caught by one question: "where does this appear in
the architecture doc?"

The audit prompt for every future change must include: "Does this change
introduce a new surface, authority, or IPC path? If so, cite the ADR or
design contract that authorizes it."

## Architecture decisions

| Number | Title | Status |
|---|---|---|
| 0001 | Use Go for core and bootstrap | Accepted |
| 0002 | Supervisor lifecycle identity model | Draft |
| 0003 | Cleanup state machine and finalization outbox | Draft |
| 0004 | Select JSON Schema and Quicktype for P0a codegen | Accepted (P0a experiment only) |
| 0005 | Multi-model review/audit (MoA) for controlled repair | Draft |
| 0006 | Reframe from "controlled repair" to "controlled coding" | Accepted |

See [ADR index](docs/adr/README.md) for details.

## Repository and session design

### Why Go + SQLite

A single researcher depends on AI coding agents for all implementation.
Go was selected over Rust (ADR-0001) because: smaller agent-edit surface
(3,592 vs 4,204 LOC), faster build feedback (8.4s vs 15.2s), built-in
race detector, fewer dependencies (2 vs 6), and all six mutation gates
passed. The main store uses WAL journal mode; the trusted supervisor
journal and P6 phase-claim durability policy require `synchronous=FULL`
with `fullfsync=ON` for crash-durable claims — no external database server.

### Why repository-rooted, not global

Task contracts must be self-contained and reproducible. If a contract's
context depends on a global memory bank it doesn't control, the same
proposal may behave differently on re-run. The repository root is the
authority boundary: `docs/experiment-ledger.md`, `docs/adr/`, contract
files, and test fixtures are all in-repo. Session history and agent
operational memory are context, not authority.

### Why session isolation

Different tasks and worktrees must not pollute each other's state. Each
AI coding agent session gets its own working directory, session state,
and process group. The supervisor enforces this at the OS level with
dedicated runtime UID leases, sandbox profiles, and disposable test
roots. No session can read another session's repair state, credentials,
or journal.

### Why at-most-once

Automatic crash recovery requires exactly-once machinery (deduplication,
idempotent replay, distributed consensus). For a single-researcher
desktop workflow, the complexity exceeds the value. Instead, any crash
after a phase claim produces signed `waiting_for_human` — the human
decides whether to retry, and a retry is a new authorization chain, not
a resume.
