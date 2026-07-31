Working...
Implemented P3f production-wrapper identity/activation core.

- Added `internal/lifecycle/omp_production_activation.go`.
  - Validates a constructor-approved, typed wrapper identity manifest.
  - Requires exact P3f wrapper kind/route, deadline, P3c action, P3d HostSpec/source hashes, source-manifest hash, and complete active P3b/P3c fence.
  - Produces only an inert typed FD request containing source/manifest/evidence descriptors.
  - Rejects nil context, invalid/aliased descriptors, expired/drifted identity, source, route, deadline, and fence inputs with one sanitized denial and no partial request.
  - Contains no executable, argv, environment, path, program, or OMP invocation surface.

- Added strict-TDD coverage in `internal/lifecycle/omp_production_activation_test.go`.
  - Seals and verifies fake artifact SHA-256:
    `internal/lifecycle/testdata/omp-production-fake-wrapper-v1`
  - Covers wrapper identity/routing, deadline, full fence, P3d/source bindings, FD-only shape, nil context, and fail-closed behavior.
  - AST guard verifies the production core imports no `os/exec` and exposes no execution configuration fields.

- Added out-of-scope note:
  `docs/experiments/p3f-production-wrapper-identity-activation-core.md`
  - Records future `ananke_hybrid_v1` inputs only.
  - Explicitly excludes MoA routing, hybrid schema/API/persistence, wrapper execution, fallbacks, and OMP invocation.

Verification passed:

- `go test ./internal/lifecycle -run '^(TestP3F|TestOMPProductionActivation)' -count=1 -timeout 180s`
- `go test ./... -count=1 -timeout 300s`
- `go test -race ./... -count=1 -timeout 300s`
- `go vet ./...`
- `gofmt -d internal/lifecycle/omp_production_activation.go internal/lifecycle/omp_production_activation_test.go`
- Normal and `--self-test` contract verification for P1a, P1c, P2a, P2c, P3a, P3d, and P3f.

No commit or push performed.
