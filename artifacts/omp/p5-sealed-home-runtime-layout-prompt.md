Resume this exact P5 session. Preserve all dirty work. Do not run the real-provider canary, edit the ledger/external wrapper, commit/push, reset/clean/restore, or add a broad artifact allowlist.

Verified OMP 17.1.4 evidence:
- Refined canary fails pre-dial as `artifact_scan_temporary_authority_home_work_root`.
- Provider-free reproduction identified exactly `.omp/logs/omp.<YYYY-MM-DD>.<pid>.log`; its bounded JSON warning contains `cwd=<WorkDir>` from OMP's `getCpuModel` fallback.
- A provider-free probe that pre-created `.omp/run`, made HOME/`.omp` state non-writable, and left only `run` writable passed with zero HOME work-authority artifacts.
- Making all HOME state non-writable fails because OMP requires `~/.omp/run`.
- All temporary diagnostic hooks have been removed. Do not reintroduce logging of relative paths/content.

Narrow TDD task: implement a descriptor-authenticated sealed HOME runtime layout, not an artifact exception.

Required contract:
1. Under invocation `HomeDir`, supervisor creates exactly `.omp` and `.omp/run`. Final modes: `HomeDir` remains private 0700; `.omp` is immutable/traversable 0500; `.omp/run` is the only writable HOME state root at 0700. OMP logs must not be creatable.
2. Extend invocation bindings/owned-root identities to include exact HOME-state and run roots. Any replacement, symlink, device/inode/owner/group/mode drift, parent drift, missing/extra expected binding, or swap must fail closed before launch/effect and during cleanup validation.
3. Implement mode transition and recapture descriptor-relatively through namespace authority. No path-based chmod/reopen race. Preserve child identity and rebuild parent-bound identities after the `.omp` mode transition. Add focused namespace-authority tests for success plus replacement/mutation races.
4. Sandbox must deny metadata/mode/replacement writes to exact `.omp` while permitting writes only beneath exact `.omp/run`; prove child cannot chmod/replace `.omp`, create `.omp/logs`, or create other `.omp` siblings, while the required run state works. Keep all existing native/agent/temp isolation.
5. `scanAuditInvocationWritableTrees` remains fully strict: no exception for OMP logs, work-root paths, HOME paths, or authority content. Existing artifact classification stays unchanged.
6. Cleanup must scrub/remove the run tree and sealed HOME structure through authenticated owned-root cleanup, including failure/timeout/restart paths, without weakening decoy/substitution protection.
7. Update the installed provider-free OMP 17.1.4 preflight to exercise the production-equivalent sealed HOME layout and assert zero `.omp/logs` artifacts plus successful single local request. Do not emit diagnostic artifact paths/content.
8. Do not change schema versions unless an existing serialized machine contract shape/semantics truly changes; explain the decision. Do not touch historical `artifacts/omp/` files.

TDD/gates:
- strict RED first for sealed layout, mode/replacement/sibling-log denial, run success, exact identity validation, cleanup;
- focused GREEN and `-count=10` where practical;
- provider-free installed OMP 17.1.4 preflight;
- focused race `-count=3`;
- full `internal/trustedsupervisor`, vet, scoped gofmt-diff, git diff check;
- no real canary.

Report exact commands/results, changed files, schema decision, and residual risks. If the namespace API cannot make this race-safe, stop with the exact blocker rather than weakening the contract.