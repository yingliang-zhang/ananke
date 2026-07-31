Implement Tasks 3, 4, and 5 from `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md` in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. Strict TDD. Scope only new `internal/repairrunner/**`; do not edit store, trusted-supervisor, docs, CLI, or artifacts. No commit.

Use the current uncommitted P6 APIs in `internal/store/p6_repair.go`.

Requirements:
- Runner loads/revalidates an exact durable prepared P6 admission by authorization hash before any effect.
- Pin `/usr/bin/git` device/inode/hash and use closed argv/env. Verify original repo top-level, clean status, exact HEAD commit/tree, repository path binding, absent target, and parent identity.
- Append a canonical sequence-2 `running` event before `git worktree add`. Its evidence hash binds the immutable pre-effect descriptor; no source bytes/raw output.
- Create exactly one detached worktree at exact base commit with closed `git worktree add --detach`; verify detached HEAD/tree and worktree device/inode/admin binding.
- Define a narrow Adapter interface. No production implementation/constructor. Fake adapter and marker exist only in `_test.go`, edit exactly one allowed regular file, and track invocation count.
- Never rerun adapter automatically. Durable `running` on entry returns explicit waiting-for-human/recovery-required and performs no filesystem/adapter effect. Do not append a misleading second running event.
- After adapter exit independently inspect through pinned Git: strict bounded porcelain-v2/raw/numstat/patch parsing. Require no untracked, rename/copy, deletion unless the policy explicitly represents it (P6a policy currently has only paths, so reject deletions), symlink, gitlink/submodule, mode-only change, binary patch, oversized diff, NUL/unusual/path traversal, or any changed path outside a nonempty subset of the sorted allowlist. Require regular tracked files.
- Compute diff SHA-256/size and ordered changed paths. Do not persist raw patch/source. Phase 2 may return a typed in-memory candidate for Phase 3, while durable state remains running; errors after effect must append failed only when failure is fully known, otherwise waiting_for_human. Retain worktree in all post-creation outcomes for review/recovery; never `git worktree remove`.
- Snapshot and recheck original repository HEAD/tree/status and tracked-byte aggregate hash before/after; any drift is waiting_for_human.
- No commit, branch creation/update, push, merge, cleanup, provider, network, shell command, or arbitrary executable.
- Test temporary real Git repos. Cover allowlisted edit and every denial: outside path, untracked, rename, deletion, symlink, mode change, binary, oversized patch, target collision, dirty original, base mismatch, repo drift, adapter error/panic/deadline, second invocation/restart. Prove retained worktree and original repo unchanged. Production marker absence test if possible without changing other packages.
- Run package count=1 then count=10, race count=3, full store+repairrunner, gofmt, vet, diff-check.
Return exact files/results and remaining Task 3-5 gaps.