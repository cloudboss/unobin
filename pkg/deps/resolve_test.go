package deps

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFetcher serves canned manifests keyed by "<dep-id>@<version>" and
// records every call. A key mapped to a manifest with no requirements is
// a known leaf; an absent key is an error.
type fakeFetcher struct {
	manifests map[string]*Manifest
	calls     []string
}

func (f *fakeFetcher) Fetch(dep Dependency, version string) (*Manifest, error) {
	key := dep.String() + "@" + version
	f.calls = append(f.calls, key)
	m, ok := f.manifests[key]
	if !ok {
		return nil, fmt.Errorf("no fake result for %s", key)
	}
	return m, nil
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

func selected(m map[Dependency]string) map[string]string {
	out := make(map[string]string, len(m))
	for d, v := range m {
		out[d.String()] = v
	}
	return out
}

func toReqs(m map[string]string) map[Dependency]Requirement {
	out := make(map[Dependency]Requirement, len(m))
	for id, v := range m {
		out[dep(id)] = Requirement{Version: v}
	}
	return out
}

// fetcherFor builds a fakeFetcher from a universe keyed by "<id>@<version>";
// each value is the requirements that dependency version declares (empty
// for a leaf).
func fetcherFor(universe map[string]map[string]string) *fakeFetcher {
	manifests := make(map[string]*Manifest, len(universe))
	for key, reqs := range universe {
		manifests[key] = &Manifest{Requires: toReqs(reqs)}
	}
	return &fakeFetcher{manifests: manifests}
}

func TestResolveSelectsVersions(t *testing.T) {
	cases := []struct {
		name     string
		root     map[string]string
		universe map[string]map[string]string
		want     map[string]string
	}{
		{
			name:     "single dependency",
			root:     map[string]string{"x/a": "v1.0.0"},
			universe: map[string]map[string]string{"x/a@v1.0.0": nil},
			want:     map[string]string{"x/a": "v1.0.0"},
		},
		{
			name: "linear chain",
			root: map[string]string{"x/a": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/b": "v1.0.0"},
				"x/b@v1.0.0": {"x/c": "v1.0.0"},
				"x/c@v1.0.0": nil,
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0"},
		},
		{
			name: "canonical mvs picks the higher shared floor",
			root: map[string]string{"x/a": "v1.2.0", "x/b": "v1.2.0"},
			universe: map[string]map[string]string{
				"x/a@v1.2.0": {"x/c": "v1.3.0"},
				"x/b@v1.2.0": {"x/c": "v1.4.0"},
				"x/c@v1.3.0": {"x/d": "v1.2.0"},
				"x/c@v1.4.0": {"x/d": "v1.2.0"},
				"x/d@v1.2.0": nil,
			},
			want: map[string]string{
				"x/a": "v1.2.0", "x/b": "v1.2.0", "x/c": "v1.4.0", "x/d": "v1.2.0",
			},
		},
		{
			name: "diamond same version",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/c": "v1.0.0"},
				"x/b@v1.0.0": {"x/c": "v1.0.0"},
				"x/c@v1.0.0": nil,
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0"},
		},
		{
			name: "diamond different versions",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/c": "v1.0.0"},
				"x/b@v1.0.0": {"x/c": "v2.0.0"},
				"x/c@v1.0.0": nil,
				"x/c@v2.0.0": nil,
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v2.0.0"},
		},
		{
			name: "deep dependency raises a shallow one",
			root: map[string]string{"x/a": "v1.0.0", "x/c": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/m": "v1.0.0"},
				"x/m@v1.0.0": {"x/c": "v2.0.0"},
				"x/c@v1.0.0": nil,
				"x/c@v2.0.0": nil,
			},
			want: map[string]string{"x/a": "v1.0.0", "x/m": "v1.0.0", "x/c": "v2.0.0"},
		},
		{
			name: "raised version pulls in a new requirement",
			root: map[string]string{"x/a": "v1.0.0", "x/e": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/b": "v1.0.0"},
				"x/e@v1.0.0": {"x/b": "v2.0.0"},
				"x/b@v1.0.0": nil,
				"x/b@v2.0.0": {"x/d": "v1.0.0"}, // only v2 needs d
				"x/d@v1.0.0": nil,
			},
			want: map[string]string{
				"x/a": "v1.0.0", "x/e": "v1.0.0", "x/b": "v2.0.0", "x/d": "v1.0.0",
			},
		},
		{
			name: "raise cascades to a transitive dependency",
			root: map[string]string{"x/a": "v1.0.0", "x/p": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/b": "v1.0.0"},
				"x/p@v1.0.0": {"x/b": "v2.0.0"},
				"x/b@v1.0.0": {"x/q": "v1.0.0"},
				"x/b@v2.0.0": {"x/q": "v2.0.0"},
				"x/q@v1.0.0": nil,
				"x/q@v2.0.0": nil,
			},
			want: map[string]string{
				"x/a": "v1.0.0", "x/p": "v1.0.0", "x/b": "v2.0.0", "x/q": "v2.0.0",
			},
		},
		{
			name: "equal floors from many requirers",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/z": "v1.0.0"},
				"x/b@v1.0.0": {"x/z": "v1.0.0"},
				"x/c@v1.0.0": {"x/z": "v1.0.0"},
				"x/z@v1.0.0": nil,
			},
			want: map[string]string{
				"x/a": "v1.0.0", "x/b": "v1.0.0", "x/c": "v1.0.0", "x/z": "v1.0.0",
			},
		},
		{
			name: "release outranks prerelease",
			root: map[string]string{"x/a": "v1.0.0-rc1", "x/b": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0":     nil,
				"x/a@v1.0.0-rc1": nil,
				"x/b@v1.0.0":     {"x/a": "v1.0.0"},
			},
			want: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"},
		},
		{
			name: "v0 minor versions",
			root: map[string]string{"x/a": "v0.1.0", "x/b": "v0.1.0"},
			universe: map[string]map[string]string{
				"x/a@v0.1.0": {"x/c": "v0.2.0"},
				"x/b@v0.1.0": {"x/c": "v0.3.0"},
				"x/c@v0.2.0": nil,
				"x/c@v0.3.0": nil,
			},
			want: map[string]string{"x/a": "v0.1.0", "x/b": "v0.1.0", "x/c": "v0.3.0"},
		},
		{
			name: "double-digit minor sorts above single",
			root: map[string]string{"x/a": "v1.9.0", "x/b": "v1.9.0"},
			universe: map[string]map[string]string{
				"x/a@v1.9.0":  {"x/c": "v1.9.0"},
				"x/b@v1.9.0":  {"x/c": "v1.10.0"},
				"x/c@v1.9.0":  nil,
				"x/c@v1.10.0": nil,
			},
			want: map[string]string{"x/a": "v1.9.0", "x/b": "v1.9.0", "x/c": "v1.10.0"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Resolve(&Manifest{Requires: toReqs(c.root)}, fetcherFor(c.universe))
			require.NoError(t, err)
			assert.Equal(t, c.want, selected(got))
		})
	}
}

// TestResolveIsDeterministic runs a graph whose map and queue order vary
// between runs and confirms the selection is identical every time:
// minimal version selection does not depend on the order requirements are
// discovered.
func TestResolveUsesIndirectRootFloors(t *testing.T) {
	root := &Manifest{Requires: map[Dependency]Requirement{
		dep("github.com/cloudboss/unobin-libraries-scratch"): {Version: "v0.8.0"},
		dep("github.com/cloudboss/unobin-library-std"): {
			Version:  "v0.2.0",
			Indirect: true,
		},
	}}
	universe := map[string]map[string]string{
		"github.com/cloudboss/unobin-libraries-scratch@v0.8.0": {
			"github.com/cloudboss/unobin-library-std": "v0.1.0",
		},
		"github.com/cloudboss/unobin-library-std@v0.2.0": nil,
	}

	got, err := Resolve(root, fetcherFor(universe))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"github.com/cloudboss/unobin-libraries-scratch": "v0.8.0",
		"github.com/cloudboss/unobin-library-std":       "v0.2.0",
	}, selected(got))
}

func TestResolveIsDeterministic(t *testing.T) {
	root := map[string]string{"x/a": "v1.0.0", "x/e": "v1.0.0", "x/p": "v1.0.0"}
	universe := map[string]map[string]string{
		"x/a@v1.0.0": {"x/b": "v1.0.0", "x/q": "v1.0.0"},
		"x/e@v1.0.0": {"x/b": "v2.0.0"},
		"x/p@v1.0.0": {"x/q": "v2.0.0"},
		"x/b@v1.0.0": nil,
		"x/b@v2.0.0": {"x/d": "v1.0.0"},
		"x/q@v1.0.0": nil,
		"x/q@v2.0.0": nil,
		"x/d@v1.0.0": nil,
	}
	want := map[string]string{
		"x/a": "v1.0.0", "x/e": "v1.0.0", "x/p": "v1.0.0",
		"x/b": "v2.0.0", "x/q": "v2.0.0", "x/d": "v1.0.0",
	}
	for i := range 25 {
		got, err := Resolve(&Manifest{Requires: toReqs(root)}, fetcherFor(universe))
		require.NoError(t, err)
		assert.Equalf(t, want, selected(got), "run %d", i)
	}
}

func TestResolveStopsAtLeaf(t *testing.T) {
	f := fetcherFor(map[string]map[string]string{"x/g@v1.2.3": nil})
	got, err := Resolve(&Manifest{Requires: toReqs(map[string]string{"x/g": "v1.2.3"})}, f)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"x/g": "v1.2.3"}, selected(got))
	assert.Equal(t, []string{"x/g@v1.2.3"}, f.calls)
}

func TestResolveFetchesSharedDependencyOnce(t *testing.T) {
	f := fetcherFor(map[string]map[string]string{
		"x/a@v1.0.0": {"x/c": "v1.0.0"},
		"x/b@v1.0.0": {"x/c": "v1.0.0"},
		"x/c@v1.0.0": nil,
	})
	root := toReqs(map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"})
	_, err := Resolve(&Manifest{Requires: root}, f)
	require.NoError(t, err)
	assert.Equal(t, 1, f.fetchCount("x/c@v1.0.0"))
}

func TestResolveRefetchesOnlyOncePerRaise(t *testing.T) {
	f := fetcherFor(map[string]map[string]string{
		"x/a@v1.0.0": {"x/b": "v1.0.0"},
		"x/p@v1.0.0": {"x/b": "v2.0.0"},
		"x/b@v1.0.0": nil,
		"x/b@v2.0.0": nil,
	})
	root := toReqs(map[string]string{"x/a": "v1.0.0", "x/p": "v1.0.0"})
	_, err := Resolve(&Manifest{Requires: root}, f)
	require.NoError(t, err)
	assert.LessOrEqual(t, f.fetchCount("x/b@v1.0.0"), 1)
	assert.Equal(t, 1, f.fetchCount("x/b@v2.0.0"))
}

func TestResolveFetchError(t *testing.T) {
	f := &fakeFetcher{manifests: map[string]*Manifest{}}
	_, err := Resolve(&Manifest{Requires: toReqs(map[string]string{"x/a": "v1.0.0"})}, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "x/a@v1.0.0")
}
