# Handoff — Ananke first GUI milestone

## Goal

Finish and publish Slice-001 (durable Go lifecycle), then implement the first usable Tauri 2 + Vanilla TypeScript GUI backed by the real Go daemon.

## Slice-001 status

- **Independent review:** candidate v11 `VERDICT: ACCEPT` (`019f8099-15bf-7000-a2ac-5014079acaa2`; `artifacts/omp/slice-001-final-review/hard-review-v11-output.md`).
- **Frozen source candidate:** `93aef9afc7771cb79c35d3c7df0fa6bca6f50e8071619d0fa36473198b82dd7f`, 68 files; manifest `candidate-manifest-v11.json`.
- **Canonical evidence:** verify, mutation 6/6, stress, blackbox, Python 26/26, gofmt/diff, and post-gate process scans at 0/1/4 seconds all passed.
- **Repair I:** fixes unsealed valid-EOF transcript publication and PID-reuse cleanup risk with a paused worker-group trampoline. Supervisor stays outside the worker group; durable authority is published before release; cleanup is atomic negative-PGID signalling before reaping.
- **P2 hardening:** token entropy errors fail closed; watcher terminal errors wake cleanup; identity file and parent directory are synced; daemon does not print auth tokens.
- **Residual accepted risk:** supervisor crash while resistant descendants remain enters ADR-0002 fail-closed `recovery_unknown`; it does not signal a possibly reused PGID.

## Changed areas

- Lifecycle/store/supervisor contracts and tests: `internal/{lifecycle,store,supervisor}`
- Entry points: `cmd/ananke*`
- Harness/reports: `scripts/`, `reports/`
- Architecture/ledger: `ARCHITECTURE.md`, `docs/adr/0002-supervisor-lifecycle-identity-model.md`, `docs/experiment-ledger.md`
- GUI research: `docs/gui-v0.1-design.md`, `docs/ui-reference-audit.md`

## GUI v0.1 next step

No Tauri scaffold exists yet. Use Node `v22.22.3` at `/Users/yingliangzhang/.hermes/node/bin`; Rust/Cargo are installed at `$HOME/.cargo/bin` but are not on PATH. Implement only the documented v0.1: backend-authoritative project/workstream/run views, launch real Go worker runs, stream status/events, and durable cancel. Do not build an IDE/chat/terminal clone.

## Authorization

User has authorized autonomous development, verification, commit, and push through GUI v0.1 completion.
