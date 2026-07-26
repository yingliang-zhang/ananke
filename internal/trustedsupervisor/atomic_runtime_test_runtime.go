//go:build ananke_test_runtime_authority

package trustedsupervisor

import "os"

var compileTimeTestRuntimeAuthorityMarker = "ananke-compile-time-test-only-runtime-authority-v1"

// NewServerWithCompileTimeTestRuntimeAuthority is available only in binaries
// built with the explicit test tag. Production builds cannot reference it.
func compileTimeTestNamespaceAuthorityOptions() auditNamespaceAuthorityOptions {
	runtimeUID := uint32(os.Getuid()) + 1
	if runtimeUID == 0 {
		runtimeUID = 1
	}
	runtimeGID := uint32(os.Getgid()) + 1
	if runtimeGID == 0 {
		runtimeGID = 1
	}
	return auditNamespaceAuthorityOptions{
		trustedOwnerUID: uint32(os.Getuid()), runtimeUID: runtimeUID, runtimeGID: runtimeGID,
		emulateBoundary: true, testOnlyStable: true,
	}
}

func NewServerWithCompileTimeTestRuntimeAuthority(config ServerConfig) (*Server, error) {
	if compileTimeTestRuntimeAuthorityMarker == "" {
		return nil, ErrUnsupportedAtomicRuntimeBoundary
	}
	return newServerWithNamespaceAuthority(config, atomicRuntimeAuthorityVerifierFunc(func(executionPolicyEntry, []byte) (*atomicRuntimeAuthorityLease, error) {
		return &atomicRuntimeAuthorityLease{}, nil
	}), compileTimeTestNamespaceAuthorityOptions())
}
