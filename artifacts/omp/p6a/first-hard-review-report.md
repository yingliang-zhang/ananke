Working...
# Verdict: CHANGES REQUESTED

The durable journal and most parser checks are well constructed, but P6a does **not** currently prove the claimed bounded, non-publication workflow. Multiple observed paths reached `waiting_for_review` after effects the contract explicitly forbids.

## Findings

### BLOCKER — Authorized “tests” can push/update branches and still reach `waiting_for_review`
**Files:** `internal/store/p6_repair.go:897-920`, `internal/repairrunner/review.go:387-409`, `internal/repairrunner/review.go:491-499`  
**Release-blocking for P6a:** **Yes**

`P6TestDeclaration` accepts any non-shell executable and arbitrary fixed argv. `runDeclaredTest` executes it without a VCS-operation policy, filesystem sandbox, or `protocol.allow=never`.

Observed sequence:

1. Authorize pinned `/usr/bin/git`.
2. Use argv `push <local-bare-repository> HEAD:refs/heads/p6_published`.
3. Finalization runs the push as a “test.”
4. The external ref is updated.
5. Worktree diff remains unchanged, so finalization persists `waiting_for_review`.

Probe result:

```text
durable state=waiting_for_review after supervisor-owned test updated external branch
refs/heads/p6_published to 4993e8667ab9ffbfb84ac06f0fcb4765799c0252
```

This is direct push/branch-update authority despite production sources containing no literal `push` call.

---

### BLOCKER — Ignored untracked files are invisible and survive into accepted review state
**Files:** `internal/repairrunner/diff.go:45-47`, `internal/repairrunner/git.go:245-247`, `internal/repairrunner/git.go:381-383`, `internal/repairrunner/review.go:249-260`  
**Release-blocking for P6a:** **Yes**

Every status invocation uses `--untracked-files=all` but not `--ignored`. Raw diff, numstat, and patch also ignore untracked files.

Observed sequence:

1. Base contains `.gitignore` for `ignored.tmp`.
2. Adapter edits the authorized tracked file and creates `ignored.tmp`.
3. Phase-2 diff validation accepts the candidate.
4. Pre/post-test review validation continues to ignore the file.
5. Durable state becomes `waiting_for_review` with the ignored payload retained.

Probe result:

```text
durable state=waiting_for_review while retained ignored untracked file contains
"untracked ignored payload\n"
```

This violates the closed filesystem scope and explicit rejection of all untracked files.

---

### BLOCKER — Adapter timeout and Git verification do not contain adapter effects
**Files:** `internal/repairrunner/runner.go:30-40`, `internal/repairrunner/runner.go:283-313`, `internal/repairrunner/runner.go:337-350`, `internal/repairrunner/git.go:264-281`, `internal/repairrunner/diff.go:45-83`  
**Release-blocking for P6a:** **Yes**

`invokeAdapter` runs an in-process goroutine. On timeout it returns immediately; an adapter that ignores context continues indefinitely with full process filesystem/network authority.

Observed timeout probe:

```text
invokeAdapter returned after 22.134083ms; adapter then wrote "mutated after return\n"
```

The adapter can also modify worktree-specific Git configuration. `rejectEffectfulLocalGitConfig` checks the original repository’s local config, but not per-worktree `config.worktree`. With `extensions.worktreeConfig=true`, the adapter installed `core.fsmonitor`; subsequent supervisor-owned Git verification executed that helper five times:

```text
supervisor-owned diff inspection executed adapter-installed worktree config helper:
"invoked\ninvoked\ninvoked\ninvoked\ninvoked\n"
```

Thus the supposedly independent verifier can execute adapter-selected commands after `RunRepair` returns. Returning `waiting_for_human` records uncertainty but does not stop ongoing effects.

Current production unreachability is not sufficient: the exported `Adapter`/`Execute` seam claims a bounded lifecycle and is the foundation intended for later activation.

---

### BLOCKER — Concurrent finalization executes the same authorized test more than once
**Files:** `internal/repairrunner/review.go:62-77`, `internal/repairrunner/review.go:95-128`, `internal/repairrunner/review.go:137-169`  
**Release-blocking for P6a:** **Yes**

There is no CAS/lease before test execution. Two callers can both observe the same `running` head and start tests. CAS occurs only after all tests finish.

Observed:

```text
same running head launched 2 concurrent test processes;
both calls returned waiting_for_review
```

One invocation can persist `waiting_for_review` while the other invocation’s test remains active. Sequential replay is correctly effect-free, but exact concurrent replay is not.

---

### BLOCKER — Test descendants can escape the process group and survive acceptance
**Files:** `internal/repairrunner/review.go:491-524`, `internal/repairrunner/review.go:576-635`  
**Release-blocking for P6a:** **Yes**

Cleanup only addresses the original process group. A test can spawn a child with `setsid`, close inherited output descriptors, and exit zero. The original process group disappears, so `clearProcessGroup` reports clean while the detached child remains alive.

Observed:

```text
durable state=waiting_for_review while detached descendant pid=51303 remained alive
```

Such a descendant can mutate files or publish after every final verification and after evidence persistence.

---

### BLOCKER — Exported store APIs can manufacture typed `waiting_for_review` without verification
**Files:** `internal/store/p6_repair.go:430-435`, `internal/store/p6_repair.go:485-571`, `internal/store/p6_repair.go:606-698`  
**Release-blocking for P6a:** **Yes**

`AppendP6RepairEvent` correctly rejects a generic `waiting_for_review` event. However, `PersistP6RepairReviewEvidence` is exported and authenticates only caller-generated, self-hashed data. No capability binds the call to `FinalizeForReview`.

A caller can:

1. Append a syntactically valid `running` event with an arbitrary pre-effect hash.
2. Manufacture matching worktree, adapter, patch, path, and successful-test hashes.
3. Self-hash the evidence using exported hash functions.
4. Call `PersistP6RepairReviewEvidence`.

Observed store-only probe:

```text
store-only fabricated hashes produced state=waiting_for_review
evidence=sha256:9497461e22193949b65f58ec569c292bc4326983aa73dc078992eb64d039456a
without any repository, worktree, adapter, diff, or test execution
```

The evidence is immutable after insertion, but immutability does not prove truthful origin.

---

### HIGH — Approval “freshness” has no maximum age and is not rechecked before effects
**Files:** `internal/store/p6_repair.go:327-328`, `internal/store/p6_repair.go:372-384`, `internal/store/p6_repair.go:859-876`, `internal/store/p6_repair.go:1248-1259`, `internal/repairrunner/runner.go:143-158`  
**Release-blocking for P6a:** **Yes**

Admission only requires:

```text
approved_at <= now < not_after
```

There is no maximum approval interval or maximum age. A year-old approval with a future expiry was accepted:

```text
accepted approval interval 2025-07-26T12:35:38.236136Z
through 2027-07-26T12:35:38.236136Z
```

Additionally, `GetP6RepairAdmission` and `Execute` do not recheck `not_after`. A short-lived approval can be admitted while valid and exercised arbitrarily later while the attempt remains `prepared`.

Actual GUI provenance is also only a caller-supplied role string; GUI/API wiring remains an explicit non-goal.

---

### HIGH — The verified executable FD is not the executable that is launched
**File:** `internal/repairrunner/review.go:430-452`, `internal/repairrunner/review.go:482-503`, `internal/repairrunner/review.go:556-557`  
**Release-blocking for P6a:** **Yes**

`openDeclaredExecutable` opens and verifies an exact device/inode/hash FD, but `runDeclaredTest` ignores that FD and calls:

```go
exec.Command(declaration.ExecutablePath, ...)
```

An attacker can replace the path between verification and `execve`; post-start identity checks occur after the unauthorized executable may already have performed effects. ABA restoration can also defeat the path checks. Exact executable execution is therefore not established.

---

### MEDIUM — Original common `.git` directory identity is checked but not retained
**Files:** `internal/repairrunner/git.go:220-231`, `internal/repairrunner/git.go:258-261`, `internal/repairrunner/runner.go:187-194`  
**Release-blocking for P6a:** **Yes, under the exact repository/admin identity requirement**

`snapshotOriginal` inspects the common `.git` directory, verifies its path, then returns only the path. Its device/inode/owner/mode are absent from `RepositorySnapshot` and `PreEffectDescriptor`.

[INFERENCE] A same-user actor can replace the common Git administration directory with a logically equivalent copy between checks; matching HEAD/tree/status/tracked bytes can pass while the worktree is created against a different administrative root.

### LOW

None.

## What held under review

- Admission, attempt, and prepared event are committed atomically before `worktree add`.
- P4 input/bundle/admission/full-fence, repository/base, path/test/route, and attempt fields participate in canonical hashes and exact durable checks.
- Admission duplicate concurrency serialized correctly in the focused tests.
- v15 is unchanged by additive v16 migration.
- Authorization, attempt, event, and review-evidence rows have insert-only triggers.
- Event CAS and evidence/event transaction atomicity are sound against normal API races.
- Staged changes, combined index/worktree changes, rename/copy, deletion, mode changes, symlinks, gitlinks, binary patches, malformed/NUL/control paths, unsorted paths, and oversized outputs fail closed in the traced parsers.
- Sequential replay after `waiting_for_review` does not rerun tests.
- No `completed` or `success` P6 state exists.
- Worktrees are retained; no production `worktree remove` path was found.

## Commands and results

All synthetic tests were supplied through Go `-overlay` files under `/tmp`; repository files were not edited.

```text
go test ./internal/store -run P6 -count=1 -timeout 300s
PASS — 4.36s

go test ./internal/repairrunner -count=1 -timeout 300s
PASS — 25.40s
```

Adversarial overlay probes, all reproducing the stated flaw:

```text
TestProbeLateMutationAfterInvokeAdapterTimeout                 PASS
TestProbeIgnoredUntrackedFileReachesWaitingForReview          PASS
TestProbeConcurrentFinalizeRunsAuthorizedTestTwice            PASS
TestProbeDetachedTestDescendantSurvivesWaitingForReview       PASS
TestProbeWorktreeSpecificFsmonitorRunsDuringDiffInspection    PASS
TestProbeYearOldApprovalIsAcceptedAsFresh                     PASS
TestProbeStoreOnlyFactsManufactureWaitingForReview            PASS
TestProbeAuthorizedTestPushesBranchAndStillWaitsForReview     PASS
```

Production reachability check:

```text
go list -deps -json ./cmd/... |
  jq -r 'select(.ImportPath == ".../internal/repairrunner") | .ImportPath'
```

Result: no output. Current production commands do not import `internal/repairrunner`.

Source searches also found:

- no production concrete `Adapter` implementation or `NewAdapter`;
- no fake-adapter marker outside `_test.go`;
- no direct production repairrunner commit/push/merge/branch/worktree-remove command;
- no production callsite of `repairrunner.Execute` or `FinalizeForReview`.

## Residual risks / non-goals

- **Production OMP/provider adapter is absent and currently unreachable from command binaries.** This is confirmed, but it does not validate the adapter boundary for future activation. A production adapter needs a separately killable, OS-confined process with explicit filesystem/network capabilities; an in-process context is not containment.
- Actual GUI authentication/provenance, automatic retry/attempt-2 scheduling, P6b independent model review, and recovery remain absent as documented.
- The retained detached worktree behavior is present and should remain.
- The supplied full-suite/race/vet evidence is consistent with the focused runs, but those suites do not cover the reproduced safety violations above.
