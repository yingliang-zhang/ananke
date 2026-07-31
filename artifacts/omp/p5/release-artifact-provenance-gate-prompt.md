Repair fifth-review blocker 1 only: repository-root production-named tagged binary / release artifact provenance. The offending untracked `./ananke-trusted-supervisor` has been removed. Read fifth-review-report-recovered.md lines 11-73. Strict TDD, no provider/model, no commit.

Implement a durable release-artifact gate:
- Prevent accidental repository-root `ananke-trusted-supervisor` build artifacts (exact .gitignore entry, not broad source ignores).
- Add a production build workflow/script that always builds the untagged cmd to an isolated temp path, verifies that exact candidate, then atomically publishes to an explicit operator-selected output path. Fail if GOFLAGS or any build setting injects `ananke_test_runtime_authority`; do not silently clear attacker/operator flags and claim provenance.
- Add a verifier for an exact binary path. It must use Go build metadata and binary marker/symbol checks to reject: build tag `ananke_test_runtime_authority`; symbol/name `NewServerWithCompileTimeTestRuntimeAuthority`; marker `ananke-compile-time-test-only-runtime-authority-v1`; wrong package path; any test-runtime server factory. It must pass a fresh normal untagged build and fail a tagged build, even when the binary has the production filename.
- Do not make test authority runtime-enableable by env/flag/config. Keep tagged test binary only in t.TempDir with unmistakable test-only path.
- Add tests for normal candidate PASS, tagged candidate FAIL, copied/renamed tagged binary FAIL, GOFLAGS tag injection FAIL, no repository-root binary after tests/build workflow.
- Document the exact release command/gate; no claim of installed artifact until run on the final output.
- Do not touch blocker 2 cleanup authority.

Run focused count=10, race=3 if applicable, go test cmd/internal relevant, full package, git diff check. Report files and commands.