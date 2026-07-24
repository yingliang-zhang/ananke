# P3f production self-hosted OMP activation contract / fixture TDD plan

**Goal:** freeze the production self-hosted activation preflight that may follow
P3d/P3e, without implementing a production wrapper, opening source, or launching
a child.

**Authorized now:** P3f canonical and preflight-red-flag fixtures, their SHA-256
manifest, a dependency-free verifier with an in-process self-test, this plan,
the P3f contract document, and a factual ledger entry. **Not authorized:**
production wrapper implementation, wrapper configuration, source checkout or
worktree, sandbox creation, OMP invocation, child process, socket, daemon,
Tauri/UI, renderer/runtime API, source/evidence materialization, command,
verification execution, commit, or push.

## Task 1: RED/GREEN — tracked commit and P3d-bound source manifest

**Files:** `contracts/p3f/fixtures/production-activation-v1.canonical.json`,
`contracts/p3f/verify.mjs`.

1. Freeze a tracked 40-hex Git commit and a closed, ordered source manifest.
   Its entry IDs and blob hashes are identifiers only: no source bytes or
   filesystem locations appear.
2. Define `source_manifest_hash` as the SHA-256 of RFC 8785 JCS bytes for the
   source manifest excluding its self-hash. Repeat it at the activation and
   launch-preflight boundaries.
3. Bind the manifest's predecessor source-snapshot hash, repository identity,
   P3d HostSpec hash, and P3d canonical-fixture SHA-256 to P3d's frozen
   canonical fixture. The P3f verifier must read and validate P3d's manifest,
   canonical bytes, route pair, deadline, source snapshot, and P3c action.
4. RED: mutate the tracked flag, commit, entry ID/hash, source-manifest hash,
   P3d host hash, or P3d source-snapshot hash in memory. GREEN: every mutation
   is rejected.

**Acceptance:** a source manifest is a canonical, content-addressed declaration
bound to P3d; it is not permission to read a repository or construct a
worktree.

## Task 2: RED/GREEN — approved production wrapper and FD/sandbox boundary

**Files:** `contracts/p3f/fixtures/production-activation-v1.canonical.json`,
`contracts/p3f/fixtures/preflight-red-flags-v1.canonical.json`,
`contracts/p3f/verify.mjs`.

1. Freeze one approved production wrapper SHA-256 together with P3d's exact
   `wrapper_kind`/`route` pair. A bare OMP executable or any other pair is not
   admitted.
2. Declare only inherited-FD interfaces for manifest, source, and evidence. No
   pathname, argv, environment, raw command, prompt, token, socket, or source
   bytes are permitted in P3f shapes.
3. Declare the necessary OS-enforced sandbox capability: source access is
   `os_enforced_read_only` and writes are `os_enforced_write_denied`. An
   advisory policy is not equivalent.
4. Declare cleanup ownership: activation owns its descriptors, binds device and
   inode identity, and closes owned descriptors before removing only its owned
   inode.
5. RED: inject a wrapper hash/route drift, non-FD interface, advisory sandbox,
   borrowed descriptor, unbound inode, argv credential, or environment
   credential. GREEN: each vector returns the exact normalized
   `waiting_for_human` output.

**Acceptance:** these are capability declarations and denial vectors only. They
do not make an OS sandbox or a wrapper binary.

## Task 3: RED/GREEN — launch-time preflight red flags

**Files:** `contracts/p3f/fixtures/preflight-red-flags-v1.canonical.json`,
`contracts/p3f/verify.mjs`.

1. Require the following exact checks at `launch_time`: P3d deadline, full
   private-fence authentication (not a fingerprint), P3c
   `retry_process_admission`, P3d source snapshot, derived source-manifest
   hash, approved wrapper SHA-256, and exact wrapper-kind/route pair.
2. Freeze one closed normalized-output declaration and the only denial shape:

   ```json
   {"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}
   ```

3. RED: add red flags for each failed launch-time check and an unknown output
   schema. GREEN: each case has exactly the denial shape, no reason text, and
   no inferred event, result, verification, sandbox, descriptor, or process
   fact.

**Acceptance:** all red flags fail closed. They are not launch attempts.

## Task 4: Future implementation gates — document only

A separately authorized production implementation must demonstrate all of the
following before any real child can be launched:

1. an accepted OS-enforced sandbox implementation proves source reads are
   actually read-only and all writes are denied, rather than merely declared;
2. an accepted production wrapper implementation resolves only the frozen
   binary SHA-256 and exact P3d route pair, with no bare OMP or fallback;
3. source, manifest, and evidence enter only through inherited descriptors;
   every descriptor and source inode is revalidated at the launch boundary and
   cleanup closes/removes only owned identities;
4. no credential enters argv or environment, and no raw execution/connection
   authority crosses a public boundary;
5. the runtime authenticates the full P3b fence privately and checks the P3c
   action, deadline, P3d source snapshot, JCS source-manifest hash, wrapper
   SHA-256, and route pair immediately before launch;
6. controlled tests prove the closed normalized output and exact
   `waiting_for_human` failure result for every red flag.

**No real child may be launched until both the sandbox and wrapper
implementation are accepted.** This P3f contract confers no launch authority.

## Contract-only gate

```sh
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

The P3f normal verifier reads P3f fixture bytes and P3d contract bytes only. It
performs the contract-chain check in process; it does not invoke P3d, a wrapper,
OMP, a sandbox, or any verification command. Its self-test mutates only
in-memory fixture values.

## Explicit non-goals

- a real wrapper binary, OMP executable, production launch, process, child,
  sandbox, filesystem mount, worktree, source checkout, source/evidence I/O,
  descriptor open, credential retrieval, or cleanup action;
- P3b/P3c store or lifecycle changes, durable result/evidence state,
  cancellation/recovery implementation, daemon, Tauri/UI, renderer DTO,
  network transport, model call, command, prompt, transcript, or verification
  execution;
- commit or push.
