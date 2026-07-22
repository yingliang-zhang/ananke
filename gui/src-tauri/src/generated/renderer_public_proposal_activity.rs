// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::ProposalActivity;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: ProposalActivity = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalActivity {
    pub approval_id: String,

    pub operation: Operation,

    pub proposal_id: String,

    pub revision: i64,

    pub revision_hash: String,

    pub sequence: i64,

    pub written_at: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Operation {
    #[serde(rename = "append_revision")]
    AppendRevision,

    #[serde(rename = "create_proposal")]
    CreateProposal,

    #[serde(rename = "decide_approval")]
    DecideApproval,

    #[serde(rename = "withdraw_proposal")]
    WithdrawProposal,
}
