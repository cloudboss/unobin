package runtime

import (
	"slices"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/stretchr/testify/require"
)

func parseStack(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	return f
}

type syntaxRuntimeFixture struct {
	body syntax.FactoryBody
	file *lang.File
}

func parseSyntaxFactory(t *testing.T, src string) *lang.File {
	t.Helper()
	return parseSyntaxFactoryFixture(t, src).file
}

func parseSyntaxFactoryFixture(t *testing.T, src string) syntaxRuntimeFixture {
	t.Helper()
	f, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, syntax.FileFactory, f.Kind)
	require.NotNil(t, f.Factory)
	return syntaxRuntimeFixture{
		body: f.Factory.Body,
		file: syntaxBodyFile(f.S, lang.FileFactory, f.Path, f.Factory.Body, f.Comments),
	}
}

func parseSyntaxCompositeFixture(t *testing.T, src string) syntaxRuntimeFixture {
	t.Helper()
	f, err := syntax.ParseSource("library.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, syntax.FileLibrary, f.Kind)
	require.NotNil(t, f.Library)
	require.Len(t, f.Library.Exports, 1)
	body := f.Library.Exports[0].Body
	return syntaxRuntimeFixture{
		body: body,
		file: syntaxBodyFile(f.Library.Exports[0].S, lang.FileExportedType, f.Path, body, f.Comments),
	}
}

func syntaxBodyFile(
	span lang.Span,
	kind lang.FileKind,
	path string,
	body syntax.FactoryBody,
	comments []lang.Comment,
) *lang.File {
	return &lang.File{
		S:        span,
		Kind:     kind,
		Path:     path,
		Body:     syntax.RuntimeFactoryBodyObject(body),
		Comments: comments,
	}
}

type dagNodeSummary struct {
	Address       string
	Kind          NodeKind
	Alias         string
	Type          string
	Name          string
	Composite     string
	IsComposite   bool
	Configuration ConfigRef
	Edges         []string
}

func requireSyntaxDAGMatch(t *testing.T, fixture syntaxRuntimeFixture, libs map[string]*Library) {
	t.Helper()
	legacy := BuildDAG(fixture.file, libs)
	typed := BuildSyntaxDAG(fixture.body, libs)
	require.Equal(t, dagSummary(legacy), dagSummary(typed))
}

func dagSummary(g *DAG) []dagNodeSummary {
	addresses := make([]string, 0, len(g.Nodes))
	for addr := range g.Nodes {
		addresses = append(addresses, addr)
	}
	slices.Sort(addresses)
	out := make([]dagNodeSummary, 0, len(addresses))
	for _, addr := range addresses {
		n := g.Nodes[addr]
		edges := slices.Clone(g.Edges[addr])
		slices.Sort(edges)
		out = append(out, dagNodeSummary{
			Address:       n.Address,
			Kind:          n.Kind,
			Alias:         n.Alias,
			Type:          n.Type,
			Name:          n.Name,
			Composite:     n.Composite,
			IsComposite:   n.IsComposite(),
			Configuration: n.Configuration,
			Edges:         edges,
		})
	}
	return out
}

func TestExtractNodesEmpty(t *testing.T) {
	f := parseStack(t, `description: 'nothing here'`)
	require.Empty(t, ExtractNodes(f, nil))
}

func TestExtractNodesResources(t *testing.T) {
	src := `
resources: {
  aws.vpc.main:           { cidr-block: '10.0.0.0/16' }
  aws.vpc.backup:         { cidr-block: '10.1.0.0/16' }
  aws.security-group.web: { name: 'web' }
}
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 3)

	require.Equal(t, "resource.aws.vpc.main", got[0].Address)
	require.Equal(t, NodeResource, got[0].Kind)
	require.Equal(t, "aws", got[0].Alias)
	require.Equal(t, "vpc", got[0].Type)
	require.Equal(t, "main", got[0].Name)

	require.Equal(t, "resource.aws.vpc.backup", got[1].Address)
	require.Equal(t, "resource.aws.security-group.web", got[2].Address)
}

func TestExtractNodesAllKinds(t *testing.T) {
	src := `
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
data:      { aws.ami.ubuntu: { most-recent: true } }
actions:   { core.command.hello: { argv: ['echo'] } }
outputs:   { vpc-id: { value: resource.aws.vpc.main.id }, static: { value: 'literal' } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	addresses := make([]string, len(got))
	for i, n := range got {
		addresses[i] = n.Address
	}
	require.Equal(t, []string{
		"resource.aws.vpc.main",
		"data.aws.ami.ubuntu",
		"action.core.command.hello",
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
  configurations: {
    std { token: var.token }
    formal: std { prefix: resource.hello.path }
  }
  resources: {
    hello: std.fs-file { path: '/tmp/hello' }
    selected: std.fs-file { @configuration: configuration.formal, path: resource.hello.path }
  }
  data: { lookup: std.file-info { path: resource.selected.path } }
  actions: { show: std.exec-command { argv: ['echo', data.lookup.path] } }
  outputs: { path: { value: resource.selected.path } }
}
`)

	requireSyntaxDAGMatch(t, fixture, nil)
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

	requireSyntaxDAGMatch(t, fixture, nil)
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
	libs := map[string]*Library{
		"outer": {
			Name: "outer",
			ResourceComposites: map[string]*CompositeType{
				"layer": {Name: "layer", Body: layer.file},
			},
		},
		"inner": {
			Name: "inner",
			ResourceComposites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: cluster.file},
			},
		},
	}

	requireSyntaxDAGMatch(t, fixture, libs)
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

func TestExtractSyntaxNodesReadsConfigurationRef(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    app: std.fs-file { @configuration: configuration.formal, path: '/tmp/app' }
  }
}
`)

	got := BuildSyntaxDAG(fixture.body, nil)
	require.Equal(t,
		ConfigRef{Alias: "std", Name: "formal"},
		got.Nodes["resource.app"].Configuration)
}

func TestExtractSyntaxNodesIgnoresAliasConfigurationSelection(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    app: std.fs-file { @configuration: std.formal, path: '/tmp/app' }
  }
}
`)

	got := BuildSyntaxDAG(fixture.body, nil)
	require.Empty(t, got.Nodes["resource.app"].Configuration)
}

func TestExtractSyntaxNodesIgnoresAliasConfigurationRemap(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  resources: { file: local.fs-file { path: '/tmp/helper' } }
}
`)
	body := composite.body
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    app: outer.greeting { @configurations: { local: local.formal } }
  }
}
`)
	libs := map[string]*Library{
		"outer": {
			Name: "outer",
			ResourceComposites: map[string]*CompositeType{
				"greeting": {Name: "greeting", SyntaxBody: &body},
			},
		},
	}

	got := BuildSyntaxDAG(fixture.body, libs)
	require.Empty(t, got.Nodes["resource.app"].ConfigurationsRemap)
}

func TestExtractNodesOutputBody(t *testing.T) {
	src := `
outputs: {
  vpc-id: { value: resource.aws.vpc.main.id }
}
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.IsType(t, &lang.DotPath{}, got[0].Body)
}

func TestExtractNodesResourceBody(t *testing.T) {
	src := `
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	body, ok := got[0].Body.(*lang.ObjectLit)
	require.True(t, ok)
	require.Len(t, body.Fields, 1)
	require.Equal(t, "cidr-block", body.Fields[0].Key.Name)
}

func TestExtractNodesExpandsComposite(t *testing.T) {
	composite := parseStack(t, `
resources: { local.file.greeting: { path: 'hello.txt', content: 'hi' } }
outputs:   { greeting-path: { value: resource.local.file.greeting.path } }
`)
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: composite},
			},
		},
	}
	stack := parseStack(t, `
resources: { net.cluster.web: { name: 'web' } }
`)
	got := ExtractNodes(stack, libs)
	require.Len(t, got, 2)

	require.Equal(t, "resource.net.cluster.web", got[0].Address)
	require.True(t, got[0].IsComposite())
	require.Equal(t, NodeResource, got[0].Kind)
	require.Equal(t, composite, got[0].CompositeBody)
	require.Empty(t, got[0].Composite)

	require.Equal(t, "resource.net.cluster.web/resource.local.file.greeting", got[1].Address)
	require.Equal(t, NodeResource, got[1].Kind)
	require.Equal(t, "resource.net.cluster.web", got[1].Composite)
	require.Equal(t, "local", got[1].Alias)
	require.Equal(t, "file", got[1].Type)
	require.Equal(t, "greeting", got[1].Name)
}

func TestExtractNodesCompositeDropsInternalOutputs(t *testing.T) {
	composite := parseStack(t, `
resources: { local.file.x: { path: 'x.txt' } }
outputs:   { path: { value: resource.local.file.x.path } }
`)
	libs := map[string]*Library{
		"m": {
			Name:               "m",
			ResourceComposites: map[string]*CompositeType{"t": {Name: "t", Body: composite}},
		},
	}
	stack := parseStack(t, `
resources: { m.t.one: {} }
`)
	got := ExtractNodes(stack, libs)
	for _, n := range got {
		require.NotEqual(t, NodeOutput, n.Kind,
			"output node should not become a DAG node; got %q", n.Address)
	}
}

func TestExtractNodesNestedComposite(t *testing.T) {
	// clusterBody is the body file for the `cluster` composite type
	// registered under library alias `inner-lib`. In a real project this
	// would live in `cluster.ub`, listed in inner-lib's `library.ub`
	// manifest as `exports: { cluster: 'cluster.ub' }`.
	clusterBody := parseStack(t, `
inputs: { path: { type: string } }

resources: { local.file.x: { path: var.path } }

outputs: { path: { value: resource.local.file.x.path } }
`)
	// layerBody is the body file for the `layer` composite type registered
	// under library alias `outer-lib`. Its body calls inner-lib's `cluster`
	// composite, which is what makes this a nested composite.
	layerBody := parseStack(t, `
inputs: { target: { type: string } }

resources: { inner-lib.cluster.only: { path: var.target } }

outputs: { path: { value: resource.inner-lib.cluster.only.path } }
`)
	libs := map[string]*Library{
		"outer-lib": {
			Name: "outer-lib",
			ResourceComposites: map[string]*CompositeType{
				"layer": {Name: "layer", Body: layerBody},
			},
		},
		"inner-lib": {
			Name: "inner-lib",
			ResourceComposites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: clusterBody},
			},
		},
	}
	stack := parseStack(t, `
resources: { outer-lib.layer.mine: { target: '/tmp/x' } }
`)
	got := ExtractNodes(stack, libs)

	byAddr := map[string]*Node{}
	for _, n := range got {
		byAddr[n.Address] = n
	}

	outerBoundary := byAddr["resource.outer-lib.layer.mine"]
	require.NotNil(t, outerBoundary, "outer boundary at root address")
	require.True(t, outerBoundary.IsComposite())
	require.Equal(t, NodeResource, outerBoundary.Kind)
	require.Empty(t, outerBoundary.Composite, "outer boundary has root scope")

	innerBoundary := byAddr["resource.outer-lib.layer.mine/resource.inner-lib.cluster.only"]
	require.NotNil(t, innerBoundary, "inner boundary nested under outer")
	require.True(t, innerBoundary.IsComposite())
	require.Equal(t, NodeResource, innerBoundary.Kind)
	require.Equal(t, "resource.outer-lib.layer.mine", innerBoundary.Composite,
		"inner boundary's direct parent is outer call site")

	leafAddr := "resource.outer-lib.layer.mine/resource.inner-lib.cluster.only/resource.local.file.x"
	leaf := byAddr[leafAddr]
	require.NotNil(t, leaf, "leaf under inner composite")
	require.Equal(t, NodeResource, leaf.Kind)
	require.Equal(t, "resource.outer-lib.layer.mine/resource.inner-lib.cluster.only", leaf.Composite,
		"leaf's direct parent is inner call site")
}

func TestExtractNodesResourceForEach(t *testing.T) {
	src := `
resources: { aws.instance.nodes: { @for-each: var.configs, instance-type: @each.value.size } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.Equal(t, "resource.aws.instance.nodes", got[0].Address)
	require.NotNil(t, got[0].ForEach, "@for-each iterable captured on the node")
	dp, ok := got[0].ForEach.(*lang.DotPath)
	require.True(t, ok, "iterable is a DotPath")
	require.Equal(t, "var", dp.Root.Name)
	require.Equal(t, "configs", dp.Segments[0].Name)
}

func TestExtractNodesActionForEach(t *testing.T) {
	src := `
actions: { core.command.many: { @for-each: var.targets, argv: ['echo', @each.value] } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.Equal(t, "action.core.command.many", got[0].Address)
	require.NotNil(t, got[0].ForEach)
}

func TestExtractNodesNoForEachLeavesFieldNil(t *testing.T) {
	src := `
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.Nil(t, got[0].ForEach)
}

func TestExtractNodesSkipsMalformed(t *testing.T) {
	src := `
resources: { aws: 'not an object', net.real.web: { size: 3 } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.Equal(t, "resource.net.real.web", got[0].Address)
}

func TestExtractNodesReadsConfigurationAlias(t *testing.T) {
	src := `
resources: {
  aws.instance.web:    { ami: 'ami-1' }
  aws.instance.mirror: { @configuration: configuration.east2, ami: 'ami-2' }
}
data:    { aws.ami.ubuntu: { @configuration: configuration.east2, most-recent: true } }
actions: { core.command.probe: { @configuration: configuration.alt, argv: ['echo'] } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 4)

	require.Equal(t, "resource.aws.instance.web", got[0].Address)
	require.Empty(t, got[0].Configuration)

	require.Equal(t, "resource.aws.instance.mirror", got[1].Address)
	require.Equal(t, ConfigRef{Alias: "aws", Name: "east2"}, got[1].Configuration)

	require.Equal(t, "data.aws.ami.ubuntu", got[2].Address)
	require.Equal(t, ConfigRef{Alias: "aws", Name: "east2"}, got[2].Configuration)

	require.Equal(t, "action.core.command.probe", got[3].Address)
	require.Equal(t, ConfigRef{Alias: "core", Name: "alt"}, got[3].Configuration)
}

func TestExtractCompositeReadsConfigurationsRemap(t *testing.T) {
	src := `
imports:   { net: 'github.com/example/net' }
resources: { net.cluster.east: { @configurations: { aws: configuration.east2 }, name: 'east' } }
`
	composite := &CompositeType{
		Body: parseStack(t, `description: 'noop'`),
	}
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	got := ExtractNodes(parseStack(t, src), libs)
	require.NotEmpty(t, got)
	require.True(t, got[0].IsComposite())
	require.Equal(t, NodeResource, got[0].Kind)
	require.Equal(t,
		map[string]ConfigRef{"aws": {Alias: "aws", Name: "east2"}},
		got[0].ConfigurationsRemap)
}

func TestExtractCompositeReadsSourceConfigurationRemap(t *testing.T) {
	src := `
imports:   { net: 'github.com/example/net' }
resources: { net.cluster.east: { @configurations: { aws: configuration.east2 }, name: 'east' } }
`
	composite := &CompositeType{
		Body: parseStack(t, `description: 'noop'`),
	}
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	got := ExtractNodes(parseStack(t, src), libs)
	require.NotEmpty(t, got)
	require.True(t, got[0].IsComposite())
	require.Equal(t,
		map[string]ConfigRef{"aws": {Alias: "aws", Name: "east2"}},
		got[0].ConfigurationsRemap)
}

func TestExtractConfigurationsRemapIgnoresAliasQualifiedValue(t *testing.T) {
	src := `
resources: { net.cluster.east: { @configurations: { aws: gcp.east2 }, name: 'east' } }
`
	composite := &CompositeType{
		Body: parseStack(t, `description: 'noop'`),
	}
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	got := ExtractNodes(parseStack(t, src), libs)
	require.NotEmpty(t, got)
	require.Empty(t, got[0].ConfigurationsRemap)
}

func TestExtractConfigurationIgnoresMismatchedAlias(t *testing.T) {
	src := `
resources: { aws.instance.web: { @configuration: gcp.something, ami: 'ami-1' } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.Empty(t, got[0].Configuration,
		"mismatched alias should yield empty configuration")
}

func TestExtractNodesExpandsDataComposite(t *testing.T) {
	composite := parseStack(t, `
data:    { aws.ami.ubuntu: { most-recent: true } }
outputs: { id: { value: data.aws.ami.ubuntu.id } }
`)
	libs := map[string]*Library{
		"img": {
			Name: "img",
			DataComposites: map[string]*CompositeType{
				"lookup": {Name: "lookup", Kind: NodeData, Body: composite},
			},
		},
	}
	stack := parseStack(t, `
data: { img.lookup.latest: { family: 'ubuntu' } }
`)
	got := ExtractNodes(stack, libs)
	require.Len(t, got, 2)

	require.Equal(t, "data.img.lookup.latest", got[0].Address)
	require.True(t, got[0].IsComposite())
	require.Equal(t, NodeData, got[0].Kind)
	require.Equal(t, composite, got[0].CompositeBody)
	require.Empty(t, got[0].Composite)

	require.Equal(t, "data.img.lookup.latest/data.aws.ami.ubuntu", got[1].Address)
	require.Equal(t, NodeData, got[1].Kind)
	require.Equal(t, "data.img.lookup.latest", got[1].Composite)
}

func TestExtractNodesExpandsActionComposite(t *testing.T) {
	composite := parseStack(t, `
actions: { core.command.run: { argv: ['echo'] } }
`)
	libs := map[string]*Library{
		"ops": {
			Name: "ops",
			ActionComposites: map[string]*CompositeType{
				"deploy": {Name: "deploy", Kind: NodeAction, Body: composite},
			},
		},
	}
	stack := parseStack(t, `
actions: { ops.deploy.go: { target: 'prod' } }
`)
	got := ExtractNodes(stack, libs)
	require.Len(t, got, 2)

	require.Equal(t, "action.ops.deploy.go", got[0].Address)
	require.True(t, got[0].IsComposite())
	require.Equal(t, NodeAction, got[0].Kind)
	require.Equal(t, composite, got[0].CompositeBody)
	require.Empty(t, got[0].Composite)

	require.Equal(t, "action.ops.deploy.go/action.core.command.run", got[1].Address)
	require.Equal(t, NodeAction, got[1].Kind)
	require.Equal(t, "action.ops.deploy.go", got[1].Composite)
}
