Implement P6 Contract Slice 4 (repository/common-.git/linked-worktree authority) in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` with strict TDD. No commit. Preserve accepted Slices 1–3 at commit `2df447790b89c3b55ed677ea5ea5658114d0134c`. Contract-only: no filesystem access, Git execution, process launch, network, store, SQLite, cleanup, deletion, prune, worktree creation, or adapter launch.

Accepted design requirement: exact permitted common-`.git` delta, config/attributes closure, retained/ambiguous worktrees, and no cleanup authority.

Observed platform oracle (Git 2.54.0 Apple Git-157) from a fresh temporary repository using `git worktree add --detach <candidate> HEAD`:
- the only added/changed common-`.git` files were exactly six members under one new `worktrees/<slot>/` subtree:
  1. `HEAD`
  2. `ORIG_HEAD`
  3. `commondir`
  4. `gitdir`
  5. `index`
  6. `logs/HEAD`
- no common-`.git` file was removed;
- detached status reported exact base OID and `(detached)`;
- candidate root `.git` was a regular gitfile pointing to that exact admin subtree.
Do not hard-code machine-local raw paths or probe hashes; freeze member IDs, repository-relative path hashes, content semantics, and verified descriptor hashes.

Required architecture:
1. Add a new pure production file (prefer `internal/repaircontract/repository_worktree.go`) and dedicated tests/docs. Reuse existing canonical/JCS/hash/time helpers, `RepositoryBinding`, `WritablePathBinding`, `SupervisorIntentAuthority`, `VerifiedAuthorization`, and opaque `VerifiedSupervisorIntentClaim`. Do not duplicate or weaken them.
2. Define closed canonical self-hashed data records for a repository/worktree materialization observation. Canonical data alone is never filesystem authority.
3. `EvaluateRepositoryWorktree` (or equivalently named API) must require:
   - exact materialization-phase `SupervisorIntentAuthority`;
   - intact matching `VerifiedAuthorization`;
   - intact fresh opaque `VerifiedSupervisorIntentClaim` for phase 1;
   - an opaque private integrity-rechecked `VerifiedRepositoryWorktreeSnapshot` minted only by future trusted in-package descriptor/Git verification (no production constructor/decoder from caller bytes);
   - explicit `now` and complete claim/dispatch/authorization/release freshness at effect time.
4. The opaque snapshot must bind and revalidate on every call:
   - authorization/approval/request/dispatch/attempt/claim/repository binding/base commit/base tree;
   - one fixed worktree slot ID and private tuple/slot uniqueness proof;
   - no-follow descriptor identity hashes for source root, common `.git` root, candidate root, candidate `.git` gitfile, and candidate admin subtree;
   - before/after common-`.git` inventory hashes and an exact delta record;
   - candidate HEAD/ORIG_HEAD both equal base commit; detached HEAD; index tree equals base tree; clean initial candidate status;
   - source worktree/root identity and protected contents unchanged;
   - exact authorized writable-path set hash and path-closure proof (no symlink/escape/alias/duplicate/case-fold collision for each authorized path and ancestors);
   - effective Git config closure and attributes closure;
   - private descriptor, delta, config, attributes, path, uniqueness, and ambiguity verification flags;
   - deep-copied canonical bytes/hashes and snapshot integrity hash.
5. Exact common-`.git` delta:
   - only one newly allocated candidate admin subtree for the exact attempt/claim/slot;
   - exactly six ordered member IDs corresponding to HEAD, ORIG_HEAD, commondir, gitdir, index, logs/HEAD; use IDs + relative-path hashes, never raw machine paths;
   - no changed/removed preexisting member and no other added member;
   - no refs/logs outside the new admin subtree, config, objects, hooks, info/exclude, info/attributes, alternates, shallow/grafts/replace, packed-refs, index, or worktree-admin sibling changes;
   - `commondir` semantic value exactly `../..`; candidate gitfile and admin gitdir cross-bind by canonical path hashes; member content hashes are observed/bound, not portable hard-coded values.
6. Config/attributes closure must explicitly prove:
   - common repo config bytes/descriptor unchanged before/after;
   - no `include.*`, `includeIf.*`, `core.hooksPath`, `core.attributesFile`, `filter.*`, `core.fsmonitor`, or external command/filter configuration can affect checkout;
   - system/global config disabled by an installed materializer profile identity (bind profile ID/hash; no caller argv/env fields);
   - system attributes disabled; common `.git/info/attributes` and `.git/info/exclude` absence/content unchanged; base-tree `.gitattributes` inventory and effective attributes are verified; no external filter/process attribute applies;
   - hooks, filters, config includes, and attribute-driven external commands are unrepresentable as accepted closure.
7. Candidate/source/path closure:
   - candidate is detached at exact base commit/tree with no branch/ref creation or update;
   - candidate root is a new exact child under installed worktree root identity; source and candidate are not aliases; common `.git` root is not writable by adapter authority;
   - authorized writable paths resolve under candidate root only, match exact ordered authorization path IDs/hashes, and have no symlink/hardlink/path-prefix/case-fold/Unicode alias; no raw authorized path may be supplied by caller.
8. Retention/recovery behavior:
   - no result ever grants cleanup/delete/prune/remove/ref-update/commit/push/merge authority;
   - exact newly materialized verified snapshot may mint an opaque `VerifiedRepositoryWorktree` capability for the next normal phase, with `EffectAllowed=false` in its assessment;
   - exact pre-existing/retained replay is status-only and mints no new capability;
   - verified ambiguity, partial delta, preexisting conflicting slot/admin subtree, orphan, descriptor drift, or uncertain ownership returns retain-for-human/waiting status and no capability;
   - nil/unverified/malformed opaque evidence rejects;
   - there is no automatic repair, cleanup, second worktree, or launch action.
9. Add a separate ordered executable Slice-4 vector registry and machine-parsed normative document (prefer `docs/experiments/p6-controlled-repair-repository-authority.md`). Keep Slice-4 count separate from the accepted 91 prior + 99 Slice-3 inventories. No decorative IDs.
10. Required RED/GREEN vectors at minimum:
   - canonical exact new materialization and opaque deep-copy/integrity;
   - second worktree for same tuple; slot/admin subtree reused across attempt/phase;
   - preexisting exact retained replay vs conflicting/partial/ambiguous retained state;
   - all six exact member omissions/duplicates/reorders/renames/content semantic swaps;
   - extra common-git member and changed/removed protected member;
   - changed config; include/includeIf; hooksPath; attributesFile; filter/process; fsmonitor; global/system config; changed info attributes/exclude; filter attribute;
   - changed ref/object/packed-refs/alternates/hooks/other worktree sibling;
   - branch HEAD, wrong HEAD/ORIG_HEAD/base tree/index tree, dirty candidate;
   - gitfile/admin cross-link mismatch, commondir mismatch, source/candidate/common-git alias;
   - authorized path missing/extra/reordered/duplicate, prefix escape, symlink, hardlink, case-fold and Unicode-normalization alias;
   - stale claim/authorization/dispatch/release at N-1/N/N+1 as applicable;
   - attempts to request cleanup/delete/prune/ref update/launch/second effect;
   - unknown/duplicate/trailing/noncanonical JSON and map-order determinism.
11. Document exact scope and non-claims. The future descriptor opener, Git invocation/materializer, common-.git snapshotter, and production capability minter are later trusted runtime work. Do not claim Slice 4 acceptance before independent review.

Gates:
- focused Slice-4 tests/registry/doc;
- package single;
- package count=10;
- race count=3;
- vet, gofmt, diff-check.

Return RED/GREEN evidence and changed files. Do not create cron jobs or call a real provider.
