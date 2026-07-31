package transportprimitives

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrCrypto is the sentinel for cryptographic verification failures.
var ErrCrypto = errors.New("cryptographic error")

// SPKIHash returns "sha256:" + hex-encoded SHA-256 of the PKIX-encoded
// Ed25519 public key.
func SPKIHash(publicKey ed25519.PublicKey) (string, error) {
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// ParsePublicKey decodes an "ed25519:"-prefixed hex Ed25519 public key
// and verifies that its SPKI hash matches expectedSPKI.
func ParsePublicKey(encoded, expectedSPKI string) (ed25519.PublicKey, error) {
	keyBytes, err := DecodePrefixedHex(encoded, "ed25519:", ed25519.PublicKeySize)
	if err != nil {
		return nil, err
	}
	key := ed25519.PublicKey(keyBytes)
	actualSPKI, err := SPKIHash(key)
	if err != nil || actualSPKI != expectedSPKI {
		return nil, fmt.Errorf("%w: public key SPKI mismatch", ErrCrypto)
	}
	return key, nil
}

// DecodePrefixedHex decodes a hex value with a required algorithm prefix
// and exact byte size.
func DecodePrefixedHex(value, prefix string, size int) ([]byte, error) {
	if !strings.HasPrefix(value, prefix) {
		return nil, fmt.Errorf("%w: missing algorithm prefix", ErrCrypto)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(decoded) != size {
		return nil, fmt.Errorf("%w: invalid encoded value", ErrCrypto)
	}
	return decoded, nil
}

// RecordValidAt returns true when at is within [issuedAt, notAfter).
func RecordValidAt(issuedAtText, notAfterText string, at time.Time) bool {
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, issuedAtText)
	notAfter, expiryErr := time.Parse(time.RFC3339Nano, notAfterText)
	return issuedErr == nil && expiryErr == nil && !at.Before(issuedAt) && at.Before(notAfter)
}
