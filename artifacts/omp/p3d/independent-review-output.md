Working...
## ACCEPT

No edits or commits made.

**Boundary audit**
- Route is closed to `ananke_omp_readonly_wrapper_v1` / `ananke_omp_read_only_audit_v1`; bare `omp` and alternate routes fail closed.
- Provider/model, deadline, attempt cap, and read-only scope are exact: `omp`, `omp_audit_model_v1`, `2026-07-30T12:00:00Z`, cap `3`, `read_only` / `sealed_payload_only` / `writes: forbidden`.
- P3a cross-binding independently matched: launch hash, model, deadline/cap, sealed materialization hash/nonce, materialization ID, run ID, attempt, and P3c `retry_process_admission`.
- P3b exposure is only `fence_fingerprint`; no token or token-hash field enters the P3d request.
- Target is `github.com/yingliang-zhang/ananke`, matching `go.mod`; it binds trusted-root and required-source-snapshot hashes. Static scan found no personal filesystem paths in P3d contracts/docs.
- Request/event/result schemas are closed and bounded. Events are exactly the three normalized shapes; result exposes counts, completed state, and `verification_state: "not_run"` only.
- Unknown transcript source, dialect, and event all produce exactly the less-informative `waiting_for_human` state with no events or result.
- Renderer command/prompt/prose and token/socket/path/raw-error vectors have no authority and fail closed. Raw authority-field rejection covers the contract fixtures.
- Four crash/cancellation boundaries preserve absent result and terminal state, emitting only their safe retry/reconnect obligation; no outcome is guessed.
- `contracts/p3d/verify.mjs` reads fixture bytes and hashes them. No child-process, write, temp-directory, adapter, runtime, worktree, daemon, or UI implementation surfaced.

**P3b/P3c alignment**
- P3b defines the full active fence as claim ID, token hash, and generation; P3d intentionally reduces it to an opaque non-credential fingerprint.
- P3c’s durable process-admission action is `retry_process_admission`; its source/tests keep real runs uncreated at this boundary.

**Contract chain passed**
```sh
node --check contracts/p3a/verify.mjs
node contracts/p1a/verify.mjs
node contracts/p1c/verify.mjs
node contracts/p2a/verify.mjs
node contracts/p2c/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
```

All commands exited `0`; P3d self-test rejected route/provider/model drift, renderer/private-field authority, predecessor/seal drift, target/transcript drift, unearned results, and recovery guesses.
