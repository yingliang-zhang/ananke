package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
)

// auditJournalAuthority is the single server peer key and pinned execution
// policy accepted by this journal. Journal key rotation is not supported: a
// restart with a different peer key cannot authenticate existing audit rows.
type auditJournalAuthority struct {
	policy       *executionPolicy
	privateKey   ed25519.PrivateKey
	publicKey    ed25519.PublicKey
	signerSPKI   string
	signerRootID string
}

func newAuditJournalAuthority(policy *executionPolicy, material *serverSigningMaterial) (*auditJournalAuthority, error) {
	if policy == nil || material == nil || len(material.privateKey) != ed25519.PrivateKeySize ||
		!protocolHashPattern.MatchString(material.signerSPKI) || !protocolIdentifierPattern.MatchString(material.rootID) {
		return nil, authenticationError("audit journal authority")
	}
	if err := policy.validateIdentity(); err != nil {
		return nil, err
	}
	certificate := material.bundle.SupervisorPeer.Certificate
	if certificate.SubjectKeySPKISHA256 != material.signerSPKI || certificate.IssuerRootID != material.rootID {
		return nil, authenticationError("audit journal signer certificate binding")
	}
	publicKey, err := parsePublicKey(certificate.SubjectPublicKey, material.signerSPKI)
	if err != nil {
		return nil, authenticationError("audit journal pinned public key")
	}
	privatePublic, ok := material.privateKey.Public().(ed25519.PublicKey)
	if !ok || !bytes.Equal(privatePublic, publicKey) {
		return nil, authenticationError("audit journal signing key possession")
	}
	return &auditJournalAuthority{
		policy: policy, privateKey: material.privateKey, publicKey: publicKey,
		signerSPKI: material.signerSPKI, signerRootID: material.rootID,
	}, nil
}

func (authority *auditJournalAuthority) validateCurrent() error {
	if authority == nil || authority.policy == nil || len(authority.privateKey) != ed25519.PrivateKeySize ||
		len(authority.publicKey) != ed25519.PublicKeySize || !protocolHashPattern.MatchString(authority.signerSPKI) ||
		!protocolIdentifierPattern.MatchString(authority.signerRootID) {
		return authenticationError("audit journal authority absent")
	}
	if err := authority.policy.validateIdentity(); err != nil {
		return err
	}
	privatePublic, ok := authority.privateKey.Public().(ed25519.PublicKey)
	if !ok || !bytes.Equal(privatePublic, authority.publicKey) {
		return authenticationError("audit journal current signing key")
	}
	return nil
}

func (authority *auditJournalAuthority) resolveIntent(intent auditExecutionIntent) (executionPolicyEntry, error) {
	if err := authority.validateCurrent(); err != nil {
		return executionPolicyEntry{}, err
	}
	entry, exists := authority.policy.entries[intent.LaunchSpecHash]
	if !exists || entry.PolicyHash != intent.PolicyHash || entry.TaskID != intent.TaskID ||
		entry.RouteMappingHash != intent.RouteMappingHash || entry.RepositoryIdentityHash != intent.RepositoryIdentityHash ||
		entry.GitCommit != intent.GitCommit || entry.GitTree != intent.GitTree ||
		entry.SourceArchiveSHA256 != intent.SourceArchiveSHA256 || entry.Wrapper.SHA256 != intent.WrapperSHA256 ||
		entry.AttemptCap != intent.AttemptCap {
		return executionPolicyEntry{}, authenticationError("audit intent execution policy authority")
	}
	return cloneExecutionPolicyEntry(entry), nil
}

func canonicalAuditExecutionEventAuthenticationPayload(authentication auditExecutionEventAuthentication) ([]byte, error) {
	return marshalCanonical(map[string]any{
		"schema_version":         authentication.SchemaVersion,
		"domain":                 "ananke.local-trusted-supervisor.audit-execution-event",
		"event_hash":             authentication.EventHash,
		"intent_hash":            authentication.IntentHash,
		"sequence":               authentication.Sequence,
		"signer_key_spki_sha256": authentication.SignerKeySPKISHA256,
		"signer_root_id":         authentication.SignerRootID,
	})
}

func (authority *auditJournalAuthority) authenticateEvent(event auditExecutionEvent) (auditExecutionEvent, error) {
	if err := authority.validateCurrent(); err != nil {
		return auditExecutionEvent{}, err
	}
	sealed, err := sealAuditExecutionEvent(event)
	if err != nil || sealed != event {
		return auditExecutionEvent{}, authenticationError("audit event canonical hash before authentication")
	}
	if event.Authentication != (auditExecutionEventAuthentication{}) {
		if err := authority.verifyEvent(event); err != nil {
			return auditExecutionEvent{}, err
		}
		return event, nil
	}
	authentication := auditExecutionEventAuthentication{
		SchemaVersion: auditExecutionEventAuthenticationSchemaVersion,
		Algorithm:     "ed25519", EventHash: event.EventHash, IntentHash: event.IntentHash, Sequence: event.Sequence,
		SignerKeySPKISHA256: authority.signerSPKI, SignerRootID: authority.signerRootID,
	}
	payload, err := canonicalAuditExecutionEventAuthenticationPayload(authentication)
	if err != nil {
		return auditExecutionEvent{}, err
	}
	signature := ed25519.Sign(authority.privateKey, payload)
	authentication.Signature = "ed25519:" + hex.EncodeToString(signature)
	zeroBytes(signature)
	event.Authentication = authentication
	return event, nil
}

func (authority *auditJournalAuthority) verifyEvent(event auditExecutionEvent) error {
	if err := authority.validateCurrent(); err != nil {
		return err
	}
	authentication := event.Authentication
	if authentication.SchemaVersion != auditExecutionEventAuthenticationSchemaVersion || authentication.Algorithm != "ed25519" ||
		authentication.EventHash != event.EventHash || authentication.IntentHash != event.IntentHash ||
		authentication.Sequence != event.Sequence || authentication.SignerKeySPKISHA256 != authority.signerSPKI ||
		authentication.SignerRootID != authority.signerRootID {
		return authenticationError("audit event authentication binding")
	}
	signature, err := decodePrefixedHex(authentication.Signature, "ed25519:", ed25519.SignatureSize)
	if err != nil {
		return authenticationError("audit event authentication signature")
	}
	defer zeroBytes(signature)
	payload, err := canonicalAuditExecutionEventAuthenticationPayload(authentication)
	if err != nil || !ed25519.Verify(authority.publicKey, payload, signature) {
		return authenticationError("audit event signing authority")
	}
	return nil
}

func (journal *serverJournal) bindAuditAuthority(policy *executionPolicy, material *serverSigningMaterial) error {
	if journal == nil {
		return ErrProtocol
	}
	authority, err := newAuditJournalAuthority(policy, material)
	if err != nil {
		return err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return ErrProtocol
	}
	if journal.auditAuthority != nil {
		if journal.auditAuthority.policy != policy || journal.auditAuthority.signerSPKI != authority.signerSPKI ||
			journal.auditAuthority.signerRootID != authority.signerRootID ||
			!bytes.Equal(journal.auditAuthority.publicKey, authority.publicKey) {
			return authenticationError("audit journal authority replacement")
		}
		return journal.auditAuthority.validateCurrent()
	}
	journal.auditAuthority = authority
	if err := validateServerJournalContent(context.Background(), journal.db, journal.auditAuthority); err != nil {
		journal.auditAuthority = nil
		return err
	}
	return nil
}

func (journal *serverJournal) requireAuditAuthority(intent auditExecutionIntent) (executionPolicyEntry, error) {
	if journal == nil || journal.auditAuthority == nil {
		return executionPolicyEntry{}, authenticationError("audit journal authority absent")
	}
	return journal.auditAuthority.resolveIntent(intent)
}
