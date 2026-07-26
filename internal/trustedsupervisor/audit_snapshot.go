package trustedsupervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	maxGitMetadataBytes       = 1024 * 1024
	maxGitArchiveBytes        = 128 * 1024 * 1024
	maxGitArchiveEntries      = 100000
	maxGitArchiveExpandedSize = 512 * 1024 * 1024
	maxGitArchivePathBytes    = 4096
	tarBlockBytes             = 512
)

type auditSnapshot struct {
	RunRoot       string
	SourceRoot    string
	ArchiveSHA256 string
	GitCommit     string
	GitTree       string
	EntryCount    int
	ExpandedBytes int64
	OwnedRoots    []auditOwnedRootIdentity
}

type auditSnapshotHooks struct {
	BeforeGitStart         func(operation string)
	AfterArchiveCapture    func()
	BeforeSnapshotMutation func(stage string)
}

func (hooks auditSnapshotHooks) beforeSnapshotMutation(stage string) {
	if hooks.BeforeSnapshotMutation != nil {
		hooks.BeforeSnapshotMutation(stage)
	}
}

type auditArchiveManifest struct {
	EntryCount    int
	FileCount     int
	ExpandedBytes int64
}

type auditArchiveEntry struct {
	name       string
	body       []byte
	directory  bool
	executable bool
}

func materializeAuditSnapshot(ctx context.Context, policy *executionPolicy, entry executionPolicyEntry, runID string, hooks auditSnapshotHooks) (auditSnapshot, error) {
	if ctx == nil || policy == nil || policy.namespaceAuthority == nil || !executionTaskIDPattern.MatchString(runID) {
		return auditSnapshot{}, ErrProtocol
	}
	if err := policy.ValidateEffectBoundary(entry); err != nil {
		return auditSnapshot{}, err
	}
	commitBytes, err := runPinnedGit(ctx, policy, entry, hooks, "commit", maxGitMetadataBytes,
		"rev-parse", "--verify", entry.GitCommit+"^{commit}")
	if err != nil || string(bytes.TrimSpace(commitBytes)) != entry.GitCommit {
		return auditSnapshot{}, authenticationError("pinned Git commit identity")
	}
	commitObject, err := runPinnedGit(ctx, policy, entry, hooks, "commit_object", maxGitMetadataBytes,
		"cat-file", "commit", entry.GitCommit)
	if err != nil || hashJournalBytes(commitObject) != entry.GitCommitObjectSHA256 {
		return auditSnapshot{}, authenticationError("pinned Git commit object")
	}
	treeBytes, err := runPinnedGit(ctx, policy, entry, hooks, "tree", maxGitMetadataBytes,
		"rev-parse", "--verify", entry.GitCommit+"^{tree}")
	if err != nil || string(bytes.TrimSpace(treeBytes)) != entry.GitTree {
		return auditSnapshot{}, authenticationError("pinned Git tree identity")
	}
	archive, err := runPinnedGit(ctx, policy, entry, hooks, "archive", maxGitArchiveBytes,
		"archive", "--format=tar", "--mtime=1970-01-01T00:00:00Z", entry.GitTree)
	if err != nil {
		return auditSnapshot{}, err
	}
	defer zeroBytes(archive)
	if hashJournalBytes(archive) != entry.SourceArchiveSHA256 {
		return auditSnapshot{}, authenticationError("canonical Git archive hash")
	}
	if hooks.AfterArchiveCapture != nil {
		hooks.AfterArchiveCapture()
	}

	lease, err := policy.namespaceAuthority.Duplicate(entry.WorkRoot)
	if err != nil {
		return auditSnapshot{}, err
	}
	defer lease.Close()
	stagingName := "." + runID + ".staging"
	runName := runID
	runRoot := filepath.Join(entry.WorkRoot, runName)
	if !validAuditNamespaceComponent(stagingName) || filepath.Dir(runRoot) != entry.WorkRoot {
		return auditSnapshot{}, authenticationError("audit snapshot run root")
	}
	hooks.beforeSnapshotMutation("staging_create")
	if err := lease.RequireAbsent(stagingName); err != nil {
		return auditSnapshot{}, authenticationError("audit snapshot staging collision")
	}
	if err := lease.RequireAbsent(runName); err != nil {
		return auditSnapshot{}, authenticationError("audit snapshot run collision")
	}
	if err := lease.Mkdir(stagingName, 0o700); err != nil {
		return auditSnapshot{}, fmt.Errorf("create private audit staging root: %w", err)
	}
	stagingDescriptor, err := lease.Open(stagingName, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		_ = lease.RemoveTree(stagingName)
		return auditSnapshot{}, authenticationError("open private audit staging root")
	}
	cleanupName := stagingName
	cleanup := true
	defer func() {
		_ = unix.Close(stagingDescriptor)
		if cleanup {
			_ = lease.RemoveTree(cleanupName)
		}
	}()

	hooks.beforeSnapshotMutation("source_create")
	if err := unix.Mkdirat(stagingDescriptor, "source", 0o700); err != nil {
		return auditSnapshot{}, fmt.Errorf("create private audit source root: %w", err)
	}
	sourceDescriptor, err := unix.Openat(stagingDescriptor, "source", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return auditSnapshot{}, authenticationError("open private audit source root")
	}
	defer unix.Close(sourceDescriptor)
	hooks.beforeSnapshotMutation("extract")
	manifest, err := validateAndExtractGitArchiveAt(archive, sourceDescriptor)
	if err != nil {
		return auditSnapshot{}, err
	}
	hooks.beforeSnapshotMutation("seal")
	if err := unix.Fchmod(stagingDescriptor, 0o555); err != nil {
		return auditSnapshot{}, fmt.Errorf("seal audit staging root: %w", err)
	}
	if err := unix.Fsync(stagingDescriptor); err != nil {
		return auditSnapshot{}, authenticationError("sync sealed audit staging root")
	}
	hooks.beforeSnapshotMutation("publish")
	if err := lease.RequireAbsent(runName); err != nil {
		return auditSnapshot{}, authenticationError("audit snapshot run collision")
	}
	if err := lease.Rename(stagingName, runName); err != nil {
		return auditSnapshot{}, fmt.Errorf("publish immutable audit snapshot: %w", err)
	}
	cleanupName = runName
	hooks.beforeSnapshotMutation("sync")
	if err := lease.Sync(); err != nil {
		return auditSnapshot{}, authenticationError("sync audit snapshot work root")
	}
	hooks.beforeSnapshotMutation("capture")
	ownedRoots, err := captureAuditSnapshotOwnedRoots(lease, runName, stagingDescriptor, sourceDescriptor)
	if err != nil {
		return auditSnapshot{}, err
	}
	cleanup = false
	return auditSnapshot{
		RunRoot: runRoot, SourceRoot: filepath.Join(runRoot, "source"), ArchiveSHA256: entry.SourceArchiveSHA256,
		GitCommit: entry.GitCommit, GitTree: entry.GitTree, EntryCount: manifest.EntryCount, ExpandedBytes: manifest.ExpandedBytes,
		OwnedRoots: ownedRoots,
	}, nil
}
func captureAuditSnapshotOwnedRoots(lease *auditNamespaceLease, runName string, runDescriptor, sourceDescriptor int) ([]auditOwnedRootIdentity, error) {
	if lease == nil || !validAuditNamespaceComponent(runName) || runDescriptor < 0 || sourceDescriptor < 0 {
		return nil, authenticationError("audit snapshot owned root authority")
	}
	work, err := lease.Capture(runName, "work", true)
	if err != nil {
		return nil, err
	}
	var openedWork unix.Stat_t
	if err := unix.Fstat(runDescriptor, &openedWork); err != nil || !auditOwnedRootStatMatches(openedWork, work) {
		return nil, authenticationError("audit snapshot work descriptor identity")
	}
	source, err := captureAuditOwnedRootAt(runDescriptor, work.Path, "source", "source_snapshot", false, namespaceDirectoryIdentityFromOwned(work))
	if err != nil {
		return nil, err
	}
	var openedSource unix.Stat_t
	if err := unix.Fstat(sourceDescriptor, &openedSource); err != nil || !auditOwnedRootStatMatches(openedSource, source) {
		return nil, authenticationError("audit snapshot source descriptor identity")
	}
	return []auditOwnedRootIdentity{work, source}, nil
}

func runPinnedGit(ctx context.Context, policy *executionPolicy, entry executionPolicyEntry, hooks auditSnapshotHooks, operation string, limit int, arguments ...string) ([]byte, error) {
	if hooks.BeforeGitStart != nil {
		hooks.BeforeGitStart(operation)
	}
	if err := policy.ValidateEffectBoundary(entry); err != nil {
		return nil, err
	}
	commandContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	argv := append([]string{"-C", entry.Repository.Path}, arguments...)
	command := exec.CommandContext(commandContext, entry.GitExecutable.Path, argv...)
	command.Env = []string{
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0",
		"HOME=/var/empty", "LANG=C", "LC_ALL=C", "TMPDIR=" + entry.TemporaryRoot, "TZ=UTC", "XDG_CONFIG_HOME=/var/empty",
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout := &boundedCommandBuffer{limit: limit}
	stderr := &boundedCommandBuffer{limit: 16 * 1024}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		stdout.zero()
		stderr.zero()
		if commandContext.Err() != nil {
			return nil, ErrDeadline
		}
		if errors.Is(stdout.err, ErrLimit) || errors.Is(stderr.err, ErrLimit) {
			return nil, ErrLimit
		}
		return nil, authenticationError("pinned Git command failed")
	}
	stderr.zero()
	if stdout.err != nil {
		stdout.zero()
		return nil, stdout.err
	}
	return stdout.take(), nil
}

type boundedCommandBuffer struct {
	buffer bytes.Buffer
	limit  int
	err    error
}

func (buffer *boundedCommandBuffer) Write(value []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	if len(value) > buffer.limit-buffer.buffer.Len() {
		buffer.err = ErrLimit
		return 0, ErrLimit
	}
	return buffer.buffer.Write(value)
}

func (buffer *boundedCommandBuffer) take() []byte {
	value := append([]byte(nil), buffer.buffer.Bytes()...)
	buffer.zero()
	return value
}

func (buffer *boundedCommandBuffer) zero() {
	contents := buffer.buffer.Bytes()
	zeroBytes(contents)
	buffer.buffer.Reset()
}

func validateAndExtractGitArchiveAt(archive []byte, destinationDescriptor int) (auditArchiveManifest, error) {
	if len(archive) < 2*tarBlockBytes || len(archive)%tarBlockBytes != 0 || len(archive) > maxGitArchiveBytes || destinationDescriptor < 0 {
		return auditArchiveManifest{}, authenticationError("canonical Git archive framing")
	}
	var destination unix.Stat_t
	if err := unix.Fstat(destinationDescriptor, &destination); err != nil || destination.Mode&unix.S_IFMT != unix.S_IFDIR || destination.Mode&0o777 != 0o700 {
		return auditArchiveManifest{}, authenticationError("private audit extraction root")
	}
	entries, manifest, err := parseCanonicalGitArchive(archive)
	if err != nil {
		return auditArchiveManifest{}, err
	}
	createdDirectories := map[string]struct{}{"": {}}
	for _, entry := range entries {
		parentName := path.Dir(entry.name)
		if parentName == "." {
			parentName = ""
		}
		if entry.directory {
			parentDescriptor, openErr := openAuditArchiveDirectoryAt(destinationDescriptor, parentName, false, createdDirectories)
			if openErr != nil {
				return auditArchiveManifest{}, authenticationError("open audit archive directory parent")
			}
			mkdirErr := unix.Mkdirat(parentDescriptor, path.Base(entry.name), 0o700)
			closeErr := unix.Close(parentDescriptor)
			if mkdirErr != nil || closeErr != nil {
				return auditArchiveManifest{}, authenticationError("create audit archive directory")
			}
			createdDirectories[entry.name] = struct{}{}
			continue
		}
		parentDescriptor, openErr := openAuditArchiveDirectoryAt(destinationDescriptor, parentName, true, createdDirectories)
		if openErr != nil {
			return auditArchiveManifest{}, authenticationError("create implicit audit archive directory")
		}
		descriptor, openErr := unix.Openat(parentDescriptor, path.Base(entry.name), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if openErr != nil {
			_ = unix.Close(parentDescriptor)
			return auditArchiveManifest{}, authenticationError("create audit archive file")
		}
		file := os.NewFile(uintptr(descriptor), path.Base(entry.name))
		if file == nil {
			_ = unix.Close(descriptor)
			_ = unix.Close(parentDescriptor)
			return auditArchiveManifest{}, authenticationError("create audit archive descriptor")
		}
		written, writeErr := file.Write(entry.body)
		mode := uint32(0o444)
		if entry.executable {
			mode = 0o555
		}
		chmodErr := unix.Fchmod(descriptor, mode)
		syncErr := file.Sync()
		closeErr := file.Close()
		parentCloseErr := unix.Close(parentDescriptor)
		if writeErr != nil || written != len(entry.body) || chmodErr != nil || syncErr != nil || closeErr != nil || parentCloseErr != nil {
			return auditArchiveManifest{}, authenticationError("write immutable audit archive file")
		}
	}
	directories := make([]string, 0, len(createdDirectories))
	for directory := range createdDirectories {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(left, right int) bool { return len(directories[left]) > len(directories[right]) })
	for _, directory := range directories {
		descriptor, openErr := openAuditArchiveDirectoryAt(destinationDescriptor, directory, false, createdDirectories)
		if openErr != nil {
			return auditArchiveManifest{}, authenticationError("open audit archive directory for sealing")
		}
		chmodErr := unix.Fchmod(descriptor, 0o555)
		syncErr := unix.Fsync(descriptor)
		closeErr := unix.Close(descriptor)
		if chmodErr != nil || syncErr != nil || closeErr != nil {
			return auditArchiveManifest{}, authenticationError("seal audit archive directory")
		}
	}
	return manifest, nil
}

func openAuditArchiveDirectoryAt(rootDescriptor int, directory string, create bool, created map[string]struct{}) (int, error) {
	descriptor, err := unix.FcntlInt(uintptr(rootDescriptor), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if directory == "" {
		return descriptor, nil
	}
	current := ""
	for _, component := range strings.Split(directory, "/") {
		if !validAuditNamespaceComponent(component) {
			_ = unix.Close(descriptor)
			return -1, authenticationError("audit archive directory component")
		}
		nextName := component
		if current != "" {
			nextName = current + "/" + component
		}
		_, known := created[nextName]
		if create && !known {
			if err := unix.Mkdirat(descriptor, component, 0o700); err != nil {
				_ = unix.Close(descriptor)
				return -1, err
			}
			created[nextName] = struct{}{}
		} else if !known {
			_ = unix.Close(descriptor)
			return -1, authenticationError("unknown audit archive directory")
		}
		next, openErr := unix.Openat(descriptor, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(descriptor)
		if openErr != nil {
			return -1, openErr
		}
		var status unix.Stat_t
		if err := unix.Fstat(next, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Mode&0o777 != 0o700 {
			_ = unix.Close(next)
			return -1, authenticationError("audit archive directory identity")
		}
		descriptor = next
		current = nextName
	}
	return descriptor, nil
}

func parseCanonicalGitArchive(archive []byte) ([]auditArchiveEntry, auditArchiveManifest, error) {
	entries := make([]auditArchiveEntry, 0)
	kinds := make(map[string]bool)
	manifest := auditArchiveManifest{}
	zeroBlocks := 0
	for offset := 0; offset < len(archive); {
		block := archive[offset : offset+tarBlockBytes]
		if allZero(block) {
			zeroBlocks++
			offset += tarBlockBytes
			if zeroBlocks >= 2 {
				if !allZero(archive[offset:]) {
					return nil, auditArchiveManifest{}, authenticationError("Git archive trailing bytes")
				}
				return entries, manifest, nil
			}
			continue
		}
		if zeroBlocks != 0 || string(block[257:263]) != "ustar\x00" || string(block[263:265]) != "00" {
			return nil, auditArchiveManifest{}, authenticationError("Git archive format ambiguity")
		}
		if !validTarChecksum(block) {
			return nil, auditArchiveManifest{}, authenticationError("Git archive checksum")
		}
		typeFlag := block[156]
		if typeFlag != 0 && typeFlag != '0' && typeFlag != '5' {
			return nil, auditArchiveManifest{}, authenticationError("Git archive entry type")
		}
		name, ok := strictTarString(block[0:100])
		prefix, prefixOK := strictTarString(block[345:500])
		linkName, linkOK := strictTarString(block[157:257])
		if !ok || !prefixOK || !linkOK || linkName != "" {
			return nil, auditArchiveManifest{}, authenticationError("Git archive string field")
		}
		if prefix != "" {
			name = prefix + "/" + name
		}
		directory := typeFlag == '5'
		if !validArchivePath(name, directory) {
			return nil, auditArchiveManifest{}, authenticationError("Git archive path")
		}
		name = strings.TrimSuffix(name, "/")
		if _, duplicate := kinds[name]; duplicate {
			return nil, auditArchiveManifest{}, authenticationError("duplicate Git archive entry")
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if parentDirectory, exists := kinds[parent]; exists && !parentDirectory {
				return nil, auditArchiveManifest{}, authenticationError("Git archive parent conflict")
			}
		}
		if !directory {
			for existing := range kinds {
				if strings.HasPrefix(existing, name+"/") {
					return nil, auditArchiveManifest{}, authenticationError("Git archive child conflict")
				}
			}
		}
		mode, modeOK := strictTarOctal(block[100:108])
		size, sizeOK := strictTarOctal(block[124:136])
		if !modeOK || !sizeOK || mode > 0o7777 || size > maxGitArchiveExpandedSize || (directory && size != 0) {
			return nil, auditArchiveManifest{}, authenticationError("Git archive numeric field")
		}
		bodyStart := offset + tarBlockBytes
		bodyEnd := bodyStart + int(size)
		paddedEnd := bodyStart + int((size+tarBlockBytes-1)/tarBlockBytes)*tarBlockBytes
		if bodyEnd < bodyStart || paddedEnd < bodyEnd || paddedEnd > len(archive) {
			return nil, auditArchiveManifest{}, authenticationError("Git archive entry bounds")
		}
		if !allZero(archive[bodyEnd:paddedEnd]) {
			return nil, auditArchiveManifest{}, authenticationError("Git archive padding")
		}
		manifest.EntryCount++
		manifest.ExpandedBytes += int64(size)
		if manifest.EntryCount > maxGitArchiveEntries || manifest.ExpandedBytes > maxGitArchiveExpandedSize {
			return nil, auditArchiveManifest{}, ErrLimit
		}
		entry := auditArchiveEntry{name: name, directory: directory, executable: mode&0o111 != 0}
		if !directory {
			manifest.FileCount++
			entry.body = archive[bodyStart:bodyEnd]
		}
		entries = append(entries, entry)
		kinds[name] = directory
		offset = paddedEnd
	}
	return nil, auditArchiveManifest{}, authenticationError("Git archive missing terminator")
}

func validTarChecksum(block []byte) bool {
	claimed, ok := strictTarOctal(block[148:156])
	if !ok {
		return false
	}
	var sum uint64
	for index, value := range block {
		if index >= 148 && index < 156 {
			sum += ' '
		} else {
			sum += uint64(value)
		}
	}
	return sum == claimed
}

func strictTarString(field []byte) (string, bool) {
	end := bytes.IndexByte(field, 0)
	if end < 0 {
		end = len(field)
	} else if !allZero(field[end:]) {
		return "", false
	}
	value := string(field[:end])
	return value, !strings.ContainsRune(value, 0)
}

func strictTarOctal(field []byte) (uint64, bool) {
	if len(field) == 0 || field[0]&0x80 != 0 {
		return 0, false
	}
	value := strings.TrimSpace(strings.TrimRight(string(field), "\x00 "))
	if value == "" {
		return 0, true
	}
	for _, digit := range value {
		if digit < '0' || digit > '7' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 8, 64)
	return parsed, err == nil
}

func validArchivePath(name string, directory bool) bool {
	if name == "" || len(name) > maxGitArchivePathBytes || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") ||
		strings.ContainsRune(name, 0) || strings.Contains(name, "//") {
		return false
	}
	if directory {
		if !strings.HasSuffix(name, "/") {
			return false
		}
		name = strings.TrimSuffix(name, "/")
	} else if strings.HasSuffix(name, "/") {
		return false
	}
	if name == "" || path.Clean(name) != name {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}

var _ io.Writer = (*boundedCommandBuffer)(nil)
