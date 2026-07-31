// Package gui implements the Ananke controlled-repair GUI wiring for the
// Tauri 2 desktop shell. It provides Go-side types, interfaces, and a
// simple HTTP API that the Tauri 2 frontend calls for repair submission,
// status monitoring, evidence display, and accept/reject actions.
//
// The GUI is intentionally minimal: it provides the API surface and types
// for the controlled-repair flow without the full Tauri 2 frontend. The
// frontend (HTML/CSS/JS) will be added in a subsequent step.
package gui
