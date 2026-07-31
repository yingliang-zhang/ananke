Working...
# SLICE CHANGES REQUESTED

Do not proceed to Slice 3. The four implementation findings are materially closed, but the frozen experiment contract contradicts the B2 release oracle and executable registry.

## HIGH (P3): frozen contract document still describes the pre-B2 trust release

**Locations**

- `docs/experiments/p6-controlled-repair-supervisor-contract.md:12`
- `docs/experiments/p6-controlled-repair-supervisor-contract.md:34-52`
- `docs/experiments/p6-controlled-repair-supervisor-contract.md:66`
- `docs/experiments/p6-controlled-repair-supervisor-contract.md:74-80`
- `docs/experiments/p6-controlled-repair-supervisor-contract.md:186-268`

**Mismatch**

The frozen document says:

- four DER certificate/SPKI inputs, rather than eight;
- ten embedded public artifacts, rather than fourteen;
- nine manifest-bound artifacts, rather than thirteen;
- old pre-B2 release hashes;
- only the repair certificate chain;
- an independent approval policy without documenting its fixed released approver identity;
- 79 executable vectors, omitting all twelve B2 vectors.

Current compiled artifacts instead contain fourteen public artifacts and a thirteen-entry manifest. Current hashes are:

```text
release_pins_hash      sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a
trust_bundle_hash      sha256:28519f0022034ed6c47b011351ce80cb0f42594f4df2ebe5f4a820678d169786
release_manifest_hash  sha256:09918769192b68de8f9f9e13c7281f37ecc24231fbce89b9bd3dce9f5fb0c095
rotation_policy_hash   sha256:069e8492a9b2bd1ac9ac6f62f2e4f9946a5004e5236349b32aa334a153d75a24
```

The document still carries:

```text
release_pins_hash      sha256:bda296df...
trust_bundle_hash      sha256:2e83c327...
release_manifest_hash  sha256:6a53929c...
rotation_policy_hash   sha256:d8e042a5...
```

**Failure sequence**

1. A Slice 3 implementation treats the frozen experiment contract as the normative Slice 1–2 handoff.
2. It implements the documented ten-artifact oracle or listed hashes.
3. It either rejects the actual canonical fourteen-artifact release, or reconstructs the pre-B2 boundary that omits the independent rotation-approver certificates/SPKIs.
4. Its acceptance inventory also omits the twelve B2 negative vectors despite the executable implementation containing 91 vectors.

The current Go verifier remains fail-closed; no present code-level trust bypass was reproduced. The defect is an inconsistent frozen security contract and therefore blocks downstream implementation.

**Required correction**

1. Update line 12 to describe eight DER certificate/SPKI files and fourteen total embedded public artifacts.
2. Update line 34 to fourteen explicit `go:embed` inputs and thirteen manifest entries.
3. Replace the stale release table with current values and include:
   - approver root certificate/SPKI hashes;
   - approver leaf certificate/SPKI hashes;
   - approver root ID;
   - fixed approver key ID;
   - fixed role and signature domain.
4. Document the exact manifest order:

   ```text
   contract_release
   public_trust_bundle
   repair_attestor_certificate_der
   repair_attestor_spki_der
   repair_root_certificate_der
   repair_root_spki_der
   rotation_approver_certificate_der
   rotation_approver_spki_der
   rotation_approver_root_certificate_der
   rotation_approver_root_spki_der
   rotation_policy
   supervisor_policy
   supervisor_profile
   ```

5. Document both Ed25519 X.509 chains, their independent SPKIs, certificate semantics, and the fixed future approval signer identity.
6. Expand the ordered inventory from 79 to 91 entries, inserting all twelve B2 vectors and renumbering the remaining entries.
7. Regenerate the 24-file candidate manifest and submit it for another read-only closure check.

## Original finding disposition

1. **Effect-time freshness — CLOSED**
   - `DecodeDispatch` revalidates the retained authorization when freshness is required.
   - Approval-age N−1/N/N+1, authorization expiry, dispatch expiry, and retained-capability reuse are covered.
   - Static replay remains clock-independent and classification-only.
   - Independent overlay rejected a retained capability at approval age `5m0.000000001s`.

2. **Mandatory embedded release — CLOSED**
   - `VerifyAuthorization` and freshness-enforcing `DecodeDispatch` independently validate the compiled release at their supplied time.
   - Certificate expiry boundaries and fresh 2029 authorization/dispatch attempts are rejected.
   - Replay classification does not become effect authority.

3. **External semantic identifiers — CLOSED**
   - Both external authority and authorization records reject foreign fence schemas, empty/malformed claims, nonpositive generations, and empty/malformed repository, route, supervisor-profile, and peer-role identifiers.
   - Independent probes recomputed matching hashes and authority values; all malformed cases were rejected.

4. **Independent rotation approver — implementation CLOSED; frozen specification NOT CLOSED**
   - Four real independent approver DER/SPKI inputs are explicitly embedded.
   - Root/leaf certificate semantics, exact DER/SPKI, manifest/pins, fixed signer identity, cryptographic separation, and V1 no-successor state passed review.
   - Missing, duplicate, and reordered manifest entries were rejected.
   - Duplicate or noncritical role extensions were rejected.
   - No active successor, cross-signature, or independent approval instance exists.
   - The experiment contract does not accurately describe this implementation.

## Commands and results

```text
go test ./internal/repaircontract \
  -run '^TestP6A2RetainedAuthorizationEffectFreshness$|^TestP6A2ReleaseTrustMandatoryAtAuthorizationAndEffect$' \
  -count=1 -v -timeout 120s
PASS — 1.435s
```

```text
go test ./internal/repaircontract \
  -run '^TestP6B2|^TestP6ActualEd25519RotationApproverChainAndCriticalExtensions$|^TestP6ReleaseArtifactByteDriftFailsClosed$|^TestP6SelfConsistentAttackerBundleFailsAgainstEmbeddedRelease$|^TestP6PublicReleaseDeclarationsContainNoPrivateOrInstallationPayload$|^TestP6V1RotationHasNoMaterializedSuccessor$' \
  -count=1 -v -timeout 120s
PASS — 0.067s
```

```text
go test -overlay=/tmp/ananke_p6_contract12_r2_overlay.json \
  ./internal/repaircontract -run '^TestR2Probe' -count=1 -v -timeout 120s
PASS — 3.114s
```

The overlay independently exercised stale retained capabilities, 2029 release expiry, matched malformed authority, missing/duplicate/reordered manifest entries, and extension criticality/duplication.

```text
go test ./internal/repaircontract \
  -run '^TestP6A2ExecutableAcceptanceVectorRegistry$' -count=1 -v
PASS
```

All 91 registered vectors executed. Missing/duplicate/order/nil-runner enforcement remained active.

```text
go test ./internal/repaircontract -count=1 -timeout 300s
PASS — 1.338s

go test ./internal/repaircontract -count=10 -timeout 300s
PASS — 6.540s

go test -race ./internal/repaircontract -count=3 -timeout 300s
PASS — 18.507s

go vet ./internal/repaircontract
PASS — no output

gofmt -d internal/repaircontract/*.go
PASS — no output
```

External certificate checks confirmed Ed25519 root/leaf chains, exclusive validity windows, critical role/domain extensions, and exact equality between certificate-derived and standalone SPKIs. All four standalone repair/approver SPKIs were distinct.

The supplied B1 primary output was policy-rejected; its neutral retry reported green. The supplied B2 output timed out and contained no completed validation evidence. The independent commands above establish the current candidate behavior instead.

## Manifest integrity

Verified before and after review:

```text
sha256sum artifacts/omp/p6a/contract12-candidate-source-manifest-r2.json
e679f47efa6448f46b7cba9639f510f754643ed617423c2e240f720005d2d3d4
```

```text
jq -r '.files | length' artifacts/omp/p6a/contract12-candidate-source-manifest-r2.json
24
```

All 24 per-file `sha256sum -c` checks reported `OK` both times. No repository file was edited. This verdict does not imply runtime/storage acceptance or full P6 `DESIGN ACCEPT`.
