// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::Run;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: Run = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public result of the Tauri list_runs command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Run {
    pub diagnostics: RunDiagnostics,

    pub id: String,

    pub state: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct RunDiagnostics {
    pub committed_offset: i64,

    pub project_id: String,

    pub supervisor_pid: i64,

    pub worker_pid: i64,

    pub workstream_id: String,
}
