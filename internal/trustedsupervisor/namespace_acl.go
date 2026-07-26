package trustedsupervisor

// auditNamespaceACLInspection is deliberately narrower than an effective ACL
// emulator. Production accepts a stable hierarchy only when the native
// platform API proves that no extended ACL entry exists at all.
type auditNamespaceACLInspection struct {
	Supported  bool
	Nontrivial bool
}

func validateAuditNamespaceACLInspection(inspection auditNamespaceACLInspection, probeErr error) error {
	if probeErr != nil || !inspection.Supported {
		return unsupportedInvocationNamespace(InvocationNamespaceCredentialBoundary)
	}
	if inspection.Nontrivial {
		return unsupportedInvocationNamespace(InvocationNamespaceWritable)
	}
	return nil
}
