package trustedsupervisor

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

const (
	privateSigningKeyBundleSchemaVersion = "ananke.local-trusted-supervisor-private-signing-key-bundle.v1"
	privateSigningKeyPrefix              = "ed25519-private:"
	maxTrustBundleBytes                  = 256 * 1024
	maxPrivateKeyBundleBytes             = 16 * 1024
)

type privateSigningKeyBundle struct {
	SchemaVersion   string                       `json:"schema_version"`
	TrustBundleHash string                       `json:"trust_bundle_hash"`
	Keys            []privateSigningKeyBundleKey `json:"keys"`
}

type privateSigningKeyBundleKey struct {
	Role       string
	RootID     string
	PublicKey  string
	SPKISHA256 string
}

type privateSigningMaterialHooks struct {
	afterPrivateFileRead func([]byte)
	afterPrivateField    func([]byte)
	afterPrivateKey      func(ed25519.PrivateKey)
}

type serverSigningMaterial struct {
	bundle     store.ExternalSupervisorTrustBundle
	privateKey ed25519.PrivateKey
	rootID     string
	signerSPKI string
	verifier   *ed25519Verifier
}

func loadServerSigningMaterial(trustBundlePath, privateBundlePath string, ownerUserID uint32, now time.Time) (*serverSigningMaterial, error) {
	return loadServerSigningMaterialWithHooks(trustBundlePath, privateBundlePath, ownerUserID, now, privateSigningMaterialHooks{})
}

func loadServerSigningMaterialWithHooks(trustBundlePath, privateBundlePath string, ownerUserID uint32, now time.Time, hooks privateSigningMaterialHooks) (*serverSigningMaterial, error) {
	if now.IsZero() || now.Location() != time.UTC {
		return nil, authenticationError("signing-material time")
	}
	publicBytes, err := readOwnerOnlyRegularFile(trustBundlePath, ownerUserID, maxTrustBundleBytes)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(publicBytes)
	bundle, err := DecodeTrustBundle(publicBytes)
	if err != nil {
		return nil, authenticationError("public trust bundle")
	}
	verifier, err := newEd25519Verifier(bundle)
	if err != nil {
		return nil, err
	}

	privateBytes, err := readOwnerOnlyRegularFile(privateBundlePath, ownerUserID, maxPrivateKeyBundleBytes)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(privateBytes)
	if hooks.afterPrivateFileRead != nil {
		hooks.afterPrivateFileRead(privateBytes)
	}
	keyBundle, privateKey, err := decodePrivateSigningKeyBundle(privateBytes, hooks)
	if err != nil {
		return nil, authenticationError("private signing bundle closed schema")
	}
	keepPrivateKey := false
	defer func() {
		if !keepPrivateKey {
			zeroBytes(privateKey)
		}
	}()
	if keyBundle.SchemaVersion != privateSigningKeyBundleSchemaVersion || keyBundle.TrustBundleHash != bundle.TrustBundleHash || len(keyBundle.Keys) != 1 {
		return nil, authenticationError("private signing bundle binding")
	}
	keyRecord := &keyBundle.Keys[0]
	if keyRecord.Role != "independent_supervisor_protocol_adapter" ||
		keyRecord.RootID != bundle.SupervisorPeer.Certificate.IssuerRootID ||
		keyRecord.PublicKey != bundle.SupervisorPeer.Certificate.SubjectPublicKey ||
		keyRecord.SPKISHA256 != bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256 {
		return nil, authenticationError("private signing identity")
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, authenticationError("private signing key type")
	}
	pinnedPublicKey, err := parsePublicKey(keyRecord.PublicKey, keyRecord.SPKISHA256)
	if err != nil || !bytes.Equal(publicKey, pinnedPublicKey) {
		return nil, authenticationError("private signing public half")
	}
	root, rootKey, err := verifier.rootAt(bundle.ReleaseRoots, now)
	if err != nil || root.RootID != keyRecord.RootID {
		return nil, authenticationError("private signing active root")
	}
	certifiedKey, err := verifyCertificateAt(bundle.SupervisorPeer, keyRecord.Role, root, rootKey, now)
	if err != nil || !bytes.Equal(certifiedKey, publicKey) {
		return nil, authenticationError("private signing certificate")
	}
	keepPrivateKey = true
	return &serverSigningMaterial{
		bundle: bundle, privateKey: privateKey, rootID: root.RootID,
		signerSPKI: keyRecord.SPKISHA256, verifier: verifier,
	}, nil
}

type privateSigningKeyBundleParser struct {
	data   []byte
	offset int
}

func decodePrivateSigningKeyBundle(data []byte, hooks privateSigningMaterialHooks) (privateSigningKeyBundle, ed25519.PrivateKey, error) {
	defer zeroBytes(data)
	parser := privateSigningKeyBundleParser{data: data}
	if !parser.consume(`{"keys":[{"private_key":`) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	privateKey, err := parser.parsePrivateKey(hooks)
	if err != nil {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	keepPrivateKey := false
	defer func() {
		if !keepPrivateKey {
			zeroBytes(privateKey)
		}
	}()
	if !parser.consume(`,"public_key":`) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	publicKey, err := parser.parseCanonicalPublicString()
	if err != nil || !parser.consume(`,"role":`) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	role, err := parser.parseCanonicalPublicString()
	if err != nil || !parser.consume(`,"root_id":`) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	rootID, err := parser.parseCanonicalPublicString()
	if err != nil || !parser.consume(`,"spki_sha256":`) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	spki, err := parser.parseCanonicalPublicString()
	if err != nil || !parser.consume(`}],"schema_version":`) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	schemaVersion, err := parser.parseCanonicalPublicString()
	if err != nil || !parser.consume(`,"trust_bundle_hash":`) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	trustBundleHash, err := parser.parseCanonicalPublicString()
	if err != nil || !parser.consume(`}`) || parser.offset != len(parser.data) {
		return privateSigningKeyBundle{}, nil, ErrProtocol
	}
	keepPrivateKey = true
	return privateSigningKeyBundle{
		SchemaVersion:   schemaVersion,
		TrustBundleHash: trustBundleHash,
		Keys: []privateSigningKeyBundleKey{{
			Role: role, RootID: rootID, PublicKey: publicKey, SPKISHA256: spki,
		}},
	}, privateKey, nil
}

func (parser *privateSigningKeyBundleParser) consume(literal string) bool {
	if parser == nil || !bytes.HasPrefix(parser.data[parser.offset:], []byte(literal)) {
		return false
	}
	parser.offset += len(literal)
	return true
}

func (parser *privateSigningKeyBundleParser) parsePrivateKey(hooks privateSigningMaterialHooks) (ed25519.PrivateKey, error) {
	const encodedKeyBytes = len(privateSigningKeyPrefix) + ed25519.PrivateKeySize*2
	if parser == nil || parser.offset >= len(parser.data) || parser.data[parser.offset] != '"' {
		return nil, ErrProtocol
	}
	start := parser.offset + 1
	end := start + encodedKeyBytes
	if end >= len(parser.data) || parser.data[end] != '"' {
		return nil, ErrProtocol
	}
	encoded := parser.data[start:end]
	if hooks.afterPrivateField != nil {
		hooks.afterPrivateField(encoded)
	}
	if !bytes.HasPrefix(encoded, []byte(privateSigningKeyPrefix)) {
		return nil, ErrProtocol
	}
	hexBytes := encoded[len(privateSigningKeyPrefix):]
	for _, value := range hexBytes {
		if value < '0' || value > '9' && (value < 'a' || value > 'f') {
			return nil, ErrProtocol
		}
	}
	privateKey := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	if _, err := hex.Decode(privateKey, hexBytes); err != nil {
		zeroBytes(privateKey)
		return nil, ErrProtocol
	}
	if hooks.afterPrivateKey != nil {
		hooks.afterPrivateKey(privateKey)
	}
	parser.offset = end + 1
	return privateKey, nil
}

func (parser *privateSigningKeyBundleParser) parseCanonicalPublicString() (string, error) {
	if parser == nil || parser.offset >= len(parser.data) || parser.data[parser.offset] != '"' {
		return "", ErrProtocol
	}
	start := parser.offset
	parser.offset++
	for parser.offset < len(parser.data) {
		switch parser.data[parser.offset] {
		case '"':
			parser.offset++
			raw := parser.data[start:parser.offset]
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return "", ErrProtocol
			}
			canonical, err := transportprimitives.MarshalCanonical(value)
			if err != nil || !bytes.Equal(raw, canonical) {
				zeroBytes(canonical)
				return "", ErrProtocol
			}
			zeroBytes(canonical)
			return value, nil
		case '\\':
			parser.offset += 2
		default:
			if parser.data[parser.offset] < 0x20 {
				return "", ErrProtocol
			}
			parser.offset++
		}
	}
	return "", ErrProtocol
}

func (material *serverSigningMaterial) Close() {
	if material == nil {
		return
	}
	zeroBytes(material.privateKey)
	material.privateKey = nil
}

func readOwnerOnlyRegularFile(path string, ownerUserID uint32, limit int64) ([]byte, error) {
	contents, err := transportprimitives.ReadOwnerOnlyRegularFile(path, ownerUserID, limit)
	if err != nil {
		if errors.Is(err, transportprimitives.ErrFileLimit) {
			return nil, fmt.Errorf("%w: %v", ErrLimit, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrAuthentication, err)
	}
	return contents, nil
}

func zeroBytes(value []byte) {
	transportprimitives.ZeroBytes(value)
}
