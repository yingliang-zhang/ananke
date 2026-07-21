# ADR-0004: Select JSON Schema and Quicktype for P0a schema code generation

**Status:** Accepted (P0a experiment only)

## Context

Ananke currently crosses two distinct JSON boundaries: private daemon request and
response messages between Go and the Rust bridge, and renderer-public DTOs
between Rust and TypeScript. Existing semantic adapters, including the flat
internal `JsonRun` to nested public `RunDto` conversion, are intentionally not
automatically generated in P0a.

P0a compared two experiment-only toolchains under the existing newline-delimited
JSON transport:

- **Arm A:** JSON Schema 2020-12 + Quicktype;
- **Arm B:** Proto3 + Buf ecosystem with ProtoJSON compatibility shims.

The frozen revision-4 contract required separate request-private,
response-internal, and renderer-public schemas; nine fixture behaviors;
TS/Rust-public privacy checks; content-difference drift rejection; clean
regeneration; optional-field evolution; and a protected-production scope guard.

## Decision

Select **JSON Schema 2020-12 + Quicktype** as the P0a code-generation
candidate for further Ananke work.

The selection is bounded to schema type generation and JSON serialization. It
does **not** migrate the daemon transport, introduce gRPC or protobuf binary
wire format, replace the existing `JsonRun -> RunDto` semantic adapter, or
promote P0a experiment artifacts into production.

The continuing boundary model is:

| Schema | Generated targets |
|---|---|
| daemon-request-private | Go, Rust-private |
| daemon-response-internal | Go, Rust-internal |
| renderer-public | Rust-public, TypeScript |

## Alternatives

### Proto3 + Buf — not selected; bounded

Arm B generated output and passed basic generation, TypeScript private-boundary,
and clean-generation checks. It did not meet the contract must-haves:

- Rust fixture round trips dropped `payload: null` and changed numeric JSON
  representation (`experiments/p0a/proto-buf/artifacts/test-fixtures.log`);
- snake/unknown/absent/payload probes targeted non-existent Rust test targets;
- drift probe did not prove a content-difference rejection;
- no complete verifier, README, optional-evolution evidence, or Buf breaking
  probe was produced after three repair attempts.

Its terminal result is **BOUNDED — CHANGES REQUESTED**, not a rejection of
protobuf generally.

### Hand-maintained DTO mirrors — not selected

They retain current duplication and do not provide reproducible generated-tree,
privacy, drift, or evolution evidence. Existing semantic adapters remain
explicitly out of scope rather than being mislabeled as mirrors.

## Consequences

- Future P0b work may use the selected schema pattern as a starting point, but
  requires a new decision before production migration.
- The generated public Rust and TypeScript artifacts must keep structural
  exclusion checks for private `token` and raw daemon `error` fields.
- Any production adoption must retain the repository-rooted protected-file guard
  and regenerate artifacts rather than hand-editing them.
- Numeric payload fidelity remains bounded to frozen fixture values; no claim is
  made for arbitrary-precision numeric payloads.
- Proto3 + Buf may be revisited only with a complete verifier that closes its
  current runtime serialization, drift, evolution, and breaking-compatibility
  gaps.

## Evidence

Arm A final verifier, run independently with Node 22.22.3 on 2026-07-21:

- `node scripts/verify.mjs` → PASS, 15.889s;
- generated output: 6 files / 677 LOC (Go 2/212, Rust 3/209, TypeScript 1/256);
- Go 1.26.5, Git 2.54.0, Quicktype 26.0.0, TypeScript 5.9.3, Cargo 1.97.1;
- final focused independent hard review → `ACCEPT`.

The detailed Arm A evidence is in its isolated experiment worktree:
`ananke-p0a-jsonschema-quicktype/experiments/p0a/jsonschema-quicktype/`.
The frozen contract remains `docs/experiments/p0a-schema-codegen-contract.md`.
