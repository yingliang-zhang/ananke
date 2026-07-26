# Local trusted-supervisor transport runbook

## Scope and current state

`ananke-trusted-supervisor-transport` remains the one-shot local Unix-domain-socket client for the P3f external-supervisor handoff runtime. It submits an already sealed handoff, reconciles an already durable receipt, or transports an already admitted cancellation. `ananke-trusted-supervisor` now also owns the P5 policy-selected READ-ONLY audit-executor foundation.

The server authenticates the predecessor projection, repository policy, execution policy, authorization chain, local client UID, replay state, and request bindings before signing protocol receipts, callbacks, or cancellation acknowledgements. A matching envelope `LaunchSpecHash` selects one closed execution-policy entry; caller wire data cannot supply a path, prompt, command, environment, model route, or credential authority.

Delivery returns the durable receipt promptly and starts the audit asynchronously. Reconciliation while an audit is `prepared`, `running`, `finalizing`, or between bounded timeout attempts returns a canonical nonterminal `pending` response and persists no terminal callback. Only bounded, verified output and session evidence can produce a signed `completed` callback. Failure, exhausted attempts, unverifiable crash recovery, malformed evidence, or policy/identity drift produces the existing closed `waiting_for_human` result with no inferred success.

This slice has executed test-created fake route-aware wrappers and an installed OMP v17.1.3 provider-free transport preflight under the real Darwin subprocess and `sandbox-exec` path. The preflight used a fake credential and a loopback gateway that returned a deterministic rejection; it invoked no real model or provider API. The sandboxed wrapper cannot mutate repository or snapshot source. After confirmed process exit, the trusted supervisor removes an owned snapshot or invocation root only after its descriptor-relative device, inode, owner, group, mode, and parent identity match the live or signed authority. An identity mismatch remains nonterminal and does not delete a decoy. The executor cannot execute a repair or create a Run. The P3d, P3f, and P4 canonical fixtures and identities are unchanged. The foundation is not accepted until a new independent hard re-review covers the current source.

## Release build and exact-artifact gate

Build and publish the production server only through the release command. Both paths are required, absolute, and clean; the output parent must already exist. The command refuses the repository-root `ananke-trusted-supervisor` path, accepts no build-tag argument, preserves the effective build environment, and fails rather than clearing `GOFLAGS` if it contains `ananke_test_runtime_authority`.

```sh
go run ./cmd/ananke-trusted-supervisor-release build \
  --repository-root /absolute/path/to/ananke \
  --output /private/operator/bin/ananke-trusted-supervisor
```

The command builds `./cmd/ananke-trusted-supervisor` without a `-tags` argument in an isolated temporary directory on the output filesystem. It checks the effective `GOFLAGS`, verifies the temporary candidate's Go command-package path and build settings, rejects test-runtime symbols, names, source-factory markers, and binary markers, syncs it, atomically renames it to the selected output, and verifies that exact published path again. It prints success only after the final-path verification succeeds.

Verify an existing selected artifact independently with:

```sh
go run ./cmd/ananke-trusted-supervisor-release verify \
  --artifact /private/operator/bin/ananke-trusted-supervisor
```

A successful result applies only to the exact absolute path passed as `--output` or `--artifact`. It is not evidence that another copy, renamed file, package-manager staging path, or installed path was verified. Do not claim an installed artifact is verified until `verify --artifact` succeeds on that final installed path.

The transport client remains a separate build:

```sh
go build -o ./bin/ananke-trusted-supervisor-transport ./cmd/ananke-trusted-supervisor-transport
```

The server and one-shot client are production composition roots. Deployment requires operator-owned trust, private-key, repository-policy, execution-policy, journal, store, and socket paths; the repository does not supply deployment credentials, private signing material, or a default execution policy.

## Socket requirements

The `--socket` value is operator-only configuration. It is never copied into the sealed envelope, framed request, receipt, callback, evidence, or lifecycle output.

Before each exchange the client requires:

- an absolute Unix socket path;
- a socket owned by `--peer-uid`;
- socket permissions with no group or other bits (normally `0600`);
- Darwin `LOCAL_PEERCRED` reporting the same peer UID;
- a per-exchange deadline no longer than 10 seconds;
- a frame limit from 1,024 through 65,536 bytes.

Use a private directory owned by the supervisor account. Do not place the socket in a group/world-writable directory.

## Required trust inputs

Every invocation requires these operator-provided inputs in addition to the store and socket paths:

- `--trust-bundle`: path to a bounded, canonical public JSON trust bundle;
- `--peer-uid`: expected Unix peer user ID;
- `--peer-pid`: expected Unix peer process ID.

The trust bundle contains only public material: active and successor Ed25519 roots for release, approval, and MoA authority; signed root rotations and revocations; signed leaf certificates for the release attestor, release approver, MoA grantor, and supervisor peer; and the signed release-attestation/approval/MoA authorization chain. It contains no private key.

The client always performs the mandatory verification. It validates bundle and record self-hashes, public-key/SPKI bindings, overlapping active/successor root lifetimes, cross-signed rotations, successor-signed revocations, root selection and revocation at each authoritative timestamp, root-signed leaf certificates, detached Ed25519 signatures on the authorization records, and Ed25519 peer-possession signatures over the exact request/channel/message bindings. Optional `AuthenticationHooks` run only after mandatory verification and cannot replace it.

Delivery additionally requires the artifact, build, route, attestation, approval, and MoA bindings to match the sealed envelope and signed authorization chain exactly. Durable receipts, callbacks, and cancellation acknowledgements are reverified against their own authoritative timestamps and the current bundle lifecycle before persistence. The response must retain the exact peer signing identity, selected release root, request hash, nonce, and Unix channel-binding hash.

The production server requires:

- `--socket`: absolute operator-owned Unix socket path;
- `--repository-policy`: owner-only canonical repository identity policy;
- `--execution-policy`: required absolute path to the owner-only, mode-`0600`, RFC-8785-canonical audit execution policy keyed by exact envelope `LaunchSpecHash`;
- `--trust-bundle`: owner-only canonical public trust bundle;
- `--private-key-bundle`: owner-only canonical private signing-key bundle path; private key bytes never enter argv;
- `--journal`: absolute durable server SQLite journal path;
- `--expected-client-uid`: expected local client Unix user ID.
- `--runtime-uid`: required nonzero UID for the untrusted audit child, distinct from the trusted supervisor UID;
- `--runtime-gid`: required nonzero GID for the untrusted audit child.

The execution-policy document has schema version `ananke.local-trusted-supervisor-execution-policy.v5` and a closed `executions` array. Every entry binds its schema version, launch-spec hash, task and repository identities, repository directory identity, pinned Git executable, exact commit/commit-object/tree/archive hashes, fixed prompt template ID/hash, route mapping, pinned route-aware wrapper, exact OMP executable and native-addon identities, immutable atomic runtime authority and bootstrap/framed-wrapper hashes, provider/model/tier, internal deadline, wrapper grace, attempt cap, and policy hash. It also binds every supervisor-owned allowed test by ID, command hash, executable and executable-root identities, closed arguments, and timeout; the runtime-read and executable roots; credential environment names; and distinct prompt, output, session, work, and temporary roots.

The server checks owner-only files, pinned directory/file identity, socket mode and ownership, Darwin peer credentials, canonical bounded frames, authorization/predecessor bindings, execution-policy bindings, and durable replay/audit state. It rechecks the policy, wrapper, Git executable, repository, allowed-test executables, and all pinned roots at effect boundaries. `--timeout` and `--max-frame-bytes` remain optional and default to `2s` and `65536`.

Production namespace admission requires an effective root supervisor and a distinct nonzero runtime UID/GID. Every configured prompt, output, session, temporary, and work parent is opened component-by-component without following symlinks; its complete ancestry must be root-owned, non-writable by the runtime principal, and stable for the server lifetime. On Darwin, every opened physical component must also pass the native descriptor ACL probe: any extended ACL is rejected rather than partially emulating allow/deny order, inheritance, named users, named groups, or `everyone` authority. A Darwin production build without CGO/native ACL inspection fails closed. The supervisor retains CLOEXEC directory descriptors and performs owned-root creation, snapshot staging/extraction/publication/synchronization, capture, verification, and removal relative to those descriptors. A user-owned temporary hierarchy is a test fixture, not a supported production namespace, and production startup rejects it before gateway creation, credential lookup, or child launch.

### Release artifact

Build the exact production server only with the release command. The destination directory must already exist and the destination pathname must be absent:

```sh
env -u GOFLAGS -u GOENV -u GOWORK -u GOTOOLCHAIN -u GOROOT \
  ananke-trusted-supervisor-release build \
  --repository-root /absolute/path/to/ananke \
  --output /Library/Ananke/bin/ananke-trusted-supervisor

ananke-trusted-supervisor-release verify \
  --artifact /Library/Ananke/bin/ananke-trusted-supervisor
```

The builder rejects every caller-supplied `GO*`/`CGO_*` or compiler/tool selector, ignores caller `PATH`, pins the compiled-in absolute Go tool, and uses a closed build environment. Before launching Go on Darwin it registers sticky `EVFILT_VNODE` watches on the retained compiler, repository, and both parent directories; write/delete/extend/metadata/link/rename/revoke activity at any point before publication rejects the build even if the original pathname is restored. Platforms without an equivalent guard fail closed. A random hidden candidate file is created atomically with `Openat(O_CREAT|O_EXCL|O_NOFOLLOW)` directly under the pinned output-directory descriptor; no separately created/opened staging directory is trusted. The builder writes through that retained candidate descriptor, derives build metadata, bytes, hash, and native symbol inventory from it, and publishes relative to the same pinned directory with Darwin `renameatx_np(RENAME_EXCL)`. An existing destination is never replaced. Success requires the published device, inode, size, and hash to remain the verified object; cleanup removes only a hidden candidate name that still identifies the atomically created inode and never removes a caller replacement.

## Invocation

### Server

```sh
ananke-trusted-supervisor \
  --socket /private/operator/trusted-supervisor.sock \
  --repository-policy /private/operator/trusted-supervisor-repositories.json \
  --execution-policy /private/operator/trusted-supervisor-execution.json \
  --trust-bundle /private/operator/trusted-supervisor-trust-bundle.json \
  --private-key-bundle /private/operator/trusted-supervisor-private-key.json \
  --journal /private/operator/trusted-supervisor-journal.sqlite \
  --expected-client-uid 501 \
  --runtime-uid 502 \
  --runtime-gid 502 \
  --timeout 2s \
  --max-frame-bytes 65536
```

The server owns the socket, v4 immutable journal, pinned policy/directory handles, in-memory signing key, and any active audit or supervisor-owned test process group until shutdown. Shutdown stops admission, closes accepted connections, durably resolves requested cancellation where possible, applies bounded TERM-to-KILL termination to owned audit and supervisor-test groups, and requires confirmed exit before cleanup and reap completion. If exit cannot be confirmed, `Close` returns a deadline error and retains the authority needed for a bounded retry. After workers finish, it removes only its own socket inode, then closes and zeroizes resources. A replacement socket is never removed.

An empty, structurally accepted v2 journal migrates normally. A v2 journal with any row in `trusted_supervisor_audit_intents` or `trusted_supervisor_audit_events` is pre-authentication legacy history and is rejected inside the migration transaction before any DDL or schema-version change. Startup reports the populated table name without row contents and includes this remediation: `archive/export the legacy database and start a fresh journal; no in-place signing migration is supported`. Preserve and archive/export the legacy database for operator records, then configure `--journal` with a new owner-only path and start with a fresh journal. Do not edit, rehash, reseal, or attempt to sign legacy rows in place; there is no supported path that promotes them into authenticated current authority.

### Client

The transport binary reads one bounded JSON object from standard input. Unknown or trailing fields are rejected.

Submit an envelope that was already sealed and admitted under the full current fence:

```json
{"operation":"submit","envelope":{...},"fence":{...}}
```

Reconcile a durable handoff:

```json
{"operation":"recover","handoff_id":"remote_handoff_p3f_001"}
```

Transport an already receipt-bound cancellation under the full current fence:

```json
{"operation":"cancel","cancellation":{...},"fence":{...}}
```

Example command shape:

```sh
ananke-trusted-supervisor-transport \
  --store /private/operator/ananke.sqlite \
  --socket /private/operator/trusted-supervisor.sock \
  --trust-bundle /private/operator/trusted-supervisor-trust-bundle.json \
  --peer-uid 501 \
  --peer-pid 4242 \
  --timeout 2s \
  --max-frame-bytes 65536 \
  < invocation.json
```

`--timeout` and `--max-frame-bytes` are optional; their defaults are `2s` and `65536`. The store, socket, and trust-bundle paths and the expected UID/PID remain local process configuration. Do not put endpoints, credentials, raw source, artifact bytes, evidence bytes, commands, argv, or environment data in an invocation or supervisor response.

## Wire, executor, and failure behavior

Each protocol operation still uses one Unix connection and one bounded canonical request/response exchange. The client validates the closed store record and release pins, authenticates Darwin peer credentials, derives the exact channel binding, and revalidates signed receipts/callbacks/acknowledgements before returning them to the store boundary.

After an accepted delivery, the server:

1. atomically stores the signed receipt and immutable audit intent;
2. uses only the matching operator policy entry;
3. verifies the pinned Git executable, repository, commit object, tree, and deterministic `git archive --mtime=1970-01-01T00:00:00Z` hash;
4. rejects traversal, duplicate, link, special-file, PAX, padding, and framing ambiguity before extracting a mode-read-only private snapshot entirely through the retained work-root descriptor;
5. creates non-overlapping prompt, output, session, temporary, and work locations;
6. launches only the exact `omp_with_timeout.sh` policy artifact under Darwin `sandbox-exec`, with closed argv, fixed environment names, explicit `OMP_SESSION_ROOT`, a new process group, and writes denied outside the isolated prompt/output/session/temporary roots;
7. decodes only the closed canonical typed model report: schema version, verdict, bounded summary, and deterministically ordered findings with severity, code, repository-relative path, line, and message;
8. independently runs the policy-pinned supervisor-owned tests under a separate sandbox with closed argv and environment, ignoring model-emitted test markers, and records typed exit/hash/size evidence only for successful commands;
9. constructs canonical read-only evidence schema `ananke.local-trusted-supervisor-readonly-audit-evidence.v5`, whose exact ordered `owned_roots` array records `role`, `path`, `parent_path`, child and parent device/inode, owner UID/GID, mode, and `cleanup_root` for every attempt prompt/output/temporary root, nested wrapper-state/agent/home root, and shared session/work/source-snapshot authority; the array contains identities only, never private file contents;
10. authenticates those evidence bytes and their hash in the signed immutable `finalizing` event, then reopens and removes roots only through the stable namespace authority after exact parent/child identity comparison; and
11. derives and signs `completed` only after the same signed identities prove absent under their trusted parents, preserving the exact finalizing evidence JSON/hash and `FinalizingEventHash`.

Timeout recovery persists the exact `[OMP_TIMEOUT]` session UUID and reuses the same immutable session root and trusted prompt state with only `--resume <uuid>` on the next bounded attempt; a verified synthesis-only recovery hint adds the fixed synthesis-only prompt. It never uses `--continue` or a fresh session. Attempt-cap exhaustion is `waiting_for_human`.

Cancellation first commits a canonical `requested` intent, including exact request, receipt, envelope, nonce, and known PID/PGID/start identity bindings, before any signal or launch decision. It blocks a pending launch, or verifies and terminates the exact owned process group with bounded TERM/KILL and wait/reap. Completion atomically persists the terminal audit event, signed acknowledgement, and cancellation outcome. Exact replay is byte-identical, and an outstanding request resumes after server restart. Identity mismatch, signal failure, unkillable groups, or unverifiable cleanup fails closed as `waiting_for_human`; no cancellation completion is guessed.

Exact terminal replays remain idempotent. Noncanonical JSON, unknown fields, extra bytes, oversized/empty frames, deadline expiry, credential/release/trust/binding drift, output tamper, secret/path leakage, malformed typed or timeout/session evidence, failed supervisor-owned tests, wrong PID identity, or unavailable crash exit status fail closed. A restarted server reconciles a still-running exact PID/start identity as nonterminal; if an orphan later disappears without reapable exit status it records `waiting_for_human`, never guessed success.

Finalizing cleanup is all-or-nothing authority: a missing name under an untrusted parent, a recreated same-path inode, a decoy, an owner/mode change, a parent identity change, an omitted or reordered role, or any path/event mismatch leaves the signed `finalizing` event nonterminal. Recovery does not rerun the provider, wrapper, or supervisor-owned tests and does not hot-loop. Live failure, timeout, and cancellation cleanup uses identities captured by the live invocation and snapshot; legacy path-only recovery is restricted to non-completing failure cleanup and cannot authorize `completed` or a completed callback.

## Operational gate

```sh
go test ./internal/trustedsupervisor ./cmd/ananke-trusted-supervisor -count=1 -timeout=180s
go test ./internal/trustedsupervisor -run '^(TestExecutionPolicy|TestMaterializeAuditSnapshot|TestCanonicalGitArchive|TestDarwinAudit|TestAudit|TestSupervisorOwnedAudit|TestProductionServer|TestServerJournal)' -count=5 -timeout=240s
go test -race ./internal/trustedsupervisor -run '^(TestExecutionPolicy|TestMaterializeAuditSnapshot|TestCanonicalGitArchive|TestDarwinAudit|TestAudit|TestSupervisorOwnedAudit|TestProductionServer|TestServerJournal)' -count=1 -timeout=300s
go test ./... -count=1 -timeout=300s
go test -race ./... -count=1 -timeout=360s
go vet ./...
node contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs --self-test
node contracts/p3f/verify.mjs && node contracts/p3f/verify.mjs --self-test
node contracts/p4/verify.mjs && node contracts/p4/verify.mjs --self-test
```

Most executor tests use dynamically created fake route-aware `omp_with_timeout.sh` wrappers. They run real subprocesses and the real Darwin sandbox to prove argv/environment closure, immutable snapshot behavior, policy/repository/wrapper TOCTOU rejection, typed model/evidence validation, supervisor-owned pinned tests, stable exact-session resume, attempt cap, durable cancellation/reaping/restart, journal integrity, wrong-PID handling, evidence bounds/tamper/leak rejection, and callback behavior. Fake-wrapper strings are absent from the production server binary.

Separately, `TestAuditInstalledOMPProviderFreeTransportPreflight` is a provider-free installed OMP v17.1.3 preflight under the real Darwin subprocess and sandbox path. It uses only a fixed fake credential and a local deterministic rejection. It is not a real model or provider API canary; no real model or provider API canary has occurred. No repair execution, source write, or Run creation has occurred. The foundation remains unaccepted until the current source passes an independent hard re-review.
