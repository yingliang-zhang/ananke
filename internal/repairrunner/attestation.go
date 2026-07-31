package repairrunner

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/repairverifier"
	"github.com/yingliang-zhang/ananke/internal/store"
)

// ErrAttestationProduction is the sentinel for attestation production failures.
var ErrAttestationProduction = errors.New("attestation production failed")

// RepairContext holds all the inputs for a controlled-repair run.
type RepairContext struct {
	AuthorizationHash             string
	ApprovalHash                  string
	RequestHash                   string
	DispatchHash                  string
	AttemptHash                   string
	AttemptNumber                 int
	AttemptCap                    int
	ReleasePinsHash               string
	TrustBundleHash               string
	RepairAttestorCertificateHash string
	RepairAttestorRootID          string
	RepairAttestorLeafSPKI        string
	RequestNonceHash              string
	ResponseNonceHash             string
	ChannelHash                   string
	RepositoryBindingHash         string
	RepositoryIdentityHash        string
	CommonGitIdentityHash         string
	GitExecutableIdentityHash     string
	// Phase claims (Slice 3) — required by validateAttestationRecord
	EffectTimeValidationTimestamp    string
	MaterializationClaimHash         string
	AdapterClaimHash                 string
	TestClaimHash                    string
	PredecessorClaimHash             string
	SupervisorJournalHeadHash        string
	SupervisorJournalPredecessorHash string
	BootEpochID                      string
	BootEpochHash                    string
}

// ProduceSignedAttestation creates a signed repair-review attestation from
// the repair context, worktree result, adapter result, and test result.
// It signs the attestation with the provided signing material and persists
// it to the store.
//
// This implements Step 8 of the P6a runtime.
func ProduceSignedAttestation(
	ctx RepairContext,
	worktree *WorktreeResult,
	adapter *AdapterResult,
	test *TestProfileResult,
	material *repairverifier.RepairSigningMaterial,
	s *store.Store,
	now time.Time,
) (store.RepairAttestationRow, error) {
	if material == nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: nil signing material", ErrAttestationProduction)
	}

	// Build the attestation record with empty attestation_hash and signature.
	// The signature is computed over the canonical record excluding BOTH
	// signature and attestation_hash fields (breaking the circular dependency
	// identified in Step 2 audit D1). The attestation_hash is then computed
	// over the record including the signature but excluding attestation_hash.
	record := repaircontract.RepairReviewAttestation{
		SchemaVersion:                 repaircontract.AttestationSchemaVersion,
		AttestationHash:               "", // empty before signing
		IssuedAt:                      now.UTC().Format(time.RFC3339Nano),
		State:                         repaircontract.AttestationWaitingForReview,
		SignatureDomain:               repaircontract.SignatureDomain,
		Signature:                     "", // empty before signing
		ReleasePinsHash:               ctx.ReleasePinsHash,
		TrustBundleHash:               ctx.TrustBundleHash,
		RepairAttestorCertificateHash: ctx.RepairAttestorCertificateHash,
		RepairAttestorRootID:          ctx.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        ctx.RepairAttestorLeafSPKI,
		RequestNonceHash:              ctx.RequestNonceHash,
		ResponseNonceHash:             ctx.ResponseNonceHash,
		ChannelHash:                   ctx.ChannelHash,
		AuthorizationHash:             ctx.AuthorizationHash,
		ApprovalHash:                  ctx.ApprovalHash,
		RequestHash:                   ctx.RequestHash,
		DispatchHash:                  ctx.DispatchHash,
		AttemptHash:                   ctx.AttemptHash,
		AttemptNumber:                 ctx.AttemptNumber,
		AttemptCap:                    ctx.AttemptCap,
		// Repository (Slice 4)
		RepositoryBindingHash:             ctx.RepositoryBindingHash,
		RepositoryIdentityHash:            ctx.RepositoryIdentityHash,
		CommonGitIdentityHash:             ctx.CommonGitIdentityHash,
		GitExecutableIdentityHash:         ctx.GitExecutableIdentityHash,
		WorktreeParentHash:                worktree.WorktreeParentHash,
		WorktreeTargetHash:                worktree.WorktreeTargetHash,
		WorktreeAdminHash:                 worktree.WorktreeAdminHash,
		WorktreeDescriptorHash:            worktree.WorktreeDescriptorHash,
		WorktreeSlotID:                    worktree.WorktreeSlotID,
		WorktreeSlotPathHash:              worktree.WorktreeSlotPathHash,
		InstalledWorktreeRootIdentityHash: worktree.InstalledWorktreeRootIdentityHash,
		// Phase claims (Slice 3) — required by validateAttestationRecord
		EffectTimeValidationTimestamp:    ctx.EffectTimeValidationTimestamp,
		MaterializationClaimHash:         ctx.MaterializationClaimHash,
		AdapterClaimHash:                 ctx.AdapterClaimHash,
		TestClaimHash:                    ctx.TestClaimHash,
		PredecessorClaimHash:             ctx.PredecessorClaimHash,
		SupervisorJournalHeadHash:        ctx.SupervisorJournalHeadHash,
		SupervisorJournalPredecessorHash: ctx.SupervisorJournalPredecessorHash,
		BootEpochID:                      ctx.BootEpochID,
		BootEpochHash:                    ctx.BootEpochHash,
		// Adapter (Slice 5)
		AdapterSeatbeltProfileHash: adapter.SeatbeltProfileHash,
		AdapterSandboxHash:         adapter.SandboxHash,
		AdapterTerminalProofHash:   adapter.TerminalProofHash,
		AdapterCapabilityHash:      adapter.CapabilityHash,
		UIDPoolHash:                adapter.UIDPoolHash,
		UIDLeaseHash:               adapter.UIDLeaseHash,
		UID:                        adapter.UID,
		GroupID:                    adapter.GroupID,
		// Patch
		PatchHash:          worktree.PatchHash,
		PatchSize:          worktree.PatchSize,
		OrderedPathsHash:   worktree.Diff.OrderedPathsHash,
		StatusHash:         worktree.Diff.StatusHash,
		RawHash:            worktree.Diff.RawHash,
		NumstatHash:        worktree.Diff.NumstatHash,
		IgnoredHash:        worktree.Diff.IgnoredHash,
		FilesystemScanHash: worktree.Diff.FilesystemScanHash,
		// Tests (Slice 6)
		ToolchainManifestHash: test.ToolchainManifestHash,
		TestProfileHash:       test.TestProfileHash,
		CandidateCopyHash:     test.CandidateCopyHash,
		TestSandboxHash:       test.TestSandboxHash,
		TestTerminalProofHash: test.TestTerminalProofHash,
		TestRootCleanupHash:   test.TestRootCleanupHash,
		TestResultHash:        test.TestResultHash,
		TestOutputHash:        test.TestOutputHash,
		TestOutputSize:        test.TestOutputSize,
		TestCommandHash:       test.TestCommandHash,
		TestCapabilityHash:    test.TestCapabilityHash,
	}

	// Generate attestation ID.
	record.AttestationID = generateAttestationID(ctx.AttemptHash, now)

	// Sign the attestation. The signed bytes exclude BOTH signature and
	// attestation_hash fields to break the circular dependency.
	signedBytes, err := repaircontract.RuntimeSignatureCanonicalBytes(record)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: canonical bytes: %v", ErrAttestationProduction, err)
	}
	signature, err := material.SignAttestationBytes(signedBytes)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: sign: %v", ErrAttestationProduction, err)
	}
	record.Signature = signature

	// Compute attestation hash using the contract layer's hashRecord function
	// (which deletes the attestation_hash key, not sets it to empty).
	attestationHash, err := repaircontract.HashAttestationRecord(record)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: hash: %v", ErrAttestationProduction, err)
	}
	record.AttestationHash = attestationHash

	// Verify our own signature before persisting. The verification uses the
	// same RuntimeSignatureCanonicalBytes function that excludes both fields.
	verifyBytes, err := repaircontract.RuntimeSignatureCanonicalBytes(record)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: verify bytes: %v", ErrAttestationProduction, err)
	}
	if err := material.VerifyAttestationBytes(verifyBytes, signature); err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: self-verify: %v", ErrAttestationProduction, err)
	}

	// Persist to store.
	row, err := s.PersistRepairAttestation(context.Background(), record)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: persist: %v", ErrAttestationProduction, err)
	}

	return row, nil
}

// VerifyAttestationWithAnanke verifies a persisted attestation using the
// repair verifier. This is what Ananke (holding only public keys) calls
// to verify a supervisor-signed attestation. Uses RuntimeSignatureCanonicalBytes
// (excludes both signature and attestation_hash) for verification, matching
// the signing path.
func VerifyAttestationWithAnanke(
	row store.RepairAttestationRow,
	verifier *repairverifier.RepairVerifier,
	publicKey ed25519.PublicKey,
) error {
	if verifier == nil {
		return fmt.Errorf("%w: nil verifier", ErrAttestationProduction)
	}

	// Parse the attestation from stored canonical JSON.
	record, err := repaircontract.DecodeRepairReviewAttestation([]byte(row.AttestationJSON))
	if err != nil {
		return fmt.Errorf("%w: decode: %v", ErrAttestationProduction, err)
	}

	// Provision the verifier with the public key.
	if err := verifier.SetAttestorPublicKey(publicKey); err != nil {
		return fmt.Errorf("%w: provision: %v", ErrAttestationProduction, err)
	}

	// Compute the signed bytes using RuntimeSignatureCanonicalBytes (excludes
	// both signature and attestation_hash, matching the signing path).
	signedBytes, err := repaircontract.RuntimeSignatureCanonicalBytes(record)
	if err != nil {
		return fmt.Errorf("%w: signed bytes: %v", ErrAttestationProduction, err)
	}

	// Parse the Ed25519 signature.
	sigHex := strings.TrimPrefix(record.Signature, "ed25519:")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("%w: signature decode: %v", ErrAttestationProduction, err)
	}

	// Verify the signature.
	if !ed25519.Verify(publicKey, signedBytes, sigBytes) {
		return fmt.Errorf("%w: Ed25519 verification failed", ErrAttestationProduction)
	}

	return nil
}

// --- helpers ---

func generateAttestationID(attemptHash string, now time.Time) string {
	// closedIdentifierPattern = ^[a-z0-9]+(?:_[a-z0-9]+)*$ — lowercase only.
	ts := now.UTC().Format("20060102_150405")
	suffix := ""
	if len(attemptHash) >= 8 {
		suffix = attemptHash[len(attemptHash)-8:]
	}
	return "attestation_" + ts + "_" + suffix
}

// (computeAttestationHash and runtimeSignatureCanonicalBytes are now
// provided by the contract layer as HashAttestationRecord and
// RuntimeSignatureCanonicalBytes respectively.)
