Resume exact Phase B session 019f9d5f-4f13-7000-9c90-3bca206fc760. Continue with tools/edits on the current worktree; the orchestrator fixed the timeout-left gap.

Current verified state:
- old path-only completion calls in audit_runtime were replaced by finalizing event -> decode signed evidence -> validate policy -> authenticated OwnedRoots cleanup/absence;
- callback now receives executionPolicy.namespaceAuthority;
- decoy live/restart, path-only rejection, signed tamper focused PASS;
- real 0555 work-root cleanup fixed by checking same dev/inode/owner/group + exact post-chmod 0700;
- focused OwnedRoot/Finaliz matrix count=10 PASS and race count=3 PASS;
- full `go test ./internal/trustedsupervisor -count=1` PASS (46.676s).

Do not re-explore. Inspect current diff and finish only:
1. prove no production completion/callback/recovery path calls legacy `scrubAndRemoveAuditOwnedRoot`, `verifyAuditOwnedRootAbsent`, or reconstructs path-only finalization authority; remove dead helpers if no longer needed;
2. ensure failure/timedout/cancelled live cleanup uses captured identities or is explicitly non-completion-authorizing and cannot create a false completed event;
3. update runbook/docs/schema references to evidence v5, namespace prerequisites, runtime UID/GID, all-or-nothing identity mismatch nonterminal behavior, release artifact CLI;
4. run gofmt, focused smoke, full package, go test ./..., go vet, git diff --check. No provider/model, no real canary, no commit.

Report exact remaining gaps only. If current implementation is coherent, finish without speculative refactor.