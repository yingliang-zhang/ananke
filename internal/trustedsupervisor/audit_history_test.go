package trustedsupervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateAuditExecutionHistoryRejectsCanonicallyResealedCrossInvariantTamper(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *auditExecutionIntent, []auditExecutionEvent)
	}{
		{
			name: "derived event ID",
			mutate: func(t *testing.T, _ *auditExecutionIntent, events []auditExecutionEvent) {
				events[0].EventID = "audit_event_resealed_wrong_001"
				events[0] = resealAuditEventForHistoryTest(t, events[0])
			},
		},
		{
			name: "attempt cap",
			mutate: func(t *testing.T, _ *auditExecutionIntent, events []auditExecutionEvent) {
				events[0].Attempt = 2
				events[0] = resealAuditEventForHistoryTest(t, events[0])
			},
		},
		{
			name: "command continuity",
			mutate: func(t *testing.T, _ *auditExecutionIntent, events []auditExecutionEvent) {
				events[1].CommandDescriptorHash = testHash("resealed-command-drift")
				events[1] = resealAuditEventForHistoryTest(t, events[1])
			},
		},
		{
			name: "process continuity",
			mutate: func(t *testing.T, _ *auditExecutionIntent, events []auditExecutionEvent) {
				events[2].PID++
				events[2].PGID = events[2].PID
				resealCompletedAuditEvidenceForHistoryTest(t, &events[2], func(report *auditEvidenceReport) {
					report.PID = events[2].PID
					report.PGID = events[2].PGID
				})
			},
		},
		{
			name: "attempt path continuity",
			mutate: func(t *testing.T, _ *auditExecutionIntent, events []auditExecutionEvent) {
				events[1].PromptPath = "/private/prompt/other/audit-prompt.txt"
				events[1] = resealAuditEventForHistoryTest(t, events[1])
			},
		},
		{
			name: "monotonic timestamp",
			mutate: func(t *testing.T, _ *auditExecutionIntent, events []auditExecutionEvent) {
				events[1].OccurredAt = "2026-07-25T23:59:59Z"
				events[1] = resealAuditEventForHistoryTest(t, events[1])
			},
		},
		{
			name: "typed evidence intent binding",
			mutate: func(t *testing.T, _ *auditExecutionIntent, events []auditExecutionEvent) {
				resealCompletedAuditEvidenceForHistoryTest(t, &events[2], func(report *auditEvidenceReport) {
					report.TaskID = "audit_task_resealed_forgery"
				})
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := validAuditExecutionHistoryForTest(t)
			testCase.mutate(t, &fixture.Intent, fixture.Events)
			if err := validateAuditExecutionHistory(fixture.Authority, fixture.Intent, fixture.Events); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("resealed history tamper error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestAuditFinalizingHistoryIsSignedNonterminalAndReplayClosed(t *testing.T) {
	fixture := validAuditExecutionHistoryForTest(t)
	finalizing := fixture.Events[2]
	if finalizing.State != auditStateFinalizing || finalizing.Authentication.Signature == "" {
		t.Fatalf("fixture finalizing authority = %+v", finalizing)
	}
	history := fixture.Events[:3]
	if err := validateAuditExecutionHistory(fixture.Authority, fixture.Intent, history); err != nil {
		t.Fatalf("valid signed finalizing history: %v", err)
	}

	unsigned := finalizing
	unsigned.Authentication = auditExecutionEventAuthentication{}
	if err := validateAuditExecutionHistory(fixture.Authority, fixture.Intent, []auditExecutionEvent{fixture.Events[0], fixture.Events[1], unsigned}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("unsigned finalizing history error = %v, want %v", err, ErrAuthentication)
	}
	tampered := finalizing
	tampered.EvidenceHash = testHash("tampered-finalizing-evidence")
	if err := validateAuditExecutionHistory(fixture.Authority, fixture.Intent, []auditExecutionEvent{fixture.Events[0], fixture.Events[1], tampered}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("tampered signed finalizing history error = %v, want %v", err, ErrAuthentication)
	}
	directCompleted := []auditExecutionEvent{fixture.Events[0], fixture.Events[1], fixture.Events[3]}
	if err := validateAuditExecutionHistory(fixture.Authority, fixture.Intent, directCompleted); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("running directly to completed error = %v, want %v", err, ErrAuthentication)
	}

	journal, err := openServerJournal(filepath.Join(t.TempDir(), "finalizing-replay.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
		t.Fatal(err)
	}
	if err := journal.storeAuditIntent(context.Background(), fixture.Intent); err != nil {
		t.Fatal(err)
	}
	for _, event := range history {
		if err := journal.appendAuditEvent(context.Background(), event); err != nil {
			t.Fatalf("append %s: %v", event.State, err)
		}
	}
	if err := journal.appendAuditEvent(context.Background(), finalizing); err != nil {
		t.Fatalf("exact finalizing replay: %v", err)
	}
	conflict := finalizing
	conflict.OutputSize++
	conflict.EventHash = ""
	conflict.Authentication = auditExecutionEventAuthentication{}
	conflict, err = sealAuditExecutionEvent(conflict)
	if err != nil {
		t.Fatal(err)
	}
	conflict, err = fixture.Authority.authenticateEvent(conflict)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.appendAuditEvent(context.Background(), conflict); !errors.Is(err, ErrReplay) {
		t.Fatalf("conflicting finalizing replay error = %v, want %v", err, ErrReplay)
	}
}

func TestAuditJournalStartupRejectsIntegrityCleanCanonicallyRehashedHistoryTamper(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		sequence int
		mutate   func(*testing.T, *auditExecutionEvent)
	}{
		{name: "event ID", sequence: 1, mutate: func(t *testing.T, event *auditExecutionEvent) {
			event.EventID = "audit_event_rehashed_wrong_001"
		}},
		{name: "attempt cap", sequence: 1, mutate: func(t *testing.T, event *auditExecutionEvent) { event.Attempt = 2 }},
		{name: "command", sequence: 2, mutate: func(t *testing.T, event *auditExecutionEvent) {
			event.CommandDescriptorHash = testHash("startup-command-drift")
		}},
		{name: "PID", sequence: 3, mutate: func(t *testing.T, event *auditExecutionEvent) {
			event.PID++
			event.PGID = event.PID
			resealCompletedAuditEvidenceForHistoryTest(t, event, func(report *auditEvidenceReport) {
				report.PID, report.PGID = event.PID, event.PGID
			})
		}},
		{name: "path", sequence: 2, mutate: func(t *testing.T, event *auditExecutionEvent) {
			event.OutputPath = "/private/output/other_attempt_1/audit-output.json"
		}},
		{name: "timestamp", sequence: 2, mutate: func(t *testing.T, event *auditExecutionEvent) {
			event.OccurredAt = "2026-07-25T23:59:59Z"
		}},
		{name: "typed evidence", sequence: 3, mutate: func(t *testing.T, event *auditExecutionEvent) {
			resealCompletedAuditEvidenceForHistoryTest(t, event, func(report *auditEvidenceReport) {
				report.EnvelopeHash = testHash("startup-evidence-envelope-drift")
			})
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.sqlite")
			journal, err := openServerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			fixture := validAuditExecutionHistoryForTest(t)
			if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
				t.Fatal(err)
			}
			intent, events := fixture.Intent, fixture.Events
			if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if err := journal.appendAuditEvent(context.Background(), event); err != nil {
					t.Fatal(err)
				}
			}
			event := events[testCase.sequence-1]
			testCase.mutate(t, &event)
			if event.EventHash != "" {
				event = resealAuditEventForHistoryTest(t, event)
			}
			encoded, err := marshalCanonical(event)
			if err != nil {
				t.Fatal(err)
			}
			mutateJournalProtectedRows(t, journal.db,
				`DROP TRIGGER trusted_supervisor_audit_events_no_update`,
				`UPDATE trusted_supervisor_audit_events SET event_bytes = ?, event_bytes_hash = ?, created_at = ? WHERE intent_hash = ? AND sequence = ?`,
				[]any{encoded, hashJournalBytes(encoded), event.OccurredAt, intent.IntentHash, testCase.sequence},
				`CREATE TRIGGER trusted_supervisor_audit_events_no_update BEFORE UPDATE ON trusted_supervisor_audit_events BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor audit event'); END`)
			assertSQLiteIntegrityClean(t, journal.db)
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			assertAuditJournalAuthorityFailure(t, path, fixture.Policy, fixture.Signing)
		})
	}
}

func TestAuditCancellationOutcomeRejectsIntegrityCleanTerminalBindingTamper(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		column string
		value  string
	}{
		{name: "event hash", column: "audit_event_hash", value: testHash("forged-cancellation-event")},
		{name: "completion timestamp", column: "completed_at", value: "2026-07-26T00:00:03Z"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cancellation-outcome.sqlite")
			journal, err := openServerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			fixture := validAuditExecutionHistoryForTest(t)
			if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
				t.Fatal(err)
			}
			intent := fixture.Intent
			if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
				t.Fatal(err)
			}
			requestBytes := []byte(`{"operation":"cancel","request":"history"}`)
			cancellation := mustSealAuditCancellationIntentForTest(t, auditCancellationIntent{
				SchemaVersion: auditCancellationIntentSchemaVersion, IntentID: "audit_cancellation_history_001",
				RequestHash: testHash("history-cancellation-request"), RequestBytes: requestBytes,
				OperationKey: "cancel:" + intent.ReceiptHash, RequestNonceHash: testHash("history-cancellation-request-nonce"),
				ResponseNonceHash: testHash("history-cancellation-response-nonce"), ExclusivityNonceHash: testHash("history-cancellation-exclusive-nonce"),
				EnvelopeHash: intent.EnvelopeHash, HandoffID: intent.HandoffID, ReceiptHash: intent.ReceiptHash,
				CancellationHash: testHash("history-cancellation"), Attempt: 1, State: auditCancellationStateRequested,
				RequestedAt: "2026-07-26T00:00:01Z",
			})
			if _, err := journal.requestAuditCancellation(context.Background(), cancellation); err != nil {
				t.Fatal(err)
			}
			promptHash := auditPromptSHA256(false)
			commandHash, err := auditCommandDescriptorHash(fixture.Entry, promptHash, intent.RunID, auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, fixture.Entry, 1)
			cancelled := mustSealAuditEventForTest(t, auditExecutionEvent{
				SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditExecutionEventID(intent, 1), IntentHash: intent.IntentHash,
				Sequence: 1, State: auditStateCancelled, Attempt: 1, CommandDescriptorHash: commandHash,
				PromptSHA256: promptHash, SessionRunID: intent.RunID,
				OccurredAt: "2026-07-26T00:00:02Z", FailureClass: "operator_cancelled_before_start",
				WorkPath: workPath, OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
			})
			if _, err := journal.completeAuditCancellation(context.Background(), cancellation, cancelled, auditCancellationOutcomeCompleted, func() ([]byte, error) {
				return []byte(`{"response":"cancelled"}`), nil
			}); err != nil {
				t.Fatal(err)
			}
			mutateJournalProtectedRows(t, journal.db,
				`DROP TRIGGER trusted_supervisor_cancellation_outcomes_no_update`,
				`UPDATE trusted_supervisor_cancellation_outcomes SET `+testCase.column+` = ? WHERE intent_hash = ?`,
				[]any{testCase.value, cancellation.IntentHash},
				`CREATE TRIGGER trusted_supervisor_cancellation_outcomes_no_update BEFORE UPDATE ON trusted_supervisor_cancellation_outcomes BEGIN SELECT RAISE(ABORT, 'immutable trusted supervisor cancellation outcome'); END`)
			assertSQLiteIntegrityClean(t, journal.db)
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			assertAuditJournalAuthorityFailure(t, path, fixture.Policy, fixture.Signing)
		})
	}
}

type auditExecutionHistoryFixture struct {
	Intent    auditExecutionIntent
	Events    []auditExecutionEvent
	Policy    *executionPolicy
	Signing   *serverSigningMaterial
	Authority *auditJournalAuthority
	Entry     executionPolicyEntry
}

func validAuditExecutionHistoryForTest(t *testing.T) auditExecutionHistoryFixture {
	t.Helper()
	now := time.Date(2026, 7, 26, 1, 0, 0, 0, time.UTC)
	serverMaterial := newServerTestMaterial(t, now)
	signing, err := loadServerSigningMaterial(serverMaterial.bundlePath, serverMaterial.keyBundlePath, uint32(os.Getuid()), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signing.Close)
	policy, err := loadExecutionPolicyForTest(serverMaterial.executionPolicyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	authority, err := newAuditJournalAuthority(policy, signing)
	if err != nil {
		t.Fatal(err)
	}
	entry := policy.entries[serverMaterial.fixture.envelope.LaunchSpecHash]
	intent := mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion, IntentID: "audit_intent_history_001",
		EnvelopeHash: testHash("history-envelope"), LaunchSpecHash: entry.LaunchSpecHash,
		HandoffID: "remote_handoff_history_001", ReceiptHash: testHash("history-receipt"), TaskID: entry.TaskID,
		AttemptCap: entry.AttemptCap, PolicyHash: entry.PolicyHash, RouteMappingHash: entry.RouteMappingHash,
		RepositoryIdentityHash: entry.RepositoryIdentityHash, GitCommit: entry.GitCommit, GitTree: entry.GitTree,
		SourceArchiveSHA256: entry.SourceArchiveSHA256, WrapperSHA256: entry.Wrapper.SHA256,
		RunID: "audit_run_history_001", CreatedAt: "2026-07-26T00:00:00Z",
	})
	workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, entry, 1)
	promptHash := auditPromptSHA256(false)
	commandHash, err := auditCommandDescriptorHash(entry, promptHash, intent.RunID, auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	prepared := mustSealAuditEventForTest(t, auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditEventID(intent, 1), IntentHash: intent.IntentHash,
		Sequence: 1, State: auditStatePrepared, Attempt: 1, CommandDescriptorHash: commandHash,
		PromptSHA256: promptHash, SessionRunID: intent.RunID, OccurredAt: "2026-07-26T00:00:01Z",
		WorkPath: workPath, OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
	})
	running := mustSealAuditEventForTest(t, auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditEventID(intent, 2), IntentHash: intent.IntentHash,
		Sequence: 2, State: auditStateRunning, Attempt: 1, CommandDescriptorHash: commandHash,
		PromptSHA256: promptHash, SessionRunID: intent.RunID, OccurredAt: "2026-07-26T00:00:02Z",
		PID: 1234, PGID: 1234, ProcessStartIdentity: "100:200", ProcessStartedAt: "2026-07-26T00:00:01.5Z",
		WorkPath: workPath, OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
	})
	modelReport := auditModelReport{
		Findings:      []auditModelFinding{{Code: "READ_001", Line: 7, Message: "unsafe read", Path: "internal/a.go", Severity: "high"}},
		SchemaVersion: auditModelReportSchemaVersion, Summary: "One finding.", Verdict: "rejected",
	}
	modelBytes, err := marshalCanonical(modelReport)
	if err != nil {
		t.Fatal(err)
	}
	finalizing := auditExecutionEvent{
		SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditEventID(intent, 3), IntentHash: intent.IntentHash,
		Sequence: 3, State: auditStateFinalizing, Attempt: 1, CommandDescriptorHash: commandHash,
		PromptSHA256: promptHash, SessionRunID: intent.RunID, OccurredAt: "2026-07-26T00:00:03Z",
		PID: 1234, PGID: 1234, ProcessStartIdentity: "100:200", ProcessStartedAt: running.ProcessStartedAt,
		ProcessFinishedAt: "2026-07-26T00:00:02.5Z", ExitCode: 0, StdoutSHA256: testHash("history-stdout"),
		StderrSHA256: testHash("history-stderr"), OutputSHA256: hashJournalBytes(modelBytes), OutputSize: int64(len(modelBytes)),
		WorkPath: workPath, OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
	}
	ownedRoots := syntheticAuditOwnedRootsForPhaseBTest(t, policy, intent, entry, 1)
	report := auditEvidenceReport{
		SchemaVersion: auditEvidenceSchemaVersion, IntentHash: intent.IntentHash, EnvelopeHash: intent.EnvelopeHash,
		LaunchSpecHash: intent.LaunchSpecHash, HandoffID: intent.HandoffID, ReceiptHash: intent.ReceiptHash, TaskID: intent.TaskID,
		RunID: intent.RunID, Attempt: 1, AttemptCap: intent.AttemptCap, PolicyHash: intent.PolicyHash,
		RouteMappingHash: intent.RouteMappingHash, RepositoryIdentityHash: intent.RepositoryIdentityHash,
		SourceArchiveSHA256: intent.SourceArchiveSHA256, GitCommit: intent.GitCommit, GitTree: intent.GitTree, WrapperSHA256: intent.WrapperSHA256,
		OMPVersion: entry.OMPVersion, OMPNativeAddonSHA256: entry.OMPNativeAddon.SHA256,
		FinalizingEventID: finalizing.EventID, FinalizingEventSequence: finalizing.Sequence, FinalizingEventOccurredAt: finalizing.OccurredAt,
		CommandDescriptorHash: commandHash, PromptSHA256: promptHash, SessionRunID: intent.RunID,
		SandboxProfileSHA256: testHash("history-sandbox"), PID: finalizing.PID, PGID: finalizing.PGID,
		ProcessStartIdentity: finalizing.ProcessStartIdentity, ProcessStartedAt: finalizing.ProcessStartedAt,
		ProcessFinishedAt: finalizing.ProcessFinishedAt, ExitCode: 0, StdoutSHA256: finalizing.StdoutSHA256,
		StderrSHA256: finalizing.StderrSHA256, OutputSHA256: finalizing.OutputSHA256, OutputSize: finalizing.OutputSize,
		ModelReportSHA256: finalizing.OutputSHA256, ModelReport: modelReport,
		TestsRun: []auditEvidenceTest{{ID: entry.AllowedTests[0].ID, CommandSHA256: entry.AllowedTests[0].CommandSHA256, ExitCode: 0,
			SandboxProfileSHA256: testHash("history-test-sandbox"), StdoutSHA256: testHash("history-test-stdout"),
			StdoutSize: 10, StderrSHA256: testHash("history-test-stderr"), StderrSize: 0}},
		OwnedRoots: ownedRoots, WorkPath: workPath, OutputPath: outputPath, SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
	}
	evidenceBytes, err := marshalCanonical(report)
	if err != nil {
		t.Fatal(err)
	}
	finalizing.EvidenceJSON = string(evidenceBytes)
	finalizing.EvidenceHash = hashJournalBytes(evidenceBytes)
	finalizing = mustSealAuditEventForTest(t, finalizing)
	completed := finalizing
	completed.EventID = auditEventID(intent, 4)
	completed.EventHash = ""
	completed.Sequence = 4
	completed.State = auditStateCompleted
	completed.OccurredAt = "2026-07-26T00:00:04Z"
	completed.FinalizingEventHash = finalizing.EventHash
	completed.Authentication = auditExecutionEventAuthentication{}
	completed = mustSealAuditEventForTest(t, completed)
	events := []auditExecutionEvent{prepared, running, finalizing, completed}
	for index := range events {
		events[index], err = authority.authenticateEvent(events[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	return auditExecutionHistoryFixture{
		Intent: intent, Events: events, Policy: policy,
		Signing: signing, Authority: authority, Entry: entry,
	}
}

func auditEventID(intent auditExecutionIntent, sequence int) string {
	return "audit_event_" + hashIDFragment(intent.IntentHash) + "_00" + string(rune('0'+sequence))
}

func resealAuditEventForHistoryTest(t *testing.T, event auditExecutionEvent) auditExecutionEvent {
	t.Helper()
	event.EventHash = ""
	return mustSealAuditEventForTest(t, event)
}

func resealCompletedAuditEvidenceForHistoryTest(t *testing.T, event *auditExecutionEvent, mutate func(*auditEvidenceReport)) {
	t.Helper()
	var report auditEvidenceReport
	if err := decodeCanonical([]byte(event.EvidenceJSON), &report); err != nil {
		t.Fatal(err)
	}
	mutate(&report)
	encoded, err := marshalCanonical(report)
	if err != nil {
		t.Fatal(err)
	}
	event.EvidenceJSON = string(encoded)
	event.EvidenceHash = hashJournalBytes(encoded)
	*event = resealAuditEventForHistoryTest(t, *event)
}

func assertAuditJournalAuthorityFailure(t *testing.T, path string, policy *executionPolicy, signing *serverSigningMaterial) {
	t.Helper()
	journal, err := openServerJournal(path)
	if err == nil {
		err = journal.bindAuditAuthority(policy, signing)
		_ = journal.Close()
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("audit journal authority error = %v, want %v", err, ErrAuthentication)
	}
}

func finalizingAuditExecutionHistoryForTest(t *testing.T) auditExecutionHistoryFixture {
	t.Helper()
	fixture := validAuditExecutionHistoryForTest(t)
	fixture.Events = fixture.Events[:len(fixture.Events)-1]
	return fixture
}
