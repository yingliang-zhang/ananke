package lifecycle

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestGrillCommandsServeFrozenPrivateReviewProtocol(t *testing.T) {
	env := newEngineEnv(t)
	created := proposalMutationFromAPI(t, engineAPI(t, env, "create-proposal", map[string]any{
		"proposal": proposalCreateInput("grill_api_create", "project_p2b", "workstream_p2b", "Review only Grill target"),
	}))
	input, fixture := grillInputForMutation(t, created)
	inputHash, err := store.HashGrillInput(input)
	if err != nil {
		t.Fatalf("HashGrillInput: %v", err)
	}
	request := map[string]any{
		"input":        grillInputMap(t, input),
		"input_hash":   inputHash,
		"rule_version": store.GrillRuleVersion,
	}

	initial := grillEvaluationFromAPI(t, engineAPI(t, env, "evaluate-grill", map[string]any{"grill": request}))
	assertPrivateGrillEvaluation(t, initial, fixture.Evaluation.Initial.NewQuestionIDs, fixture.Evaluation.Initial.ShownQuestionIDs, fixture.Evaluation.Initial.DeferredRuleClasses, "blocked", 6)

	replayed := grillEvaluationFromAPI(t, engineAPI(t, env, "evaluate-grill", map[string]any{"grill": request}))
	assertPrivateGrillEvaluation(t, replayed, nil, fixture.Evaluation.Initial.ShownQuestionIDs, fixture.Evaluation.Initial.DeferredRuleClasses, "blocked", 0)

	answer := engineAPI(t, env, "record-grill-answer", map[string]any{"grill": map[string]any{
		"proposal_id":   input.ProposalID,
		"revision":      input.Revision,
		"revision_hash": input.RevisionHash,
		"question_id":   "grill_question_acceptance_evidence",
	}})
	if answer["ok"] != true {
		t.Fatalf("answer response = %#v", answer)
	}
	override := engineAPI(t, env, "record-grill-override", map[string]any{"grill": map[string]any{
		"proposal_id":   input.ProposalID,
		"revision":      input.Revision,
		"revision_hash": input.RevisionHash,
		"question_id":   "grill_question_scope_compatibility",
	}})
	if override["ok"] != true {
		t.Fatalf("override response = %#v", override)
	}
	afterWaiver := grillEvaluationFromAPI(t, engineAPI(t, env, "evaluate-grill", map[string]any{"grill": request}))
	assertPrivateGrillEvaluation(t, afterWaiver, fixture.Evaluation.AfterScopeOverride.NewQuestionIDs, fixture.Evaluation.AfterScopeOverride.ShownQuestionIDs, fixture.Evaluation.AfterScopeOverride.DeferredRuleClasses, "blocked", 1)

	const injectedField = "raw_revision_prose_secret"
	const injectedValue = "raw_revision_prose_secret_value"
	injected := grillInputMap(t, input)
	injected[injectedField] = injectedValue
	injectedRequest := map[string]any{"input": injected, "input_hash": inputHash, "rule_version": store.GrillRuleVersion}
	serialized := grillDaemonResponse(t, env, "evaluate-grill", map[string]any{"grill": injectedRequest})
	var response struct {
		OK              bool            `json:"ok"`
		Error           string          `json:"error"`
		GrillEvaluation json.RawMessage `json:"grill_evaluation"`
	}
	if err := json.Unmarshal(serialized, &response); err != nil {
		t.Fatalf("decode Grill daemon response: %v", err)
	}
	if response.OK {
		t.Fatalf("raw revision-prose injection response = %s, want rejection", serialized)
	}
	if response.Error != "invalid grill request" {
		t.Fatalf("raw revision-prose injection error = %q, want stable invalid Grill request", response.Error)
	}
	if response.GrillEvaluation != nil {
		t.Fatalf("raw revision-prose injection exposed evaluation = %s", serialized)
	}
	for _, forbidden := range []string{injectedField, injectedValue, `json: unknown field`} {
		if bytes.Contains(serialized, []byte(forbidden)) {
			t.Fatalf("raw revision-prose injection leaked %q in serialized response %s", forbidden, serialized)
		}
	}
}

type p2aGrillAPIFixture struct {
	Input      store.GrillInput `json:"input"`
	ValidInput store.GrillInput `json:"valid_input"`
	Evaluation struct {
		Initial struct {
			EvaluationID        string   `json:"evaluation_id"`
			InputHash           string   `json:"input_hash"`
			ProposalID          string   `json:"proposal_id"`
			Revision            int      `json:"revision"`
			RevisionHash        string   `json:"revision_hash"`
			RuleVersion         string   `json:"rule_version"`
			NewQuestionIDs      []string `json:"new_question_ids"`
			ShownQuestionIDs    []string `json:"shown_question_ids"`
			DeferredRuleClasses []string `json:"deferred_rule_classes"`
			Status              string   `json:"status"`
		} `json:"initial"`
		AfterScopeOverride struct {
			EvaluationID        string   `json:"evaluation_id"`
			InputHash           string   `json:"input_hash"`
			ProposalID          string   `json:"proposal_id"`
			Revision            int      `json:"revision"`
			RevisionHash        string   `json:"revision_hash"`
			RuleVersion         string   `json:"rule_version"`
			NewQuestionIDs      []string `json:"new_question_ids"`
			ShownQuestionIDs    []string `json:"shown_question_ids"`
			DeferredRuleClasses []string `json:"deferred_rule_classes"`
			Status              string   `json:"status"`
		} `json:"after_scope_override"`
		SameInputReplay struct {
			EvaluationID        string   `json:"evaluation_id"`
			InputHash           string   `json:"input_hash"`
			ProposalID          string   `json:"proposal_id"`
			Revision            int      `json:"revision"`
			RevisionHash        string   `json:"revision_hash"`
			RuleVersion         string   `json:"rule_version"`
			NewQuestionIDs      []string `json:"new_question_ids"`
			NewRecords          int      `json:"new_records"`
			ShownQuestionIDs    []string `json:"shown_question_ids"`
			DeferredRuleClasses []string `json:"deferred_rule_classes"`
			Status              string   `json:"status"`
		} `json:"same_input_replay"`
	} `json:"evaluation"`
	Records       []json.RawMessage `json:"records"`
	Rules         []json.RawMessage `json:"rules"`
	SchemaVersion string            `json:"schema_version"`
}

func grillInputForMutation(t *testing.T, mutation map[string]any) (store.GrillInput, p2aGrillAPIFixture) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "contracts", "p2a", "fixtures", "grill-v1.canonical.json"))
	if err != nil {
		t.Fatalf("read P2a fixture: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var fixture p2aGrillAPIFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode P2a fixture: %v", err)
	}
	fixture.Input.ProposalID = proposalString(t, mutation, "proposal_id")
	fixture.Input.Revision = int(mutation["revision"].(float64))
	fixture.Input.RevisionHash = proposalString(t, mutation, "revision_hash")
	return fixture.Input, fixture
}

func grillInputMap(t *testing.T, input store.GrillInput) map[string]any {
	t.Helper()
	contents, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal Grill input: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(contents, &value); err != nil {
		t.Fatalf("unmarshal Grill input: %v", err)
	}
	return value
}

// grillDaemonResponse returns the exact JSON response emitted by the live
// daemon socket so privacy assertions inspect the serialized boundary.
func grillDaemonResponse(t *testing.T, env *engineEnv, cmd string, extra map[string]any) []byte {
	t.Helper()
	conn, err := net.Dial("unix", env.socketPath)
	if err != nil {
		t.Fatalf("dial engine: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	request := map[string]any{"cmd": cmd, "token": env.token}
	for key, value := range extra {
		request[key] = value
	}
	contents, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal Grill daemon request: %v", err)
	}
	if _, err := conn.Write(contents); err != nil {
		t.Fatalf("write Grill daemon request: %v", err)
	}
	var response json.RawMessage
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatalf("decode Grill daemon response: %v", err)
	}
	return response
}

func grillEvaluationFromAPI(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	if response["ok"] != true {
		t.Fatalf("Grill evaluation response = %#v", response)
	}
	value, ok := response["grill_evaluation"].(map[string]any)
	if !ok {
		t.Fatalf("Grill evaluation missing from %#v", response)
	}
	return value
}

func assertPrivateGrillEvaluation(t *testing.T, evaluation map[string]any, wantNew, wantShown, wantDeferred []string, wantStatus string, wantNewRecords float64) {
	t.Helper()
	if !reflect.DeepEqual(evaluation["new_question_ids"], stringsToAny(wantNew)) || !reflect.DeepEqual(evaluation["shown_question_ids"], stringsToAny(wantShown)) || !reflect.DeepEqual(evaluation["deferred_rule_classes"], stringsToAny(wantDeferred)) || evaluation["status"] != wantStatus || evaluation["new_records"] != wantNewRecords {
		t.Fatalf("Grill evaluation = %#v, want new=%v shown=%v deferred=%v status=%s records=%v", evaluation, wantNew, wantShown, wantDeferred, wantStatus, wantNewRecords)
	}
}

func stringsToAny(values []string) []any {
	if values == nil {
		return []any{}
	}
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
