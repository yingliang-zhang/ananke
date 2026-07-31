Working...
Completed all P3a hard-review repairs in contract-only artifacts.

- **Approval eligibility:** bound to exact P1 `proposal_id`, `revision`, and `revision_hash`; copied-fixture probe rejects a swapped valid-format approval ID/tuple.
- **HostSpec:** added opaque `transcript_source_fingerprint`; `host_spec_fingerprint` now binds the closed HostSpec JCS payload excluding itself. Raw `path` is forbidden and self-tested.
- **Recovery:** all vectors now model explicit absent identities or exact materialization ID/hash/nonce and Run ID/materialization/current-token `created` fact. Self-tests reject each identity swap and terminal/evidence/process guesses.
- **Staleness:** now means any mismatch from active `(claim_token_hash, fence_generation)`. Six vectors cover same-generation/different-token and lower-generation cases for Run creation, terminal facts, and evidence.
- **Fail-closed outcomes:** every `waiting_for_human` result includes the frozen abstract `(run_id, tool_call_id)` intervention reference.
- **Docs/ledger:** corrected verifier wording: only `--self-test` may spawn a copied verifier; no adapter or contract-defined process launch. Added factual repair evidence to `docs/experiment-ledger.md`.

Changed:
- `contracts/p3a/verify.mjs`
- `contracts/p3a/fixtures/{launch-admission-v1.canonical.json,adversarial-v1.canonical.json,recovery-v1.canonical.json,fixtures.sha256}`
- `docs/experiments/p3a-fenced-launch-admission-contract.md`
- `docs/plans/2026-07-23-p3a-fenced-launch-admission-contract.md`
- `docs/experiment-ledger.md`

Verification passed:

```sh
node --check contracts/p3a/verify.mjs &&
node contracts/p1a/verify.mjs &&
node contracts/p1c/verify.mjs &&
node contracts/p2a/verify.mjs &&
node contracts/p2c/verify.mjs &&
node contracts/p3a/verify.mjs &&
node contracts/p3a/verify.mjs --self-test
```

The P3a self-test rejected the new rehashed probes, including raw transcript paths, approval tuple swaps, transcript-source drift, both stale-authority modes, intervention swaps, recovery identity swaps, and terminal/evidence/process inference.
