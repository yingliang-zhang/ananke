// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::GrillEvaluation;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: GrillEvaluation = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public bounded result of evaluating one immutable proposal revision. It contains no
/// declaration input, hash, rule-version, daemon envelope, or error detail.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct GrillEvaluation {
    pub deferred_rule_classes: Vec<RuleClass>,

    pub new_question_ids: Vec<String>,

    pub new_records: i32,

    pub proposal_id: String,

    pub revision: i64,

    pub revision_hash: String,

    pub shown_questions: Vec<GrillQuestion>,

    pub status: Status,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RuleClass {
    #[serde(rename = "acceptance_evidence")]
    AcceptanceEvidence,

    #[serde(rename = "adapter_worktree_isolation")]
    AdapterWorktreeIsolation,

    #[serde(rename = "autonomy_budget")]
    AutonomyBudget,

    #[serde(rename = "destructive_external_authorization")]
    DestructiveExternalAuthorization,

    #[serde(rename = "observable_outcome")]
    ObservableOutcome,

    #[serde(rename = "scope_compatibility")]
    ScopeCompatibility,
}

#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct GrillQuestion {
    pub blocking: bool,

    #[serde(rename = "default")]
    pub grill_question_default: Default,

    pub proposal_id: String,

    pub question_id: String,

    pub question_sequence: i32,

    pub record_sequence: i32,

    pub remedial_step: RemedialStep,

    pub revision: i64,

    pub revision_hash: String,

    pub risk: Risk,

    pub rule_class: RuleClass,

    pub waivable: bool,

    pub written_at: String,

    pub written_by: WrittenBy,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Default {
    Deny,

    #[serde(rename = "needs_rewrite")]
    NeedsRewrite,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RemedialStep {
    #[serde(rename = "declare_acceptance_evidence")]
    DeclareAcceptanceEvidence,

    #[serde(rename = "declare_observable_outcome")]
    DeclareObservableOutcome,

    #[serde(rename = "declare_scope_compatibility")]
    DeclareScopeCompatibility,

    #[serde(rename = "record_local_authorization")]
    RecordLocalAuthorization,

    #[serde(rename = "require_isolated_worktree")]
    RequireIsolatedWorktree,

    #[serde(rename = "set_deadline_attempt_cap")]
    SetDeadlineAttemptCap,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Risk {
    Critical,

    High,

    Medium,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WrittenBy {
    #[serde(rename = "deterministic_grill")]
    DeterministicGrill,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Status {
    Blocked,

    Clear,

    #[serde(rename = "needs_rewrite")]
    NeedsRewrite,
}

fn p2c_valid_identifier(value: &str) -> bool {
    let bytes = value.as_bytes();
    (3..=64).contains(&bytes.len())
        && matches!(bytes.first(), Some(b'a'..=b'z'))
        && bytes[1..]
            .iter()
            .all(|byte| matches!(byte, b'a'..=b'z' | b'0'..=b'9' | b'_'))
}

fn p2c_valid_hash(value: &str) -> bool {
    value.strip_prefix("sha256:").is_some_and(|digest| {
        digest.len() == 64
            && digest
                .bytes()
                .all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f'))
    })
}

fn p2c_validate_identity(
    proposal_id: &str,
    revision: i64,
    revision_hash: &str,
) -> Result<(), &'static str> {
    if !p2c_valid_identifier(proposal_id) {
        return Err("proposal_id must match its schema pattern");
    }
    if revision < 1 {
        return Err("revision must be at least one");
    }
    if !p2c_valid_hash(revision_hash) {
        return Err("revision_hash must match its schema pattern");
    }
    Ok(())
}

fn p2c_valid_question_id(value: &str) -> bool {
    matches!(
        value,
        "grill_question_observable_outcome"
            | "grill_question_scope_compatibility"
            | "grill_question_acceptance_evidence"
            | "grill_question_destructive_external_authorization"
            | "grill_question_adapter_worktree_isolation"
            | "grill_question_autonomy_budget"
    )
}

fn p2c_validate_question_id(value: &str) -> Result<(), &'static str> {
    if p2c_valid_question_id(value) {
        Ok(())
    } else {
        Err("question_id must match its schema pattern")
    }
}

fn p2c_digits(bytes: &[u8]) -> Option<u32> {
    if bytes.is_empty() || !bytes.iter().all(u8::is_ascii_digit) {
        return None;
    }
    bytes.iter().try_fold(0_u32, |value, byte| {
        value.checked_mul(10)?.checked_add(u32::from(byte - b'0'))
    })
}

fn p2c_valid_timestamp(value: &str) -> bool {
    let bytes = value.as_bytes();
    if bytes.len() < 20
        || bytes.get(4) != Some(&b'-')
        || bytes.get(7) != Some(&b'-')
        || bytes.get(10) != Some(&b'T')
        || bytes.get(13) != Some(&b':')
        || bytes.get(16) != Some(&b':')
    {
        return false;
    }
    let (Some(year), Some(month), Some(day), Some(hour), Some(minute), Some(second)) = (
        p2c_digits(&bytes[0..4]),
        p2c_digits(&bytes[5..7]),
        p2c_digits(&bytes[8..10]),
        p2c_digits(&bytes[11..13]),
        p2c_digits(&bytes[14..16]),
        p2c_digits(&bytes[17..19]),
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
    if bytes.get(suffix) != Some(&b'Z')
        || suffix + 1 != bytes.len()
        || month == 0
        || month > 12
        || day == 0
        || hour > 23
        || minute > 59
        || second > 59
    {
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

fn p2c_question_id_for(rule_class: &RuleClass) -> &'static str {
    match rule_class {
        RuleClass::ObservableOutcome => "grill_question_observable_outcome",
        RuleClass::ScopeCompatibility => "grill_question_scope_compatibility",
        RuleClass::AcceptanceEvidence => "grill_question_acceptance_evidence",
        RuleClass::DestructiveExternalAuthorization => {
            "grill_question_destructive_external_authorization"
        }
        RuleClass::AdapterWorktreeIsolation => "grill_question_adapter_worktree_isolation",
        RuleClass::AutonomyBudget => "grill_question_autonomy_budget",
    }
}

fn p2c_validate_question(value: &GrillQuestion) -> Result<(), &'static str> {
    p2c_validate_identity(&value.proposal_id, value.revision, &value.revision_hash)?;
    if !p2c_valid_question_id(&value.question_id)
        || value.question_id != p2c_question_id_for(&value.rule_class)
    {
        return Err("question_id must match rule_class");
    }
    if !(1..=10).contains(&value.question_sequence) {
        return Err("question_sequence must remain within schema bounds");
    }
    if !(1..=40).contains(&value.record_sequence) {
        return Err("record_sequence must remain within schema bounds");
    }
    if !value.blocking {
        return Err("blocking must remain true");
    }
    if !p2c_valid_timestamp(&value.written_at) {
        return Err("written_at must be a semantic UTC timestamp");
    }
    let rule_fields_match = match &value.rule_class {
        RuleClass::ObservableOutcome => {
            value.grill_question_default == Default::NeedsRewrite
                && value.risk == Risk::High
                && value.remedial_step == RemedialStep::DeclareObservableOutcome
                && !value.waivable
        }
        RuleClass::ScopeCompatibility => {
            value.grill_question_default == Default::NeedsRewrite
                && value.risk == Risk::Medium
                && value.remedial_step == RemedialStep::DeclareScopeCompatibility
                && value.waivable
        }
        RuleClass::AcceptanceEvidence => {
            value.grill_question_default == Default::NeedsRewrite
                && value.risk == Risk::High
                && value.remedial_step == RemedialStep::DeclareAcceptanceEvidence
                && !value.waivable
        }
        RuleClass::DestructiveExternalAuthorization => {
            value.grill_question_default == Default::Deny
                && value.risk == Risk::Critical
                && value.remedial_step == RemedialStep::RecordLocalAuthorization
                && !value.waivable
        }
        RuleClass::AdapterWorktreeIsolation => {
            value.grill_question_default == Default::NeedsRewrite
                && value.risk == Risk::High
                && value.remedial_step == RemedialStep::RequireIsolatedWorktree
                && !value.waivable
        }
        RuleClass::AutonomyBudget => {
            value.grill_question_default == Default::NeedsRewrite
                && value.risk == Risk::High
                && value.remedial_step == RemedialStep::SetDeadlineAttemptCap
                && !value.waivable
        }
    };
    if !rule_fields_match {
        return Err("Question fields must match the fixed P2b rule");
    }
    Ok(())
}

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

fn p2c_validate_evaluation(value: &GrillEvaluation) -> Result<(), &'static str> {
    p2c_validate_identity(&value.proposal_id, value.revision, &value.revision_hash)?;
    if value.shown_questions.len() > 5
        || value.new_question_ids.len() > 5
        || value.deferred_rule_classes.len() > 6
        || !(0..=6).contains(&value.new_records)
    {
        return Err("Evaluation exceeds its schema bounds");
    }
    let mut previous_priority = 0;
    for (index, question) in value.shown_questions.iter().enumerate() {
        p2c_validate_question(question)?;
        if question.proposal_id != value.proposal_id
            || question.revision != value.revision
            || question.revision_hash != value.revision_hash
        {
            return Err("shown Question identity must match Evaluation identity");
        }
        let priority = p2c_rule_priority(&question.rule_class);
        if priority <= previous_priority
            || value.shown_questions[..index]
                .iter()
                .any(|prior| prior.question_id == question.question_id)
        {
            return Err("shown Questions must retain P2b priority order");
        }
        previous_priority = priority;
    }
    let mut next_shown_index = 0;
    for (index, question_id) in value.new_question_ids.iter().enumerate() {
        p2c_validate_question_id(question_id)?;
        if value.new_question_ids[..index]
            .iter()
            .any(|prior| prior == question_id)
        {
            return Err("new Question IDs must be unique");
        }
        let Some(offset) = value.shown_questions[next_shown_index..]
            .iter()
            .position(|question| &question.question_id == question_id)
        else {
            return Err("new Question IDs must preserve shown Question order");
        };
        next_shown_index += offset + 1;
    }
    let mut previous_deferred_priority = 0;
    for (index, rule_class) in value.deferred_rule_classes.iter().enumerate() {
        let priority = p2c_rule_priority(rule_class);
        if priority <= previous_deferred_priority
            || value.deferred_rule_classes[..index]
                .iter()
                .any(|prior| prior == rule_class)
            || value
                .shown_questions
                .iter()
                .any(|question| question.rule_class == *rule_class)
        {
            return Err("deferred rule classes must remain ordered, unique, and unshown");
        }
        previous_deferred_priority = priority;
    }
    let question_record_count = value.new_question_ids.len() as i32;
    if value.new_records < question_record_count || value.new_records > question_record_count + 1 {
        return Err(
            "new_records must account for each appended Question and an optional Evaluation record",
        );
    }
    if value.status == Status::Clear
        && (!value.shown_questions.is_empty()
            || !value.new_question_ids.is_empty()
            || !value.deferred_rule_classes.is_empty())
    {
        return Err("clear Evaluation cannot retain active or appended Questions");
    }
    Ok(())
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct GrillQuestionWire {
    blocking: bool,
    #[serde(rename = "default")]
    grill_question_default: Default,
    proposal_id: String,
    question_id: String,
    question_sequence: i32,
    record_sequence: i32,
    remedial_step: RemedialStep,
    revision: i64,
    revision_hash: String,
    risk: Risk,
    rule_class: RuleClass,
    waivable: bool,
    written_at: String,
    written_by: WrittenBy,
}

impl From<GrillQuestionWire> for GrillQuestion {
    fn from(wire: GrillQuestionWire) -> Self {
        Self {
            blocking: wire.blocking,
            grill_question_default: wire.grill_question_default,
            proposal_id: wire.proposal_id,
            question_id: wire.question_id,
            question_sequence: wire.question_sequence,
            record_sequence: wire.record_sequence,
            remedial_step: wire.remedial_step,
            revision: wire.revision,
            revision_hash: wire.revision_hash,
            risk: wire.risk,
            rule_class: wire.rule_class,
            waivable: wire.waivable,
            written_at: wire.written_at,
            written_by: wire.written_by,
        }
    }
}

impl GrillQuestion {
    pub fn validate(&self) -> Result<(), &'static str> {
        p2c_validate_question(self)?;
        Ok(())
    }
}

impl<'de> Deserialize<'de> for GrillQuestion {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = Self::from(GrillQuestionWire::deserialize(deserializer)?);
        value.validate().map_err(serde::de::Error::custom)?;
        Ok(value)
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct GrillEvaluationWire {
    deferred_rule_classes: Vec<RuleClass>,
    new_question_ids: Vec<String>,
    new_records: i32,
    proposal_id: String,
    revision: i64,
    revision_hash: String,
    shown_questions: Vec<GrillQuestion>,
    status: Status,
}

impl From<GrillEvaluationWire> for GrillEvaluation {
    fn from(wire: GrillEvaluationWire) -> Self {
        Self {
            deferred_rule_classes: wire.deferred_rule_classes,
            new_question_ids: wire.new_question_ids,
            new_records: wire.new_records,
            proposal_id: wire.proposal_id,
            revision: wire.revision,
            revision_hash: wire.revision_hash,
            shown_questions: wire.shown_questions,
            status: wire.status,
        }
    }
}

impl GrillEvaluation {
    pub fn validate(&self) -> Result<(), &'static str> {
        p2c_validate_evaluation(self)?;
        Ok(())
    }
}

impl<'de> Deserialize<'de> for GrillEvaluation {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = Self::from(GrillEvaluationWire::deserialize(deserializer)?);
        value.validate().map_err(serde::de::Error::custom)?;
        Ok(value)
    }
}
