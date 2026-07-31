Repair fourth-review blocker 6 and the four stale full-suite expectations exposed by blocker 5. Read report lines 215-246 and current journal migration code. Strict TDD, no provider/model, no commit.

Selected migration contract: reject any populated pre-authentication V2 audit history BEFORE committing or applying structural migration; provide a precise typed/operator-visible remediation error. Never rehash/reseal/sign legacy audit rows.

Requirements:
- Detect nonempty legacy `audit_execution_intents` OR `audit_execution_events` while still V2, inside an atomic migration transaction before DDL/version change.
- Return typed closed error identifying populated legacy history (no row contents/secrets).
- Roll back fully: user_version/schema/tables/rows byte-semantically unchanged; reopen gives same deterministic error; restart-safe.
- Empty V2 still migrates to current schema. V1 path remains valid.
- Correlated legacy row rehash/reseal edits cannot produce authenticated current authority; migration still rejects based on population.
- Add operator remediation text to runbook: archive/export legacy DB and start a fresh journal; no in-place signing path.

Regression fixture must create a genuinely populated accepted V2 database with unsigned pre-auth audit intent/event rows, verify atomic deterministic rejection and unchanged schema/data across two reopen attempts. Also test only-intent and only-event population.

Repair stale blocker-5 suite expectations without weakening production:
1) lifecycle persistence now expects prepared,running,finalizing,completed and exact FinalizingEventHash/evidence continuity;
2) migration fixture schema derivation must understand current audit-event v3 fields while constructing accepted V1/V2 schemas;
3) output mutation test must mutate before finalizing persistence/evidence finalization, not after cleanup removed output; assert no finalizing/completed callback as appropriate;
4) timeout/resume success now has 7 events including finalizing before completed; assert exact sequence and binding.

Run migration/finalizing focused count=10, race=3, full internal package, git diff check. Do not touch blocker 4 authority. Report exact behavior.