Repair stale tests/docs after immutable atomic runtime authority; production semantics are authoritative. No provider/model, no commit. Current atomic focused count=10 and race=3 pass; Darwin critical subset passes. `go test ./internal/trustedsupervisor` failures to fix:
1 TestAuditInvocationUsesExactArgvMinimalEnvironmentAndFourRoots: add required XDG_DATA_HOME exact value; keep env closed.
2 TestDarwinAuditSandboxDeniesPrivateAuthorityParentAndLocalEndpoints: expect valid localhost:<port> dual-stack trusted gateway rule, not invalid 127 literal; assert gateway owns both.
3 TestAuditInvocationMaterializesPrivateStateAndBoundModels: expect native in pinned immutable XDG hierarchy, not HOME copy.
4 TestCleanupAuditInvocationAfterTransientRejectsPinnedSourceAddonMutation: old test tries write a sealed 0400/root-authority fixture. Rewrite to assert mutation is denied and cleanup remains safe; do not chmod production authority merely to preserve old test.
5 TestAuditInvocationEnvironmentPinsStateConfigSessionAndOMPPath: PATH is now /usr/bin:/bin; assert XDG_DATA_HOME and trusted bootstrap absolute OMP path/hash instead.
6 runbook schema v4 -> v5 and consistency test.
7 TestExecutionPolicyPinsExactOMPVersionAndNativeAddonAtEveryEffect addon mutation: test authority rejection via pre-seal fixture mutation or injected verifier, not writing sealed artifact.
8 TestProductionServerSeparateProcessClientSubmitCrashRestartReconcileCancel socket not ready because production rejects user-owned fixture. Provide an explicit compile-time test-only verifier/build-tag binary for subprocess E2E, unavailable in normal production builds and impossible to enable by env/flag/config. Verify production binary lacks it. Alternatively refactor E2E in-process if it still proves crash/restart.

Also fix any latent dual-stack/accept-loop tests. Use no runtime environment bypass in production. Run gofmt, each failing test count=3, full internal package, git diff check. Do not alter blockers 5/6.