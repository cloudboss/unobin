package golibrary

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

func TestCompatibilityContextChecksCurrentDeclaration(t *testing.T) {
	dir := writeContextPackage(t, "1.2", "")
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.4"}, GeneratorAPI: "1.4",
	}
	c, err := NewCompatibilityContext(CompatibilityOptions{
		Descriptor: &descriptor, UnobinVersion: "v0.12.0",
	})
	require.NoError(t, err)
	descriptor.ImplementedAPIs[0] = "2.0"
	source := PackageSource{Module: ModuleSource{
		Dir: dir, Dependency: "example.com/lib", Version: "v0.4.0", Commit: "selected",
	}, Dir: dir, Linked: true}
	require.NoError(t, c.CheckPackage(source))
	manifest := c.Manifest()
	require.Len(t, manifest, 1)
	assert.Equal(t, "example.com/lib", manifest[0].Package)
	assert.Equal(t, "example.com/lib", manifest[0].Source.Module.Path)
	assert.Equal(t, "selected", manifest[0].Source.Module.Commit)
	assert.Equal(t, "1.2", manifest[0].Declaration.RequiredAPI)
	manifest[0].Declaration.RequiredAPISpan.Start.Line = 999
	assert.NotEqual(t, 999, c.Manifest()[0].Declaration.RequiredAPISpan.Start.Line)

	writeContextPackageAt(t, dir, "1.5", "")
	err = c.CheckPackage(source)
	var newer *libraryapi.NewerMinorError
	require.ErrorAs(t, err, &newer)
	ds := diagnostic.FromError(diagnostic.Context("import cloud", err), diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "unobin.library-api.newer-minor", ds[0].Code)
	assert.Equal(t, "example.com/lib", ds[0].LibraryCompatibility.Dependency)
	assert.Equal(t, "v0.4.0", ds[0].LibraryCompatibility.Version)
	assert.Equal(t, "selected", ds[0].LibraryCompatibility.Commit)
	assert.Equal(t, "1.5", ds[0].LibraryCompatibility.RequiredAPI)
	assert.Equal(t, []string{"1.4"}, ds[0].LibraryCompatibility.ImplementedAPIs)
	assert.Equal(t, "v0.12.0", ds[0].LibraryCompatibility.UnobinVersion)
	assert.Equal(t, filepath.Join(dir, "library.go"), ds[0].Path)
	require.NotNil(t, ds[0].Span)
	assert.Empty(t, c.Manifest())
}

func TestCompatibilityContextChecksEverySelectedPackageBeforeRegistration(t *testing.T) {
	newer := writeContextPackage(t, "1.1", "")
	unsupported := writeContextPackage(t, "2.0", "")
	compatible := writeContextPackage(t, "1.0", "")
	path := filepath.Join(compatible, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	body = []byte(strings.ReplaceAll(string(body),
		`Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},`,
		`Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Resources: unreadableRegistration(),`))
	require.NoError(t, os.WriteFile(path, body, 0o644))
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	err = c.CheckPackages([]PackageSource{{Dir: newer}, {Dir: compatible}, {Dir: unsupported}})
	var major *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &major)
	var minor *libraryapi.NewerMinorError
	require.ErrorAs(t, err, &minor)
	manifest := c.Manifest()
	require.Len(t, manifest, 1)
	assert.Equal(t, compatible, manifest[0].Source.Dir)

	c, err = NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	require.NoError(t, c.CheckPackages([]PackageSource{{Dir: compatible}}))
}

func TestCompatibilityContextPreservesDeclarationFailures(t *testing.T) {
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	dir := writeContextPackage(t, "1.0", "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), []byte(
		librarySource(runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{}
}`)), 0o644))
	err = c.CheckPackage(PackageSource{Module: ModuleSource{
		Dir: dir, Dependency: "example.com/lib", Replacement: dir,
	}, Dir: dir})
	var missing *CompatibilityError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, MissingDeclaration, missing.Kind)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, dir, ds[0].LibraryCompatibility.Replacement)
	assert.Equal(t, "example.com/lib", ds[0].LibraryCompatibility.Dependency)
	assert.NotEmpty(t, ds[0].Hint)

	dir = writeContextPackage(t, "2.0", "")
	err = c.CheckPackage(PackageSource{Module: ModuleSource{Dir: dir}, Dir: dir})
	var unsupported *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &unsupported)
	assert.Equal(t, "unobin.library-api.unsupported-major",
		diagnostic.FromError(err, diagnostic.ConvertOptions{})[0].Code)
}

func TestCompatibilityContextCoreFloors(t *testing.T) {
	dir := writeContextPackage(t, "1.0", "v0.13.0")
	c, err := NewCompatibilityContext(CompatibilityOptions{UnobinVersion: "v0.12.0+dirty"})
	require.NoError(t, err)
	source := PackageSource{Module: ModuleSource{Dir: dir}, Dir: dir}
	require.NoError(t, c.CheckPackage(source), "a schema-only module does not select core")
	manifest := c.Manifest()
	require.Len(t, manifest, 1)
	assert.False(t, manifest[0].Source.Linked)
	assert.Equal(t, "1.99.0", manifest[0].MinimumGoVersion)
	assert.Equal(t, "v0.13.0", manifest[0].RequiredCoreVersion)
	source.Linked = true
	err = c.CheckPackage(source)
	var floor *CoreFloorError
	require.ErrorAs(t, err, &floor)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "unobin.library-api.core-floor", ds[0].Code)
	assert.Equal(t, "v0.13.0", ds[0].LibraryCompatibility.RequiredCoreVersion)
	assert.Equal(t, "1.99.0", ds[0].LibraryCompatibility.MinimumGoVersion)
	assert.Equal(t, filepath.Join(dir, "go.mod"), ds[0].Path)
	require.NotNil(t, ds[0].Span)

	c, err = NewCompatibilityContext(CompatibilityOptions{UnobinVersion: "v0.13.0-rc.2"})
	require.NoError(t, err)
	writeContextPackageAt(t, dir, "1.0", "v0.13.0-rc.1")
	require.NoError(t, c.CheckPackage(source))
	assert.True(t, c.Manifest()[0].Source.Linked)
}

func TestCompatibilityContextCoreReplacement(t *testing.T) {
	core := t.TempDir()
	descriptor := libraryapi.Current()
	writeCoreDescriptor(t, core, descriptor)
	dir := writeContextPackage(t, "1.0", "v1.99.0")
	c, err := NewCompatibilityContext(CompatibilityOptions{
		UnobinVersion: "dev", CoreReplacement: core,
	})
	require.NoError(t, err)
	require.NoError(t, c.CheckPackage(PackageSource{
		Module: ModuleSource{Dir: dir}, Dir: dir, Linked: true,
	}))
	_, err = NewCompatibilityContext(CompatibilityOptions{UnobinVersion: "dev"})
	var toolchain *CoreDescriptorError
	require.ErrorAs(t, err, &toolchain)
	assert.Contains(t, err.Error(), "replacement")
	descriptor.ImplementedAPIs = []string{"1.1"}
	descriptor.GeneratorAPI = "1.1"
	writeCoreDescriptor(t, core, descriptor)
	_, err = NewCompatibilityContext(CompatibilityOptions{CoreReplacement: core})
	require.ErrorAs(t, err, &toolchain)
	assert.Contains(t, err.Error(), "descriptor")
	assert.Equal(t, "unobin.library-api.core-descriptor",
		diagnostic.FromError(err, diagnostic.ConvertOptions{})[0].Code)

	missing := t.TempDir()
	_, err = NewCompatibilityContext(CompatibilityOptions{CoreReplacement: missing})
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorAs(t, err, &toolchain)
}

func TestCompatibilityContextRejectsInvalidDescriptor(t *testing.T) {
	invalid := libraryapi.Descriptor{}
	_, err := NewCompatibilityContext(CompatibilityOptions{Descriptor: &invalid})
	require.Error(t, err)
	assert.Equal(t, "unobin.library-api.core-descriptor",
		diagnostic.FromError(err, diagnostic.ConvertOptions{})[0].Code)
}

func TestCompatibilityContextToolchainPin(t *testing.T) {
	_, err := NewCompatibilityContext(CompatibilityOptions{
		UnobinVersion: "v0.12.0", ToolchainPin: "v0.13.0", ProjectFile: "project.ub",
	})
	var pin *ToolchainPinError
	require.ErrorAs(t, err, &pin)
	assert.Equal(t, "v0.13.0", pin.Pin)
	assert.Equal(t, "v0.12.0", pin.UnobinVersion)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "unobin.library-api.toolchain-pin", ds[0].Code)
	assert.Equal(t, "project.ub", ds[0].Path)
	assert.Equal(t, "v0.13.0", ds[0].LibraryCompatibility.Floor)
	assert.Equal(t, "v0.12.0", ds[0].LibraryCompatibility.UnobinVersion)

	_, err = NewCompatibilityContext(CompatibilityOptions{
		UnobinVersion: "v0.12.0", ToolchainPin: "v0.12.0",
	})
	require.NoError(t, err)

	core := t.TempDir()
	writeCoreDescriptor(t, core, libraryapi.Current())
	_, err = NewCompatibilityContext(CompatibilityOptions{
		UnobinVersion: "dev", ToolchainPin: "v0.13.0", CoreReplacement: core,
	})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(core, "pkg", "libraryapi", "descriptor.json")))
	_, err = NewCompatibilityContext(CompatibilityOptions{
		UnobinVersion: "v0.12.0", ToolchainPin: "v0.13.0", CoreReplacement: core,
	})
	var descriptor *CoreDescriptorError
	require.ErrorAs(t, err, &descriptor)
	assert.NotErrorAs(t, err, &pin)
}

func TestCompatibilityContextDiagnosticDetails(t *testing.T) {
	dir := writeContextPackage(t, "1.0", "v0.11.0")
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	context, err := NewCompatibilityContext(CompatibilityOptions{
		Descriptor: &descriptor, UnobinVersion: "v0.12.0",
	})
	require.NoError(t, err)
	require.NoError(t, context.CheckPackage(PackageSource{
		Dir: dir, Linked: true, Module: ModuleSource{
			Dir: dir, Dependency: "example.com/lib", Version: "v0.4.0", Commit: "selected",
		},
	}))
	metadata := context.Manifest()[0]
	details := context.DiagnosticDetails(metadata)
	assert.Equal(t, &diagnostic.LibraryCompatibilityDetails{
		Dependency: "example.com/lib", Package: "example.com/lib", ModulePath: "example.com/lib",
		Version: "v0.4.0", Commit: "selected", RequiredAPI: "1.0", ImplementedAPIs: []string{"1.1"},
		UnobinVersion: "v0.12.0", RequiredCoreVersion: "v0.11.0", MinimumGoVersion: "1.99.0",
	}, details)
	details.ImplementedAPIs[0] = "2.0"
	assert.Equal(t, []string{"1.1"}, context.DiagnosticDetails(metadata).ImplementedAPIs)
}

func writeContextPackage(t *testing.T, api, floor string) string {
	t.Helper()
	dir := t.TempDir()
	writeContextPackageAt(t, dir, api, floor)
	return dir
}

func writeContextPackageAt(t *testing.T, dir, api, floor string) {
	t.Helper()
	module := "module example.com/lib\n\ngo 1.99.0\n\ntoolchain go1.100.0\n"
	if floor != "" {
		module += "\nrequire github.com/cloudboss/unobin " + floor + "\n"
		module += "\nreplace github.com/cloudboss/unobin => ./unchecked-core\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
	body := fmt.Sprintf(`func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: %q},
		Resources: removedHelper[UnavailableLifecycle](),
	}
}`, api)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), []byte(
		librarySource(runtimePackage(""), body)), 0o644))
}

func writeCoreDescriptor(t *testing.T, dir string, descriptor libraryapi.Descriptor) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg", "libraryapi"), 0o755))
	source := fmt.Sprintf(`{"format-version":%d,"implemented-apis":[%q],"generator-api":%q}`,
		descriptor.FormatVersion, descriptor.ImplementedAPIs[0], descriptor.GeneratorAPI)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pkg", "libraryapi", "descriptor.json"),
		[]byte(source), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module github.com/cloudboss/unobin\n\ngo 1.26.2\n"), 0o644))
}
