package repairrunner

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/repairverifier"
	"github.com/yingliang-zhang/ananke/internal/store"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
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
	signedBytes, err := runtimeSignatureCanonicalBytes(record)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: canonical bytes: %v", ErrAttestationProduction, err)
	}
	signature, err := material.SignAttestationBytes(signedBytes)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: sign: %v", ErrAttestationProduction, err)
	}
	record.Signature = signature

	// Compute attestation hash (covers record including signature, excluding
	// attestation_hash). Set attestation_hash to empty first.
	record.AttestationHash = ""
	attestationHash, err := transportprimitives.CanonicalHash(record)
	if err != nil {
		return store.RepairAttestationRow{}, fmt.Errorf("%w: hash: %v", ErrAttestationProduction, err)
	}
	record.AttestationHash = attestationHash

	// Verify our own signature before persisting. The verification uses the
	// same runtimeSignatureCanonicalBytes function that excludes both fields.
	if err := material.VerifyAttestationBytes(signedBytes, signature); err != nil {
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
// to verify a supervisor-signed attestation.
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

	// Verify the signature.
	if err := verifier.VerifyAttestationSignature(record, verifier.ExpectedAttestorSPKI()); err != nil {
		return fmt.Errorf("%w: verify: %v", ErrAttestationProduction, err)
	}

	return nil
}

// --- helpers ---

func generateAttestationID(attemptHash string, now time.Time) string {
	return "attestation_" + now.UTC().Format("20060102T150405Z") + "_" + attemptHash[len(attemptHash)-8:]
}

func computeAttestationHash(record repaircontract.RepairReviewAttestation) (string, error) {
	// The attestation hash is a self-hash: the canonical JSON of the entire
	// record with the attestation_hash field set to empty, then hashed.
	// This mirrors the contract layer's hashRecord approach.
	record.AttestationHash = ""
	return transportprimitives.CanonicalHash(record)
}

// runtimeSignatureCanonicalBytes computes the canonical bytes that the
// Ed25519 signature covers in the runtime. Unlike the contract layer's
// attestationSignatureCanonicalBytes (which only excludes "signature"),
// this function excludes BOTH "signature" and "attestation_hash" fields.
// This breaks the circular dependency between the attestation hash (which
// covers the signature) and the signature (which would otherwise cover the
// attestation hash). Domain separation is prepended.
func runtimeSignatureCanonicalBytes(record repaircontract.RepairReviewAttestation) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "signature")
	delete(m, "attestation_hash")
	canonical, err := transportprimitives.MarshalCanonical(m)
	if err != nil {
		return nil, err
	}
	domainPrefix := []byte(repaircontract.SignatureDomain + "\x00")
	return append(domainPrefix, canonical...), nil
}
