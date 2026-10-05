package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

var planGraphBenchmarkNodes []StepNode

func BenchmarkPlanGraphPairedInstances(b *testing.B) {
	for _, count := range []int{100, 250, 500, 1000, 2000} {
		b.Run(fmt.Sprintf("pairs=%d", count), func(b *testing.B) {
			benchmarkInstancePlanGraph(b, count, 0, false)
		})
	}
}

func BenchmarkPlanGraphNestedInstances(b *testing.B) {
	for _, depth := range []int{1, 3, 6} {
		b.Run(fmt.Sprintf("pairs=100/depth=%d", depth), func(b *testing.B) {
			benchmarkInstancePlanGraph(b, 100, depth, false)
		})
	}
}

func BenchmarkPlanGraphCartesianInstances(b *testing.B) {
	for _, count := range []int{10, 25, 50, 100} {
		b.Run(fmt.Sprintf("instances=%d", count), func(b *testing.B) {
			benchmarkInstancePlanGraph(b, count, 0, true)
		})
	}
}

func benchmarkInstancePlanGraph(b *testing.B, count, depth int, cartesian bool) {
	b.Helper()
	fixture := "pair-key"
	if cartesian {
		fixture = "cartesian"
	}
	parsed := syntaxDAG(b, ubtest.ReadValidFixture(b, "testdata/ub/apply-graph", fixture), nil)
	levels := max(depth-1, 0)
	var declaration strings.Builder
	for level := range levels {
		fmt.Fprintf(&declaration, "resource.scope-%d/", level)
	}
	nodes := make(map[string]*Node, 2)
	for _, name := range []string{"nodes", "vols"} {
		original := parsed.Nodes["resource."+name]
		require.NotNil(b, original)
		node := *original
		node.Address = declaration.String() + original.Address
		node.Composite = strings.TrimSuffix(declaration.String(), "/")
		nodes[node.Address] = &node
	}
	source := declaration.String() + "resource.nodes"
	target := declaration.String() + "resource.vols"
	dag := &DAG{Nodes: nodes, Edges: map[string][]string{source: nil, target: {source}}}
	plan := &PlanFile{Steps: make([]PlanStep, count*2)}
	for i := range count {
		key := fmt.Sprintf("key-%05d", i)
		var prefix strings.Builder
		for level := range levels {
			prefix.WriteString(instanceAddress(fmt.Sprintf("resource.scope-%d", level), key))
			prefix.WriteByte('/')
		}
		from := instanceAddress(prefix.String()+"resource.nodes", key)
		to := instanceAddress(prefix.String()+"resource.vols", key)
		plan.Steps[i] = PlanStep{Address: from, Kind: NodeResource, Decision: DecisionNoOp}
		plan.Steps[count+i] = PlanStep{Address: to, Kind: NodeResource, Decision: DecisionNoOp}
	}
	for b.Loop() {
		planGraphBenchmarkNodes = PlanGraph(plan, dag)
	}
	actual := planGraphBenchmarkNodes
	require.Len(b, actual, count*2)
	allSources := make([]string, count)
	for i := range count {
		allSources[i] = plan.Steps[i].Address
	}
	for i := range count {
		require.Equal(b, plan.Steps[i].Address, actual[i].Address)
		require.Empty(b, actual[i].DependsOn)
		require.Equal(b, plan.Steps[count+i].Address, actual[count+i].Address)
		expected := []string{allSources[i]}
		if cartesian {
			expected = allSources
		}
		require.Equal(b, expected, actual[count+i].DependsOn)
	}
	edges := count
	if cartesian {
		edges *= count
	}
	b.ReportMetric(float64(count*2), "steps")
	b.ReportMetric(float64(edges), "edges")
}
