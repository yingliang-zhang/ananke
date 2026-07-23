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
const grillPrivacyTargets = [
  ["renderer-public-grill-evaluate-input.schema.json", (schema) => schema],
  ["renderer-public-grill-record-default-input.schema.json", (schema) => schema],
  ["renderer-public-grill-record-answer-input.schema.json", (schema) => schema],
  ["renderer-public-grill-record-override-input.schema.json", (schema) => schema],
  ["renderer-public-grill-evaluation.schema.json", (schema) => schema],
  ["renderer-public-grill-evaluation.schema.json", (schema) => schema.properties.shown_questions.items],
  ["renderer-public-grill-default-record.schema.json", (schema) => schema],
  ["renderer-public-grill-answer-record.schema.json", (schema) => schema],
  ["renderer-public-grill-override-record.schema.json", (schema) => schema],
];
const grillPrivateFields = [
  "cmd", "command", "token", "error", "socket", "identity", "worker", "process", "pid", "path", "root",
  "secret", "credential", "password", "model", "prompt", "prose", "approval", "execution", "execute", "runtime",
  "transport", "input_hash", "rule_version", "declarations", "raw",
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

for (const [name, select] of grillPrivacyTargets) {
  const path = resolve(guiDirectory, "contracts", name);
  const original = await readFile(path, "utf8");
  try {
    for (const field of grillPrivateFields) {
      const schema = JSON.parse(original);
      const target = select(schema);
      target.properties[field] = { type: "string" };
      target.required.push(field);
      await writeFile(path, JSON.stringify(schema));
      const result = checkPublicFields();
      assert.notEqual(result.status, 0, `${name} must reject P2c private field ${field}`);
      assert.match(`${result.stdout}${result.stderr}`, new RegExp(`prohibited public field ${field}`));
    }
  } finally {
    await writeFile(path, original);
  }
}

console.log("Renderer-public privacy denylist rejects every prohibited field class and every P1c and P2c DTO target.");
