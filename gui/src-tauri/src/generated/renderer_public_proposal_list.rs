// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::ProposalList;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: ProposalList = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public result of the Tauri list_proposals command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalList {
    pub proposals: Vec<Proposal>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Proposal {
    pub created_at: String,

    pub created_by: CreatedBy,

    pub current_revision: i64,

    pub current_revision_hash: String,

    pub project_id: String,

    pub proposal_id: String,

    pub state: State,

    pub workstream_id: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CreatedBy {
    #[serde(rename = "local_gui_operator")]
    LocalGuiOperator,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum State {
    Approved,

    Open,

    Withdrawn,
}
