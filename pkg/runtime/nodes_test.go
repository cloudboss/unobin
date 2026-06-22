package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/ubtest"
	"github.com/stretchr/testify/require"
)

type syntaxRuntimeFixture struct {
	body syntax.FactoryBody
}

func nodeFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/nodes", name)
}

func nodeInvalidFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadFixture(t, "testdata/ub/nodes/invalid/"+name+".ub")
}

func parseSyntaxFactoryFixture(t *testing.T, src string) syntaxRuntimeFixture {
	t.Helper()
	f, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, syntax.FileFactory, f.Kind)
	require.NotNil(t, f.Factory)
	return syntaxRuntimeFixture{body: f.Factory.Body}
}

func parseSyntaxCompositeFixture(t *testing.T, src string) syntaxRuntimeFixture {
	t.Helper()
	f, err := syntax.ParseSource("library.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, syntax.FileLibrary, f.Kind)
	require.NotNil(t, f.Library)
	require.Len(t, f.Library.Exports, 1)
	body := f.Library.Exports[0].Body
	return syntaxRuntimeFixture{body: body}
}

func extractSyntaxTestNodes(
	t *testing.T,
	src string,
	libs map[string]*Library,
) []*Node {
	t.Helper()
	return extractSyntaxNodes(syntaxFactoryBody(t, src), "", libs)
}

func TestExtractNodesEmpty(t *testing.T) {
	require.Empty(t, extractSyntaxTestNodes(t, nodeFixture(t, "extract-nodes-empty"), nil))
}

func TestExtractNodesResources(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-resources")
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 3)

	require.Equal(t, "resource.main", got[0].Address)
	require.Equal(t, NodeResource, got[0].Kind)
	require.Equal(t, "aws", got[0].Alias)
	require.Equal(t, "vpc", got[0].Type)
	require.Equal(t, "main", got[0].Name)

	require.Equal(t, "resource.backup", got[1].Address)
	require.Equal(t, "resource.web", got[2].Address)
}

func TestExtractNodesAllKinds(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-all-kinds")
	got := extractSyntaxTestNodes(t, src, nil)
	addresses := make([]string, len(got))
	for i, n := range got {
		addresses[i] = n.Address
	}
	require.Equal(t, []string{
		"resource.main",
		"data.ubuntu",
		"action.hello",
		"output.vpc-id",
		"output.static",
	}, addresses)

	require.Equal(t, NodeResource, got[0].Kind)
	require.Equal(t, NodeData, got[1].Kind)
	require.Equal(t, NodeAction, got[2].Kind)
	require.Equal(t, NodeOutput, got[3].Kind)
}

func TestExtractSyntaxNodesMatchesFactoryDAG(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, nodeFixture(t, "extract-syntax-nodes-matches-factory-dag"))

	got := BuildSyntaxDAG(fixture.body, nil)
	require.Contains(t, got.Nodes, "library-config.std")
	require.Contains(t, got.Nodes, "resource.hello")
	require.Contains(t, got.Nodes, "resource.selected")
	require.Contains(t, got.Nodes, "data.lookup")
	require.Contains(t, got.Nodes, "action.show")
	require.Contains(t, got.Nodes, "output.path")
}

func TestExtractSyntaxNodesMatchesCompositeDAG(t *testing.T) {
	fixture := parseSyntaxCompositeFixture(t, nodeFixture(t, "composite-dag"))

	got := BuildSyntaxDAG(fixture.body, nil)
	require.Contains(t, got.Nodes, "resource.file")
	require.Contains(t, got.Nodes, "output.path")
	require.Contains(t, got.Edges["resource.file"], "input.path")
}

func TestExtractSyntaxNodesMatchesNestedCompositeDAG(t *testing.T) {
	cluster := parseSyntaxCompositeFixture(t, nodeFixture(t, "nested-dag-cluster"))
	layer := parseSyntaxCompositeFixture(t, nodeFixture(t, "nested-dag-layer"))
	fixture := parseSyntaxFactoryFixture(t, nodeFixture(t, "nested-dag-factory"))
	layerBody := layer.body
	clusterBody := cluster.body
	libs := map[string]*Library{
		"outer": {
			Name: "outer",
			ResourceComposites: map[string]*CompositeType{
				"layer": {Name: "layer", SyntaxBody: &layerBody},
			},
		},
		"inner": {
			Name: "inner",
			ResourceComposites: map[string]*CompositeType{
				"cluster": {Name: "cluster", SyntaxBody: &clusterBody},
			},
		},
	}

	got := BuildSyntaxDAG(fixture.body, libs)
	require.Contains(t, got.Nodes, "resource.seed")
	require.Contains(t, got.Nodes, "resource.app")
	require.Contains(t, got.Nodes, "resource.app/resource.only")
	require.Contains(t, got.Nodes, "resource.app/resource.only/resource.file")
}

func TestExtractSyntaxNodesUsesCompositeSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, nodeFixture(t, "composite-syntax-body"))
	fixture := parseSyntaxFactoryFixture(t, nodeFixture(t, "composite-syntax-call"))
	body := composite.body
	libs := map[string]*Library{
		"outer": {
			Name: "outer",
			ResourceComposites: map[string]*CompositeType{
				"greeting": {Name: "greeting", SyntaxBody: &body},
			},
		},
	}

	got := BuildSyntaxDAG(fixture.body, libs)
	node := got.Nodes["resource.app/resource.file"]
	require.NotNil(t, node)
	require.Equal(t, "local", node.Alias)
	require.Equal(t, "fs-file", node.Type)
	require.Equal(t, "file", node.Name)
	require.Contains(t, got.Edges["resource.app/resource.file"], "resource.app/resource.helper")
	require.Contains(t, got.Edges["resource.app"], "resource.app/resource.file")
}

func TestExtractSyntaxNodesReadsLibraryConfig(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, nodeFixture(t, "library-config-node"))

	got := BuildSyntaxDAG(fixture.body, nil)
	cfg := got.Nodes["library-config.std"]
	require.NotNil(t, cfg)
	require.Equal(t, NodeLibraryConfig, cfg.Kind)
	require.Equal(t, "std", cfg.Alias)
	require.Equal(t, "std", cfg.Name)
}

func TestExtractNodesOutputBody(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-output-body")
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.IsType(t, &lang.DotPath{}, got[0].Body)
}

func TestExtractNodesResourceBody(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-resource-body")
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	body, ok := got[0].Body.(*lang.ObjectLit)
	require.True(t, ok)
	require.Len(t, body.Fields, 1)
	require.Equal(t, "cidr-block", body.Fields[0].Key.Name)
}

func TestExtractNodesExpandsComposite(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", nodeFixture(t, "expand-composite-body"))
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": composite,
			},
		},
	}
	src := nodeFixture(t, "expand-composite-call")
	got := extractSyntaxTestNodes(t, src, libs)
	require.Len(t, got, 2)

	require.Equal(t, "resource.web", got[0].Address)
	require.True(t, got[0].IsComposite())
	require.Equal(t, NodeResource, got[0].Kind)
	require.Same(t, composite.SyntaxBody, got[0].CompositeSyntaxBody)
	require.Empty(t, got[0].Composite)

	require.Equal(t, "resource.web/resource.greeting", got[1].Address)
	require.Equal(t, NodeResource, got[1].Kind)
	require.Equal(t, "resource.web", got[1].Composite)
	require.Equal(t, "local", got[1].Alias)
	require.Equal(t, "file", got[1].Type)
	require.Equal(t, "greeting", got[1].Name)
}

func TestExtractNodesCompositeDropsInternalOutputs(t *testing.T) {
	composite := syntaxResourceComposite(t, "t", nodeFixture(t, "composite-internal-outputs-body"))
	libs := map[string]*Library{
		"m": {
			Name:               "m",
			ResourceComposites: map[string]*CompositeType{"t": composite},
		},
	}
	src := nodeFixture(t, "composite-internal-outputs-call")
	got := extractSyntaxTestNodes(t, src, libs)
	for _, n := range got {
		require.NotEqual(t, NodeOutput, n.Kind,
			"output node should not become a DAG node; got %q", n.Address)
	}
}

func TestExtractNodesNestedComposite(t *testing.T) {
	// clusterBody is the body for the cluster composite registered under
	// library alias inner-lib.
	clusterBody := syntaxResourceComposite(t, "cluster", nodeFixture(t, "nested-extract-cluster"))
	// layerBody is the body for the layer composite registered under
	// library alias outer-lib. It calls inner-lib.cluster.
	layerBody := syntaxResourceComposite(t, "layer", nodeFixture(t, "nested-extract-layer"))
	libs := map[string]*Library{
		"outer-lib": {
			Name: "outer-lib",
			ResourceComposites: map[string]*CompositeType{
				"layer": layerBody,
			},
		},
		"inner-lib": {
			Name: "inner-lib",
			ResourceComposites: map[string]*CompositeType{
				"cluster": clusterBody,
			},
		},
	}
	src := nodeFixture(t, "nested-extract-call")
	got := extractSyntaxTestNodes(t, src, libs)

	byAddr := map[string]*Node{}
	for _, n := range got {
		byAddr[n.Address] = n
	}

	outerBoundary := byAddr["resource.mine"]
	require.NotNil(t, outerBoundary, "outer boundary at root address")
	require.True(t, outerBoundary.IsComposite())
	require.Equal(t, NodeResource, outerBoundary.Kind)
	require.Empty(t, outerBoundary.Composite, "outer boundary has root scope")

	innerBoundary := byAddr["resource.mine/resource.only"]
	require.NotNil(t, innerBoundary, "inner boundary nested under outer")
	require.True(t, innerBoundary.IsComposite())
	require.Equal(t, NodeResource, innerBoundary.Kind)
	require.Equal(t, "resource.mine", innerBoundary.Composite,
		"inner boundary's direct parent is outer call site")

	leafAddr := "resource.mine/resource.only/resource.x"
	leaf := byAddr[leafAddr]
	require.NotNil(t, leaf, "leaf under inner composite")
	require.Equal(t, NodeResource, leaf.Kind)
	require.Equal(t, "resource.mine/resource.only", leaf.Composite,
		"leaf's direct parent is inner call site")
}

func TestExtractNodesResourceForEach(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-resource-for-each")
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.Equal(t, "resource.nodes", got[0].Address)
	require.NotNil(t, got[0].ForEach, "@for-each iterable captured on the node")
	dp, ok := got[0].ForEach.(*lang.DotPath)
	require.True(t, ok, "iterable is a DotPath")
	require.Equal(t, "input", dp.Root.Name)
	require.Equal(t, "configs", dp.Segments[0].Name)
}

func TestExtractNodesActionForEach(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-action-for-each")
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.Equal(t, "action.many", got[0].Address)
	require.NotNil(t, got[0].ForEach)
}

func TestExtractNodesNoForEachLeavesFieldNil(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-no-for-each-leaves-field-nil")
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.Nil(t, got[0].ForEach)
}

func TestExtractNodesSkipsMalformed(t *testing.T) {
	src := nodeInvalidFixture(t, "malformed-resource")
	_, err := syntax.ParseSource("factory.ub", []byte(src))
	require.Error(t, err)
	require.Contains(t, err.Error(), "resource must be written as name: alias.export { ... }")
}

func TestExtractNodesReadsLibraryConfigAlias(t *testing.T) {
	src := nodeFixture(t, "extract-nodes-reads-library-config-alias")
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 4)

	require.Equal(t, "resource.web", got[0].Address)
	require.Equal(t, "data.ubuntu", got[1].Address)
	require.Equal(t, "action.probe", got[2].Address)
	require.Equal(t, "library-config.aws", got[3].Address)
	require.Equal(t, NodeLibraryConfig, got[3].Kind)
	require.Equal(t, "aws", got[3].Alias)
}

func TestExtractNodesExpandsDataComposite(t *testing.T) {
	composite := syntaxComposite(t, "lookup", NodeData, nodeFixture(t, "data-composite-body"))
	libs := map[string]*Library{
		"img": {
			Name: "img",
			DataComposites: map[string]*CompositeType{
				"lookup": composite,
			},
		},
	}
	src := nodeFixture(t, "data-composite-call")
	got := extractSyntaxTestNodes(t, src, libs)
	require.Len(t, got, 2)

	require.Equal(t, "data.latest", got[0].Address)
	require.True(t, got[0].IsComposite())
	require.Equal(t, NodeData, got[0].Kind)
	require.Same(t, composite.SyntaxBody, got[0].CompositeSyntaxBody)
	require.Empty(t, got[0].Composite)

	require.Equal(t, "data.latest/data.ubuntu", got[1].Address)
	require.Equal(t, NodeData, got[1].Kind)
	require.Equal(t, "data.latest", got[1].Composite)
}

func TestExtractNodesExpandsActionComposite(t *testing.T) {
	composite := syntaxComposite(t, "deploy", NodeAction, nodeFixture(t, "action-composite-body"))
	libs := map[string]*Library{
		"ops": {
			Name: "ops",
			ActionComposites: map[string]*CompositeType{
				"deploy": composite,
			},
		},
	}
	src := nodeFixture(t, "action-composite-call")
	got := extractSyntaxTestNodes(t, src, libs)
	require.Len(t, got, 2)

	require.Equal(t, "action.go", got[0].Address)
	require.True(t, got[0].IsComposite())
	require.Equal(t, NodeAction, got[0].Kind)
	require.Same(t, composite.SyntaxBody, got[0].CompositeSyntaxBody)
	require.Empty(t, got[0].Composite)

	require.Equal(t, "action.go/action.run", got[1].Address)
	require.Equal(t, NodeAction, got[1].Kind)
	require.Equal(t, "action.go", got[1].Composite)
}
