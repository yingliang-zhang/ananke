package repaircontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestP6A2AuthorizationRejectsRehashedSemanticMutationsAgainstUnchangedAuthority(t *testing.T) {
	_, base := readFixture(t)
	expected := authorityFromAuthorization(base.Authorization)
	now := mustTime(t, base.Dispatch.CreatedAt)
	other := testHash("a2-external-authority-mutation")
	tests := []struct {
		name   string
		mutate func(*ContractFixture)
	}{
		{name: "repair lineage", mutate: func(f *ContractFixture) { f.Authorization.Scope.RepairLineageHash = other }},
		{name: "P4 proposal", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.P4ProposalHash = other }},
		{name: "P4 input", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.P4InputHash = other }},
		{name: "P4 evidence bundle", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.P4EvidenceBundleHash = other }},
		{name: "P4 admission", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.P4AdmissionHash = other }},
		{name: "fence claim", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.FullFence.ClaimID = "other_claim" }},
		{name: "fence token", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.FullFence.ClaimTokenHash = other }},
		{name: "fence generation", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.FullFence.FenceGeneration++ }},
		{name: "repository identity", mutate: func(f *ContractFixture) {
			f.Authorization.Scope.Repository.RepositoryIdentity = "example.invalid/other"
		}},
		{name: "repository identity hash", mutate: func(f *ContractFixture) { f.Authorization.Scope.Repository.RepositoryIdentityHash = other }},
		{name: "base commit", mutate: func(f *ContractFixture) { f.Authorization.Scope.Repository.BaseCommit = strings.Repeat("a", 40) }},
		{name: "base tree", mutate: func(f *ContractFixture) { f.Authorization.Scope.Repository.BaseTree = strings.Repeat("b", 40) }},
		{name: "writable path ID", mutate: func(f *ContractFixture) { f.Authorization.Scope.WritablePaths[0].PathID = "other_path_id" }},
		{name: "writable path identity", mutate: func(f *ContractFixture) { f.Authorization.Scope.WritablePaths[0].RepositoryRelativePathHash = other }},
		{name: "test profile ID", mutate: func(f *ContractFixture) { f.Authorization.Scope.TestProfiles[0].ProfileID = "other_profile" }},
		{name: "installed test profile", mutate: func(f *ContractFixture) { f.Authorization.Scope.TestProfiles[0].InstalledProfileHash = other }},
		{name: "route ID", mutate: func(f *ContractFixture) { f.Authorization.Scope.Route.RouteID = "other_route" }},
		{name: "route identity", mutate: func(f *ContractFixture) { f.Authorization.Scope.Route.RouteIdentityHash = other }},
		{name: "selected supervisor profile ID", mutate: func(f *ContractFixture) { f.Authorization.Scope.Route.SupervisorProfileID = "other_supervisor_profile" }},
		{name: "selected supervisor profile hash", mutate: func(f *ContractFixture) { f.Authorization.Scope.Route.SupervisorProfileHash = other }},
		{name: "channel", mutate: func(f *ContractFixture) { f.Authorization.Scope.ChannelBindingHash = other }},
		{name: "peer", mutate: func(f *ContractFixture) { f.Authorization.Scope.ExpectedPeer.UserID++ }},
		{name: "installed policy", mutate: func(f *ContractFixture) { f.Authorization.Scope.PolicyHash = other }},
		{name: "rotation mode", mutate: func(f *ContractFixture) { f.Authorization.Scope.RotationMode = "other_mode" }},
		{name: "GUI approval ID", mutate: func(f *ContractFixture) { f.Authorization.Approval.ApprovalID = "other_gui_event" }},
		{name: "GUI operator", mutate: func(f *ContractFixture) { f.Authorization.Approval.OperatorIdentity = "other_operator" }},
		{name: "GUI operator identity hash", mutate: func(f *ContractFixture) { f.Authorization.Approval.OperatorIdentityHash = other }},
		{name: "GUI authentication provenance", mutate: func(f *ContractFixture) {
			f.Authorization.Approval.AuthenticationProvenance = "other_gui_authentication"
		}},
		{name: "GUI provenance event hash", mutate: func(f *ContractFixture) { f.Authorization.Approval.GUIProvenanceHash = other }},
		{name: "GUI approved at", mutate: func(f *ContractFixture) { f.Authorization.Approval.ApprovedAt = "2026-07-26T12:01:00Z" }},
		{name: "GUI not after", mutate: func(f *ContractFixture) { f.Authorization.Approval.NotAfter = "2026-07-26T12:09:00Z" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			test.mutate(&fixture)
			rehashAuthorizationChain(t, &fixture)
			if _, err := VerifyAuthorization(expected, fixture.Authorization, nil, now, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("rehashed semantic mutation accepted against unchanged authority: %v", err)
			}
		})
	}
}

func TestP6A2CanonicalAttemptTwoUsesVerifiedFreshPredecessor(t *testing.T) {
	first := CanonicalFixture()
	firstAuthority := authorityFromAuthorization(first.Authorization)
	verifiedFirst, err := VerifyAuthorization(firstAuthority, first.Authorization, nil, mustTime(t, first.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatalf("verify canonical attempt 1: %v", err)
	}
	second := CanonicalAttemptTwoFixture()
	secondAuthority := authorityFromAuthorization(second.Authorization)
	verifiedSecond, err := VerifyAuthorization(secondAuthority, second.Authorization, verifiedFirst, mustTime(t, second.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatalf("verify canonical attempt 2: %v", err)
	}
	if verifiedSecond == nil || second.Authorization.Scope.Attempt.PreviousAuthorizationHash != first.Authorization.AuthorizationHash ||
		second.Authorization.Approval.ApprovalID == first.Authorization.Approval.ApprovalID ||
		second.Authorization.Approval.GUIProvenanceHash == first.Authorization.Approval.GUIProvenanceHash ||
		second.Authorization.Approval.ApprovedAt <= first.Authorization.Approval.ApprovedAt ||
		second.Authorization.Approval.ApprovalHash == first.Authorization.Approval.ApprovalHash ||
		second.Authorization.AuthorizationHash == first.Authorization.AuthorizationHash {
		t.Fatal("canonical attempt 2 did not bind a fresh GUI event and exact predecessor")
	}
	raw := canonicalTestArtifact(t, second.Dispatch)
	if _, err := DecodeDispatch(secondAuthority, verifiedSecond, raw, mustTime(t, second.Dispatch.CreatedAt), ValidationAdmission); err != nil {
		t.Fatalf("verify canonical attempt-2 dispatch: %v", err)
	}
}

func TestP6A2RetainedAuthorizationEffectFreshness(t *testing.T) {
	fixture := CanonicalFixture()
	expected := authorityFromAuthorization(fixture.Authorization)
	admission := mustTime(t, fixture.Dispatch.CreatedAt)
	verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, admission, ValidationAdmission)
	if err != nil {
		t.Fatalf("mint retained capability at %s: %v", admission.Format(time.RFC3339Nano), err)
	}
	raw := canonicalTestArtifact(t, fixture.Dispatch)
	approvedAt := mustTime(t, fixture.Authorization.Approval.ApprovedAt)

	for _, test := range []struct {
		name  string
		age   time.Duration
		valid bool
	}{
		{name: "approval age N-1", age: MaxApprovalAge - time.Nanosecond, valid: true},
		{name: "approval age N", age: MaxApprovalAge, valid: true},
		{name: "approval age N+1", age: MaxApprovalAge + time.Nanosecond, valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeDispatch(expected, verified, raw, approvedAt.Add(test.age), ValidationEffect)
			if test.valid && err != nil {
				t.Fatalf("retained capability rejected at approval age %s: %v", test.age, err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("retained capability accepted at approval age %s", test.age)
			}
		})
	}

	t.Run("authorization expiry", func(t *testing.T) {
		now := mustTime(t, fixture.Authorization.Approval.NotAfter)
		if _, err := DecodeDispatch(expected, verified, raw, now, ValidationEffect); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("retained capability accepted at authorization expiry %s", now.Format(time.RFC3339Nano))
		}
	})
	t.Run("dispatch expiry", func(t *testing.T) {
		now := mustTime(t, fixture.Dispatch.DispatchNotAfter)
		if _, err := DecodeDispatch(expected, verified, raw, now, ValidationEffect); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("retained capability accepted at dispatch expiry %s", now.Format(time.RFC3339Nano))
		}
	})
	t.Run("same capability admitted earlier then reused at effect", func(t *testing.T) {
		if _, err := DecodeDispatch(expected, verified, raw, admission, ValidationAdmission); err != nil {
			t.Fatalf("admit retained capability: %v", err)
		}
		staleEffect := approvedAt.Add(MaxApprovalAge + time.Nanosecond)
		if _, err := DecodeDispatch(expected, verified, raw, staleEffect, ValidationEffect); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("same retained capability accepted at stale effect %s", staleEffect.Format(time.RFC3339Nano))
		}
	})
	t.Run("static replay cannot grant effect authority", func(t *testing.T) {
		disposition, err := ClassifyDispatchReplay(expected, verified, raw, append([]byte(nil), raw...))
		if err != nil || disposition != DispatchExactReplay {
			t.Fatalf("static exact replay classification = %q, %v", disposition, err)
		}
		staleEffect := approvedAt.Add(MaxApprovalAge + time.Nanosecond)
		if _, err := DecodeDispatch(expected, verified, raw, staleEffect, ValidationEffect); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("static replay classification authorized stale effect at %s", staleEffect.Format(time.RFC3339Nano))
		}
	})
}

func TestP6A2ReleaseTrustMandatoryAtAuthorizationAndEffect(t *testing.T) {
	fixtureAt := func(now time.Time) ContractFixture {
		t.Helper()
		fixture := CanonicalFixture()
		approvedAt := now.Add(-time.Minute)
		fixture.Authorization.Approval.ApprovedAt = approvedAt.Format(time.RFC3339Nano)
		fixture.Authorization.Approval.NotAfter = approvedAt.Add(MaxAuthorizationLifetime).Format(time.RFC3339Nano)
		fixture.Dispatch.CreatedAt = now.Format(time.RFC3339Nano)
		fixture.Dispatch.DispatchNotAfter = now.Add(MaxDispatchLifetime).Format(time.RFC3339Nano)
		rehashAuthorizationChain(t, &fixture)
		return fixture
	}
	capabilityFor := func(fixture ContractFixture, expected AuthorityContext) *VerifiedAuthorization {
		t.Helper()
		canonical, err := canonicalBytes(fixture.Authorization)
		if err != nil {
			t.Fatal(err)
		}
		return &VerifiedAuthorization{
			valid:         true,
			authorization: cloneAuthorization(fixture.Authorization),
			authority:     cloneAuthorityContext(expected),
			canonical:     append([]byte(nil), canonical...),
		}
	}

	leafNotAfter := compiledRelease.leaf.NotAfter.UTC()
	for _, test := range []struct {
		name  string
		now   time.Time
		valid bool
	}{
		{name: "certificate valid N-1", now: leafNotAfter.Add(-time.Nanosecond), valid: true},
		{name: "certificate valid N", now: leafNotAfter, valid: false},
		{name: "certificate valid N+1", now: leafNotAfter.Add(time.Nanosecond), valid: false},
	} {
		t.Run("authorization "+test.name, func(t *testing.T) {
			fixture := fixtureAt(test.now)
			_, err := VerifyAuthorization(authorityFromAuthorization(fixture.Authorization), fixture.Authorization, nil, test.now, ValidationAdmission)
			if test.valid && err != nil {
				t.Fatalf("valid compiled release rejected at %s: %v", test.now.Format(time.RFC3339Nano), err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("authorization accepted outside compiled release validity at %s", test.now.Format(time.RFC3339Nano))
			}
		})
	}

	t.Run("retained capability revalidates certificate at effect", func(t *testing.T) {
		createdAt := leafNotAfter.Add(-time.Minute)
		fixture := fixtureAt(createdAt)
		expected := authorityFromAuthorization(fixture.Authorization)
		verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, createdAt, ValidationAdmission)
		if err != nil {
			t.Fatalf("mint capability before certificate expiry: %v", err)
		}
		raw := canonicalTestArtifact(t, fixture.Dispatch)
		for _, test := range []struct {
			name  string
			now   time.Time
			valid bool
		}{
			{name: "N-1", now: leafNotAfter.Add(-time.Nanosecond), valid: true},
			{name: "N", now: leafNotAfter, valid: false},
			{name: "N+1", now: leafNotAfter.Add(time.Nanosecond), valid: false},
		} {
			t.Run(test.name, func(t *testing.T) {
				_, err := DecodeDispatch(expected, verified, raw, test.now, ValidationEffect)
				if test.valid && err != nil {
					t.Fatalf("valid effect rejected at %s: %v", test.now.Format(time.RFC3339Nano), err)
				}
				if !test.valid && !errors.Is(err, ErrInvalidContract) {
					t.Fatalf("effect accepted outside compiled release validity at %s", test.now.Format(time.RFC3339Nano))
				}
			})
		}
		disposition, err := ClassifyDispatchReplay(expected, verified, raw, append([]byte(nil), raw...))
		if err != nil || disposition != DispatchExactReplay {
			t.Fatalf("immutable replay classification after release expiry = %q, %v", disposition, err)
		}
		if _, err := DecodeDispatch(expected, verified, raw, leafNotAfter, ValidationEffect); !errors.Is(err, ErrInvalidContract) {
			t.Fatal("static release identity replay granted effect authority at certificate expiry")
		}
	})

	t.Run("fresh 2029 bypass attempt", func(t *testing.T) {
		now := time.Date(2029, 1, 1, 12, 4, 0, 0, time.UTC)
		fixture := fixtureAt(now)
		expected := authorityFromAuthorization(fixture.Authorization)
		if err := VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("expired embedded release unexpectedly verified in 2029: %v", err)
		}
		if _, err := VerifyAuthorization(expected, fixture.Authorization, nil, now, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatal("fresh 2029 authorization bypassed expired compiled release trust")
		}
		verified := capabilityFor(fixture, expected)
		if _, err := DecodeDispatch(expected, verified, canonicalTestArtifact(t, fixture.Dispatch), now, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatal("fresh 2029 dispatch bypassed expired compiled release trust")
		}
	})
}

func TestP6A2AttemptPredecessorRejections(t *testing.T) {
	first := CanonicalFixture()
	firstAuthority := authorityFromAuthorization(first.Authorization)
	verifiedFirst, err := VerifyAuthorization(firstAuthority, first.Authorization, nil, mustTime(t, first.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	second := CanonicalAttemptTwoFixture()
	secondNow := mustTime(t, second.Dispatch.CreatedAt)

	t.Run("attempt 1 rejects predecessor", func(t *testing.T) {
		if _, err := VerifyAuthorization(firstAuthority, first.Authorization, verifiedFirst, mustTime(t, first.Dispatch.CreatedAt), ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("attempt 1 predecessor error = %v", err)
		}
	})
	t.Run("attempt 1 rejects previous hash", func(t *testing.T) {
		current := cloneAuthorization(first.Authorization)
		current.Scope.Attempt.PreviousAuthorizationHash = first.Authorization.AuthorizationHash
		rehashAuthorizationOnly(t, &current)
		expected := authorityFromAuthorization(current)
		if _, err := VerifyAuthorization(expected, current, nil, mustTime(t, first.Dispatch.CreatedAt), ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("attempt 1 previous hash error = %v", err)
		}
	})
	t.Run("attempt 2 rejects absent predecessor", func(t *testing.T) {
		if _, err := VerifyAuthorization(authorityFromAuthorization(second.Authorization), second.Authorization, nil, secondNow, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("absent predecessor error = %v", err)
		}
	})
	t.Run("attempt 2 rejects opaque zero predecessor", func(t *testing.T) {
		if _, err := VerifyAuthorization(authorityFromAuthorization(second.Authorization), second.Authorization, &VerifiedAuthorization{}, secondNow, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("unknown predecessor error = %v", err)
		}
	})
	t.Run("attempt 2 rejects conflicting verified predecessor", func(t *testing.T) {
		otherFirst := cloneAuthorization(first.Authorization)
		otherFirst.Scope.Repository.RepositoryIdentity = "example.invalid/conflicting-predecessor"
		otherFirst.Scope.Repository.RepositoryIdentityHash = testHash("conflicting-predecessor-repository")
		otherFirst.Scope.Repository.BaseCommit = strings.Repeat("c", 40)
		otherFirst.Scope.Repository.BaseTree = strings.Repeat("d", 40)
		rehashAuthorizationOnly(t, &otherFirst)
		otherAuthority := authorityFromAuthorization(otherFirst)
		otherVerified, err := VerifyAuthorization(otherAuthority, otherFirst, nil, mustTime(t, first.Dispatch.CreatedAt), ValidationAdmission)
		if err != nil {
			t.Fatal(err)
		}
		current := cloneAuthorization(second.Authorization)
		current.Scope.Attempt.PreviousAuthorizationHash = otherFirst.AuthorizationHash
		rehashAuthorizationOnly(t, &current)
		if _, err := VerifyAuthorization(authorityFromAuthorization(current), current, otherVerified, secondNow, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("conflicting predecessor error = %v", err)
		}
	})
	t.Run("attempt 2 rejects wrong-lineage predecessor", func(t *testing.T) {
		otherFirst := cloneAuthorization(first.Authorization)
		otherFirst.Scope.RepairLineageHash = testHash("wrong-lineage")
		rehashAuthorizationOnly(t, &otherFirst)
		otherVerified, err := VerifyAuthorization(authorityFromAuthorization(otherFirst), otherFirst, nil, mustTime(t, first.Dispatch.CreatedAt), ValidationAdmission)
		if err != nil {
			t.Fatal(err)
		}
		current := cloneAuthorization(second.Authorization)
		current.Scope.Attempt.PreviousAuthorizationHash = otherFirst.AuthorizationHash
		rehashAuthorizationOnly(t, &current)
		if _, err := VerifyAuthorization(authorityFromAuthorization(current), current, otherVerified, secondNow, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("wrong-lineage predecessor error = %v", err)
		}
	})
	t.Run("attempt 2 rejects attempt-2 predecessor", func(t *testing.T) {
		verifiedSecond, err := VerifyAuthorization(authorityFromAuthorization(second.Authorization), second.Authorization, verifiedFirst, secondNow, ValidationAdmission)
		if err != nil {
			t.Fatal(err)
		}
		current := cloneAuthorization(second.Authorization)
		current.Scope.Attempt.PreviousAuthorizationHash = second.Authorization.AuthorizationHash
		rehashAuthorizationOnly(t, &current)
		if _, err := VerifyAuthorization(authorityFromAuthorization(current), current, verifiedSecond, secondNow, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("attempt-2 predecessor error = %v", err)
		}
	})

	freshnessCases := []struct {
		name   string
		mutate func(*Authorization)
	}{
		{name: "reused approval ID", mutate: func(current *Authorization) { current.Approval.ApprovalID = first.Authorization.Approval.ApprovalID }},
		{name: "reused provenance event", mutate: func(current *Authorization) {
			current.Approval.GUIProvenanceHash = first.Authorization.Approval.GUIProvenanceHash
		}},
		{name: "reused approval time", mutate: func(current *Authorization) { current.Approval.ApprovedAt = first.Authorization.Approval.ApprovedAt }},
	}
	for _, test := range freshnessCases {
		t.Run(test.name, func(t *testing.T) {
			current := cloneAuthorization(second.Authorization)
			test.mutate(&current)
			rehashAuthorizationOnly(t, &current)
			if _, err := VerifyAuthorization(authorityFromAuthorization(current), current, verifiedFirst, secondNow, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("reused GUI authority accepted: %v", err)
			}
		})
	}
	t.Run("reused authorization bytes", func(t *testing.T) {
		if _, err := VerifyAuthorization(firstAuthority, first.Authorization, verifiedFirst, mustTime(t, first.Dispatch.CreatedAt), ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("reused authorization bytes error = %v", err)
		}
	})
}

func TestP6A2DispatchCanonicalBytesAndReplayConflicts(t *testing.T) {
	fixture := CanonicalFixture()
	expected := authorityFromAuthorization(fixture.Authorization)
	verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	existing := canonicalTestArtifact(t, fixture.Dispatch)

	invalidEncodings := map[string][]byte{
		"appended newline": append(append([]byte(nil), existing...), '\n'),
		"appended space":   append(append([]byte(nil), existing...), ' '),
		"unknown key":      bytes.Replace(existing, []byte(`{"approval_hash":`), []byte(`{"unknown":true,"approval_hash":`), 1),
		"duplicate key":    bytes.Replace(existing, []byte(`{"approval_hash":`), []byte(`{"approval_hash":"`+fixture.Dispatch.ApprovalHash+`","approval_hash":`), 1),
	}
	for name, raw := range invalidEncodings {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeDispatch(expected, verified, raw, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("DecodeDispatch error = %v", err)
			}
		})
	}

	conflicts := []struct {
		name   string
		mutate func(*ImmutableDispatch)
	}{
		{name: "same asserted hash altered bytes", mutate: func(value *ImmutableDispatch) { value.DispatchNotAfter = "2026-07-26T12:07:59Z" }},
		{name: "deadline", mutate: func(value *ImmutableDispatch) {
			value.DispatchNotAfter = "2026-07-26T12:07:59Z"
			value.DispatchHash = ""
		}},
		{name: "peer", mutate: func(value *ImmutableDispatch) {
			value.ExpectedPeer.UserID++
			value.ExpectedPeer.PeerIdentityHash = mustRecordHash(t, value.ExpectedPeer, "peer_identity_hash")
			value.DispatchHash = ""
		}},
		{name: "profile", mutate: func(value *ImmutableDispatch) {
			value.SelectedSupervisorProfileID = "other_profile"
			value.DispatchHash = ""
		}},
		{name: "channel", mutate: func(value *ImmutableDispatch) {
			value.ChannelBindingHash = testHash("other-channel")
			value.DispatchHash = ""
		}},
		{name: "policy", mutate: func(value *ImmutableDispatch) {
			value.SelectedSupervisorPolicyHash = testHash("other-policy")
			value.DispatchHash = ""
		}},
		{name: "request", mutate: func(value *ImmutableDispatch) {
			value.Request.RequestID = "other_request"
			value.Request.RequestHash = mustRecordHash(t, value.Request, "request_hash")
			value.DispatchHash = ""
		}},
		{name: "authorization", mutate: func(value *ImmutableDispatch) {
			value.AuthorizationHash = testHash("other-authorization")
			value.DispatchHash = ""
		}},
		{name: "attempt", mutate: func(value *ImmutableDispatch) { value.AttemptNumber = 2; value.DispatchHash = "" }},
	}
	for _, test := range conflicts {
		t.Run(test.name, func(t *testing.T) {
			value := cloneDispatch(t, fixture.Dispatch)
			originalHash := value.DispatchHash
			test.mutate(&value)
			if value.DispatchHash == "" {
				value.DispatchHash = mustRecordHash(t, value, "dispatch_hash")
			} else {
				value.DispatchHash = originalHash
			}
			incoming := canonicalTestArtifact(t, value)
			disposition, err := ClassifyDispatchReplay(expected, verified, existing, incoming)
			if disposition != DispatchConflict || err == nil {
				t.Fatalf("replay conflict = %q, %v", disposition, err)
			}
		})
	}

	invalid := cloneDispatch(t, fixture.Dispatch)
	invalid.ExpectedPeer.UserID++
	invalid.ExpectedPeer.PeerIdentityHash = mustRecordHash(t, invalid.ExpectedPeer, "peer_identity_hash")
	invalid.DispatchHash = mustRecordHash(t, invalid, "dispatch_hash")
	invalidRaw := canonicalTestArtifact(t, invalid)
	if disposition, err := ClassifyDispatchReplay(expected, verified, invalidRaw, invalidRaw); disposition != DispatchConflict || !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("byte-identical semantically invalid replay = %q, %v", disposition, err)
	}

	if disposition, err := ClassifyDispatchReplay(expected, verified, existing, existing); disposition != DispatchExactReplay || err != nil {
		t.Fatalf("exact replay after freshness movement = %q, %v", disposition, err)
	}
}

func TestP6A2DispatchLifetimeExactBoundaries(t *testing.T) {
	fixture := CanonicalFixture()
	expected := authorityFromAuthorization(fixture.Authorization)
	verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := mustTime(t, fixture.Dispatch.CreatedAt)
	for _, test := range []struct {
		name     string
		lifetime time.Duration
		valid    bool
	}{
		{name: "N-1", lifetime: MaxDispatchLifetime - time.Nanosecond, valid: true},
		{name: "N", lifetime: MaxDispatchLifetime, valid: true},
		{name: "N+1", lifetime: MaxDispatchLifetime + time.Nanosecond, valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dispatch := cloneDispatch(t, fixture.Dispatch)
			dispatch.DispatchNotAfter = createdAt.Add(test.lifetime).Format(time.RFC3339Nano)
			dispatch.DispatchHash = mustRecordHash(t, dispatch, "dispatch_hash")
			_, err := DecodeDispatch(expected, verified, canonicalTestArtifact(t, dispatch), createdAt, ValidationAdmission)
			if test.valid && err != nil {
				t.Fatalf("dispatch lifetime boundary rejected: %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("dispatch lifetime boundary error = %v", err)
			}
		})
	}
}

func TestP6A2RFC8785AppendixBNumbers(t *testing.T) {
	vectors := []struct {
		bits uint64
		want string
	}{
		{0x0000000000000000, "0"}, {0x8000000000000000, "0"},
		{0x0000000000000001, "5e-324"}, {0x8000000000000001, "-5e-324"},
		{0x7fefffffffffffff, "1.7976931348623157e+308"}, {0xffefffffffffffff, "-1.7976931348623157e+308"},
		{0x4340000000000000, "9007199254740992"}, {0xc340000000000000, "-9007199254740992"},
		{0x4430000000000000, "295147905179352830000"},
		{0x44b52d02c7e14af5, "9.999999999999997e+22"}, {0x44b52d02c7e14af6, "1e+23"}, {0x44b52d02c7e14af7, "1.0000000000000001e+23"},
		{0x444b1ae4d6e2ef4e, "999999999999999700000"}, {0x444b1ae4d6e2ef4f, "999999999999999900000"}, {0x444b1ae4d6e2ef50, "1e+21"},
		{0x3eb0c6f7a0b5ed8c, "9.999999999999997e-7"}, {0x3eb0c6f7a0b5ed8d, "0.000001"},
		{0x41b3de4355555553, "333333333.3333332"}, {0x41b3de4355555554, "333333333.33333325"},
		{0x41b3de4355555555, "333333333.3333333"}, {0x41b3de4355555556, "333333333.3333334"},
		{0x41b3de4355555557, "333333333.33333343"}, {0xbecbf647612f3696, "-0.0000033333333333333333"},
		{0x43143ff3c1cb0959, "1424953923781206.2"},
	}
	for _, vector := range vectors {
		name := strconv.FormatUint(vector.bits, 16)
		t.Run(name, func(t *testing.T) {
			raw, err := canonicalBytes(math.Float64frombits(vector.bits))
			if err != nil || string(raw) != vector.want {
				t.Fatalf("canonical number = %q, %v; want %q", raw, err, vector.want)
			}
		})
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := canonicalBytes(invalid); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("non-JSON number error = %v", err)
		}
	}
}

func TestP6A2RFC8785UTF16OrderingAndUnicode(t *testing.T) {
	value := map[string]any{"\ue000": 2, "\U00010000": 1}
	raw, err := canonicalBytes(value)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"\U00010000\":1,\"\ue000\":2}"; string(raw) != want {
		t.Fatalf("UTF-16 key order = %q, want %q", raw, want)
	}
	var decoded any
	pair := []byte(`"\ud83d\ude00"`)
	if err := validateRawJSONStringScalars(pair); err != nil {
		t.Fatalf("valid surrogate pair rejected: %v", err)
	}
	decoded, err = decodeUniqueJSON(pair)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalBytes(decoded)
	if err != nil || string(canonical) != `"😀"` {
		t.Fatalf("valid surrogate pair canonical form = %q, %v", canonical, err)
	}
}

func TestP6A2RFC8785NegativeZeroAndExponentThresholds(t *testing.T) {
	vectors := map[string]float64{
		"0":                     math.Copysign(0, -1),
		"0.000001":              math.Float64frombits(0x3eb0c6f7a0b5ed8d),
		"9.999999999999997e-7":  math.Float64frombits(0x3eb0c6f7a0b5ed8c),
		"999999999999999900000": math.Float64frombits(0x444b1ae4d6e2ef4f),
		"1e+21":                 math.Float64frombits(0x444b1ae4d6e2ef50),
	}
	for want, value := range vectors {
		raw, err := canonicalBytes(value)
		if err != nil || string(raw) != want {
			t.Fatalf("canonical threshold %v = %q, %v; want %q", value, raw, err, want)
		}
	}
}

func rehashAuthorizationOnly(t *testing.T, authorization *Authorization) {
	t.Helper()
	fixture := ContractFixture{Authorization: cloneAuthorization(*authorization), Dispatch: ImmutableDispatch{}}
	rehashAuthorizationChain(t, &fixture)
	*authorization = fixture.Authorization
}

func assertExecutedVectorOrder(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("executed acceptance vector order drifted\n got: %q\nwant: %q", got, want)
	}
}

func decodeCanonicalGeneric(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
