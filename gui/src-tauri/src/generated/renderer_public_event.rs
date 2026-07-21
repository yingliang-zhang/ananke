// Example code that deserializes and serializes the model.
// extern crate serde;
// #[macro_use]
// extern crate serde_derive;
// extern crate serde_json;
//
// use generated_module::Event;
//
// fn main() {
//     let json = r#"{"answer": 42}"#;
//     let model: Event = serde_json::from_str(&json).unwrap();
// }

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Public result of the Tauri list_events command.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Event {
    /// Arbitrary non-null JSON payload from the daemon event stream.
    pub payload: Payload,

    pub seq: i64,

    #[serde(rename = "type")]
    pub event_type: String,
}

/// Arbitrary non-null JSON payload from the daemon event stream.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum Payload {
    AnythingArray(Vec<Option<serde_json::Value>>),

    AnythingMap(HashMap<String, Option<serde_json::Value>>),

    Bool(bool),

    Double(f64),

    PurpleString(String),
}
