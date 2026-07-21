# P0a schema/codegen binary spike contract

**Status:** frozen experiment contract, revision 4; P0a only.
**Base:** `eedba6d`
**Out of scope:** production DTO migration, daemon transport change, gRPC,
Protobuf binary transport, proposal/Grill/adapter work, commits.

## Decision to test

Select or reject a reproducible schema/codegen toolchain for Ananke's public
Go ↔ Rust ↔ TypeScript boundary. The two arms must independently implement the
same contract and tests:

- **A:** JSON Schema 2020-12 + Quicktype.
- **B:** Proto3 + Buf, `protoc-gen-go`, `prost-build`/`pbjson-build`, and
  `protoc-gen-es`.

Each arm is a self-contained experiment under `experiments/p0a/<arm>/`. It may
add pinned experiment-only tooling and generated sources, but must not edit
`internal/lifecycle/engine.go`, `gui/src-tauri/src/lib.rs`, or `gui/src/main.ts`.

## Boundary partition

There are three different schemas. Treating them as one is a boundary defect.

| Schema | Consumers | May contain | Must not contain |
|---|---|---|---|
| `daemon-request-private` | Go daemon, Rust bridge | auth `token`, project `root`, worker launch path/args/env | Renderer types or generated TS artifacts |
| `daemon-response-internal` | Go daemon, Rust bridge | `ok`, `error`, run/events, cancellation acceptance | Generated TypeScript artifacts or renderer DTOs |
| `renderer-public` | Rust bridge, TypeScript renderer | Bootstrap, Run, Event, CancelResult, Health | token, raw daemon `error`, socket path, worker environment, supervisor identity file |

| Schema | Generated targets |
|---|---|
| `daemon-request-private` | Go, Rust-private |
| `daemon-response-internal` | Go, Rust-internal |
| `renderer-public` | Rust-public, TypeScript |

P0a tests schema types and serialization only. The existing semantic converter
`JsonRun -> RunDto` is an intentional out-of-scope adapter, not a forbidden
hand-maintained type mirror. The renderer fixture must cover the nested
`RunDto { id, state, diagnostics }` shape actually sent by the Rust bridge.

The existing `project.root` field is intentionally represented in the public
fixture because Rust currently sends it while `main.ts` does not declare it.
Renderer Run diagnostics intentionally include worker/supervisor PIDs in this
spike because current `RunDto` exposes them; P0a does not expand that surface.
P0a records whether a candidate detects/reproduces that drift; it does not
remove the field or redefine its product visibility.

## Common fixture contract

The fixture files under `contracts/p0a/fixtures/` are copied verbatim into each
arm before generation. No arm may inspect the other arm's source, generated
artifacts, manifests, test output, or review output.

| Fixture | Required behavior |
|---|---|
| `daemon-request-list-runs.json` | Private request decodes `token`, command, and optional scope. No TS artifact may contain `token`. |
| `daemon-response-empty.json` | Internal daemon response keeps `ok:true` while other response fields are absent. |
| `daemon-response-error.json` | Internal daemon response preserves wire-present `ok:false` and its error string. |
| `daemon-response-events.json` | Internal daemon response preserves run diagnostics, ordered Event sequence, and `payload: null/object/array/scalar`. |
| `daemon-response-unknown.json` | Internal daemon response tolerates an unknown inbound field while retaining known fields. |
| `renderer-bootstrap.json` | Public projection preserves all documented fields, including the currently actual `project.root` field. |
| `renderer-activity.json` | Public Run/Event projection preserves diagnostics and all Event payload variants without a daemon envelope or raw error. |
| `renderer-cancel.json` / `renderer-health.json` | Boolean/state fields preserve exact JSON names and values. |

## Wire compatibility rules

1. The current newline-delimited JSON socket remains the only transport.
   Proto arm B uses ProtoJSON for its experiment; it must not replace the
   transport with binary Protobuf.
2. Field names must exactly match fixture snake_case names on the wire.
3. `payload` must preserve JSON `null`, object, array, string, number, and
   boolean values. Coercing it to a string or losing `null` fails.
4. Inbound unknown fields are ignored for this experiment, matching current Go
   `encoding/json` and default Rust Serde behavior.
5. `ok` is a presence-sensitive boolean: `ok:false` must remain present when
   serialized, matching Go's non-omitempty `apiResponse.OK` field. Proto arm
   must use an explicit-presence representation or is incompatible.
6. Optional absent fields remain absent on serialization; no arm may emit a
   zero/default value merely because a target language represents one.
7. Generated artifacts are read-only output. Hand-editing them fails the arm.
8. `payload` fidelity is evaluated only for the frozen fixture values. Proto
   `google.protobuf.Value` double precision is a documented limitation for
   integers beyond 2^53 and arbitrary-precision decimals, not a pass claim.

### ProtoJSON configuration baseline

Proto arm must explicitly configure snake_case JSON names and unknown-field
discarding in each target runtime; default camelCase output or default unknown
field rejection does not satisfy this experiment. Explicit-presence support is
required for internal `ok:false`.

## Required tests and measurements

Both arms must provide a single reproducible command that:

1. generates Go, Rust/Serde, and TypeScript artifacts from its schema;
2. runs all nine fixture tests in their relevant generated runtimes;
3. proves no TypeScript generated source contains private `token` or raw daemon
   `error` fields, and proves the same for Rust-public generated source;
4. deliberately mutates the contents of an existing generated file, regenerates
   the same source into a separate staging tree, and proves a content diff
   fails before restoring the original file; a missing-file or inventory-only
   failure does not count as drift detection;
5. regenerates from a clean generated directory and proves the tree is clean.
6. proves `git diff --exit-code -- internal/lifecycle/engine.go gui/src-tauri/src/lib.rs gui/src/main.ts` is clean.

Record the following, with commands and raw values:

| Measure | Record |
|---|---|
| Exact tool versions and lock/config files | command output |
| Generation wall-clock | seconds |
| Focused fixture verification wall-clock | seconds |
| Generated LOC/file count by language | counts |
| New runtime/build dependencies by language | manifest diff |
| First-pass agent outcome | compile/test failures and repair rounds |
| Schema evolution probe | add optional field; classify compatibility and required edits |
| Drift probe | modified generated file rejected by exact command |

## Arm verdicts

- **VALIDATED:** all fixture, drift, and clean-generation checks pass without
  hand-editing generated artifacts.
- **BOUNDED — CHANGES REQUESTED:** the arm reaches three repair/review rounds
  without a validated result.
- **REJECTED:** it requires a transport migration, cannot preserve payload or
  optional semantics, exposes private fields to renderer-public artifacts, or
  needs hand-maintained schema type mirrors beyond the declared semantic
  converters.

A candidate is **VALIDATED** only if every must-have above passes. A candidate
is **BOUNDED — CHANGES REQUESTED** after three complete repair rounds (a round
means one full verifier run followed by one independent review/fix cycle) with
one or more must-haves still failing. Select the validated candidate with the
fewest experiment-only dependencies; use measured clean-generation time only
as a tie-breaker. Reject both if neither candidate is validated.

A P0a ADR may select A, select B, or reject both. It must not claim a winner
until both arms reach a terminal verdict and an independent reviewer checks the
comparison.
