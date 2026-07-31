# P1c publiWrote `artifacts/omp/p1c/public-protocol-contract-output.md`.

It records:

- frozen seven-command public protocol contract;
- created contract, fixture, manifest, and JSON Schema paths;
- observed P1b/daemon/Tauri baseline;
- SHA-256 evidence for the canonical fixture;
- strict public privacy boundary;
- deferred runtime/codegen/verifier work and associated risks;
- no commit or push.

Implementation and verification todos were explicitly deferred per the stop-code-change instruction.
als` | `ListProposalsInput` | `ProposalList` |
| `get_proposal` | `get-proposal` | `GetProposalInput` | `ProposalDetail` |
| `list_proposal_activity` | `list-proposal-activity` | `ListProposalActivityInput` | `ProposalActivityList` |
| `append_proposal_revision` | `append-proposal-revision` | `AppendProposalRevisionInput` | `ProposalMutation` |
| `decide_proposal_approval` | `decide-proposal-approval` | `DecideProposalApprovalInput` | `ProposalMutation` |
| `withdraw_proposal` | `withdraw-proposal` | `WithdrawProposalInput` | `ProposalMutation` |

Mutation results intentionally contain only the durable P1b replay identity:
`proposal_id`, `revision`, `revision_hash`, and `approval_id`. A separate
`get_proposal` observes mutable state, avoiding a race-prone composite
post-mutation response.

`ProposalDetail` contains the current Proposal, immutable Revision, paired
RevisionLifecycle, and paired Approval. `ProposalList` is summary-only, scoped
by logical `project_id` and `workstream_id`. Activity is the complete ascending
sequence for one Proposal. Pagination and additional filters are not part of
P1c.

## Public boundary

P1a field names, fixed policy values, identifier rules, hashes, timestamps, and
nullable Approval decision fields are retained. The fixed
`policy.model_role: "advisory_only"` is a P1a policy constant, not model data.

The existing daemon Unix-socket protocol remains private. It currently carries
`cmd` and `token` in requests and `ok` plus potentially raw `error` in
responses. None of those fields are part of P1c renderer-public DTOs.

The public allowlist excludes runtime paths, roots, sockets, identity/transcript
material, workers, processes/PIDs/environments/commands, credentials/tokens,
raw daemon errors, model/provider/prompt/completion data, audit output, and
execution data. `project_id` and `workstream_id` are logical identifiers, never
paths.

## Created contract artifacts

### Contract and implementation plan

- `docs/experiments/p1c-task-proposal-public-protocol-contract.md`
- `docs/plans/2026-07-22-p1c-task-proposal-public-protocol.md`

### Canonical golden fixture

- `contracts/p1c/fixtures/protocol-v1.canonical.json`
- `contracts/p1c/fixtures/fixtures.sha256`

The fixture contains closed public command input/result vectors for all seven
commands, with a P1a-aligned approved detail and ordered activity history.

Observed fixture digest:

```text
sha256:daa879c3827f2740a9c21ea824244072de8afb37978b63daedf6f7a98cb9b53c
```

### Renderer-public JSON Schema DTOs

Inputs:

- `gui/contracts/renderer-public-proposal-create-input.schema.json`
- `gui/contracts/renderer-public-proposal-list-input.schema.json`
- `gui/contracts/renderer-public-proposal-get-input.schema.json`
- `gui/contracts/renderer-public-proposal-activity-list-input.schema.json`
- `gui/contracts/renderer-public-proposal-append-input.schema.json`
- `gui/contracts/renderer-public-proposal-decision-input.schema.json`
- `gui/contracts/renderer-public-proposal-withdraw-input.schema.json`

Results:

- `gui/contracts/renderer-public-proposal-mutation.schema.json`
- `gui/contracts/renderer-public-proposal-list.schema.json`
- `gui/contracts/renderer-public-proposal-detail.schema.json`
- `gui/contracts/renderer-public-proposal-activity-list.schema.json`

All schemas are JSON Schema 2020-12, closed objects, and codegen-ready for the
existing Node 22 + Quicktype renderer-public pipeline.

## Inspected baseline and deferred boundary

Observed baseline:

- Branch: `feat/task-proposal-core`.
- HEAD: `4dded30d050ab797457a3cb25846c5df943616fb`
  (`feat(store): persist sealed task proposal revisions`).
- P1b exposes `CreateProposal`, `GetProposal`, `GetRevision`,
  `GetRevisionLifecycle`, `GetApproval`, `ListProposalActivity`,
  `AppendProposalRevision`, `DecideProposalApproval`, and
  `WithdrawProposal`.
- P1b has no Proposal-list query.
- The daemon dispatch has no Proposal commands.
- The Tauri bridge has no Proposal commands and sends private `cmd`/`token`
  daemon requests.

A separately authorized integration slice must start with RED tests and then:

1. add deterministic P1b `ListProposalsByTarget` support;
2. add private daemon adapters/handlers for the seven hyphenated commands;
3. add bridge conversion and Tauri registration using generated public types;
4. map storage failures through existing sanitized public bridge errors;
5. prove real bridge serialization while preserving the private daemon envelope.

No GUI, claim, worker, adapter, Grill, model, execution, commit, or push is
within that follow-up slice unless separately authorized.

## Evidence and status

Observed:

- Current daemon protocol, Tauri bridge, P0b generator, P1a contract, and P1b
  store API were inspected before the contract was defined.
- The P1c canonical fixture was generated as a one-line canonical JSON value.
- `shasum -a 256 contracts/p1c/fixtures/protocol-v1.canonical.json` produced
  the recorded digest.

Intentionally not completed after the instruction to stop code changes:

- `contracts/p1c/verify.mjs` has not been added; the manifest/shape/privacy
  verifier and its self-test are specified, not executed.
- `gui/scripts/generate-renderer-public.mjs` has not been extended with the P1c
  schema inventory.
- No generated Rust or TypeScript P1c DTOs exist.
- No `generate:renderer-public`, content-drift/privacy check, TypeScript
  typecheck, or P1c verifier command was run.
- No production command, bridge, daemon, store, or GUI code was edited.

No commit or push was performed.
