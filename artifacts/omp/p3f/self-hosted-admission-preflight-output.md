Working...
## P3f preflight result — **NOT ADMITTED for real OMP execution**

Preflight completed read-only. No OMP/wrapper launch, source materialization, worktree creation, file edit, or commit occurred.

### Verified facts

- **Canonical repository identity matches P3d**
  - `go.mod`: `github.com/yingliang-zhang/ananke`
  - `origin`: `https://github.com/yingliang-zhang/ananke.git`
  - HEAD commit: `49113ca14b3199a50083422c30b6867764162608`
  - HEAD tree: `48bab71376c91e07fa9df2b5225791041e57f554`

- **Tracked scope is clean**
  - No staged or unstaged tracked changes before/after hashing.
  - 335 tracked entries: 328 mode `100644`, 7 mode `100755`; no symlink or submodule entry was found.
  - Direct checkout scope is nevertheless **not admissible** for a real audit: 210 untracked files exist—3 under `.playwright-cli`, 207 under `artifacts`.

- **P3d contract fixture binding is intact**
  - P3d fixture manifest and all three hard-coded fixture digests verified.
  - Executed successfully:
    ```sh
    node --check contracts/p3d/verify.mjs
    node contracts/p3d/verify.mjs
    node contracts/p3d/verify.mjs --self-test
    ```
  - The normal verifier reads fixtures only; self-test mutates cloned values in memory only.

- **Exact allowed adapter pair**
  ```text
  route:        ananke_omp_read_only_audit_v1
  wrapper_kind: ananke_omp_readonly_wrapper_v1
  provider:     omp
  model:        omp_audit_model_v1
  deadline:     2026-07-30T12:00:00Z
  attempt_cap:  3
  ```
  Current UTC time was `2026-07-24T06:18:49Z`; the frozen deadline has not yet elapsed.

- **No real production activation path exists**
  - The controlled runtime’s `p3eExecAdapter` and `newOMPReadOnlyRuntime` are unexported.
  - Their only construction is test-only: the Go test binary re-executes `TestP3EFakeAdapterExecutable` with a synthetic temporary root and fixture source.
  - No `cmd/`, GUI, daemon, or public lifecycle path constructs a real OMP wrapper or dispatches the route.
  - Thus the route is a **closed contract declaration and equality check**, not a deployable route-to-wrapper registry.

- **P3e fixture behavior is controlled and fail-closed**
  - Exact HostSpec equality is required before start.
  - Source bytes are bound to an independently established seal.
  - Materialization paths are constrained; descriptor/inode checks occur before start and cleanup.
  - Unknown or malformed transcript data, cancellation, stale fence, and source/route drift fail closed.
  - Available but intentionally **not run**:
    ```sh
    go test ./internal/lifecycle -run '^TestP3E' -count=1 -timeout 120s
    ```
    It re-executes a fake adapter and creates temporary roots/materializations, prohibited by this preflight.

## Source snapshot assessment

Two in-memory planning hashes were computed from the pinned, clean Git tree:

| Binding | Value | Status |
|---|---|---|
| P3e-compatible tracked-content hash | `sha256:98c3fdae24db2f4ce43ac26a41b7e7133c7abb224386852c1cc89fa5c2eb4aee` | Planning evidence only |
| Proposed P3f source-manifest hash | `sha256:8af3debba0589d4df2f2cf54aaf6738eb7e298da27ca8fc3ec89a29ff0008598` | Planning evidence only |
| Frozen P3d required source snapshot hash | `sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4` | **Does not match** either current-tree calculation |

The P3e-compatible hash uses the existing `hashP3eMaterializationFiles` scheme:

```text
sort by relative path;
for every file:
  uint64be(path-byte-length) || path-bytes ||
  uint64be(content-byte-length) || content-bytes;
SHA-256 of the complete byte stream.
```

The P3f planning manifest additionally includes immutable repository identity, commit, tree, file mode, relative path, and per-file SHA-256. Its final sealing algorithm must be specified as RFC 8785 JCS plus SHA-256; the planning hash must be recomputed by that approved implementation at admission time.

## Sealed admission-manifest input

Do **not** persist this until a separately authorized actual-audit phase. Seal the canonical manifest excluding `manifest_hash` as:

```text
manifest_hash = "sha256:" + SHA-256(RFC8785_JCS(manifest_without_manifest_hash))
```

```json
{
  "schema_version": "ananke.p3f.self_hosted_audit_admission.v1",
  "decision": "blocked_pending_separate_authorization",
  "target": {
    "repository_identity": "github.com/yingliang-zhang/ananke",
    "head_commit": "49113ca14b3199a50083422c30b6867764162608",
    "head_tree": "48bab71376c91e07fa9df2b5225791041e57f554",
    "tracked_file_count": 335,
    "allowed_git_modes": ["100644", "100755"],
    "source_manifest_schema": "ananke.p3f.source-manifest.v1",
    "source_manifest_hash": "sha256:8af3debba0589d4df2f2cf54aaf6738eb7e298da27ca8fc3ec89a29ff0008598",
    "p3e_compatible_content_hash": "sha256:98c3fdae24db2f4ce43ac26a41b7e7133c7abb224386852c1cc89fa5c2eb4aee"
  },
  "p3d_binding": {
    "host_spec_hash": "sha256:f176da10c1c055b930b17137d790580400c9a0e6963e967290a19cb608512e8b",
    "route": "ananke_omp_read_only_audit_v1",
    "wrapper_kind": "ananke_omp_readonly_wrapper_v1",
    "provider": "omp",
    "model": "omp_audit_model_v1",
    "deadline": "2026-07-30T12:00:00Z",
    "attempt_cap": 3,
    "verification_name": "ananke_contract_verify_v1",
    "verification_mode": "read_only",
    "required_source_snapshot_hash": "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4"
  },
  "scope": {
    "tracked_tree_clean": true,
    "untracked_entry_count": 210,
    "direct_checkout_may_be_exposed_to_omp": false,
    "audit_input_must_be_pinned_tracked_snapshot": true
  },
  "required_before_launch": [
    "successor contract binds the actual source-manifest hash; P3d's opaque snapshot hash alone is insufficient",
    "approved production wrapper exists and its binary hash is sealed",
    "wrapper exposes only the exact P3d route-wrapper pair",
    "complete active P3b fence is authenticated privately",
    "P3c action is exactly retry_process_admission",
    "source is staged outside the checkout into an owned descriptor-bound root",
    "OS-enforced write denial and source-scope confinement are demonstrated",
    "deadline and attempt-cap are rechecked immediately before launch",
    "cleanup plan is descriptor/inode-bound and independently verified"
  ],
  "on_any_mismatch": {
    "launch": false,
    "public_state": "waiting_for_human",
    "events": [],
    "result": null,
    "verification_state": "not_run"
  }
}
```

## Later separate actual-audit sequence

### 1. Recheck the original checkout, read-only

This is expected to reject the current checkout as a direct OMP root because of its untracked files. Do **not** use `git clean`.

```sh
repo="${ANANKE_AUDIT_SOURCE_ROOT:?set canonical checkout root}"
expected_commit="49113ca14b3199a50083422c30b6867764162608"

test "$(git -C "$repo" remote get-url origin)" = \
  "https://github.com/yingliang-zhang/ananke.git"
test "$(git -C "$repo" rev-parse HEAD)" = "$expected_commit"
git -C "$repo" diff --quiet HEAD --
git -C "$repo" diff --cached --quiet
test -z "$(git -C "$repo" ls-files --others --exclude-standard)"
```

### 2. After explicit authorization, create the only writable isolation root

This is **not** authorized or run in this preflight. It stages tracked commit contents only; untracked source-root contents never enter the audit input.

```sh
umask 077
stage="$(mktemp -d "${TMPDIR:-/tmp}/ananke-p3f.XXXXXXXX")"
mkdir "$stage/source" "$stage/evidence"
chmod 700 "$stage" "$stage/source" "$stage/evidence"

git -C "$repo" archive --format=tar "$expected_commit" |
  tar -x -C "$stage/source"
```

Before any OMP launch, an approved source-manifest verifier must recompute and compare the sealed source-manifest hash from the staged root, reject unexpected entry types, and pass only a descriptor for that root to the wrapper.

### 3. Actual OMP command: intentionally unavailable

There is no valid current command for a real self-hosted OMP audit. Constructing one from `p3eExecAdapter`, a bare `omp` executable, an arbitrary path, prompt, or renderer input would violate P3d.

A future approved wrapper must have a narrow, documented interface equivalent to:

```text
<approved-production-wrapper>
  --manifest-fd <inherited-read-only-fd>
  --source-root-fd <inherited-read-only-directory-fd>
  --evidence-root-fd <owned-private-output-fd>
```

The executable digest, route pair, argument schema, sandbox policy, and output schema must be sealed in the admission manifest before use. Credentials, if ever required, must not appear in argv, environment, manifest, transcript, or public evidence.

## Exact red flags

1. **Hard block:** no deployable production OMP wrapper or route dispatcher exists.
2. **Hard block:** P3d’s frozen required-source hash does not match either current source calculation and has no declared derivation from the real checkout.
3. **Hard block:** 210 untracked entries make the current checkout unsafe as a directly exposed audit root.
4. **Hard block:** P3d itself explicitly grants no process launch, source collection, materialization, or verification-command execution authority.
5. **Required isolation:** P3e’s descriptor checks are useful controlled-runtime evidence, but [INFERENCE] a descriptor and `cmd.Dir="/"` alone do not confine an arbitrary real child process from the broader filesystem. A future real wrapper needs OS-enforced source allowlisting and write denial.
6. **Required cleanup:** cleanup must operate only on an owned, descriptor/inode-validated staging root; it must never recursively delete a lexical path supplied by the audit payload.
7. **Required launch-time gate:** recheck deadline, active full fence, sealed source digest, wrapper binary identity, route pair, and cleanup ownership immediately before process creation.

No repository report file was created.
