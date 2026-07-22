package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestP1ACanonicalHashesMatchFixtures(t *testing.T) {
	t.Parallel()

	revision := loadP1AFixture(t, "revision-v1.canonical.json")
	if got, err := canonicalJSONHash(revision); err != nil {
		t.Fatalf("hash revision fixture: %v", err)
	} else if want := "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263"; got != want {
		t.Fatalf("revision hash = %q, want %q", got, want)
	}

	envelopes := loadP1AFixture(t, "request-envelopes-v1.canonical.json")
	for name, envelope := range envelopes {
		envelopeObject, ok := envelope.(map[string]any)
		if !ok || name == "schema_version" {
			continue
		}
		body, ok := envelopeObject["body"]
		if !ok {
			t.Fatalf("%s envelope has no body", name)
		}
		want, ok := envelopeObject["body_hash"].(string)
		if !ok {
			t.Fatalf("%s envelope has no body_hash", name)
		}
		got, err := canonicalJSONHash(body)
		if err != nil {
			t.Fatalf("hash %s body: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s body hash = %q, want %q", name, got, want)
		}
	}
}

func TestP1ARequestBodyBuildersMatchFixtureHashes(t *testing.T) {
	t.Parallel()
	create := createProposalRequestFromFixture(t)
	var proposal Proposal
	var approval Approval
	readP1AJSONFixture(t, "proposal-v1.canonical.json", &proposal)
	readP1AJSONFixture(t, "approval-v1.canonical.json", &approval)
	target := ProposalMutation{
		ProposalID:   proposal.ProposalID,
		Revision:     proposal.CurrentRevision,
		RevisionHash: proposal.CurrentRevisionHash,
		ApprovalID:   approval.ApprovalID,
	}
	appendRequest := appendProposalRequestFromFixture(t, target)
	approve := decisionProposalRequestFromFixture(t, target, ApprovalStateApproved)
	reject := decisionProposalRequestFromFixture(t, target, ApprovalStateRejected)
	withdraw := withdrawProposalRequestFromFixture(t, target.ProposalID)

	var envelopes struct {
		Create struct {
			BodyHash string `json:"body_hash"`
		} `json:"create"`
		Append struct {
			BodyHash string `json:"body_hash"`
		} `json:"append"`
		DecisionApprove struct {
			BodyHash string `json:"body_hash"`
		} `json:"decision_approve"`
		DecisionReject struct {
			BodyHash string `json:"body_hash"`
		} `json:"decision_reject"`
		Withdraw struct {
			BodyHash string `json:"body_hash"`
		} `json:"withdraw"`
	}
	readP1AJSONFixture(t, "request-envelopes-v1.canonical.json", &envelopes)
	for _, check := range []struct {
		name string
		body any
		want string
	}{
		{name: "create", body: createRequestBody(create), want: envelopes.Create.BodyHash},
		{name: "append", body: appendRevisionRequestBody(appendRequest), want: envelopes.Append.BodyHash},
		{name: "approve", body: decisionRequestBody(approve), want: envelopes.DecisionApprove.BodyHash},
		{name: "reject", body: decisionRequestBody(reject), want: envelopes.DecisionReject.BodyHash},
		{name: "withdraw", body: withdrawProposalRequestBody(withdraw), want: envelopes.Withdraw.BodyHash},
	} {
		got, err := canonicalJSONHash(check.body)
		if err != nil {
			t.Fatalf("hash %s body: %v", check.name, err)
		}
		if got != check.want {
			t.Fatalf("%s body hash = %q, want %q", check.name, got, check.want)
		}
	}
}

func loadP1AFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	fixtureBytes, err := os.ReadFile(filepath.Join("..", "..", "contracts", "p1a", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(fixtureBytes))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return value
}
