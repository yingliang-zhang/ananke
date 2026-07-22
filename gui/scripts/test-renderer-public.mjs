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

console.log("Generated TypeScript decoders accepted public golden JSON and every P1c DTO, while rejecting malformed payloads.");
