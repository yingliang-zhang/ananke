# P4 self-development evidence verifier / bounded-repair admission fixture TDD plan

**Goal:** freeze an independently checked, design-only P4 evidence bundle and
bounded repair-admission boundary after the P3f protocol-adapter declaration.

**Authorized now:** two canonical P4 fixtures, their manifest, a dependency-free
P4 verifier and in-memory self-tests, this plan, the design contract, and a
ledger entry. **Not authorized:** supervisor, network, OMP, source/artifact
access, repair execution, callback or receipt implementation, approval/fence or
MoA issuance, commit, or push. P3e and P3f runtime paths are outside this slice.

## Task 1: bind the P1–P3f ancestry before P4 input

**Files:** `contracts/p4/verify.mjs`,
`contracts/p4/fixtures/evidence-repair-admission-v1.canonical.json`.

1. Bind P1 revision, P2 grill, P3a launch admission/spec, P3b full-fence rule,
   P3c recovery action, P3d adapter, and P3f activation/handoff/protocol-adapter
   identities.
2. Independently authenticate the P3f adapter fixture and its manifest-bound
   37-case adapter-denial fixture before any P4 manifest or fixture read.
3. Bind the P3f sealed-envelope and route-mapping identities into P4 envelope
   and route evidence.

**Acceptance:** P3f byte/count/manifest or ancestry drift rejects, and a bad P3f
adapter byte prevents every P4 read.

## Task 2: freeze immutable evidence records and bundle

1. Define closed self-hashed records for proposal, revision, approval, fence,
   envelope, receipt, callback, source, artifact, route, test, and evaluation.
2. Define a closed self-hashed bundle whose exact evidence-hash map contains all
   twelve records and whose identities bind P3f plus the verifier trust/release
   identities.
3. Keep source/artifact data hash-only. Exclude raw commands, endpoints,
   credentials, tokens, paths, prompts, prose, and secrets.
4. Mark callback evidence explicitly as non-success evidence.

**Acceptance:** self-hash, relation, evidence-map, canonical-byte, or raw-field
drift is rejected; no valid vector represents source/artifact access or a repair
result.

## Task 3: freeze verifier identity and replay behavior

1. Define trust-root SPKI and verifier release artifact/manifest identities as
   self-hashed fixture declarations, not keys, signatures, or releases.
2. Freeze an independent verifier input with exact bundle/P3f/admission/identity
   references.
3. Freeze a design-only output that can be verified while remaining
   `waiting_for_human` and `not_authorized_by_verifier` for repair execution.
4. Freeze replay to the same canonical output with zero new durable facts.

**Acceptance:** trust/release/input/output/replay binding drift rejects, and no
output can state or infer repair execution/success.

## Task 4: freeze bounded repair admission and denial projection

1. Set the fixed cap to two attempts and allow only
   `self_development_repair_runner` on the exact route evidence hash.
2. Require the exact bundle plus all twelve evidence hashes.
3. Require a fresh approval and fresh fence, each distinct from its prior hash
   and issued after the evaluation request.
4. Require a typed `ananke.moa-typed-role-grant.v1` bound to the allowed role,
   route, fresh approval/fence, exact bundle, verifier trust identity, and a
   strict validity interval.
5. Make every failure and review-finding vector project only
   `waiting_for_human`; add no retry or success inference.
6. Map each exact 38-case denial kind one-to-one to a targeted invalid
   evidence/admission mutator; rehash each enclosing dependent record before
   validation unless the denial itself is a self-hash drift.

**Acceptance:** the mutator map's cardinality and ordered kinds exactly match
the fixture inventory. Every target validator rejects its intended invariant,
and every cap, role, route, freshness, evidence, grant, inferred-success,
failure, and review-finding vector retains the one closed human-handoff
projection.

## Task 5: gate the fixture oracle

1. Pin both P4 fixture bytes in `fixtures.sha256` and verifier hard digests.
2. Run normal verification and in-memory mutation self-test.
3. Record the exact commands and the design-only boundary in the ledger.

```sh
node --check contracts/p4/verify.mjs
node contracts/p4/verify.mjs
node contracts/p4/verify.mjs --self-test
```

**Acceptance:** normal verification reads only fixtures; self-test proves P3f
first-dependency ordering and, with a complete one-to-one rehashed mutator map,
rejects every P4 policy class in memory while checking its identical closed
`waiting_for_human` projection. Neither command runs a supervisor, network,
OMP, repair, source/artifact operation, commit, or push.
