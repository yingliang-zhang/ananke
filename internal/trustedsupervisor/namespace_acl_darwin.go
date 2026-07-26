//go:build darwin && cgo

package trustedsupervisor

/*
#include <errno.h>
#include <sys/acl.h>

static int ananke_inspect_extended_acl(int fd, int *nontrivial) {
	acl_t acl;
	int saved_errno = 0;

	*nontrivial = 0;
	errno = 0;
	acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED);
	if (acl == NULL) {
		if (errno == ENOENT) {
			return 0;
		}
		return errno == 0 ? EIO : errno;
	}
	// Darwin returns an allocated ACL object only when extended ACL data is
	// present. Reject the whole object; do not emulate entry ordering,
	// inheritance, or allow/deny effective-permission semantics.
	*nontrivial = 1;
	if (acl_free(acl) != 0) {
		saved_errno = errno == 0 ? EIO : errno;
	}
	return saved_errno;
}
*/
import "C"

import "syscall"

func inspectAuditNamespaceACL(descriptor int) (auditNamespaceACLInspection, error) {
	inspection := auditNamespaceACLInspection{Supported: true}
	var nontrivial C.int
	if errno := C.ananke_inspect_extended_acl(C.int(descriptor), &nontrivial); errno != 0 {
		return inspection, syscall.Errno(errno)
	}
	inspection.Nontrivial = nontrivial != 0
	return inspection, nil
}
