package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/projectmarker"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestProjectCacheUsesNearestMarker(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	nested := filepath.Join(root, "src", "nested")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	path := filepath.Join(nested, "factory.ub")

	cache := NewProjectCache("")
	project, err := cache.ProjectForPath(path)
	require.NoError(t, err)
	require.Equal(t, root, project.Root)
	require.Equal(t, projectmarker.UB, project.Marker.Kind)
}

func TestProjectCacheInvalidatesProjectFiles(t *testing.T) {
	root := writeUBProject(t, nil, deps.NewProjectLock())
	path := filepath.Join(root, "factory.ub")
	cache := NewProjectCache("")

	first, err := cache.ProjectForPath(path)
	require.NoError(t, err)
	cache.InvalidatePath(filepath.Join(root, deps.ProjectFileName))
	second, err := cache.ProjectForPath(path)
	require.NoError(t, err)
	require.NotSame(t, first, second)

	cache.InvalidatePath(filepath.Join(root, deps.ProjectLockFileName))
	third, err := cache.ProjectForPath(path)
	require.NoError(t, err)
	require.NotSame(t, second, third)
}

func TestProjectCacheInvalidatesGoSources(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "example.com/app")
	goFile := filepath.Join(root, "library.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package app\n"), 0o644))
	cache := NewProjectCache("")

	first, err := cache.ProjectForPath(goFile)
	require.NoError(t, err)
	cache.InvalidatePath(goFile)
	second, err := cache.ProjectForPath(goFile)
	require.NoError(t, err)
	require.NotSame(t, first.GoIndex, second.GoIndex)
}

func TestImportResolverServesLocalImports(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	lib := filepath.Join(root, "lib")
	require.NoError(t, os.MkdirAll(lib, 0o755))
	cache := NewProjectCache("")
	project, err := cache.ProjectForPath(filepath.Join(root, deps.ProjectFileName))
	require.NoError(t, err)

	src, ok, err := project.Resolver.ResolveNoFetch(&resolve.LocalImport{Path: "./lib"})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, lib, src.Path)
}

func TestImportResolverServesProjectReplacements(t *testing.T) {
	dep := deps.Dependency{URL: "example.com/lib"}
	root := writeUBProject(t, nil, &deps.ProjectLock{})
	local := filepath.Join(root, "local-lib")
	require.NoError(t, os.MkdirAll(local, 0o755))
	writeGoMod(t, local, "example.com/lib")
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{dep: "./local-lib"},
	}))
	cache := NewProjectCache("")
	project, err := cache.ProjectForPath(filepath.Join(root, deps.ProjectFileName))
	require.NoError(t, err)
	ref, err := resolve.ParseImportRef("example.com/lib")
	require.NoError(t, err)

	src, ok, err := project.Resolver.ResolveNoFetch(ref)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, local, src.Path)
}

func TestImportResolverServesCachedProjectLockImports(t *testing.T) {
	cacheRoot := t.TempDir()
	cached := filepath.Join(cacheRoot, "imports", "example.com/lib", "abc123")
	require.NoError(t, os.MkdirAll(cached, 0o755))
	writeGoMod(t, cached, "example.com/lib")
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["example.com/lib"] = &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindGo, Version: "v1.0.0", Commit: "abc123",
	}
	root := writeUBProject(t, nil, lock)
	cache := newProjectCacheWithRemote("", func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: cacheRoot}, nil
	})
	project, err := cache.ProjectForPath(filepath.Join(root, deps.ProjectFileName))
	require.NoError(t, err)
	ref, err := resolve.ParseImportRef("example.com/lib")
	require.NoError(t, err)

	src, ok, err := project.Resolver.ResolveNoFetch(ref)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cached, src.Path)
	require.Equal(t, "example.com/lib", src.ModulePath)
	require.Equal(t, "example.com/lib", src.GoImportPath)
}

func TestImportResolverMissingRemoteCacheReturnsNoSource(t *testing.T) {
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["example.com/lib"] = &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindGo, Version: "v1.0.0", Commit: "missing",
	}
	root := writeUBProject(t, nil, lock)
	cache := newProjectCacheWithRemote("", func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: t.TempDir()}, nil
	})
	project, err := cache.ProjectForPath(filepath.Join(root, deps.ProjectFileName))
	require.NoError(t, err)
	ref, err := resolve.ParseImportRef("example.com/lib")
	require.NoError(t, err)

	src, ok, err := project.Resolver.ResolveNoFetch(ref)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, src)
}

func TestProjectCacheEditInvalidationDoesNotResolveRemote(t *testing.T) {
	fake := &countingCachedRemote{}
	root := writeUBProject(t, nil, nil)
	cache := newProjectCacheWithRemote("", func() (cachedRemoteSource, error) {
		return fake, nil
	})
	_, err := cache.ProjectForPath(filepath.Join(root, deps.ProjectFileName))
	require.NoError(t, err)

	cache.InvalidatePath(filepath.Join(root, "factory.ub"))
	require.Zero(t, fake.calls)
}

type countingCachedRemote struct {
	calls int
}

func (r *countingCachedRemote) CachedSource(
	ref *resolve.RemoteImport,
	commit string,
) (*resolve.Source, bool, error) {
	r.calls++
	return nil, false, nil
}

func writeUBProject(t *testing.T, project *deps.Project, lock *deps.ProjectLock) string {
	t.Helper()
	root := t.TempDir()
	if project == nil {
		project = &deps.Project{
			Requires: map[deps.Dependency]deps.Requirement{},
			Replace:  map[deps.Dependency]string{},
		}
	}
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), project))
	if lock != nil {
		if lock.Version == 0 {
			lock.Version = deps.CurrentProjectLockVersion
		}
		if lock.ToolchainVersion == "" {
			lock.ToolchainVersion = "dev"
		}
		if lock.Deps == nil {
			lock.Deps = map[string]*deps.ProjectLockDep{}
		}
		require.NoError(t, deps.WriteProjectLock(filepath.Join(root, deps.ProjectLockFileName), lock))
	}
	return root
}

func writeGoMod(t *testing.T, dir string, modulePath string) {
	t.Helper()
	body := []byte("module " + modulePath + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), body, 0o644))
}
