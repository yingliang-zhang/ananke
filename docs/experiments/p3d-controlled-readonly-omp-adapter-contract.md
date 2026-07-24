# P3d controlled read-only OMP adapter contract

**Status:** frozen contract and fixture oracle.
**Scope:** a declarative adapter boundary only. This slice implements no HostSpec
loader, persistence, daemon, renderer API, worktree, filesystem materialization,
OMP invocation, monitor, cancellation action, recovery action, verification
command execution, or process launch.

## Decision

P3d defines the next boundary after P3a admission, P3b durable fencing, and P3c
`retry_process_admission`: a **route-aware Ananke OMP read-only audit wrapper**.
It is explicitly not a bare `omp` executable declaration. The adapter request is
admitted only when its closed HostSpec and P3a/P3b/P3c binding exactly match the
frozen vector. A rendered value cannot construct or override a route, command,
prompt, prose, provider, model, target, deadline, cap, materialization, or
transcript interpretation.

The canonical audit target is the repository identity
`github.com/yingliang-zhang/ananke`, matching `go.mod`, plus opaque trusted-root
and required-source-snapshot hashes. It is never a personal filesystem location.
No fixture contains a filesystem path, socket, process identifier, token, raw
error, command line, prompt, or transcript body.

## Predecessor bindings

| Boundary | Exact P3d use | Not granted |
| --- | --- | --- |
| P3a | `launch_spec_hash`, deadline `2026-07-30T12:00:00Z`, cap `3`, provider `omp`, model `omp_audit_model_v1`, and the sealed materialization hash/nonce | A prompt, command, worktree, or process permission. |
| P3b | The current full fence is reduced to an opaque `fence_fingerprint`; the request has no token or claim secret. | A writable projection or direct access to the store. |
| P3c | `run_p3a_001`, `materialization_p3a_001`, attempt `1`, and only `retry_process_admission` are bound into the request. | Authority to execute the returned obligation. |

The P3d request contains a fingerprint rather than a P3b token. A future private
adapter implementation must authenticate the original full active fence before
producing this request; the fingerprint is an immutable correlation binding, not
a credential.

## Frozen HostSpec

`ananke.omp-readonly-host-spec.v1` is closed and canonically hashed excluding
`host_spec_fingerprint`. It fixes:

| Field | Frozen value/invariant |
| --- | --- |
| Adapter | Wrapper kind `ananke_omp_readonly_wrapper_v1` and route `ananke_omp_read_only_audit_v1`; bare `omp` is rejected. |
| Model | Only provider `omp` and model `omp_audit_model_v1`. |
| Bounds | Exact P3a deadline and cap; this contract does not run a timer or retry loop. |
| Capability | `read_only` access, `sealed_payload_only` materialization, `forbidden` writes, bounded cancellation, reconnect recovery, transcript normalization, and verification. |
| Sealed materialization | Opaque payload hash, P3a materialization hash and nonce, plus a canonical seal fingerprint. The payload bytes never appear. |
| Target | Canonical Ananke repository identity, trusted-root fingerprint, and required source-snapshot hash. No location is carried. |
| Transcript | One wrapper transcript source, input dialect, output dialect, normalization algorithm identifier, and source fingerprint. |
| Verification | Read-only verification name and command fingerprint only. This is a binding, never an argv value or execution request. |

The immutable request repeats only the HostSpec hash, immutable bounds, sealed
materialization, trusted target, and the P3a/P3b/P3c launch binding. It does not
carry a renderer-provided authority field.

## Bounded normalized IR

The fixtures freeze three closed representations:

| IR | Closed content |
| --- | --- |
| `ananke.omp-audit-request.v1` | Request ID; HostSpec hash; exact deadline/cap; P3a/P3b/P3c binding; sealed materialization; trusted target. |
| `ananke.omp-audit-event.v1` | Sequence-bounded, normalized `audit_started`, `audit_finding`, and `audit_completed` shapes only. No transcript text or role inference. |
| `ananke.omp-audit-result.v1` | Request ID, event count, two integer finding counts, completed state, and `verification_state: "not_run"`. It carries no raw OMP output and executes no verification. |

Unknown transcript source, dialect, or event shapes are not translated into a
success, completion, cancellation, or finding. Every adversarial vector instead
returns exactly the less-informative public state:

```json
{"adapter_state":"waiting_for_human","events":[],"result":null,"verification_state":"not_run"}
```

The same state applies to noncanonical routes, provider/model drift, write
capability, unsealed payload/nonce, target or source-snapshot drift, any renderer
command/prompt/prose authority, and proposed renderer token/socket/path/raw-error
fields. The result deliberately names neither an error nor a reason class.

## Cancellation and recovery

The crash fixture freezes four known durable boundaries. Each has an exact safe
next obligation and no inferred event, result, terminal state, verification, or
process outcome.

| Boundary | Safe action | Facts never inferred |
| --- | --- | --- |
| Request before adapter admission | `retry_adapter_admission` | Admission, events, result, terminal state. |
| Admission before normalized event | `reconnect_transcript_source` | Any transcript event or result. |
| Known normalized prefix before result | `reconnect_transcript_source` | Completion, finding changes, or result. |
| Cancellation requested before terminal event | `retry_bounded_cancellation` | Cancellation completion, terminal state, or result. |

These are only recovery facts. They neither open a connection nor cancel an OMP
session.

## Artifacts and contract gate

- `contracts/p3d/fixtures/omp-audit-v1.canonical.json` — closed HostSpec plus
  canonical request, normalized events, and bounded result.
- `contracts/p3d/fixtures/adversarial-v1.canonical.json` — route, scope, target,
  transcript, renderer-authority, and public-information denial vectors.
- `contracts/p3d/fixtures/crash-v1.canonical.json` — no-guess admission,
  reconnect, and cancellation boundaries.
- `contracts/p3d/fixtures/fixtures.sha256` and verifier hard digests — exact
  canonical fixture bytes.
- `contracts/p3d/verify.mjs` — dependency-free Node verifier and in-process
  in-memory fixture self-test.

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
```

The normal gate reads only fixture bytes. The self-test mutates in-memory
fixture values and invokes verifier functions in the same Node process. It
creates no host files, adapter, OMP session, socket, worktree, materialization,
daemon, UI, runtime, or child process; it executes no frozen verification
command.

## Explicit non-goals

- HostSpec loading, P3b store changes, P3c orchestration changes, leasing,
  authentication, timeout/cap enforcement, or durable adapter state;
- daemon, Tauri, UI, renderer DTO, generated code, public command, network
  transport, OMP adapter implementation, model invocation, transcript ingestion,
  socket, monitor, cancellation implementation, or recovery implementation;
- repository/worktree creation or opening, source snapshot collection,
  materialization, filesystem mutation, command/prompt/prose construction,
  verification execution, evidence settlement, terminal result persistence,
  commit, or push.
