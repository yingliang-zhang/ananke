// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::ProposalMutation;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: ProposalMutation = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public durable identity returned by a task-proposal mutation or idempotent replay.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalMutation {
    pub approval_id: String,

    pub proposal_id: String,

    pub revision: i64,

    pub revision_hash: String,
}
