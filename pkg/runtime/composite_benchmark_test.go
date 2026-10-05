package runtime

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

var compositeBenchmarkDAG *DAG

func BenchmarkBuildSyntaxDAGComposites(b *testing.B) {
	for _, count := range []int{100, 500, 1000, 2000, 4000, 8000} {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			body, libraries := compositeBenchmarkSource(b, count, 1, "factory")
			for b.Loop() {
				compositeBenchmarkDAG = BuildSyntaxDAG(body, libraries)
			}
			graph := compositeBenchmarkDAG
			require.Len(b, graph.Nodes, count*2)
			for _, decl := range body.Resources {
				boundary := "resource." + decl.Name.Name
				leaf := boundary + "/resource.leaf"
				require.True(b, graph.Nodes[boundary].IsComposite())
				require.Equal(b, boundary, graph.Nodes[leaf].Composite)
				require.Equal(b, []string{leaf}, graph.Edges[boundary])
				require.Empty(b, graph.Edges[leaf])
			}
			b.ReportMetric(float64(count*2), "nodes")
			b.ReportMetric(float64(count), "edges")
		})
	}
}

func BenchmarkCompositeInternalsInOrder(b *testing.B) {
	const branches = 100
	for _, depth := range []int{1, 3, 6} {
		b.Run(fmt.Sprintf("branches=%d/depth=%d", branches, depth), func(b *testing.B) {
			body, libraries := compositeBenchmarkSource(b, branches, depth, "factory-each")
			graph := BuildSyntaxDAG(body, libraries)
			order, err := graph.TopologicalOrder()
			require.NoError(b, err)
			executor := &Executor{DAG: graph}
			internals := make([][]*Node, branches)
			for b.Loop() {
				run := &runState{order: order}
				for i, decl := range body.Resources {
					internals[i] = executor.compositeInternalsInOrder(run, "resource."+decl.Name.Name)
				}
			}
			for i, decl := range body.Resources {
				var parent strings.Builder
				parent.WriteString("resource." + decl.Name.Name)
				var expected []string
				for range depth - 1 {
					parent.WriteString("/resource.nested")
					expected = append(expected, parent.String())
				}
				expected = append(expected, parent.String()+"/resource.leaf")
				slices.Reverse(expected)
				actual := make([]string, len(internals[i]))
				for j, node := range internals[i] {
					actual[j] = node.Address
					require.True(b, graph.UnderForEachComposite(node))
				}
				require.Equal(b, expected, actual)
			}
			b.ReportMetric(float64(branches*(depth+1)), "nodes")
			b.ReportMetric(float64(branches*depth), "edges")
			b.ReportMetric(float64(branches*depth), "visited")
		})
	}
}

func compositeBenchmarkSource(
	b *testing.B, count, depth int, factory string,
) (syntax.FactoryBody, map[string]*Library) {
	b.Helper()
	const fixtures = "testdata/ub/composite-benchmark"
	leaf := parseSyntaxCompositeFixture(b, ubtest.ReadValidFixture(b, fixtures, "leaf")).body
	library := &Library{ResourceComposites: map[string]*CompositeType{
		"box": {Name: "box", Kind: NodeResource, SyntaxBody: &leaf},
	}}
	for range depth - 1 {
		nested := parseSyntaxCompositeFixture(b, ubtest.ReadValidFixture(b, fixtures, "nested")).body
		library = &Library{ResourceComposites: map[string]*CompositeType{
			"box": {
				Name: "box", Kind: NodeResource, SyntaxBody: &nested,
				Libraries: map[string]*Library{"boxes": library},
			},
		}}
	}
	body := parseSyntaxFactoryFixture(b, ubtest.ReadValidFixture(b, fixtures, factory)).body
	decl := body.Resources[0]
	body.Resources = make([]syntax.NodeDecl, count)
	for i := range count {
		body.Resources[i] = decl
		body.Resources[i].Name.Name = fmt.Sprintf("box-%05d", i)
	}
	return body, map[string]*Library{"boxes": library}
}
