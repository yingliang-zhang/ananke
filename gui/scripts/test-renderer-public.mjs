import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import ts from "typescript";

const guiDirectory = resolve(import.meta.dirname, "..");
const fixture = JSON.parse(
  await readFile(resolve(guiDirectory, "contracts/fixtures/renderer-public-golden.json"), "utf8"),
);
const proposalFixture = JSON.parse(
  await readFile(resolve(guiDirectory, "../contracts/p1c/fixtures/protocol-v1.canonical.json"), "utf8"),
);
const grillFixture = JSON.parse(
  await readFile(resolve(guiDirectory, "../contracts/p2c/fixtures/protocol-v1.canonical.json"), "utf8"),
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

const proposalDecoderSpecs = [
  ["proposal-create-input", "toCreateProposalInput", proposalFixture.commands.create_proposal.input],
  ["proposal-list-input", "toListProposalsInput", proposalFixture.commands.list_proposals.input],
  ["proposal-get-input", "toGetProposalInput", proposalFixture.commands.get_proposal.input],
  ["proposal-activity-list-input", "toListProposalActivityInput", proposalFixture.commands.list_proposal_activity.input],
  ["proposal-append-input", "toAppendProposalRevisionInput", proposalFixture.commands.append_proposal_revision.input],
  ["proposal-decision-input", "toDecideProposalApprovalInput", proposalFixture.commands.decide_proposal_approval.input],
  ["proposal-withdraw-input", "toWithdrawProposalInput", proposalFixture.commands.withdraw_proposal.input],
  ["proposal-mutation", "toProposalMutation", proposalFixture.commands.create_proposal.result],
  ["proposal-list", "toProposalList", proposalFixture.commands.list_proposals.result],
  ["proposal-detail", "toProposalDetail", proposalFixture.commands.get_proposal.result],
  ["proposal-activity-list", "toProposalActivityList", proposalFixture.commands.list_proposal_activity.result],
  ["proposal-activity", "toProposalActivity", proposalFixture.commands.list_proposal_activity.result.activity[0]],
];
const proposalDecoders = await Promise.all(
  proposalDecoderSpecs.map(async ([name, method, value]) => ({
    converter: (await loadGeneratedModule(name)).Convert,
    method,
    value,
  })),
);
for (const { converter, method, value } of proposalDecoders) {
  assert.deepEqual(decode(converter, method, value), value, `${method} accepts the canonical P1c DTO`);
  assert.throws(
    () => decode(converter, method, { ...value, unexpected_public: true }),
    `${method} rejects unknown public fields`,
  );
}

const grillDecoderSpecs = [
  ["grill-evaluate-input", "toEvaluateGrillInput", grillFixture.commands.evaluate_grill.input],
  ["grill-record-default-input", "toRecordGrillDefaultInput", grillFixture.commands.record_grill_default.input],
  ["grill-record-answer-input", "toRecordGrillAnswerInput", grillFixture.commands.record_grill_answer.input],
  ["grill-record-override-input", "toRecordGrillOverrideInput", grillFixture.commands.record_grill_override.input],
  ["grill-evaluation", "toGrillEvaluation", grillFixture.commands.evaluate_grill.result],
  ["grill-question", "toGrillQuestion", grillFixture.commands.evaluate_grill.result.shown_questions[0]],
  ["grill-default-record", "toGrillDefaultRecord", grillFixture.commands.record_grill_default.result],
  ["grill-answer-record", "toGrillAnswerRecord", grillFixture.commands.record_grill_answer.result],
  ["grill-override-record", "toGrillOverrideRecord", grillFixture.commands.record_grill_override.result],
];
const grillDecoders = await Promise.all(
  grillDecoderSpecs.map(async ([name, method, value]) => ({
    converter: (await loadGeneratedModule(name)).Convert,
    method,
    value,
  })),
);
for (const { converter, method, value } of grillDecoders) {
  assert.deepEqual(decode(converter, method, value), value, `${method} accepts the canonical P2c DTO`);
  assert.throws(
    () => decode(converter, method, { ...value, unexpected_public: true }),
    `${method} rejects unknown public fields`,
  );
}

const grillEvaluateConverter = grillDecoders.find(({ method }) => method === "toEvaluateGrillInput").converter;
for (const field of ["cmd", "command", "model", "prose", "approval", "execution", "input_hash", "rule_version", "socket_path", "error"]) {
  assert.throws(
    () => decode(grillEvaluateConverter, "toEvaluateGrillInput", { ...grillFixture.commands.evaluate_grill.input, [field]: true }),
    `toEvaluateGrillInput rejects renderer-supplied ${field}`,
  );
}
const grillAnswerInputConverter = grillDecoders.find(({ method }) => method === "toRecordGrillAnswerInput").converter;
assert.throws(
  () => decode(grillAnswerInputConverter, "toRecordGrillAnswerInput", { ...grillFixture.commands.record_grill_answer.input, answer: "arbitrary prose" }),
  "toRecordGrillAnswerInput rejects renderer-supplied answer content",
);

const grillConvertersByMethod = new Map(grillDecoders.map(({ converter, method }) => [method, converter]));
function rejectGrillMutation(method, value, mutate, message) {
  const invalid = structuredClone(value);
  mutate(invalid);
  assert.throws(() => decode(grillConvertersByMethod.get(method), method, invalid), message);
}

for (const { method, value } of grillDecoders) {
  for (const [field, replacement] of [
    ["proposal_id", "1"],
    ["revision", 0],
    ["revision_hash", "sha256:not-a-hash"],
  ]) {
    rejectGrillMutation(method, value, (invalid) => { invalid[field] = replacement; }, `${method} enforces ${field}`);
  }
}

for (const method of ["toRecordGrillDefaultInput", "toRecordGrillAnswerInput", "toRecordGrillOverrideInput"]) {
  const value = grillDecoders.find((decoder) => decoder.method === method).value;
  rejectGrillMutation(method, value, (invalid) => { invalid.question_id = "grill_question_unknown"; }, `${method} enforces the Question ID pattern`);
}

const grillQuestion = grillFixture.commands.evaluate_grill.result.shown_questions[0];
for (const [field, replacement] of [
  ["question_id", "grill_question_autonomy_budget"],
  ["question_sequence", 0],
  ["record_sequence", 41],
  ["blocking", false],
  ["risk", "medium"],
  ["written_at", "not-a-timestamp"],
]) {
  rejectGrillMutation("toGrillQuestion", grillQuestion, (invalid) => { invalid[field] = replacement; }, `toGrillQuestion enforces ${field}`);
}

const grillEvaluation = grillFixture.commands.evaluate_grill.result;
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => {
  invalid.shown_questions.push(structuredClone(invalid.shown_questions[4]));
}, "toGrillEvaluation limits shown Questions to five");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => {
  invalid.new_question_ids.push("grill_question_autonomy_budget");
}, "toGrillEvaluation limits new Question IDs to five");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => {
  invalid.deferred_rule_classes.push(...Array(6).fill("autonomy_budget"));
}, "toGrillEvaluation limits deferred rule classes to six");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.new_records = 7; }, "toGrillEvaluation limits new records to six");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.shown_questions[0].revision = 2; }, "toGrillEvaluation matches Question Revision identity");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.shown_questions[0].proposal_id = "proposal_p1a_002"; }, "toGrillEvaluation matches Question proposal identity");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.shown_questions[0].revision_hash = `sha256:${"0".repeat(64)}`; }, "toGrillEvaluation matches Question revision hashes");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.shown_questions[0].question_id = "grill_question_autonomy_budget"; }, "toGrillEvaluation matches Question IDs to rule classes");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.shown_questions[0].blocking = false; }, "toGrillEvaluation rejects non-blocking Questions");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => {
  [invalid.shown_questions[0], invalid.shown_questions[1]] = [invalid.shown_questions[1], invalid.shown_questions[0]];
  [invalid.new_question_ids[0], invalid.new_question_ids[1]] = [invalid.new_question_ids[1], invalid.new_question_ids[0]];
}, "toGrillEvaluation retains P2b priority order");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.new_question_ids[0] = "grill_question_autonomy_budget"; }, "toGrillEvaluation preserves shown Question order for new IDs");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.new_records = 4; }, "toGrillEvaluation accounts for every appended Question");
rejectGrillMutation("toGrillEvaluation", grillEvaluation, (invalid) => { invalid.status = "clear"; }, "toGrillEvaluation permits clear only without active Questions");

for (const method of ["toGrillDefaultRecord", "toGrillAnswerRecord", "toGrillOverrideRecord"]) {
  const value = grillDecoders.find((decoder) => decoder.method === method).value;
  for (const [field, replacement] of [["record_sequence", 0], ["record_sequence", 41], ["written_at", "not-a-timestamp"]]) {
    rejectGrillMutation(method, value, (invalid) => { invalid[field] = replacement; }, `${method} enforces ${field}`);
  }
}
for (const [method, field, replacement] of [
  ["toGrillDefaultRecord", "default", "acknowledged"],
  ["toGrillAnswerRecord", "answer", "needs_rewrite"],
  ["toGrillOverrideRecord", "override", "acknowledged"],
]) {
  const value = grillDecoders.find((decoder) => decoder.method === method).value;
  rejectGrillMutation(method, value, (invalid) => { invalid.question_id = "grill_question_unknown"; }, `${method} enforces the Question ID pattern`);
  rejectGrillMutation(method, value, (invalid) => { invalid[field] = replacement; }, `${method} enforces its fixed record value`);
}

console.log("Generated TypeScript decoders accepted public golden JSON plus every P1c and P2c DTO, while rejecting malformed, private, and renderer-supplied Grill fields.");
