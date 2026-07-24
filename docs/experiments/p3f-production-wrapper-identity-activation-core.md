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

Real production execution remains blocked. The separate
`p3f-production-exec-by-fd-artifact-design-contract` now records that the
locally inspected Darwin macOS 27 SDK exposes no admitted native FD image
selector; P3f therefore permits only `none_fail_closed` before a child. A
caller digest, dynamically built artifact, test fixture, or artifact that
validates only its own supplied digest remains invalid provenance.

## `ananke_hybrid_v1` typed-role boundary — no runtime integration

The successor design fixture reserves the non-executable
`ananke.hybrid-v1-typed-role-boundary.v1`: `local_wrapper_executor` may consume
only the fixed FD contract; `moa_route_selector` and `moa_provider_delegate`
are not admitted without a future signed route grant; and
`transcript_evidence_recorder` may append typed hash-bound evidence only. A
role label grants no route authority. Fallback is forbidden.

No `ananke_hybrid_v1` runtime schema/API, persistence, route selection, MoA
routing, wrapper execution, OMP invocation, or fallback is integrated here.
The design fixture has no artifact acceptance or launch authority. A later
accepted implementation must retain P3f's FD-only, independently trusted
identity, and fail-closed constraints before it can participate in activation.
