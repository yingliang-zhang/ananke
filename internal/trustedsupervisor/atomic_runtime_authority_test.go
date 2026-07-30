package trustedsupervisor

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAtomicRuntimeBoundaryErrorIsClosedTypedContract(t *testing.T) {
	for _, testCase := range []struct {
		component AtomicRuntimeBoundaryComponent
		reason    AtomicRuntimeBoundaryReason
	}{
		{AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryWrongOwner},
		{AtomicRuntimeBoundaryNative, AtomicRuntimeBoundaryArtifactWriteBits},
		{AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryEffectiveWrite},
	} {
		err := unsupportedAtomicRuntimeBoundary(testCase.component, testCase.reason)
		if !errors.Is(err, ErrUnsupportedAtomicRuntimeBoundary) {
			t.Fatalf("typed boundary error %v does not unwrap to %v", err, ErrUnsupportedAtomicRuntimeBoundary)
		}
		var typed *AtomicRuntimeBoundaryError
		if !errors.As(err, &typed) || typed.Component != testCase.component || typed.Reason != testCase.reason ||
			typed.FailureClass != UnsupportedAtomicOMPRuntimeBoundaryFailureClass {
			t.Fatalf("typed boundary error = %#v", typed)
		}
		if strings.Contains(err.Error(), "/private/") || strings.Contains(err.Error(), "operator") {
			t.Fatalf("typed boundary error exposed untrusted detail: %q", err)
		}
	}
	if unsupportedAtomicRuntimeBoundary("caller_component", AtomicRuntimeBoundaryWrongOwner) != ErrUnsupportedAtomicRuntimeBoundary ||
		unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, "caller_reason") != ErrUnsupportedAtomicRuntimeBoundary {
		t.Fatal("unknown boundary enums did not collapse to the closed sentinel")
	}
}

func TestProductionServerRejectsMutableOMPRuntimeBeforePublishingAuthority(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	config := serverConfigForTest(material, now)
	var gatewayLookup atomic.Int32
	config.testBrokerDependencies.LookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		gatewayLookup.Add(1)
		return nil, errors.New("gateway must not be constructed")
	}
	server, err := newServerWithNamespaceAuthority(config, productionAtomicRuntimeAuthorityVerifier(), testAuditNamespaceAuthorityOptions())
	if server != nil || !errors.Is(err, ErrUnsupportedAtomicRuntimeBoundary) {
		t.Fatalf("NewServer mutable runtime = %v, %v; want typed closed rejection", server, err)
	}
	var typed *AtomicRuntimeBoundaryError
	if !errors.As(err, &typed) || typed.FailureClass != UnsupportedAtomicOMPRuntimeBoundaryFailureClass {
		t.Fatalf("NewServer boundary error = %#v", typed)
	}
	if gatewayLookup.Load() != 0 {
		t.Fatalf("NewServer constructed gateway dependencies %d times", gatewayLookup.Load())
	}
	for _, path := range []string{material.socketPath, material.journalPath, material.journalPath + "-wal", material.journalPath + "-shm"} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("NewServer published %s before runtime admission: %v", filepath.Base(path), statErr)
		}
	}
}

func TestRunAuditInvocationRejectsRuntimeBeforeEveryExternalEffect(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin production audit boundary")
	}
	material := newGitArchivePolicyMaterial(t)
	snapshot := materializeSnapshotForExecutorTest(t, material, "audit_run_atomic_boundary_001")
	invocation, err := prepareAuditInvocation(material.policy, material.entry, snapshot, "audit_run_atomic_boundary_001", "audit_run_atomic_boundary_001", auditResume{})
	if err != nil {
		t.Fatal(err)
	}
	material.policy.atomicRuntimeAuthorityVerifier = atomicRuntimeAuthorityVerifierFunc(func(executionPolicyEntry, []byte) (*atomicRuntimeAuthorityLease, error) {
		return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryEffectiveWrite)
	})
	var gatewayLookup, credentialLookup, beforeStart, startGate, brokerReady, transportReady atomic.Int32
	dependencies := material.policy.testBrokerDependencies
	dependencies.LookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		gatewayLookup.Add(1)
		return nil, errors.New("gateway must not be constructed")
	}
	material.policy.testBrokerDependencies = dependencies
	result, err := runAuditInvocation(context.Background(), material.policy, material.entry, invocation, auditInvocationHooks{
		CredentialLookup: func(string) (string, bool) { credentialLookup.Add(1); return "", false },
		BeforeStart:      func() { beforeStart.Add(1) },
		StartGate: func() (func(), error) {
			startGate.Add(1)
			return func() {}, nil
		},
		BrokerReady:    func(string, string) { brokerReady.Add(1) },
		TransportReady: func(auditInvocation) error { transportReady.Add(1); return nil },
	})
	if result.PID != 0 || !errors.Is(err, ErrUnsupportedAtomicRuntimeBoundary) {
		t.Fatalf("run mutable runtime = %+v, %v; want typed pre-start rejection", result, err)
	}
	if got := []int32{gatewayLookup.Load(), credentialLookup.Load(), beforeStart.Load(), startGate.Load(), brokerReady.Load(), transportReady.Load()}; !reflect.DeepEqual(got, []int32{0, 0, 0, 0, 0, 0}) {
		t.Fatalf("pre-admission external effects = %v", got)
	}
	if _, statErr := os.Lstat(invocation.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("wrapper or child published output before admission: %v", statErr)
	}
}

func TestAtomicRuntimeAuthorityRejectsAncestorAndArtifactMatrix(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin openat authority traversal")
	}
	for _, testCase := range []struct {
		name      string
		mutate    func(*testing.T, *atomicRuntimeAuthorityFixture, *atomicRuntimeAuthorityDependencies)
		component AtomicRuntimeBoundaryComponent
		reason    AtomicRuntimeBoundaryReason
	}{
		{
			name: "ancestor wrong owner",
			mutate: func(t *testing.T, fixture *atomicRuntimeAuthorityFixture, dependencies *atomicRuntimeAuthorityDependencies) {
				fixture.leaveOwnerForPath(t, dependencies, filepath.Dir(fixture.entry.OMPExecutable.Path))
			},
			component: AtomicRuntimeBoundaryAncestor,
			reason:    AtomicRuntimeBoundaryWrongOwner,
		},
		{
			name: "ACL supplementary group or effective write",
			mutate: func(_ *testing.T, fixture *atomicRuntimeAuthorityFixture, dependencies *atomicRuntimeAuthorityDependencies) {
				fixture.allowEffectiveWriteForPath(dependencies, filepath.Dir(fixture.entry.OMPNativeAddon.Path))
			},
			component: AtomicRuntimeBoundaryAncestor,
			reason:    AtomicRuntimeBoundaryEffectiveWrite,
		},
		{
			name: "symlink ancestor",
			mutate: func(t *testing.T, fixture *atomicRuntimeAuthorityFixture, _ *atomicRuntimeAuthorityDependencies) {
				fixture.replaceExecutableAncestorWithSymlink(t)
			},
			component: AtomicRuntimeBoundaryAncestor,
			reason:    AtomicRuntimeBoundarySymlink,
		},
		{
			name: "artifact wrong owner",
			mutate: func(t *testing.T, fixture *atomicRuntimeAuthorityFixture, dependencies *atomicRuntimeAuthorityDependencies) {
				fixture.leaveOwnerForPath(t, dependencies, fixture.entry.OMPNativeAddon.Path)
			},
			component: AtomicRuntimeBoundaryNative,
			reason:    AtomicRuntimeBoundaryWrongOwner,
		},
		{
			name: "artifact mode has write bits",
			mutate: func(t *testing.T, fixture *atomicRuntimeAuthorityFixture, _ *atomicRuntimeAuthorityDependencies) {
				if err := os.Chmod(fixture.entry.OMPExecutable.Path, 0o755); err != nil {
					t.Fatal(err)
				}
				fixture.entry.OMPExecutable.Mode = 0o755
				fixture.rebind(t)
			},
			component: AtomicRuntimeBoundaryExecutable,
			reason:    AtomicRuntimeBoundaryArtifactWriteBits,
		},
		{
			name: "A to B to A mutable hierarchy remains unsupported",
			mutate: func(t *testing.T, fixture *atomicRuntimeAuthorityFixture, dependencies *atomicRuntimeAuthorityDependencies) {
				parent := filepath.Dir(fixture.entry.OMPExecutable.Path)
				pinned := parent + ".pinned"
				if err := os.Rename(parent, pinned); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(pinned, parent); err != nil {
					t.Fatal(err)
				}
				fixture.allowEffectiveWriteForPath(dependencies, parent)
			},
			component: AtomicRuntimeBoundaryAncestor,
			reason:    AtomicRuntimeBoundaryEffectiveWrite,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAtomicRuntimeAuthorityFixture(t)
			dependencies := fixture.dependencies()
			testCase.mutate(t, &fixture, &dependencies)
			lease, err := verifyAtomicOMPRuntimeAuthority(fixture.entry, fixture.wrapper, dependencies)
			if lease != nil {
				_ = lease.Close()
			}
			var typed *AtomicRuntimeBoundaryError
			if !errors.As(err, &typed) || typed.Component != testCase.component || typed.Reason != testCase.reason {
				t.Fatalf("authority rejection = %#v, %v; want %s/%s", typed, err, testCase.component, testCase.reason)
			}
		})
	}
}

func TestAtomicRuntimeAuthorityRetainsCLOEXECDescriptorsUntilClose(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin descriptor authority")
	}
	fixture := newAtomicRuntimeAuthorityFixture(t)
	dependencies := fixture.dependencies()
	lease, err := verifyAtomicOMPRuntimeAuthority(fixture.entry, fixture.wrapper, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	descriptors := lease.descriptorNumbers()
	want := len(fixture.entry.OMPRuntimeAuthority.ExecutableAncestors) + len(fixture.entry.OMPRuntimeAuthority.NativeAddonAncestors) + 2
	if len(descriptors) != want {
		_ = lease.Close()
		t.Fatalf("retained descriptors = %d, want %d", len(descriptors), want)
	}
	for _, descriptor := range descriptors {
		flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			_ = lease.Close()
			t.Fatalf("retained FD %d flags = %#x, %v; want CLOEXEC", descriptor, flags, err)
		}
	}
	command := exec.Command("/bin/sh", "-c", "exit 0")
	if len(command.ExtraFiles) != 0 {
		_ = lease.Close()
		t.Fatal("runtime artifact descriptors were configured for child inheritance")
	}
	if err := command.Run(); err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	for _, descriptor := range descriptors {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); err != nil {
			_ = lease.Close()
			t.Fatalf("retained FD %d closed before process exit: %v", descriptor, err)
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range descriptors {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatalf("retained FD %d after close = %v, want EBADF", descriptor, err)
		}
	}
}

func TestAtomicRuntimeAuthorityHashBindsDirectLauncherAndOutputPolicy(t *testing.T) {
	fixture := newAtomicRuntimeAuthorityFixture(t)
	authority := fixture.entry.OMPRuntimeAuthority
	sealed, err := sealExecutionPolicyOMPRuntimeAuthority(authority, fixture.entry.OMPExecutable, fixture.entry.OMPNativeAddon)
	if err != nil || !reflect.DeepEqual(sealed, authority) {
		t.Fatalf("sealed runtime authority = %+v, %v", sealed, err)
	}
	for _, mutate := range []func(*executionPolicyOMPRuntimeAuthority){
		func(value *executionPolicyOMPRuntimeAuthority) { value.ExecutableAncestors[0].Inode = "999" },
		func(value *executionPolicyOMPRuntimeAuthority) { value.NativeAddonAncestors[0].Mode ^= 1 },
		func(value *executionPolicyOMPRuntimeAuthority) { value.LauncherMode = "wrapper" },
		func(value *executionPolicyOMPRuntimeAuthority) { value.OMPArgvPolicy = "caller_args" },
		func(value *executionPolicyOMPRuntimeAuthority) { value.OutputTransport = "child_file" },
		func(value *executionPolicyOMPRuntimeAuthority) { value.TimeoutOwner = "child" },
		func(value *executionPolicyOMPRuntimeAuthority) {
			value.WrapperCompatibilityOracleSHA256 = testHash("other-wrapper")
		},
		func(value *executionPolicyOMPRuntimeAuthority) { value.ArtifactFDPolicy = "inherited" },
	} {
		drifted := cloneExecutionPolicyOMPRuntimeAuthority(authority)
		mutate(&drifted)
		sealed, err := sealExecutionPolicyOMPRuntimeAuthority(drifted, fixture.entry.OMPExecutable, fixture.entry.OMPNativeAddon)
		if err == nil && sealed.AuthorityHash == authority.AuthorityHash {
			t.Fatalf("runtime authority drift retained hash: %+v", drifted)
		}
	}
}

func TestServerConfigHasNoRuntimeVerifierBypass(t *testing.T) {
	if _, found := reflect.TypeOf(ServerConfig{}).FieldByName("testAtomicRuntimeAuthorityVerifier"); found {
		t.Fatal("exported production configuration contains a runtime verifier bypass")
	}
}

func acceptingAtomicRuntimeAuthorityVerifierForTest() atomicRuntimeAuthorityVerifier {
	return atomicRuntimeAuthorityVerifierFunc(func(executionPolicyEntry, []byte) (*atomicRuntimeAuthorityLease, error) {
		return &atomicRuntimeAuthorityLease{}, nil
	})
}

func newServerForTest(config ServerConfig) (*Server, error) {
	return newServerWithNamespaceAuthority(config, acceptingAtomicRuntimeAuthorityVerifierForTest(), testAuditNamespaceAuthorityOptions())
}

func loadExecutionPolicyForTest(path string, ownerUID uint32) (*executionPolicy, error) {
	policy, err := loadExecutionPolicyWithNamespaceAuthority(path, ownerUID, testAuditNamespaceAuthorityOptions())
	if err == nil {
		policy.atomicRuntimeAuthorityVerifier = acceptingAtomicRuntimeAuthorityVerifierForTest()
	}
	return policy, err
}

type atomicRuntimeAuthorityFixture struct {
	entry       executionPolicyEntry
	wrapper     []byte
	root        string
	original    atomicRuntimeAuthorityDependencies
	ownerLeaves map[atomicRuntimeFileKey]struct{}
	writeAllows map[atomicRuntimeFileKey]struct{}
}

type atomicRuntimeFileKey struct {
	device uint64
	inode  uint64
}

func newAtomicRuntimeAuthorityFixture(t *testing.T) atomicRuntimeAuthorityFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "omp", supportedOMPVersion, "bin", "omp")
	dataRoot := filepath.Join(root, "omp-data")
	native := filepath.Join(dataRoot, "omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
	for _, directory := range []string{filepath.Dir(executable), filepath.Dir(native)} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec /bin/sleep 1\n"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("immutable-native-addon"), 0o444); err != nil {
		t.Fatal(err)
	}
	wrapper := []byte("#!/bin/bash\nset -eu\nomp --version\n")
	entry := executionPolicyEntry{
		OMPVersion: supportedOMPVersion, OMPExecutable: fileIdentityForTest(t, executable),
		OMPExecutableRoot: directoryIdentityForTest(t, filepath.Dir(executable)), OMPNativeAddon: fileIdentityForTest(t, native),
	}
	entry.Wrapper.SHA256 = hashJournalBytes(wrapper)
	entry.OMPExecutable.OwnerUID = 0
	entry.OMPExecutableRoot.OwnerUID = 0
	entry.OMPNativeAddon.OwnerUID = 0
	authority := executionPolicyOMPRuntimeAuthority{
		SchemaVersion:                    atomicOMPRuntimeAuthoritySchemaVersion,
		AuthorityPolicyVersion:           atomicOMPRuntimeAuthorityPolicyVersion,
		TrustedOwnerUID:                  0,
		ExecutableAncestors:              rootOwnedAncestorIdentitiesForTest(t, executable),
		NativeAddonAncestors:             rootOwnedAncestorIdentitiesForTest(t, native),
		NativeDataRoot:                   dataRoot,
		DeniedNativeFallbackRoots:        []string{filepath.Join(root, "home", ".omp"), filepath.Join(filepath.Dir(executable), "natives")},
		LauncherMode:                     atomicOMPLauncherModeDirectPinned,
		OMPArgvPolicy:                    atomicOMPArgvPolicyExactSudoRoute,
		SandboxTargetPolicy:              atomicOMPSandboxTargetPolicyExactPinned,
		OutputTransport:                  atomicOMPOutputTransportSupervisorStdout,
		TimeoutOwner:                     atomicOMPTimeoutOwnerSupervisor,
		WrapperCompatibilityOracleSHA256: hashJournalBytes(wrapper),
		ArtifactFDPolicy:                 atomicOMPRuntimeArtifactFDPolicyParentRetainedCLOEXEC,
		ProcessGroupPolicy:               atomicOMPProcessGroupPolicySingleGroup,
	}
	authority, err = sealExecutionPolicyOMPRuntimeAuthority(authority, entry.OMPExecutable, entry.OMPNativeAddon)
	if err != nil {
		t.Fatal(err)
	}
	entry.OMPRuntimeAuthority = authority
	return atomicRuntimeAuthorityFixture{
		entry: entry, wrapper: wrapper, root: root, original: systemAtomicRuntimeAuthorityDependencies(),
		ownerLeaves: make(map[atomicRuntimeFileKey]struct{}), writeAllows: make(map[atomicRuntimeFileKey]struct{}),
	}
}

func (fixture *atomicRuntimeAuthorityFixture) rebind(t *testing.T) {
	t.Helper()
	var err error
	fixture.entry.Wrapper.SHA256 = hashJournalBytes(fixture.wrapper)
	fixture.entry.OMPRuntimeAuthority.WrapperCompatibilityOracleSHA256 = hashJournalBytes(fixture.wrapper)
	fixture.entry.OMPRuntimeAuthority, err = sealExecutionPolicyOMPRuntimeAuthority(
		fixture.entry.OMPRuntimeAuthority, fixture.entry.OMPExecutable, fixture.entry.OMPNativeAddon,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func (fixture *atomicRuntimeAuthorityFixture) dependencies() atomicRuntimeAuthorityDependencies {
	dependencies := fixture.original
	originalFstat := dependencies.Fstat
	dependencies.Fstat = func(descriptor int, status *unix.Stat_t) error {
		if err := originalFstat(descriptor, status); err != nil {
			return err
		}
		key := atomicRuntimeFileKey{device: uint64(status.Dev), inode: status.Ino}
		if _, leave := fixture.ownerLeaves[key]; !leave {
			status.Uid = 0
		}
		return nil
	}
	originalFaccessat := dependencies.Faccessat
	dependencies.Faccessat = func(directoryFD int, path string, mode uint32, flags int) error {
		if mode != unix.W_OK || flags&unix.AT_EACCESS == 0 || flags&unix.AT_SYMLINK_NOFOLLOW == 0 {
			return unix.EINVAL
		}
		key, err := descriptorTargetKeyForTest(directoryFD, path)
		if err != nil {
			return err
		}
		if _, writable := fixture.writeAllows[key]; writable {
			return nil
		}
		_ = originalFaccessat
		return unix.EACCES
	}
	return dependencies
}

func (fixture *atomicRuntimeAuthorityFixture) leaveOwnerForPath(t *testing.T, dependencies *atomicRuntimeAuthorityDependencies, path string) {
	t.Helper()
	fixture.ownerLeaves[fileKeyForPathForTest(t, path)] = struct{}{}
	*dependencies = fixture.dependencies()
}

func (fixture *atomicRuntimeAuthorityFixture) allowEffectiveWriteForPath(dependencies *atomicRuntimeAuthorityDependencies, path string) {
	information, err := os.Stat(path)
	if err != nil {
		panic(err)
	}
	status := information.Sys().(*syscall.Stat_t)
	fixture.writeAllows[atomicRuntimeFileKey{device: uint64(status.Dev), inode: status.Ino}] = struct{}{}
	*dependencies = fixture.dependencies()
}

func (fixture *atomicRuntimeAuthorityFixture) replaceExecutableAncestorWithSymlink(t *testing.T) {
	t.Helper()
	bin := filepath.Dir(fixture.entry.OMPExecutable.Path)
	realBin := bin + ".real"
	if err := os.Rename(bin, realBin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, bin); err != nil {
		t.Fatal(err)
	}
}

func rootOwnedAncestorIdentitiesForTest(t *testing.T, artifact string) []executionPolicyDirectoryIdentity {
	t.Helper()
	parent := filepath.Dir(artifact)
	paths := []string{"/"}
	if parent != "/" {
		current := ""
		for _, component := range strings.Split(strings.TrimPrefix(parent, "/"), "/") {
			current += "/" + component
			paths = append(paths, current)
		}
	}
	identities := make([]executionPolicyDirectoryIdentity, 0, len(paths))
	for _, path := range paths {
		identity := directoryIdentityForTest(t, path)
		identity.OwnerUID = 0
		information, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		identity.Mode = uint32(information.Mode().Perm())
		identities = append(identities, identity)
	}
	return identities
}

func fileKeyForPathForTest(t *testing.T, path string) atomicRuntimeFileKey {
	t.Helper()
	information, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("path stat has no syscall identity")
	}
	return atomicRuntimeFileKey{device: uint64(status.Dev), inode: status.Ino}
}

func descriptorTargetKeyForTest(directoryFD int, path string) (atomicRuntimeFileKey, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if path == "." {
		var status unix.Stat_t
		if err := unix.Fstat(directoryFD, &status); err != nil {
			return atomicRuntimeFileKey{}, err
		}
		return atomicRuntimeFileKey{device: uint64(status.Dev), inode: status.Ino}, nil
	}
	descriptor, err := unix.Openat(directoryFD, path, flags, 0)
	if err != nil {
		return atomicRuntimeFileKey{}, err
	}
	defer unix.Close(descriptor)
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return atomicRuntimeFileKey{}, err
	}
	return atomicRuntimeFileKey{device: uint64(status.Dev), inode: status.Ino}, nil
}
