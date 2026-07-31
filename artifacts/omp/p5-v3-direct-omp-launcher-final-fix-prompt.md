Third and final exact-resume pass for P5 v3 session `019f9fef-1f36-7000-b898-48eff4d5b69c`. Do not redesign. Use the already captured full failure log `/private/tmp/ananke-p5v3-full-test.log`; do not run full package until every listed focused group is green. Do not read the external wrapper again. No commit, no real canary, no P6 edits, no credentials/raw model output.

Current hard facts:
- Compile-only PASS (0.730s).
- gofmt drift 0 and git diff-check PASS.
- Focused v3 group PASS (1.756s): AtomicRuntime, P5Direct, DarwinP5, P5RuntimeAuthority, hard timeout, typed timeout, credential projection.
- Full package has exactly 25 failure lines; list can be obtained from the saved log with `^--- FAIL:|^    --- FAIL:`. Fix them in small groups.

Required triage/fix order:
A. Stale contract names/assertions after direct migration:
- wrapper failure classes now intentionally direct (`direct_omp_or_capture_verification_failed`, `direct_omp_exit_nonzero`). Update tests only where semantics are unchanged; do not restore wrapper runtime.
- minimal environment must no longer expect `OMP_WRAPPER_*` or shell `_`.
- runbook schema must match execution-policy v6.
- rename test descriptions from wrapper to direct OMP when behavior changed.
B. Fake direct OMP execution fixtures:
- failures show sandbox-exec cannot execute the current temp fake script (`exit 71 Operation not permitted`). Replace fake-script assumptions with a test-only direct executable fixture that genuinely exercises argv/output/timeout under the production direct path. Do not weaken production sandbox and do not add runtime env/config bypass. A compile-time/test-only fixture mechanism is acceptable.
- migrate server/retry/cancellation tests to that fixture. Preserve assertions, not legacy implementation.
C. Evidence/output lifecycle:
- `output_tamper` reports missing live owned-root identity; fix root cause so retained successful output remains authenticated and verifiable until callback, while failure/timeout trees still scrub.
- oversize now fails during bounded direct capture (`ErrLimit`); update the test stage expectation if this is the intended stronger contract, without weakening the limit.
- credential leak and malformed timeout must retain closed typed failure classes and scrub all derived trees.
D. Sandbox isolation test must validate direct OMP permissions/denials using the correct direct argv/output behavior; do not expect a shell script to create `.touch` paths.
E. Pre-running journal failure, terminal journal restart, production completion/mutation/finalizing/replay/timeout retries, and cancellation tests must pass with direct output + typed timeout. Fix root causes, not timeouts or expected states.

After each group run only its named tests. Then run full package once. If green, run focused race, vet, gofmt, diff-check, tagged canary compile. Never run real canary. Return exact remaining failures if time expires; do not emit only `Working...` if you can summarize.
