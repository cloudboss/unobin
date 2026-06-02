package deps

import (
	"testing"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mapFS(files map[string]string) fstest.MapFS {
	m := make(fstest.MapFS, len(files))
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

// goSrc is a fetched source that classifies as a Go library (a .go file,
// no .ub files).
func goSrc(commit string) *resolve.Source {
	return &resolve.Source{Commit: commit, FS: mapFS(map[string]string{"lib.go": "package lib"})}
}

// ubSrc is a fetched UB-library source: its body files (kind-prefixed
// .ub) carry whatever imports the test needs for recursion.
func ubSrc(commit, hash string, files map[string]string) *resolve.Source {
	return &resolve.Source{Commit: commit, Hash: hash, FS: mapFS(files)}
}

func TestLockFromImportsRemoteGoLibrary(t *testing.T) {
	root := mapFS(map[string]string{
		"main.ub": "imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }\n",
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/core", "v0.1.0"): goSrc("c1"),
	}}
	sel := map[Dependency]string{{URL: "github.com/cloudboss/unobin"}: "v0.1.0"}
	lock, err := LockFromImports(root, sel, r)
	require.NoError(t, err)
	assert.Equal(t, map[string]*LockedDep{
		"github.com/cloudboss/unobin//pkg/libraries/core": {
			Kind: LockKindGo, Version: "v0.1.0", Commit: "c1",
		},
	}, lock.Deps)
}

func TestLockFromImportsRecursesThroughRemoteUB(t *testing.T) {
	root := mapFS(map[string]string{
		"main.ub": "imports: { hello: 'github.com/scratch/repo//ub/helloer' }\n",
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/helloer", "v0.1.0"): ubSrc("c2", "h2", map[string]string{
			"resource-greeting.ub": "imports: { local: 'github.com/cloudboss/unobin//pkg/libraries/local' }\n",
		}),
		srcKey("github.com/cloudboss/unobin", "pkg/libraries/local", "v0.1.0"): goSrc("c3"),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/scratch/repo"}:     "v0.1.0",
		{URL: "github.com/cloudboss/unobin"}: "v0.1.0",
	}
	lock, err := LockFromImports(root, sel, r)
	require.NoError(t, err)
	assert.Equal(t, map[string]*LockedDep{
		"github.com/scratch/repo//ub/helloer": {
			Kind: LockKindUB, Version: "v0.1.0", Commit: "c2", Hash: "h2",
		},
		"github.com/cloudboss/unobin//pkg/libraries/local": {
			Kind: LockKindGo, Version: "v0.1.0", Commit: "c3",
		},
	}, lock.Deps)
}

func TestLockFromImportsFollowsLocalWithoutLocking(t *testing.T) {
	root := mapFS(map[string]string{
		"main.ub":                      "imports: { greeter: './greeter' }\n",
		"greeter/resource-greeting.ub": "imports: { hello: 'github.com/scratch/repo//ub/helloer' }\n",
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/scratch/repo", "ub/helloer", "v0.1.0"): ubSrc("c2", "h2", map[string]string{
			"resource-greeting.ub": "outputs: { greeting: { value: 'hi' } }\n",
		}),
	}}
	sel := map[Dependency]string{{URL: "github.com/scratch/repo"}: "v0.1.0"}
	lock, err := LockFromImports(root, sel, r)
	require.NoError(t, err)
	assert.Equal(t, map[string]*LockedDep{
		"github.com/scratch/repo//ub/helloer": {
			Kind: LockKindUB, Version: "v0.1.0", Commit: "c2", Hash: "h2",
		},
	}, lock.Deps)
}

func TestLockFromImportsDedups(t *testing.T) {
	root := mapFS(map[string]string{
		"main.ub": "imports: { a: 'github.com/x/y//lib', b: 'github.com/x/y//lib' }\n",
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "v1.0.0"): goSrc("c"),
	}}
	sel := map[Dependency]string{{URL: "github.com/x/y"}: "v1.0.0"}
	lock, err := LockFromImports(root, sel, r)
	require.NoError(t, err)
	assert.Len(t, lock.Deps, 1)
}

func TestLockFromImportsUsesSelectionVersion(t *testing.T) {
	root := mapFS(map[string]string{
		"main.ub": "imports: { core: 'github.com/x/y//lib' }\n",
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "v2.0.0"): goSrc("c2"),
	}}
	sel := map[Dependency]string{{URL: "github.com/x/y"}: "v2.0.0"}
	lock, err := LockFromImports(root, sel, r)
	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", lock.Deps["github.com/x/y//lib"].Version)
}

func TestLockFromImportsRejectsRepoWithoutFloor(t *testing.T) {
	root := mapFS(map[string]string{
		"main.ub": "imports: { core: 'github.com/x/y//lib' }\n",
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/y", "lib", "v1.0.0"): goSrc("c1"),
	}}
	// Empty selection: nothing covers github.com/x/y.
	_, err := LockFromImports(root, map[Dependency]string{}, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.com/x/y")
	assert.Contains(t, err.Error(), "deps get")
}

func TestLockFromImportsDetectsCycle(t *testing.T) {
	root := mapFS(map[string]string{
		"main.ub": "imports: { a: 'github.com/x/a//lib' }\n",
	})
	r := &fakeResolver{sources: map[string]*resolve.Source{
		srcKey("github.com/x/a", "lib", "v1.0.0"): ubSrc("ca", "ha", map[string]string{
			"resource-a.ub": "imports: { b: 'github.com/x/b//lib' }\n",
		}),
		srcKey("github.com/x/b", "lib", "v1.0.0"): ubSrc("cb", "hb", map[string]string{
			"resource-b.ub": "imports: { a: 'github.com/x/a//lib' }\n",
		}),
	}}
	sel := map[Dependency]string{
		{URL: "github.com/x/a"}: "v1.0.0",
		{URL: "github.com/x/b"}: "v1.0.0",
	}
	_, err := LockFromImports(root, sel, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}
