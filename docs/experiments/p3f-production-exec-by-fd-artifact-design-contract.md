# P3f production exec-by-FD and trusted-wrapper artifact design contract

**Status:** frozen design-only successor contract and fixture oracle.
**Scope:** contract fixtures, verifier, and documentation only. This creates no
production wrapper or artifact, does not open a target/artifact/source/evidence
descriptor, and does not start a child, sandbox, OMP session, staging operation,
commit, or push.

## Decision

On the locally inspected macOS 27 SDK, there is **no admitted native
exec-by-FD image-selection primitive**. The exact P3f Darwin mechanism is
therefore `none_fail_closed`: emit the existing normalized
`waiting_for_human` projection before any child exists. This is an intentional
negative result, not a path-launcher fallback.

The contract nevertheless freezes the boundary a future independently trusted
wrapper must satisfy if a separately accepted platform profile supplies an
atomic kernel image selector for an already-verified FD. That future profile is
not activated by this document, the fixture, or a generic portability branch.
It requires a new accepted contract and implementation proof.

## Local Darwin primitive research

The inspected SDK was
`/Applications/Xcode-beta.app/.../MacOSX27.0.sdk`:

| Local interface | Observed shape | P3f result |
| --- | --- | --- |
| `usr/include/unistd.h:453-458` | `exec*` declarations take a `const char *` file/path argument | Not FD image selection. Never use it for the wrapper. |
| `usr/include/spawn.h:60-70` | `posix_spawn`/`posix_spawnp` take a `const char *` image argument | Not FD image selection. Never use it for the wrapper. |
| `spawn.h:80-85,181-182` | `adddup2` and `addinherit_np` only arrange child FD inheritance | May never be mistaken for selecting the verified wrapper image. |
| `usr/include/sys/fileport.h:47-51` | `fileport_makeport` and `fileport_makefd` transfer an FD | Transport only; it cannot select an image. |
| `usr/include/sys/fcntl.h:266-268,445-449` | signature-loading controls such as `F_ADDFILESIGS` | Signature loading is not image selection and is not provenance acceptance. |

An SDK-wide search found no `fexecve`, `execveat`, or `AT_EMPTY_PATH`
declaration. `/dev/fd/N`, `execve` by pathname, `posix_spawn`,
`posix_spawnp`, fileport transport, a caller-selected launcher, and any
re-exec of a test binary are explicitly rejected. No probe executed an image.

## P3d → P3f → exec-by-FD chain

`production-exec-fd-design-v1.canonical.json` binds the exact SHA-256 bytes of
P3f's existing `production-activation-v1.canonical.json`; P3f already binds
P3d's canonical fixture, HostSpec, source-manifest hash, wrapper hash, wrapper
kind, and route. The verifier consequently validates this chain in one process:

```text
P3d canonical fixture
  → P3f activation fixture
    → P3f exec-by-FD design fixture
      → P3f exec-by-FD denial vectors
```

The only mapped route is:

| P3d wrapper kind | P3d route | Required artifact protocol | Route class |
| --- | --- | --- | --- |
| `ananke_omp_readonly_wrapper_v1` | `ananke_omp_read_only_audit_v1` | `ananke.omp-wrapper-fd.v1` | independently trusted local wrapper |

There is no bare OMP route, alternate wrapper, route fallback, path launcher,
or MoA route selection in this contract.

## Future independently trusted artifact boundary

The P3f binary hash is an identity declaration, **not** acceptance of a
production artifact. `artifact_state` is deliberately
`future_independently_trusted_artifact_required_not_accepted`.

A future implementation may accept an artifact only after all checks below
succeed at the final boundary, using an activation-owned artifact FD:

1. `fstat` proves a regular file and records device/inode identity; its content
   SHA-256 equals the P3f-bound wrapper hash.
2. A detached `ananke.wrapper-release-attestation.v1` binds that SHA-256, the
   exact P3d wrapper-kind/route pair, `ananke.omp-wrapper-fd.v1`, and a build
   identity.
3. A distinct release authority, rooted at
   `ananke_wrapper_release_root_v1`, verifies the statement and a release
   approval. The builder, runtime launcher, wrapper, and test harness are not
   that authority.
4. Revocation, validity, and any transparency inclusion required by the future
   release policy are checked by that independent authority before launch.
5. The same FD is revalidated (regular kind, device/inode, digest, statement,
   and route binding) immediately before the platform-specific FD image
   selector. A caller digest, self-consistent artifact/digest pair, dynamically
   built artifact, or test fixture is rejected.

The release artifact is never an activation-owned temporary object and is never
removed during cleanup.

## Sandbox and descriptor contract

A future supported platform requires OS-enforced—not advisory—containment:

- source access is read-only through inherited FD 3 only;
- manifest access is read-only through inherited FD 4 only;
- evidence access is write-only through inherited FD 5 only;
- the image-selection FD is selector-only and close-on-launch; it is not an
  inherited wrapper input;
- every FD other than fixed 0/1/2 noninteractive endpoints and 3/4/5 closes at
  launch; 0/1/2 are closed or fixed noninteractive sinks/sources, never parent
  stdio authority;
- network is denied, filesystem writes are denied except the activation-owned
  evidence FD, and child creation is denied except the bound wrapper image;
- unsupported or unproven sandbox enforcement returns
  `waiting_for_human` before a child.

The child argument vector is a fixed, non-secret protocol selector only; it
contains no route, artifact location, source location, credential, prompt, or
target. Its environment is empty. Credentials are forbidden in both argv and
environment. The future implementation must reject an unexpected inherited FD,
including a descriptor that merely happens to be readable.

## Transcript and evidence schemas

The design reserves two typed, hash-bound shapes. They are schemas for future
controlled evidence, not records produced now.

`ananke.omp-wrapper-transcript.v1` has exactly a schema version, monotonically
increasing sequence, typed role, event kind, P3f wrapper SHA-256, route,
full-fence hash, and prior-event hash. It contains no raw transcript text,
stdout/stderr, source bytes, file locations, prompt, arguments, environment,
or credentials.

`ananke.omp-wrapper-evidence.v1` has exactly a schema version, P3f wrapper
SHA-256, route pair, detached-attestation SHA-256, source-manifest hash,
full-fence hash, sandbox-profile hash, inherited-FD-set hash, transcript hash,
and typed terminal state. It carries hashes and enums only. Public failure
still projects exactly to P3f's closed normalized output:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

## Cancellation, recovery, and cleanup

A future cancel request first authenticates the complete private fence. It then
terminates only the bound child group, reaps it, and closes parent-owned FDs
after reap. Before a typed terminal evidence record exists, the outcome is
unknown and remains `waiting_for_human`; cancellation must not infer success
from a process handle or missing receipt.

After a crash between launch issue and attested terminal evidence, recovery
reconciles only with an independently authenticated bound-child identity. It
must not infer completion, evidence, or cleanup from a PID, a namespace entry,
or a route label. An unavailable reconciliation backend fails closed.

Cleanup order is fixed: reap, close activation-owned descriptors, then remove
only activation-owned ephemeral objects after device/inode revalidation.
Replacement identities are preserved. Delivery artifacts are never removed.
No staging object is created by this contract.

## Fake execution is not the future wrapper

`internal/lifecycle/omp_production_fake_execution_test.go` is compiled only
into package tests. It verifies a fixed test fixture but re-executes only the
Go test binary; no fixture pathname becomes an image launcher. Its test
artifact is explicitly `test_fixture_non_production`, has no production
authority, and may never satisfy artifact provenance or substitute for the
future wrapper. The production build exclusion test remains the guard against
accidental runtime integration.

## `ananke_hybrid_v1` typed-role boundary

The frozen `ananke.hybrid-v1-typed-role-boundary.v1` is a policy boundary, not
a runtime API or route selector:

| Typed role | P3f capability |
| --- | --- |
| `local_wrapper_executor` | Consume only the fixed FD contract. |
| `moa_route_selector` | Not admitted without a future signed route grant. |
| `moa_provider_delegate` | Not admitted without a future signed route grant. |
| `transcript_evidence_recorder` | Append only typed, hash-bound evidence. |

Role labels grant nothing alone. A route needs both a typed signed grant and the
P3f/P3d route/artifact bindings. Hybrid runtime integration is `absent` and
fallback is `forbidden`; there is no MoA provider call, delegation, persistence,
API, route selection, or fallback implementation.

## Contract artifacts and gate

- `contracts/p3f/fixtures/production-exec-fd-design-v1.canonical.json` — P3f
  chain, Darwin no-mechanism decision, future artifact/FD/sandbox/evidence/
  cleanup/role requirements, and fake-test exclusion.
- `contracts/p3f/fixtures/exec-fd-red-flags-v1.canonical.json` — exact
  `waiting_for_human` denial vectors for unsupported Darwin, image-selection
  fallbacks, provenance, route, sandbox, FD, credentials, transcript/evidence,
  typed-role, cancellation, recovery, and schema drift.
- `contracts/p3f/verify.mjs` — validates canonical bytes, hard digests, the
  P3d→P3f→successor chain, closed shapes, and in-memory mutation rejection.

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

All commands read contract fixture bytes only. They do not launch a wrapper,
OMP, target, sandbox, staging operation, verifier command, or child.

## Explicit non-goals

- production exec-by-FD implementation, wrapper artifact, artifact download,
  signature verification runtime, code-signing control, source/evidence open,
  sandbox, descriptor operation, staging, process, child, OMP, monitor,
  cancellation, recovery, cleanup, or evidence persistence;
- `ananke_hybrid_v1` runtime/schema/API/storage/route-selector/provider/
  delegation implementation or any fallback;
- target/repository access, commit, push, network, socket, UI, daemon,
  renderer, generated code, or lifecycle/store integration.
