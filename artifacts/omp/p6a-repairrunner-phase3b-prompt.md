Implement P6 Phase 3B in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`, strict TDD, scope only `internal/repairrunner/**`. No store/trusted-supervisor/docs/CLI/artifact edits and no commit.

Use current `store.P6RepairReviewEvidence`, `P6RepairReviewTestResult`, `HashP6RepairReviewEvidence`, and `PersistP6RepairReviewEvidence` APIs.

Goal: take a successful Phase-2 `Result`/`DiffCandidate`, independently run the exact policy-owned tests, prove the candidate and original repository remain unchanged, and atomically persist typed evidence plus `waiting_for_review`.

Requirements:
- Add a `FinalizeForReview(ctx, journal, phase2 Result)` style API. Reload exact admission and current event head. Require phase2 running event/head, authorization/attempt/policy/pre-effect/worktree/candidate/adapter/diff bindings all exact. Reject replay ambiguity; if already waiting_for_review, load evidence and return exact replay without tests/effects.
- Before tests, reverify pinned `/usr/bin/git`, original repository identity/HEAD/tree/status/tracked-byte aggregate, worktree/admin/.git identities, detached HEAD/tree, and recomputed strict diff exactly matches candidate hash/size/ordered paths.
- For each ordered `P6TestDeclaration`, re-open executable O_NOFOLLOW, verify regular device/inode/SHA256 against declaration and recompute command hash. No shell.
- Run executable with fixed argv in the retained worktree under a fixed closed environment. Use a private 0700 temporary/cache root outside the worktree under its authorized parent. No caller env/credential/network/proxy inheritance. For Go-friendly fixed values include HOME/GOCACHE/GOMODCACHE/TMPDIR under that root, GOWORK=off, CGO_ENABLED=0, LANG/LC_ALL/TZ, and fixed PATH only if required; do not allow test declaration to set env.
- New process group, bounded timeout from declaration, bounded stdout/stderr (64 KiB each), exact wait/exit. On timeout terminate exact group TERM→bounded KILL→wait. Never leave children. A nonzero exit, timeout, capture overflow, executable drift, or termination ambiguity must not produce waiting_for_review; known clean failure→failed, uncertain process/identity/repo state→waiting_for_human.
- Persist only test ID/command hash/executable identity hash/exit/timed_out/output hash+size; zero raw output buffers.
- After every test and before persistence, recompute strict diff and original repository snapshot. Require exact equality to Phase-2 candidate. Reject test-created untracked files or any mutation. Revalidate worktree identity.
- Build typed review evidence with exact admission/P4/attempt/running/pre-effect/worktree/adapter/patch/path/test bindings, self-hash it, build sequence-3 waiting_for_review event, and call atomic store API.
- On exact terminal replay, return stored evidence/event without rerunning tests. No completed/success, commit, branch, push, merge, cleanup of review worktree, provider, or network.
- Tests with temp repos and exact `/usr/bin/test` fixture: happy path, restart replay invocation count zero, executable drift, argv/hash drift, timeout/child process cleanup, nonzero exit, output overflow, test mutates worktree, original repo drift, candidate tamper, worktree replacement, persistence conflict. Assert retained worktree and no raw output in DB.
- Run package count=1, focused count=10, race count=3, store+repairrunner, gofmt, vet, diff-check.
Return exact files/results and gaps.