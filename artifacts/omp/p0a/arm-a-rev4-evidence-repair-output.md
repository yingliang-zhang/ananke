Working...
Revision-4 evidence repair complete. **Every specified repair requirement passed.**

### Repairs
1. **Structural public-field proof — PASS**
   - `scripts/check-typescript-privacy.mjs` now checks only generated field declarations, TypeScript wire-map entries, Rust `pub` fields, and Serde renames.
   - It checks both:
     - `generated/typescript/**/*.ts`
     - `generated/rust/renderer_public.rs`
   - Forbidden fields: `token`, raw daemon `error`.
   - Comments such as “throw an error” do not trigger false failures.

2. **Protected production scope guard — PASS**
   - Verifier resolves repository root using `git rev-parse --show-toplevel`.
   - It runs exactly:

     ```sh
     git diff --exit-code -- internal/lifecycle/engine.go gui/src-tauri/src/lib.rs gui/src/main.ts
     ```

   - Final result: exit `0`.

3. **Revision-4 summary and README — PASS**
   - `artifacts/verify/summary.json` now records:
     - revision `4`;
     - raw tool commands/results;
     - generated file/LOC counts;
     - dependency-manifest footprint;
     - structural public-field repair outcome;
     - protected-production guard outcome;
     - fresh timings.
   - `README.md` now documents revision 4 and no longer presents revision 3 as final.

4. **Portable runtime resolution — PASS**
   - Scripts and README contain no user-specific Node/Cargo paths.
   - `node` and `cargo` resolve from `PATH`.
   - Verifier requires Node 22 from `PATH` and emits clear prerequisite errors for unavailable tools.
   - README documents portable invocation:

     ```sh
     node scripts/verify.mjs
     ```

     Prerequisites: Node 22.x, `cargo`, `go`, and `git` on `PATH`.

### Final verification

```sh
node scripts/verify.mjs
```

Result: **PASS**
- Complete verifier: `13.030s`
- Fixture verification wave: `2.396s`
- Generated artifacts: 6 files / 677 LOC
  - Go: 2 / 212
  - Rust: 3 / 209
  - TypeScript: 1 / 256

The final verifier passed fixture behavior, public TypeScript/Rust field-boundary checks, clean regeneration, staged content-drift rejection, optional-field evolution, and the protected production scope guard.

Also passed:

```sh
git diff --check
```

No output; exit `0`.

**Verdict remains: VALIDATED.** No unresolved revision-4 evidence gap observed.
