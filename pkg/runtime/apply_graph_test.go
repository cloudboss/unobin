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

func leafStep(addr string) PlanStep {
	return PlanStep{Address: addr, Kind: NodeResource, Decision: DecisionCreate}
}

func TestPersistedDependsOn(t *testing.T) {
	tests := []struct {
		name       string
		steps      []PlanStep
		dependents map[string][]string
		want       map[string][]string
	}{
		{
			name:  "one edge between leaves",
			steps: []PlanStep{leafStep("a"), leafStep("b")},
			// b is depended on by a, so a depends on b.
			dependents: map[string][]string{"b": {"a"}},
			want:       map[string][]string{"a": {"b"}},
		},
		{
			name: "collapse through a data source",
			steps: []PlanStep{
				leafStep("a"),
				{Address: "d", Kind: NodeData, Decision: DecisionRead},
				leafStep("r"),
			},
			// a -> d -> r; d is not persisted, so a records r.
			dependents: map[string][]string{"r": {"d"}, "d": {"a"}},
			want:       map[string][]string{"a": {"r"}},
		},
		{
			name: "module-call stays a node",
			steps: []PlanStep{
				leafStep("r"),
				{Address: "m", Kind: NodeComposite, Decision: DecisionEval},
				leafStep("m/internal"),
			},
			// r -> m -> m/internal; m persists, so no collapse.
			dependents: map[string][]string{"m": {"r"}, "m/internal": {"m"}},
			want: map[string][]string{
				"r": {"m"},
				"m": {"m/internal"},
			},
		},
		{
			name:  "diamond dedups",
			steps: []PlanStep{leafStep("a"), leafStep("b"), leafStep("c"), leafStep("d")},
			// a -> b, a -> c, b -> d, c -> d.
			dependents: map[string][]string{
				"b": {"a"},
				"c": {"a"},
				"d": {"b", "c"},
			},
			want: map[string][]string{
				"a": {"b", "c"},
				"b": {"d"},
				"c": {"d"},
			},
		},
		{
			name: "diamond through data sources collapses to one",
			steps: []PlanStep{
				leafStep("a"),
				{Address: "d1", Kind: NodeData, Decision: DecisionRead},
				{Address: "d2", Kind: NodeData, Decision: DecisionRead},
				leafStep("r"),
			},
			// a -> d1 -> r and a -> d2 -> r; a records r once.
			dependents: map[string][]string{
				"r":  {"d1", "d2"},
				"d1": {"a"},
				"d2": {"a"},
			},
			want: map[string][]string{"a": {"r"}},
		},
		{
			name:       "no dependencies",
			steps:      []PlanStep{leafStep("a")},
			dependents: map[string][]string{},
			want:       map[string][]string{},
		},
		{
			name: "destroyed resource is not persisted",
			steps: []PlanStep{
				leafStep("a"),
				{Address: "gone", Kind: NodeResource, Decision: DecisionDestroy},
			},
			// a depends on a resource being destroyed; the destroy is not
			// a persisted entry, so it collapses to nothing.
			dependents: map[string][]string{"gone": {"a"}},
			want:       map[string][]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &stepGraph{dependents: tt.dependents}
			got := persistedDependsOn(g, tt.steps)
			assert.Equal(t, tt.want, got)
		})
	}
}

func destroyStep(addr string, dependsOn ...string) PlanStep {
	return PlanStep{
		Address:   addr,
		Kind:      NodeResource,
		Decision:  DecisionDestroy,
		DependsOn: dependsOn,
	}
}

func TestAddDestroyEdges(t *testing.T) {
	tests := []struct {
		name         string
		steps        []PlanStep
		wantIndegree map[string]int
	}{
		{
			name:         "dependent is deleted before its dependency",
			steps:        []PlanStep{destroyStep("a"), destroyStep("b", "a")},
			wantIndegree: map[string]int{"a": 1, "b": 0},
		},
		{
			name: "chain reverses end to end",
			steps: []PlanStep{
				destroyStep("a"),
				destroyStep("b", "a"),
				destroyStep("c", "b"),
			},
			wantIndegree: map[string]int{"a": 1, "b": 1, "c": 0},
		},
		{
			name: "a dependency that is not being destroyed adds no edge",
			steps: []PlanStep{
				destroyStep("b", "a"),
				{Address: "a", Kind: NodeResource, Decision: DecisionUpdate},
			},
			wantIndegree: map[string]int{"a": 0, "b": 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &stepGraph{
				indegree:   map[string]int{},
				dependents: map[string][]string{},
			}
			for i := range tt.steps {
				g.indegree[tt.steps[i].Address] = 0
			}
			addDestroyEdges(g, tt.steps)
			for addr, want := range tt.wantIndegree {
				assert.Equal(t, want, g.indegree[addr], "indegree of %s", addr)
			}
		})
	}
}

func TestPersistedDependsOnDeterministic(t *testing.T) {
	steps := []PlanStep{leafStep("a"), leafStep("b"), leafStep("c"), leafStep("d")}
	dependents := map[string][]string{
		"b": {"a"},
		"c": {"a"},
		"d": {"b", "c"},
	}
	want := persistedDependsOn(&stepGraph{dependents: dependents}, steps)
	for i := 0; i < 20; i++ {
		got := persistedDependsOn(&stepGraph{dependents: dependents}, steps)
		assert.Equal(t, want, got)
	}
}
