Implement P6 Contract Slices 1–2 closure repair batch B1 in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` with strict RED→GREEN TDD. No commit.

Source report: `/private/tmp/ananke-p6-contract12-rereview-output/review.md` (also copy-safe content can be found in the current repo review artifacts if needed). Address exactly:
1. BLOCKER retained authorization capability bypasses effect-time approval freshness.
2. BLOCKER release trust verification is optional and not capability-linked.
3. HIGH FullFence and closed identifier semantics.
Do NOT address the independent rotation approver in this batch; four new public DER inputs already exist for B2.

Scope: `internal/repaircontract/**` and `docs/experiments/p6-controlled-repair-supervisor-contract.md` only. Preserve all A1/A2 trust, external authority, predecessor, byte replay, RFC8785, privacy, and 66-vector behavior. No runtime/store/process code. Never read or reference repo-external private keys.

Strict TDD requirements:
A. Effect freshness
- Reproduce the exact retained-capability exploit: approval 12:00, capability minted at 12:04, effect at approval age 5m+1ns while dispatch and authorization remain open; it must be RED before fix.
- When `DecodeDispatch` has `checkFreshness=true`, revalidate the private authorization at supplied `now`, not only at capability creation.
- Permanent effect tests: approval age N-1/N/N+1; authorization expiry; dispatch expiry; same capability admitted earlier then reused at effect.
- Static replay classification may skip clock freshness but must remain non-authoritative for effects.

B. Mandatory release trust
- Reproduce 2029 exploit after embedded repair leaf expiration: `VerifyReleaseTrust` fails, but current VerifyAuthorization/DecodeDispatch accepts when caller skips it.
- Choose the minimal fail-closed option: `VerifyAuthorization` and freshness-enforcing `DecodeDispatch` must internally verify the compiled embedded release trust at `now`; no caller sequencing dependency. If an opaque release capability is introduced instead, it must be non-forgeable, exact-pin/cert/SPKI/role/domain/manifest/policy/profile/rotation bound, and revalidated for current time at effect. Prefer internal verification unless code structure makes it unsafe.
- Static replay may validate immutable release identity without current certificate freshness; it cannot grant effect authority.
- Permanent tests at certificate valid N-1/N/N+1 and fresh 2029 authorization/dispatch bypass attempt.

C. FullFence and closed IDs
- Validate in both external `AuthorityContext` and authorization record: exact `FullFenceSchemaVersion`, nonempty bounded closed ClaimID grammar, valid ClaimTokenHash, FenceGeneration > 0.
- Add nonempty bounded closed grammars for repository identity, route ID, supervisor-profile ID, and peer role. Preserve the actual canonical fixture values. Reject whitespace/control/path-confusion forms as appropriate; document exact grammar/length.
- Add executable registry entries for foreign fence schema, empty claim ID, zero and negative fence generation, empty/invalid repository identity, route ID, supervisor profile ID, and peer role. Every semantic mutation must recompute all linked hashes and use a matching external authority where the finding requires that adversarial condition.

Verification:
- Capture focused RED command/output for each finding cluster.
- Run focused GREEN tests, executable registry, package single, count=10, race count=3, vet, gofmt, diff-check.
- Update experiment doc accurately; do not claim Slice ACCEPT.
- Return exact outputs and unresolved gaps.
