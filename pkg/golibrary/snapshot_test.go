package golibrary

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/libraryapi"
)

func TestSourceSnapshotTracksSourceContents(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0", inlineConfiguration, "",
		`import helper "example.com/lib/helper"`)
	helper := filepath.Join(root, "helper")
	require.NoError(t, os.Mkdir(helper, 0o755))
	path := filepath.Join(helper, "types.go")
	first := []byte("package helper\ntype Settings struct { Count int }\n")
	require.NoError(t, os.WriteFile(path, first, 0o644))
	t.Setenv("PATH", "")
	before, err := SourceSnapshot(service, nil)
	require.NoError(t, err)

	unrelated := filepath.Join(root, "unrelated")
	require.NoError(t, os.Mkdir(unrelated, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(unrelated, "bad.go"), []byte("invalid"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(helper, "broken_test.go"), []byte("invalid"), 0o644))
	same, err := SourceSnapshot(service, nil)
	require.NoError(t, err)
	assert.Equal(t, before, same)

	stamp := time.Unix(100, 0)
	require.NoError(t, os.Chtimes(path, stamp, stamp))
	require.NoError(t, os.WriteFile(path, []byte(
		"package helper\ntype Settings struct { Count int64 }\n"), 0o644))
	require.NoError(t, os.Chtimes(path, stamp, stamp))
	after, err := SourceSnapshot(service, nil)
	require.NoError(t, err)
	assert.NotEqual(t, before, after)
	require.NoError(t, os.WriteFile(path, first, 0o644))
	restored, err := SourceSnapshot(service, nil)
	require.NoError(t, err)
	assert.Equal(t, before, restored)

	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(
		"module example.com/lib\n\ngo 1.27\n"), 0o644))
	moduleChanged, err := SourceSnapshot(service, nil)
	require.NoError(t, err)
	assert.NotEqual(t, before, moduleChanged)
}

func TestSourceSnapshotTracksSelectedImportsAndMissingSource(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0", inlineConfiguration, "",
		`import helper "example.com/extra/helper"`)
	extra := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(extra, "go.mod"), []byte(
		"module example.com/extra\n\ngo 1.26\n"), 0o644))
	roots := []ModuleSource{{Path: "example.com/extra", Dir: extra}}
	missing, err := SourceSnapshot(service, roots)
	require.NoError(t, err)
	helper := writeForwardingPackage(t, extra, "helper", "2.0", inlineConfiguration, "",
		`import helper "example.com/lib/service"`)
	available, err := SourceSnapshot(service, roots)
	require.NoError(t, err)
	assert.NotEqual(t, missing, available)
	require.NoError(t, os.WriteFile(filepath.Join(helper, "new.go"), []byte(
		"package library\nvar Value = 1\n"), 0o644))
	changed, err := SourceSnapshot(service, roots)
	require.NoError(t, err)
	assert.NotEqual(t, available, changed)

	_, err = SourceSnapshot(filepath.Join(root, "absent"), nil)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestCompatibilityContextChecksSelectedDirectory(t *testing.T) {
	root := writeContextPackage(t, "1.0", "v0.13.0")
	c, err := NewCompatibilityContext(CompatibilityOptions{
		UnobinVersion: "v0.12.0", Modules: []ModuleSource{{
			Path: "example.com/lib", Dir: root, Dependency: "example.com/lib",
			Version: "v0.3.0", Commit: "selected",
		}},
	})
	require.NoError(t, err)
	require.NoError(t, c.CheckDirectory(root, false))
	manifest := c.Manifest()
	require.Len(t, manifest, 1)
	assert.Equal(t, "example.com/lib", manifest[0].Source.Module.Dependency)
	assert.Equal(t, "v0.3.0", manifest[0].Source.Module.Version)
	assert.Equal(t, "selected", manifest[0].Source.Module.Commit)
	assert.False(t, manifest[0].Source.Linked)
	var floor *CoreFloorError
	require.ErrorAs(t, c.CheckDirectory(root, true), &floor)
}

func TestCompatibilityContextSnapshotUsesSelectedRoots(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0", inlineConfiguration, "",
		`import helper "example.com/extra/helper"`)
	extra := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(extra, "go.mod"), []byte(
		"module example.com/extra\n\ngo 1.26\n"), 0o644))
	c, err := NewCompatibilityContext(CompatibilityOptions{
		Modules: []ModuleSource{{Path: "example.com/extra", Dir: extra}},
	})
	require.NoError(t, err)
	before, err := c.SourceSnapshot(service)
	require.NoError(t, err)
	writeForwardingPackage(t, extra, "helper", "2.0", inlineConfiguration, "", "")
	after, err := c.SourceSnapshot(service)
	require.NoError(t, err)
	assert.NotEqual(t, before, after)
}

func TestCompatibilityContextUsesNearestModuleForDirectory(t *testing.T) {
	outer := writeContextPackage(t, "2.0", "")
	inner := filepath.Join(outer, "nested")
	require.NoError(t, os.Mkdir(inner, 0o755))
	writeContextPackageAt(t, inner, "1.0", "")
	service := writeForwardingPackage(t, inner, "service", "1.0",
		"settings.LibraryConfiguration()", "", `import settings "example.com/lib/config"`)
	writeForwardingPackage(t, inner, "config", "1.0", "LibraryConfiguration()",
		inlineConfiguration, "")
	c, err := NewCompatibilityContext(CompatibilityOptions{
		Modules: []ModuleSource{{Path: "example.com/lib", Dir: outer, Version: "v0.3.0"}},
	})
	require.NoError(t, err)
	require.NoError(t, c.CheckDirectory(service, true))
	manifest := c.Manifest()
	packages := make([]string, 0, len(manifest))
	for _, entry := range manifest {
		assert.Equal(t, inner, entry.Source.Module.Dir)
		assert.Empty(t, entry.Source.Module.Version)
		packages = append(packages, entry.Package)
	}
	assert.Equal(t, []string{"example.com/lib/config", "example.com/lib/service"}, packages)
}

func TestCompatibilityContextRechecksCoreDescriptor(t *testing.T) {
	core := t.TempDir()
	writeCoreDescriptor(t, core, libraryapi.Current())
	c, err := NewCompatibilityContext(CompatibilityOptions{CoreReplacement: core})
	require.NoError(t, err)
	root := writeContextPackage(t, "1.0", "")
	require.NoError(t, c.CheckPackage(PackageSource{Dir: root}))
	writeCoreDescriptor(t, core, libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	})
	var mismatch *CoreDescriptorError
	require.ErrorAs(t, c.CheckPackage(PackageSource{Dir: root}), &mismatch)
}
