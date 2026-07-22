import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import ts from "typescript";

const guiDirectory = resolve(import.meta.dirname, "..");
const fixture = JSON.parse(
  await readFile(resolve(guiDirectory, "contracts/fixtures/renderer-public-golden.json"), "utf8"),
);

async function loadGeneratedModule(name) {
  const source = await readFile(
    resolve(guiDirectory, `src/generated/renderer-public-${name}.ts`),
    "utf8",
  );
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);
}

function decode(converter, method, value) {
  return converter[method](JSON.stringify(value));
}

const [bootstrap, run, event, cancel, health] = await Promise.all([
  loadGeneratedModule("bootstrap"),
  loadGeneratedModule("run"),
  loadGeneratedModule("event"),
  loadGeneratedModule("cancel"),
  loadGeneratedModule("health"),
]);

assert.deepEqual(decode(bootstrap.Convert, "toBootstrap", fixture.bootstrap), fixture.bootstrap);
assert.deepEqual(decode(run.Convert, "toRun", fixture.run), fixture.run);
assert.deepEqual(
  fixture.events.map((entry) => decode(event.Convert, "toEvent", entry)),
  fixture.events,
);
assert.deepEqual(decode(cancel.Convert, "toCancel", fixture.cancel), fixture.cancel);
assert.deepEqual(decode(health.Convert, "toHealth", fixture.health), fixture.health);

assert.throws(() => decode(bootstrap.Convert, "toBootstrap", { ...fixture.bootstrap, project: { ...fixture.bootstrap.project, root: 42 } }));
assert.throws(() => decode(run.Convert, "toRun", { ...fixture.run, diagnostics: { ...fixture.run.diagnostics, worker_pid: "1234" } }));
assert.throws(() => decode(event.Convert, "toEvent", { seq: 1, type: "missing-payload" }));
assert.throws(() => decode(cancel.Convert, "toCancel", { accepted: "true", state: "cancelling" }));
assert.throws(() => decode(health.Convert, "toHealth", { online: "true" }));

console.log("Generated TypeScript decoders accepted public golden JSON and rejected malformed payloads.");
