# P3d controlled read-only OMP adapter contract / fixture TDD plan

**Goal:** freeze the bounded post-P3c adapter envelope for a future Ananke
self-audit without creating an OMP adapter, opening a repository, or executing
anything.

**Authorized now:** P3d canonical/adversarial/crash fixtures and manifest, a
dependency-free verifier with an in-process self-test, this plan, the P3d
contract document, and a factual ledger entry. **Not authorized:** a worktree,
host-file or source-snapshot materialization, process, socket, daemon, Tauri/UI,
renderer/runtime API, OMP adapter, model call, transcript ingestion, command,
verification execution, commit, or push.

## Task 1: RED/GREEN — closed route-aware HostSpec

**Files:** `contracts/p3d/fixtures/omp-audit-v1.canonical.json`,
`contracts/p3d/verify.mjs`.

1. Bind P3a's exact launch hash, deadline/cap, provider/model, sealed
   materialization hash/nonce, materialization ID, Run ID, and P3c
   `retry_process_admission` action. Represent P3b ownership only with an
   opaque fence fingerprint; do not expose a token.
2. Freeze wrapper kind `ananke_omp_readonly_wrapper_v1` and route
   `ananke_omp_read_only_audit_v1`. Reject bare `omp` and every other route.
3. Require `read_only`, sealed-payload-only materialization, and forbidden
   writes; freeze bounded cancellation, reconnect recovery, transcript
   normalization, and verification capabilities.
4. Bind the canonical repository identity from `go.mod`, not a filesystem
   location, with trusted-root and required-source-snapshot fingerprints.
5. Seal the opaque payload hash, materialization hash, and nonce with a
   canonical fingerprint; bind the HostSpec's canonical fingerprint.
6. RED: mutate an in-memory fixture after changing route, wrapper,
   P3c action, sealed nonce, target, transcript source, or a private authority
   field. GREEN: every mutation is rejected.

**Acceptance:** the fixture carries no raw command, prompt, prose, token,
socket, path, process ID, raw error, or payload/source bytes.

## Task 2: RED/GREEN — bounded normalized audit IR

**Files:** `contracts/p3d/fixtures/omp-audit-v1.canonical.json`,
`contracts/p3d/fixtures/adversarial-v1.canonical.json`,
`contracts/p3d/verify.mjs`.

1. Freeze closed request, event, and result schema versions. Events are only
   known normalized `audit_started`, `audit_finding`, and `audit_completed`
   shapes with bounded sequence. The result exposes only count summary,
   completion, and `verification_state: "not_run"`.
2. Bind request deadline/cap, target, sealed materialization, and HostSpec hash
   exactly. A verification command fingerprint is a binding only, never an
   execution request.
3. Freeze less-information `waiting_for_human` for unknown transcript source,
   dialect, or event. Do not synthesize completion or a result.
4. Freeze the same result for renderer command/prompt/prose authority and
   renderer token/socket/path/raw-error fields; no renderer field can become
   transport or execution authority.
5. RED: mutate a known event to an unknown kind or leak a completed result from
   an adversarial in-memory fixture.

**Acceptance:** no normalized public shape exposes raw transcript content,
credentials, connection metadata, locations, errors, commands, or execution
inputs.

## Task 3: RED/GREEN — cancellation and recovery facts

**Files:** `contracts/p3d/fixtures/crash-v1.canonical.json`,
`contracts/p3d/verify.mjs`.

1. Freeze only four durable boundaries: before admission; before first event;
a known event prefix before result; and cancellation requested before terminal
event.
2. Require respectively `retry_adapter_admission`,
`reconnect_transcript_source`, `reconnect_transcript_source`, and
`retry_bounded_cancellation`.
3. Every recovery outcome emits no event, writes no result, and has terminal
state `absent`. Do not infer an OMP session, cancellation completion, terminal
state, finding, verification, or process state.
4. RED: inject a result or terminal state, or swap an action. GREEN: the
verifier rejects the in-memory fixture.

**Acceptance:** reconnect/cancellation capability is declared but never
implemented or exercised by this slice.

## Task 4: Future runtime tests — document only

A separately authorized implementation must first demonstrate:

1. It authenticates P3b's complete active fence privately before translating a
   P3c process-admission obligation into the exact P3d request; a fingerprint is
   never used as a credential.
2. It resolves the route only to the frozen wrapper and does not fall back to a
   bare OMP executable, arbitrary executable, command, prompt, or renderer
   value.
3. It validates the trusted repository identity and required source snapshot
   after each filesystem boundary, without a lexical location prefix check or a
   mutable-memory source.
4. It proves real read-only behavior, deadline/cap enforcement, bounded cancel,
   reconnect, transcript normalization, raw-error containment, and cleanup
   against controlled OMP fixtures. No unknown event may produce success.
5. It verifies no token, socket, filesystem location, raw error, transcript
   body, command, prompt, or prose crosses the public boundary.
6. It handles all four crash boundaries without inferring adapter admission,
   session state, result, terminal state, cancellation completion, or evidence.

## Contract-only gate

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
```

The self-test is in process; it reads contract fixtures and mutates only
in-memory values. It creates no host files and starts no adapter, OMP, host
process, runtime, daemon, UI, or verification command.

## Explicit non-goals

- implementation, launch, monitoring, cancellation, recovery, or cleanup of an
  OMP adapter or any other process;
- any repository/worktree/host-file creation, opening, snapshot collection, or
  materialization; any database/store/lifecycle modification;
- daemon, Tauri/UI, renderer/public protocol, generated code, socket, API,
  model, prompt, command, transcript, or verification execution;
- commit or push.
