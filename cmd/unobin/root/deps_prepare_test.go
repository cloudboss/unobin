package root

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type changingTrialResolver struct {
	first  *resolve.Source
	second *resolve.Source
	calls  int
}

func (r *changingTrialResolver) Resolve(resolve.ImportRef) (*resolve.Source, error) {
	r.calls++
	if r.calls == 1 {
		return r.first, nil
	}
	return r.second, nil
}

func TestPrepareDependenciesLeavesFilesUntouched(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "existing"}[existing], func(t *testing.T) {
			root := t.TempDir()
			factory := ubtest.ReadValidFixture(t, "testdata/ub/library-compatibility", "factory")
			require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
			dep := deps.Dependency{URL: "example.com/lib"}
			other := deps.Dependency{URL: "example.com/unused"}
			require.NoError(t, os.Mkdir(filepath.Join(root, "unused"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "unused", "go.mod"), []byte(
				"module example.com/unused\n\ngo 1.26.2\n"), 0o644))
			project := &deps.Project{
				Requires: map[deps.Dependency]deps.Requirement{dep: {Version: "v0.1.0"}},
				Replace:  map[deps.Dependency]string{other: "./unused"},
			}
			projectPath, lockPath := filepath.Join(root, deps.ProjectFileName),
				filepath.Join(root, deps.ProjectLockFileName)
			var beforeProject, beforeLock []byte
			if existing {
				_, err := deps.WriteProjectChange(projectPath, project)
				require.NoError(t, err)
				old := deps.NewProjectLock()
				old.ToolchainVersion = "v0.1.0"
				old.Deps[dep.String()] = &deps.ProjectLockDep{
					Kind: deps.ProjectLockKindGo, Version: "v0.0.1", Commit: "old-commit",
				}
				_, err = deps.WriteProjectLockChange(lockPath, old)
				require.NoError(t, err)
				beforeProject, err = os.ReadFile(projectPath)
				require.NoError(t, err)
				beforeLock, err = os.ReadFile(lockPath)
				require.NoError(t, err)
			}
			library := t.TempDir()
			require.NoError(t, os.CopyFS(library,
				os.DirFS("../../../pkg/deps/testdata/go/compatibility")))
			source := &resolve.Source{FS: os.DirFS(library), Path: library, Commit: "selected-commit"}
			stubCompileResolver(t, map[string]*resolve.Source{
				remoteSourceKey(dep.URL, "", "v0.1.0"): source,
			})
			prepared, err := prepareDependencies(root, project, "", io.Discard, nil)
			require.NoError(t, err)
			assert.Equal(t, project, prepared.Project)
			assert.NotSame(t, project, prepared.Project)
			assert.Equal(t, map[deps.Dependency]string{dep: "v0.1.0"}, prepared.Selection)
			assert.Equal(t, map[string]*deps.ProjectLockDep{
				dep.String(): {
					Kind: deps.ProjectLockKindGo, Version: "v0.1.0", Commit: "selected-commit",
				},
			}, prepared.Lock.Deps)
			assert.Equal(t, "v0.1.0", prepared.Lock.ToolchainVersion)
			require.Len(t, prepared.Libraries, 1)
			assert.Equal(t, dep.String(), prepared.Libraries[0].Package)
			assert.Equal(t, "selected-commit", prepared.Libraries[0].Source.Module.Commit)
			assert.Equal(t, "1.0", prepared.Libraries[0].Declaration.RequiredAPI)
			assert.Empty(t, prepared.Diagnostics)
			if existing {
				after, err := os.ReadFile(projectPath)
				require.NoError(t, err)
				assert.Equal(t, beforeProject, after)
				after, err = os.ReadFile(lockPath)
				require.NoError(t, err)
				assert.Equal(t, beforeLock, after)
			} else {
				require.NoFileExists(t, projectPath)
				require.NoFileExists(t, lockPath)
			}
			prepared.Project.SetRequire(dep, "v0.2.0", true)
			prepared.Project.Replace[other] = "./changed"
			assert.Equal(t, deps.Requirement{Version: "v0.1.0"}, project.Requires[dep])
			assert.Equal(t, "./unused", project.Replace[other])
		})
	}
}

func TestGetDoesNotAnnounceRejectedSelection(t *testing.T) {
	root := t.TempDir()
	factory := ubtest.ReadValidFixture(t, "testdata/ub/library-compatibility", "factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
	library := t.TempDir()
	require.NoError(t, os.CopyFS(library,
		os.DirFS("../../../pkg/deps/testdata/go/compatibility")))
	path := filepath.Join(library, "library.go")
	code, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(code),
		`RequiredAPI: "1.0"`, `RequiredAPI: "2.0"`)), 0o644))
	stubCompileResolver(t, map[string]*resolve.Source{
		remoteSourceKey("example.com/lib", "", "v0.1.0"): {
			FS: os.DirFS(library), Path: library, Commit: "selected-commit",
		},
	})
	t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
		return []string{"v0.1.0"}, nil
	}))
	announcements := []string{}
	_, err = getDependency(&depsSyncConfig{stackPath: root}, "example.com/lib@v0.1.0", io.Discard,
		func(dep deps.Dependency, version string) {
			announcements = append(announcements, dep.String()+"@"+version)
		})
	var unsupported *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, err, &unsupported)
	assert.Empty(t, announcements)
	require.NoFileExists(t, filepath.Join(root, deps.ProjectFileName))
	require.NoFileExists(t, filepath.Join(root, deps.ProjectLockFileName))
}

func TestGetKeepsTheFirstProjectCommitThroughPreparation(t *testing.T) {
	root := t.TempDir()
	factory := ubtest.ReadValidFixture(t, "testdata/ub/library-compatibility", "factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		require.NoError(t, os.CopyFS(dir,
			os.DirFS("../../../pkg/deps/testdata/go/compatibility")))
	}
	path := filepath.Join(second, "library.go")
	code, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(code),
		`RequiredAPI: "1.0"`, `RequiredAPI: "2.0"`)), 0o644))
	backend := &changingTrialResolver{
		first:  &resolve.Source{FS: os.DirFS(first), Path: first, Commit: "first-commit"},
		second: &resolve.Source{FS: os.DirFS(second), Path: second, Commit: "second-commit"},
	}
	previous := newCompileResolver
	newCompileResolver = func(string) (resolve.Resolver, error) { return backend, nil }
	t.Cleanup(func() { newCompileResolver = previous })
	t.Cleanup(SetDepsListTagsForTest(func(string) ([]string, error) {
		return []string{"v0.1.0"}, nil
	}))
	operation, err := getDependency(&depsSyncConfig{stackPath: root},
		"example.com/lib@v0.1.0", io.Discard, nil)
	require.NoError(t, err)
	assert.Equal(t, "v0.1.0", operation.Version)
	assert.Equal(t, 1, backend.calls)
	lock, err := deps.ReadProjectLock(os.DirFS(root))
	require.NoError(t, err)
	assert.Equal(t, &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindGo, Version: "v0.1.0", Commit: "first-commit",
	}, lock.Deps["example.com/lib"])
}
