package deps

import (
	"fmt"
	"testing"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFetcher serves canned results keyed by "<dep-id>@<version>" and
// records every call so tests can assert what was fetched.
type fakeFetcher struct {
	results map[string]Resolved
	calls   []string
}

func (f *fakeFetcher) Fetch(dep Dependency, version string) (Resolved, error) {
	key := dep.String() + "@" + version
	f.calls = append(f.calls, key)
	res, ok := f.results[key]
	if !ok {
		return Resolved{}, fmt.Errorf("no fake result for %s", key)
	}
	return res, nil
}

func dep(id string) Dependency {
	d, err := ParseDependency(id)
	if err != nil {
		panic(err)
	}
	return d
}

func ubResult(commit, hash string, reqs map[Dependency]string) Resolved {
	return Resolved{
		Source:   &resolve.Source{Commit: commit, Hash: hash},
		Manifest: &Manifest{Requires: reqs},
		Kind:     LockKindUB,
	}
}

func goResult(commit string) Resolved {
	return Resolved{Source: &resolve.Source{Commit: commit}, Kind: LockKindGo}
}

func TestResolveSingleDependency(t *testing.T) {
	a := dep("github.com/x/a")
	f := &fakeFetcher{results: map[string]Resolved{
		"github.com/x/a@v1.0.0": ubResult("ca", "ha", nil),
	}}
	got, err := Resolve(&Manifest{Requires: map[Dependency]string{a: "v1.0.0"}}, f)
	require.NoError(t, err)
	assert.Equal(t, map[string]*LockedDep{
		"github.com/x/a": {Kind: LockKindUB, Version: "v1.0.0", Commit: "ca", Hash: "ha"},
	}, got.Lock.Deps)
}

func TestResolveTransitive(t *testing.T) {
	a, b := dep("github.com/x/a"), dep("github.com/x/b")
	f := &fakeFetcher{results: map[string]Resolved{
		"github.com/x/a@v1.0.0": ubResult("ca", "ha", map[Dependency]string{b: "v1.0.0"}),
		"github.com/x/b@v1.0.0": ubResult("cb", "hb", nil),
	}}
	got, err := Resolve(&Manifest{Requires: map[Dependency]string{a: "v1.0.0"}}, f)
	require.NoError(t, err)
	assert.Len(t, got.Lock.Deps, 2)
	assert.Equal(t, "v1.0.0", got.Lock.Deps["github.com/x/b"].Version)
}

func TestResolveMinimalVersionSelection(t *testing.T) {
	a, c, b := dep("github.com/x/a"), dep("github.com/x/c"), dep("github.com/x/b")
	f := &fakeFetcher{results: map[string]Resolved{
		"github.com/x/a@v1.0.0": ubResult("ca", "ha", map[Dependency]string{b: "v1.0.0"}),
		"github.com/x/c@v1.0.0": ubResult("cc", "hc", map[Dependency]string{b: "v2.0.0"}),
		"github.com/x/b@v1.0.0": ubResult("cb1", "hb1", nil),
		"github.com/x/b@v2.0.0": ubResult("cb2", "hb2", nil),
	}}
	root := &Manifest{Requires: map[Dependency]string{a: "v1.0.0", c: "v1.0.0"}}
	got, err := Resolve(root, f)
	require.NoError(t, err)
	// The higher floor wins, and B is recorded at the version it was last
	// fetched at, with that fetch's commit and hash.
	assert.Equal(t, &LockedDep{Kind: LockKindUB, Version: "v2.0.0", Commit: "cb2", Hash: "hb2"},
		got.Lock.Deps["github.com/x/b"])
	assert.Contains(t, f.calls, "github.com/x/b@v2.0.0", "B must be read at the selected version")
}

func TestResolveStopsAtGoLeaf(t *testing.T) {
	g := dep("github.com/x/golib")
	f := &fakeFetcher{results: map[string]Resolved{
		"github.com/x/golib@v1.2.3": goResult("cg"),
	}}
	got, err := Resolve(&Manifest{Requires: map[Dependency]string{g: "v1.2.3"}}, f)
	require.NoError(t, err)
	assert.Equal(t, &LockedDep{Kind: LockKindGo, Version: "v1.2.3", Commit: "cg"},
		got.Lock.Deps["github.com/x/golib"])
	assert.Equal(t, []string{"github.com/x/golib@v1.2.3"}, f.calls)
}

func TestResolveDeduplicatesDiamond(t *testing.T) {
	a, b, c := dep("github.com/x/a"), dep("github.com/x/b"), dep("github.com/x/c")
	f := &fakeFetcher{results: map[string]Resolved{
		"github.com/x/a@v1.0.0": ubResult("ca", "ha", map[Dependency]string{c: "v1.0.0"}),
		"github.com/x/b@v1.0.0": ubResult("cb", "hb", map[Dependency]string{c: "v1.0.0"}),
		"github.com/x/c@v1.0.0": ubResult("cc", "hc", nil),
	}}
	root := &Manifest{Requires: map[Dependency]string{a: "v1.0.0", b: "v1.0.0"}}
	got, err := Resolve(root, f)
	require.NoError(t, err)
	assert.Len(t, got.Lock.Deps, 3)
	cFetches := 0
	for _, call := range f.calls {
		if call == "github.com/x/c@v1.0.0" {
			cFetches++
		}
	}
	assert.Equal(t, 1, cFetches, "the shared dependency is fetched once")
}

func TestResolveFetchError(t *testing.T) {
	a := dep("github.com/x/a")
	f := &fakeFetcher{results: map[string]Resolved{}}
	_, err := Resolve(&Manifest{Requires: map[Dependency]string{a: "v1.0.0"}}, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.com/x/a@v1.0.0")
}

func TestResolveRejectsUBWithoutHash(t *testing.T) {
	a := dep("github.com/x/a")
	f := &fakeFetcher{results: map[string]Resolved{
		"github.com/x/a@v1.0.0": ubResult("ca", "", nil), // ub but no hash
	}}
	_, err := Resolve(&Manifest{Requires: map[Dependency]string{a: "v1.0.0"}}, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ub dependency missing `hash`")
}
