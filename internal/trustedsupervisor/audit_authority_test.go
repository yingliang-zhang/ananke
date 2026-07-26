package trustedsupervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type auditAuthorityTestFixture struct {
	policy  *executionPolicy
	signing *serverSigningMaterial
}

func newDeterministicAuditAuthorityTestFixture(t *testing.T, policy *executionPolicy) auditAuthorityTestFixture {
	t.Helper()
	now := time.Date(2026, 7, 26, 1, 0, 0, 0, time.UTC)
	material := newServerTestMaterial(t, now)
	return newAuditAuthorityTestFixture(t, policy, material, now)
}

func newServerAuditAuthorityTestFixture(t *testing.T, material serverTestMaterial, now time.Time) auditAuthorityTestFixture {
	t.Helper()
	policy, err := loadExecutionPolicyForTest(material.executionPolicyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	return newAuditAuthorityTestFixture(t, policy, material, now)
}

func newAuditAuthorityTestFixture(t *testing.T, policy *executionPolicy, material serverTestMaterial, now time.Time) auditAuthorityTestFixture {
	t.Helper()
	signing, err := loadServerSigningMaterial(material.bundlePath, material.keyBundlePath, uint32(os.Getuid()), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signing.Close)
	return auditAuthorityTestFixture{policy: policy, signing: signing}
}

func (fixture auditAuthorityTestFixture) bind(t *testing.T, journal *serverJournal) {
	t.Helper()
	if err := journal.bindAuditAuthority(fixture.policy, fixture.signing); err != nil {
		t.Fatal(err)
	}
}

func TestAuditJournalRejectsIntentBeforeAuthorityBinding(t *testing.T) {
	journal, err := openServerJournal(filepath.Join(t.TempDir(), "audit-authority.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	fixture := validAuditExecutionHistoryForTest(t)
	if err := journal.storeAuditIntent(context.Background(), fixture.Intent); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("store without audit authority error = %v, want %v", err, ErrAuthentication)
	}
}

func TestAuditJournalAcceptsIntentAfterExplicitAuthorityBinding(t *testing.T) {
	now := time.Date(2026, 7, 26, 1, 0, 0, 0, time.UTC)
	serverMaterial := newServerTestMaterial(t, now)
	signingMaterial, err := loadServerSigningMaterial(serverMaterial.bundlePath, serverMaterial.keyBundlePath, uint32(os.Getuid()), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signingMaterial.Close)
	policy, err := loadExecutionPolicyForTest(serverMaterial.executionPolicyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := openServerJournal(filepath.Join(serverMaterial.directory, "bound-audit-authority.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.bindAuditAuthority(policy, signingMaterial); err != nil {
		t.Fatalf("bind explicit audit authority: %v", err)
	}

	entry := policy.entries[serverMaterial.fixture.envelope.LaunchSpecHash]
	intent := auditIntentForPolicyTest(t, entry, "bound")
	if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
		t.Fatalf("store with explicit audit authority: %v", err)
	}
}

func TestAuditJournalRejectsConsistentlyResealedPolicyDescriptorAndRootDrift(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*auditExecutionEvent)
	}{
		{name: "command descriptor", mutate: func(event *auditExecutionEvent) {
			event.CommandDescriptorHash = testHash("correlated-command-drift")
		}},
		{name: "all five roots", mutate: func(event *auditExecutionEvent) {
			event.WorkPath = "/private/relocated-work/" + filepath.Base(filepath.Dir(event.WorkPath)) + "/source"
			event.OutputPath = "/private/relocated-output/" + filepath.Base(filepath.Dir(event.OutputPath)) + "/audit-output.json"
			event.SessionPath = "/private/relocated-session/" + filepath.Base(event.SessionPath)
			event.PromptPath = "/private/relocated-prompt/" + filepath.Base(filepath.Dir(event.PromptPath)) + "/audit-prompt.txt"
			event.TemporaryPath = "/private/relocated-tmp/" + filepath.Base(event.TemporaryPath)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			journal, entry := newBoundAuditJournalForAuthorityTest(t, testCase.name)
			intent := auditIntentForPolicyTest(t, entry, "policy_"+strings.ReplaceAll(testCase.name, " ", "_"))
			if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
				t.Fatal(err)
			}
			workPath, outputPath, sessionPath, promptPath, temporaryPath := auditAttemptPaths(intent, entry, 1)
			promptHash := hashJournalBytes([]byte(readOnlyAuditPromptTemplate))
			descriptorHash, err := auditCommandDescriptorHash(entry, promptHash, intent.RunID, auditResume{})
			if err != nil {
				t.Fatal(err)
			}
			prepared := auditExecutionEvent{
				SchemaVersion: auditExecutionEventSchemaVersion, EventID: auditExecutionEventID(intent, 1), IntentHash: intent.IntentHash,
				Sequence: 1, State: auditStatePrepared, Attempt: 1, CommandDescriptorHash: descriptorHash,
				PromptSHA256: promptHash, SessionRunID: intent.RunID, ResumeSessionUUID: "", SynthesizeOnly: false,
				OccurredAt: "2026-07-26T01:00:01Z", WorkPath: workPath, OutputPath: outputPath,
				SessionPath: sessionPath, PromptPath: promptPath, TemporaryPath: temporaryPath,
			}
			testCase.mutate(&prepared)
			prepared = mustSealAuditEventForTest(t, prepared)
			if err := journal.appendAuditEvent(context.Background(), prepared); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("append correlated %s drift error = %v, want %v", testCase.name, err, ErrAuthentication)
			}
		})
	}
}

func TestAuditJournalAuthenticatesEventsWithBoundServerKey(t *testing.T) {
	fixture := validAuditExecutionHistoryForTest(t)
	path := filepath.Join(t.TempDir(), "signed-history.sqlite")
	journal, err := openServerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
		t.Fatal(err)
	}
	if err := journal.storeAuditIntent(context.Background(), fixture.Intent); err != nil {
		t.Fatal(err)
	}
	if err := journal.appendAuditEvent(context.Background(), fixture.Events[0]); err != nil {
		t.Fatal(err)
	}
	_, events, err := journal.loadAuditExecution(context.Background(), fixture.Intent.EnvelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Authentication.SchemaVersion != auditExecutionEventAuthenticationSchemaVersion ||
		events[0].Authentication.EventHash != events[0].EventHash || events[0].Authentication.Signature == "" ||
		events[0].Authentication.SignerKeySPKISHA256 != fixture.Signing.signerSPKI ||
		events[0].Authentication.SignerRootID != fixture.Signing.rootID {
		t.Fatalf("signed audit event authentication = %+v", events)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openServerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.bindAuditAuthority(fixture.Policy, fixture.Signing); err != nil {
		t.Fatalf("restart valid signed history: %v", err)
	}
}

func newBoundAuditJournalForAuthorityTest(t *testing.T, suffix string) (*serverJournal, executionPolicyEntry) {
	t.Helper()
	now := time.Date(2026, 7, 26, 1, 0, 0, 0, time.UTC)
	serverMaterial := newServerTestMaterial(t, now)
	signingMaterial, err := loadServerSigningMaterial(serverMaterial.bundlePath, serverMaterial.keyBundlePath, uint32(os.Getuid()), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signingMaterial.Close)
	policy, err := loadExecutionPolicyForTest(serverMaterial.executionPolicyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := openServerJournal(filepath.Join(serverMaterial.directory, "audit-authority-"+strings.ReplaceAll(suffix, " ", "_")+".sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	if err := journal.bindAuditAuthority(policy, signingMaterial); err != nil {
		t.Fatal(err)
	}
	return journal, policy.entries[serverMaterial.fixture.envelope.LaunchSpecHash]
}

func auditIntentForPolicyTest(t *testing.T, entry executionPolicyEntry, suffix string) auditExecutionIntent {
	t.Helper()
	return mustSealAuditIntentForTest(t, auditExecutionIntent{
		SchemaVersion: auditExecutionIntentSchemaVersion,
		IntentID:      "audit_intent_authority_" + suffix, EnvelopeHash: testHash("authority-envelope-" + suffix),
		LaunchSpecHash: entry.LaunchSpecHash, HandoffID: "remote_handoff_authority_" + suffix,
		ReceiptHash: testHash("authority-receipt-" + suffix), TaskID: entry.TaskID, AttemptCap: entry.AttemptCap,
		PolicyHash: entry.PolicyHash, RouteMappingHash: entry.RouteMappingHash, RepositoryIdentityHash: entry.RepositoryIdentityHash,
		GitCommit: entry.GitCommit, GitTree: entry.GitTree, SourceArchiveSHA256: entry.SourceArchiveSHA256,
		WrapperSHA256: entry.Wrapper.SHA256, RunID: "audit_run_authority_" + suffix, CreatedAt: "2026-07-26T01:00:00Z",
	})
}
