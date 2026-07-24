package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestP4EvidenceAdmissionRuntimePersistsFakeVerifierResponseAndReplaysWithoutNewFacts(t *testing.T) {
	ctx := context.Background()
	journal, err := store.Open(filepath.Join(t.TempDir(), "p4-runtime.sqlite"))
	if err != nil {
		t.Fatalf("open P4 journal: %v", err)
	}
	defer journal.Close()

	fact := store.CanonicalP4EvidenceAdmission()
	fake := &p4FakeVerifier{response: store.P4VerifierResponse{Output: fact.VerifierOutput, Replay: fact.VerifierReplay}}
	runtime, err := newP4EvidenceAdmissionRuntime(journal, fake)
	if err != nil {
		t.Fatalf("new P4 runtime: %v", err)
	}

	first := runtime.submit(ctx, fact)
	assertP4RuntimeVerifiedWaiting(t, first)
	if fake.calls() != 1 {
		t.Fatalf("fake verifier calls after first submission = %d, want 1", fake.calls())
	}
	persisted, err := journal.GetP4EvidenceAdmission(ctx, fact.VerifierRequest.InputHash)
	if err != nil {
		t.Fatalf("load persisted P4 evidence: %v", err)
	}
	if !reflect.DeepEqual(persisted, fact) {
		t.Fatalf("persisted P4 evidence = %#v, want %#v", persisted, fact)
	}
	assertP4RuntimeNoLocalRepairOrRun(t, journal)

	replay := runtime.submit(ctx, fact)
	assertP4RuntimeVerifiedWaiting(t, replay)
	if fake.calls() != 1 {
		t.Fatalf("P4 replay invoked fake verifier %d times, want 1", fake.calls())
	}
	assertP4RuntimeNoLocalRepairOrRun(t, journal)
}

func TestP4EvidenceAdmissionRuntimeFailsClosedOnVerifierAndInputFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*store.P4EvidenceAdmission, *p4FakeVerifier)
	}{
		{
			name: "fake verifier error",
			mutate: func(_ *store.P4EvidenceAdmission, fake *p4FakeVerifier) {
				fake.err = errors.New("fake verifier rejected P4 evidence")
			},
		},
		{
			name: "fake verifier authorizes repair",
			mutate: func(fact *store.P4EvidenceAdmission, fake *p4FakeVerifier) {
				fake.response.Output = fact.VerifierOutput
				fake.response.Output.RepairExecution = "authorized"
			},
		},
		{
			name: "fake verifier appends replay fact",
			mutate: func(fact *store.P4EvidenceAdmission, fake *p4FakeVerifier) {
				fake.response.Output = fact.VerifierOutput
				fake.response.Replay = fact.VerifierReplay
				fake.response.Replay.NewDurableFacts = 1
			},
		},
		{
			name: "unsafe repair admission is rejected before fake verifier",
			mutate: func(fact *store.P4EvidenceAdmission, fake *p4FakeVerifier) {
				fact.RepairAdmission.AllowedRole = "other_repair_role"
				fake.response = store.P4VerifierResponse{Output: store.CanonicalP4EvidenceAdmission().VerifierOutput, Replay: store.CanonicalP4EvidenceAdmission().VerifierReplay}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			journal, err := store.Open(filepath.Join(t.TempDir(), "p4-failure.sqlite"))
			if err != nil {
				t.Fatalf("open P4 journal: %v", err)
			}
			defer journal.Close()
			fact := store.CanonicalP4EvidenceAdmission()
			fake := &p4FakeVerifier{response: store.P4VerifierResponse{Output: fact.VerifierOutput, Replay: fact.VerifierReplay}}
			tc.mutate(&fact, fake)
			runtime, err := newP4EvidenceAdmissionRuntime(journal, fake)
			if err != nil {
				t.Fatalf("new P4 runtime: %v", err)
			}

			output := runtime.submit(context.Background(), fact)
			assertP4RuntimeFailureWaiting(t, output)
			if tc.name == "unsafe repair admission is rejected before fake verifier" && fake.calls() != 0 {
				t.Fatalf("invalid P4 input invoked fake verifier %d times", fake.calls())
			}
			assertP4RuntimeNoP4Facts(t, journal)
			assertP4RuntimeNoLocalRepairOrRun(t, journal)
		})
	}
}

func TestP4EvidenceAdmissionRuntimeConcurrentSubmissionCallsOnlyTestFakeOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "p4-runtime-concurrent.sqlite")
	leftJournal, err := store.Open(path)
	if err != nil {
		t.Fatalf("open left P4 journal: %v", err)
	}
	defer leftJournal.Close()
	rightJournal, err := store.Open(path)
	if err != nil {
		t.Fatalf("open right P4 journal: %v", err)
	}
	defer rightJournal.Close()
	fact := store.CanonicalP4EvidenceAdmission()
	fake := &p4FakeVerifier{response: store.P4VerifierResponse{Output: fact.VerifierOutput, Replay: fact.VerifierReplay}, started: make(chan struct{}), release: make(chan struct{})}
	left, err := newP4EvidenceAdmissionRuntime(leftJournal, fake)
	if err != nil {
		t.Fatalf("new left P4 runtime: %v", err)
	}
	right, err := newP4EvidenceAdmissionRuntime(rightJournal, fake)
	if err != nil {
		t.Fatalf("new right P4 runtime: %v", err)
	}

	outputs := make(chan p4EvidenceAdmissionPublicOutput, 2)
	go func() { outputs <- left.submit(ctx, fact) }()
	<-fake.started
	go func() { outputs <- right.submit(ctx, fact) }()
	close(fake.release)
	for range 2 {
		assertP4RuntimeVerifiedWaiting(t, <-outputs)
	}
	if fake.calls() != 1 {
		t.Fatalf("concurrent P4 submission invoked fake verifier %d times, want 1", fake.calls())
	}
	assertP4RuntimeNoLocalRepairOrRun(t, leftJournal)
}

func TestP4ProductionBuildExcludesFakeVerifier(t *testing.T) {
	command := exec.Command("go", "list", "-json", ".")
	encoded, err := command.Output()
	if err != nil {
		t.Fatalf("list production lifecycle package: %v", err)
	}
	var listed struct {
		GoFiles     []string
		TestGoFiles []string
	}
	if err := json.Unmarshal(encoded, &listed); err != nil {
		t.Fatalf("decode production lifecycle package: %v", err)
	}
	const fakeSource = "p4_evidence_admission_test.go"
	for _, name := range listed.GoFiles {
		if name == fakeSource || strings.Contains(strings.ToLower(name), "fake_verifier") {
			t.Fatalf("P4 fake verifier source %q compiled into production", name)
		}
	}
	found := false
	for _, name := range listed.TestGoFiles {
		if name == fakeSource {
			found = true
		}
	}
	if !found {
		t.Fatalf("P4 fake verifier source missing from test-only files: %v", listed.TestGoFiles)
	}
}

func assertP4RuntimeVerifiedWaiting(t *testing.T, output p4EvidenceAdmissionPublicOutput) {
	t.Helper()
	if output.SchemaVersion != "ananke.self-development-evidence-verifier-public-output.v1" ||
		output.State != "waiting_for_human" || output.VerificationState != "verified" ||
		output.Admission != "bounded_repair_admissible_design_only" || output.BundleHash == nil ||
		output.RepairExecution != "not_authorized_by_verifier" {
		t.Fatalf("P4 verified runtime output = %#v, want closed waiting_for_human no-repair output", output)
	}
}

func assertP4RuntimeFailureWaiting(t *testing.T, output p4EvidenceAdmissionPublicOutput) {
	t.Helper()
	if output.SchemaVersion != "ananke.self-development-evidence-verifier-public-output.v1" ||
		output.State != "waiting_for_human" || output.VerificationState != "not_run" ||
		output.Admission != "rejected" || output.BundleHash != nil || output.RepairExecution != "not_authorized" {
		t.Fatalf("P4 failed runtime output = %#v, want exact closed waiting_for_human failure output", output)
	}
}

func assertP4RuntimeNoP4Facts(t *testing.T, journal *store.Store) {
	t.Helper()
	for _, table := range []string{"p4_evidence_bundles", "p4_repair_admissions", "p4_verifier_requests", "p4_verifier_outputs", "p4_verifier_replays"} {
		var got int
		if err := journal.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != 0 {
			t.Fatalf("%s count = %d, want no durable P4 facts on failure", table, got)
		}
	}
}

func assertP4RuntimeNoLocalRepairOrRun(t *testing.T, journal *store.Store) {
	t.Helper()
	var runs int
	if err := journal.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM runs`).Scan(&runs); err != nil {
		t.Fatalf("count local runs: %v", err)
	}
	if runs != 0 {
		t.Fatalf("P4 runtime created %d local runs", runs)
	}
	var repairs int
	if err := journal.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE '%repair%run%'`).Scan(&repairs); err != nil {
		t.Fatalf("inspect local repair tables: %v", err)
	}
	if repairs != 0 {
		t.Fatalf("P4 runtime introduced local repair/run tables: %d", repairs)
	}
}

type p4FakeVerifier struct {
	mu       sync.Mutex
	response store.P4VerifierResponse
	err      error
	count    int
	started  chan struct{}
	release  chan struct{}
}

func (fake *p4FakeVerifier) VerifyP4Evidence(_ context.Context, _ store.P4VerifierRequest) (store.P4VerifierResponse, error) {
	fake.mu.Lock()
	fake.count++
	started, release := fake.started, fake.release
	response, err := fake.response, fake.err
	fake.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	return response, err
}

func (fake *p4FakeVerifier) calls() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.count
}
