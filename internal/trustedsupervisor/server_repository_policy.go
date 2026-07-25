package trustedsupervisor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	repositoryPolicySchemaVersion       = "ananke.local-trusted-supervisor-repository-policy.v1"
	repositoryPolicyEntrySchemaVersion  = "ananke.local-trusted-supervisor-repository-policy-entry.v1"
	repositoryIdentityHashSchemaVersion = "ananke.local-trusted-supervisor-repository-identity.v1"
	maxRepositoryPolicyBytes            = 256 * 1024
	maxRepositoryPolicyEntries          = 4096
)

type repositoryPolicyFile struct {
	SchemaVersion string                  `json:"schema_version"`
	Repositories  []repositoryPolicyEntry `json:"repositories"`
}

type repositoryPolicyEntry struct {
	SchemaVersion          string `json:"schema_version"`
	RepositoryIdentityHash string `json:"repository_identity_hash"`
	RepositoryIdentity     string `json:"repository_identity"`
}

type repositoryPolicy struct {
	path     string
	device   uint64
	inode    uint64
	ownerUID uint32
	entries  map[string]string
}

func repositoryIdentityHash(identity string) string {
	hash, err := canonicalHash(map[string]any{
		"schema_version":      repositoryIdentityHashSchemaVersion,
		"repository_identity": identity,
	})
	if err != nil {
		panic("repository identity must be canonicalizable")
	}
	return hash
}

func loadRepositoryPolicy(path string, ownerUID uint32) (*repositoryPolicy, error) {
	if path == "" || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
		return nil, authenticationError("absolute repository policy path required")
	}
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 {
		return nil, authenticationError("repository policy type or mode")
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || beforeStat.Uid != ownerUID {
		return nil, authenticationError("repository policy owner")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, authenticationError("open repository policy")
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, authenticationError("open repository policy descriptor")
	}
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Uid != ownerUID ||
		opened.Mode&0o777 != 0o600 || uint64(opened.Dev) != uint64(beforeStat.Dev) || opened.Ino != beforeStat.Ino {
		return nil, authenticationError("repository policy replaced")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxRepositoryPolicyBytes+1))
	if err != nil {
		return nil, authenticationError("read repository policy")
	}
	defer zeroBytes(contents)
	if len(contents) == 0 || len(contents) > maxRepositoryPolicyBytes {
		return nil, fmt.Errorf("%w: repository policy size", ErrLimit)
	}
	policy := &repositoryPolicy{
		path: path, device: uint64(opened.Dev), inode: opened.Ino, ownerUID: ownerUID,
		entries: make(map[string]string),
	}
	if err := policy.validateIdentity(); err != nil {
		return nil, err
	}
	var document repositoryPolicyFile
	if err := decodeCanonical(contents, &document); err != nil {
		return nil, authenticationError("repository policy closed canonical schema")
	}
	if document.SchemaVersion != repositoryPolicySchemaVersion || len(document.Repositories) == 0 ||
		len(document.Repositories) > maxRepositoryPolicyEntries {
		return nil, authenticationError("repository policy binding")
	}
	for _, entry := range document.Repositories {
		if entry.SchemaVersion != repositoryPolicyEntrySchemaVersion || strings.TrimSpace(entry.RepositoryIdentity) == "" ||
			!protocolHashPattern.MatchString(entry.RepositoryIdentityHash) ||
			entry.RepositoryIdentityHash != repositoryIdentityHash(entry.RepositoryIdentity) {
			return nil, authenticationError("repository policy entry")
		}
		if _, duplicate := policy.entries[entry.RepositoryIdentityHash]; duplicate {
			return nil, authenticationError("duplicate repository policy hash")
		}
		policy.entries[entry.RepositoryIdentityHash] = entry.RepositoryIdentity
	}
	return policy, nil
}

func (policy *repositoryPolicy) Resolve(repositoryHash string) (string, error) {
	if policy == nil || !protocolHashPattern.MatchString(repositoryHash) {
		return "", authenticationError("repository policy lookup")
	}
	if err := policy.validateIdentity(); err != nil {
		return "", err
	}
	identity, found := policy.entries[repositoryHash]
	if !found || identity == "" {
		return "", authenticationError("repository identity is not allowed")
	}
	return identity, nil
}

func (policy *repositoryPolicy) validateIdentity() error {
	if policy == nil {
		return authenticationError("repository policy identity")
	}
	information, err := os.Lstat(policy.path)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() || information.Mode().Perm() != 0o600 {
		return authenticationError("repository policy replacement")
	}
	pathStat, ok := information.Sys().(*syscall.Stat_t)
	if !ok || pathStat.Uid != policy.ownerUID || uint64(pathStat.Dev) != policy.device || pathStat.Ino != policy.inode {
		return authenticationError("repository policy replacement")
	}
	return nil
}
