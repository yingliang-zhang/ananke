Working...
Implemented both seventh-review HIGH fixes.

### Changed files

- `internal/releaseartifact/build.go`
- `internal/releaseartifact/filesystem.go`
- `internal/releaseartifact/launch_guard_darwin.go`
- `internal/releaseartifact/launch_guard_unsupported.go`
- `internal/releaseartifact/releaseartifact_test.go`

Earlier changes retained in the resumed session:

- `internal/releaseartifact/verifier.go`
- `internal/releaseartifact/publication_darwin.go`
- `internal/releaseartifact/publication_unsupported.go`

No release CLI source changes were required.

### Fixes

- Added a Darwin `kqueue`/`EVFILT_VNODE` launch guard before process launch.
  - Watches the retained compiler object and its parent.
  - Watches the retained repository object and its parent.
  - Sticky rejection for write, delete, extend, metadata, link, rename, and revoke events.
  - Pure access-time attribute events caused by executing the compiler are ignored only when the retained vnode’s mode, owner, flags, links, size, mtime, and ctime remain unchanged.
  - Guard checked immediately after child exit and again before publication.
  - Existing inode/hash/path checks remain.
  - Non-Darwin builds fail before launching because no equivalent guard is implemented.
- Added deterministic ABA tests:
  - A substituted compiler actually executes and completes a valid build, then the original compiler pathname is restored. Publication is rejected and output remains absent.
  - A substituted repository successfully builds an inert expected-package binary while the retained repository is deliberately uncompilable; the original repository pathname is restored before post-build checks. Publication is rejected and output remains absent.
- Removed the staging-directory lifecycle.
  - A random hidden candidate is created directly under the pinned output-directory FD with one `Openat(O_CREAT|O_EXCL|O_NOFOLLOW)` call.
  - The returned FD and inode establish ownership atomically.
  - Build, verification, fsync, and publication use that retained FD.
  - Publication is `renameatx_np(..., RENAME_EXCL)` within the same pinned directory.
  - Cleanup removes the hidden name only if it still names the atomically created inode.
  - Regression confirms a foreign replacement survives cleanup.

### TDD and verification

RED:

```text
go test ./internal/releaseartifact \
  -run 'Test(BuildRejectsCompilerPathABA|BuildRejectsRepositoryPathABA|DirectCandidateCleanup)' \
  -count=1
```

Failed as expected with undefined production primitives:

```text
undefined: buildOptions
undefined: buildAndPublishTrustedSupervisor
undefined: createStagedCandidate
```

Targeted GREEN:

```text
go test ./internal/releaseartifact \
  -run 'Test(BuildRejectsCompilerPathABA|BuildRejectsRepositoryPathABA|DirectCandidateCleanup)' \
  -count=1 -v
```

Result: `1 packages ok`.

Final package and CLI:

```text
go test ./internal/releaseartifact ./cmd/ananke-trusted-supervisor-release -count=1
```

Result: `2 packages ok`.

Focused repetition:

```text
go test ./internal/releaseartifact \
  -run 'Test(BuildRejectsCompilerPathABAThatExecutedSubstitute|BuildRejectsRepositoryPathABAThatBuiltSubstitute|DirectCandidateCleanupPreservesReplacementBeforeIdentityCapture|BuildAndPublishRefusesExistingDestinationWithoutChangingBytes|BuildAndPublishRejectsEveryCallerGOFLAGSIncludingOverlay|BuildUsesPinnedGoExecutableNotCallerPATH|RenameNoReplaceIsAtomicUnderRace|PinnedOutputDirectoryDetectsRebind)' \
  -count=10
```

Result: `1 packages ok`.

Race repetition:

```text
go test -race ./internal/releaseartifact ./cmd/ananke-trusted-supervisor-release -count=3
```

Result: `2 packages ok`.

Formatting and static analysis:

```text
gofmt -w internal/releaseartifact/*.go cmd/ananke-trusted-supervisor-release/*.go
go vet ./internal/releaseartifact ./cmd/ananke-trusted-supervisor-release
git diff --check -- internal/releaseartifact cmd/ananke-trusted-supervisor-release
```

All exited successfully with no output. A final post-format package/CLI run also passed.

### Darwin limitation

Darwin still does not execute the compiler or select the working directory directly by retained FD. The implementation makes no such claim. It uses sticky vnode mutation detection plus retained identity/hash checks and refuses publication after any observed compiler/repository ABA mutation.

The repository reports both release directories as untracked, so `git diff --check` produced no tracked diff to inspect. `gofmt`, `go vet`, all tests, and race tests covered the actual Go files. No commit or documentation/ledger changes were made.
