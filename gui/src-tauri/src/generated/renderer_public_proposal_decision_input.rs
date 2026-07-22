// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::DecideProposalApprovalInput;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: DecideProposalApprovalInput = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public arguments of the Tauri decide_proposal_approval command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DecideProposalApprovalInput {
    pub approval_id: String,

    pub decision: Decision,

    pub idempotency_key: String,

    pub proposal_id: String,

    pub reason: String,

    pub revision: i64,

    pub revision_hash: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Decision {
    Approved,

    Rejected,
}
