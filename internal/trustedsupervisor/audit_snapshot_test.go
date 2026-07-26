package trustedsupervisor

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMaterializeAuditSnapshotCapturesExactCommitReadonly(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	before, err := os.ReadFile(filepath.Join(material.entry.Repository.Path, "audit.txt"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := materializeAuditSnapshot(context.Background(), material.policy, material.entry, "audit_run_snapshot_001", auditSnapshotHooks{})
	if err != nil {
		t.Fatalf("materialize exact audit snapshot: %v", err)
	}
	t.Cleanup(func() { makeTreeRemovableForTest(snapshot.RunRoot) })
	if snapshot.ArchiveSHA256 != material.entry.SourceArchiveSHA256 || snapshot.GitCommit != material.entry.GitCommit ||
		snapshot.GitTree != material.entry.GitTree || snapshot.SourceRoot == material.entry.Repository.Path {
		t.Fatalf("snapshot lost exact source authority: %+v", snapshot)
	}
	copied, err := os.ReadFile(filepath.Join(snapshot.SourceRoot, "audit.txt"))
	if err != nil || !bytes.Equal(copied, before) {
		t.Fatalf("snapshot source = %q, %v; want %q", copied, err, before)
	}
	for _, path := range []string{snapshot.RunRoot, snapshot.SourceRoot, filepath.Join(snapshot.SourceRoot, "nested")} {
		information, err := os.Stat(path)
		if err != nil || information.Mode().Perm() != 0o555 {
			t.Fatalf("snapshot directory %s mode = %v, %v; want 0555", path, information.Mode(), err)
		}
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(snapshot.SourceRoot, "audit.txt"):         0o444,
		filepath.Join(snapshot.SourceRoot, "nested", "tool.sh"): 0o555,
	} {
		information, err := os.Stat(path)
		if err != nil || information.Mode().Perm() != mode {
			t.Fatalf("snapshot file %s mode = %v, %v; want %v", path, information.Mode(), err, mode)
		}
	}
	if after, err := os.ReadFile(filepath.Join(material.entry.Repository.Path, "audit.txt")); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("original repository changed: %q, %v", after, err)
	}
}

func TestCanonicalGitArchiveRejectsAmbiguousOrUnsafeEntries(t *testing.T) {
	cases := []struct {
		name    string
		headers []tar.Header
		bodies  [][]byte
	}{
		{name: "traversal", headers: []tar.Header{{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}}, bodies: [][]byte{[]byte("x")}},
		{name: "absolute", headers: []tar.Header{{Name: "/escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}}, bodies: [][]byte{[]byte("x")}},
		{name: "duplicate", headers: []tar.Header{{Name: "same", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, {Name: "same", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}}, bodies: [][]byte{[]byte("a"), []byte("b")}},
		{name: "symlink", headers: []tar.Header{{Name: "link", Linkname: "target", Mode: 0o777, Typeflag: tar.TypeSymlink}}, bodies: [][]byte{nil}},
		{name: "hardlink", headers: []tar.Header{{Name: "link", Linkname: "target", Mode: 0o644, Typeflag: tar.TypeLink}}, bodies: [][]byte{nil}},
		{name: "special", headers: []tar.Header{{Name: "fifo", Mode: 0o600, Typeflag: tar.TypeFifo}}, bodies: [][]byte{nil}},
		{name: "pax", headers: []tar.Header{{Name: "pax", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg, Format: tar.FormatPAX, PAXRecords: map[string]string{"comment": "ambiguous"}}}, bodies: [][]byte{[]byte("x")}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			archive := buildArchiveForTest(t, testCase.headers, testCase.bodies)
			destination := filepath.Join(t.TempDir(), "source")
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			descriptor, err := unix.Open(destination, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(descriptor)
			if _, err := validateAndExtractGitArchiveAt(archive, descriptor); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(destination), "escape")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("archive escaped destination: %v", err)
			}
		})
	}
}

func TestMaterializeAuditSnapshotRejectsCommitTreeArchiveAndTOCTOUDrift(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *gitArchivePolicyMaterial, *auditSnapshotHooks)
	}{
		{"commit object", func(_ *testing.T, material *gitArchivePolicyMaterial, _ *auditSnapshotHooks) {
			material.entry.GitCommitObjectSHA256 = testHash("wrong-commit-object")
		}},
		{"tree", func(_ *testing.T, material *gitArchivePolicyMaterial, _ *auditSnapshotHooks) {
			material.entry.GitTree = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}},
		{"archive", func(_ *testing.T, material *gitArchivePolicyMaterial, _ *auditSnapshotHooks) {
			material.entry.SourceArchiveSHA256 = testHash("wrong-archive")
		}},
		{"repository replacement", func(t *testing.T, material *gitArchivePolicyMaterial, _ *auditSnapshotHooks) {
			if err := os.Rename(material.entry.Repository.Path, material.entry.Repository.Path+".pinned"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(material.entry.Repository.Path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrapper replacement before start", func(t *testing.T, material *gitArchivePolicyMaterial, hooks *auditSnapshotHooks) {
			hooks.BeforeGitStart = func(operation string) {
				if operation != "commit" {
					return
				}
				if err := os.Rename(material.entry.Wrapper.Path, material.entry.Wrapper.Path+".pinned"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(material.entry.Wrapper.Path, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			hooks := auditSnapshotHooks{}
			testCase.mutate(t, &material, &hooks)
			if _, err := materializeAuditSnapshot(context.Background(), material.policy, material.entry, "audit_run_drift_001", hooks); err == nil {
				t.Fatal("drifted snapshot authority was accepted")
			}
		})
	}
}

func TestMaterializeAuditSnapshotNeedsNoMutableRepositoryAfterArchiveCapture(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	hooks := auditSnapshotHooks{AfterArchiveCapture: func() {
		if err := os.WriteFile(filepath.Join(material.entry.Repository.Path, "audit.txt"), []byte("working tree changed after capture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	snapshot, err := materializeAuditSnapshot(context.Background(), material.policy, material.entry, "audit_run_capture_001", hooks)
	if err != nil {
		t.Fatalf("post-capture repository change affected snapshot: %v", err)
	}
	t.Cleanup(func() { makeTreeRemovableForTest(snapshot.RunRoot) })
	contents, err := os.ReadFile(filepath.Join(snapshot.SourceRoot, "audit.txt"))
	if err != nil || string(contents) != "immutable audit source\n" {
		t.Fatalf("captured snapshot = %q, %v", contents, err)
	}
}
func TestMaterializeAuditSnapshotRetainsWorkRootDescriptorAcrossEveryMutation(t *testing.T) {
	stages := []string{"staging_create", "source_create", "extract", "seal", "publish", "sync", "capture"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			material := newGitArchivePolicyMaterial(t)
			const runID = "audit_run_work_root_swap_001"
			configuredRoot := material.entry.WorkRoot
			originalRoot := configuredRoot + ".retained-original"
			replacementMarker := filepath.Join(configuredRoot, "replacement-must-remain-empty")
			swapped := false
			hooks := auditSnapshotHooks{BeforeSnapshotMutation: func(current string) {
				if current != stage || swapped {
					return
				}
				if err := os.Rename(configuredRoot, originalRoot); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(configuredRoot, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(replacementMarker, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
				swapped = true
			}}
			snapshot, err := materializeAuditSnapshot(context.Background(), material.policy, material.entry, runID, hooks)
			if err != nil {
				t.Fatalf("descriptor-relative snapshot at %s: %v", stage, err)
			}
			if !swapped {
				t.Fatalf("snapshot never reached mutation stage %q", stage)
			}
			originalRun := filepath.Join(originalRoot, runID)
			makeTreeRemovableForTest(originalRun)
			t.Cleanup(func() { makeTreeRemovableForTest(originalRun) })
			contents, err := os.ReadFile(filepath.Join(originalRun, "source", "audit.txt"))
			if err != nil || string(contents) != "immutable audit source\n" {
				t.Fatalf("original work descriptor output = %q, %v", contents, err)
			}
			for _, name := range []string{runID, "." + runID + ".staging"} {
				if _, err := os.Lstat(filepath.Join(configuredRoot, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("replacement root received %q: %v", name, err)
				}
			}
			if marker, err := os.ReadFile(replacementMarker); err != nil || string(marker) != "replacement" {
				t.Fatalf("replacement root marker = %q, %v", marker, err)
			}
			if len(snapshot.OwnedRoots) != 2 || snapshot.OwnedRoots[0].Path != filepath.Join(configuredRoot, runID) ||
				snapshot.OwnedRoots[1].Path != filepath.Join(configuredRoot, runID, "source") {
				t.Fatalf("descriptor-relative capture lost configured identity paths: %+v", snapshot.OwnedRoots)
			}
			information, err := os.Stat(originalRun)
			status, ok := informationSyscallStat(information)
			if err != nil || !ok || snapshot.OwnedRoots[0].Device != statDecimal(uint64(status.Dev)) ||
				snapshot.OwnedRoots[0].Inode != statDecimal(status.Ino) {
				t.Fatalf("captured work identity does not target original: %+v info=%v err=%v", snapshot.OwnedRoots[0], information, err)
			}
		})
	}
}

type gitArchivePolicyMaterial struct {
	directory  string
	policyPath string
	policy     *executionPolicy
	entry      executionPolicyEntry
}

func newGitArchivePolicyMaterial(t *testing.T) gitArchivePolicyMaterial {
	t.Helper()
	base := newExecutionPolicyTestMaterial(t)
	repository := base.entry.Repository.Path
	runGitForTest(t, repository, "init", "-q")
	runGitForTest(t, repository, "config", "user.name", "Ananke Test")
	runGitForTest(t, repository, "config", "user.email", "ananke-test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "audit.txt"), []byte("immutable audit source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "nested", "tool.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, repository, "add", "--", "audit.txt", "nested/tool.sh")
	runGitForTest(t, repository, "commit", "-q", "-m", "immutable audit fixture")
	commit := strings.TrimSpace(runGitForTest(t, repository, "rev-parse", "HEAD^{commit}"))
	tree := strings.TrimSpace(runGitForTest(t, repository, "rev-parse", "HEAD^{tree}"))
	commitObject := []byte(runGitForTest(t, repository, "cat-file", "commit", commit))
	archive := runGitBytesForTest(t, repository, "archive", "--format=tar", "--mtime=1970-01-01T00:00:00Z", tree)
	base.entry.GitCommit = commit
	base.entry.GitTree = tree
	base.entry.GitCommitObjectSHA256 = hashJournalBytes(commitObject)
	base.entry.SourceArchiveSHA256 = hashJournalBytes(archive)
	base.entry = mustSealExecutionPolicyEntryForTest(t, base.entry)
	writeExecutionPolicyFileForTest(t, base.policyPath, []executionPolicyEntry{base.entry})
	policy, err := loadExecutionPolicyForTest(base.policyPath, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	policy.testBrokerDependencies = fakeAuditBrokerDependencies()
	return gitArchivePolicyMaterial{directory: base.directory, policyPath: base.policyPath, policy: policy, entry: base.entry}
}

func runGitForTest(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	return string(runGitBytesForTest(t, repository, arguments...))
}

func runGitBytesForTest(t *testing.T, repository string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command("/usr/bin/git", append([]string{"-C", repository}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Fatalf("git %v: %v\n%s", arguments, err, exit.Stderr)
		}
		t.Fatalf("git %v: %v", arguments, err)
	}
	return output
}

func buildArchiveForTest(t *testing.T, headers []tar.Header, bodies [][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for index := range headers {
		header := headers[index]
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if len(bodies[index]) != 0 {
			if _, err := writer.Write(bodies[index]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeTreeRemovableForTest(root string) {
	_ = filepath.Walk(root, func(path string, information os.FileInfo, err error) error {
		if err == nil && information.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
}
