Fresh focused repair for full-package wrapper/gateway regressions; no real provider, no commit.
Observed failures:
1) TestDarwinAuditWrapperSandboxEnforcesReadonlySnapshotAndRootIsolation: /bin/ln and /bin/sh denied.
2) TestAuditInvocationUsesExactArgvMinimalEnvironmentAndFourRoots: /usr/bin/env,/usr/bin/sed,/usr/bin/sort denied.
3) Frozen wrapper mutation tests now fail due later ValidateEffectBoundary re-reading mutated wrapper after bytes already frozen.
4) LeastAuthority test exits 85.
5) exact provider-free test name is TestAuditInstalledOMPProviderFreeTransportPreflight.
Fix:
- Test policies must explicitly pin every executable their fake wrapper uses by adding exact file identities to WrapperExecutables; do not broaden production sandbox or global allow roots.
- After policy.ValidateEffectBoundary + freezeAuditWrapper successfully copy/hash bytes, later boundaries must revalidate policy canonical bytes/all roots/Git/OMP/test executables/models config etc but treat frozen wrapper bytes/hash as authority and not reopen mutable wrapper path. Add a narrowly named ValidateEffectBoundaryWithFrozenWrapper method if needed. Prove post-freeze path replacement/in-place rewrite executes original frozen pipe bytes; pre-freeze mutation still rejects.
- Diagnose least-authority exit85 and add only exact fixture executable identity or correct expected denial; no broad process/file/network grant.
- Run exact provider-free OMP preflight count=10 (must actually run; no `[no tests to run]`), full wrapper/gateway tests count=5/race=2, full package. Probe uses no real credential/provider and redacts headers.
No signed-history/termination changes.