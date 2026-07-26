package repaircontract

import (
	"bytes"
	"reflect"
	"time"
)

// GUIApprovalEventAuthority is the exact independently authenticated GUI event
// selected by a trusted caller. No decoder in this package accepts this type
// from authorization, fixture, request, or dispatch bytes.
type GUIApprovalEventAuthority struct {
	SchemaVersion            string
	ApprovalHash             string
	ApprovalID               string
	Decision                 string
	OperatorIdentity         string
	OperatorIdentityHash     string
	AuthenticationProvenance string
	GUIProvenanceHash        string
	ApprovedScopeHash        string
	ApprovedAt               string
	NotAfter                 string
}

// AuthorityContext is the closed dynamic authority boundary for one repair
// authorization. A caller must populate it from independently verified durable
// P4, repository, installation, route, peer, policy, and GUI records.
type AuthorityContext struct {
	RepairLineageHash              string
	P4                             P4Binding
	Repository                     RepositoryBinding
	WritablePaths                  []WritablePathBinding
	TestProfiles                   []TestProfileBinding
	Route                          RouteBinding
	ChannelBindingHash             string
	ExpectedPeer                   UnixPeerIdentity
	InstalledPolicyHash            string
	InstalledSupervisorProfileHash string
	RotationMode                   string
	AttemptNumber                  int
	AttemptCap                     int
	GUIApprovalEvent               GUIApprovalEventAuthority
}

// VerifiedAuthorization is an opaque capability. Its state is deliberately
// private; only VerifyAuthorization can produce a usable value.
type VerifiedAuthorization struct {
	valid         bool
	authorization Authorization
	authority     AuthorityContext
	canonical     []byte
}

// VerifyAuthorization validates current against independently supplied dynamic
// authority and, for attempt 2, an actual verified attempt-1 capability.
func VerifyAuthorization(expected AuthorityContext, current Authorization, predecessor *VerifiedAuthorization, now time.Time, moment ValidationMoment) (*VerifiedAuthorization, error) {
	if moment != ValidationAdmission && moment != ValidationEffect {
		return nil, ErrInvalidContract
	}
	now = now.UTC()
	if err := VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now); err != nil {
		return nil, ErrInvalidContract
	}
	if err := validateAuthorityContext(expected); err != nil {
		return nil, ErrInvalidContract
	}
	if err := validateAuthorizationRecord(current, now); err != nil {
		return nil, ErrInvalidContract
	}
	if !authorizationMatchesAuthority(expected, current) {
		return nil, ErrInvalidContract
	}
	if err := validateAuthorizationPredecessor(current, predecessor); err != nil {
		return nil, ErrInvalidContract
	}
	canonical, err := canonicalBytes(current)
	if err != nil {
		return nil, ErrInvalidContract
	}
	return &VerifiedAuthorization{
		valid:         true,
		authorization: cloneAuthorization(current),
		authority:     cloneAuthorityContext(expected),
		canonical:     append([]byte(nil), canonical...),
	}, nil
}

func validateAuthorityContext(expected AuthorityContext) error {
	pins := FrozenReleasePins()
	if !validHash(expected.RepairLineageHash) || expected.P4.SchemaVersion != P4BindingSchemaVersion ||
		!validHash(expected.P4.P4ProposalHash) || expected.P4.AttemptNumber != expected.AttemptNumber ||
		expected.P4.AttemptCap != expected.AttemptCap || expected.AttemptNumber < 1 || expected.AttemptNumber > AttemptCap ||
		expected.AttemptCap != AttemptCap || expected.Repository.SchemaVersion != RepositoryBindingSchemaVersion ||
		!gitObjectPattern.MatchString(expected.Repository.BaseCommit) || !gitObjectPattern.MatchString(expected.Repository.BaseTree) ||
		expected.Route.SchemaVersion != RouteBindingSchemaVersion || expected.ExpectedPeer.SchemaVersion != UnixPeerIdentitySchemaVersion ||
		!validFullFence(expected.P4.FullFence) || !validRepositoryIdentity(expected.Repository.RepositoryIdentity) ||
		!validClosedIdentifier(expected.Route.RouteID, maxRouteIDBytes) ||
		!validClosedIdentifier(expected.Route.SupervisorProfileID, maxSupervisorProfileIDBytes) ||
		!validClosedIdentifier(expected.ExpectedPeer.PeerRole, maxPeerRoleBytes) ||
		expected.InstalledPolicyHash != pins.SupervisorPolicyHash ||
		expected.InstalledSupervisorProfileHash != pins.SupervisorProfileHash ||
		expected.Route.SupervisorProfileHash != expected.InstalledSupervisorProfileHash ||
		expected.RotationMode != "forbidden_v1" || expected.GUIApprovalEvent.SchemaVersion != OperatorApprovalSchemaVersion ||
		expected.GUIApprovalEvent.ApprovalID == "" || expected.GUIApprovalEvent.Decision != "approved" ||
		expected.GUIApprovalEvent.OperatorIdentity == "" || expected.GUIApprovalEvent.AuthenticationProvenance == "" {
		return ErrInvalidContract
	}
	if err := validateAllHashStrings(reflect.ValueOf(expected.P4)); err != nil {
		return ErrInvalidContract
	}
	if err := validateAllHashStrings(reflect.ValueOf(expected.Repository)); err != nil {
		return ErrInvalidContract
	}
	if !recordHashMatches(expected.P4.FullFence, "fence_hash", expected.P4.FullFence.FenceHash) ||
		!recordHashMatches(expected.P4, "p4_fact_hash", expected.P4.P4FactHash) ||
		!recordHashMatches(expected.Repository, "repository_binding_hash", expected.Repository.RepositoryBindingHash) ||
		!recordHashMatches(expected.Route, "route_binding_hash", expected.Route.RouteBindingHash) ||
		!recordHashMatches(expected.ExpectedPeer, "peer_identity_hash", expected.ExpectedPeer.PeerIdentityHash) {
		return ErrInvalidContract
	}
	if len(expected.WritablePaths) == 0 || len(expected.TestProfiles) == 0 ||
		hasDuplicatePaths(expected.WritablePaths) || hasDuplicateProfiles(expected.TestProfiles) {
		return ErrInvalidContract
	}
	for index, path := range expected.WritablePaths {
		if path.SchemaVersion != WritablePathBindingSchemaVersion || path.Sequence != index+1 || path.PathID == "" ||
			!recordHashMatches(path, "path_binding_hash", path.PathBindingHash) {
			return ErrInvalidContract
		}
	}
	for index, profile := range expected.TestProfiles {
		if profile.SchemaVersion != TestProfileBindingSchemaVersion || profile.Sequence != index+1 || profile.ProfileID == "" ||
			!recordHashMatches(profile, "profile_binding_hash", profile.ProfileBindingHash) {
			return ErrInvalidContract
		}
	}
	approval := approvalFromAuthority(expected.GUIApprovalEvent)
	if !recordHashMatches(approval, "approval_hash", approval.ApprovalHash) {
		return ErrInvalidContract
	}
	return nil
}

func validateAuthorizationRecord(value Authorization, now time.Time) error {
	if value.SchemaVersion != AuthorizationSchemaVersion || value.Scope.SchemaVersion != AuthorizationScopeSchemaVersion ||
		value.Approval.SchemaVersion != OperatorApprovalSchemaVersion || value.ApprovalHash != value.Approval.ApprovalHash ||
		value.PolicyHash != value.Scope.PolicyHash || value.Approval.ApprovedScopeHash != value.Scope.ScopeHash ||
		value.Scope.Attempt.SchemaVersion != AttemptBindingSchemaVersion || value.Scope.Attempt.AttemptCap != AttemptCap ||
		value.Scope.Attempt.AttemptNumber < 1 || value.Scope.Attempt.AttemptNumber > AttemptCap ||
		value.Scope.P4.AttemptNumber != value.Scope.Attempt.AttemptNumber || value.Scope.P4.AttemptCap != AttemptCap ||
		value.Scope.RotationMode != "forbidden_v1" || !validFullFence(value.Scope.P4.FullFence) ||
		!validRepositoryIdentity(value.Scope.Repository.RepositoryIdentity) ||
		!validClosedIdentifier(value.Scope.Route.RouteID, maxRouteIDBytes) ||
		!validClosedIdentifier(value.Scope.Route.SupervisorProfileID, maxSupervisorProfileIDBytes) ||
		!validClosedIdentifier(value.Scope.ExpectedPeer.PeerRole, maxPeerRoleBytes) {
		return ErrInvalidContract

	}
	if err := validateAllHashStrings(reflect.ValueOf(value)); err != nil {
		return ErrInvalidContract
	}
	if !recordHashMatches(value, "authorization_hash", value.AuthorizationHash) ||
		!recordHashMatches(value.Scope, "scope_hash", value.Scope.ScopeHash) ||
		!recordHashMatches(value.Approval, "approval_hash", value.Approval.ApprovalHash) ||
		!recordHashMatches(value.Scope.P4.FullFence, "fence_hash", value.Scope.P4.FullFence.FenceHash) ||
		!recordHashMatches(value.Scope.P4, "p4_fact_hash", value.Scope.P4.P4FactHash) ||
		!recordHashMatches(value.Scope.Repository, "repository_binding_hash", value.Scope.Repository.RepositoryBindingHash) ||
		!recordHashMatches(value.Scope.Route, "route_binding_hash", value.Scope.Route.RouteBindingHash) ||
		!recordHashMatches(value.Scope.ExpectedPeer, "peer_identity_hash", value.Scope.ExpectedPeer.PeerIdentityHash) {
		return ErrInvalidContract
	}
	if !gitObjectPattern.MatchString(value.Scope.Repository.BaseCommit) || !gitObjectPattern.MatchString(value.Scope.Repository.BaseTree) ||
		len(value.Scope.WritablePaths) == 0 || len(value.Scope.TestProfiles) == 0 ||
		hasDuplicatePaths(value.Scope.WritablePaths) || hasDuplicateProfiles(value.Scope.TestProfiles) {
		return ErrInvalidContract
	}
	for index, path := range value.Scope.WritablePaths {
		if path.SchemaVersion != WritablePathBindingSchemaVersion || path.Sequence != index+1 || path.PathID == "" ||
			!recordHashMatches(path, "path_binding_hash", path.PathBindingHash) {
			return ErrInvalidContract
		}
	}
	for index, profile := range value.Scope.TestProfiles {
		if profile.SchemaVersion != TestProfileBindingSchemaVersion || profile.Sequence != index+1 || profile.ProfileID == "" ||
			!recordHashMatches(profile, "profile_binding_hash", profile.ProfileBindingHash) {
			return ErrInvalidContract
		}
	}
	approvedAt, err := parseUTC(value.Approval.ApprovedAt)
	if err != nil {
		return ErrInvalidContract
	}
	notAfter, err := parseUTC(value.Approval.NotAfter)
	if err != nil || !notAfter.After(approvedAt) || notAfter.Sub(approvedAt) > MaxAuthorizationLifetime ||
		now.Before(approvedAt) || !now.Before(notAfter) || now.Sub(approvedAt) > MaxApprovalAge {
		return ErrInvalidContract
	}
	return nil
}

func authorizationMatchesAuthority(expected AuthorityContext, current Authorization) bool {
	scope := current.Scope
	return scope.RepairLineageHash == expected.RepairLineageHash && scope.P4 == expected.P4 &&
		scope.Repository == expected.Repository && reflect.DeepEqual(scope.WritablePaths, expected.WritablePaths) &&
		reflect.DeepEqual(scope.TestProfiles, expected.TestProfiles) && scope.Route == expected.Route &&
		scope.ChannelBindingHash == expected.ChannelBindingHash && scope.ExpectedPeer == expected.ExpectedPeer &&
		scope.PolicyHash == expected.InstalledPolicyHash && scope.Route.SupervisorProfileHash == expected.InstalledSupervisorProfileHash &&
		scope.RotationMode == expected.RotationMode && scope.Attempt.AttemptNumber == expected.AttemptNumber &&
		scope.Attempt.AttemptCap == expected.AttemptCap && current.Approval == approvalFromAuthority(expected.GUIApprovalEvent)
}

func validateAuthorizationPredecessor(current Authorization, predecessor *VerifiedAuthorization) error {
	attempt := current.Scope.Attempt
	if attempt.AttemptNumber == 1 {
		if predecessor != nil || attempt.PreviousAuthorizationHash != "" {
			return ErrInvalidContract
		}
		return nil
	}
	if predecessor == nil || !verifiedAuthorizationIntact(predecessor) || predecessor.authorization.Scope.Attempt.AttemptNumber != 1 ||
		predecessor.authorization.Scope.Attempt.AttemptCap != AttemptCap || attempt.PreviousAuthorizationHash != predecessor.authorization.AuthorizationHash ||
		!sameRepairAuthority(predecessor.authorization.Scope, current.Scope) {
		return ErrInvalidContract
	}
	previousApproval := predecessor.authorization.Approval
	currentApproval := current.Approval
	previousApprovedAt, err := parseUTC(previousApproval.ApprovedAt)
	if err != nil {
		return ErrInvalidContract
	}
	currentApprovedAt, err := parseUTC(currentApproval.ApprovedAt)
	if err != nil || !currentApprovedAt.After(previousApprovedAt) ||
		currentApproval.ApprovalID == previousApproval.ApprovalID ||
		currentApproval.GUIProvenanceHash == previousApproval.GUIProvenanceHash ||
		currentApproval.ApprovalHash == previousApproval.ApprovalHash {
		return ErrInvalidContract
	}
	currentBytes, err := canonicalBytes(current)
	if err != nil || bytes.Equal(currentBytes, predecessor.canonical) {
		return ErrInvalidContract
	}
	return nil
}

func sameRepairAuthority(previous, current AuthorizationScope) bool {
	return previous.RepairLineageHash == current.RepairLineageHash &&
		previous.P4.P4ProposalHash == current.P4.P4ProposalHash &&
		previous.P4.P4InputHash == current.P4.P4InputHash &&
		previous.P4.P4EvidenceBundleHash == current.P4.P4EvidenceBundleHash &&
		previous.P4.P4AdmissionHash == current.P4.P4AdmissionHash &&
		previous.P4.FullFence == current.P4.FullFence && previous.P4.AttemptCap == current.P4.AttemptCap &&
		previous.Repository == current.Repository && reflect.DeepEqual(previous.WritablePaths, current.WritablePaths) &&
		reflect.DeepEqual(previous.TestProfiles, current.TestProfiles) && previous.Route == current.Route &&
		previous.ChannelBindingHash == current.ChannelBindingHash && previous.ExpectedPeer == current.ExpectedPeer &&
		previous.PolicyHash == current.PolicyHash && previous.RotationMode == current.RotationMode
}

func verifiedAuthorizationIntact(value *VerifiedAuthorization) bool {
	if value == nil || !value.valid {
		return false
	}
	canonical, err := canonicalBytes(value.authorization)
	return err == nil && bytes.Equal(canonical, value.canonical) &&
		recordHashMatches(value.authorization, "authorization_hash", value.authorization.AuthorizationHash)
}

func approvalFromAuthority(value GUIApprovalEventAuthority) OperatorApproval {
	return OperatorApproval{
		SchemaVersion:            value.SchemaVersion,
		ApprovalHash:             value.ApprovalHash,
		ApprovalID:               value.ApprovalID,
		Decision:                 value.Decision,
		OperatorIdentity:         value.OperatorIdentity,
		OperatorIdentityHash:     value.OperatorIdentityHash,
		AuthenticationProvenance: value.AuthenticationProvenance,
		GUIProvenanceHash:        value.GUIProvenanceHash,
		ApprovedScopeHash:        value.ApprovedScopeHash,
		ApprovedAt:               value.ApprovedAt,
		NotAfter:                 value.NotAfter,
	}
}

func authorityFromAuthorization(value Authorization) AuthorityContext {
	return AuthorityContext{
		RepairLineageHash:              value.Scope.RepairLineageHash,
		P4:                             value.Scope.P4,
		Repository:                     value.Scope.Repository,
		WritablePaths:                  append([]WritablePathBinding(nil), value.Scope.WritablePaths...),
		TestProfiles:                   append([]TestProfileBinding(nil), value.Scope.TestProfiles...),
		Route:                          value.Scope.Route,
		ChannelBindingHash:             value.Scope.ChannelBindingHash,
		ExpectedPeer:                   value.Scope.ExpectedPeer,
		InstalledPolicyHash:            value.Scope.PolicyHash,
		InstalledSupervisorProfileHash: value.Scope.Route.SupervisorProfileHash,
		RotationMode:                   value.Scope.RotationMode,
		AttemptNumber:                  value.Scope.Attempt.AttemptNumber,
		AttemptCap:                     value.Scope.Attempt.AttemptCap,
		GUIApprovalEvent: GUIApprovalEventAuthority{
			SchemaVersion: value.Approval.SchemaVersion, ApprovalHash: value.Approval.ApprovalHash,
			ApprovalID: value.Approval.ApprovalID, Decision: value.Approval.Decision,
			OperatorIdentity: value.Approval.OperatorIdentity, OperatorIdentityHash: value.Approval.OperatorIdentityHash,
			AuthenticationProvenance: value.Approval.AuthenticationProvenance, GUIProvenanceHash: value.Approval.GUIProvenanceHash,
			ApprovedScopeHash: value.Approval.ApprovedScopeHash, ApprovedAt: value.Approval.ApprovedAt, NotAfter: value.Approval.NotAfter,
		},
	}
}

func cloneAuthorization(value Authorization) Authorization {
	value.Scope.WritablePaths = append([]WritablePathBinding(nil), value.Scope.WritablePaths...)
	value.Scope.TestProfiles = append([]TestProfileBinding(nil), value.Scope.TestProfiles...)
	return value
}

func cloneAuthorityContext(value AuthorityContext) AuthorityContext {
	value.WritablePaths = append([]WritablePathBinding(nil), value.WritablePaths...)
	value.TestProfiles = append([]TestProfileBinding(nil), value.TestProfiles...)
	return value
}
