package repaircontract

import (
	"bytes"
	"reflect"
	"time"
)

// DecodeDispatch accepts only canonical immutable-dispatch bytes and verifies
// their complete semantic binding to external authority and a verified
// authorization capability.
func DecodeDispatch(expected AuthorityContext, authorization *VerifiedAuthorization, raw []byte, now time.Time, moment ValidationMoment) (ImmutableDispatch, error) {
	value, err := decodeCanonicalRecord[ImmutableDispatch](raw)
	if err != nil {
		return ImmutableDispatch{}, ErrInvalidContract
	}
	if err := validateDispatchRecord(expected, authorization, value, now.UTC(), moment, true); err != nil {
		return ImmutableDispatch{}, ErrInvalidContract
	}
	return value, nil
}

// ClassifyDispatchReplay compares stored and incoming canonical bytes. Exact
// replay remains identifiable after freshness expires, but both byte strings
// must still be canonical and semantically valid against the supplied authority
// and verified authorization.
func ClassifyDispatchReplay(expected AuthorityContext, authorization *VerifiedAuthorization, existingCanonical, incomingCanonical []byte) (DispatchReplayDisposition, error) {
	existing, err := decodeCanonicalRecord[ImmutableDispatch](existingCanonical)
	if err != nil || validateDispatchRecord(expected, authorization, existing, time.Time{}, ValidationAdmission, false) != nil {
		return DispatchConflict, ErrInvalidContract
	}
	incoming, err := decodeCanonicalRecord[ImmutableDispatch](incomingCanonical)
	if err != nil || validateDispatchRecord(expected, authorization, incoming, time.Time{}, ValidationAdmission, false) != nil {
		return DispatchConflict, ErrDispatchConflict
	}
	if bytes.Equal(existingCanonical, incomingCanonical) {
		return DispatchExactReplay, nil
	}
	return DispatchConflict, ErrDispatchConflict
}

func validateDispatchRecord(expected AuthorityContext, verified *VerifiedAuthorization, value ImmutableDispatch, now time.Time, moment ValidationMoment, checkFreshness bool) error {
	if !verifiedAuthorizationIntact(verified) || !reflect.DeepEqual(expected, verified.authority) ||
		!authorizationMatchesAuthority(expected, verified.authorization) ||
		value.SchemaVersion != ImmutableDispatchSchemaVersion || value.Request.SchemaVersion != DispatchRequestSchemaVersion {
		return ErrInvalidContract
	}
	authorization := verified.authorization
	scope := authorization.Scope
	pins := FrozenReleasePins()
	if value.AuthorizationHash != authorization.AuthorizationHash || value.ApprovalHash != authorization.ApprovalHash ||
		value.PolicyHash != authorization.PolicyHash || value.AttemptNumber != scope.Attempt.AttemptNumber ||
		value.AttemptCap != scope.Attempt.AttemptCap || value.Request.AuthorizationHash != value.AuthorizationHash ||
		value.Request.AttemptNumber != value.AttemptNumber || value.Request.AttemptCap != value.AttemptCap ||
		value.Request.RequestID == "" || value.ChannelBindingHash != expected.ChannelBindingHash ||
		value.ExpectedPeer != expected.ExpectedPeer || value.ReleasePinsHash != pins.ReleasePinsHash ||
		value.SelectedSupervisorPolicyHash != expected.InstalledPolicyHash ||
		value.SelectedSupervisorProfileID != expected.Route.SupervisorProfileID ||
		value.SelectedSupervisorProfileHash != expected.InstalledSupervisorProfileHash {
		return ErrInvalidContract
	}
	if err := validateAllHashStrings(reflect.ValueOf(value)); err != nil ||
		!recordHashMatches(value.Request, "request_hash", value.Request.RequestHash) ||
		!recordHashMatches(value, "dispatch_hash", value.DispatchHash) {
		return ErrInvalidContract
	}
	createdAt, err := parseUTC(value.CreatedAt)
	if err != nil {
		return ErrInvalidContract
	}
	dispatchNotAfter, err := parseUTC(value.DispatchNotAfter)
	if err != nil || !dispatchNotAfter.After(createdAt) || dispatchNotAfter.Sub(createdAt) > MaxDispatchLifetime {
		return ErrInvalidContract
	}
	approvedAt, err := parseUTC(authorization.Approval.ApprovedAt)
	if err != nil {
		return ErrInvalidContract
	}
	authorizationNotAfter, err := parseUTC(authorization.Approval.NotAfter)
	if err != nil || createdAt.Before(approvedAt) || !createdAt.Before(authorizationNotAfter) || dispatchNotAfter.After(authorizationNotAfter) {
		return ErrInvalidContract
	}
	if !checkFreshness {
		return nil
	}
	if VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now) != nil ||
		validateAuthorizationRecord(authorization, now) != nil || moment != ValidationAdmission && moment != ValidationEffect ||
		now.Before(createdAt) || !now.Before(dispatchNotAfter) {
		return ErrInvalidContract
	}
	if moment == ValidationAdmission && !now.Equal(createdAt) {
		return ErrInvalidContract
	}
	return nil
}
