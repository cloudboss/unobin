package golibrary

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

const inlineConfiguration = `&cfg.ConfigurationType[any]{
	New: func() any { return &Configuration{} },
}`

func TestCompatibilityContextFollowsConfigurationPackages(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0",
		"settings.LibraryConfiguration()", "",
		`import settings "example.com/lib/config"`)
	writeForwardingPackage(t, root, "config", "1.0", "LibraryConfiguration()",
		"next.LibraryConfiguration()", `import next "example.com/lib/config2"`)
	writeForwardingPackage(t, root, "config2", "1.0", "LibraryConfiguration()",
		inlineConfiguration, "")
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	t.Setenv("PATH", "")
	require.NoError(t, c.CheckPackage(PackageSource{Module: ModuleSource{
		Dir: root, Dependency: "example.com/lib", Version: "v0.2.0", Commit: "selected",
	}, Dir: service, Linked: true}))
	manifest := c.Manifest()
	packages := make([]string, 0, len(manifest))
	for _, entry := range manifest {
		packages = append(packages, entry.Package)
		assert.True(t, entry.Source.Linked)
		assert.Equal(t, "selected", entry.Source.Module.Commit)
	}
	assert.Equal(t, []string{"example.com/lib/config", "example.com/lib/config2",
		"example.com/lib/service"}, packages)
}

func TestCompatibilityContextRejectsForwardedRequirement(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0",
		"settings.LibraryConfiguration()", "",
		`import settings "example.com/lib/config"`)
	config := writeForwardingPackage(t, root, "config", "1.1", "LibraryConfiguration()",
		inlineConfiguration, "")
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	err = c.CheckPackage(PackageSource{Module: ModuleSource{Dir: root}, Dir: service, Linked: true})
	var newer *libraryapi.NewerMinorError
	require.ErrorAs(t, err, &newer)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "example.com/lib/config", ds[0].LibraryCompatibility.Package)
	assert.Equal(t, filepath.Join(config, "library.go"), ds[0].Path)

	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	c, err = NewCompatibilityContext(CompatibilityOptions{Descriptor: &descriptor})
	require.NoError(t, err)
	require.NoError(t, c.CheckPackage(PackageSource{Module: ModuleSource{Dir: root}, Dir: service}))
}

func TestCompatibilityContextRequiresSelectedConfigurationRoot(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0",
		"settings.LibraryConfiguration()", "",
		`import settings "example.com/configs/settings"`)
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	source := PackageSource{Module: ModuleSource{Dir: root}, Dir: service, Linked: true}
	err = c.CheckPackage(source)
	var unavailable *ConfigurationSourceError
	require.ErrorAs(t, err, &unavailable)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "unobin.library-api.configuration-source", ds[0].Code)
	assert.Equal(t, filepath.Join(service, "library.go"), ds[0].Path)
	require.NotNil(t, ds[0].Span)
	assert.Contains(t, ds[0].Hint, "selected source")

	configRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configRoot, "go.mod"), []byte(
		"module example.com/configs\n\ngo 1.26.2\n"), 0o644))
	writeForwardingPackage(t, configRoot, "settings", "1.0", "LibraryConfiguration()",
		inlineConfiguration, "")
	c, err = NewCompatibilityContext(CompatibilityOptions{Modules: []ModuleSource{{
		Path: "example.com/configs", Dir: configRoot, Dependency: "example.com/configs",
		Version: "v0.3.0", Commit: "config-commit",
	}}})
	require.NoError(t, err)
	require.NoError(t, c.CheckPackage(source))
	manifest := c.Manifest()
	require.Len(t, manifest, 2)
	assert.Equal(t, "example.com/configs/settings", manifest[0].Package)
	assert.Equal(t, "config-commit", manifest[0].Source.Module.Commit)
	assert.Equal(t, "v0.3.0", manifest[0].Source.Module.Version)
	assert.True(t, manifest[0].Source.Linked)
}

func TestCompatibilityContextForwardedModuleCoreFloor(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0",
		"settings.LibraryConfiguration()", "",
		`import settings "example.com/configs/settings"`)
	configRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configRoot, "go.mod"), []byte(
		"module example.com/configs\n\ngo 1.26.2\n"+
			"\nrequire github.com/cloudboss/unobin v0.13.0\n"), 0o644))
	writeForwardingPackage(t, configRoot, "settings", "1.0", "LibraryConfiguration()",
		inlineConfiguration, "")
	c, err := NewCompatibilityContext(CompatibilityOptions{
		UnobinVersion: "v0.12.0", Modules: []ModuleSource{{
			Path: "example.com/configs", Dir: configRoot,
		}},
	})
	require.NoError(t, err)
	source := PackageSource{Module: ModuleSource{Dir: root}, Dir: service}
	require.NoError(t, c.CheckPackage(source))
	for _, entry := range c.Manifest() {
		assert.False(t, entry.Source.Linked)
	}
	source.Linked = true
	err = c.CheckPackage(source)
	var floor *CoreFloorError
	require.ErrorAs(t, err, &floor)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "example.com/configs/settings", ds[0].LibraryCompatibility.Package)
}

func TestCompatibilityContextRejectsConfigurationCyclesAndMissingFunctions(t *testing.T) {
	tests := []struct {
		name, entry, imports, message string
	}{
		{"cycle", "other.LibraryConfiguration()", `import other "example.com/lib/service"`,
			"cycle"},
		{"missing entry point", "", "", "LibraryConfiguration"},
		{"unreadable entry point", "computedConfiguration()", "", "unreadable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeContextPackage(t, "1.0", "")
			service := writeForwardingPackage(t, root, "service", "1.0",
				"settings.LibraryConfiguration()", "settings.LibraryConfiguration()",
				`import settings "example.com/lib/config"`)
			writeForwardingPackage(t, root, "config", "1.0", "LibraryConfiguration()",
				tt.entry, tt.imports)
			c, err := NewCompatibilityContext(CompatibilityOptions{})
			require.NoError(t, err)
			err = c.CheckPackage(PackageSource{Module: ModuleSource{Dir: root}, Dir: service})
			var configuration *ConfigurationSourceError
			require.ErrorAs(t, err, &configuration)
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}

func TestCompatibilityContextDoesNotInspectHelperImports(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0", inlineConfiguration, "",
		`import helper "example.com/lib/helper"`)
	writeForwardingPackage(t, root, "helper", "2.0", inlineConfiguration, "", "")
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	require.NoError(t, c.CheckPackage(PackageSource{Module: ModuleSource{Dir: root}, Dir: service}))
	manifest := c.Manifest()
	require.Len(t, manifest, 1)
	assert.Equal(t, "example.com/lib/service", manifest[0].Package)
}

func TestCompatibilityContextUsesConfigurationImportFromDeclaringFile(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0",
		"settings.LibraryConfiguration()", "", "")
	writeForwardingPackage(t, root, "config", "1.0", "LibraryConfiguration()",
		inlineConfiguration, "")
	require.NoError(t, os.WriteFile(filepath.Join(service, "other.go"), []byte(
		librarySource(`import settings "example.com/lib/config"`, "")), 0o644))
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	err = c.CheckPackage(PackageSource{Module: ModuleSource{Dir: root}, Dir: service})
	var unavailable *ConfigurationSourceError
	require.ErrorAs(t, err, &unavailable)
}

func TestCompatibilityContextReadsSingleLibraryReturnInLoop(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	body := `func Library() *runtime.Library {
	for {
		return &runtime.Library{
			Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		}
	}
}`
	require.NoError(t, os.WriteFile(filepath.Join(root, "library.go"), []byte(
		librarySource(runtimePackage(""), body)), 0o644))
	c, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	require.NoError(t, c.CheckPackage(PackageSource{Module: ModuleSource{Dir: root}, Dir: root}))
}

func TestCompatibilityDeclarationUsesParsedSourceSnapshot(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	pkg, err := parsePackage(root)
	require.NoError(t, err)
	writeContextPackageAt(t, root, "1.1", "")
	declaration, err := readCompatibilityPackage(pkg)
	require.NoError(t, err)
	assert.Equal(t, "1.0", declaration.RequiredAPI)
	current, err := ReadCompatibility(root, root)
	require.NoError(t, err)
	assert.Equal(t, "1.1", current.RequiredAPI)
}

func TestCompatibilityContextRejectsMissingAndUnselectedNestedConfiguration(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing directory", true: "nested module"}[nested],
			func(t *testing.T) {
				root := writeContextPackage(t, "1.0", "")
				service := writeForwardingPackage(t, root, "service", "1.0",
					"settings.LibraryConfiguration()", "",
					`import settings "example.com/lib/config"`)
				if nested {
					config := writeForwardingPackage(t, root, "config", "1.0",
						"LibraryConfiguration()", inlineConfiguration, "")
					require.NoError(t, os.WriteFile(filepath.Join(config, "go.mod"), []byte(
						"module example.com/lib/config\n\ngo 1.26.2\n"), 0o644))
				}
				c, err := NewCompatibilityContext(CompatibilityOptions{})
				require.NoError(t, err)
				err = c.CheckPackage(PackageSource{Module: ModuleSource{Dir: root}, Dir: service})
				var unavailable *ConfigurationSourceError
				require.ErrorAs(t, err, &unavailable)
				if nested {
					assert.Contains(t, err.Error(), "unselected nested module")
				} else {
					assert.ErrorIs(t, err, os.ErrNotExist)
				}
			})
	}
}

func writeForwardingPackage(
	t *testing.T, root, relative, api, configuration, entry, imports string,
) string {
	t.Helper()
	dir := filepath.Join(root, relative)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := fmt.Sprintf(`func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: %q},
		Configuration: %s,
	}
}
type Configuration struct{}
`, api, configuration)
	if entry != "" {
		body += fmt.Sprintf(`func LibraryConfiguration() *cfg.ConfigurationType[any] {
	return %s
}
`, entry)
	}
	importDecl := runtimePackage("") + "\n" +
		`import "github.com/cloudboss/unobin/pkg/sdk/cfg"` + "\n" + imports
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), []byte(
		librarySource(importDecl, body)), 0o644))
	return dir
}
