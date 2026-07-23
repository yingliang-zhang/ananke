// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::RecordGrillAnswerInput;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: RecordGrillAnswerInput = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public arguments of the future Tauri record_grill_answer command. The answer is the fixed
/// acknowledgement selected by the bridge; callers cannot supply prose or an answer value.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct RecordGrillAnswerInput {
    pub proposal_id: String,

    pub question_id: String,

    pub revision: i64,

    pub revision_hash: String,
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

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RecordGrillAnswerInputWire {
    proposal_id: String,
    question_id: String,
    revision: i64,
    revision_hash: String,
}

impl From<RecordGrillAnswerInputWire> for RecordGrillAnswerInput {
    fn from(wire: RecordGrillAnswerInputWire) -> Self {
        Self {
            proposal_id: wire.proposal_id,
            question_id: wire.question_id,
            revision: wire.revision,
            revision_hash: wire.revision_hash,
        }
    }
}

impl RecordGrillAnswerInput {
    pub fn validate(&self) -> Result<(), &'static str> {
        p2c_validate_identity(&self.proposal_id, self.revision, &self.revision_hash)?;
        p2c_validate_question_id(&self.question_id)?;
        Ok(())
    }
}

impl<'de> Deserialize<'de> for RecordGrillAnswerInput {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = Self::from(RecordGrillAnswerInputWire::deserialize(deserializer)?);
        value.validate().map_err(serde::de::Error::custom)?;
        Ok(value)
    }
}
