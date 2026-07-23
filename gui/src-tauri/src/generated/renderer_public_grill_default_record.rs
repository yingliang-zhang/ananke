// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::GrillDefaultRecord;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: GrillDefaultRecord = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public immutable default record returned by record_grill_default. The deterministic
/// evaluator supplies the default value.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct GrillDefaultRecord {
    #[serde(rename = "default")]
    pub grill_default_record_default: Default,

    pub proposal_id: String,

    pub question_id: String,

    pub record_sequence: i32,

    pub revision: i64,

    pub revision_hash: String,

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
pub enum WrittenBy {
    #[serde(rename = "deterministic_grill")]
    DeterministicGrill,
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

fn p2c_validate_record(
    proposal_id: &str,
    revision: i64,
    revision_hash: &str,
    question_id: &str,
    record_sequence: i32,
    written_at: &str,
) -> Result<(), &'static str> {
    p2c_validate_identity(proposal_id, revision, revision_hash)?;
    p2c_validate_question_id(question_id)?;
    if !(1..=40).contains(&record_sequence) {
        return Err("record_sequence must remain within schema bounds");
    }
    if !p2c_valid_timestamp(written_at) {
        return Err("written_at must be a semantic UTC timestamp");
    }
    Ok(())
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct GrillDefaultRecordWire {
    #[serde(rename = "default")]
    grill_default_record_default: Default,
    proposal_id: String,
    question_id: String,
    record_sequence: i32,
    revision: i64,
    revision_hash: String,
    written_at: String,
    written_by: WrittenBy,
}

impl From<GrillDefaultRecordWire> for GrillDefaultRecord {
    fn from(wire: GrillDefaultRecordWire) -> Self {
        Self {
            grill_default_record_default: wire.grill_default_record_default,
            proposal_id: wire.proposal_id,
            question_id: wire.question_id,
            record_sequence: wire.record_sequence,
            revision: wire.revision,
            revision_hash: wire.revision_hash,
            written_at: wire.written_at,
            written_by: wire.written_by,
        }
    }
}

impl GrillDefaultRecord {
    pub fn validate(&self) -> Result<(), &'static str> {
        p2c_validate_record(
            &self.proposal_id,
            self.revision,
            &self.revision_hash,
            &self.question_id,
            self.record_sequence,
            &self.written_at,
        )?;
        Ok(())
    }
}

impl<'de> Deserialize<'de> for GrillDefaultRecord {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = Self::from(GrillDefaultRecordWire::deserialize(deserializer)?);
        value.validate().map_err(serde::de::Error::custom)?;
        Ok(value)
    }
}
