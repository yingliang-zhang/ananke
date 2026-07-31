Working...
Implemented P6 Slices 1–2 repair batch A1, limited to BLOCKER 1. No commit.

## Exact cause

`testdata/p6-contract-v1.json` was still the pre-A1 fixture. The new strict decoder correctly rejected it because it contained obsolete schema members:

- top-level synthetic `bundle_file_identity`;
- synthetic release pins for bundle identity, socket, journal, and runtime;
- old label-derived certificate/SPKI/artifact hashes;
- fake successor, effective time, cross-signature, and release-approval rotation fields;
- obsolete acceptance-vector IDs.

The current `ContractFixture` deliberately excludes those fields, so `DisallowUnknownFields` collapsed the mismatch to `controlled repair contract is invalid`. This was a stale generated oracle, not a decoder defect. Regenerating it from `CanonicalFixture()` produced a canonical fixture accepted by both `DecodeFixture` and `VerifyFixture`.

Also tightened the rotation-policy decoder: removed a custom nested unmarshaler that could have bypassed unknown-field rejection and replaced it with a fully explicit closed type.

## Implemented

- Added explicit, non-wildcard `go:embed` handling in `release_artifacts.go`.
- Embedded and verified ten public artifacts:
  - four certificate/SPKI DER inputs;
  - canonical public trust bundle;
  - supervisor policy and profile;
  - contract-release declaration;
  - closed rotation policy;
  - release manifest.
- Manifest binds exact hashes for the other nine artifacts.
- Verified:
  - Ed25519 root self-signature;
  - leaf chain to the embedded root;
  - exact certificate and SPKI DER;
  - issuer/subject relationship;
  - CA, certificate-signing, and digital-signature key usages;
  - exclusive certificate validity windows;
  - critical repair-role and signature-domain extensions;
  - separation from the P5 role and SPKI.
- Removed portable claims for device, inode, owner, mode, size, socket, journal, and runtime identities.
- Retained only an uninstantiated future descriptor-observation schema.
- Replaced the fake rotation instance with `no_successor_authorized`.
- Rotation policy now freezes exact proposal, cross-signature, and independent-approval record fields, roles, domains, signed objects, validity rules, activation delay, and overlap.
- Added byte-drift, certificate/SPKI/extension-drift, synthetic-descriptor, closed-policy, and attacker-bundle tests.
- Updated the canonical fixture and experiment document.
- No private-key material was accessed or embedded. The public-artifact payload guard passed.

Final identities:

```text
fixture_hash                      sha256:8349a260a9fe7fceaf117b40a0d5dac173720003b0844b9c02d3619a051a00a3
release_pins_hash                 sha256:bda296df00c9fc3280669ca4de65a193ff51b53d05b4232c143b7b5109cdde24
trust_bundle_hash                 sha256:2e83c327160f115d7c5f42404e7d105333c12cbac1ffd35489d1bf7bc1dd4ad1
repair_root_certificate_hash      sha256:d4120da501a600663c9cfc0933224d9d03c3f15a3b3b0700013be3a773da63e7
repair_root_spki_sha256           sha256:966d00620938ed9b453497cc099daf26dbfce4a68f1e726e9aefa24b53fb2147
repair_attestor_certificate_hash  sha256:94a8c72b0994040aa1b48f48e629de1415268db0ec8c0671cf06348c46455ea0
repair_attestor_leaf_spki         sha256:79392ee343c0e72e6433fefd50c6faff129ad9a4956b73f00c3bda266af8b493
release_manifest_hash             sha256:6a53929cdedccd593db24682456b52fbcdfd942d48670336906ace55284b6877
contract_release_hash             sha256:cd587e2ac69d2c1850c73b865935f87a311a7ccf1f36488fd8e4ef34bd663811
supervisor_policy_hash            sha256:70b707cbc4215c7ae2c1cad16193e82d03402f5d5c497bf858d267d90d399bce
supervisor_profile_hash           sha256:e6f8e2f330c52b8b7eed8d1003b1c5b22947369cb87ac01e6bd004dbcc6c0a2d
rotation_policy_hash              sha256:d8e042a5fc5b1d42fd56aa2bd29d5edf28a5db1311f81118c219e63b94df37dc
```

## RED evidence

Initial focused tests showed the compiled label-derived values did not match actual bytes. Examples:

```text
root certificate:
compiled sha256:00cb34d5c1cafaf48a2942d65a7be167bc0fce53b4be4d1571d2febe568c1556
actual   sha256:d4120da501a600663c9cfc0933224d9d03c3f15a3b3b0700013be3a773da63e7

root SPKI:
compiled sha256:19d4c933450f109f3bdd14e40e6650d338e81d178ec123649932ca46487b3f81
actual   sha256:966d00620938ed9b453497cc099daf26dbfce4a68f1e726e9aefa24b53fb2147

leaf certificate:
compiled sha256:6302212a3202d9a1b918b1046b17d9692656c6cbc790c681b073ae8f35e8afe6
actual   sha256:94a8c72b0994040aa1b48f48e629de1415268db0ec8c0671cf06348c46455ea0

leaf SPKI:
compiled sha256:7393781fd6872db1a2d848f432d94fefefe68f7eacc704a5f216e07f07b22576
actual   sha256:79392ee343c0e72e6433fefd50c6faff129ad9a4956b73f00c3bda266af8b493
```

RED also reported every synthetic descriptor fact and the old `release_approved_future_only` successor fixture. The exact signed-record test initially failed to compile because `RecordFields` did not yet exist; it passed after the policy/type correction.

## Exact GREEN outputs

```text
$ go test ./internal/repaircontract -count=1 -timeout 120s
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	2.310s
```

```text
$ go test ./internal/repaircontract -count=10 -timeout 300s
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	3.505s
```

```text
$ go test -race ./internal/repaircontract -count=3 -timeout 300s
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	20.656s
```

```text
$ go vet ./internal/repaircontract
<no output; exit 0>
```

```text
$ gofmt -w internal/repaircontract/contract.go internal/repaircontract/contract_test.go internal/repaircontract/release_artifacts.go
<no output; exit 0>

$ git diff --check
<no output; exit 0>
```
