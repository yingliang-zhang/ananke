package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestP4EvidenceAdmissionPersistsImmutableBundleVerifierAndRepairAtomically(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	fact := CanonicalP4EvidenceAdmission()
	assertP4ExactContract(t, fact)

	if _, err := s.DB().ExecContext(ctx, `CREATE TRIGGER reject_p4_replay
		BEFORE INSERT ON p4_verifier_replays
		BEGIN SELECT RAISE(ABORT, 'injected replay persistence failure'); END`); err != nil {
		t.Fatalf("create P4 rollback trigger: %v", err)
	}
	if _, err := s.PersistP4EvidenceAdmission(ctx, fact); err == nil {
		t.Fatal("PersistP4EvidenceAdmission accepted a partial transaction")
	}
	assertP4TableCounts(t, s, 0)
	if _, err := s.DB().ExecContext(ctx, `DROP TRIGGER reject_p4_replay`); err != nil {
		t.Fatalf("drop P4 rollback trigger: %v", err)
	}

	persisted, err := s.PersistP4EvidenceAdmission(ctx, fact)
	if err != nil {
		t.Fatalf("PersistP4EvidenceAdmission: %v", err)
	}
	if !reflect.DeepEqual(persisted, fact) {
		t.Fatalf("persisted P4 fact = %#v, want %#v", persisted, fact)
	}
	assertP4TableCounts(t, s, 1)

	loaded, err := s.GetP4EvidenceAdmission(ctx, fact.VerifierRequest.InputHash)
	if err != nil {
		t.Fatalf("GetP4EvidenceAdmission: %v", err)
	}
	if !reflect.DeepEqual(loaded, fact) {
		t.Fatalf("loaded P4 fact = %#v, want %#v", loaded, fact)
	}

	replayed, err := s.PersistP4EvidenceAdmission(ctx, fact)
	if err != nil {
		t.Fatalf("idempotent P4 persistence: %v", err)
	}
	if !reflect.DeepEqual(replayed, fact) {
		t.Fatalf("replayed P4 fact = %#v, want %#v", replayed, fact)
	}
	assertP4TableCounts(t, s, 1)

	if _, err := s.DB().ExecContext(ctx, `UPDATE p4_evidence_bundles SET bundle_json = '{}' WHERE bundle_hash = ?`, fact.Bundle.BundleHash); err == nil {
		t.Fatal("P4 immutable evidence bundle update unexpectedly succeeded")
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM p4_repair_admissions WHERE admission_hash = ?`, fact.RepairAdmission.AdmissionHash); err == nil {
		t.Fatal("P4 immutable repair admission delete unexpectedly succeeded")
	}
}

func TestP4EvidenceAdmissionRejectsExactBindingAndLeavesNoFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*P4EvidenceAdmission)
	}{
		{
			name:   "P1 revision ancestry",
			mutate: func(fact *P4EvidenceAdmission) { fact.PredecessorChain.P1RevisionHash = p4TestHash("wrong-p1") },
		},
		{
			name:   "P2 grill ancestry",
			mutate: func(fact *P4EvidenceAdmission) { fact.PredecessorChain.P2GrillFixtureSHA256 = p4TestHash("wrong-p2") },
		},
		{
			name: "P3a launch admission ancestry",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.PredecessorChain.P3ALaunchAdmissionFixtureSHA256 = p4TestHash("wrong-p3a")
			},
		},
		{
			name:   "P3b full fence contract",
			mutate: func(fact *P4EvidenceAdmission) { fact.PredecessorChain.P3BFenceContract = "token_only" },
		},
		{
			name:   "P3c recovery ancestry",
			mutate: func(fact *P4EvidenceAdmission) { fact.PredecessorChain.P3CRecoveryAction = "other" },
		},
		{
			name: "P3d audit ancestry",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.PredecessorChain.P3DOMPAuditFixtureSHA256 = p4TestHash("wrong-p3d")
			},
		},
		{
			name: "P3f adapter fixture",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.PredecessorChain.P3FAdapterFixtureSHA256 = p4TestHash("wrong-p3f")
			},
		},
		{
			name:   "P3f exact denial inventory",
			mutate: func(fact *P4EvidenceAdmission) { fact.PredecessorChain.P3FAdapterRedFlagsCount = 36 },
		},
		{
			name: "P3f denial fixture",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.PredecessorChain.P3FAdapterRedFlagsFixtureSHA256 = p4TestHash("wrong-p3f-flags")
			},
		},
		{
			name:   "bundle evidence hash",
			mutate: func(fact *P4EvidenceAdmission) { fact.Bundle.EvidenceHashes.TestHash = p4TestHash("wrong-evidence") },
		},
		{
			name: "bundle trust identity",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.Bundle.VerifierTrustIdentityHash = p4TestHash("wrong-bundle-trust")
			},
		},
		{
			name: "release trust binding",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.VerifierIdentities.Release.TrustIdentityHash = p4TestHash("wrong-release-trust")
			},
		},
		{
			name:   "repair cap",
			mutate: func(fact *P4EvidenceAdmission) { fact.RepairAdmission.RepairAttemptCap = 3 },
		},
		{
			name:   "repair role",
			mutate: func(fact *P4EvidenceAdmission) { fact.RepairAdmission.AllowedRole = "other_role" },
		},
		{
			name: "repair route",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.RepairAdmission.AllowedRouteEvidenceHash = p4TestHash("wrong-route")
			},
		},
		{
			name: "exact evidence bundle",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.RepairAdmission.ExactEvidenceBundleHash = p4TestHash("wrong-bundle")
			},
		},
		{
			name: "fresh approval",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.RepairAdmission.PriorApprovalEvidenceHash = fact.RepairAdmission.FreshApprovalEvidenceHash
			},
		},
		{
			name:   "fresh full fence",
			mutate: func(fact *P4EvidenceAdmission) { fact.FullFence.ClaimTokenHash = "" },
		},
		{
			name:   "full fence generation",
			mutate: func(fact *P4EvidenceAdmission) { fact.FullFence.FenceGeneration = 7 },
		},
		{
			name:   "typed MoA role",
			mutate: func(fact *P4EvidenceAdmission) { fact.RepairAdmission.TypedMoAGrant.GranteeRole = "other_role" },
		},
		{
			name: "typed MoA route",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.RepairAdmission.TypedMoAGrant.RouteEvidenceHash = p4TestHash("wrong-grant-route")
			},
		},
		{
			name: "typed MoA expiry",
			mutate: func(fact *P4EvidenceAdmission) {
				fact.RepairAdmission.TypedMoAGrant.NotAfter = fact.RepairAdmission.TypedMoAGrant.IssuedAt
			},
		},
		{
			name:   "verifier request P3f count",
			mutate: func(fact *P4EvidenceAdmission) { fact.VerifierRequest.P3FAdapterRedFlagsCount = 36 },
		},
		{
			name:   "verifier output repair authority",
			mutate: func(fact *P4EvidenceAdmission) { fact.VerifierOutput.RepairExecution = "authorized" },
		},
		{
			name:   "replay durable facts",
			mutate: func(fact *P4EvidenceAdmission) { fact.VerifierReplay.NewDurableFacts = 1 },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			fact := CanonicalP4EvidenceAdmission()
			tc.mutate(&fact)
			if _, err := s.PersistP4EvidenceAdmission(context.Background(), fact); !errors.Is(err, ErrP4EvidenceInvalid) {
				t.Fatalf("PersistP4EvidenceAdmission error = %v, want %v", err, ErrP4EvidenceInvalid)
			}
			assertP4TableCounts(t, s, 0)
		})
	}
}

func TestP4EvidenceAdmissionConcurrentExactReplayAppendsNoFacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "p4-concurrent.sqlite")
	left, err := Open(path)
	if err != nil {
		t.Fatalf("open left P4 store: %v", err)
	}
	defer left.Close()
	right, err := Open(path)
	if err != nil {
		t.Fatalf("open right P4 store: %v", err)
	}
	defer right.Close()

	fact := CanonicalP4EvidenceAdmission()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var results [2]P4EvidenceAdmission
	var wait sync.WaitGroup
	for index, s := range []*Store{left, right} {
		wait.Add(1)
		go func(index int, s *Store) {
			defer wait.Done()
			<-start
			result, err := s.PersistP4EvidenceAdmission(ctx, fact)
			results[index] = result
			errs <- err
		}(index, s)
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent PersistP4EvidenceAdmission: %v", err)
		}
	}
	for _, result := range results {
		if !reflect.DeepEqual(result, fact) {
			t.Fatalf("concurrent P4 result = %#v, want %#v", result, fact)
		}
	}
	assertP4TableCounts(t, left, 1)
}

func assertP4ExactContract(t *testing.T, fact P4EvidenceAdmission) {
	t.Helper()
	if fact.PredecessorChain.P1RevisionHash != "sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263" ||
		fact.PredecessorChain.P2GrillFixtureSHA256 != "sha256:d9301e896e1cd223c6a05df37eea8fd862c955a0ba9e0985616bffcae0e35caa" ||
		fact.PredecessorChain.P3FAdapterFixtureSHA256 != "sha256:956cc3e2a7fb6426dc084f87fa55595ce8cf8767741b66eda77489db32c5cf44" ||
		fact.PredecessorChain.P3FAdapterRedFlagsFixtureSHA256 != "sha256:6c69ac6ceaac825098fc716e4bb6576ee2bf1a3f7e0b4ca9ad3ba42b3d47b525" ||
		fact.PredecessorChain.P3FAdapterRedFlagsCount != 37 {
		t.Fatalf("P4 predecessor chain = %#v, want exact P1-P3f bindings", fact.PredecessorChain)
	}
	if fact.Bundle.BundleHash != "sha256:12ec67830ffa00eb637ed0594b46b89be79c28cce3854574f540f9dc2b6a5c0d" ||
		fact.Bundle.EvidenceHashes.ProposalHash != "sha256:abcff70415d6dbf27ebcb0acb607e99106d4776eff027651f0d508e3f65c0caa" ||
		fact.Bundle.EvidenceHashes.RevisionHash != "sha256:ee57c340e2ffd89af1835cb56d6d76385c23e2fd331b074c8477e90fd902b179" ||
		fact.Bundle.EvidenceHashes.ApprovalHash != "sha256:813a115813a2f77b21c186d286a3c75ac288ae87dc39ba4c9b3ea7f55fb5df1c" ||
		fact.Bundle.EvidenceHashes.FenceHash != "sha256:5d806ac929f58e4c3d1a3022b9eb4519b0a347394414d3db64a9f01ba6443b81" ||
		fact.Bundle.EvidenceHashes.EnvelopeHash != "sha256:e49bf9b4bf1704fb415bb4593215f5293d93fe7dfcc8369258bc01a9dc156634" ||
		fact.Bundle.EvidenceHashes.ReceiptHash != "sha256:356c4c55ff87312a74bd77454bb249617cb54b97718617b8a98a18db9349ad30" ||
		fact.Bundle.EvidenceHashes.CallbackHash != "sha256:491cc0f8b2d0f4db3f5905144ed9a37218f6d5176f1e1cada6dd19a3e9bf4b4f" ||
		fact.Bundle.EvidenceHashes.SourceHash != "sha256:0431228a04bd62100d2ddbb029c66b03fb6d9eac5d0d95fd128635e1a14f600a" ||
		fact.Bundle.EvidenceHashes.ArtifactHash != "sha256:3ce83fc3d4e64e8a3398e6fa1d73453a1d51d3ef00b3224dc4272b8e902ee681" ||
		fact.Bundle.EvidenceHashes.RouteHash != "sha256:d6ef174437a6db28dbc916c014db14228b933eb23da706d77aaa6be0ff412a21" ||
		fact.Bundle.EvidenceHashes.TestHash != "sha256:b6a6295a8dd057a3336b4c1e1ce9bd3dcd2448965bbe4ea68d6be38373e68666" ||
		fact.Bundle.EvidenceHashes.EvaluationHash != "sha256:6b22a74ce9e5a764ee5b0a27017a0ddbc443dbedd8d24d44d699078fa857db3c" {
		t.Fatalf("P4 bundle evidence hashes = %#v, want all exact immutable hashes", fact.Bundle.EvidenceHashes)
	}
	if fact.VerifierIdentities.Trust.TrustIdentityHash != "sha256:a6e3ee0e4a6d2f8787395c1e5a3100db3e84aa0b2dc6cb9fa24942718ce09b51" ||
		fact.VerifierIdentities.Release.ReleaseIdentityHash != "sha256:ecbaaf39e5fe23418e98718e957240318f9ec8d3ed35449d10d13063d2273590" {
		t.Fatalf("P4 verifier identities = %#v, want exact trust/release identities", fact.VerifierIdentities)
	}
	if fact.RepairAdmission.RepairAttemptCap != 2 || fact.RepairAdmission.AllowedRole != "self_development_repair_runner" ||
		fact.RepairAdmission.AllowedRouteEvidenceHash != fact.Bundle.EvidenceHashes.RouteHash ||
		fact.FullFence.ClaimID == "" || fact.FullFence.ClaimTokenHash == "" || fact.FullFence.FenceGeneration != 8 {
		t.Fatalf("P4 bounded repair admission = %#v / full fence = %#v", fact.RepairAdmission, fact.FullFence)
	}
	if fact.VerifierReplay.NewDurableFacts != 0 || fact.VerifierReplay.InputHash != fact.VerifierRequest.InputHash || fact.VerifierReplay.OutputHash != fact.VerifierOutput.OutputHash {
		t.Fatalf("P4 replay = %#v, want exact zero-fact replay", fact.VerifierReplay)
	}
}

func assertP4TableCounts(t *testing.T, s *Store, want int) {
	t.Helper()
	for _, table := range []string{"p4_evidence_bundles", "p4_repair_admissions", "p4_verifier_requests", "p4_verifier_outputs", "p4_verifier_replays"} {
		var got int
		if err := s.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}

func p4TestHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
