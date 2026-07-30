package trustedsupervisor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	auditEvidenceSchemaVersion    = "ananke.local-trusted-supervisor-readonly-audit-evidence.v6"
	auditModelReportSchemaVersion = "ananke.local-trusted-supervisor-model-audit-report.v1"
	maxAuditOutputBytes           = 256 * 1024
	maxAuditModelFindings         = 256
	maxAuditModelSummaryBytes     = 4096
	maxAuditModelMessageBytes     = 4096
	maxAuditModelPathBytes        = 512
)

var auditModelFindingCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

type auditModelFinding struct {
	Code     string `json:"code"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Path     string `json:"path"`
	Severity string `json:"severity"`
}

type auditModelReport struct {
	Findings      []auditModelFinding `json:"findings"`
	SchemaVersion string              `json:"schema_version"`
	Summary       string              `json:"summary"`
	Verdict       string              `json:"verdict"`
}

func decodeAuditModelReport(contents []byte, entry executionPolicyEntry, invocation auditInvocation) (auditModelReport, error) {
	if len(contents) == 0 || len(contents) > maxAuditOutputBytes || bytes.Contains(contents, []byte("[OMP_TIMEOUT]")) || auditBytesLeakAuthority(contents, entry, invocation) {
		return auditModelReport{}, authenticationError("closed canonical audit model report")
	}
	var report auditModelReport
	if err := decodeCanonical(contents, &report); err != nil || !validAuditModelReport(report) {
		return auditModelReport{}, authenticationError("closed canonical audit model report")
	}
	return report, nil
}

func validAuditModelReport(report auditModelReport) bool {
	if report.SchemaVersion != auditModelReportSchemaVersion || len(report.Summary) == 0 || len(report.Summary) > maxAuditModelSummaryBytes ||
		strings.TrimSpace(report.Summary) != report.Summary || strings.ContainsAny(report.Summary, "\r\n\x00") ||
		containsAbsoluteAuditPath(report.Summary) || len(report.Findings) > maxAuditModelFindings ||
		report.Verdict != "approved" && report.Verdict != "rejected" ||
		report.Verdict == "approved" && len(report.Findings) != 0 || report.Verdict == "rejected" && len(report.Findings) == 0 {
		return false
	}
	severityRank := map[string]int{"blocker": 0, "high": 1, "medium": 2, "low": 3, "info": 4}
	for index, finding := range report.Findings {
		if _, ok := severityRank[finding.Severity]; !ok || !auditModelFindingCodePattern.MatchString(finding.Code) ||
			finding.Line < 1 || finding.Line > 100000000 || len(finding.Message) == 0 || len(finding.Message) > maxAuditModelMessageBytes ||
			strings.TrimSpace(finding.Message) != finding.Message || strings.ContainsAny(finding.Message, "\r\n\x00") ||
			containsAbsoluteAuditPath(finding.Message) || !validAuditModelFindingPath(finding.Path) ||
			index > 0 && !auditModelFindingLess(report.Findings[index-1], finding, severityRank) {
			return false
		}
	}
	return true
}

func validAuditModelFindingPath(value string) bool {
	if protocolHashPattern.MatchString(value) {
		return true
	}
	return value != "" && len(value) <= maxAuditModelPathBytes && !path.IsAbs(value) && path.Clean(value) == value && value != "." &&
		value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00")
}

func containsAbsoluteAuditPath(value string) bool {
	for _, token := range strings.Fields(value) {
		token = strings.Trim(token, `()[]{}<>,;:'"`)
		if strings.HasPrefix(token, "/") || len(token) >= 3 && ((token[0] >= 'A' && token[0] <= 'Z') || (token[0] >= 'a' && token[0] <= 'z')) && token[1] == ':' && (token[2] == '/' || token[2] == '\\') {
			return true
		}
	}
	return false
}

func auditModelFindingLess(left, right auditModelFinding, severityRank map[string]int) bool {
	leftRank, rightRank := severityRank[left.Severity], severityRank[right.Severity]
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	if left.Code != right.Code {
		return left.Code < right.Code
	}
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.Message < right.Message
}

type auditEvidenceTest struct {
	ID                   string `json:"id"`
	CommandSHA256        string `json:"command_sha256"`
	ExitCode             int    `json:"exit_code"`
	SandboxProfileSHA256 string `json:"sandbox_profile_sha256"`
	StdoutSHA256         string `json:"stdout_sha256"`
	StdoutSize           int64  `json:"stdout_size"`
	StderrSHA256         string `json:"stderr_sha256"`
	StderrSize           int64  `json:"stderr_size"`
}

type auditEvidenceReport struct {
	SchemaVersion             string                   `json:"schema_version"`
	IntentHash                string                   `json:"intent_hash"`
	EnvelopeHash              string                   `json:"envelope_hash"`
	LaunchSpecHash            string                   `json:"launch_spec_hash"`
	HandoffID                 string                   `json:"handoff_id"`
	ReceiptHash               string                   `json:"receipt_hash"`
	TaskID                    string                   `json:"task_id"`
	RunID                     string                   `json:"run_id"`
	Attempt                   int                      `json:"attempt"`
	AttemptCap                int                      `json:"attempt_cap"`
	PolicyHash                string                   `json:"policy_hash"`
	RouteMappingHash          string                   `json:"route_mapping_hash"`
	RepositoryIdentityHash    string                   `json:"repository_identity_hash"`
	SourceArchiveSHA256       string                   `json:"source_archive_sha256"`
	GitCommit                 string                   `json:"git_commit"`
	GitTree                   string                   `json:"git_tree"`
	WrapperSHA256             string                   `json:"wrapper_sha256"`
	OMPVersion                string                   `json:"omp_version"`
	OMPNativeAddonSHA256      string                   `json:"omp_native_addon_sha256"`
	FinalizingEventID         string                   `json:"finalizing_event_id"`
	FinalizingEventSequence   int                      `json:"finalizing_event_sequence"`
	FinalizingEventOccurredAt string                   `json:"finalizing_event_occurred_at"`
	CommandDescriptorHash     string                   `json:"command_descriptor_hash"`
	PromptSHA256              string                   `json:"prompt_sha256"`
	SessionRunID              string                   `json:"session_run_id"`
	ResumeSessionUUID         string                   `json:"resume_session_uuid"`
	SynthesizeOnly            bool                     `json:"synthesize_only"`
	SandboxProfileSHA256      string                   `json:"sandbox_profile_sha256"`
	PID                       int                      `json:"pid"`
	PGID                      int                      `json:"pgid"`
	ProcessStartIdentity      string                   `json:"process_start_identity"`
	ProcessStartedAt          string                   `json:"process_started_at"`
	ProcessFinishedAt         string                   `json:"process_finished_at"`
	ExitCode                  int                      `json:"exit_code"`
	StdoutSHA256              string                   `json:"stdout_sha256"`
	StderrSHA256              string                   `json:"stderr_sha256"`
	OutputSHA256              string                   `json:"output_sha256"`
	OutputSize                int64                    `json:"output_size"`
	ModelReportSHA256         string                   `json:"model_report_sha256"`
	ModelReport               auditModelReport         `json:"model_report"`
	TestsRun                  []auditEvidenceTest      `json:"tests_run"`
	OwnedRoots                []auditOwnedRootIdentity `json:"owned_roots"`
	SessionUUID               string                   `json:"session_uuid"`
	WorkPath                  string                   `json:"work_path"`
	OutputPath                string                   `json:"output_path"`
	SessionPath               string                   `json:"session_path"`
	PromptPath                string                   `json:"prompt_path"`
	TemporaryPath             string                   `json:"temporary_path"`
}

type collectedAuditEvidence struct {
	Report       auditEvidenceReport
	EvidenceJSON string
	EvidenceHash string
	OutputSHA256 string
	OutputSize   int64
}

func decodeAuditEvidenceReport(intent auditExecutionIntent, event auditExecutionEvent) (auditEvidenceReport, error) {
	if event.State != auditStateFinalizing && event.State != auditStateCompleted || event.EvidenceJSON == "" || len(event.EvidenceJSON) > maxAuditEvidenceBytes ||
		event.EvidenceHash != hashJournalBytes([]byte(event.EvidenceJSON)) {
		return auditEvidenceReport{}, authenticationError("typed audit evidence fingerprint")
	}
	var report auditEvidenceReport
	if err := decodeCanonical([]byte(event.EvidenceJSON), &report); err != nil || report.SchemaVersion != auditEvidenceSchemaVersion ||
		report.IntentHash != intent.IntentHash || report.EnvelopeHash != intent.EnvelopeHash || report.LaunchSpecHash != intent.LaunchSpecHash ||
		report.HandoffID != intent.HandoffID || report.ReceiptHash != intent.ReceiptHash || report.TaskID != intent.TaskID ||
		report.RunID != intent.RunID || report.Attempt != event.Attempt || report.AttemptCap != intent.AttemptCap ||
		report.PolicyHash != intent.PolicyHash || report.RouteMappingHash != intent.RouteMappingHash ||
		report.RepositoryIdentityHash != intent.RepositoryIdentityHash || report.SourceArchiveSHA256 != intent.SourceArchiveSHA256 ||
		report.GitCommit != intent.GitCommit || report.GitTree != intent.GitTree || report.WrapperSHA256 != intent.WrapperSHA256 ||
		report.CommandDescriptorHash != event.CommandDescriptorHash || report.PromptSHA256 != event.PromptSHA256 ||
		report.SessionRunID != event.SessionRunID || report.ResumeSessionUUID != event.ResumeSessionUUID ||
		report.SynthesizeOnly != event.SynthesizeOnly || report.PID != event.PID || report.PGID != event.PGID ||
		report.ProcessStartIdentity != event.ProcessStartIdentity || report.ProcessStartedAt != event.ProcessStartedAt ||
		report.ProcessFinishedAt != event.ProcessFinishedAt || report.ExitCode != event.ExitCode ||
		report.StdoutSHA256 != event.StdoutSHA256 || report.StderrSHA256 != event.StderrSHA256 ||
		report.OutputSHA256 != event.OutputSHA256 || report.OutputSize != event.OutputSize || report.SessionUUID != event.SessionUUID ||
		report.WorkPath != event.WorkPath || report.OutputPath != event.OutputPath || report.SessionPath != event.SessionPath ||
		report.PromptPath != event.PromptPath || report.TemporaryPath != event.TemporaryPath {
		return auditEvidenceReport{}, authenticationError("typed audit evidence cross-binding")
	}
	if event.State == auditStateFinalizing {
		if report.FinalizingEventID != event.EventID || report.FinalizingEventSequence != event.Sequence ||
			report.FinalizingEventOccurredAt != event.OccurredAt || event.FinalizingEventHash != "" {
			return auditEvidenceReport{}, authenticationError("typed audit evidence finalizing binding")
		}
	} else if event.Sequence < 2 || report.FinalizingEventID != auditExecutionEventID(intent, event.Sequence-1) ||
		report.FinalizingEventSequence != event.Sequence-1 || !protocolHashPattern.MatchString(event.FinalizingEventHash) {
		return auditEvidenceReport{}, authenticationError("typed audit evidence completion binding")
	}
	modelBytes, err := marshalCanonical(report.ModelReport)
	if err != nil || !validAuditModelReport(report.ModelReport) || report.ModelReportSHA256 != hashJournalBytes(modelBytes) ||
		report.OutputSHA256 != report.ModelReportSHA256 || !protocolHashPattern.MatchString(report.PromptSHA256) ||
		report.OMPVersion != supportedOMPVersion || !protocolHashPattern.MatchString(report.OMPNativeAddonSHA256) ||
		!executionTaskIDPattern.MatchString(report.SessionRunID) ||
		(report.ResumeSessionUUID != "" && !auditSessionUUIDPattern.MatchString(report.ResumeSessionUUID)) ||
		(report.SynthesizeOnly && report.ResumeSessionUUID == "") || !protocolHashPattern.MatchString(report.SandboxProfileSHA256) ||
		!validAuditEvidenceTests(report.TestsRun) || !validServerJournalTimestamp(report.ProcessStartedAt) ||
		!validServerJournalTimestamp(report.ProcessFinishedAt) || !validServerJournalTimestamp(report.FinalizingEventOccurredAt) ||
		report.SessionUUID != "" && !auditSessionUUIDPattern.MatchString(report.SessionUUID) {
		return auditEvidenceReport{}, authenticationError("typed audit evidence schema")
	}
	if err := validateAuditFinalizingOwnedRoots(intent.RunID, event.Attempt, executionPolicyEntry{
		PromptRoot: filepath.Dir(filepath.Dir(event.PromptPath)), OutputRoot: filepath.Dir(filepath.Dir(event.OutputPath)),
		SessionRoot: filepath.Dir(event.SessionPath), TemporaryRoot: filepath.Dir(event.TemporaryPath),
		WorkRoot: filepath.Dir(filepath.Dir(event.WorkPath)),
	}, report.OwnedRoots); err != nil {
		return auditEvidenceReport{}, err
	}
	started, _ := time.Parse(time.RFC3339Nano, report.ProcessStartedAt)
	finished, _ := time.Parse(time.RFC3339Nano, report.ProcessFinishedAt)
	finalizingAt, _ := time.Parse(time.RFC3339Nano, report.FinalizingEventOccurredAt)
	eventAt, _ := time.Parse(time.RFC3339Nano, event.OccurredAt)
	if finished.Before(started) || finalizingAt.Before(finished) || eventAt.Before(finalizingAt) {
		return auditEvidenceReport{}, authenticationError("typed audit evidence timestamps")
	}
	return report, nil
}

func validAuditEvidenceTests(tests []auditEvidenceTest) bool {
	seenIDs := make(map[string]struct{}, len(tests))
	seenCommands := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		if !executionTaskIDPattern.MatchString(test.ID) || !protocolHashPattern.MatchString(test.CommandSHA256) || test.ExitCode != 0 ||
			!protocolHashPattern.MatchString(test.SandboxProfileSHA256) || !protocolHashPattern.MatchString(test.StdoutSHA256) ||
			!protocolHashPattern.MatchString(test.StderrSHA256) || test.StdoutSize < 0 || test.StderrSize < 0 {
			return false
		}
		if _, duplicate := seenIDs[test.ID]; duplicate {
			return false
		}
		if _, duplicate := seenCommands[test.CommandSHA256]; duplicate {
			return false
		}
		seenIDs[test.ID] = struct{}{}
		seenCommands[test.CommandSHA256] = struct{}{}
	}
	return true
}

func validateAuditEvidencePolicy(report auditEvidenceReport, entry executionPolicyEntry) error {
	if report.LaunchSpecHash != entry.LaunchSpecHash || report.TaskID != entry.TaskID || report.PolicyHash != entry.PolicyHash ||
		report.RouteMappingHash != entry.RouteMappingHash || report.RepositoryIdentityHash != entry.RepositoryIdentityHash ||
		report.SourceArchiveSHA256 != entry.SourceArchiveSHA256 || report.GitCommit != entry.GitCommit || report.GitTree != entry.GitTree ||
		report.WrapperSHA256 != entry.Wrapper.SHA256 || report.OMPVersion != entry.OMPVersion ||
		report.OMPNativeAddonSHA256 != entry.OMPNativeAddon.SHA256 || report.AttemptCap != entry.AttemptCap || len(report.TestsRun) != len(entry.AllowedTests) {
		return authenticationError("typed audit evidence policy binding")
	}
	if err := validateAuditFinalizingOwnedRoots(report.RunID, report.Attempt, entry, report.OwnedRoots); err != nil {
		return err
	}
	resume := auditResume{SessionUUID: report.ResumeSessionUUID, SynthesizeOnly: report.SynthesizeOnly}
	descriptorHash, err := auditCommandDescriptorHash(entry, report.PromptSHA256, report.SessionRunID, resume)
	attemptRunID := report.RunID + "_attempt_" + strconv.Itoa(report.Attempt)
	if err != nil || report.SessionRunID != report.RunID || report.PromptSHA256 != auditPromptSHA256(report.SynthesizeOnly) ||
		descriptorHash != report.CommandDescriptorHash || report.WorkPath != filepath.Join(entry.WorkRoot, report.RunID, "source") ||
		report.OutputPath != filepath.Join(entry.OutputRoot, attemptRunID, "audit-output.json") ||
		report.SessionPath != filepath.Join(entry.SessionRoot, report.RunID) ||
		report.PromptPath != filepath.Join(entry.PromptRoot, attemptRunID, "audit-prompt.txt") ||
		report.TemporaryPath != filepath.Join(entry.TemporaryRoot, attemptRunID) {
		return authenticationError("typed audit evidence command and path authority")
	}
	for index, test := range report.TestsRun {
		if test.ID != entry.AllowedTests[index].ID || test.CommandSHA256 != entry.AllowedTests[index].CommandSHA256 {
			return authenticationError("typed audit evidence test binding")
		}
	}
	return nil
}

type auditOwnedRootSpec struct {
	role        string
	path        string
	parentPath  string
	cleanupRoot bool
}

func expectedAuditFinalizingOwnedRootSpecs(runID string, attempt int, entry executionPolicyEntry) ([]auditOwnedRootSpec, error) {
	if !executionTaskIDPattern.MatchString(runID) || attempt < 1 || attempt > 10 {
		return nil, authenticationError("audit owned root inventory attempt")
	}
	specs := make([]auditOwnedRootSpec, 0, attempt*7+3)
	for number := 1; number <= attempt; number++ {
		attemptRunID := runID + "_attempt_" + strconv.Itoa(number)
		prompt := filepath.Join(entry.PromptRoot, attemptRunID)
		output := filepath.Join(entry.OutputRoot, attemptRunID)
		temporary := filepath.Join(entry.TemporaryRoot, attemptRunID)
		home := filepath.Join(temporary, "home")
		homeState := filepath.Join(home, ".omp")
		specs = append(specs,
			auditOwnedRootSpec{role: "prompt", path: prompt, parentPath: entry.PromptRoot, cleanupRoot: true},
			auditOwnedRootSpec{role: "output", path: output, parentPath: entry.OutputRoot, cleanupRoot: true},
			auditOwnedRootSpec{role: "temporary", path: temporary, parentPath: entry.TemporaryRoot, cleanupRoot: true},
			auditOwnedRootSpec{role: "direct_omp_agent", path: filepath.Join(temporary, "omp-agent"), parentPath: temporary},
			auditOwnedRootSpec{role: "direct_omp_home", path: home, parentPath: temporary},
			auditOwnedRootSpec{role: "direct_omp_home_state", path: homeState, parentPath: home},
			auditOwnedRootSpec{role: "direct_omp_home_run", path: filepath.Join(homeState, "run"), parentPath: homeState},
		)
	}
	work := filepath.Join(entry.WorkRoot, runID)
	specs = append(specs,
		auditOwnedRootSpec{role: "session", path: filepath.Join(entry.SessionRoot, runID), parentPath: entry.SessionRoot, cleanupRoot: true},
		auditOwnedRootSpec{role: "work", path: work, parentPath: entry.WorkRoot, cleanupRoot: true},
		auditOwnedRootSpec{role: "source_snapshot", path: filepath.Join(work, "source"), parentPath: work},
	)
	for _, spec := range specs {
		if !validAuditPrivatePath(spec.path) || !validAuditPrivatePath(spec.parentPath) || filepath.Dir(spec.path) != spec.parentPath {
			return nil, authenticationError("audit owned root inventory path")
		}
	}
	return specs, nil
}

func validateAuditOwnedRootSequence(identities []auditOwnedRootIdentity) error {
	if len(identities) == 0 || len(identities) > 10*7+3 {
		return authenticationError("audit owned root inventory size")
	}
	all := make(map[string]auditOwnedRootIdentity, len(identities))
	for _, identity := range identities {
		if !validAuditOwnedRootIdentity(identity) {
			return authenticationError("audit owned root inventory identity")
		}
		if _, duplicate := all[identity.Path]; duplicate {
			return authenticationError("duplicate audit owned root inventory path")
		}
		all[identity.Path] = identity
	}
	seen := make(map[string]auditOwnedRootIdentity, len(identities))
	for _, identity := range identities {
		if parent, nested := all[identity.ParentPath]; nested {
			orderedParent, parentSeen := seen[identity.ParentPath]
			if !parentSeen || orderedParent != parent || identity.ParentDevice != parent.Device || identity.ParentInode != parent.Inode ||
				identity.ParentOwnerUID != parent.OwnerUID || identity.ParentOwnerGID != parent.OwnerGID || identity.ParentMode != parent.Mode {
				return authenticationError("audit owned root parent identity and order")
			}
		}
		seen[identity.Path] = identity
	}
	return nil
}

func validateAuditFinalizingOwnedRoots(runID string, attempt int, entry executionPolicyEntry, identities []auditOwnedRootIdentity) error {
	specs, err := expectedAuditFinalizingOwnedRootSpecs(runID, attempt, entry)
	if err != nil || len(identities) != len(specs) {
		return authenticationError("exact audit owned root inventory")
	}
	if err := validateAuditOwnedRootSequence(identities); err != nil {
		return err
	}
	for index, spec := range specs {
		identity := identities[index]
		if identity.Role != spec.role || identity.Path != spec.path || identity.ParentPath != spec.parentPath || identity.CleanupRoot != spec.cleanupRoot {
			return authenticationError("audit owned root role path and cleanup binding")
		}
	}
	return nil
}

func collectAuditFinalizingOwnedRoots(
	intent auditExecutionIntent,
	event auditExecutionEvent,
	entry executionPolicyEntry,
	snapshot auditSnapshot,
	invocationRoots []auditOwnedRootIdentity,
) ([]auditOwnedRootIdentity, error) {
	specs, err := expectedAuditFinalizingOwnedRootSpecs(intent.RunID, event.Attempt, entry)
	if err != nil || len(snapshot.OwnedRoots) != 2 {
		return nil, authenticationError("live audit owned root inventory")
	}
	invocationByPath := make(map[string]auditOwnedRootIdentity, len(invocationRoots))
	for _, identity := range invocationRoots {
		if prior, duplicate := invocationByPath[identity.Path]; duplicate && prior != identity {
			return nil, authenticationError("conflicting live audit owned root identity")
		}
		invocationByPath[identity.Path] = identity
	}
	snapshotByPath := make(map[string]auditOwnedRootIdentity, len(snapshot.OwnedRoots))
	for _, identity := range snapshot.OwnedRoots {
		if _, duplicate := snapshotByPath[identity.Path]; duplicate {
			return nil, authenticationError("duplicate snapshot owned root identity")
		}
		snapshotByPath[identity.Path] = identity
	}
	ordered := make([]auditOwnedRootIdentity, 0, len(specs))
	for _, spec := range specs {
		source := invocationByPath
		if spec.role == "work" || spec.role == "source_snapshot" {
			source = snapshotByPath
		}
		identity, ok := source[spec.path]
		if !ok {
			return nil, authenticationError("missing live audit owned root identity")
		}
		ordered = append(ordered, identity)
	}
	if len(invocationByPath)+len(snapshotByPath) != len(ordered) {
		return nil, authenticationError("unexpected live audit owned root identity")
	}
	if err := validateAuditFinalizingOwnedRoots(intent.RunID, event.Attempt, entry, ordered); err != nil {
		return nil, err
	}
	return ordered, nil
}

func validateAuditCallbackEvidence(authority *auditNamespaceAuthority, intent auditExecutionIntent, event auditExecutionEvent, entry executionPolicyEntry) error {
	report, err := decodeAuditEvidenceReport(intent, event)
	if err != nil {
		return err
	}
	if err := validateAuditEvidencePolicy(report, entry); err != nil {
		return err
	}
	return verifyAuditFinalizingRootsAbsent(authority, intent, event, entry)
}
func collectAuditEvidence(
	ctx context.Context,
	policy *executionPolicy,
	entry executionPolicyEntry,
	intent auditExecutionIntent,
	snapshot auditSnapshot,
	invocation auditInvocation,
	result auditInvocationResult,
	completed auditExecutionEvent,
	testHooks auditSupervisorTestHooks,
) (collectedAuditEvidence, error) {
	if ctx == nil || policy == nil || result.ExitCode != 0 || result.TimedOut || result.Cancelled ||
		completed.State != auditStateFinalizing || completed.IntentHash != intent.IntentHash || completed.Attempt < 1 ||
		completed.CommandDescriptorHash != invocation.CommandDescriptorHash || result.CommandDescriptorHash != invocation.CommandDescriptorHash ||
		!protocolHashPattern.MatchString(result.SandboxProfileHash) || snapshot.SourceRoot != invocation.WorkDir {
		return collectedAuditEvidence{}, authenticationError("successful audit evidence boundary")
	}
	if result.boundInvocation.NativeAddonPath != "" {
		liveRoots := result.boundInvocation.OwnedRoots
		if invocation.NativeAddonPath != "" {
			liveRoots = invocation.OwnedRoots
		}
		invocation = result.boundInvocation
		invocation.OwnedRoots = liveRoots
	}
	if err := policy.ValidateEffectBoundary(entry); err != nil {
		return collectedAuditEvidence{}, err
	}
	if err := validateAuditInvocationTransport(entry, invocation); err != nil {
		return collectedAuditEvidence{}, err
	}
	output, outputSize, err := readAuditRegularFile(invocation.OutputPath, maxAuditOutputBytes)
	if err != nil || outputSize == 0 || !protocolHashPattern.MatchString(result.ObservedOutputSHA256) ||
		outputSize != result.ObservedOutputSize || hashJournalBytes(output) != result.ObservedOutputSHA256 {
		zeroBytes(output)
		return collectedAuditEvidence{}, authenticationError("bounded audit output evidence")
	}
	defer zeroBytes(output)
	modelReport, err := decodeAuditModelReport(output, entry, invocation)
	if err != nil {
		return collectedAuditEvidence{}, err
	}
	tests, err := runSupervisorAuditTests(ctx, policy, entry, snapshot, invocation, testHooks)
	if err != nil {
		return collectedAuditEvidence{}, err
	}
	if err := validateAuditInvocationTransport(entry, invocation); err != nil {
		return collectedAuditEvidence{}, err
	}
	ownedRoots, err := collectAuditFinalizingOwnedRoots(intent, completed, entry, snapshot, result.boundInvocation.OwnedRoots)
	if err != nil {
		return collectedAuditEvidence{}, err
	}
	report := auditEvidenceReport{
		SchemaVersion: auditEvidenceSchemaVersion, IntentHash: intent.IntentHash, EnvelopeHash: intent.EnvelopeHash,
		LaunchSpecHash: intent.LaunchSpecHash, HandoffID: intent.HandoffID, ReceiptHash: intent.ReceiptHash, TaskID: intent.TaskID,
		RunID: intent.RunID, Attempt: completed.Attempt, AttemptCap: intent.AttemptCap, PolicyHash: intent.PolicyHash,
		RouteMappingHash: intent.RouteMappingHash, RepositoryIdentityHash: intent.RepositoryIdentityHash,
		SourceArchiveSHA256: intent.SourceArchiveSHA256, GitCommit: intent.GitCommit, GitTree: intent.GitTree, WrapperSHA256: intent.WrapperSHA256,
		OMPVersion: entry.OMPVersion, OMPNativeAddonSHA256: entry.OMPNativeAddon.SHA256,
		FinalizingEventID: completed.EventID, FinalizingEventSequence: completed.Sequence,
		FinalizingEventOccurredAt: completed.OccurredAt, CommandDescriptorHash: invocation.CommandDescriptorHash, PromptSHA256: invocation.PromptSHA256,
		SessionRunID: invocation.SessionRunID, ResumeSessionUUID: invocation.Resume.SessionUUID, SynthesizeOnly: invocation.Resume.SynthesizeOnly,
		SandboxProfileSHA256: result.SandboxProfileHash, PID: result.PID, PGID: result.PGID,
		ProcessStartIdentity: result.ProcessStartIdentity, ProcessStartedAt: result.StartedAt, ProcessFinishedAt: result.FinishedAt,
		ExitCode: result.ExitCode, StdoutSHA256: result.StdoutSHA256, StderrSHA256: result.StderrSHA256,
		OutputSHA256: result.ObservedOutputSHA256, OutputSize: outputSize, ModelReportSHA256: hashJournalBytes(output),
		ModelReport: modelReport, TestsRun: tests, OwnedRoots: ownedRoots, SessionUUID: invocation.Resume.SessionUUID,
		WorkPath: invocation.WorkDir, OutputPath: invocation.OutputPath, SessionPath: invocation.SessionDir,
		PromptPath: invocation.PromptPath, TemporaryPath: invocation.TemporaryDir,
	}
	encoded, err := marshalCanonical(report)
	if err != nil || len(encoded) > maxAuditEvidenceBytes {
		return collectedAuditEvidence{}, ErrLimit
	}
	return collectedAuditEvidence{
		Report: report, EvidenceJSON: string(encoded), EvidenceHash: hashJournalBytes(encoded),
		OutputSHA256: report.OutputSHA256, OutputSize: report.OutputSize,
	}, nil
}

func readAuditRegularFile(path string, limit int64) ([]byte, int64, error) {
	information, err := os.Lstat(path)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() || information.Size() < 0 || information.Size() > limit {
		return nil, 0, authenticationError("audit evidence file type or size")
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != uint32(os.Getuid()) {
		return nil, 0, authenticationError("audit evidence file owner")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, 0, authenticationError("open audit evidence file")
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, 0, authenticationError("open audit evidence descriptor")
	}
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Uid != uint32(os.Getuid()) ||
		uint64(opened.Dev) != uint64(status.Dev) || opened.Ino != status.Ino || opened.Size != information.Size() {
		return nil, 0, authenticationError("audit evidence file replacement")
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(contents)) != information.Size() || int64(len(contents)) > limit {
		zeroBytes(contents)
		return nil, 0, authenticationError("read bounded audit evidence file")
	}
	return contents, information.Size(), nil
}

func auditBytesLeakSecretAuthority(contents []byte) bool {
	lower := bytes.ToLower(contents)
	for _, marker := range [][]byte{[]byte("begin private key"), []byte("authorization: bearer"), []byte("api_key="), []byte("api-key=")} {
		if bytes.Contains(lower, marker) {
			return true
		}
	}
	for _, name := range auditCredentialEnvironmentNames {
		if value, exists := os.LookupEnv(name); exists && value != "" && bytes.Contains(contents, []byte(value)) {
			return true
		}
	}
	return false
}

type auditAuthorityKind string

const (
	auditAuthorityKindSecret      auditAuthorityKind = "secret"
	auditAuthorityKindRepository  auditAuthorityKind = "repository"
	auditAuthorityKindWrapper     auditAuthorityKind = "wrapper"
	auditAuthorityKindPromptRoot  auditAuthorityKind = "prompt_root"
	auditAuthorityKindOutputRoot  auditAuthorityKind = "output_root"
	auditAuthorityKindSessionRoot auditAuthorityKind = "session_root"
	auditAuthorityKindWorkRoot    auditAuthorityKind = "work_root"
	auditAuthorityKindPromptPath  auditAuthorityKind = "prompt_path"
	auditAuthorityKindOutputPath  auditAuthorityKind = "output_path"
	auditAuthorityKindSessionPath auditAuthorityKind = "session_path"
	auditAuthorityKindWorkPath    auditAuthorityKind = "work_path"
)

func validAuditAuthorityKind(kind auditAuthorityKind) bool {
	switch kind {
	case auditAuthorityKindSecret, auditAuthorityKindRepository, auditAuthorityKindWrapper,
		auditAuthorityKindPromptRoot, auditAuthorityKindOutputRoot, auditAuthorityKindSessionRoot,
		auditAuthorityKindWorkRoot, auditAuthorityKindPromptPath, auditAuthorityKindOutputPath,
		auditAuthorityKindSessionPath, auditAuthorityKindWorkPath:
		return true
	default:
		return false
	}
}

func classifyAuditBytesLeakAuthority(contents []byte, entry executionPolicyEntry, invocation auditInvocation) (auditAuthorityKind, bool) {
	if auditBytesLeakSecretAuthority(contents) {
		return auditAuthorityKindSecret, true
	}
	for _, candidate := range []struct {
		kind  auditAuthorityKind
		value string
	}{
		{auditAuthorityKindRepository, entry.Repository.Path},
		{auditAuthorityKindWrapper, entry.Wrapper.Path},
		{auditAuthorityKindPromptRoot, entry.PromptRoot},
		{auditAuthorityKindOutputRoot, entry.OutputRoot},
		{auditAuthorityKindSessionRoot, entry.SessionRoot},
		{auditAuthorityKindWorkRoot, entry.WorkRoot},
		{auditAuthorityKindPromptPath, invocation.PromptPath},
		{auditAuthorityKindOutputPath, invocation.OutputPath},
		{auditAuthorityKindSessionPath, invocation.SessionDir},
		{auditAuthorityKindWorkPath, invocation.WorkDir},
	} {
		if candidate.value != "" && bytes.Contains(contents, []byte(candidate.value)) {
			return candidate.kind, true
		}
	}
	return "", false
}

func auditBytesLeakAuthority(contents []byte, entry executionPolicyEntry, invocation auditInvocation) bool {
	_, leaked := classifyAuditBytesLeakAuthority(contents, entry, invocation)
	return leaked
}

const (
	auditTimeoutObservationSchemaVersion     = "ananke.local-trusted-supervisor-timeout-observation.v1"
	auditTimeoutSourceOMPInternalDeadline    = "omp_internal_deadline"
	auditTimeoutSourceSupervisorHardDeadline = "supervisor_hard_deadline"
	maxAuditTimeoutSessionHeaderBytes        = 1024 * 1024
)

type auditTimeoutEvidence struct {
	SchemaVersion                 string `json:"schema_version"`
	ObservationHash               string `json:"observation_hash"`
	TimeoutSource                 string `json:"timeout_source"`
	DeadlineObservation           string `json:"deadline_observation"`
	InternalDeadlineSeconds       int    `json:"internal_deadline_seconds"`
	SupervisorHardDeadlineSeconds int    `json:"supervisor_hard_deadline_seconds"`
	RunID                         string `json:"run_id"`
	SessionRunID                  string `json:"session_run_id"`
	CommandDescriptorHash         string `json:"command_descriptor_hash"`
	PromptSHA256                  string `json:"prompt_sha256"`
	SandboxProfileSHA256          string `json:"sandbox_profile_sha256"`
	ResumeSessionUUID             string `json:"resume_session_uuid"`
	SessionRoot                   string `json:"session_root"`
	SessionUUID                   string `json:"session_uuid"`
	SessionPath                   string `json:"session_path"`
	SynthesizeOnly                bool   `json:"synthesize_only"`
	PID                           int    `json:"pid"`
	PGID                          int    `json:"pgid"`
	ProcessStartIdentity          string `json:"process_start_identity"`
	ProcessStartedAt              string `json:"process_started_at"`
	ProcessFinishedAt             string `json:"process_finished_at"`
	ExitCode                      int    `json:"exit_code"`
	StdoutSHA256                  string `json:"stdout_sha256"`
	StderrSHA256                  string `json:"stderr_sha256"`
	ProcessGroupExitConfirmed     bool   `json:"process_group_exit_confirmed"`
}

type auditSessionPathSnapshot map[string]struct{}

func snapshotAuditSessionPaths(invocation auditInvocation) (auditSessionPathSnapshot, error) {
	if invocation.Resume.SessionUUID != "" {
		return auditSessionPathSnapshot{}, nil
	}
	rootIdentity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "session")
	if !ok || rootIdentity.Path != invocation.SessionDir {
		return nil, authenticationError("timeout session root identity")
	}
	paths := make(auditSessionPathSnapshot)
	files := 0
	err := filepath.Walk(invocation.SessionDir, func(candidate string, information os.FileInfo, walkErr error) error {
		if walkErr != nil || information == nil || information.Mode()&os.ModeSymlink != 0 {
			return authenticationError("bounded timeout session snapshot")
		}
		if information.IsDir() {
			return nil
		}
		files++
		if files > maxAuditTreeScanFiles || !information.Mode().IsRegular() {
			return ErrLimit
		}
		if filepath.Ext(candidate) == ".jsonl" {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return authenticationError("physical timeout session path")
			}
			paths[resolved] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func captureContainsDeadlineExceeded(streams ...[]byte) bool {
	for _, stream := range streams {
		for _, line := range bytes.Split(stream, []byte{'\n'}) {
			if bytes.Equal(line, []byte("Deadline exceeded")) {
				return true
			}
		}
	}
	return false
}

func buildAuditTimeoutEvidence(entry executionPolicyEntry, invocation auditInvocation, result auditInvocationResult, source string, before auditSessionPathSnapshot) (auditTimeoutEvidence, error) {
	if !result.ProcessGroupGone || result.Cancelled || result.PID <= 0 || result.PGID != result.PID || result.ProcessStartIdentity == "" ||
		!validServerJournalTimestamp(result.StartedAt) || !validServerJournalTimestamp(result.FinishedAt) ||
		source == auditTimeoutSourceSupervisorHardDeadline && !result.TimedOut || source == auditTimeoutSourceOMPInternalDeadline && result.TimedOut {
		return auditTimeoutEvidence{}, authenticationError("supervisor timeout process authority")
	}
	sessionUUID, sessionPath, err := resolveAuditTimeoutSession(invocation, before)
	if err != nil {
		return auditTimeoutEvidence{}, err
	}
	observation := auditTimeoutEvidence{
		SchemaVersion: auditTimeoutObservationSchemaVersion, TimeoutSource: source,
		DeadlineObservation: "standalone_deadline_exceeded_line", InternalDeadlineSeconds: entry.InternalDeadlineSeconds,
		SupervisorHardDeadlineSeconds: entry.InternalDeadlineSeconds + entry.WrapperGraceSeconds,
		RunID:                         invocation.RunID, SessionRunID: invocation.SessionRunID, CommandDescriptorHash: invocation.CommandDescriptorHash,
		PromptSHA256: invocation.PromptSHA256, SandboxProfileSHA256: invocation.SandboxProfileHash,
		ResumeSessionUUID: invocation.Resume.SessionUUID, SessionUUID: sessionUUID, SessionPath: sessionPath, SessionRoot: invocation.SessionDir,
		SynthesizeOnly: true, PID: result.PID, PGID: result.PGID, ProcessStartIdentity: result.ProcessStartIdentity,
		ProcessStartedAt: result.StartedAt, ProcessFinishedAt: result.FinishedAt, ExitCode: result.ExitCode,
		StdoutSHA256: result.StdoutSHA256, StderrSHA256: result.StderrSHA256, ProcessGroupExitConfirmed: true,
	}
	if source == auditTimeoutSourceSupervisorHardDeadline {
		observation.DeadlineObservation = "supervisor_hard_deadline_timer_fired"
	}
	return sealAuditTimeoutEvidence(observation)
}

func sealAuditTimeoutEvidence(observation auditTimeoutEvidence) (auditTimeoutEvidence, error) {
	observation.ObservationHash = ""
	if observation.SchemaVersion != auditTimeoutObservationSchemaVersion ||
		observation.TimeoutSource != auditTimeoutSourceOMPInternalDeadline && observation.TimeoutSource != auditTimeoutSourceSupervisorHardDeadline ||
		observation.InternalDeadlineSeconds < 1 || observation.SupervisorHardDeadlineSeconds <= observation.InternalDeadlineSeconds ||
		!executionTaskIDPattern.MatchString(observation.RunID) || !executionTaskIDPattern.MatchString(observation.SessionRunID) ||
		!protocolHashPattern.MatchString(observation.CommandDescriptorHash) || !protocolHashPattern.MatchString(observation.PromptSHA256) ||
		!protocolHashPattern.MatchString(observation.SandboxProfileSHA256) || !auditSessionUUIDPattern.MatchString(observation.SessionUUID) ||
		observation.SessionRoot == "" || !filepath.IsAbs(observation.SessionRoot) || filepath.Clean(observation.SessionRoot) != observation.SessionRoot ||
		observation.ResumeSessionUUID != "" && observation.ResumeSessionUUID != observation.SessionUUID || !observation.SynthesizeOnly ||
		observation.PID <= 0 || observation.PGID != observation.PID || observation.ProcessStartIdentity == "" ||
		!validServerJournalTimestamp(observation.ProcessStartedAt) || !validServerJournalTimestamp(observation.ProcessFinishedAt) ||
		!protocolHashPattern.MatchString(observation.StdoutSHA256) || !protocolHashPattern.MatchString(observation.StderrSHA256) ||
		!observation.ProcessGroupExitConfirmed ||
		observation.TimeoutSource == auditTimeoutSourceOMPInternalDeadline && observation.DeadlineObservation != "standalone_deadline_exceeded_line" ||
		observation.TimeoutSource == auditTimeoutSourceSupervisorHardDeadline && observation.DeadlineObservation != "supervisor_hard_deadline_timer_fired" {
		return auditTimeoutEvidence{}, authenticationError("closed supervisor timeout observation")
	}
	if observation.ResumeSessionUUID != "" && observation.SessionPath != "" ||
		observation.ResumeSessionUUID == "" && !validFreshAuditTimeoutSessionPathStructure(observation.SessionPath, observation.SessionRoot) {
		return auditTimeoutEvidence{}, authenticationError("supervisor timeout session observation")
	}
	hash, err := canonicalHash(observation)
	if err != nil {
		return auditTimeoutEvidence{}, err
	}
	observation.ObservationHash = hash
	return observation, nil
}

func validAuditTimeoutEvidence(observation auditTimeoutEvidence) bool {
	sealed, err := sealAuditTimeoutEvidence(observation)
	return err == nil && sealed == observation && protocolHashPattern.MatchString(observation.ObservationHash)
}

func resolveAuditTimeoutSession(invocation auditInvocation, before auditSessionPathSnapshot) (string, string, error) {
	if invocation.Resume.SessionUUID != "" {
		if !auditSessionUUIDPattern.MatchString(invocation.Resume.SessionUUID) {
			return "", "", authenticationError("exact timeout resume UUID")
		}
		return invocation.Resume.SessionUUID, "", nil
	}
	physicalWorkDir, err := filepath.EvalSymlinks(invocation.WorkDir)
	if err != nil {
		return "", "", authenticationError("physical timeout work directory")
	}
	prompt, err := os.ReadFile(invocation.PromptPath)
	if err != nil || hashJournalBytes(prompt) != invocation.PromptSHA256 {
		return "", "", authenticationError("exact timeout prompt")
	}
	defer zeroBytes(prompt)
	rootIdentity, ok := auditOwnedRootIdentityForRole(invocation.OwnedRoots, "session")
	if !ok || rootIdentity.Path != invocation.SessionDir {
		return "", "", authenticationError("timeout session identity")
	}
	type match struct{ uuid, path string }
	matches := make([]match, 0, 1)
	files := 0
	err = filepath.Walk(invocation.SessionDir, func(candidate string, information os.FileInfo, walkErr error) error {
		if walkErr != nil || information == nil || information.Mode()&os.ModeSymlink != 0 {
			return authenticationError("bounded timeout session discovery")
		}
		if information.IsDir() {
			return nil
		}
		files++
		if files > maxAuditTreeScanFiles || !information.Mode().IsRegular() {
			return ErrLimit
		}
		if filepath.Ext(candidate) != ".jsonl" {
			return nil
		}
		resolved, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil {
			return authenticationError("physical timeout session candidate")
		}
		if _, existed := before[resolved]; existed {
			return nil
		}
		status, ok := information.Sys().(*syscall.Stat_t)
		if !ok || status.Uid != rootIdentity.OwnerUID {
			return authenticationError("timeout session candidate owner")
		}
		uuid, matched := parseAuditSessionHeader(candidate, physicalWorkDir, string(prompt))
		if matched {
			matches = append(matches, match{uuid: uuid, path: candidate})
		}
		return nil
	})
	if err != nil || len(matches) != 1 {
		return "", "", authenticationError("unique exact timeout session")
	}
	if !validFreshAuditTimeoutSessionPath(matches[0].path, invocation.SessionDir) {
		return "", "", authenticationError("exact timeout session path")
	}
	return matches[0].uuid, matches[0].path, nil
}

func parseAuditSessionHeader(path, expectedCWD, expectedPrompt string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxAuditTimeoutSessionHeaderBytes)
	sessionUUID, sessionCWD := "", ""
	var userPrompt *string
	for line := 0; line < 50 && scanner.Scan(); line++ {
		var record struct {
			Type    string          `json:"type"`
			ID      string          `json:"id"`
			CWD     string          `json:"cwd"`
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			return "", false
		}
		if record.Type == "session" {
			sessionUUID, sessionCWD = record.ID, record.CWD
		}
		if record.Type == "message" {
			var message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}
			if json.Unmarshal(record.Message, &message) == nil && message.Role == "user" {
				var text string
				if json.Unmarshal(message.Content, &text) == nil {
					userPrompt = &text
				} else {
					var parts []struct{ Type, Text string }
					if json.Unmarshal(message.Content, &parts) == nil {
						var combined strings.Builder
						for _, part := range parts {
							if part.Type == "text" {
								combined.WriteString(part.Text)
							}
						}
						text = combined.String()
						userPrompt = &text
					}
				}
			}
		}
		if sessionUUID != "" && userPrompt != nil {
			break
		}
	}
	return sessionUUID, scanner.Err() == nil && auditSessionUUIDPattern.MatchString(sessionUUID) && sessionCWD == expectedCWD && userPrompt != nil && *userPrompt == expectedPrompt
}

func auditFreshSessionJSONLAllowsPaths(contents []byte, allowedPaths map[string]struct{}, protectedPaths []string) bool {
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), maxAuditTreeScanBytes)
	records := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		decoded, err := decodeJSONValue(line)
		if err != nil {
			return false
		}
		if _, ok := decoded.(map[string]any); !ok {
			return false
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return false
			}
			value, ok := token.(string)
			if ok && !auditFreshSessionStringAllowsPaths(value, allowedPaths, protectedPaths) {
				return false
			}
		}
		records++
	}
	return records != 0 && scanner.Err() == nil
}

func auditFreshSessionStringAllowsPaths(value string, allowedPaths map[string]struct{}, protectedPaths []string) bool {
	for _, protected := range protectedPaths {
		if protected != "" && strings.Contains(value, protected) {
			return false
		}
	}
	if _, ok := allowedPaths[value]; ok {
		return true
	}
	return !containsAuditAbsolutePathReference(value)
}

func containsAuditAbsolutePathReference(value string) bool {
	for index := range len(value) {
		if value[index] == '/' && (index == 0 || auditPathReferenceBoundary(value[index-1])) {
			return true
		}
		if index+2 < len(value) && ((value[index] >= 'A' && value[index] <= 'Z') || (value[index] >= 'a' && value[index] <= 'z')) &&
			value[index+1] == ':' && (value[index+2] == '/' || value[index+2] == '\\') && (index == 0 || auditPathReferenceBoundary(value[index-1])) {
			return true
		}
	}
	return false
}

func auditPathReferenceBoundary(value byte) bool {
	return value < '0' || value > '9' && value < 'A' || value > 'Z' && value < 'a' || value > 'z'
}

func validFreshAuditTimeoutSessionPath(reported, sessionRoot string) bool {
	if !validFreshAuditTimeoutSessionPathStructure(reported, sessionRoot) {
		return false
	}
	physicalRoot, rootErr := filepath.EvalSymlinks(sessionRoot)
	physicalPath, pathErr := filepath.EvalSymlinks(reported)
	if rootErr != nil || pathErr != nil {
		return false
	}
	relative, err := filepath.Rel(physicalRoot, physicalPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	information, err := os.Lstat(reported)
	return err == nil && information.Mode()&os.ModeSymlink == 0 && information.Mode().IsRegular()
}

func validFreshAuditTimeoutSessionPathStructure(reported, sessionRoot string) bool {
	if reported == "" || sessionRoot == "" || !filepath.IsAbs(reported) || !filepath.IsAbs(sessionRoot) ||
		filepath.Clean(reported) != reported || filepath.Clean(sessionRoot) != sessionRoot {
		return false
	}
	relative, err := filepath.Rel(sessionRoot, reported)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func verifyAuditOutputUnchanged(path, expectedHash string, expectedSize int64) error {
	contents, size, err := readAuditRegularFile(path, maxAuditOutputBytes)
	if err != nil {
		return err
	}
	defer zeroBytes(contents)
	if size != expectedSize || hashJournalBytes(contents) != expectedHash {
		return authenticationError("audit output evidence tamper")
	}
	return nil
}
