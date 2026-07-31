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
by independent models (K3, GPT-5.6). The independent auditors found
defects the implementer missed — including a cross-slice binding error
where two fields with different semantics were compared as if identical.
Tests passed. The implementer's self-review passed. Only an independent
model reading the code from zero caught it.

This is the same principle as P6's trust boundary: the adapter (executor)
is untrusted; the supervisor (verifier) is separate; the human (decision
maker) is separate. Execution, judgment, and verification are distinct
roles with distinct trust boundaries.

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

### 4. Memory is layered and durable

Models forget. Context windows are finite. Ananke treats memory as a
first-class architectural concern, not an afterthought:

- **Session memory**: conversation history and tool output — finite,
  compressed, eventually lost.
- **Project memory**: experiment ledger, planning files, normative
  documents — durable artifacts in the repository that survive across
  sessions.
- **Operational memory**: skills, ADRs, configuration — procedural
  knowledge that encodes how to do things, not what was done.

The first artifact to read when resuming work on any project is the
[experiment ledger](docs/experiment-ledger.md) — before progress notes,
before session history, before code. It records what was tried, what
worked, what was decided, and why. If a fact will be stale in a week,
it does not belong in memory; if a procedure will be needed again, it
belongs in a skill.

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

Ananke's repair flow guarantees at-most-once automatic phase launch: any
crash after a claim may mean zero or partial effects and becomes signed
`waiting_for_human`. It is never automatically resumed. This is a
deliberate trade-off: automatic resume requires exactly-once machinery
(deduplication, idempotent replay, distributed consensus) that adds
complexity without clear benefit for a single-researcher workflow. If
the MVP demonstrates a real need, it can be added later.
