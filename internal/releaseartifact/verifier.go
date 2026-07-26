package releaseartifact

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	expectedTrustedSupervisorPackage = "github.com/yingliang-zhang/ananke/cmd/ananke-trusted-supervisor"
	forbiddenTestRuntimeBuildTag     = "ananke_test_runtime_authority"
	forbiddenTestRuntimeConstructor  = "NewServerWithCompileTimeTestRuntimeAuthority"
	forbiddenTestRuntimeMarker       = "ananke-compile-time-test-only-runtime-authority-v1"
	forbiddenTestServerFactoryMarker = "ananke-compile-time-test-only-server-factory-v1"
)

var forbiddenTestServerFactoryNames = [][]byte{
	[]byte("server_factory_test_runtime.go"),
	[]byte("newTestRuntimeTrustedSupervisorServer"),
	[]byte("testRuntimeServerFactory"),
	[]byte(forbiddenTestServerFactoryMarker),
}

type verifiedArtifact struct {
	file     *os.File
	identity fileIdentity
	digest   [sha256.Size]byte
}

// VerifyTrustedSupervisor verifies one O_NOFOLLOW-opened regular binary and
// confirms that the caller-selected entry still names that same object and
// byte sequence before returning success.
func VerifyTrustedSupervisor(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("release artifact path must be absolute and clean")
	}
	parent, err := openPinnedDirectory(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("pin release artifact directory: %w", err)
	}
	defer parent.Close()
	if err := parent.validateBinding(); err != nil {
		return fmt.Errorf("validate release artifact directory: %w", err)
	}
	name := filepath.Base(path)
	fd, err := unix.Openat(parent.fd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open release artifact once without following symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()

	verified, err := verifyOpenedTrustedSupervisor(file, path)
	if err != nil {
		return err
	}
	if err := proveLinkedArtifact(parent.fd, name, verified); err != nil {
		return fmt.Errorf("prove selected release artifact binding: %w", err)
	}
	if err := parent.validateBinding(); err != nil {
		return fmt.Errorf("revalidate release artifact directory: %w", err)
	}
	return nil
}

func verifyOpenedTrustedSupervisor(file *os.File, label string) (*verifiedArtifact, error) {
	identity, contents, digest, err := readStableArtifactBytes(file)
	if err != nil {
		return nil, fmt.Errorf("read release artifact %s from retained descriptor: %w", label, err)
	}
	metadata, err := buildinfo.Read(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("read Go build metadata from retained descriptor %s: %w", label, err)
	}
	findings := make(map[string]struct{})
	if metadata.Path != expectedTrustedSupervisorPackage {
		findings[fmt.Sprintf("wrong Go package path %q; want %q", metadata.Path, expectedTrustedSupervisorPackage)] = struct{}{}
	}
	for _, setting := range metadata.Settings {
		if setting.Key == "-tags" && strings.TrimSpace(setting.Value) != "" {
			findings[fmt.Sprintf("forbidden build tags %q", setting.Value)] = struct{}{}
		}
	}
	if bytes.Contains(contents, []byte(forbiddenTestRuntimeBuildTag)) {
		findings["forbidden build tag name "+forbiddenTestRuntimeBuildTag+" in binary"] = struct{}{}
	}
	if bytes.Contains(contents, []byte(forbiddenTestRuntimeConstructor)) {
		findings["forbidden symbol/name "+forbiddenTestRuntimeConstructor] = struct{}{}
	}
	if bytes.Contains(contents, []byte(forbiddenTestRuntimeMarker)) {
		findings["forbidden marker "+forbiddenTestRuntimeMarker] = struct{}{}
	}
	for _, name := range forbiddenTestServerFactoryNames {
		if bytes.Contains(contents, name) {
			findings["forbidden test-runtime server factory "+string(name)] = struct{}{}
		}
	}
	if err := inspectGoSymbolInventory(contents, findings); err != nil {
		return nil, fmt.Errorf("inspect Go symbol inventory from retained descriptor %s: %w", label, err)
	}
	if len(findings) != 0 {
		ordered := make([]string, 0, len(findings))
		for finding := range findings {
			ordered = append(ordered, finding)
		}
		sort.Strings(ordered)
		return nil, fmt.Errorf("release artifact %s rejected: %s", label, strings.Join(ordered, "; "))
	}
	return &verifiedArtifact{file: file, identity: identity, digest: digest}, nil
}

func readStableArtifactBytes(file *os.File) (fileIdentity, []byte, [sha256.Size]byte, error) {
	identity, err := descriptorIdentity(int(file.Fd()))
	if err != nil {
		return fileIdentity{}, nil, [sha256.Size]byte{}, err
	}
	if !identity.isRegular() {
		return fileIdentity{}, nil, [sha256.Size]byte{}, errors.New("release artifact descriptor is not a regular file")
	}
	if identity.size <= 0 {
		return fileIdentity{}, nil, [sha256.Size]byte{}, errors.New("release artifact descriptor is empty")
	}
	if uint64(identity.size) > uint64(^uint(0)>>1) {
		return fileIdentity{}, nil, [sha256.Size]byte{}, errors.New("release artifact is too large to inspect")
	}
	contents := make([]byte, int(identity.size))
	if _, err := io.ReadFull(io.NewSectionReader(file, 0, identity.size), contents); err != nil {
		return fileIdentity{}, nil, [sha256.Size]byte{}, err
	}
	afterRead, err := descriptorIdentity(int(file.Fd()))
	if err != nil {
		return fileIdentity{}, nil, [sha256.Size]byte{}, err
	}
	if !identity.sameRegularObject(afterRead) {
		return fileIdentity{}, nil, [sha256.Size]byte{}, errors.New("release artifact identity or size changed while reading")
	}
	return afterRead, contents, sha256.Sum256(contents), nil
}

func (artifact *verifiedArtifact) confirmUnchanged() error {
	current, err := descriptorIdentity(int(artifact.file.Fd()))
	if err != nil {
		return err
	}
	if !artifact.identity.sameRegularObject(current) {
		return errors.New("verified release artifact identity or size changed")
	}
	digest, err := hashDescriptor(artifact.file, artifact.identity.size)
	if err != nil {
		return err
	}
	if digest != artifact.digest {
		return errors.New("verified release artifact bytes changed")
	}
	afterHash, err := descriptorIdentity(int(artifact.file.Fd()))
	if err != nil {
		return err
	}
	if !artifact.identity.sameRegularObject(afterHash) {
		return errors.New("verified release artifact changed while hashing")
	}
	return nil
}

func hashDescriptor(file *os.File, size int64) ([sha256.Size]byte, error) {
	hash := sha256.New()
	if _, err := io.CopyN(hash, io.NewSectionReader(file, 0, size), size); err != nil {
		return [sha256.Size]byte{}, err
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func proveLinkedArtifact(directoryFD int, name string, artifact *verifiedArtifact) error {
	linkedBefore, err := linkedIdentity(directoryFD, name)
	if err != nil {
		return err
	}
	if !artifact.identity.sameRegularObject(linkedBefore) {
		return errors.New("published pathname does not name the verified device and inode")
	}
	if err := artifact.confirmUnchanged(); err != nil {
		return err
	}
	linkedAfter, err := linkedIdentity(directoryFD, name)
	if err != nil {
		return err
	}
	if !artifact.identity.sameRegularObject(linkedAfter) || !linkedBefore.sameRegularObject(linkedAfter) {
		return errors.New("published pathname changed while proving verified bytes")
	}
	return nil
}

func inspectGoSymbolInventory(contents []byte, findings map[string]struct{}) error {
	symbols, err := parseGoSymbolNames(contents)
	if err != nil {
		return err
	}
	if len(symbols) == 0 {
		return errors.New("empty symbol inventory")
	}
	for _, symbol := range symbols {
		if strings.Contains(symbol, forbiddenTestRuntimeConstructor) {
			findings["forbidden symbol/name "+forbiddenTestRuntimeConstructor] = struct{}{}
		}
		lower := strings.ToLower(symbol)
		if strings.Contains(lower, "test") && strings.Contains(lower, "runtime") && strings.Contains(lower, "serverfactory") {
			findings["forbidden test-runtime server factory symbol "+symbol] = struct{}{}
		}
	}
	return nil
}

func parseGoSymbolNames(contents []byte) ([]string, error) {
	reader := bytes.NewReader(contents)
	switch {
	case len(contents) >= 4 && bytes.Equal(contents[:4], []byte{0x7f, 'E', 'L', 'F'}):
		binary, err := elf.NewFile(reader)
		if err != nil {
			return nil, err
		}
		defer binary.Close()
		symbols, err := binary.Symbols()
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(symbols))
		for _, symbol := range symbols {
			names = append(names, symbol.Name)
		}
		return names, nil
	case len(contents) >= 2 && contents[0] == 'M' && contents[1] == 'Z':
		binary, err := pe.NewFile(reader)
		if err != nil {
			return nil, err
		}
		defer binary.Close()
		names := make([]string, 0, len(binary.Symbols))
		for _, symbol := range binary.Symbols {
			names = append(names, symbol.Name)
		}
		return names, nil
	default:
		binary, err := macho.NewFile(reader)
		if err != nil {
			return nil, err
		}
		defer binary.Close()
		if binary.Symtab == nil {
			return nil, errors.New("Mach-O artifact has no symbol table")
		}
		names := make([]string, 0, len(binary.Symtab.Syms))
		for _, symbol := range binary.Symtab.Syms {
			names = append(names, symbol.Name)
		}
		return names, nil
	}
}
