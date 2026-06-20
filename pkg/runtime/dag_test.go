package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/stretchr/testify/require"
)

func syntaxFactoryBody(t *testing.T, src string) syntax.FactoryBody {
	t.Helper()
	fixture := parseSyntaxFactoryFixture(t, "factory: {\n"+src+"\n}")
	return fixture.body
}

func syntaxDAG(t *testing.T, src string, libs map[string]*Library) *DAG {
	t.Helper()
	body := syntaxFactoryBody(t, src)
	return BuildSyntaxDAG(body, libs)
}

func syntaxDAGAndBody(
	t *testing.T,
	src string,
	libs map[string]*Library,
) (*DAG, *syntax.FactoryBody) {
	t.Helper()
	body := syntaxFactoryBody(t, src)
	return BuildSyntaxDAG(body, libs), &body
}

func syntaxComposite(t *testing.T, name string, kind NodeKind, src string) *CompositeType {
	t.Helper()
	body := parseSyntaxCompositeFixture(t, name+": "+string(kind)+" {\n"+src+"\n}").body
	return &CompositeType{Name: name, Kind: kind, SyntaxBody: &body}
}

func syntaxResourceComposite(t *testing.T, name, src string) *CompositeType {
	t.Helper()
	return syntaxComposite(t, name, NodeResource, src)
}

func TestBuildDAGEmpty(t *testing.T) {
	g := syntaxDAG(t, `description: 'no nodes'`, nil)
	require.Empty(t, g.Nodes)
	require.Empty(t, g.Edges)
}

func TestBuildDAGSingleResourceNoDeps(t *testing.T) {
	g := syntaxDAG(t, `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
`, nil)
	require.Len(t, g.Nodes, 1)
	require.Empty(t, g.Edges["resource.main"])
}

func TestBuildDAGLibraryConfigDependency(t *testing.T) {
	g := syntaxDAG(t, `
inputs: { aws-config: { type: library-config('github.com/acme/aws') } }
library-configs: { aws: var.aws-config }
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
`, nil)
	require.Contains(t, g.Nodes, "library-config.aws")
	require.Equal(t, NodeLibraryConfig, g.Nodes["library-config.aws"].Kind)
	require.Equal(t, []string{"library-config.aws"}, g.Edges["resource.main"])
}

func TestBuildDAGImplicitDependency(t *testing.T) {
	g := syntaxDAG(t, `
resources: {
  main: aws.vpc { cidr-block: '10.0.0.0/16' }
  web:  aws.security-group { vpc-id: resource.main.id }
}
`, nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["resource.web"])
}

func TestBuildDAGExplicitDependsOn(t *testing.T) {
	g := syntaxDAG(t, `
resources: {
  main: aws.vpc { cidr-block: '10.0.0.0/16' }
  web:  aws.security-group { @depends-on: [resource.main], name: 'web' }
}
`, nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["resource.web"])
}

func TestBuildDAGLocalAddsEdge(t *testing.T) {
	g := syntaxDAG(t, `
locals:    { endpoint: resource.main.dns-name }
resources: { main: aws.lb { name: 'main' } }
actions:   { notify: core.command { argv: ['echo', local.endpoint] } }
`, nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["action.notify"])
}

func TestBuildDAGLocalChainAddsEdge(t *testing.T) {
	g := syntaxDAG(t, `
locals:    { raw: resource.main.dns-name, url: local.raw }
resources: { main: aws.lb { name: 'main' } }
actions:   { notify: core.command { argv: ['echo', local.url] } }
`, nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["action.notify"])
}

func TestBuildDAGLocalMergesMultipleUpstreams(t *testing.T) {
	g := syntaxDAG(t, `
locals: { pair: { a: resource.main.id, b: resource.web.id } }
resources: {
  main: aws.vpc { cidr-block: '10.0.0.0/16' }
  web:  aws.subnet { cidr-block: '10.0.1.0/24' }
}
actions: { go: core.command { argv: ['echo', local.pair] } }
`, nil)
	require.ElementsMatch(t,
		[]string{"resource.main", "resource.web"},
		g.Edges["action.go"])
}

func TestBuildDAGLiteralLocalAddsNoEdge(t *testing.T) {
	g := syntaxDAG(t, `
locals:  { greeting: 'hello' }
actions: { go: core.command { argv: ['echo', local.greeting] } }
`, nil)
	require.Empty(t, g.Edges["action.go"])
}

func TestBuildDAGMergesImplicitAndExplicit(t *testing.T) {
	g := syntaxDAG(t, `
resources: {
  main:   aws.vpc { cidr-block: '10.0.0.0/16' }
  public: aws.subnet { vpc-id: resource.main.id }
  web:    aws.security-group {
    @depends-on: [resource.public]
    vpc-id:      resource.main.id
  }
}
`, nil)
	require.ElementsMatch(t,
		[]string{"resource.main", "resource.public"},
		g.Edges["resource.web"])
}

func TestBuildDAGOutputReferencesResource(t *testing.T) {
	g := syntaxDAG(t, `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
outputs:   { vpc-id: { value: resource.main.id } }
`, nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["output.vpc-id"])
}

func TestBuildDAGActionDependsOnResource(t *testing.T) {
	g := syntaxDAG(t, `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
actions:   { log: core.command { argv: ['echo', resource.main.id] } }
`, nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["action.log"])
}

func TestBuildDAGVarReferenceCreatesEdge(t *testing.T) {
	g := syntaxDAG(t, `
resources: { main: aws.vpc { cidr-block: var.cidr } }
`, nil)
	require.Equal(t, []string{"var.cidr"}, g.Edges["resource.main"])
}

func TestBuildDAGCompositeBoundaryDependsOnInternals(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", `
resources: {
  a: local.file { path: 'a.txt' }
  b: local.file { path: 'b.txt' }
}
`)
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	g := syntaxDAG(t, `
resources: { web: net.cluster { name: 'web' } }
`, libs)
	require.ElementsMatch(t,
		[]string{"resource.web/resource.a", "resource.web/resource.b"},
		g.Edges["resource.web"])
}

func TestBuildDAGCompositeInternalRewritesSiblingRef(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", `
resources: {
  a: local.file { path: 'a.txt' }
  b: local.file { path: resource.a.path }
}
`)
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	g := syntaxDAG(t, `
resources: { web: net.cluster {} }
`, libs)
	require.Equal(t,
		[]string{"resource.web/resource.a"},
		g.Edges["resource.web/resource.b"])
}

func TestBuildDAGCompositeInternalExcludesCompositeScopedVars(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", `
resources: { x: local.file { path: var.path, content: var.message } }
`)
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": composite,
			},
		},
	}
	g := syntaxDAG(t, `
resources: { web: net.cluster { path: var.target-path, message: var.target-message } }
`, libs)
	deps := g.Edges["resource.web/resource.x"]
	require.NotContains(t, deps, "var.path")
	require.NotContains(t, deps, "var.message")
	require.Contains(t, deps, "var.target-path")
	require.Contains(t, deps, "var.target-message")
}

func TestBuildDAGCompositeInternalRewritesDataAndActionRefs(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", `
data: { ubuntu: aws.ami { most-recent: true } }
actions: {
  lookup: core.command { argv: ['echo', data.ubuntu.id] }
  verify: core.command { argv: ['check', action.lookup.stdout] }
}
resources: { x: local.file { content: action.lookup.stdout } }
`)
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": composite,
			},
		},
	}
	g := syntaxDAG(t, `
resources: { web: net.cluster {} }
`, libs)
	require.Contains(t,
		g.Edges["resource.web/action.lookup"],
		"resource.web/data.ubuntu")
	require.Contains(t,
		g.Edges["resource.web/action.verify"],
		"resource.web/action.lookup")
	require.Contains(t,
		g.Edges["resource.web/resource.x"],
		"resource.web/action.lookup")
}

func TestBuildDAGCompositeInternalInheritsCallSiteArgsRefs(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", `
resources: { x: local.file { path: var.target } }
`)
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	g := syntaxDAG(t, `
resources: {
  src: local.file { path: 'src.txt' }
  web: net.cluster { target: resource.src.path }
}
`, libs)
	require.Contains(t,
		g.Edges["resource.web/resource.x"],
		"resource.src")
}

func TestBuildDAGNestedComposite(t *testing.T) {
	clusterBody := syntaxResourceComposite(t, "cluster", `
inputs: { path: { type: string } }

resources: { x: local.file { path: var.path } }
`)
	layerBody := syntaxResourceComposite(t, "layer", `
inputs: { target: { type: string } }

resources: { only: inner-lib.cluster { path: var.target } }
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
	g := syntaxDAG(t, `
resources: {
  main: aws.vpc { cidr-block: '10.0.0.0/16' }
  mine: outer-lib.layer { target: resource.main.id }
}
`, libs)

	outerAddr := "resource.mine"
	innerAddr := outerAddr + "/resource.only"
	leafAddr := innerAddr + "/resource.x"

	require.ElementsMatch(t,
		[]string{innerAddr},
		g.Edges[outerAddr],
		"outer boundary depends on its direct internals only")
	require.ElementsMatch(t,
		[]string{leafAddr},
		g.Edges[innerAddr],
		"inner boundary depends on its direct internals only")
	require.Contains(t,
		g.Edges[leafAddr],
		"resource.main")
}

func TestTopologicalOrderEmpty(t *testing.T) {
	got, err := syntaxDAG(t, `description: 'empty'`, nil).TopologicalOrder()
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestTopologicalOrderSingle(t *testing.T) {
	g := syntaxDAG(t, `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
`, nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.main"}, got)
}

func TestTopologicalOrderLinearChain(t *testing.T) {
	g := syntaxDAG(t, `
resources: {
  main:   aws.vpc { cidr-block: '10.0.0.0/16' }
  public: aws.subnet { vpc-id: resource.main.id }
  web:    aws.security-group { vpc-id: resource.public.vpc-id }
}
`, nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{
		"resource.main",
		"resource.public",
		"resource.web",
	}, got)
}

func TestTopologicalOrderDiamond(t *testing.T) {
	g := syntaxDAG(t, `
resources: {
  main: aws.vpc { cidr-block: '10.0.0.0/16' }
  a:    aws.subnet { vpc-id: resource.main.id }
  b:    aws.subnet { vpc-id: resource.main.id }
  web:  aws.cluster { @depends-on: [resource.a, resource.b] }
}
`, nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	indexOf := func(addr string) int {
		for i, a := range got {
			if a == addr {
				return i
			}
		}
		return -1
	}
	require.Less(t, indexOf("resource.main"), indexOf("resource.a"))
	require.Less(t, indexOf("resource.main"), indexOf("resource.b"))
	require.Less(t, indexOf("resource.a"), indexOf("resource.web"))
	require.Less(t, indexOf("resource.b"), indexOf("resource.web"))
}

func TestTopologicalOrderVarsDontBlock(t *testing.T) {
	g := syntaxDAG(t, `
resources: { main: aws.vpc { cidr-block: var.cidr } }
`, nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.main"}, got)
}

func TestTopologicalOrderReportsCycle(t *testing.T) {
	g := syntaxDAG(t, `
resources: {
  x: aws.a { @depends-on: [resource.y] }
  y: aws.b { @depends-on: [resource.x] }
}
`, nil)
	_, err := g.TopologicalOrder()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
	require.Contains(t, err.Error(), "resource.x")
	require.Contains(t, err.Error(), "resource.y")
}

func TestTopologicalOrderDeterministic(t *testing.T) {
	src := `
resources: {
  x: aws.a {}
  y: aws.b {}
  z: aws.c {}
}
`
	g := syntaxDAG(t, src, nil)
	first, err := g.TopologicalOrder()
	require.NoError(t, err)
	for range 5 {
		again, err := g.TopologicalOrder()
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
}

func TestBuildDAGNarrowObjectLocalAvoidsSpuriousCycle(t *testing.T) {
	src := `
locals: { net: { vpc: resource.vpcfile.path, other: resource.b.path } }
resources: {
  vpcfile: local.file { path: 'vpc.txt' }
  a:       local.file { path: local.net.vpc }
  b:       local.file { path: resource.a.path }
}
`
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := syntaxDAG(t, src, libs)

	order, err := dag.TopologicalOrder()
	require.NoError(t, err)
	require.Len(t, order, 3)
}

func TestBuildDAGLibraryConfigNode(t *testing.T) {
	g := syntaxDAG(t, `
library-configs: { k8s: { host: resource.main.endpoint } }
resources: {
  main: aws.eks { name: 'web' }
  apps: k8s.namespace { name: 'apps' }
}
`, nil)
	cfg, ok := g.Nodes["library-config.k8s"]
	require.True(t, ok, "library config node should exist")
	require.Equal(t, NodeLibraryConfig, cfg.Kind)
	require.Equal(t, "k8s", cfg.Alias)
	require.Equal(t, "k8s", cfg.Name)
	require.Equal(t, []string{"resource.main"}, g.Edges["library-config.k8s"])
	require.Contains(t, g.Edges["resource.apps"], "library-config.k8s")
}

func TestBuildDAGNoLibraryConfigEdgeWhenAliasUnbound(t *testing.T) {
	g := syntaxDAG(t, `
resources: { main: aws.vpc { cidr-block: '10.0.0.0/16' } }
`, nil)
	require.Empty(t, g.Edges["resource.main"])
}

func TestBuildDAGLibraryConfigCycleDetected(t *testing.T) {
	g := syntaxDAG(t, `
library-configs: { aws: { token: resource.session.token } }
resources: { session: aws.sts { name: 's' } }
`, nil)
	_, err := g.TopologicalOrder()
	require.ErrorContains(t, err, "cycle")
}
