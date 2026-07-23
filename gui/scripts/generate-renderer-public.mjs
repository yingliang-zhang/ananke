import { spawnSync } from "node:child_process";
import { access, mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const guiDirectory = resolve(scriptDirectory, "..");
const rendererPublicModels = [
  {
    schemaPath: resolve(guiDirectory, "contracts/renderer-public-bootstrap.schema.json"),
    topLevel: "Bootstrap",
    targets: [
      {
        language: "rust",
        path: resolve(guiDirectory, "src-tauri/src/generated/renderer_public_bootstrap.rs"),
        args: ["--visibility", "public", "--derive-partial-eq"],
      },
      {
        language: "typescript",
        path: resolve(guiDirectory, "src/generated/renderer-public-bootstrap.ts"),
      },
    ],
  },
  {
    schemaPath: resolve(guiDirectory, "contracts/renderer-public-cancel.schema.json"),
    topLevel: "Cancel",
    targets: [
      {
        language: "rust",
        path: resolve(guiDirectory, "src-tauri/src/generated/renderer_public_cancel.rs"),
        args: ["--visibility", "public", "--derive-partial-eq"],
      },
      {
        language: "typescript",
        path: resolve(guiDirectory, "src/generated/renderer-public-cancel.ts"),
      },
    ],
  },
  {
    schemaPath: resolve(guiDirectory, "contracts/renderer-public-run.schema.json"),
    topLevel: "Run",
    targets: [
      {
        language: "rust",
        path: resolve(guiDirectory, "src-tauri/src/generated/renderer_public_run.rs"),
        args: ["--visibility", "public", "--derive-partial-eq"],
      },
      {
        language: "typescript",
        path: resolve(guiDirectory, "src/generated/renderer-public-run.ts"),
      },
    ],
  },
  {
    schemaPath: resolve(guiDirectory, "contracts/renderer-public-event.schema.json"),
    topLevel: "Event",
    targets: [
      {
        language: "rust",
        path: resolve(guiDirectory, "src-tauri/src/generated/renderer_public_event.rs"),
        args: ["--visibility", "public", "--derive-partial-eq"],
      },
      {
        language: "typescript",
        path: resolve(guiDirectory, "src/generated/renderer-public-event.ts"),
      },
    ],
  },
  {
    schemaPath: resolve(guiDirectory, "contracts/renderer-public-health.schema.json"),
    topLevel: "Health",
    targets: [
      {
        language: "rust",
        path: resolve(guiDirectory, "src-tauri/src/generated/renderer_public_health.rs"),
        args: ["--visibility", "public", "--derive-partial-eq"],
      },
      {
        language: "typescript",
        path: resolve(guiDirectory, "src/generated/renderer-public-health.ts"),
      },
    ],
  },
  ...[
    ["renderer-public-proposal-create-input.schema.json", "CreateProposalInput", "proposal-create-input"],
    ["renderer-public-proposal-list-input.schema.json", "ListProposalsInput", "proposal-list-input"],
    ["renderer-public-proposal-get-input.schema.json", "GetProposalInput", "proposal-get-input"],
    ["renderer-public-proposal-activity-list-input.schema.json", "ListProposalActivityInput", "proposal-activity-list-input"],
    ["renderer-public-proposal-append-input.schema.json", "AppendProposalRevisionInput", "proposal-append-input"],
    ["renderer-public-proposal-decision-input.schema.json", "DecideProposalApprovalInput", "proposal-decision-input"],
    ["renderer-public-proposal-withdraw-input.schema.json", "WithdrawProposalInput", "proposal-withdraw-input"],
    ["renderer-public-proposal-mutation.schema.json", "ProposalMutation", "proposal-mutation"],
    ["renderer-public-proposal-list.schema.json", "ProposalList", "proposal-list"],
    ["renderer-public-proposal-detail.schema.json", "ProposalDetail", "proposal-detail"],
    ["renderer-public-proposal-activity-list.schema.json", "ProposalActivityList", "proposal-activity-list"],
    [
      "renderer-public-proposal-activity-list.schema.json",
      "ProposalActivity",
      "proposal-activity",
      ["properties", "activity", "items"],
    ],
    ["renderer-public-grill-evaluate-input.schema.json", "EvaluateGrillInput", "grill-evaluate-input"],
    ["renderer-public-grill-record-default-input.schema.json", "RecordGrillDefaultInput", "grill-record-default-input"],
    ["renderer-public-grill-record-answer-input.schema.json", "RecordGrillAnswerInput", "grill-record-answer-input"],
    ["renderer-public-grill-record-override-input.schema.json", "RecordGrillOverrideInput", "grill-record-override-input"],
    ["renderer-public-grill-evaluation.schema.json", "GrillEvaluation", "grill-evaluation"],
    [
      "renderer-public-grill-evaluation.schema.json",
      "GrillQuestion",
      "grill-question",
      ["properties", "shown_questions", "items"],
    ],
    ["renderer-public-grill-default-record.schema.json", "GrillDefaultRecord", "grill-default-record"],
    ["renderer-public-grill-answer-record.schema.json", "GrillAnswerRecord", "grill-answer-record"],
    ["renderer-public-grill-override-record.schema.json", "GrillOverrideRecord", "grill-override-record"],
  ].map(([schemaFile, topLevel, outputName, schemaSelector]) => ({
    schemaPath: resolve(guiDirectory, "contracts", schemaFile),
    topLevel,
    schemaSelector,
    targets: [
      {
        language: "rust",
        path: resolve(guiDirectory, "src-tauri/src/generated", `renderer_public_${outputName.replaceAll("-", "_")}.rs`),
        args: ["--visibility", "public", "--derive-partial-eq"],
      },
      {
        language: "typescript",
        path: resolve(guiDirectory, "src/generated", `renderer-public-${outputName}.ts`),
      },
    ],
  })),
];
const quicktypePath = resolve(
  guiDirectory,
  "node_modules/.bin",
  process.platform === "win32" ? "quicktype.cmd" : "quicktype",
);
const targets = rendererPublicModels.flatMap((model) =>
  model.targets.map((target) => ({
    ...target,
    schemaPath: model.schemaPath,
    schemaSelector: model.schemaSelector,
    topLevel: model.topLevel,
  })),
);
const rustModulePath = resolve(guiDirectory, "src-tauri/src/generated/mod.rs");
const grillRustContractTestSource = `#[cfg(test)]
mod grill_contract_tests {
    use serde::de::DeserializeOwned;
    use serde_json::{json, Value};

    fn canonical_fixture() -> Value {
        serde_json::from_str(include_str!("../../../../contracts/p2c/fixtures/protocol-v1.canonical.json"))
            .expect("decode canonical P2c fixture")
    }

    fn assert_rejected<T: DeserializeOwned>(value: Value, message: &str) {
        assert!(serde_json::from_value::<T>(value).is_err(), "{message}");
    }

    fn assert_decoder<T: DeserializeOwned>(canonical: &Value) {
        assert!(
            serde_json::from_value::<T>(canonical.clone()).is_ok(),
            "canonical P2c DTO must decode"
        );
        for field in [
            "cmd", "command", "token", "error", "socket_path", "identity", "worker",
            "process", "pid", "path", "root", "secret", "credential", "password", "model",
            "prompt", "prose", "approval", "execution", "execute", "runtime", "transport",
            "input_hash", "rule_version", "declarations", "raw",
        ] {
            let mut injected = canonical.clone();
            injected[field] = json!(true);
            assert_rejected::<T>(injected, "P2c DTO must reject private or unknown fields");
        }
        for (field, value) in [
            ("proposal_id", json!("1")),
            ("revision", json!(0)),
            ("revision_hash", json!("sha256:not-a-hash")),
        ] {
            let mut invalid = canonical.clone();
            invalid[field] = value;
            assert_rejected::<T>(invalid, "P2c DTO must enforce the Revision identity schema");
        }
    }

    #[test]
    fn generated_grill_dto_decoders_enforce_the_p2c_contract() {
        let fixture = canonical_fixture();
        let evaluate_input = &fixture["commands"]["evaluate_grill"]["input"];
        let evaluation = &fixture["commands"]["evaluate_grill"]["result"];
        let question = &evaluation["shown_questions"][0];
        let default_input = &fixture["commands"]["record_grill_default"]["input"];
        let answer_input = &fixture["commands"]["record_grill_answer"]["input"];
        let override_input = &fixture["commands"]["record_grill_override"]["input"];
        let default_record = &fixture["commands"]["record_grill_default"]["result"];
        let answer_record = &fixture["commands"]["record_grill_answer"]["result"];
        let override_record = &fixture["commands"]["record_grill_override"]["result"];

        assert_decoder::<super::renderer_public_grill_evaluate_input::EvaluateGrillInput>(evaluate_input);
        assert_decoder::<super::renderer_public_grill_record_default_input::RecordGrillDefaultInput>(default_input);
        assert_decoder::<super::renderer_public_grill_record_answer_input::RecordGrillAnswerInput>(answer_input);
        assert_decoder::<super::renderer_public_grill_record_override_input::RecordGrillOverrideInput>(override_input);
        assert_decoder::<super::renderer_public_grill_evaluation::GrillEvaluation>(evaluation);
        assert_decoder::<super::renderer_public_grill_question::GrillQuestion>(question);
        assert_decoder::<super::renderer_public_grill_default_record::GrillDefaultRecord>(default_record);
        assert_decoder::<super::renderer_public_grill_answer_record::GrillAnswerRecord>(answer_record);
        assert_decoder::<super::renderer_public_grill_override_record::GrillOverrideRecord>(override_record);

        for (canonical, decoder) in [
            (default_input, assert_rejected::<super::renderer_public_grill_record_default_input::RecordGrillDefaultInput> as fn(Value, &str)),
            (answer_input, assert_rejected::<super::renderer_public_grill_record_answer_input::RecordGrillAnswerInput> as fn(Value, &str)),
            (override_input, assert_rejected::<super::renderer_public_grill_record_override_input::RecordGrillOverrideInput> as fn(Value, &str)),
        ] {
            let mut invalid = canonical.clone();
            invalid["question_id"] = json!("grill_question_unknown");
            decoder(invalid, "record input must enforce the Question ID pattern");
        }

        let mut invalid_question = question.clone();
        invalid_question["question_id"] = json!("grill_question_autonomy_budget");
        assert_rejected::<super::renderer_public_grill_question::GrillQuestion>(invalid_question, "Question ID must match its rule class");
        let mut invalid_question = question.clone();
        invalid_question["question_sequence"] = json!(0);
        assert_rejected::<super::renderer_public_grill_question::GrillQuestion>(invalid_question, "Question sequence must stay in schema bounds");
        let mut invalid_question = question.clone();
        invalid_question["record_sequence"] = json!(41);
        assert_rejected::<super::renderer_public_grill_question::GrillQuestion>(invalid_question, "Question record sequence must stay in schema bounds");
        let mut invalid_question = question.clone();
        invalid_question["blocking"] = json!(false);
        assert_rejected::<super::renderer_public_grill_question::GrillQuestion>(invalid_question, "Question blocking must remain true");
        let mut invalid_question = question.clone();
        invalid_question["risk"] = json!("medium");
        assert_rejected::<super::renderer_public_grill_question::GrillQuestion>(invalid_question, "Question rule properties must match the fixed P2b rule");
        let mut invalid_question = question.clone();
        invalid_question["written_at"] = json!("not-a-timestamp");
        assert_rejected::<super::renderer_public_grill_question::GrillQuestion>(invalid_question, "Question timestamp must be semantic UTC");

        let mut six_questions = evaluation.clone();
        let extra_question = six_questions["shown_questions"][4].clone();
        six_questions["shown_questions"].as_array_mut().expect("shown Questions array").push(extra_question);
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(six_questions, "Evaluation must show at most five Questions");
        let mut six_question_ids = evaluation.clone();
        six_question_ids["new_question_ids"].as_array_mut().expect("new Question IDs array").push(json!("grill_question_autonomy_budget"));
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(six_question_ids, "Evaluation must append at most five Question IDs");
        let mut seven_deferred = evaluation.clone();
        for _ in 0..6 {
            seven_deferred["deferred_rule_classes"].as_array_mut().expect("deferred rule classes array").push(json!("autonomy_budget"));
        }
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(seven_deferred, "Evaluation must bound deferred rule classes");
        let mut seven_records = evaluation.clone();
        seven_records["new_records"] = json!(7);
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(seven_records, "Evaluation must bound new records");
        let mut mismatched_identity = evaluation.clone();
        mismatched_identity["shown_questions"][0]["revision"] = json!(2);
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(mismatched_identity, "Question Revision identity must match Evaluation identity");
        let mut mismatched_proposal = evaluation.clone();
        mismatched_proposal["shown_questions"][0]["proposal_id"] = json!("proposal_p1a_002");
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(mismatched_proposal, "Question proposal identity must match Evaluation identity");
        let mut mismatched_hash = evaluation.clone();
        mismatched_hash["shown_questions"][0]["revision_hash"] = json!(format!("sha256:{}", "0".repeat(64)));
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(mismatched_hash, "Question revision hash must match Evaluation identity");
        let mut mismatched_question_id = evaluation.clone();
        mismatched_question_id["shown_questions"][0]["question_id"] = json!("grill_question_autonomy_budget");
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(mismatched_question_id, "Question ID must match its rule class");
        let mut non_blocking_question = evaluation.clone();
        non_blocking_question["shown_questions"][0]["blocking"] = json!(false);
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(non_blocking_question, "Evaluation must reject non-blocking Questions");
        let mut reordered = evaluation.clone();
        reordered["shown_questions"].as_array_mut().expect("shown Questions array").swap(0, 1);
        reordered["new_question_ids"].as_array_mut().expect("new Question IDs array").swap(0, 1);
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(reordered, "Evaluation must retain P2b priority order");
        let mut unmatched_new_id = evaluation.clone();
        unmatched_new_id["new_question_ids"][0] = json!("grill_question_autonomy_budget");
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(unmatched_new_id, "new Question IDs must preserve shown Question order");
        let mut inconsistent_new_records = evaluation.clone();
        inconsistent_new_records["new_records"] = json!(5);
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(inconsistent_new_records, "new record count must match appended Question IDs");
        let mut clear_with_questions = evaluation.clone();
        clear_with_questions["status"] = json!("clear");
        assert_rejected::<super::renderer_public_grill_evaluation::GrillEvaluation>(clear_with_questions, "clear Evaluation cannot retain active Questions");

        for (canonical, decoder) in [
            (default_record, assert_rejected::<super::renderer_public_grill_default_record::GrillDefaultRecord> as fn(Value, &str)),
            (answer_record, assert_rejected::<super::renderer_public_grill_answer_record::GrillAnswerRecord> as fn(Value, &str)),
            (override_record, assert_rejected::<super::renderer_public_grill_override_record::GrillOverrideRecord> as fn(Value, &str)),
        ] {
            let mut invalid = canonical.clone();
            invalid["record_sequence"] = json!(0);
            decoder(invalid, "record sequence must enforce the minimum");
            let mut invalid = canonical.clone();
            invalid["record_sequence"] = json!(41);
            decoder(invalid, "record sequence must enforce the maximum");
            let mut invalid = canonical.clone();
            invalid["written_at"] = json!("not-a-timestamp");
            decoder(invalid, "record timestamp must be semantic UTC");
        }
        let mut invalid_default = default_record.clone();
        invalid_default["default"] = json!("acknowledged");
        assert_rejected::<super::renderer_public_grill_default_record::GrillDefaultRecord>(invalid_default, "default record must enforce its enum");
        let mut invalid_answer = answer_record.clone();
        invalid_answer["answer"] = json!("needs_rewrite");
        assert_rejected::<super::renderer_public_grill_answer_record::GrillAnswerRecord>(invalid_answer, "answer record must enforce its const");
        let mut invalid_override = override_record.clone();
        invalid_override["override"] = json!("acknowledged");
        assert_rejected::<super::renderer_public_grill_override_record::GrillOverrideRecord>(invalid_override, "override record must enforce its const");
    }
}
`;
const rustModuleSource = `// Generated by scripts/generate-renderer-public.mjs. DO NOT EDIT.\n\n${targets
  .filter((target) => target.language === "rust")
  .map((target) => `pub mod ${basename(target.path, ".rs")};`)
  .sort()
  .join("\n")}\n\n${grillRustContractTestSource}`;
const checkMode = process.argv.slice(2).includes("--check");
const privacyCheckMode = process.argv.slice(2).includes("--check-public-fields");
const unexpectedArguments = process.argv
  .slice(2)
  .filter((argument) => argument !== "--check" && argument !== "--check-public-fields");

if (unexpectedArguments.length > 0 || (checkMode && privacyCheckMode)) {
  throw new Error("usage: node scripts/generate-renderer-public.mjs [--check | --check-public-fields]");
}

if (Number.parseInt(process.versions.node.split(".")[0], 10) !== 22) {
  throw new Error(`Node 22 is required; found ${process.version}.`);
}

async function materializeSchema(target) {
  if (target.schemaSelector === undefined) {
    return { path: target.schemaPath, cleanup: async () => {} };
  }

  const source = JSON.parse(await readFile(target.schemaPath, "utf8"));
  const schema = target.schemaSelector.reduce((value, key) => value?.[key], source);
  if (schema === null || typeof schema !== "object" || Array.isArray(schema)) {
    throw new Error(`Cannot select ${target.topLevel} from ${target.schemaPath}.`);
  }

  const directory = await mkdtemp(join(tmpdir(), "ananke-renderer-public-"));
  const path = join(directory, "schema.json");
  await writeFile(
    path,
    JSON.stringify({
      $schema: source.$schema,
      $id: `${source.$id}#/${target.schemaSelector.join("/")}`,
      ...schema,
      title: target.topLevel,
    }),
  );
  return { path, cleanup: () => rm(directory, { force: true, recursive: true }) };
}

const grillRustContracts = {
  EvaluateGrillInput: {
    fields: [
      ["proposal_id", "String"],
      ["revision", "i64"],
      ["revision_hash", "String"],
    ],
    validate: "p2c_validate_identity(&self.proposal_id, self.revision, &self.revision_hash)?;",
  },
  RecordGrillDefaultInput: {
    fields: [
      ["proposal_id", "String"],
      ["question_id", "String"],
      ["revision", "i64"],
      ["revision_hash", "String"],
    ],
    validate: "p2c_validate_identity(&self.proposal_id, self.revision, &self.revision_hash)?;\n        p2c_validate_question_id(&self.question_id)?;",
  },
  RecordGrillAnswerInput: {
    fields: [
      ["proposal_id", "String"],
      ["question_id", "String"],
      ["revision", "i64"],
      ["revision_hash", "String"],
    ],
    validate: "p2c_validate_identity(&self.proposal_id, self.revision, &self.revision_hash)?;\n        p2c_validate_question_id(&self.question_id)?;",
  },
  RecordGrillOverrideInput: {
    fields: [
      ["proposal_id", "String"],
      ["question_id", "String"],
      ["revision", "i64"],
      ["revision_hash", "String"],
    ],
    validate: "p2c_validate_identity(&self.proposal_id, self.revision, &self.revision_hash)?;\n        p2c_validate_question_id(&self.question_id)?;",
  },
  GrillQuestion: {
    fields: [
      ["blocking", "bool"],
      ["grill_question_default", "Default", "default"],
      ["proposal_id", "String"],
      ["question_id", "String"],
      ["question_sequence", "i32"],
      ["record_sequence", "i32"],
      ["remedial_step", "RemedialStep"],
      ["revision", "i64"],
      ["revision_hash", "String"],
      ["risk", "Risk"],
      ["rule_class", "RuleClass"],
      ["waivable", "bool"],
      ["written_at", "String"],
      ["written_by", "WrittenBy"],
    ],
    validate: "p2c_validate_question(self)?;",
  },
  GrillEvaluation: {
    fields: [
      ["deferred_rule_classes", "Vec<RuleClass>"],
      ["new_question_ids", "Vec<String>"],
      ["new_records", "i32"],
      ["proposal_id", "String"],
      ["revision", "i64"],
      ["revision_hash", "String"],
      ["shown_questions", "Vec<GrillQuestion>"],
      ["status", "Status"],
    ],
    validate: "p2c_validate_evaluation(self)?;",
  },
  GrillDefaultRecord: {
    fields: [
      ["grill_default_record_default", "Default", "default"],
      ["proposal_id", "String"],
      ["question_id", "String"],
      ["record_sequence", "i32"],
      ["revision", "i64"],
      ["revision_hash", "String"],
      ["written_at", "String"],
      ["written_by", "WrittenBy"],
    ],
    validate: "p2c_validate_record(&self.proposal_id, self.revision, &self.revision_hash, &self.question_id, self.record_sequence, &self.written_at)?;",
  },
  GrillAnswerRecord: {
    fields: [
      ["answer", "Answer"],
      ["proposal_id", "String"],
      ["question_id", "String"],
      ["record_sequence", "i32"],
      ["revision", "i64"],
      ["revision_hash", "String"],
      ["written_at", "String"],
      ["written_by", "WrittenBy"],
    ],
    validate: "p2c_validate_record(&self.proposal_id, self.revision, &self.revision_hash, &self.question_id, self.record_sequence, &self.written_at)?;",
  },
  GrillOverrideRecord: {
    fields: [
      ["grill_override_record_override", "Override", "override"],
      ["proposal_id", "String"],
      ["question_id", "String"],
      ["record_sequence", "i32"],
      ["revision", "i64"],
      ["revision_hash", "String"],
      ["written_at", "String"],
      ["written_by", "WrittenBy"],
    ],
    validate: "p2c_validate_record(&self.proposal_id, self.revision, &self.revision_hash, &self.question_id, self.record_sequence, &self.written_at)?;",
  },
};

const grillRustValidationSources = {
  identity: `
fn p2c_valid_identifier(value: &str) -> bool {
    let bytes = value.as_bytes();
    (3..=64).contains(&bytes.len())
        && matches!(bytes.first(), Some(b'a'..=b'z'))
        && bytes[1..]
            .iter()
            .all(|byte| matches!(byte, b'a'..=b'z' | b'0'..=b'9' | b'_'))
}

fn p2c_valid_hash(value: &str) -> bool {
    value
        .strip_prefix("sha256:")
        .is_some_and(|digest| digest.len() == 64 && digest.bytes().all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f')))
}

fn p2c_validate_identity(proposal_id: &str, revision: i64, revision_hash: &str) -> Result<(), &'static str> {
    if !p2c_valid_identifier(proposal_id) { return Err("proposal_id must match its schema pattern"); }
    if revision < 1 { return Err("revision must be at least one"); }
    if !p2c_valid_hash(revision_hash) { return Err("revision_hash must match its schema pattern"); }
    Ok(())
}
`,
  questionID: `
fn p2c_valid_question_id(value: &str) -> bool {
    matches!(value,
        "grill_question_observable_outcome" | "grill_question_scope_compatibility" |
        "grill_question_acceptance_evidence" | "grill_question_destructive_external_authorization" |
        "grill_question_adapter_worktree_isolation" | "grill_question_autonomy_budget"
    )
}
`,
  validatedQuestionID: `
fn p2c_validate_question_id(value: &str) -> Result<(), &'static str> {
    if p2c_valid_question_id(value) { Ok(()) } else { Err("question_id must match its schema pattern") }
}
`,
  timestamp: `
fn p2c_digits(bytes: &[u8]) -> Option<u32> {
    if bytes.is_empty() || !bytes.iter().all(u8::is_ascii_digit) {
        return None;
    }
    bytes.iter().try_fold(0_u32, |value, byte| value.checked_mul(10)?.checked_add(u32::from(byte - b'0')))
}

fn p2c_valid_timestamp(value: &str) -> bool {
    let bytes = value.as_bytes();
    if bytes.len() < 20 || bytes.get(4) != Some(&b'-') || bytes.get(7) != Some(&b'-') || bytes.get(10) != Some(&b'T') || bytes.get(13) != Some(&b':') || bytes.get(16) != Some(&b':') {
        return false;
    }
    let (Some(year), Some(month), Some(day), Some(hour), Some(minute), Some(second)) = (
        p2c_digits(&bytes[0..4]), p2c_digits(&bytes[5..7]), p2c_digits(&bytes[8..10]),
        p2c_digits(&bytes[11..13]), p2c_digits(&bytes[14..16]), p2c_digits(&bytes[17..19]),
    ) else {
        return false;
    };
    let mut suffix = 19;
    if bytes.get(suffix) == Some(&b'.') {
        suffix += 1;
        let fraction_start = suffix;
        while bytes.get(suffix).is_some_and(u8::is_ascii_digit) {
            suffix += 1;
        }
        if !(1..=9).contains(&(suffix - fraction_start)) {
            return false;
        }
    }
    if bytes.get(suffix) != Some(&b'Z') || suffix + 1 != bytes.len() || month == 0 || month > 12 || day == 0 || hour > 23 || minute > 59 || second > 59 {
        return false;
    }
    let days = match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) => 29,
        2 => 28,
        _ => return false,
    };
    day <= days
}
`,
  record: `
fn p2c_validate_record(proposal_id: &str, revision: i64, revision_hash: &str, question_id: &str, record_sequence: i32, written_at: &str) -> Result<(), &'static str> {
    p2c_validate_identity(proposal_id, revision, revision_hash)?;
    p2c_validate_question_id(question_id)?;
    if !(1..=40).contains(&record_sequence) { return Err("record_sequence must remain within schema bounds"); }
    if !p2c_valid_timestamp(written_at) { return Err("written_at must be a semantic UTC timestamp"); }
    Ok(())
}
`,
  question: `
fn p2c_question_id_for(rule_class: &RuleClass) -> &'static str {
    match rule_class {
        RuleClass::ObservableOutcome => "grill_question_observable_outcome",
        RuleClass::ScopeCompatibility => "grill_question_scope_compatibility",
        RuleClass::AcceptanceEvidence => "grill_question_acceptance_evidence",
        RuleClass::DestructiveExternalAuthorization => "grill_question_destructive_external_authorization",
        RuleClass::AdapterWorktreeIsolation => "grill_question_adapter_worktree_isolation",
        RuleClass::AutonomyBudget => "grill_question_autonomy_budget",
    }
}

fn p2c_validate_question(value: &GrillQuestion) -> Result<(), &'static str> {
    p2c_validate_identity(&value.proposal_id, value.revision, &value.revision_hash)?;
    if !p2c_valid_question_id(&value.question_id) || value.question_id != p2c_question_id_for(&value.rule_class) {
        return Err("question_id must match rule_class");
    }
    if !(1..=10).contains(&value.question_sequence) { return Err("question_sequence must remain within schema bounds"); }
    if !(1..=40).contains(&value.record_sequence) { return Err("record_sequence must remain within schema bounds"); }
    if !value.blocking { return Err("blocking must remain true"); }
    if !p2c_valid_timestamp(&value.written_at) { return Err("written_at must be a semantic UTC timestamp"); }
    let rule_fields_match = match &value.rule_class {
        RuleClass::ObservableOutcome => value.grill_question_default == Default::NeedsRewrite && value.risk == Risk::High && value.remedial_step == RemedialStep::DeclareObservableOutcome && !value.waivable,
        RuleClass::ScopeCompatibility => value.grill_question_default == Default::NeedsRewrite && value.risk == Risk::Medium && value.remedial_step == RemedialStep::DeclareScopeCompatibility && value.waivable,
        RuleClass::AcceptanceEvidence => value.grill_question_default == Default::NeedsRewrite && value.risk == Risk::High && value.remedial_step == RemedialStep::DeclareAcceptanceEvidence && !value.waivable,
        RuleClass::DestructiveExternalAuthorization => value.grill_question_default == Default::Deny && value.risk == Risk::Critical && value.remedial_step == RemedialStep::RecordLocalAuthorization && !value.waivable,
        RuleClass::AdapterWorktreeIsolation => value.grill_question_default == Default::NeedsRewrite && value.risk == Risk::High && value.remedial_step == RemedialStep::RequireIsolatedWorktree && !value.waivable,
        RuleClass::AutonomyBudget => value.grill_question_default == Default::NeedsRewrite && value.risk == Risk::High && value.remedial_step == RemedialStep::SetDeadlineAttemptCap && !value.waivable,
    };
    if !rule_fields_match { return Err("Question fields must match the fixed P2b rule"); }
    Ok(())
}
`,
  rulePriority: `
fn p2c_rule_priority(rule_class: &RuleClass) -> u8 {
    match rule_class {
        RuleClass::ObservableOutcome => 10,
        RuleClass::ScopeCompatibility => 20,
        RuleClass::AcceptanceEvidence => 30,
        RuleClass::DestructiveExternalAuthorization => 40,
        RuleClass::AdapterWorktreeIsolation => 50,
        RuleClass::AutonomyBudget => 60,
    }
}
`,
  evaluation: `
fn p2c_validate_evaluation(value: &GrillEvaluation) -> Result<(), &'static str> {
    p2c_validate_identity(&value.proposal_id, value.revision, &value.revision_hash)?;
    if value.shown_questions.len() > 5 || value.new_question_ids.len() > 5 || value.deferred_rule_classes.len() > 6 || !(0..=6).contains(&value.new_records) {
        return Err("Evaluation exceeds its schema bounds");
    }
    let mut previous_priority = 0;
    for (index, question) in value.shown_questions.iter().enumerate() {
        p2c_validate_question(question)?;
        if question.proposal_id != value.proposal_id || question.revision != value.revision || question.revision_hash != value.revision_hash {
            return Err("shown Question identity must match Evaluation identity");
        }
        let priority = p2c_rule_priority(&question.rule_class);
        if priority <= previous_priority || value.shown_questions[..index].iter().any(|prior| prior.question_id == question.question_id) {
            return Err("shown Questions must retain P2b priority order");
        }
        previous_priority = priority;
    }
    let mut next_shown_index = 0;
    for (index, question_id) in value.new_question_ids.iter().enumerate() {
        p2c_validate_question_id(question_id)?;
        if value.new_question_ids[..index].iter().any(|prior| prior == question_id) {
            return Err("new Question IDs must be unique");
        }
        let Some(offset) = value.shown_questions[next_shown_index..].iter().position(|question| &question.question_id == question_id) else {
            return Err("new Question IDs must preserve shown Question order");
        };
        next_shown_index += offset + 1;
    }
    let mut previous_deferred_priority = 0;
    for (index, rule_class) in value.deferred_rule_classes.iter().enumerate() {
        let priority = p2c_rule_priority(rule_class);
        if priority <= previous_deferred_priority || value.deferred_rule_classes[..index].iter().any(|prior| prior == rule_class) || value.shown_questions.iter().any(|question| question.rule_class == *rule_class) {
            return Err("deferred rule classes must remain ordered, unique, and unshown");
        }
        previous_deferred_priority = priority;
    }
    if value.new_records != 0 && value.new_records != value.new_question_ids.len() as i32 + 1 {
        return Err("new_records must include one Evaluation plus each new Question");
    }
    if value.status == Status::Clear && (!value.shown_questions.is_empty() || !value.new_question_ids.is_empty() || !value.deferred_rule_classes.is_empty() || value.new_records != 0) {
        return Err("clear Evaluation cannot retain active or appended Questions");
    }
    Ok(())
}
`,
};
const grillRustValidationDependencies = {
  EvaluateGrillInput: ["identity"],
  RecordGrillDefaultInput: ["identity", "questionID", "validatedQuestionID"],
  RecordGrillAnswerInput: ["identity", "questionID", "validatedQuestionID"],
  RecordGrillOverrideInput: ["identity", "questionID", "validatedQuestionID"],
  GrillQuestion: ["identity", "questionID", "timestamp", "question"],
  GrillEvaluation: ["identity", "questionID", "validatedQuestionID", "timestamp", "question", "rulePriority", "evaluation"],
  GrillDefaultRecord: ["identity", "questionID", "validatedQuestionID", "timestamp", "record"],
  GrillAnswerRecord: ["identity", "questionID", "validatedQuestionID", "timestamp", "record"],
  GrillOverrideRecord: ["identity", "questionID", "validatedQuestionID", "timestamp", "record"],
};

function grillRustValidationSource(type) {
  return (grillRustValidationDependencies[type] ?? [])
    .map((dependency) => grillRustValidationSources[dependency])
    .join("");
}


function replaceRustDeserialize(source, type) {
  const derived = `#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]\npub struct ${type} {`;
  if (!source.includes(derived)) throw new Error(`Cannot add P2c validation to ${type}.`);
  return source.replace(derived, `#[derive(Debug, Clone, PartialEq, Serialize)]\npub struct ${type} {`);
}

function renderGrillRustContract(contract, type) {
  const wireType = `${type}Wire`;
  const fields = contract.fields.map(([name, fieldType, jsonName]) => `${jsonName === undefined ? "" : `    #[serde(rename = \"${jsonName}\")]\n`}    ${name}: ${fieldType},`).join("\n");
  const assignments = contract.fields.map(([name]) => `            ${name}: wire.${name},`).join("\n");
  return `
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ${wireType} {
${fields}
}

impl From<${wireType}> for ${type} {
    fn from(wire: ${wireType}) -> Self {
        Self {
${assignments}
        }
    }
}

impl ${type} {
    pub fn validate(&self) -> Result<(), &'static str> {
        ${contract.validate}
        Ok(())
    }
}

impl<'de> Deserialize<'de> for ${type} {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = Self::from(${wireType}::deserialize(deserializer)?);
        value.validate().map_err(serde::de::Error::custom)?;
        Ok(value)
    }
}
`;
}

function addGrillRustValidation(source, target) {
  const contract = grillRustContracts[target.topLevel];
  if (contract === undefined) return source;
  let validated = replaceRustDeserialize(source, target.topLevel);
  if (target.topLevel === "GrillEvaluation") validated = replaceRustDeserialize(validated, "GrillQuestion");
  const questionContract = target.topLevel === "GrillEvaluation" ? grillRustContracts.GrillQuestion : undefined;
  return `${validated}${grillRustValidationSource(target.topLevel)}${questionContract === undefined ? "" : renderGrillRustContract(questionContract, "GrillQuestion")}${renderGrillRustContract(contract, target.topLevel)}`;
}

function grillTypeScriptValidationSource(schema) {
  return `
const p2cSchema = ${JSON.stringify(schema)};
const p2cRules = [
    { ruleClass: "observable_outcome", questionID: "grill_question_observable_outcome", priority: 10, defaultValue: "needs_rewrite", risk: "high", remedialStep: "declare_observable_outcome", waivable: false },
    { ruleClass: "scope_compatibility", questionID: "grill_question_scope_compatibility", priority: 20, defaultValue: "needs_rewrite", risk: "medium", remedialStep: "declare_scope_compatibility", waivable: true },
    { ruleClass: "acceptance_evidence", questionID: "grill_question_acceptance_evidence", priority: 30, defaultValue: "needs_rewrite", risk: "high", remedialStep: "declare_acceptance_evidence", waivable: false },
    { ruleClass: "destructive_external_authorization", questionID: "grill_question_destructive_external_authorization", priority: 40, defaultValue: "deny", risk: "critical", remedialStep: "record_local_authorization", waivable: false },
    { ruleClass: "adapter_worktree_isolation", questionID: "grill_question_adapter_worktree_isolation", priority: 50, defaultValue: "needs_rewrite", risk: "high", remedialStep: "require_isolated_worktree", waivable: false },
    { ruleClass: "autonomy_budget", questionID: "grill_question_autonomy_budget", priority: 60, defaultValue: "needs_rewrite", risk: "high", remedialStep: "set_deadline_attempt_cap", waivable: false },
];

function p2cFail(path: string, message: string): never {
    throw Error(path + " " + message);
}

function p2cTimestamp(value: string): boolean {
    const match = /^(\\d{4})-(\\d{2})-(\\d{2})T(\\d{2}):(\\d{2}):(\\d{2})(?:\\.\\d{1,9})?Z$/.exec(value);
    if (match === null) return false;
    const [year, month, day, hour, minute, second] = match.slice(1).map(Number);
    const days = month === 2 ? (year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28) : ([4, 6, 9, 11].includes(month) ? 30 : 31);
    return month >= 1 && month <= 12 && day >= 1 && day <= days && hour <= 23 && minute <= 59 && second <= 59;
}

function p2cType(value: unknown, type: string): boolean {
    if (type === "object") return value !== null && typeof value === "object" && !Array.isArray(value);
    if (type === "array") return Array.isArray(value);
    if (type === "integer") return Number.isInteger(value);
    if (type === "number") return typeof value === "number" && Number.isFinite(value);
    return typeof value === type;
}

function p2cValidateSchema(value: any, schema: any, path: string): void {
    if (Object.hasOwn(schema, "const") && !Object.is(value, schema.const)) p2cFail(path, "must equal its schema const");
    if (schema.enum !== undefined && !schema.enum.some((candidate: unknown) => Object.is(candidate, value))) p2cFail(path, "must equal a schema enum value");
    if (schema.type !== undefined && !(Array.isArray(schema.type) ? schema.type : [schema.type]).some((type: string) => p2cType(value, type))) p2cFail(path, "has the wrong schema type");
    if (typeof value === "string") {
        if (schema.pattern !== undefined && !(new RegExp(schema.pattern)).test(value)) p2cFail(path, "does not match its schema pattern");
        if (schema["x-ananke-utc-timestamp"] === true && !p2cTimestamp(value)) p2cFail(path, "must be a semantic UTC timestamp");
    }
    if (typeof value === "number") {
        if (schema.minimum !== undefined && value < schema.minimum) p2cFail(path, "is below its schema minimum");
        if (schema.maximum !== undefined && value > schema.maximum) p2cFail(path, "is above its schema maximum");
    }
    if (Array.isArray(value)) {
        if (schema.minItems !== undefined && value.length < schema.minItems) p2cFail(path, "has too few items");
        if (schema.maxItems !== undefined && value.length > schema.maxItems) p2cFail(path, "has too many items");
        if (schema.items !== undefined) value.forEach((entry, index) => p2cValidateSchema(entry, schema.items, path + "[" + index + "]"));
    }
    if (value !== null && typeof value === "object" && !Array.isArray(value) && schema.properties !== undefined) {
        const properties = schema.properties;
        for (const required of schema.required ?? []) if (!Object.hasOwn(value, required)) p2cFail(path, "is missing " + required);
        if (schema.additionalProperties === false) for (const key of Object.keys(value)) if (!Object.hasOwn(properties, key)) p2cFail(path + "." + key, "is an unknown field");
        for (const [key, property] of Object.entries(properties)) if (Object.hasOwn(value, key)) p2cValidateSchema(value[key], property, path + "." + key);
    }
}

function p2cRuleForQuestionID(questionID: string): any {
    return p2cRules.find((rule) => rule.questionID === questionID);
}

function p2cValidateQuestion(value: any, path: string): void {
    const rule = p2cRules.find((candidate) => candidate.ruleClass === value.rule_class);
    if (rule === undefined || value.question_id !== rule.questionID || value.blocking !== true || value.default !== rule.defaultValue || value.risk !== rule.risk || value.remedial_step !== rule.remedialStep || value.waivable !== rule.waivable) p2cFail(path, "does not match its fixed P2b rule");
}

function p2cValidateEvaluation(value: any): void {
    const shown = value.shown_questions;
    const shownIDs = new Set<string>();
    let priority = 0;
    for (const question of shown) {
        p2cValidateQuestion(question, "$.shown_questions");
        if (question.proposal_id !== value.proposal_id || question.revision !== value.revision || question.revision_hash !== value.revision_hash) p2cFail("$.shown_questions", "must match Evaluation identity");
        const rule = p2cRules.find((candidate) => candidate.ruleClass === question.rule_class)!;
        if (rule.priority <= priority || shownIDs.has(question.question_id)) p2cFail("$.shown_questions", "must retain unique P2b priority order");
        shownIDs.add(question.question_id);
        priority = rule.priority;
    }
    const newIDs = value.new_question_ids;
    if (new Set(newIDs).size !== newIDs.length) p2cFail("$.new_question_ids", "must be unique");
    let shownOffset = 0;
    for (const questionID of newIDs) {
        if (p2cRuleForQuestionID(questionID) === undefined) p2cFail("$.new_question_ids", "has an invalid Question ID");
        const offset = shown.slice(shownOffset).findIndex((question: any) => question.question_id === questionID);
        if (offset === -1) p2cFail("$.new_question_ids", "must preserve shown Question order");
        shownOffset += offset + 1;
    }
    const deferred = value.deferred_rule_classes;
    const deferredRules = new Set<string>();
    let deferredPriority = 0;
    for (const ruleClass of deferred) {
        const rule = p2cRules.find((candidate) => candidate.ruleClass === ruleClass);
        if (rule === undefined || deferredRules.has(ruleClass) || shown.some((question: any) => question.rule_class === ruleClass) || rule.priority <= deferredPriority) p2cFail("$.deferred_rule_classes", "must remain ordered, unique, and unshown");
        deferredRules.add(ruleClass);
        deferredPriority = rule.priority;
    }
    if (value.new_records !== 0 && value.new_records !== newIDs.length + 1) p2cFail("$.new_records", "must include one Evaluation plus each new Question");
    if (value.status === "clear" && (shown.length !== 0 || newIDs.length !== 0 || deferred.length !== 0 || value.new_records !== 0)) p2cFail("$", "clear Evaluations cannot retain active or appended Questions");
}

function validateP2c<T>(value: T, schema: any, topLevel: string): T {
    p2cValidateSchema(value, schema, "$");
    if (topLevel === "GrillQuestion") p2cValidateQuestion(value, "$");
    if (topLevel === "GrillEvaluation") p2cValidateEvaluation(value);
    return value;
}
`;
}

function addGrillTypeScriptValidation(source, target, schema) {
  if (grillRustContracts[target.topLevel] === undefined) return source;
  const decoder = `return cast(JSON.parse(json), r("${target.topLevel}"));`;
  const encoder = `return JSON.stringify(uncast(value, r("${target.topLevel}")), null, 2);`;
  if (!source.includes(decoder) || !source.includes(encoder)) throw new Error(`Cannot add P2c validation to ${target.topLevel}.`);
  return `${source
    .replace(decoder, `return validateP2c(cast(JSON.parse(json), r("${target.topLevel}")), p2cSchema, "${target.topLevel}");`)
    .replace(encoder, `return JSON.stringify(uncast(validateP2c(value, p2cSchema, "${target.topLevel}"), r("${target.topLevel}")), null, 2);`)}${grillTypeScriptValidationSource(schema)}`;
}

function formatRust(source) {
  const result = spawnSync("rustfmt", ["--edition", "2024"], { encoding: "utf8", input: source });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(result.stderr || "rustfmt failed.");
  return result.stdout;
}

async function runQuicktype(target) {
  const materializedSchema = await materializeSchema(target);
  try {
    const schema = JSON.parse(await readFile(materializedSchema.path, "utf8"));
    const result = spawnSync(
      quicktypePath,
      [
        "--src",
        materializedSchema.path,
        "--src-lang",
        "schema",
        "--lang",
        target.language,
        "--top-level",
        target.topLevel,
        "--telemetry",
        "disable",
        ...(target.args ?? []),
      ],
      { cwd: guiDirectory, encoding: "utf8" },
    );

    if (result.error) {
      throw result.error;
    }
    if (result.status !== 0) {
      throw new Error(result.stderr || `Quicktype failed for ${target.language}.`);
    }
    const source = target.language === "rust"
      ? result.stdout.replace("use serde::{Serialize, Deserialize};", "use serde::{Deserialize, Serialize};")
      : `// @ts-nocheck\n${result.stdout}`;
    return target.language === "rust"
      ? formatRust(addGrillRustValidation(source, target))
      : addGrillTypeScriptValidation(source, target, schema);
  } finally {
    await materializedSchema.cleanup();
  }
}

const p2cPrivateFieldFragments = [
  "cmd", "command", "token", "error", "socket", "identity", "worker", "process", "pid", "path", "root",
  "secret", "credential", "password", "model", "prompt", "prose", "approval", "execution", "execute", "runtime",
  "transport", "inputhash", "ruleversion", "declarations", "raw",
];

function isProhibitedPublicField(field, enforceP2cPrivacyPolicy = false) {
  const normalized = field.toLowerCase().replace(/[^a-z0-9]/g, "");
  return (
    normalized.includes("token") ||
    normalized.includes("error") ||
    (normalized.includes("worker") && normalized.includes("env")) ||
    normalized.includes("socket") ||
    normalized.includes("identity") ||
    (normalized.includes("adapter") && normalized.includes("secret")) ||
    normalized === "secret" ||
    normalized.endsWith("secret") ||
    normalized === "cmd" ||
    normalized.includes("command") ||
    normalized.includes("prose") ||
    normalized.includes("transport") ||
    normalized.includes("inputhash") ||
    normalized.includes("ruleversion") ||
    (enforceP2cPrivacyPolicy && p2cPrivateFieldFragments.some((fragment) => normalized.includes(fragment)) )
  );
}

function assertPublicFieldPrivacy(path, schema) {
  const enforceP2cPrivacyPolicy = basename(path).startsWith("renderer-public-grill-");
  function visit(node) {
    if (Array.isArray(node)) {
      node.forEach(visit);
      return;
    }
    if (node === null || typeof node !== "object") {
      return;
    }
    if (node.properties && typeof node.properties === "object") {
      for (const [field, property] of Object.entries(node.properties)) {
        if (isProhibitedPublicField(field, enforceP2cPrivacyPolicy)) {
          throw new Error(`${path} exposes prohibited public field ${field}.`);
        }
        visit(property);
      }
    }
    for (const [key, value] of Object.entries(node)) {
      if (key !== "properties") {
        visit(value);
      }
    }
  }

  visit(schema);
}

async function assertRendererPublicSchemaPrivacy() {
  for (const model of rendererPublicModels) {
    const source = await readIfPresent(model.schemaPath);
    if (source === null) {
      throw new Error(`${model.schemaPath} is missing.`);
    }
    assertPublicFieldPrivacy(model.schemaPath, JSON.parse(source));
  }
}

async function readIfPresent(path) {
  try {
    return await readFile(path, "utf8");
  } catch (error) {
    if (error.code === "ENOENT") {
      return null;
    }
    throw error;
  }
}

async function writeIfChanged(path, source) {
  await mkdir(dirname(path), { recursive: true });
  if ((await readIfPresent(path)) !== source) {
    await writeFile(path, source);
  }
}

async function assertQuicktypeAvailable() {
  try {
    await access(quicktypePath);
  } catch {
    throw new Error("Quicktype is missing; run npm install from gui/ first.");
  }
}

async function main() {
  await assertQuicktypeAvailable();

  if (privacyCheckMode) {
    for (const target of targets) {
      if ((await readIfPresent(target.path)) === null) {
        throw new Error(`${target.path} is missing; run npm run generate:renderer-public.`);
      }
    }
    await assertRendererPublicSchemaPrivacy();
    console.log("Renderer-public schemas expose no prohibited private fields.");
    return;
  }

  await assertRendererPublicSchemaPrivacy();
  const renderedTargets = await Promise.all(targets.map(async (target) => ({
    ...target,
    source: await runQuicktype(target),
  })));

  const expectedSources = [...renderedTargets, { path: rustModulePath, source: formatRust(rustModuleSource) }];
  if (checkMode) {
    const driftedPaths = [];
    for (const expected of expectedSources) {
      if ((await readIfPresent(expected.path)) !== expected.source) {
        driftedPaths.push(expected.path);
      }
    }
    if (driftedPaths.length > 0) {
      throw new Error(
        `Generated renderer-public models drifted: ${driftedPaths.join(", ")}. Run npm run generate:renderer-public.`,
      );
    }
    console.log("Renderer-public generated models match the canonical schema.");
    return;
  }

  for (const expected of expectedSources) {
    await writeIfChanged(expected.path, expected.source);
  }
  console.log("Generated renderer-public models from the canonical schemas.");
}

await main();
