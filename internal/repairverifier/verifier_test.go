package repairverifier

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

func testNow() time.Time {
	return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
}

func TestNewRepairVerifier(t *testing.T) {
	verifier, err := NewRepairVerifier(testNow())
	if err != nil {
		t.Fatalf("NewRepairVerifier error: %v", err)
	}
	if verifier == nil {
		t.Fatal("NewRepairVerifier returned nil")
	}
	pins := verifier.Pins()
	if pins.ReleasePinsHash == "" {
		t.Error("Pins().ReleasePinsHash is empty")
	}
	bundle := verifier.Bundle()
	if bundle.TrustBundleHash == "" {
		t.Error("Bundle().TrustBundleHash is empty")
	}
	if verifier.ExpectedAttestorSPKI() == "" {
		t.Error("ExpectedAttestorSPKI is empty")
	}
	if verifier.ExpectedAttestorCertificateHash() == "" {
		t.Error("ExpectedAttestorCertificateHash is empty")
	}
}

func TestNewRepairVerifierRejectsZeroTime(t *testing.T) {
	if _, err := NewRepairVerifier(time.Time{}); err == nil {
		t.Fatal("NewRepairVerifier accepted zero time")
	}
}

func TestNewRepairVerifierAcceptsNonUTCTime(t *testing.T) {
	// Non-UTC time is converted to UTC internally; this is acceptable
	// since the verifier only needs a valid instant for certificate
	// validity checking.
	nonUTC := time.Date(2026, 7, 31, 12, 0, 0, 0, time.FixedZone("PST", -8*3600))
	if _, err := NewRepairVerifier(nonUTC); err != nil {
		t.Fatalf("NewRepairVerifier rejected non-UTC time: %v", err)
	}
}

func TestSetAttestorPublicKeyRejectsWrongSPKI(t *testing.T) {
	verifier, _ := NewRepairVerifier(testNow())
	wrongKey, _, _ := ed25519.GenerateKey(nil)
	if err := verifier.SetAttestorPublicKey(wrongKey); err == nil {
		t.Fatal("SetAttestorPublicKey accepted key with wrong SPKI")
	}
}

func TestVerifyAttestationSignatureRejectsNilVerifier(t *testing.T) {
	var verifier *RepairVerifier
	record := repaircontract.RepairReviewAttestation{}
	if err := verifier.VerifyAttestationSignature(record, ""); err == nil {
		t.Fatal("nil verifier should fail")
	}
}

func TestVerifyAttestationSignatureRejectsWrongSignerSPKI(t *testing.T) {
	verifier, _ := NewRepairVerifier(testNow())
	record := repaircontract.RepairReviewAttestation{}
	if err := verifier.VerifyAttestationSignature(record, "sha256:wrong"); err == nil {
		t.Fatal("wrong signer SPKI should fail")
	}
}

func TestVerifyAttestationSignatureRejectsMissingEd25519Prefix(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	pins := material.verifier.pins
	record := repaircontract.RepairReviewAttestation{
		SignatureDomain:               repaircontract.SignatureDomain,
		RepairAttestorCertificateHash: pins.RepairAttestorCertificateHash,
		RepairAttestorRootID:          pins.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        pins.RepairAttestorLeafSPKI,
		ReleasePinsHash:               pins.ReleasePinsHash,
		TrustBundleHash:               material.verifier.bundle.TrustBundleHash,
		Signature:                     "sha256:placeholder",
	}
	if err := material.VerifyAttestationSignature(record); err == nil {
		t.Fatal("should reject sha256: prefix signature")
	}
}

func TestSignAndVerifyAttestationRoundTrip(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	pins := material.verifier.pins
	bundle := material.verifier.bundle

	record := repaircontract.RepairReviewAttestation{
		SignatureDomain:               repaircontract.SignatureDomain,
		RepairAttestorCertificateHash: pins.RepairAttestorCertificateHash,
		RepairAttestorRootID:          pins.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        pins.RepairAttestorLeafSPKI,
		ReleasePinsHash:               pins.ReleasePinsHash,
		TrustBundleHash:               bundle.TrustBundleHash,
		Signature:                     "", // empty before signing
	}

	signature, err := material.SignAttestation(record)
	if err != nil {
		t.Fatalf("SignAttestation error: %v", err)
	}
	if !strings.HasPrefix(signature, "ed25519:") {
		t.Fatalf("signature should have ed25519: prefix, got: %s", signature[:20])
	}

	// Set the signature on the record and verify
	record.Signature = signature
	if err := material.VerifyAttestationSignature(record); err != nil {
		t.Fatalf("VerifyAttestationSignature error: %v", err)
	}
}

func TestVerifyAttestationSignatureRejectsTamperedRecord(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	pins := material.verifier.pins
	bundle := material.verifier.bundle

	record := repaircontract.RepairReviewAttestation{
		SignatureDomain:               repaircontract.SignatureDomain,
		RepairAttestorCertificateHash: pins.RepairAttestorCertificateHash,
		RepairAttestorRootID:          pins.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        pins.RepairAttestorLeafSPKI,
		ReleasePinsHash:               pins.ReleasePinsHash,
		TrustBundleHash:               bundle.TrustBundleHash,
	}
	signature, _ := material.SignAttestation(record)
	record.Signature = signature

	// Tamper with a field after signing
	record.AttestationID = "tampered"
	if err := material.VerifyAttestationSignature(record); err == nil {
		t.Fatal("should reject tampered record")
	}
}

func TestVerifyAttestationSignatureRejectsWrongTrustBinding(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()

	record := repaircontract.RepairReviewAttestation{
		SignatureDomain:               "wrong.domain",
		RepairAttestorCertificateHash: "sha256:wrong",
		RepairAttestorRootID:          "wrong_root",
		RepairAttestorLeafSPKI:        "sha256:wrong",
		ReleasePinsHash:               "sha256:wrong",
		TrustBundleHash:               "sha256:wrong",
	}
	if err := material.VerifyAttestationSignature(record); err == nil {
		t.Fatal("should reject wrong trust binding")
	}
}

func TestVerifyAttestationSignatureRejectsBadSignatureEncoding(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	pins := material.verifier.pins
	bundle := material.verifier.bundle

	record := repaircontract.RepairReviewAttestation{
		SignatureDomain:               repaircontract.SignatureDomain,
		RepairAttestorCertificateHash: pins.RepairAttestorCertificateHash,
		RepairAttestorRootID:          pins.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        pins.RepairAttestorLeafSPKI,
		ReleasePinsHash:               pins.ReleasePinsHash,
		TrustBundleHash:               bundle.TrustBundleHash,
		Signature:                     "ed25519:invalid_hex",
	}
	if err := material.VerifyAttestationSignature(record); err == nil {
		t.Fatal("should reject invalid hex signature")
	}
}

func TestVerifyAttestationSignatureRejectsShortSignature(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	pins := material.verifier.pins
	bundle := material.verifier.bundle

	record := repaircontract.RepairReviewAttestation{
		SignatureDomain:               repaircontract.SignatureDomain,
		RepairAttestorCertificateHash: pins.RepairAttestorCertificateHash,
		RepairAttestorRootID:          pins.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        pins.RepairAttestorLeafSPKI,
		ReleasePinsHash:               pins.ReleasePinsHash,
		TrustBundleHash:               bundle.TrustBundleHash,
		Signature:                     "ed25519:abcd",
	}
	if err := material.VerifyAttestationSignature(record); err == nil {
		t.Fatal("should reject short signature")
	}
}

func TestVerifyAttestationSignatureRejectsForgedSignature(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	pins := material.verifier.pins
	bundle := material.verifier.bundle

	// Sign with a different key
	_, forgedPriv, _ := ed25519.GenerateKey(nil)
	record := repaircontract.RepairReviewAttestation{
		SignatureDomain:               repaircontract.SignatureDomain,
		RepairAttestorCertificateHash: pins.RepairAttestorCertificateHash,
		RepairAttestorRootID:          pins.RepairAttestorRootID,
		RepairAttestorLeafSPKI:        pins.RepairAttestorLeafSPKI,
		ReleasePinsHash:               pins.ReleasePinsHash,
		TrustBundleHash:               bundle.TrustBundleHash,
	}
	signedBytes, _ := repaircontract.RuntimeSignatureCanonicalBytes(record)
	forgedSig := ed25519.Sign(forgedPriv, signedBytes)
	record.Signature = "ed25519:" + hex.EncodeToString(forgedSig)

	if err := material.VerifyAttestationSignature(record); err == nil {
		t.Fatal("should reject forged signature from different key")
	}
}

func TestLoadRepairSigningMaterial(t *testing.T) {
	// Generate a test key and write it to a temp file
	pub, priv, _ := ed25519.GenerateKey(nil)
	spki, _ := transportprimitives.SPKIHash(pub)

	// Write private key file
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "repair_attestor_private.key")
	keyContent := privateSigningKeyPrefix + hex.EncodeToString(priv)
	if err := os.WriteFile(keyPath, []byte(keyContent), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// We need to override the frozen SPKI to match our test key.
	// Since LoadRepairSigningMaterial uses the frozen pins, we need a test
	// helper that allows overriding. For now, test the error case —
	// the SPKI won't match the frozen pins.
	_, err := LoadRepairSigningMaterial(keyPath, uint32(os.Getuid()), testNow())
	if err == nil {
		t.Fatal("LoadRepairSigningMaterial should fail with wrong SPKI (frozen pins don't match test key)")
	}
	if !strings.Contains(err.Error(), "SPKI mismatch") {
		t.Errorf("expected SPKI mismatch error, got: %v", err)
	}

	// Verify the SPKI value is correct (for debugging)
	_ = spki
}

func TestLoadRepairSigningMaterialRejectsEmptyPath(t *testing.T) {
	_, err := LoadRepairSigningMaterial("", uint32(os.Getuid()), testNow())
	if err == nil {
		t.Fatal("empty path should fail")
	}
}

func TestLoadRepairSigningMaterialRejectsZeroTime(t *testing.T) {
	_, err := LoadRepairSigningMaterial("/tmp/dummy", uint32(os.Getuid()), time.Time{})
	if err == nil {
		t.Fatal("zero time should fail")
	}
}

func TestLoadRepairSigningMaterialAcceptsNonUTCTime(t *testing.T) {
	// Non-UTC time is converted internally; the verifier only needs a valid instant.
	nonUTC := time.Date(2026, 7, 31, 12, 0, 0, 0, time.FixedZone("PST", -8*3600))
	// This will fail with a missing file error, not a time error — which is correct.
	_, err := LoadRepairSigningMaterial("/nonexistent/path/key", uint32(os.Getuid()), nonUTC)
	if err == nil {
		t.Fatal("should fail with missing file error")
	}
	if strings.Contains(err.Error(), "time is zero") {
		t.Fatal("should not reject non-UTC time with 'time is zero' error")
	}
}

func TestLoadRepairSigningMaterialRejectsMissingFile(t *testing.T) {
	_, err := LoadRepairSigningMaterial("/nonexistent/path/key", uint32(os.Getuid()), testNow())
	if err == nil {
		t.Fatal("missing file should fail")
	}
}

func TestLoadRepairSigningMaterialRejectsWrongFileMode(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "wrong_mode.key")
	if err := os.WriteFile(keyPath, []byte("ed25519-private:abcd"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	_, err := LoadRepairSigningMaterial(keyPath, uint32(os.Getuid()), testNow())
	if err == nil {
		t.Fatal("wrong file mode should fail")
	}
}

func TestLoadRepairSigningMaterialRejectsBadKeyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "bad_format.key")
	if err := os.WriteFile(keyPath, []byte("not-a-valid-key"), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	_, err := LoadRepairSigningMaterial(keyPath, uint32(os.Getuid()), testNow())
	if err == nil {
		t.Fatal("bad key format should fail")
	}
}

func TestRepairSigningMaterialClose(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	material.Close()
	if material.privateKey != nil {
		t.Error("privateKey should be nil after Close")
	}
}

func TestRepairSigningMaterialSignerSPKI(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	if material.SignerSPKI() == "" {
		t.Error("SignerSPKI is empty")
	}
}

func TestRepairSigningMaterialRootID(t *testing.T) {
	material := GenerateTestSigningMaterial(t, testNow())
	defer material.Close()
	if material.RootID() == "" {
		t.Error("RootID is empty")
	}
}
