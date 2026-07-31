Implement P6 Phase 3A in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`, strict TDD, scope only `internal/store/p6_repair.go`, `internal/store/p6_repair_test.go`, and `internal/store/store.go`. No commit/docs/artifacts.

Goal: add typed immutable review evidence and atomic running→waiting_for_review persistence. Current P6 events only retain EvidenceHash, which is insufficient for restart/review.

Requirements:
- Add migration v16 table `p6_repair_review_evidence` with evidence_hash PK, authorization_hash, attempt_hash, prior_event_hash, canonical evidence_json, created_at; exact FKs; insert-only update/delete triggers. Register v16 without mutating v15 semantics.
- Define closed typed structs for P6 review evidence: schema version, authorization/policy/P4-binding/attempt hashes and attempt number/cap, prior running event hash, pre-effect descriptor hash, worktree descriptor hash, adapter identity hash/classification/counters, patch hash/size, strictly sorted nonempty changed paths, ordered test results, and evidence hash. Test result binds test ID, command hash, executable identity hash, exit code, timeout flag, stdout/stderr hash+size. No raw patch/source/output/prompt/credential/path/argv/environment/endpoint.
- Exact bounds: attempt 1..2/cap2; changed paths <=128 clean relative, sorted unique; tests nonempty <=64 and IDs ordered; sizes bounded; only adapter classification `completed`; all SHA256 values exact. Successful test evidence requires exit 0 and not timed out.
- Canonical self-hash excluding EvidenceHash; canonical exact row bytes including hash.
- Add `PersistP6RepairReviewEvidence(ctx, evidence, event)` that uses one immediate transaction: load/revalidate authorization+attempt and exact current head; require head state running; require evidence prior/head/bindings exact; require event be next `waiting_for_review`, event EvidenceHash equal evidence hash, and event exact prior binding; insert evidence and event atomically. Exact replay returns same evidence/event with no new rows; conflicts reject.
- Modify generic `AppendP6RepairEvent` to reject `waiting_for_review`; this state can be reached only through the atomic evidence API. Existing failed/waiting_for_human paths remain.
- Add `GetP6RepairReviewEvidence(ctx, evidenceHash)` that revalidates canonical bytes/self-hash/bindings and corresponding waiting_for_review event.
- Tests: migration idempotence, rollback injection between evidence/event, exact replay, conflict, concurrent duplicate, tamper/immutability triggers, missing/wrong running head, projected/wrong auth/attempt/prior, every schema/bounds/order denial, generic append denial, restart readback.
- Run focused count=1, count=10, race count=3, full store, gofmt, vet, diff-check.
Return exact files/results and gaps.