Working...
# SLICE CHANGES REQUESTED

**Gate:** Do not proceed to Slice 3. Slices 1–2 have four release-blocking contract defects and two evidence/privacy defects.

The current implementation has strong internal canonicalization and cross-record consistency for its single fixture. It does **not** yet freeze a deployable release trust root, fresh attempt-2 authorization, byte-exact replay, or a reusable verifier boundary.

## Release-blocking findings

### BLOCKER 1 — Release pins are compiled but do not identify real artifacts

**Files:** `internal/repaircontract/contract.go:392-498,1053-1055`  
**Contradicting claim:** `docs/experiments/p6-controlled-repair-supervisor-contract.md:29-62`

`FrozenReleasePins()` is independent of fixture input: `VerifyFixture` compares it directly at `contract.go:712-715`, and fully rehashed root/leaf/role/bundle substitutions were rejected.

However, the values are SHA-256 digests of public labels rather than real artifact bytes:

```go
BuildIdentityHash: fixedHash("controlled-repair-build-identity-v1")
RepairAttestorLeafSPKI:
    fixedHash("controlled-repair-review-attestor-leaf-spki-v1")
ContentSHA256:
    fixedHash("controlled-repair-public-trust-bundle-content-v1")
```

The same applies to the root SPKI, manifest, policy, profile, socket, journal, runtime, successor root, cross-signature, and release approval.

The modeled bundle contains only SPKI/certificate hashes, not an Ed25519 public key or verifiable certificate. Its declared file-content hash is not the hash of the canonical modeled bundle. The compiled device/inode/size are likewise synthetic and cannot describe a portable released installation.

Temporary probe result:

```text
all trust/build/policy identities are SHA-256 digests of public labels;
declared file content
sha256:4624a124...f0490f
is not modeled bundle bytes
sha256:56f62f44...ca3a764
```

**Failure sequence**

1. Slice 3 or a later runtime opens a real public bundle, release manifest, supervisor binary, or policy.
2. It computes the actual content/SPKI hashes.
3. Those values cannot match SHA-256 digests of unrelated labels without infeasible preimages.
4. The runtime must therefore either reject every real deployment or ignore/replace the “frozen” pins.
5. Ignoring or replacing them recreates caller/runtime trust substitution.

The rotation record has the same problem at `contract.go:482-498`: the cross-signature and release-approval fields are hashes of labels, with no canonical referenced object, signer role, signature bytes, or verification relation. Equality to one compiled sample is not a frozen rotation protocol.

**Required correction**

Before Slice 3:

- Generate pins from actual checked-in/release-produced artifacts:
  - canonical public trust-bundle bytes;
  - real DER/SPKI bytes for root and repair leaf;
  - actual signed certificate;
  - actual release/build manifest;
  - canonical supervisor policy/profile declarations.
- Check in a public bundle oracle usable by the verifier.
- Separate portable release pins from installation-specific descriptor facts:
  - compile content/SPKI/manifest hashes;
  - at runtime verify no-follow regular-file status, owner/mode, and descriptor stability;
  - do not compile a synthetic device/inode unless the product truly provisions one immutable machine-specific release image.
- Define canonical rotation approval and cross-signature records, their signature domains, signer roles, activation/overlap rules, and successor public material.

**Positive:** once real values replace the placeholders, the current direct comparison against `FrozenReleasePins()` is an appropriate non-substitutable pure-contract boundary. The fully linked mutation overlay rejected root, leaf, role, bundle, and rotation substitutions.

---

### BLOCKER 2 — Attempt 2 does not require a new human authorization or real predecessor

**Files:**  
`internal/repaircontract/contract.go:214-219,789-823,830-839`  
`internal/repaircontract/contract_test.go:470-475,643-673`  
**Requirement:** `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md:66`

For attempt 2, the verifier checks only that `previous_authorization_hash` is syntactically a SHA-256 string:

```go
if value.Attempt.AttemptNumber == 2 &&
    !validHash(value.Attempt.PreviousAuthorizationHash) {
    return ErrInvalidContract
}
```

It does not receive or verify the actual attempt-1 authorization. The permanent “attempt two” test retains the same:

- `approval_id`;
- `approved_at`;
- operator identity;
- GUI provenance;
- authorization fixture;

then changes the attempt fields, inserts an arbitrary hash, and recomputes public self-hashes.

The approval itself is only self-hashed. `OperatorIdentityHash` and `GUIProvenanceHash` are static label digests, not references to a unique authenticated GUI event.

**Exploit sequence reproduced**

1. Observe attempt-1 authorization.
2. Change the attempt fields to `2`.
3. Set `previous_authorization_hash` to any well-formed digest.
4. Retain the attempt-1 approval ID, timestamp, and provenance.
5. Recompute scope, approval, authorization, request, dispatch, and fixture hashes.
6. `VerifyFixture(..., ValidationAdmission)` returns success.

Observed:

```text
attempt 2 accepted unobserved predecessor
sha256:9498cfc15910b64451cc98c5116d001fa13e30a1ce616831db32b639e42e8dbc
```

This violates “attempt 1 and attempt 2 each require a new human authorization.”

**Required correction**

Freeze a predecessor-aware API, for example:

```go
VerifyAuthorization(
    expected AuthorityContext,
    current Authorization,
    predecessor *Authorization,
    now time.Time,
    moment ValidationMoment,
) error
```

For attempt 2 require:

- predecessor is a verified attempt-1 authorization;
- `previous_authorization_hash` equals its exact canonical authorization hash;
- same repair lineage/P4 proposal and attempt cap;
- a distinct GUI approval event ID and provenance-event hash;
- a fresh `approved_at`;
- a newly verified operator gesture;
- no reuse of attempt-1 approval bytes or provenance event.

Attempt 1 must reject any predecessor. Attempt 2 must reject absent, unknown, conflicting, attempt-2, or wrong-lineage predecessors.

---

### BLOCKER 3 — Replay classification is typed equality, not byte-exact replay

**File:** `internal/repaircontract/contract.go:1057-1065`  
**Contradicting claim:** `docs/experiments/p6-controlled-repair-supervisor-contract.md:124-133`

The comment says “byte-identical,” but the API receives two structs:

```go
func ClassifyDispatchReplay(
    existing, incoming ImmutableDispatch,
) (DispatchReplayDisposition, error) {
    if existing == incoming {
        return DispatchExactReplay, nil
    }
    return DispatchConflict, ErrDispatchConflict
}
```

It cannot observe encoding differences.

**Exploit sequence reproduced**

1. Serialize a dispatch.
2. Append a newline while preserving every member and the same `dispatch_hash`.
3. Decode both byte sequences using `json.Unmarshal`.
4. Pass the resulting structs to `ClassifyDispatchReplay`.
5. The function reports `exact_replay`.

Observed:

```text
same dispatch hash
sha256:88aa68cb26b4a6d43f9642c7308baf8256aa31c19076a8eef9e3fcd014faee1c
with altered bytes classified exact_replay
```

Changed deadline, peer, and profile structs correctly classified as conflicts. The defect is loss of byte identity before classification.

**Required correction**

The replay API must consume and retain canonical bytes, not only structs:

1. Strictly decode each dispatch with duplicate detection, closed schema, no trailing data, and canonical-byte equality.
2. Verify its self-hash and authoritative context.
3. Persist the canonical immutable bytes.
4. Return `exact_replay` only when `bytes.Equal(existingCanonical, incomingCanonical)`.
5. Return conflict for:
   - noncanonical alternate bytes;
   - same asserted hash with changed bytes;
   - changed deadline, peer, profile, channel, policy, request, authorization, or attempt;
   - two byte-identical but semantically invalid dispatches.

There is currently no strict dispatch-only decoder, so Slice 3 cannot safely call this classifier directly.

---

### BLOCKER 4 — Dynamic authority is replaced with one compiled sample fixture

**File:** `internal/repaircontract/contract.go:705-758,789-923`

`VerifyFixture` accepts no authoritative P4, operator, GUI provenance, repository, installed-profile, route, channel, or peer context. Instead, `validateFrozenScope` compares all request-specific data with constructors for one hardcoded example:

- P4/fence: `contract.go:840-850`;
- repository/base: `contract.go:852-859`;
- paths/profiles/route/peer: `contract.go:861-880`;
- operator/provenance labels: `contract.go:803-808`.

This makes the current mutation tests reject, but for the wrong architectural reason: the sample request is compiled into the verifier.

**Failure sequence**

A Slice 3 implementation has only two options:

1. Keep these comparisons, in which case only the fixture’s repository, commit, paths, profiles, P4 fact, operator label, and peer can ever be authorized.
2. Remove them to support real requests, in which case no API parameter identifies the authoritative expected P4 fact, GUI authentication event, installed profiles, peer, or policy. Self-consistent attacker values then become indistinguishable from legitimate dynamic values.

The internal tuple itself is comprehensive: it includes P4 input/bundle/admission, full fence, repository/base, ordered paths/profiles, route, channel, peer, policy, and repeated attempt fields. The missing piece is the **external authority against which that tuple is checked**.

**Required correction**

Split fixture generation from semantic verification:

- `CanonicalFixture()` remains an oracle.
- `VerifyReleaseTrust` compares with compiled release pins.
- `VerifyAuthorization` accepts a closed, trusted `AuthorityContext` containing:
  - exact durable P4 fact/fence;
  - repository/base decision;
  - GUI operator and provenance-event facts;
  - allowed ordered path/profile identities;
  - selected route/channel/peer and installed policy/profile hashes;
  - actual predecessor authorization for attempt 2.
- The authorization record must equal that context and bind it through its hashes.
- Tests must mutate both the request and its self-hashes while leaving the external context unchanged.

Do not move these authority values into Slice 3’s journal request and then treat request equality as authorization.

## Additional release-blocking evidence gap

### HIGH — Acceptance-vector inventory is decorative rather than execution-linked

**Files:**  
`internal/repaircontract/contract.go:322-390,611-635`  
`internal/repaircontract/contract_test.go:16-83,105-114,118-178,221-232,631-635`

`frozenAcceptanceVectors()` stamps `LinkedHashesRecomputed: true` onto every declared vector. The fixture test verifies only that:

- the IDs match a second manually duplicated list;
- `expected` is a recognized string;
- the boolean is true.

There is no registry mapping each vector ID to an executed mutation/test result. Removing a test does not invalidate the inventory.

The existing “self-consistent attacker root” test also does not rehash every linked field: `rehashPinsAndFixture` updates the pins and fixture hashes but not `Dispatch.ReleasePinsHash`. Its declaration still says all linked hashes were recomputed.

Permanent coverage is missing for:

- official RFC 8785 Appendix B numbers;
- UTF-16 key order;
- negative-zero and exponent thresholds;
- valid surrogate-pair canonicalization;
- dispatch lifetime N−1/N/N+1;
- same-hash/altered-byte replay;
- changed deadline/peer/profile replay conflicts;
- actual attempt-1 predecessor verification.

Temporary overlays confirmed that the current canonicalizer passes Appendix B numbers, UTF-16 ordering, negative zero, exponent thresholds, and dispatch-lifetime boundaries. Those passing probes are not permanent regression evidence.

**Required correction**

Make acceptance vectors executable:

- one table keyed by vector ID;
- each entry carries a mutation and expected result;
- the test records every executed ID and compares that set/order with `AcceptanceVectorIDs`;
- accepted boundary vectors execute positive validation;
- rejected vectors execute the actual attack with all linked hashes recomputed;
- no standalone `linked_hashes_recomputed: true` assertion.

## Privacy/error finding

### MEDIUM — Exported `HashRecord` leaks caller-controlled diagnostics

**File:** `internal/repaircontract/canonical.go:47-71`  
**Contradicting claim:** `docs/experiments/p6-controlled-repair-supervisor-contract.md:19`

`HashRecord` returns `json.Marshal` and nested canonicalization errors unchanged. A caller-supplied `json.Marshaler` can place sensitive text in that error.

Temporary probe:

```text
HashRecord returned caller-controlled diagnostic
"json: error calling MarshalJSON for type repaircontract.probeLeakyJSON:
 probe-private-value"
```

`DecodeFixture` and `VerifyFixture` correctly collapse their failures to `ErrInvalidContract`; the exported generic hash API does not.

**Required correction**

Either:

- make generic hash/canonical helpers package-private and expose only typed record hash functions; or
- wrap all public failures as `ErrInvalidContract`, preserving detailed causes only in an internal non-user-facing diagnostic channel.

No prohibited raw command, argv, environment, credential, private-key, PID, or socket-path field was found in the closed schemas. `ExecutableIdentityHash` is an identity digest, not an executable path.

## Positive findings

- Fully linked root/leaf/role/bundle/rotation substitutions are rejected by the compiled-pin equality boundary.
- Distinct repair role and signature domain are present.
- The canonicalizer passed temporary RFC 8785 Appendix B number vectors, UTF-16 key ordering, negative zero, exponent thresholds, and valid surrogate-pair probes.
- Duplicate keys, nested duplicate keys, trailing JSON, invalid UTF-8, lone surrogates, noncharacters, and unknown nested fields are rejected.
- `HashRecord` deletes only the named top-level self-hash; a nested member with the same name remains hash-significant.
- Self-hash validation covers the fixture, pins, file identity, root, certificate, bundle, rotation, fence, P4 binding, repository, path/profile entries, route, peer, scope, approval, authorization, request, and dispatch.
- Approval-age and authorization-lifetime N−1/N/N+1 behavior is correct. Dispatch N−1/N/N+1 also passed the temporary overlay.
- Expiry instants are exclusive; future approval/effect instants fail; canonical UTC parsing is strict.
- The current fixture’s complete P4/fence/repository/base/path/profile/route/channel/peer/policy tuple is internally bound.
- The experiment document clearly disclaims storage, transport, signature execution, process launch, filesystem effects, and full P6 acceptance at lines 3, 13, and 238.

## Commands and observed results

```text
go test ./internal/repaircontract -count=1 -timeout 120s
PASS — 1.254s

go test ./internal/repaircontract -count=10 -timeout 300s
PASS — 6.390s

go test -race ./internal/repaircontract -count=3 -timeout 300s
PASS — 16.732s

go vet ./internal/repaircontract
PASS — no output
```

Temporary read-only `-overlay` probes:

```text
Fully linked root/leaf/role/bundle/rotation substitutions
PASS — all rejected

RFC 8785 Appendix B numbers
PASS

UTF-16 key order, negative zero, exponent thresholds,
surrogate pairs, noncharacters, invalid UTF-8
PASS after correcting the temporary probe to apply invalid UTF-8
at DecodeFixture rather than its scalar-only helper

Dispatch lifetime N−1/N/N+1
PASS

Attempt 2 with fabricated unobserved predecessor
REPRODUCED — verifier accepted

Same hash with altered JSON bytes
REPRODUCED — classified exact_replay

Changed deadline/peer/profile
PASS — classified conflict

Caller-controlled HashRecord diagnostic
REPRODUCED — sensitive error text returned
```

`go test -json` confirmed the permanent test suite has no RFC 8785 number/key-order test or dispatch-lifetime boundary test.

No repository file was edited. Temporary probe and overlay files were created only under `/tmp`.

This verdict applies only to Contract Slices 1–2. It does not review the archived runtime, unrelated P5 changes, storage, transport, supervisor claims, or later P6 slices.
