package trustedsupervisor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	auditSandboxExecutable              = "/usr/bin/sandbox-exec"
	auditCommandDescriptorSchemaVersion = "ananke.local-trusted-supervisor-command-descriptor.v7"
	auditGitRepositoryDiscoveryPolicy   = "exact_snapshot_parent_ceiling_no_global_or_system_config_v1"
	auditChildExecutablePath            = "/Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin"
	maxAuditCaptureBytes                = 64 * 1024
	maxAuditTreeScanBytes               = 4 * 1024 * 1024
	maxAuditTreeScanFiles               = 512
	maxAuditModelsConfigBytes           = 4096
)

var auditSessionUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type auditResume struct {
	SessionUUID    string
	SynthesizeOnly bool
}

type auditGitEnvironmentBoundary struct {
	CeilingDirectories string `json:"GIT_CEILING_DIRECTORIES"`
	ConfigGlobal       string `json:"GIT_CONFIG_GLOBAL"`
	ConfigNoSystem     string `json:"GIT_CONFIG_NOSYSTEM"`
	Path               string `json:"PATH"`
}

type auditInvocation struct {
	RunID                 string
	SessionRunID          string
	PromptDir             string
	PromptPath            string
	OutputDir             string
	OutputPath            string
	SessionDir            string
	TemporaryDir          string
	WorkDir               string
	AgentDir              string
	ModelsPath            string
	HomeDir               string
	HomeStateDir          string
	HomeRunDir            string
	NativeAddonPath       string
	GatewayAuthority      string
	ModelsSHA256          string
	ModelsSize            int64
	NativeAddonSHA256     string
	NativeAddonSize       int64
	modelsIdentity        executionPolicyFileIdentity
	nativeAddonIdentity   executionPolicyFileIdentity
	OMPExecutablePath     string
	Arguments             []string
	CommandDescriptorHash string
	PromptSHA256          string
	SandboxProfile        string
	SandboxProfileHash    string
	Resume                auditResume
	OwnedRoots            []auditOwnedRootIdentity
	sharedOwnedRoots      []auditOwnedRootIdentity
	namespaceAuthority    *auditNamespaceAuthority
	namespaceLeases       *auditNamespaceLeaseSet
}

type auditInvocationHooks struct {
	CredentialLookup   func(string) (string, bool)
	BeforeStart        func()
	BrokerReady        func(string, string)
	TransportReady     func(auditInvocation) error
	StartGate          func() (func(), error)
	AfterStart         func(auditProcessIdentity) error
	BeforeArtifactScan func(auditInvocation)
	ProcessOperations  auditProcessOperations
	StartCommand       func(*exec.Cmd, *os.File, *os.File) error
	TerminationBounds  auditTerminationBounds
	HardTimeout        <-chan time.Time
}

type auditSupervisorTestHooks struct {
	StartGate         func() (func(), error)
	AfterStart        func(auditProcessIdentity) error
	AfterFinish       func(auditProcessIdentity)
	ProcessOperations auditProcessOperations
	TerminationBounds auditTerminationBounds
	HardTimeout       <-chan time.Time
}

type auditProcessResultError interface {
	error
	auditProcessResult() auditInvocationResult
}

type auditInvocationStageError struct {
	failureClass string
	cause        error
}

func (failure *auditInvocationStageError) Error() string {
	return "audit invocation stage failed"
}

func (failure *auditInvocationStageError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func failAuditInvocationStage(failureClass string, cause error) error {
	if cause == nil || !protocolIdentifierPattern.MatchString(failureClass) {
		return ErrProtocol
	}
	return &auditInvocationStageError{failureClass: failureClass, cause: cause}
}

type auditArtifactScanRole string

const (
	auditArtifactScanRolePrompt       auditArtifactScanRole = "prompt"
	auditArtifactScanRoleOutput       auditArtifactScanRole = "output"
	auditArtifactScanRoleSession      auditArtifactScanRole = "session"
	auditArtifactScanRoleTemporary    auditArtifactScanRole = "temporary"
	auditArtifactScanRoleUnclassified auditArtifactScanRole = "unclassified"
)

func validAuditArtifactScanRole(role auditArtifactScanRole) bool {
	switch role {
	case auditArtifactScanRolePrompt, auditArtifactScanRoleOutput, auditArtifactScanRoleSession,
		auditArtifactScanRoleTemporary, auditArtifactScanRoleUnclassified:
		return true
	default:
		return false
	}
}

type auditArtifactScanReason string

const (
	auditArtifactScanReasonWalk                auditArtifactScanReason = "walk"
	auditArtifactScanReasonSymlink             auditArtifactScanReason = "symlink"
	auditArtifactScanReasonSpecial             auditArtifactScanReason = "special"
	auditArtifactScanReasonLimit               auditArtifactScanReason = "limit"
	auditArtifactScanReasonRead                auditArtifactScanReason = "read"
	auditArtifactScanReasonTimeoutSecret       auditArtifactScanReason = "timeout_secret"
	auditArtifactScanReasonFreshAuthentication auditArtifactScanReason = "fresh_authentication"
	auditArtifactScanReasonFreshAuthority      auditArtifactScanReason = "fresh_authority"
	auditArtifactScanReasonAuthority           auditArtifactScanReason = "authority"
	auditArtifactScanReasonProtected           auditArtifactScanReason = "protected"
)

func validAuditArtifactScanReason(reason auditArtifactScanReason) bool {
	switch reason {
	case auditArtifactScanReasonWalk, auditArtifactScanReasonSymlink, auditArtifactScanReasonSpecial,
		auditArtifactScanReasonLimit, auditArtifactScanReasonRead, auditArtifactScanReasonTimeoutSecret,
		auditArtifactScanReasonFreshAuthentication, auditArtifactScanReasonFreshAuthority,
		auditArtifactScanReasonAuthority, auditArtifactScanReasonProtected:
		return true
	default:
		return false
	}
}

type auditArtifactTemporaryLocation string

const (
	auditArtifactTemporaryLocationAgent auditArtifactTemporaryLocation = "agent"
	auditArtifactTemporaryLocationHome  auditArtifactTemporaryLocation = "home"
	auditArtifactTemporaryLocationRoot  auditArtifactTemporaryLocation = "temporary_root"
	auditArtifactTemporaryLocationOther auditArtifactTemporaryLocation = "other_temporary"
)

func validAuditArtifactTemporaryLocation(location auditArtifactTemporaryLocation) bool {
	switch location {
	case auditArtifactTemporaryLocationAgent, auditArtifactTemporaryLocationHome,
		auditArtifactTemporaryLocationRoot, auditArtifactTemporaryLocationOther:
		return true
	default:
		return false
	}
}

func auditArtifactPathWithinRoot(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func classifyAuditArtifactTemporaryLocation(invocation auditInvocation, path string) auditArtifactTemporaryLocation {
	if !auditArtifactPathWithinRoot(path, invocation.TemporaryDir) {
		return ""
	}
	if auditArtifactPathWithinRoot(path, invocation.AgentDir) {
		return auditArtifactTemporaryLocationAgent
	}
	if auditArtifactPathWithinRoot(path, invocation.HomeDir) {
		return auditArtifactTemporaryLocationHome
	}
	if path == invocation.TemporaryDir || filepath.Dir(path) == invocation.TemporaryDir {
		return auditArtifactTemporaryLocationRoot
	}
	return auditArtifactTemporaryLocationOther
}

type auditArtifactScanError struct {
	role              auditArtifactScanRole
	reason            auditArtifactScanReason
	authorityKind     auditAuthorityKind
	temporaryLocation auditArtifactTemporaryLocation
	cause             error
}

func (failure *auditArtifactScanError) Error() string {
	return failure.failureClass()
}

func (failure *auditArtifactScanError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (failure *auditArtifactScanError) failureClass() string {
	if failure == nil || !validAuditArtifactScanRole(failure.role) || !validAuditArtifactScanReason(failure.reason) {
		return ""
	}
	base := "artifact_scan_" + string(failure.role) + "_" + string(failure.reason)
	if failure.reason != auditArtifactScanReasonAuthority {
		if failure.authorityKind != "" || failure.temporaryLocation != "" {
			return ""
		}
		return base
	}
	if !validAuditAuthorityKind(failure.authorityKind) {
		return ""
	}
	if failure.role == auditArtifactScanRoleTemporary {
		if !validAuditArtifactTemporaryLocation(failure.temporaryLocation) {
			return ""
		}
		return base + "_" + string(failure.temporaryLocation) + "_" + string(failure.authorityKind)
	}
	if failure.temporaryLocation != "" {
		return ""
	}
	return base + "_" + string(failure.authorityKind)
}

func newAuditArtifactScanError(role auditArtifactScanRole, reason auditArtifactScanReason, cause error) error {
	failure := &auditArtifactScanError{role: role, reason: reason, cause: cause}
	if cause == nil || failure.failureClass() == "" {
		return ErrProtocol
	}
	return failure
}

func newAuditArtifactAuthorityScanError(role auditArtifactScanRole, location auditArtifactTemporaryLocation, kind auditAuthorityKind, cause error) error {
	failure := &auditArtifactScanError{
		role: role, reason: auditArtifactScanReasonAuthority,
		authorityKind: kind, temporaryLocation: location, cause: cause,
	}
	if cause == nil || failure.failureClass() == "" {
		return ErrProtocol
	}
	return failure
}

func failAuditArtifactAuthorityScan(invocation auditInvocation, root, path string, kind auditAuthorityKind, cause error) error {
	role := auditArtifactScanRoleForRoot(invocation, root)
	location := auditArtifactTemporaryLocation("")
	if role == auditArtifactScanRoleTemporary {
		location = classifyAuditArtifactTemporaryLocation(invocation, path)
	}
	return newAuditArtifactAuthorityScanError(role, location, kind, cause)
}

func auditArtifactScanRoleForRoot(invocation auditInvocation, root string) auditArtifactScanRole {
	switch root {
	case invocation.PromptDir:
		return auditArtifactScanRolePrompt
	case invocation.OutputDir:
		return auditArtifactScanRoleOutput
	case invocation.SessionDir:
		return auditArtifactScanRoleSession
	case invocation.TemporaryDir:
		return auditArtifactScanRoleTemporary
	default:
		return auditArtifactScanRoleUnclassified
	}
}

func failAuditArtifactScan(invocation auditInvocation, root string, reason auditArtifactScanReason, cause error) error {
	return newAuditArtifactScanError(auditArtifactScanRoleForRoot(invocation, root), reason, cause)
}

func failAuditArtifactScanStage(err error) error {
	var failure *auditArtifactScanError
	if !errors.As(err, &failure) {
		return err
	}
	failureClass := failure.failureClass()
	if failureClass == "" {
		return err
	}
	return failAuditInvocationStage(failureClass, err)
}

type auditSupervisorTestError struct {
	result auditInvocationResult
	cause  error
}

func (failure *auditSupervisorTestError) Error() string {
	if failure == nil || failure.cause == nil {
		return "supervisor audit test failed"
	}
	return failure.cause.Error()
}

func (failure *auditSupervisorTestError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (failure *auditSupervisorTestError) auditProcessResult() auditInvocationResult {
	if failure == nil {
		return auditInvocationResult{}
	}
	return failure.result
}

type auditOwnedProcessError struct {
	identity auditProcessIdentity
	waiter   *auditProcessWaiter
	failure  error
}

func (failure *auditOwnedProcessError) Error() string {
	if failure == nil || failure.failure == nil {
		return "owned audit process lifecycle failed"
	}
	return failure.failure.Error()
}

func (failure *auditOwnedProcessError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.failure
}

func (failure *auditOwnedProcessError) auditProcessResult() auditInvocationResult {
	if failure == nil {
		return auditInvocationResult{}
	}
	return auditInvocationResult{
		PID: failure.identity.PID, PGID: failure.identity.PGID, ProcessStartIdentity: failure.identity.ProcessStartIdentity,
		StartedAt: failure.identity.StartedAt, ExitCode: -1, processWaiter: failure.waiter,
	}
}

func auditProcessResultFromError(err error) (auditInvocationResult, bool) {
	var failure auditProcessResultError
	if !errors.As(err, &failure) {
		return auditInvocationResult{}, false
	}
	return failure.auditProcessResult(), true
}

type auditProcessIdentity struct {
	PID                  int
	PGID                 int
	ProcessStartIdentity string
	StartedAt            string
}

type auditInvocationResult struct {
	PID                   int
	PGID                  int
	ProcessStartIdentity  string
	StartedAt             string
	FinishedAt            string
	ExitCode              int
	TimedOut              bool
	Cancelled             bool
	Stdout                string
	Stderr                string
	StdoutSHA256          string
	StderrSHA256          string
	ObservedOutputSHA256  string
	ObservedOutputSize    int64
	boundInvocation       auditInvocation
	CommandDescriptorHash string
	SandboxProfileHash    string
	ProcessGroupGone      bool
	TimeoutEvidence       auditTimeoutEvidence
	GatewayRejectionDiag  string
	processWaiter         *auditProcessWaiter
	runtimeAuthorityLease *atomicRuntimeAuthorityLease
}

func prepareAuditInvocation(policy *executionPolicy, entry executionPolicyEntry, snapshot auditSnapshot, runID, sessionRunID string, resume auditResume) (auditInvocation, error) {
	if policy == nil || policy.namespaceAuthority == nil || !executionTaskIDPattern.MatchString(runID) || !executionTaskIDPattern.MatchString(sessionRunID) ||
		snapshot.RunRoot == "" || snapshot.SourceRoot == "" || snapshot.ArchiveSHA256 != entry.SourceArchiveSHA256 ||
		snapshot.GitCommit != entry.GitCommit || snapshot.GitTree != entry.GitTree || filepath.Dir(snapshot.RunRoot) != entry.WorkRoot ||
		filepath.Dir(snapshot.SourceRoot) != snapshot.RunRoot {
		return auditInvocation{}, authenticationError("audit invocation snapshot binding")
	}
	if err := policy.ValidateEffectBoundary(entry); err != nil {
		return auditInvocation{}, err
	}
	if resume.SessionUUID != "" && !auditSessionUUIDPattern.MatchString(resume.SessionUUID) {
		return auditInvocation{}, authenticationError("audit resume session UUID")
	}
	if resume.SynthesizeOnly && resume.SessionUUID == "" {
		return auditInvocation{}, authenticationError("synthesis resume requires session UUID")
	}
	promptDir := filepath.Join(entry.PromptRoot, runID)
	outputDir := filepath.Join(entry.OutputRoot, runID)
	sessionDir := filepath.Join(entry.SessionRoot, sessionRunID)
	temporaryDir := filepath.Join(entry.TemporaryRoot, runID)
	for root, directory := range map[string]string{
		entry.PromptRoot: promptDir, entry.OutputRoot: outputDir, entry.SessionRoot: sessionDir, entry.TemporaryRoot: temporaryDir,
	} {
		if filepath.Dir(directory) != root {
			return auditInvocation{}, authenticationError("audit invocation root binding")
		}
	}
	leases, err := newAuditNamespaceLeaseSet(policy.namespaceAuthority, entry.PromptRoot, entry.OutputRoot, entry.SessionRoot, entry.TemporaryRoot)
	if err != nil {
		return auditInvocation{}, err
	}
	owned := make([]auditOwnedRootIdentity, 0, 6)
	created := make([]auditOwnedRootIdentity, 0, 4)
	cleanup := true
	defer func() {
		if cleanup {
			if len(created) != 0 {
				_ = scrubAndRemoveAuthenticatedAuditRoots(policy.namespaceAuthority, created)
			}
			_ = leases.Close()
		}
	}()
	create := func(root, name, role string) (auditOwnedRootIdentity, error) {
		lease, err := leases.lease(root)
		if err != nil {
			return auditOwnedRootIdentity{}, err
		}
		if err := lease.Mkdir(name, 0o700); err != nil {
			if errors.Is(err, unix.EEXIST) {
				return auditOwnedRootIdentity{}, authenticationError("audit invocation root collision")
			}
			return auditOwnedRootIdentity{}, err
		}
		if err := policy.namespaceAuthority.prepareRuntimeOwnedChild(lease, name); err != nil {
			return auditOwnedRootIdentity{}, err
		}
		identity, err := lease.Capture(name, role, true)
		if err != nil {
			return auditOwnedRootIdentity{}, err
		}
		created = append(created, identity)
		return identity, nil
	}
	promptIdentity, err := create(entry.PromptRoot, runID, "prompt")
	if err != nil {
		return auditInvocation{}, err
	}
	outputIdentity, err := create(entry.OutputRoot, runID, "output")
	if err != nil {
		return auditInvocation{}, err
	}
	temporaryIdentity, err := create(entry.TemporaryRoot, runID, "temporary")
	if err != nil {
		return auditInvocation{}, err
	}
	var sessionIdentity auditOwnedRootIdentity
	if resume.SessionUUID == "" {
		sessionIdentity, err = create(entry.SessionRoot, sessionRunID, "session")
	} else {
		lease, leaseErr := leases.lease(entry.SessionRoot)
		if leaseErr != nil {
			return auditInvocation{}, leaseErr
		}
		sessionIdentity, err = lease.Capture(sessionRunID, "session", true)
	}
	if err != nil {
		return auditInvocation{}, authenticationError("stable audit resume session root")
	}
	owned = append(owned, promptIdentity, outputIdentity, temporaryIdentity, sessionIdentity)
	prompt := readOnlyAuditPromptTemplate
	if resume.SynthesizeOnly {
		prompt += readOnlyAuditSynthesizePromptSuffix
	}
	if err := policy.namespaceAuthority.writeOwnedFile(promptIdentity, "audit-prompt.txt", []byte(prompt), 0o400); err != nil {
		return auditInvocation{}, fmt.Errorf("write fixed audit prompt: %w", err)
	}
	promptPath := filepath.Join(promptDir, "audit-prompt.txt")
	outputPath := filepath.Join(outputDir, "audit-output.json")
	arguments, err := auditOMPArguments(entry, sessionDir, resume)
	if err != nil {
		return auditInvocation{}, err
	}
	promptHash := auditPromptSHA256(resume.SynthesizeOnly)
	descriptorHash, err := auditCommandDescriptorHash(entry, promptHash, sessionRunID, resume)
	if err != nil {
		return auditInvocation{}, err
	}
	cleanup = false
	return auditInvocation{
		RunID: runID, SessionRunID: sessionRunID, PromptDir: promptDir, PromptPath: promptPath, OutputDir: outputDir, OutputPath: outputPath,
		SessionDir: sessionDir, TemporaryDir: temporaryDir, WorkDir: snapshot.SourceRoot, OMPExecutablePath: entry.OMPExecutable.Path, Arguments: arguments,
		CommandDescriptorHash: descriptorHash, PromptSHA256: promptHash, Resume: resume, OwnedRoots: owned,
		sharedOwnedRoots:   append([]auditOwnedRootIdentity(nil), snapshot.OwnedRoots...),
		namespaceAuthority: policy.namespaceAuthority, namespaceLeases: leases,
	}, nil
}

func auditOMPArguments(entry executionPolicyEntry, sessionDir string, resume auditResume) ([]string, error) {
	if !validAuditModelRoute(entry) || sessionDir == "" || !filepath.IsAbs(sessionDir) || filepath.Clean(sessionDir) != sessionDir ||
		strings.IndexByte(sessionDir, 0) >= 0 || resume.SessionUUID != "" && !auditSessionUUIDPattern.MatchString(resume.SessionUUID) ||
		resume.SynthesizeOnly && resume.SessionUUID == "" {
		return nil, authenticationError("closed direct OMP argv policy")
	}
	prompt := readOnlyAuditPromptTemplate
	if resume.SynthesizeOnly {
		prompt += readOnlyAuditSynthesizePromptSuffix
	}
	arguments := []string{"-p", prompt, "--yolo", "--max-time", strconv.Itoa(entry.InternalDeadlineSeconds)}
	if resume.SessionUUID != "" {
		arguments = append(arguments, "--resume", resume.SessionUUID)
	}
	arguments = append(arguments,
		"--model", "sudo/gpt-5.6-sol",
		"--thinking", "xhigh",
		"--session-dir", sessionDir,
	)
	return arguments, nil
}

func fixedAuditGitEnvironmentBoundary(workDir string) (auditGitEnvironmentBoundary, error) {
	ceilingDirectory := filepath.Dir(workDir)
	if workDir == "" || !filepath.IsAbs(workDir) || filepath.Clean(workDir) != workDir || strings.IndexByte(workDir, 0) >= 0 ||
		filepath.Base(workDir) != "source" || ceilingDirectory == "/" || ceilingDirectory == "." {
		return auditGitEnvironmentBoundary{}, authenticationError("fixed Git environment boundary")
	}
	return auditGitEnvironmentBoundary{
		CeilingDirectories: ceilingDirectory,
		ConfigGlobal:       "/dev/null",
		ConfigNoSystem:     "1",
		Path:               auditChildExecutablePath,
	}, nil
}

func auditCommandDescriptorHash(entry executionPolicyEntry, promptHash, sessionRunID string, resume auditResume) (string, error) {
	sessionDir := filepath.Join(entry.SessionRoot, sessionRunID)
	workDir := filepath.Join(entry.WorkRoot, sessionRunID, "source")
	arguments, err := auditOMPArguments(entry, sessionDir, resume)
	gitEnvironment, gitEnvironmentErr := fixedAuditGitEnvironmentBoundary(workDir)
	if err != nil || gitEnvironmentErr != nil || promptHash != auditPromptSHA256(resume.SynthesizeOnly) {
		return "", authenticationError("direct OMP command descriptor")
	}
	return canonicalHash(map[string]any{
		"schema_version":                      auditCommandDescriptorSchemaVersion,
		"launcher_mode":                       atomicOMPLauncherModeDirectPinned,
		"omp_argv_policy":                     atomicOMPArgvPolicyExactSudoRoute,
		"omp_arguments":                       arguments,
		"direct_sandbox_target":               entry.OMPExecutable,
		"sandbox_target_policy":               atomicOMPSandboxTargetPolicyExactPinned,
		"git_executable":                      entry.GitExecutable,
		"git_environment":                     gitEnvironment,
		"git_repository_discovery_policy":     auditGitRepositoryDiscoveryPolicy,
		"internal_deadline_seconds":           entry.InternalDeadlineSeconds,
		"supervisor_hard_deadline_seconds":    entry.InternalDeadlineSeconds + entry.WrapperGraceSeconds,
		"timeout_owner":                       atomicOMPTimeoutOwnerSupervisor,
		"prompt_sha256":                       promptHash,
		"output_role":                         "audit_model_report",
		"output_transport":                    atomicOMPOutputTransportSupervisorStdout,
		"output_capture_limit_bytes":          maxAuditCaptureBytes,
		"provider":                            entry.HermesProvider,
		"provider_endpoint":                   entry.ProviderEndpoint,
		"model":                               entry.HermesModel,
		"task_tier":                           entry.TaskTier,
		"omp_model":                           "sudo/gpt-5.6-sol",
		"omp_thinking":                        "xhigh",
		"credential_projection":               map[string]string{"source": "SUDO_CODING_KEY", "runtime": "SUDO_API_KEY"},
		"omp_version":                         entry.OMPVersion,
		"omp_native_addon":                    entry.OMPNativeAddon,
		"omp_runtime_authority":               entry.OMPRuntimeAuthority,
		"session_role":                        "stable_isolated_session",
		"session_root_id":                     sessionRunID,
		"work_archive_sha256":                 entry.SourceArchiveSHA256,
		"resume_session_uuid":                 resume.SessionUUID,
		"synthesize_only":                     resume.SynthesizeOnly,
		"wrapper_compatibility_oracle_sha256": entry.Wrapper.SHA256,
	})
}

func auditModelsConfigBytes(entry executionPolicyEntry, gatewayAuthority string) ([]byte, error) {
	host, port, err := net.SplitHostPort(gatewayAuthority)
	parsedPort, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host != "127.0.0.1" || parsedPort < 1 || parsedPort > 65535 {
		return nil, authenticationError("audit models gateway authority")
	}
	if !validAuditModelDeclaration(entry) {
		return nil, authenticationError("audit models provider declaration")
	}
	return []byte("providers:\n" +
		"  sudo:\n" +
		"    baseUrl: http://" + gatewayAuthority + "/v1\n" +
		"    apiKey: SUDO_API_KEY\n" +
		"    api: openai-responses\n" +
		"    models:\n" +
		"    - id: gpt-5.6-sol\n" +
		"      name: Sudo GPT-5.6 Sol\n" +
		"      api: openai-responses\n" +
		"      reasoning: true\n" +
		"      input:\n" +
		"      - text\n" +
		"      contextWindow: 372000\n" +
		"      maxTokens: 65536\n" +
		"      omitMaxOutputTokens: true\n" +
		"      compat:\n" +
		"        supportsReasoningEffort: true\n" +
		"        supportsDeveloperRole: true\n"), nil
}

func writePrivateAuditFile(path string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(contents)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || written != len(contents) || syncErr != nil || closeErr != nil {
		return authenticationError("write private audit file")
	}
	return nil
}
func canonicalizeAuditCapturedOutput(contents []byte) []byte {
	trimmed := bytes.TrimSpace(contents)
	if len(trimmed) == 0 {
		return contents
	}
	normalized, err := decodeJSONValue(trimmed)
	if err != nil {
		return contents
	}
	var canonical bytes.Buffer
	if err := appendCanonicalValue(&canonical, normalized); err != nil || canonical.Len() == 0 || canonical.Len() > maxAuditCaptureBytes {
		return contents
	}
	return canonical.Bytes()
}

func writeAuditCapturedOutput(invocation auditInvocation, contents []byte) error {
	if invocation.namespaceAuthority == nil || len(contents) > maxAuditCaptureBytes {
		return ErrLimit
	}
	outputIdentity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "output")
	if !ok || outputIdentity.Path != invocation.OutputDir || invocation.OutputPath != filepath.Join(outputIdentity.Path, "audit-output.json") {
		return authenticationError("authenticated direct OMP output root")
	}
	if err := invocation.namespaceAuthority.writeOwnedFile(outputIdentity, "audit-output.json", contents, 0o600); err != nil {
		return authenticationError("publish authenticated direct OMP output")
	}
	information, err := os.Lstat(invocation.OutputPath)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() || information.Mode().Perm() != 0o600 {
		return authenticationError("private direct OMP output metadata")
	}
	observed, size, err := readAuditRegularFile(invocation.OutputPath, maxAuditCaptureBytes)
	if err != nil {
		return err
	}
	defer zeroBytes(observed)
	if size != int64(len(contents)) || !bytes.Equal(observed, contents) {
		return authenticationError("authenticated direct OMP output bytes")
	}
	return nil
}

func bindAuditInvocationTransport(entry executionPolicyEntry, invocation *auditInvocation, gatewayAuthority string) error {
	if invocation == nil || invocation.namespaceAuthority == nil || invocation.namespaceLeases == nil || len(invocation.OwnedRoots) != 4 ||
		invocation.TemporaryDir == "" || invocation.AgentDir != "" || invocation.ModelsPath != "" || invocation.HomeDir != "" ||
		invocation.HomeStateDir != "" || invocation.HomeRunDir != "" || invocation.NativeAddonPath != "" || invocation.GatewayAuthority != "" ||
		invocation.ModelsSHA256 != "" || invocation.ModelsSize != 0 || invocation.NativeAddonSHA256 != "" || invocation.NativeAddonSize != 0 ||
		invocation.modelsIdentity != (executionPolicyFileIdentity{}) || invocation.nativeAddonIdentity != (executionPolicyFileIdentity{}) {
		return authenticationError("unbound audit invocation transport")
	}
	modelsContents, err := auditModelsConfigBytes(entry, gatewayAuthority)
	if err != nil {
		return err
	}
	temporaryIdentity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "temporary")
	if !ok || temporaryIdentity.Path != invocation.TemporaryDir {
		return authenticationError("audit invocation temporary identity")
	}
	created := make([]auditOwnedRootIdentity, 0, 4)
	complete := false
	defer func() {
		if complete || len(created) == 0 {
			return
		}
		invocation.OwnedRoots = append(invocation.OwnedRoots, created...)
		all := append([]auditOwnedRootIdentity(nil), invocation.OwnedRoots...)
		_ = scrubAndRemoveAuthenticatedAuditRoots(invocation.namespaceAuthority, all)
	}()
	agentIdentity, err := invocation.namespaceAuthority.mkdirAndCaptureOwnedChild(temporaryIdentity, "omp-agent", "direct_omp_agent", false, true)
	if err != nil {
		return authenticationError("create audit invocation agent root")
	}
	created = append(created, agentIdentity)
	homeIdentity, err := invocation.namespaceAuthority.mkdirAndCaptureOwnedChild(temporaryIdentity, "home", "direct_omp_home", false, true)
	if err != nil {
		return authenticationError("create audit invocation HOME root")
	}
	created = append(created, homeIdentity)
	homeStateIdentity, err := invocation.namespaceAuthority.mkdirAndCaptureOwnedChild(homeIdentity, ".omp", "direct_omp_home_state", false, true, temporaryIdentity)
	if err != nil {
		return authenticationError("create audit invocation HOME state root")
	}
	created = append(created, homeStateIdentity)
	homeRunIdentity, err := invocation.namespaceAuthority.mkdirAndCaptureOwnedChild(homeStateIdentity, "run", "direct_omp_home_run", false, true, homeIdentity, temporaryIdentity)
	if err != nil {
		return authenticationError("create audit invocation HOME run root")
	}
	created = append(created, homeRunIdentity)
	homeStateIdentity, err = invocation.namespaceAuthority.sealAndRecaptureOwnedDirectory(homeStateIdentity, 0o500, homeIdentity, temporaryIdentity)
	if err != nil {
		return authenticationError("seal audit invocation HOME state root")
	}
	created[len(created)-2] = homeStateIdentity
	homeRunIdentity, err = invocation.namespaceAuthority.captureOwnedChild(homeStateIdentity, "run", "direct_omp_home_run", false, homeIdentity, temporaryIdentity)
	if err != nil {
		return authenticationError("rebind audit invocation HOME run root")
	}
	created[len(created)-1] = homeRunIdentity
	agentDir, homeDir := agentIdentity.Path, homeIdentity.Path
	homeStateDir, homeRunDir := homeStateIdentity.Path, homeRunIdentity.Path
	modelsPath := filepath.Join(agentDir, "models.yml")
	if err := invocation.namespaceAuthority.writeOwnedFile(agentIdentity, "models.yml", modelsContents, 0o600, temporaryIdentity); err != nil {
		return authenticationError("create audit models configuration")
	}
	modelsRead, modelsIdentity, err := readPinnedRegularFile(modelsPath, agentIdentity.OwnerUID, true, maxAuditModelsConfigBytes)
	if err != nil {
		return authenticationError("pin audit models configuration")
	}
	defer zeroBytes(modelsRead)
	if !bytes.Equal(modelsRead, modelsContents) {
		return authenticationError("canonical audit models configuration")
	}
	if err := validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, 0); err != nil {
		return authenticationError("pinned immutable OMP native addon")
	}
	commandHash, err := auditCommandDescriptorHash(entry, invocation.PromptSHA256, invocation.SessionRunID, invocation.Resume)
	if err != nil || commandHash != invocation.CommandDescriptorHash {
		return authenticationError("stable audit invocation command descriptor")
	}
	invocation.AgentDir, invocation.ModelsPath = agentDir, modelsPath
	invocation.HomeDir, invocation.HomeStateDir, invocation.HomeRunDir = homeDir, homeStateDir, homeRunDir
	invocation.NativeAddonPath, invocation.GatewayAuthority = entry.OMPNativeAddon.Path, gatewayAuthority
	invocation.ModelsSHA256, invocation.ModelsSize = modelsIdentity.SHA256, modelsIdentity.Size
	invocation.NativeAddonSHA256, invocation.NativeAddonSize = entry.OMPNativeAddon.SHA256, entry.OMPNativeAddon.Size
	invocation.modelsIdentity, invocation.nativeAddonIdentity = modelsIdentity, entry.OMPNativeAddon
	invocation.OwnedRoots = append(invocation.OwnedRoots, created...)
	complete = true
	return nil
}

func validateAuditInvocationTransport(entry executionPolicyEntry, invocation auditInvocation) error {
	if invocation.namespaceAuthority == nil || len(invocation.OwnedRoots) != 8 ||
		invocation.AgentDir != filepath.Join(invocation.TemporaryDir, "omp-agent") ||
		invocation.ModelsPath != filepath.Join(invocation.AgentDir, "models.yml") ||
		invocation.HomeDir != filepath.Join(invocation.TemporaryDir, "home") ||
		invocation.HomeStateDir != filepath.Join(invocation.HomeDir, ".omp") ||
		invocation.HomeRunDir != filepath.Join(invocation.HomeStateDir, "run") ||
		invocation.OMPExecutablePath != entry.OMPExecutable.Path || invocation.NativeAddonPath != entry.OMPNativeAddon.Path ||
		!protocolHashPattern.MatchString(invocation.ModelsSHA256) || invocation.ModelsSize < 1 || invocation.ModelsSize > maxAuditModelsConfigBytes ||
		invocation.modelsIdentity.Path != invocation.ModelsPath || invocation.modelsIdentity.SHA256 != invocation.ModelsSHA256 ||
		invocation.modelsIdentity.Size != invocation.ModelsSize || invocation.modelsIdentity.Mode != 0o600 ||
		invocation.NativeAddonSHA256 != entry.OMPNativeAddon.SHA256 || invocation.NativeAddonSize != entry.OMPNativeAddon.Size ||
		invocation.nativeAddonIdentity != entry.OMPNativeAddon {
		return authenticationError("audit invocation transport descriptor")
	}
	currentRoots, err := currentAuditInvocationOwnedRoots(invocation)
	if err != nil {
		return err
	}
	if err := validateAuditOwnedRootsPresent(invocation.namespaceAuthority, currentRoots); err != nil {
		return err
	}
	if err := validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, 0); err != nil {
		return err
	}
	expected, err := auditModelsConfigBytes(entry, invocation.GatewayAuthority)
	if err != nil || hashJournalBytes(expected) != invocation.ModelsSHA256 || int64(len(expected)) != invocation.ModelsSize {
		return authenticationError("audit invocation models descriptor")
	}
	contents, actual, err := readPinnedRegularFile(invocation.ModelsPath, invocation.modelsIdentity.OwnerUID, true, maxAuditModelsConfigBytes)
	if err != nil {
		return authenticationError("audit invocation models replacement")
	}
	defer zeroBytes(contents)
	if actual != invocation.modelsIdentity || !bytes.Equal(contents, expected) {
		return authenticationError("audit invocation models replacement")
	}
	want, err := auditCommandDescriptorHash(entry, invocation.PromptSHA256, invocation.SessionRunID, invocation.Resume)
	if err != nil || want != invocation.CommandDescriptorHash {
		return authenticationError("audit invocation command descriptor")
	}
	return nil
}

func auditOwnedRootIdentityForRole(identities []auditOwnedRootIdentity, role string) (auditOwnedRootIdentity, bool) {
	for _, identity := range identities {
		if identity.Role == role {
			return identity, true
		}
	}
	return auditOwnedRootIdentity{}, false
}

func currentAuditInvocationOwnedRoots(invocation auditInvocation) ([]auditOwnedRootIdentity, error) {
	expected := []struct {
		role string
		path string
		mode uint32
	}{
		{role: "prompt", path: invocation.PromptDir, mode: 0o700},
		{role: "output", path: invocation.OutputDir, mode: 0o700},
		{role: "temporary", path: invocation.TemporaryDir, mode: 0o700},
		{role: "session", path: invocation.SessionDir, mode: 0o700},
		{role: "direct_omp_agent", path: invocation.AgentDir, mode: 0o700},
		{role: "direct_omp_home", path: invocation.HomeDir, mode: 0o700},
		{role: "direct_omp_home_state", path: invocation.HomeStateDir, mode: 0o500},
		{role: "direct_omp_home_run", path: invocation.HomeRunDir, mode: 0o700},
	}
	if len(invocation.OwnedRoots) != len(expected) {
		return nil, authenticationError("exact audit invocation owned root inventory")
	}
	current := make([]auditOwnedRootIdentity, 0, len(expected))
	for index, want := range expected {
		identity := invocation.OwnedRoots[index]
		if !validAuditOwnedRootIdentity(identity) || identity.Role != want.role || identity.Path != want.path || identity.Mode&0o777 != want.mode {
			return nil, authenticationError("audit invocation owned root order role path and mode")
		}
		current = append(current, identity)
	}
	return current, nil
}

func validateAuditOwnedRootsPresent(authority *auditNamespaceAuthority, identities []auditOwnedRootIdentity) error {
	byPath := make(map[string]auditOwnedRootIdentity, len(identities))
	for _, identity := range identities {
		if !validAuditOwnedRootIdentity(identity) {
			return authenticationError("audit owned root identity")
		}
		byPath[identity.Path] = identity
	}
	for _, identity := range identities {
		descriptor, present, err := authority.openAuthenticatedOwnedRoot(identity, byPath)
		if err != nil || !present {
			if err != nil {
				return err
			}
			return authenticationError("audit owned root absent")
		}
		if err := unix.Close(descriptor); err != nil {
			return authenticationError("close audit owned root descriptor")
		}
	}
	return nil
}

func informationSyscallStat(information os.FileInfo) (*syscall.Stat_t, bool) {
	if information == nil {
		return nil, false
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	return status, ok
}

func runAuditInvocation(ctx context.Context, policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation, hooks auditInvocationHooks) (result auditInvocationResult, returnedErr error) {
	if ctx == nil || policy == nil || !auditPlatformSupported(runtime.GOOS) {
		return auditInvocationResult{}, failAuditInvocationStage("invocation_context_unavailable", authenticationError("production audit sandbox unavailable"))
	}
	if err := validateAuditInvocation(policy, entry, invocation); err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("invocation_validation_failed", err)
	}
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			if err := cleanupAuthenticatedAuditInvocationAll(entry, invocation); err != nil && returnedErr == nil {
				returnedErr = failAuditInvocationStage("artifact_cleanup_failed", authenticationError("scrub audit invocation trees"))
			}
		}
	}()
	if err := policy.ValidateEffectBoundary(entry); err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("effect_boundary_verification_failed", err)
	}
	frozenWrapper, err := freezeAuditWrapper(entry.Wrapper)
	if err != nil || hashJournalBytes(frozenWrapper) != entry.Wrapper.SHA256 {
		zeroBytes(frozenWrapper)
		return auditInvocationResult{}, failAuditInvocationStage("wrapper_compatibility_oracle_verification_failed", authenticationError("frozen wrapper compatibility oracle"))
	}
	defer zeroBytes(frozenWrapper)
	authorityLease, err := runtimeAuthorityVerifier(policy).Verify(entry, frozenWrapper)
	if err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("runtime_authority_verification_failed", err)
	}
	if authorityLease == nil {
		return auditInvocationResult{}, failAuditInvocationStage("runtime_authority_verification_failed", unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryAuthorityBinding))
	}
	defer func() {
		if result.processWaiter != nil {
			result.runtimeAuthorityLease = authorityLease
			return
		}
		if err := authorityLease.Close(); err != nil && returnedErr == nil {
			returnedErr = failAuditInvocationStage("runtime_authority_verification_failed", unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryIdentityChanged))
		}
	}()
	if err := validateRootOwnedSystemExecutable(auditSandboxExecutable); err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("sandbox_runtime_verification_failed", err)
	}
	gatewayLifetime := time.Duration(entry.InternalDeadlineSeconds+entry.WrapperGraceSeconds) * time.Second
	gateway, err := startAuditHTTPGateway(ctx, entry.HermesProvider, entry.ProviderEndpoint, gatewayLifetime, policy.testBrokerDependencies)
	if err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("provider_gateway_setup_failed", err)
	}
	defer func() {
		if err := gateway.Close(); err != nil && returnedErr == nil {
			returnedErr = failAuditInvocationStage("provider_gateway_cleanup_failed", authenticationError("close audit HTTP gateway"))
		}
	}()
	if err := bindAuditInvocationTransport(entry, &invocation, gateway.Address()); err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("transport_binding_failed", err)
	}
	if err := validateAuditInvocationTransport(entry, invocation); err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("transport_binding_failed", err)
	}
	sandboxGatewayAuthority, err := auditSandboxBrokerAddress(gateway.Address())
	if err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("transport_binding_failed", err)
	}
	invocation.SandboxProfile = auditSandboxProfile(policy, entry, invocation.WorkDir, invocation.PromptDir, invocation.OutputDir, invocation.SessionDir, invocation.TemporaryDir, invocation.NativeAddonPath, sandboxGatewayAuthority)
	invocation.SandboxProfileHash = hashJournalBytes([]byte(invocation.SandboxProfile))
	if hooks.BrokerReady != nil {
		hooks.BrokerReady(gateway.Address(), invocation.SandboxProfile)
	}
	if hooks.TransportReady != nil {
		if err := hooks.TransportReady(invocation); err != nil {
			return auditInvocationResult{}, failAuditInvocationStage("transport_binding_failed", err)
		}
	}
	if hooks.BeforeStart != nil {
		hooks.BeforeStart()
	}
	if err := policy.ValidateEffectBoundaryWithFrozenWrapper(entry, frozenWrapper); err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("effect_boundary_verification_failed", err)
	}
	if err := validateAuditInvocationTransport(entry, invocation); err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("transport_binding_failed", err)
	}
	sessionPathsBefore, err := snapshotAuditSessionPaths(invocation)
	if err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("session_snapshot_failed", err)
	}
	credentialLookup := hooks.CredentialLookup
	if credentialLookup == nil {
		credentialLookup = os.LookupEnv
	}
	environment, credentialValues, err := auditInvocationEnvironmentWithLookup(entry, invocation, credentialLookup)
	if err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("credential_binding_failed", err)
	}
	commandArguments := auditSandboxCommandArguments(invocation)
	command := exec.Command(auditSandboxExecutable, commandArguments...)
	command.Dir = invocation.WorkDir
	command.Env = environment
	childCredential, err := policy.namespaceAuthority.ChildCredential()
	if err != nil {
		return auditInvocationResult{}, failAuditInvocationStage("command_identity_setup_failed", err)
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: childCredential}
	stdout := &boundedCommandBuffer{limit: maxAuditCaptureBytes}
	stderr := &boundedCommandBuffer{limit: maxAuditCaptureBytes}
	command.Stdout, command.Stderr = stdout, stderr
	operations := hooks.ProcessOperations
	if operations == nil {
		operations = systemAuditProcessOperations{}
	}
	bounds, err := resolveAuditTerminationBounds(hooks.TerminationBounds)
	if err != nil {
		return auditInvocationResult{}, err
	}
	releaseStart := func() {}
	if hooks.StartGate != nil {
		var gateErr error
		releaseStart, gateErr = hooks.StartGate()
		if gateErr != nil || releaseStart == nil {
			stdout.zero()
			stderr.zero()
			if gateErr == nil {
				gateErr = ErrProtocol
			}
			return auditInvocationResult{}, failAuditInvocationStage("start_gate_failed", gateErr)
		}
	}
	startedAt := time.Now().UTC()
	startCommand := hooks.StartCommand
	if startCommand == nil {
		startCommand = startAuditCommand
	}
	if command.Stdin != nil || len(command.ExtraFiles) != 0 {
		releaseStart()
		stdout.zero()
		stderr.zero()
		return auditInvocationResult{}, failAuditInvocationStage("command_transport_setup_failed", authenticationError("direct OMP command descriptor transport"))
	}
	if err := startCommand(command, nil, nil); err != nil {
		releaseStart()
		stdout.zero()
		stderr.zero()
		return auditInvocationResult{}, failAuditInvocationStage("command_start_failed", authenticationError("start sandboxed direct OMP"))
	}
	if command.Process == nil {
		releaseStart()
		stdout.zero()
		stderr.zero()
		return auditInvocationResult{}, failAuditInvocationStage("command_start_failed", authenticationError("start sandboxed direct OMP"))
	}
	waiter := startAuditProcessWaiter(command)
	pid := command.Process.Pid
	identity, err := inspectAuditProcess(pid)
	if err != nil {
		releaseStart()
		_ = command.Process.Kill()
		cleanupNeeded = false
		result = auditInvocationResult{PID: pid, PGID: pid, StartedAt: startedAt.Format(time.RFC3339Nano), processWaiter: waiter}
		return result, failAuditInvocationStage("process_identity_capture_failed", authenticationError("capture direct OMP process start identity"))
	}
	identity.StartedAt = startedAt.Format(time.RFC3339Nano)
	result = auditInvocationResult{
		PID: identity.PID, PGID: identity.PGID, ProcessStartIdentity: identity.ProcessStartIdentity, StartedAt: identity.StartedAt,
		CommandDescriptorHash: invocation.CommandDescriptorHash, SandboxProfileHash: invocation.SandboxProfileHash, boundInvocation: invocation,
	}
	if err := validateAuditInvocationTransport(entry, invocation); err != nil {
		releaseStart()
		termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
		if termination.Outcome != auditTerminationConfirmedExit {
			cleanupNeeded = false
			result.processWaiter = waiter
			return result, termination.Failure
		}
		result.ProcessGroupGone = true
		stdout.zero()
		stderr.zero()
		return result, failAuditInvocationStage("transport_binding_failed", err)
	}
	if hooks.AfterStart != nil {
		if err := hooks.AfterStart(identity); err != nil {
			releaseStart()
			termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
			if termination.Outcome != auditTerminationConfirmedExit {
				cleanupNeeded = false
				result.processWaiter = waiter
				return result, termination.Failure
			}
			result.ProcessGroupGone = true
			stdout.zero()
			stderr.zero()
			return result, failAuditInvocationStage("running_identity_persistence_failed", err)
		}
	}
	releaseStart()
	hardDeadline := time.NewTimer(time.Duration(entry.InternalDeadlineSeconds+entry.WrapperGraceSeconds) * time.Second)
	defer hardDeadline.Stop()
	hardTimeout := hardDeadline.C
	if hooks.HardTimeout != nil {
		hardTimeout = hooks.HardTimeout
	}
	var waitErr error
	select {
	case <-waiter.done:
		waitErr = waiter.result()
	case <-hardTimeout:
		termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
		if termination.Outcome != auditTerminationConfirmedExit {
			cleanupNeeded = false
			result.processWaiter = waiter
			return result, termination.Failure
		}
		result.ProcessGroupGone = true
		result.TimedOut = true
	case <-ctx.Done():
		termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
		if termination.Outcome != auditTerminationConfirmedExit {
			cleanupNeeded = false
			result.processWaiter = waiter
			return result, termination.Failure
		}
		result.ProcessGroupGone = true
		result.Cancelled = true
	}
	if !result.ProcessGroupGone {
		confirmation := confirmAuditProcessExit(context.Background(), identity, waiter, operations, bounds)
		if confirmation.Outcome != auditTerminationConfirmedExit {
			cleanupNeeded = false
			result.processWaiter = waiter
			return result, confirmation.Failure
		}
		result.ProcessGroupGone = true
	}
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	} else {
		result.ExitCode = -1
	}
	stdoutBytes := stdout.take()
	stderrBytes := stderr.take()
	defer zeroBytes(stdoutBytes)
	defer zeroBytes(stderrBytes)
	if stdout.err != nil || stderr.err != nil {
		return result, ErrLimit
	}
	for _, credential := range credentialValues {
		if credential != "" && (bytes.Contains(stdoutBytes, []byte(credential)) || bytes.Contains(stderrBytes, []byte(credential))) {
			return result, authenticationError("credential leaked by direct OMP")
		}
	}
	result.Stdout = string(stdoutBytes)
	result.Stderr = string(stderrBytes)
	result.StdoutSHA256 = hashJournalBytes(stdoutBytes)
	result.StderrSHA256 = hashJournalBytes(stderrBytes)
	if err := policy.ValidateEffectBoundaryWithFrozenWrapper(entry, frozenWrapper); err != nil {
		return result, err
	}
	if err := validateAuditInvocationTransport(entry, invocation); err != nil {
		return result, err
	}
	if result.Cancelled {
		return result, nil
	}
	timeoutSource := ""
	if result.TimedOut {
		timeoutSource = auditTimeoutSourceSupervisorHardDeadline
	} else if result.ExitCode != 0 && captureContainsDeadlineExceeded(stdoutBytes, stderrBytes) {
		timeoutSource = auditTimeoutSourceOMPInternalDeadline
	}
	if timeoutSource != "" {
		evidence, err := buildAuditTimeoutEvidence(entry, invocation, result, timeoutSource, sessionPathsBefore)
		if err != nil {
			return result, fmt.Errorf("%w: %v", errAuditMalformedTimeoutEvidence, err)
		}
		result.TimeoutEvidence = evidence
		if err := scanAuditInvocationTimeoutTrees(policy, entry, invocation, evidence); err != nil {
			return result, err
		}
		if err := cleanupAuditInvocationTransient(entry, invocation); err != nil {
			return result, authenticationError("scrub audit timeout transient trees")
		}
		cleanupNeeded = false
		return result, nil
	}
	captured := canonicalizeAuditCapturedOutput(stdoutBytes)
	if err := writeAuditCapturedOutput(invocation, captured); err != nil {
		return result, err
	}
	result.ObservedOutputSHA256 = hashJournalBytes(captured)
	result.ObservedOutputSize = int64(len(captured))
	if hooks.BeforeArtifactScan != nil {
		hooks.BeforeArtifactScan(invocation)
	}
	if err := scanAuditInvocationWritableTrees(policy, entry, invocation); err != nil {
		return result, failAuditArtifactScanStage(err)
	}
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			return result, authenticationError("wait sandboxed direct OMP")
		}
	}
	if result.ExitCode == 0 {
		cleanupNeeded = false
	}
	result.GatewayRejectionDiag = gateway.RejectionDiagnostic()
	return result, nil
}

func validateAuditInvocation(policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation) error {
	if !executionTaskIDPattern.MatchString(invocation.RunID) || !executionTaskIDPattern.MatchString(invocation.SessionRunID) ||
		filepath.Dir(invocation.PromptDir) != entry.PromptRoot || filepath.Dir(invocation.OutputDir) != entry.OutputRoot ||
		filepath.Dir(invocation.SessionDir) != entry.SessionRoot || filepath.Base(invocation.SessionDir) != invocation.SessionRunID ||
		filepath.Dir(invocation.TemporaryDir) != entry.TemporaryRoot || filepath.Dir(invocation.WorkDir) == "" ||
		filepath.Dir(invocation.PromptPath) != invocation.PromptDir || filepath.Dir(invocation.OutputPath) != invocation.OutputDir ||
		filepath.Base(invocation.OutputPath) != "audit-output.json" || invocation.OMPExecutablePath != entry.OMPExecutable.Path ||
		!protocolHashPattern.MatchString(invocation.CommandDescriptorHash) || !protocolHashPattern.MatchString(invocation.PromptSHA256) ||
		invocation.SandboxProfile != "" || invocation.SandboxProfileHash != "" || policy == nil {
		return authenticationError("audit invocation descriptor")
	}
	wantDescriptor, err := auditCommandDescriptorHash(entry, invocation.PromptSHA256, invocation.SessionRunID, invocation.Resume)
	if err != nil || wantDescriptor != invocation.CommandDescriptorHash {
		return authenticationError("audit invocation command descriptor")
	}
	wantArguments, err := auditOMPArguments(entry, invocation.SessionDir, invocation.Resume)
	if err != nil || !reflectStringSlicesEqual(wantArguments, invocation.Arguments) {
		return authenticationError("audit invocation direct OMP argv")
	}
	runRoot := filepath.Dir(invocation.WorkDir)
	if filepath.Base(invocation.WorkDir) != "source" || filepath.Dir(runRoot) != entry.WorkRoot {
		return authenticationError("audit invocation work root")
	}
	for _, directory := range []string{invocation.PromptDir, invocation.OutputDir, invocation.SessionDir, invocation.TemporaryDir} {
		information, err := os.Lstat(directory)
		if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() || information.Mode().Perm() != 0o700 {
			return authenticationError("audit invocation isolated root")
		}
	}
	prompt, err := os.ReadFile(invocation.PromptPath)
	if err != nil || hashJournalBytes(prompt) != invocation.PromptSHA256 || len(invocation.Arguments) < 2 || string(prompt) != invocation.Arguments[1] {
		return authenticationError("audit invocation prompt")
	}
	if _, err := os.Lstat(invocation.OutputPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return authenticationError("audit invocation output collision")
	}
	return nil
}

func auditSandboxBrokerAddress(brokerAddress string) (string, error) {
	host, port, err := net.SplitHostPort(brokerAddress)
	parsedHost := net.ParseIP(host)
	parsedPort, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || parsedHost == nil || !parsedHost.IsLoopback() || parsedHost.String() != "127.0.0.1" || parsedPort < 1 || parsedPort > 65535 {
		return "", authenticationError("audit CONNECT broker sandbox address")
	}
	return net.JoinHostPort("localhost", port), nil
}

func auditInvocationEnvironment(entry executionPolicyEntry, invocation auditInvocation) ([]string, []string, error) {
	return auditInvocationEnvironmentWithLookup(entry, invocation, os.LookupEnv)
}

func auditInvocationEnvironmentWithLookup(entry executionPolicyEntry, invocation auditInvocation, lookup func(string) (string, bool)) ([]string, []string, error) {
	if lookup == nil {
		return nil, nil, authenticationError("audit invocation credential lookup")
	}
	if err := validateAuditInvocationTransport(entry, invocation); err != nil {
		return nil, nil, err
	}
	gitEnvironment, err := fixedAuditGitEnvironmentBoundary(invocation.WorkDir)
	if err != nil {
		return nil, nil, err
	}
	environment := []string{
		"HOME=" + invocation.HomeDir,
		"GIT_CEILING_DIRECTORIES=" + gitEnvironment.CeilingDirectories,
		"GIT_CONFIG_GLOBAL=" + gitEnvironment.ConfigGlobal,
		"GIT_CONFIG_NOSYSTEM=" + gitEnvironment.ConfigNoSystem,
		"LANG=C",
		"LC_ALL=C",
		"OMP_SESSION_ROOT=" + invocation.SessionDir,
		"PATH=" + gitEnvironment.Path,
		"PI_CODING_AGENT_DIR=" + invocation.AgentDir,
		"PI_CONFIG_DIR=.omp/run",
		"TMPDIR=" + invocation.TemporaryDir,
		"TZ=UTC",
		"XDG_CACHE_HOME=" + invocation.HomeRunDir,
		"XDG_DATA_HOME=" + entry.OMPRuntimeAuthority.NativeDataRoot,
	}
	credentials := make([]string, 0, 1)
	value, exists := lookup("SUDO_CODING_KEY")
	if !exists || value == "" || len(entry.CredentialEnvironmentNames) != 1 || entry.CredentialEnvironmentNames[0] != "SUDO_CODING_KEY" {
		return nil, nil, authenticationError("direct OMP credential projection")
	}
	environment = append(environment, "SUDO_API_KEY="+value)
	credentials = append(credentials, value)
	sort.Strings(environment)
	return environment, credentials, nil
}

func reflectStringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

const maxAuditVerificationOutputBytes = 64 * 1024

func runSupervisorAuditTests(
	ctx context.Context,
	policy *executionPolicy,
	entry executionPolicyEntry,
	snapshot auditSnapshot,
	invocation auditInvocation,
	hooks auditSupervisorTestHooks,
) ([]auditEvidenceTest, error) {
	if ctx == nil || policy == nil || ctx.Err() != nil || snapshot.SourceRoot != invocation.WorkDir ||
		snapshot.ArchiveSHA256 != entry.SourceArchiveSHA256 || snapshot.GitCommit != entry.GitCommit || snapshot.GitTree != entry.GitTree {
		return nil, authenticationError("supervisor audit test binding")
	}
	if err := policy.ValidateEffectBoundary(entry); err != nil {
		return nil, err
	}
	operations := hooks.ProcessOperations
	if operations == nil {
		operations = systemAuditProcessOperations{}
	}
	bounds, err := resolveAuditTerminationBounds(hooks.TerminationBounds)
	if err != nil {
		return nil, err
	}
	verificationRoot := filepath.Join(invocation.TemporaryDir, "supervisor_tests")
	if filepath.Dir(verificationRoot) != invocation.TemporaryDir {
		return nil, authenticationError("supervisor audit test root")
	}
	if _, err := os.Lstat(verificationRoot); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nil, authenticationError("supervisor audit test root collision")
	}
	if err := os.Mkdir(verificationRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create supervisor audit test root: %w", err)
	}
	cleanupVerification := true
	defer func() {
		if cleanupVerification {
			_ = scrubAndRemoveAuditTree(verificationRoot)
		}
	}()
	profile := auditVerificationSandboxProfile(policy, entry, snapshot.SourceRoot, verificationRoot)
	profileHash := hashJournalBytes([]byte(profile))
	if err := validateRootOwnedSystemExecutable(auditSandboxExecutable); err != nil {
		return nil, err
	}
	results := make([]auditEvidenceTest, 0, len(entry.AllowedTests))
	for _, test := range entry.AllowedTests {
		if err := policy.ValidateEffectBoundary(entry); err != nil {
			return nil, err
		}
		if !validSealedExecutionPolicyTestCommand(test) || validateFileIdentity(test.Executable, false, policy.ownerUID) != nil {
			return nil, authenticationError("pinned supervisor audit test command")
		}
		commandArguments := append([]string{"-p", profile, test.Executable.Path}, test.Arguments...)
		command := exec.Command(auditSandboxExecutable, commandArguments...)
		command.Dir = snapshot.SourceRoot
		command.Env = []string{
			"GOCACHE=off", "HOME=/var/empty", "LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin",
			"TMPDIR=" + verificationRoot, "TZ=UTC", "XDG_CACHE_HOME=" + verificationRoot,
		}
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdout := &boundedCommandBuffer{limit: maxAuditVerificationOutputBytes}
		stderr := &boundedCommandBuffer{limit: maxAuditVerificationOutputBytes}
		command.Stdout, command.Stderr = stdout, stderr
		releaseStart := func() {}
		if hooks.StartGate != nil {
			var gateErr error
			releaseStart, gateErr = hooks.StartGate()
			if gateErr != nil {
				stdout.zero()
				stderr.zero()
				return nil, gateErr
			}
			if releaseStart == nil {
				stdout.zero()
				stderr.zero()
				return nil, ErrProtocol
			}
		}
		startedAt := time.Now().UTC()
		if err := command.Start(); err != nil {
			releaseStart()
			stdout.zero()
			stderr.zero()
			return nil, authenticationError("start supervisor audit test")
		}
		waiter := startAuditProcessWaiter(command)
		pid := command.Process.Pid
		identity, exists, inspectErr := operations.Inspect(pid)
		if inspectErr != nil || !exists || identity.PID != pid || identity.PGID != pid || identity.ProcessStartIdentity == "" {
			releaseStart()
			cleanupVerification = false
			return nil, &auditOwnedProcessError{
				identity: auditProcessIdentity{PID: pid, PGID: pid, StartedAt: startedAt.Format(time.RFC3339Nano)}, waiter: waiter,
				failure: &auditTerminationError{FailureClass: "process_identity_unavailable", Cause: authenticationError("capture supervisor audit test process identity")},
			}
		}
		identity.StartedAt = startedAt.Format(time.RFC3339Nano)
		processResult := auditInvocationResult{
			PID: identity.PID, PGID: identity.PGID, ProcessStartIdentity: identity.ProcessStartIdentity,
			StartedAt: identity.StartedAt, ExitCode: -1,
		}
		finish := func() {
			if hooks.AfterFinish != nil {
				hooks.AfterFinish(identity)
			}
		}
		if hooks.AfterStart != nil {
			if err := hooks.AfterStart(identity); err != nil {
				releaseStart()
				termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
				if termination.Outcome != auditTerminationConfirmedExit {
					cleanupVerification = false
					return nil, &auditOwnedProcessError{identity: identity, waiter: waiter, failure: termination.Failure}
				}
				stdout.zero()
				stderr.zero()
				processResult.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
				finish()
				return nil, &auditSupervisorTestError{result: processResult, cause: err}
			}
		}
		releaseStart()
		timer := time.NewTimer(time.Duration(test.TimeoutSeconds) * time.Second)
		hardTimeout := timer.C
		if hooks.HardTimeout != nil {
			hardTimeout = hooks.HardTimeout
		}
		var waitErr error
		var lifecycleErr error
		select {
		case <-waiter.done:
			waitErr = waiter.result()
			confirmation := confirmAuditProcessExit(context.Background(), identity, waiter, operations, bounds)
			if confirmation.Outcome != auditTerminationConfirmedExit {
				lifecycleErr = confirmation.Failure
			}
		case <-hardTimeout:
			termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
			if termination.Outcome != auditTerminationConfirmedExit {
				lifecycleErr = termination.Failure
			} else {
				lifecycleErr = ErrDeadline
			}
		case <-ctx.Done():
			termination := terminateOwnedAuditProcess(context.Background(), identity, waiter, operations, bounds)
			if termination.Outcome != auditTerminationConfirmedExit {
				lifecycleErr = termination.Failure
			} else {
				lifecycleErr = ctx.Err()
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if lifecycleErr != nil {
			var termination *auditTerminationError
			if errors.As(lifecycleErr, &termination) {
				cleanupVerification = false
				return nil, &auditOwnedProcessError{identity: identity, waiter: waiter, failure: lifecycleErr}
			}
			stdout.zero()
			stderr.zero()
			processResult.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if command.ProcessState != nil {
				processResult.ExitCode = command.ProcessState.ExitCode()
			}
			finish()
			return nil, &auditSupervisorTestError{result: processResult, cause: lifecycleErr}
		}
		processResult.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if command.ProcessState != nil {
			processResult.ExitCode = command.ProcessState.ExitCode()
		}
		finish()
		if stdout.err != nil || stderr.err != nil {
			stdout.zero()
			stderr.zero()
			return nil, &auditSupervisorTestError{result: processResult, cause: authenticationError("bounded supervisor audit test")}
		}
		stdoutBytes, stderrBytes := stdout.take(), stderr.take()
		result := auditEvidenceTest{
			ID: test.ID, CommandSHA256: test.CommandSHA256, ExitCode: processResult.ExitCode, SandboxProfileSHA256: profileHash,
			StdoutSHA256: hashJournalBytes(stdoutBytes), StdoutSize: int64(len(stdoutBytes)),
			StderrSHA256: hashJournalBytes(stderrBytes), StderrSize: int64(len(stderrBytes)),
		}
		if waitErr != nil || processResult.ExitCode != 0 {
			zeroBytes(stdoutBytes)
			zeroBytes(stderrBytes)
			return nil, &auditSupervisorTestError{result: processResult, cause: authenticationError("supervisor audit test failed")}
		}
		zeroBytes(stdoutBytes)
		zeroBytes(stderrBytes)
		results = append(results, result)
	}
	return results, nil
}

func auditVerificationSandboxProfile(policy *executionPolicy, entry executionPolicyEntry, sourceRoot, temporaryRoot string) string {
	var profile strings.Builder
	profile.WriteString("(version 1)\n(deny default)\n")
	profile.WriteString("(deny process-info* (target others))\n(deny signal (target others))\n(deny network-outbound)\n")
	if policy != nil && len(policy.protectedPaths) != 0 {
		profile.WriteString("(deny file-read*")
		for _, protected := range sandboxPathVariants(policy.protectedPaths...) {
			writeSandboxFilter(&profile, "literal", protected)
		}
		profile.WriteString(")\n")
	}
	profile.WriteString("(allow process-fork)\n(allow process-info* (target self) (target children))\n(allow signal (target self) (target children))\n")
	profile.WriteString("(allow sysctl-read (sysctl-name \"security.mac.lockdown_mode_state\" \"kern.bootargs\"))\n")
	readRoots := []string{sourceRoot, temporaryRoot}
	for _, identity := range entry.RuntimeReadRoots {
		readRoots = append(readRoots, identity.Path)
	}
	for _, test := range entry.AllowedTests {
		readRoots = append(readRoots, test.ExecutableRoot.Path)
	}
	profile.WriteString("(allow file-read-metadata file-test-existence (literal \"/\")")
	for _, root := range sandboxPathVariants(readRoots...) {
		writeSandboxFilter(&profile, "path-ancestors", root)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-read* file-test-existence (literal \"/\") (literal \"/dev/null\") (literal \"/dev/zero\")")
	for _, root := range sandboxPathVariants(readRoots...) {
		writeSandboxFilter(&profile, "subpath", root)
		writeSandboxFilter(&profile, "literal", root)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-map-executable (subpath \"/usr/lib\") (subpath \"/System/Library\") (subpath \"/Library/Apple\")")
	for _, test := range entry.AllowedTests {
		for _, root := range sandboxPathVariants(test.ExecutableRoot.Path) {
			writeSandboxFilter(&profile, "subpath", root)
		}
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-write* (literal \"/dev/null\")")
	for _, root := range sandboxPathVariants(temporaryRoot) {
		writeSandboxFilter(&profile, "subpath", root)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow process-exec")
	for _, test := range entry.AllowedTests {
		for _, root := range sandboxPathVariants(test.ExecutableRoot.Path) {
			writeSandboxFilter(&profile, "subpath", root)
		}
		for _, executable := range sandboxPathVariants(test.Executable.Path) {
			writeSandboxFilter(&profile, "literal", executable)
		}
	}
	for _, identity := range entry.ExecutableRoots {
		writeSandboxFilter(&profile, "subpath", identity.Path)
	}
	profile.WriteString(")\n")
	return profile.String()
}

func auditSandboxProfile(policy *executionPolicy, entry executionPolicyEntry, sourceRoot, promptDir, outputDir, sessionDir, temporaryDir, nativeAddonPath, gatewayAddress string) string {
	var profile strings.Builder
	profile.WriteString("(version 1)\n(deny default)\n")
	profile.WriteString("(deny process-info* (target others))\n(deny signal (target others))\n")
	if policy != nil && len(policy.protectedPaths) != 0 {
		profile.WriteString("(deny file-read*")
		for _, path := range sandboxPathVariants(policy.protectedPaths...) {
			writeSandboxFilter(&profile, "literal", path)
		}
		profile.WriteString(")\n")
	}
	profile.WriteString("(allow process-fork)\n(allow process-info* (target self) (target children))\n(allow signal (target self) (target children))\n")
	profile.WriteString("(allow sysctl-read (sysctl-name \"security.mac.lockdown_mode_state\" \"kern.bootargs\" \"kern.osproductversion\" \"kern.iossupportversion\" \"kern.osvariant_status\" \"hw.ephemeral_storage\" \"hw.pagesize_compat\" \"hw.memsize\" \"hw.activecpu\" \"hw.ncpu\" \"hw.model\" \"kern.osrelease\" \"kern.version\" \"machdep.cpu.brand_string\" \"hw.optional.arm.FEAT_AES\" \"hw.optional.arm.FEAT_LSE\" \"hw.optional.arm.FEAT_JSCVT\" \"hw.optional.arm.FEAT_FP16\" \"hw.optional.arm.FEAT_FRINTTS\" \"hw.optional.arm.FEAT_SHA3\" \"hw.optional.arm.FEAT_DotProd\"))\n")
	profile.WriteString("(allow mach-lookup (global-name \"com.apple.logd\" \"com.apple.system.opendirectoryd.libinfo\" \"com.apple.system.opendirectoryd.membership\" \"com.apple.bsd.dirhelper\"))\n")
	profile.WriteString("(allow file-write* (literal \"/dev/dtracehelper\"))\n")
	readRoots := []string{sourceRoot, promptDir, outputDir, sessionDir, temporaryDir}
	if entry.OMPRuntimeAuthority.NativeDataRoot != "" {
		readRoots = append(readRoots, entry.OMPRuntimeAuthority.NativeDataRoot)
	}
	for _, identity := range entry.RuntimeReadRoots {
		if identity.Path != "/bin" && identity.Path != "/usr/bin" {
			readRoots = append(readRoots, identity.Path)
		}
	}
	executablePaths := []string{entry.OMPExecutable.Path, entry.GitExecutable.Path}

	profile.WriteString("(allow file-read-metadata file-test-existence (literal \"/\")")
	metadataPaths := append(append([]string(nil), readRoots...), executablePaths...)
	metadataPaths = append(metadataPaths, nativeAddonPath)
	for _, path := range sandboxPathVariants(metadataPaths...) {
		writeSandboxFilter(&profile, "path-ancestors", path)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-read-metadata (literal \"/System/Cryptexes/OS\") (literal \"/System/Volumes/Data\"))\n")
	profile.WriteString("(allow file-read* file-test-existence")
	writeSandboxFilter(&profile, "literal", "/")
	for _, path := range []string{"/dev/null", "/dev/zero", "/dev/urandom"} {
		writeSandboxFilter(&profile, "literal", path)
	}
	for _, path := range sandboxPathVariants(readRoots...) {
		writeSandboxFilter(&profile, "subpath", path)
	}
	for _, path := range sandboxPathVariants(executablePaths...) {
		writeSandboxFilter(&profile, "literal", path)
	}
	for _, path := range sandboxPathVariants(nativeAddonPath) {
		writeSandboxFilter(&profile, "literal", path)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-read-data (literal \"/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld\") (literal \"/dev/dtracehelper\"))\n")
	writeAuditRuntimeAncestorReadDataRule(&profile, entry.OMPRuntimeAuthority)
	writeAuditSourceAncestorReadDataRule(&profile, sourceRoot, entry.WorkRoot)
	profile.WriteString("(allow file-map-executable (subpath \"/usr/lib\") (subpath \"/System/Library\") (subpath \"/Library/Apple\")")
	for _, path := range sandboxPathVariants(executablePaths...) {
		writeSandboxFilter(&profile, "literal", path)
	}
	for _, path := range sandboxPathVariants(nativeAddonPath) {
		writeSandboxFilter(&profile, "literal", path)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-write* (literal \"/dev/null\")")
	for _, path := range sandboxPathVariants(sessionDir) {
		writeSandboxFilter(&profile, "subpath", path)
	}
	if entry.OMPRuntimeAuthority.NativeDataRoot != "" {
		for _, path := range sandboxPathVariants(entry.OMPRuntimeAuthority.NativeDataRoot) {
			writeSandboxFilter(&profile, "subpath", path)
		}
	}
	writeAuditSealedTemporaryWriteFilters(&profile, temporaryDir)
	profile.WriteString(")\n")
	writeAuditNativeWriteDenials(&profile, nativeAddonPath)
	writeAuditNativeFallbackDenials(&profile, entry.OMPRuntimeAuthority.DeniedNativeFallbackRoots)
	profile.WriteString("(allow process-exec")
	for _, path := range sandboxPathVariants(executablePaths...) {
		writeSandboxFilter(&profile, "literal", path)
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow network-outbound (remote tcp \"")
	profile.WriteString(sandboxLiteral(gatewayAddress))
	profile.WriteString("\"))\n")
	return profile.String()
}

func writeAuditSealedTemporaryWriteFilters(profile *strings.Builder, temporaryDir string) {
	if profile == nil {
		return
	}
	for _, temporary := range sandboxPathVariants(temporaryDir) {
		home := filepath.Join(temporary, "home")
		homeState := filepath.Join(home, ".omp")
		homeRun := filepath.Join(homeState, "run")
		profile.WriteString(" (require-all (subpath \"")
		profile.WriteString(sandboxLiteral(temporary))
		profile.WriteString("\") (require-not (literal \"")
		profile.WriteString(sandboxLiteral(home))
		profile.WriteString("\")) (require-not (subpath \"")
		profile.WriteString(sandboxLiteral(home))
		profile.WriteString("\")))")
		writeSandboxFilter(profile, "subpath", homeRun)
	}
}

func writeAuditRuntimeAncestorReadDataRule(profile *strings.Builder, authority executionPolicyOMPRuntimeAuthority) {
	if profile == nil {
		return
	}
	seen := make(map[string]struct{}, len(authority.ExecutableAncestors)+len(authority.NativeAddonAncestors))
	paths := make([]string, 0, len(seen))
	for _, identity := range append(append([]executionPolicyDirectoryIdentity(nil), authority.ExecutableAncestors...), authority.NativeAddonAncestors...) {
		for _, path := range sandboxPathVariants(identity.Path) {
			if path == "/" {
				continue
			}
			if _, exists := seen[path]; exists {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return
	}
	profile.WriteString("(allow file-read-data")
	for _, path := range paths {
		writeSandboxFilter(profile, "literal", path)
	}
	profile.WriteString(")\n")
}

func writeAuditSourceAncestorReadDataRule(profile *strings.Builder, sourceRoot, workRoot string) {
	if profile == nil {
		return
	}
	runRoot := filepath.Dir(sourceRoot)
	if filepath.Base(sourceRoot) != "source" || filepath.Dir(runRoot) != workRoot {
		return
	}
	profile.WriteString("(allow file-read-data")
	for _, path := range sandboxPathVariants(runRoot, workRoot) {
		writeSandboxFilter(profile, "literal", path)
	}
	profile.WriteString(")\n")
}

func writeAuditNativeWriteDenials(profile *strings.Builder, nativeAddonPath string) {
	for _, path := range sandboxPathVariants(filepath.Dir(nativeAddonPath)) {
		profile.WriteString("(deny file-write*")
		writeSandboxFilter(profile, "subpath", path)
		profile.WriteString(")\n")
	}
	for _, path := range sandboxPathVariants(nativeAddonPath) {
		profile.WriteString("(deny file-write*")
		writeSandboxFilter(profile, "literal", path)
		profile.WriteString(")\n")
	}
}
func writeAuditNativeFallbackDenials(profile *strings.Builder, roots []string) {
	for _, root := range sandboxPathVariants(roots...) {
		profile.WriteString("(deny file-read* file-map-executable")
		writeSandboxFilter(profile, "literal", root)
		writeSandboxFilter(profile, "subpath", root)
		profile.WriteString(")\n")
	}
}

func sandboxPathVariants(paths ...string) []string {
	unique := make(map[string]struct{}, len(paths)*2)
	add := func(candidate string) {
		if candidate == "" {
			return
		}
		unique[candidate] = struct{}{}
		if strings.HasPrefix(candidate, "/var/") {
			unique["/private"+candidate] = struct{}{}
		} else if strings.HasPrefix(candidate, "/private/var/") {
			unique[strings.TrimPrefix(candidate, "/private")] = struct{}{}
		}
	}
	for _, path := range paths {
		add(path)
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			add(resolved)
		}
	}
	values := make([]string, 0, len(unique))
	for path := range unique {
		values = append(values, path)
	}
	sort.Strings(values)
	return values
}

func writeSandboxFilter(profile *strings.Builder, kind, path string) {
	profile.WriteString(" (")
	profile.WriteString(kind)
	profile.WriteString(" \"")
	profile.WriteString(sandboxLiteral(path))
	profile.WriteString("\")")
}

func auditSandboxCommandArguments(invocation auditInvocation) []string {
	arguments := make([]string, 0, len(invocation.Arguments)+3)
	arguments = append(arguments, "-p", invocation.SandboxProfile, invocation.OMPExecutablePath)
	return append(arguments, invocation.Arguments...)
}

func freezeAuditWrapper(identity executionPolicyFileIdentity) ([]byte, error) {
	fd, err := unix.Open(identity.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, authenticationError("open pinned audit wrapper")
	}
	file := os.NewFile(uintptr(fd), filepath.Base(identity.Path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, authenticationError("open pinned audit wrapper descriptor")
	}
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || !auditWrapperStatMatches(before, identity) {
		return nil, authenticationError("pinned audit wrapper identity before read")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxExecutionPolicyBytes*8+1))
	if err != nil {
		zeroBytes(contents)
		return nil, authenticationError("read pinned audit wrapper")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || !auditWrapperStatMatches(after, identity) || !sameAuditWrapperStat(before, after) ||
		int64(len(contents)) != identity.Size || int64(len(contents)) > maxExecutionPolicyBytes*8 || hashJournalBytes(contents) != identity.SHA256 {
		zeroBytes(contents)
		return nil, authenticationError("pinned audit wrapper changed during read")
	}
	return contents, nil
}

func auditWrapperStatMatches(stat unix.Stat_t, identity executionPolicyFileIdentity) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && statDecimal(uint64(stat.Dev)) == identity.Device && statDecimal(stat.Ino) == identity.Inode &&
		stat.Uid == identity.OwnerUID && uint32(stat.Mode&0o777) == identity.Mode && stat.Size == identity.Size
}

func sameAuditWrapperStat(left, right unix.Stat_t) bool {
	return uint64(left.Dev) == uint64(right.Dev) && left.Ino == right.Ino && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Mode == right.Mode && left.Size == right.Size
}

func startAuditCommand(command *exec.Cmd, reader, writer *os.File) error {
	if command == nil || reader != nil || writer != nil || command.Stdin != nil || len(command.ExtraFiles) != 0 {
		return authenticationError("start sandboxed direct OMP")
	}
	if err := command.Start(); err != nil {
		return authenticationError("start sandboxed direct OMP")
	}
	return nil
}

func validateRootOwnedSystemExecutable(path string) error {
	if path != auditSandboxExecutable {
		return authenticationError("approved system executable path")
	}
	contents, identity, err := readPinnedRegularFile(path, 0, false, maxExecutionPolicyBytes*8)
	if err != nil {
		return authenticationError("root-owned immutable system executable")
	}
	defer zeroBytes(contents)
	if identity.OwnerUID != 0 || identity.Mode&0o022 != 0 || identity.Mode&0o111 == 0 || identity.SHA256 != hashJournalBytes(contents) {
		return authenticationError("root-owned immutable system executable")
	}
	return nil
}

func scanAuditInvocationWritableTrees(policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation) error {
	return scanAuditInvocationWritableTreesExcept(policy, entry, invocation, "", auditTimeoutEvidence{})
}

func scanAuditInvocationTimeoutTrees(policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation, evidence auditTimeoutEvidence) error {
	return scanAuditInvocationWritableTreesExcept(policy, entry, invocation, invocation.OutputPath, evidence)
}

func scanAuditInvocationWritableTreesExcept(policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation, excludedPath string, timeoutEvidence auditTimeoutEvidence) error {
	files := 0
	freshSessionArtifacts := 0
	ompLogArtifacts := 0
	var total int64
	allowFreshSession := excludedPath == "" && timeoutEvidence == (auditTimeoutEvidence{}) && invocation.Resume == (auditResume{})
	allowOMPLogs := allowFreshSession
	for _, root := range []string{invocation.PromptDir, invocation.OutputDir, invocation.SessionDir, invocation.TemporaryDir} {
		err := filepath.Walk(root, func(path string, information os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return failAuditArtifactScan(invocation, root, auditArtifactScanReasonWalk, authenticationError("walk bounded audit invocation tree"))
			}
			if path == excludedPath {
				return nil
			}
			if path == invocation.NativeAddonPath {
				return nil
			}
			if information.IsDir() {
				return nil
			}
			if information.Mode()&os.ModeSymlink != 0 {
				if root == invocation.SessionDir {
					return failAuditArtifactScan(invocation, root, auditArtifactScanReasonSymlink, authenticationError("audit invocation session symlink artifact"))
				}
				return nil
			}
			if !information.Mode().IsRegular() || information.Size() < 0 {
				return failAuditArtifactScan(invocation, root, auditArtifactScanReasonSpecial, authenticationError("audit invocation special artifact"))
			}
			files++
			total += information.Size()
			if files > maxAuditTreeScanFiles || total > maxAuditTreeScanBytes {
				return failAuditArtifactScan(invocation, root, auditArtifactScanReasonLimit, ErrLimit)
			}
			contents, _, err := readAuditRegularFile(path, maxAuditTreeScanBytes)
			if err != nil {
				return failAuditArtifactScan(invocation, root, auditArtifactScanReasonRead, err)
			}
			defer zeroBytes(contents)
			if validAuditTimeoutSessionArtifact(path, invocation, timeoutEvidence) {
				if auditBytesLeakSecretAuthority(contents) {
					return failAuditArtifactScan(invocation, root, auditArtifactScanReasonTimeoutSecret, authenticationError("audit timeout session secret authority leakage"))
				}
			} else if allowFreshSession && root == invocation.SessionDir && filepath.Ext(path) == ".jsonl" {
				usedFallback := false
				if filepath.Dir(path) != invocation.SessionDir || !validAuditFreshSessionArtifact(path, contents, policy, entry, invocation) {
					if !validAuditRealOMPSessionArtifact(path, contents, policy, entry, invocation) {
						return failAuditArtifactScan(invocation, root, auditArtifactScanReasonFreshAuthentication, authenticationError("audit fresh session artifact authentication"))
					}
					usedFallback = true
				}
				freshSessionArtifacts++
				if freshSessionArtifacts > 1 || (!usedFallback && auditBytesLeakSecretAuthority(contents)) {
					return failAuditArtifactScan(invocation, root, auditArtifactScanReasonFreshAuthority, authenticationError("audit fresh session artifact authority leakage"))
				}
			} else if allowOMPLogs && root == invocation.TemporaryDir && validAuditOMPLogArtifact(path, contents, policy, entry, invocation) {
				if auditBytesLeakSecretAuthority(contents) {
					return failAuditArtifactScan(invocation, root, auditArtifactScanReasonTimeoutSecret, authenticationError("audit OMP log secret authority leakage"))
				}
				ompLogArtifacts++
				if ompLogArtifacts > maxAuditOMPLogArtifacts {
					return failAuditArtifactScan(invocation, root, auditArtifactScanReasonLimit, authenticationError("audit OMP log artifact count limit"))
				}
			} else if authorityKind, leaked := classifyAuditBytesLeakAuthority(contents, entry, invocation); leaked {
				return failAuditArtifactAuthorityScan(invocation, root, path, authorityKind, authenticationError("audit invocation artifact authority leakage"))
			}

			if policy != nil {
				for _, protected := range policy.protectedPaths {
					if protected != "" && bytes.Contains(contents, []byte(protected)) {
						return failAuditArtifactScan(invocation, root, auditArtifactScanReasonProtected, authenticationError("audit invocation artifact protected path leakage"))
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func validAuditFreshSessionArtifact(path string, contents []byte, policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation) bool {
	if invocation.Resume != (auditResume{}) || filepath.Ext(path) != ".jsonl" || filepath.Dir(path) != invocation.SessionDir ||
		!validFreshAuditTimeoutSessionPath(path, invocation.SessionDir) {
		return false
	}
	sessionIdentity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "session")
	if !ok || sessionIdentity.Path != invocation.SessionDir {
		return false
	}
	physicalWorkDir, err := filepath.EvalSymlinks(invocation.WorkDir)
	if err != nil {
		return false
	}
	prompt, _, err := readAuditRegularFile(invocation.PromptPath, maxAuditTimeoutSessionHeaderBytes)
	if err != nil {
		return false
	}
	defer zeroBytes(prompt)
	if hashJournalBytes(prompt) != invocation.PromptSHA256 {
		return false
	}
	if _, matched := parseAuditSessionHeader(path, physicalWorkDir, string(prompt)); !matched {
		return false
	}
	allowedPaths := make(map[string]struct{}, 12)
	for _, allowed := range []string{
		path, invocation.PromptDir, invocation.PromptPath, invocation.OutputDir, invocation.OutputPath,
		invocation.SessionDir, invocation.TemporaryDir, invocation.WorkDir, physicalWorkDir,
		invocation.AgentDir, invocation.ModelsPath, invocation.HomeDir,
	} {
		if allowed != "" {
			allowedPaths[allowed] = struct{}{}
		}
	}
	protectedPaths := []string(nil)
	if policy != nil {
		protectedPaths = policy.protectedPaths
	}
	return auditFreshSessionJSONLAllowsPaths(contents, allowedPaths, protectedPaths)
}

func validAuditRealOMPSessionArtifact(path string, contents []byte, policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation) bool {
	if invocation.Resume != (auditResume{}) || filepath.Ext(path) != ".jsonl" || filepath.Dir(path) != invocation.SessionDir {
		return false
	}
	sessionIdentity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "session")
	if !ok || sessionIdentity.Path != invocation.SessionDir {
		return false
	}
	if !auditSessionJSONLHasValidUUID(contents) {
		return false
	}
	prompt, _, err := readAuditRegularFile(invocation.PromptPath, maxAuditTimeoutSessionHeaderBytes)
	if err != nil {
		return false
	}
	defer zeroBytes(prompt)
	if hashJournalBytes(prompt) != invocation.PromptSHA256 {
		return false
	}
	if !auditSessionJSONLContainsPrompt(contents, string(prompt)) {
		return false
	}
	if !auditSessionJSONLHasValidCWD(contents, invocation.WorkDir) {
		return false
	}
	if policy != nil {
		for _, protected := range policy.protectedPaths {
			if protected != "" && bytes.Contains(contents, []byte(protected)) {
				return false
			}
		}
	}
	if entry.Repository.Path != "" && bytes.Contains(contents, []byte(entry.Repository.Path)) {
		return false
	}
	if entry.Wrapper.Path != "" && bytes.Contains(contents, []byte(entry.Wrapper.Path)) {
		return false
	}
	if !auditSessionJSONLHasNoForeignPaths(contents, entry, invocation) {
		return false
	}
	return true
}

func auditSessionJSONLHasValidCWD(contents []byte, expectedCWD string) bool {
	physicalCWD, err := filepath.EvalSymlinks(expectedCWD)
	if err != nil {
		return false
	}
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), maxAuditTimeoutSessionHeaderBytes)
	for line := 0; line < 50 && scanner.Scan(); line++ {
		var record struct {
			Type string `json:"type"`
			CWD  string `json:"cwd"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if record.Type == "session" {
			return record.CWD == expectedCWD || record.CWD == physicalCWD
		}
	}
	return false
}

func auditSessionJSONLHasNoForeignPaths(contents []byte, entry executionPolicyEntry, invocation auditInvocation) bool {
	validRoots := make([]string, 0, 16)
	for _, root := range []string{
		invocation.WorkDir, invocation.PromptDir, invocation.PromptPath,
		invocation.OutputDir, invocation.OutputPath, invocation.SessionDir,
		invocation.TemporaryDir, invocation.AgentDir, invocation.ModelsPath,
		invocation.HomeDir, invocation.HomeStateDir, invocation.HomeRunDir,
		invocation.NativeAddonPath,
	} {
		if root != "" {
			validRoots = append(validRoots, root)
			if physical, err := filepath.EvalSymlinks(root); err == nil && physical != root {
				validRoots = append(validRoots, physical)
			}
		}
	}
	if entry.OMPRuntimeAuthority.NativeDataRoot != "" {
		validRoots = append(validRoots, entry.OMPRuntimeAuthority.NativeDataRoot)
	}
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), maxAuditTreeScanBytes)
	for scanner.Scan() {
		var record map[string]json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		pathRaw, hasPath := record["path"]
		if !hasPath {
			continue
		}
		var pathValue string
		if json.Unmarshal(pathRaw, &pathValue) != nil {
			continue
		}
		if !containsAuditAbsolutePathReference(pathValue) {
			continue
		}
		underKnownRoot := false
		for _, root := range validRoots {
			if strings.HasPrefix(pathValue, root) || strings.Contains(pathValue, root) {
				underKnownRoot = true
				break
			}
		}
		if !underKnownRoot {
			return false
		}
	}
	return scanner.Err() == nil
}

func auditSessionJSONLHasValidUUID(contents []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), maxAuditTimeoutSessionHeaderBytes)
	for line := 0; line < 50 && scanner.Scan(); line++ {
		var record struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			return false
		}
		if record.Type == "session" {
			return auditSessionUUIDPattern.MatchString(record.ID)
		}
	}
	return false
}

func auditSessionJSONLContainsPrompt(contents []byte, expectedPrompt string) bool {
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), maxAuditTimeoutSessionHeaderBytes)
	for line := 0; line < 50 && scanner.Scan(); line++ {
		var record struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if record.Type != "message" {
			continue
		}
		var message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(record.Message, &message) != nil {
			continue
		}
		if message.Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(message.Content, &text) == nil && text == expectedPrompt {
			return true
		}
		var parts []struct{ Type, Text string }
		if json.Unmarshal(message.Content, &parts) == nil {
			var combined strings.Builder
			for _, part := range parts {
				if part.Type == "text" {
					combined.WriteString(part.Text)
				}
			}
			if combined.String() == expectedPrompt {
				return true
			}
		}
	}
	return false
}

const maxAuditOMPLogArtifacts = 8

func validAuditOMPLogArtifact(path string, contents []byte, policy *executionPolicy, entry executionPolicyEntry, invocation auditInvocation) bool {
	if invocation.HomeRunDir == "" || invocation.WorkDir == "" || invocation.AgentDir == "" {
		return false
	}
	logsDir := filepath.Join(invocation.HomeRunDir, "logs")
	if !auditArtifactPathWithinRoot(path, logsDir) {
		return false
	}
	homeRunIdentity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "direct_omp_home_run")
	if !ok || homeRunIdentity.Path != invocation.HomeRunDir {
		return false
	}
	if entry.Repository.Path != "" && bytes.Contains(contents, []byte(entry.Repository.Path)) {
		return false
	}
	if entry.Wrapper.Path != "" && bytes.Contains(contents, []byte(entry.Wrapper.Path)) {
		return false
	}
	return true
}

func validAuditTimeoutSessionArtifact(path string, invocation auditInvocation, evidence auditTimeoutEvidence) bool {
	if filepath.Ext(path) != ".jsonl" || filepath.Dir(path) != invocation.SessionDir {
		return false
	}
	if evidence != (auditTimeoutEvidence{}) && evidence.SessionPath != "" {
		return path == evidence.SessionPath
	}
	if invocation.Resume.SessionUUID == "" || evidence != (auditTimeoutEvidence{}) && evidence.SessionUUID != invocation.Resume.SessionUUID {
		return false
	}
	physicalWorkDir, err := filepath.EvalSymlinks(invocation.WorkDir)
	if err != nil {
		return false
	}
	prompt, err := os.ReadFile(invocation.PromptPath)
	if err != nil {
		return false
	}
	expectedPrompt := strings.TrimSuffix(string(prompt), readOnlyAuditSynthesizePromptSuffix)
	uuid, matched := parseAuditSessionHeader(path, physicalWorkDir, expectedPrompt)
	return matched && uuid == invocation.Resume.SessionUUID
}

func cleanupAuditInvocationTransient(entry executionPolicyEntry, invocation auditInvocation) error {
	roots, err := authenticatedAuditInvocationCleanupRoots(entry, invocation, false)
	if err != nil {
		return err
	}
	if err := scrubAndRemoveAuthenticatedAuditRoots(invocation.namespaceAuthority, roots); err != nil {
		return err
	}
	if err := verifyAuthenticatedAuditCleanupRootsAbsent(invocation.namespaceAuthority, roots); err != nil {
		return err
	}
	return validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid()))
}

func cleanupAuthenticatedAuditInvocationAll(entry executionPolicyEntry, invocation auditInvocation) error {
	roots, err := authenticatedAuditInvocationCleanupRoots(entry, invocation, true)
	if err != nil {
		return err
	}
	if len(invocation.sharedOwnedRoots) != 2 || invocation.sharedOwnedRoots[0].Role != "work" ||
		invocation.sharedOwnedRoots[0].Path != filepath.Dir(invocation.WorkDir) || !invocation.sharedOwnedRoots[0].CleanupRoot ||
		invocation.sharedOwnedRoots[1].Role != "source_snapshot" || invocation.sharedOwnedRoots[1].Path != invocation.WorkDir ||
		invocation.sharedOwnedRoots[1].CleanupRoot {
		return authenticationError("live audit shared cleanup identity binding")
	}
	roots = append(roots, invocation.sharedOwnedRoots...)
	if err := validateAuditOwnedRootSequence(roots); err != nil {
		return err
	}
	if err := scrubAndRemoveAuthenticatedAuditRoots(invocation.namespaceAuthority, roots); err != nil {
		return err
	}
	if err := verifyAuthenticatedAuditCleanupRootsAbsent(invocation.namespaceAuthority, roots); err != nil {
		return err
	}
	return validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid()))
}

func authenticatedAuditInvocationCleanupRoots(entry executionPolicyEntry, invocation auditInvocation, includeSession bool) ([]auditOwnedRootIdentity, error) {
	if invocation.namespaceAuthority == nil {
		return nil, authenticationError("live audit cleanup namespace authority")
	}
	if _, err := validateAuditCleanupInvocation(entry, invocation); err != nil {
		return nil, err
	}
	byPath := make(map[string]auditOwnedRootIdentity, len(invocation.OwnedRoots))
	for _, identity := range invocation.OwnedRoots {
		if !validAuditOwnedRootIdentity(identity) {
			return nil, authenticationError("live audit cleanup root identity")
		}
		if prior, duplicate := byPath[identity.Path]; duplicate && prior != identity {
			return nil, authenticationError("conflicting live audit cleanup root identity")
		}
		byPath[identity.Path] = identity
	}
	expected := []struct {
		role string
		path string
	}{
		{role: "prompt", path: invocation.PromptDir},
		{role: "output", path: invocation.OutputDir},
		{role: "temporary", path: invocation.TemporaryDir},
	}
	if invocation.NativeAddonPath != "" {
		if len(invocation.OwnedRoots) != 8 {
			return nil, authenticationError("exact bound audit cleanup root inventory")
		}
		expected = append(expected,
			struct{ role, path string }{role: "direct_omp_agent", path: invocation.AgentDir},
			struct{ role, path string }{role: "direct_omp_home", path: invocation.HomeDir},
			struct{ role, path string }{role: "direct_omp_home_state", path: invocation.HomeStateDir},
			struct{ role, path string }{role: "direct_omp_home_run", path: invocation.HomeRunDir},
		)
	} else {
		if len(invocation.OwnedRoots) != 4 {
			return nil, authenticationError("exact unbound audit cleanup root inventory")
		}
		if err := validateAuditCleanupTransport(entry, invocation); err != nil {
			return nil, err
		}
	}
	if includeSession {
		expected = append(expected, struct{ role, path string }{role: "session", path: invocation.SessionDir})
	}
	roots := make([]auditOwnedRootIdentity, 0, len(expected))
	for _, want := range expected {
		identity, ok := byPath[want.path]
		if !ok || identity.Role != want.role {
			return nil, authenticationError("live audit cleanup role and path binding")
		}
		roots = append(roots, identity)
	}
	if err := validateAuditOwnedRootSequence(roots); err != nil {
		return nil, err
	}
	return roots, nil
}

// cleanupAuditInvocationAll is retained for explicit legacy cleanup tests and
// non-completing recovery only. Live execution cleanup uses captured identities.
func cleanupAuditInvocationAll(entry executionPolicyEntry, invocation auditInvocation) error {
	runRoot, err := validateAuditCleanupInvocation(entry, invocation)
	if err != nil {
		return err
	}
	transientPresent, err := auditCleanupRootSetPresent(invocation.PromptDir, invocation.OutputDir, invocation.TemporaryDir)
	if err != nil {
		return err
	}
	if transientPresent {
		if err := validateAuditCleanupTransport(entry, invocation); err != nil {
			return err
		}
		for _, root := range []string{invocation.SessionDir, runRoot} {
			present, err := inspectAuditCleanupRoot(root)
			if err != nil {
				return err
			}
			if !present {
				return authenticationError("partial audit cleanup roots")
			}
		}
		if err := scrubAuditTrees(invocation.PromptDir, invocation.OutputDir, invocation.SessionDir, invocation.TemporaryDir, runRoot); err != nil {
			return err
		}
	} else {
		for _, root := range []string{invocation.SessionDir, runRoot} {
			if _, err := inspectAuditCleanupRoot(root); err != nil {
				return err
			}
		}
		if err := scrubAuditTrees(invocation.SessionDir, runRoot); err != nil {
			return err
		}
	}
	return validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, uint32(os.Getuid()))
}

func validateAuditCleanupInvocation(entry executionPolicyEntry, invocation auditInvocation) (string, error) {
	runRoot := filepath.Dir(invocation.WorkDir)
	if !executionTaskIDPattern.MatchString(invocation.RunID) || !executionTaskIDPattern.MatchString(invocation.SessionRunID) ||
		filepath.Dir(invocation.PromptDir) != entry.PromptRoot || filepath.Base(invocation.PromptDir) != invocation.RunID ||
		filepath.Dir(invocation.OutputDir) != entry.OutputRoot || filepath.Base(invocation.OutputDir) != invocation.RunID ||
		filepath.Dir(invocation.SessionDir) != entry.SessionRoot || filepath.Base(invocation.SessionDir) != invocation.SessionRunID ||
		filepath.Dir(invocation.TemporaryDir) != entry.TemporaryRoot || filepath.Base(invocation.TemporaryDir) != invocation.RunID ||
		filepath.Base(invocation.WorkDir) != "source" || filepath.Dir(runRoot) != entry.WorkRoot || invocation.OMPExecutablePath != entry.OMPExecutable.Path {
		return "", authenticationError("audit cleanup descriptor")
	}
	return runRoot, nil
}

func validateAuditCleanupTransport(entry executionPolicyEntry, invocation auditInvocation) error {
	if invocation.NativeAddonPath != "" {
		return validateAuditInvocationTransport(entry, invocation)
	}
	if invocation.AgentDir != "" || invocation.ModelsPath != "" || invocation.HomeDir != "" || invocation.HomeStateDir != "" || invocation.HomeRunDir != "" ||
		invocation.GatewayAuthority != "" || invocation.ModelsSHA256 != "" || invocation.ModelsSize != 0 ||
		invocation.NativeAddonSHA256 != "" || invocation.NativeAddonSize != 0 || invocation.modelsIdentity != (executionPolicyFileIdentity{}) ||
		invocation.nativeAddonIdentity != (executionPolicyFileIdentity{}) {
		return authenticationError("partial audit cleanup transport")
	}
	return nil
}

func auditCleanupRootSetPresent(roots ...string) (bool, error) {
	present := 0
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if _, duplicate := seen[root]; duplicate {
			return false, authenticationError("duplicate audit cleanup root")
		}
		seen[root] = struct{}{}
		exists, err := inspectAuditCleanupRoot(root)
		if err != nil {
			return false, err
		}
		if exists {
			present++
		}
	}
	if present != 0 && present != len(roots) {
		return false, authenticationError("partial audit cleanup roots")
	}
	return present == len(roots), nil
}

func inspectAuditCleanupRoot(root string) (bool, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return false, authenticationError("absolute audit cleanup root")
	}
	information, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	status, ok := informationSyscallStat(information)
	if err != nil || !ok || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() || status.Uid != uint32(os.Getuid()) {
		return false, authenticationError("unexpected audit cleanup root")
	}
	return true, nil
}

func scrubAuditTrees(roots ...string) error {
	var first error
	for _, root := range roots {
		if err := scrubAndRemoveAuditTree(root); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func scrubAndRemoveAuditTree(root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return authenticationError("absolute audit cleanup root")
	}
	information, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	status, ok := informationSyscallStat(information)
	if err != nil || !ok || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() || status.Uid != uint32(os.Getuid()) {
		return authenticationError("unexpected audit cleanup root")
	}
	var first error
	err = filepath.Walk(root, func(path string, information os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if first == nil {
				first = walkErr
			}
			return nil
		}
		if information.IsDir() {
			if err := os.Chmod(path, 0o700); err != nil && first == nil {
				first = err
			}
			return nil
		}
		if !information.Mode().IsRegular() {
			return nil
		}
		if err := os.Chmod(path, 0o600); err != nil && first == nil {
			first = err
		}
		fd, openErr := unix.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			if first == nil {
				first = openErr
			}
			return nil
		}
		file := os.NewFile(uintptr(fd), filepath.Base(path))
		if file == nil {
			_ = unix.Close(fd)
			if first == nil {
				first = authenticationError("audit cleanup descriptor")
			}
			return nil
		}
		remaining := information.Size()
		zeros := make([]byte, 32*1024)
		for remaining > 0 {
			chunk := int64(len(zeros))
			if remaining < chunk {
				chunk = remaining
			}
			written, writeErr := file.Write(zeros[:chunk])
			remaining -= int64(written)
			if writeErr != nil || written == 0 {
				if first == nil {
					first = writeErr
					if first == nil {
						first = io.ErrShortWrite
					}
				}
				break
			}
		}
		if err := file.Truncate(0); err != nil && first == nil {
			first = err
		}
		if err := file.Sync(); err != nil && first == nil {
			first = err
		}
		if err := file.Close(); err != nil && first == nil {
			first = err
		}
		return nil
	})
	if err != nil && first == nil {
		first = err
	}
	if err := os.RemoveAll(root); err != nil && first == nil {
		first = err
	}
	return first
}

func scrubAndRemoveAuditTreeAtIdentity(parent int, name string, zeros []byte, identity *auditOwnedRootIdentity) error {
	var expected unix.Stat_t
	if err := unix.Fstatat(parent, name, &expected, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil || expected.Mode&unix.S_IFMT != unix.S_IFDIR {
		return authenticationError("unexpected audit cleanup root descriptor")
	}
	if identity != nil && !auditOwnedRootStatMatches(expected, *identity) {
		return authenticationError("authenticated audit cleanup root identity mismatch")
	}
	if err := unix.Fchmodat(parent, name, 0o700, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return authenticationError("make audit cleanup directory removable")
	}
	descriptor, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return authenticationError("open audit cleanup directory descriptor")
	}
	directory := os.NewFile(uintptr(descriptor), name)
	if directory == nil {
		_ = unix.Close(descriptor)
		return authenticationError("open audit cleanup directory")
	}
	var opened unix.Stat_t
	if err := unix.Fstat(descriptor, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(opened.Dev) != uint64(expected.Dev) || opened.Ino != expected.Ino ||
		identity != nil && !auditOwnedRootStatMatchesAfterCleanupChmod(opened, *identity) {
		_ = directory.Close()
		return authenticationError("audit cleanup directory replacement")
	}
	names, err := directory.Readdirnames(-1)
	if err != nil {
		_ = directory.Close()
		return authenticationError("read audit cleanup directory")
	}
	for _, child := range names {
		if child == "" || child == "." || child == ".." || filepath.Base(child) != child || strings.IndexByte(child, 0) >= 0 {
			_ = directory.Close()
			return authenticationError("audit cleanup child name")
		}
		var childStatus unix.Stat_t
		if err := unix.Fstatat(descriptor, child, &childStatus, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
			continue
		} else if err != nil {
			_ = directory.Close()
			return authenticationError("inspect audit cleanup child")
		}
		switch childStatus.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			if err := scrubAndRemoveAuditTreeAtIdentity(descriptor, child, zeros, nil); err != nil {
				_ = directory.Close()
				return err
			}
		case unix.S_IFREG:
			if err := scrubAndRemoveAuditFileAt(descriptor, child, childStatus, zeros); err != nil {
				_ = directory.Close()
				return err
			}
		default:
			if err := unix.Unlinkat(descriptor, child, 0); err != nil && !errors.Is(err, unix.ENOENT) {
				_ = directory.Close()
				return authenticationError("remove audit cleanup special artifact")
			}
		}
	}
	if err := unix.Fsync(descriptor); err != nil {
		_ = directory.Close()
		return authenticationError("sync scrubbed audit cleanup directory")
	}
	if err := directory.Close(); err != nil {
		return authenticationError("close scrubbed audit cleanup directory")
	}
	var linked unix.Stat_t
	if err := unix.Fstatat(parent, name, &linked, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil || linked.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(linked.Dev) != uint64(opened.Dev) || linked.Ino != opened.Ino ||
		identity != nil && !auditOwnedRootStatMatchesAfterCleanupChmod(linked, *identity) {
		return authenticationError("audit cleanup directory relinked before removal")
	}
	if err := unix.Unlinkat(parent, name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
		return authenticationError("remove scrubbed audit cleanup directory")
	}
	if err := unix.Fsync(parent); err != nil {
		return authenticationError("sync audit cleanup parent")
	}
	return nil
}

func auditOwnedRootStatMatchesAfterCleanupChmod(status unix.Stat_t, identity auditOwnedRootIdentity) bool {
	return status.Mode&unix.S_IFMT == unix.S_IFDIR && status.Mode&0o777 == 0o700 &&
		statDecimal(uint64(status.Dev)) == identity.Device && statDecimal(status.Ino) == identity.Inode &&
		status.Uid == identity.OwnerUID && status.Gid == identity.OwnerGID
}

func scrubAndRemoveAuditFileAt(parent int, name string, expected unix.Stat_t, zeros []byte) error {
	if expected.Mode&unix.S_IFMT != unix.S_IFREG || expected.Size < 0 {
		return authenticationError("unexpected audit cleanup file descriptor")
	}
	if err := unix.Fchmodat(parent, name, 0o600, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return authenticationError("make audit cleanup file removable")
	}
	descriptor, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return authenticationError("open audit cleanup file descriptor")
	}
	file := os.NewFile(uintptr(descriptor), name)
	if file == nil {
		_ = unix.Close(descriptor)
		return authenticationError("open audit cleanup file")
	}
	var opened unix.Stat_t
	if err := unix.Fstat(descriptor, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFREG ||
		uint64(opened.Dev) != uint64(expected.Dev) || opened.Ino != expected.Ino || opened.Size != expected.Size {
		_ = file.Close()
		return authenticationError("audit cleanup file replacement")
	}
	remaining := opened.Size
	for remaining > 0 {
		chunk := int64(len(zeros))
		if remaining < chunk {
			chunk = remaining
		}
		written, err := file.Write(zeros[:chunk])
		if err != nil || int64(written) != chunk {
			_ = file.Close()
			return authenticationError("overwrite audit cleanup file")
		}
		remaining -= int64(written)
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return authenticationError("truncate audit cleanup file")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return authenticationError("sync audit cleanup file")
	}
	if err := file.Close(); err != nil {
		return authenticationError("close audit cleanup file")
	}
	var linked unix.Stat_t
	if err := unix.Fstatat(parent, name, &linked, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil || linked.Mode&unix.S_IFMT != unix.S_IFREG || uint64(linked.Dev) != uint64(opened.Dev) || linked.Ino != opened.Ino {
		return authenticationError("audit cleanup file relinked before removal")
	}
	if err := unix.Unlinkat(parent, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return authenticationError("remove scrubbed audit cleanup file")
	}
	return nil
}

func sandboxLiteral(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func auditPlatformSupported(goos string) bool {
	return goos == "darwin"
}

func auditProcessStartIdentity(pid int) (string, error) {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || int(process.Proc.P_pid) != pid || int(process.Eproc.Ppid) != os.Getpid() {
		return "", authenticationError("audit process identity")
	}
	return strconv.FormatInt(process.Proc.P_starttime.Sec, 10) + ":" + strconv.FormatInt(int64(process.Proc.P_starttime.Usec), 10), nil
}
