package repaircontract

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const fixturePath = "testdata/p6-contract-v1.json"

func TestP6ContractFixtureIsCanonicalClosedAndValid(t *testing.T) {
	raw, fixture := readFixture(t)
	canonical, err := canonicalBytes(fixture)
	if err != nil {
		t.Fatalf("canonical fixture: %v", err)
	}
	if !bytes.Equal(raw, canonical) {
		t.Fatal("fixture bytes are not RFC 8785/JCS canonical bytes")
	}
	if fixture.ReleasePins != FrozenReleasePins() {
		t.Fatalf("fixture release pins differ from compiled release pins")
	}
	if err := VerifyFixture(fixture, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission); err != nil {
		t.Fatalf("verify fixture at admission: %v", err)
	}
	effectAt := mustTime(t, fixture.Dispatch.CreatedAt).Add(time.Second)
	if err := VerifyFixture(fixture, effectAt, ValidationEffect); err != nil {
		t.Fatalf("verify fixture at effect: %v", err)
	}

	assertNoAuthorityPayload(t, raw)
}

func TestP6ReleasePinsUseActualPublicArtifactBytes(t *testing.T) {
	artifacts := requiredReleaseArtifactBytes(t)
	pins := FrozenReleasePins()
	bundle := FrozenTrustBundle()

	checks := []struct {
		name string
		got  string
		want string
	}{
		{name: "root certificate DER", got: bundle.Root.RootHash, want: sha256Digest(artifacts["repair_root_certificate_der"])},
		{name: "root certificate release pin", got: pins.RepairRootCertificateHash, want: sha256Digest(artifacts["repair_root_certificate_der"])},
		{name: "root SPKI DER", got: bundle.Root.RootSPKISHA256, want: sha256Digest(artifacts["repair_root_spki_der"])},
		{name: "root SPKI release pin", got: pins.RepairRootSPKISHA256, want: sha256Digest(artifacts["repair_root_spki_der"])},
		{name: "attestor certificate DER", got: pins.RepairAttestorCertificateHash, want: sha256Digest(artifacts["repair_attestor_certificate_der"])},
		{name: "attestor modeled certificate DER", got: bundle.RepairAttestor.CertificateHash, want: sha256Digest(artifacts["repair_attestor_certificate_der"])},
		{name: "attestor SPKI DER", got: pins.RepairAttestorLeafSPKI, want: sha256Digest(artifacts["repair_attestor_spki_der"])},
		{name: "approver root certificate DER", got: bundle.RotationApproverRoot.RootHash, want: sha256Digest(artifacts["rotation_approver_root_certificate_der"])},
		{name: "approver root certificate release pin", got: pins.RotationApproverRootCertificateHash, want: sha256Digest(artifacts["rotation_approver_root_certificate_der"])},
		{name: "approver root SPKI DER", got: bundle.RotationApproverRoot.RootSPKISHA256, want: sha256Digest(artifacts["rotation_approver_root_spki_der"])},
		{name: "approver root SPKI release pin", got: pins.RotationApproverRootSPKISHA256, want: sha256Digest(artifacts["rotation_approver_root_spki_der"])},
		{name: "approver certificate DER", got: bundle.RotationApprover.CertificateHash, want: sha256Digest(artifacts["rotation_approver_certificate_der"])},
		{name: "approver certificate release pin", got: pins.RotationApproverCertificateHash, want: sha256Digest(artifacts["rotation_approver_certificate_der"])},
		{name: "approver SPKI DER", got: bundle.RotationApprover.SubjectSPKISHA256, want: sha256Digest(artifacts["rotation_approver_spki_der"])},
		{name: "approver SPKI release pin", got: pins.RotationApproverLeafSPKI, want: sha256Digest(artifacts["rotation_approver_spki_der"])},
		{name: "public trust bundle", got: pins.TrustBundleHash, want: sha256Digest(artifacts["public_trust_bundle"])},
		{name: "contract build release", got: pins.BuildIdentityHash, want: sha256Digest(artifacts["contract_release"])},
		{name: "release manifest", got: pins.ReleaseManifestHash, want: sha256Digest(artifacts["release_manifest"])},
		{name: "supervisor policy", got: pins.SupervisorPolicyHash, want: sha256Digest(artifacts["supervisor_policy"])},
		{name: "supervisor profile", got: pins.SupervisorProfileHash, want: sha256Digest(artifacts["supervisor_profile"])},
		{name: "rotation policy", got: pins.RotationPolicyHash, want: sha256Digest(artifacts["rotation_policy"])},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if check.got != check.want {
				t.Errorf("compiled pin = %s, actual artifact SHA-256 = %s", check.got, check.want)
			}
		})
	}
}

func TestP6ActualEd25519CertificateChainAndCriticalRepairExtensions(t *testing.T) {
	artifacts := requiredReleaseArtifactBytes(t)
	root, err := x509.ParseCertificate(artifacts["repair_root_certificate_der"])
	if err != nil {
		t.Fatalf("parse root certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(artifacts["repair_attestor_certificate_der"])
	if err != nil {
		t.Fatalf("parse repair attestor certificate: %v", err)
	}
	if err := root.CheckSignatureFrom(root); err != nil {
		t.Fatalf("verify root self-signature: %v", err)
	}
	if err := leaf.CheckSignatureFrom(root); err != nil {
		t.Fatalf("verify repair attestor chain: %v", err)
	}
	if root.SignatureAlgorithm != x509.PureEd25519 || leaf.SignatureAlgorithm != x509.PureEd25519 {
		t.Fatalf("signature algorithms = root %v, leaf %v; want Ed25519", root.SignatureAlgorithm, leaf.SignatureAlgorithm)
	}
	if !root.IsCA || root.KeyUsage&x509.KeyUsageCertSign == 0 || root.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Fatalf("root CA/key usages are invalid: is_ca=%t key_usage=%v", root.IsCA, root.KeyUsage)
	}
	if leaf.IsCA || leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Fatalf("leaf CA/key usages are invalid: is_ca=%t key_usage=%v", leaf.IsCA, leaf.KeyUsage)
	}
	if !bytes.Equal(root.RawSubject, root.RawIssuer) || !bytes.Equal(leaf.RawIssuer, root.RawSubject) {
		t.Fatal("certificate issuer/subject chain is not exact")
	}
	if !bytes.Equal(root.RawSubjectPublicKeyInfo, artifacts["repair_root_spki_der"]) ||
		!bytes.Equal(leaf.RawSubjectPublicKeyInfo, artifacts["repair_attestor_spki_der"]) {
		t.Fatal("checked-in SPKI DER does not equal certificate SPKI")
	}
	assertCriticalExtension(t, leaf, "1.3.6.1.4.1.57264.1.6", RepairAttestorRole)
	assertCriticalExtension(t, leaf, "1.3.6.1.4.1.57264.1.7", SignatureDomain)
	if RepairAttestorRole == ProtocolAdapterRole || sha256Digest(leaf.RawSubjectPublicKeyInfo) == ProtocolAdapterLeafSPKIHash {
		t.Fatal("repair attestor reused the P5 protocol role or key")
	}

	modeled := FrozenTrustBundle()
	if modeled.Root.ValidFrom != root.NotBefore.UTC().Format(time.RFC3339Nano) || modeled.Root.NotAfter != root.NotAfter.UTC().Format(time.RFC3339Nano) ||
		modeled.RepairAttestor.ValidFrom != leaf.NotBefore.UTC().Format(time.RFC3339Nano) || modeled.RepairAttestor.NotAfter != leaf.NotAfter.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("modeled validity does not match actual certificates: root=%s..%s leaf=%s..%s", modeled.Root.ValidFrom, modeled.Root.NotAfter, modeled.RepairAttestor.ValidFrom, modeled.RepairAttestor.NotAfter)
	}
	if err := verifyEmbeddedReleaseTrust(time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC)); err != nil {
		t.Fatalf("certificate validity window rejected valid instant: %v", err)
	}
	for _, invalidAt := range []time.Time{leaf.NotBefore.Add(-time.Nanosecond), leaf.NotAfter, root.NotAfter} {
		if err := verifyEmbeddedReleaseTrust(invalidAt); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("certificate validity instant %s error = %v, want %v", invalidAt, err, ErrInvalidContract)
		}
	}
}

func TestP6ActualEd25519RotationApproverChainAndCriticalExtensions(t *testing.T) {
	artifacts := requiredReleaseArtifactBytes(t)
	repairRoot, err := x509.ParseCertificate(artifacts["repair_root_certificate_der"])
	if err != nil {
		t.Fatal(err)
	}
	repairLeaf, err := x509.ParseCertificate(artifacts["repair_attestor_certificate_der"])
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(artifacts["rotation_approver_root_certificate_der"])
	if err != nil {
		t.Fatalf("parse rotation approver root: %v", err)
	}
	leaf, err := x509.ParseCertificate(artifacts["rotation_approver_certificate_der"])
	if err != nil {
		t.Fatalf("parse rotation approver leaf: %v", err)
	}
	if err := validateRotationApproverCertificateSemantics(root, leaf, repairRoot, repairLeaf,
		artifacts["rotation_approver_root_spki_der"], artifacts["rotation_approver_spki_der"]); err != nil {
		t.Fatalf("validate rotation approver chain: %v", err)
	}
	if err := verifyCertificateTime(root, leaf, time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC)); err != nil {
		t.Fatalf("verify rotation approver validity: %v", err)
	}
	for _, invalidAt := range []time.Time{leaf.NotBefore.Add(-time.Nanosecond), leaf.NotAfter, root.NotAfter} {
		if err := verifyCertificateTime(root, leaf, invalidAt); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("rotation approver validity instant %s error = %v, want %v", invalidAt, err, ErrInvalidContract)
		}
	}
	if bytes.Equal(root.Raw, repairRoot.Raw) || bytes.Equal(root.RawSubjectPublicKeyInfo, repairRoot.RawSubjectPublicKeyInfo) ||
		bytes.Equal(leaf.Raw, repairLeaf.Raw) || bytes.Equal(leaf.RawSubjectPublicKeyInfo, repairLeaf.RawSubjectPublicKeyInfo) ||
		sha256Digest(leaf.RawSubjectPublicKeyInfo) == ProtocolAdapterLeafSPKIHash {
		t.Fatal("rotation approver chain reuses repair or P5 identity")
	}
	modeled := FrozenTrustBundle()
	if modeled.RotationApproverRoot.ValidFrom != root.NotBefore.UTC().Format(time.RFC3339Nano) ||
		modeled.RotationApproverRoot.NotAfter != root.NotAfter.UTC().Format(time.RFC3339Nano) ||
		modeled.RotationApprover.ValidFrom != leaf.NotBefore.UTC().Format(time.RFC3339Nano) ||
		modeled.RotationApprover.NotAfter != leaf.NotAfter.UTC().Format(time.RFC3339Nano) ||
		modeled.RotationApprover.KeyID != RotationApproverKeyID || modeled.RotationApprover.Role != RotationApproverRole ||
		modeled.RotationApprover.SignatureDomain != RotationApproverSignatureDomain {
		t.Fatal("frozen rotation approver projection differs from verified certificates and identity")
	}
}

func TestP6PortableReleasePinsExcludeInstallationDescriptorFacts(t *testing.T) {
	pinsJSON, err := json.Marshal(FrozenReleasePins())
	if err != nil {
		t.Fatal(err)
	}
	fixtureJSON, err := json.Marshal(CanonicalFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"bundle_file_identity_hash", "supervisor_socket_identity_hash", "supervisor_journal_identity_hash", "supervisor_runtime_identity_hash"} {
		if bytes.Contains(pinsJSON, []byte(`"`+forbidden+`"`)) {
			t.Errorf("portable release pins contain synthetic identity %q", forbidden)
		}
	}
	for _, forbidden := range []string{"bundle_file_identity", "device", "inode", "owner_user_id", "owner_group_id", "mode", "size"} {
		if bytes.Contains(fixtureJSON, []byte(`"`+forbidden+`"`)) {
			t.Errorf("portable fixture contains installation descriptor fact %q", forbidden)
		}
	}
}

func TestP6SyntheticDescriptorFactsAreRejected(t *testing.T) {
	raw, err := canonicalBytes(CanonicalFixture())
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture["bundle_file_identity"] = map[string]any{
		"content_sha256": sha256Digest([]byte("synthetic")), "device": 1, "inode": 2,
		"mode": 292, "owner_group_id": 0, "owner_user_id": 0, "size": 4096,
	}
	mutated := canonicalTestArtifact(t, fixture)
	if _, err := DecodeFixture(mutated); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("synthetic descriptor facts error = %v, want %v", err, ErrInvalidContract)
	}
}

func TestP6V1RotationHasNoMaterializedSuccessor(t *testing.T) {
	rotation := CanonicalFixture().Rotation
	if rotation.State != "no_successor_authorized" {
		t.Errorf("rotation state = %q, want no_successor_authorized", rotation.State)
	}
	raw, err := json.Marshal(rotation)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"successor_root_id", "successor_root_spki_sha256", "effective_at", "current_root_cross_signature_hash", "release_approval_hash"} {
		if bytes.Contains(raw, []byte(`"`+forbidden+`"`)) {
			t.Errorf("unmaterialized V1 rotation contains %q", forbidden)
		}
	}
}

func TestP6EmbeddedReleaseArtifactsAreCanonicalAndManifestBound(t *testing.T) {
	want := requiredReleaseArtifactBytes(t)
	got := embeddedReleaseArtifactSet().byID()
	if len(got) != len(want) {
		t.Fatalf("embedded artifact count = %d, want %d", len(got), len(want))
	}
	for id, wantBytes := range want {
		gotBytes, ok := got[id]
		if !ok {
			t.Errorf("embedded artifact %q is missing", id)
			continue
		}
		if !bytes.Equal([]byte(gotBytes), wantBytes) {
			t.Errorf("embedded artifact %q differs from canonical checked-in bytes", id)
		}
	}
	if err := verifyReleaseArtifactSet(embeddedReleaseArtifactSet(), FrozenReleasePins(), time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC)); err != nil {
		t.Fatalf("verify embedded public release artifacts: %v", err)
	}
}

func TestP6B2IndependentApproverArtifactsAreReleased(t *testing.T) {
	const (
		approverRootID = "ananke_controlled_repair_rotation_approver_root_v1"
		approverKeyID  = "ananke_controlled_repair_rotation_release_approver_v1"
		approverRole   = "controlled_repair_rotation_release_approver"
		approverDomain = "ananke.controlled-repair.root-rotation-release-approval.v1"
	)
	readPublicInput := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile("testdata/release-v1/" + name)
		if err != nil {
			t.Fatalf("read public approver input %q: %v", name, err)
		}
		return raw
	}
	wantArtifacts := map[string][]byte{
		"rotation_approver_root_certificate_der": readPublicInput("rotation-approver-root-cert.der"),
		"rotation_approver_root_spki_der":        readPublicInput("rotation-approver-root-spki.der"),
		"rotation_approver_certificate_der":      readPublicInput("rotation-approver-cert.der"),
		"rotation_approver_spki_der":             readPublicInput("rotation-approver-spki.der"),
	}

	t.Run("explicit embedded inputs", func(t *testing.T) {
		embedded := embeddedReleaseArtifactSet().byID()
		for id, want := range wantArtifacts {
			got, ok := embedded[id]
			if !ok {
				t.Errorf("embedded public approver artifact %q is missing", id)
				continue
			}
			if !bytes.Equal([]byte(got), want) {
				t.Errorf("embedded public approver artifact %q differs from checked-in bytes", id)
			}
		}
		if len(embedded) != 14 {
			t.Errorf("embedded public artifact count = %d, want 14", len(embedded))
		}
	})

	t.Run("canonical bundle", func(t *testing.T) {
		var bundle map[string]any
		if err := json.Unmarshal([]byte(embeddedReleaseArtifactSet().publicTrustBundle), &bundle); err != nil {
			t.Fatal(err)
		}
		for id, want := range wantArtifacts {
			encoded, ok := bundle[id].(string)
			if !ok {
				t.Errorf("public trust bundle field %q is missing", id)
				continue
			}
			got, err := base64.StdEncoding.Strict().DecodeString(encoded)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("public trust bundle field %q does not contain exact DER: %v", id, err)
			}
		}
	})

	t.Run("manifest hashes", func(t *testing.T) {
		manifest, err := decodeCanonicalReleaseArtifact[releaseManifestDeclaration]([]byte(embeddedReleaseArtifactSet().releaseManifest))
		if err != nil {
			t.Fatal(err)
		}
		got := make(map[string]string, len(manifest.Artifacts))
		for _, entry := range manifest.Artifacts {
			got[entry.ArtifactID] = entry.ContentSHA256
		}
		for id, raw := range wantArtifacts {
			if got[id] != sha256Digest(raw) {
				t.Errorf("manifest hash for %q = %q, want %q", id, got[id], sha256Digest(raw))
			}
		}
		if len(manifest.Artifacts) != 13 {
			t.Errorf("manifest entry count = %d, want 13", len(manifest.Artifacts))
		}
	})

	t.Run("compiled pins and frozen trust", func(t *testing.T) {
		pins := reflect.ValueOf(FrozenReleasePins())
		wantPins := map[string]string{
			"RotationApproverRootCertificateHash": sha256Digest(wantArtifacts["rotation_approver_root_certificate_der"]),
			"RotationApproverRootSPKISHA256":      sha256Digest(wantArtifacts["rotation_approver_root_spki_der"]),
			"RotationApproverCertificateHash":     sha256Digest(wantArtifacts["rotation_approver_certificate_der"]),
			"RotationApproverLeafSPKI":            sha256Digest(wantArtifacts["rotation_approver_spki_der"]),
			"RotationApproverRootID":              approverRootID,
			"RotationApproverKeyID":               approverKeyID,
			"RotationApproverRole":                approverRole,
			"RotationApproverDomain":              approverDomain,
		}
		for field, want := range wantPins {
			value := pins.FieldByName(field)
			if !value.IsValid() {
				t.Errorf("ReleasePins.%s is missing", field)
				continue
			}
			if value.String() != want {
				t.Errorf("ReleasePins.%s = %q, want %q", field, value.String(), want)
			}
		}
		bundleJSON, err := json.Marshal(FrozenTrustBundle())
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"rotation_approver_root", "rotation_approver"} {
			if !bytes.Contains(bundleJSON, []byte(`"`+field+`"`)) {
				t.Errorf("frozen trust bundle field %q is missing", field)
			}
		}
	})
}

func TestP6B2RotationPolicyFixesPublishedApproverIdentity(t *testing.T) {
	const (
		approverKeyID  = "ananke_controlled_repair_rotation_release_approver_v1"
		approverSPKI   = "sha256:95b82df9b281943f2229a0ab6d830c4b0133eda9bcec9eee8cffdd4f82db1d1d"
		approverRole   = "controlled_repair_rotation_release_approver"
		approverDomain = "ananke.controlled-repair.root-rotation-release-approval.v1"
	)
	var policy map[string]any
	if err := json.Unmarshal([]byte(embeddedReleaseArtifactSet().rotationPolicy), &policy); err != nil {
		t.Fatal(err)
	}
	approval, ok := policy["independent_release_approval"].(map[string]any)
	if !ok {
		t.Fatal("independent release approval declaration is missing")
	}
	for field, want := range map[string]string{
		"signer_key_id":      approverKeyID,
		"signer_spki_sha256": approverSPKI,
		"signer_role":        approverRole,
		"signature_domain":   approverDomain,
	} {
		t.Run(field, func(t *testing.T) {
			if got, _ := approval[field].(string); got != want {
				t.Fatalf("fixed approval %s = %q, want %q", field, got, want)
			}
		})
	}
	identity := releasedRotationApprovalSignerIdentity()
	if err := validateRotationApprovalSignerIdentity(identity); err != nil {
		t.Fatalf("released approval signer identity rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*rotationApprovalSignerIdentity)
	}{
		{name: "record signer key ID mismatch", mutate: func(value *rotationApprovalSignerIdentity) { value.SignerKeyID = "other_approver" }},
		{name: "record signer SPKI mismatch", mutate: func(value *rotationApprovalSignerIdentity) { value.SignerSPKISHA256 = testHash("other-approver-spki") }},
		{name: "record signer role mismatch", mutate: func(value *rotationApprovalSignerIdentity) { value.SignerRole = RepairAttestorRole }},
		{name: "record signature domain mismatch", mutate: func(value *rotationApprovalSignerIdentity) { value.SignatureDomain = SignatureDomain }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := identity
			test.mutate(&changed)
			if err := validateRotationApprovalSignerIdentity(changed); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("mismatched approval record identity error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6PublicReleaseDeclarationsContainNoPrivateOrInstallationPayload(t *testing.T) {
	artifacts := embeddedReleaseArtifactSet().byID()
	if len(artifacts) != 14 {
		t.Fatalf("public release input count = %d, want 14", len(artifacts))
	}
	for id, raw := range artifacts {
		for _, forbidden := range [][]byte{
			[]byte("-----BEGIN PRIVATE KEY-----"), []byte("PRIVATE KEY"), []byte("private-key"),
			[]byte(".key"), []byte("id_ed25519"), []byte("/Users/"), []byte("/private/"), []byte("/tmp/"),
		} {
			if bytes.Contains([]byte(raw), forbidden) {
				t.Errorf("public release input %q contains forbidden private or path marker %q", id, forbidden)
			}
		}
	}
	for _, id := range []string{"contract_release", "public_trust_bundle", "release_manifest", "rotation_policy", "supervisor_policy", "supervisor_profile"} {
		var value any
		if err := json.Unmarshal([]byte(artifacts[id]), &value); err != nil {
			t.Fatalf("decode public artifact %q: %v", id, err)
		}
		assertNoPrivateOrInstallationPayload(t, id, value)
	}
	for _, id := range []string{"repair_root_spki_der", "repair_attestor_spki_der", "rotation_approver_root_spki_der", "rotation_approver_spki_der"} {
		if _, err := x509.ParsePKIXPublicKey([]byte(artifacts[id])); err != nil {
			t.Fatalf("parse public key artifact %q: %v", id, err)
		}
	}
	for _, id := range []string{"repair_root_certificate_der", "repair_attestor_certificate_der", "rotation_approver_root_certificate_der", "rotation_approver_certificate_der"} {
		certificate, err := x509.ParseCertificate([]byte(artifacts[id]))
		if err != nil {
			t.Fatalf("parse public certificate artifact %q: %v", id, err)
		}
		if certificate.PublicKey == nil || certificate.PublicKeyAlgorithm != x509.Ed25519 {
			t.Fatalf("certificate artifact %q does not expose the expected Ed25519 public key", id)
		}
	}
}

func TestP6RotationPolicyRejectsUnknownNestedMembers(t *testing.T) {
	var policy map[string]any
	if err := json.Unmarshal([]byte(embeddedReleaseArtifactSet().rotationPolicy), &policy); err != nil {
		t.Fatal(err)
	}
	approval, ok := policy["independent_release_approval"].(map[string]any)
	if !ok {
		t.Fatal("independent release approval declaration is missing")
	}
	approval["unknown_signature_authority"] = true
	mutated := canonicalTestArtifact(t, policy)
	if _, err := decodeCanonicalReleaseArtifact[rotationPolicyDeclaration](mutated); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unknown nested rotation member error = %v, want %v", err, ErrInvalidContract)
	}
}

func TestP6RotationPolicyFreezesFutureSignedRecordFields(t *testing.T) {
	policy, err := decodeCanonicalReleaseArtifact[rotationPolicyDeclaration]([]byte(embeddedReleaseArtifactSet().rotationPolicy))
	if err != nil {
		t.Fatal(err)
	}
	wantProposalFields := []string{
		"schema_version", "proposal_hash", "current_root_id", "current_root_certificate_hash",
		"successor_root_id", "successor_root_certificate_hash", "successor_root_spki_sha256",
		"successor_not_before", "successor_not_after", "current_root_not_after", "release_manifest_hash",
	}
	wantCrossSignatureFields := []string{
		"schema_version", "cross_signature_hash", "signature_domain", "signer_role", "signer_root_id",
		"signer_spki_sha256", "proposal_hash", "signature_base64",
	}
	wantReleaseApprovalFields := []string{
		"schema_version", "release_approval_hash", "signature_domain", "signer_role", "signer_key_id",
		"signer_spki_sha256", "proposal_hash", "current_root_cross_signature_hash", "approved_at", "signature_base64",
	}
	if !reflect.DeepEqual(policy.ProposalFields, wantProposalFields) {
		t.Fatalf("rotation proposal fields = %q, want %q", policy.ProposalFields, wantProposalFields)
	}
	if !reflect.DeepEqual(policy.CurrentRootCrossSignature.RecordFields, wantCrossSignatureFields) {
		t.Fatalf("current-root cross-signature fields = %q, want %q", policy.CurrentRootCrossSignature.RecordFields, wantCrossSignatureFields)
	}
	if !reflect.DeepEqual(policy.IndependentReleaseApproval.RecordFields, wantReleaseApprovalFields) {
		t.Fatalf("independent release-approval fields = %q, want %q", policy.IndependentReleaseApproval.RecordFields, wantReleaseApprovalFields)
	}
}

func TestP6ReleaseArtifactByteDriftFailsClosed(t *testing.T) {
	validAt := time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC)
	base := embeddedReleaseArtifactSet()
	for id, raw := range base.byID() {
		t.Run(id, func(t *testing.T) {
			mutated, ok := base.withArtifact(id, raw+" ")
			if !ok {
				t.Fatalf("unknown embedded artifact %q", id)
			}
			if err := verifyReleaseArtifactSet(mutated, FrozenReleasePins(), validAt); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("drifted artifact error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6CertificateSPKIRoleAndDomainDriftFailsClosed(t *testing.T) {
	validAt := time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC)
	base := embeddedReleaseArtifactSet()
	for _, test := range []struct {
		name string
		id   string
	}{
		{name: "root certificate", id: "repair_root_certificate_der"},
		{name: "root SPKI", id: "repair_root_spki_der"},
		{name: "attestor certificate", id: "repair_attestor_certificate_der"},
		{name: "attestor SPKI", id: "repair_attestor_spki_der"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := base.byID()[test.id]
			mutatedBytes := append([]byte(nil), []byte(raw)...)
			mutatedBytes[0] ^= 1
			mutated, _ := base.withArtifact(test.id, string(mutatedBytes))
			if err := verifyReleaseArtifactSet(mutated, FrozenReleasePins(), validAt); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("drift error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}

	root, err := x509.ParseCertificate([]byte(base.repairRootCertificateDER))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate([]byte(base.repairAttestorCertificateDER))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		oid  string
	}{
		{name: "repair role", oid: "1.3.6.1.4.1.57264.1.6"},
		{name: "signature domain", oid: "1.3.6.1.4.1.57264.1.7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutated := *leaf
			mutated.Extensions = append([]pkix.Extension(nil), leaf.Extensions...)
			changed := false
			for index := range mutated.Extensions {
				if mutated.Extensions[index].Id.String() == test.oid {
					mutated.Extensions[index].Value = []byte("drifted")
					changed = true
				}
			}
			if !changed {
				t.Fatalf("extension %s is missing", test.oid)
			}
			if err := validateCertificateSemantics(root, &mutated, root.RawSubjectPublicKeyInfo, leaf.RawSubjectPublicKeyInfo); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("extension drift error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6SelfConsistentAttackerBundleFailsAgainstEmbeddedRelease(t *testing.T) {
	base := embeddedReleaseArtifactSet()
	attackerBundle := strings.Replace(base.publicTrustBundle, "ananke_controlled_repair_public_trust_bundle_v1", "attacker_controlled_repair_public_trust_bundle_v1", 1)
	if attackerBundle == base.publicTrustBundle {
		t.Fatal("trust bundle ID replacement did not apply")
	}

	var manifest map[string]any
	if err := json.Unmarshal([]byte(base.releaseManifest), &manifest); err != nil {
		t.Fatal(err)
	}
	entries, ok := manifest["artifacts"].([]any)
	if !ok {
		t.Fatal("release manifest artifacts are not an array")
	}
	updated := false
	for _, entryValue := range entries {
		entry, ok := entryValue.(map[string]any)
		if ok && entry["artifact_id"] == "public_trust_bundle" {
			entry["content_sha256"] = sha256Digest([]byte(attackerBundle))
			updated = true
		}
	}
	if !updated {
		t.Fatal("public trust bundle manifest entry is missing")
	}
	attackerManifest := string(canonicalTestArtifact(t, manifest))
	attacker, _ := base.withArtifact("public_trust_bundle", attackerBundle)
	attacker, _ = attacker.withArtifact("release_manifest", attackerManifest)
	attackerPins := FrozenReleasePins()
	attackerPins.TrustBundleHash = sha256Digest([]byte(attackerBundle))
	attackerPins.ReleaseManifestHash = sha256Digest([]byte(attackerManifest))
	attackerPins.ReleasePinsHash = mustRecordHash(t, attackerPins, "release_pins_hash")
	if err := verifyReleaseArtifactSet(attacker, attackerPins, time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("attacker bundle error = %v, want %v", err, ErrInvalidContract)
	}
}

func TestP6ContractStrictJSONAndUnicodeRejections(t *testing.T) {
	raw, _ := readFixture(t)

	firstKeyEnd := bytes.Index(raw, []byte{':'})
	if firstKeyEnd < 0 {
		t.Fatal("fixture has no top-level key")
	}
	firstComma := bytes.Index(raw, []byte{','})
	if firstComma < 0 {
		t.Fatal("fixture has no top-level comma")
	}
	firstMember := raw[1:firstComma]
	duplicateTop := append([]byte{'{'}, append(firstMember, append([]byte{','}, raw[1:]...)...)...)

	nestedNeedle := []byte(`"repair_attestor":{`)
	nestedAt := bytes.Index(raw, nestedNeedle)
	if nestedAt < 0 {
		t.Fatal("fixture has no repair_attestor object")
	}
	nestedValueAt := nestedAt + len(nestedNeedle)
	nestedComma := bytes.IndexByte(raw[nestedValueAt:], ',')
	if nestedComma < 0 {
		t.Fatal("repair_attestor object has no member")
	}
	nestedFirst := raw[nestedValueAt : nestedValueAt+nestedComma]
	duplicateNested := append([]byte(nil), raw[:nestedValueAt]...)
	duplicateNested = append(duplicateNested, nestedFirst...)
	duplicateNested = append(duplicateNested, ',')
	duplicateNested = append(duplicateNested, raw[nestedValueAt:]...)

	invalidUTF8 := append([]byte(nil), raw...)
	quote := bytes.Index(invalidUTF8, []byte(`ananke.controlled-repair-contract-fixture.v1`))
	if quote < 0 {
		t.Fatal("fixture schema not found")
	}
	invalidUTF8[quote] = 0xff

	loneSurrogate := bytes.Replace(raw, []byte(`ananke.controlled-repair-contract-fixture.v1`), []byte(`\ud800`), 1)
	noncharacter := bytes.Replace(raw, []byte(`ananke.controlled-repair-contract-fixture.v1`), []byte(`\ufdd0`), 1)
	trailing := append(append([]byte(nil), raw...), []byte(`{}`)...)
	noncanonical := append([]byte{' '}, raw...)

	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{name: "duplicate top-level key", raw: duplicateTop},
		{name: "duplicate nested key", raw: duplicateNested},
		{name: "trailing JSON", raw: trailing},
		{name: "invalid UTF-8", raw: invalidUTF8},
		{name: "lone surrogate", raw: loneSurrogate},
		{name: "Unicode noncharacter", raw: noncharacter},
		{name: "noncanonical whitespace", raw: noncanonical},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeFixture(test.raw); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("DecodeFixture error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6ContractRejectsUnknownKeysAtEveryNestingLevel(t *testing.T) {
	raw, _ := readFixture(t)
	var generic any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	variants := unknownFieldVariants(t, generic)
	if len(variants) < 20 {
		t.Fatalf("only %d object nesting points found", len(variants))
	}
	for index, variant := range variants {
		mutated, err := canonicalBytes(variant)
		if err != nil {
			t.Fatalf("canonical unknown-field variant %d: %v", index, err)
		}
		if _, err := DecodeFixture(mutated); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("unknown-field variant %d error = %v, want %v", index, err, ErrInvalidContract)
		}
	}

	secret := bytes.Replace(raw, []byte(`"schema_version":"ananke.controlled-repair-contract-fixture.v1"`), []byte(`"private_key":"do-not-leak-this-value","schema_version":"ananke.controlled-repair-contract-fixture.v1"`), 1)
	_, err := DecodeFixture(secret)
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("secret-looking unknown field error = %v", err)
	}
	if strings.Contains(err.Error(), "private_key") || strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("secret-looking unknown field leaked in diagnostic: %q", err)
	}
}

func TestP6TrustBootstrapAndRoleSeparationNegativeVectors(t *testing.T) {
	_, base := readFixture(t)
	now := mustTime(t, base.Dispatch.CreatedAt)
	other := testHash("other")

	tests := []struct {
		name   string
		mutate func(*ContractFixture)
	}{
		{name: "self-consistent attacker root and bundle", mutate: func(f *ContractFixture) {
			f.TrustBundle.Root.RootID = "attacker_repair_root_v1"
			f.TrustBundle.Root.RootSPKISHA256 = testHash("attacker-root-spki")
			f.TrustBundle.RepairAttestor.IssuerRootID = f.TrustBundle.Root.RootID
			f.TrustBundle.RepairAttestor.SubjectSPKISHA256 = testHash("attacker-leaf-spki")
			rehashTrust(t, f)
			f.ReleasePins.TrustBundleHash = f.TrustBundle.TrustBundleHash
			f.ReleasePins.RepairAttestorCertificateHash = f.TrustBundle.RepairAttestor.CertificateHash
			f.ReleasePins.RepairAttestorRootID = f.TrustBundle.Root.RootID
			f.ReleasePins.RepairAttestorLeafSPKI = f.TrustBundle.RepairAttestor.SubjectSPKISHA256
			rehashPinsAndFixture(t, f)
		}},
		{name: "release pin mismatch", mutate: func(f *ContractFixture) { f.ReleasePins.BuildIdentityHash = other; rehashPinsAndFixture(t, f) }},
		{name: "protocol leaf reused", mutate: func(f *ContractFixture) {
			f.TrustBundle.RepairAttestor.SubjectSPKISHA256 = ProtocolAdapterLeafSPKIHash
			rehashTrust(t, f)
			f.ReleasePins.TrustBundleHash = f.TrustBundle.TrustBundleHash
			f.ReleasePins.RepairAttestorCertificateHash = f.TrustBundle.RepairAttestor.CertificateHash
			f.ReleasePins.RepairAttestorLeafSPKI = ProtocolAdapterLeafSPKIHash
			rehashPinsAndFixture(t, f)
		}},
		{name: "protocol role reused", mutate: func(f *ContractFixture) {
			f.TrustBundle.RepairAttestor.Role = ProtocolAdapterRole
			rehashTrustAndFixture(t, f)
		}},
		{name: "wrong leaf SPKI", mutate: func(f *ContractFixture) {
			f.TrustBundle.RepairAttestor.SubjectSPKISHA256 = other
			rehashTrustAndFixture(t, f)
		}},
		{name: "wrong root", mutate: func(f *ContractFixture) {
			f.TrustBundle.RepairAttestor.IssuerRootID = "other_root"
			rehashTrustAndFixture(t, f)
		}},
		{name: "wrong bundle", mutate: func(f *ContractFixture) { f.TrustBundle.BundleID = "other_bundle"; rehashTrustAndFixture(t, f) }},
		{name: "revoked leaf", mutate: func(f *ContractFixture) {
			f.TrustBundle.RepairAttestor.RevocationState = "revoked"
			rehashTrustAndFixture(t, f)
		}},
		{name: "revoked root", mutate: func(f *ContractFixture) { f.TrustBundle.Root.RevocationState = "revoked"; rehashTrustAndFixture(t, f) }},
		{name: "stale certificate", mutate: func(f *ContractFixture) {
			f.TrustBundle.RepairAttestor.NotAfter = now.Add(-time.Second).Format(time.RFC3339Nano)
			rehashTrustAndFixture(t, f)
		}},
		{name: "future certificate", mutate: func(f *ContractFixture) {
			f.TrustBundle.RepairAttestor.ValidFrom = now.Add(time.Second).Format(time.RFC3339Nano)
			rehashTrustAndFixture(t, f)
		}},
		{name: "TOFU", mutate: func(f *ContractFixture) {
			f.ReleasePins.TrustBootstrapMode = "trust_on_first_use"
			rehashPinsAndFixture(t, f)
		}},
		{name: "database replacement", mutate: func(f *ContractFixture) {
			f.ReleasePins.DatabaseTrustMode = "database_replaceable"
			rehashPinsAndFixture(t, f)
		}},
		{name: "runtime install", mutate: func(f *ContractFixture) {
			f.ReleasePins.RuntimeInstallMode = "runtime_install_allowed"
			rehashPinsAndFixture(t, f)
		}},
		{name: "permissive verifier", mutate: func(f *ContractFixture) {
			f.ReleasePins.VerifierSelection = "caller_injected_permissive"
			rehashPinsAndFixture(t, f)
		}},
		{name: "signature domain", mutate: func(f *ContractFixture) {
			f.ReleasePins.SignatureDomain = "ananke.other.v1"
			rehashPinsAndFixture(t, f)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			test.mutate(&fixture)
			if err := VerifyFixture(fixture, now, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("VerifyFixture error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6RotationPolicyForbidsUnmaterializedSuccessors(t *testing.T) {
	raw, base := readFixture(t)
	now := mustTime(t, base.Dispatch.CreatedAt)
	tests := []struct {
		name   string
		mutate func(*ContractFixture)
	}{
		{name: "normal request rotates", mutate: func(f *ContractFixture) {
			f.Authorization.Scope.RotationMode = "rotate"
			rehashAuthorizationChain(t, f)
		}},
		{name: "successor state without material", mutate: func(f *ContractFixture) {
			f.Rotation.State = "successor_authorized"
			rehashRotationAndFixture(t, f)
		}},
		{name: "current root mismatch", mutate: func(f *ContractFixture) {
			f.Rotation.CurrentRootID = "other_root"
			rehashRotationAndFixture(t, f)
		}},
		{name: "rotation policy mismatch", mutate: func(f *ContractFixture) {
			f.Rotation.RotationPolicyHash = testHash("other-rotation-policy")
			rehashRotationAndFixture(t, f)
		}},
		{name: "manifest mismatch", mutate: func(f *ContractFixture) {
			f.Rotation.ReleaseManifestHash = testHash("other-manifest")
			rehashRotationAndFixture(t, f)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			test.mutate(&fixture)
			if err := VerifyFixture(fixture, now, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("VerifyFixture error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}

	for _, field := range []string{"successor_root_id", "successor_root_spki_sha256", "effective_at", "current_root_cross_signature_hash", "release_approval_hash"} {
		mutated := bytes.Replace(raw, []byte(`"rotation":{`), []byte(`"rotation":{"`+field+`":"unmaterialized",`), 1)
		if bytes.Equal(mutated, raw) {
			t.Fatalf("rotation object not found for field %q", field)
		}
		var generic any
		if err := json.Unmarshal(mutated, &generic); err != nil {
			t.Fatalf("decode mutation for canonicalization: %v", err)
		}
		canonical := canonicalTestArtifact(t, generic)
		if _, err := DecodeFixture(canonical); !errors.Is(err, ErrInvalidContract) {
			t.Fatalf("unmaterialized field %q error = %v, want %v", field, err, ErrInvalidContract)
		}
	}
}

func TestP6AuthorizationFreshnessExactBoundaries(t *testing.T) {
	_, base := readFixture(t)
	now := mustTime(t, base.Dispatch.CreatedAt)

	for _, test := range []struct {
		name  string
		age   time.Duration
		valid bool
	}{
		{name: "N-1", age: MaxApprovalAge - time.Nanosecond, valid: true},
		{name: "N", age: MaxApprovalAge, valid: true},
		{name: "N+1", age: MaxApprovalAge + time.Nanosecond, valid: false},
		{name: "year old", age: 365 * 24 * time.Hour, valid: false},
	} {
		t.Run("approval age "+test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			approved := now.Add(-test.age)
			fixture.Authorization.Approval.ApprovedAt = approved.Format(time.RFC3339Nano)
			fixture.Authorization.Approval.NotAfter = approved.Add(MaxAuthorizationLifetime).Format(time.RFC3339Nano)
			fixture.Dispatch.DispatchNotAfter = now.Add(time.Minute).Format(time.RFC3339Nano)
			rehashAuthorizationChain(t, &fixture)
			err := verifyFixtureWithMatchedAuthority(t, fixture, now, ValidationAdmission)
			if test.valid && err != nil {
				t.Fatalf("boundary rejected: %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("boundary error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}

	for _, test := range []struct {
		name     string
		lifetime time.Duration
		valid    bool
	}{
		{name: "N-1", lifetime: MaxAuthorizationLifetime - time.Nanosecond, valid: true},
		{name: "N", lifetime: MaxAuthorizationLifetime, valid: true},
		{name: "N+1", lifetime: MaxAuthorizationLifetime + time.Nanosecond, valid: false},
	} {
		t.Run("authorization lifetime "+test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			approved := now.Add(-time.Minute)
			fixture.Authorization.Approval.ApprovedAt = approved.Format(time.RFC3339Nano)
			fixture.Authorization.Approval.NotAfter = approved.Add(test.lifetime).Format(time.RFC3339Nano)
			fixture.Dispatch.DispatchNotAfter = now.Add(time.Minute).Format(time.RFC3339Nano)
			rehashAuthorizationChain(t, &fixture)
			err := verifyFixtureWithMatchedAuthority(t, fixture, now, ValidationAdmission)
			if test.valid && err != nil {
				t.Fatalf("boundary rejected: %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("boundary error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6AdmissionAndEffectTimeFreshness(t *testing.T) {
	_, base := readFixture(t)
	admission := mustTime(t, base.Dispatch.CreatedAt)
	if err := VerifyFixture(base, admission, ValidationAdmission); err != nil {
		t.Fatalf("valid admission: %v", err)
	}
	if err := VerifyFixture(base, mustTime(t, base.Authorization.Approval.NotAfter), ValidationEffect); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("expired authorization effect error = %v", err)
	}
	if err := VerifyFixture(base, mustTime(t, base.Dispatch.DispatchNotAfter), ValidationEffect); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("expired dispatch effect error = %v", err)
	}

	fixture := cloneFixture(t, base)
	fixture.Dispatch.CreatedAt = fixture.Authorization.Approval.NotAfter
	fixture.Dispatch.DispatchNotAfter = mustTime(t, fixture.Authorization.Approval.NotAfter).Add(time.Second).Format(time.RFC3339Nano)
	rehashAuthorizationChain(t, &fixture)
	if err := VerifyFixture(fixture, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("admitted-then-expired dispatch error = %v", err)
	}
}

func TestP6CrossRecordBindingsRejectConsistentlyRehashedSwaps(t *testing.T) {
	_, base := readFixture(t)
	now := mustTime(t, base.Dispatch.CreatedAt)
	other := testHash("swapped")

	tests := []struct {
		name   string
		mutate func(*ContractFixture)
	}{
		{name: "P4 input", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.P4InputHash = other }},
		{name: "P4 bundle", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.P4EvidenceBundleHash = other }},
		{name: "P4 admission", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.P4AdmissionHash = other }},
		{name: "fence claim", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.FullFence.ClaimID = "swapped_claim" }},
		{name: "fence token", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.FullFence.ClaimTokenHash = other }},
		{name: "fence generation", mutate: func(f *ContractFixture) { f.Authorization.Scope.P4.FullFence.FenceGeneration++ }},
		{name: "repository", mutate: func(f *ContractFixture) { f.Authorization.Scope.Repository.RepositoryIdentityHash = other }},
		{name: "base commit", mutate: func(f *ContractFixture) { f.Authorization.Scope.Repository.BaseCommit = strings.Repeat("a", 40) }},
		{name: "base tree", mutate: func(f *ContractFixture) { f.Authorization.Scope.Repository.BaseTree = strings.Repeat("b", 40) }},
		{name: "writable path", mutate: func(f *ContractFixture) { f.Authorization.Scope.WritablePaths[0].RepositoryRelativePathHash = other }},
		{name: "test profile", mutate: func(f *ContractFixture) { f.Authorization.Scope.TestProfiles[0].InstalledProfileHash = other }},
		{name: "route", mutate: func(f *ContractFixture) { f.Authorization.Scope.Route.RouteIdentityHash = other }},
		{name: "channel", mutate: func(f *ContractFixture) { f.Authorization.Scope.ChannelBindingHash = other }},
		{name: "peer", mutate: func(f *ContractFixture) { f.Authorization.Scope.ExpectedPeer.UserID++ }},
		{name: "policy", mutate: func(f *ContractFixture) { f.Authorization.Scope.PolicyHash = other }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			test.mutate(&fixture)
			rehashAuthorizationChain(t, &fixture)
			if err := VerifyFixture(fixture, now, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("VerifyFixture error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6OrderedListsAndAttemptBounds(t *testing.T) {
	_, base := readFixture(t)
	now := mustTime(t, base.Dispatch.CreatedAt)

	tests := []struct {
		name   string
		valid  bool
		mutate func(*ContractFixture)
	}{
		{name: "attempt one", valid: true, mutate: func(*ContractFixture) {}},
		{name: "attempt zero", mutate: func(f *ContractFixture) { setAttempt(f, 0, AttemptCap) }},
		{name: "attempt two without verified predecessor", mutate: func(f *ContractFixture) {
			setAttempt(f, 2, AttemptCap)
			f.Authorization.Scope.Attempt.PreviousAuthorizationHash = testHash("attempt-one-authorization")
		}},
		{name: "attempt three", mutate: func(f *ContractFixture) { setAttempt(f, 3, AttemptCap) }},
		{name: "cap one", mutate: func(f *ContractFixture) { setAttempt(f, 1, 1) }},
		{name: "duplicate path", mutate: func(f *ContractFixture) {
			f.Authorization.Scope.WritablePaths = append(f.Authorization.Scope.WritablePaths, f.Authorization.Scope.WritablePaths[0])
		}},
		{name: "duplicate profile", mutate: func(f *ContractFixture) {
			f.Authorization.Scope.TestProfiles = append(f.Authorization.Scope.TestProfiles, f.Authorization.Scope.TestProfiles[0])
		}},
		{name: "path order", mutate: func(f *ContractFixture) {
			f.Authorization.Scope.WritablePaths[0], f.Authorization.Scope.WritablePaths[1] = f.Authorization.Scope.WritablePaths[1], f.Authorization.Scope.WritablePaths[0]
		}},
		{name: "profile order", mutate: func(f *ContractFixture) {
			f.Authorization.Scope.TestProfiles[0], f.Authorization.Scope.TestProfiles[1] = f.Authorization.Scope.TestProfiles[1], f.Authorization.Scope.TestProfiles[0]
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			test.mutate(&fixture)
			rehashAuthorizationChain(t, &fixture)
			err := VerifyFixture(fixture, now, ValidationAdmission)
			if test.valid && err != nil {
				t.Fatalf("valid attempt rejected: %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("invalid attempt/list error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestP6DispatchReplayIdentityAndConflict(t *testing.T) {
	_, base := readFixture(t)
	expected := authorityFromAuthorization(base.Authorization)
	verified, err := VerifyAuthorization(expected, base.Authorization, nil, mustTime(t, base.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	existing := canonicalTestArtifact(t, base.Dispatch)
	replay := append([]byte(nil), existing...)
	disposition, err := ClassifyDispatchReplay(expected, verified, existing, replay)
	if err != nil || disposition != DispatchExactReplay {
		t.Fatalf("exact restart replay = %q, %v; want %q, nil", disposition, err, DispatchExactReplay)
	}

	conflicts := []ImmutableDispatch{cloneDispatch(t, base.Dispatch), cloneDispatch(t, base.Dispatch), cloneDispatch(t, base.Dispatch)}
	conflicts[0].Request.RequestID = "conflicting_request"
	conflicts[1].ChannelBindingHash = testHash("conflicting-channel")
	conflicts[2].SelectedSupervisorPolicyHash = testHash("conflicting-policy")
	for index := range conflicts {
		conflicts[index].Request.RequestHash = mustRecordHash(t, conflicts[index].Request, "request_hash")
		conflicts[index].DispatchHash = mustRecordHash(t, conflicts[index], "dispatch_hash")
		incoming := canonicalTestArtifact(t, conflicts[index])
		if disposition, err := ClassifyDispatchReplay(expected, verified, existing, incoming); disposition != DispatchConflict || !errors.Is(err, ErrDispatchConflict) {
			t.Fatalf("conflict %d = %q, %v; want %q, %v", index, disposition, err, DispatchConflict, ErrDispatchConflict)
		}
	}
}

func TestP6A2ExternalDynamicAuthorityAcceptsIndependentlyTrustedValues(t *testing.T) {
	_, base := readFixture(t)
	fixture := cloneFixture(t, base)
	fixture.Authorization.Scope.Repository.RepositoryIdentity = "example.invalid/independently-trusted-repair"
	fixture.Authorization.Scope.Repository.RepositoryIdentityHash = testHash("independently-trusted-repository")
	fixture.Authorization.Scope.Repository.BaseCommit = strings.Repeat("c", 40)
	fixture.Authorization.Scope.Repository.BaseTree = strings.Repeat("d", 40)
	fixture.Authorization.Approval.ApprovalID = "independent_gui_event_002"
	fixture.Authorization.Approval.OperatorIdentity = "independently_authenticated_operator"
	fixture.Authorization.Approval.OperatorIdentityHash = testHash("independently-authenticated-operator")
	fixture.Authorization.Approval.AuthenticationProvenance = "independently_authenticated_gui_event"
	fixture.Authorization.Approval.GUIProvenanceHash = testHash("independent-gui-provenance-event-002")
	rehashAuthorizationChain(t, &fixture)

	expected := authorityFromAuthorization(fixture.Authorization)
	verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil || verified == nil {
		t.Fatalf("independently trusted dynamic authority rejected: %v", err)
	}
}

func TestP6A2AttemptTwoRejectsFabricatedPredecessorAndReusedGUIEvent(t *testing.T) {
	_, base := readFixture(t)
	fixture := cloneFixture(t, base)
	setAttempt(&fixture, 2, AttemptCap)
	fixture.Authorization.Scope.Attempt.PreviousAuthorizationHash = testHash("fabricated-unobserved-predecessor")
	rehashAuthorizationChain(t, &fixture)
	expected := authorityFromAuthorization(fixture.Authorization)

	if _, err := VerifyAuthorization(expected, fixture.Authorization, nil, mustTime(t, fixture.Dispatch.CreatedAt), ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("attempt 2 accepted fabricated predecessor and reused GUI event: %v", err)
	}
}

func TestP6A2ReplayRejectsSameStructFromAlteredBytes(t *testing.T) {
	_, base := readFixture(t)
	expected := authorityFromAuthorization(base.Authorization)
	verified, err := VerifyAuthorization(expected, base.Authorization, nil, mustTime(t, base.Dispatch.CreatedAt), ValidationAdmission)
	if err != nil {
		t.Fatal(err)
	}
	canonical := canonicalTestArtifact(t, base.Dispatch)
	altered := append(append([]byte(nil), canonical...), '\n')
	disposition, err := ClassifyDispatchReplay(expected, verified, canonical, altered)
	if disposition != DispatchConflict || !errors.Is(err, ErrDispatchConflict) {
		t.Fatalf("same asserted hash from altered bytes = %q, %v; want %q, %v", disposition, err, DispatchConflict, ErrDispatchConflict)
	}
}

func TestP6A2AcceptanceVectorsAreExecutableNotFixtureClaims(t *testing.T) {
	raw, _ := readFixture(t)
	if bytes.Contains(raw, []byte(`"acceptance_vectors"`)) || bytes.Contains(raw, []byte(`"linked_hashes_recomputed"`)) {
		t.Fatal("fixture still carries decorative acceptance-vector claims")
	}
}

type leakyJSONMarshaler struct{}

func (leakyJSONMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("probe-private-value")
}

func TestP6A2GenericHashErrorsNeverLeakMarshalerDiagnostics(t *testing.T) {
	_, err := hashRecord(leakyJSONMarshaler{}, "hash")
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("generic hash error = %v, want %v", err, ErrInvalidContract)
	}
	if strings.Contains(err.Error(), "probe-private-value") || strings.Contains(err.Error(), "leakyJSONMarshaler") {
		t.Fatalf("generic hash error leaked caller-controlled diagnostic: %q", err)
	}
}

func TestP6MalformedHashesAndTimestampsFailClosed(t *testing.T) {
	_, base := readFixture(t)
	now := mustTime(t, base.Dispatch.CreatedAt)
	tests := []struct {
		name   string
		mutate func(*ContractFixture)
	}{
		{name: "short hash", mutate: func(f *ContractFixture) { f.Dispatch.ChannelBindingHash = "sha256:abc" }},
		{name: "uppercase hash", mutate: func(f *ContractFixture) { f.Dispatch.ChannelBindingHash = "sha256:" + strings.Repeat("A", 64) }},
		{name: "wrong prefix", mutate: func(f *ContractFixture) { f.Dispatch.ChannelBindingHash = "SHA256:" + strings.Repeat("a", 64) }},
		{name: "impossible date", mutate: func(f *ContractFixture) { f.Dispatch.CreatedAt = "2026-02-30T12:00:00Z" }},
		{name: "non-UTC offset", mutate: func(f *ContractFixture) { f.Dispatch.CreatedAt = "2026-07-26T14:00:00+02:00" }},
		{name: "noncanonical UTC", mutate: func(f *ContractFixture) { f.Dispatch.CreatedAt = "2026-07-26t12:00:00z" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneFixture(t, base)
			test.mutate(&fixture)
			fixture.Dispatch.DispatchHash = mustRecordHash(t, fixture.Dispatch, "dispatch_hash")
			fixture.FixtureHash = mustRecordHash(t, fixture, "fixture_hash")
			if err := VerifyFixture(fixture, now, ValidationAdmission); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("VerifyFixture error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func readFixture(t *testing.T) ([]byte, ContractFixture) {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fixture, err := DecodeFixture(raw)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return raw, fixture
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse fixture time %q: %v", value, err)
	}
	return parsed
}

func testHash(label string) string {
	return sha256Digest([]byte(label))
}
func requiredReleaseArtifactBytes(t *testing.T) map[string][]byte {
	t.Helper()
	read := func(name string) []byte {
		raw, err := os.ReadFile("testdata/release-v1/" + name)
		if err != nil {
			t.Fatalf("read public release input %q: %v", name, err)
		}
		return raw
	}
	artifacts := map[string][]byte{
		"repair_root_certificate_der":            read("repair-root-cert.der"),
		"repair_root_spki_der":                   read("repair-root-spki.der"),
		"repair_attestor_certificate_der":        read("repair-attestor-cert.der"),
		"repair_attestor_spki_der":               read("repair-attestor-spki.der"),
		"rotation_approver_root_certificate_der": read("rotation-approver-root-cert.der"),
		"rotation_approver_root_spki_der":        read("rotation-approver-root-spki.der"),
		"rotation_approver_certificate_der":      read("rotation-approver-cert.der"),
		"rotation_approver_spki_der":             read("rotation-approver-spki.der"),
	}
	artifacts["public_trust_bundle"] = canonicalTestArtifact(t, map[string]any{
		"bundle_id":                              "ananke_controlled_repair_public_trust_bundle_v1",
		"repair_attestor_certificate_der":        base64.StdEncoding.EncodeToString(artifacts["repair_attestor_certificate_der"]),
		"repair_attestor_spki_der":               base64.StdEncoding.EncodeToString(artifacts["repair_attestor_spki_der"]),
		"repair_root_certificate_der":            base64.StdEncoding.EncodeToString(artifacts["repair_root_certificate_der"]),
		"repair_root_spki_der":                   base64.StdEncoding.EncodeToString(artifacts["repair_root_spki_der"]),
		"rotation_approver_root_certificate_der": base64.StdEncoding.EncodeToString(artifacts["rotation_approver_root_certificate_der"]),
		"rotation_approver_root_spki_der":        base64.StdEncoding.EncodeToString(artifacts["rotation_approver_root_spki_der"]),
		"rotation_approver_certificate_der":      base64.StdEncoding.EncodeToString(artifacts["rotation_approver_certificate_der"]),
		"rotation_approver_spki_der":             base64.StdEncoding.EncodeToString(artifacts["rotation_approver_spki_der"]),
		"schema_version":                         "ananke.controlled-repair-public-trust-bundle.v1",
	})
	artifacts["supervisor_policy"] = canonicalTestArtifact(t, map[string]any{
		"ananke_key_custody":  "public_material_only_no_private_key",
		"database_trust_mode": "mirror_only_no_replacement",
		"descriptor_observation": map[string]any{
			"content_hash_required":           true,
			"device_inode_stability_required": true,
			"nofollow_required":               true,
			"owner_mode_check_required":       true,
			"regular_file_required":           true,
			"schema_version":                  "ananke.controlled-repair-descriptor-observation-requirements.v1",
		},
		"policy_id":            "controlled_repair_supervisor_policy_v1",
		"rotation_mode":        "forbidden_v1",
		"runtime_install_mode": "forbidden",
		"schema_version":       "ananke.controlled-repair-supervisor-policy.v1",
		"trust_bootstrap_mode": "release_pinned_only",
		"verifier_selection":   "compiled_embedded_release_artifacts_only",
	})
	artifacts["supervisor_profile"] = canonicalTestArtifact(t, map[string]any{
		"policy_id":            "controlled_repair_supervisor_policy_v1",
		"profile_id":           "controlled_repair_supervisor_local_v1",
		"repair_attestor_role": RepairAttestorRole,
		"rotation_policy_id":   "controlled_repair_trust_rotation_policy_v1",
		"schema_version":       "ananke.controlled-repair-supervisor-profile.v1",
		"signature_domain":     SignatureDomain,
		"trust_bundle_id":      "ananke_controlled_repair_public_trust_bundle_v1",
	})
	artifacts["contract_release"] = canonicalTestArtifact(t, map[string]any{
		"build_mode":           "source_embedded_public_artifacts",
		"contract_id":          "p6_controlled_repair_supervisor_contract_slices_1_2",
		"public_material_only": true,
		"release_id":           "p6_contract_slices_1_2_repair_batch_a1_v1",
		"schema_version":       "ananke.controlled-repair-contract-release.v1",
	})
	artifacts["rotation_policy"] = canonicalTestArtifact(t, map[string]any{
		"activation_rules": map[string]any{
			"activation_requires":    []string{"valid_current_root_cross_signature", "valid_independent_release_approval", "release_manifest_hash_match", "successor_certificate_valid_at_activation"},
			"current_root_not_after": "exclusive",

			"minimum_activation_delay_seconds": 86400,
			"minimum_overlap_seconds":          86400,
			"not_before_rule":                  "successor_not_before_at_or_after_release_approved_at_plus_minimum_activation_delay",
			"overlap_rule":                     "current_root_not_after_at_or_after_successor_not_before_plus_minimum_overlap",
			"successor_not_after":              "exclusive",
		},
		"canonicalization": "RFC8785_JCS",
		"current_root_cross_signature": map[string]any{
			"algorithm": "Ed25519",
			"record_fields": []string{
				"schema_version", "cross_signature_hash", "signature_domain", "signer_role", "signer_root_id",
				"signer_spki_sha256", "proposal_hash", "signature_base64",
			},
			"schema_version":               "ananke.controlled-repair-current-root-cross-signature.v1",
			"signature_domain":             "ananke.controlled-repair.root-rotation-cross-signature.v1",
			"signature_encoding":           "base64_raw_64_bytes",
			"signed_object_members":        []string{"signature_domain", "proposal_hash"},
			"signed_object_schema_version": "ananke.controlled-repair-trust-rotation-proposal.v1",
			"signer_role":                  "controlled_repair_current_root",
		},
		"independent_release_approval": map[string]any{
			"algorithm": "Ed25519",
			"record_fields": []string{
				"schema_version", "release_approval_hash", "signature_domain", "signer_role", "signer_key_id",
				"signer_spki_sha256", "proposal_hash", "current_root_cross_signature_hash", "approved_at", "signature_base64",
			},
			"schema_version":               "ananke.controlled-repair-independent-release-approval.v1",
			"signature_domain":             RotationApproverSignatureDomain,
			"signature_encoding":           "base64_raw_64_bytes",
			"signed_object_members":        []string{"signature_domain", "proposal_hash", "current_root_cross_signature_hash", "approved_at"},
			"signed_object_schema_version": "ananke.controlled-repair-trust-rotation-proposal.v1",
			"signer_key_id":                RotationApproverKeyID,
			"signer_role":                  RotationApproverRole,
			"signer_spki_must_differ_from": "current_root_and_successor_root",
			"signer_spki_sha256":           rotationApproverLeafSPKIHash,
		},
		"policy_id": "controlled_repair_trust_rotation_policy_v1",
		"proposal_fields": []string{
			"schema_version", "proposal_hash", "current_root_id", "current_root_certificate_hash",
			"successor_root_id", "successor_root_certificate_hash", "successor_root_spki_sha256",
			"successor_not_before", "successor_not_after", "current_root_not_after", "release_manifest_hash",
		},
		"proposal_schema_version": "ananke.controlled-repair-trust-rotation-proposal.v1",
		"schema_version":          "ananke.controlled-repair-trust-rotation-policy.v1",
		"v1_fixture_state":        "no_successor_authorized",
	})

	manifestOrder := []string{
		"contract_release", "public_trust_bundle", "repair_attestor_certificate_der", "repair_attestor_spki_der",
		"repair_root_certificate_der", "repair_root_spki_der", "rotation_approver_certificate_der", "rotation_approver_spki_der",
		"rotation_approver_root_certificate_der", "rotation_approver_root_spki_der",
		"rotation_policy", "supervisor_policy", "supervisor_profile",
	}
	entries := make([]map[string]any, len(manifestOrder))
	for index, id := range manifestOrder {
		entries[index] = map[string]any{"artifact_id": id, "content_sha256": sha256Digest(artifacts[id])}
	}
	artifacts["release_manifest"] = canonicalTestArtifact(t, map[string]any{
		"artifacts":      entries,
		"release_id":     "p6_contract_slices_1_2_repair_batch_a1_v1",
		"schema_version": "ananke.controlled-repair-release-manifest.v1",
	})
	return artifacts
}

func canonicalTestArtifact(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := canonicalBytes(value)
	if err != nil {
		t.Fatalf("canonical public release artifact: %v", err)
	}
	return raw
}

func assertCriticalExtension(t *testing.T, certificate *x509.Certificate, oid, want string) {
	t.Helper()
	matches := 0
	for _, extension := range certificate.Extensions {
		if extension.Id.String() != oid {
			continue
		}
		matches++
		if !extension.Critical || string(extension.Value) != want {
			t.Errorf("extension %s = critical %t, value %q; want critical true, value %q", oid, extension.Critical, extension.Value, want)
		}
	}
	if matches != 1 {
		t.Errorf("extension %s count = %d, want 1", oid, matches)
	}
}

func cloneFixture(t *testing.T, value ContractFixture) ContractFixture {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone ContractFixture
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func cloneDispatch(t *testing.T, value ImmutableDispatch) ImmutableDispatch {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone ImmutableDispatch
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func mustRecordHash(t *testing.T, value any, ownField string) string {
	t.Helper()
	hash, err := hashRecord(value, ownField)
	if err != nil {
		t.Fatalf("hash %T excluding %q: %v", value, ownField, err)
	}
	return hash
}

func rehashTrust(t *testing.T, fixture *ContractFixture) {
	t.Helper()
	fixture.FixtureHash = mustRecordHash(t, *fixture, "fixture_hash")
}

func rehashTrustAndFixture(t *testing.T, fixture *ContractFixture) {
	t.Helper()
	rehashTrust(t, fixture)
}

func rehashPinsAndFixture(t *testing.T, fixture *ContractFixture) {
	t.Helper()
	fixture.ReleasePins.ReleasePinsHash = mustRecordHash(t, fixture.ReleasePins, "release_pins_hash")
	fixture.Dispatch.ReleasePinsHash = fixture.ReleasePins.ReleasePinsHash
	fixture.Dispatch.SelectedSupervisorPolicyHash = fixture.ReleasePins.SupervisorPolicyHash
	fixture.Dispatch.SelectedSupervisorProfileHash = fixture.ReleasePins.SupervisorProfileHash
	fixture.Dispatch.DispatchHash = mustRecordHash(t, fixture.Dispatch, "dispatch_hash")
	fixture.FixtureHash = mustRecordHash(t, *fixture, "fixture_hash")
}

func rehashRotationAndFixture(t *testing.T, fixture *ContractFixture) {
	t.Helper()
	fixture.Rotation.RotationHash = mustRecordHash(t, fixture.Rotation, "rotation_hash")
	fixture.FixtureHash = mustRecordHash(t, *fixture, "fixture_hash")
}

func rehashAuthorizationChain(t *testing.T, fixture *ContractFixture) {
	t.Helper()
	scope := &fixture.Authorization.Scope
	scope.P4.FullFence.FenceHash = mustRecordHash(t, scope.P4.FullFence, "fence_hash")
	scope.P4.P4FactHash = mustRecordHash(t, scope.P4, "p4_fact_hash")
	scope.Repository.RepositoryBindingHash = mustRecordHash(t, scope.Repository, "repository_binding_hash")
	for index := range scope.WritablePaths {
		scope.WritablePaths[index].PathBindingHash = mustRecordHash(t, scope.WritablePaths[index], "path_binding_hash")
	}
	for index := range scope.TestProfiles {
		scope.TestProfiles[index].ProfileBindingHash = mustRecordHash(t, scope.TestProfiles[index], "profile_binding_hash")
	}
	scope.Route.RouteBindingHash = mustRecordHash(t, scope.Route, "route_binding_hash")
	scope.ExpectedPeer.PeerIdentityHash = mustRecordHash(t, scope.ExpectedPeer, "peer_identity_hash")
	scope.ScopeHash = mustRecordHash(t, *scope, "scope_hash")

	fixture.Authorization.Approval.ApprovedScopeHash = scope.ScopeHash
	fixture.Authorization.Approval.ApprovalHash = mustRecordHash(t, fixture.Authorization.Approval, "approval_hash")
	fixture.Authorization.PolicyHash = scope.PolicyHash
	fixture.Authorization.ApprovalHash = fixture.Authorization.Approval.ApprovalHash
	fixture.Authorization.AuthorizationHash = mustRecordHash(t, fixture.Authorization, "authorization_hash")

	fixture.Dispatch.AuthorizationHash = fixture.Authorization.AuthorizationHash
	fixture.Dispatch.ApprovalHash = fixture.Authorization.ApprovalHash
	fixture.Dispatch.PolicyHash = fixture.Authorization.PolicyHash
	fixture.Dispatch.Request.AuthorizationHash = fixture.Authorization.AuthorizationHash
	fixture.Dispatch.Request.AttemptNumber = scope.Attempt.AttemptNumber
	fixture.Dispatch.Request.AttemptCap = scope.Attempt.AttemptCap
	fixture.Dispatch.Request.RequestHash = mustRecordHash(t, fixture.Dispatch.Request, "request_hash")
	fixture.Dispatch.DispatchHash = mustRecordHash(t, fixture.Dispatch, "dispatch_hash")
	fixture.FixtureHash = mustRecordHash(t, *fixture, "fixture_hash")
}

func setAttempt(fixture *ContractFixture, number, cap int) {
	fixture.Authorization.Scope.P4.AttemptNumber = number
	fixture.Authorization.Scope.P4.AttemptCap = cap
	fixture.Authorization.Scope.Attempt.AttemptNumber = number
	fixture.Authorization.Scope.Attempt.AttemptCap = cap
	fixture.Dispatch.AttemptNumber = number
	fixture.Dispatch.AttemptCap = cap
}

func unknownFieldVariants(t *testing.T, root any) []any {
	t.Helper()
	var variants []any
	var walk func(any, []any)
	walk = func(value any, path []any) {
		switch typed := value.(type) {
		case map[string]any:
			clone := cloneGeneric(t, root)
			target := genericAtPath(t, clone, path).(map[string]any)
			target["unexpected_contract_field"] = true
			variants = append(variants, clone)
			for key, child := range typed {
				walk(child, append(append([]any(nil), path...), key))
			}
		case []any:
			for index, child := range typed {
				walk(child, append(append([]any(nil), path...), index))
			}
		}
	}
	walk(root, nil)
	return variants
}

func cloneGeneric(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var clone any
	if err := decoder.Decode(&clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func genericAtPath(t *testing.T, value any, path []any) any {
	t.Helper()
	current := value
	for _, component := range path {
		switch typed := component.(type) {
		case string:
			current = current.(map[string]any)[typed]
		case int:
			current = current.([]any)[typed]
		default:
			t.Fatalf("unknown generic path component %T", component)
		}
	}
	return current
}

func verifyFixtureWithMatchedAuthority(t *testing.T, fixture ContractFixture, now time.Time, moment ValidationMoment) error {
	t.Helper()
	expected := authorityFromAuthorization(fixture.Authorization)
	verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, now, moment)
	if err != nil {
		return err
	}
	raw, err := canonicalBytes(fixture.Dispatch)
	if err != nil {
		return err
	}
	_, err = DecodeDispatch(expected, verified, raw, now, moment)
	return err
}

func assertNoAuthorityPayload(t *testing.T, raw []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	forbiddenKeys := map[string]struct{}{
		"argv": {}, "command": {}, "credential": {}, "credentials": {}, "env": {}, "environment": {},
		"executable": {}, "path": {}, "pid": {}, "private_key": {}, "process": {}, "raw_path": {},
		"secret": {}, "socket_path": {},
	}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if _, forbidden := forbiddenKeys[key]; forbidden {
					t.Errorf("fixture contains forbidden authority key %q", key)
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			for _, marker := range []string{"-----BEGIN", "unix://", "/private/", "/tmp/", "AKIA"} {
				if strings.Contains(typed, marker) {
					t.Errorf("fixture contains forbidden authority value marker %q", marker)
				}
			}
		}
	}
	walk(value)
}

func assertNoPrivateOrInstallationPayload(t *testing.T, artifactID string, value any) {
	t.Helper()
	forbiddenKeys := map[string]struct{}{
		"argv": {}, "command": {}, "credential": {}, "credentials": {}, "device": {}, "env": {},
		"environment": {}, "executable": {}, "inode": {}, "mode": {}, "owner_group_id": {},
		"owner_user_id": {}, "path": {}, "private_key": {}, "raw_path": {}, "secret": {}, "size": {},
	}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if _, forbidden := forbiddenKeys[key]; forbidden {
					t.Errorf("public artifact %q contains forbidden key %q", artifactID, key)
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			if strings.HasPrefix(typed, "/") || strings.Contains(typed, "PRIVATE KEY") {
				t.Errorf("public artifact %q contains a path or private material", artifactID)
			}
		}
	}
	walk(value)
}
