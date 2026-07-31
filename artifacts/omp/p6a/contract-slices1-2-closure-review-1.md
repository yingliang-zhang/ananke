Working...
# SLICE CHANGES REQUESTED

**Gate:** Do not proceed to Slice 3. The original attempt-2, replay, executable-vector, generic-error, and RFC 8785 findings are closed. Three new authorization/trust defects and one incomplete rotation trust contract remain release-blocking.

## Release-blocking findings

### BLOCKER — Retained authorization capability bypasses effect-time approval freshness

**Files:**  
`internal/repaircontract/authorization.go:46-83,138-187`  
`internal/repaircontract/dispatch.go:12-20,42-92`

`VerifyAuthorization` checks approval freshness only when it creates `VerifiedAuthorization`. The capability stores no verification instant or freshness bound.

`DecodeDispatch(..., ValidationEffect)` verifies:

- dispatch creation/expiry;
- static authorization bindings;
- canonical hashes.

It does **not** re-run `validateAuthorizationRecord` against the effect-time `now`. Consequently, a capability minted while `MaxApprovalAge` is valid remains usable after that age expires, provided the dispatch and longer authorization lifetime remain open.

**Exploit sequence reproduced**

1. Approval occurs at `12:00`.
2. Call `VerifyAuthorization` at dispatch admission `12:04`; approval age is four minutes.
3. Retain the returned opaque capability.
4. At `12:05:00.000000001`, approval age exceeds the inclusive five-minute maximum.
5. Dispatch remains valid until `12:08`; authorization remains valid until `12:10`.
6. Call `DecodeDispatch(..., ValidationEffect)`.
7. The effect dispatch is accepted.

Observed:

```text
effect accepted at approval age 5m0.000000001s while maximum is 5m0s
```

This contradicts `docs/experiments/p6-controlled-repair-supervisor-contract.md:102`, which requires approval freshness at both admission and effect time.

**Required correction**

When `checkFreshness` is true, `validateDispatchRecord` must revalidate the private authorization at the supplied `now`, including:

```go
validateAuthorizationRecord(verified.authorization, now)
```

Add permanent capability-retention tests for:

- approval age N−1/N/N+1 at dispatch effect;
- authorization expiry;
- dispatch expiry;
- an authorization capability created at admission and reused at effect.

Replay classification may intentionally skip clock freshness, but it must remain a static classification operation with no effect authority.

---

### BLOCKER — Release trust verification is optional and not capability-linked

**Files:**  
`internal/repaircontract/release_artifacts.go:247-302`  
`internal/repaircontract/authorization.go:46-83`  
`internal/repaircontract/dispatch.go:12-20,42-65`

The active release artifacts are now real and properly embedded. However, the public API does not enforce their verification:

- `VerifyReleaseTrust` returns only `error`.
- `VerifyAuthorization` neither accepts a verified-release capability nor calls `VerifyReleaseTrust`.
- `DecodeDispatch` accepts only `AuthorityContext` and `VerifiedAuthorization`.
- `VerifiedAuthorization` contains no release-pins hash, certificate-validity proof, or release-verification capability.

A caller can therefore omit `VerifyReleaseTrust` entirely.

**Exploit sequence reproduced**

1. Move `now` to `2029-01-01`; the embedded repair-attestor certificate expired at `2028-07-01`.
2. Construct a fresh 2029 GUI authorization and dispatch against the compiled policy/profile hashes.
3. `VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), ..., now)` correctly returns `ErrInvalidContract`.
4. Skip that call.
5. `VerifyAuthorization` accepts the authorization.
6. `DecodeDispatch` accepts the dispatch.

Observed:

```text
authorization and dispatch accepted at 2029-01-01T12:04:00Z
after repair leaf expiration
```

The documentation instruction to call three functions in sequence is not a security boundary.

**Required correction**

Either:

1. Have `VerifyAuthorization` and effect-time `DecodeDispatch` internally verify the compiled release trust at `now`; or
2. Return an opaque `VerifiedReleaseTrust` from `VerifyReleaseTrust` and require it when creating `VerifiedAuthorization` and admitting effects.

The capability must bind:

- exact release-pins hash;
- root and leaf certificate/SPKI hashes;
- role and signature domain;
- certificate validity interval;
- release manifest, policy, profile, contract-release, and rotation-policy hashes.

A stale release capability must not authorize a new authorization or effect. Static replay classification may use an immutable release-identity proof without requiring the certificate to remain currently valid.

---

### HIGH — The reusable authority verifier does not close FullFence semantics

**Files:**  
`internal/repaircontract/authorization.go:86-135,138-176`  
`internal/repaircontract/contract.go:150-168`

The new external `AuthorityContext` correctly compares all P4/fence/repository/path/profile/route/channel/peer/policy/GUI fields with the authorization. However, both authority and authorization validation omit semantic checks for the nested `FullFence`:

- no `FullFence.SchemaVersion == FullFenceSchemaVersion`;
- no nonempty/closed `ClaimID`;
- no positive `FenceGeneration`.

Because an independently supplied context and authorization can contain the same malformed value, equality and self-hashes do not reject it.

**Exploit sequence reproduced**

For each mutation:

1. Change the full fence.
2. Recompute fence, P4, scope, approval, authorization, request, dispatch, and fixture hashes.
3. Construct a matching external `AuthorityContext`.
4. Call `VerifyAuthorization`.

Accepted values included:

```text
full_fence.schema_version = "attacker.foreign-fence.v9"
claim_id                  = ""
fence_generation          = 0
fence_generation          = -1
```

A foreign fence version is especially unsafe: its semantics may not represent the P4 admission fence the supervisor believes it is enforcing.

**Required correction**

Validate in both `validateAuthorityContext` and `validateAuthorizationRecord`:

- exact `FullFenceSchemaVersion`;
- closed/nonempty claim-ID grammar and length;
- valid claim-token hash;
- `FenceGeneration > 0`;
- exact attempt/cap relations already enforced at the P4 layer.

Add executable registry vectors for foreign fence schema, empty claim ID, zero generation, and negative generation.

The same semantic pass should add nonempty closed-ID rules for repository identity, route ID, supervisor-profile ID, and peer role. Temporary probes showed those fields also accept empty values when the external context matches.

---

### HIGH — Future independent rotation approval has no trust anchor

**Files:**  
`internal/repaircontract/release_artifacts.go:194-237,520-570`  
`internal/repaircontract/testdata/release-v1/rotation-policy.json:1`  
`docs/experiments/p6-controlled-repair-supervisor-contract.md:70-82`

The correction properly removed the fake active successor and fake signature references. V1 is exactly `no_successor_authorized`.

The future rotation policy nevertheless declares an “independent release approval” containing caller-supplied:

- `signer_key_id`;
- `signer_spki_sha256`;
- signature.

It requires only that the signer SPKI differ from the current and successor roots. No current release artifact or pin identifies the authorized independent approver key, certificate, root, role certificate, key usage, or validity interval.

“Valid independent release approval” is therefore undefined as a trust decision.

**Failure sequence**

1. A successor proposal receives a current-root cross-signature, legitimately or after current-root compromise.
2. The proposer generates another arbitrary Ed25519 key.
3. The key differs from the current/successor roots.
4. The proposer places its key ID/SPKI in the independent-approval record and signs it.
5. Every currently frozen independent-approval rule is satisfied unless a later implementation invents an undeclared trust source.

This defeats the intended independent second authorization factor.

**Required correction**

Before declaring Slice 1 rotation frozen, either:

- embed and manifest-bind the independent release-approver trust root/certificate/SPKI, exact role/domain, key usage, and validity rules; or
- remove the claim that future rotation approval is frozen and defer the complete rotation protocol to a separately reviewed slice.

The approver key must be authenticated by a trust anchor already controlled by the current release—not by the `signer_spki_sha256` supplied inside the approval being verified.

## Original finding closure status

| Original finding | Status |
|---|---|
| Real deployable release pins | **Active artifacts fixed.** Real Ed25519 DER/SPKI, bundle, manifest, policy, profile, contract-release, role/domain, chain, key use, validity, and portable descriptor separation verified. Public API sequencing remains blocked as above. |
| Attempt-2 predecessor/fresh GUI event | **Closed.** Requires an opaque verified attempt-1 capability, exact predecessor hash, same lineage/proposal/cap, later time, distinct approval ID/provenance/hash/bytes. Wrong, absent, conflicting, wrong-lineage, attempt-2, and reused approvals are rejected. |
| Byte-exact replay | **Closed.** Strict dispatch-only decoder rejects duplicate/unknown/trailing/noncanonical input. Replay compares canonical bytes, statically validates both values, rejects identical invalid bytes, and conflicts on changed bindings. |
| Reusable dynamic authority | **Substantially closed.** Exported `AuthorityContext` is independently supplied; fixture is only an oracle; request-derived helper is unexported. FullFence semantic closure remains blocked. |
| Decorative acceptance inventory | **Closed.** All 66 ordered IDs have executable entries and were observed executing. Missing, duplicate, nil, reordered, and sentinel-mismatched entries fail. The attacker-root mutation updates pins, dispatch release-pin binding, dispatch hash, and fixture hash. |
| Generic hash/error leak | **Closed.** Generic canonical/hash helpers are package-private and public decoders/verifiers return stable sentinel errors. |
| Permanent RFC 8785/boundary coverage | **Closed.** Appendix B, UTF-16 ordering, negative zero, exponent thresholds, valid surrogate pairs, approval/lifetime boundaries, timestamp rejection, and dispatch N−1/N/N+1 are permanent tests. |

## Positive trust evidence

The active V1 release trust now satisfies the first review’s artifact requirements:

- Ten explicit non-wildcard `go:embed` inputs.
- Manifest binds the other nine artifacts in exact order.
- Pins bind the manifest, bundle, root/leaf DER and SPKI, contract release, policy, profile, and rotation policy.
- Root and leaf are real Ed25519 X.509 certificates.
- Root self-signature and leaf chain verify.
- Root usage: digital signature plus certificate signing, CA=true.
- Leaf usage: digital signature only, CA=false.
- Exact critical leaf extensions:
  - role OID `1.3.6.1.4.1.57264.1.6`;
  - domain OID `1.3.6.1.4.1.57264.1.7`.
- Root validity: `[2026-07-01, 2031-07-01)`.
- Leaf validity: `[2026-07-01, 2028-07-01)`.
- Repair role/key are distinct from the P5 protocol adapter.
- Portable pins contain no device, inode, mode, owner, socket, journal, or runtime observation.
- No active successor, cross-signature, or release-approval instance exists.

The capability-copy probe also passed: mutating caller-owned path/profile slices after `VerifyAuthorization` did not alter the opaque capability.

## Commands and results

### Candidate integrity before review

```text
sha256sum artifacts/omp/p6a/contract12-candidate-source-manifest.json
906f65401455e1647001c833dd5600a520debf89e2512781a31c0eec31d12dca
```

Manifest contained exactly 20 files. Every listed size/hash target was present; all 20 `sha256sum -c` checks returned `OK`.

### Focused behavior

```text
go test ./internal/repaircontract \
  -run '^TestP6A2ExecutableAcceptanceVectorRegistry$' \
  -count=1 -v
PASS — all 66 named vectors executed, 0.423s
```

Focused predecessor, replay, dynamic-authority, privacy, certificate, and canonical-boundary selections all passed.

Temporary read-only overlay probes:

```text
Stale VerifiedAuthorization used at effect N+1
REPRODUCED — accepted

Fresh 2029 authorization after leaf certificate expiry
REPRODUCED — VerifyAuthorization and DecodeDispatch accepted

Foreign FullFence schema
REPRODUCED — accepted

Empty claim ID; zero/negative fence generation
REPRODUCED — accepted

Caller-slice mutation after capability creation
PASS — capability remained isolated
```

### Package verification

```text
go test ./internal/repaircontract -count=1 -timeout 300s
PASS — 1.231s

go test ./internal/repaircontract -count=10 -timeout 300s
PASS — 5.605s

go test -race ./internal/repaircontract -count=3 -timeout 300s
PASS — 14.517s

go vet ./internal/repaircontract
PASS — no output
```

## Final manifest integrity

After all reads, certificate inspection, tests, and temporary-overlay probes:

```text
manifest SHA-256:
906f65401455e1647001c833dd5600a520debf89e2512781a31c0eec31d12dca

manifest file count:
20
```

All 20 final per-file checks again returned `OK`. The review candidate remained identical throughout the review.

No repository file was edited. Temporary probes existed only under `/tmp`. This verdict applies only to P6 Contract Slices 1–2; it is not runtime, storage, or full P6 `DESIGN ACCEPT`.
