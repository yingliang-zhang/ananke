package releaseartifact

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type fileIdentity struct {
	device uint64
	inode  uint64
	size   int64
	mode   uint32
}

func identityFromStat(status *unix.Stat_t) fileIdentity {
	return fileIdentity{
		device: uint64(status.Dev),
		inode:  uint64(status.Ino),
		size:   status.Size,
		mode:   uint32(status.Mode),
	}
}

func descriptorIdentity(fd int) (fileIdentity, error) {
	var status unix.Stat_t
	if err := unix.Fstat(fd, &status); err != nil {
		return fileIdentity{}, err
	}
	return identityFromStat(&status), nil
}

func linkedIdentity(directoryFD int, name string) (fileIdentity, error) {
	var status unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &status, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fileIdentity{}, err
	}
	return identityFromStat(&status), nil
}

func (identity fileIdentity) isRegular() bool {
	return identity.mode&unix.S_IFMT == unix.S_IFREG
}

func (identity fileIdentity) isDirectory() bool {
	return identity.mode&unix.S_IFMT == unix.S_IFDIR
}

func (identity fileIdentity) sameObject(other fileIdentity) bool {
	return identity.device == other.device && identity.inode == other.inode
}

func (identity fileIdentity) sameRegularObject(other fileIdentity) bool {
	return identity.isRegular() && other.isRegular() && identity.sameObject(other) && identity.size == other.size
}

type pinnedDirectory struct {
	path     string
	fd       int
	identity fileIdentity
	closed   bool
}

func openPinnedDirectory(path string) (*pinnedDirectory, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("directory path must be absolute and clean")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open directory without following symlinks: %w", err)
	}
	identity, err := descriptorIdentity(fd)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("inspect opened directory descriptor: %w", err)
	}
	if !identity.isDirectory() {
		unix.Close(fd)
		return nil, fmt.Errorf("opened path is not a directory: %s", path)
	}
	return &pinnedDirectory{path: path, fd: fd, identity: identity}, nil
}

func (directory *pinnedDirectory) Close() error {
	if directory == nil || directory.closed {
		return nil
	}
	directory.closed = true
	return unix.Close(directory.fd)
}

func (directory *pinnedDirectory) validateBinding() error {
	if directory == nil || directory.closed {
		return errors.New("pinned directory is closed")
	}
	fd, err := unix.Open(directory.path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("reopen pinned directory without following symlinks: %w", err)
	}
	defer unix.Close(fd)
	current, err := descriptorIdentity(fd)
	if err != nil {
		return fmt.Errorf("inspect current directory binding: %w", err)
	}
	opened, err := descriptorIdentity(directory.fd)
	if err != nil {
		return fmt.Errorf("inspect retained directory descriptor: %w", err)
	}
	if !directory.identity.sameObject(opened) || !directory.identity.sameObject(current) || !current.isDirectory() {
		return fmt.Errorf("directory path binding changed after it was pinned: %s", directory.path)
	}
	return nil
}

func (directory *pinnedDirectory) sync() error {
	if directory == nil || directory.closed {
		return errors.New("pinned directory is closed")
	}
	return unix.Fsync(directory.fd)
}

func randomStagingName() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return releaseStagingPrefix + hex.EncodeToString(entropy[:]), nil
}

type stagedCandidate struct {
	parent   *pinnedDirectory
	name     string
	file     *os.File
	identity fileIdentity
	closed   bool
}

// createStagedCandidate creates and opens one hidden file in a single Openat
// operation. The returned descriptor is the ownership authority; no pathname
// opened after creation can be attributed to this invocation.
func createStagedCandidate(parent *pinnedDirectory, afterOpen func(string) error) (*stagedCandidate, error) {
	if err := parent.validateBinding(); err != nil {
		return nil, err
	}
	for range 32 {
		name, err := randomStagingName()
		if err != nil {
			return nil, fmt.Errorf("generate release candidate name: %w", err)
		}
		fd, err := unix.Openat(parent.fd, name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o700)
		if err != nil {
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			return nil, fmt.Errorf("create descriptor-relative release candidate: %w", err)
		}
		file := os.NewFile(uintptr(fd), name)
		hookErr := error(nil)
		if afterOpen != nil {
			hookErr = afterOpen(name)
		}
		identity, identityErr := descriptorIdentity(fd)
		candidate := &stagedCandidate{parent: parent, name: name, file: file, identity: identity}
		if identityErr != nil {
			file.Close()
			return nil, fmt.Errorf("inspect atomically created release candidate: %w", identityErr)
		}
		if !identity.isRegular() {
			candidate.Close()
			return nil, errors.New("release candidate descriptor is not a regular file")
		}
		if hookErr != nil {
			cleanupErr := candidate.cleanupName()
			candidate.Close()
			return nil, errors.Join(hookErr, cleanupErr)
		}
		if err := parent.sync(); err != nil {
			cleanupErr := candidate.cleanupName()
			candidate.Close()
			return nil, errors.Join(fmt.Errorf("sync release output directory after candidate creation: %w", err), cleanupErr)
		}
		return candidate, nil
	}
	return nil, errors.New("allocate unique release candidate")
}

func (candidate *stagedCandidate) Close() error {
	if candidate == nil || candidate.closed {
		return nil
	}
	candidate.closed = true
	return candidate.file.Close()
}

func (candidate *stagedCandidate) validateBinding(expected fileIdentity) error {
	linked, err := linkedIdentity(candidate.parent.fd, candidate.name)
	if err != nil {
		return fmt.Errorf("inspect linked release candidate: %w", err)
	}
	if !expected.sameRegularObject(linked) || !candidate.identity.sameObject(linked) {
		return errors.New("release candidate pathname no longer names the atomically created verified object")
	}
	return nil
}

func (candidate *stagedCandidate) cleanupName() error {
	if candidate == nil {
		return nil
	}
	linked, err := linkedIdentity(candidate.parent.fd, candidate.name)
	switch {
	case errors.Is(err, unix.ENOENT):
		return nil
	case err != nil:
		return fmt.Errorf("inspect direct candidate during cleanup: %w", err)
	case !candidate.identity.sameObject(linked):
		return errors.New("refuse to remove replacement at direct candidate pathname")
	default:
		if err := unix.Unlinkat(candidate.parent.fd, candidate.name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("remove atomically created direct candidate: %w", err)
		}
		if err := candidate.parent.sync(); err != nil {
			return fmt.Errorf("sync release output directory after candidate cleanup: %w", err)
		}
		return nil
	}
}
