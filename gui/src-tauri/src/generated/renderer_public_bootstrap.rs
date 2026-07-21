// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::Bootstrap;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: Bootstrap = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public result of the Tauri bootstrap command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Bootstrap {
    pub project: Project,

    pub workstream: Workstream,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Project {
    pub id: String,

    pub name: String,

    pub root: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Workstream {
    pub id: String,

    pub name: String,

    pub project_id: String,
}
