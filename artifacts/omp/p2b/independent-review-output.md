Working...
## CHANGES REQUESTED

### M1 — Grill decoder leaks raw parser errors

`decodeGrillPayload` correctly uses `DisallowUnknownFields`, but returns the decoder’s raw error to the daemon client:

- `internal/lifecycle/engine.go:2080–2089` builds `Error: "invalid grill request: " + err.Error()`.
- A live isolated daemon probe sent an unknown Grill field and received:

```json
{"ok":false,"error":"invalid grill request: json: unknown field \"raw_revision_prose_secret\""}
```

This violates the required raw-error privacy boundary. The existing lifecycle test only asserts rejection and absence of `grill_evaluation`; it does **not** assert a stable non-revealing error (`internal/lifecycle/grill_api_test.go:57–66`).

**Required correction:** return a fixed public error for every nested Grill decode failure, without concatenating parser errors; add a daemon-level regression asserting injected field names/values and decoder diagnostics never appear in the response.

---

## Audit evidence

| Area | Result | Evidence |
|---|---|---|
| Frozen six-rule evaluator | Pass | Exact P2a rule table at `internal/store/grill.go:138–145`; triggers match the contract at `:265–282`; priority sorting, five-visible bound, waiver slot, and ten-question cap are implemented at `:316–363`. |
| JCS SHA-256 input hash | Pass | `HashGrillInput` validates then hashes only the closed input projection (`grill.go:147–175`). Shared encoder sorts UTF-16 keys, validates UTF-8, uses JCS number handling, and hashes exact bytes (`proposal_canonical.go:17–236`). Canonical P2a hash test passed. |
| Exact P1 revision binding | Pass | Runtime validates `(proposal_id, revision, revision_hash)` against `task_proposal_revisions` before writes (`grill.go:399–405`, `:443–455`); v10 adds matching deferred FKs (`store.go:746–749`, `:778–780`). |
| Append-only / sequences / idempotency / restart / concurrency / cap | Pass | v10 primary/unique keys scope sequences to the exact revision (`store.go:775–777`); update/delete triggers reject mutation (`:784–789`). Immediate transactions are configured per handle (`store.go:31–33`, `:46–50`). Focused Store tests cover restart replay, concurrent evaluation, independent revision streams, and nine-to-ten cap (`grill_test.go:102–176`, `:251–360`). |
| Default and waiver authority | Pass | Default is read from the persisted deterministic question; answer is fixed to `acknowledged`; override requires the persisted question’s `waivable` bit and writes only `waived` (`grill.go:594–650`). Non-waivable override is explicitly tested (`grill_test.go:239–248`). |
| No Proposal / Approval mutation | Pass | Grill paths only query the bound revision and Grill tables; no Proposal/Approval mutation call exists in Grill handlers. Store regression snapshots and compares Proposal/Approval state (`grill_test.go:112–129`, `:332–359`). |
| Private native wire; no public renderer/Tauri/UI exposure | Pass | Native Grill types are private bridge structs (`gui/src-tauri/src/lib.rs:444–510`); Tauri’s registered handler list contains no Grill command (`:1115–1235`). The native test serializes only `cmd`, `token`, and `grill`, and denies task/approval/command/model/worker/execution fields (`:1271–1326`). No `grill` match exists in generated renderer DTOs, renderer source, public schemas, or GUI scripts. |
| No model / worker / claim / adapter / execution coupling | Pass | Repository Grill references are restricted to Store, daemon private handlers, their tests, and private native bridge types; none occur in `cmd/` or `internal/supervisor/`. |

## Verification run

Passed:

- `node --check contracts/p2a/verify.mjs`
- `node contracts/p2a/verify.mjs`
- `node contracts/p2a/verify.mjs --self-test`
- `node contracts/p1a/verify.mjs`
- `node contracts/p1c/verify.mjs`
- `go test ./internal/store ./internal/lifecycle -run Grill -count=1 -timeout 120s`
- `cargo test --manifest-path gui/src-tauri/Cargo.toml private_grill_wire_is_closed_and_not_renderer_public`
- All GUI public gates: TypeScript check, generated-model check, public-field privacy check, state test, decoder test, privacy mutation test.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml` — 21 tests passed.
- `go test ./... -count=1 -timeout 300s` — 3 packages passed; 3 had no tests.

Verification caveat: the first full Go invocation failed once in unrelated `TestLaunchWorkerStartsPausedDistinctGroupLeader` with an empty partial helper result. Its isolated `-count=3` run passed, and the immediate full-suite rerun passed.

No repository files were edited or committed.
