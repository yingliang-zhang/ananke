# P1c verifDeadline exceeded
e

## Status

**Incomplete — not ready for independent verification.**

The missing dependency-free verifier was added at `contracts/p1c/verify.mjs`.
It has not yet completed a green canonical run or self-test. The only executed
verifier gate stopped at the committed manifest formatting defect described
below.

No production, daemon, bridge, runtime, generator, or GUI source was edited.
No commit or push was performed.

## Changed files

- `contracts/p1c/verify.mjs`
  - New dependency-free Node verifier.
  - Reads the canonical P1c fixture and versioned SHA-256 manifest.
  - Validates UTF-8, no BOM, unpaired-surrogate rejection, canonical one-line
    JSON bytes, exact fixture digest, closed command inventory, and command
    input/result DTO validation.
  - Validates all 11 committed Proposal JSON Schema documents plus the embedded
    closed `ProposalActivity` item target: 12 DTO schema targets in total.
  - Locks each committed schema document to a canonical JSON SHA-256 digest;
    validates every object is closed and every property is required.
  - Validates P1a identifier, idempotency-key, SHA-256, semantic UTC timestamp,
    actor, revision, approval, lifecycle, activity, and fixed-policy rules.
  - Validates create/list/get/activity/append/decision/withdraw cross-record
    identity, revision-hash, timestamp, and ordered-activity links.
  - Denies private runtime-path/root/directory/socket/identity/worker/process/
    PID/token/credential/secret/authorization/cookie/model/adapter/execution/
    runtime/environment/raw-error/error/prompt/completion/provider/audit-output
    field-name categories, except the fixed public P1a `policy.adapter` and
    `policy.model_role` constants.
  - Includes a temp-copy self-test design for consistently rehashed fixture
    content drift, injected private fixture fields, injected unknown fixture
    fields, and injected private schema fields.

- `contracts/p1c/fixtures/fixtures.sha256`
  - Rewritten without a trailing newline, matching the repository's P1a
    canonical-manifest rule.

- `artifacts/omp/p1c/verifier-repair-output.md`
  - This evidence record.

## Exact commands already executed

1. `shasum -a 256 contracts/p1a/fixtures/revision-v1.canonical.json`

   Outcome: exited `0`; returned:

   ```text
   114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263  contracts/p1a/fixtures/revision-v1.canonical.json
   ```

2. `node --check contracts/p1c/verify.mjs && node contracts/p1c/verify.mjs`

   Outcome: `node --check` passed; the canonical verifier exited `1` before
   semantic validation with:

   ```text
   AssertionError [ERR_ASSERTION]: fixtures.sha256 must not end with a newline
   ```

   The manifest has since been normalized, but this command has **not** been
   rerun. Therefore there is no green verifier result.

3. Canonical hash inspection of the `get_proposal.result.revision` embedded in
   `contracts/p1c/fixtures/protocol-v1.canonical.json`.

   Outcome: the embedded Revision hashes to:

   ```text
   77142beab4abcc2d46b88fc61c04e83f860e3631adbfa48fb3fb6e3d96cbf5a4
   ```

   This differs from the frozen P1a root Revision fixture digest above, even
   though the P1c Proposal/lifecycle/Approval/activity links advertise the P1a
   hash `sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263`.

4. Canonical hash inspection of the eleven committed P1c Proposal schema
   documents.

   Outcome: completed successfully and supplied the schema-digest constants
   locked in `contracts/p1c/verify.mjs`. No generated code or schema was
   modified.

No `node contracts/p1c/verify.mjs --self-test` command has run.

## Unresolved risks and required repair

1. **P1c Revision/hash incompatibility.** The P1c embedded Revision does not
   canonically hash to the P1a root Revision hash used by its linked records.
   The current verifier checks the advertised linked hash but must also require
   `hashCanonical(detail.revision) === proposal.current_revision_hash`.
   Doing so will correctly fail the committed P1c vector until the P1c Revision
   content is reconciled to the frozen P1a canonical Revision or the contract
   explicitly changes its cross-record hash semantics. The first option is the
   compatible one.

2. **No green verifier evidence.** The manifest normalization occurred after the
   failed command. The canonical verifier must be rerun, then repaired for any
   remaining failures before docs can claim verification.

3. **No self-test evidence.** The self-test has not exercised its consistently
   rehashed content-drift, private-field, and unknown-field rejection probes.

4. **Schema contract scope.** There are eleven committed P1c Proposal JSON
   Schema files, not twelve standalone files. The verifier counts the embedded
   closed `ProposalActivity` item shape as the twelfth DTO schema target. This
   interpretation must remain explicit in any later evidence.

5. **Independent review requires executable evidence.** A reviewer should not
   accept the existing P1c documentation claim that `contracts/p1c/verify.mjs`
   validates the contract until the corrected verifier and self-test both exit
   `0` against committed inputs.

## Readiness

Not ready. The next authorized verification sequence is:

```sh
node --check contracts/p1c/verify.mjs
node contracts/p1c/verify.mjs
node contracts/p1c/verify.mjs --self-test
```

Only after all three succeed should the P1c contract/ledger be updated from
actual gates. The next repair must remain contract-only: reconcile the P1c
embedded Revision and linked P1a hash, tighten the verifier's revision-hash
proof, rerun the self-test, and avoid generator/runtime/daemon/bridge changes.
