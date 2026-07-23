// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::EvaluateGrillInput;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: EvaluateGrillInput = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public arguments of the future Tauri evaluate_grill command. The bridge derives the
/// private Grill declaration, hash, and rule version from the immutable revision.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct EvaluateGrillInput {
    pub proposal_id: String,

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

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct EvaluateGrillInputWire {
    proposal_id: String,
    revision: i64,
    revision_hash: String,
}

impl From<EvaluateGrillInputWire> for EvaluateGrillInput {
    fn from(wire: EvaluateGrillInputWire) -> Self {
        Self {
            proposal_id: wire.proposal_id,
            revision: wire.revision,
            revision_hash: wire.revision_hash,
        }
    }
}

impl EvaluateGrillInput {
    pub fn validate(&self) -> Result<(), &'static str> {
        p2c_validate_identity(&self.proposal_id, self.revision, &self.revision_hash)?;
        Ok(())
    }
}

impl<'de> Deserialize<'de> for EvaluateGrillInput {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = Self::from(EvaluateGrillInputWire::deserialize(deserializer)?);
        value.validate().map_err(serde::de::Error::custom)?;
        Ok(value)
    }
}
