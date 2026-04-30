package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGraphAcyclic(t *testing.T) {
	g := NewGraph()
	g.AddEdge("a", "b")
	g.AddEdge("a", "c")
	g.AddEdge("b", "d")
	g.AddEdge("c", "d")
	require.Empty(t, g.DetectCycles())
}

func TestGraphSelfLoop(t *testing.T) {
	g := NewGraph()
	g.AddEdge("a", "a")
	cycles := g.DetectCycles()
	require.Len(t, cycles, 1)
	require.Equal(t, []string{"a", "a"}, cycles[0])
}

func TestGraphTwoNodeCycle(t *testing.T) {
	g := NewGraph()
	g.AddEdge("a", "b")
	g.AddEdge("b", "a")
	cycles := g.DetectCycles()
	require.Len(t, cycles, 1)
	require.Equal(t, []string{"a", "b", "a"}, cycles[0])
}

func TestGraphThreeNodeCycle(t *testing.T) {
	g := NewGraph()
	g.AddEdge("a", "b")
	g.AddEdge("b", "c")
	g.AddEdge("c", "a")
	cycles := g.DetectCycles()
	require.Len(t, cycles, 1)
	require.Equal(t, []string{"a", "b", "c", "a"}, cycles[0])
}

func TestGraphCycleAmongNonCycleNodes(t *testing.T) {
	g := NewGraph()
	g.AddEdge("root", "branch")
	g.AddEdge("root", "leafA")
	g.AddEdge("branch", "spinX")
	g.AddEdge("spinX", "spinY")
	g.AddEdge("spinY", "spinX")
	cycles := g.DetectCycles()
	require.Len(t, cycles, 1)
	require.Equal(t, []string{"spinX", "spinY", "spinX"}, cycles[0])
}

func TestGraphIsolatedNodes(t *testing.T) {
	g := NewGraph()
	g.AddNode("alone")
	require.Empty(t, g.DetectCycles())
}

func TestGraphAddEdgeDeduplicates(t *testing.T) {
	g := NewGraph()
	g.AddEdge("a", "b")
	g.AddEdge("a", "b")
	g.AddEdge("a", "b")
	require.Empty(t, g.DetectCycles())
}

func TestCheckSameRepoVersionsHappy(t *testing.T) {
	refs := map[string]ImportRef{
		"aws":  &RemoteImport{URL: "github.com/x/y", Subdir: "aws", Version: "v1.0.0"},
		"net":  &RemoteImport{URL: "github.com/x/y", Subdir: "net", Version: "v1.0.0"},
		"util": &RemoteImport{URL: "github.com/a/b", Version: "v0.3.0"},
		"site": &LocalImport{Path: "./local"},
	}
	require.Empty(t, CheckSameRepoVersions(refs))
}

func TestCheckSameRepoVersionsConflict(t *testing.T) {
	refs := map[string]ImportRef{
		"aws": &RemoteImport{URL: "github.com/x/y", Subdir: "aws", Version: "v1.0.0"},
		"net": &RemoteImport{URL: "github.com/x/y", Subdir: "net", Version: "v1.1.0"},
	}
	errs := CheckSameRepoVersions(refs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0].Error(), "github.com/x/y")
	require.Contains(t, errs[0].Error(), "v1.0.0")
	require.Contains(t, errs[0].Error(), "v1.1.0")
}

func TestCheckSameRepoVersionsMultipleRepos(t *testing.T) {
	refs := map[string]ImportRef{
		"a1": &RemoteImport{URL: "github.com/a/a", Version: "v1.0.0"},
		"a2": &RemoteImport{URL: "github.com/a/a", Version: "v1.1.0"},
		"b1": &RemoteImport{URL: "github.com/b/b", Version: "v2.0.0"},
		"b2": &RemoteImport{URL: "github.com/b/b", Version: "v2.5.0"},
	}
	errs := CheckSameRepoVersions(refs)
	require.Len(t, errs, 2)
	require.Contains(t, errs[0].Error(), "github.com/a/a")
	require.Contains(t, errs[1].Error(), "github.com/b/b")
}

func TestCheckSameRepoVersionsIgnoresLocal(t *testing.T) {
	refs := map[string]ImportRef{
		"x": &LocalImport{Path: "./x"},
		"y": &LocalImport{Path: "./y"},
	}
	require.Empty(t, CheckSameRepoVersions(refs))
}
