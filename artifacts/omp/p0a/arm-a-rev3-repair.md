Resume this exact Arm A session for a revision-3 repair. You may use tools. Do not read Arm B or other P0a arms. Read the shared contract and fixtures again: revision 3 replaces the flawed two-schema assumption with three schemas.

Required correction:
- `daemon-request-private` and `daemon-response-internal` generate Go+Rust only. The internal response owns `ok`, `error`, runs/events, and the daemon response fixtures, including wire-present `ok:false`.
- `renderer-public` generates Rust+TypeScript (Go optional only if useful) and owns bootstrap/activity/cancel/health. It must not contain `token` or raw daemon `error`.
- `renderer-activity.json` is the public Run/Event fixture; daemon-response-events.json is not a renderer-public fixture.

Your prior Arm A revision-2 test correctly failed because its renderer-public schema discarded raw daemon `error`; do NOT solve it by adding `error` to TypeScript public types. Refactor experiment-only schemas, generation, tests, privacy check, drift probe, and clean regeneration around the three boundaries. Make the 9 fixtures test in relevant runtimes. Regenerate and run full verifier. Add README evidence including original RED, rev2 boundary failure, rev3 recovery, versions/metrics, and terminal verdict. Keep all work under experiments/p0a/jsonschema-quicktype; no production/shared edits/commits.
