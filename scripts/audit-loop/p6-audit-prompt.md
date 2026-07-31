# P6 Contract Slices 1–6 Hard Audit

## Repository
`/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` on branch `feat/task-proposal-core` at HEAD `43b53f3`.

## Scope
Audit the complete P6 controlled-repair supervisor contract: Slices 1–6 in `internal/repaircontract/`.

### Files in scope (read-only audit)
- `internal/repaircontract/contract.go` — Slice 1: trust bootstrap, role separation, rotation
- `internal/repaircontract/canonical.go` — Slice 1: canonical RFC 8785/JCS helpers
- `internal/repaircontract/supervisor_intent.go` — Slice 3: phase claims, recovery
- `internal/repaircontract/repository_worktree.go` — Slice 4: repository, common-.git, worktree authority
- `internal/repaircontract/adapter_sandbox.go` — Slice 5: adapter sandbox, UID terminal proof
- `internal/repaircontract/test_profile.go` — Slice 6: closed offline Go test profile
- All corresponding `_test.go` and `_registry_test.go` and `_document_test.go` files
- `docs/experiments/p6-*.md` — normative documents with machine-contract JSON blocks

### NOT in scope
- `internal/trustedsupervisor/` — P5 runtime (different package)
- `internal/store/` — storage layer
- Any runtime/execution code

## Audit categories

### 1. Contract correctness
- Every self-hashed record type uses `mustHashRecord`/`recordHashMatches` correctly
- `mustDerive...` functions panic on hash mismatch
- `deriveFrozen...` requires exact `reflect.DeepEqual` with frozen compiled value
- Frozen compiled values are deterministic (same values every init)
- `VerifyReleaseTrust` is called first in every evaluator before any other check
- `EffectAllowed` is always `false` — no production effect path exists
- No standalone production minter exists for any capability type

### 2. Closed enum integrity
- Every string enum has a switch-based validator
- Unknown enum values are rejected before evaluation
- State/reason/action/cleanup-result enums are complete and closed
- No string literal in switch cases that isn't in the enum constant set

### 3. Opaque capability boundary
- Capability structs have private fields only
- `verified...Intact` re-derives all seals from decoded observation under frozen verifier
- No production constructor or decoder from caller bytes
- Integrity hash covers all binding fields
- Mutation of any binding field breaks integrity check

### 4. Cross-slice binding
- Each evaluator validates the correct predecessor phase and sequence
- Slice 4 evaluator checks Slice 3 claim/event
- Slice 5 evaluator checks Slice 4 worktree capability
- Slice 6 evaluator checks Slice 5 adapter sandbox capability
- Authority matching includes repository binding hashes (P3 from Slice 5 review)

### 5. Verification seal completeness
- Each verification kind produces a self-hashed seal with kind-specific evidence
- Evidence structs bind the correct observation fields
- Aggregate seals hash is SHA-256 of canonical ordered array
- All seals are recomputed during integrity check (not cached)

### 6. Test coverage
- All mandatory vectors from the plan are present in each slice's registry
- Vector IDs match canonical inventory exactly (order, count, names)
- Mutation isolation probes cover all binding fields
- Frozen determinism probes exist for each frozen compiled value
- Enum rejection probes exist for each closed enum
- Registry test uses `assertExecutedVectorOrder` with `reflect.DeepEqual`

### 7. Canonical JSON / schema
- `Decode...Observation` rejects unknown fields, duplicate keys, trailing data, BOM, invalid UTF-8
- Hash format is `sha256:` + 64 lowercase hex digits
- Schema versions match between Go types and normative documents
- Machine-contract JSON in normative docs matches types/fixture/inventory

## Output format

Produce a structured audit report with findings classified as:
- **P0** — critical security/correctness defect (blocker)
- **P1** — major correctness or security issue
- **P2** — moderate issue with potential impact
- **P3** — minor issue, improvement opportunity
- **P4** — nit, style, documentation

For each finding:
- Severity (P0–P4)
- Category (1–7)
- File and line number
- Description of the issue
- Evidence (code snippet or test output)
- Recommended fix

End with a verdict: `ACCEPT` (no P0–P3 findings) or `CHANGES REQUESTED` (one or more P0–P3 findings).

## Verification commands to run

```bash
go build ./... && echo "BUILD_PASS"
go vet ./... && echo "VET_PASS"
go test ./internal/repaircontract -run '^TestP6Slice' -count=1 -timeout 120s -v
gofmt -d internal/repaircontract/*.go
```
