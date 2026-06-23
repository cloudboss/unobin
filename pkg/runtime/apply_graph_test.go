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
				"instance": MakeResource[plainResource, any, any](),
				"volume":   MakeResource[plainResource, any, any](),
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
				{Address: "d", Kind: NodeDataSource, Decision: DecisionRead},
				leafStep("r"),
			},
			// a -> d -> r; d is not persisted, so a records r.
			dependents: map[string][]string{"r": {"d"}, "d": {"a"}},
			want:       map[string][]string{"a": {"r"}},
		},
		{
			name: "collapse through a library config",
			steps: []PlanStep{
				{Address: "a", Kind: NodeAction, Decision: DecisionCreate},
				{Address: "cfg", Kind: NodeLibraryConfig, Decision: DecisionEval},
				leafStep("r"),
			},
			// a -> cfg -> r; the config evaluation is not an entry, so a
			// records r and destroy sequences a before r.
			dependents: map[string][]string{"r": {"cfg"}, "cfg": {"a"}},
			want:       map[string][]string{"a": {"r"}},
		},
		{
			name: "library-call stays a node",
			steps: []PlanStep{
				leafStep("r"),
				{Address: "m", Composite: true, Decision: DecisionEval},
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
				{Address: "d1", Kind: NodeDataSource, Decision: DecisionRead},
				{Address: "d2", Kind: NodeDataSource, Decision: DecisionRead},
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

// TestPersistedDependsOnCollapsesLibraryConfigs pins the recorded
// depends-on for every consumer form that reaches a resource only
// through a library config node: a plain leaf, @for-each instances, and
// a composite-internal leaf. Each must record the resource the config
// reads, since destroy ordering has no config entry to sequence against.
func TestPersistedDependsOnCollapsesLibraryConfigs(t *testing.T) {
	dag := newDAG(map[string][]string{
		"library-config.greet":      {"resource.flourish"},
		"action.solo":               {"library-config.greet"},
		"action.many":               {"library-config.greet"},
		"resource.web/action.inner": {"library-config.greet"},
		"resource.flourish":         nil,
	})
	steps := []PlanStep{
		{Address: "resource.flourish", Kind: NodeResource, Decision: DecisionCreate},
		{Address: "library-config.greet", Kind: NodeLibraryConfig, Decision: DecisionEval},
		{Address: "action.solo", Kind: NodeAction, Decision: DecisionCreate},
		{Address: "action.many['k1']", Kind: NodeAction, Decision: DecisionCreate},
		{Address: "action.many['k2']", Kind: NodeAction, Decision: DecisionCreate},
		{
			Address:  "resource.web/action.inner",
			Kind:     NodeAction,
			Decision: DecisionCreate,
		},
	}
	addrs := make([]string, len(steps))
	for i := range steps {
		addrs[i] = steps[i].Address
	}
	g := buildStepGraphFromAddresses(addrs, dag)
	got := persistedDependsOn(g, steps)
	assert.Equal(t, map[string][]string{
		"action.solo":       {"resource.flourish"},
		"action.many['k1']": {"resource.flourish"},
		"action.many['k2']": {"resource.flourish"},
		"resource.web/action.inner": {
			"resource.flourish",
		},
	}, got)
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
	for range 20 {
		got := persistedDependsOn(&stepGraph{dependents: dependents}, steps)
		assert.Equal(t, want, got)
	}
}
