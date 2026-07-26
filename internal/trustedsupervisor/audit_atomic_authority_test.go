package trustedsupervisor

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinInstalledOMPDescriptorExecutionProbeIsUnsupported(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin /dev/fd executable authority probe")
	}
	executable, _ := installedOMPFixturesForAtomicProbe(t)
	candidate := filepath.Join(t.TempDir(), "omp")
	linkOrCopyFileForAtomicProbe(t, executable, candidate)
	identity := fileIdentityForTest(t, candidate)

	fd, err := unix.Open(candidate, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	opened := os.NewFile(uintptr(fd), "verified-omp")
	if opened == nil {
		_ = unix.Close(fd)
		t.Fatal("opened OMP descriptor is nil")
	}
	defer opened.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || !auditWrapperStatMatches(stat, identity) {
		t.Fatalf("opened OMP identity = %+v, %v; want %+v", stat, err, identity)
	}

	pinnedPath := candidate + ".pinned"
	if err := os.Rename(candidate, pinnedPath); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "replacement-executed")
	replacement := "#!/bin/sh\nprintf replacement > " + shellSingleQuoteForAtomicProbe(marker) + "\nexit 91\n"
	if err := os.WriteFile(candidate, []byte(replacement), 0o700); err != nil {
		t.Fatal(err)
	}

	frozenWrapper := []byte("set -eu\n/dev/fd/3 --version\n")
	command := exec.Command(auditBashExecutable, "-s", "--")
	command.Stdin = bytes.NewReader(frozenWrapper)
	command.ExtraFiles = []*os.File{opened}
	command.Env = []string{"HOME=" + t.TempDir(), "LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin", "TZ=UTC"}
	output, err := command.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("/dev/fd/3: Permission denied")) {
		t.Fatalf("Darwin descriptor execution boundary = %q, %v; want EACCES", output, err)
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("replacement executable ran through frozen stream: %v", err)
	}
	if hashJournalBytes(frozenWrapper) != hashJournalBytes([]byte("set -eu\n/dev/fd/3 --version\n")) {
		t.Fatal("frozen wrapper bytes drifted")
	}
}

func TestDarwinInstalledOMPLoaderDoesNotSelectInheritedNativeDescriptor(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin inherited native descriptor probe")
	}
	executable, nativeAddon := installedOMPFixturesForAtomicProbe(t)
	nativeFile := openNoFollowForAtomicProbe(t, nativeAddon)
	defer nativeFile.Close()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	xdg := filepath.Join(root, "xdg")
	mutableNative := filepath.Join(xdg, "omp", "natives", supportedOMPVersion, auditOMPNativeAddonFilename)
	if err := os.MkdirAll(filepath.Dir(mutableNative), 0o700); err != nil {
		t.Fatal(err)
	}
	nativeInformation, err := nativeFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := os.OpenFile(mutableNative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Truncate(nativeInformation.Size()); err != nil {
		_ = replacement.Close()
		t.Fatal(err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	frozenWrapper := []byte("set -eu\n" + shellSingleQuoteForAtomicProbe(executable) + " grep inherited_fd_must_not_match " + shellSingleQuoteForAtomicProbe(root) + "\n")
	command := exec.Command(auditBashExecutable, "-s", "--")
	command.Stdin = bytes.NewReader(frozenWrapper)
	command.ExtraFiles = []*os.File{nativeFile}
	command.Env = []string{
		"HOME=" + home,
		"LANG=C",
		"LC_ALL=C",
		"PATH=/usr/bin:/bin",
		"TZ=UTC",
		"XDG_DATA_HOME=" + xdg,
	}
	output, err := command.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("Failed to load pi_natives native addon")) {
		t.Fatalf("inherited native FD unexpectedly selected: output=%q error=%v", output, err)
	}
	if bytes.Contains(output, []byte("/dev/fd/3")) {
		t.Fatalf("installed loader unexpectedly exposed inherited native FD candidate: %q", output)
	}
}

func installedOMPFixturesForAtomicProbe(t *testing.T) (string, string) {
	t.Helper()
	executable := os.Getenv("ANANKE_PINNED_OMP_FIXTURE")
	nativeAddon := os.Getenv("ANANKE_PINNED_OMP_NATIVE_FIXTURE")
	if executable == "" || nativeAddon == "" {
		t.Skip("ANANKE_PINNED_OMP_FIXTURE and ANANKE_PINNED_OMP_NATIVE_FIXTURE not supplied")
	}
	return executable, nativeAddon
}

func openNoFollowForAtomicProbe(t *testing.T, path string) *os.File {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = unix.Close(fd)
		t.Fatal("opened descriptor is nil")
	}
	return file
}

func linkOrCopyFileForAtomicProbe(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.Link(source, destination); err == nil {
		return
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	information, err := input.Stat()
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, information.Mode().Perm())
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy installed OMP fixture: copy=%v close=%v", copyErr, closeErr)
	}
}

func shellSingleQuoteForAtomicProbe(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
