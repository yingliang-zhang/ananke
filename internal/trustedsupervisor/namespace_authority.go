package trustedsupervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrUnsupportedInvocationNamespace = errors.New("trusted supervisor invocation namespace is unsupported")

type InvocationNamespaceReason string

const (
	InvocationNamespaceInvalidPath         InvocationNamespaceReason = "invalid_path"
	InvocationNamespaceSymlink             InvocationNamespaceReason = "symlink"
	InvocationNamespaceWrongOwner          InvocationNamespaceReason = "wrong_owner"
	InvocationNamespaceWrongType           InvocationNamespaceReason = "wrong_type"
	InvocationNamespaceWritable            InvocationNamespaceReason = "untrusted_write"
	InvocationNamespaceIdentityChanged     InvocationNamespaceReason = "identity_changed"
	InvocationNamespaceSupervisorPrivilege InvocationNamespaceReason = "supervisor_privilege"
	InvocationNamespaceCredentialBoundary  InvocationNamespaceReason = "credential_boundary"

	UnsupportedInvocationNamespaceFailureClass = "unsupported_invocation_namespace"
)

type InvocationNamespaceError struct {
	Reason       InvocationNamespaceReason
	FailureClass string
}

func (failure *InvocationNamespaceError) Error() string {
	return ErrUnsupportedInvocationNamespace.Error()
}
func (failure *InvocationNamespaceError) Unwrap() error { return ErrUnsupportedInvocationNamespace }

func unsupportedInvocationNamespace(reason InvocationNamespaceReason) error {
	switch reason {
	case InvocationNamespaceInvalidPath, InvocationNamespaceSymlink, InvocationNamespaceWrongOwner,
		InvocationNamespaceWrongType, InvocationNamespaceWritable, InvocationNamespaceIdentityChanged,
		InvocationNamespaceSupervisorPrivilege, InvocationNamespaceCredentialBoundary:
		return &InvocationNamespaceError{Reason: reason, FailureClass: UnsupportedInvocationNamespaceFailureClass}
	default:
		return ErrUnsupportedInvocationNamespace
	}
}

// auditNamespaceAuthority is the single authority for invocation-owned names.
// Production admission opens every absolute component without following links,
// requires a root-owned non-writable hierarchy and keeps only CLOEXEC directory
// descriptors. Later effects are relative to these descriptors, never to a
// newly opened absolute invocation-root path.
type auditNamespaceAuthority struct {
	mu         sync.Mutex
	roots      map[string]*auditNamespaceRoot
	credential *syscall.Credential
	stable     bool
	closed     bool
}

type auditNamespaceRoot struct {
	fd       int
	path     string
	identity auditNamespaceDirectoryIdentity
}

type auditNamespaceDirectoryIdentity struct {
	Device   uint64
	Inode    uint64
	OwnerUID uint32
	OwnerGID uint32
	Mode     uint32
}

type auditNamespaceAuthorityOptions struct {
	trustedOwnerUID  uint32
	runtimeUID       uint32
	runtimeGID       uint32
	emulateBoundary  bool
	testOnlyStable   bool
	requireRootOwner bool
}

type auditNamespaceLease struct {
	mu       sync.Mutex
	fd       int
	rootPath string
	parent   auditNamespaceDirectoryIdentity
	closed   bool
}

type auditNamespaceLeaseSet struct {
	mu     sync.Mutex
	leases map[string]*auditNamespaceLease
	closed bool
}

type auditOwnedRootIdentity struct {
	Role           string `json:"role"`
	Path           string `json:"path"`
	ParentPath     string `json:"parent_path"`
	Device         string `json:"device"`
	Inode          string `json:"inode"`
	OwnerUID       uint32 `json:"owner_uid"`
	OwnerGID       uint32 `json:"owner_gid"`
	Mode           uint32 `json:"mode"`
	ParentDevice   string `json:"parent_device"`
	ParentInode    string `json:"parent_inode"`
	ParentOwnerUID uint32 `json:"parent_owner_uid"`
	ParentOwnerGID uint32 `json:"parent_owner_gid"`
	ParentMode     uint32 `json:"parent_mode"`
	CleanupRoot    bool   `json:"cleanup_root"`
}

func productionAuditNamespaceAuthorityOptions(supervisorUID, runtimeUID, runtimeGID uint32) auditNamespaceAuthorityOptions {
	return auditNamespaceAuthorityOptions{
		trustedOwnerUID: supervisorUID, runtimeUID: runtimeUID, runtimeGID: runtimeGID, requireRootOwner: true,
	}
}

func openAuditNamespaceAuthority(paths []string, options auditNamespaceAuthorityOptions) (*auditNamespaceAuthority, error) {
	if len(paths) == 0 || options.runtimeUID == 0 || options.runtimeGID == 0 || options.runtimeUID == options.trustedOwnerUID ||
		!options.emulateBoundary && (options.trustedOwnerUID != 0 || os.Geteuid() != 0 || os.Getegid() != 0) {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceSupervisorPrivilege)
	}
	unique := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) || strings.IndexByte(path, 0) >= 0 {
			return nil, unsupportedInvocationNamespace(InvocationNamespaceInvalidPath)
		}
		unique[path] = struct{}{}
	}
	ordered := make([]string, 0, len(unique))
	for path := range unique {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	authority := &auditNamespaceAuthority{roots: make(map[string]*auditNamespaceRoot, len(ordered))}
	closeFailure := func(err error) (*auditNamespaceAuthority, error) {
		_ = authority.Close()
		return nil, err
	}
	for _, path := range ordered {
		fd, identity, err := openAuditNamespacePath(path, options)
		if err != nil {
			return closeFailure(err)
		}
		authority.roots[path] = &auditNamespaceRoot{fd: fd, path: path, identity: identity}
	}
	// testOnlyStable is reachable only through explicit in-package or tagged
	// test construction. Production becomes stable only after every opened
	// physical component passes owner, mode, and native ACL proof.
	if options.testOnlyStable || options.requireRootOwner {
		authority.stable = true
	}
	if !options.emulateBoundary {
		authority.credential = &syscall.Credential{Uid: options.runtimeUID, Gid: options.runtimeGID, Groups: []uint32{}, NoSetGroups: false}
	}
	return authority, nil
}

func openAuditNamespacePath(path string, options auditNamespaceAuthorityOptions) (int, auditNamespaceDirectoryIdentity, error) {
	openPath := path
	if options.emulateBoundary {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !filepath.IsAbs(resolved) || filepath.Clean(resolved) != resolved {
			return -1, auditNamespaceDirectoryIdentity{}, unsupportedInvocationNamespace(InvocationNamespaceInvalidPath)
		}
		openPath = resolved
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, auditNamespaceDirectoryIdentity{}, unsupportedInvocationNamespace(InvocationNamespaceInvalidPath)
	}
	var rootStatus unix.Stat_t
	if err := unix.Fstat(fd, &rootStatus); err != nil {
		return -1, auditNamespaceDirectoryIdentity{}, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	if err := validateAuditNamespaceDirectory(fd, rootStatus, options); err != nil {
		return -1, auditNamespaceDirectoryIdentity{}, err
	}
	closeFD := true
	defer func() {
		if closeFD {
			_ = unix.Close(fd)
		}
	}()
	components := strings.Split(strings.TrimPrefix(openPath, string(filepath.Separator)), string(filepath.Separator))
	var identity auditNamespaceDirectoryIdentity
	if len(components) == 0 {
		return -1, identity, unsupportedInvocationNamespace(InvocationNamespaceInvalidPath)
	}
	for _, component := range components {
		if !validAuditNamespaceComponent(component) {
			return -1, identity, unsupportedInvocationNamespace(InvocationNamespaceInvalidPath)
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			if errors.Is(openErr, unix.ELOOP) {
				return -1, identity, unsupportedInvocationNamespace(InvocationNamespaceSymlink)
			}
			return -1, identity, unsupportedInvocationNamespace(InvocationNamespaceInvalidPath)
		}
		_ = unix.Close(fd)
		fd = next
		var status unix.Stat_t
		if unix.Fstat(fd, &status) != nil {
			return -1, identity, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
		}
		identity = namespaceDirectoryIdentity(status)
		if err := validateAuditNamespaceDirectory(fd, status, options); err != nil {
			return -1, identity, err
		}
	}
	closeFD = false
	return fd, identity, nil
}

func validateAuditNamespaceDirectory(descriptor int, status unix.Stat_t, options auditNamespaceAuthorityOptions) error {
	if status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unsupportedInvocationNamespace(InvocationNamespaceWrongType)
	}
	if options.requireRootOwner && status.Uid != 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceWrongOwner)
	}
	// A root owner plus no group/other write bits excludes the runtime UID,
	// runtime GID, and every possible supplementary group from adding,
	// deleting, renaming, or writing directory entries through Unix modes.
	if options.requireRootOwner && status.Mode&0o022 != 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceWritable)
	}
	if options.requireRootOwner && status.Mode&0o001 == 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceCredentialBoundary)
	}
	if options.testOnlyStable {
		return nil
	}
	inspection, err := inspectAuditNamespaceACL(descriptor)
	return validateAuditNamespaceACLInspection(inspection, err)
}

func validAuditNamespaceComponent(component string) bool {
	return component != "" && component != "." && component != ".." && filepath.Base(component) == component && strings.IndexByte(component, 0) < 0
}

func namespaceDirectoryIdentity(status unix.Stat_t) auditNamespaceDirectoryIdentity {
	return auditNamespaceDirectoryIdentity{
		Device: uint64(status.Dev), Inode: status.Ino, OwnerUID: status.Uid, OwnerGID: status.Gid, Mode: uint32(status.Mode),
	}
}

func namespaceIdentityMatches(status unix.Stat_t, expected auditNamespaceDirectoryIdentity) bool {
	return status.Mode&unix.S_IFMT == unix.S_IFDIR && uint64(status.Dev) == expected.Device && status.Ino == expected.Inode &&
		status.Uid == expected.OwnerUID && status.Gid == expected.OwnerGID && uint32(status.Mode) == expected.Mode
}

func (authority *auditNamespaceAuthority) ValidateRoot(path string) error {
	if authority == nil {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	root := authority.roots[path]
	if authority.closed || root == nil || root.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	var status unix.Stat_t
	if err := unix.Fstat(root.fd, &status); err != nil || !namespaceIdentityMatches(status, root.identity) {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return nil
}

func (authority *auditNamespaceAuthority) stableParentProofHolds() bool {
	if authority == nil {
		return false
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	return !authority.closed && authority.stable
}

func (authority *auditNamespaceAuthority) Duplicate(path string) (*auditNamespaceLease, error) {
	if authority == nil {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	root := authority.roots[path]
	if authority.closed || root == nil || root.fd < 0 {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	var status unix.Stat_t
	if err := unix.Fstat(root.fd, &status); err != nil || !namespaceIdentityMatches(status, root.identity) {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	duplicated, err := unix.FcntlInt(uintptr(root.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return &auditNamespaceLease{fd: duplicated, rootPath: path, parent: root.identity}, nil
}

func (authority *auditNamespaceAuthority) ChildCredential() (*syscall.Credential, error) {
	if authority == nil {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceCredentialBoundary)
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	if authority.credential == nil {
		return nil, nil
	}
	credential := *authority.credential
	credential.Groups = append([]uint32(nil), authority.credential.Groups...)
	return &credential, nil
}

func (authority *auditNamespaceAuthority) Close() error {
	if authority == nil {
		return nil
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed {
		return nil
	}
	authority.closed = true
	var first error
	for _, root := range authority.roots {
		if root.fd >= 0 {
			if err := unix.Close(root.fd); err != nil && first == nil {
				first = err
			}
			root.fd = -1
		}
	}
	return first
}

func (authority *auditNamespaceAuthority) descriptorsForTest() []int {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	paths := make([]string, 0, len(authority.roots))
	for path := range authority.roots {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	descriptors := make([]int, 0, len(paths))
	for _, path := range paths {
		descriptors = append(descriptors, authority.roots[path].fd)
	}
	return descriptors
}

func newAuditNamespaceLeaseSet(authority *auditNamespaceAuthority, paths ...string) (*auditNamespaceLeaseSet, error) {
	if authority == nil || len(paths) == 0 {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	set := &auditNamespaceLeaseSet{leases: make(map[string]*auditNamespaceLease, len(paths))}
	for _, path := range paths {
		if _, duplicate := set.leases[path]; duplicate {
			_ = set.Close()
			return nil, authenticationError("duplicate audit namespace lease")
		}
		lease, err := authority.Duplicate(path)
		if err != nil {
			_ = set.Close()
			return nil, err
		}
		set.leases[path] = lease
	}
	return set, nil
}

func (set *auditNamespaceLeaseSet) lease(path string) (*auditNamespaceLease, error) {
	if set == nil {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	lease := set.leases[path]
	if set.closed || lease == nil {
		return nil, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return lease, nil
}

func (set *auditNamespaceLeaseSet) Close() error {
	if set == nil {
		return nil
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	if set.closed {
		return nil
	}
	set.closed = true
	var first error
	for _, lease := range set.leases {
		if err := lease.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (lease *auditNamespaceLease) descriptorForTest() int {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.fd
}

func (lease *auditNamespaceLease) Mkdir(name string, mode uint32) error {
	if lease == nil || !validAuditNamespaceComponent(name) || mode != 0o700 {
		return authenticationError("audit namespace mkdir authority")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	if err := unix.Mkdirat(lease.fd, name, mode); err != nil {
		return fmt.Errorf("create isolated audit root: %w", err)
	}
	return nil
}

func (lease *auditNamespaceLease) Chown(name string, uid, gid uint32) error {
	if lease == nil || !validAuditNamespaceComponent(name) {
		return authenticationError("audit namespace chown authority")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return unix.Fchownat(lease.fd, name, int(uid), int(gid), unix.AT_SYMLINK_NOFOLLOW)
}

func (lease *auditNamespaceLease) Open(name string, flags int, mode uint32) (int, error) {
	if lease == nil || !validAuditNamespaceComponent(name) {
		return -1, authenticationError("audit namespace open authority")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return -1, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	descriptor, err := unix.Openat(lease.fd, name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, mode)
	if err != nil {
		return -1, err
	}
	return descriptor, nil
}

func (lease *auditNamespaceLease) RequireAbsent(name string) error {
	if lease == nil || !validAuditNamespaceComponent(name) {
		return authenticationError("audit namespace absence authority")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	var status unix.Stat_t
	if err := unix.Fstatat(lease.fd, name, &status, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return authenticationError("inspect audit namespace child")
	}
	return authenticationError("audit namespace child collision")
}

func (lease *auditNamespaceLease) Rename(from, to string) error {
	if lease == nil || !validAuditNamespaceComponent(from) || !validAuditNamespaceComponent(to) {
		return authenticationError("audit namespace rename authority")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return unix.Renameat(lease.fd, from, lease.fd, to)
}

func (lease *auditNamespaceLease) Chmod(name string, mode uint32) error {
	if lease == nil || !validAuditNamespaceComponent(name) {
		return authenticationError("audit namespace chmod authority")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return unix.Fchmodat(lease.fd, name, mode, unix.AT_SYMLINK_NOFOLLOW)
}

func (lease *auditNamespaceLease) Sync() error {
	if lease == nil {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return unix.Fsync(lease.fd)
}

func (lease *auditNamespaceLease) RemoveTree(name string) error {
	if lease == nil || !validAuditNamespaceComponent(name) {
		return authenticationError("audit namespace removal authority")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return scrubAndRemoveAuditTreeAtIdentity(lease.fd, name, make([]byte, 32*1024), nil)
}

func (lease *auditNamespaceLease) Capture(name, role string, cleanupRoot bool) (auditOwnedRootIdentity, error) {
	if lease == nil || !validAuditNamespaceComponent(name) || !executionValuePattern.MatchString(role) {
		return auditOwnedRootIdentity{}, authenticationError("audit owned root identity")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed || lease.fd < 0 {
		return auditOwnedRootIdentity{}, unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
	}
	return captureAuditOwnedRootAt(lease.fd, lease.rootPath, name, role, cleanupRoot, lease.parent)
}

func (authority *auditNamespaceAuthority) prepareRuntimeOwnedChild(lease *auditNamespaceLease, name string) error {
	credential, err := authority.ChildCredential()
	if err != nil || credential == nil {
		return err
	}
	return lease.Chown(name, credential.Uid, credential.Gid)
}

func auditOwnedRootLineage(parent auditOwnedRootIdentity, ancestors ...auditOwnedRootIdentity) (map[string]auditOwnedRootIdentity, error) {
	byPath := make(map[string]auditOwnedRootIdentity, len(ancestors)+1)
	for _, identity := range append([]auditOwnedRootIdentity{parent}, ancestors...) {
		if !validAuditOwnedRootIdentity(identity) {
			return nil, authenticationError("audit owned root lineage identity")
		}
		if _, duplicate := byPath[identity.Path]; duplicate {
			return nil, authenticationError("audit owned root lineage duplicate")
		}
		byPath[identity.Path] = identity
	}
	return byPath, nil
}

func (authority *auditNamespaceAuthority) mkdirAndCaptureOwnedChild(parent auditOwnedRootIdentity, name, role string, cleanupRoot, runtimeOwned bool, ancestors ...auditOwnedRootIdentity) (auditOwnedRootIdentity, error) {
	byPath, err := auditOwnedRootLineage(parent, ancestors...)
	if err != nil {
		return auditOwnedRootIdentity{}, err
	}
	descriptor, present, err := authority.openAuthenticatedOwnedRoot(parent, byPath)
	if err != nil || !present {
		if err != nil {
			return auditOwnedRootIdentity{}, err
		}
		return auditOwnedRootIdentity{}, authenticationError("audit owned parent absent")
	}
	defer unix.Close(descriptor)
	if !validAuditNamespaceComponent(name) || !executionValuePattern.MatchString(role) {
		return auditOwnedRootIdentity{}, authenticationError("audit owned child identity")
	}
	if err := unix.Mkdirat(descriptor, name, 0o700); err != nil {
		return auditOwnedRootIdentity{}, err
	}
	if runtimeOwned {
		if credential, credentialErr := authority.ChildCredential(); credentialErr != nil {
			return auditOwnedRootIdentity{}, credentialErr
		} else if credential != nil {
			if err := unix.Fchownat(descriptor, name, int(credential.Uid), int(credential.Gid), unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return auditOwnedRootIdentity{}, err
			}
		}
	}
	return captureAuditOwnedRootAt(descriptor, parent.Path, name, role, cleanupRoot, namespaceDirectoryIdentityFromOwned(parent))
}

func (authority *auditNamespaceAuthority) captureOwnedChild(parent auditOwnedRootIdentity, name, role string, cleanupRoot bool, ancestors ...auditOwnedRootIdentity) (auditOwnedRootIdentity, error) {
	byPath, err := auditOwnedRootLineage(parent, ancestors...)
	if err != nil {
		return auditOwnedRootIdentity{}, err
	}
	descriptor, present, err := authority.openAuthenticatedOwnedRoot(parent, byPath)
	if err != nil || !present {
		if err != nil {
			return auditOwnedRootIdentity{}, err
		}
		return auditOwnedRootIdentity{}, authenticationError("audit owned parent absent")
	}
	defer unix.Close(descriptor)
	return captureAuditOwnedRootAt(descriptor, parent.Path, name, role, cleanupRoot, namespaceDirectoryIdentityFromOwned(parent))
}

func (authority *auditNamespaceAuthority) sealAndRecaptureOwnedDirectory(directory auditOwnedRootIdentity, mode uint32, ancestors ...auditOwnedRootIdentity) (auditOwnedRootIdentity, error) {
	if mode != 0o500 {
		return auditOwnedRootIdentity{}, authenticationError("sealed audit owned directory mode")
	}
	byPath, err := auditOwnedRootLineage(directory, ancestors...)
	if err != nil {
		return auditOwnedRootIdentity{}, err
	}
	descriptor, present, err := authority.openAuthenticatedOwnedRoot(directory, byPath)
	if err != nil || !present {
		if err != nil {
			return auditOwnedRootIdentity{}, err
		}
		return auditOwnedRootIdentity{}, authenticationError("sealed audit owned directory absent")
	}
	defer unix.Close(descriptor)
	var before unix.Stat_t
	if err := unix.Fstat(descriptor, &before); err != nil || !auditOwnedRootStatMatches(before, directory) {
		return auditOwnedRootIdentity{}, authenticationError("sealed audit owned directory descriptor")
	}
	if err := unix.Fchmod(descriptor, mode); err != nil || unix.Fsync(descriptor) != nil {
		return auditOwnedRootIdentity{}, authenticationError("seal audit owned directory")
	}
	var after unix.Stat_t
	if err := unix.Fstat(descriptor, &after); err != nil || uint64(after.Dev) != uint64(before.Dev) || after.Ino != before.Ino ||
		after.Uid != before.Uid || after.Gid != before.Gid || after.Mode&unix.S_IFMT != unix.S_IFDIR || uint32(after.Mode)&0o777 != mode {
		return auditOwnedRootIdentity{}, authenticationError("sealed audit owned directory changed")
	}
	parent, err := authority.openAuthenticatedParent(directory, byPath)
	if err != nil {
		return auditOwnedRootIdentity{}, err
	}
	defer unix.Close(parent)
	parentIdentity, ok := byPath[directory.ParentPath]
	if !ok {
		return auditOwnedRootIdentity{}, authenticationError("sealed audit owned directory parent binding")
	}
	recaptured, err := captureAuditOwnedRootAt(parent, directory.ParentPath, filepath.Base(directory.Path), directory.Role, directory.CleanupRoot, namespaceDirectoryIdentityFromOwned(parentIdentity))
	if err != nil || recaptured.Device != directory.Device || recaptured.Inode != directory.Inode || recaptured.OwnerUID != directory.OwnerUID || recaptured.OwnerGID != directory.OwnerGID || recaptured.Mode&0o777 != mode {
		return auditOwnedRootIdentity{}, authenticationError("recapture sealed audit owned directory")
	}
	return recaptured, nil
}

func (authority *auditNamespaceAuthority) writeOwnedFile(parent auditOwnedRootIdentity, name string, contents []byte, mode uint32, ancestors ...auditOwnedRootIdentity) error {
	byPath := make(map[string]auditOwnedRootIdentity, len(ancestors)+1)
	byPath[parent.Path] = parent
	for _, ancestor := range ancestors {
		if !validAuditOwnedRootIdentity(ancestor) {
			return authenticationError("audit owned file ancestor identity")
		}
		if _, duplicate := byPath[ancestor.Path]; duplicate {
			return authenticationError("duplicate audit owned file ancestor identity")
		}
		byPath[ancestor.Path] = ancestor
	}
	directory, present, err := authority.openAuthenticatedOwnedRoot(parent, byPath)
	if err != nil || !present {
		if err != nil {
			return err
		}
		return authenticationError("audit owned file parent absent")
	}
	defer unix.Close(directory)
	if !validAuditNamespaceComponent(name) || mode&^0o777 != 0 {
		return authenticationError("audit owned file name or mode")
	}
	descriptor, err := unix.Openat(directory, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(descriptor), name)
	if file == nil {
		_ = unix.Close(descriptor)
		return authenticationError("audit owned file descriptor")
	}
	if credential, credentialErr := authority.ChildCredential(); credentialErr != nil {
		_ = file.Close()
		return credentialErr
	} else if credential != nil {
		if err := unix.Fchown(descriptor, int(credential.Uid), int(credential.Gid)); err != nil {
			_ = file.Close()
			return err
		}
	}
	written, writeErr := file.Write(contents)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || written != len(contents) || syncErr != nil || closeErr != nil {
		return authenticationError("write audit owned file")
	}
	return nil
}

func namespaceDirectoryIdentityFromOwned(identity auditOwnedRootIdentity) auditNamespaceDirectoryIdentity {
	device, deviceErr := strconv.ParseUint(identity.Device, 10, 64)
	inode, inodeErr := strconv.ParseUint(identity.Inode, 10, 64)
	if deviceErr != nil || inodeErr != nil || statDecimal(device) != identity.Device || statDecimal(inode) != identity.Inode {
		panic("validated audit stat decimal became invalid")
	}
	return auditNamespaceDirectoryIdentity{
		Device: device, Inode: inode, OwnerUID: identity.OwnerUID, OwnerGID: identity.OwnerGID, Mode: identity.Mode,
	}
}

func (lease *auditNamespaceLease) Close() error {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed {
		return nil
	}
	lease.closed = true
	if lease.fd < 0 {
		return nil
	}
	err := unix.Close(lease.fd)
	lease.fd = -1
	return err
}

func captureAuditOwnedRootAt(parentFD int, parentPath, name, role string, cleanupRoot bool, parent auditNamespaceDirectoryIdentity) (auditOwnedRootIdentity, error) {
	var status unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &status, unix.AT_SYMLINK_NOFOLLOW); err != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return auditOwnedRootIdentity{}, authenticationError("capture audit owned root identity")
	}
	descriptor, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return auditOwnedRootIdentity{}, authenticationError("open audit owned root identity")
	}
	defer unix.Close(descriptor)
	var opened unix.Stat_t
	if err := unix.Fstat(descriptor, &opened); err != nil || uint64(opened.Dev) != uint64(status.Dev) || opened.Ino != status.Ino ||
		opened.Uid != status.Uid || opened.Gid != status.Gid || opened.Mode != status.Mode {
		return auditOwnedRootIdentity{}, authenticationError("audit owned root changed during capture")
	}
	return auditOwnedRootIdentity{
		Role: role, Path: filepath.Join(parentPath, name), ParentPath: parentPath,
		Device: statDecimal(uint64(status.Dev)), Inode: statDecimal(status.Ino), OwnerUID: status.Uid, OwnerGID: status.Gid, Mode: uint32(status.Mode),
		ParentDevice: statDecimal(parent.Device), ParentInode: statDecimal(parent.Inode), ParentOwnerUID: parent.OwnerUID,
		ParentOwnerGID: parent.OwnerGID, ParentMode: parent.Mode, CleanupRoot: cleanupRoot,
	}, nil
}

func validAuditOwnedRootIdentity(identity auditOwnedRootIdentity) bool {
	return executionValuePattern.MatchString(identity.Role) && validAuditPrivatePath(identity.Path) && validAuditPrivatePath(identity.ParentPath) &&
		filepath.Dir(identity.Path) == identity.ParentPath && validStatDecimal(identity.Device) && validStatDecimal(identity.Inode) &&
		validStatDecimal(identity.ParentDevice) && validStatDecimal(identity.ParentInode) && identity.Mode&unix.S_IFMT == unix.S_IFDIR &&
		identity.ParentMode&unix.S_IFMT == unix.S_IFDIR && identity.Mode&0o022 == 0 && identity.ParentMode&0o022 == 0
}

func validateExactAuditOwnedRootIdentities(actual, expected []auditOwnedRootIdentity) error {
	if len(actual) == 0 || len(actual) != len(expected) {
		return authenticationError("audit owned root identity set")
	}
	for index := range expected {
		if !validAuditOwnedRootIdentity(actual[index]) || actual[index] != expected[index] {
			return authenticationError("audit owned root identity order and binding")
		}
	}
	return nil
}

func scrubAndRemoveAuthenticatedAuditRoots(authority *auditNamespaceAuthority, identities []auditOwnedRootIdentity) error {
	if authority == nil || len(identities) == 0 {
		return authenticationError("authenticated audit cleanup roots")
	}
	byPath := make(map[string]auditOwnedRootIdentity, len(identities))
	for _, identity := range identities {
		if !validAuditOwnedRootIdentity(identity) {
			return authenticationError("authenticated audit cleanup identity")
		}
		if _, duplicate := byPath[identity.Path]; duplicate {
			return authenticationError("duplicate authenticated audit cleanup identity")
		}
		byPath[identity.Path] = identity
	}
	presentCleanupRoots := make(map[string]bool, len(identities))
	for _, identity := range identities {
		if !identity.CleanupRoot {
			continue
		}
		descriptor, present, err := authority.openAuthenticatedOwnedRoot(identity, byPath)
		if err != nil {
			return err
		}
		if present {
			_ = unix.Close(descriptor)
		}
		presentCleanupRoots[identity.Path] = present
	}
	for _, identity := range identities {
		if identity.CleanupRoot {
			continue
		}
		coveredByAbsentRoot := false
		for candidate := identity.ParentPath; candidate != filepath.Dir(candidate); candidate = filepath.Dir(candidate) {
			if present, ok := presentCleanupRoots[candidate]; ok {
				coveredByAbsentRoot = !present
				break
			}
		}
		if coveredByAbsentRoot {
			continue
		}
		descriptor, present, err := authority.openAuthenticatedOwnedRoot(identity, byPath)
		if err != nil {
			return err
		}
		if !present {
			return authenticationError("nested authenticated audit cleanup identity absent")
		}
		_ = unix.Close(descriptor)
	}
	zeros := make([]byte, 32*1024)
	for _, identity := range identities {
		if !identity.CleanupRoot || !presentCleanupRoots[identity.Path] {
			continue
		}
		parent, err := authority.openAuthenticatedParent(identity, byPath)
		if err != nil {
			return err
		}
		err = scrubAndRemoveAuditTreeAtIdentity(parent, filepath.Base(identity.Path), zeros, &identity)
		closeErr := unix.Close(parent)
		if err != nil {
			return err
		}
		if closeErr != nil {
			return authenticationError("close authenticated audit cleanup parent")
		}
	}
	return nil
}

func verifyAuthenticatedAuditCleanupRootsAbsent(authority *auditNamespaceAuthority, identities []auditOwnedRootIdentity) error {
	if authority == nil || len(identities) == 0 {
		return authenticationError("authenticated audit cleanup absence")
	}
	byPath := make(map[string]auditOwnedRootIdentity, len(identities))
	for _, identity := range identities {
		if !validAuditOwnedRootIdentity(identity) {
			return authenticationError("authenticated audit cleanup absence identity")
		}
		if _, duplicate := byPath[identity.Path]; duplicate {
			return authenticationError("duplicate authenticated audit cleanup absence identity")
		}
		byPath[identity.Path] = identity
	}
	for _, identity := range identities {
		if !identity.CleanupRoot {
			continue
		}
		descriptor, present, err := authority.openAuthenticatedOwnedRoot(identity, byPath)
		if err != nil {
			return err
		}
		if present {
			_ = unix.Close(descriptor)
			return authenticationError("authenticated audit cleanup root remains")
		}
	}
	return nil
}

func (authority *auditNamespaceAuthority) openAuthenticatedParent(identity auditOwnedRootIdentity, byPath map[string]auditOwnedRootIdentity) (int, error) {
	if parentIdentity, nested := byPath[identity.ParentPath]; nested {
		descriptor, present, err := authority.openAuthenticatedOwnedRoot(parentIdentity, byPath)
		if err != nil {
			return -1, err
		}
		if !present {
			return -1, authenticationError("authenticated cleanup parent absent")
		}
		var status unix.Stat_t
		if err := unix.Fstat(descriptor, &status); err != nil || !auditOwnedParentStatMatches(status, identity) {
			_ = unix.Close(descriptor)
			return -1, authenticationError("authenticated cleanup parent identity")
		}
		return descriptor, nil
	}
	lease, err := authority.Duplicate(identity.ParentPath)
	if err != nil {
		return -1, err
	}
	lease.mu.Lock()
	if lease.closed || lease.fd < 0 || identity.ParentDevice != statDecimal(lease.parent.Device) || identity.ParentInode != statDecimal(lease.parent.Inode) ||
		identity.ParentOwnerUID != lease.parent.OwnerUID || identity.ParentOwnerGID != lease.parent.OwnerGID || identity.ParentMode != lease.parent.Mode {
		lease.mu.Unlock()
		_ = lease.Close()
		return -1, authenticationError("authenticated cleanup retained parent identity")
	}
	descriptor := lease.fd
	lease.fd = -1
	lease.closed = true
	lease.mu.Unlock()
	return descriptor, nil
}

func (authority *auditNamespaceAuthority) openAuthenticatedOwnedRoot(identity auditOwnedRootIdentity, byPath map[string]auditOwnedRootIdentity) (int, bool, error) {
	parent, err := authority.openAuthenticatedParent(identity, byPath)
	if err != nil {
		return -1, false, err
	}
	defer unix.Close(parent)
	name := filepath.Base(identity.Path)
	var status unix.Stat_t
	if err := unix.Fstatat(parent, name, &status, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		if authority.stableParentProofHolds() {
			return -1, false, nil
		}
		return -1, false, authenticationError("owned audit cleanup identity absent without stable parent proof")
	} else if err != nil || !auditOwnedRootStatMatches(status, identity) {
		return -1, false, authenticationError("owned audit cleanup identity mismatch")
	}
	descriptor, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, false, authenticationError("open authenticated audit cleanup root")
	}
	var opened unix.Stat_t
	if err := unix.Fstat(descriptor, &opened); err != nil || !auditOwnedRootStatMatches(opened, identity) {
		_ = unix.Close(descriptor)
		return -1, false, authenticationError("authenticated audit cleanup root changed")
	}
	return descriptor, true, nil
}

func auditOwnedRootStatMatches(status unix.Stat_t, identity auditOwnedRootIdentity) bool {
	return status.Mode&unix.S_IFMT == unix.S_IFDIR && statDecimal(uint64(status.Dev)) == identity.Device && statDecimal(status.Ino) == identity.Inode &&
		status.Uid == identity.OwnerUID && status.Gid == identity.OwnerGID && uint32(status.Mode) == identity.Mode
}

func auditOwnedParentStatMatches(status unix.Stat_t, identity auditOwnedRootIdentity) bool {
	return status.Mode&unix.S_IFMT == unix.S_IFDIR && statDecimal(uint64(status.Dev)) == identity.ParentDevice && statDecimal(status.Ino) == identity.ParentInode &&
		status.Uid == identity.ParentOwnerUID && status.Gid == identity.ParentOwnerGID && uint32(status.Mode) == identity.ParentMode
}
