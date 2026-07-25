package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestEd25519AuthorizationValidatesSignaturesBindingsAndRootLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	fixture := newSignedAuthorizationFixture(t, now)
	if fixture.envelope.ReleaseAttestationHash == fixture.bundle.Authorization.ReleaseAttestation.AttestationHash ||
		fixture.envelope.ReleaseApprovalHash == fixture.bundle.Authorization.ReleaseApproval.ApprovalHash {
		t.Fatal("later authorization records replaced frozen predecessor release identities")
	}
	verifier, err := newEd25519Verifier(fixture.bundle, predecessorReleaseIdentityFromEnvelope(fixture.envelope))
	if err != nil {
		t.Fatalf("newEd25519Verifier: %v", err)
	}
	if err := verifier.verifyAuthorizationAt(context.Background(), fixture.envelope, fixture.bundle.Authorization, now); err != nil {
		t.Fatalf("verify valid signed authorization: %v", err)
	}

	t.Run("detached signature drift", func(t *testing.T) {
		drifted := fixture.bundle.Authorization
		drifted.ReleaseAttestationSignature.Signature = signatureText(bytes.Repeat([]byte{0x7f}, ed25519.SignatureSize))
		if err := verifier.verifyAuthorizationAt(context.Background(), fixture.envelope, drifted, now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("signature drift error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("release pin drift", func(t *testing.T) {
		drifted := fixture.envelope
		drifted.ReleaseApprovalHash = testHash("other-release-approval")
		drifted, err = store.SealExternalSupervisorEnvelope(drifted)
		if err != nil {
			t.Fatalf("seal drifted envelope: %v", err)
		}
		if err := verifier.verifyAuthorizationAt(context.Background(), drifted, fixture.bundle.Authorization, now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("release pin drift error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("approval expiry boundary", func(t *testing.T) {
		expires, err := time.Parse(time.RFC3339Nano, fixture.bundle.Authorization.ReleaseApproval.NotAfter)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifier.verifyAuthorizationAt(context.Background(), fixture.envelope, fixture.bundle.Authorization, expires); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("expiry boundary error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("active root revoked", func(t *testing.T) {
		drifted := fixture.bundle
		drifted.ReleaseRoots.Revocation.EffectiveAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
		drifted.ReleaseRoots.Revocation, err = store.SealExternalSupervisorRootRevocation(drifted.ReleaseRoots.Revocation)
		if err != nil {
			t.Fatalf("seal revocation: %v", err)
		}
		drifted.ReleaseRoots.RevocationSignature = detachedTestSignature(t, fixture.keys["release-successor"], drifted.ReleaseRoots.Revocation)
		drifted, err = store.SealExternalSupervisorTrustBundle(drifted)
		if err != nil {
			t.Fatalf("seal trust bundle: %v", err)
		}
		revokedVerifier, err := newEd25519Verifier(drifted, predecessorReleaseIdentityFromEnvelope(fixture.envelope))
		if err != nil {
			t.Fatalf("new verifier with revocation: %v", err)
		}
		if err := revokedVerifier.verifyAuthorizationAt(context.Background(), fixture.envelope, drifted.Authorization, now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("revoked root error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("predecessor downgrade after successor activation", func(t *testing.T) {
		activation, err := time.Parse(time.RFC3339Nano, fixture.bundle.ReleaseRoots.Successor.ValidFrom)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifier.verifyAuthorizationAt(context.Background(), fixture.envelope, fixture.bundle.Authorization, activation); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("root downgrade error = %v, want %v", err, ErrAuthentication)
		}
	})

	encoded, err := json.Marshal(fixture.bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range fixture.keys {
		if bytes.Contains(encoded, private) {
			t.Fatal("private key bytes escaped into the client trust bundle")
		}
	}
	if bytes.Contains(bytes.ToLower(encoded), []byte("private")) {
		t.Fatal("private-key field escaped into the client trust bundle")
	}
}

var forbiddenAuthorizationIdentifierCases = [...]struct {
	name  string
	value string
}{
	{name: "raw authority", value: "raw_authority_payload_001"},
	{name: "credential", value: "credential_payload_001"},
	{name: "secret", value: "secret_payload_001"},
	{name: "key", value: "key_payload_001"},
	{name: "private key", value: "private_key_payload_001"},
	{name: "key marker", value: "signer_key_material_001"},
	{name: "command", value: "command_payload_001"},
	{name: "argv", value: "argv_payload_001"},
	{name: "environment", value: "environment_payload_001"},
	{name: "source", value: "source_payload_001"},
	{name: "artifact", value: "artifact_payload_001"},
	{name: "evidence", value: "evidence_payload_001"},
	{name: "path", value: "path_payload_001"},
	{name: "URL", value: "https://approval.invalid/release"},
	{name: "whitespace", value: "release approval 001"},
}

var signedAuthorizationIdentifierFields = [...]struct {
	name  string
	field string
}{
	{name: "release approval ID", field: "approval_id"},
	{name: "MoA grant ID", field: "grant_id"},
}

func TestEd25519AuthorizationRejectsSignedForbiddenOpaqueIdentifierValues(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	validFixture := newSignedAuthorizationFixture(t, now)
	for _, testCase := range forbiddenAuthorizationIdentifierCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, identifier := range signedAuthorizationIdentifierFields {
				t.Run(identifier.name, func(t *testing.T) {
					fixture := forgedSignedAuthorizationIdentifier(t, validFixture, identifier.field, testCase.value)
					verifier, err := newEd25519Verifier(fixture.bundle, predecessorReleaseIdentityFromEnvelope(fixture.envelope))
					if err != nil {
						t.Fatalf("construct verifier for signed denial proof: %v", err)
					}
					if err := verifier.verifyAuthorizationAt(context.Background(), fixture.envelope, fixture.bundle.Authorization, now); !errors.Is(err, ErrAuthentication) {
						t.Fatalf("signed forbidden %s %q error = %v, want %v", identifier.field, testCase.value, err, ErrAuthentication)
					}
				})
			}
		})
	}
}

func forgedSignedAuthorizationIdentifier(t *testing.T, fixture signedAuthorizationFixture, field, value string) signedAuthorizationFixture {
	t.Helper()
	chain := fixture.bundle.Authorization
	if field == "approval_id" {
		chain.ReleaseApproval.ApprovalID = value
		chain.ReleaseApproval.ApprovalHash = forgedRecordHash(t, chain.ReleaseApproval, "approval_hash")
		chain.ReleaseApprovalSignature = detachedTestSignature(t, fixture.keys["approver"], chain.ReleaseApproval)
		chain.MoARoleGrant.ReleaseApprovalHash = chain.ReleaseApproval.ApprovalHash
	} else if field == "grant_id" {
		chain.MoARoleGrant.GrantID = value
	} else {
		t.Fatalf("unsupported forged authorization field %q", field)
	}
	chain.MoARoleGrant.GrantHash = forgedRecordHash(t, chain.MoARoleGrant, "grant_hash")
	chain.MoARoleGrantSignature = detachedTestSignature(t, fixture.keys["grantor"], chain.MoARoleGrant)
	fixture.bundle.Authorization = chain
	sealed, err := store.SealExternalSupervisorTrustBundle(fixture.bundle)
	if err != nil {
		t.Fatalf("seal forged signed authorization bundle: %v", err)
	}
	fixture.bundle = sealed
	assertForgedAuthorizationSelfHashesAndSignatures(t, fixture, field, value)
	return fixture
}

func assertForgedAuthorizationSelfHashesAndSignatures(t *testing.T, fixture signedAuthorizationFixture, field, value string) {
	t.Helper()
	chain := fixture.bundle.Authorization
	if field == "approval_id" && chain.ReleaseApproval.ApprovalID != value {
		t.Fatalf("forged release approval ID = %q, want %q", chain.ReleaseApproval.ApprovalID, value)
	}
	if field == "grant_id" && chain.MoARoleGrant.GrantID != value {
		t.Fatalf("forged MoA grant ID = %q, want %q", chain.MoARoleGrant.GrantID, value)
	}
	if got := forgedRecordHash(t, chain.ReleaseApproval, "approval_hash"); got != chain.ReleaseApproval.ApprovalHash {
		t.Fatalf("forged release approval self hash = %q, want %q", chain.ReleaseApproval.ApprovalHash, got)
	}
	if got := forgedRecordHash(t, chain.MoARoleGrant, "grant_hash"); got != chain.MoARoleGrant.GrantHash {
		t.Fatalf("forged MoA grant self hash = %q, want %q", chain.MoARoleGrant.GrantHash, got)
	}
	if err := verifyDetachedSignature(fixture.keys["approver"].Public().(ed25519.PublicKey), chain.ReleaseApproval.ApproverKeySPKISHA256, chain.ReleaseApproval, chain.ReleaseApprovalSignature); err != nil {
		t.Fatalf("forged release approval Ed25519 signature: %v", err)
	}
	if err := verifyDetachedSignature(fixture.keys["grantor"].Public().(ed25519.PublicKey), chain.MoARoleGrant.GrantorKeySPKISHA256, chain.MoARoleGrant, chain.MoARoleGrantSignature); err != nil {
		t.Fatalf("forged MoA grant Ed25519 signature: %v", err)
	}
}

func forgedRecordHash(t *testing.T, value any, hashField string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal forged record: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode forged record: %v", err)
	}
	delete(object, hashField)
	hash, err := canonicalHash(object)
	if err != nil {
		t.Fatalf("hash forged record: %v", err)
	}
	return hash
}

type signedAuthorizationFixture struct {
	bundle   store.ExternalSupervisorTrustBundle
	envelope store.ExternalSupervisorEnvelope
	keys     map[string]ed25519.PrivateKey
}

func newSignedAuthorizationFixture(t *testing.T, now time.Time) signedAuthorizationFixture {
	t.Helper()
	keys := make(map[string]ed25519.PrivateKey)
	newKey := func(name string) ed25519.PrivateKey {
		digest := sha256.Sum256([]byte("ananke-test-ed25519:" + name))
		key := ed25519.NewKeyFromSeed(digest[:])
		keys[name] = key
		return key
	}
	for _, name := range []string{
		"release-active", "release-successor", "approval-active", "approval-successor",
		"moa-active", "moa-successor", "attestor", "approver", "grantor", "peer",
	} {
		newKey(name)
	}

	rootLifecycle := func(kind, activeName, successorName string) store.ExternalSupervisorTrustRootLifecycle {
		activeKey := keys[activeName]
		successorKey := keys[successorName]
		activeID := "ananke_" + kind + "_root_v1"
		successorID := "ananke_" + kind + "_root_v2"
		activeNotAfter := now.Add(2 * time.Hour)
		successorValidFrom := now.Add(time.Hour)
		rotation, err := store.SealExternalSupervisorRootRotation(store.ExternalSupervisorRootRotation{
			SchemaVersion:               store.ExternalSupervisorRootRotationSchemaVersion,
			CrossSignatureReferenceHash: testHash(kind + "-cross-signature-reference"),
			OldRootID:                   activeID,
			NewRootID:                   successorID,
			NewRootSPKISHA256:           testSPKIHash(t, successorKey.Public().(ed25519.PublicKey)),
			NewRootValidFrom:            successorValidFrom.Format(time.RFC3339Nano),
			OldRootNotAfter:             activeNotAfter.Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("seal %s rotation: %v", kind, err)
		}
		revocation, err := store.SealExternalSupervisorRootRevocation(store.ExternalSupervisorRootRevocation{
			SchemaVersion:         store.ExternalSupervisorRootRevocationSchemaVersion,
			RevokedRootID:         activeID,
			IssuerRootID:          successorID,
			EffectiveAt:           successorValidFrom.Format(time.RFC3339Nano),
			RevocationReasonClass: "key_compromise_or_policy_withdrawal",
		})
		if err != nil {
			t.Fatalf("seal %s revocation: %v", kind, err)
		}
		return store.ExternalSupervisorTrustRootLifecycle{
			Active: store.ExternalSupervisorTrustRootKey{
				RootID: activeID, PublicKey: publicKeyText(activeKey.Public().(ed25519.PublicKey)),
				SPKISHA256: testSPKIHash(t, activeKey.Public().(ed25519.PublicKey)),
				ValidFrom:  now.Add(-2 * time.Hour).Format(time.RFC3339Nano), NotAfter: activeNotAfter.Format(time.RFC3339Nano),
			},
			Successor: store.ExternalSupervisorTrustRootKey{
				RootID: successorID, PublicKey: publicKeyText(successorKey.Public().(ed25519.PublicKey)),
				SPKISHA256: testSPKIHash(t, successorKey.Public().(ed25519.PublicKey)),
				ValidFrom:  successorValidFrom.Format(time.RFC3339Nano), NotAfter: now.Add(24 * time.Hour).Format(time.RFC3339Nano),
			},
			Rotation: rotation, RotationSignature: detachedTestSignature(t, activeKey, rotation),
			Revocation: revocation, RevocationSignature: detachedTestSignature(t, successorKey, revocation),
		}
	}

	bundle := store.ExternalSupervisorTrustBundle{
		SchemaVersion: store.ExternalSupervisorTrustBundleSchemaVersion,
		ReleaseRoots:  rootLifecycle("release", "release-active", "release-successor"),
		ApprovalRoots: rootLifecycle("approval", "approval-active", "approval-successor"),
		MoARoots:      rootLifecycle("moa_role_grant", "moa-active", "moa-successor"),
	}
	bundle.ReleaseAttestor = signedTestCertificate(t, "release_attestor", bundle.ReleaseRoots.Active.RootID, keys["release-active"], keys["attestor"], now)
	bundle.ReleaseApprover = signedTestCertificate(t, "release_approver", bundle.ApprovalRoots.Active.RootID, keys["approval-active"], keys["approver"], now)
	bundle.MoAGrantor = signedTestCertificate(t, "moa_grantor", bundle.MoARoots.Active.RootID, keys["moa-active"], keys["grantor"], now)
	bundle.SupervisorPeer = signedTestCertificate(t, "independent_supervisor_protocol_adapter", bundle.ReleaseRoots.Active.RootID, keys["release-active"], keys["peer"], now)

	routeHash := testHash("p3f-route")
	attestation, err := store.SealExternalSupervisorReleaseAttestation(store.ExternalSupervisorReleaseAttestation{
		SchemaVersion:  store.ExternalSupervisorReleaseAttestationSchemaVersion,
		ArtifactSHA256: testHash("supervisor-artifact"), BuildIdentityHash: testHash("supervisor-build"),
		RouteMappingHash: routeHash, ReleaseRootID: bundle.ReleaseRoots.Active.RootID,
		AttestorKeySPKISHA256: bundle.ReleaseAttestor.Certificate.SubjectKeySPKISHA256,
		IssuedAt:              now.Add(-30 * time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(90 * time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seal attestation: %v", err)
	}
	approval, err := store.SealExternalSupervisorReleaseApproval(store.ExternalSupervisorReleaseApproval{
		SchemaVersion: store.ExternalSupervisorReleaseApprovalSchemaVersion,
		ApprovalID:    "independent_release_approval_001", ApproverRootID: bundle.ApprovalRoots.Active.RootID,
		ApproverKeySPKISHA256: bundle.ReleaseApprover.Certificate.SubjectKeySPKISHA256,
		AttestationHash:       attestation.AttestationHash, RouteMappingHash: routeHash, Decision: "approved",
		IssuedAt: now.Add(-20 * time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(80 * time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seal approval: %v", err)
	}
	grant, err := store.SealExternalSupervisorMoARoleGrant(store.ExternalSupervisorMoARoleGrant{
		SchemaVersion: store.ExternalSupervisorMoARoleGrantSchemaVersion,
		GrantID:       "moa_remote_supervisor_runner_grant_001", GranteeRole: "remote_supervisor_runner",
		GrantorRootID: bundle.MoARoots.Active.RootID, GrantorKeySPKISHA256: bundle.MoAGrantor.Certificate.SubjectKeySPKISHA256,
		ReleaseAttestationHash: attestation.AttestationHash, ReleaseApprovalHash: approval.ApprovalHash,
		RouteMappingHash: routeHash, IssuedAt: now.Add(-10 * time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(70 * time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seal MoA grant: %v", err)
	}
	bundle.Authorization = store.ExternalSupervisorAuthorizationChain{
		ReleaseAttestation: attestation, ReleaseAttestationSignature: detachedTestSignature(t, keys["attestor"], attestation),
		ReleaseApproval: approval, ReleaseApprovalSignature: detachedTestSignature(t, keys["approver"], approval),
		MoARoleGrant: grant, MoARoleGrantSignature: detachedTestSignature(t, keys["grantor"], grant),
	}
	bundle, err = store.SealExternalSupervisorTrustBundle(bundle)
	if err != nil {
		t.Fatalf("seal trust bundle: %v", err)
	}

	envelope, err := store.SealExternalSupervisorEnvelope(store.ExternalSupervisorEnvelope{
		SchemaVersion: store.ExternalSupervisorEnvelopeSchemaVersion, HandoffID: "remote_handoff_signed_001",
		IdempotencyKeyHash: testHash("idempotency"), LaunchSpecHash: testHash("launch-spec"), FenceBindingHash: testHash("fence"),
		Deadline: now.Add(time.Hour).Format(time.RFC3339Nano), AttemptNumber: 1, AttemptCap: 3,
		RouteMappingHash: routeHash, SourceSnapshotHash: testHash("source-snapshot"), SourceManifestHash: testHash("source-manifest"),
		RepositoryIdentity: "github.com/yingliang-zhang/ananke", SupervisorArtifactSHA256: attestation.ArtifactSHA256,
		BuildIdentityHash: testHash("supervisor-build"), ReleaseAttestationHash: testHash("predecessor-release-attestation"),
		ReleaseApprovalHash: testHash("predecessor-release-approval"), EvidenceContractHash: testHash("evidence-contract"),
		EvidenceSchemaVersion: "ananke.remote-supervisor-evidence.v1",
	})
	if err != nil {
		t.Fatalf("seal envelope: %v", err)
	}
	return signedAuthorizationFixture{bundle: bundle, envelope: envelope, keys: keys}
}

func signedTestCertificate(t *testing.T, role, rootID string, rootKey, subjectKey ed25519.PrivateKey, now time.Time) store.ExternalSupervisorSignedCertificate {
	t.Helper()
	certificate, err := store.SealExternalSupervisorSigningCertificate(store.ExternalSupervisorSigningCertificate{
		SchemaVersion: store.ExternalSupervisorSigningCertificateSchemaVersion,
		Role:          role, SubjectKeySPKISHA256: testSPKIHash(t, subjectKey.Public().(ed25519.PublicKey)),
		SubjectPublicKey: publicKeyText(subjectKey.Public().(ed25519.PublicKey)), IssuerRootID: rootID,
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), NotAfter: now.Add(12 * time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seal %s certificate: %v", role, err)
	}
	return store.ExternalSupervisorSignedCertificate{Certificate: certificate, Signature: detachedTestSignature(t, rootKey, certificate)}
}

func detachedTestSignature(t *testing.T, key ed25519.PrivateKey, value any) store.ExternalSupervisorDetachedSignature {
	t.Helper()
	canonical, err := marshalCanonical(value)
	if err != nil {
		t.Fatalf("canonical signature input: %v", err)
	}
	signature := ed25519.Sign(key, canonical)
	result, err := store.SealExternalSupervisorDetachedSignature(store.ExternalSupervisorDetachedSignature{
		SchemaVersion: store.ExternalSupervisorDetachedSignatureSchemaVersion,
		Algorithm:     "ed25519", SignerKeySPKISHA256: testSPKIHash(t, key.Public().(ed25519.PublicKey)), Signature: signatureText(signature),
	})
	if err != nil {
		t.Fatalf("seal detached signature: %v", err)
	}
	return result
}

func publicKeyText(key ed25519.PublicKey) string { return "ed25519:" + hex.EncodeToString(key) }
func signatureText(signature []byte) string      { return "ed25519:" + hex.EncodeToString(signature) }
func testSPKIHash(t *testing.T, publicKey ed25519.PublicKey) string {
	t.Helper()
	hash, err := spkiHash(publicKey)
	if err != nil {
		t.Fatalf("derive SPKI hash: %v", err)
	}
	return hash
}
