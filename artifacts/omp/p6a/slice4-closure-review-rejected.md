Working...
# P6 Contract Slice 4 — Independent Hard-Closure Review

## Frozen verification

### Initial verification

- Commit base and `HEAD`: `2df447790b89c3b55ed677ea5ea5658114d0134c`.
- Manifest: `artifacts/omp/p6a/slice4-candidate-source-manifest.json`.
- Manifest schema: `ananke.frozen-source-manifest.v1`.
- Computed manifest digest: `sha256:f9c168531bb0561c1465cc69ef51115558083fca92579f632e543a14a1bc891c`.
- Entries: 36.
- Unique repository-relative paths: 36.
- Expected aggregate bytes: 621184.
- Observed aggregate bytes: 621184.
- Duplicate paths: 0.
- Invalid, absolute, escaping, or non-normalized paths: 0.
- Symlink components: 0.
- Missing paths: 0.
- Non-regular files: 0.
- Size mismatches: 0.
- SHA-256 mismatches: 0.

Scoped status contained exactly the five frozen untracked Slice-4 files:

- `docs/experiments/p6-controlled-repair-repository-authority.md`
- `internal/repaircontract/repository_worktree.go`
- `internal/repaircontract/repository_worktree_document_test.go`
- `internal/repaircontract/repository_worktree_registry_test.go`
- `internal/repaircontract/repository_worktree_test.go`

The other manifest members and `go.mod` matched the base within the scoped comparison.

### Post-review verification and drift

The in-session post-command recheck produced the same manifest digest, 36 unique entries, 621184 bytes, and zero path, type, size, or hash mismatches. Scoped repository status remained the same five untracked candidate files.

The orchestrator independently reverified after the timeout:

```text
P6_SLICE4_MANIFEST_MISMATCHES=0
```

No repository file was edited. Ephemeral probes and overlay metadata were created only under `/tmp`; they did not alter the frozen candidate.

---

# Findings

## P0

None.

## P1 — Same-package code can forge both opaque evidence and the resulting predecessor capability

**Locations**

- `internal/repaircontract/repository_worktree.go:667-700`
- `internal/repaircontract/repository_worktree.go:933-984`

`repositoryWorktreeSnapshotIntegrityHash` is a deterministic hash of caller-populated private fields. The seven purported verification hashes are predictable public-label hashes:

```go
fixedHash("trusted-descriptor-verification-" + observationHash)
fixedHash("trusted-delta-verification-" + observationHash)
...
```

No verifier identity, secret, unforgeable token, opaque verifier capability, or release-pinned verifier seal participates in this integrity mechanism.

More critically, `mintVerifiedRepositoryWorktree` accepts any `*VerifiedRepositoryWorktreeSnapshot` and does not call:

- `verifiedRepositoryWorktreeSnapshotIntact`;
- `repositoryWorktreeSnapshotMatchesAuthority`;
- `exactRepositoryWorktreeClosure`; or
- any trusted verifier operation.

`verifiedRepositoryWorktreeIntact` subsequently checks that `snapshotIntegrityHash` is merely a syntactically valid hash. It does not establish that this hash came from an intact, trusted snapshot.

### Reproduction 1: complete snapshot forgery through `EvaluateRepositoryWorktree`

An ephemeral same-package overlay probe:

1. Changed the canonical observation’s `HEAD` member content hash to an invented value.
2. Rehashed the canonical observation.
3. Constructed `VerifiedRepositoryWorktreeSnapshot` directly.
4. Set all verification booleans.
5. Generated every “trusted verification” hash using the predictable labels used by production.
6. Recomputed `integrityHash`.
7. Called `EvaluateRepositoryWorktree`.

Observed result: the forged snapshot was accepted and an intact `VerifiedRepositoryWorktree` was minted. The overlay test passed.

### Reproduction 2: direct production-minter bypass

An additional ephemeral same-package probe:

1. Constructed an unverified snapshot containing invented canonical observation content.
2. Left verification flags and verification hashes unset.
3. Supplied only canonical bytes, their hash, and an arbitrary syntactically valid `integrityHash`.
4. Called production helper `mintVerifiedRepositoryWorktree` directly.
5. Checked the result with `verifiedRepositoryWorktreeIntact`.

Observed result: the production helper minted a capability that `verifiedRepositoryWorktreeIntact` accepted. The overlay test passed.

This directly violates the required opaque evidence/capability boundary and the documentation’s statement that production snapshot minting remains future trusted runtime work. Package-private fields do not protect against same-package production helpers, which were explicitly within review scope.

## P2 — Installation root, worktree slot, and materializer profile are caller-constructible context without authorization or release-policy binding

**Locations**

- `internal/repaircontract/repository_worktree.go:116-124`
- `internal/repaircontract/repository_worktree.go:422-432`
- `internal/repaircontract/repository_worktree.go:473-480`
- `internal/repaircontract/repository_worktree.go:707-720`

`RepositoryWorktreeInstallationAuthority` is an exported struct with five exported fields:

- `WorktreeSlotID`
- `WorktreeSlotPathHash`
- `InstalledWorktreeRootIdentityHash`
- `MaterializerProfileID`
- `MaterializerProfileHash`

Its validator checks only identifier and hash syntax. The fields are not bound to:

- `VerifiedAuthorization`;
- its release-pinned authority;
- `FrozenReleasePins`;
- a compiled installation policy;
- a verifier identity;
- a Git 2.54 profile constant; or
- an opaque accepted-installation capability.

`repositoryWorktreeSnapshotMatchesAuthority` proves only that the snapshot repeats the installation values supplied to the same call. It does not prove that authorization or release policy selected those values.

### Negative reproduction: mutation against a preexisting snapshot

An ephemeral probe changed each installation field independently while retaining the original frozen snapshot. All five calls were rejected with `ErrInvalidRepositoryWorktree`, nil capability, and `EffectAllowed == false`.

That establishes useful contextual equality: a caller cannot change installation context independently of an already minted snapshot.

It does **not** establish policy authorization. There is no separately pinned verifier in this slice. Combined with P1, same-package code can construct a snapshot agreeing with caller-selected installation values. [INFERENCE] The current equality check therefore cannot serve as the required authorization/release-policy binding.

No explicit Git 2.54 identifier exists in the Slice-4 contract. The materializer profile is an arbitrary syntactically valid ID/hash pair supplied through this same exported struct. Consequently, the exact Git 2.54 materializer profile is not frozen by the contract.

## P3 — Several declared enums are open on status-only observations, and orphan-state coverage is absent

**Locations**

- `internal/repaircontract/repository_worktree.go:500-507`
- `internal/repaircontract/repository_worktree.go:523-531`
- `internal/repaircontract/repository_worktree.go:548-560`
- `internal/repaircontract/repository_worktree.go:573-579`
- `internal/repaircontract/repository_worktree_registry_test.go:16-35`
- `internal/repaircontract/repository_worktree_registry_test.go:146-167`

The following values are checked only with `validClosedIdentifier`, not against their declared constant sets:

- `RepositoryWorktreeAmbiguityReason`
- `RepositoryDescriptorID`
- `CommonGitMemberID`
- `CommonGitMemberSemantic`
- IDs in `AddedMemberIDs`
- `CommonGitProtectedDomainID`

### Reproduction: unknown status-only enums are accepted

An ephemeral probe inserted, separately:

- `unknown_ambiguity_reason`
- `unknown_descriptor`
- `unknown_member`
- `unknown_semantic`
- `unknown_added_member`
- `unknown_domain`

Each observation decoded successfully. Each was accepted by `EvaluateRepositoryWorktree` as:

- `Disposition == RepositoryWorktreeWaitingForHuman`
- `NextRequirement == RepositoryWorktreeHumanReview`
- nil capability
- `EffectAllowed == false`

Therefore unknown values do not create authority, but the claimed closed schema is not actually closed. Under this review’s stated requirement that all enums be closed, status-only acceptance is still a contract violation.

A second probe placed unknown descriptor/member/semantic/domain values in exact-new observations. Those values also decoded, but exact closure rejected them and minted no capability.

The following enums were closed correctly:

- `RepositoryWorktreeState`
- `CommonGitDeltaState`
- `RepositoryDescriptorObjectKind`
- `RepositoryCandidateHeadMode`
- `RepositoryCandidateStatus`
- caller-supplied `RepositoryWorktreeAction`

Disposition and requirement values are evaluator outputs rather than decoded observation authority.

No explicit orphan ambiguity reason, orphan state, or orphan executable vector exists. Static inventory found:

- 150 unique Slice-4 vector IDs;
- zero orphan vector IDs;
- zero enum-unknown vector IDs beyond the generic unknown-JSON-member test.

The generic ambiguous path currently mints no capability, but an exact named orphan contract and executable probe required by the review goal are absent.

## P4

None beyond the higher-severity findings.

---

# Closure evidence

## Installation authority

The installation values are matched exactly across:

- the supplied installation struct;
- top-level observation fields;
- source closure;
- common-`.git` delta slot;
- configuration/materializer closure.

Changing any one of the five installation fields against an intact existing snapshot was rejected.

However, the values are not authorization- or release-selected in this slice. They are exported caller-constructible expected context, and no opaque accepted-installation/verifier capability pins them. The P1 forgery path removes the only practical protection supplied by the snapshot.

## Opaque evidence and capability boundary

Zero and nil snapshots are rejected. Mutating canonical bytes or verification booleans without consistently recomputing private state is rejected. Caller-owned canonical bytes and observation structures are deep-copied by the test minter.

Those protections are insufficient:

- verification hashes are predictable;
- complete same-package snapshot forgery was reproduced;
- a production capability minter accepts an unverified snapshot directly;
- the minted value’s integrity checker does not re-establish trusted snapshot verification.

## Enum closure

Closed and authority-safe:

- worktree states;
- common-`.git` delta states;
- descriptor object kinds;
- candidate head modes;
- candidate status;
- actions.

Open but status-only:

- ambiguity reasons;
- descriptor IDs;
- member IDs;
- member semantics;
- added-member IDs;
- protected-domain IDs.

Unknown exact values decoded but did not create authority. Unknown status-only values decoded and produced human-review status with no capability. This is fail-safe for effects but violates the required closed schema.

## Descriptor closure

The exact-new path requires five ordered descriptor roles:

1. source root — directory;
2. common `.git` root — directory;
3. candidate root — directory;
4. candidate gitfile — regular file;
5. candidate admin subtree — directory.

Exact closure rejects wrong IDs or kinds. Canonical-path and no-follow-identity hashes must be pairwise distinct. Candidate gitfile and candidate admin paths are cross-linked. The candidate root identity is bound to writable-path closure. Descriptor, observation, repository, base commit/tree, attempt, authorization, dispatch, and claim values are transitively bound by the observation and snapshot-matching checks.

## Exact common-`.git` delta

The exact-new closure requires six ordered members:

1. `HEAD`
2. `ORIG_HEAD`
3. `commondir`
4. `gitdir`
5. `index`
6. `logs/HEAD`

It checks exact:

- sequence;
- member ID;
- repository-relative path hash;
- semantic;
- semantic target;
- distinct member descriptor identities.

It also checks:

- detached `HEAD` at base commit;
- `ORIG_HEAD` at base commit;
- `commondir` exactly `../..`;
- admin/candidate gitfile backlinks;
- index at base tree;
- checkout log target;
- before/after inventory linkage;
- exact added-member inventory;
- no changed preexisting members;
- no removed preexisting members;
- no extra added members.

The fourteen ordered protected domains cover refs, outside logs, config, objects, hooks, info/exclude, info/attributes, alternates, shallow, grafts, replace, packed refs, common index, and sibling worktree administration. Each must be unchanged with identical before/after hashes.

## Configuration and attributes

Exact closure requires:

- common config descriptor unchanged;
- common config bytes unchanged;
- system config disabled;
- global config disabled;
- no includes;
- no `includeIf`;
- no hooks path;
- no attributes file;
- no filters;
- no fsmonitor;
- no external commands;
- system attributes disabled;
- unchanged info attributes;
- unchanged info exclude;
- verified base-tree `.gitattributes` inventory;
- bound effective attributes hash;
- no external filter attributes;
- no process-filter attributes;
- no external-command attributes.

The remaining gap is provenance of the materializer profile itself, covered by P2.

## Candidate, source, and writable paths

Exact-new closure requires:

- detached candidate;
- correct `HEAD`, `ORIG_HEAD`, index tree;
- clean initial status;
- no branch creation or update;
- no other ref update;
- candidate gitfile/admin cross-links;
- source identity unchanged;
- source protected contents unchanged;
- candidate exact child;
- candidate new;
- no source/candidate/common-`.git` alias;
- common `.git` not writable by the adapter;
- exact ordered authorization path set;
- no missing, extra, reordered, or duplicate path;
- all paths under candidate;
- no-follow ancestors;
- no symlink;
- no hardlink;
- no prefix escape;
- no case-fold collision;
- no Unicode-normalization collision.

## Retention, recovery, and forbidden effects

Exact newly materialized state is the only evaluator branch that returns a `VerifiedRepositoryWorktree`.

Exact retained replay returns status only and no capability. Ambiguous/conflicting/partial/drift states return human-review status and no capability.

Unknown or forbidden actions were rejected, including:

- cleanup;
- delete;
- prune;
- remove;
- ref update;
- commit;
- push;
- merge;
- launch;
- second worktree;
- second effect.

All observed assessment results had `EffectAllowed == false`.

Explicit orphan classification and inventory remain missing, as recorded in P3.

## Freshness and Slices 1–3 binding

`EvaluateRepositoryWorktree` performs freshness validation before selecting new, retained, or human-review state. Therefore status and retained branches cannot bypass the initial claim/authorization/dispatch/release checks.

Executable probes covered N−1 ns, N, and N+1 ns boundaries for:

- claim;
- authorization;
- dispatch;
- release.

The observation is bound to:

- authorization and approval;
- request and dispatch;
- attempt hash/number/cap;
- phase-1 claim;
- repository identity and binding;
- base commit and tree.

Static inventory and executable document/registry tests established:

- prior accepted registry: 91 unique vectors;
- Slice-3 registry: 99 unique vectors;
- Slice-4 registry: 150 unique vectors;
- Slice-4 canonical inventory and executable registry have identical order;
- all 150 registered Slice-4 probes executed.

## Canonical and document closure

Observation decoding requires:

- canonical JSON;
- duplicate-free objects;
- no unknown JSON members;
- no trailing data;
- valid UTF-8 and scalar strings;
- exact record hashes.

The probes covered unknown members, duplicate members, trailing data, noncanonical JSON, self-hash mismatch, deterministic map ordering, and caller-owned deep-copy behavior.

The normative document’s machine block is parsed with unknown fields rejected and compared to compiled constants, vector inventories, and the canonical fixture.

The document remains explicitly “candidate pending independent frozen-source review” and does not claim Slice-4, runtime, descriptor, Git, cleanup, or production-minter acceptance. It does not overclaim acceptance. Its statement that production minting is future work conflicts with the production helper identified in P1.

## Pure-contract scope

`repository_worktree.go` imports only `errors`, `reflect`, and `time`. No filesystem, Git, process, network, store, SQLite, cleanup, or runtime materialization operation was found in Slice-4 production code.

The production code does contain a capability-minter helper, but not a runtime effect implementation.

---

# Commands and observed results

All Go commands used existing caches with:

```text
GOPROXY=off
GOSUMDB=off
```

| Command | Result |
|---|---|
| `go test ./internal/repaircontract -run '^TestP6Slice4' -count=1` | PASS |
| `go test ./internal/repaircontract -count=1` | PASS |
| `go test ./internal/repaircontract -count=10` | PASS |
| `go test -race ./internal/repaircontract -count=3` | PASS |
| `go vet ./internal/repaircontract` | PASS; no diagnostics |
| Scoped tracked `git diff --check` | PASS; exit 0, no diagnostics |
| Per-file `git diff --no-index --check /dev/null <new-file>` for all five untracked candidate files | PASS; expected exit 1 for content differences, no whitespace diagnostics |
| `gofmt -d` on the four new Go files | PASS; no output |
| `go test -overlay=/tmp/p6_slice4_review_overlay.json ./internal/repaircontract -run '^TestP6Slice4Review' -count=1` | PASS for all accumulated ephemeral probes |

The overlay probes demonstrated:

- all five independent installation-field mutations were rejected against the original snapshot;
- unknown status-only enum values decoded and returned human-review status without authority;
- unknown exact enum values decoded but were denied authority;
- a same-package forged snapshot passed `EvaluateRepositoryWorktree`;
- the production minter produced an “intact” capability from an unverified snapshot.

Passing the final two probes confirms the P1 reproductions; it is not evidence of safe behavior.

SLICE CHANGES REQUESTED
