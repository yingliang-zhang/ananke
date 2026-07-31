# Ananke P6a Completion — New Session Handoff Prompt

## Role

You are the Hermes Orchestrator. Continue the Ananke P6a controlled-repair
foundation project. Complete all remaining tasks until Ananke can be used to
develop project code. Every review and audit must use dual-model (K3 + GLM-5.2
parallel), with GLM-5.2 for fix execution.

## Repository

- Path: `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`
- Branch: `feat/task-proposal-core`
- HEAD: `e84208c` (Slice 8 audit fix committed and pushed)
- Remote: `origin` → `github.com:yingliang-zhang/ananke.git`
- Go module, `go.mod` present
- Python: `python3=3.11.15`, `uv=installed`, PEP 668 (use venv or uv)

## What's done (P6 Slices 1–8, 89% complete)

### Contract layer (`internal/repaircontract/`, 35 Go files, ~20K LOC)

| Slice | Content | Commit | Audit |
|---|---|---|---|
| 1–2 | Trust bootstrap + authorization | early commits | K3+GLM ACCEPT |
| 3 | Supervisor intent claims | early commits | K3+GLM ACCEPT |
| 4 | Repository worktree materialization | `19f01e0` | K3+GLM ACCEPT |
| 5 | Adapter sandbox contract | `039c6cc` | K3+GLM ACCEPT |
| 6 | Closed offline Go test profile | `43b53f3` | K3+GLM ACCEPT |
| 7 | Canonical repair-review attestation | `3e6c6b3` | K3+GLM ACCEPT |
| 8 | Ananke verification + persistence | `943fb5c`+`e84208c` | K3 ACCEPT, GLM timeout (no P0-P3) |

### Established patterns (MUST follow for all new code)

1. **Canonical records**: RFC 8785/JCS canonical JSON, `hashRecord(record, "own_hash_field")`
   for self-hash, `recordHashMatches` for validation, `decodeCanonicalRecord[T]` for decode.
2. **Frozen compiled values**: `mustDerive...()` panics at init on hash mismatch;
   `deriveFrozen...()` requires exact `reflect.DeepEqual` with the init-time frozen value.
3. **Opaque capabilities**: Private fields only, `verified*Intact` recomputes all seals
   from decoded canonical under frozen verifier (defense-in-depth, NOT cached).
4. **Closed string enums**: Switch-based validators, unknown values rejected before evaluation.
5. **Evaluator pattern**: `VerifyReleaseTrust` first → derive frozen verifier → validate
   predecessor capabilities intact → validate snapshot → cross-bind to frozen values →
   mint capability → re-check intact. `EffectAllowed` always `false`.
6. **Test structure**: `*_test.go` (fixture + vectors), `*_registry_test.go` (ordered
   vector registry with `assertExecutedVectorOrder`), `*_document_test.go` (normative
   document with machine-contract JSON block).
7. **Normative documents**: `docs/experiments/p6-*.md` with `<!-- BEGIN P6 SLICE N MACHINE CONTRACT -->` JSON block.
8. **Gate matrix**: go build, go vet, focused `-count=1/10`, race `-count=3`, full package
   `-count=1`, gofmt, `git diff --check`, tagged build (`ananke_real_provider_canary,ananke_test_runtime_authority`).
9. **Manifest**: Generate twice, must be byte-identical.
10. **Commits**: English, conventional format (`feat(repaircontract): ...` / `fix(repaircontract): ...`).

### Key shared helpers (in `contract_test.go` / `canonical.go`)

- `testHash(label)` → `"sha256:" + sha256(label)`
- `mustTime(t, value)` → parse RFC3339Nano
- `mustRecordHash(t, value, field)` → hashRecord wrapper
- `canonicalTestArtifact(t, value)` → canonicalBytes wrapper
- `canonicalSupervisorAttempt` → fixture chain root (authorities, claims, terminal events)
- `canonical*FixtureForTest(t)` → chained fixture builders (Slice 4→5→6→7→8)

### Dual-model audit process

For each slice/step after implementation:
1. Write audit prompt to `/tmp/p6-sliceN-audit-prompt.md`
2. Launch K3 audit: `omp_with_timeout.sh 900 ... --provider custom:sudo-kimi-k3 --model t9s/kimi-k3 --role audit --task-tier normal`
3. Launch GLM audit: `omp_with_timeout.sh 600 ... --provider custom:sudo --model glm-5.2 --role audit --task-tier normal`
4. Both in parallel (`terminal background=true notify_on_complete=true`)
5. Aggregate findings, fix P0–P3 (P4 optional but fix if simple)
6. GLM-5.2 executes fixes, K3 verifies

### OMP environment

```bash
export HERMES_CODING_WORKFLOW=coupled-v1
# Wrapper: ~/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh
# K3 provider: custom:sudo-kimi-k3, model: t9s/kimi-k3
# GLM provider: custom:sudo, model: glm-5.2
# K3 times out ~900s, GLM ~600s — if GLM times out, extract JSONL findings
```

## Remaining tasks

### Task 1: P6 Slice 9 — Pre-release schema/API cutover contract

**Plan spec** (from `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md`):
- Remove rejected exported effect/evidence APIs
- Squash/replace unreleased P6 migrations (store is at v14, no v15/v16 yet)
- Reject any populated local rejected-schema DB during migration instead of interpreting it
- Prove production binaries contain no in-process adapter, arbitrary-test runner, rejected API marker, or reused P5 protocol key

**Implementation approach**:
This is a contract-layer slice (no runtime), so implement as:
1. `internal/repaircontract/schema_cutover.go` — canonical contract that:
   - Defines `SchemaCutoverRecord` (self-hashed, closed, RFC 8785/JCS)
   - Binds the current store schema version (14) to the accepted P6 contract
   - Rejects v15/v16 (unreleased) schemas as foreign
   - Proves no rejected API markers exist in production binaries (contract simulation)
2. `internal/repaircontract/schema_cutover_test.go` — test vectors
3. `internal/repaircontract/schema_cutover_registry_test.go` — ordered registry
4. `internal/repaircontract/schema_cutover_document_test.go` — normative document
5. `docs/experiments/p6-schema-cutover.md` — normative document with machine contract

Follow the EXACT patterns from Slices 1–8 (frozen verifier, seals, opaque capability,
mutation isolation, enum rejection, canonical JSON closure, document test).

**Gate matrix**: Same as Slices 1–8.
**Dual-model audit**: K3 + GLM parallel, fix P0–P3, commit + push.

### Task 2: Post-contract runtime implementation (steps 1–12)

After Slice 9 contract ACCEPT, implement the runtime. Follow the plan's
post-ACCEPT implementation order. Each step needs:
- Implementation (GLM-5.2)
- Focused tests
- Dual-model audit (K3 + GLM)
- Fix findings (GLM-5.2)
- Commit + push

**Key runtime packages to create/extend**:
- `internal/repairsupervisor/` (0 files now — create)
- `internal/repairrunner/` (0 files now — create)
- Extend `internal/store/` (39 files, at v14)
- Extend `internal/trustedsupervisor/` (61 files)
- `internal/gui/` (0 files now — create, Tauri 2 shell)

**Critical**: Steps 11 (real OMP adapter) and 12 (GUI wiring) are the last
gates before Ananke can be used to develop project code.

### Task 3: Provider-free E2E (step 9)

Run a full end-to-end test from dispatch request through verified
`waiting_for_review` state, using only contract-layer capabilities (no real
OMP, no real OS operations). This proves the entire chain works.

### Task 4: Independent hard review (step 10)

Fresh K3 + GLM parallel hard review of the entire P6a implementation.
Repair/resume until both models return ACCEPT.

## Conventions

- Code/comments/commits: English
- User-facing reports: Chinese (concise + tables)
- Quality claims must cite: run name + commit + metric
- No `text` code fences (Hermes Desktop renders them as leaked prefixes)
- Commit format: `feat(repaircontract): P6 Slice N — ...` or `fix(repaircontract): ...`
- Push after each accepted slice
- Known issue: `internal/trustedsupervisor` TestProductionServer* timeout cluster is pre-existing (not a regression)

## Autonomous execution

You are pre-authorized for: edits, tests, feature-branch git (commit/push),
verification, and venv setup. Escalate only for: main merge, destructive
operations, or major direction changes.

Work autonomously through Slice 9 → runtime steps 1–12 → E2E → hard review.
Pause only for major blockers or direction changes.

## First action

1. `cd /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`
2. `git log --oneline -5` (confirm HEAD = e84208c)
3. Read `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md` (full plan)
4. Read existing Slice 8 files for pattern reference
5. Start Slice 9 implementation
