Working...
# CHANGES REQUESTED

Both prior findings are improved, but neither is completely closed.

## 1. HIGH — Release packaging can overwrite destinations and verify a replaced/non-production artifact

**Evidence**

- Repository-root `ananke-trusted-supervisor` is absent. `.gitignore:6` contains the exact root entry.
- Normal artifacts pass and tagged artifacts are rejected by `internal/releaseartifact/verifier.go:45-78`.
- However, `internal/releaseartifact/build.go:66` uses `os.Rename(candidatePath, outputPath)` without an atomic no-replace condition. On Darwin this replaces an existing destination.
- Candidate and published verification repeatedly reopen pathnames:
  - `internal/releaseartifact/verifier.go:37`
  - `internal/releaseartifact/verifier.go:45`
  - `internal/releaseartifact/verifier.go:59`
  - `internal/releaseartifact/verifier.go:78`
- `internal/releaseartifact/build.go:40-52` passes the supplied environment to `go build`; `rejectInjectedGOFLAGS` at `build.go:85-97` rejects only the named test tag, not other source-changing inputs such as `-overlay`.

**Adversarial results**

1. An existing sentinel destination was supplied to the release CLI. It returned success, printed `published and verified exact artifact`, and replaced the sentinel.
2. `GOFLAGS=-overlay=<file>` replaced the supervisor `main.go` with an inert program. The build pipeline published and verified it successfully because its Go package path remained correct and it lacked the known test markers.
3. A controlled `go tool nm` wrapper replaced a verified normal pathname with the tagged binary after symbol enumeration. Verify mode returned success while the selected pathname contained the tagged artifact.

**Required fix**

- Publish with atomic no-replace semantics, such as `renameatx_np(..., RENAME_EXCL)` on Darwin.
- Secure and descriptor-pin the output directory.
- Open the candidate once with `O_NOFOLLOW`; verify build metadata, bytes, hash, and symbols against the same device/inode descriptor.
- Ensure the published inode is the verified inode and cannot be replaced before success is reported.
- Reject non-allowlisted `GOFLAGS` and source/toolchain-changing build inputs, including `-overlay`, `-toolexec`, arbitrary tags, and an untrusted `go` selected through `PATH`.

## 2. HIGH — “Stable parent” is inferred from mode bits without validating Darwin ACL authority

The durable identity design itself is substantially correct:

- Evidence v5 and `owned_roots`: `internal/trustedsupervisor/audit_evidence.go:20`, `124-173`
- Exact ordered inventory and parent-before-child validation: `audit_evidence.go:303-424`
- Signed evidence drives finalization: `internal/trustedsupervisor/audit_runtime.go:636-709`
- Callback replay revalidates signed identities and absence: `audit_evidence.go:427-435`, `server.go:419-445`
- Same-path/new-inode decoys, nested roots, path-only finalization, multi-attempt roots, and replay recreation are rejected by the traced production paths.

The remaining defect is namespace admission:

- `internal/trustedsupervisor/namespace_authority.go:188-217` checks root ownership and Unix mode bits only.
- It does not inspect Darwin ACL entries or prove that the configured runtime UID/GID lacks effective `add_file`, `delete_child`, rename, or write authority.
- Production nevertheless sets the namespace as stable at `namespace_authority.go:122-126`.
- Once marked stable, a missing signed child is treated as already cleaned at `namespace_authority.go:793-797`.

**Failure mechanism**

A root-owned ancestor can have non-writable Unix mode bits while a Darwin ACL grants the runtime principal deletion/rename authority. Admission accepts that hierarchy. After signed `finalizing`, the runtime principal can rename the original root away without creating a decoy. Cleanup sees `ENOENT`, treats it as successful prior cleanup because `stable == true`, verifies pathname absence, and can append `completed` while the signed original inode survives elsewhere.

Additionally, snapshot staging and publication still reopen absolute paths at `internal/trustedsupervisor/audit_snapshot.go:94-137`, rather than operating through the retained work-root descriptor. An ACL-writable work parent therefore restores a creation-time path-rebinding surface.

**Required fix**

- Inspect every physical ancestor and configured namespace root with Darwin ACL APIs.
- Reject any ACL/effective permission that lets the runtime UID/GID or an inherited group add, delete, rename, or write.
- Set `stable` only after that effective-authority proof.
- Make snapshot staging, extraction, publication, and synchronization descriptor-relative.
- Add live and restart regressions where an ACL-authorized runtime principal renames the signed original away and leaves the expected name absent. The state must remain `finalizing`; no callback or `completed` event may be produced.

## Non-success states

This portion is closed. `completed` is constructed only from signed `finalizing` in `internal/trustedsupervisor/audit_runtime.go:636-667`. `internal/trustedsupervisor/audit_journal.go:475-517` rejects completion from `failed`, `timed_out`, `cancelled`, or `waiting_for_human`.

## Verification

Passed locally during this review:

- Release package/CLI tests
- Focused owned-root/finalization matrix
- Focused race matrix
- `go test ./... -count=1`
- `go vet ./...`
- `git diff --check`
- P3d/P3f/P4 normal verifiers and self-tests

These gates do not cover the successful existing-destination replacement, `GOFLAGS=-overlay` publication, verifier pathname replacement, or ACL-authorized rename-without-decoy cases above.
