// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::ProposalDetail;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: ProposalDetail = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public result of the Tauri get_proposal command for the current revision and its paired
/// records.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalDetail {
    pub approval: Approval,

    pub lifecycle: RevisionLifecycle,

    pub proposal: ProposalDetailProposal,

    pub revision: Revision,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Approval {
    pub approval_id: String,

    pub created_at: String,

    pub created_by: CreatedBy,

    pub decided_at: Option<String>,

    pub decided_by: Option<String>,

    pub decision_idempotency_key: Option<String>,

    pub proposal_id: String,

    pub reason: Option<String>,

    pub revision: i64,

    pub revision_hash: String,

    pub state: ApprovalState,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CreatedBy {
    #[serde(rename = "local_gui_operator")]
    LocalGuiOperator,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ApprovalState {
    Approved,

    Pending,

    Rejected,

    Superseded,

    Withdrawn,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct RevisionLifecycle {
    pub approval_id: String,

    pub created_at: String,

    pub proposal_id: String,

    pub revision: i64,

    pub revision_hash: String,

    pub state: ApprovalState,

    pub updated_at: String,

    pub version: i64,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalDetailProposal {
    pub created_at: String,

    pub created_by: CreatedBy,

    pub current_revision: i64,

    pub current_revision_hash: String,

    pub project_id: String,

    pub proposal_id: String,

    pub state: ProposalState,

    pub workstream_id: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ProposalState {
    Approved,

    Open,

    Withdrawn,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Revision {
    pub acceptance_criteria: Vec<String>,

    pub created_at: String,

    pub created_by: CreatedBy,

    pub idempotency_key: String,

    pub parent_revision: Option<i64>,

    pub parent_revision_hash: Option<String>,

    pub policy: ProposalPolicy,

    pub proposal_id: String,

    pub revision: i64,

    pub schema_version: SchemaVersion,

    pub task: ProposalTask,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalPolicy {
    pub adapter: ProposalAdapterPolicy,

    pub authority: Authority,

    pub budget: ProposalBudgetPolicy,

    pub model_role: ModelRole,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalAdapterPolicy {
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
pub struct ProposalBudgetPolicy {
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
pub enum SchemaVersion {
    #[serde(rename = "ananke.proposal-revision.v1")]
    AnankeProposalRevisionV1,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProposalTask {
    pub instructions: String,

    pub title: String,
}
