package trustedsupervisor

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

const (
	executionPolicySchemaVersion      = "ananke.local-trusted-supervisor-execution-policy.v5"
	executionPolicyEntrySchemaVersion = "ananke.local-trusted-supervisor-execution-policy-entry.v5"
	readOnlyAuditPromptTemplateID     = "ananke_read_only_audit_v1"
	supportedOMPVersion               = "17.1.3"
	auditOMPNativeAddonFilename       = "pi_natives.darwin-arm64.node"
	maxExecutionPolicyBytes           = 1024 * 1024
	maxAuditOMPExecutableBytes        = 256 * 1024 * 1024
	maxAuditOMPNativeAddonBytes       = 256 * 1024 * 1024
	maxExecutionPolicyEntries         = 4096
	maxExecutionPolicyTests           = 64
	auditGitExecutable                = "/usr/bin/git"
)

const readOnlyAuditPromptTemplate = `Perform a read-only audit of the supplied immutable source snapshot.
Do not modify source, create repairs, invoke Run, or claim tests that are not present in wrapper logs.
Return only the bounded audit report requested by the trusted supervisor.`

const readOnlyAuditSynthesizePromptSuffix = "\nDo not call more tools; synthesize only from the existing session evidence."

var (
	gitObjectIDPattern     = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	executionValuePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	executionTaskIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,127}$`)

	auditCredentialEnvironmentNames = []string{
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
		"HERMES_API_KEY",
		"OPENAI_API_KEY",
		"SUDO_API_KEY",
	}
	auditCredentialEnvironmentAllowlist = map[string]struct{}{
		"ANTHROPIC_API_KEY": {},
		"GEMINI_API_KEY":    {},
		"GOOGLE_API_KEY":    {},
		"HERMES_API_KEY":    {},
		"OPENAI_API_KEY":    {},
		"SUDO_API_KEY":      {},
	}
	auditRuntimeReadRootAllowlist = map[string]struct{}{
		"/bin": {}, "/usr/bin": {}, "/usr/lib": {}, "/usr/share": {}, "/System/Library": {},
		"/Library/Apple": {}, "/private/var/db/timezone": {},
	}
	auditExecutableRootAllowlist = map[string]struct{}{
		"/bin": {}, "/usr/bin": {},
	}
	auditProviderCredentialOptions = map[string][][]string{
		"custom:sudo": {{"SUDO_API_KEY"}},
	}
	auditProviderEndpointAllowlist = map[string]executionPolicyEndpoint{
		"custom:sudo": {Hostname: "coding.sudoai.cc", Port: 443},
	}
	auditWrapperSystemExecutables = []string{
		"/bin/bash", "/bin/cat", "/bin/chmod", "/bin/date", "/bin/kill", "/bin/mkdir", "/bin/mv", "/bin/ps", "/bin/rm", "/bin/rmdir", "/bin/sleep",
		"/usr/bin/awk", "/usr/bin/cksum", "/usr/bin/dirname", "/usr/bin/git", "/usr/bin/grep", "/usr/bin/mktemp", "/usr/bin/python3", "/usr/bin/tr", "/usr/bin/wc",
	}
)

type executionPolicyFile struct {
	SchemaVersion string                 `json:"schema_version"`
	Executions    []executionPolicyEntry `json:"executions"`
}

type executionPolicyFileIdentity struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Device   string `json:"device"`
	Inode    string `json:"inode"`
	OwnerUID uint32 `json:"owner_uid"`
	Mode     uint32 `json:"mode"`
	Size     int64  `json:"size"`
}

type executionPolicyDirectoryIdentity struct {
	Path     string `json:"path"`
	Device   string `json:"device"`
	Inode    string `json:"inode"`
	OwnerUID uint32 `json:"owner_uid"`
	Mode     uint32 `json:"mode"`
}

type executionPolicyTestCommand struct {
	ID             string                           `json:"id"`
	CommandSHA256  string                           `json:"command_sha256"`
	Executable     executionPolicyFileIdentity      `json:"executable"`
	ExecutableRoot executionPolicyDirectoryIdentity `json:"executable_root"`
	Arguments      []string                         `json:"arguments"`
	TimeoutSeconds int                              `json:"timeout_seconds"`
}

func sealExecutionPolicyTestCommand(command executionPolicyTestCommand) (executionPolicyTestCommand, error) {
	command.CommandSHA256 = ""
	hash, err := canonicalHash(command)
	if err != nil {
		return executionPolicyTestCommand{}, err
	}
	command.CommandSHA256 = hash
	return command, nil
}

func validSealedExecutionPolicyTestCommand(command executionPolicyTestCommand) bool {
	sealed, err := sealExecutionPolicyTestCommand(command)
	if err != nil {
		return false
	}
	sealedBytes, sealedErr := marshalCanonical(sealed)
	commandBytes, commandErr := marshalCanonical(command)
	return sealedErr == nil && commandErr == nil && bytes.Equal(sealedBytes, commandBytes)
}

type executionPolicyEndpoint struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
}

type executionPolicyEntry struct {
	SchemaVersion              string                             `json:"schema_version"`
	LaunchSpecHash             string                             `json:"launch_spec_hash"`
	TaskID                     string                             `json:"task_id"`
	RepositoryIdentity         string                             `json:"repository_identity"`
	RepositoryIdentityHash     string                             `json:"repository_identity_hash"`
	Repository                 executionPolicyDirectoryIdentity   `json:"repository"`
	GitExecutable              executionPolicyFileIdentity        `json:"git_executable"`
	GitCommit                  string                             `json:"git_commit"`
	GitCommitObjectSHA256      string                             `json:"git_commit_object_sha256"`
	GitTree                    string                             `json:"git_tree"`
	SourceArchiveSHA256        string                             `json:"source_archive_sha256"`
	PromptTemplateID           string                             `json:"prompt_template_id"`
	PromptTemplateHash         string                             `json:"prompt_template_hash"`
	RouteMappingHash           string                             `json:"route_mapping_hash"`
	Wrapper                    executionPolicyFileIdentity        `json:"wrapper"`
	OMPExecutable              executionPolicyFileIdentity        `json:"omp_executable"`
	OMPExecutableRoot          executionPolicyDirectoryIdentity   `json:"omp_executable_root"`
	OMPVersion                 string                             `json:"omp_version"`
	OMPNativeAddon             executionPolicyFileIdentity        `json:"omp_native_addon"`
	OMPRuntimeAuthority        executionPolicyOMPRuntimeAuthority `json:"omp_runtime_authority"`
	WrapperExecutables         []executionPolicyFileIdentity      `json:"wrapper_executables"`
	HermesProvider             string                             `json:"hermes_provider"`
	HermesModel                string                             `json:"hermes_model"`
	ProviderEndpoint           executionPolicyEndpoint            `json:"provider_endpoint"`
	TaskTier                   string                             `json:"task_tier"`
	InternalDeadlineSeconds    int                                `json:"internal_deadline_seconds"`
	WrapperGraceSeconds        int                                `json:"wrapper_grace_seconds"`
	AttemptCap                 int                                `json:"attempt_cap"`
	AllowedTests               []executionPolicyTestCommand       `json:"allowed_tests"`
	RuntimeReadRoots           []executionPolicyDirectoryIdentity `json:"runtime_read_roots"`
	ExecutableRoots            []executionPolicyDirectoryIdentity `json:"executable_roots"`
	CredentialEnvironmentNames []string                           `json:"credential_environment_names"`
	PromptRoot                 string                             `json:"prompt_root"`
	OutputRoot                 string                             `json:"output_root"`
	SessionRoot                string                             `json:"session_root"`
	WorkRoot                   string                             `json:"work_root"`
	TemporaryRoot              string                             `json:"temporary_root"`
	PolicyHash                 string                             `json:"policy_hash"`
}

type executionPolicy struct {
	path                           string
	device                         string
	inode                          string
	ownerUID                       uint32
	size                           int64
	contentSHA256                  string
	canonicalBytes                 []byte
	entries                        map[string]executionPolicyEntry
	rootPins                       map[string]executionPolicyDirectoryIdentity
	physicalPins                   map[string]string
	protectedPaths                 []string
	protectedPathsInvalid          bool
	testBrokerDependencies         auditBrokerDependencies
	atomicRuntimeAuthorityVerifier atomicRuntimeAuthorityVerifier
	namespaceAuthority             *auditNamespaceAuthority
}

func readOnlyAuditPromptTemplateHash() string {
	return hashJournalBytes([]byte(readOnlyAuditPromptTemplate))
}

func auditPromptSHA256(synthesizeOnly bool) string {
	prompt := readOnlyAuditPromptTemplate
	if synthesizeOnly {
		prompt += readOnlyAuditSynthesizePromptSuffix
	}
	return hashJournalBytes([]byte(prompt))
}

func sealExecutionPolicyEntry(entry executionPolicyEntry) (executionPolicyEntry, error) {
	entry.PolicyHash = ""
	hash, err := canonicalHash(entry)
	if err != nil {
		return executionPolicyEntry{}, err
	}
	entry.PolicyHash = hash
	return entry, nil
}

func loadExecutionPolicy(path string, ownerUID, runtimeUID, runtimeGID uint32) (*executionPolicy, error) {
	return loadExecutionPolicyWithNamespaceAuthority(path, ownerUID, productionAuditNamespaceAuthorityOptions(ownerUID, runtimeUID, runtimeGID))
}

func loadExecutionPolicyWithNamespaceAuthority(path string, ownerUID uint32, namespaceOptions auditNamespaceAuthorityOptions) (*executionPolicy, error) {
	if path == "" || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
		return nil, authenticationError("absolute execution policy path required")
	}
	contents, opened, err := readPinnedRegularFile(path, ownerUID, true, maxExecutionPolicyBytes)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(contents)
	policy := &executionPolicy{
		path: path, device: opened.Device, inode: opened.Inode, ownerUID: ownerUID,
		size: opened.Size, contentSHA256: opened.SHA256, canonicalBytes: append([]byte(nil), contents...),
		entries: make(map[string]executionPolicyEntry), rootPins: make(map[string]executionPolicyDirectoryIdentity),
		physicalPins: make(map[string]string), protectedPaths: []string{path},
	}
	namespacePaths := make([]string, 0)
	var document executionPolicyFile
	if err := decodeCanonical(contents, &document); err != nil {
		return nil, authenticationError("execution policy closed canonical schema")
	}
	if document.SchemaVersion != executionPolicySchemaVersion || len(document.Executions) == 0 || len(document.Executions) > maxExecutionPolicyEntries {
		return nil, authenticationError("execution policy binding")
	}
	for _, entry := range document.Executions {
		if err := validateExecutionPolicyEntry(entry, ownerUID); err != nil {
			return nil, err
		}
		physicalPins, err := pinExecutionPolicyPhysicalAuthorities(entry, ownerUID)
		if err != nil {
			return nil, err
		}
		for authorityPath, physicalPath := range physicalPins {
			if pinnedPath, exists := policy.physicalPins[authorityPath]; exists && pinnedPath != physicalPath {
				return nil, authenticationError("execution policy physical authority changed during load")
			}
			policy.physicalPins[authorityPath] = physicalPath
		}
		if _, duplicate := policy.entries[entry.LaunchSpecHash]; duplicate {
			return nil, authenticationError("duplicate execution policy launch hash")
		}
		for _, root := range executionEntryRoots(entry) {
			pin, err := inspectPinnedDirectory(root, ownerUID, false)
			if err != nil {
				return nil, err
			}
			policy.rootPins[root] = pin
		}
		namespacePaths = append(namespacePaths, executionEntryRoots(entry)...)
		policy.entries[entry.LaunchSpecHash] = cloneExecutionPolicyEntry(entry)
	}
	namespaceAuthority, err := openAuditNamespaceAuthority(namespacePaths, namespaceOptions)
	if err != nil {
		return nil, err
	}
	policy.namespaceAuthority = namespaceAuthority
	if err := policy.validateIdentity(); err != nil {
		_ = policy.namespaceAuthority.Close()
		return nil, err
	}
	return policy, nil
}

func (policy *executionPolicy) Close() error {
	if policy == nil || policy.namespaceAuthority == nil {
		return nil
	}
	return policy.namespaceAuthority.Close()
}

func validateExecutionPolicyEntry(entry executionPolicyEntry, ownerUID uint32) error {
	sealed, err := sealExecutionPolicyEntry(entry)
	if err != nil || entry.SchemaVersion != executionPolicyEntrySchemaVersion || sealed.PolicyHash != entry.PolicyHash ||
		!protocolHashPattern.MatchString(entry.LaunchSpecHash) || !executionTaskIDPattern.MatchString(entry.TaskID) ||
		strings.TrimSpace(entry.RepositoryIdentity) == "" || entry.RepositoryIdentityHash != repositoryIdentityHash(entry.RepositoryIdentity) ||
		!gitObjectIDPattern.MatchString(entry.GitCommit) || !gitObjectIDPattern.MatchString(entry.GitTree) ||
		!protocolHashPattern.MatchString(entry.GitCommitObjectSHA256) || !protocolHashPattern.MatchString(entry.SourceArchiveSHA256) ||
		entry.PromptTemplateID != readOnlyAuditPromptTemplateID || entry.PromptTemplateHash != readOnlyAuditPromptTemplateHash() ||
		!protocolHashPattern.MatchString(entry.RouteMappingHash) || entry.OMPVersion != supportedOMPVersion || !validAuditModelRoute(entry) ||
		!executionValuePattern.MatchString(entry.TaskTier) || entry.InternalDeadlineSeconds < 1 || entry.InternalDeadlineSeconds > 86400 ||
		entry.WrapperGraceSeconds < 1 || entry.WrapperGraceSeconds > 300 || entry.AttemptCap < 1 || entry.AttemptCap > 10 ||
		len(entry.AllowedTests) == 0 || len(entry.AllowedTests) > maxExecutionPolicyTests {
		return authenticationError("execution policy entry")
	}
	if filepath.Base(entry.Wrapper.Path) != "omp_with_timeout.sh" || entry.Wrapper.OwnerUID != ownerUID {
		return authenticationError("execution policy wrapper authority")
	}
	if filepath.Base(entry.OMPExecutable.Path) != "omp" || validatePinnedOMPExecutableRoot(entry.OMPExecutableRoot, entry.OMPExecutable) != nil ||
		validateOMPExecutableIdentity(entry.OMPExecutable) != nil || validateOMPNativeAddonIdentity(entry.OMPVersion, entry.OMPNativeAddon, ownerUID) != nil ||
		!validAuditWrapperExecutables(entry.WrapperExecutables) {
		return authenticationError("execution policy OMP and wrapper executable authority")
	}
	if err := validateExecutionPolicyAtomicRuntimeAuthority(entry); err != nil {
		return err
	}
	seenTestIDs := make(map[string]struct{}, len(entry.AllowedTests))
	seenTestHashes := make(map[string]struct{}, len(entry.AllowedTests))
	for _, test := range entry.AllowedTests {
		if !validSealedExecutionPolicyTestCommand(test) || !executionTaskIDPattern.MatchString(test.ID) ||
			test.TimeoutSeconds < 1 || test.TimeoutSeconds > 600 || len(test.Arguments) > 64 ||
			validatePinnedTestExecutableRoot(test.ExecutableRoot, test.Executable) != nil ||
			validateFileIdentity(test.Executable, false, ownerUID) != nil {
			return authenticationError("execution policy test command")
		}
		for _, argument := range test.Arguments {
			if len(argument) > 4096 || strings.IndexByte(argument, 0) >= 0 {
				return authenticationError("execution policy test argv")
			}
		}
		for _, root := range append([]string{entry.Repository.Path}, executionEntryRoots(entry)...) {
			if pathsOverlap(test.Executable.Path, root) {
				return authenticationError("execution policy test executable authority")
			}
		}
		if _, duplicate := seenTestIDs[test.ID]; duplicate {
			return authenticationError("duplicate execution policy test ID")
		}
		if _, duplicate := seenTestHashes[test.CommandSHA256]; duplicate {
			return authenticationError("duplicate execution policy test command")
		}
		seenTestIDs[test.ID] = struct{}{}
		seenTestHashes[test.CommandSHA256] = struct{}{}
	}
	roots := executionEntryRoots(entry)
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) || strings.IndexByte(root, 0) >= 0 || filepath.Clean(root) != root {
			return authenticationError("execution policy root")
		}
	}
	for left := range roots {
		for right := left + 1; right < len(roots); right++ {
			if pathsOverlap(roots[left], roots[right]) {
				return authenticationError("execution policy roots overlap")
			}
		}
	}
	if pathsOverlap(entry.Repository.Path, entry.PromptRoot) || pathsOverlap(entry.Repository.Path, entry.OutputRoot) ||
		pathsOverlap(entry.Repository.Path, entry.SessionRoot) || pathsOverlap(entry.Repository.Path, entry.WorkRoot) ||
		pathsOverlap(entry.Repository.Path, entry.TemporaryRoot) {
		return authenticationError("execution policy repository overlaps roots")
	}
	for _, root := range append([]string{entry.Repository.Path}, roots...) {
		if pathsOverlap(entry.OMPNativeAddon.Path, root) {
			return authenticationError("execution policy OMP native addon overlaps source or invocation root")
		}
	}
	if err := validateDirectoryIdentity(entry.Repository, ownerUID); err != nil {
		return err
	}
	if err := validateGitExecutableIdentity(entry.GitExecutable); err != nil {
		return fmt.Errorf("git executable: %w", err)
	}
	if err := validateFileIdentity(entry.Wrapper, true, ownerUID); err != nil {
		return fmt.Errorf("OMP wrapper: %w", err)
	}
	if err := validateSandboxPolicyAuthority(entry); err != nil {
		return err
	}
	return nil
}

func (policy *executionPolicy) Resolve(envelope store.ExternalSupervisorEnvelope) (executionPolicyEntry, error) {
	if policy == nil || !protocolHashPattern.MatchString(envelope.LaunchSpecHash) {
		return executionPolicyEntry{}, authenticationError("execution policy lookup")
	}
	if err := policy.validateIdentity(); err != nil {
		return executionPolicyEntry{}, err
	}
	entry, found := policy.entries[envelope.LaunchSpecHash]
	if !found || entry.RepositoryIdentity != envelope.RepositoryIdentity || entry.RepositoryIdentityHash != repositoryIdentityHash(envelope.RepositoryIdentity) ||
		entry.RouteMappingHash != envelope.RouteMappingHash || entry.AttemptCap != envelope.AttemptCap {
		return executionPolicyEntry{}, authenticationError("execution policy envelope binding")
	}
	if err := policy.ValidateEffectBoundary(entry); err != nil {
		return executionPolicyEntry{}, err
	}
	return cloneExecutionPolicyEntry(entry), nil
}

func (policy *executionPolicy) ValidateEffectBoundary(entry executionPolicyEntry) error {
	return policy.validateEffectBoundary(entry, true)
}

func (policy *executionPolicy) ValidateEffectBoundaryWithFrozenWrapper(entry executionPolicyEntry, frozenWrapper []byte) error {
	if err := policy.validateEffectBoundary(entry, false); err != nil {
		return err
	}
	if int64(len(frozenWrapper)) != entry.Wrapper.Size || hashJournalBytes(frozenWrapper) != entry.Wrapper.SHA256 {
		return authenticationError("frozen audit wrapper command binding")
	}
	return nil
}

func (policy *executionPolicy) validateEffectBoundary(entry executionPolicyEntry, validateWrapperPath bool) error {
	if policy == nil {
		return authenticationError("execution policy identity")
	}
	if err := policy.validateIdentity(); err != nil {
		return err
	}
	stored, found := policy.entries[entry.LaunchSpecHash]
	storedBytes, storedErr := marshalCanonical(stored)
	entryBytes, entryErr := marshalCanonical(entry)
	if !found || storedErr != nil || entryErr != nil || !bytes.Equal(storedBytes, entryBytes) {
		return authenticationError("execution policy selected entry drift")
	}
	if err := validateDirectoryIdentity(stored.Repository, policy.ownerUID); err != nil {
		return err
	}
	if err := validateGitExecutableIdentity(stored.GitExecutable); err != nil {
		return err
	}
	if validateWrapperPath {
		if err := validateFileIdentity(stored.Wrapper, true, policy.ownerUID); err != nil {
			return err
		}
	}
	if err := validatePinnedOMPExecutableRoot(stored.OMPExecutableRoot, stored.OMPExecutable); err != nil {
		return err
	}
	if err := validateOMPExecutableIdentity(stored.OMPExecutable); err != nil {
		return err
	}
	if err := validateOMPNativeAddonIdentity(stored.OMPVersion, stored.OMPNativeAddon, policy.ownerUID); err != nil {
		return err
	}
	if !validAuditWrapperExecutables(stored.WrapperExecutables) {
		return authenticationError("execution policy wrapper executable replacement")
	}
	for _, test := range stored.AllowedTests {
		if err := validatePinnedTestExecutableRoot(test.ExecutableRoot, test.Executable); err != nil {
			return err
		}
		if err := validateFileIdentity(test.Executable, false, policy.ownerUID); err != nil {
			return err
		}
	}
	if err := validateSandboxPolicyAuthority(stored); err != nil {
		return err
	}
	physicalPins, err := pinExecutionPolicyPhysicalAuthorities(stored, policy.ownerUID)
	if err != nil {
		return err
	}
	for authorityPath, physicalPath := range physicalPins {
		if pinnedPath, exists := policy.physicalPins[authorityPath]; !exists || pinnedPath != physicalPath {
			return authenticationError("execution policy physical authority replacement")
		}
	}
	for _, root := range executionEntryRoots(stored) {
		if policy.namespaceAuthority == nil {
			return unsupportedInvocationNamespace(InvocationNamespaceIdentityChanged)
		}
		if err := policy.namespaceAuthority.ValidateRoot(root); err != nil {
			return err
		}
	}
	return nil
}

func (policy *executionPolicy) validateIdentity() error {
	if policy == nil || policy.protectedPathsInvalid || len(policy.canonicalBytes) == 0 || policy.size != int64(len(policy.canonicalBytes)) ||
		policy.contentSHA256 != hashJournalBytes(policy.canonicalBytes) {
		return authenticationError("execution policy identity")
	}
	contents, actual, err := readPinnedRegularFile(policy.path, policy.ownerUID, true, maxExecutionPolicyBytes)
	if err != nil {
		return authenticationError("execution policy replacement")
	}
	defer zeroBytes(contents)
	if actual.Device != policy.device || actual.Inode != policy.inode || actual.OwnerUID != policy.ownerUID || actual.Mode != 0o600 ||
		actual.Size != policy.size || actual.SHA256 != policy.contentSHA256 || hashJournalBytes(contents) != policy.contentSHA256 ||
		!bytes.Equal(contents, policy.canonicalBytes) {
		return authenticationError("execution policy replacement")
	}
	return nil
}

func validateFileIdentity(identity executionPolicyFileIdentity, requireOwner bool, ownerUID uint32) error {
	if identity.Path == "" || !filepath.IsAbs(identity.Path) || filepath.Clean(identity.Path) != identity.Path ||
		strings.IndexByte(identity.Path, 0) >= 0 || !protocolHashPattern.MatchString(identity.SHA256) || identity.Size < 1 ||
		!validStatDecimal(identity.Device) || !validStatDecimal(identity.Inode) || identity.Mode&0o022 != 0 ||
		identity.Mode&0o111 == 0 || (requireOwner && identity.OwnerUID != ownerUID) {
		return authenticationError("execution policy file identity")
	}
	contents, actual, err := readPinnedRegularFile(identity.Path, identity.OwnerUID, false, maxExecutionPolicyBytes*8)
	if err != nil {
		return err
	}
	defer zeroBytes(contents)
	if actual.Path != identity.Path {
		return authenticationError("execution policy file path replacement")
	}
	if actual.Device != identity.Device {
		return authenticationError("execution policy file device replacement")
	}
	if actual.Inode != identity.Inode {
		return authenticationError("execution policy file inode replacement")
	}
	if actual.OwnerUID != identity.OwnerUID {
		return authenticationError("execution policy file owner replacement")
	}
	if actual.Mode != identity.Mode || actual.Size != identity.Size {
		return authenticationError("execution policy file metadata replacement")
	}
	if actual.SHA256 != identity.SHA256 || hashJournalBytes(contents) != identity.SHA256 {
		return authenticationError("execution policy file content replacement")
	}
	return nil
}

func validatePinnedTestExecutableRoot(root executionPolicyDirectoryIdentity, executable executionPolicyFileIdentity) error {
	if root.Path == "" || !filepath.IsAbs(root.Path) || filepath.Clean(root.Path) != root.Path || strings.IndexByte(root.Path, 0) >= 0 ||
		filepath.Dir(executable.Path) != root.Path || root.OwnerUID != executable.OwnerUID || !validStatDecimal(root.Device) || !validStatDecimal(root.Inode) {
		return authenticationError("pinned supervisor test executable root")
	}
	information, err := os.Lstat(root.Path)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() || information.Mode().Perm()&0o022 != 0 {
		return authenticationError("pinned supervisor test executable root authority")
	}
	actual, err := inspectPinnedDirectory(root.Path, root.OwnerUID, false)
	if err != nil || actual != root {
		return authenticationError("pinned supervisor test executable root replacement")
	}
	return nil
}

func validateGitExecutableIdentity(identity executionPolicyFileIdentity) error {
	if identity.Path != auditGitExecutable || identity.OwnerUID != 0 {
		return authenticationError("immutable system Git executable")
	}
	return validateFileIdentity(identity, true, 0)
}

func validatePinnedOMPExecutableRoot(root executionPolicyDirectoryIdentity, executable executionPolicyFileIdentity) error {
	if root.Path == "" || !filepath.IsAbs(root.Path) || filepath.Clean(root.Path) != root.Path || strings.IndexByte(root.Path, 0) >= 0 ||
		filepath.Dir(executable.Path) != root.Path || root.OwnerUID != executable.OwnerUID || !validStatDecimal(root.Device) || !validStatDecimal(root.Inode) {
		return authenticationError("pinned OMP executable root")
	}
	actual, err := inspectPinnedDirectory(root.Path, root.OwnerUID, false)
	if err != nil || actual != root {
		return authenticationError("pinned OMP executable root replacement")
	}
	return nil
}

func validateOMPExecutableIdentity(identity executionPolicyFileIdentity) error {
	if identity.Path == "" || filepath.Base(identity.Path) != "omp" || !filepath.IsAbs(identity.Path) || filepath.Clean(identity.Path) != identity.Path ||
		strings.IndexByte(identity.Path, 0) >= 0 || !protocolHashPattern.MatchString(identity.SHA256) || identity.Size < 1 ||
		identity.Size > maxAuditOMPExecutableBytes || !validStatDecimal(identity.Device) || !validStatDecimal(identity.Inode) ||
		identity.Mode&0o022 != 0 || identity.Mode&0o111 == 0 {
		return authenticationError("execution policy OMP executable identity")
	}
	contents, actual, err := readPinnedRegularFile(identity.Path, identity.OwnerUID, false, maxAuditOMPExecutableBytes)
	if err != nil {
		return authenticationError("pinned OMP executable")
	}
	defer zeroBytes(contents)
	if actual != identity || hashJournalBytes(contents) != identity.SHA256 {
		return authenticationError("pinned OMP executable replacement")
	}
	return nil
}

func validateOMPNativeAddonIdentity(version string, identity executionPolicyFileIdentity, _ uint32) error {
	contents, err := readValidatedOMPNativeAddon(version, identity, 0)
	if err != nil {
		return err
	}
	zeroBytes(contents)
	return nil
}

func readValidatedOMPNativeAddon(version string, identity executionPolicyFileIdentity, _ uint32) ([]byte, error) {
	versionDirectory := filepath.Dir(identity.Path)
	nativesDirectory := filepath.Dir(versionDirectory)
	ompDirectory := filepath.Dir(nativesDirectory)
	if version != supportedOMPVersion || identity.Path == "" || !filepath.IsAbs(identity.Path) || filepath.Clean(identity.Path) != identity.Path ||
		strings.IndexByte(identity.Path, 0) >= 0 || filepath.Base(identity.Path) != auditOMPNativeAddonFilename ||
		filepath.Base(versionDirectory) != version || filepath.Base(nativesDirectory) != "natives" ||
		(filepath.Base(ompDirectory) != ".omp" && filepath.Base(ompDirectory) != "omp") ||
		!protocolHashPattern.MatchString(identity.SHA256) || identity.Size < 1 || identity.Size > maxAuditOMPNativeAddonBytes ||
		!validStatDecimal(identity.Device) || !validStatDecimal(identity.Inode) || identity.Mode&0o022 != 0 {
		return nil, authenticationError("execution policy OMP native addon identity")
	}
	return readPinnedOMPNativeAddon(identity)
}

func readPinnedOMPNativeAddon(identity executionPolicyFileIdentity) ([]byte, error) {
	beforePath, err := os.Lstat(identity.Path)
	if err != nil || !fileInformationMatchesIdentity(beforePath, identity) {
		return nil, authenticationError("pinned OMP native addon path before read")
	}
	fd, err := unix.Open(identity.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, authenticationError("open pinned OMP native addon")
	}
	file := os.NewFile(uintptr(fd), auditOMPNativeAddonFilename)
	if file == nil {
		_ = unix.Close(fd)
		return nil, authenticationError("open pinned OMP native addon descriptor")
	}
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || !auditWrapperStatMatches(before, identity) {
		return nil, authenticationError("pinned OMP native addon identity before read")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxAuditOMPNativeAddonBytes+1))
	if err != nil {
		zeroBytes(contents)
		return nil, authenticationError("read pinned OMP native addon")
	}
	var after unix.Stat_t
	afterPath, pathErr := os.Lstat(identity.Path)
	if err := unix.Fstat(fd, &after); err != nil || pathErr != nil || !auditWrapperStatMatches(after, identity) ||
		!fileInformationMatchesIdentity(afterPath, identity) || !sameAuditWrapperStat(before, after) ||
		int64(len(contents)) != identity.Size || hashJournalBytes(contents) != identity.SHA256 {
		zeroBytes(contents)
		return nil, authenticationError("pinned OMP native addon changed during read")
	}
	return contents, nil
}

func fileInformationMatchesIdentity(information os.FileInfo, identity executionPolicyFileIdentity) bool {
	if information == nil || information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() ||
		uint32(information.Mode().Perm()) != identity.Mode || information.Size() != identity.Size {
		return false
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	return ok && statDecimal(uint64(status.Dev)) == identity.Device && statDecimal(status.Ino) == identity.Inode && status.Uid == identity.OwnerUID
}

func auditWrapperDependencyPaths() []string {
	return append([]string(nil), auditWrapperSystemExecutables...)
}

func validAuditWrapperExecutables(identities []executionPolicyFileIdentity) bool {
	if len(identities) < len(auditWrapperSystemExecutables) {
		return false
	}
	seen := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		if _, duplicate := seen[identity.Path]; duplicate {
			return false
		}
		if _, allowedRoot := auditExecutableRootAllowlist[filepath.Dir(identity.Path)]; !allowedRoot ||
			identity.OwnerUID != 0 || validateFileIdentity(identity, true, 0) != nil {
			return false
		}
		seen[identity.Path] = struct{}{}
	}
	for _, path := range auditWrapperSystemExecutables {
		if _, found := seen[path]; !found {
			return false
		}
	}
	return true
}

func validateSandboxPolicyAuthority(entry executionPolicyEntry) error {
	if len(entry.RuntimeReadRoots) == 0 || len(entry.ExecutableRoots) == 0 || !validRouteCredentialNames(entry.HermesProvider, entry.CredentialEnvironmentNames) {
		return authenticationError("execution policy sandbox authority")
	}
	readRoots := make(map[string]struct{}, len(entry.RuntimeReadRoots))
	for _, root := range entry.RuntimeReadRoots {
		if _, allowed := auditRuntimeReadRootAllowlist[root.Path]; !allowed {
			return authenticationError("execution policy runtime read root")
		}
		if _, duplicate := readRoots[root.Path]; duplicate {
			return authenticationError("duplicate execution policy runtime read root")
		}
		if err := validateSystemDirectoryIdentity(root); err != nil {
			return err
		}
		readRoots[root.Path] = struct{}{}
	}
	executableRoots := make(map[string]struct{}, len(entry.ExecutableRoots))
	for _, root := range entry.ExecutableRoots {
		if _, allowed := auditExecutableRootAllowlist[root.Path]; !allowed {
			return authenticationError("execution policy executable root")
		}
		if _, readable := readRoots[root.Path]; !readable {
			return authenticationError("execution policy executable root is not readable")
		}
		if _, duplicate := executableRoots[root.Path]; duplicate {
			return authenticationError("duplicate execution policy executable root")
		}
		if err := validateSystemDirectoryIdentity(root); err != nil {
			return err
		}
		executableRoots[root.Path] = struct{}{}
	}
	for _, required := range []string{"/bin", "/usr/bin"} {
		if _, found := executableRoots[required]; !found {
			return authenticationError("missing execution policy executable root")
		}
	}
	return nil
}

func validateSystemDirectoryIdentity(identity executionPolicyDirectoryIdentity) error {
	if identity.OwnerUID != 0 {
		return authenticationError("root-owned immutable system directory")
	}
	actual, err := inspectPinnedDirectory(identity.Path, 0, false)
	if err != nil || actual != identity {
		return authenticationError("root-owned immutable system directory")
	}
	return nil
}

func validAuditProviderEndpoint(provider string, endpoint executionPolicyEndpoint) bool {
	expected, found := auditProviderEndpointAllowlist[provider]
	return found && endpoint == expected && endpoint.Port == 443
}

func validAuditModelDeclaration(entry executionPolicyEntry) bool {
	return entry.HermesProvider == "custom:sudo" && entry.HermesModel == "gpt-5.6-sol" &&
		validRouteCredentialNames(entry.HermesProvider, entry.CredentialEnvironmentNames)
}

func validAuditModelRoute(entry executionPolicyEntry) bool {
	return validAuditModelDeclaration(entry) && validAuditProviderEndpoint(entry.HermesProvider, entry.ProviderEndpoint)
}

func validRouteCredentialNames(provider string, names []string) bool {
	options, found := auditProviderCredentialOptions[provider]
	if !found || len(names) != 1 {
		return false
	}
	if _, allowed := auditCredentialEnvironmentAllowlist[names[0]]; !allowed {
		return false
	}
	for _, option := range options {
		if len(option) == 1 && option[0] == names[0] {
			return true
		}
	}
	return false
}

func validateDirectoryIdentity(identity executionPolicyDirectoryIdentity, ownerUID uint32) error {
	if identity.Path == "" || !filepath.IsAbs(identity.Path) || filepath.Clean(identity.Path) != identity.Path ||
		strings.IndexByte(identity.Path, 0) >= 0 || identity.OwnerUID != ownerUID ||
		!validStatDecimal(identity.Device) || !validStatDecimal(identity.Inode) {
		return authenticationError("execution policy repository identity")
	}
	actual, err := inspectPinnedDirectory(identity.Path, ownerUID, false)
	if err != nil {
		return err
	}
	if actual != identity {
		return authenticationError("execution policy repository replacement")
	}
	return nil
}

func readPinnedRegularFile(path string, ownerUID uint32, require0600 bool, limit int64) ([]byte, executionPolicyFileIdentity, error) {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || (require0600 && before.Mode().Perm() != 0o600) {
		return nil, executionPolicyFileIdentity{}, authenticationError("operator file type or mode")
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || beforeStat.Uid != ownerUID {
		return nil, executionPolicyFileIdentity{}, authenticationError("operator file owner")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, executionPolicyFileIdentity{}, authenticationError("open pinned operator file")
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, executionPolicyFileIdentity{}, authenticationError("open pinned operator descriptor")
	}
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Uid != ownerUID ||
		uint64(opened.Dev) != uint64(beforeStat.Dev) || opened.Ino != beforeStat.Ino || (require0600 && opened.Mode&0o777 != 0o600) {
		return nil, executionPolicyFileIdentity{}, authenticationError("operator file replaced")
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, executionPolicyFileIdentity{}, authenticationError("read pinned operator file")
	}
	if len(contents) == 0 || int64(len(contents)) > limit || opened.Size != int64(len(contents)) {
		zeroBytes(contents)
		return nil, executionPolicyFileIdentity{}, fmt.Errorf("%w: operator file size", ErrLimit)
	}
	identity := executionPolicyFileIdentity{
		Path: path, SHA256: hashJournalBytes(contents), Device: statDecimal(uint64(opened.Dev)), Inode: statDecimal(opened.Ino),
		OwnerUID: opened.Uid, Mode: uint32(opened.Mode & 0o777), Size: opened.Size,
	}
	return contents, identity, nil
}

func inspectPinnedDirectory(path string, ownerUID uint32, require0700 bool) (executionPolicyDirectoryIdentity, error) {
	information, err := os.Lstat(path)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() ||
		(require0700 && information.Mode().Perm() != 0o700) || (!require0700 && information.Mode().Perm()&0o022 != 0) {
		return executionPolicyDirectoryIdentity{}, authenticationError("operator directory type or mode")
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != ownerUID {
		return executionPolicyDirectoryIdentity{}, authenticationError("operator directory owner")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return executionPolicyDirectoryIdentity{}, authenticationError("open pinned operator directory")
	}
	defer unix.Close(fd)
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFDIR || opened.Uid != ownerUID ||
		uint64(opened.Dev) != uint64(status.Dev) || opened.Ino != status.Ino ||
		(require0700 && opened.Mode&0o777 != 0o700) || (!require0700 && opened.Mode&0o022 != 0) {
		return executionPolicyDirectoryIdentity{}, authenticationError("operator directory replaced")
	}
	return executionPolicyDirectoryIdentity{Path: path, Device: statDecimal(uint64(opened.Dev)), Inode: statDecimal(opened.Ino), OwnerUID: opened.Uid, Mode: uint32(opened.Mode & 0o777)}, nil
}

func statDecimal(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func validStatDecimal(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && statDecimal(parsed) == value
}

func executionEntryRoots(entry executionPolicyEntry) []string {
	return []string{entry.PromptRoot, entry.OutputRoot, entry.SessionRoot, entry.WorkRoot, entry.TemporaryRoot}
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right {
		return true
	}
	leftToRight, leftErr := filepath.Rel(left, right)
	rightToLeft, rightErr := filepath.Rel(right, left)
	return leftErr == nil && leftToRight != ".." && !strings.HasPrefix(leftToRight, ".."+string(filepath.Separator)) ||
		rightErr == nil && rightToLeft != ".." && !strings.HasPrefix(rightToLeft, ".."+string(filepath.Separator))
}

func pinExecutionPolicyPhysicalAuthorities(entry executionPolicyEntry, ownerUID uint32) (map[string]string, error) {
	pins := make(map[string]string, len(executionEntryRoots(entry))+len(entry.AllowedTests)*2+2)
	invocationPaths := executionEntryRoots(entry)
	invocationPhysical := make([]string, len(invocationPaths))
	for index, path := range invocationPaths {
		identity, physicalPath, err := resolvePinnedDirectoryPhysicalPath(path, ownerUID, false)
		if err != nil {
			return nil, authenticationError("execution policy invocation root physical identity")
		}
		pins[identity.Path] = physicalPath
		invocationPhysical[index] = physicalPath
	}
	for left := range invocationPhysical {
		for right := left + 1; right < len(invocationPhysical); right++ {
			if pathsOverlap(invocationPhysical[left], invocationPhysical[right]) {
				return nil, authenticationError("execution policy physical invocation roots overlap")
			}
		}
	}
	repositoryIdentity, repositoryPhysical, err := resolvePinnedDirectoryPhysicalPath(entry.Repository.Path, ownerUID, false)
	if err != nil || repositoryIdentity != entry.Repository {
		return nil, authenticationError("execution policy repository physical identity")
	}
	pins[entry.Repository.Path] = repositoryPhysical
	for _, physicalRoot := range invocationPhysical {
		if pathsOverlap(repositoryPhysical, physicalRoot) {
			return nil, authenticationError("execution policy repository physically overlaps invocation root")
		}
	}
	nativePhysical, err := resolvePinnedRegularFilePhysicalPath(entry.OMPNativeAddon)
	if err != nil {
		return nil, authenticationError("execution policy OMP native addon physical identity")
	}
	pins[entry.OMPNativeAddon.Path] = nativePhysical
	if pathsOverlap(nativePhysical, repositoryPhysical) {
		return nil, authenticationError("execution policy OMP native addon physically overlaps repository")
	}
	for _, physicalRoot := range invocationPhysical {
		if pathsOverlap(nativePhysical, physicalRoot) {
			return nil, authenticationError("execution policy OMP native addon physically overlaps invocation root")
		}
	}
	for _, test := range entry.AllowedTests {
		rootIdentity, rootPhysical, err := resolvePinnedDirectoryPhysicalPath(test.ExecutableRoot.Path, test.ExecutableRoot.OwnerUID, false)
		if err != nil || rootIdentity != test.ExecutableRoot {
			return nil, authenticationError("execution policy test executable root physical identity")
		}
		executablePhysical, err := resolvePinnedRegularFilePhysicalPath(test.Executable)
		if err != nil || filepath.Dir(executablePhysical) != rootPhysical {
			return nil, authenticationError("execution policy test executable physical identity")
		}
		pins[test.ExecutableRoot.Path] = rootPhysical
		pins[test.Executable.Path] = executablePhysical
		if pathsOverlap(rootPhysical, repositoryPhysical) || pathsOverlap(rootPhysical, nativePhysical) {
			return nil, authenticationError("execution policy test executable root physically overlaps repository or native addon")
		}
		for _, physicalRoot := range invocationPhysical {
			if pathsOverlap(rootPhysical, physicalRoot) {
				return nil, authenticationError("execution policy test executable root physically overlaps invocation root")
			}
		}
	}
	return pins, nil
}

func resolvePinnedDirectoryPhysicalPath(path string, ownerUID uint32, require0700 bool) (executionPolicyDirectoryIdentity, string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return executionPolicyDirectoryIdentity{}, "", authenticationError("physical directory path")
	}
	before, err := inspectPinnedDirectory(path, ownerUID, require0700)
	if err != nil {
		return executionPolicyDirectoryIdentity{}, "", err
	}
	physicalPath, err := filepath.EvalSymlinks(path)
	if err != nil || physicalPath == "" || !filepath.IsAbs(physicalPath) || filepath.Clean(physicalPath) != physicalPath || strings.IndexByte(physicalPath, 0) >= 0 {
		return executionPolicyDirectoryIdentity{}, "", authenticationError("resolve physical directory path")
	}
	physicalIdentity, err := inspectPinnedDirectory(physicalPath, ownerUID, require0700)
	if err != nil || !samePinnedDirectoryIdentity(before, physicalIdentity) {
		return executionPolicyDirectoryIdentity{}, "", authenticationError("physical directory descriptor identity")
	}
	after, err := inspectPinnedDirectory(path, ownerUID, require0700)
	resolvedAfter, resolveErr := filepath.EvalSymlinks(path)
	if err != nil || resolveErr != nil || after != before || resolvedAfter != physicalPath {
		return executionPolicyDirectoryIdentity{}, "", authenticationError("physical directory resolution changed")
	}
	return before, physicalPath, nil
}

func samePinnedDirectoryIdentity(left, right executionPolicyDirectoryIdentity) bool {
	return left.Device == right.Device && left.Inode == right.Inode && left.OwnerUID == right.OwnerUID
}

func resolvePinnedRegularFilePhysicalPath(identity executionPolicyFileIdentity) (string, error) {
	if identity.Path == "" || !filepath.IsAbs(identity.Path) || filepath.Clean(identity.Path) != identity.Path || strings.IndexByte(identity.Path, 0) >= 0 ||
		!pinnedRegularFilePathMatchesIdentity(identity.Path, identity) {
		return "", authenticationError("physical regular file path")
	}
	physicalPath, err := filepath.EvalSymlinks(identity.Path)
	if err != nil || physicalPath == "" || !filepath.IsAbs(physicalPath) || filepath.Clean(physicalPath) != physicalPath || strings.IndexByte(physicalPath, 0) >= 0 ||
		!pinnedRegularFilePathMatchesIdentity(physicalPath, identity) {
		return "", authenticationError("resolve physical regular file path")
	}
	resolvedAfter, err := filepath.EvalSymlinks(identity.Path)
	if err != nil || resolvedAfter != physicalPath || !pinnedRegularFilePathMatchesIdentity(identity.Path, identity) {
		return "", authenticationError("physical regular file resolution changed")
	}
	return physicalPath, nil
}

func pinnedRegularFilePathMatchesIdentity(path string, identity executionPolicyFileIdentity) bool {
	information, err := os.Lstat(path)
	if err != nil || !fileInformationMatchesIdentity(information, identity) {
		return false
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	var opened unix.Stat_t
	return unix.Fstat(fd, &opened) == nil && auditWrapperStatMatches(opened, identity)
}

func (policy *executionPolicy) setProtectedPaths(paths ...string) {
	if policy == nil {
		return
	}
	unique := make(map[string]struct{}, len(paths)+len(policy.entries)+1)
	unique[policy.path] = struct{}{}
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
			policy.protectedPathsInvalid = true
			continue
		}
		unique[path] = struct{}{}
	}
	policy.protectedPaths = policy.protectedPaths[:0]
	for path := range unique {
		policy.protectedPaths = append(policy.protectedPaths, path)
	}
	sort.Strings(policy.protectedPaths)
}

func cloneExecutionPolicyEntry(entry executionPolicyEntry) executionPolicyEntry {
	entry.AllowedTests = append([]executionPolicyTestCommand(nil), entry.AllowedTests...)
	for index := range entry.AllowedTests {
		entry.AllowedTests[index].Arguments = append([]string(nil), entry.AllowedTests[index].Arguments...)
	}
	entry.RuntimeReadRoots = append([]executionPolicyDirectoryIdentity(nil), entry.RuntimeReadRoots...)
	entry.WrapperExecutables = append([]executionPolicyFileIdentity(nil), entry.WrapperExecutables...)
	entry.OMPRuntimeAuthority = cloneExecutionPolicyOMPRuntimeAuthority(entry.OMPRuntimeAuthority)
	entry.ExecutableRoots = append([]executionPolicyDirectoryIdentity(nil), entry.ExecutableRoots...)
	entry.CredentialEnvironmentNames = append([]string(nil), entry.CredentialEnvironmentNames...)
	return entry
}
