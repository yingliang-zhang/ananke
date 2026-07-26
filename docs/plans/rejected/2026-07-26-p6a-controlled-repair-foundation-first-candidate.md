# REJECTED — P6a Controlled Repair Foundation First Candidate

> This plan is retained only as audit history. It is not actionable. Independent review reports `artifacts/omp/p6a/first-hard-review-report.md` and `artifacts/omp/p6a/design-rereview-report.md` rejected the in-process adapter/test runner and unsigned evidence architecture.

> **2026-07-26 hard-review correction:** The first implementation was rejected because an in-process adapter/test runner and unsigned self-asserted review evidence cannot establish effect containment or truthful authority. The replacement architecture below supersedes any task that allows `internal/repairrunner` to launch arbitrary adapters/tests or lets a caller persist unsigned `waiting_for_review` evidence.

## Replacement architecture after first hard review

Ananke is the durable request/review authority and holds no private key. The already authenticated local trusted-supervisor process is the only effect authority:

1. Ananke persists a fresh, bounded P6 authorization and attempt, then emits an authenticated repair request over the existing signed Unix transport.
2. The trusted supervisor claims the exact attempt durably before any effect. A second caller/restart cannot execute the adapter or tests again; ambiguous crash state becomes `waiting_for_human`.
3. The trusted supervisor creates the detached worktree, launches the future OMP adapter only as an OS-sandboxed subprocess, verifies the candidate, and runs project tests in a separate disposable no-network sandbox. No in-process `Adapter` interface is production-reachable.
4. The trusted supervisor signs a canonical repair-review attestation with its existing Ed25519 signing material. The attestation binds the full authorization/P4/fence/attempt/claim/pre-effect/worktree/diff/test tuple and contains hashes/counters only.
5. Ananke verifies the signature against the pinned public trust bundle before atomically persisting the attestation and `waiting_for_review`. Generic/unsigned store APIs cannot create that state.
6. Human accept/reject remains separate. Neither signed review evidence nor `waiting_for_review` commits, updates a branch, pushes, merges, or deletes the retained review worktree.

### Immediate contract repairs

- Require approval age and lifetime bounds and recheck expiry immediately before dispatch/effects.
- Record and revalidate the original common `.git` directory/config identity.
- Reject ignored untracked files in every candidate/test verification pass.
- Remove arbitrary executable/argv “tests” from the Ananke-side authority contract. Test profiles are closed trusted-supervisor policy identities, not caller-provided commands.
- Add a one-shot durable effect claim before adapter execution and before test execution. Crash after claim never auto-runs again.
- Treat all existing unsigned P6 review-evidence rows/APIs as pre-release schema only; migration v17 replaces them before any commit/release.

### Required RED vectors before replacement implementation

The first hard-review probes become permanent tests and must fail before GREEN implementation:

- authorized test cannot push/update any local or remote ref;
- ignored untracked file cannot reach review state;
- timed-out adapter cannot mutate after supervisor returns;
- worktree `config.worktree`/`core.fsmonitor` cannot influence verification;
- two concurrent finalizers cause exactly one effect invocation;
- detached/setsid descendant cannot survive or mutate after acceptance;
- unsigned/store-only fabricated hashes cannot create `waiting_for_review`;
- year-old/expired approval is rejected at admission and effect time;
- executable path replacement cannot substitute the verified executable;
- common `.git` directory replacement is detected.

> **For Hermes:** Implement this plan task-by-task with TDD and independent review.

**Goal:** Add the smallest durable, provider-free Ananke workflow that can authorize one bounded code edit in an isolated detached Git worktree, verify its scope and tests, and stop at `waiting_for_review` without committing, pushing, merging, or claiming success.

**Architecture:** A new insert-only P6 authority in `internal/store` binds an exact accepted P4 fact, fresh local-GUI human decision, repository/base revision, closed path/test policy, route identity, and attempt 1..2 before any filesystem effect. A new `internal/repairrunner` creates a detached worktree using pinned Git, invokes only an injected adapter, independently validates the resulting diff and policy-owned tests, persists immutable state events/evidence, and retains the worktree for review. Production has no concrete repair adapter in P6a; the only editor is a `_test.go` fake.

**Tech Stack:** Go, SQLite, `/usr/bin/git`, SHA-256/JCS-compatible canonical JSON, Darwin filesystem identity, existing Ananke store transaction conventions.

---

## Frozen acceptance contract

1. P4 remains `design_only_no_repair_execution`; it grants no execution by itself.
2. A fresh P6 human authorization is required and must bind the complete current P4 admission, evidence bundle, and full fence plus repository/base/worktree/path/test/route/attempt policy.
3. Authorization and prepared event commit atomically before `git worktree` or adapter effects.
4. Attempt cap is exactly 2; attempt number is 1 or 2.
5. The original repository is read-only from the runner's perspective and must remain byte/status identical.
6. The repair worktree is detached at the exact base commit and is never committed, branched, pushed, merged, or automatically deleted.
7. Only ordered allowlisted regular tracked files may change. Untracked files, symlinks, submodules, binary diffs, path escapes, oversized diffs, and any non-allowlisted change fail closed.
8. Tests are supervisor-owned exact executable + fixed argv declarations. Model/adapter output never selects commands.
9. Successful edit/test collection ends at `waiting_for_review`, never `completed` or `success`.
10. P6a has no production repair adapter. A fake adapter exists only in `_test.go`; the production binary must not contain its marker.
11. Crash recovery never reruns the adapter automatically. A durable `running` attempt found after restart becomes/stays explicit nonterminal `waiting_for_human` pending later P6b recovery design.
12. No raw prompt, credential, endpoint, arbitrary command/environment, source bytes, or raw test output enters durable authority/evidence.

## Task 1: Durable P6 schemas and migration

**Objective:** Add append-only authorization, attempt, event, and evidence storage.

**Files:**
- Create: `internal/store/p6_repair.go`
- Create: `internal/store/p6_repair_test.go`
- Modify: store migration/schema registration files discovered from existing P4 conventions.

**Steps:**
1. RED tests for migration idempotence, insert-only rows, exact replay, conflict rejection, and transaction rollback.
2. Define closed exported input/result structs and private canonical row structs.
3. Add tables with immutable unique hashes/IDs and foreign-key/order constraints:
   - `p6_repair_authorizations`
   - `p6_repair_attempts`
   - `p6_repair_events`
4. Event states are exactly `prepared`, `running`, `waiting_for_review`, `failed`, `waiting_for_human`.
5. Add atomic `AdmitP6Repair` that verifies the exact persisted P4 fact and inserts authorization + attempt + sequence-1 `prepared` in one transaction.
6. Add compare-and-append event methods that require exact prior sequence/hash/state.
7. Run focused tests, `-count=10`, and `-race -count=3`.

## Task 2: Canonical authorization validation

**Objective:** Make every effect-relevant decision machine-verifiable before admission.

**Files:**
- Modify: `internal/store/p6_repair.go`
- Modify: `internal/store/p6_repair_test.go`

**Steps:**
1. RED table tests for stale/missing P4, projected fence, rejected human decision, wrong operator role, attempt 0/3, cap other than 2, duplicate/unsorted paths or tests, traversal/absolute paths, mutable branch names, malformed hashes, and forbidden authority fields.
2. Require exact schema versions, `local_gui_operator`, decision `approved`, exact 40/64-hex base commit/tree, absolute clean repository/worktree parent paths, detached worktree name, exact route role, and self-hashes.
3. Permit only repository-relative clean slash allowlisted paths; reject `.git`, empty, dot, parent traversal, backslashes, duplicates, and prefixes that ambiguously overlap.
4. Tests declare ID, pinned executable identity, fixed argv, timeout, and command hash; no shell strings.
5. Recompute all canonical hashes rather than trusting input hash fields.

## Task 3: Isolated detached worktree preparation

**Objective:** Create exactly one review-retained worktree after durable admission.

**Files:**
- Create: `internal/repairrunner/runner.go`
- Create: `internal/repairrunner/git.go`
- Create: `internal/repairrunner/runner_test.go`

**Steps:**
1. RED tests using a temporary Git repository; snapshot original status and tracked-content hash.
2. Define a narrow `Adapter` interface accepting only the authorized worktree descriptor/path and immutable attempt metadata. Production exposes no constructor.
3. Pin `/usr/bin/git` identity; use a closed environment and argv only.
4. Verify repository top-level, exact clean HEAD/base commit/tree, no existing target, and parent identity.
5. Append `running` before `git worktree add --detach --no-checkout`; materialize exact base commit and verify detached HEAD/tree afterward.
6. Record worktree device/inode and Git administrative binding in immutable event data.
7. On pre-adapter crash/restart, do not recreate or invoke automatically; return `waiting_for_human`.
8. Assert original repository status/tracked hash unchanged.

## Task 4: Provider-free fake edit and bounded adapter lifecycle

**Objective:** Prove one controlled edit without introducing production model execution.

**Files:**
- Create: `internal/repairrunner/fake_adapter_test.go`
- Modify: `internal/repairrunner/runner_test.go`
- Add production-binary absence test beside existing trusted-supervisor production marker tests if appropriate.

**Steps:**
1. RED test that no production adapter is available.
2. Add `_test.go` fake with a unique marker that edits exactly one configured allowlisted regular file and returns bounded typed metadata.
3. Test adapter error, panic boundary, context deadline, second invocation prevention, and attempt-cap rejection.
4. Persist no raw adapter output; only adapter identity hash, exit classification, and bounded counters.
5. Prove production binaries omit fake marker/factory names.

## Task 5: Independent diff policy

**Objective:** Reject every change outside the exact authorization.

**Files:**
- Create: `internal/repairrunner/diff.go`
- Create: `internal/repairrunner/diff_test.go`

**Steps:**
1. RED tests for allowed edit plus: outside path, untracked file, rename/copy, deletion if not explicitly allowed, symlink, submodule/gitlink, mode change, binary patch, oversized patch, NUL/unusual path, traversal-like path, and dirty original repo.
2. Use pinned Git with closed argv to obtain porcelain-v2 status, raw diff metadata, numstat, and binary-safe patch bytes.
3. Parse outputs with strict bounds; never shell-split.
4. Require ordered changed paths equal a nonempty subset of the authorization allowlist and all entries remain regular tracked files.
5. Compute patch SHA-256/size and changed-path list; do not persist raw patch/source bytes in the authority DB.
6. Keep the worktree for later review.

## Task 6: Supervisor-owned tests and evidence

**Objective:** Run only preauthorized tests and persist bounded evidence.

**Files:**
- Create: `internal/repairrunner/tests.go`
- Create: `internal/repairrunner/tests_test.go`
- Create: `internal/repairrunner/evidence.go`
- Create: `internal/repairrunner/evidence_test.go`

**Steps:**
1. RED tests for executable identity drift, argv drift, timeout, nonzero exit, capture overflow, output leak, reordered tests, and adapter-selected command attempts.
2. Revalidate executable device/inode/hash and exact command hash immediately before launch.
3. Use closed environment, worktree cwd, bounded stdout/stderr buffers, process-group timeout/termination, and exact exit observation.
4. Persist only test ID, command hash, executable identity hash, exit code, timeout flag, stdout/stderr hash+size.
5. Evidence binds authorization/P4/spec/attempt/base commit/tree/worktree identity/adapter identity/diff hash+size/ordered paths/tests/prior event hash.
6. Append `waiting_for_review` only after all validations/tests pass. Any failure appends `failed`; uncertain post-effect state appends `waiting_for_human`.
7. Exact replay returns the same immutable evidence and performs no adapter/test/filesystem effect.

## Task 7: End-to-end provider-free acceptance

**Objective:** Demonstrate Ananke can safely produce a reviewable code change.

**Files:**
- Create: `internal/repairrunner/e2e_test.go`
- Modify: `docs/experiment-ledger.md`
- Modify/create: P6 runbook section under `docs/`.

**Steps:**
1. Build a temporary repository with base commit and one allowlisted file.
2. Persist a valid P4 fact and fresh P6 authorization.
3. Execute fake adapter edit in detached worktree.
4. Run one pinned read-only test.
5. Assert terminal P6a state `waiting_for_review`, exact evidence hashes, changed path, and retained worktree.
6. Assert original repository HEAD/status/tracked bytes unchanged and no branch/commit/push/merge occurred.
7. Restart store and prove replay does not rerun effects.
8. Simulate durable `running` restart and prove it becomes/stays `waiting_for_human` without invoking adapter.

## Verification gates

Run after focused RED→GREEN loops:

```sh
go test ./internal/store -run 'P6' -count=10 -timeout 300s
go test -race ./internal/store -run 'P6' -count=3 -timeout 300s
go test ./internal/repairrunner -count=10 -timeout 600s
go test -race ./internal/repairrunner -count=3 -timeout 600s
go test ./... -count=1 -timeout 600s
go test -race ./... -count=1 -timeout 600s
go vet ./...
git diff --check
```

Then run an independent fresh-context review focused on admission-before-effect, original-repo immutability, path/diff parser closure, exact test authority, no production adapter, no inferred success, replay, and crash behavior.

## Explicit non-goals

- Real OMP/provider repair execution.
- Automatic retry or attempt-2 scheduling.
- Independent model review implementation (P6b).
- Commit, branch update, push, merge, or rollback.
- GUI/API wiring (P7).
- Deleting the review worktree.
- Extending P5 trusted read-only supervisor behavior.
