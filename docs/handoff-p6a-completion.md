# Ananke P6a Completion — New Session Handoff Prompt

## Role

You are the Hermes Orchestrator. Continue the Ananke P6a controlled-repair
project. The contract layer (Slices 1–9) is 100% complete and audited. The
next phase is post-contract runtime implementation (steps 1–12). Every review
and audit must use dual-model (K3 + GLM-5.2 parallel), with GLM-5.2 for fix
execution.

## Repository

- Path: `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`
- Branch: `feat/task-proposal-core`
- HEAD: `a85642c` (Slice 9 audit fix committed and pushed)
- Remote: `origin` → `github.com:yingliang-zhang/ananke.git`
- Go module, `go.mod` present
- Python: `python3=3.11.15`, `uv=installed`, PEP 668 (use venv or uv)

## What's done — Contract layer 100% (P6 Slices 1–9)

### Slice completion table

| Slice | Content | Commit | Audit |
|---|---|---|---|
| 1–2 | Trust bootstrap + authorization | early commits | K3+GLM ACCEPT |
| 3 | Supervisor intent claims | early commits | K3+GLM ACCEPT |
| 4 | Repository worktree materialization | `19f01e0` | K3+GLM ACCEPT |
| 5 | Adapter sandbox contract | `039c6cc` | K3+GLM ACCEPT |
| 6 | Closed offline Go test profile | `43b53f3` | K3+GLM ACCEPT |
| 7 | Canonical repair-review attestation | `3e6c6b3` | K3+GLM ACCEPT |
| 8 | Ananke verification + persistence | `943fb5c`+`e84208c` | K3 ACCEPT, GLM timeout (no P0-P3) |
| 9 | Pre-release schema/API cutover | `dc682f5`+`a85642c` | K3 P3 fixed (CutoverID cross-bind); GLM timeout (partial analysis consistent) |

**50 Go files, ~20K LOC contract code, 580+ test vectors. All gates green.**

### Slice 9 audit details (2026-07-31)

- K3 (900s): CHANGES REQUESTED → 1×P3 (CutoverID form-validated but not cross-bound
  to frozen authority) + 2×P4 (dead code, non-blocking). Fixed: added
  `record.CutoverID != authority.CutoverID ||` to evaluator cross-binding,
  added `wrong_cutover_id_rejects` vector (count 26→27), regenerated machine-contract JSON.
- GLM (600s): timed out. JSONL 633KB, partial analysis investigating same area
  (CutoverID, recordIntegrityHash). Consistent with K3 findings.

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
- `canonical*FixtureForTest(t)` → chained fixture builders (Slice 4→5→6→7→8→9)

### Dual-model audit process

For each runtime step after implementation:
1. Write audit prompt to `/tmp/p6-stepN-audit-prompt.md` (copy to unique files per model)
2. Launch K3 audit: `omp_with_timeout.sh 900 ... --hermes-provider custom:sudo-kimi-k3 --hermes-model t9s/kimi-k3 --role audit --task-tier normal`
3. Launch GLM audit: `omp_with_timeout.sh 600 ... --hermes-provider custom:sudo --hermes-model glm-5.2 --role audit --task-tier normal`
4. Both in parallel (`terminal background=true notify_on_complete=true`)
5. Aggregate findings, fix P0–P3 (P4 optional but fix if simple)
6. GLM-5.2 executes fixes, K3 verifies
7. If GLM times out, try no-tools resume (120s) or extract from JSONL

### OMP environment

```bash
export HERMES_CODING_WORKFLOW=coupled-v1
# Wrapper: ~/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh
# K3 provider: custom:sudo-kimi-k3, model: t9s/kimi-k3
# GLM provider: custom:sudo, model: glm-5.2
# K3 times out ~900s, GLM ~600s — if GLM times out, extract JSONL findings
# Prompt/output files MUST be in /tmp/, NOT in the repo (path overlap causes exit 2)
# Each parallel session needs unique prompt copy + output file + session-dir
# Clean stale hard-admission slots before dispatch: rm -rf ~/.hermes/profiles/orchestrator/run/omp-hard-admission/slots/slot.* and capacity file
```

## Remaining tasks — Post-contract runtime implementation

### Task 2: Post-contract runtime implementation (steps 1–12)

Follow the plan's post-ACCEPT implementation order. Each step needs:
- Implementation (GLM-5.2 or orchestrator direct for small fixes)
- Focused tests
- Dual-model audit (K3 + GLM)
- Fix findings (GLM-5.2)
- Commit + push

**Implementation order (from `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md`):**

1. Extract shared public cryptographic/Unix-transport primitives without changing P5 semantics.
   - Source: `internal/trustedsupervisor/` (62 files, P5 codebase)
   - Key files: `authentication.go`, `peer_darwin.go`, `canonical.go`, `protocol.go`, `frame.go`, `server_keys.go`
   - Target: shared package for reuse by P6 runtime
2. Implement release-pinned repair verifier and dedicated key-role provisioning.
3. Replace unreleased P6 schema with signed attestation + outbox mirror contract.
   - Extend `internal/store/` (39 files, at v14)
4. Implement dedicated supervisor FULL-sync journal and at-most-once claim/recovery matrix.
   - Create `internal/repairsupervisor/` (0 files now)
5. Implement descriptor-bound worktree materialization and ignored/filesystem diff closure.
6. Implement sandboxed provider-free fake adapter with UID terminal proof.
7. Implement closed offline Go profile in disposable sandbox with the same terminal proof.
8. Implement signed attestation and Ananke atomic verification/persistence.
   - Create `internal/repairrunner/` (0 files now)
   - Extend `internal/store/`
9. Run provider-free E2E ending only in verified `waiting_for_review`.
10. Independent implementation hard review; repair/resume until ACCEPT.
11. Only then add the real route-aware OMP repair adapter and repeat independent review.
12. Only after all gates wire GUI submission/status/evidence/accept-reject. No automatic commit/push/merge.
   - Create `internal/gui/` (0 files now, Tauri 2 shell)

**Critical**: Steps 11 (real OMP adapter) and 12 (GUI wiring) are the last
gates before Ananke can be used to develop project code.

### Task 3: Provider-free E2E (step 9)

Run a full end-to-end test from dispatch request through verified
`waiting_for_review` state, using only contract-layer capabilities (no real
OMP, no real OS operations). This proves the entire chain works.

### Task 4: Independent hard review (step 10)

Fresh K3 + GLM parallel hard review of the entire P6a implementation.
Repair/resume until both models return ACCEPT.

## Verification commands after implementation

Run focused RED/GREEN tests after each tracer bullet, then final gates:

- `go test ./internal/repaircontract -count=10`
- `go test -race ./internal/repaircontract -count=3`
- `go test ./internal/store ./internal/repairsupervisor -count=10`
- `go test -race ./internal/store ./internal/repairsupervisor -count=3`
- `go test ./... -count=1`
- `go test -race ./internal/store ./internal/repairsupervisor ./internal/trustedsupervisor -count=1`
- `go vet ./...`
- `git diff --check`

## Conventions

- Code/comments/commits: English
- User-facing reports: Chinese (concise + tables)
- Quality claims must cite: run name + commit + metric
- No `text` code fences (Hermes Desktop renders them as leaked prefixes)
- Commit format: `feat(repaircontract): P6 Slice N — ...` or `fix(repaircontract): ...`
- Push after each accepted step
- Known issue: `internal/trustedsupervisor` TestProductionServer* timeout cluster is pre-existing (not a regression)

## Autonomous execution

You are pre-authorized for: edits, tests, feature-branch git (commit/push),
verification, and venv setup. Escalate only for: main merge, destructive
operations, or major direction changes.

Work autonomously through runtime steps 1–12 → E2E → hard review.
Pause only for major blockers or direction changes.

## First action

1. `cd /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`
2. `git log --oneline -5` (confirm HEAD = a85642c)
3. Read `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md` (full plan, post-ACCEPT section)
4. Read `internal/trustedsupervisor/` key files for Step 1 (extract shared primitives)
5. Start Step 1 implementation
