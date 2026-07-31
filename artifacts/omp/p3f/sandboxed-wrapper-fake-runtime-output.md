Working...
Implemented P3f private sandboxed fake-child runtime.

**Changed**
- `internal/lifecycle/p3f_runtime.go`
  - Private fake-child-only runtime; no command, daemon, UI, or production callsite.
  - Stages only from a regular inherited descriptor for a pinned Git tar archive:
    - validates Git PAX commit;
    - validates ordered opaque manifest entry hashes and canonical manifest hash;
    - rejects plain/untracked archives.
  - Passes source, manifest, and evidence exclusively as inherited FDs.
  - Validates complete P3b fence, P3c action, exact deadline, P3d HostSpec/source bindings, source-manifest hash, and wrapper SHA-256/kind/route both before staging and again in final fence admission.
  - Darwin Seatbelt adapter uses `sandbox-exec` with `deny file-write*`; unavailable capability fails closed.
  - Descriptor/inode-bound cleanup closes owned descriptors and preserves foreign replacements.
  - Public output is always the closed normalized `waiting_for_human`, `result: null` shape.

- `internal/lifecycle/p3f_runtime_test.go`
  - Deterministic test-binary child only—no OMP, real wrapper, real Ananke target, or credentials.
  - Creates temporary synthetic Git archives; proves source read via inherited FD and OS-enforced write denial while source remains DAC-writable.
  - Covers credential-free child argv/environment, FD-only manifest/source/evidence, source/wrapper/fence/action/deadline drift, final-boundary revalidation, untracked archive rejection, unsupported sandbox fail-close, and descriptor replacement preservation.

- `docs/experiment-ledger.md`
  - Recorded TDD, platform sandbox evidence, verification gates, and remaining production boundary.

**Platform sandbox result**
- Observed on this Darwin host: `/usr/bin/sandbox-exec` ran the deterministic fake child.
- Focused test recorded: `P3f Darwin Seatbelt sandbox enforced inherited-source reads and denied writes`.

**TDD / verification**
- RED established first: focused P3f test compilation failed on absent runtime/sandbox/manifest symbols.
- `go test -v ./internal/lifecycle -run '^TestP3F' -count=1 -timeout 60s` — PASS.
- `go test ./... -count=1 -timeout 300s` — PASS.
- `go test -race ./... -count=1 -timeout 360s` — PASS.
- `go vet ./...` — PASS.
- P3d/P3f contract syntax, normal verification, and in-memory self-tests — PASS.

**Remaining activation boundary**
- This remains fake-child-only. It does not hash or invoke a real wrapper, target a real checkout, call OMP, connect externally, emit audit results, or grant production launch authority.
- A real activation still requires separately accepted production-wrapper identity proof, production sandbox design, controlled source authority, and explicit authorization.
