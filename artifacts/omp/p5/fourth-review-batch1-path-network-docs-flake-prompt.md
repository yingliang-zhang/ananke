Repair fourth-review blockers 1, 2, 3, 7, and 8 only. Read artifacts/omp/p5/fourth-review-report-recovered.md first. Strict TDD; no real provider/model; no commit/push; preserve existing contracts.

1) Physical root separation: execution policy must reject roots that are alias-equivalent or physically nested after Darwin symlink resolution, including /var vs /private/var. Pin/validate directory identity and compare physical containment before effects. Cover all five invocation-root pairs, repository vs roots, native addon vs repository/roots, and allowed-test executable roots. Fail closed on unresolved/nonexistent/symlink ambiguity without weakening descriptor identity checks.

2) Sandbox gateway authority: remove ambiguous localhost permission. Gateway owns 127.0.0.1 only, and sandbox must allow exact IPv4 127.0.0.1:<port> without also allowing ::1. Add a real Darwin sandbox test with IPv4 production gateway plus competing ::1 same-port listener; prove IPv4 reachable, IPv6 denied, fake credential absent at IPv6. Preserve installed OMP provider-free compatibility.

3) Gateway redirects: configure HTTP client to reject all redirects. Add 301/302/303/307/308 tests asserting exactly one upstream POST /v1/responses, no second request, and no Authorization at redirect target.

7) Docs: runbook operational section must distinguish fake-wrapper majority, separate installed OMP v17.1.3 provider-free preflight, and no real model/provider API. Replace policy schema v1 with exact v4, describe chosen populated-V2 behavior only if blocker 6 is implemented elsewhere (otherwise mark unresolved). Add constant/document consistency test.

8) Flaky supervisor termination fixture: replace one-second marker scheduling assumption with bounded process-aware synchronization that detects early child exit and does not simply increase timeout. Reproduce full focused matrix count=5 and focused race. If instrumentation reveals production failure, fix it.

Run focused RED/GREEN count=10/race=3, full internal package, provider-free installed OMP preflight count=3, git diff check. Report exact files/tests and any remaining blocker. Do not touch blockers 4/5/6 beyond necessary interfaces.