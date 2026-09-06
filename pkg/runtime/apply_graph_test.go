package runtime

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func newDAG(edges map[string][]string) *DAG {
	nodes := map[string]*Node{}
	for from, deps := range edges {
		nodes[from] = &Node{Address: from}
		for _, d := range deps {
			nodes[d] = &Node{Address: d}
		}
	}
	return &DAG{Nodes: nodes, Edges: edges}
}

func sortDependents(g *stepGraph) {
	for k := range g.dependents {
		slices.Sort(g.dependents[k])
	}
}

func TestBuildStepGraphPlainLeaves(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.subnet": {"resource.vpc"},
		"resource.vpc":    nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.vpc",
		"resource.subnet",
	}, dag)
	sortDependents(g)
	assert.Equal(t, 0, g.indegree["resource.vpc"])
	assert.Equal(t, 1, g.indegree["resource.subnet"])
	assert.Equal(t,
		[]string{"resource.subnet"},
		g.dependents["resource.vpc"])
}

func TestBuildStepGraphForEachOnPlain(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.nodes":  {"resource.subnet"},
		"resource.subnet": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.subnet",
		"resource.nodes['alpha']",
		"resource.nodes['beta']",
	}, dag)
	sortDependents(g)
	assert.Equal(t, 0, g.indegree["resource.subnet"])
	assert.Equal(t, 1, g.indegree["resource.nodes['alpha']"])
	assert.Equal(t, 1, g.indegree["resource.nodes['beta']"])
	assert.Equal(t,
		[]string{
			"resource.nodes['alpha']",
			"resource.nodes['beta']",
		},
		g.dependents["resource.subnet"])
}

func TestBuildStepGraphPlainDependsOnForEach(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.lb":    {"resource.nodes"},
		"resource.nodes": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.nodes['alpha']",
		"resource.nodes['beta']",
		"resource.lb",
	}, dag)
	assert.Equal(t, 2, g.indegree["resource.lb"])
	assert.Equal(t,
		[]string{"resource.lb"},
		g.dependents["resource.nodes['alpha']"])
	assert.Equal(t,
		[]string{"resource.lb"},
		g.dependents["resource.nodes['beta']"])
}

func TestBuildStepGraphForEachOnForEachCartesian(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.vols":  {"resource.nodes"},
		"resource.nodes": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.nodes['alpha']",
		"resource.nodes['beta']",
		"resource.vols['alpha']",
		"resource.vols['beta']",
	}, dag)
	assert.Equal(t, 2, g.indegree["resource.vols['alpha']"])
	assert.Equal(t, 2, g.indegree["resource.vols['beta']"])
}

func TestBuildStepGraphCompositeInternalsSameKeyOnly(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.web/resource.subnet": {
			"resource.web/resource.vpc",
		},
		"resource.web/resource.vpc": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.web['k1']/resource.vpc",
		"resource.web['k1']/resource.subnet",
		"resource.web['k2']/resource.vpc",
		"resource.web['k2']/resource.subnet",
	}, dag)
	sortDependents(g)
	assert.Equal(t, 1, g.indegree["resource.web['k1']/resource.subnet"])
	assert.Equal(t, 1, g.indegree["resource.web['k2']/resource.subnet"])
	assert.Equal(t,
		[]string{"resource.web['k1']/resource.subnet"},
		g.dependents["resource.web['k1']/resource.vpc"])
	assert.Equal(t,
		[]string{"resource.web['k2']/resource.subnet"},
		g.dependents["resource.web['k2']/resource.vpc"])
}

func TestBuildStepGraphCompositeInternalsSameKeyWithSlash(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.web/resource.subnet": {
			"resource.web/resource.vpc",
		},
		"resource.web/resource.vpc": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.web['a/b']/resource.vpc",
		"resource.web['a/b']/resource.subnet",
		"resource.web['a/c']/resource.vpc",
		"resource.web['a/c']/resource.subnet",
	}, dag)
	sortDependents(g)
	assert.Equal(t, 1, g.indegree["resource.web['a/b']/resource.subnet"])
	assert.Equal(t, 1, g.indegree["resource.web['a/c']/resource.subnet"])
	assert.Equal(t,
		[]string{"resource.web['a/b']/resource.subnet"},
		g.dependents["resource.web['a/b']/resource.vpc"])
	assert.Equal(t,
		[]string{"resource.web['a/c']/resource.subnet"},
		g.dependents["resource.web['a/c']/resource.vpc"])
}

func TestBuildStepGraphForEachCompositeBoundary(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.web": {
			"resource.web/resource.vpc",
			"resource.web/resource.subnet",
		},
		"resource.web/resource.vpc":    nil,
		"resource.web/resource.subnet": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.web['k1']/resource.vpc",
		"resource.web['k1']/resource.subnet",
		"resource.web['k1']",
		"resource.web['k2']/resource.vpc",
		"resource.web['k2']/resource.subnet",
		"resource.web['k2']",
	}, dag)
	assert.Equal(t, 2, g.indegree["resource.web['k1']"])
	assert.Equal(t, 2, g.indegree["resource.web['k2']"])
}

func TestBuildStepGraphOrphanHasNoPredecessors(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.vpc": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.vpc",
		"resource.zombie",
	}, dag)
	assert.Equal(t, 0, g.indegree["resource.zombie"])
	assert.Nil(t, g.dependents["resource.zombie"])
}

func TestBuildStepGraphPairKeyNarrowsForEachCrossDeps(t *testing.T) {
	libs := map[string]*Library{
		"aws": {
			Name: "aws",
			Resources: map[string]ResourceRegistration{
				"instance": MakeResource[plainResource, *plainResourceOutput, any](
					plainResourceDefinition(),
				),
				"volume": MakeResource[plainResource, *plainResourceOutput, any](
					plainResourceDefinition(),
				),
			},
		},
	}
	dag := syntaxDAG(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-graph", "pair-key"), libs)
	addresses := []string{
		"resource.nodes['alpha']",
		"resource.nodes['beta']",
		"resource.vols['alpha']",
		"resource.vols['beta']",
	}
	pairKey := map[string]map[string]bool{}
	for _, addr := range addresses {
		if node, ok := dag.Nodes[templateAddress(addr)]; ok {
			if pk := pairKeyDeps(node.Body, dag.Nodes, node.Composite); pk != nil {
				pairKey[addr] = pk
			}
		}
	}
	g := buildStepGraphWithPairKey(addresses, dag, pairKey, nil)
	assert.Equal(t, 1, g.indegree["resource.vols['alpha']"],
		"alpha vol should depend on only the alpha node, not both")
	assert.Equal(t, 1, g.indegree["resource.vols['beta']"],
		"beta vol should depend on only the beta node, not both")
}

func TestKeyPath(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want []keyPosition
	}{
		{
			name: "no key",
			addr: "resource.vpc",
			want: nil,
		},
		{
			name: "single key at root",
			addr: "resource.nodes['alpha']",
			want: []keyPosition{{at: "resource.nodes", key: "alpha"}},
		},
		{
			name: "key at composite boundary",
			addr: "resource.web['k1']/resource.vpc",
			want: []keyPosition{{at: "resource.web", key: "k1"}},
		},
		{
			name: "key only at internal",
			addr: "resource.web/resource.nodes['alpha']",
			want: []keyPosition{
				{at: "resource.web/resource.nodes", key: "alpha"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, keyPath(tt.addr))
		})
	}
}

func TestKeyPathsAgree(t *testing.T) {
	a := keyPath("resource.web['k1']/resource.subnet")
	b := keyPath("resource.web['k1']/resource.vpc")
	c := keyPath("resource.web['k2']/resource.vpc")
	d := keyPath("resource.subnet")
	assert.True(t, keyPathsAgree(a, b))
	assert.False(t, keyPathsAgree(a, c))
	assert.True(t, keyPathsAgree(a, d))
	assert.True(t, keyPathsAgree(d, a))
	assert.True(t, keyPathsAgree(d, d))
}
