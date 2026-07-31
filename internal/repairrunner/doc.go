// Package repairrunner implements the controlled-repair runtime that ties
// together worktree materialization, adapter execution, test execution, and
// signed attestation production.
//
// The repair runner orchestrates the P6 controlled-repair flow:
// 1. Materialize a git worktree from a descriptor (Step 5)
// 2. Run a provider-free fake adapter with UID terminal proof (Step 6)
// 3. Run a closed offline Go test profile with the same UID proof (Step 7)
// 4. Produce a signed attestation and verify it with Ananke (Step 8)
package repairrunner
