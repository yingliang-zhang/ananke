// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::ListProposalsInput;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: ListProposalsInput = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public arguments of the Tauri list_proposals command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ListProposalsInput {
    pub project_id: String,

    pub workstream_id: String,
}
