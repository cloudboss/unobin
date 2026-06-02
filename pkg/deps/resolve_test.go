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

func (f *fakeFetcher) fetchCount(key string) int {
	n := 0
	for _, c := range f.calls {
		if c == key {
			n++
		}
	}
	return n
}

func dep(id string) Dependency {
	d, err := ParseDependency(id)
	if err != nil {
		panic(err)
	}
	return d
}

func toDeps(m map[string]string) map[Dependency]string {
	out := make(map[Dependency]string, len(m))
	for id, v := range m {
		out[dep(id)] = v
	}
	return out
}

func selectedVersions(r *Result) map[string]string {
	out := make(map[string]string, len(r.Lock.Deps))
	for id, d := range r.Lock.Deps {
		out[id] = d.Version
	}
	return out
}

// node describes one dependency version in a fake universe: its kind and,
// for a ub dependency, the floors its own manifest declares.
type node struct {
	kind LockKind
	reqs map[string]string
}

func ub(reqs map[string]string) node { return node{kind: LockKindUB, reqs: reqs} }
func goLib() node                    { return node{kind: LockKindGo} }

// fetcherFor builds a fakeFetcher from a universe keyed by "<id>@<version>".
func fetcherFor(universe map[string]node) *fakeFetcher {
	results := make(map[string]Resolved, len(universe))
	for key, n := range universe {
		if n.kind == LockKindGo {
			results[key] = Resolved{Source: &resolve.Source{Commit: "c-" + key}, Kind: LockKindGo}
			continue
		}
		results[key] = Resolved{
			Source:   &resolve.Source{Commit: "c-" + key, Hash: "h-" + key},
			Manifest: &Manifest{Requires: toDeps(n.reqs)},
			Kind:     LockKindUB,
		}
	}
	return &fakeFetcher{results: results}
}

func TestResolveSelectsVersions(t *testing.T) {
	cases := []struct {
		name     string
		root     map[string]string
		universe map[string]node
		want     map[string]string
	}{
		{
			name:     "single dependency",
			root:     map[string]string{"x/a": "v1.0.0"},
			universe: map[string]node{"x/a@v1.0.0": ub(nil)},
			want:     map[string]string{"x/a": "v1.0.0"},
		},
		{
			name: "linear chain",
			root: map[string]string{"x/a": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/b": "v1.0.0"}),
				"x/b@v1.0.0": ub(map[string]string{"x/c": "v1.0.0"}),
				"x/c@v1.0.0": ub(nil),
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0"},
		},
		{
			name: "canonical mvs picks the higher shared floor",
			root: map[string]string{"x/a": "v1.2.0", "x/b": "v1.2.0"},
			universe: map[string]node{
				"x/a@v1.2.0": ub(map[string]string{"x/c": "v1.3.0"}),
				"x/b@v1.2.0": ub(map[string]string{"x/c": "v1.4.0"}),
				"x/c@v1.3.0": ub(map[string]string{"x/d": "v1.2.0"}),
				"x/c@v1.4.0": ub(map[string]string{"x/d": "v1.2.0"}),
				"x/d@v1.2.0": ub(nil),
			},
			want: map[string]string{
				"x/a": "v1.2.0", "x/b": "v1.2.0", "x/c": "v1.4.0", "x/d": "v1.2.0",
			},
		},
		{
			name: "diamond same version",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/c": "v1.0.0"}),
				"x/b@v1.0.0": ub(map[string]string{"x/c": "v1.0.0"}),
				"x/c@v1.0.0": ub(nil),
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0"},
		},
		{
			name: "diamond different versions",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/c": "v1.0.0"}),
				"x/b@v1.0.0": ub(map[string]string{"x/c": "v2.0.0"}),
				"x/c@v1.0.0": ub(nil),
				"x/c@v2.0.0": ub(nil),
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v2.0.0"},
		},
		{
			name: "deep dependency raises a shallow one",
			root: map[string]string{"x/a": "v1.0.0", "x/c": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/m": "v1.0.0"}),
				"x/m@v1.0.0": ub(map[string]string{"x/c": "v2.0.0"}),
				"x/c@v1.0.0": ub(nil),
				"x/c@v2.0.0": ub(nil),
			},
			want: map[string]string{"x/a": "v1.0.0", "x/m": "v1.0.0", "x/c": "v2.0.0"},
		},
		{
			name: "raised version pulls in a new requirement",
			root: map[string]string{"x/a": "v1.0.0", "x/e": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/b": "v1.0.0"}),
				"x/e@v1.0.0": ub(map[string]string{"x/b": "v2.0.0"}),
				"x/b@v1.0.0": ub(nil),
				"x/b@v2.0.0": ub(map[string]string{"x/d": "v1.0.0"}), // only v2 needs d
				"x/d@v1.0.0": ub(nil),
			},
			want: map[string]string{
				"x/a": "v1.0.0", "x/e": "v1.0.0", "x/b": "v2.0.0", "x/d": "v1.0.0",
			},
		},
		{
			name: "raise cascades to a transitive dependency",
			root: map[string]string{"x/a": "v1.0.0", "x/p": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/b": "v1.0.0"}),
				"x/p@v1.0.0": ub(map[string]string{"x/b": "v2.0.0"}),
				"x/b@v1.0.0": ub(map[string]string{"x/q": "v1.0.0"}),
				"x/b@v2.0.0": ub(map[string]string{"x/q": "v2.0.0"}),
				"x/q@v1.0.0": ub(nil),
				"x/q@v2.0.0": ub(nil),
			},
			want: map[string]string{
				"x/a": "v1.0.0", "x/p": "v1.0.0", "x/b": "v2.0.0", "x/q": "v2.0.0",
			},
		},
		{
			name: "equal floors from many requirers",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/z": "v1.0.0"}),
				"x/b@v1.0.0": ub(map[string]string{"x/z": "v1.0.0"}),
				"x/c@v1.0.0": ub(map[string]string{"x/z": "v1.0.0"}),
				"x/z@v1.0.0": ub(nil),
			},
			want: map[string]string{
				"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0", "x/z": "v1.0.0",
			},
		},
		{
			name: "release outranks prerelease",
			root: map[string]string{"x/a": "v1.0.0-rc1", "x/b": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0":     ub(nil),
				"x/a@v1.0.0-rc1": ub(nil),
				"x/b@v1.0.0":     ub(map[string]string{"x/a": "v1.0.0"}),
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"},
		},
		{
			name: "v0 minor versions",
			root: map[string]string{"x/a": "v0.1.0", "x/b": "v0.1.0"},
			universe: map[string]node{
				"x/a@v0.1.0": ub(map[string]string{"x/c": "v0.2.0"}),
				"x/b@v0.1.0": ub(map[string]string{"x/c": "v0.3.0"}),
				"x/c@v0.2.0": ub(nil),
				"x/c@v0.3.0": ub(nil),
			},
			want: map[string]string{"x/a": "v0.1.0", "x/b": "v0.1.0", "x/c": "v0.3.0"},
		},
		{
			name: "double-digit minor sorts above single",
			root: map[string]string{"x/a": "v1.9.0", "x/b": "v1.9.0"},
			universe: map[string]node{
				"x/a@v1.9.0":  ub(map[string]string{"x/c": "v1.9.0"}),
				"x/b@v1.9.0":  ub(map[string]string{"x/c": "v1.10.0"}),
				"x/c@v1.9.0":  ub(nil),
				"x/c@v1.10.0": ub(nil),
			},
			want: map[string]string{"x/a": "v1.9.0", "x/b": "v1.9.0", "x/c": "v1.10.0"},
		},
		{
			name:     "go leaf",
			root:     map[string]string{"x/g": "v1.2.3"},
			universe: map[string]node{"x/g@v1.2.3": goLib()},
			want:     map[string]string{"x/g": "v1.2.3"},
		},
		{
			name: "mixed go and ub",
			root: map[string]string{"x/a": "v1.0.0", "x/g": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0": ub(map[string]string{"x/b": "v1.0.0"}),
				"x/b@v1.0.0": ub(nil),
				"x/g@v1.0.0": goLib(),
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/g": "v1.0.0"},
		},
		{
			name: "wide graph",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0"},
			universe: map[string]node{
				"x/a@v1.0.0":  ub(map[string]string{"x/la": "v1.0.0"}),
				"x/b@v1.0.0":  ub(map[string]string{"x/lb": "v1.0.0"}),
				"x/c@v1.0.0":  ub(map[string]string{"x/lc": "v1.0.0"}),
				"x/la@v1.0.0": ub(nil),
				"x/lb@v1.0.0": ub(nil),
				"x/lc@v1.0.0": ub(nil),
			},
			want: map[string]string{
				"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0",
				"x/la": "v1.0.0", "x/lb": "v1.0.0", "x/lc": "v1.0.0",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Resolve(&Manifest{Requires: toDeps(c.root)}, fetcherFor(c.universe))
			require.NoError(t, err)
			assert.Equal(t, c.want, selectedVersions(got))
		})
	}
}

// TestResolveIsDeterministic runs a graph whose map iteration and queue
// order vary between runs and confirms the encoded lock is byte-identical
// every time: minimal version selection does not depend on the order
// requirements are discovered.
func TestResolveIsDeterministic(t *testing.T) {
	root := map[string]string{"x/a": "v1.0.0", "x/e": "v1.0.0", "x/p": "v1.0.0"}
	universe := map[string]node{
		"x/a@v1.0.0": ub(map[string]string{"x/b": "v1.0.0", "x/q": "v1.0.0"}),
		"x/e@v1.0.0": ub(map[string]string{"x/b": "v2.0.0"}),
		"x/p@v1.0.0": ub(map[string]string{"x/q": "v2.0.0"}),
		"x/b@v1.0.0": ub(nil),
		"x/b@v2.0.0": ub(map[string]string{"x/d": "v1.0.0"}),
		"x/q@v1.0.0": ub(nil),
		"x/q@v2.0.0": ub(nil),
		"x/d@v1.0.0": ub(nil),
	}
	var first string
	for i := range 25 {
		got, err := Resolve(&Manifest{Requires: toDeps(root)}, fetcherFor(universe))
		require.NoError(t, err)
		b, err := EncodeLock(got.Lock)
		require.NoError(t, err)
		if i == 0 {
			first = string(b)
			continue
		}
		assert.Equal(t, first, string(b), "run %d produced a different lock", i)
	}
}

func TestResolveStopsAtGoLeaf(t *testing.T) {
	f := fetcherFor(map[string]node{"x/g@v1.2.3": goLib()})
	got, err := Resolve(&Manifest{Requires: toDeps(map[string]string{"x/g": "v1.2.3"})}, f)
	require.NoError(t, err)
	assert.Equal(t, &LockedDep{Kind: LockKindGo, Version: "v1.2.3", Commit: "c-x/g@v1.2.3"},
		got.Lock.Deps["x/g"])
	assert.Equal(t, []string{"x/g@v1.2.3"}, f.calls)
}

func TestResolveFetchesSharedDependencyOnce(t *testing.T) {
	f := fetcherFor(map[string]node{
		"x/a@v1.0.0": ub(map[string]string{"x/c": "v1.0.0"}),
		"x/b@v1.0.0": ub(map[string]string{"x/c": "v1.0.0"}),
		"x/c@v1.0.0": ub(nil),
	})
	root := toDeps(map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"})
	_, err := Resolve(&Manifest{Requires: root}, f)
	require.NoError(t, err)
	assert.Equal(t, 1, f.fetchCount("x/c@v1.0.0"))
}

func TestResolveRefetchesOnlyOncePerRaise(t *testing.T) {
	f := fetcherFor(map[string]node{
		"x/a@v1.0.0": ub(map[string]string{"x/b": "v1.0.0"}),
		"x/p@v1.0.0": ub(map[string]string{"x/b": "v2.0.0"}),
		"x/b@v1.0.0": ub(nil),
		"x/b@v2.0.0": ub(nil),
	})
	root := toDeps(map[string]string{"x/a": "v1.0.0", "x/p": "v1.0.0"})
	_, err := Resolve(&Manifest{Requires: root}, f)
	require.NoError(t, err)
	// b is fetched at most once per distinct version, never repeatedly at
	// the same one.
	assert.LessOrEqual(t, f.fetchCount("x/b@v1.0.0"), 1)
	assert.Equal(t, 1, f.fetchCount("x/b@v2.0.0"))
}

func TestResolveFetchError(t *testing.T) {
	f := &fakeFetcher{results: map[string]Resolved{}}
	_, err := Resolve(&Manifest{Requires: toDeps(map[string]string{"x/a": "v1.0.0"})}, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "x/a@v1.0.0")
}

func TestResolveRejectsUBWithoutHash(t *testing.T) {
	f := &fakeFetcher{results: map[string]Resolved{
		"x/a@v1.0.0": {
			Source:   &resolve.Source{Commit: "ca"}, // ub but no hash
			Manifest: &Manifest{},
			Kind:     LockKindUB,
		},
	}}
	_, err := Resolve(&Manifest{Requires: toDeps(map[string]string{"x/a": "v1.0.0"})}, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ub dependency missing `hash`")
}
