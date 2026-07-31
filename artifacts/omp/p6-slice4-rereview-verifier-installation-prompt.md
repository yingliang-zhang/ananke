Independent fresh hard review of the P6 Contract Slice 4 candidate (repository/worktree authority, Repair C + Repair P1/P2 applied) in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. READ-ONLY: create no files inside the repo, run no mutating commands, make no commits. Ephemeral probes only under `/tmp` (overlays allowed). End with an explicit verdict line: `ACCEPT` or `SLICE CHANGES REQUESTED` plus findings (P0/P1/...).

## Scope and frozen source verification (do this FIRST)

1. Manifest: `artifacts/omp/p6a/slice4-candidate-source-manifest.json` (schema `ananke.frozen-source-manifest.v1`). Recompute it fresh: for each of the 36 entries verify path normalization, no symlink components, regular file, size, and exact SHA-256; verify duplicate-path count 0 and aggregate bytes. Report entry count, aggregate bytes, and the manifest digest `sha256:6530b7c73e20f29c575d61cd7f39f3ad26e47bbe79f61e42d1f2be3c8fed0335` matched or not.
2. `git status --short` scoped to the manifest members: only the six Slice-4 files below may appear as untracked; everything else must match HEAD `74fd451`.
   - `internal/repaircontract/repository_worktree.go`
   - `internal/repaircontract/repository_worktree_test.go`
   - `internal/repaircontract/repository_worktree_registry_test.go`
   - `internal/repaircontract/repository_worktree_document_test.go`
   - `internal/repaircontract/repository_worktree_repair_c_test.go`
   - `docs/experiments/p6-controlled-repair-repository-authority.md`
3. Re-verify after review with the same procedure and report drift count (target 0).

## What this slice is (and prior findings under repair)

Pure-contract slice: canonical self-hashed repository materialization observations, closed enums (Repair C — already accepted), exact common-`.git` delta, config/attributes closure, candidate/source/writable-path closure, opaque verified snapshot evidence, retained/ambiguous/orphan status, opaque next-normal-phase capability. No filesystem/Git/process/runtime effects anywhere in production code.

Prior closure review (`artifacts/omp/p6a/slice4-closure-review-rejected.md`, verdict SLICE CHANGES REQUESTED) found: P1 verifier provenance — seven predictable `fixedHash("trusted-<kind>-verification-"+observationHash)` labels forgeable by same-package code plus production `mintVerifiedRepositoryWorktree` minting an intact capability from an unverified snapshot, and the capability intact check not re-establishing trusted verification; P2 installation authority — `RepositoryWorktreeInstallationAuthority` only syntax-checked, slot/root/profile not bound to accepted authorization or release-installed policy (no explicit Git 2.54 identifier); P3 enums — closed by Repair C, accepted.

## Repair P1/P2 as implemented (verify the claims adversarially)

- Release-pinned verifier authority: compiled self-hashed `RepositoryWorktreeMaterializerProfile` (Git 2.54, `installed_git_2_54_detached_worktree_materializer_v1`) and `RepositoryWorktreeVerifierAuthority` (`controlled_repair_repository_worktree_verifier_v1`, release pins hash, 7 closed verification kinds), derived at init with panic-on-mismatch; `EvaluateRepositoryWorktree`/`VerifyRepositoryWorktreeInstallation` first re-establish fresh `VerifyReleaseTrust` and derived-equals-frozen equality.
- Verification seals: `RepositoryWorktreeVerificationSeal` records (kind, verifier authority hash, observation hash, canonical hash, evidence hash) replace all predictable labels; snapshot intact requires recomputation-equality of all seven seals from decoded observation + booleans + evaluator-derived authority plus verifier-authority self-hash recheck.
- Capability boundary: production `mintVerifiedRepositoryWorktree` DELETED; only inline construction inside `EvaluateRepositoryWorktree` new_exact after all checks; capability binds `verifierAuthorityHash` + `verificationSealsHash`; `verifiedRepositoryWorktreeIntact` re-establishes provenance.
- Authorization-derivable content: HEAD/ORIG_HEAD content must equal `sha256256(base+"\n")`, commondir `sha256("../..\n")`; gitdir/index/logs remain verifier-attested evidence (documented non-claim).
- Verified installation capability: `VerifyRepositoryWorktreeInstallation` requires fresh release trust, frozen verifier equality, intact authorization, supervisor authority, exact frozen Git 2.54 profile, slot ID grammar `attempt_<N>_materialization_worktree_001`, and `WorktreeSlotPathHash == sha256("worktrees/"+slotID)`; evaluation takes `*VerifiedRepositoryWorktreeInstallation`; installed root identity must differ from every descriptor no-follow identity and canonical path hash.
- 258 executable vectors total (226 prior incl. Repair C + 32 new), orchestrator-independent gates: focused `TestP6Slice4` -count=1 (13.107s), package -count=1 (17.389s), focused -count=10 (126.267s), race -count=3 (361.761s), build+vet both tag sets, gofmt/diff-check all PASS.

## Required adversarial probing (at minimum)

1. Replay the rejected review's P1 reproductions as ephemeral same-package overlay probes under `/tmp`: (a) forged snapshot with mutated HEAD/ORIG_HEAD/commondir content and FULLY self-consistent recomputed seals/integrity must now be REJECTED by `EvaluateRepositoryWorktree`; (b) attempt every remaining capability-mint path — prove none exists besides the evaluator's inline mint; (c) capability whose `verifierAuthorityHash`/`verificationSealsHash`/integrity chain is tampered must fail `verifiedRepositoryWorktreeIntact` if reachable.
2. Determinism criticism: the seals are deterministic over (frozen verifier authority, frozen release, observation). Same-package code can recompute them. Assess whether the contract's residual claim (seals name provenance and bind verifier/release/observation; they do not claim a trusted verifier physically executed; production snapshot minting remains future trusted runtime work with NO production snapshot or capability minter in-slice) is implemented EXACTLY as documented in `docs/experiments/p6-controlled-repair-repository-authority.md`, and whether any overclaim remains anywhere in code or doc. Flag any write-protection or integrity claim the code makes that a same-package forger can still defeat on the new_exact path beyond documented non-claims.
3. Installation binding: (a) mint installation with wrong profile id/hash, wrong slot id (invented), wrong attempt's slot, wrong slot path hash, arbitrary content — all must reject; (b) attempt-2 slot accepted under attempt-2 authority; reused slot across attempts rejected; (c) installed root identity aliasing any descriptor no-follow identity or canonical path hash rejected at evaluation; (d) installation minted under authorization A used under authorization B rejected everywhere.
4. Enum/orphan (Repair C) regression spot-check: unknown enums still reject at decode; orphan state/reason semantics unchanged; 150-vector base claims in doc updated consistently (226→258).
5. Doc/machine-contract coherence: document test parses the doc machine block with unknown fields rejected; verify the block's new fields (verifier authority identity+hash, profile id+hash, slot grammar rule, counts, new vector IDs, corrected fixture hashes) equal compiled constants and canonical fixture recomputation.
6. Gates spot-rerun: focused -count=1, package -count=1 at minimum; race if time. All Go commands with `GOPROXY=off GOSUMDB=off` using existing caches. Do NOT run the full `./...` suite (pre-existing trustedsupervisor environmental deadline cluster is out of scope and reproduces on pristine HEAD).

## Report format

Frozen verification (initial + post-review with digests), findings by severity with file:symbol locations and executable reproduction for every claim, closure evidence per category (installation authority, opaque evidence boundary, enum closure, descriptor closure, exact common-`.git` delta, config/attributes, candidate/source/paths, retention/recovery/forbidden effects, freshness and Slices 1–3 binding, canonical/document closure, pure-contract scope), commands and observed results (PASS/FAIL + seconds), then the verdict line.
