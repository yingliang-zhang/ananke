Implement P6 Contract Slice 4 Repair P1+P2 (verifier provenance + installation authority binding) in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. No commit. Do not touch `internal/trustedsupervisor/` or any P5 file. Work ONLY on these existing untracked Slice-4 files plus (if strictly necessary) read other package files:

- `internal/repaircontract/repository_worktree.go`
- `internal/repaircontract/repository_worktree_test.go`
- `internal/repaircontract/repository_worktree_registry_test.go`
- `internal/repaircontract/repository_worktree_document_test.go`
- `internal/repaircontract/repository_worktree_repair_c_test.go`
- `docs/experiments/p6-controlled-repair-repository-authority.md`

First read the rejected independent review at `artifacts/omp/p6a/slice4-closure-review-rejected.md` (P1 at its lines 54–106, P2 at 108–145) and the Slice-4 ledger section in `docs/experiment-ledger.md` (search "P6 Contract Slice 4"). Repair C (closed enums + orphan state) is already complete and locally accepted; preserve it intact. Repair C changed the candidate, so rejected-review line numbers may have drifted; locate code by symbol, not line.

## Verified defects to close (from the rejected review)

P1a: same-package code forged a snapshot (mutated HEAD member content hash, re-set all verification booleans, recomputed the predictable `fixedHash("trusted-<kind>-verification-"+observationHash)` labels and `integrityHash`) and `EvaluateRepositoryWorktree` accepted it and minted an intact capability.
P1b: production helper `mintVerifiedRepositoryWorktree` mints an `intact` capability from an unverified snapshot without calling any check; and `verifiedRepositoryWorktreeIntact` performs only a syntactic hash check without re-establishing trusted verification.
P2: exported `RepositoryWorktreeInstallationAuthority` is only syntax-checked and repeated by snapshot equality. Worktree slot, slot path hash, installed root identity, and materializer profile are not selected by the accepted authorization or by release-installed policy. No explicit Git 2.54 identifier exists anywhere in the contract.

## Frozen repair design (implement exactly; deviations require reporting why)

### A. Release-pinned verifier authority (P1)

1. Add a compiled materializer profile record type `RepositoryWorktreeMaterializerProfile`, schema `ananke.controlled-repair-repository-git-254-materializer-profile.v1`, self-hashed field `materializer_profile_hash`, with: `materializer_profile_id = "installed_git_2_54_detached_worktree_materializer_v1"`, `git_version = "2.54"`, and closed boolean semantics freezing the profile policy (detached-head-only, six-member exact delta, commondir parent-parent only, system/global config disabled, no includes, no includeIf, no hooks path, no attributes file, no filters, no fsmonitor, no external commands, system attributes disabled, no external filter/process-filter/command attributes). Derive once at package init via a `mustDerive...` that panics on self-hash mismatch (mirror the `mustDeriveReleaseMaterial` pattern in `release_artifacts.go`).
2. Add `RepositoryWorktreeVerifierAuthority`, schema `ananke.controlled-repair-repository-worktree-verifier-authority.v1`, self-hashed field `verifier_authority_hash`, exported accessor `FrozenRepositoryWorktreeVerifierAuthority()`: `verifier_id = "controlled_repair_repository_worktree_verifier_v1"`, the frozen materializer profile ID+hash, `release_pins_hash = FrozenReleasePins().ReleasePinsHash`, and the ordered closed list of exactly seven verification kinds (new closed string enum `RepositoryWorktreeVerificationKind`: `descriptor_verification`, `delta_verification`, `config_verification`, `attributes_verification`, `path_verification`, `uniqueness_verification`, `ambiguity_verification`). Derive at init, panic on self-hash mismatch.
3. `EvaluateRepositoryWorktree` must call `VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now)` and require the derived authority to equal the frozen value before any other evaluation step. Fail closed with `ErrInvalidRepositoryWorktree`.

### B. Verification seals replace predictable labels (P1)

1. New record `RepositoryWorktreeVerificationSeal`, schema `ananke.controlled-repair-repository-worktree-verification-seal.v1`, self-hashed field `seal_hash`, fields: `seal_kind`, `verifier_authority_hash`, `observation_hash`, `canonical_hash`, `evidence_hash`.
2. In `VerifiedRepositoryWorktreeSnapshot`, replace the seven `fixedHash(...)` label fields with the same seven seal-named fields whose values are computed seals, and add `verifierAuthorityHash`. Evidence hash per kind, deterministic from the observation and snapshot booleans (canonical-encode a small purpose struct and sha256 it):
   - descriptor: ordered five descriptor hashes (`DescriptorHash` values, canonical list);
   - delta: canonical struct of `CommonGitDelta.DeltaHash`, `BeforeCommonGitInventoryHash`, `AfterCommonGitInventoryHash`;
   - config: `Config.ConfigClosureHash`; attributes: `Attributes.AttributesClosureHash`; paths: `WritablePaths.PathClosureHash`;
   - uniqueness: canonical struct of `tupleUnique`, `slotUnique`, `ownershipCertain` booleans;
   - ambiguity: canonical struct of `unambiguous`, `Observation.State`, `Observation.AmbiguityReason`.
3. Extend `repositoryWorktreeSnapshotIntegrity` with the new seal fields + `verifierAuthorityHash`. Change `verifiedRepositoryWorktreeSnapshotIntact(value, verifier)` to recompute every seal from the decoded observation + snapshot booleans + the evaluator-derived verifier authority and require exact equality, require `value.verifierAuthorityHash == verifier.VerifierAuthorityHash`, and keep the existing canonical-hash/deep-equal checks.

### C. Boundary the capability mint (P1)

1. DELETE the `mintVerifiedRepositoryWorktree` function entirely. Construct the capability inline inside `EvaluateRepositoryWorktree`'s `new_exact` branch only after every check has passed (fresh release trust, snapshot intact-with-verifier, authority match, exact closure, uniqueness booleans, action). No standalone production helper may mint `VerifiedRepositoryWorktree`.
2. Extend the capability with `verifierAuthorityHash` and a `verificationSealsHash` (sha256 over canonical encoding of the ordered seven seal strings). Extend `verifiedRepositoryWorktreeIntegrity` accordingly.
3. `verifiedRepositoryWorktreeIntact` must re-establish provenance: recompute the seven seals from the capability's decoded observation plus the frozen verifier authority and require `verificationSealsHash` equality and `verifierAuthorityHash == FrozenRepositoryWorktreeVerifierAuthority().VerifierAuthorityHash`, in addition to existing syntactic checks. (The frozen derivation is time-independent; release-trust freshness was already verified by the evaluator for the evaluation `now`.)

### D. Authorization-derivable member content (P1, blocks review reproduction 1)

In `exactCommonGitMemberClosure` (both new and retained exact paths), additionally require:
- `HEAD` member `ContentHash == sha256Digest([]byte(BaseCommit + "\n"))`;
- `ORIG_HEAD` member `ContentHash == sha256Digest([]byte(BaseCommit + "\n"))`;
- `commondir` member `ContentHash == sha256Digest([]byte("../..\n"))`;
`gitdir`, `index`, and `logs/HEAD` content hashes remain verifier-attested evidence (documented as such). Update the test fixture to set these three content hashes accordingly.

### E. Verified installation capability (P2)

1. Keep exported `RepositoryWorktreeInstallationAuthority` as the plain record. Add opaque `VerifiedRepositoryWorktreeInstallation` (private fields: valid, installation record, verifierAuthorityHash, authorizationHash, attemptHash, integrityHash) minted only by new `VerifyRepositoryWorktreeInstallation(installation RepositoryWorktreeInstallationAuthority, expected SupervisorIntentAuthority, authorization *VerifiedAuthorization, now time.Time) (*VerifiedRepositoryWorktreeInstallation, error)` which requires: fresh `VerifyReleaseTrust(now)`; frozen verifier equality; `verifiedAuthorizationIntact(authorization)`; `validateSupervisorIntentAuthority(expected, authorization)`; existing syntax validation; materializer profile ID+hash exactly equal to the frozen Git 2.54 profile (release-installed policy binding); worktree slot ID exactly equal to the frozen grammar `"attempt_" + strconv.Itoa(expected.AttemptNumber) + "_materialization_worktree_001"` (authorization-derived slot); `WorktreeSlotPathHash == sha256Digest([]byte("worktrees/" + WorktreeSlotID))` (frozen derivation — this changes the previous fixture value, update fixture/doc).
2. Change `EvaluateRepositoryWorktree` to accept `installation *VerifiedRepositoryWorktreeInstallation`; intact-check it (integrity chain + verifier authority + authorization/attempt match) and then use its embedded record everywhere the raw struct was used.
3. In exact closure, require `InstalledWorktreeRootIdentityHash` to differ from every descriptor's `NoFollowIdentityHash` and `CanonicalPathHash` (anti-alias).

### F. Tests, vectors, document

1. Update the test minter to compute seals via the production seal derivation; update fixtures (slot path hash derivation, three derived content hashes); route evaluation through `VerifyRepositoryWorktreeInstallation`. Preserve every existing vector's intent: the review's reproduction scenarios must now be regression vectors where a FULLY self-consistent forgery (seals + integrity recomputed) is still rejected at content closure, and an unverified snapshot can never be admitted.
2. Add executable registry vectors (each named in the registry and executed) covering at minimum: forged HEAD/ORIG_HEAD/commondir content rejected; snapshot with wrong/empty verifier authority rejected; each of seven stale seals rejected; capability integrity/verifier binding mutation rejected; verifier authority release binding holds; unknown profile ID/hash rejected at installation verify; nil/zero/cross-authorization installation rejected; slot grammar frozen for attempts 1 and 2 (attempt-2 canonical slot accepted, caller-invented slot rejected, reused slot across attempts rejected); wrong slot path hash rejected; installed root identity aliasing each of the five descriptor no-follow identities rejected and aliasing a canonical path hash rejected; installation integrity mutation rejected; frozen verifier/profile equality with the document machine block.
3. Update `docs/experiments/p6-controlled-repair-repository-authority.md`: status remains candidate pending independent frozen-source review (no acceptance claim); add sections describing the release-pinned verifier authority, seal semantics and their explicit non-claim (seals name provenance and bind verifier/release/observation; they do not claim a trusted verifier physically executed — production snapshot minting remains future trusted runtime work and NO production snapshot or capability minter exists in this slice), the Git 2.54 materializer profile freeze, the authorization-derived slot grammar and path-hash derivation, and installed-root anti-alias. Update the machine contract block (add verifier authority + profile identities and hash values, slot grammar rule, all new vector IDs, corrected `slice_4_vector_count`, corrected canonical fixture `worktree_slot_path_hash` and `snapshot_integrity_hash`). Keep the prior 91 and Slice-3 99 inventories untouched.
4. All identifiers/code/comments/doc in English.

## Gates (run in order; report exact evidence)

1. `go build ./... && go vet ./...`
2. `go test ./internal/repaircontract -run '^TestP6Slice4' -count=1`
3. `go test ./internal/repaircontract -count=1`
4. `go test ./internal/repaircontract -run '^TestP6Slice4' -count=10`
5. `go test -race ./internal/repaircontract -run '^TestP6Slice4' -count=3`
6. Tagged compile both gate sets: `go build -tags 'ananke_real_provider_canary ananke_test_runtime_authority' ./...` and `go vet -tags 'ananke_real_provider_canary ananke_test_runtime_authority' ./...` plus tagged `-run '^$'` test compile of `./internal/repaircontract`.
7. `gofmt -d` on every Slice-4 Go file (expect empty) and `git diff --no-index --check /dev/null <file>` per untracked file (exit 1 with no whitespace diagnostics is fine); scoped tracked `git diff --check`.
Do NOT run the full `./...` suite; the `internal/trustedsupervisor` deadline-exceeded cluster is a pre-existing environmental issue reproduced on pristine HEAD.

## Return

Report: exact gate outputs (PASS/FAIL + package seconds), final Slice-4 vector count, complete changed-file list, the computed frozen verifier authority hash / profile hash / new canonical fixture `snapshot_integrity_hash` / `worktree_slot_path_hash` values, and any design deviation with justification. Do not create cron jobs. Do not commit.
