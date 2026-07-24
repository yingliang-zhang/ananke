# P3f production exec-by-FD / trusted-wrapper artifact design-contract plan

**Goal:** turn P3f's former exec-by-FD blocker into a precise Darwin design
contract without creating production launch authority.

**Authorized:** canonical fixtures, denial vectors, a dependency-free verifier,
contract documents, and ledger evidence. **Not authorized:** production wrapper
or artifact creation, target/source/staging access, descriptor opening, process
launch, sandbox application, OMP, runtime integration, commit, or push.

## Task 1: local primitive decision

1. Inspect the local macOS SDK declarations for `exec*`, `posix_spawn`, FD
   inheritance, fileport, and signature controls.
2. Reject any mechanism whose image selector is a pathname or whose capability
   is FD transport/inheritance rather than atomic image selection.
3. Freeze the Darwin macOS 27 mechanism as `none_fail_closed` because the SDK
   has no `fexecve`, `execveat`, or `AT_EMPTY_PATH` declaration.

**Acceptance:** no `/dev/fd`, `execve`, `posix_spawn`, fileport, or test-binary
workaround is portrayed as production exec-by-FD; unsupported Darwin returns
`waiting_for_human` before a child.

## Task 2: chain and trusted artifact boundary

1. Add an exec-by-FD canonical fixture under P3f and bind it to P3f's exact
   activation fixture digest, source-manifest hash, wrapper hash, kind, and
   P3d route.
2. Require a future detached, independently released attestation and release
   approval bound to artifact SHA-256, route pair, artifact protocol, and build
   identity.
3. Reject caller digests, self-consistent artifact/digest pairs, dynamic builds,
   and test fixtures. Do not declare an actual wrapper accepted.

**Acceptance:** P3d → P3f activation → successor fixture is verified from
canonical bytes; the frozen P3f wrapper hash is identity only, not artifact
provenance.

## Task 3: future FD, sandbox, evidence, and hybrid boundary

1. Freeze source/manifest/evidence FD 3/4/5 inheritance, selector-only wrapper
   FD, close-on-launch handling for all other descriptors, empty credential-free
   environment, and fixed non-secret argument protocol.
2. Require OS-enforced FD-only source/evidence policy, network denial, controlled
   child creation, owned descriptor cleanup, replacement preservation, and no
   release-artifact deletion.
3. Define typed transcript/evidence schemas, cancellation/recovery no-inference
   policy, and `ananke_hybrid_v1` typed roles with no runtime integration,
   fallback, or MoA route admission.

**Acceptance:** fixtures encode the full declarative policy; no runtime API,
launcher, executor, or persistence changes occur.

## Task 4: denial oracle and verification

1. Add exact normalized `waiting_for_human` vectors for unsupported platform,
   path/FD-indirection/fileport launchers, trust/route/sandbox/FD/credential
   drift, raw transcript, hybrid integration, cancellation/recovery inference,
   and unknown schemas.
2. Extend P3f's verifier to authenticate P3d, the existing P3f activation
   fixture, and both successor fixtures. Its self-test must mutate every new
   policy class in memory and reject it.
3. Run syntax, normal, and self-test chain gates only.

**Acceptance:** the normal gate reads fixture bytes only, while the self-test
mutates copies in memory. Neither invokes a contract-defined process.

## Non-goals

No production execution, real wrapper/OMP/target/staging, file descriptor or
sandbox action, artifact materialization, cancellation/recovery runtime,
`ananke_hybrid_v1` runtime integration, commit, or push.
