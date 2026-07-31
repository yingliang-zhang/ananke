Resume and COMPLETE the P6 Slice 4 Repair P1+P2 implementation. Your previous turn was cut by the internal deadline mid-implementation: `go vet ./internal/repaircontract` currently fails with `repository_worktree_registry_test.go: cannot use fixture.installation (RepositoryWorktreeInstallationAuthority) as *VerifiedRepositoryWorktreeInstallation` — the production signature change is in, but callers/helpers are not fully migrated. The original task prompt (already in your context) remains the binding design; do not re-plan.

Remaining work:
1. Migrate ALL callers/test helpers/registry probes to the new `VerifyRepositoryWorktreeInstallation` + `*VerifiedRepositoryWorktreeInstallation` flow and make `go build ./cmd/... ./internal/... && go vet ./internal/...` clean.
2. Finish the sealed-verifier pieces from the frozen design (seven verification seals, verifier authority, capability `verificationSealsHash`/`verifierAuthorityHash` binding, exact content derivation for HEAD/ORIG_HEAD/commondir, slot grammar + path-hash derivation, installed-root anti-alias), the new registry vectors, and the document machine-contract block updates (verifier authority + Git 2.54 profile + slot grammar rule + new vector IDs + corrected `slice_4_vector_count` + corrected canonical fixture `worktree_slot_path_hash` and `snapshot_integrity_hash`).
3. Run these gates and report exact outputs:
   - `go vet ./internal/...`
   - `go test ./internal/repaircontract -count=1`
   - `go test -race ./internal/repaircontract -run '^TestP6Slice4' -count=3`
   - `go build -tags 'ananke_real_provider_canary ananke_test_runtime_authority' ./...`
   - `gofmt -l ./internal/cmd ./internal/repaircontract ./cmd` (expect empty)
   (Skip the -count=10 focused loop; the orchestrator will run it independently.)
4. Write your final report into the output at the end: gate outputs with package seconds, final Slice-4 vector count, changed-file list, frozen verifier authority hash + profile hash + new canonical fixture values, and any design deviation with justification. Do not commit. Do not create cron jobs.
