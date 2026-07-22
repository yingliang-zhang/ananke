# P1c task-proposal public protocol contract

**Status:** approved bounded contract.

## Decision

P1c defines the renderer-public argument and result DTOs for the task-proposal
boundary. It does not add a GUI view, Tauri command, daemon handler, daemon
wire schema, store query, worker, claim, adapter, Grill integration, commit, or
push.

The existing daemon socket protocol remains private: every bridge request
contains `cmd` and `token`; every response contains `ok` and may contain raw
`error`. None of those fields are renderer-public. A later integration slice
must translate the public Tauri command names to the hyphenated daemon commands
and map all failures through the existing sanitized bridge error path.

## Public commands

| Tauri command | Future daemon command | Input DTO | Result DTO |
| --- | --- | --- | --- |
| `create_proposal` | `create-proposal` | `CreateProposalInput` | `ProposalMutation` |
| `list_proposals` | `list-proposals` | `ListProposalsInput` | `ProposalList` |
| `get_proposal` | `get-proposal` | `GetProposalInput` | `ProposalDetail` |
| `list_proposal_activity` | `list-proposal-activity` | `ListProposalActivityInput` | `ProposalActivityList` |
| `append_proposal_revision` | `append-proposal-revision` | `AppendProposalRevisionInput` | `ProposalMutation` |
| `decide_proposal_approval` | `decide-proposal-approval` | `DecideProposalApprovalInput` | `ProposalMutation` |
| `withdraw_proposal` | `withdraw-proposal` | `WithdrawProposalInput` | `ProposalMutation` |

`ProposalDetail` contains the Proposal’s current immutable Revision, its paired
RevisionLifecycle, and its paired Approval. `ProposalList` is deliberately a
list of Proposal summaries only; callers fetch the current detail explicitly.
`ProposalActivityList` is the complete ascending sequence for one Proposal.
P1c adds neither pagination nor filters beyond the target project/workstream.

Inputs preserve P1a’s mutation bodies and caller idempotency keys, but omit the
P1a internal envelope fields (`schema_version`, `operation`, `scope`, and
`body_hash`). The bridge is responsible for constructing the private daemon
request; it must not make those private transport details renderer-visible.

All four mutation commands return only `ProposalMutation`:
`proposal_id`, `revision`, `revision_hash`, and `approval_id`. A client reads
`ProposalDetail` or activity after a mutation when it needs mutable state.
This preserves P1b’s replay identity without promising a race-prone composite
post-mutation snapshot.

## Compatibility with P1a and P1b

P1c is a projection of the frozen P1a records:

- Proposal, Revision, RevisionLifecycle, Approval, and ProposalActivity retain
  their P1a wire names and closed shapes.
- `Revision.revision_hash` remains absent because it is not part of the hashed
  snapshot. The paired lifecycle, approval, activity, and mutation records
  carry that identity where needed.
- The fixed future-only policy remains visible exactly as P1a specified. It
  contains no budget value, adapter parameter, audit result, provider, model
  name, prompt, completion, or execution command.
- The current P1b store exposes `CreateProposal`, `GetProposal`,
  `GetRevision`, `GetRevisionLifecycle`, `GetApproval`,
  `ListProposalActivity`, `AppendProposalRevision`,
  `DecideProposalApproval`, and `WithdrawProposal`. It does not expose a
  proposal-list query and the current daemon dispatch has no proposal commands.
  P1c specifies the required public list contract; it does not pretend that
  either missing implementation exists.

## Privacy allowlist

The only renderer-public record fields are those in the generated schemas and
canonical fixture. They are Proposal/Revision/RevisionLifecycle/Approval/
ProposalActivity data, mutation identity, and the seven command inputs above.
No renderer-public shape may add runtime paths, worker or process data,
credentials, tokens, socket or identity material, daemon command/envelope
metadata, raw errors, model/provider/prompt/output data, adapter output, or
execution data.

`project_id` and `workstream_id` are logical identifiers, never paths.
`policy.model_role: "advisory_only"` is the frozen P1a policy constant, not
model data. Closed schemas and the P1c verifier are the boundary; the verifier
also rejects defense-in-depth private names.

## Contract artifacts

- `contracts/p1c/fixtures/protocol-v1.canonical.json` is the canonical public
  command/result vector; `fixtures.sha256` freezes its bytes.
- `contracts/p1c/verify.mjs` validates JCS bytes, manifest, closed DTO shapes,
  IDs/hashes/timestamps/policy, command mapping, cross-record links, and the
  public allowlist. Its self-test demonstrates rejection of content drift,
  private runtime fields, and unknown public fields.
- `gui/contracts/renderer-public-proposal-*.schema.json` defines public command
  inputs and results. The existing Node 22 + Quicktype generator creates the
  corresponding Rust and TypeScript artifacts and its existing privacy/content
  checks cover every target.

## Explicit non-goals

- Tauri command registration, bridge-to-daemon conversion, daemon dispatch, or
  daemon private request/response schema changes;
- P1b store migrations, query APIs, or mutation changes;
- GUI screens, polling, pagination, authorization, claims, workers, adapters,
  Grill, model calls, policy execution, budgets, or execution;
- changes to current run/lifecycle behavior; commits or pushes.
