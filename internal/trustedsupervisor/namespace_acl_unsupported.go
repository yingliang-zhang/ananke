//go:build !darwin || !cgo

package trustedsupervisor

func inspectAuditNamespaceACL(int) (auditNamespaceACLInspection, error) {
	// Stable cleanup cannot be proved without Darwin's descriptor ACL API.
	return auditNamespaceACLInspection{}, nil
}
