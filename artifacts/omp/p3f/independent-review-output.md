Working...
## ACCEPT

No edits or commits made.

- **P3d binding:** P3f pins P3d’s complete fixture manifest, canonical fixture digest, HostSpec hash, repository/source-snapshot identity, route pair, deadline, and `retry_process_admission` action. Independent recomputation matched the pinned P3d SHA-256.
- **Canonical source manifest:** Recomputed the JCS-style sorted JSON SHA-256 excluding `source_manifest_hash`; it matches `sha256:842188d5…f5765cc15`. The manifest is tracked, commit-shaped, ordered, opaque, and P3d snapshot-bound.
- **Wrapper identity:** Exactly one approved wrapper binary hash and the exact P3d `{wrapper_kind, route}` pair are required at both declaration and launch-preflight boundaries.
- **FD-only interface:** Source, manifest, and evidence are all fixed to `inherited_fd_only`; each non-FD interface has a fail-closed red flag.
- **Sandbox declaration:** Requires `os_enforced_read_only` source access and `os_enforced_write_denied` writes. Correctly declarative only; no sandbox implementation is claimed.
- **Owned cleanup:** Requires activation-owned descriptors, device/inode binding, and close-owned-descriptors-before-removing-owned-inode behavior.
- **Credentials:** argv and environment credentials are explicitly forbidden; fixture schema blocks raw command, path, credential, environment, token, socket, and related authority fields.
- **Launch-time gates:** Requires `launch_time`, full private-fence authentication—not a public fingerprint—exact P3c action, P3d deadline, source-manifest/snapshot bindings, and wrapper identity. The P3a → P3d → P3f action chain independently resolves to `retry_process_admission`.
- **Red flags:** All 23 cases have the sole normalized output: `waiting_for_human`, empty events, null result, and `not_run`.
- **No real activation:** P3f is read-only fixture verification; static review found no wrapper, sandbox, source open, OMP invocation, network, write, or child-process mechanism. P3a’s self-test only spawns a copied Node verifier, not an activation.

**Verification passed**

```text
node --check contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs
node contracts/p3a/verify.mjs --self-test
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs
node contracts/p3f/verify.mjs --self-test
```

All normal verifiers and self-tests passed.
