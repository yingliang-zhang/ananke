Working...
Implemented the P3f **design-only** exec-by-FD / independently trusted-wrapper contract.

- **Darwin decision:** local MacOSX 27 SDK inspection found path-based `exec*`/`posix_spawn*`, FD inheritance/transport helpers, and signature-loading controls—but no declared `fexecve`, `execveat`, or `AT_EMPTY_PATH`. The sole allowed Darwin mechanism is now `none_fail_closed`, before any child exists. `/dev/fd`, pathname launch, spawn, fileport transport, and test-binary re-exec are explicit denial vectors.
- **New immutable fixtures:**
  - `contracts/p3f/fixtures/production-exec-fd-design-v1.canonical.json`
  - `contracts/p3f/fixtures/exec-fd-red-flags-v1.canonical.json`
  - Updated `contracts/p3f/fixtures/fixtures.sha256`
- **Chain verification:** `contracts/p3f/verify.mjs` now validates **P3d → existing P3f activation → exec-by-FD successor**, including P3f wrapper/source/route bindings.
- **Future artifact boundary:** requires detached independent-release attestation and approval bound to wrapper SHA-256, P3d route pair, artifact protocol, and build identity. Caller digests, self-consistent pairs, dynamic builds, and test fixtures fail closed. No artifact is accepted now.
- **Policy frozen:** fixed FD 3/4/5 inheritance, selector-only wrapper FD, OS-enforced sandbox requirements, empty credential-free environment, fixed non-secret argv, typed transcript/evidence schemas, replacement-safe cleanup, fence-authenticated cancellation/recovery, and `ananke_hybrid_v1` typed roles with absent runtime integration and forbidden fallback.
- **Fake/real separation:** the existing Go fake executor remains test-only and is explicitly prohibited from serving as real-wrapper provenance.

Documentation and ledger:
- `docs/experiments/p3f-production-exec-by-fd-artifact-design-contract.md`
- `docs/plans/2026-07-24-p3f-production-exec-by-fd-artifact-design-contract.md`
- Updated existing P3f contract/core docs and `docs/experiment-ledger.md`.

Verification passed:

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
go test ./internal/lifecycle -run '^TestP3FProductionBuildExcludesFakeExecution$' -count=1 -timeout 60s
```

No production execution, real OMP/wrapper/target/staging, descriptor opening, sandbox application, commit, or push occurred.
