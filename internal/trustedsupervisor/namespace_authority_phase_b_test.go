package trustedsupervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func syntheticAuditOwnedRootsForPhaseBTest(
	t *testing.T,
	policy *executionPolicy,
	intent auditExecutionIntent,
	entry executionPolicyEntry,
	attempt int,
) []auditOwnedRootIdentity {
	t.Helper()
	specs, err := expectedAuditFinalizingOwnedRootSpecs(intent.RunID, attempt, entry)
	if err != nil {
		t.Fatal(err)
	}
	identities := make([]auditOwnedRootIdentity, 0, len(specs))
	byPath := make(map[string]auditOwnedRootIdentity, len(specs))
	for index, spec := range specs {
		var parent auditNamespaceDirectoryIdentity
		if nested, ok := byPath[spec.parentPath]; ok {
			parent = namespaceDirectoryIdentityFromOwned(nested)
		} else {
			lease, leaseErr := policy.namespaceAuthority.Duplicate(spec.parentPath)
			if leaseErr != nil {
				t.Fatal(leaseErr)
			}
			parent = lease.parent
			if closeErr := lease.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		identity := auditOwnedRootIdentity{
			Role: spec.role, Path: spec.path, ParentPath: spec.parentPath,
			Device: "9001", Inode: strconv.Itoa(100000 + index), OwnerUID: uint32(os.Getuid()), OwnerGID: uint32(os.Getgid()),
			Mode: uint32(unix.S_IFDIR) | 0o700, ParentDevice: statDecimal(parent.Device), ParentInode: statDecimal(parent.Inode),
			ParentOwnerUID: parent.OwnerUID, ParentOwnerGID: parent.OwnerGID, ParentMode: parent.Mode, CleanupRoot: spec.cleanupRoot,
		}
		identities = append(identities, identity)
		byPath[identity.Path] = identity
	}
	return identities
}

func TestAuditEvidenceOwnedRootsRejectsTamperReorderOmissionDuplicateAndParentMutation(t *testing.T) {
	fixture := validAuditExecutionHistoryForTest(t)
	finalizing := fixture.Events[2]
	var original auditEvidenceReport
	if err := decodeCanonical([]byte(finalizing.EvidenceJSON), &original); err != nil {
		t.Fatal(err)
	}
	if len(original.OwnedRoots) < 4 {
		t.Fatalf("owned root inventory length = %d, want complete signed inventory", len(original.OwnedRoots))
	}

	t.Run("evidence bytes tamper", func(t *testing.T) {
		tampered := finalizing
		tampered.EvidenceJSON += " "
		if _, err := decodeAuditEvidenceReport(fixture.Intent, tampered); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("tampered evidence error = %v, want %v", err, ErrAuthentication)
		}
	})

	for _, testCase := range []struct {
		name   string
		mutate func(*auditEvidenceReport)
	}{
		{name: "reordered", mutate: func(report *auditEvidenceReport) {
			report.OwnedRoots[0], report.OwnedRoots[1] = report.OwnedRoots[1], report.OwnedRoots[0]
		}},
		{name: "omitted role", mutate: func(report *auditEvidenceReport) {
			report.OwnedRoots = append([]auditOwnedRootIdentity(nil), report.OwnedRoots[1:]...)
		}},
		{name: "duplicate path", mutate: func(report *auditEvidenceReport) {
			report.OwnedRoots[1].Path = report.OwnedRoots[0].Path
		}},
		{name: "parent identity mutation", mutate: func(report *auditEvidenceReport) {
			for index := range report.OwnedRoots {
				if report.OwnedRoots[index].ParentPath == report.OwnedRoots[2].Path {
					report.OwnedRoots[index].ParentInode = "999999999"
					return
				}
			}
			t.Fatal("fixture has no nested owned root")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			report := original
			report.OwnedRoots = append([]auditOwnedRootIdentity(nil), original.OwnedRoots...)
			testCase.mutate(&report)
			mutated, sealErr := resealAuditEvidenceEventForPhaseBTest(t, finalizing, report)
			if sealErr == nil {
				if _, err := decodeAuditEvidenceReport(fixture.Intent, mutated); !errors.Is(err, ErrAuthentication) {
					t.Fatalf("mutated owned roots error = %v, want %v", err, ErrAuthentication)
				}
			}
		})
	}
}

func TestPathOnlyFinalizingEventCannotRecoverOrComplete(t *testing.T) {
	fixture := finalizingAuditExecutionHistoryForTest(t)
	pathOnly := fixture.Events[len(fixture.Events)-1]
	var report auditEvidenceReport
	if err := decodeCanonical([]byte(pathOnly.EvidenceJSON), &report); err != nil {
		t.Fatal(err)
	}
	report.OwnedRoots = nil
	var err error
	pathOnly, err = resealAuditEvidenceEventForPhaseBTest(t, pathOnly, report)
	if err != nil {
		return
	}
	pathOnly, err = fixture.Authority.authenticateEvent(pathOnly)
	if err != nil {
		t.Fatal(err)
	}

	journal, err := openServerJournal(filepath.Join(t.TempDir(), "path-only-finalizing.sqlite"))
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
	for _, event := range fixture.Events[:len(fixture.Events)-1] {
		if err := journal.appendAuditEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	appendErr := journal.appendAuditEvent(context.Background(), pathOnly)
	if appendErr == nil {
		executor, createErr := newUnrecoveredAuditExecutor(journal, fixture.Policy)
		if createErr != nil {
			t.Fatal(createErr)
		}
		events := append(append([]auditExecutionEvent(nil), fixture.Events[:len(fixture.Events)-1]...), pathOnly)
		resumeErr := executor.resumeFinalizing(fixture.Intent, &events, fixture.Entry, auditInvocation{RunID: filepath.Base(filepath.Dir(pathOnly.PromptPath))})
		executor.cancel()
		if !errors.Is(resumeErr, ErrAuthentication) {
			t.Fatalf("path-only finalizing resume error = %v, want %v", resumeErr, ErrAuthentication)
		}
	}
	_, events, loadErr := journal.loadAuditExecution(context.Background(), fixture.Intent.EnvelopeHash)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(events) != 0 && events[len(events)-1].State == auditStateCompleted {
		t.Fatalf("path-only finalizing authority completed: %+v", events)
	}
}

func TestFinalizingOwnedRootDecoysRemainNonterminalLiveAndRestart(t *testing.T) {
	for _, recovery := range []string{"live", "restart"} {
		t.Run(recovery, func(t *testing.T) {
			probe := finalizingAuditExecutionHistoryForTest(t)
			probeRoots := materializeSignedFinalizingRootsForPhaseBTest(t, &probe)
			roles := make([]string, len(probeRoots))
			for index, identity := range probeRoots {
				roles[index] = identity.Role + "_" + strconv.Itoa(index)
			}
			for rootIndex, name := range roles {
				t.Run(name, func(t *testing.T) {
					fixture := finalizingAuditExecutionHistoryForTest(t)
					roots := materializeSignedFinalizingRootsForPhaseBTest(t, &fixture)
					target := roots[rootIndex]
					retained := target.Path + ".retained-original"
					if err := os.Rename(target.Path, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(target.Path, 0o700); err != nil {
						t.Fatal(err)
					}
					decoyArtifact := filepath.Join(target.Path, "decoy-must-survive")
					if err := os.WriteFile(decoyArtifact, []byte("decoy"), 0o600); err != nil {
						t.Fatal(err)
					}

					journal, err := openServerJournal(filepath.Join(t.TempDir(), "owned-root-decoy.sqlite"))
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
					for _, event := range fixture.Events {
						if err := journal.appendAuditEvent(context.Background(), event); err != nil {
							t.Fatal(err)
						}
					}
					executor, err := newUnrecoveredAuditExecutor(journal, fixture.Policy)
					if err != nil {
						t.Fatal(err)
					}
					started := false
					executor.hooks.beforeStart = func(string) { started = true }
					events := append([]auditExecutionEvent(nil), fixture.Events...)
					if recovery == "live" {
						_ = executor.resumeFinalizing(fixture.Intent, &events, fixture.Entry, auditInvocation{RunID: filepath.Base(filepath.Dir(events[len(events)-1].PromptPath))})
					} else {
						executor.recoverExisting(fixture.Intent, events, executor.newActive())
					}
					executor.cancel()
					_, persisted, err := journal.loadAuditExecution(context.Background(), fixture.Intent.EnvelopeHash)
					if err != nil {
						t.Fatal(err)
					}
					if started || len(persisted) != len(fixture.Events) || persisted[len(persisted)-1].State != auditStateFinalizing {
						t.Fatalf("decoy recovery exposed completion or reran effects: started=%t events=%+v", started, persisted)
					}
					for _, path := range []string{retained, decoyArtifact} {
						if _, err := os.Stat(path); err != nil {
							t.Fatalf("decoy recovery altered %q: %v", path, err)
						}
					}
				})
			}
		})
	}
}

func TestFinalizingOwnedRootAbsentUnderUnstableParentRemainsNonterminalLiveAndRestart(t *testing.T) {
	for _, recovery := range []string{"live", "restart"} {
		t.Run(recovery, func(t *testing.T) {
			assertFinalizingRenameWithoutDecoyRemainsNonterminal(t, recovery, nil)
		})
	}
}

func assertFinalizingRenameWithoutDecoyRemainsNonterminal(
	t *testing.T,
	recovery string,
	authorizeRename func(*testing.T, *auditExecutionHistoryFixture, auditOwnedRootIdentity),
) {
	t.Helper()
	fixture := finalizingAuditExecutionHistoryForTest(t)
	roots := materializeSignedFinalizingRootsForPhaseBTest(t, &fixture)
	target := roots[0]
	if authorizeRename != nil {
		authorizeRename(t, &fixture, target)
	}
	// This is the deterministic fail-closed seam for a hierarchy whose ACL
	// authority could not be proved safe at admission.
	fixture.Policy.namespaceAuthority.stable = false
	retained := target.Path + ".retained-original"
	if err := os.Rename(target.Path, retained); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { makeTreeRemovableForTest(retained) })

	journal, err := openServerJournal(filepath.Join(t.TempDir(), "owned-root-absent.sqlite"))
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
	for _, event := range fixture.Events {
		if err := journal.appendAuditEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	executor, err := newUnrecoveredAuditExecutor(journal, fixture.Policy)
	if err != nil {
		t.Fatal(err)
	}
	started := false
	executor.hooks.beforeStart = func(string) { started = true }
	events := append([]auditExecutionEvent(nil), fixture.Events...)
	if recovery == "live" {
		_ = executor.resumeFinalizing(fixture.Intent, &events, fixture.Entry, auditInvocation{RunID: filepath.Base(filepath.Dir(events[len(events)-1].PromptPath))})
	} else {
		executor.recoverExisting(fixture.Intent, events, executor.newActive())
	}
	executor.cancel()
	_, persisted, err := journal.loadAuditExecution(context.Background(), fixture.Intent.EnvelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	if started || len(persisted) != len(fixture.Events) || persisted[len(persisted)-1].State != auditStateFinalizing {
		t.Fatalf("absent-root recovery exposed completion or reran effects: started=%t events=%+v", started, persisted)
	}
	if _, err := os.Lstat(target.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected signed name was recreated: %v", err)
	}
	if _, err := os.Stat(retained); err != nil {
		t.Fatalf("renamed signed inode was altered: %v", err)
	}
}

func materializeSignedFinalizingRootsForPhaseBTest(t *testing.T, fixture *auditExecutionHistoryFixture) []auditOwnedRootIdentity {
	t.Helper()
	if fixture == nil || len(fixture.Events) == 0 {
		t.Fatal("missing finalizing fixture")
	}
	finalizingIndex := len(fixture.Events) - 1
	finalizing := fixture.Events[finalizingIndex]
	var report auditEvidenceReport
	if err := decodeCanonical([]byte(finalizing.EvidenceJSON), &report); err != nil {
		t.Fatal(err)
	}
	specs, err := expectedAuditFinalizingOwnedRootSpecs(fixture.Intent.RunID, finalizing.Attempt, fixture.Entry)
	if err != nil {
		t.Fatal(err)
	}
	identities := make([]auditOwnedRootIdentity, 0, len(specs))
	byPath := make(map[string]auditOwnedRootIdentity, len(specs))
	for _, spec := range specs {
		var identity auditOwnedRootIdentity
		if parent, nested := byPath[spec.parentPath]; nested {
			identity, err = fixture.Policy.namespaceAuthority.mkdirAndCaptureOwnedChild(parent, filepath.Base(spec.path), spec.role, spec.cleanupRoot, false)
		} else {
			lease, leaseErr := fixture.Policy.namespaceAuthority.Duplicate(spec.parentPath)
			if leaseErr != nil {
				t.Fatal(leaseErr)
			}
			if err = lease.Mkdir(filepath.Base(spec.path), 0o700); err == nil {
				identity, err = lease.Capture(filepath.Base(spec.path), spec.role, spec.cleanupRoot)
			}
			if closeErr := lease.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
		byPath[identity.Path] = identity
	}
	report.OwnedRoots = identities
	finalizing, err = resealAuditEvidenceEventForPhaseBTest(t, finalizing, report)
	if err != nil {
		t.Fatal(err)
	}
	finalizing, err = fixture.Authority.authenticateEvent(finalizing)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Events[finalizingIndex] = finalizing
	return identities
}

func resealAuditEvidenceEventForPhaseBTest(t *testing.T, event auditExecutionEvent, report auditEvidenceReport) (auditExecutionEvent, error) {
	t.Helper()
	encoded, err := marshalCanonical(report)
	if err != nil {
		t.Fatal(err)
	}
	event.EvidenceJSON = string(encoded)
	event.EvidenceHash = hashJournalBytes(encoded)
	event.EventHash = ""
	event.Authentication = auditExecutionEventAuthentication{}
	return sealAuditExecutionEvent(event)
}
