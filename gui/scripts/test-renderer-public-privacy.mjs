import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const guiDirectory = resolve(import.meta.dirname, "..");
const generatorPath = resolve(guiDirectory, "scripts/generate-renderer-public.mjs");
const schemaPath = resolve(guiDirectory, "contracts/renderer-public-run.schema.json");
const prohibitedFields = [
  "token",
  "error",
  "worker_env",
  "socket_path",
  "identity_file",
  "adapter_secret",
];

const proposalPrivacyTargets = [
  ["renderer-public-proposal-create-input.schema.json", (schema) => schema],
  ["renderer-public-proposal-list-input.schema.json", (schema) => schema],
  ["renderer-public-proposal-get-input.schema.json", (schema) => schema],
  ["renderer-public-proposal-activity-list-input.schema.json", (schema) => schema],
  ["renderer-public-proposal-append-input.schema.json", (schema) => schema],
  ["renderer-public-proposal-decision-input.schema.json", (schema) => schema],
  ["renderer-public-proposal-withdraw-input.schema.json", (schema) => schema],
  ["renderer-public-proposal-mutation.schema.json", (schema) => schema],
  ["renderer-public-proposal-list.schema.json", (schema) => schema],
  ["renderer-public-proposal-detail.schema.json", (schema) => schema],
  ["renderer-public-proposal-activity-list.schema.json", (schema) => schema],
  ["renderer-public-proposal-activity-list.schema.json", (schema) => schema.properties.activity.items],
];
function checkPublicFields() {
  return spawnSync(process.execPath, [generatorPath, "--check-public-fields"], {
    cwd: guiDirectory,
    encoding: "utf8",
  });
}

const baseline = checkPublicFields();
assert.equal(
  baseline.status,
  0,
  `current generated public models must pass the privacy check:\n${baseline.stdout}${baseline.stderr}`,
);

const originalSchema = await readFile(schemaPath, "utf8");
try {
  for (const field of prohibitedFields) {
    const mutatedSchema = originalSchema.replace(
      '"properties": {',
      `"properties": {\n    "${field}": { "type": "string" },`,
    );
    await writeFile(schemaPath, mutatedSchema);
    const result = checkPublicFields();
    assert.notEqual(result.status, 0, `${field} must be rejected from the public schema`);
    assert.match(
      `${result.stdout}${result.stderr}`,
      new RegExp(`prohibited public field ${field}`),
    );
  }
} finally {
  await writeFile(schemaPath, originalSchema);
}

for (const [name, select] of proposalPrivacyTargets) {
  const path = resolve(guiDirectory, "contracts", name);
  const original = await readFile(path, "utf8");
  try {
    const schema = JSON.parse(original);
    select(schema).properties.token = { type: "string" };
    await writeFile(path, JSON.stringify(schema));
    const result = checkPublicFields();
    assert.notEqual(result.status, 0, `${name} must reject a private field`);
    assert.match(`${result.stdout}${result.stderr}`, /prohibited public field token/);
  } finally {
    await writeFile(path, original);
  }
}

console.log("Renderer-public privacy denylist rejects every prohibited field class and every P1c DTO target.");
