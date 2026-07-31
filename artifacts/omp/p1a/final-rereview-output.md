Working...
ACCEPT

- **Immutable state:** `RevisionLifecycle` is the sole mutable record, keyed by `(proposal_id, revision)`, hash-linked to the immutable revision and one Approval; lifecycle/Approval state agreement is verified.
- **Requests/idempotency:** envelopes use canonical body hashes distinct from `revision_hash`, fixed `(actor, operation, resource)` scopes, record links, and lookup-before-mutable-check semantics. A consistently rehashed reordered scope was rejected.
- **JCS/timestamps/privacy:** verifier rejects unpaired surrogates in keys and values; self-test rejects consistently rehashed invalid calendar/time values. Context-specific closed schemas rejected a consistently rehashed, non-denylist `operator_note` field.
- **Matrix:** exact 15-case inventory covers append from pending, append after rejection, rejected withdrawal, create/append/decision restart replay, competing appends, competing decisions, append-vs-approve, same-key replay, and both append-vs-reject linearizations. The reject-first branch requires two commits, retained rejected predecessor, pending successor, open Proposal, and zero partial writes; a consistently rehashed downgrade to one commit was rejected.
- **Authority/scope:** frozen policy remains deterministic/advisory-only/future read-only audit; every envelope scope begins with `local_gui_operator`. No P1a operation identifiers were found under `cmd`, `internal`, or `gui`.
- **Executed:** `node --check contracts/p1a/verify.mjs && node contracts/p1a/verify.mjs && node contracts/p1a/verify.mjs --self-test` passed; final verifier rerun passed.
