package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestDefinitionInputName(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "input.region", "region"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"region: { type", "region")
}

func TestDefinitionLocalName(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "local.name", "name"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"name: input.region", "name")
}

func TestDefinitionResourceReference(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "resource.server }", "server"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"server: bundle.web", "server")
}

func TestDefinitionDataSourceAndActionReferences(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "data-source.lookup", "lookup"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"lookup: bundle.lookup", "lookup")

	locations, rpcErr = DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "action.deploy", "deploy"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"deploy: bundle.deploy", "deploy")
}

func TestDefinitionUBCompositeSelector(t *testing.T) {
	root, factoryPath, factorySource, libraryPath := definitionProject(t)
	cache := NewProjectCache(root)
	librarySource := ubtest.ReadFixture(t, libraryPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "bundle.web", "web"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, libraryPath, librarySource,
		"web: resource", "web")
}

func TestDefinitionUBCompositeInputField(t *testing.T) {
	root, factoryPath, factorySource, libraryPath := definitionProject(t)
	cache := NewProjectCache(root)
	librarySource := ubtest.ReadFixture(t, libraryPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "name: local.name", "name"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, libraryPath, librarySource,
		"name: { type: string }", "name")
}

func TestDefinitionCachedUBCompositePackageInputField(t *testing.T) {
	_, factoryPath, factorySource, libraryPath, cache := cachedUBCompositePackageProject(t)
	librarySource := readTestFile(t, libraryPath)
	tests := []struct {
		contextText string
		input       string
	}{
		{contextText: "message: 'heyo'", input: "message"},
		{contextText: "path: '/the/path/to/nowhere'", input: "path"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			locations, rpcErr := DefinitionForText(factoryPath, factorySource,
				positionInText(factorySource, tt.contextText, tt.input), cache)
			require.Nil(t, rpcErr)
			requireDefinitionLocation(t, locations, libraryPath, librarySource,
				tt.input+": { type: string }", tt.input)
		})
	}
}

func TestDefinitionUBCompositeOutputReference(t *testing.T) {
	root, factoryPath, factorySource, libraryPath := definitionProject(t)
	cache := NewProjectCache(root)
	librarySource := ubtest.ReadFixture(t, libraryPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "resource.server.id", "id"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, libraryPath, librarySource,
		"id: { value: 'web-id' }", "id")
}

func TestDefinitionUBImportAliasWithOneExport(t *testing.T) {
	root, factoryPath, factorySource, libraryPath := singleExportImportProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, libraryPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "single: './single'", "single"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, libraryPath, librarySource,
		"web: resource", "web")
}

func TestDefinitionUBImportAliasWithMultipleExportsReturnsNoLocation(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "bundle: './bundle'", "bundle"), cache)
	require.Nil(t, rpcErr)
	require.Empty(t, locations)
}

func TestDefinitionGoImportAlias(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))
	start := offsetInText(factorySource, "def: 'example.com/definition'", "def")

	for i := range len("def") {
		locations, rpcErr := DefinitionForText(factoryPath, factorySource,
			OffsetToLSP(factorySource, start+i), cache)
		require.Nil(t, rpcErr)
		requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
			"func Library()", "Library")
	}
}

func TestDefinitionLibraryConfigTypeConstructor(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "type: library-config", "library-config"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
		"type Config struct", "Config")
}

func TestDefinitionLibraryConfigSchemaDependency(t *testing.T) {
	root, factoryPath, factorySource, moduleDir := configForwardDefinitionProject(t)
	cache := NewProjectCache(root)
	configPath := filepath.Join(moduleDir, "awscfg", "configuration.go")
	configSource := readTestFile(t, configPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "type: library-config", "library-config"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, configPath, configSource,
		"type Configuration struct", "Configuration")
}

func TestDefinitionLibraryConfigSchemaDependencyPathLiteral(t *testing.T) {
	root, factoryPath, factorySource, moduleDir := configForwardDefinitionProject(t)
	cache := NewProjectCache(root)
	configPath := filepath.Join(moduleDir, "awscfg", "configuration.go")
	configSource := readTestFile(t, configPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "example.com/aws//config", "config"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, configPath, configSource,
		"type Configuration struct", "Configuration")
}

func TestDefinitionLibraryConfigSchemaDependencyDefaultField(t *testing.T) {
	root, factoryPath, factorySource, moduleDir := configForwardDefinitionProject(t)
	cache := NewProjectCache(root)
	configPath := filepath.Join(moduleDir, "awscfg", "configuration.go")
	configSource := readTestFile(t, configPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "region: 'us-east-1'", "region"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, configPath, configSource,
		"Region string", "Region")
}

func TestDefinitionLibraryConfigAlias(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))
	start := offsetInText(factorySource, "def: input.definition-config", "def")

	for i := range len("def") {
		locations, rpcErr := DefinitionForText(factoryPath, factorySource,
			OffsetToLSP(factorySource, start+i), cache)
		require.Nil(t, rpcErr)
		requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
			"type Config struct", "Config")
	}
}

func TestDefinitionGoNodeSelector(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "def.server", "server"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
		"\"server\": runtime.MakeResource", "\"server\"")
}

func TestDefinitionGoUsesProjectLockCacheBeforeReplacement(t *testing.T) {
	root, factoryPath, factorySource, cacheRoot := cachedGoDefinitionProject(t)
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: cacheRoot}, nil
	})
	cachedLibrary := filepath.Join(
		cacheRoot, "imports", "example.com/definition", "abc123", "library.go",
	)
	librarySource := readTestFile(t, cachedLibrary)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "def.server", "server"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, cachedLibrary, librarySource,
		"\"server\": runtime.MakeResource", "\"server\"")
}

func TestDefinitionCachedUBCompositeGoSelector(t *testing.T) {
	libraryPath, librarySource, cachedLibrary, cache := cachedUBCompositeWithImportProject(t)
	goSource := readTestFile(t, cachedLibrary)

	locations, rpcErr := DefinitionForText(libraryPath, librarySource,
		positionInText(librarySource, "def.server", "server"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, cachedLibrary, goSource,
		"\"server\": runtime.MakeResource", "\"server\"")
}

func TestDefinitionGoNodeBodyFields(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))
	sharedSource := readTestFile(t, filepath.Join(goDir, "shared", "shared.go"))

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "server-name: 'web'", "server-name"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
		"Name     string", "Name")

	locations, rpcErr = DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "url: 'https://example.com'", "url"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "shared", "shared.go"),
		sharedSource, "URL  string", "URL")
}

func TestDefinitionGoNodeBodyFieldWorksInsideKey(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))
	pos := positionInText(factorySource, "server-name: 'web'", "name")

	locations, rpcErr := DefinitionForText(factoryPath, factorySource, pos, cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
		"Name     string", "Name")
}

func TestDefinitionGoNestedOutputReference(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProjectFixture(
		t, "testdata/ub/definition/valid/go-backed-nested-output.ub",
	)
	cache := NewProjectCache(root)
	sharedSource := readTestFile(t, filepath.Join(goDir, "shared", "shared.go"))

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "resource.server.endpoint.url", "url"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "shared", "shared.go"),
		sharedSource, "URL  string", "URL")
}

func TestDefinitionGoInputOutputCollisionPrefersOutputForRefs(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "id: 'input-id'", "id"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
		"ID       string", "ID")

	locations, rpcErr = DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "resource.server.id", "id"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
		"type ServerOutput struct {\n\tID", "ID")
}

func TestDefinitionGoLibraryConfigField(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	sharedSource := readTestFile(t, filepath.Join(goDir, "shared", "shared.go"))

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "count: 3", "count"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "shared", "shared.go"),
		sharedSource, "Count int", "Count")
}

func TestDefinitionGoFunctionCall(t *testing.T) {
	root, factoryPath, factorySource, goDir := goDefinitionProject(t)
	cache := NewProjectCache(root)
	librarySource := readTestFile(t, filepath.Join(goDir, "library.go"))

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "def.slug('v1')", "slug"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, filepath.Join(goDir, "library.go"), librarySource,
		"func makeSlug", "makeSlug")
}

func TestDefinitionFunctionWithMissingCachedSourceReturnsNoLocations(t *testing.T) {
	_, factoryPath, factorySource, cache := missingCachedGoDefinitionProject(t)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "def.slug('v1')", "slug"), cache)
	require.Nil(t, rpcErr)
	require.Empty(t, locations)
}

func TestDefinitionInvalidSourceReturnsNoLocations(t *testing.T) {
	root, path, source := inputDeclarationCompletionProject(t)
	source, pos := inputDeclarationSourceWithPrefix(t, source, "h")

	locations, rpcErr := DefinitionForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.Empty(t, locations)
}

func TestSessionDefinitionReturnsLocations(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	session := NewSession("dev")
	session.projects = NewProjectCache(root)
	uri := PathToFileURI(factoryPath)
	rpcErr := openDocument(t, session, uri, 1, factorySource)
	require.Nil(t, rpcErr)

	result, rpcErr := requestDefinition(t, session, uri,
		positionInText(factorySource, "input.region", "region"))
	require.Nil(t, rpcErr)
	locations, ok := result.([]protocol.Location)
	require.True(t, ok)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"region: { type", "region")
}

func definitionProject(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{},
	}))
	factorySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/factory.ub")
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	bundleDir := filepath.Join(root, "bundle")
	require.NoError(t, os.MkdirAll(bundleDir, 0o755))
	librarySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/bundle/library.ub")
	libraryPath := filepath.Join(bundleDir, "library.ub")
	require.NoError(t, os.WriteFile(libraryPath, []byte(librarySource), 0o644))
	return root, factoryPath, factorySource, libraryPath
}

func singleExportImportProject(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := writeUBProject(t, nil, nil)
	factorySource := ubtest.ReadFixture(
		t, "testdata/ub/definition/valid/single-import-factory.ub",
	)
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	singleDir := filepath.Join(root, "single")
	require.NoError(t, os.MkdirAll(singleDir, 0o755))
	librarySource := ubtest.ReadFixture(
		t, "testdata/ub/definition/valid/single/library.ub",
	)
	libraryPath := filepath.Join(singleDir, "library.ub")
	require.NoError(t, os.WriteFile(libraryPath, []byte(librarySource), 0o644))
	return root, factoryPath, factorySource, libraryPath
}

func goDefinitionProject(t *testing.T) (string, string, string, string) {
	t.Helper()
	return goDefinitionProjectFixture(
		t, "testdata/ub/definition/valid/go-backed-factory.ub",
	)
}

func configForwardDefinitionProject(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	moduleDir, err := filepath.Abs(filepath.Join("..", "goschema", "testdata", "configforward"))
	require.NoError(t, err)
	dep := deps.Dependency{URL: "example.com/aws"}
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{dep: moduleDir},
	}))
	factorySource := ubtest.ReadFixture(
		t, "testdata/ub/definition/valid/schema-config-factory.ub",
	)
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	return root, factoryPath, factorySource, moduleDir
}

func goDefinitionProjectFixture(
	t *testing.T,
	fixture string,
) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	goDir, err := filepath.Abs(filepath.Join("..", "goschema", "testdata", "definition"))
	require.NoError(t, err)
	dep := deps.Dependency{URL: "example.com/definition"}
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{dep: goDir},
	}))
	factorySource := ubtest.ReadFixture(t, fixture)
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	return root, factoryPath, factorySource, goDir
}

func cachedGoDefinitionProject(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	fixtureDir := filepath.Join("..", "goschema", "testdata", "definition")
	localDir := filepath.Join(root, "local-definition")
	copyTestTree(t, fixtureDir, localDir)
	cacheRoot := filepath.Join(root, "cache")
	cachedDir := filepath.Join(
		cacheRoot, "imports", "example.com/definition", "abc123",
	)
	copyTestTree(t, fixtureDir, cachedDir)

	dep := deps.Dependency{URL: "example.com/definition"}
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{dep: localDir},
	}))
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps[dep.String()] = &deps.ProjectLockDep{
		Kind:    deps.ProjectLockKindGo,
		Version: "v1.0.0",
		Commit:  "abc123",
	}
	require.NoError(t, deps.WriteProjectLock(filepath.Join(root, deps.ProjectLockFileName), lock))

	factorySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/go-backed-factory.ub")
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	return root, factoryPath, factorySource, cacheRoot
}

func cachedUBCompositePackageProject(
	t *testing.T,
) (string, string, string, string, *ProjectCache) {
	t.Helper()
	root := t.TempDir()
	dep := deps.Dependency{URL: "example.com/lib"}
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{},
	}))
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps[dep.String()] = &deps.ProjectLockDep{
		Kind:    deps.ProjectLockKindUB,
		Version: "v1.0.0",
		Commit:  "abc123",
		Hash:    "sha256:test",
	}
	require.NoError(t, deps.WriteProjectLock(filepath.Join(root, deps.ProjectLockFileName), lock))

	factorySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/cached-package-factory.ub")
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	cacheRoot := filepath.Join(root, "cache")
	libraryDir := filepath.Join(cacheRoot, "imports", "example.com/lib", "abc123", "ub", "helloer")
	require.NoError(t, os.MkdirAll(libraryDir, 0o755))
	librarySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/cached-package-library.ub")
	libraryPath := filepath.Join(libraryDir, "library.ub")
	require.NoError(t, os.WriteFile(libraryPath, []byte(librarySource), 0o644))
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: cacheRoot}, nil
	})
	return root, factoryPath, factorySource, libraryPath, cache
}

func cachedUBCompositeWithImportProject(
	t *testing.T,
) (string, string, string, *ProjectCache) {
	t.Helper()
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "cache")
	libRoot := filepath.Join(cacheRoot, "imports", "example.com/lib", "abc123")
	goRoot := filepath.Join(cacheRoot, "imports", "example.com/definition", "def123")
	require.NoError(t, os.MkdirAll(libRoot, 0o755))
	copyTestTree(t, filepath.Join("..", "goschema", "testdata", "definition"), goRoot)

	dep := deps.Dependency{URL: "example.com/definition"}
	require.NoError(t, deps.WriteProject(filepath.Join(libRoot, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{
			dep: {Version: "v1.0.0"},
		},
		Replace: map[deps.Dependency]string{},
	}))
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps[dep.String()] = &deps.ProjectLockDep{
		Kind:    deps.ProjectLockKindGo,
		Version: "v1.0.0",
		Commit:  "def123",
	}
	require.NoError(t, deps.WriteProjectLock(filepath.Join(libRoot, deps.ProjectLockFileName), lock))

	libraryDir := filepath.Join(libRoot, "ub", "helloer")
	require.NoError(t, os.MkdirAll(libraryDir, 0o755))
	librarySource := ubtest.ReadFixture(
		t, "testdata/ub/definition/valid/cached-package-import-library.ub",
	)
	libraryPath := filepath.Join(libraryDir, "resource-hello.ub")
	require.NoError(t, os.WriteFile(libraryPath, []byte(librarySource), 0o644))
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: cacheRoot}, nil
	})
	return libraryPath, librarySource, filepath.Join(goRoot, "library.go"), cache
}

func missingCachedGoDefinitionProject(t *testing.T) (string, string, string, *ProjectCache) {
	t.Helper()
	root := t.TempDir()
	dep := deps.Dependency{URL: "example.com/definition"}
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{},
	}))
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps[dep.String()] = &deps.ProjectLockDep{
		Kind:    deps.ProjectLockKindGo,
		Version: "v1.0.0",
		Commit:  "missing",
	}
	require.NoError(t, deps.WriteProjectLock(filepath.Join(root, deps.ProjectLockFileName), lock))
	factorySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/go-backed-factory.ub")
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: filepath.Join(root, "cache")}, nil
	})
	return root, factoryPath, factorySource, cache
}

func copyTestTree(t *testing.T, src string, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dst, 0o755))
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyTestTree(t, srcPath, dstPath)
			continue
		}
		info, err := entry.Info()
		require.NoError(t, err)
		body, err := os.ReadFile(srcPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(dstPath, body, info.Mode()))
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func requestDefinition(
	t *testing.T,
	session *Session,
	uri string,
	pos protocol.Position,
) (any, *protocol.ResponseError) {
	t.Helper()
	params := protocol.DefinitionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/definition", Params: body,
	})
}

func positionInText(text string, contextText string, target string) protocol.Position {
	return OffsetToLSP(text, offsetInText(text, contextText, target))
}

func offsetInText(text string, contextText string, target string) int {
	contextOffset := strings.Index(text, contextText)
	if contextOffset < 0 {
		return 0
	}
	targetOffset := strings.Index(contextText, target)
	if targetOffset < 0 {
		return 0
	}
	return contextOffset + targetOffset
}

func requireDefinitionLocation(
	t *testing.T,
	locations []protocol.Location,
	path string,
	text string,
	contextText string,
	target string,
) {
	t.Helper()
	require.Len(t, locations, 1)
	require.Equal(t, PathToFileURI(path), locations[0].URI)
	contextOffset := strings.Index(text, contextText)
	require.NotEqual(t, -1, contextOffset)
	targetOffset := strings.Index(contextText, target)
	require.NotEqual(t, -1, targetOffset)
	start := contextOffset + targetOffset
	require.Equal(t, protocol.Range{
		Start: OffsetToLSP(text, start),
		End:   OffsetToLSP(text, start+len(target)),
	}, locations[0].Range)
}
