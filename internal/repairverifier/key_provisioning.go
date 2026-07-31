package repairverifier

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

// ErrKeyProvisioning is the sentinel for key provisioning failures.
var ErrKeyProvisioning = errors.New("repair key provisioning failed")

const (
	// privateSigningKeyPrefix is the expected prefix for the Ed25519
	// private key in the key bundle file.
	privateSigningKeyPrefix = "ed25519-private:"

	// maxPrivateKeyBundleBytes is the maximum size of the private key
	// bundle file.
	maxPrivateKeyBundleBytes = 16 * 1024
)

// RepairSigningMaterial holds the provisioned repair attestor signing
// material. The private key is held in memory and must be zeroized when
// no longer needed via Close().
type RepairSigningMaterial struct {
	verifier   *RepairVerifier
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	rootID     string
	signerSPKI string
}

// LoadRepairSigningMaterial loads the repair attestor's private key from
// an owner-only file and verifies it matches the public key pinned in the
// frozen trust bundle. The file must be a regular file owned by ownerUserID
// with mode 0600 (enforced by transportprimitives.ReadOwnerOnlyRegularFile).
//
// The file format is a single line containing the Ed25519 private key in
// "ed25519-private:" + hex encoding. This mirrors the P5 trusted supervisor
// pattern but uses a simpler format since the repair attestor has a single
// dedicated key role with no certificate chain to verify (the certificate
// chain is verified by the contract layer's VerifyReleaseTrust).
func LoadRepairSigningMaterial(privateKeyPath string, ownerUserID uint32, now time.Time) (*RepairSigningMaterial, error) {
	now = now.UTC()
	if now.IsZero() {
		return nil, fmt.Errorf("%w: time is zero", ErrKeyProvisioning)
	}
	if privateKeyPath == "" {
		return nil, fmt.Errorf("%w: empty private key path", ErrKeyProvisioning)
	}

	// Create the verifier first — this verifies the release trust boundary
	// and gives us access to the pinned SPKI hash.
	verifier, err := NewRepairVerifier(now)
	if err != nil {
		return nil, fmt.Errorf("%w: verifier: %w", ErrKeyProvisioning, err)
	}

	// Read the private key file with strict security checks.
	privateKeyBytes, err := transportprimitives.ReadOwnerOnlyRegularFile(privateKeyPath, ownerUserID, maxPrivateKeyBundleBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: read private key: %w", ErrKeyProvisioning, err)
	}
	defer transportprimitives.ZeroBytes(privateKeyBytes)

	// Parse the private key.
	privateKey, err := parsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse private key: %v", ErrKeyProvisioning, err)
	}

	keepKey := false
	defer func() {
		if !keepKey {
			transportprimitives.ZeroBytes(privateKey)
		}
	}()

	// Derive the public key from the private key.
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: invalid public key type", ErrKeyProvisioning)
	}

	// Verify the public key's SPKI matches the pinned value.
	spki, err := transportprimitives.SPKIHash(publicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: SPKI hash: %w", ErrKeyProvisioning, err)
	}
	if spki != verifier.ExpectedAttestorSPKI() {
		return nil, fmt.Errorf("%w: key SPKI mismatch (expected %s, got %s)", ErrKeyProvisioning, verifier.ExpectedAttestorSPKI(), spki)
	}

	// Seed/pub consistency self-check: verify the private key's seed half
	// regenerates the same public key. This catches a corrupted key file
	// where only the seed or only the public half is damaged — the SPKI
	// check would pass but signing would produce unverifiable signatures.
	seed := ed25519.NewKeyFromSeed(privateKey[:ed25519.SeedSize])
	seedPub := seed.Public()
	if !bytes.Equal(seedPub.(ed25519.PublicKey), publicKey) {
		return nil, fmt.Errorf("%w: seed/pub consistency check failed", ErrKeyProvisioning)
	}

	// Provision the verifier with the public key for signature verification.
	if err := verifier.SetAttestorPublicKey(publicKey); err != nil {
		return nil, fmt.Errorf("%w: provision verifier: %v", ErrKeyProvisioning, err)
	}

	keepKey = true
	return &RepairSigningMaterial{
		verifier:   verifier,
		privateKey: privateKey,
		publicKey:  publicKey,
		rootID:     verifier.pins.RepairAttestorRootID,
		signerSPKI: spki,
	}, nil
}

// Verifier returns the repair verifier associated with this signing material.
func (m *RepairSigningMaterial) Verifier() *RepairVerifier {
	return m.verifier
}

// SignerSPKI returns the SPKI hash of the signer's public key.
func (m *RepairSigningMaterial) SignerSPKI() string {
	return m.signerSPKI
}

// PublicKey returns the provisioned public key for verification.
func (m *RepairSigningMaterial) PublicKey() ed25519.PublicKey {
	if m == nil || len(m.publicKey) == 0 {
		return nil
	}
	key := make(ed25519.PublicKey, ed25519.PublicKeySize)
	copy(key, m.publicKey)
	return key
}

// RootID returns the trust root ID for the repair attestor.
func (m *RepairSigningMaterial) RootID() string {
	return m.rootID
}

// SignAttestationBytes signs raw canonical bytes using the provisioned
// private key. The signature is returned as "ed25519:" + hex-encoded bytes.
// This is used by the repair runner which computes the signed bytes itself
// (excluding both signature and attestation_hash fields to break the
// circular dependency).
func (m *RepairSigningMaterial) SignAttestationBytes(signedBytes []byte) (string, error) {
	if m == nil || len(m.privateKey) == 0 {
		return "", fmt.Errorf("%w: signing material not provisioned", ErrKeyProvisioning)
	}
	signature := ed25519.Sign(m.privateKey, signedBytes)
	return signaturePrefix + hex.EncodeToString(signature), nil
}

// VerifyAttestationBytes verifies a raw Ed25519 signature over the given
// canonical bytes using the provisioned public key.
func (m *RepairSigningMaterial) VerifyAttestationBytes(signedBytes []byte, signature string) error {
	if m == nil || len(m.privateKey) == 0 {
		return fmt.Errorf("%w: signing material not provisioned", ErrKeyProvisioning)
	}
	if !strings.HasPrefix(signature, signaturePrefix) {
		return fmt.Errorf("%w: signature missing ed25519 prefix", ErrKeyProvisioning)
	}
	sigHex := strings.TrimPrefix(signature, signaturePrefix)
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("%w: invalid signature encoding: %v", ErrKeyProvisioning, err)
	}
	if !ed25519.Verify(m.publicKey, signedBytes, sigBytes) {
		return fmt.Errorf("%w: Ed25519 signature verification failed", ErrKeyProvisioning)
	}
	return nil
}

// SignAttestation signs a repair-review attestation using the provisioned
// private key. The signature is returned as "ed25519:" + hex-encoded bytes.
// The attestation's Signature field should be empty or a placeholder when
// calling this method; the returned signature should be set on the
// attestation before computing the attestation hash.
func (m *RepairSigningMaterial) SignAttestation(record repaircontract.RepairReviewAttestation) (string, error) {
	if m == nil || len(m.privateKey) == 0 {
		return "", fmt.Errorf("%w: signing material not provisioned", ErrKeyProvisioning)
	}
	signedBytes, err := repaircontract.RuntimeSignatureCanonicalBytes(record)
	if err != nil {
		return "", fmt.Errorf("%w: canonical bytes: %v", ErrKeyProvisioning, err)
	}
	signature := ed25519.Sign(m.privateKey, signedBytes)
	return signaturePrefix + hex.EncodeToString(signature), nil
}

// VerifyAttestationSignature verifies an attestation signature using the
// provisioned public key. This is used by Ananke (which holds only the
// public key) to verify attestations signed by the repair supervisor.
func (m *RepairSigningMaterial) VerifyAttestationSignature(record repaircontract.RepairReviewAttestation) error {
	return m.verifier.VerifyAttestationSignature(record, m.signerSPKI)
}

// Close zeroizes the private key. Must be called when the signing material
// is no longer needed.
func (m *RepairSigningMaterial) Close() {
	if m == nil {
		return
	}
	transportprimitives.ZeroBytes(m.privateKey)
	m.privateKey = nil
}

// parsePrivateKey parses an Ed25519 private key from the file contents.
// The expected format is "ed25519-private:" + hex-encoded 64-byte private key.
//
// This function operates entirely on byte slices (not Go strings) to ensure
// all intermediate private-key material can be explicitly zeroized. This
// mirrors the P5 trustedsupervisor pattern (server_keys.go:128-216) which
// hex-decodes directly from the byte buffer into the key slice.
func parsePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	// Trim whitespace at byte level (avoid string conversion)
	trimmed := bytes.TrimSpace(data)
	prefixBytes := []byte(privateSigningKeyPrefix)
	if !bytes.HasPrefix(trimmed, prefixBytes) {
		transportprimitives.ZeroBytes(trimmed)
		return nil, fmt.Errorf("missing prefix %q", privateSigningKeyPrefix)
	}
	hexPart := trimmed[len(prefixBytes):]
	// Pre-allocate the key slice and decode directly into it
	key := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	n, err := hex.Decode(key, hexPart)
	if err != nil {
		transportprimitives.ZeroBytes(key)
		transportprimitives.ZeroBytes(trimmed)
		return nil, fmt.Errorf("hex decode: %v", err)
	}
	if n != ed25519.PrivateKeySize || len(hexPart) != ed25519.PrivateKeySize*2 {
		transportprimitives.ZeroBytes(key)
		transportprimitives.ZeroBytes(trimmed)
		return nil, fmt.Errorf("expected %d hex chars, got %d", ed25519.PrivateKeySize*2, len(hexPart))
	}
	// Zeroize the trimmed buffer (which still contains the hex-encoded key)
	transportprimitives.ZeroBytes(trimmed)
	return key, nil
}

// GenerateSigningMaterial generates an Ed25519 key pair for repair signing.
// Unlike GenerateTestSigningMaterial, this does not require a testing.T
// and is suitable for CLI use. The verifier's pinned SPKI is overridden
// to accept the generated key.
func GenerateSigningMaterial(now time.Time) (*RepairSigningMaterial, error) {
	now = now.UTC()
	if now.IsZero() {
		return nil, fmt.Errorf("%w: time is zero", ErrKeyProvisioning)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("%w: generate key: %v", ErrKeyProvisioning, err)
	}
	keepKey := false
	defer func() {
		if !keepKey {
			transportprimitives.ZeroBytes(priv)
		}
	}()
	spki, err := transportprimitives.SPKIHash(pub)
	if err != nil {
		return nil, fmt.Errorf("%w: SPKI hash: %v", ErrKeyProvisioning, err)
	}
	verifier := &RepairVerifier{
		pins:     repaircontract.FrozenReleasePins(),
		bundle:   repaircontract.FrozenTrustBundle(),
		rotation: repaircontract.FrozenTrustRotation(),
	}
	verifier.pins.RepairAttestorLeafSPKI = spki
	if err := verifier.SetAttestorPublicKey(pub); err != nil {
		return nil, fmt.Errorf("%w: set attestor: %v", ErrKeyProvisioning, err)
	}
	keepKey = true
	return &RepairSigningMaterial{
		verifier:   verifier,
		privateKey: priv,
		publicKey:  pub,
		rootID:     verifier.pins.RepairAttestorRootID,
		signerSPKI: spki,
	}, nil
}

// GenerateTestSigningMaterial creates a RepairSigningMaterial for testing
// using a generated Ed25519 key pair. The key's SPKI is set as the
// verifier's expected SPKI, overriding the frozen release pin. This is
// ONLY for testing and must never be used in production.
func GenerateTestSigningMaterial(t interface{ Helper() }, now time.Time) *RepairSigningMaterial {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	spki, _ := transportprimitives.SPKIHash(pub)
	verifier := &RepairVerifier{
		pins:     repaircontract.FrozenReleasePins(),
		bundle:   repaircontract.FrozenTrustBundle(),
		rotation: repaircontract.FrozenTrustRotation(),
	}
	// Override the pinned SPKI for testing
	verifier.pins.RepairAttestorLeafSPKI = spki
	verifier.SetAttestorPublicKey(pub)
	return &RepairSigningMaterial{
		verifier:   verifier,
		privateKey: priv,
		publicKey:  pub,
		rootID:     verifier.pins.RepairAttestorRootID,
		signerSPKI: spki,
	}
}
