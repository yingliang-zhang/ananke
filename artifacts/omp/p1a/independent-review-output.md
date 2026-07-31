Working...
## CHANGES REQUESTED

No files edited or commits made. Reviewed branch `feat/task-proposal-core`.

### Blocking contract defects

1. **Revision state is unrepresentable.**  
   `Revision` is defined as an immutable, hashed snapshot with an exhaustive field list that omits `state` (`docs/experiments/p1a-task-proposal-contract.md:47-64`; verifier likewise requires exactly those keys at `contracts/p1a/verify.mjs:125-129`).  
   The normative state table nevertheless requires `Revision: pending → approved|rejected|superseded|withdrawn` (`docs/experiments/...:99-114`).

   A SQLite implementation cannot both keep the Revision snapshot immutable/hash-stable and apply those Revision state transitions without an unspecified separate lifecycle record. Define that record—its key, fields, atomic transitions, and relation to Approval—or make the state graph explicitly apply only to Approval.

   Related ambiguity: rejection leaves Proposal `open`, while withdrawal is defined only for its “current pending pair.” The Proposal graph permits `open → withdrawn` after rejection, but the paired-record result is unspecified.

2. **Idempotency request identity is underspecified and the golden fixture conflates it with `revision_hash`.**  
   The contract requires a canonical **request body** hash (`docs/experiments/...:117-134`), but `acceptance-v1.canonical.json` uses the Revision snapshot hash as `body_hash` for creation. The Proposal’s immutable `project_id` and `workstream_id` are absent from the Revision snapshot. Thus two create requests differing only in target can have the same fixture-defined body hash, contradicting “same scope/key with a different body hash returns `idempotency_conflict`.”

   Define versioned canonical request envelopes for:
   - create: proposal target plus revision input;
   - append: expected revision/hash plus proposed snapshot input;
   - decision: approval/revision identity, decision, and reason.

   Define exact operation-scope tuple values and ordering: durable idempotency lookup/hash-conflict **before** mutable base/state checks, so an exact retry after later state changes still replays its original identity.

3. **The verifier is not RFC 8785-compliant for Unicode.**  
   `canonicalJson()` uses `JSON.parse()` then `JSON.stringify()` without rejecting unpaired surrogates (`contracts/p1a/verify.mjs:43-58,76-84`). Actual Node behavior confirms that the bytes `{"title":"\ud800"}` round-trip identically (`true`), so this verifier would classify them canonical once the manifest is updated. RFC 8785 requires lone surrogates to terminate with an error because they can break signatures: [RFC 8785 §3.2.2.2](https://www.rfc-editor.org/rfc/rfc8785.html#section-3.2.2.2).

   Reject unpaired high/low surrogates in both keys and strings before canonicalization. Keep the existing RFC-8785 sorting and ECMAScript number serialization.

4. **The claimed privacy-field enforcement is false for the fixture surface.**  
   The contract says paths/repository roots and other sensitive field names are forbidden and “the fixture verifier rejects those private field names” (`docs/experiments/...:174-186`). The verifier’s denylist omits `repository_root` and most listed variants (`contracts/p1a/verify.mjs:19-34`); `verifyAcceptance()` does not close `given` or `requests` object shapes (`:158-185`).

   I copied fixtures to a temporary directory, inserted canonical `"repository_root":"/private"` into `create_replay.given`, recomputed that fixture’s manifest digest, and ran:

   ```sh
   node contracts/p1a/verify.mjs --fixtures <temporary-copy>
   ```

   Result: `P1a proposal contract fixtures verified.`

   Use context-specific closed schemas/allowlists for every fixture object. A partial denylist cannot substantiate the ledger’s privacy claim.

5. **Append, replay, restart, and concurrency acceptance is insufficient.**  
   The verifier fixes the acceptance inventory to only six cases: creation replay/conflict, stale append, creation restart replay, two-appends race, and approve-vs-reject race (`contracts/p1a/verify.mjs:161-185`). Missing normative vectors include:
   - successful append from pending: parent link, Proposal current pointer, former Revision **and** Approval superseded, new pair pending;
   - append after rejection: rejected predecessor remains rejected;
   - withdrawal after a rejected current revision;
   - append racing approval/rejection of the same current revision;
   - same-key/same-body concurrent replay. This is explicitly required by the plan (`docs/plans/...:64-74`) but absent from the fixture inventory;
   - restart replay for append and decision, including a replay after the Proposal/Approval has subsequently moved state;
   - zero-partial-write assertions for every losing branch.

   The fixture currently proves only the listed result labels, not these safety invariants.

### Confirmed sound

- **No hash self-reference:** `revision_hash` is correctly excluded from the hashed Revision snapshot, and both Proposal and Approval reference the SHA-256 of exact Revision bytes.
- **Frozen hash matches:** direct `shasum -a 256` produced  
  `114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263`, matching the fixture manifest and ledger.
- **Authority boundary is clear:** only `local_gui_operator` mutates; model output is advisory; future adapter is read-only and marked `future`.
- **No P2/P3 runtime leakage found:** `contracts/p1a/` contains fixture/verifier-only material; no SQLite, GUI/IPC, worker, claim, adapter execution, or network implementation is present. The signed future `policy` stub is deliberate, though its later-version migration should be made explicit when request envelopes are added.

### Verification performed

```sh
node --check contracts/p1a/verify.mjs
# exit 0

node contracts/p1a/verify.mjs
# P1a proposal contract fixtures verified.

node contracts/p1a/verify.mjs --self-test
# P1a fixture verifier self-test rejected a canonical content mutation.
```

The self-test genuinely detects a Revision-content change without a manifest update. It does not cover the contract defects above, and the ledger overstates the verifier’s privacy-field coverage.
