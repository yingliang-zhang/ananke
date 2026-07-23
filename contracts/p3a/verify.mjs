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
  "launch-admission-v1.canonical.json",
  "adversarial-v1.canonical.json",
  "recovery-v1.canonical.json",
];
const fixtureHashVersion = "ananke-launch-admission-contract-v1";
const canonicalFixtureDigests = new Map([
  ["launch-admission-v1.canonical.json", "4e6afde3722009df0447ef95271cb72629d7ca3bff103cee15fe229a6f4bea16"],
  ["adversarial-v1.canonical.json", "93f865db428a3dd73a4b8a27509fd35d81dfe09985b72d6eade494a647a6f953"],
  ["recovery-v1.canonical.json", "f4b01e47a487918edbe820010e579d39deb3dc70c7def7a101e53e5090e85e57"],
]);
const allowFixtureDrift = process.env.ANANKE_P3A_SELF_TEST_ALLOW_FIXTURE_DRIFT === "1";
const revisionIdentity = {
  proposal_id: "proposal_p1a_001",
  revision: 1,
  revision_hash: "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263",
};
const localActor = "local_gui_operator";
const waitingForHumanToolCallID = "tool_call_p3a_001";
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const hashPattern = /^sha256:[0-9a-f]{64}$/;
const noncePattern = /^nonce:[0-9a-f]{64}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const rawAuthorityFields = new Set([
  "argv", "command", "environment", "exec", "execution", "instructions", "path", "prompt", "prose", "script", "shell", "task",
]);
function failClosedOutput(run) {
  return {
    admission: "rejected",
    evidence_written: false,
    intervention: {
      run_id: run.run_id,
      tool_call_id: waitingForHumanToolCallID,
    },
    process_started: false,
    run_state_fact: "waiting_for_human",
    terminal_fact_written: false,
  };
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
  return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalJson(value[key])}`).join(",")}}`;
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

function assertHash(value, name) {
  assert.ok(typeof value === "string" && hashPattern.test(value), `${name} must be a SHA-256 hash`);
}

function assertNonce(value, name) {
  assert.ok(typeof value === "string" && noncePattern.test(value), `${name} must be an opaque nonce`);
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

function assertRevisionIdentity(value, name) {
  expectKeys(value, ["proposal_id", "revision", "revision_hash"], name);
  assertIdentifier(value.proposal_id, `${name}.proposal_id`);
  assertPositiveInteger(value.revision, `${name}.revision`);
  assertHash(value.revision_hash, `${name}.revision_hash`);
  assert.deepEqual(value, revisionIdentity, `${name} must bind the frozen P1 Revision tuple and hash`);
}

function assertNoRawAuthority(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoRawAuthority(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    assert.ok(!rawAuthorityFields.has(key), `forbidden raw authority field ${path}.${key}`);
    assertNoRawAuthority(entry, `${path}.${key}`);
  }
}

function assertApprovalEligibility(value) {
  expectKeys(value, ["approval_id", "approved_at", "approved_by", "proposal_id", "revision", "revision_hash", "state"], "approval eligibility");
  assertIdentifier(value.approval_id, "approval eligibility.approval_id");
  assertRevisionIdentity({
    proposal_id: value.proposal_id,
    revision: value.revision,
    revision_hash: value.revision_hash,
  }, "approval eligibility revision");
  assertTimestamp(value.approved_at, "approval eligibility.approved_at");
  assert.equal(value.approved_by, localActor, "approval eligibility requires local operator approval");
  assert.equal(value.state, "approved", "approval eligibility state");
}

function assertHostSpec(value) {
  expectKeys(value, ["capabilities", "executable_route_fingerprint", "host_spec_fingerprint", "required_files_fingerprint", "transcript_source_fingerprint", "worktree_layout_fingerprint"], "HostSpec");
  assert.deepEqual(value.capabilities, [
    "bounded_cancellation",
    "read_only_retrieval",
    "reconnect_recovery",
    "shape_only_transcript",
    "verification",
  ], "HostSpec capability inventory");
  const { host_spec_fingerprint, ...fingerprintedHostSpec } = value;
  assertHash(host_spec_fingerprint, "HostSpec.host_spec_fingerprint");
  assert.equal(host_spec_fingerprint, hashCanonical(fingerprintedHostSpec), "HostSpec canonical fingerprint binding");
  for (const key of ["executable_route_fingerprint", "required_files_fingerprint", "transcript_source_fingerprint", "worktree_layout_fingerprint"]) {
    assertHash(value[key], `HostSpec.${key}`);
  }
}

function assertLaunchSpec(value) {
  expectKeys(value, ["attempt_cap", "deadline", "host_spec", "model", "read_only_scope", "revision", "schema_version", "sealed_contract", "transcript", "verification"], "launch spec");
  assert.equal(value.schema_version, "ananke.launch-spec.v1", "launch spec schema version");
  assertRevisionIdentity(value.revision, "launch spec revision");
  assertTimestamp(value.deadline, "launch spec deadline");
  assert.ok(Number.isInteger(value.attempt_cap) && value.attempt_cap >= 1 && value.attempt_cap <= 100, "launch spec attempt cap must be 1 through 100");

  expectKeys(value.model, ["model", "provider"], "launch spec model");
  assertIdentifier(value.model.provider, "launch spec provider");
  assertIdentifier(value.model.model, "launch spec model");

  expectKeys(value.read_only_scope, ["access", "retrieval", "scope_fingerprint", "writes"], "read-only scope");
  assert.deepEqual(value.read_only_scope, {
    access: "read_only",
    retrieval: "sealed_contract_only",
    scope_fingerprint: value.read_only_scope.scope_fingerprint,
    writes: "forbidden",
  }, "read-only scope values");
  assertHash(value.read_only_scope.scope_fingerprint, "read-only scope fingerprint");

  expectKeys(value.sealed_contract, ["materialization_hash", "nonce"], "sealed contract");
  assertHash(value.sealed_contract.materialization_hash, "sealed contract materialization hash");
  assertNonce(value.sealed_contract.nonce, "sealed contract nonce");

  assertHostSpec(value.host_spec);

  expectKeys(value.transcript, ["dialect", "dialect_fingerprint", "parse"], "transcript declaration");
  assert.equal(value.transcript.dialect, "omp_shape_v1", "transcript dialect");
  assertHash(value.transcript.dialect_fingerprint, "transcript dialect fingerprint");
  assert.equal(value.transcript.parse, "shape_only", "transcript parse mode");

  expectKeys(value.verification, ["kind", "verification_command_fingerprint"], "verification declaration");
  assert.equal(value.verification.kind, "read_only", "verification declaration is read-only");
  assertHash(value.verification.verification_command_fingerprint, "verification command fingerprint");
}

function assertTaskClaim(value, launchSpecHash) {
  expectKeys(value, ["attempt", "claim_id", "claim_token_hash", "fence_generation", "launch_spec_hash", "owner_id", "state"], "task claim");
  assertPositiveInteger(value.attempt, "task claim attempt");
  assertIdentifier(value.claim_id, "task claim id");
  assertHash(value.claim_token_hash, "task claim token hash");
  assertPositiveInteger(value.fence_generation, "task claim fence generation");
  assert.equal(value.launch_spec_hash, launchSpecHash, "task claim launch spec binding");
  assertIdentifier(value.owner_id, "task claim owner");
  assert.equal(value.state, "active", "task claim state");
}

function assertFencedProjection(value, expectedKeys, name, claim, launchSpecHash) {
  expectKeys(value, expectedKeys, name);
  assert.equal(value.claim_id, claim.claim_id, `${name} claim binding`);
  assert.equal(value.claim_token_hash, claim.claim_token_hash, `${name} token binding`);
  assert.equal(value.fence_generation, claim.fence_generation, `${name} fence generation binding`);
  assert.equal(value.launch_spec_hash, launchSpecHash, `${name} launch spec binding`);
}

function staleTokenOutcome(given, claim) {
  assert.equal(given.claim_id, claim.claim_id, "stale token claim binding");
  assertHash(given.claim_token_hash, "stale token hash");
  assertPositiveInteger(given.fence_generation, "stale token fence generation");
  assert.ok(
    given.claim_token_hash !== claim.claim_token_hash || given.fence_generation !== claim.fence_generation,
    "stale authority must differ from the active token/fence tuple",
  );
  return {
    evidence_written: false,
    outcome: "rejected_stale_token",
    run_created: false,
    terminal_fact_written: false,
  };
}

function verifyLaunchAdmission(value) {
  assertNoRawAuthority(value);
  expectKeys(value, ["admission", "launch_outbox", "materialization", "run", "schema_version", "task_claim", "token_fence_cases"], "launch admission fixture");
  assert.equal(value.schema_version, "ananke.launch-admission.fixture.v1", "launch admission fixture schema version");

  expectKeys(value.admission, ["approval_eligibility", "launch_spec", "launch_spec_hash"], "admission");
  assertApprovalEligibility(value.admission.approval_eligibility);
  assertLaunchSpec(value.admission.launch_spec);
  assertHash(value.admission.launch_spec_hash, "admission launch spec hash");
  assert.equal(value.admission.launch_spec_hash, hashCanonical(value.admission.launch_spec), "admission launch spec canonical hash");

  const launchSpecHash = value.admission.launch_spec_hash;
  const claim = value.task_claim;
  assertTaskClaim(claim, launchSpecHash);

  assertFencedProjection(
    value.materialization,
    ["claim_id", "claim_token_hash", "fence_generation", "launch_spec_hash", "materialization_hash", "materialization_id", "nonce", "state"],
    "materialization",
    claim,
    launchSpecHash,
  );
  assertIdentifier(value.materialization.materialization_id, "materialization id");
  assertHash(value.materialization.materialization_hash, "materialization hash");
  assertNonce(value.materialization.nonce, "materialization nonce");
  assert.equal(value.materialization.state, "ready", "materialization state");
  assert.equal(value.materialization.materialization_hash, value.admission.launch_spec.sealed_contract.materialization_hash, "materialization hash binding");
  assert.equal(value.materialization.nonce, value.admission.launch_spec.sealed_contract.nonce, "materialization nonce binding");

  assertFencedProjection(
    value.launch_outbox,
    ["claim_id", "claim_token_hash", "fence_generation", "launch_spec_hash", "outbox_id", "state"],
    "launch outbox",
    claim,
    launchSpecHash,
  );
  assertIdentifier(value.launch_outbox.outbox_id, "launch outbox id");
  assert.equal(value.launch_outbox.state, "pending_process_admission", "launch outbox state");

  expectKeys(value.run, ["attempt", "claim_id", "claim_token_hash", "fence_generation", "launch_spec_hash", "materialization_id", "run_id", "state_fact"], "Run");
  assert.equal(value.run.attempt, claim.attempt, "Run attempt binding");
  assert.equal(value.run.claim_id, claim.claim_id, "Run claim binding");
  assert.equal(value.run.claim_token_hash, claim.claim_token_hash, "Run token binding");
  assert.equal(value.run.fence_generation, claim.fence_generation, "Run fence generation binding");
  assert.equal(value.run.launch_spec_hash, launchSpecHash, "Run launch spec binding");
  assert.equal(value.run.materialization_id, value.materialization.materialization_id, "Run materialization binding");
  assertIdentifier(value.run.run_id, "Run id");
  expectKeys(value.run.state_fact, ["kind", "sequence", "token_hash"], "Run state fact");
  assert.equal(value.run.state_fact.kind, "created", "Run state fact kind");
  assert.equal(value.run.state_fact.sequence, 1, "Run state fact sequence");
  assert.equal(value.run.state_fact.token_hash, claim.claim_token_hash, "Run state fact token ownership");

  assert.ok(Array.isArray(value.token_fence_cases) && value.token_fence_cases.length === 6, "token fence case inventory");
  const expectedTokenFenceCases = [
    ["same_generation_different_token_cannot_create_run", "create_run"],
    ["lower_generation_cannot_create_run", "create_run"],
    ["same_generation_different_token_cannot_append_terminal_fact", "append_terminal_fact"],
    ["lower_generation_cannot_append_terminal_fact", "append_terminal_fact"],
    ["same_generation_different_token_cannot_settle_evidence", "settle_evidence"],
    ["lower_generation_cannot_settle_evidence", "settle_evidence"],
  ];
  value.token_fence_cases.forEach((testCase, index) => {
    expectKeys(testCase, ["given", "id", "operation", "then"], `token fence case ${index + 1}`);
    assert.deepEqual([testCase.id, testCase.operation], expectedTokenFenceCases[index], `token fence case ${index + 1} identity`);
    expectKeys(testCase.given, ["claim_id", "claim_token_hash", "fence_generation"], `token fence case ${index + 1} given`);
    assert.deepEqual(testCase.then, staleTokenOutcome(testCase.given, claim), `token fence case ${testCase.id} outcome`);
  });
  return {
    claim,
    launchSpecHash,
    launchSpec: value.admission.launch_spec,
    materialization: value.materialization,
    run: value.run,
  };
}

function failClosedOutcome(kind, given, launchSpec, run) {
  switch (kind) {
    case "unknown_launch_input":
      expectKeys(given, ["unknown_field"], "unknown launch input");
      assert.ok(["command", "prompt"].includes(given.unknown_field), "unknown launch input field");
      break;
    case "unverified_materialization":
      expectKeys(given, ["materialization_hash"], "unverified materialization");
      assertHash(given.materialization_hash, "unverified materialization hash");
      assert.notEqual(given.materialization_hash, launchSpec.sealed_contract.materialization_hash, "unverified materialization must not match sealed hash");
      break;
    case "missing_required_field":
      expectKeys(given, ["missing_field"], "missing required launch field");
      assert.ok(["provider", "model", "deadline", "attempt_cap"].includes(given.missing_field), "missing required launch field identity");
      break;
    case "non_read_only_scope":
      expectKeys(given, ["access"], "non-read-only scope");
      assert.notEqual(given.access, "read_only", "non-read-only scope must be rejected");
      break;
    case "unknown_transcript_dialect":
      expectKeys(given, ["dialect"], "unknown transcript dialect");
      assert.notEqual(given.dialect, launchSpec.transcript.dialect, "unknown transcript dialect must differ from HostSpec dialect");
      break;
    case "unknown_transcript_event":
      expectKeys(given, ["event_shape"], "unknown transcript event");
      assert.equal(given.event_shape, "unrecognized", "unknown transcript event shape");
      break;
    default:
      throw new Error(`unsupported fail-closed case ${kind}`);
  }
  return failClosedOutput(run);
}

function verifyAdversarial(value, launch) {
  assertNoRawAuthority(value);
  expectKeys(value, ["cases", "schema_version"], "adversarial fixture");
  assert.equal(value.schema_version, "ananke.launch-admission.adversarial.v1", "adversarial fixture schema version");
  assert.ok(Array.isArray(value.cases) && value.cases.length === 10, "adversarial case inventory");
  const expected = [
    ["unknown_command_input_waits_for_human", "unknown_launch_input"],
    ["unknown_prompt_input_waits_for_human", "unknown_launch_input"],
    ["unverified_materialization_waits_for_human", "unverified_materialization"],
    ["missing_provider_waits_for_human", "missing_required_field"],
    ["missing_model_waits_for_human", "missing_required_field"],
    ["missing_deadline_waits_for_human", "missing_required_field"],
    ["missing_attempt_cap_waits_for_human", "missing_required_field"],
    ["non_read_only_scope_waits_for_human", "non_read_only_scope"],
    ["unknown_transcript_dialect_waits_for_human", "unknown_transcript_dialect"],
    ["unknown_transcript_event_waits_for_human", "unknown_transcript_event"],
  ];
  value.cases.forEach((testCase, index) => {
    expectKeys(testCase, ["given", "id", "kind", "then"], `adversarial case ${index + 1}`);
    assert.deepEqual([testCase.id, testCase.kind], expected[index], `adversarial case ${index + 1} identity`);
    assert.deepEqual(testCase.then, failClosedOutcome(testCase.kind, testCase.given, launch.launchSpec, launch.run), `fail-closed outcome ${testCase.id}`);
  });
}

function recoveryOutcome(id, facts, launch) {
  const { claim, launchSpecHash, materialization, run } = launch;
  expectKeys(facts, ["claim", "launch_spec_hash", "materialization", "outbox", "process", "run"], `recovery ${id} facts`);
  expectKeys(facts.claim, ["claim_id", "claim_token_hash", "fence_generation"], `recovery ${id} claim`);
  assert.deepEqual(facts.claim, {
    claim_id: claim.claim_id,
    claim_token_hash: claim.claim_token_hash,
    fence_generation: claim.fence_generation,
  }, `recovery ${id} current token ownership`);
  assert.equal(facts.launch_spec_hash, launchSpecHash, `recovery ${id} launch spec binding`);

  const expectedFacts = {
    claim_before_materialization: { materialization: "absent", outbox: "pending_materialization", process: "not_created", run: "absent" },
    materialization_before_run: { materialization: "ready", outbox: "pending_run_admission", process: "not_created", run: "absent" },
    run_before_process: { materialization: "ready", outbox: "pending_process_admission", process: "not_started", run: "created" },
  };
  const expected = expectedFacts[id];
  assert.ok(expected, `unsupported recovery boundary ${id}`);
  assert.equal(facts.outbox, expected.outbox, `recovery ${id} outbox boundary`);
  assert.equal(facts.process, expected.process, `recovery ${id} process boundary`);

  expectKeys(facts.materialization, ["materialization_hash", "materialization_id", "nonce", "state"], `recovery ${id} materialization`);
  if (expected.materialization === "absent") {
    assert.deepEqual(facts.materialization, {
      materialization_hash: null,
      materialization_id: null,
      nonce: null,
      state: "absent",
    }, `recovery ${id} materialization absence`);
  } else {
    assert.deepEqual(facts.materialization, {
      materialization_hash: materialization.materialization_hash,
      materialization_id: materialization.materialization_id,
      nonce: materialization.nonce,
      state: "ready",
    }, `recovery ${id} materialization identity`);
  }

  expectKeys(facts.run, ["materialization_id", "run_id", "state", "state_fact"], `recovery ${id} Run`);
  if (expected.run === "absent") {
    assert.deepEqual(facts.run, {
      materialization_id: null,
      run_id: null,
      state: "absent",
      state_fact: null,
    }, `recovery ${id} Run absence`);
  } else {
    assert.deepEqual(facts.run, {
      materialization_id: run.materialization_id,
      run_id: run.run_id,
      state: "created",
      state_fact: {
        kind: "created",
        sequence: 1,
        token_hash: claim.claim_token_hash,
      },
    }, `recovery ${id} Run identity`);
  }

  const actions = {
    claim_before_materialization: "retry_materialization",
    materialization_before_run: "retry_run_admission",
    run_before_process: "retry_process_admission",
  };
  return {
    action: actions[id],
    evidence_state: "unsettled",
    process_state: facts.process,
    run_state: expected.run,
    terminal_fact: "absent",
  };
}

function verifyRecovery(value, launch) {
  assertNoRawAuthority(value);
  expectKeys(value, ["cases", "schema_version"], "recovery fixture");
  assert.equal(value.schema_version, "ananke.launch-admission.recovery.v1", "recovery fixture schema version");
  assert.ok(Array.isArray(value.cases) && value.cases.length === 3, "recovery case inventory");
  const expectedIDs = ["claim_before_materialization", "materialization_before_run", "run_before_process"];
  value.cases.forEach((testCase, index) => {
    expectKeys(testCase, ["facts", "id", "then"], `recovery case ${index + 1}`);
    assert.equal(testCase.id, expectedIDs[index], `recovery case ${index + 1} identity`);
    assert.deepEqual(testCase.then, recoveryOutcome(testCase.id, testCase.facts, launch), `recovery ${testCase.id} outcome`);
  });
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

async function verify(directory) {
  const manifest = await readManifest(directory);
  const fixtures = Object.fromEntries(await Promise.all(fixtureNames.map(async (name) => [name, await readCanonical(directory, name, manifest)])));
  const launch = verifyLaunchAdmission(fixtures["launch-admission-v1.canonical.json"]);
  verifyAdversarial(fixtures["adversarial-v1.canonical.json"], launch);
  verifyRecovery(fixtures["recovery-v1.canonical.json"], launch);
}

async function rewriteManifest(directory) {
  const entries = await Promise.all(fixtureNames.map(async (name) => `${fixtureHashVersion} sha256 ${digest(await readFile(join(directory, name)))} ${name}`));
  await writeFile(join(directory, "fixtures.sha256"), entries.join("\n"));
}

async function readJSON(directory, name) {
  return JSON.parse(await readFile(join(directory, name), "utf8"));
}

async function writeCanonical(directory, name, value) {
  await writeFile(join(directory, name), canonicalJson(value));
}

function runCopiedVerifier(directory, { allowDrift = false } = {}) {
  return spawnSync(process.execPath, [scriptPath, "--fixtures", directory], {
    encoding: "utf8",
    env: { ...process.env, ANANKE_P3A_SELF_TEST_ALLOW_FIXTURE_DRIFT: allowDrift ? "1" : "0" },
  });
}

function assertRejected(result, pattern, name) {
  assert.notEqual(result.status, 0, `${name} was accepted`);
  assert.match(`${result.stdout}${result.stderr}`, pattern, `${name} rejection reason`);
}

async function selfTest() {
  const root = await mkdtemp(join(tmpdir(), "ananke-p3a-contract-"));
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
    const drifted = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    drifted.admission.launch_spec.attempt_cap = 4;
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", drifted);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures), /canonical fixture digest mismatch/, "consistently rehashed launch spec drift");

    for (const field of ["command", "prompt"]) {
      await resetCopiedFixtures();
      const injected = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
      injected.admission.launch_spec[field] = "forbidden";
      await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", injected);
      await rewriteManifest(copiedFixtures);
      assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), new RegExp(`launch spec fields|forbidden raw authority field`), `${field} authority injection`);
    }

    await resetCopiedFixtures();
    const wrongRevision = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    wrongRevision.admission.launch_spec.revision.revision_hash = `sha256:${"1".repeat(64)}`;
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", wrongRevision);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /frozen P1 Revision tuple and hash/, "P1 revision identity mismatch");

    await resetCopiedFixtures();
    const wrongApproval = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    wrongApproval.admission.approval_eligibility.approved_by = "adapter_actor";
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", wrongApproval);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /local operator approval/, "non-local approval eligibility");

    await resetCopiedFixtures();
    const swappedApproval = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    swappedApproval.admission.approval_eligibility.approval_id = "approval_p1a_999";
    swappedApproval.admission.approval_eligibility.proposal_id = "proposal_p1a_999";
    swappedApproval.admission.approval_eligibility.revision = 2;
    swappedApproval.admission.approval_eligibility.revision_hash = `sha256:${"9".repeat(64)}`;
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", swappedApproval);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /approval eligibility revision must bind the frozen P1 Revision tuple and hash/, "swapped valid approval identity/tuple");

    await resetCopiedFixtures();
    const wrongTranscriptSource = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    wrongTranscriptSource.admission.launch_spec.host_spec.transcript_source_fingerprint = `sha256:${"8".repeat(64)}`;
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", wrongTranscriptSource);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /HostSpec canonical fingerprint binding/, "transcript source fingerprint binding");

    await resetCopiedFixtures();
    const rawTranscriptSource = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    rawTranscriptSource.admission.launch_spec.host_spec.path = "/private/transcript";
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", rawTranscriptSource);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /forbidden raw authority field/, "raw transcript source path");

    await resetCopiedFixtures();
    const wrongMaterialization = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    wrongMaterialization.materialization.materialization_hash = `sha256:${"2".repeat(64)}`;
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", wrongMaterialization);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /materialization hash binding/, "unverified materialization binding");

    await resetCopiedFixtures();
    const writableScope = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
    writableScope.admission.launch_spec.read_only_scope.access = "write";
    await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", writableScope);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /read-only scope values/, "non-read-only scope");

    for (const [index, name] of [
      [0, "same-generation different-token stale authority"],
      [1, "lower-generation stale authority"],
    ]) {
      await resetCopiedFixtures();
      const currentToken = await readJSON(copiedFixtures, "launch-admission-v1.canonical.json");
      currentToken.token_fence_cases[index].given.claim_token_hash = currentToken.task_claim.claim_token_hash;
      currentToken.token_fence_cases[index].given.fence_generation = currentToken.task_claim.fence_generation;
      await writeCanonical(copiedFixtures, "launch-admission-v1.canonical.json", currentToken);
      await rewriteManifest(copiedFixtures);
      assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /stale authority must differ from the active token\/fence tuple/, name);
    }

    await resetCopiedFixtures();
    const inferredSuccess = await readJSON(copiedFixtures, "adversarial-v1.canonical.json");
    inferredSuccess.cases[8].then.admission = "accepted";
    await writeCanonical(copiedFixtures, "adversarial-v1.canonical.json", inferredSuccess);
    await rewriteManifest(copiedFixtures);
    assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /fail-closed outcome/, "unknown dialect success inference");

    for (const [field, value] of [
      ["run_id", "run_p3a_999"],
      ["tool_call_id", "tool_call_p3a_999"],
    ]) {
      await resetCopiedFixtures();
      const swappedIntervention = await readJSON(copiedFixtures, "adversarial-v1.canonical.json");
      swappedIntervention.cases[0].then.intervention[field] = value;
      await writeCanonical(copiedFixtures, "adversarial-v1.canonical.json", swappedIntervention);
      await rewriteManifest(copiedFixtures);
      assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /fail-closed outcome/, `waiting_for_human intervention ${field} binding`);
    }

    const recoveryIdentitySwaps = [
      ["materialization id", (recovery) => { recovery.cases[1].facts.materialization.materialization_id = "materialization_p3a_999"; }, /recovery materialization_before_run materialization identity/],
      ["materialization hash", (recovery) => { recovery.cases[1].facts.materialization.materialization_hash = `sha256:${"9".repeat(64)}`; }, /recovery materialization_before_run materialization identity/],
      ["materialization nonce", (recovery) => { recovery.cases[1].facts.materialization.nonce = `nonce:${"9".repeat(64)}`; }, /recovery materialization_before_run materialization identity/],
      ["Run id", (recovery) => { recovery.cases[2].facts.run.run_id = "run_p3a_999"; }, /recovery run_before_process Run identity/],
      ["Run materialization reference", (recovery) => { recovery.cases[2].facts.run.materialization_id = "materialization_p3a_999"; }, /recovery run_before_process Run identity/],
      ["Run created fact token", (recovery) => { recovery.cases[2].facts.run.state_fact.token_hash = `sha256:${"9".repeat(64)}`; }, /recovery run_before_process Run identity/],
    ];
    for (const [name, swap, pattern] of recoveryIdentitySwaps) {
      await resetCopiedFixtures();
      const recoveryIdentity = await readJSON(copiedFixtures, "recovery-v1.canonical.json");
      swap(recoveryIdentity);
      await writeCanonical(copiedFixtures, "recovery-v1.canonical.json", recoveryIdentity);
      await rewriteManifest(copiedFixtures);
      assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), pattern, `recovery ${name} identity swap`);
    }

    for (const [field, value] of [
      ["terminal_fact", "completed"],
      ["evidence_state", "settled"],
      ["process_state", "started"],
    ]) {
      await resetCopiedFixtures();
      const recoveryGuess = await readJSON(copiedFixtures, "recovery-v1.canonical.json");
      recoveryGuess.cases[2].then[field] = value;
      await writeCanonical(copiedFixtures, "recovery-v1.canonical.json", recoveryGuess);
      await rewriteManifest(copiedFixtures);
      assertRejected(runCopiedVerifier(copiedFixtures, { allowDrift: true }), /recovery run_before_process outcome/, `recovery ${field} guess`);
    }
  } finally {
    await rm(root, { force: true, recursive: true });
  }
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log("P3a fenced launch-admission self-test rejected launch-spec drift, raw command/prompt/transcript-path authority, P1 identity, approval tuple, transcript source, sealed materialization, writable scope, same-generation and lower-generation stale authorities, intervention swaps, recovery identity swaps, and terminal/evidence/process guesses.");
} else {
  await verify(resolve(optionValue("--fixtures") ?? sourceFixtureDirectory));
  console.log("P3a fenced launch-admission fixtures verified: immutable P1-bound read-only launch spec and approval eligibility, sealed materialization, canonical HostSpec/transcript source/verification fingerprints, fenced projections, tuple-mismatch stale denial, run/tool-call-bound fail-closed interventions, and exact crash-recovery identities.");
}
