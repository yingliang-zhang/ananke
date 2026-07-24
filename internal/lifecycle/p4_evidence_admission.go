package lifecycle

import (
	"context"
	"errors"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const p4EvidenceAdmissionPublicOutputSchemaVersion = "ananke.self-development-evidence-verifier-public-output.v1"

var errP4EvidenceAdmissionRuntimeDenied = errors.New("P4 evidence admission runtime denied")

// p4EvidenceAdmissionPublicOutput is the only runtime projection. A verified
// immutable fact is still waiting_for_human and never authorizes local repair.
type p4EvidenceAdmissionPublicOutput struct {
	Admission         string  `json:"admission"`
	BundleHash        *string `json:"bundle_hash"`
	RepairExecution   string  `json:"repair_execution"`
	SchemaVersion     string  `json:"schema_version"`
	State             string  `json:"state"`
	VerificationState string  `json:"verification_state"`
}

func p4EvidenceAdmissionFailureOutput() p4EvidenceAdmissionPublicOutput {
	return p4EvidenceAdmissionPublicOutput{
		Admission:         "rejected",
		BundleHash:        nil,
		RepairExecution:   "not_authorized",
		SchemaVersion:     p4EvidenceAdmissionPublicOutputSchemaVersion,
		State:             "waiting_for_human",
		VerificationState: "not_run",
	}
}

func p4EvidenceAdmissionVerifiedOutput(verified store.P4VerifierOutput) p4EvidenceAdmissionPublicOutput {
	bundleHash := verified.BundleHash
	return p4EvidenceAdmissionPublicOutput{
		Admission:         verified.Admission,
		BundleHash:        &bundleHash,
		RepairExecution:   verified.RepairExecution,
		SchemaVersion:     p4EvidenceAdmissionPublicOutputSchemaVersion,
		State:             "waiting_for_human",
		VerificationState: verified.VerificationState,
	}
}

// p4EvidenceAdmissionRuntime owns no repair, process, source, artifact, OMP,
// network, or verifier implementation. Its verifier is an injected test seam.
type p4EvidenceAdmissionRuntime struct {
	journal  *store.Store
	verifier store.P4Verifier
}

func newP4EvidenceAdmissionRuntime(journal *store.Store, verifier store.P4Verifier) (*p4EvidenceAdmissionRuntime, error) {
	if journal == nil || verifier == nil {
		return nil, errP4EvidenceAdmissionRuntimeDenied
	}
	return &p4EvidenceAdmissionRuntime{journal: journal, verifier: verifier}, nil
}

// submit durably records only the exact immutable P4 evidence fact and a
// verifier response from the injected test fake. Every outcome remains the
// closed waiting_for_human projection; it never creates a local repair or run.
func (runtime *p4EvidenceAdmissionRuntime) submit(ctx context.Context, fact store.P4EvidenceAdmission) p4EvidenceAdmissionPublicOutput {
	if runtime == nil || ctx == nil {
		return p4EvidenceAdmissionFailureOutput()
	}
	persisted, err := runtime.journal.VerifyAndPersistP4EvidenceAdmission(ctx, fact, runtime.verifier)
	if err != nil {
		return p4EvidenceAdmissionFailureOutput()
	}
	return p4EvidenceAdmissionVerifiedOutput(persisted.VerifierOutput)
}
