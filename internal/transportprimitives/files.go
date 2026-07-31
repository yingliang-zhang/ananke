package transportprimitives

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	// ErrFileSecurity is the sentinel for file-security violations
	// (wrong owner, mode, type, symlink, replacement, etc.).
	ErrFileSecurity = errors.New("file security violation")

	// ErrFileLimit is the sentinel for file-size limit violations.
	ErrFileLimit = errors.New("file size limit exceeded")
)

// ReadOwnerOnlyRegularFile reads a file that must be:
//   - an absolute path with no NUL bytes,
//   - a regular file (not a symlink) with mode 0600,
//   - owned by ownerUserID before and after open,
//   - the same inode/device after open (no replacement),
//   - non-empty and within limit bytes.
//
// The file is opened with O_RDONLY|O_CLOEXEC|O_NOFOLLOW.
func ReadOwnerOnlyRegularFile(path string, ownerUserID uint32, limit int64) ([]byte, error) {
	if path == "" || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 || limit <= 0 {
		return nil, fmt.Errorf("%w: absolute operator file path required", ErrFileSecurity)
	}
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%w: operator file type or mode", ErrFileSecurity)
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || beforeStat.Uid != ownerUserID {
		return nil, fmt.Errorf("%w: operator file owner", ErrFileSecurity)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: open operator file", ErrFileSecurity)
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: open operator file descriptor", ErrFileSecurity)
	}
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Uid != ownerUserID || opened.Mode&0o777 != 0o600 ||
		uint64(opened.Dev) != uint64(beforeStat.Dev) || opened.Ino != beforeStat.Ino {
		return nil, fmt.Errorf("%w: operator file replaced", ErrFileSecurity)
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		ZeroBytes(contents)
		return nil, fmt.Errorf("%w: read operator file", ErrFileSecurity)
	}
	if len(contents) == 0 || int64(len(contents)) > limit {
		ZeroBytes(contents)
		return nil, fmt.Errorf("%w: operator file size", ErrFileLimit)
	}
	after, err := os.Lstat(path)
	if err != nil || after.Mode()&os.ModeSymlink != 0 {
		ZeroBytes(contents)
		return nil, fmt.Errorf("%w: operator file replaced", ErrFileSecurity)
	}
	afterStat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || uint64(afterStat.Dev) != uint64(opened.Dev) || afterStat.Ino != opened.Ino || afterStat.Uid != ownerUserID || after.Mode().Perm() != 0o600 {
		ZeroBytes(contents)
		return nil, fmt.Errorf("%w: operator file replaced", ErrFileSecurity)
	}
	return contents, nil
}

// ZeroBytes overwrites every byte of value with zeros.
func ZeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
