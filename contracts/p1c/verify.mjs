import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const contractDirectory = dirname(scriptPath);
const projectDirectory = resolve(contractDirectory, "..", "..");
const sourceFixtureDirectory = resolve(contractDirectory, "fixtures");
const sourceSchemaDirectory = resolve(projectDirectory, "gui", "contracts");
const fixtureName = "protocol-v1.canonical.json";
const hashVersion = "ananke-proposal-public-protocol-v1";
const canonicalFixtureDigest = "31a7f02ee79bf5bee66c546433a358bf3d25850e8ba8e9d017d32183d6c489ad";
const p1aRootRevisionHash = "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263";
const enforceCanonicalFixtureDigest = process.env.ANANKE_P1C_SELF_TEST_ALLOW_FIXTURE_DRIFT !== "1";
const localActor = "local_gui_operator";
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const idempotencyKeyPattern = /^[a-z][a-z0-9_]{2,127}$/;
const hashPattern = /^sha256:[0-9a-f]{64}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;

const schemaDocuments = [
  {
    key: "createInput",
    file: "renderer-public-proposal-create-input.schema.json",
    title: "CreateProposalInput",
    digest: "f595c6312a3fb6d56f8326e7caac1bafc6a2147c659802a64e98a4e6493c76c9",
  },
  {
    key: "listInput",
    file: "renderer-public-proposal-list-input.schema.json",
    title: "ListProposalsInput",
    digest: "f28264af5a45312a4a2dcadf48b3e3ea7a34b912ef4cfff326741a7c2445f23b",
  },
  {
    key: "getInput",
    file: "renderer-public-proposal-get-input.schema.json",
    title: "GetProposalInput",
    digest: "43403f73831b89fa30ba5cb4e714917ce83b4132aa6ace40f9352eaee7bac59a",
  },
  {
    key: "activityInput",
    file: "renderer-public-proposal-activity-list-input.schema.json",
    title: "ListProposalActivityInput",
    digest: "c105d3d5b590dd373bba69db4c7a449380ca84ef7fb1d13090ba2d27d6be1659",
  },
  {
    key: "appendInput",
    file: "renderer-public-proposal-append-input.schema.json",
    title: "AppendProposalRevisionInput",
    digest: "4df45bd1db5223a303a778020135b171e337f977e25f35a18532759c99f2ab14",
  },
  {
    key: "decisionInput",
    file: "renderer-public-proposal-decision-input.schema.json",
    title: "DecideProposalApprovalInput",
    digest: "eacf45c740c21621342c95b5e8f8350ec1186dd8c5435560f25bad26032a374e",
  },
  {
    key: "withdrawInput",
    file: "renderer-public-proposal-withdraw-input.schema.json",
    title: "WithdrawProposalInput",
    digest: "d3677295f5bfcf1c022470b216e4845fa132570e4157bf188fbfb14786e99f89",
  },
  {
    key: "mutation",
    file: "renderer-public-proposal-mutation.schema.json",
    title: "ProposalMutation",
    digest: "f5a63ed7667e13697e7d5bc3b11e31e0814c6ffb21822eb9520e7f094d2d85de",
  },
  {
    key: "list",
    file: "renderer-public-proposal-list.schema.json",
    title: "ProposalList",
    digest: "5adcb3f6b04f926e4bcf6cbea81bcedb102b6e8eed3eb04c2868dfc67a85c5cb",
  },
  {
    key: "detail",
    file: "renderer-public-proposal-detail.schema.json",
    title: "ProposalDetail",
    digest: "7000d857eb7581d41ec2896ae518d8bfc68698a1e4768ead65ca56d64dd69a01",
  },
  {
    key: "activityList",
    file: "renderer-public-proposal-activity-list.schema.json",
    title: "ProposalActivityList",
    digest: "e28daf27e34179beaeb2c1c1081662b78aa5f7668750cfe59074e59d5695cc9f",
  },
];

// P1c has eleven committed JSON Schema documents. ProposalActivity is a closed
// embedded schema target in ProposalActivityList, making twelve validated DTO
// schema targets in total.
const schemaTargets = [
  { name: "CreateProposalInput", document: "createInput", select: (schema) => schema },
  { name: "ListProposalsInput", document: "listInput", select: (schema) => schema },
  { name: "GetProposalInput", document: "getInput", select: (schema) => schema },
  { name: "ListProposalActivityInput", document: "activityInput", select: (schema) => schema },
  { name: "AppendProposalRevisionInput", document: "appendInput", select: (schema) => schema },
  { name: "DecideProposalApprovalInput", document: "decisionInput", select: (schema) => schema },
  { name: "WithdrawProposalInput", document: "withdrawInput", select: (schema) => schema },
  { name: "ProposalMutation", document: "mutation", select: (schema) => schema },
  { name: "ProposalList", document: "list", select: (schema) => schema },
  { name: "ProposalDetail", document: "detail", select: (schema) => schema },
  { name: "ProposalActivityList", document: "activityList", select: (schema) => schema },
  {
    name: "ProposalActivity",
    document: "activityList",
    select: (schema) => schema.properties.activity.items,
  },
];

const commandSpecs = [
  { name: "create_proposal", daemon: "create-proposal", input: "createInput", result: "mutation" },
  { name: "list_proposals", daemon: "list-proposals", input: "listInput", result: "list" },
  { name: "get_proposal", daemon: "get-proposal", input: "getInput", result: "detail" },
  {
    name: "list_proposal_activity",
    daemon: "list-proposal-activity",
    input: "activityInput",
    result: "activityList",
  },
  {
    name: "append_proposal_revision",
    daemon: "append-proposal-revision",
    input: "appendInput",
    result: "mutation",
  },
  {
    name: "decide_proposal_approval",
    daemon: "decide-proposal-approval",
    input: "decisionInput",
    result: "mutation",
  },
  {
    name: "withdraw_proposal",
    daemon: "withdraw-proposal",
    input: "withdrawInput",
    result: "mutation",
  },
];

const tauriDaemonMappings = [
  { tauri: "create_proposal", daemon: "create-proposal" },
  { tauri: "list_proposals", daemon: "list-proposals" },
  { tauri: "get_proposal", daemon: "get-proposal" },
  { tauri: "list_proposal_activity", daemon: "list-proposal-activity" },
  { tauri: "append_proposal_revision", daemon: "append-proposal-revision" },
  { tauri: "decide_proposal_approval", daemon: "decide-proposal-approval" },
  { tauri: "withdraw_proposal", daemon: "withdraw-proposal" },
];

function assertCommandMappings(specifications) {
  assert.deepEqual(
    specifications.map(({ name, daemon }) => ({ tauri: name, daemon })),
    tauriDaemonMappings,
    "Tauri-to-daemon command mapping data",
  );
  for (const { name, daemon } of specifications) {
    assert.equal(daemon, name.replaceAll("_", "-"), `Tauri-to-daemon command mapping for ${name}`);
  }
}

function optionValue(name) {
  const index = process.argv.indexOf(name);
  if (index === -1) return undefined;
  assert.ok(process.argv[index + 1], `${name} requires a value`);
  return process.argv[index + 1];
}

function expectObject(value, name) {
  assert.ok(value !== null && typeof value === "object" && !Array.isArray(value), `${name} must be an object`);
}

function expectKeys(value, expected, name) {
  expectObject(value, name);
  assert.deepEqual(Object.keys(value).sort(), [...expected].sort(), `${name} fields`);
}

function assertNoUnpairedSurrogates(value, path = "$") {
  if (typeof value === "string") {
    for (let index = 0; index < value.length; index += 1) {
      const codeUnit = value.charCodeAt(index);
      if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
        const next = value.charCodeAt(index + 1);
        assert.ok(next >= 0xdc00 && next <= 0xdfff, `unpaired Unicode surrogate at ${path}[${index}]`);
        index += 1;
      } else {
        assert.ok(codeUnit < 0xdc00 || codeUnit > 0xdfff, `unpaired Unicode surrogate at ${path}[${index}]`);
      }
    }
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoUnpairedSurrogates(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    assertNoUnpairedSurrogates(key, `${path}.{key}`);
    assertNoUnpairedSurrogates(entry, `${path}.${key}`);
  }
}

function canonicalJson(value) {
  if (value === null) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "string") return JSON.stringify(value);
  if (typeof value === "number") {
    assert.ok(Number.isFinite(value), "canonical JSON forbids non-finite numbers");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  assert.equal(typeof value, "object", `unsupported canonical JSON value: ${typeof value}`);
  assert.equal(Object.getPrototypeOf(value), Object.prototype, "canonical JSON requires plain objects");
  return `{${Object.keys(value)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${canonicalJson(value[key])}`)
    .join(",")}}`;
}

function digest(value) {
  return createHash("sha256").update(value).digest("hex");
}

function hashCanonical(value) {
  return `sha256:${digest(Buffer.from(canonicalJson(value), "utf8"))}`;
}

function assertIdentifier(value, name) {
  assert.ok(typeof value === "string" && identifierPattern.test(value), `${name} must be an identifier`);
}

function assertIdempotencyKey(value, name) {
  assert.ok(typeof value === "string" && idempotencyKeyPattern.test(value), `${name} must be an idempotency key`);
}

function assertHash(value, name) {
  assert.ok(typeof value === "string" && hashPattern.test(value), `${name} must be a SHA-256 hash`);
}

function assertTimestamp(value, name) {
  const message = `${name} must be a semantic UTC RFC 3339/RFC3339Nano timestamp`;
  assert.ok(typeof value === "string", message);
  const match = timestampPattern.exec(value);
  assert.ok(match, message);
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const second = Number(match[6]);
  assert.ok(month >= 1 && month <= 12, message);
  const daysInMonth = month === 2
    ? (year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28)
    : (month === 4 || month === 6 || month === 9 || month === 11 ? 30 : 31);
  assert.ok(day >= 1 && day <= daysInMonth, message);
  assert.ok(hour <= 23 && minute <= 59 && second <= 59, message);
}

function assertPositiveInteger(value, name) {
  assert.ok(Number.isInteger(value) && value > 0, `${name} must be a positive integer`);
}

function assertText(value, maximumBytes, name) {
  assert.ok(typeof value === "string", `${name} must be text`);
  const bytes = Buffer.byteLength(value, "utf8");
  assert.ok(bytes >= 1 && bytes <= maximumBytes, `${name} byte length`);
}

function isAllowedPolicyField(key, parentPath) {
  return parentPath.endsWith(".policy") && (key === "adapter" || key === "model_role");
}

function isPrivateFieldName(key, parentPath) {
  if (isAllowedPolicyField(key, parentPath)) return false;
  const normalized = key.toLowerCase().replace(/[^a-z0-9]/g, "");
  return [
    "path",
    "root",
    "directory",
    "socket",
    "identity",
    "worker",
    "process",
    "pid",
    "token",
    "credential",
    "password",
    "secret",
    "authorization",
    "cookie",
    "model",
    "adapter",
    "execution",
    "execute",
    "runtime",
    "environment",
    "rawerror",
    "error",
    "prompt",
    "completion",
    "provider",
    "auditoutput",
  ].some((fragment) => normalized.includes(fragment));
}

function assertPublicFieldName(key, parentPath, kind) {
  assert.ok(!isPrivateFieldName(key, parentPath), `private ${kind} field ${parentPath}.${key} is not allowlisted`);
}

function assertNoPrivateFields(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoPrivateFields(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    assertPublicFieldName(key, path, "fixture");
    assertNoPrivateFields(entry, `${path}.${key}`);
  }
}

function assertNoPrivateSchemaFields(schema, path) {
  if (schema === null || typeof schema !== "object") return;
  if (schema.properties !== undefined) {
    expectObject(schema.properties, `${path}.properties`);
    for (const [field, property] of Object.entries(schema.properties)) {
      assertPublicFieldName(field, path, "schema");
      assertNoPrivateSchemaFields(property, `${path}.${field}`);
    }
  }
  if (schema.items !== undefined) assertNoPrivateSchemaFields(schema.items, `${path}[]`);
  if (schema.prefixItems !== undefined) {
    schema.prefixItems.forEach((item, index) => assertNoPrivateSchemaFields(item, `${path}[${index}]`));
  }
}

function typeMatches(value, type) {
  if (type === "null") return value === null;
  if (type === "array") return Array.isArray(value);
  if (type === "object") return value !== null && typeof value === "object" && !Array.isArray(value);
  if (type === "string") return typeof value === "string";
  if (type === "integer") return Number.isInteger(value);
  if (type === "number") return typeof value === "number" && Number.isFinite(value);
  if (type === "boolean") return typeof value === "boolean";
  throw new Error(`unsupported JSON Schema type ${type}`);
}

function schemaMatches(value, schema, path) {
  try {
    validateSchema(value, schema, path);
    return true;
  } catch {
    return false;
  }
}

function validateSchema(value, schema, path) {
  if (schema.const !== undefined) assert.deepEqual(value, schema.const, `${path} const`);
  if (schema.enum !== undefined) {
    assert.ok(schema.enum.some((candidate) => Object.is(candidate, value)), `${path} enum`);
  }
  if (schema.type !== undefined) {
    const allowed = Array.isArray(schema.type) ? schema.type : [schema.type];
    assert.ok(allowed.some((type) => typeMatches(value, type)), `${path} type`);
  }
  if (typeof value === "string") {
    if (schema.pattern !== undefined) assert.match(value, new RegExp(schema.pattern), `${path} pattern`);
    if (schema.minLength !== undefined) assert.ok(value.length >= schema.minLength, `${path} minimum length`);
    if (schema.maxLength !== undefined) assert.ok(value.length <= schema.maxLength, `${path} maximum length`);
    if (schema["x-ananke-utc-timestamp"] === true) assertTimestamp(value, path);
    if (schema["x-ananke-max-utf8-bytes"] !== undefined) {
      assert.ok(
        Buffer.byteLength(value, "utf8") <= schema["x-ananke-max-utf8-bytes"],
        `${path} UTF-8 byte length`,
      );
    }
  }
  if (typeof value === "number" && schema.minimum !== undefined) {
    assert.ok(value >= schema.minimum, `${path} minimum`);
  }
  if (Array.isArray(value)) {
    if (schema.minItems !== undefined) assert.ok(value.length >= schema.minItems, `${path} minimum item count`);
    if (schema.maxItems !== undefined) assert.ok(value.length <= schema.maxItems, `${path} maximum item count`);
    if (schema.prefixItems !== undefined) {
      schema.prefixItems.forEach((itemSchema, index) => {
        if (index < value.length) validateSchema(value[index], itemSchema, `${path}[${index}]`);
      });
    }
    if (schema.items !== undefined) {
      value.forEach((item, index) => validateSchema(item, schema.items, `${path}[${index}]`));
    }
  }
  if (value !== null && typeof value === "object" && !Array.isArray(value) && schema.properties !== undefined) {
    const properties = schema.properties;
    for (const required of schema.required ?? []) {
      assert.ok(Object.hasOwn(value, required), `${path} missing required property ${required}`);
    }
    if (schema.additionalProperties === false) {
      for (const key of Object.keys(value)) {
        assert.ok(Object.hasOwn(properties, key), `${path}.${key} has unexpected property`);
      }
    }
    for (const [key, propertySchema] of Object.entries(properties)) {
      if (Object.hasOwn(value, key)) validateSchema(value[key], propertySchema, `${path}.${key}`);
    }
  }
  if (schema["x-ananke-revision-parent"] === true) {
    expectObject(value, path);
    if (value.revision === 1) {
      assert.equal(value.parent_revision, null, `${path} root revision parents must be null`);
      assert.equal(value.parent_revision_hash, null, `${path} root revision parents must be null`);
    } else {
      assert.equal(value.parent_revision, value.revision - 1, `${path} immediate parent revision`);
      assertHash(value.parent_revision_hash, `${path} immediate parent revision hash`);
    }
  }
  if (schema["x-ananke-approval-decision"] === true) {
    expectObject(value, path);
    if (["approved", "rejected"].includes(value.state)) {
      assertTimestamp(value.decided_at, `${path} terminal approval timestamp`);
      assert.equal(value.decided_by, localActor, `${path} terminal approval actor`);
      assertIdempotencyKey(value.decision_idempotency_key, `${path} terminal approval idempotency key`);
      assertText(value.reason, 1000, `${path} terminal approval reason`);
    } else {
      assert.equal(value.decided_at, null, `${path} non-terminal approval decision fields must be null`);
      assert.equal(value.decided_by, null, `${path} non-terminal approval decision fields must be null`);
      assert.equal(value.decision_idempotency_key, null, `${path} non-terminal approval decision fields must be null`);
      assert.equal(value.reason, null, `${path} non-terminal approval decision fields must be null`);
    }
  }
  if (schema.allOf !== undefined) {
    for (const branch of schema.allOf) {
      if (branch.if === undefined) {
        validateSchema(value, branch, path);
      } else if (schemaMatches(value, branch.if, `${path} condition`)) {
        if (branch.then !== undefined) validateSchema(value, branch.then, path);
      } else if (branch.else !== undefined) {
        validateSchema(value, branch.else, path);
      }
    }
  }
}

const supportedSchemaKeywords = new Set([
  "$schema",
  "$id",
  "title",
  "description",
  "type",
  "additionalProperties",
  "required",
  "properties",
  "pattern",
  "minLength",
  "maxLength",
  "minimum",
  "minItems",
  "maxItems",
  "items",
  "prefixItems",
  "const",
  "enum",
  "allOf",
  "if",
  "then",
  "else",
  "x-ananke-max-utf8-bytes",
  "x-ananke-approval-decision",
  "x-ananke-revision-parent",
  "x-ananke-utc-timestamp",
]);

function assertClosedSchemaNode(schema, path) {
  expectObject(schema, path);
  for (const key of Object.keys(schema)) {
    assert.ok(supportedSchemaKeywords.has(key), `${path} uses unsupported schema keyword ${key}`);
  }
  if (schema["x-ananke-max-utf8-bytes"] !== undefined) {
    assert.ok(
      Number.isInteger(schema["x-ananke-max-utf8-bytes"]) && schema["x-ananke-max-utf8-bytes"] > 0,
      `${path} UTF-8 byte limit`,
    );
  }
  if (schema["x-ananke-utc-timestamp"] !== undefined) {
    assert.equal(schema["x-ananke-utc-timestamp"], true, `${path} UTC timestamp marker`);
  }
  if (schema["x-ananke-revision-parent"] !== undefined) {
    assert.equal(schema["x-ananke-revision-parent"], true, `${path} revision parent marker`);
  }
  if (schema["x-ananke-approval-decision"] !== undefined) {
    assert.equal(schema["x-ananke-approval-decision"], true, `${path} approval decision marker`);
  }
  if (schema.properties !== undefined) {
    expectObject(schema.properties, `${path}.properties`);
    for (const [field, property] of Object.entries(schema.properties)) {
      assertClosedSchemaNode(property, `${path}.${field}`);
    }
  }
  if (schema.type !== undefined) {
    const types = Array.isArray(schema.type) ? schema.type : [schema.type];
    types.forEach((type) => assert.ok(["null", "array", "object", "string", "integer", "number", "boolean"].includes(type), `${path} schema type`));
    if (types.includes("object")) {
      expectObject(schema.properties, `${path}.properties`);
      assert.equal(schema.additionalProperties, false, `${path} must close object properties`);
      assert.ok(Array.isArray(schema.required), `${path}.required must be an array`);
      assert.deepEqual([...schema.required].sort(), Object.keys(schema.properties).sort(), `${path} required properties`);
    }
  }
  if (schema.items !== undefined) assertClosedSchemaNode(schema.items, `${path}[]`);
  if (schema.prefixItems !== undefined) {
    assert.ok(Array.isArray(schema.prefixItems), `${path}.prefixItems must be an array`);
    schema.prefixItems.forEach((item, index) => assertClosedSchemaNode(item, `${path}[${index}]`));
  }
  for (const [keyword, node] of [["allOf", schema.allOf], ["if", schema.if], ["then", schema.then], ["else", schema.else]]) {
    if (node === undefined) continue;
    if (keyword === "allOf") {
      assert.ok(Array.isArray(node), `${path}.allOf must be an array`);
      node.forEach((branch, index) => assertClosedSchemaNode(branch, `${path}.allOf[${index}]`));
    } else {
      assertClosedSchemaNode(node, `${path}.${keyword}`);
    }
  }
}

function verifySchemaDocument(schema, specification) {
  const path = specification.file;
  expectKeys(
    schema,
    ["$schema", "$id", "additionalProperties", "description", "properties", "required", "title", "type"],
    path,
  );
  assert.equal(schema.$schema, "https://json-schema.org/draft/2020-12/schema", `${path} draft`);
  assert.equal(schema.$id, `https://ananke.local/contracts/${specification.file}`, `${path} identifier`);
  assert.equal(schema.title, specification.title, `${path} title`);
  assertClosedSchemaNode(schema, specification.file);
  assertNoPrivateSchemaFields(schema, specification.file);
  assert.equal(digest(Buffer.from(canonicalJson(schema), "utf8")), specification.digest, `${path} contract digest`);
}

async function readSchemas(directory) {
  const entries = await Promise.all(schemaDocuments.map(async (specification) => {
    const bytes = await readFile(join(directory, specification.file));
    assert.ok(!bytes.subarray(0, 3).equals(Buffer.from([0xef, 0xbb, 0xbf])), `${specification.file} has a UTF-8 BOM`);
    const text = bytes.toString("utf8");
    assert.ok(Buffer.from(text, "utf8").equals(bytes), `${specification.file} is not UTF-8`);
    const schema = JSON.parse(text);
    assertNoUnpairedSurrogates(schema, specification.file);
    verifySchemaDocument(schema, specification);
    return [specification.key, schema];
  }));
  const schemas = new Map(entries);
  assert.equal(schemaTargets.length, 12, "P1c public DTO schema target inventory");
  for (const target of schemaTargets) {
    const selected = target.select(schemas.get(target.document));
    expectObject(selected, `${target.name} schema target`);
    assertClosedSchemaNode(selected, `${target.name} schema target`);
  }
  return schemas;
}

async function readManifest(directory) {
  const text = await readFile(join(directory, "fixtures.sha256"), "utf8");
  assert.ok(!text.endsWith("\n"), "fixtures.sha256 must not end with a newline");
  const match = text.match(/^([a-z0-9-]+) sha256 ([0-9a-f]{64}) ([a-z0-9.-]+)$/);
  assert.ok(match, `invalid hash manifest: ${text}`);
  assert.equal(match[1], hashVersion, "fixture hash version");
  assert.equal(match[3], fixtureName, "fixture hash manifest inventory");
  return match[2];
}

async function readCanonicalFixture(directory, manifestDigest) {
  const bytes = await readFile(join(directory, fixtureName));
  assert.equal(digest(bytes), manifestDigest, "fixture digest mismatch");
  if (enforceCanonicalFixtureDigest) {
    assert.equal(digest(bytes), canonicalFixtureDigest, "canonical fixture digest mismatch");
  }
  assert.ok(!bytes.subarray(0, 3).equals(Buffer.from([0xef, 0xbb, 0xbf])), `${fixtureName} has a UTF-8 BOM`);
  const text = bytes.toString("utf8");
  assert.ok(Buffer.from(text, "utf8").equals(bytes), `${fixtureName} is not UTF-8`);
  const value = JSON.parse(text);
  assertNoUnpairedSurrogates(value);
  assert.equal(text, canonicalJson(value), `${fixtureName} is not canonical JCS bytes`);
  return value;
}

function assertPolicy(value, name) {
  expectKeys(value, ["adapter", "authority", "budget", "model_role"], name);
  expectKeys(value.adapter, ["access", "kind", "status"], `${name}.adapter`);
  expectKeys(value.budget, ["dimensions", "status"], `${name}.budget`);
  assert.deepEqual(value, {
    adapter: { access: "read_only", kind: "omp_audit", status: "future" },
    authority: "deterministic",
    budget: { dimensions: ["deadline", "attempt_cap"], status: "future" },
    model_role: "advisory_only",
  }, `${name} fixed P1a policy`);
}

function assertRevisionInput(value, name) {
  expectKeys(value, ["acceptance_criteria", "policy", "task"], name);
  assert.ok(Array.isArray(value.acceptance_criteria), `${name}.acceptance_criteria must be an array`);
  assert.ok(value.acceptance_criteria.length >= 1 && value.acceptance_criteria.length <= 32, `${name}.acceptance_criteria length`);
  value.acceptance_criteria.forEach((criterion, index) => assertText(criterion, 1000, `${name}.acceptance_criteria[${index}]`));
  expectKeys(value.task, ["instructions", "title"], `${name}.task`);
  assertText(value.task.title, 160, `${name}.task.title`);
  assertText(value.task.instructions, 8000, `${name}.task.instructions`);
  assertPolicy(value.policy, `${name}.policy`);
}

function verifyProposal(value, name) {
  expectKeys(value, ["created_at", "created_by", "current_revision", "current_revision_hash", "project_id", "proposal_id", "state", "workstream_id"], name);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertIdentifier(value.project_id, `${name}.project_id`);
  assertIdentifier(value.workstream_id, `${name}.workstream_id`);
  assertTimestamp(value.created_at, `${name}.created_at`);
  assert.equal(value.created_by, localActor, `${name}.created_by`);
  assert.ok(["open", "approved", "withdrawn"].includes(value.state), `${name}.state`);
  assertPositiveInteger(value.current_revision, `${name}.current_revision`);
  assertHash(value.current_revision_hash, `${name}.current_revision_hash`);
}

function verifyRevision(value, name) {
  expectKeys(value, ["acceptance_criteria", "created_at", "created_by", "idempotency_key", "parent_revision", "parent_revision_hash", "policy", "proposal_id", "revision", "schema_version", "task"], name);
  assert.equal(value.schema_version, "ananke.proposal-revision.v1", `${name}.schema_version`);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertTimestamp(value.created_at, `${name}.created_at`);
  assert.equal(value.created_by, localActor, `${name}.created_by`);
  assertIdempotencyKey(value.idempotency_key, `${name}.idempotency_key`);
  if (value.revision === 1) {
    assert.equal(value.parent_revision, null, `${name} root parent revision`);
    assert.equal(value.parent_revision_hash, null, `${name} root parent revision hash`);
  } else {
    assert.equal(value.parent_revision, value.revision - 1, `${name} parent revision`);
    assertHash(value.parent_revision_hash, `${name}.parent_revision_hash`);
  }
  assertRevisionInput({ acceptance_criteria: value.acceptance_criteria, policy: value.policy, task: value.task }, `${name} input`);
}

function verifyLifecycle(value, name) {
  expectKeys(value, ["approval_id", "created_at", "proposal_id", "revision", "revision_hash", "state", "updated_at", "version"], name);
  assertIdentifier(value.approval_id, `${name}.approval_id`);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertHash(value.revision_hash, `${name}.revision_hash`);
  assert.ok(["pending", "approved", "rejected", "superseded", "withdrawn"].includes(value.state), `${name}.state`);
  assertTimestamp(value.created_at, `${name}.created_at`);
  assertTimestamp(value.updated_at, `${name}.updated_at`);
  assertPositiveInteger(value.version, `${name}.version`);
}

function verifyApproval(value, name) {
  expectKeys(value, ["approval_id", "created_at", "created_by", "decided_at", "decided_by", "decision_idempotency_key", "proposal_id", "reason", "revision", "revision_hash", "state"], name);
  assertIdentifier(value.approval_id, `${name}.approval_id`);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertHash(value.revision_hash, `${name}.revision_hash`);
  assertTimestamp(value.created_at, `${name}.created_at`);
  assert.equal(value.created_by, localActor, `${name}.created_by`);
  assert.ok(["pending", "approved", "rejected", "superseded", "withdrawn"].includes(value.state), `${name}.state`);
  if (["approved", "rejected"].includes(value.state)) {
    assertTimestamp(value.decided_at, `${name}.decided_at`);
    assert.equal(value.decided_by, localActor, `${name}.decided_by`);
    assertIdempotencyKey(value.decision_idempotency_key, `${name}.decision_idempotency_key`);
    assertText(value.reason, 1000, `${name}.reason`);
  } else {
    assert.equal(value.decided_at, null, `${name}.decided_at`);
    assert.equal(value.decided_by, null, `${name}.decided_by`);
    assert.equal(value.decision_idempotency_key, null, `${name}.decision_idempotency_key`);
    assert.equal(value.reason, null, `${name}.reason`);
  }
}

function verifyMutation(value, name) {
  expectKeys(value, ["approval_id", "proposal_id", "revision", "revision_hash"], name);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertHash(value.revision_hash, `${name}.revision_hash`);
  assertIdentifier(value.approval_id, `${name}.approval_id`);
}

function verifyActivity(value, name) {
  expectKeys(value, ["approval_id", "operation", "proposal_id", "revision", "revision_hash", "sequence", "written_at"], name);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.sequence, `${name}.sequence`);
  assert.ok(["create_proposal", "append_revision", "decide_approval", "withdraw_proposal"].includes(value.operation), `${name}.operation`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertHash(value.revision_hash, `${name}.revision_hash`);
  assertIdentifier(value.approval_id, `${name}.approval_id`);
  assertTimestamp(value.written_at, `${name}.written_at`);
}

function verifyProtocol(value, schemas) {
  assertCommandMappings(commandSpecs);
  assertNoPrivateFields(value);
  expectKeys(value, ["commands", "schema_version"], "P1c protocol fixture");
  assert.equal(value.schema_version, "ananke.proposal-public-protocol.v1", "P1c protocol schema version");
  expectKeys(value.commands, commandSpecs.map(({ name }) => name), "P1c command mapping");

  for (const specification of commandSpecs) {
    const vector = value.commands[specification.name];
    expectKeys(vector, ["input", "result"], `${specification.name} vector`);
    validateSchema(vector.input, schemas.get(specification.input), `${specification.name}.input`);
    validateSchema(vector.result, schemas.get(specification.result), `${specification.name}.result`);
  }

  const create = value.commands.create_proposal;
  const list = value.commands.list_proposals;
  const get = value.commands.get_proposal;
  const activity = value.commands.list_proposal_activity;
  const append = value.commands.append_proposal_revision;
  const decision = value.commands.decide_proposal_approval;
  const withdraw = value.commands.withdraw_proposal;

  assertIdempotencyKey(create.input.idempotency_key, "create input idempotency key");
  assertIdentifier(create.input.project_id, "create input project id");
  assertIdentifier(create.input.workstream_id, "create input workstream id");
  assertRevisionInput(create.input.revision_input, "create input revision input");

  assertIdentifier(list.input.project_id, "list input project id");
  assertIdentifier(list.input.workstream_id, "list input workstream id");
  assertIdentifier(get.input.proposal_id, "get input proposal id");
  assertIdentifier(activity.input.proposal_id, "activity input proposal id");

  assertIdempotencyKey(append.input.idempotency_key, "append input idempotency key");
  assertIdentifier(append.input.proposal_id, "append input proposal id");
  assertPositiveInteger(append.input.expected_current_revision, "append input expected revision");
  assertHash(append.input.expected_current_revision_hash, "append input expected revision hash");
  assertRevisionInput(append.input.revision_input, "append input revision input");

  assertIdempotencyKey(decision.input.idempotency_key, "decision input idempotency key");
  assertIdentifier(decision.input.approval_id, "decision input approval id");
  assertIdentifier(decision.input.proposal_id, "decision input proposal id");
  assertPositiveInteger(decision.input.revision, "decision input revision");
  assertHash(decision.input.revision_hash, "decision input revision hash");
  assert.ok(["approved", "rejected"].includes(decision.input.decision), "decision input decision");
  assertText(decision.input.reason, 1000, "decision input reason");

  assertIdempotencyKey(withdraw.input.idempotency_key, "withdraw input idempotency key");
  assertIdentifier(withdraw.input.proposal_id, "withdraw input proposal id");

  verifyMutation(create.result, "create result");
  verifyMutation(append.result, "append result");
  verifyMutation(decision.result, "decision result");
  verifyMutation(withdraw.result, "withdraw result");

  expectKeys(list.result, ["proposals"], "list result");
  assert.ok(Array.isArray(list.result.proposals) && list.result.proposals.length === 1, "list result proposal inventory");
  verifyProposal(list.result.proposals[0], "list result proposal");

  expectKeys(get.result, ["approval", "lifecycle", "proposal", "revision"], "get result");
  verifyProposal(get.result.proposal, "detail proposal");
  verifyRevision(get.result.revision, "detail revision");
  verifyLifecycle(get.result.lifecycle, "detail lifecycle");
  verifyApproval(get.result.approval, "detail approval");

  expectKeys(activity.result, ["activity"], "activity result");
  assert.ok(Array.isArray(activity.result.activity) && activity.result.activity.length === 2, "activity result inventory");
  activity.result.activity.forEach((record, index) => verifyActivity(record, `activity result[${index}]`));

  const { proposal, revision, lifecycle, approval } = get.result;
  assert.equal(get.input.proposal_id, proposal.proposal_id, "get target proposal link");
  assert.equal(list.input.project_id, proposal.project_id, "list target project link");
  assert.equal(list.input.workstream_id, proposal.workstream_id, "list target workstream link");
  assert.deepEqual(list.result.proposals[0], proposal, "list summary/detail proposal link");
  assert.equal(create.input.project_id, proposal.project_id, "create/detail project link");
  assert.equal(create.input.workstream_id, proposal.workstream_id, "create/detail workstream link");
  assert.equal(create.input.idempotency_key, revision.idempotency_key, "create/revision idempotency link");
  assert.deepEqual(
    create.input.revision_input,
    { acceptance_criteria: revision.acceptance_criteria, policy: revision.policy, task: revision.task },
    "create/revision immutable input link",
  );
  assert.equal(proposal.proposal_id, revision.proposal_id, "proposal/revision link");
  assert.equal(proposal.current_revision, revision.revision, "proposal current revision link");
  const canonicalRevisionHash = hashCanonical(revision);
  assert.equal(canonicalRevisionHash, proposal.current_revision_hash, "detail revision/proposal canonical hash link");
  assert.equal(canonicalRevisionHash, lifecycle.revision_hash, "detail revision/lifecycle canonical hash link");
  assert.equal(canonicalRevisionHash, approval.revision_hash, "detail revision/approval canonical hash link");
  assert.equal(canonicalRevisionHash, p1aRootRevisionHash, "detail revision P1a canonical hash");
  assert.equal(lifecycle.proposal_id, proposal.proposal_id, "lifecycle proposal link");
  assert.equal(lifecycle.revision, revision.revision, "lifecycle revision link");
  assert.equal(lifecycle.revision_hash, proposal.current_revision_hash, "lifecycle revision hash link");
  assert.equal(lifecycle.approval_id, approval.approval_id, "lifecycle approval link");
  assert.equal(approval.proposal_id, proposal.proposal_id, "approval proposal link");
  assert.equal(approval.revision, revision.revision, "approval revision link");
  assert.equal(approval.revision_hash, proposal.current_revision_hash, "approval revision hash link");
  assert.equal(proposal.state, approval.state, "proposal/approval state link");
  assert.equal(lifecycle.state, approval.state, "lifecycle/approval state link");
  assert.equal(proposal.state, "approved", "golden proposal state");
  assert.equal(lifecycle.created_at, approval.created_at, "lifecycle/approval creation timestamp link");
  assert.equal(lifecycle.updated_at, approval.decided_at, "lifecycle/approval update timestamp link");
  assert.ok(Date.parse(proposal.created_at) <= Date.parse(approval.created_at), "proposal/approval timestamp order");
  assert.ok(Date.parse(approval.created_at) <= Date.parse(approval.decided_at), "approval decision timestamp order");

  assert.deepEqual(create.result, {
    proposal_id: proposal.proposal_id,
    revision: revision.revision,
    revision_hash: proposal.current_revision_hash,
    approval_id: approval.approval_id,
  }, "create mutation identity link");
  assert.equal(append.input.proposal_id, proposal.proposal_id, "append target proposal link");
  assert.equal(append.input.expected_current_revision, proposal.current_revision, "append expected revision link");
  assert.equal(append.input.expected_current_revision_hash, proposal.current_revision_hash, "append expected revision hash link");
  assert.equal(append.result.proposal_id, proposal.proposal_id, "append mutation proposal link");
  assert.equal(append.result.revision, proposal.current_revision + 1, "append mutation next revision");
  assert.notEqual(append.result.revision_hash, proposal.current_revision_hash, "append mutation new revision hash");
  assert.notEqual(append.result.approval_id, approval.approval_id, "append mutation new approval identity");
  assert.equal(decision.input.approval_id, approval.approval_id, "decision target approval link");
  assert.equal(decision.input.proposal_id, proposal.proposal_id, "decision target proposal link");
  assert.equal(decision.input.revision, revision.revision, "decision target revision link");
  assert.equal(decision.input.revision_hash, proposal.current_revision_hash, "decision target revision hash link");
  assert.equal(decision.input.idempotency_key, approval.decision_idempotency_key, "decision/approval idempotency link");
  assert.equal(decision.input.decision, approval.state, "decision/approval state link");
  assert.equal(decision.input.reason, approval.reason, "decision/approval reason link");
  assert.deepEqual(decision.result, create.result, "decision mutation identity link");
  assert.equal(withdraw.input.proposal_id, proposal.proposal_id, "withdraw target proposal link");
  assert.deepEqual(withdraw.result, create.result, "withdraw mutation identity link");

  const expectedActivity = [
    { operation: "create_proposal", sequence: 1, written_at: proposal.created_at },
    { operation: "decide_approval", sequence: 2, written_at: approval.decided_at },
  ];
  assert.equal(activity.input.proposal_id, proposal.proposal_id, "activity target proposal link");
  activity.result.activity.forEach((record, index) => {
    const expected = expectedActivity[index];
    assert.equal(record.proposal_id, proposal.proposal_id, `activity ${index} proposal link`);
    assert.equal(record.sequence, expected.sequence, `activity ${index} sequence`);
    assert.equal(record.operation, expected.operation, `activity ${index} operation`);
    assert.equal(record.revision, revision.revision, `activity ${index} revision link`);
    assert.equal(record.revision_hash, proposal.current_revision_hash, `activity ${index} revision hash link`);
    assert.equal(record.approval_id, approval.approval_id, `activity ${index} approval link`);
    assert.equal(record.written_at, expected.written_at, `activity ${index} timestamp link`);
  });
}

async function verify(fixturesDirectory, schemasDirectory) {
  const [manifestDigest, schemas] = await Promise.all([
    readManifest(fixturesDirectory),
    readSchemas(schemasDirectory),
  ]);
  const fixture = await readCanonicalFixture(fixturesDirectory, manifestDigest);
  verifyProtocol(fixture, schemas);
}

async function rewriteManifest(directory) {
  const bytes = await readFile(join(directory, fixtureName));
  await writeFile(join(directory, "fixtures.sha256"), `${hashVersion} sha256 ${digest(bytes)} ${fixtureName}`);
}

async function readJsonFixture(directory) {
  return JSON.parse(await readFile(join(directory, fixtureName), "utf8"));
}

async function writeCanonicalFixture(directory, value) {
  await writeFile(join(directory, fixtureName), canonicalJson(value));
}

function runCopiedVerifier(fixturesDirectory, schemasDirectory, { allowFixtureDrift = false } = {}) {
  return spawnSync(
    process.execPath,
    [scriptPath, "--fixtures", fixturesDirectory, "--schemas", schemasDirectory],
    {
      encoding: "utf8",
      env: { ...process.env, ANANKE_P1C_SELF_TEST_ALLOW_FIXTURE_DRIFT: allowFixtureDrift ? "1" : "0" },
    },
  );
}

function assertRejected(result, pattern, name) {
  assert.notEqual(result.status, 0, `${name} was accepted`);
  assert.match(`${result.stdout}${result.stderr}`, pattern, `${name} rejection reason`);
}

async function selfTest() {
  const root = await mkdtemp(join(tmpdir(), "ananke-p1c-contract-"));
  const copiedFixtures = join(root, "fixtures");
  const copiedSchemas = join(root, "schemas");
  const resetCopiedInputs = async () => {
    await rm(copiedFixtures, { force: true, recursive: true });
    await rm(copiedSchemas, { force: true, recursive: true });
    await Promise.all([
      cp(sourceFixtureDirectory, copiedFixtures, { recursive: true }),
      cp(sourceSchemaDirectory, copiedSchemas, { recursive: true }),
    ]);
  };
  try {
    await resetCopiedInputs();
    const baseline = runCopiedVerifier(copiedFixtures, copiedSchemas);
    assert.equal(baseline.status, 0, `fixture verifier baseline failed:\n${baseline.stdout}${baseline.stderr}`);
    const typoedCommandSpecs = commandSpecs.map((specification) => ({ ...specification }));
    typoedCommandSpecs[3].daemon = "list-proposal-activty";
    assert.throws(
      () => assertCommandMappings(typoedCommandSpecs),
      /Tauri-to-daemon command mapping/,
      "A typo in a future daemon command is rejected",
    );
    const semanticSchemas = await readSchemas(sourceSchemaDirectory);
    const semanticFixture = await readJsonFixture(sourceFixtureDirectory);
    const invalidTimestamp = structuredClone(semanticFixture.commands.list_proposals.result.proposals[0]);
    invalidTimestamp.created_at = "2026-02-30T09:00:00Z";
    assert.throws(
      () => validateSchema(invalidTimestamp, semanticSchemas.get("list").properties.proposals.items, "direct timestamp test"),
      /semantic UTC RFC 3339\/RFC3339Nano timestamp/,
      "Proposal timestamps retain P1a UTC semantics in the schema",
    );

    const oversizedTaskTitle = structuredClone(semanticFixture.commands.create_proposal.input.revision_input.task);
    oversizedTaskTitle.title = "😀".repeat(41);
    assert.throws(
      () => validateSchema(oversizedTaskTitle, semanticSchemas.get("createInput").properties.revision_input.properties.task, "direct byte limit test"),
      /UTF-8 byte length/,
      "Proposal text limits count UTF-8 bytes rather than JavaScript characters",
    );

    const pendingApprovalWithDecision = structuredClone(semanticFixture.commands.get_proposal.result.approval);
    pendingApprovalWithDecision.state = "pending";
    assert.throws(
      () => validateSchema(pendingApprovalWithDecision, semanticSchemas.get("detail").properties.approval, "direct pending approval test"),
      /non-terminal approval decision fields must be null/,
      "Pending Approvals cannot retain a decision",
    );

    const terminalApprovalWithForeignActor = structuredClone(semanticFixture.commands.get_proposal.result.approval);
    terminalApprovalWithForeignActor.decided_by = "another_actor";
    assert.throws(
      () => validateSchema(terminalApprovalWithForeignActor, semanticSchemas.get("detail").properties.approval, "direct terminal approval test"),
      /terminal approval actor/,
      "Terminal Approval decisions retain the local actor",
    );

    const rootRevisionWithParent = structuredClone(semanticFixture.commands.get_proposal.result.revision);
    rootRevisionWithParent.parent_revision = 1;
    rootRevisionWithParent.parent_revision_hash = `sha256:${"1".repeat(64)}`;
    assert.throws(
      () => validateSchema(rootRevisionWithParent, semanticSchemas.get("detail").properties.revision, "direct root revision test"),
      /root revision parents must be null/,
      "Root Revisions cannot have parents",
    );

    const revisionWithNonImmediateParent = structuredClone(semanticFixture.commands.get_proposal.result.revision);
    revisionWithNonImmediateParent.revision = 2;
    revisionWithNonImmediateParent.parent_revision = 2;
    revisionWithNonImmediateParent.parent_revision_hash = `sha256:${"1".repeat(64)}`;
    assert.throws(
      () => validateSchema(revisionWithNonImmediateParent, semanticSchemas.get("detail").properties.revision, "direct immediate parent test"),
      /immediate parent revision/,
      "Non-root Revisions require their immediate parent",
    );
    await resetCopiedInputs();
    const driftedFixture = await readJsonFixture(copiedFixtures);
    driftedFixture.commands.create_proposal.input.revision_input.task.title = "Drifted P1a proposal contract";
    await writeCanonicalFixture(copiedFixtures, driftedFixture);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, copiedSchemas), /canonical fixture digest mismatch/, "consistently rehashed content drift");

    await resetCopiedInputs();
    const mismatchedEmbeddedRevision = await readJsonFixture(copiedFixtures);
    mismatchedEmbeddedRevision.commands.get_proposal.result.revision.task.title = "Mismatched P1a proposal contract";
    mismatchedEmbeddedRevision.commands.create_proposal.input.revision_input.task.title = "Mismatched P1a proposal contract";
    await writeCanonicalFixture(copiedFixtures, mismatchedEmbeddedRevision);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /detail revision\/proposal canonical hash link/,
      "consistently rehashed embedded revision/hash mismatch",
    );

    await resetCopiedInputs();
    const privateFieldFixture = await readJsonFixture(copiedFixtures);
    privateFieldFixture.commands.get_proposal.input.socket_path = "/private/ananke.sock";
    await writeCanonicalFixture(copiedFixtures, privateFieldFixture);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /private fixture field/,
      "consistently rehashed private fixture field",
    );

    await resetCopiedInputs();
    const unknownFieldFixture = await readJsonFixture(copiedFixtures);
    unknownFieldFixture.commands.list_proposals.result.proposals[0].unexpected_public = "not allowed";
    await writeCanonicalFixture(copiedFixtures, unknownFieldFixture);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /unexpected property/,
      "consistently rehashed unknown fixture field",
    );

    await resetCopiedInputs();
    const privateFieldSchemaPath = join(copiedSchemas, "renderer-public-proposal-get-input.schema.json");
    const privateFieldSchema = JSON.parse(await readFile(privateFieldSchemaPath, "utf8"));
    privateFieldSchema.properties.identity_file = { type: "string" };
    privateFieldSchema.required.push("identity_file");
    await writeFile(privateFieldSchemaPath, JSON.stringify(privateFieldSchema));
    assertRejected(runCopiedVerifier(copiedFixtures, copiedSchemas), /private schema field/, "private schema field");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log("P1c fixture verifier self-test rejected a Tauri-to-daemon typo, invalid P1a timestamp, byte, Approval, and Revision semantics, consistently rehashed content drift, embedded revision/hash mismatch, private fixture and schema fields, and unknown fixture fields.");
} else {
  await verify(
    resolve(optionValue("--fixtures") ?? sourceFixtureDirectory),
    resolve(optionValue("--schemas") ?? sourceSchemaDirectory),
  );
  console.log("P1c proposal public protocol fixtures and 12 DTO schema targets verified.");
}
