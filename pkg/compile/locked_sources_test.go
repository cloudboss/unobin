package compile

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestProjectLockUBSourcesUseCacheAndVerifyHash(t *testing.T) {
	fsys := fstest.MapFS{
		deps.ProjectFileName: &fstest.MapFile{Data: []byte(ubtest.ReadValidFixture(
			t, "testdata/ub/wrap-replaces", "empty-project"))},
	}
	hash, err := deps.HashUBProject(fsys)
	require.NoError(t, err)
	backend := &lockedSourceResolver{
		source: &resolve.Source{FS: fsys, Commit: "locked"}, cached: true,
	}
	lock := deps.NewProjectLock()
	lock.Deps["example.com/repo"] = &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindUB, Version: "v1.0.0", Commit: "locked", Hash: hash,
	}
	resolver := WrapProjectLockSources(backend, lock)
	ref := &resolve.RemoteImport{URL: "example.com/repo", Version: "v1.0.0"}
	_, err = resolver.Resolve(ref)
	require.NoError(t, err)
	assert.Empty(t, backend.calls)
	lock.Deps["example.com/repo"].Hash = "changed"
	_, err = resolver.Resolve(ref)
	require.ErrorContains(t, err, "hash mismatch")
}

func TestProjectLockGoSourcesUseCommit(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "fetch", true: "cache"}[cached], func(t *testing.T) {
			source := &resolve.Source{Commit: "locked", ModulePath: "example.com/repo/lib/v2"}
			backend := &lockedSourceResolver{source: source, cached: cached}
			lock := deps.NewProjectLock()
			lock.Deps["example.com/repo//lib"] = &deps.ProjectLockDep{
				Kind: deps.ProjectLockKindGo, Version: "v2.0.0-rc.1", Commit: "locked",
			}
			ref := &resolve.RemoteImport{
				URL: "example.com/repo", Subdir: "lib/service", Version: "v2.0.0-rc.1",
			}
			got, err := WrapProjectLockSources(backend, lock).Resolve(ref)
			require.NoError(t, err)
			assert.Same(t, source, got)
			assert.Equal(t, "v2.0.0-rc.1", ref.Version)
			assert.Equal(t, "v2.0.0-rc.1", lock.Deps["example.com/repo//lib"].Version)
			if cached {
				assert.Empty(t, backend.calls)
			} else {
				require.Len(t, backend.calls, 1)
				assert.Equal(t, "locked", backend.calls[0].Version)
				assert.Equal(t, "lib", backend.calls[0].ProjectSubdir)
				assert.Equal(t, "lib/service", backend.calls[0].PackageSubdir)
			}
			assert.Equal(t, []string{"locked"}, backend.cacheCommits)
		})
	}
}

func TestProjectLockGoSourceNeverFallsBackFromMissingCommit(t *testing.T) {
	unavailable := errors.New("commit unavailable")
	backend := &lockedSourceResolver{err: unavailable}
	lock := deps.NewProjectLock()
	lock.Deps["example.com/repo"] = &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindGo, Version: "v1.0.0", Commit: "locked",
	}
	_, err := WrapProjectLockSources(backend, lock).Resolve(&resolve.RemoteImport{
		URL: "example.com/repo", Version: "v1.0.0",
	})
	require.ErrorIs(t, err, unavailable)
	require.Len(t, backend.calls, 1)
	assert.Equal(t, "locked", backend.calls[0].Version)
}

func TestProjectLockGoSourceReportsCacheFailure(t *testing.T) {
	cacheFailure := errors.New("cache unreadable")
	backend := &lockedSourceResolver{cacheErr: cacheFailure}
	lock := deps.NewProjectLock()
	lock.Deps["example.com/repo"] = &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindGo, Version: "v1.0.0", Commit: "locked",
	}
	_, err := WrapProjectLockSources(backend, lock).Resolve(&resolve.RemoteImport{
		URL: "example.com/repo", Version: "v1.0.0",
	})
	require.ErrorIs(t, err, cacheFailure)
	assert.Empty(t, backend.calls)
}

type lockedSourceResolver struct {
	source       *resolve.Source
	cached       bool
	err          error
	cacheErr     error
	calls        []resolve.RemoteImport
	cacheCommits []string
}

func (r *lockedSourceResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	ri := ref.(*resolve.RemoteImport)
	r.calls = append(r.calls, *ri)
	return r.source, r.err
}

func (r *lockedSourceResolver) CachedSource(
	_ *resolve.RemoteImport, commit string,
) (*resolve.Source, bool, error) {
	r.cacheCommits = append(r.cacheCommits, commit)
	return r.source, r.cached, r.cacheErr
}
