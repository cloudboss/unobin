package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/deps"
)

// writeCompositeGoSpecsFixture lays out a factory whose stack imports a
// Go library (disk) twice: directly at the root, and again inside a UB
// composite (files.archive). The disk library's file resource declares
// an input default and a constraint, so both import paths must end up
// with the same embedded specs. Returns the stack path and a build
// output directory.
func writeCompositeGoSpecsFixture(t *testing.T) (string, string) {
	t.Helper()
	setCLIVersion(t, "dev")
	rootDir := findUnobinRoot(t)

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"), []byte(`
inputs: {
  path: { type: string }
}
imports: {
  disk:  'github.com/example/disk'
  files: './libraries/files'
}
resources: {
  disk:  { file: { direct: { path: var.path, content: 'direct' } } }
  files: { archive: { this: { path: var.path, body: 'nested' } } }
}
`), 0o644))

	filesDir := filepath.Join(dir, "libraries", "files")
	require.NoError(t, os.MkdirAll(filesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(filesDir, "resource-archive.ub"), []byte(`
description: 'writes one archive member file'
inputs: {
  path: { type: string }
  body: { type: string }
}
imports: {
  disk: 'github.com/example/disk'
}
resources: {
  disk: { file: { this: { path: var.path, content: var.body } } }
}
`), 0o644))

	diskDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(diskDir, "go.mod"),
		[]byte("module github.com/example/disk\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(diskDir, "disk.go"), []byte(`package disk

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "disk",
		Resources: map[string]runtime.ResourceRegistration{
			"file": runtime.MakeResource[File, *FileOutput](),
		},
	}
}

type File struct {
	Path            string
	Content         string
	Mode            int64
	CreateDirectory bool
}

func (f File) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Mode, 420),
		defaults.Optional(f.CreateDirectory),
	}
}

func (f File) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Present(f.Path)).
			Message("a file needs a path"),
	}
}

type FileOutput struct {
	SHA256 string
}

func (f *File) SchemaVersion() int { return 1 }

func (f *File) Create(_ context.Context, _ any) (*FileOutput, error) {
	return &FileOutput{}, nil
}

func (f *File) Read(_ context.Context, _ any, _ *FileOutput) (*FileOutput, error) {
	return &FileOutput{}, nil
}

func (f *File) Update(
	_ context.Context, _ any, _ runtime.Prior[File, *FileOutput],
) (*FileOutput, error) {
	return &FileOutput{}, nil
}

func (f *File) Delete(_ context.Context, _ any, _ *FileOutput) error {
	return nil
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		[]byte("requires: {}\nreplace: {\n"+
			"  'github.com/cloudboss/unobin': '"+rootDir+"'\n"+
			"  'github.com/example/disk': '"+diskDir+"'\n"+
			"}\n"), 0o644))

	return filepath.Join(dir, "main.ub"), filepath.Join(t.TempDir(), "build")
}

// compileCompositeGoSpecsFixture compiles the fixture and returns the
// generated main.go and the generated files package source.
func compileCompositeGoSpecsFixture(t *testing.T) (string, string) {
	t.Helper()
	stackPath, outDir := writeCompositeGoSpecsFixture(t)
	_, err := runCommand(t, "compile", "-p", stackPath, "-o", outDir)
	require.NoError(t, err)

	mainBytes, err := os.ReadFile(filepath.Join(outDir, "main.go"))
	require.NoError(t, err)
	pkgBytes, err := os.ReadFile(filepath.Join(outDir, "internal", "files", "files.go"))
	require.NoError(t, err)
	return string(mainBytes), string(pkgBytes)
}

// TestCompileEmbedsGoDefaultsForCompositeImport proves a Go library's
// declared input defaults reach the generated source for BOTH of its
// import paths. At run time a composite-internal node resolves its
// library against the composite boundary's table, never the root one,
// so specs missing from the generated UB-library package are invisible
// at plan and apply: the disk file inside files.archive would be
// created without its mode default.
func TestCompileEmbedsGoDefaultsForCompositeImport(t *testing.T) {
	mainSrc, filesSrc := compileCompositeGoSpecsFixture(t)

	// Root import: the defaults injection main.go performs today.
	// This half calibrates the exact spec form the extractor emits.
	require.Contains(t, mainSrc, `libraries["disk"].Defaults`)
	require.Contains(t, mainSrc, `{Field: "var.mode", Value: "420"}`)
	require.Contains(t, mainSrc, `{Field: "var.create-directory", Optional: true}`)

	// Composite import: the generated files package must embed the
	// same specs for its own disk binding.
	require.Contains(t, filesSrc, "lang.DefaultSpec",
		"the generated UB-library package embeds no defaults at all")
	require.Contains(t, filesSrc, `{Field: "var.mode", Value: "420"}`)
	require.Contains(t, filesSrc, `{Field: "var.create-directory", Optional: true}`)
}

// TestCompileEmbedsGoConstraintsForCompositeImport proves a Go
// library's declared constraints reach the generated source for BOTH
// of its import paths. Plan-time enforcement reads the boundary
// table's Constraints for a composite-internal step, so specs missing
// from the generated UB-library package mean the library's rules are
// silently skipped for every body inside a composite.
func TestCompileEmbedsGoConstraintsForCompositeImport(t *testing.T) {
	mainSrc, filesSrc := compileCompositeGoSpecsFixture(t)

	// Root import: the constraints injection main.go performs today.
	require.Contains(t, mainSrc, `libraries["disk"].Constraints`)
	require.Contains(t, mainSrc, "a file needs a path")

	// Composite import: the generated files package must embed the
	// same specs for its own disk binding.
	require.Contains(t, filesSrc, "lang.ConstraintSpec",
		"the generated UB-library package embeds no constraints at all")
	require.Contains(t, filesSrc, "a file needs a path")
}
