import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const sourceFixtureDirectory = resolve(dirname(scriptPath), "fixtures");
const sourceP3dFixtureDirectory = resolve(dirname(scriptPath), "..", "p3d", "fixtures");
const fixtureNames = ["production-activation-v1.canonical.json", "preflight-red-flags-v1.canonical.json"];
const fixtureHashVersion = "ananke-omp-production-activation-contract-v1";
const canonicalFixtureDigests = new Map([
  ["production-activation-v1.canonical.json", "49d5e64ba52f7521f4bc043bb55df7ece07ccd3504c5e6c3c927939b8ec5598a"],
  ["preflight-red-flags-v1.canonical.json", "73e8563b1cf7b9ab6c0319b2458d524ecdccea2448225be195ee5582dd808b9a"],
]);
const p3dCanonicalDigest = "9c8ca561416c82f98ad49d08c625bb5b11be468fb306cd254e7700468ac0e7f3";
const p3dManifest = [
  "ananke-omp-readonly-audit-contract-v1 sha256 9c8ca561416c82f98ad49d08c625bb5b11be468fb306cd254e7700468ac0e7f3 omp-audit-v1.canonical.json",
  "ananke-omp-readonly-audit-contract-v1 sha256 12e2fa336c0f374859eec7cb5a5311bc660df4d36e6b1c8671c575e0d6e2bab8 adversarial-v1.canonical.json",
  "ananke-omp-readonly-audit-contract-v1 sha256 e81798ad7aef51980a0a62c2c3ebfd9de7ca714b2698c52c4b3bf9bc29c4254c crash-v1.canonical.json",
].join("\n");

const p3dHostSpecHash = "sha256:f176da10c1c055b930b17137d790580400c9a0e6963e967290a19cb608512e8b";
const p3dSourceSnapshotHash = "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4";
const productionWrapperHash = "sha256:ac36f5816b1a6caaf4e4bed488e90d94c426cf9f126678c4c0f1eb50dc231a91";
const wrapperKind = "ananke_omp_readonly_wrapper_v1";
const route = "ananke_omp_read_only_audit_v1";
const deadline = "2026-07-30T12:00:00Z";
const repositoryIdentity = "github.com/yingliang-zhang/ananke";
const sourceEntries = [
  { blob_sha256: "sha256:345a0beaa4382342de09d045eea77b9caa1409f3d9b026dd11658d5274cb4489", entry_id: "go_module" },
  { blob_sha256: "sha256:ae4fa3ea0fc785e24cae8319e26599f95d5c1c84db59b5509ef247470f582e0d", entry_id: "lifecycle_core" },
  { blob_sha256: "sha256:d629fffc1bd8c0e4d1b19c29b9dfaa6bb84a78bd73e9ccffd3d0f12484c11b84", entry_id: "supervisor_core" },
];
const sourceManifestHash = "sha256:842188d5ce1e461839bf33fb50a4040a3bf9f2e44d94c31be640058f5765cc15";
const failClosedOutput = {
  events: [],
  result: null,
  schema_version: "ananke.omp-production-output.v1",
  state: "waiting_for_human",
  verification_state: "not_run",
};

const hashPattern = /^sha256:[0-9a-f]{64}$/;
const gitCommitPattern = /^[0-9a-f]{40}$/;
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const forbiddenRawFragments = ["command", "credential", "environment", "error", "exec", "instruction", "password", "path", "pid", "prompt", "prose", "secret", "socket", "token"];

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

function assertHash(value, name) {
  assert.ok(typeof value === "string" && hashPattern.test(value), `${name} must be a SHA-256 hash`);
}

function assertIdentifier(value, name) {
  assert.ok(typeof value === "string" && identifierPattern.test(value), `${name} must be an identifier`);
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

function normalizedKey(key) {
  return key.toLowerCase().replace(/[^a-z0-9]/g, "");
}

function isAllowedCredentialPolicy(path, key) {
  return (path === "$" && key === "credential_policy") || (path === "$.credential_policy" && ["argv_credentials", "environment_credentials"].includes(key));
}

function assertNoRawAuthority(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoRawAuthority(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    const normalized = normalizedKey(key);
    assert.ok(
      isAllowedCredentialPolicy(path, key) || !forbiddenRawFragments.some((fragment) => normalized === fragment || normalized.startsWith(fragment) || normalized.endsWith(fragment)),
      `forbidden raw authority field ${path}.${key}`,
    );
    assertNoRawAuthority(entry, `${path}.${key}`);
  }
}

function assertP3dAnchor(value) {
  expectKeys(value, ["audit_request", "audit_result", "host_spec", "host_spec_hash", "normalized_events", "schema_version"], "P3d canonical fixture");
  assert.equal(value.schema_version, "ananke.omp-readonly-audit.fixture.v1", "P3d fixture schema version");
  assert.equal(value.host_spec_hash, p3dHostSpecHash, "P3d HostSpec hash");
  expectKeys(value.host_spec, ["adapter", "attempt_cap", "capabilities", "deadline", "host_spec_fingerprint", "model", "read_only_capability", "schema_version", "sealed_materialization", "target", "transcript", "verification"], "P3d HostSpec");
  assert.equal(value.host_spec.host_spec_fingerprint, p3dHostSpecHash, "P3d HostSpec canonical fingerprint");
  assert.equal(value.host_spec.schema_version, "ananke.omp-readonly-host-spec.v1", "P3d HostSpec schema version");
  assert.deepEqual(value.host_spec.adapter, { route, wrapper_kind: wrapperKind }, "P3d route pair");
  assert.equal(value.host_spec.deadline, deadline, "P3d deadline");
  expectKeys(value.host_spec.target, ["repository_identity", "required_source_snapshot_hash", "root_identity_fingerprint", "target_kind"], "P3d target");
  assert.equal(value.host_spec.target.repository_identity, repositoryIdentity, "P3d repository identity");
  assert.equal(value.host_spec.target.required_source_snapshot_hash, p3dSourceSnapshotHash, "P3d required source snapshot hash");
  expectKeys(value.audit_request, ["attempt_cap", "deadline", "host_spec_hash", "launch_binding", "request_id", "schema_version", "sealed_materialization", "target"], "P3d audit request");
  assert.equal(value.audit_request.host_spec_hash, p3dHostSpecHash, "P3d audit request HostSpec binding");
  assert.equal(value.audit_request.deadline, deadline, "P3d audit request deadline");
  assert.equal(value.audit_request.launch_binding.p3c_action, "retry_process_admission", "P3d P3c action");
  return {
    deadline: value.audit_request.deadline,
    host_spec_hash: value.host_spec_hash,
    required_source_snapshot_hash: value.host_spec.target.required_source_snapshot_hash,
    route: value.host_spec.adapter.route,
    wrapper_kind: value.host_spec.adapter.wrapper_kind,
  };
}

function assertP3dBinding(value, anchor) {
  expectKeys(value, ["host_spec_hash", "p3d_fixture_sha256", "required_source_snapshot_hash"], "P3f P3d binding");
  assertHash(value.host_spec_hash, "P3f P3d binding HostSpec hash");
  assertHash(value.p3d_fixture_sha256, "P3f P3d binding fixture hash");
  assertHash(value.required_source_snapshot_hash, "P3f P3d binding source snapshot hash");
  assert.deepEqual(value, {
    host_spec_hash: anchor.host_spec_hash,
    p3d_fixture_sha256: `sha256:${p3dCanonicalDigest}`,
    required_source_snapshot_hash: anchor.required_source_snapshot_hash,
  }, "P3f must bind the frozen P3d contract anchor");
}

function assertSourceManifest(value, anchor) {
  expectKeys(value, ["entries", "git_commit", "p3d_required_source_snapshot_hash", "repository_identity", "schema_version", "source_manifest_hash", "tracked"], "tracked source manifest");
  assert.equal(value.schema_version, "ananke.tracked-source-manifest.v1", "source manifest schema version");
  assert.ok(value.tracked === true, "source manifest must name a tracked git commit");
  assert.ok(typeof value.git_commit === "string" && gitCommitPattern.test(value.git_commit), "source manifest git commit");
  assert.equal(value.repository_identity, repositoryIdentity, "source manifest repository identity");
  assertHash(value.p3d_required_source_snapshot_hash, "source manifest P3d source snapshot hash");
  assert.equal(value.p3d_required_source_snapshot_hash, anchor.required_source_snapshot_hash, "source manifest P3d source binding");
  assertHash(value.source_manifest_hash, "source manifest hash");
  assert.ok(Array.isArray(value.entries) && value.entries.length === sourceEntries.length, "source manifest entry inventory");
  value.entries.forEach((entry, index) => {
    expectKeys(entry, ["blob_sha256", "entry_id"], `source manifest entry ${index + 1}`);
    assertIdentifier(entry.entry_id, `source manifest entry ${index + 1} id`);
    assertHash(entry.blob_sha256, `source manifest entry ${index + 1} blob hash`);
    assert.deepEqual(entry, sourceEntries[index], `source manifest entry ${index + 1}`);
  });
  const { source_manifest_hash, ...hashInput } = value;
  assert.equal(source_manifest_hash, hashCanonical(hashInput), "source manifest canonical JCS hash derivation");
  assert.equal(source_manifest_hash, sourceManifestHash, "source manifest frozen hash");
  return source_manifest_hash;
}

function assertApprovedWrapper(value, anchor) {
  expectKeys(value, ["binary_sha256", "route", "wrapper_kind"], "approved production wrapper");
  assertHash(value.binary_sha256, "approved production wrapper SHA-256");
  assert.deepEqual(value, {
    binary_sha256: productionWrapperHash,
    route: anchor.route,
    wrapper_kind: anchor.wrapper_kind,
  }, "approved production wrapper binary and route pair");
}

function assertFDInterface(value) {
  expectKeys(value, ["evidence", "manifest", "source"], "inherited FD interface");
  assert.deepEqual(value, {
    evidence: "inherited_fd_only",
    manifest: "inherited_fd_only",
    source: "inherited_fd_only",
  }, "manifest/source/evidence must remain inherited FD-only");
}

function assertSandboxCapability(value) {
  expectKeys(value, ["source_access", "write_policy"], "sandbox capability");
  assert.deepEqual(value, {
    source_access: "os_enforced_read_only",
    write_policy: "os_enforced_write_denied",
  }, "sandbox must enforce read-only source and denied writes in the OS");
}

function assertCleanupCapability(value) {
  expectKeys(value, ["descriptor_ownership", "inode_identity", "on_exit"], "cleanup capability");
  assert.deepEqual(value, {
    descriptor_ownership: "activation_owned",
    inode_identity: "device_inode_bound",
    on_exit: "close_owned_descriptors_and_remove_owned_inode",
  }, "cleanup must own descriptor and inode identity");
}

function assertCredentialPolicy(value) {
  expectKeys(value, ["argv_credentials", "environment_credentials"], "credential policy");
  assert.deepEqual(value, {
    argv_credentials: "forbidden",
    environment_credentials: "forbidden",
  }, "argv and environment credentials are forbidden");
}

function assertLaunchPreflight(value, anchor, manifestHash) {
  expectKeys(value, ["check_phase", "deadline", "full_private_fence", "p3c_action", "p3d_required_source_snapshot_hash", "route", "source_manifest_hash", "wrapper_binary_sha256", "wrapper_kind"], "launch preflight");
  assert.equal(value.check_phase, "launch_time", "preflight must occur at launch time");
  assertTimestamp(value.deadline, "launch preflight deadline");
  assert.equal(value.deadline, anchor.deadline, "launch preflight P3d deadline binding");
  assert.equal(value.full_private_fence, "authenticate_full_private_fence", "launch preflight requires full private fence authentication");
  assert.equal(value.p3c_action, "retry_process_admission", "launch preflight P3c action");
  assertHash(value.p3d_required_source_snapshot_hash, "launch preflight P3d source snapshot hash");
  assert.equal(value.p3d_required_source_snapshot_hash, anchor.required_source_snapshot_hash, "launch preflight P3d source binding");
  assertHash(value.source_manifest_hash, "launch preflight source manifest hash");
  assert.equal(value.source_manifest_hash, manifestHash, "launch preflight source manifest binding");
  assertHash(value.wrapper_binary_sha256, "launch preflight wrapper binary SHA-256");
  assert.equal(value.wrapper_binary_sha256, productionWrapperHash, "launch preflight approved wrapper binding");
  assert.equal(value.wrapper_kind, anchor.wrapper_kind, "launch preflight wrapper kind");
  assert.equal(value.route, anchor.route, "launch preflight route");
}

function assertNormalizedOutput(value) {
  expectKeys(value, ["event_schema_version", "failure_state", "result_schema_version", "schema_version"], "normalized output declaration");
  assert.deepEqual(value, {
    event_schema_version: "ananke.omp-audit-event.v1",
    failure_state: "waiting_for_human",
    result_schema_version: "ananke.omp-audit-result.v1",
    schema_version: "ananke.omp-production-output.v1",
  }, "normalized output schema");
}

function verifyCanonicalFixture(value, p3dAnchor) {
  assertNoRawAuthority(value);
  expectKeys(value, ["activation_state", "approved_wrapper", "cleanup_capability", "credential_policy", "inherited_fd_interface", "launch_preflight", "normalized_output", "p3d_binding", "sandbox_capability", "schema_version", "source_manifest", "source_manifest_hash"], "P3f canonical fixture");
  assert.equal(value.schema_version, "ananke.omp-production-activation.v1", "P3f canonical fixture schema version");
  assert.equal(value.activation_state, "contract_only_not_launched", "P3f fixture must not claim a launched child");
  assertP3dBinding(value.p3d_binding, p3dAnchor);
  const manifestHash = assertSourceManifest(value.source_manifest, p3dAnchor);
  assertHash(value.source_manifest_hash, "P3f fixture source manifest hash");
  assert.equal(value.source_manifest_hash, manifestHash, "P3f fixture source manifest binding");
  assertApprovedWrapper(value.approved_wrapper, p3dAnchor);
  assertFDInterface(value.inherited_fd_interface);
  assertSandboxCapability(value.sandbox_capability);
  assertCleanupCapability(value.cleanup_capability);
  assertCredentialPolicy(value.credential_policy);
  assertLaunchPreflight(value.launch_preflight, p3dAnchor, manifestHash);
  assertNormalizedOutput(value.normalized_output);
}

function assertFailClosed(value, name) {
  assert.deepEqual(value, failClosedOutput, `${name} must return only normalized waiting_for_human`);
}

function assertRedFlagGiven(testCase, canonical) {
  const { given, id, kind } = testCase;
  switch (kind) {
    case "untracked_commit":
      expectKeys(given, ["tracked"], `${id} given`);
      assert.equal(given.tracked, false, `${id} must be untracked`);
      break;
    case "source_manifest_hash_mismatch":
    case "preflight_source_hash_drift":
      expectKeys(given, ["source_manifest_hash"], `${id} given`);
      assertHash(given.source_manifest_hash, `${id} source manifest hash`);
      assert.notEqual(given.source_manifest_hash, canonical.source_manifest_hash, `${id} source manifest hash must differ`);
      break;
    case "p3d_source_snapshot_drift":
      expectKeys(given, ["required_source_snapshot_hash"], `${id} given`);
      assertHash(given.required_source_snapshot_hash, `${id} P3d source snapshot hash`);
      assert.notEqual(given.required_source_snapshot_hash, canonical.p3d_binding.required_source_snapshot_hash, `${id} P3d source snapshot hash must differ`);
      break;
    case "p3d_host_spec_drift":
      expectKeys(given, ["host_spec_hash"], `${id} given`);
      assertHash(given.host_spec_hash, `${id} P3d HostSpec hash`);
      assert.notEqual(given.host_spec_hash, canonical.p3d_binding.host_spec_hash, `${id} P3d HostSpec hash must differ`);
      break;
    case "wrapper_binary_hash_drift":
      expectKeys(given, ["binary_sha256"], `${id} given`);
      assertHash(given.binary_sha256, `${id} wrapper binary hash`);
      assert.notEqual(given.binary_sha256, canonical.approved_wrapper.binary_sha256, `${id} wrapper binary hash must differ`);
      break;
    case "wrong_wrapper_kind":
      expectKeys(given, ["wrapper_kind"], `${id} given`);
      assert.notEqual(given.wrapper_kind, canonical.approved_wrapper.wrapper_kind, `${id} wrapper kind must differ`);
      break;
    case "wrong_route":
      expectKeys(given, ["route"], `${id} given`);
      assert.notEqual(given.route, canonical.approved_wrapper.route, `${id} route must differ`);
      break;
    case "non_fd_interface":
      expectKeys(given, ["interface", "mode"], `${id} given`);
      assert.ok(["source", "manifest", "evidence"].includes(given.interface), `${id} FD interface`);
      assert.notEqual(given.mode, "inherited_fd_only", `${id} must not be FD-only`);
      break;
    case "sandbox_source_access":
      expectKeys(given, ["source_access"], `${id} given`);
      assert.notEqual(given.source_access, canonical.sandbox_capability.source_access, `${id} source access must differ`);
      break;
    case "sandbox_write_policy":
      expectKeys(given, ["write_policy"], `${id} given`);
      assert.notEqual(given.write_policy, canonical.sandbox_capability.write_policy, `${id} write policy must differ`);
      break;
    case "descriptor_cleanup":
      expectKeys(given, ["descriptor_ownership"], `${id} given`);
      assert.notEqual(given.descriptor_ownership, canonical.cleanup_capability.descriptor_ownership, `${id} descriptor ownership must differ`);
      break;
    case "inode_cleanup":
      expectKeys(given, ["inode_identity"], `${id} given`);
      assert.notEqual(given.inode_identity, canonical.cleanup_capability.inode_identity, `${id} inode identity must differ`);
      break;
    case "credential_channel":
      expectKeys(given, ["channel"], `${id} given`);
      assert.ok(["argv", "environment"].includes(given.channel), `${id} credential channel`);
      break;
    case "deadline_drift":
      expectKeys(given, ["deadline"], `${id} given`);
      assertTimestamp(given.deadline, `${id} deadline`);
      assert.notEqual(given.deadline, canonical.launch_preflight.deadline, `${id} deadline must differ`);
      break;
    case "fence_check_downgrade":
      expectKeys(given, ["full_private_fence"], `${id} given`);
      assert.notEqual(given.full_private_fence, canonical.launch_preflight.full_private_fence, `${id} full private fence must differ`);
      break;
    case "p3c_action_drift":
      expectKeys(given, ["p3c_action"], `${id} given`);
      assert.notEqual(given.p3c_action, canonical.launch_preflight.p3c_action, `${id} P3c action must differ`);
      break;
    case "preflight_wrapper_hash_drift":
      expectKeys(given, ["wrapper_binary_sha256"], `${id} given`);
      assertHash(given.wrapper_binary_sha256, `${id} wrapper hash`);
      assert.notEqual(given.wrapper_binary_sha256, canonical.launch_preflight.wrapper_binary_sha256, `${id} wrapper hash must differ`);
      break;
    case "preflight_route_pair_drift":
      expectKeys(given, ["route", "wrapper_kind"], `${id} given`);
      assert.ok(given.route !== canonical.launch_preflight.route || given.wrapper_kind !== canonical.launch_preflight.wrapper_kind, `${id} route pair must differ`);
      break;
    case "unknown_output_schema":
      expectKeys(given, ["schema_version"], `${id} given`);
      assert.notEqual(given.schema_version, canonical.normalized_output.schema_version, `${id} output schema must differ`);
      break;
    default:
      throw new Error(`unsupported P3f preflight red flag ${kind}`);
  }
}

function verifyRedFlagsFixture(value, canonical) {
  assertNoRawAuthority(value);
  expectKeys(value, ["cases", "schema_version"], "P3f preflight red flags fixture");
  assert.equal(value.schema_version, "ananke.omp-production-activation.preflight-red-flags.v1", "P3f preflight red flags schema version");
  const expected = [
    ["untracked_commit_waits_for_human", "untracked_commit"],
    ["source_manifest_hash_mismatch_waits_for_human", "source_manifest_hash_mismatch"],
    ["p3d_source_snapshot_drift_waits_for_human", "p3d_source_snapshot_drift"],
    ["p3d_host_spec_drift_waits_for_human", "p3d_host_spec_drift"],
    ["wrapper_binary_hash_drift_waits_for_human", "wrapper_binary_hash_drift"],
    ["wrong_wrapper_kind_waits_for_human", "wrong_wrapper_kind"],
    ["wrong_route_waits_for_human", "wrong_route"],
    ["non_fd_source_interface_waits_for_human", "non_fd_interface"],
    ["non_fd_manifest_interface_waits_for_human", "non_fd_interface"],
    ["non_fd_evidence_interface_waits_for_human", "non_fd_interface"],
    ["sandbox_source_not_read_only_waits_for_human", "sandbox_source_access"],
    ["sandbox_writes_not_denied_waits_for_human", "sandbox_write_policy"],
    ["unowned_descriptor_cleanup_waits_for_human", "descriptor_cleanup"],
    ["unbound_inode_cleanup_waits_for_human", "inode_cleanup"],
    ["argv_credential_waits_for_human", "credential_channel"],
    ["environment_credential_waits_for_human", "credential_channel"],
    ["deadline_drift_waits_for_human", "deadline_drift"],
    ["fence_fingerprint_not_full_private_waits_for_human", "fence_check_downgrade"],
    ["wrong_p3c_action_waits_for_human", "p3c_action_drift"],
    ["preflight_source_hash_drift_waits_for_human", "preflight_source_hash_drift"],
    ["preflight_wrapper_hash_drift_waits_for_human", "preflight_wrapper_hash_drift"],
    ["preflight_route_pair_drift_waits_for_human", "preflight_route_pair_drift"],
    ["unknown_normalized_output_waits_for_human", "unknown_output_schema"],
  ];
  assert.ok(Array.isArray(value.cases) && value.cases.length === expected.length, "P3f preflight red flag inventory");
  value.cases.forEach((testCase, index) => {
    expectKeys(testCase, ["given", "id", "kind", "then"], `P3f red flag ${index + 1}`);
    assert.deepEqual([testCase.id, testCase.kind], expected[index], `P3f red flag ${index + 1} identity`);
    assertRedFlagGiven(testCase, canonical);
    assertFailClosed(testCase.then, `P3f red flag ${testCase.id}`);
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

async function readP3dAnchor(directory) {
  const manifest = await readFile(join(directory, "fixtures.sha256"), "utf8");
  assert.equal(manifest, p3dManifest, "P3d manifest anchor");
  const bytes = await readFile(join(directory, "omp-audit-v1.canonical.json"));
  assert.equal(digest(bytes), p3dCanonicalDigest, "P3d canonical fixture digest anchor");
  assert.ok(!bytes.subarray(0, 3).equals(Buffer.from([0xef, 0xbb, 0xbf])), "P3d canonical fixture has a UTF-8 BOM");
  const text = bytes.toString("utf8");
  assert.ok(Buffer.from(text, "utf8").equals(bytes), "P3d canonical fixture is not UTF-8");
  const value = JSON.parse(text);
  assertNoUnpairedSurrogates(value);
  assert.equal(text, canonicalJson(value), "P3d canonical fixture is not canonical JCS bytes");
  return { anchor: assertP3dAnchor(value), value };
}

async function verify(directory, p3dDirectory) {
  const manifest = await readManifest(directory);
  const fixtures = Object.fromEntries(await Promise.all(fixtureNames.map(async (name) => [name, await readCanonical(directory, name, manifest)])));
  const { anchor } = await readP3dAnchor(p3dDirectory);
  verifyCanonicalFixture(fixtures["production-activation-v1.canonical.json"], anchor);
  verifyRedFlagsFixture(fixtures["preflight-red-flags-v1.canonical.json"], fixtures["production-activation-v1.canonical.json"]);
}

async function assertRejected(action, pattern, name) {
  await assert.rejects(action, pattern, `${name} rejection reason`);
}

async function selfTest() {
  const manifest = await readManifest(sourceFixtureDirectory);
  const fixtures = Object.fromEntries(await Promise.all(fixtureNames.map(async (name) => [name, await readCanonical(sourceFixtureDirectory, name, manifest)])));
  const { anchor, value: p3dFixture } = await readP3dAnchor(sourceP3dFixtureDirectory);
  const canonicalFixture = fixtures["production-activation-v1.canonical.json"];
  verifyCanonicalFixture(canonicalFixture, anchor);
  verifyRedFlagsFixture(fixtures["preflight-red-flags-v1.canonical.json"], canonicalFixture);

  const drifted = structuredClone(canonicalFixture);
  drifted.source_manifest_hash = `sha256:${"9".repeat(64)}`;
  await assertRejected(async () => verifyCanonicalFixture(drifted, anchor), /source manifest binding/, "source manifest fixture drift");

  const canonicalMutations = [
    ["untracked commit", (fixture) => { fixture.source_manifest.tracked = false; }, /must name a tracked git commit/],
    ["source manifest entry", (fixture) => { fixture.source_manifest.entries[0].entry_id = "other_entry"; }, /source manifest entry 1/],
    ["source manifest derivation", (fixture) => { fixture.source_manifest.git_commit = "a".repeat(40); }, /canonical JCS hash derivation/],
    ["P3d source binding", (fixture) => { fixture.p3d_binding.required_source_snapshot_hash = `sha256:${"8".repeat(64)}`; }, /frozen P3d contract anchor/],
    ["wrapper binary", (fixture) => { fixture.approved_wrapper.binary_sha256 = `sha256:${"7".repeat(64)}`; }, /approved production wrapper binary and route pair/],
    ["route pair", (fixture) => { fixture.approved_wrapper.route = "other_route"; }, /approved production wrapper binary and route pair/],
    ["FD-only interface", (fixture) => { fixture.inherited_fd_interface.evidence = "path_based"; }, /FD-only/],
    ["OS read-only sandbox", (fixture) => { fixture.sandbox_capability.source_access = "advisory_read_only"; }, /enforce read-only source/],
    ["OS write-denied sandbox", (fixture) => { fixture.sandbox_capability.write_policy = "advisory_denied"; }, /enforce read-only source/],
    ["descriptor ownership", (fixture) => { fixture.cleanup_capability.descriptor_ownership = "borrowed"; }, /cleanup must own descriptor/],
    ["inode identity", (fixture) => { fixture.cleanup_capability.inode_identity = "unbound"; }, /cleanup must own descriptor/],
    ["argv credential policy", (fixture) => { fixture.credential_policy.argv_credentials = "allowed"; }, /argv and environment credentials are forbidden/],
    ["environment credential policy", (fixture) => { fixture.credential_policy.environment_credentials = "allowed"; }, /argv and environment credentials are forbidden/],
    ["deadline", (fixture) => { fixture.launch_preflight.deadline = "2026-07-30T12:00:01Z"; }, /P3d deadline binding/],
    ["private fence", (fixture) => { fixture.launch_preflight.full_private_fence = "fence_fingerprint_only"; }, /full private fence authentication/],
    ["P3c action", (fixture) => { fixture.launch_preflight.p3c_action = "retry_other_action"; }, /P3c action/],
    ["preflight source", (fixture) => { fixture.launch_preflight.source_manifest_hash = `sha256:${"6".repeat(64)}`; }, /source manifest binding/],
    ["preflight wrapper", (fixture) => { fixture.launch_preflight.wrapper_binary_sha256 = `sha256:${"5".repeat(64)}`; }, /approved wrapper binding/],
    ["preflight route", (fixture) => { fixture.launch_preflight.route = "other_route"; }, /launch preflight route/],
    ["normalized output", (fixture) => { fixture.normalized_output.schema_version = "ananke.omp.unknown-output.v1"; }, /normalized output schema/],
    ["raw authority", (fixture) => { fixture.launch_preflight.command = "forbidden"; }, /forbidden raw authority field/],
  ];
  for (const [name, mutate, pattern] of canonicalMutations) {
    const fixture = structuredClone(canonicalFixture);
    mutate(fixture);
    await assertRejected(async () => verifyCanonicalFixture(fixture, anchor), pattern, name);
  }

  const p3dDrift = structuredClone(p3dFixture);
  p3dDrift.host_spec.adapter.route = "other_route";
  await assertRejected(async () => assertP3dAnchor(p3dDrift), /P3d route pair/, "P3d route chain drift");

  const redFlags = structuredClone(fixtures["preflight-red-flags-v1.canonical.json"]);
  redFlags.cases[17].then.state = "completed";
  await assertRejected(async () => verifyRedFlagsFixture(redFlags, canonicalFixture), /normalized waiting_for_human/, "red flag information leak");
  redFlags.cases[17] = structuredClone(fixtures["preflight-red-flags-v1.canonical.json"].cases[17]);
  redFlags.cases[14].given.channel = "socket";
  await assertRejected(async () => verifyRedFlagsFixture(redFlags, canonicalFixture), /credential channel/, "credential red flag drift");
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log("P3f production activation self-test rejected source-manifest derivation and P3d-chain drift, wrapper/route and FD/sandbox/cleanup-policy drift, argv or environment credentials, launch-time fence/action/deadline checks, raw authority, and non-waiting-for-human red-flag outputs.");
} else {
  await verify(
    resolve(optionValue("--fixtures") ?? sourceFixtureDirectory),
    resolve(optionValue("--p3d-fixtures") ?? sourceP3dFixtureDirectory),
  );
  console.log("P3f production activation fixtures verified: tracked git commit to canonical JCS source manifest/hash, P3d-bound approved wrapper route pair, inherited FD-only interface, OS-enforced sandbox declaration, owned descriptor/inode cleanup, credential denial, launch-time full-fence checks, normalized output, and waiting_for_human preflight red flags.");
}
