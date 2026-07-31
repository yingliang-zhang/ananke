Working...
All tasks complete. Here is the full independent hard review report.

---

# P6 Contract Slice 4 — Independent Fresh Hard Review (Repair C + Repair P1/P2)

## Frozen source verification

### Initial verification

- Manifest: `artifacts/omp/p6a/slice4-candidate-source-manifest.json`, schema `ananke.frozen-source-manifest.v1` (field `schema_version`).
- Manifest schema-version field: `ananke.frozen-source-manifest.v1` ✓
- Entries: **36** ✓
- Aggregate bytes: **686,977** ✓
- Duplicate paths: **0** ✓
- Non-regular files: 0 ✓
- Size mismatches: 0 ✓
- SHA-256 mismatches: 0 ✓
- Symlink components: 0 ✓
- Manifest digest: `sha256:6530b7c73e20f29c575d61cd7f39f3ad26e47bbe79f61e42d1f2be3c8fed0335` — **MATCHED** ✓
- `git status --short` scoped to manifest members: exactly **6 untracked** Slice-4 files; all other 30 members match HEAD `74fd451` (0 modified, 0 staged, 0 extra untracked).
- The 6 untracked files:
  - `internal/repaircontract/repository_worktree.go`
  - `internal/repaircontract/repository_worktree_test.go`
  - `internal/repaircontract/repository_worktree_registry_test.go`
  - `internal/repaircontract/repository_worktree_document_test.go`
  - `internal/repaircontract/repository_worktree_repair_c_test.go`
  - `docs/experiments/p6-controlled-repair-repository-authority.md`

### Post-review verification and drift

- Re-run after all review activity: 36 entries, 686,977 bytes, 0 path/type/size/hash problems, manifest digest `sha256:6530b7c73e20f29c575d61cd7f39f3ad26e47bbe79f61e42d1f2be3c8fed0335` **MATCHED**.
- **Drift count: 0** (all 6 files still untracked, all 30 tracked members still match HEAD).
- No repository file was edited; no files created inside the repo. Ephemeral probes were under `/tmp` only and cleaned up.

---

## Findings

### P0 — None

### P1 — None (prior P1 fully repaired)

The rejected review found two P1 issues: (a) seven predictable `fixedHash("trusted-<kind>-verification-"+observationHash)` labels forgeable by same-package code, and (b) production `mintVerifiedRepositoryWorktree` minting an intact capability from an unverified snapshot. Both are repaired and verified:

**Repair (a) — verification seals replace predictable labels:** `RepositoryWorktreeVerificationSeal` (lines 196-204) records (kind, verifier authority hash, observation hash, canonical hash, evidence hash) and is self-hashed via `mustHashRecord(seal, "seal_hash")` (line 1110). The evidence hash is `repositoryWorktreeSealEvidenceHash` (lines 1058-1095), a `sha256(canonicalBytes(evidence))` over kind-specific evidence structs, not a predictable label. `verifiedRepositoryWorktreeSnapshotIntact` (lines 1254-1276) recomputes all seven seals from the decoded observation + booleans + evaluator-derived authority and requires exact match — a same-package forger who recomputes seals for a forged observation with mutated HEAD/ORIG_HEAD/commondir content still fails `exactRepositoryWorktreeClosure` because `exactCommonGitMemberClosure` (lines 1370-1415) enforces `member.ContentHash == sha256(base+"\n")` for HEAD/ORIG_HEAD and `== sha256("../..\n")` for commondir.

**Adversarial reproduction (P1a):** Ephemeral overlay probe `TestP6Slice4ReviewForgedSelfConsistentSnapshotRejected` mutated all three authorization-derivable member contents to invented values, rehashed the full observation chain, recomputed all seven seals + integrity, and called `EvaluateRepositoryWorktree`. **Result: REJECTED** (`ErrInvalidRepositoryWorktree`, nil capability). ✓

**Repair (b) — production minter deleted, inline-only construction:** `grep` confirms no `mintVerifiedRepositoryWorktree` function exists anywhere in the package. There is exactly **one** `&VerifiedRepositoryWorktree{` construction site in production code: line 566 inside `EvaluateRepositoryWorktree`'s `new_exact` branch, after fresh release trust, verifier-authority equality, installation intactness, snapshot seal recomputation, uniqueness booleans, exact closure, and the admit action all pass. The comment at lines 561-564 states this explicitly.

**Adversarial reproduction (P1b):** Overlay probe `TestP6Slice4ReviewNoProductionMinter` verified: (1) the canonical path produces an intact capability, (2) tampering `verifierAuthorityHash` + recomputing integrity → `verifiedRepositoryWorktreeIntact` fails, (3) tampering `verificationSealsHash` + recomputing integrity → fails, (4) tampering canonical bytes → fails (canonicalHash ≠ sha256(canonical)), (5) tampering observationHash → fails (recomputed from canonical won't match). ✓

**Adversarial reproduction (P1c):** Overlay probe `TestP6Slice4ReviewTamperedSnapshotFailsIntact` verified: tampered `verifierAuthorityHash` → `verifiedRepositoryWorktreeSnapshotIntact` fails, swapped seal → fails (recomputation mismatch), tampered canonical → fails (hash mismatch), tampered embedded observation → fails (decoded canonical ≠ embedded observation). ✓

### P2 — None (prior P2 fully repaired)

The rejected review found `RepositoryWorktreeInstallationAuthority` was only syntax-checked with no authorization or release-policy binding, and no explicit Git 2.54 identifier. Both are repaired:

**Git 2.54 profile frozen:** `RepositoryWorktreeMaterializerProfile` (lines 154-175) is a compiled struct with `MaterializerProfileID = "installed_git_2_54_detached_worktree_materializer_v1"` and `GitVersion = "2.54"` (lines 35-36). `deriveRepositoryWorktreeMaterializerProfile` (lines 919-946) sets all 16 boolean policy fields to `true` and self-hashes. `mustDeriveRepositoryWorktreeMaterializerProfile` (lines 911-917) panics on self-hash mismatch at init.

**Verifier authority release-pinned:** `RepositoryWorktreeVerifierAuthority` (lines 180-188) binds `VerifierID`, `MaterializerProfileID/Hash`, `ReleasePinsHash` (from `FrozenReleasePins().ReleasePinsHash`), and the 7 closed verification kinds. `deriveRepositoryWorktreeVerifierAuthority` (lines 956-974) requires `profile == FrozenRepositoryWorktreeMaterializerProfile()`. `deriveFrozenRepositoryWorktreeVerifierAuthority` (lines 978-984) re-derives and requires `reflect.DeepEqual(derived, FrozenRepositoryWorktreeVerifierAuthority())`.

**Installation binding:** `VerifyRepositoryWorktreeInstallation` (lines 1160-1184) requires: fresh `VerifyReleaseTrust`, `deriveFrozenRepositoryWorktreeVerifierAuthority` equality, `verifiedAuthorizationIntact`, `validateSupervisorIntentAuthority`, profile ID+hash == frozen Git 2.54 profile, `WorktreeSlotID == deriveRepositoryWorktreeSlotID(expected.AttemptNumber)`, `WorktreeSlotPathHash == deriveRepositoryWorktreeSlotPathHash(WorktreeSlotID)`. `verifiedRepositoryWorktreeInstallationIntact` (lines 1211-1225) re-checks all of these plus the authorization hash and attempt hash match.

**Adversarial reproductions (P2):** Overlay probes verified all reject:
- Wrong profile ID → REJECT ✓
- Wrong profile hash → REJECT ✓
- Invented slot (`attempt_1_materialization_worktree_002`) → REJECT ✓
- Wrong attempt's slot (attempt-2 slot under attempt-1 authority) → REJECT ✓
- Wrong slot path hash → REJECT ✓
- Stale release (release NotAfter + 1ns) → REJECT ✓; fresh (−1ns) → ACCEPT ✓
- Cross-authorization installation (minted under auth B, used under auth A) → REJECT at evaluation ✓
- Installed root identity aliasing each of 5 descriptor no-follow identities → REJECT ✓
- Installed root identity aliasing descriptor canonical path → REJECT ✓

### P3 — None (Repair C accepted, no regression)

Repair C closed the rejected review's P3 finding. All six previously-open enums are now closed via switch-case validation functions: `validRepositoryWorktreeAmbiguityReason` (lines 679-691), `validRepositoryDescriptorID` (lines 693-702), `validCommonGitMemberID` (lines 708-716), `validCommonGitMemberSemantic` (lines 718-726), `validCommonGitProtectedDomainID` (lines 732-743). The orphan state `orphaned_status_only` with reason `orphaned_materialization` is a distinct status-only state (lines 55, 67, 607-615). The `validRepositoryWorktreeStateReason` function (lines 662-673) enforces all state/reason combinations. The Repair C test file (`repository_worktree_repair_c_test.go`) generates 76 vectors: 12 enum-unknown (decode+evaluation), 48 state×reason combinations, 1 orphan status, 12 orphan action rejections, 3 orphan freshness boundaries. Unknown enums reject at decode for all 6 enum types ✓.

### P4 — None

---

## Closure evidence per category

**Installation authority:** `VerifyRepositoryWorktreeInstallation` binds the installed record to fresh release trust, derived-equals-frozen verifier authority, intact verified authorization, exact frozen Git 2.54 profile (ID+hash), frozen slot grammar `attempt_<N>_materialization_worktree_001`, and `WorktreeSlotPathHash == sha256("worktrees/"+slotID)`. `verifiedRepositoryWorktreeInstallationIntact` re-establishes the integrity chain plus all frozen bindings. Cross-authorization and slot-reuse-across-attempts are rejected. ✓

**Opaque evidence boundary:** `VerifiedRepositoryWorktreeSnapshot` is opaque with all-private fields (lines 399-424). `verifiedRepositoryWorktreeSnapshotIntact` recomputes all seven seals from decoded canonical + booleans + evaluator-derived authority, verifies canonicalHash == sha256(canonical), and verifies decoded observation == embedded observation. No production constructor or decoder-from-caller-bytes exists. The test-only `mintRepositoryWorktreeSnapshotForTest` deep-copies canonical bytes. ✓

**Enum closure:** All 11 enum domains are closed via switch-case validation: worktree states (4), delta states (3), descriptor object kinds (2), descriptor IDs (5), member IDs (6), member semantics (6), protected domain IDs (14), candidate head modes (2), candidate statuses (2), ambiguity reasons (11), verification kinds (7). Unknown values reject at decode. ✓

**Descriptor closure:** `exactRepositoryDescriptorClosure` (lines 1323-1352) requires 5 ordered descriptor roles with correct IDs and kinds, pairwise-distinct no-follow identities, and pairwise-distinct canonical path hashes. ✓

**Exact common-`.git` delta:** `exactCommonGitMemberClosure` (lines 1370-1415) requires 6 ordered members with exact IDs, path hashes (`sha256(paths[index])`), semantics, and semantic targets. HEAD/ORIG_HEAD/commondir content hashes must equal `sha256(base+"\n")` and `sha256("../..\n")` (authorization-derivable). gitdir/index/logs/HEAD are verifier-attested evidence (documented non-claim). `exactCommonGitProtectedDomainClosure` (lines 1424-1441) requires 14 ordered unchanged protected domains. ✓

**Config/attributes closure:** `exactRepositoryConfigClosure` (lines 1500-1507) requires common config unchanged, all 9 disabled flags, and materializer profile match. `exactRepositoryAttributesClosure` (lines 1509-1524) requires system attributes disabled, info attributes/exclude unchanged, base-tree gitattributes verified, and no external filter/process/command attributes. ✓

**Candidate/source/paths closure:** `exactRepositoryCandidateClosure` (lines 1443-1451) requires detached HEAD at base commit, clean status, no ref updates, gitfile-admin crosslinks. `exactRepositorySourceClosure` (lines 1453-1461) requires source unchanged, candidate exact child/new, no aliases. `exactRepositoryWritablePathClosure` (lines 1463-1498) requires ordered path match with authorization set, all under candidate, no-follow verified, no symlinks/hardlinks/escapes/duplicates/case-fold/Unicode collisions. ✓

**Retention/recovery/forbidden effects:** Retained exact replay is status-only (no capability). All 10 non-orphan ambiguity reasons produce `waiting_for_human` with no capability. Orphaned materialization is a distinct status-only state. All 12 forbidden actions (cleanup, delete, prune, remove, ref_update, commit, push, merge, launch, second_worktree, second_effect, and unknown) are rejected. `EffectAllowed` is always `false`. ✓

**Freshness and Slices 1–3 binding:** `EvaluateRepositoryWorktree` re-establishes fresh `VerifyReleaseTrust`, requires exact phase-1 materialization `SupervisorIntentAuthority` (Phase == MaterializationClaimPhase, Sequence == 1), intact `VerifiedAuthorization`, intact `VerifiedSupervisorIntentClaim`, and claim-freshness boundaries at nanosecond precision. The 91-vector prior registry and 99-vector Slice-3 registry counts are unchanged in the doc. ✓

**Canonical/document closure:** The document test (`TestP6Slice4NormativeDocumentMatchesTypesFixtureAndInventory`) parses the machine block with `DisallowUnknownFields()`, deep-equals all fields against compiled constants and canonical fixture recomputation. 258 unique vector IDs. Verifier authority hash, profile hash, release pins hash, slot grammar, fixture hashes all match. ✓

**Pure-contract scope:** No filesystem access, descriptor open, Git execution, process launch, network operation, store/SQLite operation, cleanup, deletion, prune, worktree creation, ref update, commit, push, merge, adapter launch, or automatic repair anywhere in production code. All effects are classification-only with `EffectAllowed == false`. ✓

---

## Determinism criticism assessment

The seals are deterministic over (frozen verifier authority, frozen release, observation). Same-package code can recompute them. The contract's residual claim — "Seals name provenance and bind verifier, release, and observation; they do **not** claim that a trusted verifier physically executed; production snapshot minting remains future trusted runtime work with no production snapshot or capability minter in this slice" — is implemented **exactly as documented**:

- Code comment on `RepositoryWorktreeVerificationSeal` (lines 190-195): "It names provenance only: it does not claim that a trusted verifier physically executed, and production snapshot minting remains future trusted runtime work with no production minter in this slice." ✓
- Code comment on `VerifiedRepositoryWorktreeSnapshot` (lines 396-398): "opaque evidence from a future trusted no-follow descriptor/Git verifier. No production constructor or decoder from caller bytes exists in this slice." ✓
- Doc non-claims section (lines 484-487): "Verification seals name provenance and bind verifier, release, and observation; they do not claim that a trusted verifier physically executed. Production snapshot minting remains future trusted runtime work, and no production snapshot or capability minter exists in this slice." ✓
- No "unforgeable", "tamper-proof", "write-protection", or "impossible to forge" claims appear anywhere in code or doc. ✓
- The residual protection on the `new_exact` path is the authorization-derivable content constraints (`sha256(base+"\n")` for HEAD/ORIG_HEAD, `sha256("../..\n")` for commondir) enforced by `exactCommonGitMemberClosure`, not seal unforgeability. A same-package forger who controls the snapshot can recompute seals, but cannot make non-derivable content pass content closure, and there is no production minter to bypass the evaluator. This is exactly the documented boundary. ✓

No overclaim remains anywhere in code or doc.

---

## Commands and observed results

| Command | Result | Time |
|---------|--------|------|
| Manifest recompute (36 entries, sizes, SHA-256, digest) | PASS | 0.07s |
| git status scoped to manifest members | PASS (6 untracked, 30 match HEAD) | 1.24s |
| `go build ./internal/repaircontract/` | PASS | 0.28s |
| `go vet ./internal/repaircontract/` | PASS | 0.29s |
| `go build -tags trustedsupervisor ./internal/repaircontract/` | PASS | — |
| `go vet -tags trustedsupervisor ./internal/repaircontract/` | PASS | — |
| `gofmt -l` (6 Slice-4 files) | PASS (no diffs) | 0.06s |
| Focused `-run TestP6Slice4 -count=1` | PASS | 13.2s |
| Package `-count=1 ./internal/repaircontract/` | PASS | 24.7s |
| Race `-race -run TestP6Slice4 -count=1` | PASS | 142.9s |
| Overlay probes (11 adversarial tests) | PASS | 1.1s |
| Document test (DisallowUnknownFields, deep-equal) | PASS | 0.3s |
| Post-review manifest re-verify | PASS (0 drift) | 1.34s |

---

## Verdict

**ACCEPT**

All prior P1 and P2 findings are fully repaired and verified adversarially. Repair C (P3) remains accepted with no regression. The frozen source manifest is intact with 0 drift. All gates pass. The determinism criticism is resolved: the contract's residual claim is implemented exactly as documented with no overclaim. No P0, P1, P2, P3, or P4 findings.
