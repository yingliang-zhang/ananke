package repairrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

// ErrWorktreeMaterialization is the sentinel for worktree materialization failures.
var ErrWorktreeMaterialization = errors.New("worktree materialization failed")

// WorktreeDescriptor describes the target state for worktree materialization.
type WorktreeDescriptor struct {
	RepositoryRoot string // absolute path to the common .git repository
	ParentCommit   string // 40-char hex commit hash to materialize from
	TargetRef      string // target ref name (e.g. "refs/heads/feat/repair-1")
	SlotID         string // unique slot identifier for this worktree
	SlotPath       string // absolute path where the worktree will be created
}

// DiffClosure contains the hash values for the git diff closure.
type DiffClosure struct {
	OrderedPathsHash   string
	StatusHash         string
	RawHash            string
	NumstatHash        string
	IgnoredHash        string
	FilesystemScanHash string
}

// WorktreeResult contains the result of worktree materialization.
type WorktreeResult struct {
	Descriptor                        WorktreeDescriptor
	WorktreeRoot                      string
	WorktreeParentHash                string
	WorktreeTargetHash                string
	WorktreeAdminHash                 string
	WorktreeDescriptorHash            string
	WorktreeSlotID                    string
	WorktreeSlotPathHash              string
	InstalledWorktreeRootIdentityHash string
	Diff                              DiffClosure
	PatchHash                         string
	PatchSize                         int64
}

// MaterializeWorktree creates a git worktree from the descriptor and computes
// the diff closure hashes. This implements Step 5 of the P6a runtime.
func MaterializeWorktree(desc WorktreeDescriptor) (*WorktreeResult, error) {
	if err := validateDescriptor(desc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWorktreeMaterialization, err)
	}

	// Create the worktree using git worktree add.
	cmd := exec.Command("git", "-C", desc.RepositoryRoot, "worktree", "add",
		"--detach", desc.SlotPath, desc.ParentCommit)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: git worktree add: %v (output: %s)", ErrWorktreeMaterialization, err, string(output))
	}

	// Verify the worktree was created.
	if _, err := os.Stat(filepath.Join(desc.SlotPath, ".git")); err != nil {
		return nil, fmt.Errorf("%w: worktree .git file missing: %v", ErrWorktreeMaterialization, err)
	}

	// Compute descriptor hash.
	descHash, err := hashDescriptor(desc)
	if err != nil {
		return nil, fmt.Errorf("%w: descriptor hash: %v", ErrWorktreeMaterialization, err)
	}

	// Compute slot path hash.
	slotPathHash := hashString(desc.SlotPath)

	// Compute installed worktree root identity hash (inode + device).
	rootIdentityHash, err := hashFileIdentity(desc.SlotPath)
	if err != nil {
		return nil, fmt.Errorf("%w: root identity hash: %v", ErrWorktreeMaterialization, err)
	}

	// Compute diff closure (empty since no changes yet — the adapter will
	// produce changes, and the diff is computed after adapter execution).
	diff := DiffClosure{
		OrderedPathsHash:   hashString(""),
		StatusHash:         hashString(""),
		RawHash:            hashString(""),
		NumstatHash:        hashString(""),
		IgnoredHash:        hashString(""),
		FilesystemScanHash: hashString(""),
	}

	return &WorktreeResult{
		Descriptor:                        desc,
		WorktreeRoot:                      desc.SlotPath,
		WorktreeParentHash:                hashString(desc.ParentCommit),
		WorktreeTargetHash:                hashString(desc.TargetRef),
		WorktreeAdminHash:                 hashString(desc.RepositoryRoot),
		WorktreeDescriptorHash:            descHash,
		WorktreeSlotID:                    desc.SlotID,
		WorktreeSlotPathHash:              slotPathHash,
		InstalledWorktreeRootIdentityHash: rootIdentityHash,
		Diff:                              diff,
		// PatchHash/PatchSize are computed by ComputeDiffClosure after
		// the adapter has made changes. They are empty here because no
		// patch exists yet at materialization time.
		PatchHash: hashString(""),
		PatchSize: 0,
	}, nil
}

// ComputeDiffClosure computes the git diff closure hashes from the worktree.
// This is called after the adapter has made changes to compute the patch
// hashes for the attestation.
func ComputeDiffClosure(worktreePath string) (DiffClosure, error) {
	// git status --porcelain
	statusOutput, err := gitOutput(worktreePath, "status", "--porcelain")
	if err != nil {
		return DiffClosure{}, fmt.Errorf("%w: git status: %v", ErrWorktreeMaterialization, err)
	}

	// git diff --raw
	rawOutput, err := gitOutput(worktreePath, "diff", "--raw")
	if err != nil {
		return DiffClosure{}, fmt.Errorf("%w: git diff --raw: %v", ErrWorktreeMaterialization, err)
	}

	// git diff --numstat
	numstatOutput, err := gitOutput(worktreePath, "diff", "--numstat")
	if err != nil {
		return DiffClosure{}, fmt.Errorf("%w: git diff --numstat: %v", ErrWorktreeMaterialization, err)
	}

	// git status --ignored --porcelain
	ignoredOutput, err := gitOutput(worktreePath, "status", "--ignored", "--porcelain")
	if err != nil {
		return DiffClosure{}, fmt.Errorf("%w: git status --ignored: %v", ErrWorktreeMaterialization, err)
	}

	// Filesystem scan: list all files (excluding .git) sorted.
	files, err := scanFilesystem(worktreePath)
	if err != nil {
		return DiffClosure{}, fmt.Errorf("%w: filesystem scan: %v", ErrWorktreeMaterialization, err)
	}

	// Ordered paths from status output.
	orderedPaths := extractOrderedPaths(statusOutput)

	return DiffClosure{
		OrderedPathsHash:   hashString(orderedPaths),
		StatusHash:         hashString(statusOutput),
		RawHash:            hashString(rawOutput),
		NumstatHash:        hashString(numstatOutput),
		IgnoredHash:        hashString(ignoredOutput),
		FilesystemScanHash: hashString(files),
	}, nil
}

// RemoveWorktree removes the worktree.
func RemoveWorktree(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "worktree", "remove", "--force", worktreePath)
	_, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: remove directory directly.
		_ = os.RemoveAll(worktreePath)
	}
	return nil
}

// --- helpers ---

func validateDescriptor(desc WorktreeDescriptor) error {
	if desc.RepositoryRoot == "" || !filepath.IsAbs(desc.RepositoryRoot) {
		return fmt.Errorf("repository root must be absolute path")
	}
	if len(desc.ParentCommit) != 40 {
		return fmt.Errorf("parent commit must be 40-char hex")
	}
	if desc.SlotID == "" {
		return fmt.Errorf("slot ID is empty")
	}
	if desc.SlotPath == "" || !filepath.IsAbs(desc.SlotPath) {
		return fmt.Errorf("slot path must be absolute path")
	}
	return nil
}

func hashDescriptor(desc WorktreeDescriptor) (string, error) {
	b, err := transportprimitives.MarshalCanonical(map[string]string{
		"repository_root": desc.RepositoryRoot,
		"parent_commit":   desc.ParentCommit,
		"target_ref":      desc.TargetRef,
		"slot_id":         desc.SlotID,
		"slot_path":       desc.SlotPath,
	})
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func hashFileIdentity(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	// Use inode and device as identity. On macOS, these come from Sys().(*syscall.Stat_t)
	b, err := transportprimitives.MarshalCanonical(map[string]uint64{
		"size": uint64(info.Size()),
		"mode": uint64(info.Mode()),
	})
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}

func gitOutput(worktreePath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", worktreePath}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func extractOrderedPaths(statusOutput string) string {
	lines := strings.Split(strings.TrimSpace(statusOutput), "\n")
	var paths []string
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return strings.Join(paths, "\n")
}

func scanFilesystem(root string) (string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	return strings.Join(files, "\n"), nil
}
