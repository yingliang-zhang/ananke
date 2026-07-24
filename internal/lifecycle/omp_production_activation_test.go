package lifecycle

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const canonicalP3dSourceSnapshotSHA256 = "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4"

func TestOMPProductionActivationPreparesPinnedWrapperFDRequest(t *testing.T) {
	approval := ompProductionApprovedWrapperIdentityForTest()
	journal, fence := newP3fAdmittedFence(t)
	preparer, err := newOMPProductionActivationPreparer(journal, approval, func() time.Time {
		return time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("construct production activation preparer: %v", err)
	}
	input := ompProductionActivationInputForTest(t, approval, fence)

	prepared, err := preparer.prepare(context.Background(), input)
	if err != nil {
		t.Fatalf("prepare sealed fake-wrapper activation: %v", err)
	}
	if prepared.launchSpecHash != input.launchSpecHash || prepared.fence != input.fence ||
		!prepared.deadline.Equal(input.deadline) || prepared.p3cAction != input.p3cAction ||
		prepared.p3dHostSpecHash != input.p3dHostSpecHash ||
		prepared.p3dSourceSnapshotHash != input.p3dSourceSnapshotHash ||
		prepared.sourceManifestHash != input.sourceManifestHash || prepared.wrapper != approval {
		t.Fatalf("prepared activation identity = %+v, want exact input identity", prepared)
	}
	if prepared.source != input.descriptors.source || prepared.manifest != input.descriptors.manifest || prepared.evidence != input.descriptors.evidence {
		t.Fatal("prepared activation did not retain the exact typed FD-only descriptors")
	}
}

func TestOMPProductionActivationPreparationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ompProductionActivationInput)
	}{
		{name: "wrapper hash", mutate: func(input *ompProductionActivationInput) {
			input.wrapper.binarySHA256 = "sha256:" + strings.Repeat("a", 64)
		}},
		{name: "wrapper kind", mutate: func(input *ompProductionActivationInput) { input.wrapper.kind = "other_wrapper" }},
		{name: "wrapper route", mutate: func(input *ompProductionActivationInput) { input.wrapper.route = "other_route" }},
		{name: "deadline drift", mutate: func(input *ompProductionActivationInput) { input.deadline = input.deadline.Add(time.Second) }},
		{name: "P3c action", mutate: func(input *ompProductionActivationInput) { input.p3cAction = "retry_other_action" }},
		{name: "P3d HostSpec hash", mutate: func(input *ompProductionActivationInput) { input.p3dHostSpecHash = "sha256:" + strings.Repeat("b", 64) }},
		{name: "P3d source hash malformed", mutate: func(input *ompProductionActivationInput) {
			input.p3dSourceSnapshotHash = "sha256:" + strings.Repeat("C", 64)
		}},
		{name: "P3d source hash bad length", mutate: func(input *ompProductionActivationInput) {
			input.p3dSourceSnapshotHash = "sha256:" + strings.Repeat("c", 63)
		}},
		{name: "P3d source hash drift", mutate: func(input *ompProductionActivationInput) {
			input.p3dSourceSnapshotHash = "sha256:" + strings.Repeat("c", 64)
		}},
		{name: "source manifest hash", mutate: func(input *ompProductionActivationInput) {
			input.sourceManifestHash = "sha256:" + strings.Repeat("d", 64)
		}},
		{name: "launch spec", mutate: func(input *ompProductionActivationInput) { input.launchSpecHash = "sha256:" + strings.Repeat("e", 64) }},
		{name: "full fence", mutate: func(input *ompProductionActivationInput) {
			input.fence.ClaimTokenHash = "sha256:" + strings.Repeat("f", 64)
		}},
		{name: "missing source descriptor", mutate: func(input *ompProductionActivationInput) { input.descriptors.source = nil }},
		{name: "aliased descriptors", mutate: func(input *ompProductionActivationInput) { input.descriptors.manifest = input.descriptors.source }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			approval := ompProductionApprovedWrapperIdentityForTest()
			journal, fence := newP3fAdmittedFence(t)
			preparer, err := newOMPProductionActivationPreparer(journal, approval, func() time.Time {
				return time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
			})
			if err != nil {
				t.Fatalf("construct production activation preparer: %v", err)
			}
			input := ompProductionActivationInputForTest(t, approval, fence)
			tc.mutate(&input)

			prepared, prepareErr := preparer.prepare(context.Background(), input)
			assertOMPProductionActivationDenied(t, prepared, prepareErr)
		})
	}
}

func TestOMPProductionActivationPreparationRejectsExpiredDeadline(t *testing.T) {
	approval := ompProductionApprovedWrapperIdentityForTest()
	journal, fence := newP3fAdmittedFence(t)
	preparer, err := newOMPProductionActivationPreparer(journal, approval, func() time.Time {
		return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("construct production activation preparer: %v", err)
	}

	prepared, prepareErr := preparer.prepare(context.Background(), ompProductionActivationInputForTest(t, approval, fence))
	assertOMPProductionActivationDenied(t, prepared, prepareErr)
}

func TestOMPProductionActivationPreparationRejectsNilContext(t *testing.T) {
	approval := ompProductionApprovedWrapperIdentityForTest()
	journal, fence := newP3fAdmittedFence(t)
	preparer, err := newOMPProductionActivationPreparer(journal, approval, func() time.Time {
		return time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("construct production activation preparer: %v", err)
	}

	prepared, prepareErr := preparer.prepare(nil, ompProductionActivationInputForTest(t, approval, fence))
	assertOMPProductionActivationDenied(t, prepared, prepareErr)
}

func TestOMPProductionActivationRejectsUnapprovedWrapperManifest(t *testing.T) {
	approval := ompProductionApprovedWrapperIdentityForTest()
	for _, tc := range []struct {
		name   string
		mutate func(*ompApprovedWrapperIdentityManifest)
	}{
		{name: "schema", mutate: func(manifest *ompApprovedWrapperIdentityManifest) { manifest.schemaVersion = "other.v1" }},
		{name: "invalid hash", mutate: func(manifest *ompApprovedWrapperIdentityManifest) { manifest.binarySHA256 = "not-a-hash" }},
		{name: "bare OMP", mutate: func(manifest *ompApprovedWrapperIdentityManifest) { manifest.route = "omp" }},
		{name: "other wrapper", mutate: func(manifest *ompApprovedWrapperIdentityManifest) { manifest.kind = "other_wrapper" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := approval
			tc.mutate(&candidate)
			preparer, err := newOMPProductionActivationPreparer(nil, candidate, time.Now)
			if preparer != nil || !errors.Is(err, errOMPProductionActivationDenied) || err.Error() != errOMPProductionActivationDenied.Error() {
				t.Fatalf("unsafe approved wrapper manifest result = preparer:%v err:%v, want sanitized denial", preparer, err)
			}
		})
	}
}

func TestOMPProductionP3dSourceSnapshotHashPinsCanonicalSHA256(t *testing.T) {
	if ompProductionP3dSourceSnapshotHash != canonicalP3dSourceSnapshotSHA256 {
		t.Fatalf("P3d source snapshot hash = %q, want canonical P3d value %q", ompProductionP3dSourceSnapshotHash, canonicalP3dSourceSnapshotSHA256)
	}
	if !validOMPSHA256(canonicalP3dSourceSnapshotSHA256) {
		t.Fatalf("canonical P3d source snapshot hash is not a valid SHA-256: %q", canonicalP3dSourceSnapshotSHA256)
	}
}

func TestOMPProductionActivationCoreExposesNoExecutionSurface(t *testing.T) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "omp_production_activation.go", nil, 0)
	if err != nil {
		t.Fatalf("parse production activation core: %v", err)
	}
	osAliases := map[string]bool{}
	syscallAliases := map[string]bool{}
	for _, imported := range parsed.Imports {
		importPath := strings.Trim(imported.Path.Value, `"`)
		if importPath == "os/exec" {
			t.Fatal("production activation core imports os/exec")
		}
		alias := ""
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		switch importPath {
		case "os":
			if alias == "." {
				t.Fatal("production activation core dot-imports os")
			}
			if alias == "" {
				alias = "os"
			}
			if alias != "_" {
				osAliases[alias] = true
			}
		case "syscall":
			if alias == "." {
				t.Fatal("production activation core dot-imports syscall")
			}
			if alias == "" {
				alias = "syscall"
			}
			if alias != "_" {
				syscallAliases[alias] = true
			}
		}
	}
	forbiddenNames := map[string]bool{
		"argv":       true,
		"command":    true,
		"env":        true,
		"executable": true,
		"path":       true,
		"program":    true,
	}
	forbiddenOSSelectors := map[string]bool{"Args": true, "Environ": true, "StartProcess": true}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.TypeSpec:
			if _, isStruct := current.Type.(*ast.StructType); isStruct && forbiddenNames[normalizeOMPProductionASTName(current.Name.Name)] {
				t.Fatalf("production activation core exposes forbidden execution struct %q", current.Name.Name)
			}
		case *ast.Field:
			for _, name := range current.Names {
				if forbiddenNames[normalizeOMPProductionASTName(name.Name)] {
					t.Fatalf("production activation core exposes forbidden execution field %q", name.Name)
				}
			}
		case *ast.SelectorExpr:
			packageName, ok := current.X.(*ast.Ident)
			if !ok {
				return true
			}
			if osAliases[packageName.Name] && forbiddenOSSelectors[current.Sel.Name] {
				t.Fatalf("production activation core references forbidden os.%s", current.Sel.Name)
			}
			if syscallAliases[packageName.Name] && current.Sel.Name == "Exec" {
				t.Fatal("production activation core references forbidden syscall.Exec")
			}
		}
		return true
	})
}

func normalizeOMPProductionASTName(value string) string {
	return strings.Map(func(character rune) rune {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			return character
		}
		return -1
	}, strings.ToLower(value))
}

func ompProductionApprovedWrapperIdentityForTest() ompApprovedWrapperIdentityManifest {
	return ompProductionApprovedWrapperIdentity()
}

func ompProductionActivationInputForTest(t *testing.T, approval ompApprovedWrapperIdentityManifest, fence store.LaunchFence) ompProductionActivationInput {
	t.Helper()
	return ompProductionActivationInput{
		launchSpecHash:        p3eLaunchSpecHash,
		fence:                 fence,
		deadline:              time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		p3cAction:             "retry_process_admission",
		p3dHostSpecHash:       "sha256:f176da10c1c055b930b17137d790580400c9a0e6963e967290a19cb608512e8b",
		p3dSourceSnapshotHash: canonicalP3dSourceSnapshotSHA256,
		sourceManifestHash:    "sha256:842188d5ce1e461839bf33fb50a4040a3bf9f2e44d94c31be640058f5765cc15",
		wrapper:               approval,
		descriptors:           ompProductionActivationDescriptorsForTest(t),
	}
}

func ompProductionActivationDescriptorsForTest(t *testing.T) ompProductionActivationDescriptors {
	t.Helper()
	source, sourceWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create source descriptor: %v", err)
	}
	manifest, manifestWriter, err := os.Pipe()
	if err != nil {
		_ = source.Close()
		_ = sourceWriter.Close()
		t.Fatalf("create manifest descriptor: %v", err)
	}
	evidence, evidenceWriter, err := os.Pipe()
	if err != nil {
		_ = source.Close()
		_ = sourceWriter.Close()
		_ = manifest.Close()
		_ = manifestWriter.Close()
		t.Fatalf("create evidence descriptor: %v", err)
	}
	t.Cleanup(func() {
		_ = source.Close()
		_ = sourceWriter.Close()
		_ = manifest.Close()
		_ = manifestWriter.Close()
		_ = evidence.Close()
		_ = evidenceWriter.Close()
	})
	return ompProductionActivationDescriptors{source: source, manifest: manifest, evidence: evidence}
}

func assertOMPProductionActivationDenied(t *testing.T, prepared ompPreparedFDActivationRequest, err error) {
	t.Helper()
	if !errors.Is(err, errOMPProductionActivationDenied) || err.Error() != errOMPProductionActivationDenied.Error() {
		t.Fatalf("unsafe activation preparation error = %v, want sanitized %v", err, errOMPProductionActivationDenied)
	}
	if prepared.source != nil || prepared.manifest != nil || prepared.evidence != nil || prepared.launchSpecHash != "" || prepared.wrapper != (ompApprovedWrapperIdentityManifest{}) {
		t.Fatalf("unsafe activation preparation returned request %+v", prepared)
	}
}
