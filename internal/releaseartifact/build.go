package releaseartifact

import (
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const releaseStagingPrefix = ".ananke-trusted-supervisor-release-"

type buildLaunchMutationGuard interface {
	Check() error
	Close() error
}

type buildOptions struct {
	goExecutablePath string
	goRoot           string
	beforeStart      func() error
	afterWait        func(error) error
}

// BuildAndPublishTrustedSupervisor builds the exact untagged production
// package into a retained descriptor, verifies that descriptor, and publishes
// it with descriptor-relative atomic no-replace semantics.
func BuildAndPublishTrustedSupervisor(ctx context.Context, repositoryRoot, outputPath string, environment []string) error {
	return buildAndPublishTrustedSupervisor(ctx, repositoryRoot, outputPath, environment, buildOptions{})
}

func buildAndPublishTrustedSupervisor(ctx context.Context, repositoryRoot, outputPath string, environment []string, options buildOptions) (resultErr error) {
	if repositoryRoot == "" || !filepath.IsAbs(repositoryRoot) || filepath.Clean(repositoryRoot) != repositoryRoot {
		return errors.New("repository root must be absolute and clean")
	}
	if outputPath == "" || !filepath.IsAbs(outputPath) || filepath.Clean(outputPath) != outputPath {
		return errors.New("release output path must be explicit, absolute, and clean")
	}
	rootOutput := filepath.Join(repositoryRoot, "ananke-trusted-supervisor")
	if outputPath == rootOutput {
		return fmt.Errorf("release output path must not be the repository root production binary %s", rootOutput)
	}
	if environment == nil {
		environment = os.Environ()
	}
	if err := validateBuildEnvironment(environment); err != nil {
		return err
	}
	if options.goRoot == "" {
		options.goRoot = filepath.Clean(runtime.GOROOT())
	}
	if options.goExecutablePath == "" {
		options.goExecutablePath = filepath.Join(options.goRoot, "bin", "go")
	}

	repositoryParent, err := openPinnedDirectory(filepath.Dir(repositoryRoot))
	if err != nil {
		return fmt.Errorf("pin repository parent: %w", err)
	}
	defer repositoryParent.Close()
	repository, err := openPinnedDirectory(repositoryRoot)
	if err != nil {
		return fmt.Errorf("pin repository root: %w", err)
	}
	defer repository.Close()
	outputDirectory, err := openPinnedDirectory(filepath.Dir(outputPath))
	if err != nil {
		return fmt.Errorf("pin release output directory: %w", err)
	}
	defer outputDirectory.Close()
	if err := outputDirectory.validateBinding(); err != nil {
		return fmt.Errorf("validate release output directory: %w", err)
	}
	outputName := filepath.Base(outputPath)
	if err := requireAbsentAt(outputDirectory.fd, outputName); err != nil {
		return fmt.Errorf("release destination must not already exist: %w", err)
	}

	goTool, err := openTrustedGoExecutable(options.goRoot, options.goExecutablePath)
	if err != nil {
		return err
	}
	defer goTool.Close()
	buildEnvironment, err := closedBuildEnvironment(goTool.root)
	if err != nil {
		return err
	}
	candidate, err := createStagedCandidate(outputDirectory, nil)
	if err != nil {
		return err
	}
	candidateCleaned := false
	defer func() {
		if candidateCleaned {
			return
		}
		if cleanupErr := candidate.cleanupName(); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("clean direct release candidate without removing replacements: %w", cleanupErr))
		}
	}()
	defer candidate.Close()

	guard, err := newBuildLaunchMutationGuard(
		int(goTool.file.Fd()), goTool.parent.fd,
		repository.fd, repositoryParent.fd,
	)
	if err != nil {
		return fmt.Errorf("establish fail-closed build launch mutation guard: %w", err)
	}
	defer guard.Close()
	if err := repositoryParent.validateBinding(); err != nil {
		return fmt.Errorf("validate repository parent before build: %w", err)
	}
	if err := repository.validateBinding(); err != nil {
		return fmt.Errorf("validate repository root before build: %w", err)
	}
	if err := goTool.validateBinding(); err != nil {
		return fmt.Errorf("validate pinned Go executable before build: %w", err)
	}
	if err := outputDirectory.validateBinding(); err != nil {
		return fmt.Errorf("validate release output directory before build: %w", err)
	}
	build := exec.CommandContext(ctx, goTool.path,
		"build", "-trimpath", "-buildvcs=false", "-mod=readonly",
		"-o", "/dev/fd/3", "./cmd/ananke-trusted-supervisor",
	)
	build.Dir = repositoryRoot
	build.Env = buildEnvironment
	build.ExtraFiles = []*os.File{candidate.file}
	var buildOutput strings.Builder
	build.Stdout = &buildOutput
	build.Stderr = &buildOutput
	if options.beforeStart != nil {
		if err := options.beforeStart(); err != nil {
			return fmt.Errorf("prepare build launch mutation regression: %w", err)
		}
	}
	buildErr := build.Start()
	if buildErr == nil {
		buildErr = build.Wait()
	}
	if options.afterWait != nil {
		if err := options.afterWait(buildErr); err != nil {
			return fmt.Errorf("finish build launch mutation regression: %w", err)
		}
	}
	if err := guard.Check(); err != nil {
		return fmt.Errorf("reject build launch mutation: %w", err)
	}
	if buildErr != nil {
		return fmt.Errorf("build exact untagged trusted-supervisor package: %w: %s", buildErr, strings.TrimSpace(buildOutput.String()))
	}
	if err := repositoryParent.validateBinding(); err != nil {
		return fmt.Errorf("repository parent changed during build: %w", err)
	}
	if err := repository.validateBinding(); err != nil {
		return fmt.Errorf("repository root changed during build: %w", err)
	}
	if err := goTool.validateBinding(); err != nil {
		return fmt.Errorf("trusted Go executable changed during build: %w", err)
	}
	builtIdentity, err := descriptorIdentity(int(candidate.file.Fd()))
	if err != nil {
		return fmt.Errorf("inspect built release candidate descriptor: %w", err)
	}
	if !candidate.identity.sameObject(builtIdentity) || !builtIdentity.isRegular() || builtIdentity.size == 0 {
		return errors.New("build did not populate the retained release candidate descriptor")
	}
	if err := unix.Fchmod(int(candidate.file.Fd()), 0o555); err != nil {
		return fmt.Errorf("set release candidate executable mode: %w", err)
	}
	if err := candidate.file.Sync(); err != nil {
		return fmt.Errorf("sync built release candidate: %w", err)
	}
	if err := outputDirectory.sync(); err != nil {
		return fmt.Errorf("sync release output directory before verification: %w", err)
	}

	verified, err := verifyOpenedTrustedSupervisor(candidate.file, "retained release candidate descriptor")
	if err != nil {
		return fmt.Errorf("verify untagged trusted-supervisor candidate: %w", err)
	}
	if err := verified.confirmUnchanged(); err != nil {
		return fmt.Errorf("recheck verified release candidate: %w", err)
	}
	if err := candidate.validateBinding(verified.identity); err != nil {
		return err
	}
	if err := repositoryParent.validateBinding(); err != nil {
		return fmt.Errorf("repository parent changed before publication: %w", err)
	}
	if err := repository.validateBinding(); err != nil {
		return fmt.Errorf("repository root changed before publication: %w", err)
	}
	if err := goTool.validateBinding(); err != nil {
		return fmt.Errorf("trusted Go executable changed before publication: %w", err)
	}
	if err := guard.Check(); err != nil {
		return fmt.Errorf("reject build launch mutation before publication: %w", err)
	}
	if err := outputDirectory.validateBinding(); err != nil {
		return fmt.Errorf("release output directory changed before publication: %w", err)
	}
	if err := renameNoReplace(outputDirectory.fd, candidate.name, outputDirectory.fd, outputName); err != nil {
		return fmt.Errorf("atomically publish verified release candidate without replacement: %w", err)
	}
	if err := outputDirectory.sync(); err != nil {
		return fmt.Errorf("sync published release output directory: %w", err)
	}
	if err := candidate.cleanupName(); err != nil {
		return fmt.Errorf("clean direct release candidate without removing replacements: %w", err)
	}
	candidateCleaned = true
	if err := outputDirectory.validateBinding(); err != nil {
		return fmt.Errorf("release output directory changed after publication: %w", err)
	}
	if err := proveLinkedArtifact(outputDirectory.fd, outputName, verified); err != nil {
		return fmt.Errorf("prove published artifact device, inode, and hash: %w", err)
	}
	if err := outputDirectory.validateBinding(); err != nil {
		return fmt.Errorf("release output directory changed during final proof: %w", err)
	}
	return nil
}

func requireAbsentAt(directoryFD int, name string) error {
	_, err := linkedIdentity(directoryFD, name)
	switch {
	case err == nil:
		return os.ErrExist
	case errors.Is(err, unix.ENOENT):
		return nil
	default:
		return err
	}
}

func validateBuildEnvironment(environment []string) error {
	for _, setting := range environment {
		name, _, found := strings.Cut(setting, "=")
		if !found || name == "" {
			return fmt.Errorf("invalid caller build environment entry %q", setting)
		}
		if prohibitedBuildEnvironmentName(name) {
			return fmt.Errorf("caller build environment setting %q is forbidden; the release builder uses fixed trusted values", setting)
		}
	}
	return nil
}

func prohibitedBuildEnvironmentName(name string) bool {
	if strings.HasPrefix(name, "GO") || strings.HasPrefix(name, "CGO_") {
		return true
	}
	switch name {
	case "CC", "CXX", "AR", "FC", "F77", "GCCGO", "GOLLVM", "LLVM_CONFIG", "PKG_CONFIG", "CFLAGS", "CPPFLAGS", "CXXFLAGS", "LDFLAGS":
		return true
	default:
		return false
	}
}

func closedBuildEnvironment(goRoot string) ([]string, error) {
	account, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("resolve trusted build account: %w", err)
	}
	home := filepath.Clean(account.HomeDir)
	if home == "." || !filepath.IsAbs(home) {
		return nil, errors.New("trusted build account home must be absolute")
	}
	cache := filepath.Join(home, ".cache", "go-build")
	if runtime.GOOS == "darwin" {
		cache = filepath.Join(home, "Library", "Caches", "go-build")
	}
	gopath := filepath.Join(home, "go")
	return []string{
		"HOME=" + home,
		"PATH=/usr/bin:/bin",
		"TMPDIR=/tmp",
		"GOROOT=" + goRoot,
		"GOTOOLCHAIN=local",
		"GOENV=off",
		"GOWORK=off",
		"GOFLAGS=",
		"GO111MODULE=on",
		"GOOS=" + runtime.GOOS,
		"GOARCH=" + runtime.GOARCH,
		"GOPATH=" + gopath,
		"GOMODCACHE=" + filepath.Join(gopath, "pkg", "mod"),
		"GOCACHE=" + cache,
		"GOPROXY=https://proxy.golang.org,direct",
		"GOSUMDB=sum.golang.org",
		"GOPRIVATE=",
		"GONOPROXY=",
		"GONOSUMDB=",
		"GOVCS=public:git|hg,private:off",
		"GOTELEMETRY=off",
		"CGO_ENABLED=0",
	}, nil
}

type trustedGoExecutable struct {
	root     string
	path     string
	name     string
	parent   *pinnedDirectory
	file     *os.File
	identity fileIdentity
	digest   [32]byte
}

func openTrustedGoExecutable(root, path string) (*trustedGoExecutable, error) {
	root = filepath.Clean(root)
	if root == "." || !filepath.IsAbs(root) {
		return nil, errors.New("trusted Go root must be absolute")
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("trusted Go executable path must be absolute and clean")
	}
	parent, err := openPinnedDirectory(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("pin trusted Go executable directory: %w", err)
	}
	name := filepath.Base(path)
	fd, err := unix.Openat(parent.fd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		parent.Close()
		return nil, fmt.Errorf("open trusted absolute Go executable: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	identity, err := descriptorIdentity(fd)
	if err != nil {
		file.Close()
		parent.Close()
		return nil, fmt.Errorf("inspect trusted Go executable: %w", err)
	}
	if !identity.isRegular() || identity.mode&0o111 == 0 || identity.size == 0 {
		file.Close()
		parent.Close()
		return nil, errors.New("trusted Go executable is not a nonempty executable regular file")
	}
	metadata, err := buildinfo.Read(file)
	if err != nil {
		file.Close()
		parent.Close()
		return nil, fmt.Errorf("read trusted Go executable build metadata: %w", err)
	}
	if metadata.Path != "cmd/go" || metadata.GoVersion != runtime.Version() {
		file.Close()
		parent.Close()
		return nil, fmt.Errorf("trusted Go executable identity is %q %q; want cmd/go %q", metadata.Path, metadata.GoVersion, runtime.Version())
	}
	digest, err := hashDescriptor(file, identity.size)
	if err != nil {
		file.Close()
		parent.Close()
		return nil, fmt.Errorf("hash trusted Go executable: %w", err)
	}
	tool := &trustedGoExecutable{root: root, path: path, name: name, parent: parent, file: file, identity: identity, digest: digest}
	if err := tool.validateBinding(); err != nil {
		tool.Close()
		return nil, err
	}
	return tool, nil
}

func (tool *trustedGoExecutable) validateBinding() error {
	if err := tool.parent.validateBinding(); err != nil {
		return err
	}
	linked, err := linkedIdentity(tool.parent.fd, tool.name)
	if err != nil {
		return err
	}
	current, err := descriptorIdentity(int(tool.file.Fd()))
	if err != nil {
		return err
	}
	if !tool.identity.sameRegularObject(linked) || !tool.identity.sameRegularObject(current) {
		return errors.New("trusted absolute Go executable path no longer names the pinned identity")
	}
	digest, err := hashDescriptor(tool.file, tool.identity.size)
	if err != nil {
		return err
	}
	if digest != tool.digest {
		return errors.New("trusted absolute Go executable bytes changed")
	}
	return tool.parent.validateBinding()
}

func (tool *trustedGoExecutable) Close() error {
	return errors.Join(tool.file.Close(), tool.parent.Close())
}
