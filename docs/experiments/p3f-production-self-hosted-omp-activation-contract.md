# P3f production self-hosted OMP activation contract

**Status:** frozen successor contract and fixture oracle.
**Scope:** a declarative production-activation preflight only. P3f implements no
production wrapper, sandbox, repository/source operation, descriptor open,
credential path, OMP invocation, child process, monitor, persistence, or
runtime API.

## Decision

P3f freezes the only production self-hosted activation shape that may succeed
P3d's route-aware controlled read-only OMP boundary and P3e's fake-only private
proof. The contract admits a future activation only after all declared
identities and capabilities agree. It is intentionally not an executable
configuration and it does not authorize a launch.

The source is represented by a tracked Git commit and a canonical JCS manifest,
not a checkout location or source bytes. The production wrapper is represented
by a single approved SHA-256 and P3d's exact wrapper-kind/route pair, not an
executable path or argv. Source, manifest, and evidence are inherited-FD-only
interfaces. Credentials are forbidden in argv and environment.

## P3d contract-chain binding

P3f pins the exact SHA-256 bytes of
`contracts/p3d/fixtures/omp-audit-v1.canonical.json`, its three-entry manifest,
and these semantic predecessor facts:

| P3d fact | Frozen P3f use |
| --- | --- |
| `host_spec_hash` | Exact successor binding. |
| Repository identity and required source-snapshot hash | Input to the tracked source manifest and launch-time check. |
| Wrapper kind and route | Exact production route pair. No bare OMP or alternate route is admitted. |
| Deadline | Exact launch-time deadline check. |
| `retry_process_admission` | Exact launch-time P3c action check. |

The P3f normal verifier performs this chain check directly against P3d's frozen
fixture bytes. It neither starts P3d's verifier nor treats a fingerprint as a
credential.

## Tracked source manifest

`ananke.tracked-source-manifest.v1` contains only:

- a `tracked: true` 40-hex Git commit;
- the canonical Ananke repository identity;
- P3d's required source-snapshot hash;
- three ordered opaque entry IDs and content SHA-256 values; and
- a `source_manifest_hash` equal to the SHA-256 of its RFC 8785 JCS object
  excluding the self-hash.

The activation and launch-preflight objects repeat that derived hash. The P3d
source snapshot and the P3f source-manifest hash are distinct bindings: P3d's
hash preserves its opaque predecessor declaration, while P3f's hash authenticates
the closed tracked-commit declaration. Neither value supplies source bytes,
paths, a worktree, or permission to open files.

## Production capability declaration

| Area | Frozen declaration | Denied alternative |
| --- | --- | --- |
| Wrapper | `ananke_omp_readonly_wrapper_v1`, `ananke_omp_read_only_audit_v1`, and one approved binary SHA-256 | Bare OMP, another wrapper, another route, mutable path, argv. |
| Manifest/source/evidence | `inherited_fd_only` for all three | Path-based or copied authority. |
| Sandbox | `os_enforced_read_only` source and `os_enforced_write_denied` writes | Advisory/read-only-by-convention policy. |
| Cleanup | Activation-owned descriptors, device/inode binding, close owned descriptors then remove owned inode | Borrowed descriptor, unbound inode, namespace-only cleanup. |
| Credentials | argv and environment credentials are `forbidden` | Any argv or environment credential channel. |

These are required implementation targets, not claims that the targets exist.
No fixture contains a source location, source byte, executable path, argv,
environment value, credential, command, prompt, token, socket, raw error, or
process identifier.

## Launch-time preflight

A future implementation must, immediately before launch, prove all of the
following privately:

1. P3d's exact deadline is still valid.
2. The complete active P3b fence is authenticated privately. A public or opaque
   fence fingerprint is insufficient.
3. P3c still names `retry_process_admission`.
4. The P3d required source-snapshot hash and the derived JCS
   `source_manifest_hash` both match the frozen declaration.
5. The wrapper binary SHA-256 and exact wrapper-kind/route pair match the
   approved declaration.
6. Source, manifest, and evidence remain descriptor-only; sandbox enforcement,
   descriptor/inode identity, and cleanup ownership are all established.

A failed or unknown preflight has exactly one public normalized output:

```json
{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
```

It carries no reason, transcript, result, verification fact, sandbox fact,
credential, descriptor, inode, or process state. P3f's red-flag fixture covers
source-manifest/P3d drift; wrapper/route drift; all non-FD interfaces; advisory
sandbox declarations; cleanup ownership loss; argv/environment credentials;
deadline/fence/P3c/source/wrapper/route launch-time drift; and unknown output
schema.

## Activation gate

**A real child cannot be launched until the sandbox and production wrapper
implementation are both accepted.** Acceptance must include controlled evidence
that the OS actually prevents source writes, the wrapper proves the pinned
binary and route pair, descriptors/inodes are owned and cleaned safely, full
private fence authentication and every launch-time check occur at the final
boundary, and every red flag remains `waiting_for_human`.

Until then P3f is contract-only. It creates no sandbox, wrapper, worktree,
source snapshot, file descriptor, process, OMP session, command, or child.

Real production execution remains blocked pending a separate approved
exec-by-FD design with independently trusted artifact identity. It must not
fall back to a path launcher, caller-provided digest, dynamically built test
artifact, or self-consistent artifact/digest pair.

## Artifacts and gate

- `contracts/p3f/fixtures/production-activation-v1.canonical.json` — closed
  P3d-bound activation declaration.
- `contracts/p3f/fixtures/preflight-red-flags-v1.canonical.json` — complete
  fail-closed preflight-red-flag inventory.
- `contracts/p3f/fixtures/fixtures.sha256` — exact canonical fixture bytes.
- `contracts/p3f/verify.mjs` — dependency-free normal and in-memory self-test
  verifier, including the P3d chain check.

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

The normal P3f gate reads only contract fixture bytes. The self-test reads those
fixtures and mutates cloned values in the same Node process. Neither mode
launches a wrapper, OMP, sandbox, process, or verification command.

## Explicit non-goals

- wrapper, sandbox, source-manifest collector, descriptor implementation,
  credential provider, real OMP activation, process, worktree, source/evidence
  opening, monitoring, cancellation, recovery, verification execution, or
  cleanup implementation;
- daemon, store/lifecycle change, Tauri/UI, renderer/public protocol, generated
  code, socket, network, model, transcript, command, prompt, commit, or push.
