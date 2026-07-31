package trustedsupervisor

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

type ed25519Verifier struct {
	bundle              store.ExternalSupervisorTrustBundle
	expectedPredecessor store.ExternalSupervisorPredecessorReleaseIdentity
}

func newEd25519Verifier(bundle store.ExternalSupervisorTrustBundle, expected ...store.ExternalSupervisorPredecessorReleaseIdentity) (*ed25519Verifier, error) {
	if len(expected) > 1 {
		return nil, authenticationError("ambiguous predecessor release identity")
	}
	if store.ValidateExternalSupervisorTrustBundle(bundle) != nil {
		return nil, authenticationError("invalid trust bundle")
	}
	verifier := &ed25519Verifier{bundle: bundle}
	if len(expected) == 1 {
		if !validPredecessorReleaseIdentity(expected[0]) {
			return nil, authenticationError("invalid predecessor release identity")
		}
		verifier.expectedPredecessor = expected[0]
	}
	for name, lifecycle := range map[string]store.ExternalSupervisorTrustRootLifecycle{
		"release":  bundle.ReleaseRoots,
		"approval": bundle.ApprovalRoots,
		"moa":      bundle.MoARoots,
	} {
		if err := verifier.validateRootLifecycle(lifecycle); err != nil {
			return nil, authenticationError(name + " root lifecycle: " + err.Error())
		}
	}
	for role, certificate := range map[string]store.ExternalSupervisorSignedCertificate{
		"release_attestor": bundle.ReleaseAttestor,
		"release_approver": bundle.ReleaseApprover,
		"moa_grantor":      bundle.MoAGrantor,
		"independent_supervisor_protocol_adapter": bundle.SupervisorPeer,
	} {
		if certificate.Certificate.Role != role {
			return nil, authenticationError("certificate role mismatch")
		}
		if _, err := parsePublicKey(certificate.Certificate.SubjectPublicKey, certificate.Certificate.SubjectKeySPKISHA256); err != nil {
			return nil, authenticationError("certificate public key")
		}
		sealed, err := store.SealExternalSupervisorSigningCertificate(certificate.Certificate)
		if err != nil || sealed != certificate.Certificate {
			return nil, authenticationError("certificate self hash")
		}
	}
	return verifier, nil
}

func (verifier *ed25519Verifier) validateRootLifecycle(lifecycle store.ExternalSupervisorTrustRootLifecycle) error {
	activeKey, err := validateTrustRootKey(lifecycle.Active)
	if err != nil {
		return err
	}
	successorKey, err := validateTrustRootKey(lifecycle.Successor)
	if err != nil {
		return err
	}
	activeNotAfter, _ := time.Parse(time.RFC3339Nano, lifecycle.Active.NotAfter)
	successorValidFrom, _ := time.Parse(time.RFC3339Nano, lifecycle.Successor.ValidFrom)
	if !successorValidFrom.Before(activeNotAfter) || lifecycle.Rotation.OldRootID != lifecycle.Active.RootID ||
		lifecycle.Rotation.NewRootID != lifecycle.Successor.RootID || lifecycle.Rotation.NewRootSPKISHA256 != lifecycle.Successor.SPKISHA256 ||
		lifecycle.Rotation.NewRootValidFrom != lifecycle.Successor.ValidFrom || lifecycle.Rotation.OldRootNotAfter != lifecycle.Active.NotAfter {
		return fmt.Errorf("invalid successor overlap or binding")
	}
	sealedRotation, err := store.SealExternalSupervisorRootRotation(lifecycle.Rotation)
	if err != nil || sealedRotation != lifecycle.Rotation {
		return fmt.Errorf("invalid rotation self hash")
	}
	if err := verifyDetachedSignature(activeKey, lifecycle.Active.SPKISHA256, lifecycle.Rotation, lifecycle.RotationSignature); err != nil {
		return fmt.Errorf("invalid cross-signed rotation")
	}
	sealedRevocation, err := store.SealExternalSupervisorRootRevocation(lifecycle.Revocation)
	if err != nil || sealedRevocation != lifecycle.Revocation || lifecycle.Revocation.RevokedRootID != lifecycle.Active.RootID ||
		lifecycle.Revocation.IssuerRootID != lifecycle.Successor.RootID {
		return fmt.Errorf("invalid revocation binding")
	}
	if err := verifyDetachedSignature(successorKey, lifecycle.Successor.SPKISHA256, lifecycle.Revocation, lifecycle.RevocationSignature); err != nil {
		return fmt.Errorf("invalid signed revocation")
	}
	return nil
}

func validateTrustRootKey(root store.ExternalSupervisorTrustRootKey) (ed25519.PublicKey, error) {
	if !protocolIdentifierPattern.MatchString(root.RootID) {
		return nil, fmt.Errorf("invalid root ID")
	}
	validFrom, validFromErr := time.Parse(time.RFC3339Nano, root.ValidFrom)
	notAfter, notAfterErr := time.Parse(time.RFC3339Nano, root.NotAfter)
	if validFromErr != nil || notAfterErr != nil || !validFrom.Before(notAfter) {
		return nil, fmt.Errorf("invalid root validity")
	}
	return parsePublicKey(root.PublicKey, root.SPKISHA256)
}

func (verifier *ed25519Verifier) verifyAuthorizationAt(ctx context.Context, envelope store.ExternalSupervisorEnvelope, chain store.ExternalSupervisorAuthorizationChain, verificationTime time.Time) error {
	if verifier == nil || ctx == nil {
		return authenticationError("authorization input")
	}
	if err := verificationDeadlineError(ctx); err != nil {
		return err
	}
	if store.ValidateExternalSupervisorEnvelope(envelope) != nil || chain != verifier.bundle.Authorization ||
		(verifier.expectedPredecessor != (store.ExternalSupervisorPredecessorReleaseIdentity{}) && predecessorReleaseIdentityFromEnvelope(envelope) != verifier.expectedPredecessor) {
		return authenticationError("authorization input")
	}
	attestation, approval := chain.ReleaseAttestation, chain.ReleaseApproval
	if envelope.SupervisorArtifactSHA256 != attestation.ArtifactSHA256 || envelope.BuildIdentityHash != attestation.BuildIdentityHash ||
		envelope.RouteMappingHash != attestation.RouteMappingHash || envelope.ReleaseAttestationHash == attestation.AttestationHash ||
		envelope.ReleaseApprovalHash == approval.ApprovalHash {
		return authenticationError("authorization predecessor binding")
	}
	return verifier.verifyAuthorizationChainAt(ctx, chain, verificationTime)
}

func (verifier *ed25519Verifier) verifyAuthorizationChainAt(ctx context.Context, chain store.ExternalSupervisorAuthorizationChain, verificationTime time.Time) error {
	if verifier == nil || ctx == nil {
		return authenticationError("authorization chain input")
	}
	if err := verificationDeadlineError(ctx); err != nil {
		return err
	}
	if chain != verifier.bundle.Authorization {
		return authenticationError("authorization chain input")
	}
	verificationTime = verificationTime.UTC()
	if verificationTime.IsZero() {
		return authenticationError("authorization time")
	}
	attestation, approval, grant := chain.ReleaseAttestation, chain.ReleaseApproval, chain.MoARoleGrant
	if sealed, err := store.SealExternalSupervisorReleaseAttestation(attestation); err != nil || sealed != attestation {
		return authenticationError("attestation self hash")
	}
	if sealed, err := store.SealExternalSupervisorReleaseApproval(approval); err != nil || sealed != approval {
		return authenticationError("approval self hash")
	}
	if sealed, err := store.SealExternalSupervisorMoARoleGrant(grant); err != nil || sealed != grant {
		return authenticationError("MoA grant self hash")
	}
	if approval.AttestationHash != attestation.AttestationHash || approval.RouteMappingHash != attestation.RouteMappingHash ||
		grant.ReleaseAttestationHash != attestation.AttestationHash || grant.ReleaseApprovalHash != approval.ApprovalHash ||
		grant.RouteMappingHash != attestation.RouteMappingHash || grant.GranteeRole != "remote_supervisor_runner" {
		return authenticationError("authorization transitive binding")
	}
	if !recordValidAt(attestation.IssuedAt, attestation.NotAfter, verificationTime) ||
		!recordValidAt(approval.IssuedAt, approval.NotAfter, verificationTime) || !recordValidAt(grant.IssuedAt, grant.NotAfter, verificationTime) {
		return authenticationError("authorization validity")
	}
	attestationIssued, _ := time.Parse(time.RFC3339Nano, attestation.IssuedAt)
	approvalIssued, _ := time.Parse(time.RFC3339Nano, approval.IssuedAt)
	grantIssued, _ := time.Parse(time.RFC3339Nano, grant.IssuedAt)
	if approvalIssued.Before(attestationIssued) || grantIssued.Before(approvalIssued) {
		return authenticationError("authorization order")
	}
	checks := []struct {
		lifecycle   store.ExternalSupervisorTrustRootLifecycle
		rootID      string
		certificate store.ExternalSupervisorSignedCertificate
		role        string
		record      any
		signature   store.ExternalSupervisorDetachedSignature
		signerSPKI  string
	}{
		{verifier.bundle.ReleaseRoots, attestation.ReleaseRootID, verifier.bundle.ReleaseAttestor, "release_attestor", attestation, chain.ReleaseAttestationSignature, attestation.AttestorKeySPKISHA256},
		{verifier.bundle.ApprovalRoots, approval.ApproverRootID, verifier.bundle.ReleaseApprover, "release_approver", approval, chain.ReleaseApprovalSignature, approval.ApproverKeySPKISHA256},
		{verifier.bundle.MoARoots, grant.GrantorRootID, verifier.bundle.MoAGrantor, "moa_grantor", grant, chain.MoARoleGrantSignature, grant.GrantorKeySPKISHA256},
	}
	for _, check := range checks {
		if err := verificationDeadlineError(ctx); err != nil {
			return err
		}
		root, rootKey, err := verifier.rootAt(check.lifecycle, verificationTime)
		if err != nil || root.RootID != check.rootID {
			return authenticationError("authorization root")
		}
		leafKey, err := verifyCertificateAt(check.certificate, check.role, root, rootKey, verificationTime)
		if err != nil || check.certificate.Certificate.SubjectKeySPKISHA256 != check.signerSPKI {
			return authenticationError("authorization certificate")
		}
		if err := verifyDetachedSignature(leafKey, check.signerSPKI, check.record, check.signature); err != nil {
			return authenticationError("authorization signature")
		}
	}
	return verificationDeadlineError(ctx)
}

func (verifier *ed25519Verifier) rootAt(lifecycle store.ExternalSupervisorTrustRootLifecycle, at time.Time) (store.ExternalSupervisorTrustRootKey, ed25519.PublicKey, error) {
	successorFrom, _ := time.Parse(time.RFC3339Nano, lifecycle.Successor.ValidFrom)
	selected := lifecycle.Active
	if !at.Before(successorFrom) {
		selected = lifecycle.Successor
	}
	validFrom, _ := time.Parse(time.RFC3339Nano, selected.ValidFrom)
	notAfter, _ := time.Parse(time.RFC3339Nano, selected.NotAfter)
	if at.Before(validFrom) || !at.Before(notAfter) {
		return store.ExternalSupervisorTrustRootKey{}, nil, fmt.Errorf("root outside validity")
	}
	revokedAt, _ := time.Parse(time.RFC3339Nano, lifecycle.Revocation.EffectiveAt)
	if lifecycle.Revocation.RevokedRootID == selected.RootID && !at.Before(revokedAt) {
		return store.ExternalSupervisorTrustRootKey{}, nil, fmt.Errorf("root revoked")
	}
	key, err := parsePublicKey(selected.PublicKey, selected.SPKISHA256)
	return selected, key, err
}

func verifyCertificateAt(certificate store.ExternalSupervisorSignedCertificate, role string, root store.ExternalSupervisorTrustRootKey, rootKey ed25519.PublicKey, at time.Time) (ed25519.PublicKey, error) {
	if certificate.Certificate.Role != role || certificate.Certificate.IssuerRootID != root.RootID || !recordValidAt(certificate.Certificate.IssuedAt, certificate.Certificate.NotAfter, at) {
		return nil, fmt.Errorf("certificate binding or validity")
	}
	sealed, err := store.SealExternalSupervisorSigningCertificate(certificate.Certificate)
	if err != nil || sealed != certificate.Certificate {
		return nil, fmt.Errorf("certificate self hash")
	}
	if err := verifyDetachedSignature(rootKey, root.SPKISHA256, certificate.Certificate, certificate.Signature); err != nil {
		return nil, err
	}
	return parsePublicKey(certificate.Certificate.SubjectPublicKey, certificate.Certificate.SubjectKeySPKISHA256)
}

func verifyDetachedSignature(publicKey ed25519.PublicKey, expectedSPKI string, value any, signature store.ExternalSupervisorDetachedSignature) error {
	sealed, err := store.SealExternalSupervisorDetachedSignature(signature)
	if err != nil || sealed != signature || signature.SignerKeySPKISHA256 != expectedSPKI {
		return fmt.Errorf("detached signature metadata")
	}
	encoded, err := decodePrefixedHex(signature.Signature, "ed25519:", ed25519.SignatureSize)
	if err != nil {
		return err
	}
	canonical, err := marshalCanonical(value)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, canonical, encoded) {
		return fmt.Errorf("detached signature verification")
	}
	return nil
}

func parsePublicKey(encoded, expectedSPKI string) (ed25519.PublicKey, error) {
	return transportprimitives.ParsePublicKey(encoded, expectedSPKI)
}

func decodePrefixedHex(value, prefix string, size int) ([]byte, error) {
	return transportprimitives.DecodePrefixedHex(value, prefix, size)
}

func spkiHash(publicKey ed25519.PublicKey) (string, error) {
	return transportprimitives.SPKIHash(publicKey)
}

func predecessorReleaseIdentityFromEnvelope(envelope store.ExternalSupervisorEnvelope) store.ExternalSupervisorPredecessorReleaseIdentity {
	return store.ExternalSupervisorPredecessorReleaseIdentity{
		SupervisorArtifactSHA256: envelope.SupervisorArtifactSHA256,
		BuildIdentityHash:        envelope.BuildIdentityHash,
		ReleaseAttestationHash:   envelope.ReleaseAttestationHash,
		ReleaseApprovalHash:      envelope.ReleaseApprovalHash,
	}
}

func validPredecessorReleaseIdentity(identity store.ExternalSupervisorPredecessorReleaseIdentity) bool {
	return protocolHashPattern.MatchString(identity.SupervisorArtifactSHA256) &&
		protocolHashPattern.MatchString(identity.BuildIdentityHash) &&
		protocolHashPattern.MatchString(identity.ReleaseAttestationHash) &&
		protocolHashPattern.MatchString(identity.ReleaseApprovalHash)
}

func recordValidAt(issuedAtText, notAfterText string, at time.Time) bool {
	return transportprimitives.RecordValidAt(issuedAtText, notAfterText, at)
}

func authenticationError(reason string) error {
	return fmt.Errorf("%w: %s", ErrAuthentication, reason)
}
