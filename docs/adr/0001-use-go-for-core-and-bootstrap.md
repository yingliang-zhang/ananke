# ADR-0001: Use Go for core and bootstrap

## Status

Accepted — 2026-07-17

## Context

A binary language spike compared Rust and Go implementations of the same
frozen supervisor-lifecycle contract. Both candidates passed all automated
gates (tests, mutations, stress, race, black-box) but failed independent
hard review with lifecycle BLOCKERs.

Key findings from the spike:

- Both languages retained the same core defect: numeric PGID identity
  after process-group leader reap. Rust's type system did not prevent it.
- Go had a smaller agent-edit surface (3,592 vs 4,204 nonblank LOC).
- Go had faster build feedback (8.4s clean vs 15.2s clean).
- Go completed all six mutation gates; Rust completed four.
- Go had a full race detector; Rust relied on Clippy and tests.
- Go had fewer direct dependencies (2 vs 6).
- Rust had lower idle RSS (3.7 MiB vs 13.8 MiB), but both are acceptable
  for a desktop daemon.

The user's workflow depends on AI coding agents for all implementation.
Shorter feedback loops and smaller code surfaces outweigh Rust's
compile-time safety advantages for this specific use case.

## Decision

Implement `ananke-core` and `ananke-bootstrap` in Go. The Tauri 2
desktop shell retains its required thin Rust layer, but Rust is not the
durable authority.

## Alternatives

### Rust

Rejected as the durable authority. Its type system did not prevent the
lifecycle defects that dominated the spike. The larger code surface and
slower build feedback increase agent-maintenance cost without
commensurate safety gains for this project's failure modes.

### TypeScript/Node

Eliminated before the spike. Runtime lifecycle surface, dependency churn,
and process-control ergonomics are unsuitable for a durable supervisor.

### Python

Reserved for out-of-process capability packs. Not suitable for the
durable authority due to runtime embedding, packaging, and lifecycle
control limitations.

## Consequences

- Go's goroutine/channel model is the primary concurrency primitive.
- `golang.org/x/sys` provides OS-specific lifecycle operations.
- `modernc.org/sqlite` provides CGO-free SQLite for portability.
- The race detector is a standard gate, not optional.
- Tauri's Rust layer is explicitly scoped to shell glue; it does not
  own state, recovery, or lifecycle decisions.
