Resume this exact Arm B session for a revision-3 repair. You may use tools. Do not read Arm A or other P0a arms. Read the shared contract and fixtures again: revision 3 replaces the flawed two-schema assumption with three schemas.

Required correction:
- Generate `daemon-request-private` and `daemon-response-internal` for Go+Rust only. Internal response owns `ok`, `error`, runs/events, daemon fixtures, and must preserve explicit `ok:false` (use proto optional/presence correctly if necessary).
- Generate `renderer-public` for Rust+TypeScript and use renderer-bootstrap/activity/cancel/health only. Generated TS must contain neither `token` nor raw daemon `error`.
- Complete the verifier: actual per-runtime tests, controlled content-mutation/staging drift rejection, clean regeneration, optional evolution plus Buf breaking probe, README with raw timings/version/dependency evidence. The existing verify.sh inventory pass is insufficient.
- Root `generated/` remains forbidden. Keep sources/build outputs in experiments/p0a/proto-buf only; no production/shared edits/commits.

Before final response run your full reproducible command, git diff --check, and state a truthful terminal verdict.
