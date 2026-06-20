package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/stretchr/testify/require"
)

type syntaxRuntimeFixture struct {
	body syntax.FactoryBody
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
	require.Empty(t, extractSyntaxTestNodes(t, `description: 'nothing here'`, nil))
}

func TestExtractNodesResources(t *testing.T) {
	src := `
resources: {
  main:   aws.vpc { cidr-block: '10.0.0.0/16' }
  backup: aws.vpc { cidr-block: '10.1.0.0/16' }
  web:    aws.security-group { name: 'web' }
}
`
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
	src := `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
data:      { ubuntu: aws.ami { most-recent: true } }
actions:   { hello: core.command { argv: ['echo'] } }
outputs:   { vpc-id: { value: resource.main.id }, static: { value: 'literal' } }
`
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
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  library-configs: { std: { prefix: resource.hello.path } }
  resources: {
    hello: std.fs-file { path: '/tmp/hello' }
    selected: std.fs-file { path: resource.hello.path }
  }
  data: { lookup: std.file-info { path: resource.selected.path } }
  actions: { show: std.exec-command { argv: ['echo', data.lookup.path] } }
  outputs: { path: { value: resource.selected.path } }
}
`)

	got := BuildSyntaxDAG(fixture.body, nil)
	require.Contains(t, got.Nodes, "library-config.std")
	require.Contains(t, got.Nodes, "resource.hello")
	require.Contains(t, got.Nodes, "resource.selected")
	require.Contains(t, got.Nodes, "data.lookup")
	require.Contains(t, got.Nodes, "action.show")
	require.Contains(t, got.Nodes, "output.path")
}

func TestExtractSyntaxNodesMatchesCompositeDAG(t *testing.T) {
	fixture := parseSyntaxCompositeFixture(t, `
greeting: resource {
  inputs: { path: { type: string } }
  locals: { target: var.path }
  resources: { file: local.fs-file { path: local.target } }
  outputs: { path: { value: resource.file.path } }
}
`)

	got := BuildSyntaxDAG(fixture.body, nil)
	require.Contains(t, got.Nodes, "resource.file")
	require.Contains(t, got.Nodes, "output.path")
	require.Contains(t, got.Edges["resource.file"], "var.path")
}

func TestExtractSyntaxNodesMatchesNestedCompositeDAG(t *testing.T) {
	cluster := parseSyntaxCompositeFixture(t, `
cluster: resource {
  inputs: { path: { type: string } }
  resources: { file: local.fs-file { path: var.path } }
}
`)
	layer := parseSyntaxCompositeFixture(t, `
layer: resource {
  inputs: { target: { type: string } }
  resources: { only: inner.cluster { path: var.target } }
}
`)
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    seed: local.fs-file { path: '/tmp/seed' }
    app: outer.layer { target: resource.seed.path }
  }
}
`)
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
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  locals: { target: resource.helper.path }
  resources: {
    helper: local.fs-file { path: '/tmp/helper' }
    file: local.fs-file { path: local.target }
  }
}
`)
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    app: outer.greeting { path: '/tmp/app' }
  }
}
`)
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
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  library-configs: { std: { path: '/tmp/app' } }
  resources: { app: std.fs-file { path: '/tmp/app' } }
}
`)

	got := BuildSyntaxDAG(fixture.body, nil)
	cfg := got.Nodes["library-config.std"]
	require.NotNil(t, cfg)
	require.Equal(t, NodeLibraryConfig, cfg.Kind)
	require.Equal(t, "std", cfg.Alias)
	require.Equal(t, "std", cfg.Name)
}

func TestExtractNodesOutputBody(t *testing.T) {
	src := `
outputs: {
  vpc-id: { value: resource.main.id }
}
`
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.IsType(t, &lang.DotPath{}, got[0].Body)
}

func TestExtractNodesResourceBody(t *testing.T) {
	src := `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
`
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	body, ok := got[0].Body.(*lang.ObjectLit)
	require.True(t, ok)
	require.Len(t, body.Fields, 1)
	require.Equal(t, "cidr-block", body.Fields[0].Key.Name)
}

func TestExtractNodesExpandsComposite(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", `
resources: { greeting: local.file { path: 'hello.txt', content: 'hi' } }
outputs:   { greeting-path: { value: resource.greeting.path } }
`)
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": composite,
			},
		},
	}
	src := `
resources: { web: net.cluster { name: 'web' } }
`
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
	composite := syntaxResourceComposite(t, "t", `
resources: { x: local.file { path: 'x.txt' } }
outputs:   { path: { value: resource.x.path } }
`)
	libs := map[string]*Library{
		"m": {
			Name:               "m",
			ResourceComposites: map[string]*CompositeType{"t": composite},
		},
	}
	src := `
resources: { one: m.t {} }
`
	got := extractSyntaxTestNodes(t, src, libs)
	for _, n := range got {
		require.NotEqual(t, NodeOutput, n.Kind,
			"output node should not become a DAG node; got %q", n.Address)
	}
}

func TestExtractNodesNestedComposite(t *testing.T) {
	// clusterBody is the body for the cluster composite registered under
	// library alias inner-lib.
	clusterBody := syntaxResourceComposite(t, "cluster", `
inputs: { path: { type: string } }

resources: { x: local.file { path: var.path } }

outputs: { path: { value: resource.x.path } }
`)
	// layerBody is the body for the layer composite registered under
	// library alias outer-lib. It calls inner-lib.cluster.
	layerBody := syntaxResourceComposite(t, "layer", `
inputs: { target: { type: string } }

resources: { only: inner-lib.cluster { path: var.target } }

outputs: { path: { value: resource.only.path } }
`)
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
	src := `
resources: { mine: outer-lib.layer { target: '/tmp/x' } }
`
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
	src := `
resources: { nodes: aws.instance { @for-each: var.configs, instance-type: @each.value.size } }
`
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.Equal(t, "resource.nodes", got[0].Address)
	require.NotNil(t, got[0].ForEach, "@for-each iterable captured on the node")
	dp, ok := got[0].ForEach.(*lang.DotPath)
	require.True(t, ok, "iterable is a DotPath")
	require.Equal(t, "var", dp.Root.Name)
	require.Equal(t, "configs", dp.Segments[0].Name)
}

func TestExtractNodesActionForEach(t *testing.T) {
	src := `
actions: { many: core.command { @for-each: var.targets, argv: ['echo', @each.value] } }
`
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.Equal(t, "action.many", got[0].Address)
	require.NotNil(t, got[0].ForEach)
}

func TestExtractNodesNoForEachLeavesFieldNil(t *testing.T) {
	src := `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
`
	got := extractSyntaxTestNodes(t, src, nil)
	require.Len(t, got, 1)
	require.Nil(t, got[0].ForEach)
}

func TestExtractNodesSkipsMalformed(t *testing.T) {
	src := `factory: {
resources: { aws: 'not an object', web: net.real { size: 3 } }
}`
	_, err := syntax.ParseSource("factory.ub", []byte(src))
	require.Error(t, err)
	require.Contains(t, err.Error(), "resource must be written as name: alias.export { ... }")
}

func TestExtractNodesReadsLibraryConfigAlias(t *testing.T) {
	src := `
library-configs: { aws: { region: 'us-east-1' } }
resources: { web: aws.instance { ami: 'ami-1' } }
data:    { ubuntu: aws.ami { most-recent: true } }
actions: { probe: core.command { argv: ['echo'] } }
`
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
	composite := syntaxComposite(t, "lookup", NodeData, `
data:    { ubuntu: aws.ami { most-recent: true } }
outputs: { id: { value: data.ubuntu.id } }
`)
	libs := map[string]*Library{
		"img": {
			Name: "img",
			DataComposites: map[string]*CompositeType{
				"lookup": composite,
			},
		},
	}
	src := `
data: { latest: img.lookup { family: 'ubuntu' } }
`
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
	composite := syntaxComposite(t, "deploy", NodeAction, `
actions: { run: core.command { argv: ['echo'] } }
`)
	libs := map[string]*Library{
		"ops": {
			Name: "ops",
			ActionComposites: map[string]*CompositeType{
				"deploy": composite,
			},
		},
	}
	src := `
actions: { go: ops.deploy { target: 'prod' } }
`
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
