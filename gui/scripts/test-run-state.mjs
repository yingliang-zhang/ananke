import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import ts from "typescript";

const sourcePath = resolve(import.meta.dirname, "../src/run-state.ts");
const source = await readFile(sourcePath, "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const { isActiveRunState } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);

for (const state of ["created", "running", "cancelling", "cleanup_required", "recovery_unknown"]) {
  assert.equal(isActiveRunState(state), true, `${state} must remain active and cancellable`);
}
for (const state of ["completed", "failed", "cancelled"]) {
  assert.equal(isActiveRunState(state), false, `${state} must be settled`);
}
