Implement P6 Contract Slices 1–2 repair batch A1 in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` using strict TDD. Read `artifacts/omp/p6a/contract-slices1-2-first-review.md`, but address only BLOCKER 1 (real release artifacts and rotation protocol). Do not attempt the AuthorityContext, attempt-2, replay, vector-registry, or HashRecord fixes in this batch. No commit.

Public input artifacts already generated and safe to read:
- `internal/repaircontract/testdata/release-v1/repair-root-cert.der`
- `repair-root-spki.der`
- `repair-attestor-cert.der`
- `repair-attestor-spki.der`
Their SHA-256 values are real bytes, not label hashes. Private keys exist only outside the repository under `$HOME/.ananke/controlled-repair/v1/`; DO NOT read, copy, print, hash, inspect, or reference private-key bytes/paths in any repo artifact or output.

Strict TDD:
1. Add focused RED tests proving the current label-derived pins differ from actual DER/SPKI/certificate/bundle/manifest/policy/profile bytes and cannot verify the X.509 chain/critical repair-role + signature-domain extensions.
2. Implement minimal GREEN.

Required correction:
- Create checked-in canonical PUBLIC artifacts under `internal/repaircontract/testdata/release-v1/`: a public trust-bundle oracle containing the actual root/leaf cert and SPKI public material (base64 or another closed canonical public representation), a supervisor policy declaration, supervisor profile declaration, contract build/release declaration, and release manifest binding exact content hashes. These must contain no private material, secrets, paths, executable argv/env, or installation-specific device/inode.
- Prefer `go:embed` so the compiled pure-contract package derives/verifies pins from actual checked-in artifact bytes with no caller path, environment, DB, or runtime substitution. The release manifest must bind exact artifact hashes; no hash may be `SHA256(public label)` pretending to identify an artifact.
- Parse/verify actual Ed25519 X.509 root self-signature and leaf chain. Verify exact DER/SPKI hashes, leaf role extension `controlled_repair_review_attestor`, signature-domain extension `ananke.controlled-repair.review-attestation.v1`, validity window, key usages, issuer, and distinctness from the P5 protocol role/key.
- Separate portable release pins (artifact content/SPKI/manifest/policy/profile hashes) from installation-specific file identity. Remove synthetic compiled device/inode/size. Keep only a schema for future no-follow descriptor observations if needed; do not claim a portable release pins them.
- Remove the fake valid rotation instance whose cross-signature/approval hashes are label digests. Freeze a closed rotation POLICY/protocol: exact future rotation schema, signature domains, current-root and independent release-approver roles, canonical signed objects, activation/overlap/not-before/not-after rules, and `no_successor_authorized` as the valid V1 fixture state. No successor is active in this batch. Rotation attempts in normal authorization remain forbidden. Do not invent signature bytes.
- Update fixture and docs accurately. Keep this pure contract only; no filesystem open, DB, runtime, command, socket, private-key, or signature-generation code.
- Ensure the attacker-generated self-consistent bundle still fails against embedded actual release artifacts.
- Add permanent tests for bundle/manifest/policy/profile byte drift, certificate/SPKI/role/domain drift, synthetic descriptor facts rejection, and unmaterialized rotation attempts.
- Run package single, count=10, race count=3, package vet, gofmt, git diff --check. Return RED evidence and exact GREEN results.

Allowed edits: `internal/repaircontract/**` and `docs/experiments/p6-controlled-repair-supervisor-contract.md` only. Do not edit store/runtime/trustedsupervisor/commands/migrations/go.mod/plan.
