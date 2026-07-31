Working...
Implemented the P1a focused-rereview repairs in contract, fixtures, docs, and ledger only.

- **Append vs reject:** frozen both linearizations in the 15-case acceptance matrix.
  - Append-first: one append commits; reject gets `approval_conflict`; `partial_writes: 0`.
  - Reject-first: both commit; Proposal stays open, rejected predecessor remains rejected, successor pair is pending; `partial_writes: 0`.
  - Contract prose and future storage RED-test plan now match.

- **Timestamps:** replaced lexical matching with semantic UTC RFC3339/RFC3339Nano validation:
  - exact `Z` form, 1–9 fractional digits;
  - Gregorian calendar/day validation;
  - `00:00:00`–`23:59:59` validation.
  - Self-test consistently rehashes all linked Revision hashes, request body hashes, acceptance references, and manifest before rejecting:
    - `2026-99-99T99:99:99Z`
    - `2026-02-29T12:00:00Z`
    - `2026-07-22T24:00:00Z`

- **Envelope identities:** verifier now cross-checks canonical request bodies against Proposal, immutable Revision, RevisionLifecycle, and Approval:
  - create target and revision input;
  - append proposal/revision/hash identity;
  - decision target/revision/hash identity plus approved decision key/reason;
  - withdraw proposal target.
  - Self-test rejects consistently rehashed create `project_id` and approved-decision `revision_hash` tampering.

- **Fixtures/ledger:** updated `acceptance-v1.canonical.json`, rehashed `fixtures.sha256`, and recorded only executed evidence. Current acceptance fixture SHA-256:
  `d87ef3d21b169ca9b715061c01378d02a84daa23a7f861421bf314a74a7ca940`.

Verification — final command exited `0` in `0.51s`:

```text
node --check contracts/p1a/verify.mjs &&
node contracts/p1a/verify.mjs &&
node contracts/p1a/verify.mjs --self-test

P1a proposal contract fixtures verified.
P1a fixture verifier self-test rejected drift, private fields, unpaired Unicode surrogates, request-hash conflation, rehashed timestamp and envelope-identity tampering, and missing vectors.
```
