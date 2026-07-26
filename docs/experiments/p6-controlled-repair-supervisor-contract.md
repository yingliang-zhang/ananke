# P6 controlled-repair supervisor contract: Slices 1–2

**Status:** pure contract freeze and canonical fixture oracle. This document does not claim the full P6 contract acceptance gate or `DESIGN ACCEPT`.

**Scope:** only trust bootstrap/role separation/rotation and GUI authorization/immutable dispatch. The artifacts are:

- `internal/repaircontract/contract.go` — closed Go schemas, release-pin comparisons, semantic verifier, and replay classifier;
- `internal/repaircontract/release_artifacts.go` — explicit `go:embed` public release oracle, manifest verification, and Ed25519 X.509 verification;
- `internal/repaircontract/canonical.go` — duplicate-aware strict JSON decoding and RFC 8785/JCS hashing;
- `internal/repaircontract/contract_test.go` and `a2_contract_test.go` — RED→GREEN contract vectors and public-artifact drift tests;
- `internal/repaircontract/acceptance_registry_test.go` — the separately frozen canonical vector-ID inventory and its ordered executable registry;
- `internal/repaircontract/testdata/release-v1/` — eight public DER certificate/SPKI files plus six canonical public declarations, for fourteen explicitly embedded public artifacts total;
- `internal/repaircontract/testdata/p6-contract-v1.json` — one canonical valid fixture with no decorative vector declarations;
- this document.

These artifacts implement **no storage, migration, process launch, adapter, test runner, sandbox, worktree, Unix socket, network transport, filesystem open, database trust bootstrap, signature generation, private-key operation, or runtime dispatch**. Descriptor/open, peer-credential, future rotation signature verification, outbox, and effect behavior are future implementation obligations. The values here are pure contracts and verifier inputs only.

## Canonical and closed-record rules

Every named record has an exact `schema_version` and an exact Go-typed key set. `DecodeFixture` rejects unknown members at every nesting level. It also rejects duplicate object keys at every nesting level, trailing JSON, a UTF-8 BOM, invalid UTF-8, lone UTF-16 surrogate escapes, Unicode noncharacters, malformed JSON, and any byte sequence that is not already its RFC 8785/JCS representation.

All fields ending in `_hash`, `_sha256`, or `_spki` are `sha256:` followed by exactly 64 lowercase hexadecimal digits, except that `previous_authorization_hash` is empty only for attempt 1. Every self-hashed record is hashed over its JCS object after deleting only that record's own hash member. Hash errors and unknown secret-looking members project only `controlled repair contract is invalid`; their names and values are not echoed.

UTC timestamps are canonical RFC 3339 strings ending in `Z`. Parsing must round-trip through `time.RFC3339Nano`, so impossible calendar dates, offsets, lowercase separators, and noncanonical fractional forms fail closed. Validity is always `valid_from <= now < not_after`.

The canonical fixture hash is:

```text
sha256:8349a260a9fe7fceaf117b40a0d5dac173720003b0844b9c02d3619a051a00a3
```

## Slice 1: release-pinned repair trust

`FrozenReleasePins()` is derived at package initialization from fourteen explicitly named `go:embed` public artifacts: eight DER certificate/SPKI files and six JSON declarations. It takes no database row, environment value, caller path, callback, or runtime verifier substitution. Wildcard embedding is not used. The manifest binds the exact bytes of thirteen artifacts and is the fourteenth embedded artifact; the compiled release-pins record binds the manifest itself:

```text
release_pins_hash                         sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a
trust_bundle_hash                         sha256:28519f0022034ed6c47b011351ce80cb0f42594f4df2ebe5f4a820678d169786
repair_root_certificate_hash              sha256:d4120da501a600663c9cfc0933224d9d03c3f15a3b3b0700013be3a773da63e7
repair_root_spki_sha256                   sha256:966d00620938ed9b453497cc099daf26dbfce4a68f1e726e9aefa24b53fb2147
repair_attestor_certificate_hash          sha256:94a8c72b0994040aa1b48f48e629de1415268db0ec8c0671cf06348c46455ea0
repair_attestor_root_id                   ananke_controlled_repair_attestation_root_v1
repair_attestor_leaf_spki                 sha256:79392ee343c0e72e6433fefd50c6faff129ad9a4956b73f00c3bda266af8b493
rotation_approver_root_certificate_hash   sha256:a90cfe559f5e5b2ccfee3618212849c6129cfce440c43f288835f9d2740f42f1
rotation_approver_root_spki_sha256        sha256:2e1d84cae2dd6f87de7090205af7a900436be99d5ff1a0868762896622fa834c
rotation_approver_certificate_hash        sha256:a428af51ea351d9e34cb6f139539bd2a2d9edbfc5acc519915c5323283e41180
rotation_approver_root_id                 ananke_controlled_repair_rotation_approver_root_v1
rotation_approver_key_id                  ananke_controlled_repair_rotation_release_approver_v1
rotation_approver_leaf_spki               sha256:95b82df9b281943f2229a0ab6d830c4b0133eda9bcec9eee8cffdd4f82db1d1d
rotation_approver_role                    controlled_repair_rotation_release_approver
rotation_approver_domain                  ananke.controlled-repair.root-rotation-release-approval.v1
release_manifest_hash                     sha256:09918769192b68de8f9f9e13c7281f37ecc24231fbce89b9bd3dce9f5fb0c095
build_identity_hash                       sha256:cd587e2ac69d2c1850c73b865935f87a311a7ccf1f36488fd8e4ef34bd663811
supervisor_policy_hash                    sha256:70b707cbc4215c7ae2c1cad16193e82d03402f5d5c497bf858d267d90d399bce
supervisor_profile_hash                   sha256:e6f8e2f330c52b8b7eed8d1003b1c5b22947369cb87ac01e6bd004dbcc6c0a2d
rotation_policy_hash                      sha256:069e8492a9b2bd1ac9ac6f62f2e4f9946a5004e5236349b32aa334a153d75a24
signature_domain                          ananke.controlled-repair.review-attestation.v1
```

Every hash above is the SHA-256 of actual checked-in artifact bytes or, for `release_pins_hash`, the JCS release-pins record. The public trust-bundle declaration carries base64 of the exact eight standalone certificate DER and SPKI DER files. The thirteen manifest entries bind these artifact bytes in this exact order; the manifest does not list itself:

1. `contract_release` — `sha256:cd587e2ac69d2c1850c73b865935f87a311a7ccf1f36488fd8e4ef34bd663811`
2. `public_trust_bundle` — `sha256:28519f0022034ed6c47b011351ce80cb0f42594f4df2ebe5f4a820678d169786`
3. `repair_attestor_certificate_der` — `sha256:94a8c72b0994040aa1b48f48e629de1415268db0ec8c0671cf06348c46455ea0`
4. `repair_attestor_spki_der` — `sha256:79392ee343c0e72e6433fefd50c6faff129ad9a4956b73f00c3bda266af8b493`
5. `repair_root_certificate_der` — `sha256:d4120da501a600663c9cfc0933224d9d03c3f15a3b3b0700013be3a773da63e7`
6. `repair_root_spki_der` — `sha256:966d00620938ed9b453497cc099daf26dbfce4a68f1e726e9aefa24b53fb2147`
7. `rotation_approver_certificate_der` — `sha256:a428af51ea351d9e34cb6f139539bd2a2d9edbfc5acc519915c5323283e41180`
8. `rotation_approver_spki_der` — `sha256:95b82df9b281943f2229a0ab6d830c4b0133eda9bcec9eee8cffdd4f82db1d1d`
9. `rotation_approver_root_certificate_der` — `sha256:a90cfe559f5e5b2ccfee3618212849c6129cfce440c43f288835f9d2740f42f1`
10. `rotation_approver_root_spki_der` — `sha256:2e1d84cae2dd6f87de7090205af7a900436be99d5ff1a0868762896622fa834c`
11. `rotation_policy` — `sha256:069e8492a9b2bd1ac9ac6f62f2e4f9946a5004e5236349b32aa334a153d75a24`
12. `supervisor_policy` — `sha256:70b707cbc4215c7ae2c1cad16193e82d03402f5d5c497bf858d267d90d399bce`
13. `supervisor_profile` — `sha256:e6f8e2f330c52b8b7eed8d1003b1c5b22947369cb87ac01e6bd004dbcc6c0a2d`

Tests reject byte drift and missing, duplicate, extra, or reordered manifest entries.

The exact modes, themselves parsed from and bound to the supervisor-policy bytes, are:

```text
trust_bootstrap_mode  release_pinned_only
database_trust_mode   mirror_only_no_replacement
runtime_install_mode  forbidden
verifier_selection    compiled_embedded_release_artifacts_only
ananke_key_custody    public_material_only_no_private_key
```

The portable release pins contain no device, inode, owner, mode, size, socket identity, journal identity, or installation-specific runtime identity. `BundleDescriptorObservation` is only a future no-follow observation schema; no compiled fixture instantiates it. The supervisor policy requires a no-follow regular-file observation, owner/mode checks, stable device/inode during use, and an exact content-hash match without pinning machine-local values.

The release contains two independent Ed25519 X.509 chains: the repair release root (`Ananke Controlled Repair Release Root v1`) signs the review-attestor leaf (`Ananke Controlled Repair Review Attestor v1`), while the rotation-approver root (`Ananke Controlled Repair Rotation Approver Root v1`) signs the rotation release-approver leaf (`Ananke Controlled Repair Rotation Release Approver v1`). For both chains, verification requires PureEd25519 signatures and Ed25519 public keys, an exact self-signed root and exact root-to-leaf issuer/subject chain, equality between certificate-derived SPKI and its separately embedded SPKI DER, a CA root with valid basic constraints, path length 1, and exactly digital-signature plus certificate-signing key usage, and a non-CA leaf with exactly digital-signature key usage. Each root has the exclusive validity window `2026-07-01T00:00:00Z <= now < 2031-07-01T00:00:00Z`; each leaf has `2026-07-01T00:00:00Z <= now < 2028-07-01T00:00:00Z`.

Each leaf must contain exactly one critical role extension OID `1.3.6.1.4.1.57264.1.6` and exactly one critical signature-domain extension OID `1.3.6.1.4.1.57264.1.7`; neither root may contain either extension. The repair leaf values are `controlled_repair_review_attestor` and `ananke.controlled-repair.review-attestation.v1`. The rotation-approver leaf values are `controlled_repair_rotation_release_approver` and `ananke.controlled-repair.root-rotation-release-approval.v1`. The repair root, repair leaf, approver root, and approver leaf standalone SPKIs are all distinct; the two leaves are also distinct from the P5 protocol-adapter SPKI, and the two roles are distinct from each other and from the protocol-adapter role.

The public declarations contain no private material, secret, filesystem path, executable, argv, environment, or installation descriptor fact. A self-consistent attacker bundle and correspondingly rehashed manifest/pins remain rejected because they cannot change the explicit embedded release oracle.

### Rotation

The V1 fixture rotation state is exactly `no_successor_authorized`. Its closed record contains only the current-root ID, exact release-manifest and rotation-policy hashes, and its own record hash. It contains no successor ID/SPKI/certificate, activation instant, cross-signature, independent approval, or invented signature bytes. No active current-root cross-signature or independent release-approval instance exists in V1.

The embedded `ananke.controlled-repair-trust-rotation-policy.v1` declaration freezes the future protocol without authorizing an instance:

- proposal schema `ananke.controlled-repair-trust-rotation-proposal.v1` and its exact ordered fields bind current and successor root certificate/SPKI identities, successor `not_before`/`not_after`, current-root `not_after`, and release manifest;
- current-root cross-signature schema `ananke.controlled-repair-current-root-cross-signature.v1`, role `controlled_repair_current_root`, domain `ananke.controlled-repair.root-rotation-cross-signature.v1`, exact record fields, and JCS signed object `[signature_domain, proposal_hash]`;
- independent approval schema `ananke.controlled-repair-independent-release-approval.v1`, fixed role `controlled_repair_rotation_release_approver`, fixed domain `ananke.controlled-repair.root-rotation-release-approval.v1`, fixed key ID `ananke_controlled_repair_rotation_release_approver_v1`, and fixed signer SPKI `sha256:95b82df9b281943f2229a0ab6d830c4b0133eda9bcec9eee8cffdd4f82db1d1d` under approver root `ananke_controlled_repair_rotation_approver_root_v1`; its exact record fields bind that released signer identity, require approver-SPKI separation from current and successor roots, and define the JCS signed object `[signature_domain, proposal_hash, current_root_cross_signature_hash, approved_at]`;
- Ed25519 signatures use raw 64-byte base64 encoding; activation requires both valid signatures, exact release-manifest binding, and a successor certificate valid at activation;
- `successor_not_before >= approved_at + 86400s`, both `not_after` instants are exclusive, and `current_root_not_after >= successor_not_before + 86400s` supplies the minimum overlap.

Every normal authorization scope has `rotation_mode: forbidden_v1`. A normal request cannot install, select, activate, or rotate a root. TOFU, database replacement, runtime installation, caller-selected verification, and permissive verification remain invalid.

## Slice 2: GUI authorization

The approval record binds:

- operator identity `local_gui_operator` and its exact identity hash;
- provenance `authenticated_local_gui_session` and its exact provenance hash;
- decision `approved`;
- the complete approved-scope hash;
- `approved_at` and exclusive `not_after`.

The verifier takes an explicit `now`; it does not read the clock. The constants are deliberately small for a local GUI gesture:

```text
MaxApprovalAge           5 minutes, inclusive
MaxAuthorizationLifetime 10 minutes, inclusive
MaxDispatchLifetime      4 minutes, inclusive
```

At admission and effect time, `approved_at <= now < not_after` and `now - approved_at <= MaxApprovalAge`. Authorization lifetime may equal 10 minutes but may not exceed it. At effect time, `created_at <= now < dispatch_not_after` also holds. `not_after` and `dispatch_not_after` themselves are always expired instants. The tests cover each bounded maximum at `N-1ns`, `N`, and `N+1ns`, plus a year-old approval and admission-before-expiry/effect-after-expiry.

`VerifyAuthorization` and freshness-enforcing `DecodeDispatch` each verify the compiled release pins, trust bundle, and rotation at their supplied `now`. The embedded certificate `not_after` is exclusive, so authorization and effect fail at `N`, `N+1ns`, and later instants even when authorization and dispatch timestamps are otherwise fresh.

### Complete approved scope

The authorization scope carries the full P4 identity rather than a projection:

- P4 verifier input hash `sha256:c7d9a26636b16df70d77d443a37df7c91d640731c1dbbb9ad339990cd9b77eb8`;
- P4 evidence-bundle hash `sha256:12ec67830ffa00eb637ed0594b46b89be79c28cce3854574f540f9dc2b6a5c0d`;
- P4 admission hash `sha256:54446404a8e615d1abf63abd396b303ae86047be14a1eeeaabb6176c2d9deedb`;
- complete fence tuple: claim `p4_repair_fence_claim_001`, claim-token hash `sha256:7506737a97ecf137840f1f6ec0c2c9c210733fc35751fcda967a75dfe084eacd`, and generation `8`;
- attempt number and cap, repeated consistently through scope, P4 binding, request, and dispatch.

Repository binding is exact:

```text
repository identity  github.com/yingliang-zhang/ananke
base commit           7a1f7ce102f6611a6f4ddbd6ee45263f211e9588
base tree             9b5f88f170846bf4b5fc7595f53344f993bfde12
```

The fixture carries no raw path strings. Its ordered path IDs and repository-relative path hashes bind these exact approved paths:

1. `authorized_source_member_1` → `internal/lifecycle/backend.go` → `sha256:d19d9fbf6155b5dacc1f8f25fe2c51c3344ca1d43d2c4718d55ef3a981d9b257`
2. `authorized_source_member_2` → `internal/lifecycle/engine.go` → `sha256:de82e0e736b370c729ed4918ed195e97cfeb191d29b2774f19583f1615ea5b5f`

The supervisor-installed test-profile list is ordered and contains identities only:

1. `go_unit_offline_v1` / `sha256:9f4550ebcbaacd08ea6219b47309c5130617610eab869c783bb1c89091b10f71`
2. `go_race_offline_v1` / `sha256:377955353579968643681799fe71bb44647472bb55b6f0dd229c3ccd13298868`

No test profile field can carry an executable, argv, environment, working directory, cache, or raw path. The profile definitions and any execution are later-slice work.

The route is `controlled_repair_local_supervisor_v1`, route identity `sha256:0e1e34e671d9852ce4ac5654613b832c539b58999b533ddccea24d341c47e95f`, and supervisor profile `controlled_repair_supervisor_local_v1`. Authorization also binds the release-pinned policy/profile hashes, channel-binding hash, and full expected Unix peer identity. The peer identity contains fixed UID/GID, code-signing, executable, and runtime hashes; it has no PID or socket pathname.

Full-fence validation requires the exact `ananke.controlled-repair-full-fence.v1` schema, valid fence and claim-token hashes, a positive generation, and a closed claim ID. Claim IDs, route IDs, supervisor-profile IDs, and peer roles use ASCII `^[a-z0-9]+(?:_[a-z0-9]+)*$` with an inclusive 1–128-byte bound. Repository identities use ASCII `^[a-z0-9]+(?:[.-][a-z0-9]+)*(?:/[a-z0-9]+(?:[-._][a-z0-9]+)*)+$` with an inclusive 256-byte maximum (the grammar's minimum form is `a/a`). Both the external authority context and authorization record enforce these rules.

Attempts are exactly `1..2` with cap exactly `2`. Attempt 1 has no predecessor authorization. Attempt 2 carries the prior authorization hash and produces a different approved-scope, approval, authorization, request, and dispatch hash. A replay cannot turn attempt 1 authorization into attempt 2 authority.

## Immutable dispatch and replay

`ananke.controlled-repair-immutable-dispatch.v1` binds authorization, approval, policy, attempt/cap, request ID/hash, channel-binding hash, expected Unix peer, compiled release-pins hash, selected supervisor policy/profile, `created_at`, and `dispatch_not_after`.

The request and dispatch each have independent self-hashes. Every duplicated identity must equal the authorization scope and compiled release pins. Dispatch creation must occur inside authorization validity; dispatch expiry cannot exceed authorization expiry.

`ClassifyDispatchReplay` has only two results:

- byte-identical immutable value → `exact_replay`, with no new dispatch identity;
- any changed request, authorization, attempt, channel, peer, policy, profile, or deadline → `conflict` and `ErrDispatchConflict`.

Replay classification intentionally remains clock-independent so an expired byte-identical record can still be identified. It is not effect authority: every admission or effect must pass freshness-enforcing `DecodeDispatch` and current compiled-release verification.

The records contain no process/effect authority. In particular, the closed fixture has no command, executable, argv, environment, credential, secret, private key, PID, process, raw path, or socket-path member.

## Closed schema inventory

The fixture uses these exact schema versions:

| Record | Schema |
| --- | --- |
| fixture | `ananke.controlled-repair-contract-fixture.v1` |
| release pins | `ananke.controlled-repair-release-pins.v1` |
| trust root | `ananke.controlled-repair-trust-root.v1` |
| repair certificate | `ananke.controlled-repair-attestor-certificate.v1` |
| rotation approver certificate | `ananke.controlled-repair-rotation-approver-certificate.v1` |
| trust bundle projection | `ananke.controlled-repair-trust-bundle.v1` |
| no-successor rotation state | `ananke.controlled-repair-trust-rotation.v1` |
| full fence | `ananke.controlled-repair-full-fence.v1` |
| P4 binding | `ananke.controlled-repair-p4-binding.v1` |
| repository binding | `ananke.controlled-repair-repository-binding.v1` |
| writable-path binding | `ananke.controlled-repair-writable-path-binding.v1` |
| test-profile binding | `ananke.controlled-repair-test-profile-binding.v1` |
| route binding | `ananke.controlled-repair-route-binding.v1` |
| attempt binding | `ananke.controlled-repair-attempt-binding.v1` |
| Unix peer identity | `ananke.controlled-repair-unix-peer-identity.v1` |
| authorization scope | `ananke.controlled-repair-authorization-scope.v1` |
| operator approval | `ananke.controlled-repair-operator-approval.v1` |
| authorization | `ananke.controlled-repair-authorization.v1` |
| dispatch request | `ananke.controlled-repair-dispatch-request.v1` |
| immutable dispatch | `ananke.controlled-repair-immutable-dispatch.v1` |
| acceptance vector | `ananke.controlled-repair-acceptance-vector.v1` |

The embedded public declarations additionally freeze `ananke.controlled-repair-public-trust-bundle.v1`, `ananke.controlled-repair-supervisor-policy.v1`, `ananke.controlled-repair-descriptor-observation-requirements.v1`, `ananke.controlled-repair-supervisor-profile.v1`, `ananke.controlled-repair-contract-release.v1`, `ananke.controlled-repair-release-manifest.v1`, and `ananke.controlled-repair-trust-rotation-policy.v1`. The rotation policy reserves the proposal, current-root cross-signature, and independent release-approval schemas listed above; no instance of those three future schemas is active in V1.

## Ordered acceptance-vector inventory

The fixture carries no acceptance-vector declarations or claimed booleans. `canonicalAcceptanceVectorIDs` separately freezes the exact 91-ID order below, and `executableAcceptanceVectorRegistry` maps every ID to its named positive or negative behavior. The executable registry test is `TestP6A2ExecutableAcceptanceVectorRegistry`; it invokes all 91 entries, records actual execution order, rejects missing, duplicate, renamed, or nil entries, compares the executed IDs with the canonical inventory, and checks each entry's expected sentinel result. Semantic mutation probes recompute their complete linked self-hash chains; the attacker-root probe also rebinds `Dispatch.ReleasePinsHash` and recomputes the dispatch and fixture hashes before the compiled release boundary rejects it.

1. `duplicate_key`
2. `unknown_key_every_nesting_level`
3. `trailing_data`
4. `invalid_utf8`
5. `lone_unicode_surrogate`
6. `unicode_noncharacter`
7. `malformed_hash`
8. `noncanonical_hash`
9. `invalid_utc_timestamp`
10. `self_consistent_attacker_trust_bundle_root`
11. `release_pin_mismatch`
12. `protocol_adapter_leaf_reused`
13. `wrong_repair_role`
14. `wrong_repair_leaf_spki`
15. `wrong_repair_root`
16. `wrong_repair_bundle`
17. `revoked_certificate`
18. `stale_certificate`
19. `future_certificate`
20. `changed_rotation_approver_root_certificate`
21. `changed_rotation_approver_root_spki`
22. `changed_rotation_approver_leaf_certificate`
23. `changed_rotation_approver_leaf_spki`
24. `wrong_rotation_approver_role`
25. `wrong_rotation_approver_domain`
26. `expired_rotation_approver_certificate`
27. `future_rotation_approver_certificate`
28. `repair_root_reused_for_rotation_approver`
29. `repair_leaf_reused_for_rotation_approver`
30. `rotation_approval_signer_id_mismatch`
31. `rotation_approval_signer_spki_mismatch`
32. `tofu_mode`
33. `database_replacement_mode`
34. `runtime_install_mode`
35. `permissive_verifier_mode`
36. `synthetic_descriptor_facts`
37. `year_old_approval`
38. `overlong_lifetime`
39. `approval_age_n_minus_1`
40. `approval_age_n`
41. `approval_age_n_plus_1`
42. `lifetime_n_minus_1`
43. `lifetime_n`
44. `lifetime_n_plus_1`
45. `admitted_then_expired_dispatch`
46. `admitted_then_expired_effect`
47. `swapped_p4_input`
48. `swapped_p4_bundle`
49. `swapped_p4_admission`
50. `foreign_fence_schema`
51. `empty_fence_claim_id`
52. `invalid_fence_claim_id`
53. `zero_fence_generation`
54. `negative_fence_generation`
55. `empty_repository_identity`
56. `invalid_repository_identity`
57. `empty_route_id`
58. `invalid_route_id`
59. `empty_supervisor_profile_id`
60. `invalid_supervisor_profile_id`
61. `empty_peer_role`
62. `invalid_peer_role`
63. `swapped_fence_claim`
64. `swapped_fence_token`
65. `swapped_fence_generation`
66. `swapped_repository`
67. `swapped_base_commit`
68. `swapped_base_tree`
69. `swapped_writable_path`
70. `swapped_test_profile`
71. `swapped_route`
72. `swapped_channel`
73. `swapped_peer`
74. `swapped_policy`
75. `duplicate_writable_path`
76. `duplicate_test_profile`
77. `attempt_zero`
78. `attempt_one`
79. `attempt_two`
80. `attempt_three`
81. `attempt_cap_not_two`
82. `normal_request_rotation`
83. `unmaterialized_rotation_successor`
84. `unmaterialized_rotation_cross_signature`
85. `unmaterialized_rotation_release_approval`
86. `duplicate_dispatch`
87. `conflicting_dispatch`
88. `restart_exact_replay`
89. `restart_conflicting_replay`
90. `unknown_secret_field_redacted_diagnostic`
91. `fixture_has_no_authority_payload`

## Gate and remaining work

The focused package tests prove only these pure Slices 1–2 records, canonical bytes, semantic relations, and in-memory mutations. No P6 store, outbox persistence, transport, supervisor, signature, sandbox, process, filesystem effect, or migration is implemented or claimed. Contract Slices 3–9, a fresh independent hard review, and the plan's full `DESIGN ACCEPT` gate remain separate prerequisites before runtime or storage implementation.
