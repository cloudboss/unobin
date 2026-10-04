package compile

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

func TestCheckSelectedLibraryModules(t *testing.T) {
	cases := []struct {
		name          string
		version       string
		api           string
		hint          bool
		missing       bool
		schemaOnly    bool
		replaced      bool
		actualReplace bool
		sameDirectory bool
		remoteReplace bool
		coreFloor     bool
		missingRecord bool
		code          string
	}{
		{name: "same selected version", version: "v0.1.0", api: "1.0"},
		{name: "higher version with the same API", version: "v0.2.0", api: "1.0",
			code: "module-selection"},
		{name: "different authored API under the same tag", version: "v0.1.0", api: "1.1",
			code: "module-source"},
		{name: "different publisher hint under the same tag", version: "v0.1.0", api: "1.0",
			hint: true, code: "module-source"},
		{name: "missing linked module", missing: true, code: "module-selection"},
		{name: "absent schema-only module", schemaOnly: true, missing: true},
		{name: "changed schema-only module", schemaOnly: true, version: "v0.2.0", api: "2.0"},
		{name: "matching local replacement", version: "v0.2.0", api: "1.0",
			replaced: true, actualReplace: true, sameDirectory: true},
		{name: "different local replacement", version: "v0.1.0", api: "1.0",
			replaced: true, actualReplace: true, code: "module-selection"},
		{name: "unexpected local replacement", version: "v0.1.0", api: "1.0",
			actualReplace: true, code: "module-selection"},
		{name: "missing expected replacement", version: "v0.1.0", api: "1.0",
			replaced: true, code: "module-selection"},
		{name: "replacement by another release", version: "v0.1.0", api: "1.0",
			remoteReplace: true, code: "module-selection"},
		{name: "actual core floor", version: "v0.1.0", api: "1.0", coreFloor: true,
			code: "core-floor"},
		{name: "missing actual record", version: "v0.1.0", missingRecord: true,
			code: "module-source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selected := t.TempDir()
			require.NoError(t, os.CopyFS(selected, os.DirFS("../deps/testdata/go/compatibility")))
			descriptor := libraryapi.Descriptor{
				FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
			}
			context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
				UnobinVersion: "v0.12.0", Descriptor: &descriptor,
			})
			require.NoError(t, err)
			source := golibrary.PackageSource{
				Dir: selected, Linked: !tc.schemaOnly,
				Module: golibrary.ModuleSource{
					Dir: selected, Dependency: "example.com/lib", Version: "v0.1.0", Commit: "locked",
				},
			}
			if tc.replaced {
				source.Module.Replacement = selected
			}
			require.NoError(t, context.CheckPackage(source))
			manifest := context.Manifest()
			actual := t.TempDir()
			require.NoError(t, os.CopyFS(actual, os.DirFS(selected)))
			if tc.sameDirectory {
				actual = selected
			}
			path := filepath.Join(actual, "library.go")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			body = []byte(strings.ReplaceAll(string(body), `RequiredAPI: "1.0"`,
				`RequiredAPI: "`+tc.api+`"`))
			if tc.hint {
				body = []byte(strings.ReplaceAll(string(body), `RequiredAPI: "1.0"`,
					`RequiredAPI: "1.0", SuggestedUnobinVersion: "v0.13.0"`))
			}
			if tc.missingRecord {
				body = []byte("package library\n")
			}
			require.NoError(t, os.WriteFile(path, body, 0o644))
			if tc.coreFloor {
				require.NoError(t, os.WriteFile(filepath.Join(actual, "go.mod"), []byte(
					"module example.com/lib\n\ngo 1.26.2\nrequire github.com/cloudboss/unobin v0.13.0\n"),
					0o644))
			}
			module := selectedLibraryModule{Path: "example.com/lib", Version: tc.version, Dir: actual}
			if tc.actualReplace {
				module.Replace = &selectedLibraryModule{Path: actual, Dir: actual}
			}
			if tc.remoteReplace {
				module.Replace = &selectedLibraryModule{
					Path: "example.com/fork", Version: "v0.9.0", Dir: actual,
				}
			}
			modules := []selectedLibraryModule{module}
			if tc.missing {
				modules = nil
			}
			err = checkSelectedLibraryModules(context, manifest, modules)
			if tc.code == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
			require.Len(t, ds, 1)
			assert.Equal(t, "unobin.library-api."+tc.code, ds[0].Code)
			details := ds[0].LibraryCompatibility
			require.NotNil(t, details)
			assert.Equal(t, "example.com/lib", details.Dependency)
			assert.Equal(t, "locked", details.Commit)
			assert.Equal(t, "v0.1.0", details.Version)
			if tc.code == "module-source" {
				assert.Equal(t, tc.api, details.ActualRequiredAPI)
				assert.Contains(t, ds[0].Hint, "immutable")
			}
			if tc.code == "module-selection" {
				assert.Contains(t, ds[0].Hint, "deps sync")
				assert.Equal(t, tc.version, details.ActualVersion)
			}
		})
	}
}

func TestVerifySelectedLibrariesReadsActualGoModules(t *testing.T) {
	goBin, err := exec.LookPath("go")
	require.NoError(t, err)
	main := t.TempDir()
	library := t.TempDir()
	require.NoError(t, os.CopyFS(library, os.DirFS("../deps/testdata/go/compatibility")))
	mod := "module example.com/factory\n\ngo 1.26.2\n\n" +
		"require example.com/lib v0.1.0\nreplace example.com/lib => " + library + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(main, "go.mod"), []byte(mod), 0o644))
	modules, err := readSelectedLibraryModules(goBin, main)
	require.NoError(t, err)
	require.Equal(t, []selectedLibraryModule{
		{Path: "example.com/factory", Dir: main},
		{Path: "example.com/lib", Version: "v0.1.0", Dir: library,
			Replace: &selectedLibraryModule{Path: library, Dir: library}},
	}, modules)
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{})
	require.NoError(t, err)
	require.NoError(t, context.CheckPackage(golibrary.PackageSource{
		Dir: library, Linked: true, Module: golibrary.ModuleSource{
			Dir: library, Version: "v0.1.0", Replacement: library,
		},
	}))
	require.NoError(t, verifySelectedLibraries(goBin, main, context, context.Manifest()))
	manifest := context.Manifest()
	manifest[0].Source.Module.Replacement = t.TempDir()
	err = verifySelectedLibraries(goBin, main, context, manifest)
	var selection *libraryModuleError
	require.ErrorAs(t, err, &selection)

	manifest[0].Source.Linked = false
	require.NoError(t, verifySelectedLibraries("unavailable-go", main, context, manifest))
	_, err = readSelectedLibraryModules("unavailable-go", main)
	require.ErrorIs(t, err, exec.ErrNotFound)
}

func TestGoBuildRejectsChangedMetadataAfterTidy(t *testing.T) {
	main := t.TempDir()
	library := t.TempDir()
	require.NoError(t, os.CopyFS(library, os.DirFS("../deps/testdata/go/compatibility")))
	core, err := filepath.Abs("../..")
	require.NoError(t, err)
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		UnobinVersion: "dev", CoreReplacement: core,
	})
	require.NoError(t, err)
	require.NoError(t, context.CheckPackage(golibrary.PackageSource{
		Dir: library, Linked: true, Module: golibrary.ModuleSource{
			Dir: library, Version: "v0.1.0", Replacement: library,
		},
	}))
	mod := "module example.com/factory\n\ngo 1.26.2\n\nrequire (\n" +
		" example.com/lib v0.1.0\n github.com/cloudboss/unobin " + replacedVersion + "\n)\n" +
		"replace example.com/lib => " + library + "\n" +
		"replace github.com/cloudboss/unobin => " + core + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(main, "go.mod"), []byte(mod), 0o644))
	source := "package main\nimport lib \"example.com/lib\"\n" +
		"func main() { lib.Library(); missingBuildSymbol() }\n"
	require.NoError(t, os.WriteFile(filepath.Join(main, "main.go"), []byte(source), 0o644))
	path := filepath.Join(library, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	body = []byte(strings.ReplaceAll(string(body), `RequiredAPI: "1.0"`, `RequiredAPI: "1.1"`))
	require.NoError(t, os.WriteFile(path, body, 0o644))
	result, err := runGoBuild(io.Discard, io.Discard, &diagnostic.Collector{},
		main, "factory", "v0.0.0", replacedVersion, context, context.Manifest())
	var mismatch *libraryModuleError
	require.ErrorAs(t, err, &mismatch)
	require.Empty(t, result.ContentRevision)
	require.Contains(t, result.Files, filechange.Change{
		Path: filepath.Join(main, "go.sum"), Action: filechange.ActionCreated,
	})
	require.NoFileExists(t, filepath.Join(main, "factory"))
}

func TestCheckSelectedLibraryModulesDoesNotResolveOutsideBuild(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS("../deps/testdata/go/compatibility")))
	lookups := 0
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		ResolveModule: func(string) (golibrary.ModuleSource, error) {
			lookups++
			return golibrary.ModuleSource{}, nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, context.CheckPackage(golibrary.PackageSource{
		Dir: dir, Linked: true, Module: golibrary.ModuleSource{
			Dir: dir, Dependency: "example.com/lib", Version: "v0.1.0", Commit: "locked",
		},
	}))
	manifest := context.Manifest()
	source := "package library\nimport (\n" +
		"\"github.com/cloudboss/unobin/pkg/runtime\"\n" +
		"settings \"example.com/configs/entry\"\n)\n" +
		"func Library() *runtime.Library { return &runtime.Library{\n" +
		"Compatibility: runtime.LibraryCompatibility{RequiredAPI: \"1.0\"},\n" +
		"Configuration: settings.LibraryConfiguration(),\n} }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), []byte(source), 0o644))
	err = checkSelectedLibraryModules(context, manifest, []selectedLibraryModule{
		{Path: "example.com/lib", Dir: dir, Version: "v0.1.0"},
	})
	var unavailable *golibrary.ConfigurationSourceError
	require.ErrorAs(t, err, &unavailable)
	assert.Zero(t, lookups)
}

func TestCheckSelectedLibraryModulesKeepsConfigurationProvenance(t *testing.T) {
	service := t.TempDir()
	config := t.TempDir()
	for _, dir := range []string{service, config} {
		require.NoError(t, os.CopyFS(dir, os.DirFS("../deps/testdata/go/compatibility")))
	}
	require.NoError(t, os.WriteFile(filepath.Join(config, "go.mod"), []byte(
		"module example.com/configs\n\ngo 1.26.2\n"), 0o644))
	source := "package library\nimport (\n" +
		"\"github.com/cloudboss/unobin/pkg/runtime\"\n" +
		"settings \"example.com/configs\"\n)\n" +
		"func Library() *runtime.Library { return &runtime.Library{\n" +
		"Compatibility: runtime.LibraryCompatibility{RequiredAPI: \"1.0\"},\n" +
		"Configuration: settings.LibraryConfiguration(),\n} }\n"
	require.NoError(t, os.WriteFile(filepath.Join(service, "library.go"), []byte(source), 0o644))
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		UnobinVersion: "v0.12.0", Modules: []golibrary.ModuleSource{{
			Path: "example.com/configs", Dir: config, Dependency: "example.com/configs",
			Version: "v0.1.0", Commit: "configuration-commit",
		}},
	})
	require.NoError(t, err)
	require.NoError(t, context.CheckPackage(golibrary.PackageSource{
		Dir: service, Linked: true, Module: golibrary.ModuleSource{
			Dir: service, Dependency: "example.com/lib", Version: "v0.1.0", Commit: "service-commit",
		},
	}))
	manifest := context.Manifest()
	require.NoError(t, os.WriteFile(filepath.Join(config, "go.mod"), []byte(
		"module example.com/configs\n\ngo 1.26.2\n"+
			"require github.com/cloudboss/unobin v0.13.0\n"), 0o644))
	err = checkSelectedLibraryModules(context, manifest, []selectedLibraryModule{
		{Path: "example.com/lib", Dir: service, Version: "v0.1.0"},
		{Path: "example.com/configs", Dir: config, Version: "v0.1.0"},
	})
	var floor *golibrary.CoreFloorError
	require.ErrorAs(t, err, &floor)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "example.com/configs", ds[0].LibraryCompatibility.Dependency)
	assert.Equal(t, "configuration-commit", ds[0].LibraryCompatibility.Commit)
	assert.Equal(t, "v0.13.0", ds[0].LibraryCompatibility.RequiredCoreVersion)
}
