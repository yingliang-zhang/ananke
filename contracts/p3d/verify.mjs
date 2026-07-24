import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const sourceFixtureDirectory = resolve(dirname(scriptPath), "fixtures");
const fixtureNames = ["omp-audit-v1.canonical.json", "adversarial-v1.canonical.json", "crash-v1.canonical.json"];
const fixtureHashVersion = "ananke-omp-readonly-audit-contract-v1";
const canonicalFixtureDigests = new Map([
  ["omp-audit-v1.canonical.json", "9c8ca561416c82f98ad49d08c625bb5b11be468fb306cd254e7700468ac0e7f3"],
  ["adversarial-v1.canonical.json", "12e2fa336c0f374859eec7cb5a5311bc660df4d36e6b1c8671c575e0d6e2bab8"],
  ["crash-v1.canonical.json", "e81798ad7aef51980a0a62c2c3ebfd9de7ca714b2698c52c4b3bf9bc29c4254c"],
]);
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const hashPattern = /^sha256:[0-9a-f]{64}$/;
const noncePattern = /^nonce:[0-9a-f]{64}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;

const p3aBinding = {
  attempt: 1,
  fence_fingerprint: "sha256:9a4f8c7e6d5b3a2910f1e2d3c4b5a697887766554433221100ffeeddccbbaa99",
  launch_spec_hash: "sha256:bbc43093a3b00c49c1d2ac26db08e6dd36ff72174ded15de9408702af3a9e658",
  materialization_id: "materialization_p3a_001",
  p3c_action: "retry_process_admission",
  run_id: "run_p3a_001",
};
const sealedMaterialization = {
  materialization_hash: "sha256:27f6f25e5a3cd790f634a3541d60d5681fa23c5d4a19c1b294ea70e168363ef7",
  nonce: "nonce:034836706f8a359785406c36188f90edb94522896c44e3f000e9eede2d658f29",
  payload_hash: "sha256:8294e0b7c8d3a5f2e1b0c9d8f7a6e5d4c3b2a190876543210fedcba987654321",
  seal_fingerprint: "sha256:d50cec5aada78a1c4797b5071ffbf84cbebbfc4d9ca032cc5de56bb029315b0a",
};
const trustedTarget = {
  repository_identity: "github.com/yingliang-zhang/ananke",
  required_source_snapshot_hash: "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4",
  root_identity_fingerprint: "sha256:0876d8d61df302e652ee9a9b1c2c4d6e8f0123456789abcdef0123456789abcd",
  target_kind: "canonical_ananke_repository",
};
const transcript = {
  input_dialect: "omp_audit_stream_v1",
  normalization: "known_omp_events_to_ananke_audit_v1",
  output_dialect: "ananke_omp_audit_event_v1",
  source: "omp_readonly_wrapper_transcript_v1",
  source_fingerprint: "sha256:4329a8b7c6d5e4f30123456789abcdef0123456789abcdef0123456789abcdef",
};
const normalizedEvents = [
  { event_id: "omp_audit_event_p3d_001", kind: "audit_started", sequence: 1 },
  { event_id: "omp_audit_event_p3d_002", kind: "audit_finding", sequence: 2 },
  { event_id: "omp_audit_event_p3d_003", kind: "audit_completed", sequence: 3 },
];
const failClosedResult = { adapter_state: "waiting_for_human", events: [], result: null, verification_state: "not_run" };
const rawAuthorityFragments = [
  "argv", "command", "credential", "environment", "error", "exec", "execution", "instruction", "password", "path", "pid", "prompt", "prose", "raw", "script", "secret", "shell", "socket", "task", "token",
];

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

function normalizeField(key) {
  return key.toLowerCase().replace(/[^a-z0-9]/g, "");
}

function allowedSensitiveFingerprint(key, path) {
  return key === "command_fingerprint" && path === "$.host_spec.verification";
}

function isRawAuthorityField(key) {
  if (key === "transcript") return false;
  const normalized = normalizeField(key);
  return rawAuthorityFragments.some((fragment) => normalized === fragment || normalized.startsWith(fragment) || normalized.endsWith(fragment));
}

function assertNoRawAuthority(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoRawAuthority(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    assert.ok(
      allowedSensitiveFingerprint(key, path) || !isRawAuthorityField(key),
      `forbidden raw/public authority field ${path}.${key}`,
    );
    assertNoRawAuthority(entry, `${path}.${key}`);
  }
}

function assertSealedMaterialization(value, name) {
  expectKeys(value, ["materialization_hash", "nonce", "payload_hash", "seal_fingerprint"], name);
  assertHash(value.materialization_hash, `${name}.materialization_hash`);
  assertNonce(value.nonce, `${name}.nonce`);
  assertHash(value.payload_hash, `${name}.payload_hash`);
  assertHash(value.seal_fingerprint, `${name}.seal_fingerprint`);
  assert.equal(value.seal_fingerprint, hashCanonical({
    materialization_hash: value.materialization_hash,
    nonce: value.nonce,
    payload_hash: value.payload_hash,
  }), `${name} canonical seal binding`);
  assert.deepEqual(value, sealedMaterialization, `${name} must bind the P3a sealed materialization`);
}

function assertTrustedTarget(value, name) {
  expectKeys(value, ["repository_identity", "required_source_snapshot_hash", "root_identity_fingerprint", "target_kind"], name);
  assertHash(value.required_source_snapshot_hash, `${name}.required_source_snapshot_hash`);
  assertHash(value.root_identity_fingerprint, `${name}.root_identity_fingerprint`);
  assert.deepEqual(value, trustedTarget, `${name} must bind the canonical Ananke repository identity and source snapshot`);
}

function assertTranscript(value, name) {
  expectKeys(value, ["input_dialect", "normalization", "output_dialect", "source", "source_fingerprint"], name);
  assertHash(value.source_fingerprint, `${name}.source_fingerprint`);
  assert.deepEqual(value, transcript, `${name} source and dialect normalization`);
}

function assertHostSpec(value) {
  expectKeys(value, ["adapter", "attempt_cap", "capabilities", "deadline", "host_spec_fingerprint", "model", "read_only_capability", "schema_version", "sealed_materialization", "target", "transcript", "verification"], "HostSpec");
  assert.equal(value.schema_version, "ananke.omp-readonly-host-spec.v1", "HostSpec schema version");
  expectKeys(value.adapter, ["route", "wrapper_kind"], "HostSpec adapter");
  assert.deepEqual(value.adapter, {
    route: "ananke_omp_read_only_audit_v1",
    wrapper_kind: "ananke_omp_readonly_wrapper_v1",
  }, "HostSpec adapter route-aware wrapper");
  assert.notEqual(value.adapter.wrapper_kind, "omp", "HostSpec forbids a bare omp wrapper");
  expectKeys(value.model, ["model", "provider"], "HostSpec model");
  assert.deepEqual(value.model, { model: "omp_audit_model_v1", provider: "omp" }, "HostSpec allowed provider/model");
  assertTimestamp(value.deadline, "HostSpec deadline");
  assert.equal(value.deadline, "2026-07-30T12:00:00Z", "HostSpec P3a deadline binding");
  assert.equal(value.attempt_cap, 3, "HostSpec P3a attempt cap binding");
  assert.deepEqual(value.capabilities, ["bounded_cancellation", "read_only_audit", "reconnect_recovery", "transcript_normalization", "verification"], "HostSpec capability inventory");
  expectKeys(value.read_only_capability, ["access", "materialization", "writes"], "HostSpec read-only capability");
  assert.deepEqual(value.read_only_capability, { access: "read_only", materialization: "sealed_payload_only", writes: "forbidden" }, "HostSpec read-only capability values");
  assertSealedMaterialization(value.sealed_materialization, "HostSpec sealed materialization");
  assertTrustedTarget(value.target, "HostSpec trusted target");
  assertTranscript(value.transcript, "HostSpec transcript");
  expectKeys(value.verification, ["command_fingerprint", "mode", "name"], "HostSpec verification");
  assertHash(value.verification.command_fingerprint, "HostSpec verification command fingerprint");
  assert.deepEqual(value.verification, {
    command_fingerprint: "sha256:54a6b8c0d2e4f60123456789abcdef0123456789abcdef0123456789abcdef01",
    mode: "read_only",
    name: "ananke_contract_verify_v1",
  }, "HostSpec verification command binding");
  const { host_spec_fingerprint, ...fingerprintedHostSpec } = value;
  assertHash(host_spec_fingerprint, "HostSpec fingerprint");
  assert.equal(host_spec_fingerprint, hashCanonical(fingerprintedHostSpec), "HostSpec canonical fingerprint binding");
  return host_spec_fingerprint;
}

function assertAuditRequest(value, hostSpecHash) {
  expectKeys(value, ["attempt_cap", "deadline", "host_spec_hash", "launch_binding", "request_id", "schema_version", "sealed_materialization", "target"], "audit request");
  assert.equal(value.schema_version, "ananke.omp-audit-request.v1", "audit request schema version");
  assertIdentifier(value.request_id, "audit request id");
  assertHash(value.host_spec_hash, "audit request HostSpec hash");
  assert.equal(value.host_spec_hash, hostSpecHash, "audit request HostSpec binding");
  assertTimestamp(value.deadline, "audit request deadline");
  assert.equal(value.deadline, "2026-07-30T12:00:00Z", "audit request immutable deadline");
  assert.equal(value.attempt_cap, 3, "audit request immutable attempt cap");
  expectKeys(value.launch_binding, ["attempt", "fence_fingerprint", "launch_spec_hash", "materialization_id", "p3c_action", "run_id"], "audit request P3a/P3b/P3c binding");
  assertHash(value.launch_binding.fence_fingerprint, "audit request fence fingerprint");
  assertHash(value.launch_binding.launch_spec_hash, "audit request launch spec hash");
  assertIdentifier(value.launch_binding.materialization_id, "audit request materialization id");
  assertIdentifier(value.launch_binding.run_id, "audit request Run id");
  assert.deepEqual(value.launch_binding, p3aBinding, "audit request P3a/P3b/P3c binding");
  assertSealedMaterialization(value.sealed_materialization, "audit request sealed materialization");
  assertTrustedTarget(value.target, "audit request trusted target");
}

function assertNormalizedEvent(value, index, name) {
  expectKeys(value, ["event_id", "kind", "sequence"], name);
  assertIdentifier(value.event_id, `${name}.event_id`);
  assert.ok(Number.isInteger(value.sequence) && value.sequence > 0 && value.sequence <= 3, `${name}.sequence bounded`);
  assert.deepEqual(value, normalizedEvents[index], `${name} known normalized event`);
}

function assertKnownEventPrefix(value, count, name) {
  assert.ok(Array.isArray(value) && value.length === count, `${name} event inventory`);
  value.forEach((event, index) => assertNormalizedEvent(event, index, `${name}[${index}]`));
}

function assertAuditResult(value, requestID) {
  expectKeys(value, ["event_count", "finding_summary", "request_id", "schema_version", "state", "verification_state"], "audit result");
  assert.equal(value.schema_version, "ananke.omp-audit-result.v1", "audit result schema version");
  assert.equal(value.request_id, requestID, "audit result request binding");
  assert.equal(value.event_count, 3, "audit result bounded event count");
  expectKeys(value.finding_summary, ["advisory", "blocking"], "audit result finding summary");
  assert.deepEqual(value.finding_summary, { advisory: 1, blocking: 0 }, "audit result bounded finding summary");
  assert.equal(value.state, "completed", "audit result state derives only from known terminal event");
  assert.equal(value.verification_state, "not_run", "audit result does not execute verification");
}

function verifyCanonicalFixture(value) {
  assertNoRawAuthority(value);
  expectKeys(value, ["audit_request", "audit_result", "host_spec", "host_spec_hash", "normalized_events", "schema_version"], "canonical P3d fixture");
  assert.equal(value.schema_version, "ananke.omp-readonly-audit.fixture.v1", "canonical fixture schema version");
  const hostSpecHash = assertHostSpec(value.host_spec);
  assertHash(value.host_spec_hash, "canonical fixture HostSpec hash");
  assert.equal(value.host_spec_hash, hostSpecHash, "canonical fixture HostSpec hash binding");
  assertAuditRequest(value.audit_request, hostSpecHash);
  assertKnownEventPrefix(value.normalized_events, 3, "canonical normalized events");
  assertAuditResult(value.audit_result, value.audit_request.request_id);
}

function assertFailClosed(value, name) {
  assert.deepEqual(value, failClosedResult, `${name} must fail closed with less information`);
}

function assertAdversarialGiven(testCase, hostSpec) {
  const { given, id, kind } = testCase;
  switch (kind) {
    case "bare_wrapper":
      expectKeys(given, ["wrapper_kind"], `${id} given`);
      assert.equal(given.wrapper_kind, "omp", `${id} must name bare omp`);
      break;
    case "wrong_route":
      expectKeys(given, ["route"], `${id} given`);
      assert.notEqual(given.route, hostSpec.adapter.route, `${id} route must differ`);
      break;
    case "wrong_provider":
      expectKeys(given, ["provider"], `${id} given`);
      assert.notEqual(given.provider, hostSpec.model.provider, `${id} provider must differ`);
      break;
    case "wrong_model":
      expectKeys(given, ["model"], `${id} given`);
      assert.notEqual(given.model, hostSpec.model.model, `${id} model must differ`);
      break;
    case "non_read_only_capability":
      expectKeys(given, ["access"], `${id} given`);
      assert.notEqual(given.access, "read_only", `${id} access must differ`);
      break;
    case "unsealed_payload":
      expectKeys(given, ["payload_hash"], `${id} given`);
      assertHash(given.payload_hash, `${id} payload hash`);
      assert.notEqual(given.payload_hash, sealedMaterialization.payload_hash, `${id} payload must differ`);
      break;
    case "wrong_nonce":
      expectKeys(given, ["nonce"], `${id} given`);
      assertNonce(given.nonce, `${id} nonce`);
      assert.notEqual(given.nonce, sealedMaterialization.nonce, `${id} nonce must differ`);
      break;
    case "noncanonical_target":
      expectKeys(given, ["repository_identity"], `${id} given`);
      assert.notEqual(given.repository_identity, trustedTarget.repository_identity, `${id} target must differ`);
      break;
    case "wrong_source_snapshot":
      expectKeys(given, ["required_source_snapshot_hash"], `${id} given`);
      assertHash(given.required_source_snapshot_hash, `${id} source snapshot hash`);
      assert.notEqual(given.required_source_snapshot_hash, trustedTarget.required_source_snapshot_hash, `${id} source snapshot must differ`);
      break;
    case "unknown_transcript_source":
      expectKeys(given, ["source"], `${id} given`);
      assert.notEqual(given.source, transcript.source, `${id} source must differ`);
      break;
    case "unknown_transcript_dialect":
      expectKeys(given, ["dialect"], `${id} given`);
      assert.ok(![transcript.input_dialect, transcript.output_dialect].includes(given.dialect), `${id} dialect must be unknown`);
      break;
    case "unknown_transcript_event":
      expectKeys(given, ["kind"], `${id} given`);
      assert.ok(!normalizedEvents.some((event) => event.kind === given.kind), `${id} event kind must be unknown`);
      break;
    case "renderer_authority":
      expectKeys(given, ["renderer_field"], `${id} given`);
      assert.ok(["command", "prompt", "prose"].includes(given.renderer_field), `${id} renderer authority field`);
      break;
    case "private_renderer_field":
      expectKeys(given, ["renderer_field"], `${id} given`);
      assert.ok(["token", "socket", "path", "raw_error"].includes(given.renderer_field), `${id} private renderer field`);
      break;
    default:
      throw new Error(`unsupported adversarial case ${kind}`);
  }
}

function verifyAdversarialFixture(value, hostSpec) {
  assertNoRawAuthority(value);
  expectKeys(value, ["cases", "schema_version"], "P3d adversarial fixture");
  assert.equal(value.schema_version, "ananke.omp-readonly-audit.adversarial.v1", "adversarial fixture schema version");
  const expected = [
    ["bare_omp_wrapper_waits_for_human", "bare_wrapper"],
    ["wrong_adapter_route_waits_for_human", "wrong_route"],
    ["wrong_provider_waits_for_human", "wrong_provider"],
    ["wrong_model_waits_for_human", "wrong_model"],
    ["non_read_only_capability_waits_for_human", "non_read_only_capability"],
    ["unsealed_payload_waits_for_human", "unsealed_payload"],
    ["wrong_nonce_waits_for_human", "wrong_nonce"],
    ["noncanonical_target_waits_for_human", "noncanonical_target"],
    ["wrong_source_snapshot_waits_for_human", "wrong_source_snapshot"],
    ["unknown_transcript_source_waits_for_human", "unknown_transcript_source"],
    ["unknown_transcript_dialect_waits_for_human", "unknown_transcript_dialect"],
    ["unknown_transcript_event_waits_for_human", "unknown_transcript_event"],
    ["renderer_command_has_no_authority", "renderer_authority"],
    ["renderer_prompt_has_no_authority", "renderer_authority"],
    ["renderer_prose_has_no_authority", "renderer_authority"],
    ["renderer_token_is_not_public", "private_renderer_field"],
    ["renderer_socket_is_not_public", "private_renderer_field"],
    ["renderer_path_is_not_public", "private_renderer_field"],
    ["renderer_raw_error_is_not_public", "private_renderer_field"],
  ];
  assert.ok(Array.isArray(value.cases) && value.cases.length === expected.length, "adversarial case inventory");
  value.cases.forEach((testCase, index) => {
    expectKeys(testCase, ["given", "id", "kind", "then"], `adversarial case ${index + 1}`);
    assert.deepEqual([testCase.id, testCase.kind], expected[index], `adversarial case ${index + 1} identity`);
    assertAdversarialGiven(testCase, hostSpec);
    assertFailClosed(testCase.then, `adversarial case ${testCase.id}`);
  });
}

function crashOutcome(id, facts) {
  const expected = {
    request_before_adapter_admission: {
      action: "retry_adapter_admission", adapter_state: "not_admitted", cancellation_state: "not_requested", events: 0,
    },
    admission_before_first_normalized_event: {
      action: "reconnect_transcript_source", adapter_state: "admitted", cancellation_state: "not_requested", events: 0,
    },
    normalized_events_before_result: {
      action: "reconnect_transcript_source", adapter_state: "monitoring", cancellation_state: "not_requested", events: 2,
    },
    cancellation_before_terminal_event: {
      action: "retry_bounded_cancellation", adapter_state: "cancel_requested", cancellation_state: "requested", events: 1,
    },
  }[id];
  assert.ok(expected, `unsupported crash boundary ${id}`);
  expectKeys(facts, ["adapter_state", "audit_result", "cancellation_state", "normalized_events", "request_id"], `crash ${id} facts`);
  assert.equal(facts.request_id, "omp_audit_request_p3d_001", `crash ${id} request binding`);
  assert.equal(facts.adapter_state, expected.adapter_state, `crash ${id} adapter state`);
  assert.equal(facts.cancellation_state, expected.cancellation_state, `crash ${id} cancellation state`);
  assert.equal(facts.audit_result, null, `crash ${id} result must remain absent`);
  assertKnownEventPrefix(facts.normalized_events, expected.events, `crash ${id} normalized events`);
  return { action: expected.action, emitted_events: [], result: null, terminal_state: "absent" };
}

function verifyCrashFixture(value) {
  assertNoRawAuthority(value);
  expectKeys(value, ["cases", "schema_version"], "P3d crash fixture");
  assert.equal(value.schema_version, "ananke.omp-readonly-audit.crash.v1", "crash fixture schema version");
  const expectedIDs = ["request_before_adapter_admission", "admission_before_first_normalized_event", "normalized_events_before_result", "cancellation_before_terminal_event"];
  assert.ok(Array.isArray(value.cases) && value.cases.length === expectedIDs.length, "crash case inventory");
  value.cases.forEach((testCase, index) => {
    expectKeys(testCase, ["facts", "id", "then"], `crash case ${index + 1}`);
    assert.equal(testCase.id, expectedIDs[index], `crash case ${index + 1} identity`);
    assert.deepEqual(testCase.then, crashOutcome(testCase.id, testCase.facts), `crash ${testCase.id} outcome`);
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
  assert.equal(digest(bytes), canonicalFixtureDigests.get(name), `canonical fixture digest mismatch: ${name}`);
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
  verifyCanonicalFixture(fixtures["omp-audit-v1.canonical.json"]);
  verifyAdversarialFixture(fixtures["adversarial-v1.canonical.json"], fixtures["omp-audit-v1.canonical.json"].host_spec);
  verifyCrashFixture(fixtures["crash-v1.canonical.json"]);
}


async function assertRejected(action, pattern, name) {
  await assert.rejects(action, pattern, `${name} rejection reason`);
}

async function selfTest() {
  const manifest = await readManifest(sourceFixtureDirectory);
  const fixtures = Object.fromEntries(await Promise.all(fixtureNames.map(async (name) => [name, await readCanonical(sourceFixtureDirectory, name, manifest)])));
  const canonicalFixture = fixtures["omp-audit-v1.canonical.json"];
  verifyCanonicalFixture(canonicalFixture);
  verifyAdversarialFixture(fixtures["adversarial-v1.canonical.json"], canonicalFixture.host_spec);
  verifyCrashFixture(fixtures["crash-v1.canonical.json"]);

  const drifted = structuredClone(canonicalFixture);
  drifted.audit_request.attempt_cap = 4;
  await assertRejected(async () => {
    assert.equal(
      digest(Buffer.from(canonicalJson(drifted), "utf8")),
      canonicalFixtureDigests.get("omp-audit-v1.canonical.json"),
      "canonical fixture digest mismatch: omp-audit-v1.canonical.json",
    );
  }, /canonical fixture digest mismatch/, "in-memory audit request drift");

  const canonicalMutations = [
    ["route", (fixture) => { fixture.host_spec.adapter.route = "other_omp_route"; }, /adapter route-aware wrapper/],
    ["bare wrapper", (fixture) => { fixture.host_spec.adapter.wrapper_kind = "omp"; }, /adapter route-aware wrapper/],
    ["renderer command authority", (fixture) => { fixture.audit_request.command = "forbidden"; }, /forbidden raw\/public authority field/],
    ["private socket", (fixture) => { fixture.audit_request.socket = "forbidden"; }, /forbidden raw\/public authority field/],
    ["P3c action", (fixture) => { fixture.audit_request.launch_binding.p3c_action = "retry_other_action"; }, /P3a\/P3b\/P3c binding/],
    ["sealed nonce", (fixture) => { fixture.audit_request.sealed_materialization.nonce = `nonce:${"9".repeat(64)}`; }, /canonical seal binding/],
    ["canonical target", (fixture) => { fixture.host_spec.target.repository_identity = "local_checkout"; }, /canonical Ananke repository identity/],
    ["transcript source", (fixture) => { fixture.host_spec.transcript.source = "unknown_transcript_source"; }, /source and dialect normalization/],
    ["unknown event", (fixture) => { fixture.normalized_events[1].kind = "unrecognized"; }, /known normalized event/],
    ["unearned result", (fixture) => { fixture.audit_result.state = "waiting_for_human"; }, /audit result state/],
  ];
  for (const [name, mutate, pattern] of canonicalMutations) {
    const fixture = structuredClone(canonicalFixture);
    mutate(fixture);
    await assertRejected(async () => verifyCanonicalFixture(fixture), pattern, name);
  }

  const adversarial = structuredClone(fixtures["adversarial-v1.canonical.json"]);
  adversarial.cases[10].then.result = { state: "completed" };
  await assertRejected(
    async () => verifyAdversarialFixture(adversarial, canonicalFixture.host_spec),
    /must fail closed with less information/,
    "unknown transcript dialect information leak",
  );

  for (const [name, mutate, pattern] of [
    ["crash result guess", (fixture) => { fixture.cases[2].facts.audit_result = { state: "completed" }; }, /result must remain absent/],
    ["crash recovery action", (fixture) => { fixture.cases[3].then.action = "completed"; }, /crash cancellation_before_terminal_event outcome/],
    ["crash terminal guess", (fixture) => { fixture.cases[1].then.terminal_state = "completed"; }, /crash admission_before_first_normalized_event outcome/],
  ]) {
    const crash = structuredClone(fixtures["crash-v1.canonical.json"]);
    mutate(crash);
    await assertRejected(async () => verifyCrashFixture(crash), pattern, name);
  }
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log("P3d controlled OMP adapter self-test rejected fixture drift, bare or wrong route/provider/model, renderer authority and private fields, P3a/P3b/P3c or sealed-materialization drift, noncanonical targets, unknown transcript data, unearned results, and crash outcome guesses.");
} else {
  await verify(resolve(optionValue("--fixtures") ?? sourceFixtureDirectory));
  console.log("P3d controlled OMP adapter fixtures verified: route-aware OMP wrapper, exact provider/model and immutable P3a bounds, sealed materialization and canonical Ananke source identity, normalized bounded IR, fail-closed public boundary, and no-guess cancellation/recovery facts.");
}
