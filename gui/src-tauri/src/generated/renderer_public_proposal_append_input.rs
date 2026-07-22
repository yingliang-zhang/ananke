// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::AppendProposalRevisionInput;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: AppendProposalRevisionInput = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public arguments of the Tauri append_proposal_revision command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AppendProposalRevisionInput {
    pub expected_current_revision: i64,

    pub expected_current_revision_hash: String,

    pub idempotency_key: String,

    pub proposal_id: String,

    pub revision_input: AppendProposalRevisionInputBody,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AppendProposalRevisionInputBody {
    pub acceptance_criteria: Vec<String>,

    pub policy: AppendProposalPolicy,

    pub task: AppendProposalTask,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AppendProposalPolicy {
    pub adapter: AppendProposalAdapterPolicy,

    pub authority: Authority,

    pub budget: AppendProposalBudgetPolicy,

    pub model_role: ModelRole,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AppendProposalAdapterPolicy {
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
pub struct AppendProposalBudgetPolicy {
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
pub struct AppendProposalTask {
    pub instructions: String,

    pub title: String,
}
