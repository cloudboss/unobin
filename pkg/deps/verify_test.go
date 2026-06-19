package deps

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyMatchingHash(t *testing.T) {
	fsys := mapFS(map[string]string{
		ManifestFileName: "manifest: { requires: {} }\n",
		"library.ub":     "hello: resource { description: 'hi' }\n",
	})
	lock := NewLock()
	lock.Deps["github.com/x/y//lib"] = &LockedDep{
		Kind: LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: hashProject(t, fsys),
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "c1"): {FS: fsys},
	}}
	mismatches, err := Verify(lock, r)
	require.NoError(t, err)
	assert.Empty(t, mismatches)
}

func TestVerifyHashesUBProjectRootWhenResolverHashEmpty(t *testing.T) {
	fsys := mapFS(map[string]string{
		ManifestFileName: "manifest: { requires: {} }\n",
		"ub/helloer/library.ub": `
hello: resource {
  outputs: { message: { value: 'hi' } }
}
`,
	})
	hash := hashProject(t, fsys)
	lock := NewLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["github.com/scratch/repo"] = &LockedDep{
		Kind:    LockKindUB,
		Version: "v0.8.0",
		Commit:  "c1",
		Hash:    hash,
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "", "c1"): {Commit: "c1", FS: fsys},
	}}

	mismatches, err := Verify(lock, r)

	require.NoError(t, err)
	require.Empty(t, mismatches)
}

func TestVerifyRequiresUBProjectMarker(t *testing.T) {
	fsys := mapFS(map[string]string{
		"library.ub": "thing: resource {}\n",
	})
	hash, err := HashUBProject(fsys)
	require.NoError(t, err)
	lock := NewLock()
	lock.Deps["github.com/x/y"] = &LockedDep{
		Kind: LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: hash,
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "", "c1"): {FS: fsys},
	}}

	_, err = Verify(lock, r)

	require.Error(t, err)
	require.Contains(t, err.Error(), "expected UB project marker")
}

func TestVerifyDetectsMismatch(t *testing.T) {
	fsys := mapFS(map[string]string{
		ManifestFileName: "manifest: { requires: {} }\n",
		"library.ub":     "hello: resource { description: 'hi' }\n",
	})
	lock := NewLock()
	lock.Deps["github.com/x/y//lib"] = &LockedDep{
		Kind: LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: "sha256:wrong",
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "c1"): {FS: fsys},
	}}
	mismatches, err := Verify(lock, r)
	require.NoError(t, err)
	require.Len(t, mismatches, 1)
	assert.Contains(t, mismatches[0], "github.com/x/y//lib")
	assert.Contains(t, mismatches[0], "hash mismatch")
}

func TestVerifySkipsGoEntries(t *testing.T) {
	lock := NewLock()
	lock.Deps["github.com/x/golib"] = &LockedDep{Kind: LockKindGo, Version: "v1.0.0", Commit: "c1"}
	// An empty resolver: if Verify tried to fetch the go entry it would
	// error, so an empty result proves go entries are skipped.
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	mismatches, err := Verify(lock, r)
	require.NoError(t, err)
	assert.Empty(t, mismatches)
}

func TestVerifyResolveError(t *testing.T) {
	lock := NewLock()
	lock.Deps["github.com/x/y//lib"] = &LockedDep{
		Kind: LockKindUB, Version: "v1.0.0", Commit: "c1", Hash: "h1",
	}
	r := &fakeResolver{sources: map[string]*resolve.Source{}}
	_, err := Verify(lock, r)
	require.Error(t, err)
}
