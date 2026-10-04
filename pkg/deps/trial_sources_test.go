package deps

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestTrialResolverPinsProjectSources(t *testing.T) {
	for _, subdir := range []string{"", "configs"} {
		t.Run(map[string]string{"": "repository", "configs": "nested project"}[subdir],
			func(t *testing.T) {
				project := ProjectID{URL: "example.com/lib", Subdir: subdir}
				modulePath := project.URL
				if subdir != "" {
					modulePath += "/" + subdir
				}
				first := t.TempDir()
				second := t.TempDir()
				for _, dir := range []string{first, second} {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(
						"module "+modulePath+"\n\ngo 1.26.2\n"), 0o644))
					require.NoError(t, os.Mkdir(filepath.Join(dir, "service"), 0o755))
				}
				require.NoError(t, os.WriteFile(filepath.Join(first, "service", "library.go"),
					[]byte("package first\n"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(second, "service", "library.go"),
					[]byte("package second\n"), 0o644))
				rootRef := &resolve.RemoteImport{
					URL: project.URL, Subdir: subdir, Version: ProjectTag(project, "v0.1.0"),
				}
				rootKey := srcKey(rootRef.URL, rootRef.Subdir, rootRef.Version)
				backend := &fakeResolver{sources: map[string]*resolve.Source{
					rootKey: {FS: os.DirFS(first), Path: first, Commit: "first-commit"},
				}}
				trial := NewTrialResolver(backend)
				root, err := trial.Resolve(rootRef)
				require.NoError(t, err)
				assert.Equal(t, "first-commit", root.Commit)
				backend.sources[rootKey] = &resolve.Source{
					FS: os.DirFS(second), Path: second, Commit: "second-commit",
				}
				packageSubdir := "service"
				if subdir != "" {
					packageSubdir = subdir + "/service"
				}
				ref := remotePackageRef(RemotePackage{URL: project.URL, Subdir: packageSubdir},
					PackageOwner{Project: project, PackageSubdir: "service"}, "v0.1.0")
				packageSource, err := resolve.ResolveImportFrom(trial, ref, root)
				require.NoError(t, err)
				assert.Equal(t, "first-commit", packageSource.Commit)
				assert.Equal(t, filepath.Join(first, "service"), packageSource.Path)
				assert.Equal(t, subdir, packageSource.ProjectSubdir)
				assert.Equal(t, packageSubdir, packageSource.PackageSubdir)
				assert.Equal(t, modulePath, packageSource.ModulePath)
				assert.Equal(t, first, packageSource.ModuleRootPath)
				assert.Equal(t, modulePath+"/service", packageSource.GoImportPath)
				body, err := fs.ReadFile(packageSource.FS, "library.go")
				require.NoError(t, err)
				assert.Equal(t, "package first\n", string(body))
				assert.Equal(t, []*resolve.RemoteImport{rootRef}, backend.refs)
				assert.Equal(t, ProjectTag(project, "v0.1.0"), ref.Version)
				_, err = trial.Resolve(remoteProjectRef(project, "v0.1.0"))
				require.NoError(t, err)
				assert.Equal(t, []*resolve.RemoteImport{rootRef}, backend.refs)
				fresh := NewTrialResolver(backend)
				_, err = fresh.Resolve(rootRef)
				require.NoError(t, err)
				packageSource, err = fresh.Resolve(ref)
				require.NoError(t, err)
				assert.Equal(t, "second-commit", packageSource.Commit)
				body, err = fs.ReadFile(packageSource.FS, "library.go")
				require.NoError(t, err)
				assert.Equal(t, "package second\n", string(body))
			})
	}
}

func TestTrialResolverDoesNotRetryAFailedCommitAtItsTag(t *testing.T) {
	backend := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("example.com/lib", "", "v0.1.0"):        goSrc("first-commit"),
		srcKey("example.com/lib", "service", "v0.1.0"): goSrc("second-commit"),
	}}
	trial := NewTrialResolver(backend)
	rootRef := remoteProjectRef(ProjectID{URL: "example.com/lib"}, "v0.1.0")
	_, err := trial.Resolve(rootRef)
	require.NoError(t, err)
	ref := remotePackageRef(RemotePackage{URL: "example.com/lib", Subdir: "service"},
		PackageOwner{Project: ProjectID{URL: "example.com/lib"}, PackageSubdir: "service"}, "v0.1.0")
	_, err = trial.Resolve(ref)
	require.Error(t, err)
	pinned := *ref
	pinned.Version = "first-commit"
	assert.Equal(t, []*resolve.RemoteImport{rootRef, &pinned}, backend.refs)
}

func TestTrialResolverPreservesLocalImportContext(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "package")
	child := filepath.Join(packageDir, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(child, "library.go"), []byte(
		"package child\n"), 0o644))
	trial := NewTrialResolver(resolve.NewLocalResolver(root))
	source, err := resolve.ResolveImportFrom(trial, &resolve.LocalImport{Path: "./child"},
		&resolve.Source{FS: os.DirFS(packageDir), Path: packageDir})
	require.NoError(t, err)
	assert.Equal(t, child, source.Path)
	assert.Empty(t, source.Commit)
}
