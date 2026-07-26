Working...
# CHANGES REQUESTED

## A. Release artifact builder — CHANGES REQUESTED

### 1. HIGH — The retained compiler and repository descriptors do not control the process actually executed

- The builder validates the retained repository and Go executable immediately before launch: `internal/releaseartifact/build.go:90-98`.
- It then executes the mutable pathname `goTool.path`, not `goTool.file`: `internal/releaseartifact/build.go:99-105`.
- Likewise, the child working directory is the caller-selected pathname `repositoryRoot`, not the retained repository descriptor: `internal/releaseartifact/build.go:103`.
- Post-build validation occurs only after the child exits: `internal/releaseartifact/build.go:109-114`, with another late check at `internal/releaseartifact/build.go:154-158`.
- The retained Go descriptor is used for metadata, hashing, and pathname comparison, but never as execution authority: `internal/releaseartifact/build.go:274-308`, `internal/releaseartifact/build.go:316-338`.

A concurrent actor can replace either pathname after the pre-launch checks, run a different compiler or repository, and restore the original binding before the post-build checks. A binary built from substituted source can retain the expected package path, omit build tags and forbidden markers, and pass the artifact checks at `internal/releaseartifact/verifier.go:78-121`.

Required: bind process launch to the validated compiler and repository objects, or otherwise fail closed against an ABA replacement. Add a deterministic regression that substitutes the compiler or repository for the actual build and restores the original pathname before post-build validation.

### 2. HIGH — Staging-directory cleanup can attribute and delete a replacement directory

- Staging creation uses separate `Mkdirat` and `Openat` operations: `internal/releaseartifact/filesystem.go:155-164`.
- The identity returned by the later `Openat` is recorded as if it were the directory created by `Mkdirat`: `internal/releaseartifact/filesystem.go:165-179`.
- Cleanup removes the stage whenever the current name matches that recorded identity: `internal/releaseartifact/filesystem.go:247-256`.

If the newly created directory is renamed and replaced between `Mkdirat` and `Openat`, the builder records the replacement’s identity. Cleanup can then remove that foreign replacement, while the directory actually created by this invocation remains elsewhere. This violates the requirement that cleanup remove only invocation-created objects.

Required: establish non-substitutable ownership of the created staging directory before cleanup can authorize removal. Add a regression that replaces the stage between creation and opening and proves the replacement survives.

### Passing portions

- Darwin publication uses descriptor-relative `RENAME_EXCL`: `internal/releaseartifact/publication_darwin.go:7-8`.
- Artifact metadata, bytes, digest, package identity, marker checks, and native symbols derive from one retained descriptor/read: `internal/releaseartifact/verifier.go:78-121`, `internal/releaseartifact/verifier.go:124-175`.
- Published pathname, inode, size, and digest are reproved against the retained artifact: `internal/releaseartifact/verifier.go:187-205`.
- Existing-destination, caller-environment, build-tag, marker, and primitive concurrent no-replace tests passed.

The nominal selected-file-change test is not evidence of an actual replacement: its replacement script is installed as a fake `go`, but verification correctly never invokes it, so lines `internal/releaseartifact/releaseartifact_test.go:243-251` only prove the fake tool was not called.

## B. Workspace directory lifecycle — ACCEPT

No blocking correctness issue found in the reviewed paths.

Evidence:

- Production options require root-owned hierarchy admission: `internal/trustedsupervisor/namespace_authority.go:122-125`.
- Every physical component is opened with `O_NOFOLLOW` and checked before `stable` is set: `internal/trustedsupervisor/namespace_authority.go:150-162`, `internal/trustedsupervisor/namespace_authority.go:178-246`.
- Darwin rejects the entire extended ACL object rather than partially interpreting it: `internal/trustedsupervisor/namespace_acl_darwin.go:15-29`, `internal/trustedsupervisor/namespace_acl_darwin.go:36-43`.
- Non-Darwin or CGO-disabled builds report no supported inspection, which admission rejects: `internal/trustedsupervisor/namespace_acl_unsupported.go:1-7`, `internal/trustedsupervisor/namespace_acl.go:11-17`.
- Snapshot staging, source creation, extraction, sealing, publication, synchronization, and capture use retained leases/descriptors: `internal/trustedsupervisor/audit_snapshot.go:102-171`.
- Captured work/source identities are checked against the already-open descriptors: `internal/trustedsupervisor/audit_snapshot.go:182-202`.
- Evidence v5 requires the exact ordered, parent-linked inventory: `internal/trustedsupervisor/audit_evidence.go:20`, `internal/trustedsupervisor/audit_evidence.go:303-424`.
- Finalization decodes that signed inventory, cleans it, verifies absence, and only then constructs `completed`: `internal/trustedsupervisor/audit_runtime.go:636-709`.
- Journal validation permits direct completion only from exact `finalizing`; failed and cancelled are terminal, while timed-out may only retry or close non-completed: `internal/trustedsupervisor/audit_journal.go:475-490`. Exact finalizing/completed continuity is enforced at `internal/trustedsupervisor/audit_journal.go:503-517`.
- Reconcile revalidates inventory and absence before projecting a completed callback: `internal/trustedsupervisor/server.go:421-445`, `internal/trustedsupervisor/server_protocol.go:449-475`.

## Verification performed

All passed locally:

- Release focused package tests: `-count=1`
- Release core matrix: `-count=3`
- Release focused race matrix
- Release CLI tests
- Workspace focused namespace/snapshot/finalization/callback matrix: `-count=1`
- Workspace core matrix: `-count=3`
- Workspace focused race matrix
- Native Darwin ACL cases, including named-user, named-group, everyone, inherited ACL, live recovery, and restart recovery
- `CGO_ENABLED=0` closed ACL-admission unit seam

Release tests ran under `sandbox-exec` with network denied. Other Go invocations used `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local`, and `GOENV=off`. No remote services or review edits were used.
