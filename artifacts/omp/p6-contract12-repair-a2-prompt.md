Implement P6 Contract Slices 1–2 repair batch A2 in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` using strict RED→GREEN TDD. Read `artifacts/omp/p6a/contract-slices1-2-first-review.md` and current A1 code. A1 real release trust artifacts are green and must remain intact. Address only BLOCKER 2, BLOCKER 3, BLOCKER 4, the executable acceptance-vector evidence gap, and generic hash error privacy. No commit.

Scope: `internal/repaircontract/**` and `docs/experiments/p6-controlled-repair-supervisor-contract.md` only. Do not edit store/runtime/trustedsupervisor/commands/migrations/go.mod/plan. Do not read or reference repo-external private keys.

Strict TDD:
1. Add focused RED tests reproducing each review exploit/gap.
2. Run the exact focused tests and capture expected RED.
3. Implement minimal GREEN without weakening A1 trust/X.509/artifact verification.

Required architecture:

A. External dynamic authority boundary
- Split release verification, authorization verification, and fixture generation.
- Define a closed typed `AuthorityContext` that is supplied by a trusted caller and is NOT accepted from untrusted fixture/request bytes. It must bind exact durable P4 fact/full fence, repair lineage, repository/base decision, ordered writable path identities, ordered installed test-profile identities, selected route/channel/peer, installed policy/profile hashes, and exact authenticated GUI approval-event authority.
- `CanonicalFixture()` remains only an oracle. Its sample values must not be hardcoded semantic authority in the reusable verifier.
- `VerifyAuthorization(expected AuthorityContext, current Authorization, predecessor ..., now, moment)` must compare every current field/hash with external context. Mutation tests change request + all self-hashes while the unchanged external context causes semantic rejection.
- Document that runtime code must construct AuthorityContext from independently verified durable records, never from the request itself.

B. Verified predecessor and fresh attempt 2
- Introduce an opaque verified-authorization result/capability that callers cannot construct with arbitrary fields; only successful verification returns it.
- Attempt 1 requires nil predecessor and empty previous hash.
- Attempt 2 requires an actually verified attempt-1 predecessor for the same repair lineage/P4 proposal and cap; previous hash equals its exact canonical authorization hash.
- Attempt 2 requires a distinct GUI approval event ID, distinct provenance-event hash, strictly later fresh approved_at, new approval hash/authorization bytes, and newly matched external GUI authority. Reject absent, arbitrary hash, unknown, conflicting, wrong-lineage, attempt-2 predecessor, reused approval/provenance/time/bytes.
- Add permanent positive attempt-2 fixture/vector with a real verified predecessor.

C. Canonical-byte dispatch replay
- Add a strict dispatch-only decoder: duplicate detection, closed schema, canonical-byte equality, no trailing data, semantic/hash/context validation.
- Replay classification must consume stored/incoming canonical bytes, not decoded structs, and return exact replay only on `bytes.Equal` after both values are semantically valid against external authority/verified authorization.
- Exact idempotent replay may remain identifiable after freshness moves on, but two byte-identical semantically invalid records are never accepted.
- Reject appended newline/whitespace, same asserted hash with changed bytes, duplicate/unknown keys, and changed deadline/peer/profile/channel/policy/request/authorization/attempt.

D. Executable acceptance vectors and canonical permanent tests
- Replace decorative `LinkedHashesRecomputed: true` inventory. Use one executable registry keyed by exact vector ID; each entry runs its real positive/negative behavior. Test records the actual executed ID/order and compares to the canonical inventory; deleting/renaming a vector test must fail.
- For semantic attacks, recompute all linked hashes before verification. Fix the prior attacker-root helper so `Dispatch.ReleasePinsHash` and every linked value are truly updated.
- Permanently cover RFC 8785 Appendix B numbers, UTF-16 key ordering, negative zero, exponent thresholds, valid surrogate pairs, dispatch lifetime N-1/N/N+1, same-hash altered-byte replay, changed deadline/peer/profile, and predecessor cases.
- Remove vector declarations from the fixture if they remain non-authoritative; docs may list only registry-backed executed vectors.

E. Error privacy
- Make generic canonical/hash helpers package-private unless a public typed API needs them.
- Public typed canonical/hash/decode/verify APIs must return only stable sentinel errors, never caller-controlled `json.Marshaler` diagnostics. Add the reviewer's leaky-marshaler RED vector.
- Preserve generic sanitized unknown-field errors and no authority payload fields.

Verification:
- Run focused tests for each RED/GREEN tracer bullet.
- Then package single, count=10, race count=3, package vet, gofmt, git diff --check.
- Return exact RED evidence, changed files, GREEN results, and any unresolved gap. Do not claim Slice ACCEPT; independent rereview follows.
