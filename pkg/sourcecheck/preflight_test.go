package sourcecheck

import (
	"errors"
	"maps"
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
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestImportAnalysisChecksEveryDeclarationBeforeSchemaErrors(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "UB bodies"}[nested], func(t *testing.T) {
			path := fixturePath("valid/import-analysis/factory/factory")
			resolver := newTestResolver(t, filepath.Dir(path))
			versions := map[string]string{}
			refs := map[string]resolve.ImportRef{}
			for name, api := range map[string]string{
				"schema": "1.0", "minor": "1.1", "major": "2.0",
			} {
				dir := writeImportAnalysisGoLibrary(t)
				for _, file := range []string{"go.mod", "library.go"} {
					body, err := os.ReadFile(filepath.Join(dir, file))
					require.NoError(t, err)
					body = []byte(strings.ReplaceAll(string(body), "example.com/schema",
						"example.com/"+name))
					body = []byte(strings.ReplaceAll(string(body), `"1.0"`, `"`+api+`"`))
					require.NoError(t, os.WriteFile(filepath.Join(dir, file), body, 0o644))
				}
				url := "example.com/" + name
				resolver.remotes[url] = &resolve.Source{
					FS: os.DirFS(dir), Path: dir, ModulePath: url, GoImportPath: url,
				}
				refs["z-"+name] = &resolve.RemoteImport{URL: url}
				versions[url] = "v1.0.0"
			}
			if nested {
				body := parseFactoryAt(t, path)
				imports, errs := resolve.ExtractSyntaxBodyImports(body)
				require.Empty(t, errs)
				maps.Copy(refs, imports)
			} else {
				refs["a-schema"] = refs["z-schema"]
				delete(refs, "z-schema")
			}
			calls := 0
			schemaErr := errors.New("registration is unreadable")
			_, err := AnalyzeImports(refs, ImportAnalysisOptions{
				Resolver: resolver, Versions: versions, GeneratePackages: true,
				Source: &resolve.Source{FS: os.DirFS(filepath.Dir(path)), Path: filepath.Dir(path)},
				SchemaCache: NewSchemaCacheWithReader(
					func(string) (*runtime.LibrarySchema, []string, error) {
						calls++
						return nil, nil, schemaErr
					}),
			})
			var major *libraryapi.UnsupportedMajorError
			require.ErrorAs(t, err, &major)
			var minor *libraryapi.NewerMinorError
			require.ErrorAs(t, err, &minor)
			assert.NotErrorIs(t, err, schemaErr)
			assert.Zero(t, calls)
			ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
			packages := map[string]string{}
			for _, d := range ds {
				packages[d.LibraryCompatibility.Package] = d.LibraryCompatibility.Version
			}
			assert.Equal(t, map[string]string{
				"example.com/major": "v1.0.0", "example.com/minor": "v1.0.0",
			}, packages)
		})
	}
}

func TestImportAnalysisUsesSelectedContextForLibraryAndConfigurationReads(t *testing.T) {
	path := fixturePath("valid/schema-dependencies/import-and-schema/factory")
	body := parseFactoryAt(t, path)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)
	dir := writeImportAnalysisGoLibrary(t)
	libraryPath := filepath.Join(dir, "library.go")
	source, err := os.ReadFile(libraryPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(libraryPath, []byte(strings.ReplaceAll(
		string(source), `"1.0"`, `"1.1"`)), 0o644))
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		Descriptor: &descriptor, UnobinVersion: "v0.12.0",
	})
	require.NoError(t, err)
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom"}[custom], func(t *testing.T) {
			resolver := newTestResolver(t, filepath.Dir(path))
			resolver.resolveCalls = map[string]int{}
			resolver.remotes["example.com/schema"] = &resolve.Source{
				FS: os.DirFS(dir), Path: dir, Commit: "selected-commit",
				ModulePath: "example.com/schema", GoImportPath: "example.com/schema",
			}
			cache := NewSchemaCache()
			if custom {
				cache = NewSchemaCacheWithReader(
					func(string) (*runtime.LibrarySchema, []string, error) {
						return importAnalysisSchema(), nil, nil
					})
			}
			analysis, err := AnalyzeImports(refs, ImportAnalysisOptions{
				Resolver: resolver, Versions: map[string]string{"example.com/schema": "v1.0.0"},
				SchemaCache: cache, Body: &body, Compatibility: context,
			})
			require.NoError(t, err)
			require.NotNil(t, analysis.Compatibility)
			assert.Same(t, analysis.Compatibility, cache.CompatibilityContext())
			manifest := analysis.Compatibility.Manifest()
			require.Len(t, manifest, 1)
			assert.Equal(t, "1.1", manifest[0].Declaration.RequiredAPI)
			assert.Equal(t, "example.com/schema", manifest[0].Source.Module.Dependency)
			assert.Equal(t, "v1.0.0", manifest[0].Source.Module.Version)
			assert.Equal(t, "selected-commit", manifest[0].Source.Module.Commit)
			assert.True(t, manifest[0].Source.Linked)
			assert.Equal(t, map[string]int{"example.com/schema//v1.0.0": 1}, resolver.resolveCalls)
		})
	}
}

func TestImportAnalysisIncludesOnlyLinkedForwardingModules(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "schema only", true: "linked"}[linked], func(t *testing.T) {
			dir := writeImportAnalysisGoLibrary(t)
			config := writeImportAnalysisGoLibrary(t)
			modulePath := filepath.Join(config, "go.mod")
			module, err := os.ReadFile(modulePath)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(modulePath, []byte(strings.ReplaceAll(
				string(module), "example.com/schema", "example.com/configs")), 0o644))
			libraryPath := filepath.Join(dir, "library.go")
			library, err := os.ReadFile(libraryPath)
			require.NoError(t, err)
			library = []byte(strings.ReplaceAll(string(library), "import (",
				"import (\n\tsettings \"example.com/configs\""))
			library = []byte(strings.ReplaceAll(string(library), "[*Configuration]",
				"[*settings.Configuration]"))
			library = []byte(strings.ReplaceAll(string(library),
				"return &cfg.ConfigurationType[*settings.Configuration]{\n"+
					"\t\tNew: func() *Configuration { return &Configuration{} },\n\t}",
				"return settings.LibraryConfiguration()"))
			require.NoError(t, os.WriteFile(libraryPath, library, 0o644))
			context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
				UnobinVersion: "v0.12.0", Modules: []golibrary.ModuleSource{{
					Path: "example.com/configs", Dir: config, Dependency: "example.com/configs",
					Version: "v1.2.0", Commit: "configuration-commit",
				}},
			})
			require.NoError(t, err)
			name := "valid/schema-dependencies/root/factory"
			if linked {
				name = "valid/schema-dependencies/import-and-schema/factory"
			}
			path := fixturePath(name)
			body := parseFactoryAt(t, path)
			refs, errs := resolve.ExtractSyntaxBodyImports(body)
			require.Empty(t, errs)
			resolver := newTestResolver(t, filepath.Dir(path))
			resolver.remotes["example.com/schema"] = &resolve.Source{
				FS: os.DirFS(dir), Path: dir, ModulePath: "example.com/schema",
				GoImportPath: "example.com/schema", Commit: "service-commit",
			}
			cache := NewSchemaCache()
			opts := ImportAnalysisOptions{
				Resolver: resolver, Versions: map[string]string{"example.com/schema": "v1.0.0"},
				SchemaCache: cache, Compatibility: context, Body: &body,
			}
			analysis, err := AnalyzeImports(refs, opts)
			require.NoError(t, err)
			expected := map[string]string{}
			if linked {
				expected = map[string]string{
					"example.com/schema": "v1.0.0", "example.com/configs": "v1.2.0",
				}
			}
			assert.Equal(t, expected, analysis.GoModules)
			manifest := analysis.Compatibility.Manifest()
			require.Len(t, manifest, 2)
			assert.Equal(t, "example.com/configs", manifest[0].Package)
			assert.Equal(t, "configuration-commit", manifest[0].Source.Module.Commit)
			assert.Equal(t, linked, manifest[0].Source.Linked)
			opts.Compatibility = nil
			_, err = AnalyzeImports(refs, opts)
			var unavailable *golibrary.ConfigurationSourceError
			require.ErrorAs(t, err, &unavailable)
			opts.Compatibility = context
			module, err = os.ReadFile(modulePath)
			require.NoError(t, err)
			module = append(module, []byte("\nrequire github.com/cloudboss/unobin v0.13.0\n")...)
			require.NoError(t, os.WriteFile(modulePath, module, 0o644))
			_, err = AnalyzeImports(refs, opts)
			if linked {
				var floor *golibrary.CoreFloorError
				require.ErrorAs(t, err, &floor)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestImportAnalysisKeepsPreflightMetadata(t *testing.T) {
	dir := writeImportAnalysisGoLibrary(t)
	descriptor := libraryapi.Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.1",
	}
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
		Descriptor: &descriptor,
	})
	require.NoError(t, err)
	path := fixturePath("valid/schema-dependencies/import-and-schema/factory")
	body := parseFactoryAt(t, path)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)
	resolver := newTestResolver(t, filepath.Dir(path))
	resolver.remotes["example.com/schema"] = &resolve.Source{
		FS: os.DirFS(dir), Path: dir, ModulePath: "example.com/schema",
		GoImportPath: "example.com/schema", Commit: "selected",
	}
	analysis, err := AnalyzeImports(refs, ImportAnalysisOptions{
		Resolver: resolver, Versions: map[string]string{"example.com/schema": "v1.0.0"},
		Compatibility: context, Body: &body,
	})
	require.NoError(t, err)
	require.Len(t, analysis.LibraryMetadata, 1)
	assert.Equal(t, "1.0", analysis.LibraryMetadata[0].Declaration.RequiredAPI)
	file := filepath.Join(dir, "library.go")
	source, err := os.ReadFile(file)
	require.NoError(t, err)
	source = []byte(strings.ReplaceAll(string(source), `RequiredAPI: "1.0"`, `RequiredAPI: "1.1"`))
	require.NoError(t, os.WriteFile(file, source, 0o644))
	require.NoError(t, analysis.Compatibility.CheckDirectory(dir, true))
	assert.Equal(t, "1.1", analysis.Compatibility.Manifest()[0].Declaration.RequiredAPI)
	assert.Equal(t, "1.0", analysis.LibraryMetadata[0].Declaration.RequiredAPI)
}

func TestImportAnalysisResolvesSelectedConfigurationModule(t *testing.T) {
	service := writeImportAnalysisGoLibrary(t)
	config := writeImportAnalysisGoLibrary(t)
	entry := filepath.Join(config, "entry")
	require.NoError(t, os.Mkdir(entry, 0o755))
	require.NoError(t, os.Rename(filepath.Join(config, "library.go"),
		filepath.Join(entry, "library.go")))
	moduleFile := filepath.Join(config, "go.mod")
	module, err := os.ReadFile(moduleFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(moduleFile, []byte(strings.ReplaceAll(string(module),
		"example.com/schema", "example.com/configs")), 0o644))
	path := filepath.Join(service, "library.go")
	library, err := os.ReadFile(path)
	require.NoError(t, err)
	library = []byte(strings.ReplaceAll(string(library), "import (",
		"import (\n\tsettings \"example.com/configs/entry\""))
	library = []byte(strings.ReplaceAll(string(library), "[*Configuration]",
		"[*settings.Configuration]"))
	library = []byte(strings.ReplaceAll(string(library),
		"return &cfg.ConfigurationType[*settings.Configuration]{\n"+
			"\t\tNew: func() *Configuration { return &Configuration{} },\n\t}",
		"return settings.LibraryConfiguration()"))
	require.NoError(t, os.WriteFile(path, library, 0o644))
	factory := fixturePath("valid/schema-dependencies/import-and-schema/factory")
	body := parseFactoryAt(t, factory)
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	require.Empty(t, errs)
	resolver := newTestResolver(t, filepath.Dir(factory))
	resolver.resolveCalls = map[string]int{}
	resolver.remotes["example.com/schema"] = &resolve.Source{
		FS: os.DirFS(service), Path: service, ModulePath: "example.com/schema",
		GoImportPath: "example.com/schema", Commit: "service-commit",
	}
	resolver.remotes["example.com/configs"] = &resolve.Source{
		FS: os.DirFS(config), Path: config, ModulePath: "example.com/configs",
		GoImportPath: "example.com/configs", Commit: "configuration-commit",
	}
	for _, test := range []struct {
		name       string
		mode       Mode
		uncached   bool
		required   string
		unselected bool
	}{
		{name: "fetch", mode: ModeFetch, required: "1.0"},
		{name: "cached", mode: ModeNoFetch, required: "1.0"},
		{name: "uncached configuration", mode: ModeNoFetch, uncached: true, required: "1.0"},
		{name: "incompatible cached service", mode: ModeNoFetch, uncached: true, required: "2.0"},
		{name: "unselected configuration", mode: ModeNoFetch, required: "1.0", unselected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := strings.ReplaceAll(string(library), `RequiredAPI: "1.0"`,
				`RequiredAPI: "`+test.required+`"`)
			require.NoError(t, os.WriteFile(path, []byte(current), 0o644))
			resolver.noFetchMissing["example.com/configs"] = test.uncached
			resolver.resolveCalls = map[string]int{}
			versions := map[string]string{
				"example.com/schema": "v1.0.0", "example.com/configs": "v1.2.0",
				"example.com/unused": "v1.0.0",
			}
			if test.unselected {
				delete(versions, "example.com/configs")
			}
			reads := []string{}
			var schemas *SchemaCache
			if test.uncached {
				schemas = NewSchemaCacheWithReader(func(dir string) (*runtime.LibrarySchema,
					[]string, error,
				) {
					reads = append(reads, dir)
					return nil, nil, errors.New("schema reader must not run")
				})
			}
			analysis, err := AnalyzeImports(refs, ImportAnalysisOptions{
				Resolver: resolver, Body: &body, Mode: test.mode, Versions: versions,
				SchemaCache: schemas,
			})
			if test.required == "2.0" {
				var unsupported *libraryapi.UnsupportedMajorError
				require.ErrorAs(t, err, &unsupported)
				assert.Empty(t, reads)
			} else if test.unselected {
				var unavailable *golibrary.ConfigurationSourceError
				require.ErrorAs(t, err, &unavailable)
				assert.Contains(t, err.Error(), "no selected source")
			} else if test.uncached {
				require.NoError(t, err)
				assert.Empty(t, reads)
				require.Contains(t, analysis.Libraries, "std")
				assert.Nil(t, analysis.Libraries["std"].Schema)
				assert.Equal(t, map[string]runtime.LibraryConfigSchema{
					"example.com/schema": {},
				}, analysis.LibraryConfigSchemas)
				_, err = CheckFactoryBody(body, Options{
					Resolver: resolver, Versions: versions, Mode: test.mode, SchemaCache: schemas,
				})
				require.NoError(t, err)
				assert.Empty(t, reads)
			} else {
				require.NoError(t, err)
				assert.Equal(t, map[string]string{
					"example.com/schema": "v1.0.0", "example.com/configs": "v1.2.0",
				}, analysis.GoModules)
				require.Len(t, analysis.LibraryMetadata, 2)
				metadata := analysis.LibraryMetadata[0]
				assert.Equal(t, "example.com/configs/entry", metadata.Package)
				assert.Equal(t, "configuration-commit", metadata.Source.Module.Commit)
				assert.Equal(t, "example.com/configs", metadata.Source.Module.Dependency)
				assert.True(t, metadata.Source.Linked)
			}
			calls := map[string]int{"example.com/schema//v1.0.0": 1,
				"example.com/configs//v1.2.0": 1}
			if test.mode == ModeNoFetch {
				calls = map[string]int{}
			}
			assert.Equal(t, calls, resolver.resolveCalls)
		})
	}
}
