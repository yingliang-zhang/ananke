package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var encodedSpec string
var expectedGitExecutablePath string

type fixtureSpec struct {
	Scenario                    string   `json:"scenario"`
	Output                      string   `json:"output,omitempty"`
	ResumeOutput                string   `json:"resume_output,omitempty"`
	SessionUUID                 string   `json:"session_uuid,omitempty"`
	ExitCode                    int      `json:"exit_code,omitempty"`
	DelayMilliseconds           int      `json:"delay_milliseconds,omitempty"`
	ExpectedGitExecutablePath   string   `json:"expected_git_executable_path,omitempty"`
	OriginalPath                string   `json:"original_path,omitempty"`
	ProtectedPaths              []string `json:"protected_paths,omitempty"`
	UnixSocketPath              string   `json:"unix_socket_path,omitempty"`
	TCPAddress                  string   `json:"tcp_address,omitempty"`
	SpoofLog                    string   `json:"spoof_log,omitempty"`
	EmitCredential              bool     `json:"emit_credential,omitempty"`
	WriteCredentialArtifacts    bool     `json:"write_credential_artifacts,omitempty"`
	WriteTemporaryWorkAuthority bool     `json:"write_temporary_work_authority,omitempty"`
	FreshSessionMode            string   `json:"fresh_session_mode,omitempty"`
}

type invocation struct {
	Prompt     string
	MaxTime    int
	SessionDir string
	ResumeUUID string
}

func main() {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "--fake-child":
			time.Sleep(30 * time.Second)
			return
		case "--forbidden-exec-probe":
			return
		}
	}
	spec, err := loadSpec()
	if err != nil {
		os.Exit(64)
	}
	call, err := parseInvocation(os.Args[1:])
	if err != nil {
		os.Exit(65)
	}
	if os.Getenv("OMP_SESSION_ROOT") != call.SessionDir {
		os.Exit(66)
	}
	if spec.DelayMilliseconds > 0 {
		time.Sleep(time.Duration(spec.DelayMilliseconds) * time.Millisecond)
	}
	if err := runFixture(spec, call); err != nil {
		os.Exit(70)
	}
	if spec.ExitCode != 0 {
		os.Exit(spec.ExitCode)
	}
}

func loadSpec() (fixtureSpec, error) {
	var spec fixtureSpec
	encoded, err := base64.RawURLEncoding.DecodeString(encodedSpec)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(encoded, &spec); err != nil || spec.Scenario == "" || spec.ExpectedGitExecutablePath == "" ||
		expectedGitExecutablePath == "" || spec.ExpectedGitExecutablePath != expectedGitExecutablePath {
		return fixtureSpec{}, errors.New("invalid fixture")
	}
	return spec, nil
}

func parseInvocation(arguments []string) (invocation, error) {
	if len(arguments) < 11 || arguments[0] != "-p" || arguments[2] != "--yolo" || arguments[3] != "--max-time" {
		return invocation{}, errors.New("invalid direct arguments")
	}
	seconds, err := strconv.Atoi(arguments[4])
	if err != nil || seconds < 1 {
		return invocation{}, errors.New("invalid deadline")
	}
	call := invocation{Prompt: arguments[1], MaxTime: seconds}
	for index := 5; index < len(arguments); {
		if index+1 >= len(arguments) {
			return invocation{}, errors.New("unpaired argument")
		}
		name, value := arguments[index], arguments[index+1]
		switch name {
		case "--resume":
			call.ResumeUUID = value
		case "--model":
			if value != "sudo/gpt-5.6-sol" {
				return invocation{}, errors.New("wrong model")
			}
		case "--thinking":
			if value != "xhigh" {
				return invocation{}, errors.New("wrong thinking")
			}
		case "--session-dir":
			call.SessionDir = value
		default:
			return invocation{}, errors.New("unknown argument")
		}
		index += 2
	}
	if call.SessionDir == "" {
		return invocation{}, errors.New("missing session directory")
	}
	return call, nil
}

func runFixture(spec fixtureSpec, call invocation) error {
	credential := os.Getenv("SUDO_API_KEY")
	if spec.EmitCredential {
		if _, err := io.WriteString(os.Stdout, credential); err != nil {
			return err
		}
	}
	if spec.WriteCredentialArtifacts {
		if err := os.WriteFile(filepath.Join(call.SessionDir, "secret"), []byte(credential), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("TMPDIR"), "secret"), []byte(credential), 0o600); err != nil {
			return err
		}
	}
	if spec.WriteTemporaryWorkAuthority {
		workPath, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), "authority"), []byte(workPath), 0o600); err != nil {
			return err
		}
	}
	if spec.FreshSessionMode != "" {
		if err := writeFreshSession(spec, call); err != nil {
			return err
		}
	}

	switch spec.Scenario {
	case "report":
		return emitReportAndSpoof(spec, call)
	case "hang":
		time.Sleep(30 * time.Second)
		return nil
	case "hang_child":
		return waitForChild()
	case "oversize":
		_, err := io.WriteString(os.Stdout, strings.Repeat("x", 262145))
		return err

	case "timeout_once":
		if call.ResumeUUID == "" {
			return emitInternalTimeout(spec, call, true)
		}
		if call.ResumeUUID != spec.SessionUUID || !strings.Contains(call.Prompt, "Do not call more tools; synthesize") {
			return errors.New("invalid exact resume")
		}
		_, err := io.WriteString(os.Stdout, spec.ResumeOutput)
		return err
	case "timeout_always":
		return emitInternalTimeout(spec, call, call.ResumeUUID == "")
	case "malformed_timeout":
		_, err := io.WriteString(os.Stdout, "Deadline exceeded\n")
		return err
	case "evidence_rejection":
		if _, err := io.WriteString(os.Stdout, spec.Output); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(call.SessionDir, "incomplete.log"), []byte("incomplete"), 0o600)
	case "environment":
		return captureEnvironment(call)
	case "git_startup_boundary":
		return probeGitStartupBoundary(spec)
	case "sandbox_isolation":
		return probeSandboxIsolation(spec, call)
	case "sealed_home":
		return probeSealedHome()
	case "least_authority":
		return probeLeastAuthority(spec, call)
	case "dual_stack":
		return probeDualStack(spec)
	default:
		return errors.New("unknown fixture scenario")
	}
}

func emitReportAndSpoof(spec fixtureSpec, call invocation) error {
	if spec.SpoofLog != "" {
		if err := os.WriteFile(filepath.Join(call.SessionDir, "spoof.log"), []byte(spec.SpoofLog), 0o600); err != nil {
			return err
		}
	}
	_, err := io.WriteString(os.Stdout, spec.Output)
	return err
}

func waitForChild() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, "--fake-child")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	return command.Wait()
}

func writeFreshSession(spec fixtureSpec, call invocation) error {
	if call.ResumeUUID != "" || spec.SessionUUID == "" {
		return errors.New("invalid fresh session fixture")
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	physicalDirectory, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		return err
	}
	records := []any{
		map[string]any{"type": "session", "id": spec.SessionUUID, "cwd": physicalDirectory},
		map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": call.Prompt}},
		map[string]any{"type": "invocation_paths", "paths": []string{
			physicalDirectory, call.SessionDir, os.Getenv("TMPDIR"), os.Getenv("HOME"), os.Getenv("PI_CODING_AGENT_DIR"),
		}},
	}
	switch spec.FreshSessionMode {
	case "authenticated":
	case "leaking":
		if spec.OriginalPath == "" {
			return errors.New("missing leaking session path")
		}
		records = append(records, map[string]any{"path": spec.OriginalPath})
	default:
		return errors.New("invalid fresh session mode")
	}
	file, err := os.OpenFile(filepath.Join(call.SessionDir, "fresh.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

func emitInternalTimeout(spec fixtureSpec, call invocation, createSession bool) error {
	if spec.SessionUUID == "" {
		return errors.New("missing timeout session UUID")
	}
	if createSession {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return err
		}
		physicalDirectory, err := filepath.EvalSymlinks(workingDirectory)
		if err != nil {
			return err
		}
		path := filepath.Join(call.SessionDir, "probe.jsonl")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(file)
		records := []any{
			map[string]any{"type": "session", "id": spec.SessionUUID, "cwd": physicalDirectory},
			map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": call.Prompt}},
		}
		for _, record := range records {
			if err := encoder.Encode(record); err != nil {
				_ = file.Close()
				return err
			}
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	_, err := io.WriteString(os.Stdout, "Deadline exceeded\n")
	if err == nil {
		os.Exit(124)
	}
	return err
}

func captureEnvironment(call invocation) error {
	names := make([]string, 0, len(os.Environ()))
	for _, declaration := range os.Environ() {
		name, _, _ := strings.Cut(declaration, "=")
		names = append(names, name)
	}
	sort.Strings(names)
	if err := os.WriteFile(filepath.Join(call.SessionDir, "env.names"), []byte(strings.Join(names, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(call.SessionDir, "xdg-data-home"), []byte(os.Getenv("XDG_DATA_HOME")), 0o600)
}

func probeGitStartupBoundary(spec fixtureSpec) error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return failGitStartupBoundaryProbe("01")
	}
	ceiling := os.Getenv("GIT_CEILING_DIRECTORIES")
	snapshotParent := filepath.Dir(workingDirectory)
	sameCeiling := ceiling == snapshotParent || "/private"+ceiling == snapshotParent || "/private"+snapshotParent == ceiling
	wantPath := filepath.Dir(spec.ExpectedGitExecutablePath) + ":/usr/bin:/bin"
	if !sameCeiling || os.Getenv("GIT_CONFIG_GLOBAL") != "/dev/null" || os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" ||
		os.Getenv("PATH") != wantPath || os.Getenv("GIT_CONFIG_COUNT") != "" || os.Getenv("GIT_CONFIG_KEY_0") != "" ||
		os.Getenv("GIT_CONFIG_VALUE_0") != "" {
		return failGitStartupBoundaryProbe("02")
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".gitconfig"), []byte("[ananke]\n\tboundary = forbidden-global-config\n"), 0o600); err != nil {
		return failGitStartupBoundaryProbe("03")
	}
	resolvedGit, err := exec.LookPath("git")
	if err != nil || resolvedGit != spec.ExpectedGitExecutablePath || resolvedGit != expectedGitExecutablePath {
		return failGitStartupBoundaryProbe("04")
	}
	discovery := exec.Command("git", "rev-parse", "--show-toplevel")
	discovery.Dir = workingDirectory
	discoveryOutput, err := discovery.CombinedOutput()
	var exitError *exec.ExitError
	if err == nil {
		return failGitStartupBoundaryProbe("05S")
	}
	if !errors.As(err, &exitError) {
		return failGitStartupBoundaryProbe("05X")
	}
	if exitError.ExitCode() != 128 {
		switch exitError.ExitCode() {
		case 1:
			output := string(discoveryOutput)
			switch {
			case strings.Contains(output, "not a git command"):
				return failGitStartupBoundaryProbe("05C")
			case strings.Contains(output, "Operation not permitted") || strings.Contains(output, "operation not permitted"):
				switch {
				case ceiling == snapshotParent:
					return failGitStartupBoundaryProbe("05PE")
				case "/private"+ceiling == snapshotParent:
					return failGitStartupBoundaryProbe("05PV")
				default:
					return failGitStartupBoundaryProbe("05PA")
				}
			case strings.Contains(output, "not a git repository"):
				return failGitStartupBoundaryProbe("05N1")
			case strings.Contains(output, "xcrun"):
				return failGitStartupBoundaryProbe("05R")
			default:
				return failGitStartupBoundaryProbe("05E1")
			}
		case 69:
			return failGitStartupBoundaryProbe("05E69")
		case 126:
			return failGitStartupBoundaryProbe("05E126")
		case 127:
			return failGitStartupBoundaryProbe("05E127")
		default:
			return failGitStartupBoundaryProbe("05EO")
		}
	}
	if !strings.Contains(string(discoveryOutput), "not a git repository") {
		return failGitStartupBoundaryProbe("05O")
	}
	config := exec.Command("git", "config", "--list", "--show-origin")
	config.Dir = workingDirectory
	configOutput, err := config.CombinedOutput()
	if err != nil || len(strings.TrimSpace(string(configOutput))) != 0 {
		return failGitStartupBoundaryProbe("06")
	}
	sourcePath := filepath.Join(workingDirectory, "audit.txt")
	sourceBefore, err := os.ReadFile(sourcePath)
	if err != nil {
		return failGitStartupBoundaryProbe("07")
	}
	patchPath := filepath.Join(os.Getenv("TMPDIR"), "source.patch")
	patch := "diff --git a/audit.txt b/audit.txt\n--- a/audit.txt\n+++ b/audit.txt\n@@ -1 +1 @@\n-immutable audit source\n+tampered by git\n"
	if err := os.WriteFile(patchPath, []byte(patch), 0o600); err != nil {
		return failGitStartupBoundaryProbe("08")
	}
	apply := exec.Command("git", "apply", patchPath)
	apply.Dir = workingDirectory
	if err := apply.Run(); !errors.As(err, &exitError) {
		return failGitStartupBoundaryProbe("09")
	}
	sourceAfter, err := os.ReadFile(sourcePath)
	if err != nil || string(sourceAfter) != string(sourceBefore) {
		return failGitStartupBoundaryProbe("10")
	}
	parentRepository := filepath.Dir(filepath.Dir(workingDirectory))
	refMutation := exec.Command("git", "-C", parentRepository, "symbolic-ref", "HEAD", "refs/heads/ananke-mutated")
	if err := refMutation.Run(); !errors.As(err, &exitError) {
		return failGitStartupBoundaryProbe("11")
	}
	executable, err := os.Executable()
	if err != nil {
		return failGitStartupBoundaryProbe("12A")
	}
	executableBytes, err := os.ReadFile(executable)
	if err != nil {
		return failGitStartupBoundaryProbe("12B")
	}
	arbitraryExecutable := filepath.Join(os.Getenv("TMPDIR"), "arbitrary-test-executable")
	if err := os.WriteFile(arbitraryExecutable, executableBytes, 0o700); err != nil {
		return failGitStartupBoundaryProbe("12C")
	}
	denied := []struct {
		path      string
		arguments []string
	}{
		{path: "/usr/bin/git", arguments: []string{"--version"}},
		{path: "/bin/sh", arguments: []string{"-c", "exit 0"}},
		{path: "/bin/bash", arguments: []string{"-c", "exit 0"}},
		{path: "/usr/bin/env"},
		{path: arbitraryExecutable, arguments: []string{"--forbidden-exec-probe"}},
		{path: "/Applications/Xcode.app/Contents/Developer/usr/bin/git", arguments: []string{"--version"}},
		{path: "/Applications/Xcode-beta.app/Contents/Developer/usr/bin/git", arguments: []string{"--version"}},
		{path: "/Library/Developer/CommandLineTools/usr/bin/make", arguments: []string{"--version"}},
	}
	for index, candidate := range denied {
		if err := exec.Command(candidate.path, candidate.arguments...).Run(); err == nil {
			return failGitStartupBoundaryProbe("13D" + strconv.Itoa(index))
		}
	}
	if err := os.Remove(arbitraryExecutable); err != nil {
		return failGitStartupBoundaryProbe("13R")
	}
	if _, err := io.WriteString(os.Stdout, "git-startup-isolated"); err != nil {
		return failGitStartupBoundaryProbe("14")
	}
	return nil
}

func failGitStartupBoundaryProbe(stage string) error {
	_, _ = io.WriteString(os.Stderr, "git-startup-stage-"+stage)
	return errors.New("Git startup boundary probe failed")
}

func probeSandboxIsolation(spec fixtureSpec, call invocation) error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	source := filepath.Join(workingDirectory, "audit.txt")
	if os.Chmod(source, 0o600) == nil || os.Rename(source, filepath.Join(workingDirectory, "moved.txt")) == nil {
		return errors.New("source mutation allowed")
	}
	link := filepath.Join(os.Getenv("TMPDIR"), "source-link")
	if err := os.Symlink(source, link); err != nil {
		return err
	}
	if os.WriteFile(link, []byte("tamper"), 0o600) == nil || os.WriteFile(spec.OriginalPath, []byte("tamper"), 0o600) == nil {
		return errors.New("repository mutation allowed")
	}
	native := filepath.Join(os.Getenv("HOME"), ".omp", "natives", "17.1.8", "pi_natives.darwin-arm64.node")
	if os.WriteFile(native, []byte("tamper"), 0o600) == nil || os.Mkdir(filepath.Join(filepath.Dir(native), "child-mutation"), 0o700) == nil {
		return errors.New("native mutation allowed")
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("TMPDIR"), "temporary"), []byte("temporary"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(call.SessionDir, "session.uuid"), []byte("session"), 0o600); err != nil {
		return err
	}
	_, err = io.WriteString(os.Stdout, "audit-success")
	return err
}

func probeSealedHome() error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, ancestor := range []string{filepath.Dir(workingDirectory), filepath.Dir(filepath.Dir(workingDirectory))} {
		if _, err := os.ReadDir(ancestor); err != nil {
			return errors.New("required source ancestor inventory denied")
		}
		if _, err := os.ReadFile(filepath.Join(ancestor, "ancestor-secret")); err == nil {
			return errors.New("source ancestor child data allowed")
		}
	}
	home := os.Getenv("HOME")
	homeState := filepath.Join(os.Getenv("HOME"), ".omp")
	homeRun := filepath.Join(homeState, "run")
	if err := os.WriteFile(filepath.Join(homeRun, "runtime-state"), []byte("run-state"), 0o600); err != nil {
		return err
	}
	if os.Chmod(homeState, 0o700) == nil {
		return errors.New("sealed HOME mode mutation allowed")
	}
	if os.Rename(homeState, homeState+".moved") == nil {
		return errors.New("sealed HOME replacement allowed")
	}
	if os.Mkdir(filepath.Join(homeState, "logs"), 0o700) == nil {
		return errors.New("sealed HOME logs allowed")
	}
	if os.WriteFile(filepath.Join(homeState, "sibling"), []byte("forbidden"), 0o600) == nil {
		return errors.New("sealed HOME sibling allowed")
	}
	if os.WriteFile(filepath.Join(home, "sibling"), []byte("forbidden"), 0o600) == nil {
		return errors.New("HOME sibling allowed outside runtime state")
	}
	information, err := os.Lstat(homeState)
	if err != nil || !information.IsDir() || information.Mode().Perm() != 0o500 {
		return errors.New("sealed HOME identity changed")
	}
	_, err = io.WriteString(os.Stdout, "sealed-home-success")
	return err
}

func probeLeastAuthority(spec fixtureSpec, call invocation) error {
	for _, path := range spec.ProtectedPaths {
		if _, err := os.ReadFile(path); err == nil {
			return errors.New("protected path readable")
		}
	}
	if spec.OriginalPath != "" && os.WriteFile(spec.OriginalPath, []byte("tamper"), 0o600) == nil {
		return errors.New("original path writable")
	}
	native := filepath.Join(os.Getenv("XDG_DATA_HOME"), "omp", "natives", "17.1.8", "pi_natives.darwin-arm64.node")
	if os.WriteFile(native, []byte("tamper"), 0o600) == nil || os.Mkdir(filepath.Join(filepath.Dir(native), "child-mutation"), 0o700) == nil {
		return errors.New("native authority writable")
	}
	if spec.UnixSocketPath != "" {
		if connection, err := net.DialTimeout("unix", spec.UnixSocketPath, 100*time.Millisecond); err == nil {
			_ = connection.Close()
			return errors.New("unix endpoint reachable")
		}
	}
	if spec.TCPAddress != "" {
		if connection, err := net.DialTimeout("tcp", spec.TCPAddress, 100*time.Millisecond); err == nil {
			_ = connection.Close()
			return errors.New("tcp endpoint reachable")
		}
	}
	if err := sendGatewayRequest("tcp4", "127.0.0.1"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(call.SessionDir, "session.uuid"), []byte("session"), 0o600); err != nil {
		return err
	}
	_, err := io.WriteString(os.Stdout, spec.Output)
	return err
}

func probeDualStack(spec fixtureSpec) error {
	if err := sendGatewayRequest("tcp4", "127.0.0.1"); err != nil {
		return err
	}
	if err := sendGatewayRequest("tcp6", "::1"); err != nil {
		return err
	}
	_, err := io.WriteString(os.Stdout, spec.Output)
	return err
}

func sendGatewayRequest(network, host string) error {
	authority, err := gatewayAuthority()
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(authority)
	if err != nil {
		return err
	}
	connection, err := net.DialTimeout(network, net.JoinHostPort(host, port), time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	request := fmt.Sprintf("POST /v1/responses HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}", authority, os.Getenv("SUDO_API_KEY"))
	if _, err := io.WriteString(connection, request); err != nil {
		return err
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	status, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if !strings.HasPrefix(status, "HTTP/1.1 502 Bad Gateway") {
		return errors.New("unexpected gateway response")
	}
	return nil
}

func gatewayAuthority() (string, error) {
	contents, err := os.ReadFile(filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "models.yml"))
	if err != nil {
		return "", err
	}
	const prefix = "baseUrl: http://"
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) && strings.HasSuffix(trimmed, "/v1") {
			return strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), "/v1"), nil
		}
	}
	return "", errors.New("missing gateway authority")
}
