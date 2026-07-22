// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::Cancel;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: Cancel = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};

/// Public result of the Tauri cancel_run command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Cancel {
    pub accepted: bool,

    pub state: String,
}
