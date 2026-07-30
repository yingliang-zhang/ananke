package trustedsupervisor

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func testAuditNamespaceAuthorityOptions() auditNamespaceAuthorityOptions {
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

func TestNamespaceACLInspectionFailsClosed(t *testing.T) {
	cases := []struct {
		name       string
		inspection auditNamespaceACLInspection
		probeErr   error
		wantReason InvocationNamespaceReason
	}{
		{name: "unsupported platform", inspection: auditNamespaceACLInspection{}, wantReason: InvocationNamespaceCredentialBoundary},
		{name: "probe failure", inspection: auditNamespaceACLInspection{Supported: true}, probeErr: unix.EIO, wantReason: InvocationNamespaceCredentialBoundary},
		{name: "nontrivial ACL", inspection: auditNamespaceACLInspection{Supported: true, Nontrivial: true}, wantReason: InvocationNamespaceWritable},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateAuditNamespaceACLInspection(testCase.inspection, testCase.probeErr)
			var typed *InvocationNamespaceError
			if !errors.As(err, &typed) || typed.Reason != testCase.wantReason {
				t.Fatalf("ACL inspection error = %#v, want reason %q", err, testCase.wantReason)
			}
		})
	}
	if err := validateAuditNamespaceACLInspection(auditNamespaceACLInspection{Supported: true}, nil); err != nil {
		t.Fatalf("empty native ACL inspection rejected: %v", err)
	}
}

func TestProductionNamespaceAdmissionRejectsUserOwnedFixture(t *testing.T) {
	material := newExecutionPolicyTestMaterial(t)
	runtimeUID := uint32(os.Getuid()) + 1
	if runtimeUID == 0 {
		runtimeUID = 1
	}
	runtimeGID := uint32(os.Getgid()) + 1
	if runtimeGID == 0 {
		runtimeGID = 1
	}
	policy, err := loadExecutionPolicy(material.policyPath, uint32(os.Getuid()), runtimeUID, runtimeGID)
	if policy != nil || !errors.Is(err, ErrUnsupportedInvocationNamespace) {
		t.Fatalf("load production policy in user-owned namespace = %v, %v; want typed closed rejection", policy, err)
	}
	var typed *InvocationNamespaceError
	if !errors.As(err, &typed) || typed.FailureClass != UnsupportedInvocationNamespaceFailureClass ||
		typed.Reason != InvocationNamespaceWrongOwner && typed.Reason != InvocationNamespaceSupervisorPrivilege {
		t.Fatalf("namespace boundary error = %#v", typed)
	}
}

func TestNamespaceAuthorityDescriptorSurvivesControllingPathSwap(t *testing.T) {
	container := t.TempDir()
	root := filepath.Join(container, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	authority, err := openAuditNamespaceAuthority([]string{root}, testAuditNamespaceAuthorityOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	lease, err := authority.Duplicate(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()

	original := filepath.Join(container, "original-root")
	if err := os.Rename(root, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := lease.Mkdir("attempt", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(original, "attempt")); err != nil {
		t.Fatalf("descriptor-relative mkdir missed original root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "attempt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement root received operation: %v", err)
	}
}

func TestNamespaceAuthoritySealsAndRecapturesNestedOwnedDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	authority, err := openAuditNamespaceAuthority([]string{root}, testAuditNamespaceAuthorityOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	lease, err := authority.Duplicate(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := lease.Mkdir("attempt", 0o700); err != nil {
		t.Fatal(err)
	}
	attempt, err := lease.Capture("attempt", "temporary", true)
	if err != nil {
		t.Fatal(err)
	}
	home, err := authority.mkdirAndCaptureOwnedChild(attempt, "home", "direct_omp_home", false, false)
	if err != nil {
		t.Fatal(err)
	}
	restoreAuditSealedHomeModeForTest(t, home.Path)
	state, err := authority.mkdirAndCaptureOwnedChild(home, ".omp", "direct_omp_home_state", false, false, attempt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = authority.mkdirAndCaptureOwnedChild(state, "run", "direct_omp_home_run", false, false, home, attempt)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := authority.sealAndRecaptureOwnedDirectory(state, 0o500, home, attempt)
	if err != nil {
		t.Fatal(err)
	}
	run, err := authority.captureOwnedChild(sealed, "run", "direct_omp_home_run", false, home, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Device != state.Device || sealed.Inode != state.Inode || sealed.OwnerUID != state.OwnerUID || sealed.OwnerGID != state.OwnerGID || sealed.Mode&0o777 != 0o500 ||
		run.ParentDevice != sealed.Device || run.ParentInode != sealed.Inode || run.ParentOwnerUID != sealed.OwnerUID || run.ParentOwnerGID != sealed.OwnerGID || run.ParentMode != sealed.Mode {
		t.Fatalf("sealed/rebound identities = state %+v sealed %+v run %+v", state, sealed, run)
	}
}

func TestNamespaceAuthoritySealingRejectsCapturedReplacementAndModeMutation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "replacement", mutate: func(t *testing.T, path string) {
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mode mutation", mutate: func(t *testing.T, path string) {
			if err := os.Chmod(path, 0o500); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			authority, err := openAuditNamespaceAuthority([]string{root}, testAuditNamespaceAuthorityOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer authority.Close()
			lease, err := authority.Duplicate(root)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			if err := lease.Mkdir("attempt", 0o700); err != nil {
				t.Fatal(err)
			}
			attempt, err := lease.Capture("attempt", "temporary", true)
			if err != nil {
				t.Fatal(err)
			}
			home, err := authority.mkdirAndCaptureOwnedChild(attempt, "home", "direct_omp_home", false, false)
			if err != nil {
				t.Fatal(err)
			}
			restoreAuditSealedHomeModeForTest(t, home.Path)
			state, err := authority.mkdirAndCaptureOwnedChild(home, ".omp", "direct_omp_home_state", false, false, attempt)
			if err != nil {
				t.Fatal(err)
			}
			testCase.mutate(t, state.Path)
			if _, err := authority.sealAndRecaptureOwnedDirectory(state, 0o500, home, attempt); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("stale sealed directory error = %v, want %v", err, ErrAuthentication)
			}
		})
	}
}

func TestAuthenticatedOwnedRootCleanupRejectsRenamedOriginalAndDecoy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	authority, err := openAuditNamespaceAuthority([]string{root}, testAuditNamespaceAuthorityOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	lease, err := authority.Duplicate(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Mkdir("attempt", 0o700); err != nil {
		t.Fatal(err)
	}
	identity, err := lease.Capture("attempt", "attempt_001_prompt", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "retained-original")
	if err := os.Rename(filepath.Join(root, "attempt"), original); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(root, "attempt")
	if err := os.Mkdir(decoy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "decoy"), []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scrubAndRemoveAuthenticatedAuditRoots(authority, []auditOwnedRootIdentity{identity}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("cleanup decoy error = %v, want authentication rejection", err)
	}
	for _, path := range []string{original, filepath.Join(decoy, "decoy")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cleanup altered retained path %q: %v", path, err)
		}
	}
}

func TestSignedOwnedRootIdentityRejectsTamperReorderAndOmission(t *testing.T) {
	expected := []auditOwnedRootIdentity{
		{Role: "attempt_001_prompt", Path: "/trusted/prompt/run", ParentPath: "/trusted/prompt", Device: "1", Inode: "11", OwnerUID: 0, OwnerGID: 0, Mode: uint32(unix.S_IFDIR | 0o700), ParentDevice: "1", ParentInode: "2", ParentOwnerUID: 0, ParentOwnerGID: 0, ParentMode: uint32(unix.S_IFDIR | 0o700), CleanupRoot: true},
		{Role: "shared_work", Path: "/trusted/work/run", ParentPath: "/trusted/work", Device: "1", Inode: "12", OwnerUID: 0, OwnerGID: 0, Mode: uint32(unix.S_IFDIR | 0o555), ParentDevice: "1", ParentInode: "3", ParentOwnerUID: 0, ParentOwnerGID: 0, ParentMode: uint32(unix.S_IFDIR | 0o700), CleanupRoot: true},
	}
	if err := validateExactAuditOwnedRootIdentities(expected, expected); err != nil {
		t.Fatalf("exact identities rejected: %v", err)
	}
	cases := map[string][]auditOwnedRootIdentity{
		"tamper": func() []auditOwnedRootIdentity {
			value := append([]auditOwnedRootIdentity(nil), expected...)
			value[0].Inode = "99"
			return value
		}(),
		"reorder":  {expected[1], expected[0]},
		"omission": {expected[0]},
	}
	for name, actual := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateExactAuditOwnedRootIdentities(actual, expected); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("identity mutation error = %v, want authentication rejection", err)
			}
		})
	}
}

func TestNamespaceDescriptorsAreCLOEXECAndCloseDeterministically(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	authority, err := openAuditNamespaceAuthority([]string{root}, testAuditNamespaceAuthorityOptions())
	if err != nil {
		t.Fatal(err)
	}
	descriptors := authority.descriptorsForTest()
	if len(descriptors) != 1 {
		t.Fatalf("retained descriptors = %v, want one", descriptors)
	}
	lease, err := authority.Duplicate(root)
	if err != nil {
		t.Fatal(err)
	}
	descriptors = append(descriptors, lease.descriptorForTest())
	for _, descriptor := range descriptors {
		flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			t.Fatalf("descriptor %d flags = %#x, %v; want CLOEXEC", descriptor, flags, err)
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range descriptors {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatalf("descriptor %d after close = %v, want EBADF", descriptor, err)
		}
	}
}

func TestProductionConfigHasRuntimeCredentialWithoutNamespaceBypass(t *testing.T) {
	typeOf := reflect.TypeOf(ServerConfig{})
	for _, name := range []string{"RuntimeUserID", "RuntimeGroupID"} {
		if _, found := typeOf.FieldByName(name); !found {
			t.Fatalf("ServerConfig missing %s", name)
		}
	}
	for _, name := range []string{"TestNamespaceBypass", "AllowUnsafeNamespace", "testNamespaceAuthority"} {
		if _, found := typeOf.FieldByName(name); found {
			t.Fatalf("production configuration exposes namespace bypass %s", name)
		}
	}
	options := productionAuditNamespaceAuthorityOptions(0, 501, 20)
	if options.emulateBoundary || options.testOnlyStable {
		t.Fatalf("production namespace options expose test stability: %+v", options)
	}
}
