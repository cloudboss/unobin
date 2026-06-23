package deps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestVerifyHashesUBProjectRootWhenResolverHashEmpty(t *testing.T) {
	fsys := mapFS(map[string]string{
		ProjectFileName: ubtest.ReadValidFixture(
			t, "testdata/ub/verify", "empty-project"),
		"ub/helloer/library.ub": ubtest.ReadValidFixture(
			t, "testdata/ub/verify", "helloer-library"),
	})
	hash := hashProject(t, fsys)
	projectLock := NewProjectLock()
	projectLock.ToolchainVersion = "dev"
	projectLock.Deps["github.com/scratch/repo"] = &ProjectLockDep{
		Kind:    ProjectLockKindUB,
		Version: "v0.8.0",
		Commit:  "c1",
		Hash:    hash,
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "", "c1"): {Commit: "c1", FS: fsys},
	}}

	mismatches, err := Verify(projectLock, r)

	require.NoError(t, err)
	require.Empty(t, mismatches)
}

func TestVerifySkipsGoEntries(t *testing.T) {
	projectLock := NewProjectLock()
	projectLock.Deps["github.com/x/golib"] = &ProjectLockDep{Kind: ProjectLockKindGo, Version: "v1.0.0", Commit: "c1"}
	// An empty resolver: if Verify tried to fetch the go entry it would
	// error, so an empty result proves go entries are skipped.
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	mismatches, err := Verify(projectLock, r)
	require.NoError(t, err)
	assert.Empty(t, mismatches)
}

func TestVerifyResolveError(t *testing.T) {
	projectLock := NewProjectLock()
	projectLock.Deps["github.com/x/y//lib"] = &ProjectLockDep{
		Kind: ProjectLockKindUB, Version: "v1.0.0", Commit: "c1", Hash: "h1",
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	_, err := Verify(projectLock, r)
	require.Error(t, err)
}
