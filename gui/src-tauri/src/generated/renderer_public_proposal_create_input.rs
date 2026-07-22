// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::CreateProposalInput;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: CreateProposalInput = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public arguments of the Tauri create_proposal command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct CreateProposalInput {
    pub idempotency_key: String,

    pub project_id: String,

    pub revision_input: CreateProposalRevisionInput,

    pub workstream_id: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct CreateProposalRevisionInput {
    pub acceptance_criteria: Vec<String>,

    pub policy: CreateProposalPolicy,

    pub task: CreateProposalTask,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct CreateProposalPolicy {
    pub adapter: CreateProposalAdapterPolicy,

    pub authority: Authority,

    pub budget: CreateProposalBudgetPolicy,

    pub model_role: ModelRole,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct CreateProposalAdapterPolicy {
    pub access: Access,

    pub kind: Kind,

    pub status: Status,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Access {
    #[serde(rename = "read_only")]
    ReadOnly,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Kind {
    #[serde(rename = "omp_audit")]
    OmpAudit,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Status {
    Future,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Authority {
    Deterministic,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct CreateProposalBudgetPolicy {
    pub dimensions: Vec<String>,

    pub status: Status,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ModelRole {
    #[serde(rename = "advisory_only")]
    AdvisoryOnly,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct CreateProposalTask {
    pub instructions: String,

    pub title: String,
}
