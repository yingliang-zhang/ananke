package trustedsupervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var ErrUnsupportedAtomicRuntimeBoundary = errors.New("installed OMP runtime lacks atomic executable/native authority")

type AtomicRuntimeBoundaryComponent string

type AtomicRuntimeBoundaryReason string

type AtomicRuntimeBoundaryFailureClass string

const (
	AtomicRuntimeBoundaryExecutable AtomicRuntimeBoundaryComponent = "executable"
	AtomicRuntimeBoundaryNative     AtomicRuntimeBoundaryComponent = "native"
	AtomicRuntimeBoundaryAncestor   AtomicRuntimeBoundaryComponent = "ancestor"

	AtomicRuntimeBoundaryInvalidPath       AtomicRuntimeBoundaryReason = "invalid_path"
	AtomicRuntimeBoundaryUnsupportedLayout AtomicRuntimeBoundaryReason = "unsupported_layout"
	AtomicRuntimeBoundarySymlink           AtomicRuntimeBoundaryReason = "symlink"
	AtomicRuntimeBoundaryWrongOwner        AtomicRuntimeBoundaryReason = "wrong_owner"
	AtomicRuntimeBoundaryWrongType         AtomicRuntimeBoundaryReason = "wrong_type"
	AtomicRuntimeBoundaryWrongMode         AtomicRuntimeBoundaryReason = "wrong_mode"
	AtomicRuntimeBoundaryArtifactWriteBits AtomicRuntimeBoundaryReason = "artifact_write_bits"
	AtomicRuntimeBoundaryEffectiveWrite    AtomicRuntimeBoundaryReason = "effective_write"
	AtomicRuntimeBoundaryIdentityChanged   AtomicRuntimeBoundaryReason = "identity_changed"
	AtomicRuntimeBoundaryHashMismatch      AtomicRuntimeBoundaryReason = "hash_mismatch"
	AtomicRuntimeBoundaryAuthorityBinding  AtomicRuntimeBoundaryReason = "authority_binding"

	UnsupportedAtomicOMPRuntimeBoundaryFailureClass AtomicRuntimeBoundaryFailureClass = "unsupported_atomic_omp_runtime_boundary"

	atomicOMPRuntimeAuthoritySchemaVersion                = "ananke.atomic-omp-runtime-authority.v3"
	atomicOMPRuntimeAuthorityPolicyVersion                = "root-owned-immutable-hierarchy-direct-exec.v3"
	atomicOMPRuntimeArtifactFDPolicyParentRetainedCLOEXEC = "parent_retained_cloexec_not_inherited"
	atomicOMPProcessGroupPolicySingleGroup                = "trusted_supervisor_single_group_v1"
	atomicOMPLauncherModeDirectPinned                     = "sandbox_exec_direct_pinned_omp_v1"
	atomicOMPArgvPolicyExactSudoRoute                     = "omp_print_exact_prompt_sudo_route_v1"
	atomicOMPSandboxTargetPolicyExactPinned               = "exact_pinned_omp_executable_v1"
	atomicOMPOutputTransportSupervisorStdout              = "supervisor_bounded_stdout_private_file_v1"
	atomicOMPTimeoutOwnerSupervisor                       = "trusted_supervisor_typed_observation_v1"
)

type AtomicRuntimeBoundaryError struct {
	Component    AtomicRuntimeBoundaryComponent
	Reason       AtomicRuntimeBoundaryReason
	FailureClass AtomicRuntimeBoundaryFailureClass
}

func (failure *AtomicRuntimeBoundaryError) Error() string {
	return ErrUnsupportedAtomicRuntimeBoundary.Error()
}

func (failure *AtomicRuntimeBoundaryError) Unwrap() error {
	return ErrUnsupportedAtomicRuntimeBoundary
}

func unsupportedAtomicRuntimeBoundary(component AtomicRuntimeBoundaryComponent, reason AtomicRuntimeBoundaryReason) error {
	if !validAtomicRuntimeBoundaryComponent(component) || !validAtomicRuntimeBoundaryReason(reason) {
		return ErrUnsupportedAtomicRuntimeBoundary
	}
	return &AtomicRuntimeBoundaryError{
		Component: component, Reason: reason, FailureClass: UnsupportedAtomicOMPRuntimeBoundaryFailureClass,
	}
}

func validAtomicRuntimeBoundaryComponent(component AtomicRuntimeBoundaryComponent) bool {
	switch component {
	case AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryNative, AtomicRuntimeBoundaryAncestor:
		return true
	default:
		return false
	}
}

func validAtomicRuntimeBoundaryReason(reason AtomicRuntimeBoundaryReason) bool {
	switch reason {
	case AtomicRuntimeBoundaryInvalidPath, AtomicRuntimeBoundaryUnsupportedLayout, AtomicRuntimeBoundarySymlink,
		AtomicRuntimeBoundaryWrongOwner, AtomicRuntimeBoundaryWrongType, AtomicRuntimeBoundaryWrongMode,
		AtomicRuntimeBoundaryArtifactWriteBits, AtomicRuntimeBoundaryEffectiveWrite, AtomicRuntimeBoundaryIdentityChanged,
		AtomicRuntimeBoundaryHashMismatch, AtomicRuntimeBoundaryAuthorityBinding:
		return true
	default:
		return false
	}
}

type executionPolicyOMPRuntimeAuthority struct {
	SchemaVersion                    string                             `json:"schema_version"`
	AuthorityPolicyVersion           string                             `json:"authority_policy_version"`
	TrustedOwnerUID                  uint32                             `json:"trusted_owner_uid"`
	ExecutableAncestors              []executionPolicyDirectoryIdentity `json:"executable_ancestors"`
	NativeAddonAncestors             []executionPolicyDirectoryIdentity `json:"native_addon_ancestors"`
	NativeDataRoot                   string                             `json:"native_data_root"`
	DeniedNativeFallbackRoots        []string                           `json:"denied_native_fallback_roots"`
	LauncherMode                     string                             `json:"launcher_mode"`
	OMPArgvPolicy                    string                             `json:"omp_argv_policy"`
	SandboxTargetPolicy              string                             `json:"sandbox_target_policy"`
	OutputTransport                  string                             `json:"output_transport"`
	TimeoutOwner                     string                             `json:"timeout_owner"`
	WrapperCompatibilityOracleSHA256 string                             `json:"wrapper_compatibility_oracle_sha256"`
	ArtifactFDPolicy                 string                             `json:"artifact_fd_policy"`
	ProcessGroupPolicy               string                             `json:"process_group_policy"`
	AuthorityHash                    string                             `json:"authority_hash"`
}

func sealExecutionPolicyOMPRuntimeAuthority(
	authority executionPolicyOMPRuntimeAuthority,
	executable executionPolicyFileIdentity,
	nativeAddon executionPolicyFileIdentity,
) (executionPolicyOMPRuntimeAuthority, error) {
	authority.AuthorityHash = ""
	hashValue, err := canonicalHash(struct {
		SchemaVersion string                             `json:"schema_version"`
		Authority     executionPolicyOMPRuntimeAuthority `json:"authority"`
		Executable    executionPolicyFileIdentity        `json:"executable"`
		NativeAddon   executionPolicyFileIdentity        `json:"native_addon"`
	}{
		SchemaVersion: atomicOMPRuntimeAuthoritySchemaVersion,
		Authority:     authority, Executable: executable, NativeAddon: nativeAddon,
	})
	if err != nil {
		return executionPolicyOMPRuntimeAuthority{}, err
	}
	authority.AuthorityHash = hashValue
	return authority, nil
}

func cloneExecutionPolicyOMPRuntimeAuthority(authority executionPolicyOMPRuntimeAuthority) executionPolicyOMPRuntimeAuthority {
	authority.ExecutableAncestors = append([]executionPolicyDirectoryIdentity(nil), authority.ExecutableAncestors...)
	authority.NativeAddonAncestors = append([]executionPolicyDirectoryIdentity(nil), authority.NativeAddonAncestors...)
	authority.DeniedNativeFallbackRoots = append([]string(nil), authority.DeniedNativeFallbackRoots...)
	return authority
}
func validateExecutionPolicyAtomicRuntimeAuthority(entry executionPolicyEntry) error {
	authority := entry.OMPRuntimeAuthority
	sealed, err := sealExecutionPolicyOMPRuntimeAuthority(authority, entry.OMPExecutable, entry.OMPNativeAddon)
	if err != nil || authority.SchemaVersion != atomicOMPRuntimeAuthoritySchemaVersion ||
		authority.AuthorityPolicyVersion != atomicOMPRuntimeAuthorityPolicyVersion || authority.TrustedOwnerUID != 0 ||
		authority.ArtifactFDPolicy != atomicOMPRuntimeArtifactFDPolicyParentRetainedCLOEXEC ||
		authority.ProcessGroupPolicy != atomicOMPProcessGroupPolicySingleGroup ||
		authority.LauncherMode != atomicOMPLauncherModeDirectPinned || authority.OMPArgvPolicy != atomicOMPArgvPolicyExactSudoRoute ||
		authority.SandboxTargetPolicy != atomicOMPSandboxTargetPolicyExactPinned ||
		authority.OutputTransport != atomicOMPOutputTransportSupervisorStdout || authority.TimeoutOwner != atomicOMPTimeoutOwnerSupervisor ||
		sealed.AuthorityHash != authority.AuthorityHash || !protocolHashPattern.MatchString(authority.AuthorityHash) ||
		!validAtomicNativeLayout(entry, authority) || !validDeniedNativeFallbackRoots(authority.NativeDataRoot, authority.DeniedNativeFallbackRoots) ||
		!validExecutionPolicyAtomicAncestors(entry.OMPExecutable.Path, authority.ExecutableAncestors) ||
		!validExecutionPolicyAtomicAncestors(entry.OMPNativeAddon.Path, authority.NativeAddonAncestors) ||
		len(authority.ExecutableAncestors) == 0 || entry.OMPExecutableRoot != authority.ExecutableAncestors[len(authority.ExecutableAncestors)-1] {
		return authenticationError("execution policy atomic OMP runtime authority")
	}
	wrapper, err := freezeAuditWrapper(entry.Wrapper)
	if err != nil {
		return err
	}
	defer zeroBytes(wrapper)
	if hashJournalBytes(wrapper) != authority.WrapperCompatibilityOracleSHA256 ||
		authority.WrapperCompatibilityOracleSHA256 != entry.Wrapper.SHA256 {
		return authenticationError("execution policy wrapper compatibility-oracle binding")
	}
	return nil
}

func validExecutionPolicyAtomicAncestors(artifact string, ancestors []executionPolicyDirectoryIdentity) bool {
	paths, ok := atomicRuntimeAncestorPaths(artifact)
	if !ok || len(paths) != len(ancestors) {
		return false
	}
	for index, path := range paths {
		identity := ancestors[index]
		if identity.Path != path || !validStatDecimal(identity.Device) || !validStatDecimal(identity.Inode) || identity.Mode > 0o777 {
			return false
		}
	}
	return true
}

func admitAtomicOMPRuntimeAuthority(policy *executionPolicy, verifier atomicRuntimeAuthorityVerifier) error {
	if policy == nil || verifier == nil || len(policy.entries) == 0 {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding)
	}
	launchHashes := make([]string, 0, len(policy.entries))
	for launchHash := range policy.entries {
		launchHashes = append(launchHashes, launchHash)
	}
	sort.Strings(launchHashes)
	for _, launchHash := range launchHashes {
		entry := policy.entries[launchHash]
		wrapper, err := freezeAuditWrapper(entry.Wrapper)
		if err != nil {
			return err
		}
		lease, verifyErr := verifier.Verify(entry, wrapper)
		zeroBytes(wrapper)
		if verifyErr != nil {
			if lease != nil {
				_ = lease.Close()
			}
			return verifyErr
		}
		if lease == nil {
			return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding)
		}
		if err := lease.Close(); err != nil {
			return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryIdentityChanged)
		}
	}
	policy.atomicRuntimeAuthorityVerifier = verifier
	return nil
}

func runtimeAuthorityVerifier(policy *executionPolicy) atomicRuntimeAuthorityVerifier {
	if policy != nil && policy.atomicRuntimeAuthorityVerifier != nil {
		return policy.atomicRuntimeAuthorityVerifier
	}
	return productionAtomicRuntimeAuthorityVerifier()
}

type atomicRuntimeAuthorityDependencies struct {
	Open      func(string, int, uint32) (int, error)
	Openat    func(int, string, int, uint32) (int, error)
	Fstat     func(int, *unix.Stat_t) error
	Faccessat func(int, string, uint32, int) error
	Close     func(int) error
}

func systemAtomicRuntimeAuthorityDependencies() atomicRuntimeAuthorityDependencies {
	return atomicRuntimeAuthorityDependencies{
		Open: unix.Open, Openat: unix.Openat, Fstat: unix.Fstat, Faccessat: unix.Faccessat, Close: unix.Close,
	}
}

type atomicRuntimeAuthorityVerifier interface {
	Verify(executionPolicyEntry, []byte) (*atomicRuntimeAuthorityLease, error)
}

type atomicRuntimeAuthorityVerifierFunc func(executionPolicyEntry, []byte) (*atomicRuntimeAuthorityLease, error)

func (verifier atomicRuntimeAuthorityVerifierFunc) Verify(entry executionPolicyEntry, wrapper []byte) (*atomicRuntimeAuthorityLease, error) {
	if verifier == nil {
		return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding)
	}
	return verifier(entry, wrapper)
}

func productionAtomicRuntimeAuthorityVerifier() atomicRuntimeAuthorityVerifier {
	return atomicRuntimeAuthorityVerifierFunc(func(entry executionPolicyEntry, wrapper []byte) (*atomicRuntimeAuthorityLease, error) {
		return verifyAtomicOMPRuntimeAuthority(entry, wrapper, systemAtomicRuntimeAuthorityDependencies())
	})
}

type atomicRuntimeAuthorityLease struct {
	mu    sync.Mutex
	files []*os.File
}

func (lease *atomicRuntimeAuthorityLease) descriptorNumbers() []int {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	descriptors := make([]int, 0, len(lease.files))
	for _, file := range lease.files {
		if file != nil {
			descriptors = append(descriptors, int(file.Fd()))
		}
	}
	return descriptors
}

func (lease *atomicRuntimeAuthorityLease) Close() error {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	var first error
	for index := len(lease.files) - 1; index >= 0; index-- {
		if lease.files[index] == nil {
			continue
		}
		if err := lease.files[index].Close(); err != nil && first == nil {
			first = err
		}
		lease.files[index] = nil
	}
	lease.files = nil
	return first
}

func verifyAtomicOMPRuntimeAuthority(
	entry executionPolicyEntry,
	wrapper []byte,
	dependencies atomicRuntimeAuthorityDependencies,
) (*atomicRuntimeAuthorityLease, error) {
	if dependencies.Open == nil || dependencies.Openat == nil || dependencies.Fstat == nil || dependencies.Faccessat == nil || dependencies.Close == nil {
		return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding)
	}
	authority := entry.OMPRuntimeAuthority
	sealed, sealErr := sealExecutionPolicyOMPRuntimeAuthority(authority, entry.OMPExecutable, entry.OMPNativeAddon)
	if sealErr != nil || authority.SchemaVersion != atomicOMPRuntimeAuthoritySchemaVersion ||
		authority.AuthorityPolicyVersion != atomicOMPRuntimeAuthorityPolicyVersion || authority.TrustedOwnerUID != 0 ||
		authority.ArtifactFDPolicy != atomicOMPRuntimeArtifactFDPolicyParentRetainedCLOEXEC ||
		authority.ProcessGroupPolicy != atomicOMPProcessGroupPolicySingleGroup || authority.LauncherMode != atomicOMPLauncherModeDirectPinned ||
		authority.OMPArgvPolicy != atomicOMPArgvPolicyExactSudoRoute || authority.SandboxTargetPolicy != atomicOMPSandboxTargetPolicyExactPinned ||
		authority.OutputTransport != atomicOMPOutputTransportSupervisorStdout || authority.TimeoutOwner != atomicOMPTimeoutOwnerSupervisor ||
		sealed.AuthorityHash != authority.AuthorityHash || !protocolHashPattern.MatchString(authority.AuthorityHash) {
		return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding)
	}
	if len(wrapper) == 0 || hashJournalBytes(wrapper) != authority.WrapperCompatibilityOracleSHA256 ||
		authority.WrapperCompatibilityOracleSHA256 != entry.Wrapper.SHA256 {
		return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryHashMismatch)
	}
	if !validAtomicNativeLayout(entry, authority) {
		return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryNative, AtomicRuntimeBoundaryUnsupportedLayout)
	}
	if !validDeniedNativeFallbackRoots(authority.NativeDataRoot, authority.DeniedNativeFallbackRoots) {
		return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryNative, AtomicRuntimeBoundaryAuthorityBinding)
	}
	lease := &atomicRuntimeAuthorityLease{}
	closeFailure := func(err error) (*atomicRuntimeAuthorityLease, error) {
		_ = lease.Close()
		return nil, err
	}
	if err := openAtomicRuntimeArtifact(
		entry.OMPExecutable, authority.ExecutableAncestors, AtomicRuntimeBoundaryExecutable, true,
		maxAuditOMPExecutableBytes, dependencies, lease,
	); err != nil {
		return closeFailure(err)
	}
	if err := openAtomicRuntimeArtifact(
		entry.OMPNativeAddon, authority.NativeAddonAncestors, AtomicRuntimeBoundaryNative, false,
		maxAuditOMPNativeAddonBytes, dependencies, lease,
	); err != nil {
		return closeFailure(err)
	}
	return lease, nil
}

func validAtomicNativeLayout(entry executionPolicyEntry, authority executionPolicyOMPRuntimeAuthority) bool {
	root := authority.NativeDataRoot
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.IndexByte(root, 0) >= 0 ||
		entry.OMPVersion != supportedOMPVersion {
		return false
	}
	expected := filepath.Join(root, "omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
	return entry.OMPNativeAddon.Path == expected
}

func validDeniedNativeFallbackRoots(nativeDataRoot string, roots []string) bool {
	if len(roots) < 2 {
		return false
	}
	seen := make(map[string]struct{}, len(roots))
	selectedRoot := filepath.Join(nativeDataRoot, "omp")
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.IndexByte(root, 0) >= 0 ||
			pathsOverlap(root, selectedRoot) {
			return false
		}
		if _, duplicate := seen[root]; duplicate {
			return false
		}
		seen[root] = struct{}{}
	}
	return true
}

func openAtomicRuntimeArtifact(
	identity executionPolicyFileIdentity,
	ancestors []executionPolicyDirectoryIdentity,
	component AtomicRuntimeBoundaryComponent,
	requireExecutable bool,
	limit int64,
	dependencies atomicRuntimeAuthorityDependencies,
	lease *atomicRuntimeAuthorityLease,
) error {
	expectedPaths, ok := atomicRuntimeAncestorPaths(identity.Path)
	if !ok || len(expectedPaths) != len(ancestors) {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryInvalidPath)
	}
	for index, expected := range expectedPaths {
		if ancestors[index].Path != expected {
			return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryAuthorityBinding)
		}
	}

	directoryFD, err := dependencies.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, classifyAtomicRuntimeOpenReason(err))
	}
	if err := retainAtomicRuntimeDescriptor(directoryFD, "/", lease, dependencies); err != nil {
		return err
	}
	if err := validateAtomicRuntimeDirectory(directoryFD, ancestors[0], dependencies); err != nil {
		return err
	}
	for index := 1; index < len(expectedPaths); index++ {
		name := filepath.Base(expectedPaths[index])
		nextFD, openErr := dependencies.Openat(directoryFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, classifyAtomicRuntimeOpenReason(openErr))
		}
		if err := retainAtomicRuntimeDescriptor(nextFD, expectedPaths[index], lease, dependencies); err != nil {
			return err
		}
		directoryFD = nextFD
		if err := validateAtomicRuntimeDirectory(directoryFD, ancestors[index], dependencies); err != nil {
			return err
		}
	}

	artifactFD, err := dependencies.Openat(directoryFD, filepath.Base(identity.Path), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return unsupportedAtomicRuntimeBoundary(component, classifyAtomicRuntimeOpenReason(err))
	}
	if err := retainAtomicRuntimeDescriptor(artifactFD, identity.Path, lease, dependencies); err != nil {
		return err
	}
	if err := validateAtomicRuntimeArtifact(artifactFD, directoryFD, identity, component, requireExecutable, limit, dependencies, lease); err != nil {
		return err
	}
	return nil
}

func atomicRuntimeAncestorPaths(artifact string) ([]string, bool) {
	if artifact == "" || !filepath.IsAbs(artifact) || filepath.Clean(artifact) != artifact || strings.IndexByte(artifact, 0) >= 0 || artifact == "/" {
		return nil, false
	}
	parent := filepath.Dir(artifact)
	paths := []string{"/"}
	if parent == "/" {
		return paths, true
	}
	current := ""
	for _, component := range strings.Split(strings.TrimPrefix(parent, "/"), "/") {
		if component == "" || component == "." || component == ".." {
			return nil, false
		}
		current += "/" + component
		paths = append(paths, current)
	}
	return paths, true
}

func retainAtomicRuntimeDescriptor(
	descriptor int,
	name string,
	lease *atomicRuntimeAuthorityLease,
	dependencies atomicRuntimeAuthorityDependencies,
) error {
	file := os.NewFile(uintptr(descriptor), name)
	if file == nil {
		_ = dependencies.Close(descriptor)
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryAuthorityBinding)
	}
	lease.files = append(lease.files, file)
	return nil
}

func validateAtomicRuntimeDirectory(
	descriptor int,
	expected executionPolicyDirectoryIdentity,
	dependencies atomicRuntimeAuthorityDependencies,
) error {
	var status unix.Stat_t
	if err := dependencies.Fstat(descriptor, &status); err != nil {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryIdentityChanged)
	}
	if status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryWrongType)
	}
	if status.Uid != 0 || expected.OwnerUID != 0 {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryWrongOwner)
	}
	if expected.Mode != uint32(status.Mode&0o777) || expected.Device != statDecimal(uint64(status.Dev)) || expected.Inode != statDecimal(status.Ino) {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryIdentityChanged)
	}
	if err := dependencies.Faccessat(descriptor, ".", unix.W_OK, unix.AT_EACCESS|unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryEffectiveWrite)
	} else if !atomicRuntimeWriteDenied(err) {
		return unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryAncestor, AtomicRuntimeBoundaryIdentityChanged)
	}
	return nil
}

func validateAtomicRuntimeArtifact(
	descriptor int,
	parentFD int,
	expected executionPolicyFileIdentity,
	component AtomicRuntimeBoundaryComponent,
	requireExecutable bool,
	limit int64,
	dependencies atomicRuntimeAuthorityDependencies,
	lease *atomicRuntimeAuthorityLease,
) error {
	var before unix.Stat_t
	if err := dependencies.Fstat(descriptor, &before); err != nil {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryIdentityChanged)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryWrongType)
	}
	if before.Uid != 0 || expected.OwnerUID != 0 {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryWrongOwner)
	}
	mode := uint32(before.Mode & 0o777)
	if mode&0o222 != 0 {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryArtifactWriteBits)
	}
	if requireExecutable && mode&0o111 == 0 {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryWrongMode)
	}
	if expected.Mode != mode || expected.Device != statDecimal(uint64(before.Dev)) || expected.Inode != statDecimal(before.Ino) || expected.Size != before.Size {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryIdentityChanged)
	}
	if before.Size < 1 || before.Size > limit || !protocolHashPattern.MatchString(expected.SHA256) {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryAuthorityBinding)
	}
	if err := dependencies.Faccessat(parentFD, filepath.Base(expected.Path), unix.W_OK, unix.AT_EACCESS|unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryEffectiveWrite)
	} else if !atomicRuntimeWriteDenied(err) {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryIdentityChanged)
	}
	file := lease.files[len(lease.files)-1]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryIdentityChanged)
	}
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(file, limit+1))
	if err != nil || read != before.Size || read > limit || "sha256:"+hex.EncodeToString(digest.Sum(nil)) != expected.SHA256 {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryHashMismatch)
	}
	var after unix.Stat_t
	if err := dependencies.Fstat(descriptor, &after); err != nil || !sameAtomicRuntimeStat(before, after) {
		return unsupportedAtomicRuntimeBoundary(component, AtomicRuntimeBoundaryIdentityChanged)
	}
	return nil
}

func sameAtomicRuntimeStat(left, right unix.Stat_t) bool {
	return uint64(left.Dev) == uint64(right.Dev) && left.Ino == right.Ino && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Mode == right.Mode && left.Size == right.Size
}

func classifyAtomicRuntimeOpenReason(err error) AtomicRuntimeBoundaryReason {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return AtomicRuntimeBoundarySymlink
	}
	return AtomicRuntimeBoundaryIdentityChanged
}

func atomicRuntimeWriteDenied(err error) bool {
	return errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EROFS)
}
