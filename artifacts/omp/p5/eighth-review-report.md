Working...
# ACCEPT

No blocking correctness findings in the requested scope. Review was read-only; no repository files were modified and no remote services were used.

## 1. Build-interval object consistency

Confirmed.

- The Darwin guard is established over the retained compiler, compiler parent directory, repository root, and repository parent directory before launch: `internal/releaseartifact/launch_guard_darwin.go:34-74`, `internal/releaseartifact/build.go:110-117`.
- Each watch requests `NOTE_WRITE`, `NOTE_DELETE`, `NOTE_EXTEND`, `NOTE_ATTRIB`, `NOTE_LINK`, `NOTE_RENAME`, and `NOTE_REVOKE`: `internal/releaseartifact/launch_guard_darwin.go:65-68`.
- Observed mutations become permanently sticky through `guard.mutated`; later checks fail without clearing that state: `internal/releaseartifact/launch_guard_darwin.go:77-108`.
- Pure `NOTE_ATTRIB` noise is ignored only when mode, ownership, flags, link count, size, mtime, and ctime still equal the captured fingerprint. Fingerprint failures fail closed: `internal/releaseartifact/launch_guard_darwin.go:14-24`, `internal/releaseartifact/launch_guard_darwin.go:96-102`, `internal/releaseartifact/launch_guard_darwin.go:111-121`.
- The first guard check occurs after `Start`/`Wait` and after the regression restoration hook: `internal/releaseartifact/build.go:145-156`. A second sticky check occurs immediately in the pre-publication validation sequence: `internal/releaseartifact/build.go:196-211`.
- Repository-root and parent pathname identities are checked before launch, after build, and before publication: `internal/releaseartifact/build.go:118-123`, `internal/releaseartifact/build.go:160-165`, `internal/releaseartifact/build.go:196-201`.
- The compiler retains descriptor identity, pathname identity, build metadata, and SHA-256 validation: `internal/releaseartifact/build.go:320-372`, `internal/releaseartifact/build.go:375-397`.
- The compiler ABA regression genuinely installs and executes a forwarding substitute, restores the original names after successful build completion, requires a sticky mutation rejection, and proves no publication: `internal/releaseartifact/releaseartifact_test.go:296-338`.
- The repository ABA regression starts with an uncompilable retained repository, swaps in a valid expected-package repository, restores the original names after the successful build, and requires rejection with no publication: `internal/releaseartifact/releaseartifact_test.go:341-377`.

## 2. Candidate ownership and cleanup

Confirmed.

- One random hidden file is created directly beneath the pinned output-directory FD using `Openat(O_RDWR|O_CREAT|O_EXCL|O_CLOEXEC|O_NOFOLLOW)`: `internal/releaseartifact/filesystem.go:127-132`, `internal/releaseartifact/filesystem.go:143-161`.
- Ownership is derived from `Fstat` of the returned FD, not by reopening the pathname—even after the regression hook runs: `internal/releaseartifact/filesystem.go:162-175`.
- That same candidate descriptor is retained as child FD 3, checked after the build, chmodded, fsynced, verified, and matched back to the hidden link: `internal/releaseartifact/build.go:95-108`, `internal/releaseartifact/build.go:130-137`, `internal/releaseartifact/build.go:169-195`.
- Publication uses Darwin `RenameatxNp(..., RENAME_EXCL)`: `internal/releaseartifact/publication_darwin.go:7-8`.
- The descriptor remains retained through publication and final device/inode/hash proof: `internal/releaseartifact/build.go:211-229`, `internal/releaseartifact/verifier.go:152-205`.
- Cleanup compares the hidden entry against the atomically created inode and refuses to unlink a replacement: `internal/releaseartifact/filesystem.go:211-230`.
- The replacement-before-identity-capture regression moves the created inode, installs a foreign replacement, confirms cleanup refusal, and verifies both the foreign replacement and moved created object survive: `internal/releaseartifact/releaseartifact_test.go:380-413`.

## Existing invariants

- **Existing destination preservation:** early absence check plus atomic no-replace publication: `internal/releaseartifact/build.go:81-84`, `internal/releaseartifact/build.go:211-213`.
- **No destructive rollback:** the only unlink in scoped production code targets `candidate.name`; the published destination is never removed: `internal/releaseartifact/filesystem.go:211-230`.
- **Closed environment / PATH independence:** caller `GO*`, `CGO_*`, and native-tool selectors are rejected; the child receives a newly constructed environment, fixed PATH, trusted GOROOT, disabled workspace/environment overrides, and `CGO_ENABLED=0`: `internal/releaseartifact/build.go:245-307`. The invoked Go pathname is absolute: `internal/releaseartifact/build.go:59-61`, `internal/releaseartifact/build.go:86-94`, `internal/releaseartifact/build.go:130-136`.
- **One-descriptor verification:** one `O_NOFOLLOW` descriptor supplies stable bytes, build metadata, native symbols, and SHA-256 state: `internal/releaseartifact/verifier.go:45-75`, `internal/releaseartifact/verifier.go:78-121`, `internal/releaseartifact/verifier.go:124-185`, `internal/releaseartifact/verifier.go:208-271`.
- **Output-directory replacement detection:** retained and reopened directory device/inode identities must agree: `internal/releaseartifact/filesystem.go:69-117`; checks surround build and publication in `internal/releaseartifact/build.go:73-84`, `internal/releaseartifact/build.go:127-129`, `internal/releaseartifact/build.go:208-229`.
- **Final proof:** linked device/inode/size is checked before and after rehashing the retained descriptor: `internal/releaseartifact/verifier.go:187-205`.

## Verification run

All passed:

- Three named regressions, verbose single run: **PASS** (`2.299s`).
- Exact three-regression matrix, `-count=10`: **PASS** (`11.380s`).
- Exact three-regression race matrix, `-race -count=3`: **PASS** (`4.901s`).
- `go test ./internal/releaseartifact -count=1`: **PASS** (`11.236s`).
- `go test ./cmd/ananke-trusted-supervisor-release -count=1`: **PASS** (`3.314s`).
- `go test -race ./internal/releaseartifact -count=1`: **PASS** (`12.644s`).
- `go test -race ./cmd/ananke-trusted-supervisor-release -count=1`: **PASS** (`3.879s`).
- Scoped `go vet`: **PASS**.
