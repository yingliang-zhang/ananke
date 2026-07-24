import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const sourceFixtureDirectory = resolve(dirname(scriptPath), "fixtures");
const sourceP3fFixtureDirectory = resolve(dirname(scriptPath), "..", "p3f", "fixtures");
const fixtureHashVersion = "ananke-self-development-evidence-repair-contract-v1";
const fixtureNames = [
  "evidence-repair-admission-v1.canonical.json",
  "repair-admission-red-flags-v1.canonical.json",
];
const fixtureDigests = new Map([
  ["evidence-repair-admission-v1.canonical.json", "aa7d94f96b123ff200bf4f84ec55d7b5edbd157f4578ba99ed3b4fdbc93ee36c"],
  ["repair-admission-red-flags-v1.canonical.json", "91c900ce7cc2c53ce360775be0909b3e679a971756075d643f3b0d0e3eb4ce0f"],
]);
const p3fAdapterFixtureName = "independent-supervisor-protocol-adapter-v1.canonical.json";
const p3fRedFlagsFixtureName = "independent-supervisor-protocol-adapter-red-flags-v1.canonical.json";
const p3fAdapterDigest = "956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44";
const p3fRedFlagsDigest = "6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525";
const p3fRedFlagCount = 37;
const p1RevisionHash = "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263";
const expectedChain = {
  p1_revision_hash: p1RevisionHash,
  p2_grill_fixture_sha256: "sha256:d9301e896e1cd223c6a05df37eea8fd862c955a0ba9e0985616bffcae0e35caa",
  p3a_launch_admission_fixture_sha256: "sha256:4e6afde3722009df0447ef95271cb72629d7ca3bff103cee15fe229a6f4bea16",
  p3a_launch_spec_hash: "sha256:bbc43093a3b00c49c1d2ac26db08e6dd36ff72174ded15de9408702af3a9e658",
  p3b_fence_contract: "current_full_fence_required_no_token_projection",
  p3c_recovery_action: "retry_process_admission",
  p3d_omp_audit_fixture_sha256: "sha256:9c8ca561416c82f98ad49d08c625bb5b11be468fb306cd254e7700468ac0e7f3",
  p3f_adapter_fixture_sha256: `sha256:${p3fAdapterDigest}`,
  p3f_adapter_red_flags_fixture_sha256: `sha256:${p3fRedFlagsDigest}`,
  p3f_adapter_red_flags_count: p3fRedFlagCount,
  p3f_predecessor_envelope_hash: "sha256:3dc8c169234fcd2e496e38ab5de327c058f276be91b65cf13f1c9ae7faa12473",
  p3f_route_mapping_hash: "sha256:a468e940e5dd5752285b8aba2533109cfde2d8b259a007647ca6f431e0736603",
};
const evidenceNames = ["proposal", "revision", "approval", "fence", "envelope", "receipt", "callback", "source", "artifact", "route", "test", "evaluation"];
const redFlagKinds = [
  "p3f_adapter_fixture_drift", "p3f_adapter_denial_count_drift", "p3f_adapter_denial_digest_drift",
  "bundle_hash_drift", "bundle_schema_drift", "proposal_evidence_hash_drift", "revision_evidence_hash_drift",
  "approval_evidence_hash_drift", "fence_evidence_hash_drift", "envelope_evidence_hash_drift",
  "receipt_evidence_hash_drift", "callback_evidence_hash_drift", "source_evidence_hash_drift",
  "artifact_evidence_hash_drift", "route_evidence_hash_drift", "test_evidence_hash_drift",
  "evaluation_evidence_hash_drift", "evidence_record_self_hash_drift", "input_canonicality_failure",
  "input_p3f_binding_drift", "verifier_trust_identity_drift", "verifier_release_identity_drift",
  "replay_output_drift", "repair_attempt_cap_exceeded", "repair_attempt_number_nonpositive",
  "repair_role_not_allowed", "repair_route_not_allowed", "exact_evidence_bundle_missing",
  "exact_evidence_hash_set_drift", "fresh_approval_absent", "fresh_fence_absent", "moa_grant_missing",
  "moa_grant_role_drift", "moa_grant_route_drift", "moa_grant_evidence_drift", "repair_success_inferred",
  "failure_result_inferred", "review_finding_result_inferred",
];
const hashPattern = /^sha256:[0-9a-f]{64}$/;
const identifierPattern = /^[a-z][a-z0-9_]{2,63}$/;
const timestampPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/;
const forbiddenRawFields = new Set(["argv", "command", "credential", "credentials", "endpoint", "environment", "network", "path", "prompt", "prose", "secret", "token"]);
const failureProjection = {
  admission: "rejected",
  bundle_hash: null,
  repair_execution: "not_authorized",
  state: "waiting_for_human",
  verification_state: "not_run",
};

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
  const match = typeof value === "string" ? timestampPattern.exec(value) : null;
  assert.ok(match, `${name} must be a semantic UTC RFC 3339 timestamp`);
  const [year, month, day, hour, minute, second] = match.slice(1).map(Number);
  const daysInMonth = month === 2
    ? (year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28)
    : (month === 4 || month === 6 || month === 9 || month === 11 ? 30 : 31);
  assert.ok(month >= 1 && month <= 12 && day >= 1 && day <= daysInMonth && hour <= 23 && minute <= 59 && second <= 59, `${name} must be a semantic UTC RFC 3339 timestamp`);
}

function assertTimestampBefore(left, right, name) {
  assertTimestamp(left, `${name} left timestamp`);
  assertTimestamp(right, `${name} right timestamp`);
  assert.ok(Date.parse(left) < Date.parse(right), `${name} timestamp order`);
}

function assertNoRawAuthority(value, path = "$") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertNoRawAuthority(entry, `${path}[${index}]`));
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, entry] of Object.entries(value)) {
    assert.ok(!forbiddenRawFields.has(key), `forbidden raw authority field ${path}.${key}`);
    assertNoRawAuthority(entry, `${path}.${key}`);
  }
}

function assertSelfHash(value, hashField, name) {
  assertHash(value[hashField], `${name}.${hashField}`);
  const hashInput = { ...value };
  delete hashInput[hashField];
  assert.equal(value[hashField], hashCanonical(hashInput), `${name} canonical self hash`);
}

function assertP3fFixture(value, name) {
  assertNoRawAuthority(value);
  if (name === p3fAdapterFixtureName) {
    assert.equal(value.schema_version, "ananke.independent-supervisor-protocol-adapter-design.v1", "P3f adapter schema version");
    expectKeys(value.predecessor_binding, ["activation_fixture_sha256", "exec_fd_design_fixture_sha256", "external_supervisor_handoff_fixture_sha256", "p3d_fixture_sha256", "p3d_host_spec_hash", "p3f_wrapper_binary_sha256", "predecessor_envelope_hash", "predecessor_route_mapping_hash", "source_manifest_hash", "source_snapshot_hash"], "P3f adapter predecessor binding");
    assert.equal(value.predecessor_binding.predecessor_envelope_hash, expectedChain.p3f_predecessor_envelope_hash, "P3f envelope chain binding");
    assert.equal(value.predecessor_binding.predecessor_route_mapping_hash, expectedChain.p3f_route_mapping_hash, "P3f route chain binding");
  } else {
    expectKeys(value, ["cases", "schema_version"], "P3f adapter denial fixture");
    assert.equal(value.schema_version, "ananke.independent-supervisor-protocol-adapter-design.red-flags.v1", "P3f denial schema version");
    assert.equal(value.cases.length, p3fRedFlagCount, "P3f adapter exact denial count");
  }
}

async function readCanonical(directory, name, manifest, hardDigests, readFixtureFile = readFile) {
  const bytes = await readFixtureFile(join(directory, name));
  assert.equal(digest(bytes), manifest.get(name), `fixture digest mismatch: ${name}`);
  assert.equal(digest(bytes), hardDigests.get(name), `canonical fixture digest mismatch: ${name}`);
  assert.ok(!bytes.subarray(0, 3).equals(Buffer.from([0xef, 0xbb, 0xbf])), `${name} has a UTF-8 BOM`);
  const text = bytes.toString("utf8");
  assert.ok(Buffer.from(text, "utf8").equals(bytes), `${name} is not UTF-8`);
  const value = JSON.parse(text);
  assertNoUnpairedSurrogates(value);
  assert.equal(text, canonicalJson(value), `${name} is not canonical JCS bytes`);
  return value;
}

async function authenticateP3f(directory, readFixtureFile = readFile) {
  const manifestText = await readFixtureFile(join(directory, "fixtures.sha256"), "utf8");
  assert.ok(!manifestText.endsWith("\n"), "P3f manifest must not end with a newline");
  const entries = manifestText.split("\n").map((line) => {
    const match = line.match(/^([a-z0-9-]+) sha256 ([0-9a-f]{64}) ([a-z0-9.-]+)$/);
    assert.ok(match, `invalid P3f hash manifest entry: ${line}`);
    return { version: match[1], digest: match[2], name: match[3] };
  });
  const manifest = new Map(entries.map(({ name, digest: entryDigest }) => [name, entryDigest]));
  assert.equal(manifest.get(p3fAdapterFixtureName), p3fAdapterDigest, "P3f adapter manifest binding");
  assert.equal(manifest.get(p3fRedFlagsFixtureName), p3fRedFlagsDigest, "P3f adapter denial manifest binding");
  const hardDigests = new Map([[p3fAdapterFixtureName, p3fAdapterDigest], [p3fRedFlagsFixtureName, p3fRedFlagsDigest]]);
  const adapter = await readCanonical(directory, p3fAdapterFixtureName, manifest, hardDigests, readFixtureFile);
  const redFlags = await readCanonical(directory, p3fRedFlagsFixtureName, manifest, hardDigests, readFixtureFile);
  assertP3fFixture(adapter, p3fAdapterFixtureName);
  assertP3fFixture(redFlags, p3fRedFlagsFixtureName);
  return { adapter, redFlags };
}

async function readP4Fixtures(directory, readFixtureFile = readFile) {
  const text = await readFixtureFile(join(directory, "fixtures.sha256"), "utf8");
  assert.ok(!text.endsWith("\n"), "P4 manifest must not end with a newline");
  const entries = text.split("\n").map((line) => {
    const match = line.match(/^([a-z0-9-]+) sha256 ([0-9a-f]{64}) ([a-z0-9.-]+)$/);
    assert.ok(match, `invalid P4 hash manifest entry: ${line}`);
    return { version: match[1], digest: match[2], name: match[3] };
  });
  assert.deepEqual(entries.map(({ name }) => name), fixtureNames, "P4 fixture manifest inventory");
  entries.forEach(({ version }) => assert.equal(version, fixtureHashVersion, "P4 fixture manifest version"));
  const manifest = new Map(entries.map(({ name, digest: entryDigest }) => [name, entryDigest]));
  return Object.fromEntries(await Promise.all(fixtureNames.map(async (name) => [name, await readCanonical(directory, name, manifest, fixtureDigests, readFixtureFile)])));
}

function assertFailureProjection(value, name) {
  assert.deepEqual(value, failureProjection, `${name} must project only waiting_for_human`);
}

function assertEvidenceRecords(value, schemas) {
  expectKeys(value, evidenceNames, "immutable evidence records");
  expectKeys(schemas, evidenceNames, "immutable evidence record schema inventory");
  for (const name of evidenceNames) {
    const record = value[name];
    const schema = schemas[name];
    expectKeys(schema, ["hash_field", "required_fields", "schema_version"], `${name} evidence schema`);
    assert.equal(schema.hash_field, "evidence_hash", `${name} evidence schema hash field`);
    assert.ok(Array.isArray(schema.required_fields), `${name} evidence schema required fields`);
    assert.deepEqual(schema.required_fields, Object.keys(record).sort(), `${name} evidence schema closed fields`);
    assert.equal(record.schema_version, schema.schema_version, `${name} evidence schema version`);
    assertSelfHash(record, "evidence_hash", `${name} evidence record`);
  }
  assert.equal(value.proposal.schema_version, "ananke.self-development-evidence-proposal.v1", "proposal evidence schema");
  assert.equal(value.proposal.proposal_id, "proposal_p1a_001", "proposal identity");
  assert.equal(value.proposal.p1_revision_hash, p1RevisionHash, "proposal P1 revision binding");
  assert.equal(value.revision.schema_version, "ananke.self-development-evidence-revision.v1", "revision evidence schema");
  assert.equal(value.revision.p1_revision_hash, p1RevisionHash, "revision P1 binding");
  assert.equal(value.revision.proposal_evidence_hash, value.proposal.evidence_hash, "revision proposal binding");
  assert.equal(value.approval.approval_state, "approved_for_bounded_repair_review_only", "approval review-only state");
  assert.equal(value.approval.p1_revision_hash, p1RevisionHash, "approval P1 binding");
  assertTimestamp(value.approval.issued_at, "approval issue time");
  assert.equal(value.fence.fence_generation, 8, "fence generation");
  assert.equal(value.fence.p3a_launch_spec_hash, expectedChain.p3a_launch_spec_hash, "fence P3a binding");
  assertTimestamp(value.fence.issued_at, "fence issue time");
  assert.equal(value.envelope.predecessor_envelope_hash, expectedChain.p3f_predecessor_envelope_hash, "envelope P3f binding");
  assert.equal(value.envelope.route_mapping_hash, expectedChain.p3f_route_mapping_hash, "envelope route binding");
  assert.equal(value.receipt.envelope_evidence_hash, value.envelope.evidence_hash, "receipt envelope binding");
  assert.equal(value.callback.receipt_evidence_hash, value.receipt.evidence_hash, "callback receipt binding");
  assert.equal(value.callback.callback_state, "evidence_only_not_execution_success", "callback cannot establish execution success");
  assert.equal(value.source.source_snapshot_hash, "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4", "source snapshot binding");
  assert.equal(value.source.source_state, "hash_only_no_source_access", "source no-access boundary");
  assert.equal(value.artifact.supervisor_artifact_sha256, "sha256:ac36f5816b1a6caaf4e4bed488e90d94c426cf9f126678c4c0f1eb50dc231a91", "artifact identity binding");
  assert.equal(value.artifact.artifact_state, "hash_only_no_artifact_access", "artifact no-access boundary");
  assert.equal(value.route.route_mapping_hash, expectedChain.p3f_route_mapping_hash, "route P3f binding");
  assert.equal(value.route.route_name, "ananke_self_development_evidence_repair_v1", "bounded repair route");
  assert.equal(value.test.test_state, "canonical_fixture_verifier_only", "test evidence boundary");
  assert.equal(value.evaluation.evaluation_class, "repair_admission_request", "evaluation request class");
  assert.equal(value.evaluation.test_evidence_hash, value.test.evidence_hash, "evaluation test binding");
  assertTimestamp(value.evaluation.issued_at, "evaluation issue time");
}

function evidenceHashes(records) {
  return Object.fromEntries(evidenceNames.map((name) => [`${name}_hash`, records[name].evidence_hash]));
}

function assertVerifierIdentities(value) {
  expectKeys(value, ["release", "trust"], "verifier identity inventory");
  const { release, trust } = value;
  expectKeys(trust, ["schema_version", "trust_identity_hash", "trust_root_id", "trust_root_spki_sha256"], "verifier trust identity");
  assert.equal(trust.schema_version, "ananke.self-development-verifier-trust-identity.v1", "verifier trust schema");
  assertIdentifier(trust.trust_root_id, "verifier trust root id");
  assertHash(trust.trust_root_spki_sha256, "verifier trust root SPKI");
  assertSelfHash(trust, "trust_identity_hash", "verifier trust identity");
  expectKeys(release, ["release_artifact_sha256", "release_id", "release_identity_hash", "release_manifest_hash", "schema_version", "trust_identity_hash"], "verifier release identity");
  assert.equal(release.schema_version, "ananke.self-development-verifier-release-identity.v1", "verifier release schema");
  assertIdentifier(release.release_id, "verifier release id");
  assertHash(release.release_artifact_sha256, "verifier release artifact");
  assertHash(release.release_manifest_hash, "verifier release manifest");
  assert.equal(release.trust_identity_hash, trust.trust_identity_hash, "verifier release trust binding");
  assertSelfHash(release, "release_identity_hash", "verifier release identity");
  return { release, trust };
}

function assertBundle(value, records, identities) {
  expectKeys(value, ["bundle_hash", "bundle_id", "evidence_hashes", "issued_at", "p3f_adapter_fixture_sha256", "p3f_adapter_red_flags_count", "p3f_adapter_red_flags_fixture_sha256", "schema_version", "verifier_release_identity_hash", "verifier_trust_identity_hash"], "immutable evidence bundle");
  assert.equal(value.schema_version, "ananke.self-development-evidence-bundle.v1", "evidence bundle schema");
  assertIdentifier(value.bundle_id, "evidence bundle id");
  assertTimestamp(value.issued_at, "evidence bundle issue time");
  assertSelfHash(value, "bundle_hash", "immutable evidence bundle");
  assert.deepEqual(value.evidence_hashes, evidenceHashes(records), "bundle exact evidence hash set");
  assert.equal(value.p3f_adapter_fixture_sha256, expectedChain.p3f_adapter_fixture_sha256, "bundle P3f fixture binding");
  assert.equal(value.p3f_adapter_red_flags_fixture_sha256, expectedChain.p3f_adapter_red_flags_fixture_sha256, "bundle P3f denial binding");
  assert.equal(value.p3f_adapter_red_flags_count, p3fRedFlagCount, "bundle P3f denial count");
  assert.equal(value.verifier_trust_identity_hash, identities.trust.trust_identity_hash, "bundle verifier trust binding");
  assert.equal(value.verifier_release_identity_hash, identities.release.release_identity_hash, "bundle verifier release binding");
}

function assertBoundedRepair(value, bundle, records, identities) {
  expectKeys(value, ["admission_hash", "admission_id", "admission_state", "allowed_role", "allowed_route_evidence_hash", "exact_evidence_bundle_hash", "exact_evidence_hashes", "fresh_approval_evidence_hash", "fresh_fence_evidence_hash", "inferred_success", "prior_approval_evidence_hash", "prior_fence_evidence_hash", "repair_attempt_cap", "repair_attempt_number", "schema_version", "typed_moa_grant"], "bounded repair admission");
  assert.equal(value.schema_version, "ananke.self-development-bounded-repair-admission.v1", "bounded repair admission schema");
  assertIdentifier(value.admission_id, "repair admission id");
  assert.equal(value.repair_attempt_cap, 2, "bounded repair attempt cap");
  assert.ok(Number.isInteger(value.repair_attempt_number) && value.repair_attempt_number >= 1 && value.repair_attempt_number <= value.repair_attempt_cap, "bounded repair attempt number");
  assert.equal(value.allowed_role, "self_development_repair_runner", "bounded repair allowed role");
  assert.equal(value.allowed_route_evidence_hash, records.route.evidence_hash, "bounded repair allowed route");
  assert.equal(value.exact_evidence_bundle_hash, bundle.bundle_hash, "bounded repair exact bundle requirement");
  assert.deepEqual(value.exact_evidence_hashes, bundle.evidence_hashes, "bounded repair exact evidence requirement");
  assert.notEqual(value.prior_approval_evidence_hash, value.fresh_approval_evidence_hash, "bounded repair requires fresh approval");
  assert.notEqual(value.prior_fence_evidence_hash, value.fresh_fence_evidence_hash, "bounded repair requires fresh fence");
  assert.equal(value.fresh_approval_evidence_hash, records.approval.evidence_hash, "fresh approval evidence binding");
  assert.equal(value.fresh_fence_evidence_hash, records.fence.evidence_hash, "fresh fence evidence binding");
  assert.equal(value.admission_state, "design_only_no_repair_execution", "repair remains design-only");
  assert.equal(value.inferred_success, "forbidden", "repair success inference forbidden");
  const grant = value.typed_moa_grant;
  expectKeys(grant, ["approval_evidence_hash", "evidence_bundle_hash", "fence_evidence_hash", "grant_hash", "grant_id", "grantee_role", "issued_at", "issuer_trust_identity_hash", "not_after", "route_evidence_hash", "schema_version"], "typed MoA repair grant");
  assert.equal(grant.schema_version, "ananke.moa-typed-role-grant.v1", "typed MoA repair grant schema");
  assertIdentifier(grant.grant_id, "typed MoA repair grant id");
  assert.equal(grant.grantee_role, value.allowed_role, "typed MoA repair role");
  assert.equal(grant.route_evidence_hash, value.allowed_route_evidence_hash, "typed MoA repair route");
  assert.equal(grant.approval_evidence_hash, value.fresh_approval_evidence_hash, "typed MoA fresh approval binding");
  assert.equal(grant.fence_evidence_hash, value.fresh_fence_evidence_hash, "typed MoA fresh fence binding");
  assert.equal(grant.evidence_bundle_hash, bundle.bundle_hash, "typed MoA exact bundle binding");
  assert.equal(grant.issuer_trust_identity_hash, identities.trust.trust_identity_hash, "typed MoA verifier trust binding");
  assertTimestampBefore(records.evaluation.issued_at, records.approval.issued_at, "approval follows evaluation");
  assertTimestampBefore(records.evaluation.issued_at, records.fence.issued_at, "fence follows evaluation");
  assertTimestampBefore(records.approval.issued_at, grant.issued_at, "grant follows fresh approval");
  assertTimestampBefore(records.fence.issued_at, grant.issued_at, "grant follows fresh fence");
  assertTimestampBefore(grant.issued_at, grant.not_after, "typed MoA repair grant validity");
  assertSelfHash(grant, "grant_hash", "typed MoA repair grant");
  assertSelfHash(value, "admission_hash", "bounded repair admission");
}

function assertVerifierIO(value, bundle, repair, identities) {
  expectKeys(value, ["implementation", "input", "output", "replay"], "independent verifier contract");
  assert.equal(value.implementation, "dependency_free_canonical_fixture_oracle_no_runtime_or_repair", "independent verifier implementation boundary");
  const { input, output, replay } = value;
  expectKeys(input, ["bundle_hash", "input_hash", "p3f_adapter_fixture_sha256", "p3f_adapter_red_flags_count", "p3f_adapter_red_flags_fixture_sha256", "repair_admission_hash", "schema_version", "verifier_release_identity_hash", "verifier_trust_identity_hash"], "independent verifier input");
  assert.equal(input.schema_version, "ananke.self-development-evidence-verifier-input.v1", "verifier input schema");
  assert.equal(input.bundle_hash, bundle.bundle_hash, "verifier input bundle binding");
  assert.equal(input.repair_admission_hash, repair.admission_hash, "verifier input admission binding");
  assert.equal(input.p3f_adapter_fixture_sha256, expectedChain.p3f_adapter_fixture_sha256, "verifier input P3f fixture binding");
  assert.equal(input.p3f_adapter_red_flags_fixture_sha256, expectedChain.p3f_adapter_red_flags_fixture_sha256, "verifier input P3f denials binding");
  assert.equal(input.p3f_adapter_red_flags_count, p3fRedFlagCount, "verifier input P3f denial count");
  assert.equal(input.verifier_trust_identity_hash, identities.trust.trust_identity_hash, "verifier input trust binding");
  assert.equal(input.verifier_release_identity_hash, identities.release.release_identity_hash, "verifier input release binding");
  assertSelfHash(input, "input_hash", "independent verifier input");
  expectKeys(output, ["admission", "bundle_hash", "output_hash", "repair_execution", "schema_version", "state", "verification_state"], "independent verifier output");
  assert.equal(output.schema_version, "ananke.self-development-evidence-verifier-output.v1", "verifier output schema");
  assert.equal(output.admission, "bounded_repair_admissible_design_only", "verifier output admission");
  assert.equal(output.bundle_hash, bundle.bundle_hash, "verifier output bundle binding");
  assert.equal(output.repair_execution, "not_authorized_by_verifier", "verifier output execution boundary");
  assert.equal(output.state, "waiting_for_human", "verifier output human state");
  assert.equal(output.verification_state, "verified", "verifier output verification state");
  assertSelfHash(output, "output_hash", "independent verifier output");
  expectKeys(replay, ["input_hash", "new_durable_facts", "output_hash", "replay_hash", "replay_result", "schema_version"], "independent verifier replay");
  assert.equal(replay.schema_version, "ananke.self-development-evidence-verifier-replay.v1", "verifier replay schema");
  assert.equal(replay.input_hash, input.input_hash, "replay input binding");
  assert.equal(replay.output_hash, output.output_hash, "replay output binding");
  assert.equal(replay.new_durable_facts, 0, "replay cannot append durable facts");
  assert.equal(replay.replay_result, "exact_canonical_output", "replay result");
  assertSelfHash(replay, "replay_hash", "independent verifier replay");
}

function verifyFixture(value) {
  assertNoRawAuthority(value);
  expectKeys(value, ["bounded_repair_admission", "contract_state", "evidence_record_schemas", "evidence_records", "failure_projection", "immutable_evidence_bundle", "independent_verifier", "predecessor_chain", "schema_version", "verifier_identities"], "P4 evidence and repair fixture");
  assert.equal(value.schema_version, "ananke.self-development-evidence-repair-admission-design.v1", "P4 fixture schema");
  assert.equal(value.contract_state, "design_only_no_supervisor_network_omp_repair_or_vcs", "P4 design-only boundary");
  assertFailureProjection(value.failure_projection, "P4 failure projection");
  assert.deepEqual(value.predecessor_chain, expectedChain, "P1 through P3f chain binding");
  assertEvidenceRecords(value.evidence_records, value.evidence_record_schemas);
  const identities = assertVerifierIdentities(value.verifier_identities);
  assertBundle(value.immutable_evidence_bundle, value.evidence_records, identities);
  assertBoundedRepair(value.bounded_repair_admission, value.immutable_evidence_bundle, value.evidence_records, identities);
  assertVerifierIO(value.independent_verifier, value.immutable_evidence_bundle, value.bounded_repair_admission, identities);
  return value.independent_verifier.output;
}

function verifyRedFlags(value) {
  assertNoRawAuthority(value);
  expectKeys(value, ["cases", "schema_version"], "P4 repair denial fixture");
  assert.equal(value.schema_version, "ananke.self-development-evidence-repair-admission.red-flags.v1", "P4 repair denial schema");
  assert.equal(value.cases.length, redFlagKinds.length, "P4 exact repair denial count");
  value.cases.forEach((testCase, index) => {
    const kind = redFlagKinds[index];
    expectKeys(testCase, ["given", "id", "kind", "then"], `P4 repair denial ${index + 1}`);
    assert.equal(testCase.id, `${kind}_waits_for_human`, `P4 repair denial ${index + 1} id`);
    assert.equal(testCase.kind, kind, `P4 repair denial ${index + 1} kind`);
    assert.deepEqual(testCase.given, { class: kind }, `P4 repair denial ${index + 1} given`);
    assertFailureProjection(testCase.then, `P4 repair denial ${index + 1}`);
  });
}

async function verify(directory, p3fDirectory, { onP3fAuthenticated = () => {}, readFixtureFile = readFile } = {}) {
  const p3f = await authenticateP3f(p3fDirectory, readFixtureFile);
  onP3fAuthenticated(p3f);
  const fixtures = await readP4Fixtures(directory, readFixtureFile);
  const output = verifyFixture(fixtures["evidence-repair-admission-v1.canonical.json"]);
  verifyRedFlags(fixtures["repair-admission-red-flags-v1.canonical.json"]);
  return output;
}

async function assertRejected(action, pattern, name) {
  await assert.rejects(action, pattern, `${name} rejection reason`);
}

function isP4FixtureRead(path) {
  const absolutePath = resolve(path);
  return absolutePath === join(sourceFixtureDirectory, "fixtures.sha256") || fixtureNames.some((name) => absolutePath === join(sourceFixtureDirectory, name));
}

async function assertP3fFirstDependency() {
  const reads = [];
  let p3fAuthenticated = false;
  const tracedReadFile = async (...args) => {
    const absolutePath = resolve(args[0]);
    if (isP4FixtureRead(absolutePath)) assert.ok(p3fAuthenticated, "P3f adapter evidence must authenticate before any P4 fixture read");
    reads.push(absolutePath);
    return readFile(...args);
  };
  await verify(sourceFixtureDirectory, sourceP3fFixtureDirectory, {
    onP3fAuthenticated: () => { p3fAuthenticated = true; },
    readFixtureFile: tracedReadFile,
  });
  assert.ok(p3fAuthenticated, "P3f adapter authentication dependency proof");
  let p4ReadAfterRejectedP3f = false;
  const corruptP3fReadFile = async (...args) => {
    const absolutePath = resolve(args[0]);
    if (isP4FixtureRead(absolutePath)) p4ReadAfterRejectedP3f = true;
    if (absolutePath === join(sourceP3fFixtureDirectory, p3fAdapterFixtureName)) return Buffer.from("{}", "utf8");
    return readFile(...args);
  };
  await assertRejected(
    () => verify(sourceFixtureDirectory, sourceP3fFixtureDirectory, { readFixtureFile: corruptP3fReadFile }),
    /fixture digest mismatch: independent-supervisor-protocol-adapter-v1\.canonical\.json/,
    "P3f adapter authentication precedes P4 reads",
  );
  assert.equal(p4ReadAfterRejectedP3f, false, "rejected P3f adapter evidence must prevent every P4 read");
}

function rehashSelf(value, hashField) {
  const hashInput = { ...value };
  delete hashInput[hashField];
  value[hashField] = hashCanonical(hashInput);
}

function invalidHash(label) {
  return `sha256:${digest(Buffer.from(`invalid:${label}`, "utf8"))}`;
}

function rehashRepairAndVerifier(fixture) {
  const repair = fixture.bounded_repair_admission;
  const { input, output, replay } = fixture.independent_verifier;
  if (repair.typed_moa_grant !== null) rehashSelf(repair.typed_moa_grant, "grant_hash");
  rehashSelf(repair, "admission_hash");
  input.repair_admission_hash = repair.admission_hash;
  rehashSelf(input, "input_hash");
  rehashSelf(output, "output_hash");
  replay.input_hash = input.input_hash;
  replay.output_hash = output.output_hash;
  rehashSelf(replay, "replay_hash");
}

function rehashBundleAndDependents(fixture) {
  const bundle = fixture.immutable_evidence_bundle;
  const repair = fixture.bounded_repair_admission;
  const { input, output } = fixture.independent_verifier;
  rehashSelf(bundle, "bundle_hash");
  repair.exact_evidence_bundle_hash = bundle.bundle_hash;
  repair.exact_evidence_hashes = structuredClone(bundle.evidence_hashes);
  repair.typed_moa_grant.evidence_bundle_hash = bundle.bundle_hash;
  input.bundle_hash = bundle.bundle_hash;
  output.bundle_hash = bundle.bundle_hash;
  rehashRepairAndVerifier(fixture);
}

function rehashInputAndReplay(fixture) {
  const { input, replay } = fixture.independent_verifier;
  rehashSelf(input, "input_hash");
  replay.input_hash = input.input_hash;
  rehashSelf(replay, "replay_hash");
}

const p4DenialMutators = new Map([
  ["p3f_adapter_fixture_drift", {
    mutate: (fixture) => { fixture.predecessor_chain.p3f_adapter_fixture_sha256 = invalidHash("p3f_adapter_fixture"); },
    pattern: /P1 through P3f chain binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["p3f_adapter_denial_count_drift", {
    mutate: (fixture) => { fixture.predecessor_chain.p3f_adapter_red_flags_count = p3fRedFlagCount - 1; },
    pattern: /P1 through P3f chain binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["p3f_adapter_denial_digest_drift", {
    mutate: (fixture) => { fixture.predecessor_chain.p3f_adapter_red_flags_fixture_sha256 = invalidHash("p3f_adapter_denial_digest"); },
    pattern: /P1 through P3f chain binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["bundle_hash_drift", {
    mutate: (fixture) => { fixture.immutable_evidence_bundle.bundle_hash = invalidHash("bundle_hash"); },
    pattern: /immutable evidence bundle canonical self hash/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["bundle_schema_drift", {
    mutate: (fixture) => {
      fixture.immutable_evidence_bundle.schema_version = "ananke.self-development-evidence-bundle.v2";
      rehashBundleAndDependents(fixture);
    },
    pattern: /evidence bundle schema/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ...evidenceNames.map((name) => [`${name}_evidence_hash_drift`, {
    mutate: (fixture) => {
      fixture.immutable_evidence_bundle.evidence_hashes[`${name}_hash`] = invalidHash(`${name}_evidence_hash`);
      rehashBundleAndDependents(fixture);
    },
    pattern: /bundle exact evidence hash set/,
    validate: (fixture) => verifyFixture(fixture),
  }]),
  ["evidence_record_self_hash_drift", {
    mutate: (fixture) => { fixture.evidence_records.source.source_state = "other"; },
    pattern: /source evidence record canonical self hash/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["input_canonicality_failure", {
    mutate: (fixture) => { fixture.independent_verifier.input.input_hash = invalidHash("input_canonicality"); },
    pattern: /independent verifier input canonical self hash/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["input_p3f_binding_drift", {
    mutate: (fixture) => {
      fixture.independent_verifier.input.p3f_adapter_fixture_sha256 = invalidHash("input_p3f_binding");
      rehashInputAndReplay(fixture);
    },
    pattern: /verifier input P3f fixture binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["verifier_trust_identity_drift", {
    mutate: (fixture) => {
      fixture.immutable_evidence_bundle.verifier_trust_identity_hash = invalidHash("bundle_trust_identity");
      rehashBundleAndDependents(fixture);
    },
    pattern: /bundle verifier trust binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["verifier_release_identity_drift", {
    mutate: (fixture) => {
      fixture.verifier_identities.release.trust_identity_hash = invalidHash("release_trust_identity");
      rehashSelf(fixture.verifier_identities.release, "release_identity_hash");
    },
    pattern: /verifier release trust binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["replay_output_drift", {
    mutate: (fixture) => {
      fixture.independent_verifier.replay.output_hash = invalidHash("replay_output");
      rehashSelf(fixture.independent_verifier.replay, "replay_hash");
    },
    pattern: /replay output binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["repair_attempt_cap_exceeded", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.repair_attempt_number = fixture.bounded_repair_admission.repair_attempt_cap + 1;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair attempt number/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["repair_attempt_number_nonpositive", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.repair_attempt_number = 0;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair attempt number/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["repair_role_not_allowed", {
    mutate: (fixture) => {
      const repair = fixture.bounded_repair_admission;
      repair.allowed_role = "other_repair_role";
      repair.typed_moa_grant.grantee_role = repair.allowed_role;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair allowed role/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["repair_route_not_allowed", {
    mutate: (fixture) => {
      const repair = fixture.bounded_repair_admission;
      repair.allowed_route_evidence_hash = invalidHash("repair_route");
      repair.typed_moa_grant.route_evidence_hash = repair.allowed_route_evidence_hash;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair allowed route/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["exact_evidence_bundle_missing", {
    mutate: (fixture) => {
      const repair = fixture.bounded_repair_admission;
      repair.exact_evidence_bundle_hash = null;
      repair.typed_moa_grant.evidence_bundle_hash = null;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair exact bundle requirement/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["exact_evidence_hash_set_drift", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.exact_evidence_hashes.test_hash = invalidHash("repair_evidence_hashes");
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair exact evidence requirement/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["fresh_approval_absent", {
    mutate: (fixture) => {
      const repair = fixture.bounded_repair_admission;
      repair.prior_approval_evidence_hash = repair.fresh_approval_evidence_hash;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair requires fresh approval/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["fresh_fence_absent", {
    mutate: (fixture) => {
      const repair = fixture.bounded_repair_admission;
      repair.prior_fence_evidence_hash = repair.fresh_fence_evidence_hash;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /bounded repair requires fresh fence/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["moa_grant_missing", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.typed_moa_grant = null;
      rehashRepairAndVerifier(fixture);
    },
    pattern: /typed MoA repair grant must be an object/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["moa_grant_role_drift", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.typed_moa_grant.grantee_role = "other_repair_role";
      rehashRepairAndVerifier(fixture);
    },
    pattern: /typed MoA repair role/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["moa_grant_route_drift", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.typed_moa_grant.route_evidence_hash = invalidHash("moa_grant_route");
      rehashRepairAndVerifier(fixture);
    },
    pattern: /typed MoA repair route/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["moa_grant_evidence_drift", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.typed_moa_grant.evidence_bundle_hash = invalidHash("moa_grant_evidence");
      rehashRepairAndVerifier(fixture);
    },
    pattern: /typed MoA exact bundle binding/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["repair_success_inferred", {
    mutate: (fixture) => {
      fixture.bounded_repair_admission.inferred_success = "allowed";
      rehashRepairAndVerifier(fixture);
    },
    pattern: /repair success inference forbidden/,
    validate: (fixture) => verifyFixture(fixture),
  }],
  ["failure_result_inferred", {
    mutate: (_fixture, redFlags) => {
      redFlags.cases.find(({ kind }) => kind === "failure_result_inferred").then.admission = "repair_result_inferred";
    },
    pattern: /waiting_for_human/,
    validate: (_fixture, redFlags) => verifyRedFlags(redFlags),
  }],
  ["review_finding_result_inferred", {
    mutate: (_fixture, redFlags) => {
      redFlags.cases.find(({ kind }) => kind === "review_finding_result_inferred").then.repair_execution = "completed";
    },
    pattern: /waiting_for_human/,
    validate: (_fixture, redFlags) => verifyRedFlags(redFlags),
  }],
]);

async function assertP4DenialMutators(canonical, redFlags) {
  const fixtureKinds = redFlags.cases.map(({ kind }) => kind);
  assert.equal(p4DenialMutators.size, fixtureKinds.length, "P4 denial mutator map cardinality");
  assert.deepEqual([...p4DenialMutators.keys()], fixtureKinds, "P4 denial mutator map exactly matches fixture kinds");
  assert.deepEqual(fixtureKinds, redFlagKinds, "P4 fixture inventory exactly matches denial kinds");
  for (const [kind, mutator] of p4DenialMutators) {
    const testCase = redFlags.cases.find((entry) => entry.kind === kind);
    assert.ok(testCase, `P4 denial mutator ${kind} fixture case`);
    assert.deepEqual(testCase.then, canonical.failure_projection, `${kind} must retain the canonical failure projection`);
    assertFailureProjection(testCase.then, `${kind} denial projection`);
    const fixture = structuredClone(canonical);
    const invalidFlags = structuredClone(redFlags);
    mutator.mutate(fixture, invalidFlags);
    await assertRejected(async () => mutator.validate(fixture, invalidFlags), mutator.pattern, `${kind} targeted rejection`);
  }
}

async function selfTest() {
  await assertP3fFirstDependency();
  const fixtures = await readP4Fixtures(sourceFixtureDirectory);
  const canonical = fixtures["evidence-repair-admission-v1.canonical.json"];
  const redFlags = fixtures["repair-admission-red-flags-v1.canonical.json"];
  verifyFixture(canonical);
  verifyRedFlags(redFlags);
  await assertP4DenialMutators(canonical, redFlags);
  const incompleteFlags = structuredClone(redFlags);
  incompleteFlags.cases.pop();
  await assertRejected(async () => verifyRedFlags(incompleteFlags), /exact repair denial count/, "repair denial inventory count");
  const driftedFlags = structuredClone(redFlags);
  driftedFlags.cases[0].given.class = "other";
  await assertRejected(async () => verifyRedFlags(driftedFlags), /given/, "repair denial input drift");
}

if (process.argv.includes("--self-test")) {
  await selfTest();
  console.log(`P4 independent fixture verifier self-test authenticated the P3f protocol-adapter fixture and its exact ${p3fRedFlagCount}-case denial oracle before P4 reads, then exercised a one-to-one, rehashed ${redFlagKinds.length}-case P4 denial-mutator map: every target validator rejected its intended invariant and every declared denial retained the identical closed waiting_for_human projection.`);
} else {
  await verify(
    resolve(optionValue("--fixtures") ?? sourceFixtureDirectory),
    resolve(optionValue("--p3f-fixtures") ?? sourceP3fFixtureDirectory),
  );
  console.log(`P4 evidence and bounded-repair design fixtures verified: canonical immutable proposal/revision/approval/fence/envelope/receipt/callback/source/artifact/route/test/evaluation hashes, independent verifier trust/release identity and replayable input/output, P1 through P3f bindings including the exact ${p3fRedFlagCount}-case P3f adapter denial oracle, and design-only bounded repair admission that remains waiting_for_human.`);
}
