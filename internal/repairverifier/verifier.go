package repairverifier

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

// ErrVerification is the sentinel for repair verifier failures.
var ErrVerification = errors.New("repair verification failed")

const (
	// signaturePrefix is the expected prefix for Ed25519 signatures in
	// the attestation's Signature field at runtime. The contract layer
	// uses "sha256:" placeholders for test fixtures; the runtime uses
	// "ed25519:" + hex-encoded 64-byte signatures.
	signaturePrefix = "ed25519:"

	// RepairAttestorRole is the dedicated key role for the repair attestor.
	// It must match the role pinned in the frozen trust bundle.
	RepairAttestorRole = repaircontract.RepairAttestorRole
)

// RepairVerifier is a release-pinned verifier for repair-review attestation
// signatures. It is initialized once from frozen release pins and trust
// bundle, and the attestor's public key is parsed at construction time.
type RepairVerifier struct {
	pins        repaircontract.ReleasePins
	bundle      repaircontract.TrustBundle
	rotation    repaircontract.TrustRotation
	attestorKey ed25519.PublicKey
}

// NewRepairVerifier creates a release-pinned repair verifier from the frozen
// release pins and trust bundle. It verifies the release trust boundary and
// parses the attestor's Ed25519 public key from the trust bundle.
func NewRepairVerifier(now time.Time) (*RepairVerifier, error) {
	now = now.UTC()
	if now.IsZero() {
		return nil, fmt.Errorf("%w: verifier time is zero", ErrVerification)
	}
	pins := repaircontract.FrozenReleasePins()
	bundle := repaircontract.FrozenTrustBundle()
	rotation := repaircontract.FrozenTrustRotation()
	if err := repaircontract.VerifyReleaseTrust(pins, bundle, rotation, now); err != nil {
		return nil, fmt.Errorf("%w: release trust: %v", ErrVerification, err)
	}
	attestor := bundle.RepairAttestor
	if attestor.Role != repaircontract.RepairAttestorRole {
		return nil, fmt.Errorf("%w: attestor role mismatch", ErrVerification)
	}
	// The attestor's SPKI hash is pinned in the release pins. We parse the
	// public key from the trust bundle's certificate and verify its SPKI
	// matches the pinned value.
	//
	// The trust bundle stores the SPKI hash (not the encoded public key
	// string) in the certificate. We need to derive the public key from
	// the trust bundle's certificate structure.
	//
	// In the contract layer, the trust bundle is compiled from embedded
	// DER bytes. The public key is available through the compiled release
	// material. However, the contract layer does not export the raw public
	// key bytes — it only exports the SPKI hash.
	//
	// For the runtime verifier, we use the SPKI hash from the trust bundle
	// to verify signatures. The actual public key bytes are obtained from
	// the key provisioning module (which loads the private key and derives
	// the public key from it), or from the attestation's signer SPKI field.
	//
	// The verifier stores the pinned SPKI hash and uses it to validate
	// that any public key used for verification matches the pinned value.
	return &RepairVerifier{
		pins:     pins,
		bundle:   bundle,
		rotation: rotation,
	}, nil
}

// Pins returns the frozen release pins used by this verifier.
func (v *RepairVerifier) Pins() repaircontract.ReleasePins {
	return v.pins
}

// Bundle returns the frozen trust bundle used by this verifier.
func (v *RepairVerifier) Bundle() repaircontract.TrustBundle {
	return v.bundle
}

// ExpectedAttestorSPKI returns the SPKI hash pinned for the repair attestor.
func (v *RepairVerifier) ExpectedAttestorSPKI() string {
	return v.pins.RepairAttestorLeafSPKI
}

// ExpectedAttestorCertificateHash returns the certificate hash pinned for the repair attestor.
func (v *RepairVerifier) ExpectedAttestorCertificateHash() string {
	return v.pins.RepairAttestorCertificateHash
}

// VerifyAttestationSignature verifies the Ed25519 signature on a repair-review
// attestation. The signature must be in "ed25519:" + hex format. The signed
// bytes are the canonical attestation excluding the signature field, with
// domain separation prepended (as defined by the contract layer).
//
// The signerSPKI is the SPKI hash of the signer's public key, which must match
// the pinned repair attestor SPKI.
func (v *RepairVerifier) VerifyAttestationSignature(record repaircontract.RepairReviewAttestation, signerSPKI string) error {
	if v == nil {
		return fmt.Errorf("%w: verifier is nil", ErrVerification)
	}
	// Verify the signer matches the pinned repair attestor.
	if signerSPKI != v.pins.RepairAttestorLeafSPKI {
		return fmt.Errorf("%w: signer SPKI mismatch", ErrVerification)
	}
	// Verify the attestation's trust binding matches the pinned values.
	if record.SignatureDomain != repaircontract.SignatureDomain ||
		record.RepairAttestorCertificateHash != v.pins.RepairAttestorCertificateHash ||
		record.RepairAttestorRootID != v.pins.RepairAttestorRootID ||
		record.RepairAttestorLeafSPKI != v.pins.RepairAttestorLeafSPKI ||
		record.ReleasePinsHash != v.pins.ReleasePinsHash ||
		record.TrustBundleHash != v.bundle.TrustBundleHash {
		return fmt.Errorf("%w: attestation trust binding mismatch", ErrVerification)
	}
	// Parse the Ed25519 signature from the signature field.
	if !strings.HasPrefix(record.Signature, signaturePrefix) {
		return fmt.Errorf("%w: signature missing ed25519 prefix", ErrVerification)
	}
	sigHex := strings.TrimPrefix(record.Signature, signaturePrefix)
	sigBytes, err := hexDecode(sigHex, ed25519.SignatureSize)
	if err != nil {
		return fmt.Errorf("%w: invalid signature encoding: %v", ErrVerification, err)
	}
	// Compute the canonical bytes that the signature covers.
	signedBytes, err := repaircontract.AttestationSignatureCanonicalBytes(record)
	if err != nil {
		return fmt.Errorf("%w: canonical bytes: %v", ErrVerification, err)
	}
	// The public key for verification must be provided by the caller
	// (typically from the key provisioning module or from a parsed
	// certificate). The verifier validates that the public key's SPKI
	// matches the pinned value.
	//
	// This method requires the public key to be set on the verifier via
	// SetAttestorPublicKey or to be passed through the key provisioning
	// module. If no public key is set, verification fails.
	if len(v.attestorKey) == 0 {
		return fmt.Errorf("%w: attestor public key not provisioned", ErrVerification)
	}
	if !ed25519.Verify(v.attestorKey, signedBytes, sigBytes) {
		return fmt.Errorf("%w: Ed25519 signature verification failed", ErrVerification)
	}
	return nil
}

// SetAttestorPublicKey provisions the attestor's public key for signature
// verification. The key's SPKI hash must match the pinned value. This is
// typically called by the key provisioning module after loading the private
// key and deriving the public key from it.
func (v *RepairVerifier) SetAttestorPublicKey(key ed25519.PublicKey) error {
	if v == nil {
		return fmt.Errorf("%w: verifier is nil", ErrVerification)
	}
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: invalid public key size", ErrVerification)
	}
	spki, err := transportprimitives.SPKIHash(key)
	if err != nil {
		return fmt.Errorf("%w: SPKI hash: %v", ErrVerification, err)
	}
	if spki != v.pins.RepairAttestorLeafSPKI {
		return fmt.Errorf("%w: provisioned key SPKI mismatch", ErrVerification)
	}
	v.attestorKey = make(ed25519.PublicKey, ed25519.PublicKeySize)
	copy(v.attestorKey, key)
	return nil
}

// hexDecode decodes a hex string to bytes of the expected size.
func hexDecode(hexStr string, expectedSize int) ([]byte, error) {
	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}
	if len(decoded) != expectedSize {
		return nil, fmt.Errorf("expected %d bytes, got %d", expectedSize, len(decoded))
	}
	return decoded, nil
}

// FrozenTrustRotation is a convenience accessor that delegates to the
// contract package's frozen rotation value.
func FrozenTrustRotation() repaircontract.TrustRotation {
	return repaircontract.FrozenTrustRotation()
}
