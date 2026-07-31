Implement P6 Contract Slices 1–2 closure repair batch B2 in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` using strict RED→GREEN TDD. This is local Go release-certificate bundle validation. No commit.

Current state:
- B1 is green: effect-time authorization/release-date validation and FullFence/closed IDs are implemented; 79 executable vectors pass.
- Four new PUBLIC DER inputs exist under `internal/repaircontract/testdata/release-v1/`:
  - `rotation-approver-root-cert.der` SHA-256 `a90cfe559f5e5b2ccfee3618212849c6129cfce440c43f288835f9d2740f42f1`
  - `rotation-approver-root-spki.der` SHA-256 `2e1d84cae2dd6f87de7090205af7a900436be99d5ff1a0868762896622fa834c`
  - `rotation-approver-cert.der` SHA-256 `a428af51ea351d9e34cb6f139539bd2a2d9edbfc5acc519915c5323283e41180`
  - `rotation-approver-spki.der` SHA-256 `95b82df9b281943f2229a0ab6d830c4b0133eda9bcec9eee8cffdd4f82db1d1d`
- These are a distinct Ed25519 X.509 root/leaf chain. Leaf critical role extension: `controlled_repair_rotation_release_approver`; critical domain extension: `ananke.controlled-repair.root-rotation-release-approval.v1`.
- Never read, print, hash, copy, or reference any repo-external private-key file/path.

Goal: the frozen future rotation declaration must identify an independently published approver key from the current release, rather than accepting a signer identity supplied only by a future approval record. V1 still has `no_successor_authorized`; do not create any active successor or signature instance.

Strict TDD:
1. Add focused RED tests showing current embedded bundle/manifest/pins do not include these four real public artifacts and the future approval declaration has no fixed published approver identity.
2. Implement minimal GREEN:
   - add four explicit non-wildcard `go:embed` inputs;
   - extend canonical public trust bundle with exact base64 DER/SPKI for the independent approver root/leaf;
   - update release manifest to bind exact hashes for all other public artifacts (now fourteen total including manifest, if the current ten plus four count remains correct);
   - extend `ReleasePins` and frozen trust structures with exact approver root/leaf certificate and SPKI hashes, fixed approver key ID, role, and domain;
   - parse and verify root self-signature, leaf chain, exact DER/SPKI, Ed25519 algorithm, CA/key usages, exclusive validity, issuer/subject, critical role/domain, and distinction from repair root/leaf and P5 role/key;
   - update rotation policy so the independent approval record must match the fixed released approver key ID/SPKI/role/domain and cannot choose them dynamically;
   - preserve exact future proposal/cross-signature/approval canonical record fields and `no_successor_authorized` state.
3. Add permanent negative vectors for changed approver root/leaf/SPKI, wrong role/domain, expired/future certificate, reuse of repair root/leaf, and approval record signer ID/SPKI differing from the published approver.
4. Extend executable ordered registry with each named behavior; every entry runs its specific probe.
5. Regenerate canonical JSON artifacts and fixture from code. Assert all JSON is canonical, manifest hashes exact, all fourteen public inputs are public-only, and no private material/path/name appears.
6. Preserve every B1/A1/A2 behavior. No runtime/store/process/socket/filesystem-open/signature-generation code.
7. Update `docs/experiments/p6-controlled-repair-supervisor-contract.md` accurately.

Allowed edits: `internal/repaircontract/**` and the experiment doc only. Run focused RED/GREEN, registry, package single, count=10, race count=3, vet, gofmt, diff-check. Return exact evidence and unresolved failures. Do not claim Slice ACCEPT; another read-only closure review follows. Do not create cron jobs.
