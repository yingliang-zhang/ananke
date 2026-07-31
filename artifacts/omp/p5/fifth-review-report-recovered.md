# CHANGES REQUESTED

Base reviewed: `1bbc880576173913d62f13200ea54b25d46f4393` plus the complete uncommitted worktree.

Worktree observed: `0` staged, `17` modified tracked files, `53` untracked files. No source edits, commits, pushes, credentials, root provisioning, or real model/provider requests were performed.

Two high-severity blockers remain.

## Remaining blockers

### 1. High — A deployable, production-named binary contains the compile-time test authority bypass

**Locations**

- `./ananke-trusted-supervisor` — untracked Mach-O artifact; not line-addressable
- `cmd/ananke-trusted-supervisor/server_factory_test_runtime.go:1-8`
- `internal/trustedsupervisor/atomic_runtime_test_runtime.go:1-15`

**Observed evidence**

`go version -m ./ananke-trusted-supervisor` reports:

```text
path github.com/yingliang-zhang/ananke/cmd/ananke-trusted-supervisor
build -tags=ananke_test_runtime_authority
```

`go tool nm ./ananke-trusted-supervisor` contains:

```text
github.com/yingliang-zhang/ananke/internal/trustedsupervisor.NewServerWithCompileTimeTestRuntimeAuthority
```

That constructor installs a verifier that returns an empty `atomicRuntimeAuthorityLease` without validating the executable, native addon, or ancestor authority.

The binary is named exactly like the production executable. Its SHA-256 is:

```text
38576bb18bfa9e46ff8d7317068a47d6031c1df747418e183695caafbc055106
```

By contrast, the normal untagged build at `/tmp/ananke-p5-fifth-review-supervisor` has SHA-256:

```text
f697400092fe2d82a12f72f6b555428729dbf1dbb29081e05b06e42ec35a3bec
```

and its symbol inventory contained no test-authority constructor or marker.

**Attack/failure mechanism**

An operator, packaging script, or deployment job can select the repository-root binary because it has the expected production name. That executable routes normal CLI server construction through `NewServerWithCompileTimeTestRuntimeAuthority`, bypassing the immutable root-owned OMP/native boundary. User-owned Homebrew OMP and `~/.omp` native artifacts could consequently become runtime authority despite production source correctly rejecting them.

Source-only tests and normal `go list` checks do not constrain an already-built untracked executable.

**Missing regression**

A release-artifact test must inspect the exact binary selected for installation and fail if any of these are present:

- build tag `ananke_test_runtime_authority`;
- symbol `NewServerWithCompileTimeTestRuntimeAuthority`;
- marker `ananke-compile-time-test-only-runtime-authority-v1`;
- any test-runtime server factory.

It must also prove that the installed artifact is the freshly built untagged artifact, not merely that a separate temporary production build is clean.

**Required fix**

- Remove the tagged binary from the deployable worktree.
- Generate test-authority binaries only in an isolated temporary directory under an unmistakably test-only name.
- Make packaging start from a clean allowlisted source tree and build without the tag.
- Add a release gate over the final installation artifact using both Go build metadata and marker/symbol rejection.
- Never allow an environment variable, flag, configuration field, or fallback path to enable this verifier.

---

### 2. High — Invocation and finalization cleanup authority is path-reopened without durable root identity or ancestor continuity

**Locations**

- `internal/trustedsupervisor/execution_policy.go:440-457`
- `internal/trustedsupervisor/execution_policy.go:911-953`
- `internal/trustedsupervisor/audit_executor.go:208-244`
- `internal/trustedsupervisor/audit_journal.go:51-85`
- `internal/trustedsupervisor/audit_runtime.go:610-660`
- `internal/trustedsupervisor/audit_executor.go:1720-1749`
- `internal/trustedsupervisor/audit_executor.go:1752-1775`
- `internal/trustedsupervisor/audit_executor.go:1819-1824`
- `docs/local-trusted-supervisor-transport-runbook.md:11`

**Attack/failure mechanism**

Execution-policy admission records physical paths and final directory identities, but effect-boundary validation returns without retaining directory descriptors. Subsequent root creation and use are pathname-based:

```go
os.Mkdir(directory, 0o700)
```

Physical resolution also relies on `EvalSymlinks` plus before/after checks, leaving a check/use interval after validation.

Finalization has a second, independently exploitable form of the same defect:

1. Signed `finalizing` events bind root paths, but not the device/inode identities of every created prompt, output, temporary, session, and work root.
2. `auditFinalizingOwnedRoots` reconstructs only strings.
3. `openAuditCleanupParent` accepts whichever same-UID, mode-`0700` directory currently occupies the parent path; it does not compare it with the execution-policy pinned device/inode.
4. `scrubAndRemoveAuditTreeAt` accepts whichever same-UID directory currently occupies the child name.
5. Absence verification establishes only that the expected pathname is absent.

A concurrent same-UID replacement can therefore:

- rename the original owned root or one of its controlling parents;
- place a valid same-UID decoy at the expected pathname;
- let cleanup scrub and remove the decoy;
- restore the policy-pinned parent before the post-cleanup `ValidateEffectBoundary`;
- leave the original audit artifacts reachable under their renamed path;
- allow path-absence verification and a signed `completed` event.

The same A→B→A replacement strategy can redirect invocation-root operations between effect-boundary validations because operations are not anchored to retained parent descriptors. This defeats durable “cleanup before completion” and can retain session/output/source-snapshot material after signed success.

The existing descriptor recursion prevents symlink traversal after a directory has been opened, but it does not authenticate the initially opened directory against an identity signed at creation/finalization.

**Missing regressions**

Required deterministic tests:

1. Place an invocation authority beneath a same-UID writable ancestor; rebind the root between `ValidateEffectBoundary` and `Mkdir`/materialization/start. No operation may occur under the replacement.
2. In `afterFinalizingPersist`, rename each attempt-specific root and both shared roots, place same-UID mode-`0700` decoys at the original names, then restore policy paths before the final effect-boundary check.
3. Assert that:
   - no `completed` event or callback is emitted;
   - the original inode cannot survive under a different name;
   - a decoy is not mistaken for the original authority;
   - restart recovery behaves identically;
   - all attempts plus shared session/work roots are covered.
4. Repeat the matrix under `-race`.

**Required production fix**

- Pin and validate the complete physical ancestor chain for every repository, invocation, allowed-test, and cleanup authority.
- Reject controlling ancestors that are symlinks, unexpectedly owned, ACL/effectively writable by an untrusted principal, or replaceable.
- Retain CLOEXEC parent directory descriptors for the complete live execution and perform creation, opening, verification, and removal through `mkdirat`/`openat`/`fstatat`/`unlinkat`.
- Bind device, inode, owner, mode, and pinned-parent identity for every created owned root into the authenticated `finalizing` authority.
- Compare cleanup descriptors against those signed identities before scrubbing.
- For crash recovery, use a durable trusted namespace that cannot be renamed by the untrusted runtime principal. If that cannot be guaranteed, an identity/path mismatch must remain nonterminal and must never produce `completed`.
- Correct the runbook’s unconditional cleanup claim only after this invariant is enforced.

## Disposition of the eight fourth-review blockers

| # | Prior blocker | Disposition |
|---|---|---|
| 1 | Physical root separation, including `/var` versus `/private/var` | **Closed for the exact prior static-alias defect.** `pinExecutionPolicyPhysicalAuthorities` now canonicalizes and compares invocation, repository, native, and allowed-test authorities, and the focused physical-overlap regressions passed repeatedly and under race. The new check/use and ancestor-replacement defect in blocker 2 is distinct and still prevents acceptance. |
| 2 | Exclusive dual-stack localhost gateway ownership and clean shutdown | **Closed.** The gateway acquires `127.0.0.1` and `::1` on the same port, rolls IPv4 back if IPv6 acquisition fails, tracks both accept loops, and waits for listener/worker shutdown. Ownership, rollback, and reaping tests passed repeatedly and under race. |
| 3 | Reject 301/302/303/307/308 after one request without Authorization leakage | **Closed.** `CheckRedirect` rejects every redirect before a second request. The status matrix verifies exactly one upstream request and no credential arrival at the redirect destination; focused repetition and race passed. |
| 4 | Honest Darwin descriptor limitations; immutable runtime authority; exact XDG native selection; trusted framing; CLOEXEC; early fail-closed; no production test authority | **Not closed at the complete-worktree/deployment-artifact boundary.** The normal source build correctly excludes test authority, retains CLOEXEC descriptors, verifies root-owned ancestor/artifact identities, uses exact `XDG_DATA_HOME`, denies fallbacks, binds bootstrap/wrapper framing, and fails before gateway/credential/child effects. Darwin `/dev/fd` and inherited-native limitations are represented honestly. However, the production-named untracked binary was built with the bypass tag, as detailed in blocker 1. |
| 5 | Signed durable nonterminal `finalizing`, cleanup before completion, crash recovery, replay/history/tamper validation | **Not closed.** Signed `finalizing`, nonterminal reconciliation, restart recovery, finalizing→completed hash continuity, and tamper/history validation exist and their focused tests pass. Cleanup nevertheless authenticates path presence rather than the durable identities of the roots being deleted, permitting false completion after replacement as detailed in blocker 2. |
| 6 | Atomic rejection of populated legacy V2 audit history; empty V2/V1 migration | **Closed.** Populated accepted V2 audit history returns the typed legacy-migration error without in-place signing or schema mutation. Valid empty legacy paths migrate transactionally. Reopen/rollback tests passed repeatedly and under race. |
| 7 | Accurate runbook/ledger claims and schemas | **Partially closed.** The execution-policy v5, journal v4, audit-event v3, evidence v4, provider-free preflight, fake credential, deterministic local rejection, and no-real-canary distinctions are consistent. The unconditional claim that owned roots are scrubbed before completion remains too strong until blocker 2 is fixed. |
| 8 | Stable, bounded, process-aware supervisor termination synchronization | **Closed.** Readiness is published only after TERM handling is installed; wait logic tracks process identity and early exit; TERM/KILL/poll/wait bounds are finite. TERM success, ignored TERM→KILL, kill error, wait timeout, wrong identity, context shutdown, and retained-resource retry tests passed repeatedly and under race. |

## Reproduced commands and results

### Worktree and build inventory

```text
git status --short
```

Result: `0` staged, `17` modified tracked files, `53` untracked files.

```text
git diff --name-status 1bbc880576173913d62f13200ea54b25d46f4393
git ls-files --others --exclude-standard
```

Result: tracked changes and all untracked source, tests, artifacts, and the repository-root binary were enumerated.

```text
go list -f '{{.GoFiles}} | ignored={{.IgnoredGoFiles}}' ./internal/trustedsupervisor
go list -f '{{.GoFiles}} | ignored={{.IgnoredGoFiles}}' ./cmd/ananke-trusted-supervisor
```

Result: normal builds exclude:

```text
atomic_runtime_test_runtime.go
server_factory_test_runtime.go
```

```text
go build -o /tmp/ananke-p5-fifth-review-supervisor ./cmd/ananke-trusted-supervisor
```

Result: PASS.

```text
go version -m ./ananke-trusted-supervisor
go tool nm ./ananke-trusted-supervisor
```

Result: the untracked binary declares `-tags=ananke_test_runtime_authority` and contains `NewServerWithCompileTimeTestRuntimeAuthority`.

Normal untagged binary symbol search: no test-authority constructor or marker.

### Focused prior-blocker matrix

A count-1 matrix covering physical aliases, redirect statuses, dual-stack rollback/shutdown, immutable runtime admission, CLOEXEC descriptors, bootstrap process identity, early effect denial, finalizing history/recovery, migration, and supervisor termination passed:

```text
go test ./internal/trustedsupervisor -run '<focused blocker matrix>' -count=1 -timeout 300s
```

Result: PASS.

The primary prior-blocker matrix was then repeated:

```text
go test ./internal/trustedsupervisor -run '<physical|gateway|runtime-authority|finalizing|migration matrix>' -count=10 -timeout 900s
```

Result: PASS.

```text
go test -race ./internal/trustedsupervisor -run '<physical|gateway|runtime-authority|finalizing|migration matrix>' -count=3 -timeout 900s
```

Result: PASS.

Termination-specific repetition:

```text
go test ./internal/trustedsupervisor \
  -run '^(TestAuditExecutorRestartFailsClosedOnWrongPIDStartIdentity|TestSupervisorAuditTestContextShutdownUsesOwnedTermination|TestProductionServerSupervisorTestTerminationFailureRetainsResourcesUntilCloseRetry)$' \
  -count=10 -timeout 600s
```

Result: PASS.

```text
go test -race ./internal/trustedsupervisor \
  -run '^(TestAuditExecutorRestartFailsClosedOnWrongPIDStartIdentity|TestSupervisorAuditTestContextShutdownUsesOwnedTermination|TestProductionServerSupervisorTestTerminationFailureRetainsResourcesUntilCloseRetry)$' \
  -count=3 -timeout 600s
```

Result: PASS.

### Provider-free installed-OMP gate

An initial invocation without fixture variables skipped, correctly reporting both fixtures were absent.

The prescribed provider-free invocation was then run with the fixed local fixtures:

```text
ANANKE_PINNED_OMP_FIXTURE=/opt/homebrew/Cellar/omp/17.1.3/bin/omp \
ANANKE_PINNED_OMP_NATIVE_FIXTURE=/Users/yingliangzhang/.omp/natives/17.1.3/pi_natives.darwin-arm64.node \
go test ./internal/trustedsupervisor \
  -run '^TestAuditInstalledOMPProviderFreeTransportPreflight$' \
  -count=3 -timeout 300s
```

Result: PASS. It used the fixed fake credential and deterministic loopback rejection. No real provider/model request was made.

This preflight proves transport compatibility only. The user-owned Homebrew/`~/.omp` fixture layout remains inadmissible to the production runtime-authority verifier.

### Full suites

```text
go test ./internal/trustedsupervisor -count=1 -timeout 600s
```

Result: PASS.

```text
go test ./... -count=1 -timeout 600s
```

Result: PASS — six packages passed; three packages had no tests.

```text
go test -race ./... -count=1 -timeout 600s
```

Result: PASS, independently completed by the orchestrator before dispatch. The duplicate review-session invocation was cancelled when that session was disposed; it did not report a test failure.

The fifth-review session did not independently rerun:

```text
go vet ./...
git diff --check
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
node contracts/p4/verify.mjs
node contracts/p4/verify.mjs --self-test
```

Those remain required release gates; no passing claim is made for them here.

## Accepted deployment prerequisites

Deployment is not accepted until both blockers are repaired and independently re-reviewed. The required boundary is:

1. **Artifact provenance**
   - Clean allowlisted source tree.
   - No repository-root or packaged test-authority binary.
   - Untagged build only.
   - Final installation artifact inspected for build tags, test markers, and bypass symbols.

2. **OMP executable authority**
   - Exact immutable executable, such as `/Library/Ananke/omp/17.1.3/bin/omp`.
   - Root-owned complete physical ancestor chain.
   - No supervisor-effective write permission, writable ACL, symlink component, or mutable alias.

3. **Native authority**
   - Exact selected path under the pinned XDG hierarchy, such as `/Library/Ananke/omp-data/omp/natives/17.1.3/pi_natives.darwin-arm64.node`.
   - Root-owned immutable ancestors and file.
   - Exact version/hash/device/inode/mode/size binding.
   - All Home, executable-relative, packaged, and other loader fallbacks denied.

4. **Invocation and cleanup namespace**
   - Parent/root identities protected from same-UID A→B→A replacement.
   - Descriptor-relative live operations.
   - Durable signed root identities and recovery semantics that cannot mistake a decoy for the original root.
   - No completion until the signed original authorities are proven absent.

5. **Gateway**
   - Exclusive availability of both `127.0.0.1` and `::1` on one port.
   - Startup fails closed if either address cannot be acquired.

6. **Journal migration**
   - Populated legacy V2 audit history must be archived/exported and a fresh journal started.
   - No in-place signing or re-signing migration.

7. **Release verification**
   - Focused count/race matrices.
   - Full normal and race suites.
   - `go vet`, contract verifier/self-tests, and `git diff --check`.
   - Exact provider-free preflight with fake credential and deterministic local rejection.

A real provider/model canary remains prohibited until these blockers are fixed and a clean production artifact and deployment authority are independently accepted.
