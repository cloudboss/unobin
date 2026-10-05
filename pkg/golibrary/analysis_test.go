package golibrary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/libraryapi"
)

func TestCompatibilityAnalysisReusesCurrentMetadata(t *testing.T) {
	dir := writeContextPackage(t, "1.0", "")
	base, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	c := base.ForAnalysis()
	require.NoError(t, c.CheckDirectory(dir, true))
	before, err := c.SourceSnapshot(dir)
	require.NoError(t, err)
	writeContextPackageAt(t, dir, "2.0", "")
	require.NoError(t, c.CheckDirectory(dir, true))
	again, err := c.SourceSnapshot(dir)
	require.NoError(t, err)
	require.Equal(t, before, again)
	require.Equal(t, "1.0", c.Manifest()[0].Declaration.RequiredAPI)
	var unsupported *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, c.ValidateSources(), &unsupported)
	require.Empty(t, base.Manifest())
	require.ErrorAs(t, base.CheckDirectory(dir, true), &unsupported)
	require.ErrorAs(t, base.ForAnalysis().CheckDirectory(dir, true), &unsupported)
	c.EndAnalysis()
	require.ErrorAs(t, c.CheckDirectory(dir, true), &unsupported)
}

func TestCompatibilityAnalysisRejectsReachableSourceChanges(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0",
		"settings.LibraryConfiguration()", "", `import settings "example.com/lib/config"`)
	config := writeForwardingPackage(t, root, "config", "1.0", "LibraryConfiguration()",
		inlineConfiguration, "")
	base, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	c := base.ForAnalysis()
	require.NoError(t, c.CheckDirectory(service, true))
	var packages []string
	for _, metadata := range c.Manifest() {
		packages = append(packages, metadata.Package)
	}
	require.Equal(t, []string{"example.com/lib/config", "example.com/lib/service"}, packages)
	require.NoError(t, c.ValidateSources())
	path := filepath.Join(config, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o644))
	require.ErrorContains(t, c.ValidateSources(), "source changed")
}

func TestCompatibilityAnalysisRetainsLinkedCoreFloor(t *testing.T) {
	dir := writeContextPackage(t, "1.0", "v0.13.0")
	base, err := NewCompatibilityContext(CompatibilityOptions{UnobinVersion: "v0.12.0"})
	require.NoError(t, err)
	c := base.ForAnalysis()
	require.NoError(t, c.CheckDirectory(dir, false))
	var floor *CoreFloorError
	require.ErrorAs(t, c.CheckDirectory(dir, true), &floor)
	require.NoError(t, c.CheckDirectory(dir, false))
}

func TestCompatibilityAnalysisRejectsChangedBuildContext(t *testing.T) {
	dir := writeContextPackage(t, "1.0", "")
	base, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	c := base.ForAnalysis()
	require.NoError(t, c.CheckDirectory(dir, true))
	t.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -tags=analysis_changed"))
	require.ErrorContains(t, c.ValidateSources(), "source changed")
}

func TestCompatibilityAnalysisRetainsSharedConfigurationErrors(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	first := writeForwardingPackage(t, root, "first", "1.0",
		"settings.LibraryConfiguration()", "", `import settings "example.com/lib/config"`)
	second := writeForwardingPackage(t, root, "second", "1.0",
		"settings.LibraryConfiguration()", "", `import settings "example.com/lib/config"`)
	writeForwardingPackage(t, root, "config", "2.0", "LibraryConfiguration()",
		inlineConfiguration, "")
	base, err := NewCompatibilityContext(CompatibilityOptions{})
	require.NoError(t, err)
	c := base.ForAnalysis()
	var unsupported *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, c.CheckDirectory(first, true), &unsupported)
	require.ErrorAs(t, c.CheckDirectory(second, true), &unsupported)
}

func TestCompatibilityAnalysisIncludesSelectedConfigurationInSnapshots(t *testing.T) {
	root := writeContextPackage(t, "1.0", "")
	service := writeForwardingPackage(t, root, "service", "1.0",
		"settings.LibraryConfiguration()", "", `import settings "example.com/configs/settings"`)
	configRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configRoot, "go.mod"), []byte(
		"module example.com/configs\n\ngo 1.26.2\n"), 0o644))
	config := writeForwardingPackage(t, configRoot, "settings", "1.0",
		"LibraryConfiguration()", inlineConfiguration, "")
	calls := 0
	base, err := NewCompatibilityContext(CompatibilityOptions{
		ResolveModule: func(path string) (ModuleSource, error) {
			require.Equal(t, "example.com/configs/settings", path)
			calls++
			return ModuleSource{Path: "example.com/configs", Dir: configRoot}, nil
		},
	})
	require.NoError(t, err)
	c := base.ForAnalysis()
	require.NoError(t, c.CheckDirectory(service, true))
	before, err := c.SourceSnapshot(service)
	require.NoError(t, err)
	expected, err := SourceSnapshot(service, c.ModuleSources())
	require.NoError(t, err)
	require.Equal(t, expected, before)
	require.NoError(t, c.CheckDirectory(service, true))
	require.Equal(t, 1, calls)
	require.NoError(t, c.ValidateSources())
	path := filepath.Join(config, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o644))
	require.ErrorContains(t, c.ValidateSources(), "source changed")
	next := base.ForAnalysis()
	require.NoError(t, next.CheckDirectory(service, true))
	after, err := next.SourceSnapshot(service)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
	require.Equal(t, 2, calls)
}
