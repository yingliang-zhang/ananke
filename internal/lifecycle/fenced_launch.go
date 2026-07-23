package lifecycle

import (
	"context"
	"errors"

	"github.com/yingliang-zhang/ananke/internal/store"
)

var (
	errFencedLaunchBoundaryUnsafe = errors.New("fenced launch boundary cannot safely advance")
	errFencedLaunchOutcomeUnknown = errors.New("fenced launch terminal or evidence outcome is unknown")
)

// fencedLaunchOrchestrator advances only P3b's durable modeled facts. It does
// not materialize a worktree, create a real Run, or invoke any process-facing
// subsystem.
type fencedLaunchStore interface {
	StoreLaunchSpec(context.Context, store.LaunchAdmissionRequest) (store.StoredLaunchSpec, error)
	AcquireLaunchClaim(context.Context, store.LaunchClaimRequest) (store.TaskClaim, error)
	GetLaunchRecoveryBoundary(context.Context, string) (store.LaunchRecoveryBoundary, error)
	ListLaunchRecoveryBoundaries(context.Context) ([]store.LaunchRecoveryResult, error)
	RecordLaunchMaterializationReady(context.Context, store.LaunchMaterializationRequest) (store.LaunchMaterialization, error)
	CreateLaunchRunIntent(context.Context, store.LaunchRunIntentRequest) (store.LaunchRunIntent, error)
}

type fencedLaunchOrchestrator struct {
	store fencedLaunchStore
}

// fencedLaunchAction names the exact next durable obligation. WaitingForHuman
// is returned with the original store error whenever the persisted authority is
// missing or corrupt; no process, terminal, or evidence outcome is inferred.
type fencedLaunchAction struct {
	LaunchSpecHash  string
	Boundary        store.LaunchRecoveryBoundary
	WaitingForHuman bool
	Cause           error
}

func newFencedLaunchOrchestrator(journal fencedLaunchStore) *fencedLaunchOrchestrator {
	return &fencedLaunchOrchestrator{store: journal}
}

// admit records the immutable launch spec then its exact initial claim. A
// replay reconnects only when the durable active claim fully matches the
// supplied identity; a different owner/token/generation remains rejected.
func (o *fencedLaunchOrchestrator) admit(ctx context.Context, admission store.LaunchAdmissionRequest, request store.LaunchClaimRequest) (fencedLaunchAction, error) {
	if admission.LaunchSpecHash != request.LaunchSpecHash {
		return fencedLaunchAction{}, store.ErrLaunchSpecHashMismatch
	}
	stored, err := o.store.StoreLaunchSpec(ctx, admission)
	if err != nil {
		return fencedLaunchAction{}, err
	}

	claim, err := o.store.AcquireLaunchClaim(ctx, request)
	if err == nil {
		return o.recoverExpectedFence(ctx, stored.LaunchSpecHash, claim.Fence)
	}
	if !errors.Is(err, store.ErrLaunchClaimAlreadyActive) {
		return fencedLaunchAction{}, err
	}

	action, recoveryErr := o.recover(ctx, stored.LaunchSpecHash)
	if recoveryErr != nil {
		return action, recoveryErr
	}
	if !matchesLaunchClaimRequest(action.Boundary.Claim, request) {
		return fencedLaunchAction{}, err
	}
	return action, nil
}

// recordTrustedMaterializationReady accepts an externally verified opaque
// materialization identity only. Store validation binds it to the sealed
// launch spec and writes the next outbox stage atomically.
func (o *fencedLaunchOrchestrator) recordTrustedMaterializationReady(ctx context.Context, request store.LaunchMaterializationRequest) (fencedLaunchAction, error) {
	materialization, err := o.store.RecordLaunchMaterializationReady(ctx, request)
	if err != nil {
		return fencedLaunchAction{}, err
	}
	if materialization.LaunchFence != request.Fence || materialization.MaterializationID != request.MaterializationID || materialization.MaterializationHash != request.MaterializationHash || materialization.Nonce != request.Nonce {
		return fencedLaunchAction{LaunchSpecHash: materialization.LaunchSpecHash, WaitingForHuman: true, Cause: errFencedLaunchBoundaryUnsafe}, errFencedLaunchBoundaryUnsafe
	}
	return o.recoverExpectedFence(ctx, materialization.LaunchSpecHash, materialization.LaunchFence)
}

// admitRunIntent records only the modeled current-token created fact. It never
// calls CreateRun and returns the durable process-admission obligation instead
// of launching anything.
func (o *fencedLaunchOrchestrator) admitRunIntent(ctx context.Context, request store.LaunchRunIntentRequest) (fencedLaunchAction, error) {
	run, err := o.store.CreateLaunchRunIntent(ctx, request)
	if err != nil {
		return fencedLaunchAction{}, err
	}
	if run.LaunchFence != request.Fence || run.MaterializationID != request.MaterializationID || run.RunID != request.RunID || run.Attempt != request.Attempt || run.StateFact.Kind != store.LaunchStateFactCreated || run.StateFact.Sequence != 1 || run.StateFact.TokenHash != request.Fence.ClaimTokenHash {
		return fencedLaunchAction{LaunchSpecHash: run.LaunchSpecHash, WaitingForHuman: true, Cause: errFencedLaunchBoundaryUnsafe}, errFencedLaunchBoundaryUnsafe
	}
	return o.recoverExpectedFence(ctx, run.LaunchSpecHash, run.LaunchFence)
}

// recover reconnects one known launch spec from its durable active boundary.
// Expected absence at a valid outbox stage is a retry action. An unknown or
// corrupt boundary is returned as waiting_for_human with its original error.
func (o *fencedLaunchOrchestrator) recover(ctx context.Context, launchSpecHash string) (fencedLaunchAction, error) {
	boundary, err := o.store.GetLaunchRecoveryBoundary(ctx, launchSpecHash)
	if err != nil {
		if isFencedLaunchHumanIntervention(err) {
			return fencedLaunchAction{LaunchSpecHash: launchSpecHash, WaitingForHuman: true, Cause: err}, err
		}
		return fencedLaunchAction{}, err
	}
	return fencedLaunchActionFromBoundary(boundary)
}

// recoverAll reconnects every independently durable active claim in the
// store's stable order. It performs no retry side effect; callers receive only
// the safe next obligation for each claim.
func (o *fencedLaunchOrchestrator) recoverAll(ctx context.Context) ([]fencedLaunchAction, error) {
	results, err := o.store.ListLaunchRecoveryBoundaries(ctx)
	if err != nil {
		return nil, err
	}
	actions := make([]fencedLaunchAction, 0, len(results))
	for _, result := range results {
		if result.Cause != nil {
			if !isFencedLaunchHumanIntervention(result.Cause) {
				return nil, result.Cause
			}
			actions = append(actions, fencedLaunchAction{
				LaunchSpecHash:  result.LaunchSpecHash,
				WaitingForHuman: true,
				Cause:           result.Cause,
			})
			continue
		}
		if result.Boundary == nil {
			actions = append(actions, fencedLaunchAction{
				LaunchSpecHash:  result.LaunchSpecHash,
				WaitingForHuman: true,
				Cause:           errFencedLaunchBoundaryUnsafe,
			})
			continue
		}
		action, _ := fencedLaunchActionFromBoundary(*result.Boundary)
		actions = append(actions, action)
	}
	return actions, nil
}

func (o *fencedLaunchOrchestrator) recoverExpectedFence(ctx context.Context, launchSpecHash string, fence store.LaunchFence) (fencedLaunchAction, error) {
	action, err := o.recover(ctx, launchSpecHash)
	if err != nil {
		return action, err
	}
	if action.Boundary.Claim.LaunchFence != fence {
		return fencedLaunchAction{LaunchSpecHash: launchSpecHash}, store.ErrLaunchStaleFence
	}
	return action, nil
}

func fencedLaunchActionFromBoundary(boundary store.LaunchRecoveryBoundary) (fencedLaunchAction, error) {
	action := fencedLaunchAction{LaunchSpecHash: boundary.LaunchSpecHash, Boundary: boundary}
	if err := validateFencedLaunchBoundary(boundary); err != nil {
		action.WaitingForHuman = true
		action.Cause = err
		return action, err
	}
	return action, nil
}

func validateFencedLaunchBoundary(boundary store.LaunchRecoveryBoundary) error {
	claim := boundary.Claim
	if boundary.LaunchSpecHash == "" || claim.LaunchSpecHash != boundary.LaunchSpecHash || claim.State != store.TaskClaimStateActive || claim.Fence != claim.LaunchFence || boundary.Outbox.LaunchSpecHash != boundary.LaunchSpecHash || boundary.Outbox.LaunchFence != claim.LaunchFence {
		return errFencedLaunchBoundaryUnsafe
	}
	if boundary.TerminalIntent != nil || boundary.EvidenceIntent != nil {
		return errFencedLaunchOutcomeUnknown
	}

	switch boundary.Action {
	case store.LaunchRecoveryRetryMaterialization:
		if boundary.Materialization != nil || boundary.RunIntent != nil {
			return errFencedLaunchBoundaryUnsafe
		}
	case store.LaunchRecoveryRetryRunAdmission:
		if !validFencedLaunchMaterialization(boundary.Materialization, claim) || boundary.RunIntent != nil {
			return errFencedLaunchBoundaryUnsafe
		}
	case store.LaunchRecoveryRetryProcessAdmission:
		if !validFencedLaunchMaterialization(boundary.Materialization, claim) || !validFencedLaunchRunIntent(boundary.RunIntent, boundary.Materialization, claim) {
			return errFencedLaunchBoundaryUnsafe
		}
	default:
		return errFencedLaunchBoundaryUnsafe
	}
	return nil
}

func validFencedLaunchMaterialization(materialization *store.LaunchMaterialization, claim store.TaskClaim) bool {
	return materialization != nil && materialization.LaunchSpecHash == claim.LaunchSpecHash && materialization.LaunchFence == claim.LaunchFence && materialization.State == store.LaunchMaterializationStateReady
}

func validFencedLaunchRunIntent(run *store.LaunchRunIntent, materialization *store.LaunchMaterialization, claim store.TaskClaim) bool {
	return run != nil && materialization != nil && run.LaunchSpecHash == claim.LaunchSpecHash && run.LaunchFence == claim.LaunchFence && run.MaterializationID == materialization.MaterializationID && run.Attempt == claim.Attempt && run.StateFact.Kind == store.LaunchStateFactCreated && run.StateFact.Sequence == 1 && run.StateFact.TokenHash == claim.ClaimTokenHash
}

func matchesLaunchClaimRequest(claim store.TaskClaim, request store.LaunchClaimRequest) bool {
	return claim.LaunchSpecHash == request.LaunchSpecHash && claim.ClaimID == request.ClaimID && claim.ClaimTokenHash == request.ClaimTokenHash && claim.OwnerID == request.OwnerID && claim.Attempt == request.Attempt && claim.State == store.TaskClaimStateActive
}

func isFencedLaunchHumanIntervention(err error) bool {
	return errors.Is(err, store.ErrLaunchClaimNotFound) || errors.Is(err, store.ErrLaunchSpecNotFound) || errors.Is(err, store.ErrLaunchRecordCorrupt)
}
