// Package gui provides the repair execution logic for the Go daemon.
// The HTTP server and embedded HTML frontend have been removed (audit
// divergence fix: the Tauri 2 native GUI is the only operator surface).
// RunRepair is the salvaged entry point for the Go daemon's repair IPC.
package gui
