import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import ts from "typescript";

const guiDirectory = resolve(import.meta.dirname, "..");
const fixture = JSON.parse(
  await readFile(resolve(guiDirectory, "../contracts/p2c/fixtures/protocol-v1.canonical.json"), "utf8"),
);
const source = await readFile(resolve(guiDirectory, "src/grill-review.ts"), "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const { GrillReviewController, bindGrillReview, renderGrillReview } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`,
);

const identity = fixture.commands.evaluate_grill.input;
const evaluation = fixture.commands.evaluate_grill.result;

function deferred() {
  let resolve;
  const promise = new Promise((nextResolve) => {
    resolve = nextResolve;
  });
  return { promise, resolve };
}

function recordingClient({ evaluate = async () => evaluation, answer = async () => undefined } = {}) {
  const calls = [];
  return {
    calls,
    client: {
      async evaluate(input) {
        calls.push(["evaluate", structuredClone(input)]);
        return evaluate(input);
      },
      async recordDefault(input) {
        calls.push(["record-default", structuredClone(input)]);
      },
      async recordAnswer(input) {
        calls.push(["record-answer", structuredClone(input)]);
        return answer(input);
      },
      async recordOverride(input) {
        calls.push(["record-override", structuredClone(input)]);
      },
    },
  };
}

{
  const { client } = recordingClient();
  const controller = new GrillReviewController(client);
  const guarded = renderGrillReview(controller.state);
  assert.match(guarded, /Grill review/);
  assert.match(guarded, /Awaiting a current proposal revision/);
  assert.match(guarded, /data-grill-action="refresh"[^>]*disabled/);

  controller.setRevision(identity);
  await controller.refresh();
  const panel = renderGrillReview(controller.state);
  assert.match(panel, /Proposal ID<\/dt><dd>proposal_p1a_001/);
  assert.match(panel, /Revision<\/dt><dd>1/);
  assert.match(panel, /Revision hash<\/dt><dd><code>sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263/);
  assert.match(panel, /Review status<\/dt><dd[^>]*>blocked/);
  assert.equal((panel.match(/class="grill-question"/g) ?? []).length, 5, "the panel bounds visible questions at five");
  assert.ok(
    panel.indexOf("observable outcome") < panel.indexOf("scope compatibility") &&
      panel.indexOf("scope compatibility") < panel.indexOf("acceptance evidence"),
    "questions render in deterministic sequence order",
  );
  assert.equal((panel.match(/data-grill-action="waive"/g) ?? []).length, 1, "only scope compatibility exposes waiver");
  assert.match(panel, /Risk<\/dt><dd>critical/);
  assert.match(panel, /Default<\/dt><dd>deny/);
  assert.match(panel, /Remedial step<\/dt><dd>record local authorization/);
  assert.match(panel, /Waivable<\/dt><dd>yes/);
}

{
  const answerGate = deferred();
  const { client, calls } = recordingClient({ answer: () => answerGate.promise });
  const controller = new GrillReviewController(client);
  controller.setRevision(identity);
  await controller.refresh();
  const acknowledgement = controller.record("acknowledge", "grill_question_acceptance_evidence");
  assert.equal(controller.state.pending, true, "all review actions become pending immediately");
  const pendingPanel = renderGrillReview(controller.state);
  assert.match(pendingPanel, /data-grill-action="acknowledge"[^>]*disabled/);
  assert.deepEqual(calls.at(-1), ["record-answer", fixture.commands.record_grill_answer.input]);
  answerGate.resolve();
  await acknowledgement;
  assert.equal(controller.state.pending, false);
  assert.deepEqual(
    calls.map(([kind]) => kind),
    ["evaluate", "record-answer", "evaluate"],
    "a successful record refreshes the deterministic evaluation",
  );
  await controller.record("waive", "grill_question_observable_outcome");
  assert.equal(calls.some(([kind]) => kind === "record-override"), false, "non-scope questions cannot be waived");
}

{
  const privateError = "socket /private/ananke/token=top-secret";
  const { client } = recordingClient({
    evaluate: async () => {
      throw new Error(privateError);
    },
  });
  const controller = new GrillReviewController(client);
  controller.setRevision(identity);
  await controller.refresh();
  assert.equal(controller.state.error, "Grill review is unavailable. Retry the deterministic review.");
  const panel = renderGrillReview(controller.state);
  assert.match(panel, /role="alert"/);
  assert.doesNotMatch(panel, /top-secret|private\/ananke|token=/);
}

{
  const { client, calls } = recordingClient();
  const controller = new GrillReviewController(client);
  controller.setRevision(identity);
  await controller.refresh();
  const buttons = [{
    dataset: { grillAction: "default", grillQuestion: "grill_question_scope_compatibility" },
    disabled: true,
    onclick: null,
  }];
  bindGrillReview({ querySelectorAll: () => buttons }, controller);
  buttons[0].onclick();
  assert.equal(
    calls.some(([kind]) => kind === "record-default"),
    false,
    "disabled Grill DOM controls cannot start a record operation",
  );
}

console.log("Grill review state and DOM contracts keep review identity bounded, actions guarded, and failures sanitized.");
