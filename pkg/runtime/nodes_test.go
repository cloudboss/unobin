package runtime

import (
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

func parseSyntaxFactory(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, syntax.FileFactory, f.Kind)
	require.NotNil(t, f.Factory)
	return &lang.File{
		S:        f.S,
		Kind:     lang.FileFactory,
		Path:     f.Path,
		Body:     syntax.RuntimeFactoryBodyObject(f.Factory.Body),
		Comments: f.Comments,
	}
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
  aws.instance.mirror: { @configuration: aws.east2, ami: 'ami-2' }
}
data:    { aws.ami.ubuntu: { @configuration: aws.east2, most-recent: true } }
actions: { core.command.probe: { @configuration: core.alt, argv: ['echo'] } }
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 4)

	require.Equal(t, "resource.aws.instance.web", got[0].Address)
	require.Empty(t, got[0].Configuration)

	require.Equal(t, "resource.aws.instance.mirror", got[1].Address)
	require.Equal(t, "east2", got[1].Configuration)

	require.Equal(t, "data.aws.ami.ubuntu", got[2].Address)
	require.Equal(t, "east2", got[2].Configuration)

	require.Equal(t, "action.core.command.probe", got[3].Address)
	require.Equal(t, "alt", got[3].Configuration)
}

func TestExtractCompositeReadsConfigurationsRemap(t *testing.T) {
	src := `
imports:   { net: 'github.com/example/net' }
resources: { net.cluster.east: { @configurations: { aws: aws.east2 }, name: 'east' } }
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
		map[string]ConfigRef{"aws": {Alias: "aws", Configuration: "east2"}},
		got[0].ConfigurationsRemap)
}

func TestExtractConfigurationsRemapKeepsMismatchedAliasForValidation(t *testing.T) {
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
	require.Equal(t,
		map[string]ConfigRef{"aws": {Alias: "gcp", Configuration: "east2"}},
		got[0].ConfigurationsRemap,
		"mismatched alias is kept so the validator can report it")
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
