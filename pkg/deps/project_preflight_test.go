package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func projectCompatibilitySource(t testing.TB, modulePath, api string) *resolve.Source {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "library.go"} {
		body, err := os.ReadFile(filepath.Join("testdata", "go", "compatibility", name))
		require.NoError(t, err)
		body = []byte(strings.ReplaceAll(string(body), "example.com/lib", modulePath))
		body = []byte(strings.ReplaceAll(string(body), `"1.0"`, `"`+api+`"`))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0o644))
	}
	return &resolve.Source{
		FS: os.DirFS(dir), Path: dir, ModuleRootPath: dir, ModulePath: modulePath,
		GoImportPath: modulePath, Commit: "selected-commit",
	}
}

func TestProjectLockChecksEveryDeclarationBeforeRegistration(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "library-api-preflight"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	selection := map[Dependency]string{}
	for name, api := range map[string]string{
		"registration": "1.0", "minor": "1.1", "major": "2.0",
	} {
		url := "example.com/" + name
		source := projectCompatibilitySource(t, url, api)
		if name == "registration" {
			path := filepath.Join(source.Path, "library.go")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			body = []byte(strings.ReplaceAll(string(body), "Configuration: LibraryConfiguration(),",
				""))
			require.NoError(t, os.WriteFile(path, body, 0o644))
		}
		r.sources[srcKey(url, "", "v1.0.0")] = source
		selection[Dependency{URL: url}] = "v1.0.0"
	}
	_, err := ProjectLockFromImports(root, selection, r, nil)
	var major *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &major)
	var minor *libraryapi.NewerMinorError
	require.ErrorAs(t, err, &minor)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	apis := map[string]string{}
	for _, d := range ds {
		apis[d.LibraryCompatibility.Dependency] = d.LibraryCompatibility.RequiredAPI
	}
	assert.Equal(t, map[string]string{"example.com/major": "2.0", "example.com/minor": "1.1"}, apis)
}

func TestProjectLockChecksConfigurationAndReplacementMetadata(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "replacement"}[replaced], func(t *testing.T) {
			root := mapFS(map[string]string{
				"factory.ub": projectLockWalkFixture(t, "project-lock-from-schema-dependency"),
			})
			source := projectCompatibilitySource(t, "example.com/aws", "2.0")
			r := &fakeResolver{sources: map[string]*resolve.Source{
				srcKey("example.com/aws", "config", "v0.1.0"): source,
				srcKey("example.com/aws", "config", ""):       source,
			}}
			replace := map[Dependency]string{}
			if replaced {
				replace[Dependency{URL: "example.com/aws"}] = source.Path
			}
			_, err := ProjectLockFromImports(root,
				map[Dependency]string{{URL: "example.com/aws"}: "v0.1.0"}, r, replace)
			var major *libraryapi.UnsupportedMajorError
			require.ErrorAs(t, err, &major)
			ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
			require.Len(t, ds, 1)
			assert.Equal(t, "example.com/aws", ds[0].LibraryCompatibility.Dependency)
			if replaced {
				assert.Equal(t, source.ModuleRootPath, ds[0].LibraryCompatibility.Replacement)
			}
		})
	}
}

func TestProjectLockUsesOneDescriptorForLibraryAndConfigurationValidation(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "library-api-linked-schema"),
	})
	source := projectCompatibilitySource(t, "example.com/aws", "1.1")
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		Descriptor: &descriptor, UnobinVersion: "v0.12.0",
	})
	require.NoError(t, err)
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/aws", "config", "v0.1.0"): source,
	}}
	prepared, err := PrepareProjectLock(root,
		map[Dependency]string{{URL: "example.com/aws"}: "v0.1.0"}, r, nil,
		ProjectLockOptions{Compatibility: context})
	require.NoError(t, err)
	assert.Equal(t, map[string]*ProjectLockDep{
		"example.com/aws": {Kind: ProjectLockKindGo, Version: "v0.1.0", Commit: "selected-commit"},
	}, prepared.Lock.Deps)
	manifest := prepared.Compatibility.Manifest()
	require.Len(t, manifest, 1)
	assert.Equal(t, "1.1", manifest[0].Declaration.RequiredAPI)
	assert.Equal(t, "v0.1.0", manifest[0].Source.Module.Version)
	assert.Equal(t, "selected-commit", manifest[0].Source.Module.Commit)
	assert.True(t, manifest[0].Source.Linked)
}

func TestProjectLockChecksLocalConfigurationMetadata(t *testing.T) {
	source := projectCompatibilitySource(t, "example.com/lib", "2.0")
	dir := source.ModuleRootPath
	config := filepath.Join(dir, "config")
	require.NoError(t, os.Mkdir(config, 0o755))
	require.NoError(t, os.Rename(
		filepath.Join(dir, "library.go"), filepath.Join(config, "library.go"),
	))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(
		projectLockWalkFixture(t, "library-api-local-schema")), 0o644))
	_, err := ProjectLockFromImports(os.DirFS(dir), nil, resolve.NewLocalResolver(dir), nil)
	var major *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &major)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Len(t, ds, 1)
	assert.Equal(t, "example.com/lib/config", ds[0].LibraryCompatibility.Package)
}

func TestProjectLockChecksCoreFloorWhenConfigurationIsAlsoLinked(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "schema only", true: "linked"}[linked], func(t *testing.T) {
			name := "project-lock-from-schema-dependency"
			if linked {
				name = "library-api-linked-schema"
			}
			root := mapFS(map[string]string{"factory.ub": projectLockWalkFixture(t, name)})
			source := projectCompatibilitySource(t, "example.com/aws", "1.0")
			modulePath := filepath.Join(source.Path, "go.mod")
			module, err := os.ReadFile(modulePath)
			require.NoError(t, err)
			module = append(module, []byte("\nrequire github.com/cloudboss/unobin v0.13.0\n")...)
			require.NoError(t, os.WriteFile(modulePath, module, 0o644))
			context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
				UnobinVersion: "v0.12.0",
			})
			require.NoError(t, err)
			r := &fakeResolver{sources: map[string]*resolve.Source{
				srcKey("example.com/aws", "config", "v0.1.0"): source,
			}}
			prepared, err := PrepareProjectLock(root,
				map[Dependency]string{{URL: "example.com/aws"}: "v0.1.0"}, r, nil,
				ProjectLockOptions{Compatibility: context})
			if linked {
				var floor *golibrary.CoreFloorError
				require.ErrorAs(t, err, &floor)
			} else {
				require.NoError(t, err)
				manifest := prepared.Compatibility.Manifest()
				require.Len(t, manifest, 1)
				assert.False(t, manifest[0].Source.Linked)
				assert.Equal(t, "v0.13.0", manifest[0].RequiredCoreVersion)
			}
		})
	}
}

func TestProjectLockDoesNotHideMalformedMetadataAfterDiscoveryErrors(t *testing.T) {
	root := mapFS(map[string]string{
		"factory.ub": projectLockWalkFixture(t, "library-api-preflight"),
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/minor", "", "v1.0.0"): projectCompatibilitySource(
			t, "example.com/minor", "invalid",
		),
		srcKey("example.com/major", "", "v1.0.0"): projectCompatibilitySource(
			t, "example.com/major", "2.0",
		),
	}}
	_, err := ProjectLockFromImports(root, map[Dependency]string{
		{URL: "example.com/minor"}: "v1.0.0", {URL: "example.com/major"}: "v1.0.0",
	}, r, nil)
	assert.Contains(t, err.Error(), "no owning project version")
	var malformed *golibrary.CompatibilityError
	require.ErrorAs(t, err, &malformed)
	assert.Equal(t, golibrary.InvalidDeclaration, malformed.Kind)
	var major *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &major)
}
