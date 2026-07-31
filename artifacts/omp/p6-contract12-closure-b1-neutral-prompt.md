Complete the current P6 contract B1 RED→GREEN work in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. This is ordinary local Go contract/date/identifier validation. No commit.

The previous model run stopped before finishing. Current state:
- New RED tests are already present in `internal/repaircontract/a2_contract_test.go`.
- `dispatch.go` already re-runs `validateAuthorizationRecord(authorization, now)` when freshness is required. Preserve it.
- Current package failure is `TestP6A2ReleaseTrustMandatoryAtAuthorizationAndEffect`: authorization/effect are accepted at and after the embedded leaf exclusive `NotAfter`, including 2029.
- FullFence and closed-ID validation from the existing tests/report still needs implementation.

Implement minimal GREEN:
1. In `VerifyAuthorization`, before returning a capability, verify the compiled `FrozenReleasePins()`, `FrozenTrustBundle()`, and `frozenRotation()` at `now`. Return `ErrInvalidContract` on failure.
2. In freshness-enforcing dispatch validation, verify the same compiled release artifacts at effect/admission `now`. Static replay classification remains clock-independent and cannot be used as effect validation.
3. In both `validateAuthorityContext` and `validateAuthorizationRecord`, require exact `FullFenceSchemaVersion`, valid hash, a bounded nonempty closed ClaimID, and `FenceGeneration > 0`.
4. Add bounded nonempty closed validation for repository identity, route ID, supervisor profile ID, and peer role in both paths. Keep canonical fixture values valid. Reject empty, whitespace/control, and malformed forms using small explicit helpers/regexes with documented length bounds.
5. Ensure executable registry contains and runs the new named cases for foreign fence version, empty claim ID, zero/negative generation, and empty/malformed dynamic identifiers; update canonical ID order.
6. Keep all existing A1/A2 behavior unchanged. Do not touch the new rotation-approver DER files in this batch. Never read repo-external key files.
7. Run the two focused B1 test groups, executable registry, package single, count=10, race count=3, vet, gofmt, diff-check. Update the experiment doc only if needed.

Allowed edits: `internal/repaircontract/**` and `docs/experiments/p6-controlled-repair-supervisor-contract.md`. Return exact commands/results and any remaining failure. Do not create cron jobs or inspect unrelated code.
