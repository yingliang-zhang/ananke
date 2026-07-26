package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsOpenOrImplicitArguments(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "artifact")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		arguments []string
	}{
		{name: "no mode"},
		{name: "unknown mode", arguments: []string{"publish"}},
		{name: "build missing output", arguments: []string{"build", "--repository-root", root}},
		{name: "build relative root", arguments: []string{"build", "--repository-root", ".", "--output", absolute}},
		{name: "build relative output", arguments: []string{"build", "--repository-root", root, "--output", "ananke-trusted-supervisor"}},
		{name: "build unknown argument", arguments: []string{"build", "--repository-root", root, "--output", absolute, "--tags", "safe"}},
		{name: "build duplicate root", arguments: []string{"build", "--repository-root", root, "--repository-root", root, "--output", absolute}},
		{name: "verify missing artifact", arguments: []string{"verify"}},
		{name: "verify relative artifact", arguments: []string{"verify", "--artifact", "ananke-trusted-supervisor"}},
		{name: "verify trailing argument", arguments: []string{"verify", "--artifact", absolute, "extra"}},
		{name: "verify duplicate artifact", arguments: []string{"verify", "--artifact", absolute, "--artifact", absolute}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := run(context.Background(), testCase.arguments, &output); err == nil {
				t.Fatalf("run(%q) succeeded", testCase.arguments)
			}
			if output.Len() != 0 {
				t.Fatalf("failed command emitted success output %q", output.String())
			}
		})
	}
}

func TestRunVerifyAcceptsUntaggedAndRejectsTaggedProductionNamedArtifact(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	normal := filepath.Join(t.TempDir(), "normal-candidate", "ananke-trusted-supervisor")
	buildTrustedSupervisor(t, root, normal)
	var normalOutput bytes.Buffer
	if err := run(context.Background(), []string{"verify", "--artifact", normal}, &normalOutput); err != nil {
		t.Fatalf("verify normal artifact: %v", err)
	}
	if !strings.Contains(normalOutput.String(), normal) || !strings.Contains(normalOutput.String(), "verified exact artifact") {
		t.Fatalf("normal verification output = %q", normalOutput.String())
	}

	tagged := filepath.Join(t.TempDir(), "test-only-tagged-artifact", "ananke-trusted-supervisor")
	buildTrustedSupervisor(t, root, tagged, "-tags", "ananke_test_runtime_authority")
	var taggedOutput bytes.Buffer
	err = run(context.Background(), []string{"verify", "--artifact", tagged}, &taggedOutput)
	if err == nil {
		t.Fatal("tagged production-named artifact passed CLI verification")
	}
	for _, evidence := range []string{
		"ananke_test_runtime_authority",
		"NewServerWithCompileTimeTestRuntimeAuthority",
		"ananke-compile-time-test-only-runtime-authority-v1",
		"test-runtime server factory",
	} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("tagged verification error %q does not report %q", err, evidence)
		}
	}
	if taggedOutput.Len() != 0 {
		t.Fatalf("tagged verification emitted success output %q", taggedOutput.String())
	}
}

func buildTrustedSupervisor(t *testing.T, root, output string, flags ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	arguments := append([]string{"build", "-o", output}, flags...)
	arguments = append(arguments, "./cmd/ananke-trusted-supervisor")
	command := exec.Command("go", arguments...)
	command.Dir = root
	command.Env = append(environmentWithoutGOFLAGS(), "GOFLAGS=")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build trusted supervisor: %v\n%s", err, result)
	}
}

func environmentWithoutGOFLAGS() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, setting := range os.Environ() {
		if !strings.HasPrefix(setting, "GOFLAGS=") {
			environment = append(environment, setting)
		}
	}
	return environment
}
