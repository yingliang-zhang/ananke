# ADR-0005: Multi-model review/audit (MoA) for controlled repair

## Status

Draft — 2026-07-31

## Context

Ananke's P6 controlled-repair contract (Slices 1–9) defines a single
repair-attestor that signs canonical attestations, and a single
release-pinned verifier in Ananke that validates signatures before
presenting attested evidence to the human operator. This establishes
*what happened* inside the repair sandbox — the factual record of
adapter execution, test results, and worktree effects.

It does not establish *whether the repair was correct*. That judgment
is currently delegated to a single independent hard review (runtime
implementation step 10) or to the human operator directly.

### Evidence from development

During P6 contract development (Slices 1–6), two rounds of dual-model
audit-review-loops were run with the following results:

| Round | Implementer | Auditors | Finding | Who caught it |
|---|---|---|---|---|
| R1 | GLM-5.2 | K3 | 2 × P3 (dead validator, unanchored slot ID) | K3 only |
| R2 | GLM-5.2 | K3 + GLM-5.2 | 1 × P2 (WorktreeSlotPathHash bound to writablePathSetHash; InstalledWorktreeRootIdentityHash bound to candidateRootIdentityHash) | Both, but K3 found the second sub-finding GLM missed |

The implementation model (GLM-5.2) never found defects in its own code.
Independent models (K3) consistently found defects the implementer
missed — including a cross-slice semantic binding error where
`InstalledWorktreeRootIdentityHash` was compared against
`candidateRootIdentityHash`, a quantity that Slice 4's anti-alias
invariant explicitly requires to *differ*. The implementation model
wrote this comparison, tests passed, and the model's own review did not
flag it.

### First-principles analysis

The fundamental question is: **a human with limited attention needs
trustworthy information to decide whether to accept a code change
produced by a model inside a sandbox.**

This decomposes into two orthogonal problems:

1. **What happened?** — The supervisor signs a canonical attestation
   recording the diff, test results, and sandbox state. This is a
   factual record, not a judgment.

2. **Is it correct?** — Does the diff actually address the request? Are
   there security issues, regressions, or contract violations? This
   requires judgment, and a single model's judgment is unreliable
   because:

   - **Cognitive homology**: a model that wrote code builds assumptions
     that it re-applies when reviewing the same code. The failure mode
     that produced a bug is the same failure mode that prevents seeing
     it. This is not carelessness — it is a structural property of
     self-review.

   - **Failure-mode independence**: different models have different
     training, different reasoning paths, and different blind spots. A
     finding that model A misses may be obvious to model B because B
     approaches the question from a different angle. The P2 evidence
     confirms this: K3's independent reading of the binding semantics
     exposed what GLM's authorial perspective concealed.

   - **Severity asymmetry**: in security-critical contexts, a single
     confident finding must not be suppressed by majority indifference.
     A voting mechanism that averages model outputs would lose the P2
     finding if only one of three models found it.

## Decision

Add an optional **multi-model review/audit layer (MoA)** between
attestation verification and human decision. The MoA layer:

1. **Runs after attestation is signed and verified** — it does not
   block attestation creation and does not alter the at-most-once
   semantics of the repair flow.

2. **Uses multiple independent models** — each model reviews the
   attested evidence (diff, test results, sandbox state) in an isolated
   sandbox with read-only access. No model sees another model's review
   before submitting its own.

3. **Aggregates by coverage-union, not vote** — the final finding set is
   the union of all models' findings. A finding's confidence is the
   fraction of models that independently found it, but confidence is
   information for the human, not a threshold for suppression.

4. **Does not make accept/reject decisions** — the MoA layer provides
   information. The human operator is the sole authority for acceptance
   or rejection.

5. **Separates execution, judgment, and verification** — three roles
   with distinct trust boundaries:

   | Role | Authority | Analogous P6 component |
   |---|---|---|
   | Supervisor | Execute (sandbox, sign attestation) | Repair-attestor |
   | Review-MoA | Judge (read-only review of attested evidence) | New |
   | Ananke | Verify + present (verify signatures, show to human) | Release-pinned verifier |

6. **Supports orthogonal and redundant review** — models may be
   assigned different review lenses (security, correctness, contract
   consistency, regression) for orthogonal coverage, or the same lens
   for redundant defense-in-depth. Both modes can coexist.

### MVP scope

The first implementation (runtime step 10) will:

- Run 2–3 models in parallel against verified attestation bytes
- Use prompt templates for each review lens
- Aggregate findings with deduplication and confidence scoring
- Present findings alongside attestation in the GUI

The MVP does **not** include:

- A separate review service process (runs inside Ananke)
- A separate signing key for review bundles (unsigned in MVP)
- Automated gating based on review findings (human decides)

These are v2 enhancements, deferred until the MVP demonstrates real
need.

## Alternatives considered

### Single-model review
Rejected: the P6 evidence shows a single model (including the
implementer) misses defects in its own code. Cognitive homology makes
self-review structurally unreliable.

### Majority voting
Rejected: a single high-confidence finding (e.g., a P2 security
binding error found by 1 of 3 models) would be suppressed by majority
indifference. Security findings must be unioned, not averaged.

### Human-only review (no MoA)
Rejected for the common case: the human's attention is the scarcest
resource. MoA's purpose is to surface relevant findings so the human
can focus attention where it matters, not to replace human judgment.

### MoA as blocking gate
Rejected: making MoA a blocking gate before attestation would violate
the at-most-once semantics and create a new failure mode (MoA service
down → repair stuck). MoA is non-blocking: attestation is created
regardless; review findings are additive information.

## Consequences

- **P6 Slices 1–9 remain unchanged** — MoA is a runtime enhancement,
  not a contract modification. The single-verifier attestation path is
  the authority; MoA adds information on top.

- **New runtime component** — Ananke gains a review-dispatch module that
  reads verified attestations and dispatches parallel model reviews.

- **Prompt templates become versioned artifacts** — review prompts for
  each lens (security, correctness, contract, regression) are frozen
  and pinned like other P6 contract artifacts.

- **GUI must present review findings** — alongside attestation evidence,
  the GUI shows aggregated findings with severity, confidence, and
  reviewer attribution.

- **Cost model** — each repair review costs 2–3 model invocations. This
  is acceptable for a single-researcher workflow where repairs are
  infrequent and high-stakes.

- **First-principles requirement** — every model participating in MoA
  must reason from first principles, not pattern-match against training
  data. Review prompts must instruct models to trace semantics from
  source rather than rely on surface-level code similarity. This is an
  operational constraint on prompt design, not a contract enforcement
  mechanism.
