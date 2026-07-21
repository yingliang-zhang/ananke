Working...
Both final evidence repairs passed.

1. **Exact Go/Git version evidence**
   - Verifier now runs and records:
     ```text
     go version
     go version go1.26.5 darwin/arm64

     git --version
     git version 2.54.0 (Apple Git-157)
     ```
   - Logs:
     - `artifacts/verify/go-version.log`
     - `artifacts/verify/git-version.log`
   - `artifacts/verify/summary.json` now includes both commands, timings, and `tool_versions.go` / `tool_versions.git`.

2. **Portable generation command record**
   - `scripts/generate.mjs` now writes exactly:
     ```text
     node node_modules/.bin/quicktype
     ```
   - Confirmed in `artifacts/generation-command.txt`.
   - No resolved executable path is recorded.

Verification:

```sh
node scripts/verify.mjs
```

Result: **PASS** (`11.800s`), including Go/Git version steps.

```sh
git diff --check
```

Result: pass; no output.
