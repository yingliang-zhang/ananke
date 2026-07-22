import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const sourceFixtureDirectory = resolve(dirname(scriptPath), "fixtures");
const fixtureNames = [
  "acceptance-v1.canonical.json",
  "approval-v1.canonical.json",
  "proposal-v1.canonical.json",
  "request-envelopes-v1.canonical.json",
  "revision-lifecycle-v1.canonical.json",
  "revision-v1.canonical.json",
  "state-machine-v1.canonical.json",
];
const hashVersion = "ananke-proposal-canonical-json-v1";
const prohibitedKeys = new Set([
  "access_token",
  "api_key",
  "api_token",
  "audit_output",
  "authorization",
  "authorization_header",
  "command",
  "completion",
  "cookie",
  "cookies",
  "credential",
  "credentials",
  "environment",
  "file_path",
  "identity_file",
  "identity_path",
  "model_completion",
  "model_output",
  "model_prompt",
  "omp_audit_output",
  "password",
  "path",
  "pid",
  "process_id",
  "prompt",
  "refresh_token",
  "repo_root",
  "repository_path",
  "repository_root",
  "root_path",
  "secret",
  "socket_path",
  "ssh_key",
  "ssh_key_path",
  "token",
  "transcript_path",
  "worker_args",
  "worker_command",
  "worker_env",
  "worker_path",
  "worktree_path",
  "workspace_path",
]);
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const idempotencyKeyPattern = /^[a-z][a-z0-9_]{2,127}$/;
const hashPattern = /^sha256:[0-9a-f]{64}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const localActor = "local_gui_operator";

function optionValue(name) {
  const index = process.argv.indexOf(name);
  if (index === -1) return undefined;
  assert.ok(process.argv[index + 1], `${name} requires a value`);
  return process.argv[index + 1];
}

function assertNoUnpairedSurrogates(value, path = "$") {
  if (typeof value === "string") {
    for (let index = 0; index < value.length; index += 1) {
      const codeUnit = value.charCodeAt(index);
      if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
        const next = value.charCodeAt(index + 1);
        assert.ok(
          next >= 0xdc00 && next <= 0xdfff,
          `unpaired Unicode surrogate at ${path}[${index}]`,
        );
        index += 1;
      } else {
        assert.ok(
          codeUnit < 0xdc00 || codeUnit > 0xdfff,
          `unpaired Unicode surrogate at ${path}[${index}]`,
        );
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

function expectObject(value, name) {
  assert.ok(value !== null && typeof value === "object" && !Array.isArray(value), `${name} must be an object`);
}

function expectKeys(value, expected, name) {
  expectObject(value, name);
  assert.deepEqual(Object.keys(value).sort(), [...expected].sort(), `${name} fields`);
}

function assertNoPrivateKeys(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoPrivateKeys(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    assert.ok(!prohibitedKeys.has(key), `private field ${path}.${key} is not allowlisted`);
    assertNoPrivateKeys(entry, `${path}.${key}`);
  }
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

function assertPolicy(value, name) {
  expectKeys(value, ["adapter", "authority", "budget", "model_role"], name);
  expectKeys(value.adapter, ["access", "kind", "status"], `${name}.adapter`);
  expectKeys(value.budget, ["dimensions", "status"], `${name}.budget`);
  assert.deepEqual(value, {
    adapter: { access: "read_only", kind: "omp_audit", status: "future" },
    authority: "deterministic",
    budget: { dimensions: ["deadline", "attempt_cap"], status: "future" },
    model_role: "advisory_only",
  }, `${name} values`);
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

async function readCanonical(directory, name) {
  const bytes = await readFile(join(directory, name));
  assert.ok(!bytes.subarray(0, 3).equals(Buffer.from([0xef, 0xbb, 0xbf])), `${name} has a UTF-8 BOM`);
  const text = bytes.toString("utf8");
  assert.ok(Buffer.from(text, "utf8").equals(bytes), `${name} is not UTF-8`);
  const value = JSON.parse(text);
  assertNoUnpairedSurrogates(value);
  assert.equal(text, canonicalJson(value), `${name} is not canonical JCS bytes`);
  assertNoPrivateKeys(value);
  return { bytes, value };
}

async function readManifest(directory) {
  const text = await readFile(join(directory, "fixtures.sha256"), "utf8");
  assert.ok(!text.endsWith("\n"), "fixtures.sha256 must not end with a newline");
  const entries = text.split("\n").map((line) => {
    const match = line.match(/^([a-z0-9-]+) sha256 ([0-9a-f]{64}) ([a-z0-9.-]+)$/);
    assert.ok(match, `invalid hash manifest entry: ${line}`);
    return { version: match[1], digest: match[2], name: match[3] };
  });
  assert.deepEqual(entries.map(({ name }) => name), fixtureNames, "hash manifest fixture inventory");
  for (const entry of entries) assert.equal(entry.version, hashVersion, `hash version for ${entry.name}`);
  return new Map(entries.map((entry) => [entry.name, entry.digest]));
}

function digest(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function hashCanonical(value) {
  return `sha256:${digest(Buffer.from(canonicalJson(value), "utf8"))}`;
}

function verifyStateMachine(value) {
  expectKeys(value, ["approval", "proposal", "revision_lifecycle", "schema_version"], "state machine");
  assert.equal(value.schema_version, "ananke.proposal.state-machine.v1");
  const pendingTransitions = ["approved", "rejected", "superseded", "withdrawn"];
  const lifecycleTransitions = {
    approved: [],
    pending: pendingTransitions,
    rejected: [],
    superseded: [],
    withdrawn: [],
  };
  assert.deepEqual(value.proposal, { approved: [], open: ["approved", "withdrawn"], withdrawn: [] });
  assert.deepEqual(value.revision_lifecycle, lifecycleTransitions);
  assert.deepEqual(value.approval, lifecycleTransitions);
}

function verifyProposal(value) {
  expectKeys(
    value,
    ["created_at", "created_by", "current_revision", "current_revision_hash", "project_id", "proposal_id", "state", "workstream_id"],
    "proposal",
  );
  assertIdentifier(value.proposal_id, "proposal.proposal_id");
  assertIdentifier(value.project_id, "proposal.project_id");
  assertIdentifier(value.workstream_id, "proposal.workstream_id");
  assertTimestamp(value.created_at, "proposal.created_at");
  assert.equal(value.created_by, localActor, "proposal.created_by");
  assert.ok(["open", "approved", "withdrawn"].includes(value.state), "proposal.state");
  assertPositiveInteger(value.current_revision, "proposal.current_revision");
  assertHash(value.current_revision_hash, "proposal.current_revision_hash");
}

function verifyRevision(value) {
  expectKeys(
    value,
    ["acceptance_criteria", "created_at", "created_by", "idempotency_key", "parent_revision", "parent_revision_hash", "policy", "proposal_id", "revision", "schema_version", "task"],
    "revision snapshot",
  );
  assert.equal(value.schema_version, "ananke.proposal-revision.v1");
  assertIdentifier(value.proposal_id, "revision.proposal_id");
  assertPositiveInteger(value.revision, "revision.revision");
  assertTimestamp(value.created_at, "revision.created_at");
  assert.equal(value.created_by, localActor, "revision.created_by");
  assertIdempotencyKey(value.idempotency_key, "revision.idempotency_key");
  if (value.revision === 1) {
    assert.equal(value.parent_revision, null, "revision 1 parent_revision");
    assert.equal(value.parent_revision_hash, null, "revision 1 parent_revision_hash");
  } else {
    assert.equal(value.parent_revision, value.revision - 1, "revision parent_revision");
    assertHash(value.parent_revision_hash, "revision.parent_revision_hash");
  }
  assertRevisionInput(
    { acceptance_criteria: value.acceptance_criteria, policy: value.policy, task: value.task },
    "revision snapshot input",
  );
}

function verifyApproval(value) {
  expectKeys(
    value,
    ["approval_id", "created_at", "created_by", "decided_at", "decided_by", "decision_idempotency_key", "proposal_id", "reason", "revision", "revision_hash", "state"],
    "approval",
  );
  assertIdentifier(value.approval_id, "approval.approval_id");
  assertIdentifier(value.proposal_id, "approval.proposal_id");
  assertPositiveInteger(value.revision, "approval.revision");
  assertHash(value.revision_hash, "approval.revision_hash");
  assertTimestamp(value.created_at, "approval.created_at");
  assert.equal(value.created_by, localActor, "approval.created_by");
  assert.ok(["pending", "approved", "rejected", "superseded", "withdrawn"].includes(value.state), "approval.state");
  if (["approved", "rejected"].includes(value.state)) {
    assertTimestamp(value.decided_at, "approval.decided_at");
    assert.equal(value.decided_by, localActor, "approval.decided_by");
    assertIdempotencyKey(value.decision_idempotency_key, "approval.decision_idempotency_key");
    assertText(value.reason, 1000, "approval.reason");
  } else {
    assert.equal(value.decided_at, null, `approval ${value.state} decided_at`);
    assert.equal(value.decided_by, null, `approval ${value.state} decided_by`);
    assert.equal(value.decision_idempotency_key, null, `approval ${value.state} decision_idempotency_key`);
    assert.equal(value.reason, null, `approval ${value.state} reason`);
  }
}

function verifyRevisionLifecycle(value, { approval, proposal, revision, revisionHash }) {
  expectKeys(value, ["lifecycle_records", "schema_version"], "revision lifecycle fixture");
  assert.equal(value.schema_version, "ananke.proposal.revision-lifecycle.v1");
  assert.ok(Array.isArray(value.lifecycle_records) && value.lifecycle_records.length === 1, "revision lifecycle record inventory");
  const [record] = value.lifecycle_records;
  expectKeys(
    record,
    ["approval_id", "created_at", "proposal_id", "revision", "revision_hash", "state", "updated_at", "version"],
    "revision lifecycle record",
  );
  assertIdentifier(record.approval_id, "revision lifecycle approval_id");
  assertIdentifier(record.proposal_id, "revision lifecycle proposal_id");
  assertPositiveInteger(record.revision, "revision lifecycle revision");
  assertHash(record.revision_hash, "revision lifecycle revision_hash");
  assert.ok(["pending", "approved", "rejected", "superseded", "withdrawn"].includes(record.state), "revision lifecycle state");
  assertTimestamp(record.created_at, "revision lifecycle created_at");
  assertTimestamp(record.updated_at, "revision lifecycle updated_at");
  assertPositiveInteger(record.version, "revision lifecycle version");
  assert.equal(record.proposal_id, proposal.proposal_id, "revision lifecycle proposal reference");
  assert.equal(record.revision, revision.revision, "revision lifecycle revision reference");
  assert.equal(record.revision_hash, revisionHash, "revision lifecycle revision hash reference");
  assert.equal(record.approval_id, approval.approval_id, "revision lifecycle approval reference");
  assert.equal(record.state, approval.state, "revision lifecycle and approval state agreement");
  assert.equal(record.created_at, approval.created_at, "revision lifecycle creation timestamp");
  assert.equal(record.updated_at, approval.decided_at, "revision lifecycle update timestamp");
  assert.equal(record.version, 2, "revision lifecycle version after one decision");
}

function verifyRecords({ proposal, revision, approval, revisionHash }) {
  verifyProposal(proposal);
  verifyRevision(revision);
  verifyApproval(approval);
  assert.equal(proposal.proposal_id, revision.proposal_id, "proposal/revision reference");
  assert.equal(proposal.current_revision, revision.revision, "proposal current revision");
  assert.equal(proposal.current_revision_hash, revisionHash, "proposal current revision hash");
  assert.equal(proposal.state, "approved", "golden proposal state");
  assert.equal(approval.proposal_id, revision.proposal_id, "approval proposal reference");
  assert.equal(approval.revision, revision.revision, "approval revision reference");
  assert.equal(approval.revision_hash, revisionHash, "approval revision hash reference");
  assert.equal(approval.state, "approved", "golden approval state");
}

function verifyRequestEnvelope(value, { name, operation, scope, revisionHash }) {
  expectKeys(value, ["body", "body_hash", "idempotency_key", "operation", "schema_version", "scope"], `${name} envelope`);
  assert.equal(value.schema_version, "ananke.proposal.request-envelope.v1", `${name} envelope schema_version`);
  assert.equal(value.operation, operation, `${name} envelope operation`);
  assert.deepEqual(value.scope, scope, `${name} envelope scope`);
  assert.equal(value.scope.length, 3, `${name} envelope scope length`);
  assert.equal(value.scope[0], localActor, `${name} envelope scope actor`);
  assertIdempotencyKey(value.idempotency_key, `${name} envelope idempotency_key`);
  assertHash(value.body_hash, `${name} envelope body_hash`);
  assert.equal(value.body_hash, hashCanonical(value.body), `${name} request body hash`);
  assert.notEqual(value.body_hash, revisionHash, `${name} request body hash must not equal revision snapshot hash`);

  if (operation === "create_proposal") {
    expectKeys(value.body, ["project_id", "revision_input", "schema_version", "workstream_id"], `${name} body`);
    assert.equal(value.body.schema_version, "ananke.proposal.create-request.v1", `${name} body schema_version`);
    assertIdentifier(value.body.project_id, `${name} body project_id`);
    assertIdentifier(value.body.workstream_id, `${name} body workstream_id`);
    assertRevisionInput(value.body.revision_input, `${name} body revision_input`);
    return;
  }
  if (operation === "append_revision") {
    expectKeys(value.body, ["expected_current_revision", "expected_current_revision_hash", "proposal_id", "revision_input", "schema_version"], `${name} body`);
    assert.equal(value.body.schema_version, "ananke.proposal.append-request.v1", `${name} body schema_version`);
    assertIdentifier(value.body.proposal_id, `${name} body proposal_id`);
    assertPositiveInteger(value.body.expected_current_revision, `${name} body expected_current_revision`);
    assertHash(value.body.expected_current_revision_hash, `${name} body expected_current_revision_hash`);
    assertRevisionInput(value.body.revision_input, `${name} body revision_input`);
    return;
  }
  if (operation === "decide_approval") {
    expectKeys(value.body, ["approval_id", "decision", "proposal_id", "reason", "revision", "revision_hash", "schema_version"], `${name} body`);
    assert.equal(value.body.schema_version, "ananke.proposal.decision-request.v1", `${name} body schema_version`);
    assertIdentifier(value.body.approval_id, `${name} body approval_id`);
    assertIdentifier(value.body.proposal_id, `${name} body proposal_id`);
    assertPositiveInteger(value.body.revision, `${name} body revision`);
    assertHash(value.body.revision_hash, `${name} body revision_hash`);
    assert.ok(["approved", "rejected"].includes(value.body.decision), `${name} body decision`);
    assertText(value.body.reason, 1000, `${name} body reason`);
    return;
  }
  expectKeys(value.body, ["proposal_id", "schema_version"], `${name} body`);
  assert.equal(value.body.schema_version, "ananke.proposal.withdraw-request.v1", `${name} body schema_version`);
  assertIdentifier(value.body.proposal_id, `${name} body proposal_id`);
}

function verifyRequestEnvelopes(value, { approval, proposal, revision, revisionHash, revisionLifecycle }) {
  expectKeys(value, ["append", "create", "decision_approve", "decision_reject", "schema_version", "withdraw"], "request envelopes fixture");
  assert.equal(value.schema_version, "ananke.proposal.request-envelopes.v1");
  verifyRequestEnvelope(value.create, {
    name: "create",
    operation: "create_proposal",
    revisionHash,
    scope: [localActor, "create_proposal", "proposal_collection"],
  });
  verifyRequestEnvelope(value.append, {
    name: "append",
    operation: "append_revision",
    revisionHash,
    scope: [localActor, "append_revision", proposal.proposal_id],
  });
  verifyRequestEnvelope(value.decision_approve, {
    name: "decision approve",
    operation: "decide_approval",
    revisionHash,
    scope: [localActor, "decide_approval", approval.approval_id],
  });
  verifyRequestEnvelope(value.decision_reject, {
    name: "decision reject",
    operation: "decide_approval",
    revisionHash,
    scope: [localActor, "decide_approval", approval.approval_id],
  });
  verifyRequestEnvelope(value.withdraw, {
    name: "withdraw",
    operation: "withdraw_proposal",
    revisionHash,
    scope: [localActor, "withdraw_proposal", proposal.proposal_id],
  });

  assert.equal(value.create.body.project_id, proposal.project_id, "create body project target");
  assert.equal(value.create.body.workstream_id, proposal.workstream_id, "create body workstream target");
  assert.equal(value.create.idempotency_key, revision.idempotency_key, "create envelope revision idempotency identity");
  assert.deepEqual(
    value.create.body.revision_input,
    { acceptance_criteria: revision.acceptance_criteria, policy: revision.policy, task: revision.task },
    "create body immutable revision input",
  );

  const [lifecycle] = revisionLifecycle.lifecycle_records;
  assert.equal(value.append.body.proposal_id, proposal.proposal_id, "append body proposal target");
  assert.equal(value.append.body.expected_current_revision, proposal.current_revision, "append body expected proposal revision");
  assert.equal(value.append.body.expected_current_revision, revision.revision, "append body expected immutable revision");
  assert.equal(value.append.body.expected_current_revision, lifecycle.revision, "append body expected lifecycle revision");
  assert.equal(value.append.body.expected_current_revision, approval.revision, "append body expected approval revision");
  assert.equal(value.append.body.expected_current_revision_hash, proposal.current_revision_hash, "append body expected proposal hash");
  assert.equal(value.append.body.expected_current_revision_hash, revisionHash, "append body expected immutable revision hash");
  assert.equal(value.append.body.expected_current_revision_hash, lifecycle.revision_hash, "append body expected lifecycle hash");
  assert.equal(value.append.body.expected_current_revision_hash, approval.revision_hash, "append body expected approval hash");

  function assertDecisionReferences(envelope, name) {
    assert.equal(envelope.body.approval_id, approval.approval_id, `${name} body approval target`);
    assert.equal(envelope.body.proposal_id, proposal.proposal_id, `${name} body proposal target`);
    assert.equal(envelope.body.revision, revision.revision, `${name} body immutable revision`);
    assert.equal(envelope.body.revision, lifecycle.revision, `${name} body lifecycle revision`);
    assert.equal(envelope.body.revision, approval.revision, `${name} body approval revision`);
    assert.equal(envelope.body.revision_hash, revisionHash, `${name} body immutable revision hash`);
    assert.equal(envelope.body.revision_hash, lifecycle.revision_hash, `${name} body lifecycle revision hash`);
    assert.equal(envelope.body.revision_hash, approval.revision_hash, `${name} body approval revision hash`);
  }

  assertDecisionReferences(value.decision_approve, "decision approve");
  assert.equal(value.decision_approve.idempotency_key, approval.decision_idempotency_key, "decision approve idempotency identity");
  assert.equal(value.decision_approve.body.decision, approval.state, "decision approve body decision");
  assert.equal(value.decision_approve.body.reason, approval.reason, "decision approve body reason");
  assertDecisionReferences(value.decision_reject, "decision reject");
  assert.equal(value.decision_reject.body.decision, "rejected", "decision reject body decision");
  assert.equal(value.decision_reject.body.reason, "Does not meet the frozen P1a contract.", "decision reject body reason");

  assert.equal(value.withdraw.body.proposal_id, proposal.proposal_id, "withdraw body proposal target");
  return value;
}

function requestReference(envelope, idempotencyKey = envelope.idempotency_key) {
  return {
    body_hash: envelope.body_hash,
    idempotency_key: idempotencyKey,
    scope: envelope.scope,
  };
}

function verifyAcceptance(value, { envelopes, revisionHash }) {
  expectKeys(value, ["cases", "schema_version"], "acceptance fixture");
  assert.equal(value.schema_version, "ananke.proposal.acceptance.v1");
  const revisionTwoHash = `sha256:${"2".repeat(64)}`;
  const currentThreeHash = `sha256:${"3".repeat(64)}`;
  const staleCurrentHash = `sha256:${"b".repeat(64)}`;
  const alternateBodyHash = `sha256:${"f".repeat(64)}`;
  const create = requestReference(envelopes.create);
  const append = requestReference(envelopes.append);
  const approve = requestReference(envelopes.decision_approve);
  const reject = requestReference(envelopes.decision_reject);
  const withdraw = requestReference(envelopes.withdraw);
  const expectedCases = [
    {
      given: { durable_writes: 0, proposal_exists: false },
      id: "create_replay",
      operation: "create_proposal",
      requests: [create, create],
      then: { durable_writes: 1, idempotency_lookup: "before_mutable_checks", result: "same persisted proposal and revision hash" },
    },
    {
      given: { durable_writes: 1, proposal_exists: true },
      id: "idempotency_conflict",
      operation: "create_proposal",
      requests: [create, { ...create, body_hash: alternateBodyHash }],
      then: { error: "idempotency_conflict", idempotency_lookup: "before_mutable_checks", new_durable_writes: 0 },
    },
    {
      given: { current_revision: 2, current_revision_hash: staleCurrentHash, proposal_state: "open" },
      id: "revision_conflict",
      operation: "append_revision",
      requests: [append],
      then: { error: "revision_conflict", new_durable_writes: 0 },
    },
    {
      given: {
        current_approval_state: "pending",
        current_lifecycle_state: "pending",
        current_revision: 1,
        current_revision_hash: revisionHash,
        proposal_state: "open",
      },
      id: "append_from_pending",
      operation: "append_revision",
      requests: [append],
      then: {
        atomic: true,
        former_approval_state: "superseded",
        former_lifecycle_state: "superseded",
        new_approval_state: "pending",
        new_lifecycle_state: "pending",
        new_revision_parent_revision: 1,
        new_revision_parent_revision_hash: revisionHash,
        proposal_current_revision: 2,
        proposal_current_revision_hash: revisionTwoHash,
      },
    },
    {
      given: {
        current_approval_state: "rejected",
        current_lifecycle_state: "rejected",
        current_revision: 1,
        current_revision_hash: revisionHash,
        proposal_state: "open",
      },
      id: "append_after_rejection",
      operation: "append_revision",
      requests: [append],
      then: {
        atomic: true,
        new_approval_state: "pending",
        new_lifecycle_state: "pending",
        new_revision_parent_revision: 1,
        new_revision_parent_revision_hash: revisionHash,
        predecessor_approval_state: "rejected",
        predecessor_lifecycle_state: "rejected",
        proposal_current_revision: 2,
        proposal_current_revision_hash: revisionTwoHash,
      },
    },
    {
      given: {
        current_approval_state: "rejected",
        current_lifecycle_state: "rejected",
        proposal_state: "open",
      },
      id: "withdraw_after_rejection",
      operation: "withdraw_proposal",
      requests: [withdraw],
      then: {
        atomic: true,
        current_approval_state: "rejected",
        current_lifecycle_state: "rejected",
        proposal_state: "withdrawn",
      },
    },
    {
      given: {
        after_restart: true,
        approval_state: "approved",
        idempotency_record_exists: true,
        proposal_state: "approved",
        revision_lifecycle_state: "approved",
      },
      id: "restart_create_replay_after_state_change",
      operation: "create_proposal",
      requests: [create],
      then: { idempotency_lookup: "before_mutable_checks", new_durable_writes: 0, result: "same persisted proposal and revision hash" },
    },
    {
      given: {
        after_restart: true,
        approval_state: "rejected",
        current_revision: 3,
        current_revision_hash: currentThreeHash,
        idempotency_record_exists: true,
        proposal_state: "open",
        revision_lifecycle_state: "rejected",
      },
      id: "restart_append_replay_after_state_change",
      operation: "append_revision",
      requests: [append],
      then: { idempotency_lookup: "before_mutable_checks", new_durable_writes: 0, result: "same persisted proposal and revision hash" },
    },
    {
      given: {
        after_restart: true,
        approval_state: "approved",
        idempotency_record_exists: true,
        proposal_state: "approved",
        revision_lifecycle_state: "approved",
      },
      id: "restart_decision_replay_after_state_change",
      operation: "decide_approval",
      requests: [approve],
      then: { idempotency_lookup: "before_mutable_checks", new_durable_writes: 0, result: "same persisted approval decision" },
    },
    {
      given: {
        current_approval_state: "pending",
        current_lifecycle_state: "pending",
        current_revision: 1,
        current_revision_hash: revisionHash,
        proposal_state: "open",
      },
      id: "concurrent_append",
      operation: "append_revision",
      requests: [requestReference(envelopes.append, "revise_p1a_002_a"), requestReference(envelopes.append, "revise_p1a_002_b")],
      then: { commits: 1, loser_error: "revision_conflict", loser_new_durable_writes: 0 },
    },
    {
      given: {
        approval_state: "pending",
        proposal_state: "open",
        revision_hash: revisionHash,
        revision_lifecycle_state: "pending",
      },
      id: "concurrent_decision",
      operation: "decide_approval",
      requests: [approve, reject],
      then: {
        commits: 1,
        loser_error: "approval_conflict",
        loser_new_durable_writes: 0,
        terminal_decisions: ["approved", "rejected"],
      },
    },
    {
      given: {
        approval_state: "pending",
        current_revision: 1,
        current_revision_hash: revisionHash,
        proposal_state: "open",
        revision_lifecycle_state: "pending",
      },
      id: "concurrent_append_approve",
      operation: "append_or_decide",
      requests: [append, approve],
      then: {
        commits: 1,
        loser_errors: ["approval_conflict", "revision_conflict"],
        loser_new_durable_writes: 0,
        partial_writes: 0,
        winner_operations: ["append_revision", "decide_approval"],
      },
    },
    {
      given: {
        approval_state: "pending",
        current_revision: 1,
        current_revision_hash: revisionHash,
        proposal_state: "open",
        revision_lifecycle_state: "pending",
      },
      id: "concurrent_append_reject_append_first",
      operation: "append_or_decide",
      requests: [append, reject],
      then: {
        commits: 1,
        linearization_order: ["append_revision", "decide_approval"],
        loser_error: "approval_conflict",
        loser_new_durable_writes: 0,
        partial_writes: 0,
        winner_operation: "append_revision",
      },
    },
    {
      given: {
        approval_state: "pending",
        current_revision: 1,
        current_revision_hash: revisionHash,
        proposal_state: "open",
        revision_lifecycle_state: "pending",
      },
      id: "concurrent_append_reject_reject_first",
      operation: "append_or_decide",
      requests: [append, reject],
      then: {
        commits: 2,
        final_current_approval_state: "pending",
        final_current_lifecycle_state: "pending",
        final_current_revision: 2,
        final_current_revision_hash: revisionTwoHash,
        final_predecessor_approval_state: "rejected",
        final_predecessor_lifecycle_state: "rejected",
        final_proposal_state: "open",
        linearization_order: ["decide_approval", "append_revision"],
        partial_writes: 0,
      },
    },
    {
      given: { durable_writes: 0, proposal_exists: false },
      id: "concurrent_same_key_replay",
      operation: "create_proposal",
      requests: [create, create],
      then: {
        commits: 1,
        durable_writes: 1,
        replay_new_durable_writes: 0,
        replays: 1,
        result: "same persisted proposal and revision hash",
      },
    },
  ];
  assert.deepEqual(value.cases, expectedCases, "acceptance case order, closed schemas, and outcomes");
}

async function verify(directory) {
  const manifest = await readManifest(directory);
  const fixtures = Object.fromEntries(
    await Promise.all(
      fixtureNames.map(async (name) => {
        const fixture = await readCanonical(directory, name);
        assert.equal(digest(fixture.bytes), manifest.get(name), `fixture digest mismatch: ${name}`);
        return [name, fixture];
      }),
    ),
  );
  const revisionHash = `sha256:${digest(fixtures["revision-v1.canonical.json"].bytes)}`;
  const proposal = fixtures["proposal-v1.canonical.json"].value;
  const revision = fixtures["revision-v1.canonical.json"].value;
  const approval = fixtures["approval-v1.canonical.json"].value;
  verifyRecords({ proposal, revision, approval, revisionHash });
  const revisionLifecycle = fixtures["revision-lifecycle-v1.canonical.json"].value;
  verifyRevisionLifecycle(revisionLifecycle, { approval, proposal, revision, revisionHash });
  verifyStateMachine(fixtures["state-machine-v1.canonical.json"].value);
  const envelopes = verifyRequestEnvelopes(fixtures["request-envelopes-v1.canonical.json"].value, {
    approval,
    proposal,
    revision,
    revisionHash,
    revisionLifecycle,
  });
  verifyAcceptance(fixtures["acceptance-v1.canonical.json"].value, { envelopes, revisionHash });
}

async function rewriteManifest(directory) {
  const entries = await Promise.all(
    fixtureNames.map(async (name) => {
      const bytes = await readFile(join(directory, name));
      return `${hashVersion} sha256 ${digest(bytes)} ${name}`;
    }),
  );
  await writeFile(join(directory, "fixtures.sha256"), entries.join("\n"));
}

function replaceExactStrings(value, replacements) {
  if (typeof value === "string") return replacements.get(value) ?? value;
  if (Array.isArray(value)) {
    for (let index = 0; index < value.length; index += 1) value[index] = replaceExactStrings(value[index], replacements);
    return value;
  }
  if (value === null || typeof value !== "object") return value;
  for (const key of Object.keys(value)) value[key] = replaceExactStrings(value[key], replacements);
  return value;
}

async function readJsonFixture(directory, name) {
  return JSON.parse(await readFile(join(directory, name), "utf8"));
}

async function writeCanonicalFixture(directory, name, value) {
  await writeFile(join(directory, name), canonicalJson(value));
}

async function rehashTimestampFixtureLinks(directory, timestamp) {
  const [proposal, revision, approval, revisionLifecycle, envelopes, acceptance] = await Promise.all([
    readJsonFixture(directory, "proposal-v1.canonical.json"),
    readJsonFixture(directory, "revision-v1.canonical.json"),
    readJsonFixture(directory, "approval-v1.canonical.json"),
    readJsonFixture(directory, "revision-lifecycle-v1.canonical.json"),
    readJsonFixture(directory, "request-envelopes-v1.canonical.json"),
    readJsonFixture(directory, "acceptance-v1.canonical.json"),
  ]);
  const oldRevisionHash = hashCanonical(revision);
  const oldEnvelopeHashes = new Map([
    ["append", envelopes.append.body_hash],
    ["decision_approve", envelopes.decision_approve.body_hash],
    ["decision_reject", envelopes.decision_reject.body_hash],
  ]);
  revision.created_at = timestamp;
  const newRevisionHash = hashCanonical(revision);
  proposal.current_revision_hash = newRevisionHash;
  approval.revision_hash = newRevisionHash;
  revisionLifecycle.lifecycle_records[0].revision_hash = newRevisionHash;
  envelopes.append.body.expected_current_revision_hash = newRevisionHash;
  envelopes.append.body_hash = hashCanonical(envelopes.append.body);
  for (const envelope of [envelopes.decision_approve, envelopes.decision_reject]) {
    envelope.body.revision_hash = newRevisionHash;
    envelope.body_hash = hashCanonical(envelope.body);
  }
  replaceExactStrings(acceptance, new Map([
    [oldRevisionHash, newRevisionHash],
    [oldEnvelopeHashes.get("append"), envelopes.append.body_hash],
    [oldEnvelopeHashes.get("decision_approve"), envelopes.decision_approve.body_hash],
    [oldEnvelopeHashes.get("decision_reject"), envelopes.decision_reject.body_hash],
  ]));
  await Promise.all([
    writeCanonicalFixture(directory, "proposal-v1.canonical.json", proposal),
    writeCanonicalFixture(directory, "revision-v1.canonical.json", revision),
    writeCanonicalFixture(directory, "approval-v1.canonical.json", approval),
    writeCanonicalFixture(directory, "revision-lifecycle-v1.canonical.json", revisionLifecycle),
    writeCanonicalFixture(directory, "request-envelopes-v1.canonical.json", envelopes),
    writeCanonicalFixture(directory, "acceptance-v1.canonical.json", acceptance),
  ]);
  await rewriteManifest(directory);
}

function runCopiedVerifier(directory) {
  return spawnSync(process.execPath, [scriptPath, "--fixtures", directory], { encoding: "utf8" });
}

function assertRejected(result, pattern, name) {
  assert.notEqual(result.status, 0, `${name} was accepted`);
  assert.match(`${result.stdout}${result.stderr}`, pattern, `${name} rejection reason`);
}

async function selfTest() {
  const root = await mkdtemp(join(tmpdir(), "ananke-p1a-contract-"));
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
    const revisionPath = join(copiedFixtures, "revision-v1.canonical.json");
    const revisionText = await readFile(revisionPath, "utf8");
    await writeFile(revisionPath, revisionText.replace("Freeze P1a proposal contract", "Drifted proposal contract"));
    assertRejected(runCopiedVerifier(copiedFixtures), /fixture digest mismatch/, "content-drifted canonical revision");

    await resetCopiedFixtures();
    const acceptance = await readJsonFixture(copiedFixtures, "acceptance-v1.canonical.json");
    acceptance.cases[0].given.repository_root = "/private";
    await writeCanonicalFixture(copiedFixtures, "acceptance-v1.canonical.json", acceptance);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /(private field|closed schemas)/, "private acceptance field");

    await resetCopiedFixtures();
    const unicodeValue = await readJsonFixture(copiedFixtures, "revision-v1.canonical.json");
    unicodeValue.task.title = "\ud800";
    await writeCanonicalFixture(copiedFixtures, "revision-v1.canonical.json", unicodeValue);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /unpaired Unicode surrogate/, "unpaired Unicode surrogate value");

    await resetCopiedFixtures();
    const unicodeKeyAcceptance = await readJsonFixture(copiedFixtures, "acceptance-v1.canonical.json");
    unicodeKeyAcceptance.cases[0].given["\udc00"] = "invalid";
    await writeCanonicalFixture(copiedFixtures, "acceptance-v1.canonical.json", unicodeKeyAcceptance);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /unpaired Unicode surrogate/, "unpaired Unicode surrogate key");

    await resetCopiedFixtures();
    const requestPath = join(copiedFixtures, "request-envelopes-v1.canonical.json");
    const requestEnvelopes = await readJsonFixture(copiedFixtures, "request-envelopes-v1.canonical.json");
    requestEnvelopes.create.body_hash = `sha256:${digest(Buffer.from(revisionText, "utf8"))}`;
    await writeCanonicalFixture(copiedFixtures, "request-envelopes-v1.canonical.json", requestEnvelopes);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /create request body hash/, "conflated create request hash");

    await resetCopiedFixtures();
    const tamperedCreate = await readJsonFixture(copiedFixtures, "request-envelopes-v1.canonical.json");
    tamperedCreate.create.body.project_id = "project_other";
    tamperedCreate.create.body_hash = hashCanonical(tamperedCreate.create.body);
    await writeCanonicalFixture(copiedFixtures, "request-envelopes-v1.canonical.json", tamperedCreate);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /create body project target/, "rehashed create target");

    await resetCopiedFixtures();
    const tamperedDecision = await readJsonFixture(copiedFixtures, "request-envelopes-v1.canonical.json");
    tamperedDecision.decision_approve.body.revision_hash = `sha256:${"d".repeat(64)}`;
    tamperedDecision.decision_approve.body_hash = hashCanonical(tamperedDecision.decision_approve.body);
    await writeCanonicalFixture(copiedFixtures, "request-envelopes-v1.canonical.json", tamperedDecision);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /decision approve body immutable revision hash/, "rehashed decision revision hash");

    await resetCopiedFixtures();
    const missingVectorAcceptance = await readJsonFixture(copiedFixtures, "acceptance-v1.canonical.json");
    missingVectorAcceptance.cases.pop();
    await writeCanonicalFixture(copiedFixtures, "acceptance-v1.canonical.json", missingVectorAcceptance);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /acceptance case order, closed schemas, and outcomes/, "missing acceptance vector");

    for (const timestamp of ["2026-99-99T99:99:99Z", "2026-02-29T12:00:00Z", "2026-07-22T24:00:00Z"]) {
      await resetCopiedFixtures();
      await rehashTimestampFixtureLinks(copiedFixtures, timestamp);
      assertRejected(
        runCopiedVerifier(copiedFixtures),
        /revision.created_at must be a semantic UTC RFC 3339/,
        `rehashed invalid timestamp ${timestamp}`,
      );
    }
  } finally {
    await rm(root, { force: true, recursive: true });
  }
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log("P1a fixture verifier self-test rejected drift, private fields, unpaired Unicode surrogates, request-hash conflation, rehashed timestamp and envelope-identity tampering, and missing vectors.");
} else {
  await verify(resolve(optionValue("--fixtures") ?? sourceFixtureDirectory));
  console.log("P1a proposal contract fixtures verified.");
}
