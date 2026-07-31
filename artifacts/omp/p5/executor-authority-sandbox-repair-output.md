Working...
Implemented blockers 1–3 plus atomic readiness only.

### Repairs

- **Execution-policy authority**
  - Retains canonical policy bytes, SHA-256, and size.
  - Reopens with `O_NOFOLLOW` and rechecks bytes/hash/size/device/inode/owner/mode at each effect boundary.
  - Detects same-inode, same-size in-place mutation.
  - Closed schema now declares pinned runtime read roots, executable roots, and route-specific credential environment names.
  - Production Git restricted to pinned, root-owned `/usr/bin/git`, with identity/content revalidation.

- **Immutable Darwin execution**
  - Wrapper opened with `O_NOFOLLOW`; exact descriptor is `fstat`/size/hash validated before and immediately before `Start`.
  - `sandbox-exec` runs immutable `/bin/bash -s -- <wrapper args>`.
  - Bash reads the already-open wrapper descriptor through stdin—no wrapper pathname execution or fallback.
  - Path replacement between validation and `Start` executes the original opened bytes.
  - In-place descriptor content mutation fails closed.

- **Least-authority sandbox**
  - Removed unrestricted `process*`, `mach-lookup`, `file-read*`, and outbound networking.
  - Added only observed Bash requirements: exact sysctls, pinned runtime roots, canonical `/private` aliases, executable mappings, self/child process operations, DNS, and remote TCP 443.
  - Parent inspection/signalling, private authority reads, unrelated Unix/TCP endpoints, and source/original writes remain denied.
  - Only policy-selected route credentials are inherited.

- **Cleanup**
  - Writable trees are bounded-scanned on every process exit.
  - Failure, cancellation, malformed timeout, evidence rejection, and credential leakage scrub prompt/output/session/tmp/work trees.
  - Scrubbing overwrites regular files, truncates, syncs, then removes.
  - Secret-bearing timeout fails closed; clean timeout remains eligible for bounded session preservation.
  - Verification failures now close as `failed`; attempt-cap remains `waiting_for_human`.
  - Output-tamper test first verifies a retained successful artifact, then tampers it.

- **Atomic readiness**
  - Process test readiness now uses create/write/sync/close/rename plus directory sync.
  - Short `/tmp` Unix socket paths avoid Darwin `sun_path` overflow.

### Verification

All passed:

- Immutable wrapper tests: `-count=10`
- Sandbox adversarial tests: `-count=10`
- Cleanup/timeout/cancellation tests: `-count=5`
- Atomic readiness test: `-count=10`
- Package batch: `go test ./internal/trustedsupervisor -count=3`
- Focused race batch: `-race -count=3`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`

No real OMP/model execution. No commit or push.
