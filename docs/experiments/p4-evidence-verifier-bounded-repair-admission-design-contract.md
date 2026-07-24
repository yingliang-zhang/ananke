# P4 self-development evidence verifier and bounded-repair admission design contract

**Status:** frozen design contract and canonical fixture oracle.
**Scope:** immutable evidence declarations, a dependency-free fixture verifier,
two canonical fixtures, this contract, its plan, and a ledger entry. This slice
creates **no** supervisor, network client/server, OMP session, repair executor,
source or artifact access path, process, child, commit, or push. Fixture hashes
are declarations, never release, execution, repair, source, artifact, receipt,
callback, or approval authority.

## Decision

P4 is a fail-closed evidence-and-admission boundary for Ananke self-development.
It can verify a sealed canonical evidence bundle and determine only that a
**design-only bounded repair admission** is structurally admissible. It cannot
start, infer, report, or authorize a repair. Both a valid vector and every
rejected vector retain `state: "waiting_for_human"`; the valid vector's
`verification_state: "verified"` means only that its immutable fixture bindings
were checked.

The canonical ancestry is intentionally explicit:

```text
P1a immutable Proposal / Revision / Approval
  → P2a deterministic Grill review
    → P3a immutable launch admission
      → P3b current-full-fence requirement
        → P3c retry_process_admission obligation
          → P3d controlled read-only OMP adapter declaration
            → P3f activation → Darwin none_fail_closed exec-by-FD design
              → P3f external-supervisor handoff
                → P3f independently trusted protocol-adapter declaration
                  → P4 evidence verifier / bounded-repair admission
```

P4 binds the P1 revision hash, P2a grill fixture, P3a launch fixture and launch
spec, P3b fence rule, P3c action, P3d fixture, and the P3f protocol-adapter
fixture. It independently authenticates the exact P3f adapter fixture
`sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44`
and its exact **37**-case denial fixture
`sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525`
before reading any P4 fixture. The adapter's sealed-envelope and route-mapping
hashes are transitive P4 inputs. P3e and P3f runtime paths remain out of scope;
this is not a claim about their presence or execution elsewhere.

## Immutable evidence bundle and records

All records are RFC 8785 JCS UTF-8 without a BOM or trailing whitespace. Each
record has a closed schema and `evidence_hash` equal to SHA-256 over its complete
canonical object excluding that field. The bundle likewise has a closed
`bundle_hash` over its canonical object excluding `bundle_hash`. Hashes must be
`sha256:<64-lowercase-hex>`.

`ananke.self-development-evidence-bundle.v1` carries an exact `evidence_hashes`
set. Its twelve required members point only to immutable, self-hashed evidence
records:

| Evidence record | Schema | Required binding, not a capability |
| --- | --- | --- |
| proposal | `ananke.self-development-evidence-proposal.v1` | P1 proposal identity and revision hash |
| revision | `ananke.self-development-evidence-revision.v1` | P1 revision and proposal evidence hash |
| approval | `ananke.self-development-evidence-approval.v1` | review-only approval and issuance time |
| fence | `ananke.self-development-evidence-fence.v1` | P3a launch-spec hash, generation, issuance time |
| envelope | `ananke.self-development-evidence-envelope.v1` | exact P3f predecessor envelope and route mapping |
| receipt | `ananke.self-development-evidence-receipt.v1` | envelope evidence and opaque predecessor receipt hash |
| callback | `ananke.self-development-evidence-callback.v1` | receipt evidence and opaque predecessor callback hash; explicitly not execution success |
| source | `ananke.self-development-evidence-source.v1` | source snapshot hash only; no source access |
| artifact | `ananke.self-development-evidence-artifact.v1` | artifact hash only; no artifact access |
| route | `ananke.self-development-evidence-route.v1` | exact P3f route mapping and the single repair route |
| test | `ananke.self-development-evidence-test.v1` | canonical-fixture-verifier test manifest only |
| evaluation | `ananke.self-development-evidence-evaluation.v1` | repair-admission request linked to test evidence |

The canonical bundle must bind the exact P3f adapter fixture, P3f denial-fixture
digest and count, and both verifier identities. A changed bundle member, record
self-hash, P3f identity, record relation, or schema is a denial. Raw source,
artifact, endpoint, command, argv, environment, credential, token, prompt,
prose, secret, and path data are unrepresentable.

## Independent verifier input, output, replay, and identity

`contracts/p4/verify.mjs` is independent of the P3f verifier: it imports no P3f
code and does not invoke it. It authenticates the P3f manifest entries and
canonical bytes itself, checks the adapter's closed predecessor binding and its
37 ordered denial cases, then authenticates the P4 manifest and bytes. Its only
input material is canonical fixture bytes and hash declarations.

The fixture freezes these closed self-hashed records:

- `ananke.self-development-evidence-verifier-input.v1`: exact bundle hash,
  P3f adapter/red-flag identities and count, repair-admission hash, verifier
  trust identity, and verifier release identity.
- `ananke.self-development-evidence-verifier-output.v1`: a verified,
  design-only admission with `repair_execution:
  "not_authorized_by_verifier"` and `state: "waiting_for_human"`. It cannot
  encode repair success, a result, or an execution result.
- `ananke.self-development-evidence-verifier-replay.v1`: input and output
  hashes, `new_durable_facts: 0`, and `replay_result:
  "exact_canonical_output"`. A replay of the same canonical input must produce
  the same canonical output and no new fact.
- `ananke.self-development-verifier-trust-identity.v1` and
  `ananke.self-development-verifier-release-identity.v1`: a pinned trust-root
  SPKI hash plus a separately self-hashed verifier release artifact/manifest
  identity. These are fixture identities, not keys, signatures, or a release
  implementation.

A future production verifier must independently perform signature, trust-root,
revocation, and release-validation work. A self-consistent P4 fixture is never a
substitute for those operations.

## Bounded repair admission

`ananke.self-development-bounded-repair-admission.v1` is the only repair policy
shape. The canonical vector allows attempt `1` of cap `2`, the one typed role
`self_development_repair_runner`, and the one exact route evidence hash for
`ananke_self_development_evidence_repair_v1`. It carries no executable repair
content, command, source, artifact, endpoint, process, or VCS capability.

Admission requires all of the following, without a fallback:

1. The exact bundle hash and all twelve exact evidence hashes.
2. A fresh approval evidence hash different from the prior approval and issued
   after the evaluation request.
3. A fresh fence evidence hash different from the prior fence and issued after
   the evaluation request.
4. A self-hashed `ananke.moa-typed-role-grant.v1` bound to that exact bundle,
   fresh approval, fresh fence, allowed route, allowed role, verifier trust
   identity, and a strict validity interval after both fresh facts.
5. An integer attempt in `1..attempt_cap`. Replays, a cap overrun, a nonpositive
   attempt, another role, another route, a stale approval/fence, absent grant,
   or any grant-binding drift are denials.

`admission_state: "design_only_no_repair_execution"` and
`inferred_success: "forbidden"` are normative. Neither a receipt, callback,
test, evaluation, valid admission, nor a replay can imply that a repair occurred
or succeeded. A failure, malformed input, unavailable evidence, verifier
failure, or review finding projects exactly:

```json
{"admission":"rejected","bundle_hash":null,"repair_execution":"not_authorized","state":"waiting_for_human","verification_state":"not_run"}
```

A review finding is therefore a human handoff, not a retry trigger and not a
repair result.

## Fixture oracle and gate

- `contracts/p4/fixtures/evidence-repair-admission-v1.canonical.json` — P1–P3f
  chain, closed evidence schemas/records/bundle, verifier identities,
  independent verifier input/output/replay, and bounded repair policy;
  SHA-256 `aa7d94f96b123ff200bf4f84ec55d7b5edbd157f4578ba99ed3b4fdbc93ee36c`.
- `contracts/p4/fixtures/repair-admission-red-flags-v1.canonical.json` — 38
  ordered P4 denial classes: P3f identity/count drift, every evidence hash,
  canonicality, verifier identity/replay, cap/role/route/evidence/freshness/MoA
  drift, inferred success, failure inference, and review-finding inference;
  SHA-256 `91c900ce7cc2c53ce360775be0909b3e679a971756075d643f3b0d0e3eb4ce0f`.
- `contracts/p4/fixtures/fixtures.sha256` and verifier hard digests pin exact
  bytes.
- `contracts/p4/verify.mjs` validates the P3f-first dependency, all JCS and
  self-hash derivations, every closed relation, the exact P3f 37-case inventory,
  and in-memory denial mutations.

```sh
node --check contracts/p4/verify.mjs
node contracts/p4/verify.mjs
node contracts/p4/verify.mjs --self-test
```

The normal gate reads fixture bytes only. The self-test mutates in-memory copies
only. Neither command creates a supervisor/network/OMP/repair/source/artifact
operation, child, process, commit, or push.

## Explicit non-goals

- supervisor, protocol adapter, network/RPC, TLS/mTLS, endpoint, signature,
  key, trust store, release implementation, OMP session, process, child, or
  source/artifact/evidence I/O implementation;
- repair planning/execution, state persistence, retry scheduling, approval or
  fence issuance, MoA-grant issuance, callback/receipt acceptance, or inferred
  success/failure/review outcome;
- commands, prompts, raw source/artifact/evidence, verification execution,
  commits, and pushes.
