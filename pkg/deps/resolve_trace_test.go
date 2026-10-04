package deps

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveWithTrace(t *testing.T) {
	step := func(project, version, target, floor string) RequirementStep {
		var declaring Dependency
		if project != "" {
			declaring = dep(project)
		}
		return RequirementStep{
			Dependency: declaring, Version: version,
			Requires: dep(target), MinimumVersion: floor,
		}
	}
	tests := []struct {
		name         string
		root         map[string]string
		universe     map[string]map[string]string
		selection    map[string]string
		requirements []RequirementStep
		chains       map[Dependency][]RequirementStep
		calls        []string
	}{
		{
			name: "empty project", selection: map[string]string{},
			requirements: []RequirementStep{}, chains: map[Dependency][]RequirementStep{},
		},
		{
			name: "root wins a tied floor",
			root: map[string]string{"x/a": "v1.0.0", "x/b": "v2.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/b": "v2.0.0"}, "x/b@v2.0.0": nil,
			},
			selection: map[string]string{"x/a": "v1.0.0", "x/b": "v2.0.0"},
			requirements: []RequirementStep{
				step("", "", "x/a", "v1.0.0"), step("", "", "x/b", "v2.0.0"),
				step("x/a", "v1.0.0", "x/b", "v2.0.0"),
			},
			chains: map[Dependency][]RequirementStep{
				dep("x/a"): {step("", "", "x/a", "v1.0.0")},
				dep("x/b"): {step("", "", "x/b", "v2.0.0")},
			},
			calls: []string{"x/a@v1.0.0", "x/b@v2.0.0"},
		},
		{
			name: "shortest then lexical declaring project",
			root: map[string]string{"x/a": "v1.0.0", "x/p": "v1.0.0", "x/z": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/b": "v1.0.0"},
				"x/p@v1.0.0": {"x/c": "v2.0.0"},
				"x/z@v1.0.0": {"x/c": "v2.0.0"},
				"x/b@v1.0.0": {"x/c": "v2.0.0"}, "x/c@v2.0.0": nil,
			},
			selection: map[string]string{
				"x/a": "v1.0.0", "x/p": "v1.0.0", "x/z": "v1.0.0",
				"x/b": "v1.0.0", "x/c": "v2.0.0",
			},
			requirements: []RequirementStep{
				step("", "", "x/a", "v1.0.0"), step("", "", "x/p", "v1.0.0"),
				step("", "", "x/z", "v1.0.0"), step("x/a", "v1.0.0", "x/b", "v1.0.0"),
				step("x/p", "v1.0.0", "x/c", "v2.0.0"),
				step("x/z", "v1.0.0", "x/c", "v2.0.0"),
				step("x/b", "v1.0.0", "x/c", "v2.0.0"),
			},
			chains: map[Dependency][]RequirementStep{
				dep("x/a"): {step("", "", "x/a", "v1.0.0")},
				dep("x/p"): {step("", "", "x/p", "v1.0.0")},
				dep("x/z"): {step("", "", "x/z", "v1.0.0")},
				dep("x/b"): {
					step("", "", "x/a", "v1.0.0"), step("x/a", "v1.0.0", "x/b", "v1.0.0"),
				},
				dep("x/c"): {
					step("", "", "x/p", "v1.0.0"), step("x/p", "v1.0.0", "x/c", "v2.0.0"),
				},
			},
			calls: []string{
				"x/a@v1.0.0", "x/p@v1.0.0", "x/z@v1.0.0", "x/b@v1.0.0", "x/c@v2.0.0",
			},
		},
		{
			name: "earlier requirements retain the version that declared them",
			root: map[string]string{"x/a": "v1.0.0", "x/p": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/b": "v1.0.0"},
				"x/p@v1.0.0": {"x/c": "v1.0.0"},
				"x/b@v1.0.0": {"x/d": "v3.0.0"},
				"x/c@v1.0.0": {"x/b": "v2.0.0"},
				"x/d@v3.0.0": nil, "x/b@v2.0.0": nil,
			},
			selection: map[string]string{
				"x/a": "v1.0.0", "x/p": "v1.0.0", "x/c": "v1.0.0",
				"x/b": "v2.0.0", "x/d": "v3.0.0",
			},
			requirements: []RequirementStep{
				step("", "", "x/a", "v1.0.0"), step("", "", "x/p", "v1.0.0"),
				step("x/a", "v1.0.0", "x/b", "v1.0.0"),
				step("x/p", "v1.0.0", "x/c", "v1.0.0"),
				step("x/b", "v1.0.0", "x/d", "v3.0.0"),
				step("x/c", "v1.0.0", "x/b", "v2.0.0"),
			},
			chains: map[Dependency][]RequirementStep{
				dep("x/a"): {step("", "", "x/a", "v1.0.0")},
				dep("x/p"): {step("", "", "x/p", "v1.0.0")},
				dep("x/c"): {
					step("", "", "x/p", "v1.0.0"), step("x/p", "v1.0.0", "x/c", "v1.0.0"),
				},
				dep("x/b"): {
					step("", "", "x/p", "v1.0.0"), step("x/p", "v1.0.0", "x/c", "v1.0.0"),
					step("x/c", "v1.0.0", "x/b", "v2.0.0"),
				},
				dep("x/d"): {
					step("", "", "x/a", "v1.0.0"), step("x/a", "v1.0.0", "x/b", "v1.0.0"),
					step("x/b", "v1.0.0", "x/d", "v3.0.0"),
				},
			},
			calls: []string{
				"x/a@v1.0.0", "x/p@v1.0.0", "x/b@v1.0.0", "x/c@v1.0.0",
				"x/d@v3.0.0", "x/b@v2.0.0",
			},
		},
		{
			name: "cycle uses the root path",
			root: map[string]string{"x/a": "v1.0.0"},
			universe: map[string]map[string]string{
				"x/a@v1.0.0": {"x/b": "v1.0.0"},
				"x/b@v1.0.0": {"x/a": "v1.0.0"},
			},
			selection: map[string]string{"x/a": "v1.0.0", "x/b": "v1.0.0"},
			requirements: []RequirementStep{
				step("", "", "x/a", "v1.0.0"), step("x/a", "v1.0.0", "x/b", "v1.0.0"),
				step("x/b", "v1.0.0", "x/a", "v1.0.0"),
			},
			chains: map[Dependency][]RequirementStep{
				dep("x/a"): {step("", "", "x/a", "v1.0.0")},
				dep("x/b"): {
					step("", "", "x/a", "v1.0.0"), step("x/a", "v1.0.0", "x/b", "v1.0.0"),
				},
			},
			calls: []string{"x/a@v1.0.0", "x/b@v1.0.0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 10 {
				fetch := fetcherFor(tt.universe)
				got, err := ResolveWithTrace(&Project{Requires: toReqs(tt.root)}, fetch)
				require.NoError(t, err)
				assert.Equal(t, tt.selection, selected(got.Selection))
				assert.Equal(t, tt.requirements, got.Requirements)
				assert.Equal(t, tt.chains, got.Chains)
				assert.Equal(t, tt.calls, fetch.calls)
			}
		})
	}
}

type failingTraceFetcher struct {
	err error
}

func (f failingTraceFetcher) Fetch(Dependency, string) (*Project, error) {
	return nil, f.err
}

func TestResolveWithTracePreservesFailureAndOrigin(t *testing.T) {
	cause := errors.New("repository unavailable")
	root := &Project{Requires: toReqs(map[string]string{"x/a": "v1.0.0"})}
	got, err := ResolveWithTrace(root, failingTraceFetcher{err: cause})
	require.ErrorIs(t, err, cause)
	assert.EqualError(t, err, "resolve x/a@v1.0.0: repository unavailable")
	require.NotNil(t, got)
	assert.Equal(t, map[string]string{"x/a": "v1.0.0"}, selected(got.Selection))
	assert.Equal(t, []RequirementStep{{Requires: dep("x/a"), MinimumVersion: "v1.0.0"}},
		got.Requirements)
	assert.Equal(t, map[Dependency][]RequirementStep{dep("x/a"): got.Requirements}, got.Chains)
	selection, err := Resolve(root, failingTraceFetcher{err: cause})
	require.ErrorIs(t, err, cause)
	assert.Nil(t, selection)
}
