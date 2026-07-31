Focused full-package repair, no real provider, no commit.
1) TestExecutionPolicyLoadsCanonicalPinnedLaunchEntry has stale expected api.anthropic.com for custom:sudo. Update assertion to exact coding.sudoai.cc:443 and preserve route validation.
2) Multi-attempt cleanup ownership bug: cleanupOwned loops cleanupAuditInvocationAll for each attempt; SessionDir and snapshot work root are shared. Attempt 1 removes shared session, attempt 2 transport validation fails and leaves its output/prompt/tmp. Refactor cleanupOwned closed order:
- For every owned invocation, call idempotent cleanupAuditInvocationTransient (per-attempt prompt/output/temp) while shared SessionDir still exists.
- Validate all invocations bind to the same exact signed SessionDir and snapshot RunRoot/WorkDir.
- After all per-attempt transient cleanup succeeds, scrub shared SessionDir exactly once and snapshot.RunRoot exactly once.
- Continue collecting first error while making safe best-effort cleanup; partial/mismatched roots fail closed. Keep cleanupAuditInvocationAll for single/recovery paths and source addon checks. Validate pinned source addon at end.
Add/revise test for timeout attempt 1→exact resume attempt 2→complete: all prompt/output/temp per-attempt roots and shared session/work empty; count=20/race=5. Run endpoint test count=10, full package. Preserve failure/cancel cleanup and provider-free probe.