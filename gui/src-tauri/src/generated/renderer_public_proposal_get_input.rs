// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::GetProposalInput;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: GetProposalInput = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public arguments of the Tauri get_proposal command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct GetProposalInput {
    pub proposal_id: String,
}
