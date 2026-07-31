Working...
# ACCEPT

All five prior authority-binding blockers are repaired and enforced by the P3a verifier/fixtures.

- **Approval eligibility:** requires `state: "approved"`, `approved_by: "local_gui_operator"`, and the exact frozen P1 tuple `(proposal_p1a_001, 1, sha256:114a…0263)`. This matches P1a’s approved fixture, and the P3a self-test rejects a swapped approval tuple.
- **Transcript source:** `HostSpec` has required opaque `transcript_source_fingerprint`; its self-excluding canonical SHA-256 binds it. Tampering is rejected; raw `path` injection is rejected.
- **Recovery identities:** all three boundaries enforce either explicit null/absent materialization/Run identities, or the exact ready materialization ID/hash/nonce and exact Run/materialization/current-token `created` fact. Identity swaps and inferred terminal/evidence/process states are rejected.
- **Staleness:** the predicate rejects any mismatch from active `(claim_token_hash, fence_generation)`. The fixture contains same-generation/different-token and lower-generation vectors for `create_run`, `append_terminal_fact`, and `settle_evidence`. I additionally changed each of the six isolated copied vectors to the active tuple; all six were rejected.
- **Human intervention:** every fail-closed result is exactly rejected, non-starting, non-terminal, evidence-free `waiting_for_human`, with canonical `(run_id: run_p3a_001, tool_call_id: tool_call_p3a_001)`. Both reference swaps are rejected.

Scope documentation is accurate:

- `contracts/p3a/verify.mjs` calls `spawnSync` only from `selfTest()`.
- Plan, contract, and ledger consistently state that only `--self-test` may spawn a copied Node verifier over copied fixtures; it never launches an adapter or contract-defined process.
- No P3a runtime markers exist in `cmd`, `internal`, `gui`, or `tests`; P3a artifacts are confined to contract/docs/audit locations. Documents explicitly exclude runtime, daemon, adapter, OMP, and process implementation.

Verification passed:

```text
node --check contracts/p3a/verify.mjs
node contracts/p1a/verify.mjs
node contracts/p1c/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2c/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p1a/verify.mjs --self-test
node contracts/p1c/verify.mjs --self-test
node contracts/p2a/verify.mjs --self-test
node contracts/p2c/verify.mjs --self-test
node contracts/p3a/verify.mjs --self-test
```

No repository edits or commits made.
