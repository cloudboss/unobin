package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func TestStepInstancesParseAddresses(t *testing.T) {
	addresses := []string{
		"resource.box['a/b']/resource.nodes['leaf']",
		"resource.box['a\\'b']/resource.nodes['other']",
		"resource.plain",
		"output.value",
		"resource.nodes['broken",
	}
	index := indexStepInstances(addresses)
	declarations := make([]string, len(index.instances))
	keys := make([][]keyPosition, len(index.instances))
	for i := range index.instances {
		require.Equal(t, addresses[i], index.instances[i].Address)
		declarations[i] = index.instances[i].DeclarationAddress
		keys[i] = index.instances[i].keys
	}
	require.Equal(t, []string{
		"resource.box/resource.nodes", "resource.box/resource.nodes",
		"resource.plain", "output.value", "resource.nodes['broken",
	}, declarations)
	require.Equal(t, [][]keyPosition{
		{{at: "resource.box", key: "a/b"}, {at: "resource.box/resource.nodes", key: "leaf"}},
		{{at: "resource.box", key: "a'b"}, {at: "resource.box/resource.nodes", key: "other"}},
		nil, nil, nil,
	}, keys)
	group := index.declarations["resource.box/resource.nodes"]
	groupAddresses := make([]string, len(group))
	for i := range group {
		groupAddresses[i] = group[i].Address
	}
	require.Equal(t, addresses[:2], groupAddresses)
	require.Empty(t, indexStepInstances(nil).instances)
}

func TestInstanceGraphRebuildsAfterBodyChanges(t *testing.T) {
	dag := syntaxDAG(t, ubtest.ReadValidFixture(t, "testdata/ub/apply-graph", "pair-key"), nil)
	plan := &PlanFile{Steps: []PlanStep{
		{Address: "resource.nodes['alpha']", Kind: NodeResource, Decision: DecisionNoOp},
		{Address: "resource.nodes['beta']", Kind: NodeResource, Decision: DecisionNoOp},
		{Address: "resource.vols['alpha']", Kind: NodeResource, Decision: DecisionNoOp},
	}}
	first := PlanGraph(plan, dag)
	require.Equal(t, []string{"resource.nodes['alpha']"}, first[2].DependsOn)
	dag.Nodes["resource.vols"].Body = nil
	second := PlanGraph(plan, dag)
	require.Equal(t, []string{
		"resource.nodes['alpha']", "resource.nodes['beta']",
	}, second[2].DependsOn)
}

func TestInstanceGraphPreservesKeyConstraints(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		dep      string
		targets  []string
		paired   bool
		expected []string
	}{
		{
			name:   "missing shared key stays eligible",
			source: "resource.web['alpha']/resource.vols", dep: "resource.web/resource.nodes",
			targets: []string{
				"resource.web/resource.nodes['front']", "resource.web['beta']/resource.nodes",
				"resource.web['alpha']/resource.nodes",
				"resource.web/resource.nodes['back']", "resource.web/resource.nodes",
			},
			expected: []string{
				"resource.web/resource.nodes['front']", "resource.web['alpha']/resource.nodes",
				"resource.web/resource.nodes['back']", "resource.web/resource.nodes",
			},
		},
		{
			name:   "one key can match at any dependency position",
			source: "resource.vols['alpha']", dep: "resource.web/resource.nodes", paired: true,
			targets: []string{
				"resource.web['alpha']/resource.nodes['alpha']",
				"resource.web['beta']/resource.nodes['alpha']",
				"resource.web['alpha']/resource.nodes['beta']",
				"resource.web['beta']/resource.nodes['beta']",
				"resource.web/resource.nodes",
			},
			expected: []string{
				"resource.web['alpha']/resource.nodes['alpha']",
				"resource.web['beta']/resource.nodes['alpha']",
				"resource.web['alpha']/resource.nodes['beta']",
			},
		},
		{
			name:   "several levels retain conservative matching",
			source: "resource.web['alpha']/resource.vols['x']",
			dep:    "resource.web/resource.nodes", paired: true,
			targets: []string{
				"resource.web['alpha']/resource.nodes['y']",
				"resource.web['beta']/resource.nodes['x']",
			},
			expected: []string{"resource.web['alpha']/resource.nodes['y']"},
		},
		{
			name:   "remaining key constraints filter the smallest candidate set",
			source: "resource.web['alpha']/resource.group['red']/resource.vols['x']",
			dep:    "resource.web/resource.group/resource.nodes",
			targets: []string{
				"resource.web['alpha']/resource.group['blue']/resource.nodes['x']",
				"resource.web['beta']/resource.group['red']/resource.nodes['x']",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			declaration := declarationAddress(tt.source)
			dag := newDAG(map[string][]string{declaration: {tt.dep}, tt.dep: nil})
			addresses := append(append([]string(nil), tt.targets...), tt.source)
			pairs := map[string]map[string]bool{}
			if tt.paired {
				pairs[tt.source] = map[string]bool{tt.dep: true}
			}
			graph := buildStepGraphWithPairKey(addresses, dag, pairs, nil)
			require.Equal(t, len(tt.expected), graph.indegree[tt.source])
			var dependencies []string
			for _, address := range tt.targets {
				if len(graph.dependents[address]) != 0 {
					require.Equal(t, []string{tt.source}, graph.dependents[address])
					dependencies = append(dependencies, address)
				}
			}
			require.Equal(t, tt.expected, dependencies)
		})
	}
}
