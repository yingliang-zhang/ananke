# P3f production-wrapper identity / FD activation core

**Status:** implemented as an inert, private lifecycle core. It is not a
wrapper launcher, an OMP adapter, or an activation authority.

## Current boundary

The core accepts only P3f's exact frozen wrapper identity manifest. At
preparation it requires the P3f wrapper kind/route and binary identity, P3d
source and HostSpec hashes, P3f source-manifest hash, exact deadline, P3c
`retry_process_admission`, and a complete active P3b/P3c fence. It returns
only the same three typed source/manifest/evidence descriptors; it opens,
reads, duplicates, closes, or executes none of them.

`omp_production_fake_execution_test.go` contains the entire fake executor. It
checks the fixed `testdata/omp-production-fake-wrapper-v1` artifact against its
frozen SHA-256, but executes only the fixed Go test binary as
`TestOMPProductionFakeWrapperChild`; no artifact path becomes a launcher. The
compiler `GoFiles` test rejects a production build that includes that fake
runtime or the removed production execution source. The production package
therefore has no FD-to-path launcher surface, executable, command, argv,
environment, program, or path field. Invalid, expired, or unknown input
returns only the stable activation-preparation denial and no partially prepared
request.

Real production execution remains blocked. It requires a separately approved
design for exec-by-FD plus independently trusted artifact identity; a caller
digest, a dynamically built artifact, or an artifact that validates only its
own supplied digest is not an approval mechanism.

## Future `ananke_hybrid_v1` inputs — out of scope

These are non-binding design inputs for a later separately accepted design:

- an explicit hybrid-mode identity and version;
- a route-selection authority between a locally pinned wrapper and any future
  MoA path;
- provider/model and delegation capability identities;
- credential and evidence ownership boundaries; and
- a final-boundary proof that the selected route still matches the active
  fence, deadline, and source bindings.

No `ananke_hybrid_v1` schema, API, persistence, route selection, MoA routing,
wrapper execution, OMP invocation, or fallback is integrated here. A future
hybrid design must retain P3f's FD-only, pinned-identity, and fail-closed
constraints before it can participate in any activation path.
