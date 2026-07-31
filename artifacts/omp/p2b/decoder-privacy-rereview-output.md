Working...
## ACCEPT

The prior blocker is closed.

- **Daemon privacy:** `decodeGrillPayload` retains `DisallowUnknownFields` and returns exactly `invalid grill request` for both initial and trailing decoder failures (`internal/lifecycle/engine.go:2073-2089`).
- **Live regression:** `TestGrillCommandsServeFrozenPrivateReviewProtocol` injects nested `raw_revision_prose_secret` plus its sentinel value through the live Unix socket. It requires rejection, no `grill_evaluation`, the exact stable error, and absence of the field name, value, and `json: unknown field` from serialized output (`internal/lifecycle/grill_api_test.go:59-85`). Passed.
- **Bridge boundary:** daemon rejections are converted to the generic public message `The daemon rejected this request.`; raw daemon text is not exposed through Tauri (`gui/src-tauri/src/lib.rs:92-117,1104-1113`). No Grill command is registered in the Tauri invoke list.
- **P2b invariants:** exact P1 revision tuple validation occurs before writes; v10 keys/FKs scope records to that tuple; update/delete triggers enforce append-only `grill_records`; evaluator caps Question records at ten and returns `needs_rewrite` without writing at capacity. Restart, replay, concurrent evaluation, separate revisions, waiver authority, and cap behavior are covered by `internal/store/grill_test.go`.
- **Private-only scope:** no Grill references found in renderer source, renderer contracts/generated DTOs/scripts, `cmd`, or `internal/supervisor`. The Rust private-wire test serializes only `cmd`, `token`, and closed `grill` data, denying task/approval/command/model/worker/execution fields.

### Executed gates

- `go test ./internal/store ./internal/lifecycle -run Grill -count=1 -timeout 120s` — passed.
- `go test ./... -count=1 -timeout 300s` — passed; 3 tested packages, 3 without tests.
- P2a syntax/verifier/self-test; P1a and P1c verifiers — passed.
- Focused Rust private-wire test — 1 passed.
- Rust `--all-targets` immediately after focused test — 21 passed; no `BrokenPipe`.
- Rust normal full suite — 21 passed across 3 suites.
- GUI typecheck, generated-model/privacy checks, state, decoder, privacy tests, and production web build — passed.
- Go and Cargo formatter checks — clean.

**Non-blocking hygiene:** global `git diff --check` reports only `docs/experiment-ledger.md:845: new blank line at EOF`; source-only diff hygiene passes.
