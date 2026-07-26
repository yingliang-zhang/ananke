package releaseartifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	trustedSupervisorPackagePath = "github.com/yingliang-zhang/ananke/cmd/ananke-trusted-supervisor"
	testRuntimeBuildTag          = "ananke_test_runtime_authority"
	testRuntimeConstructor       = "NewServerWithCompileTimeTestRuntimeAuthority"
	testRuntimeMarker            = "ananke-compile-time-test-only-runtime-authority-v1"
)

func TestVerifyTrustedSupervisorCandidateRejectsEveryTestRuntimeArtifact(t *testing.T) {
	root := repositoryRoot(t)
	normal := filepath.Join(t.TempDir(), "normal-candidate", "ananke-trusted-supervisor")
	buildGoBinary(t, root, normal, "./cmd/ananke-trusted-supervisor")
	if err := VerifyTrustedSupervisor(normal); err != nil {
		t.Fatalf("verify normal untagged candidate: %v", err)
	}

	tagged := filepath.Join(t.TempDir(), "test-only", "ananke-trusted-supervisor-test-only")
	buildGoBinary(t, root, tagged, "-tags", testRuntimeBuildTag, "./cmd/ananke-trusted-supervisor")
	assertTaggedArtifactRejected(t, tagged)

	renamed := filepath.Join(t.TempDir(), "renamed-test-only-artifact", "ananke-trusted-supervisor")
	copyFile(t, tagged, renamed)
	assertTaggedArtifactRejected(t, renamed)

	wrongPackage := filepath.Join(t.TempDir(), "wrong-package", "ananke-trusted-supervisor")
	buildGoBinary(t, root, wrongPackage, "./cmd/ananke-trusted-supervisor-transport")
	if err := VerifyTrustedSupervisor(wrongPackage); err == nil || !strings.Contains(err.Error(), trustedSupervisorPackagePath) {
		t.Fatalf("wrong-package verification error = %v; want expected package path", err)
	}
}

func TestBuildAndPublishTrustedSupervisorReleaseRejectsGOFLAGSTagInjectionAndLeavesNoRootBinary(t *testing.T) {
	root := repositoryRoot(t)
	rootBinary := filepath.Join(root, "ananke-trusted-supervisor")
	assertPathAbsent(t, rootBinary)
	if err := BuildAndPublishTrustedSupervisor(context.Background(), root, rootBinary, environmentWithout("GOFLAGS")); err == nil || !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("repository-root output error = %v; want explicit rejection", err)
	}
	assertPathAbsent(t, rootBinary)

	outputDirectory := t.TempDir()
	output := filepath.Join(outputDirectory, "published", "ananke-trusted-supervisor")
	if err := os.Mkdir(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := BuildAndPublishTrustedSupervisor(context.Background(), root, output, environmentWithout("GOFLAGS")); err != nil {
		t.Fatalf("build and publish normal candidate: %v", err)
	}
	if err := VerifyTrustedSupervisor(output); err != nil {
		t.Fatalf("verify exact published output: %v", err)
	}
	assertNoReleaseStagingDirectories(t, filepath.Dir(output))
	assertPathAbsent(t, rootBinary)

	injectedOutput := filepath.Join(outputDirectory, "injected", "ananke-trusted-supervisor")
	if err := os.Mkdir(filepath.Dir(injectedOutput), 0o700); err != nil {
		t.Fatal(err)
	}
	injectedEnvironment := append(environmentWithout("GOFLAGS"), "GOFLAGS=-tags="+testRuntimeBuildTag)
	err := BuildAndPublishTrustedSupervisor(context.Background(), root, injectedOutput, injectedEnvironment)
	if err == nil || !strings.Contains(err.Error(), "GOFLAGS") || !strings.Contains(err.Error(), testRuntimeBuildTag) {
		t.Fatalf("injected GOFLAGS error = %v; want explicit test-runtime tag rejection", err)
	}
	assertPathAbsent(t, injectedOutput)
	assertNoReleaseStagingDirectories(t, filepath.Dir(injectedOutput))
	assertPathAbsent(t, rootBinary)
}

func TestRepositoryRootProductionBinaryHasExactIgnoreAndIsAbsent(t *testing.T) {
	root := repositoryRoot(t)
	contents, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "/ananke-trusted-supervisor" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("exact repository-root ignore count = %d, want 1", count)
	}
	assertPathAbsent(t, filepath.Join(root, "ananke-trusted-supervisor"))
}

func TestBuildAndPublishRefusesExistingDestinationWithoutChangingBytes(t *testing.T) {
	root := repositoryRoot(t)
	outputDirectory := t.TempDir()
	output := filepath.Join(outputDirectory, "ananke-trusted-supervisor")
	const sentinel = "caller-owned sentinel bytes\x00must survive"
	if err := os.WriteFile(output, []byte(sentinel), 0o640); err != nil {
		t.Fatal(err)
	}

	err := BuildAndPublishTrustedSupervisor(context.Background(), root, output, environmentWithout("GOFLAGS"))
	if err == nil {
		t.Fatal("build replaced an existing destination")
	}
	contents, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatalf("read existing destination after refused publication: %v", readErr)
	}
	if string(contents) != sentinel {
		t.Fatalf("existing destination changed: got %q, want %q", contents, sentinel)
	}
	assertNoReleaseStagingDirectories(t, outputDirectory)
}

func TestBuildAndPublishRejectsEveryCallerGOFLAGSIncludingOverlay(t *testing.T) {
	root := repositoryRoot(t)
	overlayDirectory := t.TempDir()
	inertMain := filepath.Join(overlayDirectory, "inert-main.go")
	if err := os.WriteFile(inertMain, []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(overlayDirectory, "overlay.json")
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{
		filepath.Join(root, "cmd", "ananke-trusted-supervisor", "main.go"): inertMain,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"GOFLAGS=",
		"GOFLAGS=-overlay=" + overlayPath,
		"GOFLAGS=-toolexec=/definitely/not/a/trusted-tool",
		"GOFLAGS=-tags=arbitrary_release_probe",
	}
	for _, setting := range tests {
		t.Run(setting, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "ananke-trusted-supervisor")
			environment := append(environmentWithout("GOFLAGS"), setting)
			err := BuildAndPublishTrustedSupervisor(context.Background(), root, output, environment)
			if err == nil || !strings.Contains(err.Error(), "GOFLAGS") {
				t.Fatalf("BuildAndPublishTrustedSupervisor with %q error = %v; want explicit GOFLAGS rejection", setting, err)
			}
			assertPathAbsent(t, output)
		})
	}
}

func TestBuildEnvironmentRejectsSourceAndToolchainSelectors(t *testing.T) {
	tests := []string{
		"GOTOOLCHAIN=local",
		"GOENV=off",
		"GOWORK=off",
		"GOROOT=/trusted-looking-but-caller-selected",
		"GOOS=darwin",
		"GOARCH=arm64",
		"GOPATH=/caller-selected",
		"GOMODCACHE=/caller-selected",
		"GOCACHE=/caller-selected",
		"GOEXPERIMENT=fieldtrack",
		"CGO_ENABLED=1",
		"CC=/caller-selected/compiler",
		"CXX=/caller-selected/compiler",
		"PKG_CONFIG=/caller-selected/pkg-config",
	}
	for _, setting := range tests {
		t.Run(setting, func(t *testing.T) {
			err := validateBuildEnvironment([]string{"HOME=/safe/operator/home", setting})
			if err == nil || !strings.Contains(err.Error(), strings.SplitN(setting, "=", 2)[0]) {
				t.Fatalf("validateBuildEnvironment(%q) error = %v; want named rejection", setting, err)
			}
		})
	}
}

func TestBuildUsesPinnedGoExecutableNotCallerPATH(t *testing.T) {
	root := repositoryRoot(t)
	fakeDirectory := t.TempDir()
	invoked := filepath.Join(fakeDirectory, "fake-go-invoked")
	fakeGo := filepath.Join(fakeDirectory, "go")
	script := fmt.Sprintf("#!/bin/sh\nprintf invoked > %s\nexit 91\n", shellQuote(invoked))
	if err := os.WriteFile(fakeGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "ananke-trusted-supervisor")
	environment := replaceEnvironment(environmentWithout("GOFLAGS"), "PATH", fakeDirectory)
	if err := BuildAndPublishTrustedSupervisor(context.Background(), root, output, environment); err != nil {
		t.Fatalf("build with hostile caller PATH: %v", err)
	}
	assertPathAbsent(t, invoked)
	if err := VerifyTrustedSupervisor(output); err != nil {
		t.Fatalf("verify artifact built without caller PATH: %v", err)
	}
}

func TestVerifyDoesNotInvokeMutableGoToolOrAcceptItsPathReplacement(t *testing.T) {
	root := repositoryRoot(t)
	directory := t.TempDir()
	normal := filepath.Join(directory, "selected")
	tagged := filepath.Join(directory, "tagged")
	buildGoBinary(t, root, normal, "./cmd/ananke-trusted-supervisor")
	buildGoBinary(t, root, tagged, "-tags", testRuntimeBuildTag, "./cmd/ananke-trusted-supervisor")
	normalContents, err := os.ReadFile(normal)
	if err != nil {
		t.Fatal(err)
	}

	fakeDirectory := t.TempDir()
	invoked := filepath.Join(fakeDirectory, "nm-wrapper-invoked")
	replacement := filepath.Join(directory, "replacement")
	fakeGo := filepath.Join(fakeDirectory, "go")
	realGo := filepath.Join(runtime.GOROOT(), "bin", "go")
	script := fmt.Sprintf(`#!/bin/sh
set -eu
printf invoked > %s
inventory=%s
%s "$@" > "$inventory"
cp %s %s
mv %s %s
cat "$inventory"
`, shellQuote(invoked), shellQuote(filepath.Join(fakeDirectory, "inventory")), shellQuote(realGo), shellQuote(tagged), shellQuote(replacement), shellQuote(replacement), shellQuote(normal))
	if err := os.WriteFile(fakeGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	verifyErr := VerifyTrustedSupervisor(normal)
	selectedContents, readErr := os.ReadFile(normal)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if verifyErr == nil && string(selectedContents) != string(normalContents) {
		t.Fatal("verification succeeded after mutable go tool replaced the selected pathname")
	}
	assertPathAbsent(t, invoked)
}

func TestBuildRejectsSymlinkOutputDirectory(t *testing.T) {
	root := repositoryRoot(t)
	parent := t.TempDir()
	realDirectory := filepath.Join(parent, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkDirectory := filepath.Join(parent, "selected")
	if err := os.Symlink(realDirectory, symlinkDirectory); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(symlinkDirectory, "ananke-trusted-supervisor")
	err := BuildAndPublishTrustedSupervisor(context.Background(), root, output, environmentWithout("GOFLAGS"))
	if err == nil {
		t.Fatal("build accepted a symlink output directory")
	}
	assertPathAbsent(t, filepath.Join(realDirectory, filepath.Base(output)))
}

func TestPinnedOutputDirectoryDetectsRebind(t *testing.T) {
	parent := t.TempDir()
	selected := filepath.Join(parent, "selected")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	pinned, err := openPinnedDirectory(selected)
	if err != nil {
		t.Fatalf("pin output directory: %v", err)
	}
	defer pinned.Close()
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(selected, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := pinned.validateBinding(); err == nil {
		t.Fatal("pinned output directory accepted a same-path replacement")
	}
}

func TestBuildRejectsCompilerPathABAThatExecutedSubstitute(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin vnode launch guard regression")
	}
	root := repositoryRoot(t)
	compilerDirectory := t.TempDir()
	compilerPath := filepath.Join(compilerDirectory, "go")
	retainedCompilerPath := filepath.Join(compilerDirectory, "go.retained")
	fakeCompilerPath := filepath.Join(compilerDirectory, "go.fake")
	invokedPath := filepath.Join(compilerDirectory, "substituted-compiler-invoked")
	copyFile(t, filepath.Join(runtime.GOROOT(), "bin", "go"), compilerPath)
	output := filepath.Join(t.TempDir(), "ananke-trusted-supervisor")
	buildSucceeded := false

	options := buildOptions{
		goExecutablePath: compilerPath,
		goRoot:           runtime.GOROOT(),
		beforeStart: func() error {
			if err := os.Rename(compilerPath, retainedCompilerPath); err != nil {
				return err
			}
			script := fmt.Sprintf("#!/bin/sh\nprintf invoked > %s\nexec %s \"$@\"\n", shellQuote(invokedPath), shellQuote(retainedCompilerPath))
			return os.WriteFile(compilerPath, []byte(script), 0o700)
		},
		afterWait: func(waitErr error) error {
			buildSucceeded = waitErr == nil
			if err := os.Rename(compilerPath, fakeCompilerPath); err != nil {
				return err
			}
			return os.Rename(retainedCompilerPath, compilerPath)
		},
	}
	err := buildAndPublishTrustedSupervisor(context.Background(), root, output, environmentWithout("GOFLAGS"), options)
	if err == nil || !strings.Contains(err.Error(), "launch mutation") {
		t.Fatalf("compiler ABA error = %v; want sticky launch mutation rejection", err)
	}
	if !buildSucceeded {
		t.Fatal("substituted compiler did not complete the build interval")
	}
	if contents, readErr := os.ReadFile(invokedPath); readErr != nil || string(contents) != "invoked" {
		t.Fatalf("substituted compiler invocation proof = %q, %v", contents, readErr)
	}
	assertPathAbsent(t, output)
}

func TestBuildRejectsRepositoryPathABAThatBuiltSubstitute(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin vnode launch guard regression")
	}
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	retainedRepository := filepath.Join(parent, "repository.retained")
	substituteRepository := filepath.Join(parent, "repository.substitute")
	usedSubstituteRepository := filepath.Join(parent, "repository.used-substitute")
	writeMinimalTrustedSupervisorRepository(t, repository, "package main\nfunc main(")
	writeMinimalTrustedSupervisorRepository(t, substituteRepository, "package main\nfunc main() {}\n")
	output := filepath.Join(t.TempDir(), "ananke-trusted-supervisor")
	buildSucceeded := false

	options := buildOptions{
		beforeStart: func() error {
			if err := os.Rename(repository, retainedRepository); err != nil {
				return err
			}
			return os.Rename(substituteRepository, repository)
		},
		afterWait: func(waitErr error) error {
			buildSucceeded = waitErr == nil
			if err := os.Rename(repository, usedSubstituteRepository); err != nil {
				return err
			}
			return os.Rename(retainedRepository, repository)
		},
	}
	err := buildAndPublishTrustedSupervisor(context.Background(), repository, output, environmentWithout("GOFLAGS"), options)
	if err == nil || !strings.Contains(err.Error(), "launch mutation") {
		t.Fatalf("repository ABA error = %v; want sticky launch mutation rejection", err)
	}
	if !buildSucceeded {
		t.Fatal("substituted repository did not complete a build that the invalid retained repository could not complete")
	}
	assertPathAbsent(t, output)
}

func TestDirectCandidateCleanupPreservesReplacementBeforeIdentityCapture(t *testing.T) {
	directoryPath := t.TempDir()
	directory, err := openPinnedDirectory(directoryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	const foreign = "foreign replacement must survive"
	var movedPath, replacementPath string
	candidate, err := createStagedCandidate(directory, func(name string) error {
		replacementPath = filepath.Join(directoryPath, name)
		movedPath = replacementPath + ".moved"
		if err := os.Rename(replacementPath, movedPath); err != nil {
			return err
		}
		return os.WriteFile(replacementPath, []byte(foreign), 0o600)
	})
	if err != nil {
		t.Fatalf("create direct staged candidate: %v", err)
	}
	defer candidate.Close()
	if err := candidate.cleanupName(); err == nil || !strings.Contains(err.Error(), "replacement") {
		t.Fatalf("candidate cleanup error = %v; want replacement refusal", err)
	}
	contents, err := os.ReadFile(replacementPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != foreign {
		t.Fatalf("foreign replacement changed: got %q, want %q", contents, foreign)
	}
	if _, err := os.Stat(movedPath); err != nil {
		t.Fatalf("atomically created candidate was not preserved at moved path: %v", err)
	}
}

func writeMinimalTrustedSupervisorRepository(t *testing.T, root, mainSource string) {
	t.Helper()
	commandDirectory := filepath.Join(root, "cmd", "ananke-trusted-supervisor")
	if err := os.MkdirAll(commandDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/yingliang-zhang/ananke\n\ngo 1.26.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commandDirectory, "main.go"), []byte(mainSource), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsEveryBuildTagAndInjectedMarker(t *testing.T) {
	root := repositoryRoot(t)
	directory := t.TempDir()
	tagged := filepath.Join(directory, "arbitrary-tag")
	buildGoBinary(t, root, tagged, "-tags", "arbitrary_release_probe", "./cmd/ananke-trusted-supervisor")
	if err := VerifyTrustedSupervisor(tagged); err == nil || !strings.Contains(err.Error(), "build tag") {
		t.Fatalf("arbitrary-tag verification error = %v; want all build tags rejected", err)
	}

	normal := filepath.Join(directory, "normal")
	buildGoBinary(t, root, normal, "./cmd/ananke-trusted-supervisor")
	for _, marker := range []string{testRuntimeConstructor, testRuntimeMarker, forbiddenTestServerFactoryMarker} {
		t.Run(marker, func(t *testing.T) {
			injected := filepath.Join(directory, strings.NewReplacer("/", "-", " ", "-").Replace(marker))
			copyFile(t, normal, injected)
			file, err := os.OpenFile(injected, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString(marker); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if err := VerifyTrustedSupervisor(injected); err == nil || !strings.Contains(err.Error(), marker) {
				t.Fatalf("injected-marker verification error = %v; want %q", err, marker)
			}
		})
	}
}

func TestRenameNoReplaceIsAtomicUnderRace(t *testing.T) {
	directory := t.TempDir()
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	const racers = 16
	for index := range racers {
		name := fmt.Sprintf("candidate-%02d", index)
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	var successes atomic.Int32
	var winner atomic.Int32
	winner.Store(-1)
	var unexpectedMu sync.Mutex
	var unexpected []error
	var wait sync.WaitGroup
	for index := range racers {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			err := renameNoReplace(fd, fmt.Sprintf("candidate-%02d", index), fd, "published")
			if err == nil {
				successes.Add(1)
				winner.Store(int32(index))
				return
			}
			if !errors.Is(err, os.ErrExist) {
				unexpectedMu.Lock()
				unexpected = append(unexpected, err)
				unexpectedMu.Unlock()
			}
		}(index)
	}
	close(start)
	wait.Wait()
	if len(unexpected) != 0 {
		t.Fatalf("unexpected no-replace errors: %v", unexpected)
	}
	if successes.Load() != 1 {
		t.Fatalf("successful no-replace publications = %d, want exactly 1", successes.Load())
	}
	contents, err := os.ReadFile(filepath.Join(directory, "published"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("candidate-%02d", winner.Load())
	if string(contents) != want {
		t.Fatalf("published bytes = %q, want winning candidate %q", contents, want)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func replaceEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	replaced := make([]string, 0, len(environment)+1)
	for _, setting := range environment {
		if !strings.HasPrefix(setting, prefix) {
			replaced = append(replaced, setting)
		}
	}
	return append(replaced, prefix+value)
}

func assertTaggedArtifactRejected(t *testing.T, path string) {
	t.Helper()
	err := VerifyTrustedSupervisor(path)
	if err == nil {
		t.Fatalf("tagged artifact %q passed verification", path)
	}
	for _, evidence := range []string{testRuntimeBuildTag, testRuntimeConstructor, testRuntimeMarker, "test-runtime server factory"} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("tagged artifact error %q does not report %q", err, evidence)
		}
	}
}

func buildGoBinary(t *testing.T, root, output string, arguments ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", append([]string{"build", "-o", output}, arguments...)...)
	command.Dir = root
	command.Env = append(environmentWithout("GOFLAGS"), "GOFLAGS=")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %q: %v\n%s", output, err, result)
	}
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, contents, 0o700); err != nil {
		t.Fatal(err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func environmentWithout(name string) []string {
	prefix := name + "="
	environment := make([]string, 0, len(os.Environ()))
	for _, setting := range os.Environ() {
		if !strings.HasPrefix(setting, prefix) {
			environment = append(environment, setting)
		}
	}
	return environment
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q exists or cannot be checked: %v", path, err)
	}
}

func assertNoReleaseStagingDirectories(t *testing.T, parent string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(parent, ".ananke-trusted-supervisor-release-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("release staging paths remain: %q", matches)
	}
}
