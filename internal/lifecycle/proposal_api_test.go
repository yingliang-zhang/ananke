package lifecycle

import (
	"reflect"
	"testing"
)

func TestProposalCommandsServePrivateProtocol(t *testing.T) {
	env := newEngineEnv(t)
	projectID := "project_p1c"
	workstreamID := "workstream_p1c"

	createA := proposalCreateInput("proposal_api_create_a", projectID, workstreamID, "Approve proposal")
	createdA := proposalMutationFromAPI(t, engineAPI(t, env, "create-proposal", map[string]any{"proposal": createA}))
	replayedA := proposalMutationFromAPI(t, engineAPI(t, env, "create-proposal", map[string]any{"proposal": createA}))
	if !reflect.DeepEqual(replayedA, createdA) {
		t.Fatalf("create replay = %#v, want %#v", replayedA, createdA)
	}
	conflict := engineAPI(t, env, "create-proposal", map[string]any{
		"proposal": proposalCreateInput("proposal_api_create_a", projectID, workstreamID, "Conflicting proposal"),
	})
	if conflict["ok"] != false || conflict["error"] != "idempotency_conflict" {
		t.Fatalf("conflicting replay = %#v, want idempotency_conflict", conflict)
	}

	proposalAID := proposalString(t, createdA, "proposal_id")
	detailA := proposalDetailFromAPI(t, engineAPI(t, env, "get-proposal", map[string]any{
		"proposal": map[string]any{"proposal_id": proposalAID},
	}))
	if proposalString(t, detailA["proposal"].(map[string]any), "proposal_id") != proposalAID {
		t.Fatalf("get detail does not belong to created proposal: %#v", detailA)
	}

	appendedA := proposalMutationFromAPI(t, engineAPI(t, env, "append-proposal-revision", map[string]any{
		"proposal": map[string]any{
			"idempotency_key":                "proposal_api_append_a",
			"proposal_id":                    proposalAID,
			"expected_current_revision":      createdA["revision"],
			"expected_current_revision_hash": proposalString(t, createdA, "revision_hash"),
			"revision_input":                 proposalRevisionInput("Approve proposal revision"),
		},
	}))
	approvedA := proposalMutationFromAPI(t, engineAPI(t, env, "decide-proposal-approval", map[string]any{
		"proposal": proposalDecisionInput("proposal_api_approve_a", appendedA, "approved", "Meets the reviewed contract."),
	}))
	if !reflect.DeepEqual(approvedA, appendedA) {
		t.Fatalf("approved mutation = %#v, want %#v", approvedA, appendedA)
	}

	createdB := proposalMutationFromAPI(t, engineAPI(t, env, "create-proposal", map[string]any{
		"proposal": proposalCreateInput("proposal_api_create_b", projectID, workstreamID, "Reject proposal"),
	}))
	proposalMutationFromAPI(t, engineAPI(t, env, "decide-proposal-approval", map[string]any{
		"proposal": proposalDecisionInput("proposal_api_reject_b", createdB, "rejected", "Needs a narrower review."),
	}))

	createdC := proposalMutationFromAPI(t, engineAPI(t, env, "create-proposal", map[string]any{
		"proposal": proposalCreateInput("proposal_api_create_c", projectID, workstreamID, "Withdraw proposal"),
	}))
	withdrawnC := proposalMutationFromAPI(t, engineAPI(t, env, "withdraw-proposal", map[string]any{
		"proposal": map[string]any{
			"idempotency_key": "proposal_api_withdraw_c",
			"proposal_id":     proposalString(t, createdC, "proposal_id"),
		},
	}))
	if !reflect.DeepEqual(withdrawnC, createdC) {
		t.Fatalf("withdraw mutation = %#v, want %#v", withdrawnC, createdC)
	}

	listed := engineAPI(t, env, "list-proposals", map[string]any{
		"proposal": map[string]any{"project_id": projectID, "workstream_id": workstreamID},
	})
	proposals, ok := listed["proposals"].([]any)
	if !ok || len(proposals) != 3 {
		t.Fatalf("list response = %#v, want three target proposals", listed)
	}
	for _, value := range proposals {
		proposal, ok := value.(map[string]any)
		if !ok || proposal["project_id"] != projectID || proposal["workstream_id"] != workstreamID {
			t.Fatalf("list leaked another target: %#v", value)
		}
	}

	activityA := proposalActivityFromAPI(t, engineAPI(t, env, "list-proposal-activity", map[string]any{
		"proposal": map[string]any{"proposal_id": proposalAID},
	}))
	if len(activityA) != 3 || activityA[0]["operation"] != "create_proposal" || activityA[1]["operation"] != "append_revision" || activityA[2]["operation"] != "decide_approval" {
		t.Fatalf("approved proposal activity = %#v, want create/append/decision", activityA)
	}
	activityB := proposalActivityFromAPI(t, engineAPI(t, env, "list-proposal-activity", map[string]any{
		"proposal": map[string]any{"proposal_id": proposalString(t, createdB, "proposal_id")},
	}))
	if len(activityB) != 2 || activityB[1]["operation"] != "decide_approval" {
		t.Fatalf("rejected proposal activity = %#v, want create/rejection", activityB)
	}
	activityC := proposalActivityFromAPI(t, engineAPI(t, env, "list-proposal-activity", map[string]any{
		"proposal": map[string]any{"proposal_id": proposalString(t, createdC, "proposal_id")},
	}))
	if len(activityC) != 2 || activityC[1]["operation"] != "withdraw_proposal" {
		t.Fatalf("withdrawn proposal activity = %#v, want create/withdrawal", activityC)
	}
}

func TestListProposalActivityMissingProposalRetainsPrivateNotFoundError(t *testing.T) {
	env := newEngineEnv(t)
	response := engineAPI(t, env, "list-proposal-activity", map[string]any{
		"proposal": map[string]any{"proposal_id": "proposal_missing"},
	})
	if response["ok"] != false || response["error"] != "proposal not found" {
		t.Fatalf("missing proposal activity response = %#v, want private proposal-not-found error", response)
	}
	if _, exists := response["proposal_activity"]; exists {
		t.Fatalf("missing proposal activity response exposed an activity list: %#v", response)
	}
}

func proposalCreateInput(idempotencyKey, projectID, workstreamID, title string) map[string]any {
	return map[string]any{
		"idempotency_key": idempotencyKey,
		"project_id":      projectID,
		"workstream_id":   workstreamID,
		"revision_input":  proposalRevisionInput(title),
	}
}

func proposalRevisionInput(title string) map[string]any {
	return map[string]any{
		"task": map[string]any{
			"title":        title,
			"instructions": "Preserve the frozen proposal boundary without execution.",
		},
		"acceptance_criteria": []any{"Use only durable proposal records."},
		"policy": map[string]any{
			"adapter": map[string]any{
				"access": "read_only",
				"kind":   "omp_audit",
				"status": "future",
			},
			"authority": "deterministic",
			"budget": map[string]any{
				"dimensions": []any{"deadline", "attempt_cap"},
				"status":     "future",
			},
			"model_role": "advisory_only",
		},
	}
}

func proposalDecisionInput(idempotencyKey string, mutation map[string]any, decision, reason string) map[string]any {
	return map[string]any{
		"idempotency_key": idempotencyKey,
		"approval_id":     proposalString(nil, mutation, "approval_id"),
		"proposal_id":     proposalString(nil, mutation, "proposal_id"),
		"revision":        mutation["revision"],
		"revision_hash":   proposalString(nil, mutation, "revision_hash"),
		"decision":        decision,
		"reason":          reason,
	}
}

func proposalMutationFromAPI(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	if response["ok"] != true {
		t.Fatalf("proposal mutation response = %#v", response)
	}
	mutation, ok := response["proposal_mutation"].(map[string]any)
	if !ok {
		t.Fatalf("proposal mutation missing from %#v", response)
	}
	for _, key := range []string{"proposal_id", "revision", "revision_hash", "approval_id"} {
		if _, ok := mutation[key]; !ok {
			t.Fatalf("proposal mutation missing %q: %#v", key, mutation)
		}
	}
	return mutation
}

func proposalDetailFromAPI(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	if response["ok"] != true {
		t.Fatalf("proposal detail response = %#v", response)
	}
	detail, ok := response["proposal_detail"].(map[string]any)
	if !ok {
		t.Fatalf("proposal detail missing from %#v", response)
	}
	for _, key := range []string{"proposal", "revision", "lifecycle", "approval"} {
		if _, ok := detail[key]; !ok {
			t.Fatalf("proposal detail missing %q: %#v", key, detail)
		}
	}
	return detail
}

func proposalActivityFromAPI(t *testing.T, response map[string]any) []map[string]any {
	t.Helper()
	if response["ok"] != true {
		t.Fatalf("proposal activity response = %#v", response)
	}
	values, ok := response["proposal_activity"].([]any)
	if !ok {
		t.Fatalf("proposal activity missing from %#v", response)
	}
	activity := make([]map[string]any, 0, len(values))
	for _, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("proposal activity record = %#v", value)
		}
		activity = append(activity, record)
	}
	return activity
}

func proposalString(t *testing.T, value map[string]any, key string) string {
	if t != nil {
		t.Helper()
	}
	stringValue, ok := value[key].(string)
	if !ok || stringValue == "" {
		if t != nil {
			t.Fatalf("proposal value %q = %#v, want nonempty string", key, value[key])
		}
		panic("proposal mutation lacks " + key)
	}
	return stringValue
}
