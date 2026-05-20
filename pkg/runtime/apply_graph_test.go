package runtime

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
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
		sort.Strings(g.dependents[k])
	}
}

func TestBuildStepGraphPlainLeaves(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.aws.subnet.this": {"resource.aws.vpc.main"},
		"resource.aws.vpc.main":    nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.aws.vpc.main",
		"resource.aws.subnet.this",
	}, dag)
	sortDependents(g)
	assert.Equal(t, 0, g.indegree["resource.aws.vpc.main"])
	assert.Equal(t, 1, g.indegree["resource.aws.subnet.this"])
	assert.Equal(t,
		[]string{"resource.aws.subnet.this"},
		g.dependents["resource.aws.vpc.main"])
}

func TestBuildStepGraphForEachOnPlain(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.aws.instance.nodes": {"resource.aws.subnet.this"},
		"resource.aws.subnet.this":    nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.aws.subnet.this",
		"resource.aws.instance.nodes['alpha']",
		"resource.aws.instance.nodes['beta']",
	}, dag)
	sortDependents(g)
	assert.Equal(t, 0, g.indegree["resource.aws.subnet.this"])
	assert.Equal(t, 1, g.indegree["resource.aws.instance.nodes['alpha']"])
	assert.Equal(t, 1, g.indegree["resource.aws.instance.nodes['beta']"])
	assert.Equal(t,
		[]string{
			"resource.aws.instance.nodes['alpha']",
			"resource.aws.instance.nodes['beta']",
		},
		g.dependents["resource.aws.subnet.this"])
}

func TestBuildStepGraphPlainDependsOnForEach(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.aws.lb.web":         {"resource.aws.instance.nodes"},
		"resource.aws.instance.nodes": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.aws.instance.nodes['alpha']",
		"resource.aws.instance.nodes['beta']",
		"resource.aws.lb.web",
	}, dag)
	assert.Equal(t, 2, g.indegree["resource.aws.lb.web"])
	assert.Equal(t,
		[]string{"resource.aws.lb.web"},
		g.dependents["resource.aws.instance.nodes['alpha']"])
	assert.Equal(t,
		[]string{"resource.aws.lb.web"},
		g.dependents["resource.aws.instance.nodes['beta']"])
}

func TestBuildStepGraphForEachOnForEachCartesian(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.aws.volume.vols":    {"resource.aws.instance.nodes"},
		"resource.aws.instance.nodes": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.aws.instance.nodes['alpha']",
		"resource.aws.instance.nodes['beta']",
		"resource.aws.volume.vols['alpha']",
		"resource.aws.volume.vols['beta']",
	}, dag)
	assert.Equal(t, 2, g.indegree["resource.aws.volume.vols['alpha']"])
	assert.Equal(t, 2, g.indegree["resource.aws.volume.vols['beta']"])
}

func TestBuildStepGraphCompositeInternalsSameKeyOnly(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.net.cluster.web/aws.subnet.this": {
			"resource.net.cluster.web/aws.vpc.this",
		},
		"resource.net.cluster.web/aws.vpc.this": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.net.cluster.web['k1']/aws.vpc.this",
		"resource.net.cluster.web['k1']/aws.subnet.this",
		"resource.net.cluster.web['k2']/aws.vpc.this",
		"resource.net.cluster.web['k2']/aws.subnet.this",
	}, dag)
	sortDependents(g)
	assert.Equal(t, 1, g.indegree["resource.net.cluster.web['k1']/aws.subnet.this"])
	assert.Equal(t, 1, g.indegree["resource.net.cluster.web['k2']/aws.subnet.this"])
	assert.Equal(t,
		[]string{"resource.net.cluster.web['k1']/aws.subnet.this"},
		g.dependents["resource.net.cluster.web['k1']/aws.vpc.this"])
	assert.Equal(t,
		[]string{"resource.net.cluster.web['k2']/aws.subnet.this"},
		g.dependents["resource.net.cluster.web['k2']/aws.vpc.this"])
}

func TestBuildStepGraphForEachCompositeBoundary(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.net.cluster.web": {
			"resource.net.cluster.web/aws.vpc.this",
			"resource.net.cluster.web/aws.subnet.this",
		},
		"resource.net.cluster.web/aws.vpc.this":    nil,
		"resource.net.cluster.web/aws.subnet.this": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.net.cluster.web['k1']/aws.vpc.this",
		"resource.net.cluster.web['k1']/aws.subnet.this",
		"resource.net.cluster.web['k1']",
		"resource.net.cluster.web['k2']/aws.vpc.this",
		"resource.net.cluster.web['k2']/aws.subnet.this",
		"resource.net.cluster.web['k2']",
	}, dag)
	assert.Equal(t, 2, g.indegree["resource.net.cluster.web['k1']"])
	assert.Equal(t, 2, g.indegree["resource.net.cluster.web['k2']"])
}

func TestBuildStepGraphOrphanHasNoPredecessors(t *testing.T) {
	dag := newDAG(map[string][]string{
		"resource.aws.vpc.main": nil,
	})
	g := buildStepGraphFromAddresses([]string{
		"resource.aws.vpc.main",
		"resource.aws.deleted.zombie",
	}, dag)
	assert.Equal(t, 0, g.indegree["resource.aws.deleted.zombie"])
	assert.Nil(t, g.dependents["resource.aws.deleted.zombie"])
}

func TestBuildStepGraphPairKeyNarrowsForEachCrossDeps(t *testing.T) {
	src := `
resources: {
  aws: {
    instance: {
      nodes: { @for-each: var.cfgs, name: @each.value }
    }
    volume: {
      vols: {
        @for-each: var.cfgs
        instance:  resource.aws.instance.nodes[@each.key].name
      }
    }
  }
}
`
	mods := map[string]*Module{
		"aws": {
			Name: "aws",
			Resources: map[string]ResourceRegistration{
				"instance": MakeResource[plainResource, any](),
				"volume":   MakeResource[plainResource, any](),
			},
		},
	}
	f := parseStack(t, src)
	dag := BuildDAG(f, mods)
	addresses := []string{
		"resource.aws.instance.nodes['alpha']",
		"resource.aws.instance.nodes['beta']",
		"resource.aws.volume.vols['alpha']",
		"resource.aws.volume.vols['beta']",
	}
	pairKey := map[string]map[string]bool{}
	for _, addr := range addresses {
		if node, ok := dag.Nodes[templateAddress(addr)]; ok {
			if pk := pairKeyDeps(node.Body); pk != nil {
				pairKey[addr] = pk
			}
		}
	}
	g := buildStepGraphWithPairKey(addresses, dag, pairKey)
	assert.Equal(t, 1, g.indegree["resource.aws.volume.vols['alpha']"],
		"alpha vol should depend on only the alpha node, not both")
	assert.Equal(t, 1, g.indegree["resource.aws.volume.vols['beta']"],
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
			addr: "resource.aws.vpc.main",
			want: nil,
		},
		{
			name: "single key at root",
			addr: "resource.aws.instance.nodes['alpha']",
			want: []keyPosition{{at: "resource.aws.instance.nodes", key: "alpha"}},
		},
		{
			name: "key at composite boundary",
			addr: "resource.net.cluster.web['k1']/aws.vpc.this",
			want: []keyPosition{{at: "resource.net.cluster.web", key: "k1"}},
		},
		{
			name: "key only at internal",
			addr: "resource.net.cluster.web/aws.instance.nodes['alpha']",
			want: []keyPosition{
				{at: "resource.net.cluster.web/aws.instance.nodes", key: "alpha"},
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
	a := keyPath("resource.net.cluster.web['k1']/aws.subnet.this")
	b := keyPath("resource.net.cluster.web['k1']/aws.vpc.this")
	c := keyPath("resource.net.cluster.web['k2']/aws.vpc.this")
	d := keyPath("resource.aws.subnet.this")
	assert.True(t, keyPathsAgree(a, b))
	assert.False(t, keyPathsAgree(a, c))
	assert.True(t, keyPathsAgree(a, d))
	assert.True(t, keyPathsAgree(d, a))
	assert.True(t, keyPathsAgree(d, d))
}
