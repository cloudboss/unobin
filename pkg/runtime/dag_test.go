package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildDAGEmpty(t *testing.T) {
	g := BuildDAG(parseStack(t, `description: 'no nodes'`), nil)
	require.Empty(t, g.Nodes)
	require.Empty(t, g.Edges)
}

func TestBuildDAGSingleResourceNoDeps(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
`), nil)
	require.Len(t, g.Nodes, 1)
	require.Empty(t, g.Edges["resource.aws.vpc.main"])
}

func TestBuildDAGImplicitDependency(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws.vpc.main:           { cidr-block: '10.0.0.0/16' }
  aws.security-group.web: { vpc-id: resource.aws.vpc.main.id }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["resource.aws.security-group.web"])
}

func TestBuildDAGExplicitDependsOn(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws.vpc.main:           { cidr-block: '10.0.0.0/16' }
  aws.security-group.web: { @depends-on: [resource.aws.vpc.main], name: 'web' }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["resource.aws.security-group.web"])
}

func TestBuildDAGLocalCarriesEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals:    { endpoint: resource.aws.lb.main.dns-name }
resources: { aws.lb.main: { name: 'main' } }
actions:   { core.command.notify: { argv: ['echo', local.endpoint] } }
`), nil)
	require.Equal(t,
		[]string{"resource.aws.lb.main"},
		g.Edges["action.core.command.notify"])
}

func TestBuildDAGLocalChainCarriesEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals:    { raw: resource.aws.lb.main.dns-name, url: local.raw }
resources: { aws.lb.main: { name: 'main' } }
actions:   { core.command.notify: { argv: ['echo', local.url] } }
`), nil)
	require.Equal(t,
		[]string{"resource.aws.lb.main"},
		g.Edges["action.core.command.notify"])
}

func TestBuildDAGLocalMergesMultipleUpstreams(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals: { pair: { a: resource.aws.vpc.main.id, b: resource.aws.subnet.web.id } }
resources: {
  aws.vpc.main:   { cidr-block: '10.0.0.0/16' }
  aws.subnet.web: { cidr-block: '10.0.1.0/24' }
}
actions: { core.command.go: { argv: ['echo', local.pair] } }
`), nil)
	require.ElementsMatch(t,
		[]string{"resource.aws.vpc.main", "resource.aws.subnet.web"},
		g.Edges["action.core.command.go"])
}

func TestBuildDAGLiteralLocalAddsNoEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals:  { greeting: 'hello' }
actions: { core.command.go: { argv: ['echo', local.greeting] } }
`), nil)
	require.Empty(t, g.Edges["action.core.command.go"])
}

func TestBuildDAGMergesImplicitAndExplicit(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws.vpc.main:      { cidr-block: '10.0.0.0/16' }
  aws.subnet.public: { vpc-id: resource.aws.vpc.main.id }
  aws.security-group.web: {
    @depends-on: [resource.aws.subnet.public]
    vpc-id:      resource.aws.vpc.main.id
  }
}
`), nil)
	require.ElementsMatch(t,
		[]string{"resource.aws.vpc.main", "resource.aws.subnet.public"},
		g.Edges["resource.aws.security-group.web"])
}

func TestBuildDAGOutputReferencesResource(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
outputs:   { vpc-id: { value: resource.aws.vpc.main.id } }
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["output.vpc-id"])
}

func TestBuildDAGActionDependsOnResource(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
actions:   { core.command.log: { argv: ['echo', resource.aws.vpc.main.id] } }
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["action.core.command.log"])
}

func TestBuildDAGVarReferenceCreatesEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: { aws.vpc.main: { cidr-block: var.cidr } }
`), nil)
	require.Equal(t,
		[]string{"var.cidr"},
		g.Edges["resource.aws.vpc.main"])
}

func TestBuildDAGCompositeBoundaryDependsOnInternals(t *testing.T) {
	composite := parseStack(t, `
resources: { local.file.a: { path: 'a.txt' }, local.file.b: { path: 'b.txt' } }
`)
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": {Name: "cluster", Body: composite}},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: { net.cluster.web: { name: 'web' } }
`), libs)
	require.ElementsMatch(t,
		[]string{
			"resource.net.cluster.web/resource.local.file.a",
			"resource.net.cluster.web/resource.local.file.b",
		},
		g.Edges["resource.net.cluster.web"])
}

func TestBuildDAGCompositeInternalRewritesSiblingRef(t *testing.T) {
	composite := parseStack(t, `
resources: { local.file.a: { path: 'a.txt' }, local.file.b: { path: resource.local.file.a.path } }
`)
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": {Name: "cluster", Body: composite}},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: { net.cluster.web: {} }
`), libs)
	require.Equal(t,
		[]string{"resource.net.cluster.web/resource.local.file.a"},
		g.Edges["resource.net.cluster.web/resource.local.file.b"])
}

func TestBuildDAGCompositeInternalDropsCompositeScopedVars(t *testing.T) {
	composite := parseStack(t, `
resources: { local.file.x: { path: var.path, content: var.message } }
`)
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: composite},
			},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: { net.cluster.web: { path: var.target-path, message: var.target-message } }
`), libs)
	deps := g.Edges["resource.net.cluster.web/resource.local.file.x"]
	require.NotContains(t, deps, "var.path",
		"composite-scoped var.path should not appear as a parent-scope dep")
	require.NotContains(t, deps, "var.message",
		"composite-scoped var.message should not appear as a parent-scope dep")
	require.Contains(t, deps, "var.target-path",
		"the call-site args' parent-scope refs should still appear")
	require.Contains(t, deps, "var.target-message",
		"the call-site args' parent-scope refs should still appear")
}

func TestBuildDAGCompositeInternalRewritesDataAndActionRefs(t *testing.T) {
	composite := parseStack(t, `
data: { aws.ami.ubuntu: { most-recent: true } }
actions: {
  core.command.lookup: { argv: ['echo', data.aws.ami.ubuntu.id] }
  core.command.verify: { argv: ['check', action.core.command.lookup.stdout] }
}
resources: { local.file.x: { content: action.core.command.lookup.stdout } }
`)
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: composite},
			},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: { net.cluster.web: {} }
`), libs)
	require.Contains(t,
		g.Edges["resource.net.cluster.web/action.core.command.lookup"],
		"resource.net.cluster.web/data.aws.ami.ubuntu",
		"internal action -> internal data ref should be composite-prefixed")
	require.Contains(t,
		g.Edges["resource.net.cluster.web/action.core.command.verify"],
		"resource.net.cluster.web/action.core.command.lookup",
		"internal action -> internal action ref should be composite-prefixed")
	require.Contains(t,
		g.Edges["resource.net.cluster.web/resource.local.file.x"],
		"resource.net.cluster.web/action.core.command.lookup",
		"internal resource -> internal action ref should be composite-prefixed")
}

func TestBuildDAGCompositeInternalInheritsCallSiteArgsRefs(t *testing.T) {
	composite := parseStack(t, `
resources: { local.file.x: { path: var.target } }
`)
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": {Name: "cluster", Body: composite}},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: {
  local.file.src:  { path: 'src.txt' }
  net.cluster.web: { target: resource.local.file.src.path }
}
`), libs)
	require.Contains(t,
		g.Edges["resource.net.cluster.web/resource.local.file.x"],
		"resource.local.file.src",
		"internal should inherit the call-site args' root refs")
}

func TestBuildDAGNestedComposite(t *testing.T) {
	clusterBody := parseStack(t, `
inputs: { path: { type: string } }

resources: { local.file.x: { path: var.path } }
`)
	layerBody := parseStack(t, `
inputs: { target: { type: string } }

resources: { inner-lib.cluster.only: { path: var.target } }
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
	g := BuildDAG(parseStack(t, `
resources: {
  aws.vpc.main:         { cidr-block: '10.0.0.0/16' }
  outer-lib.layer.mine: { target: resource.aws.vpc.main.id }
}
`), libs)

	outerAddr := "resource.outer-lib.layer.mine"
	innerAddr := outerAddr + "/resource.inner-lib.cluster.only"
	leafAddr := innerAddr + "/resource.local.file.x"

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
		"resource.aws.vpc.main",
		"leaf inherits root refs from the outer call site's args via walk-up")
}

func TestTopologicalOrderEmpty(t *testing.T) {
	got, err := BuildDAG(parseStack(t, `description: 'empty'`), nil).TopologicalOrder()
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestTopologicalOrderSingle(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
`), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.aws.vpc.main"}, got)
}

func TestTopologicalOrderLinearChain(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws.vpc.main:           { cidr-block: '10.0.0.0/16' }
  aws.subnet.public:      { vpc-id: resource.aws.vpc.main.id }
  aws.security-group.web: { vpc-id: resource.aws.subnet.public.vpc-id }
}
`), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{
		"resource.aws.vpc.main",
		"resource.aws.subnet.public",
		"resource.aws.security-group.web",
	}, got)
}

func TestTopologicalOrderDiamond(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws.vpc.main:    { cidr-block: '10.0.0.0/16' }
  aws.subnet.a:    { vpc-id: resource.aws.vpc.main.id }
  aws.subnet.b:    { vpc-id: resource.aws.vpc.main.id }
  aws.cluster.web: { @depends-on: [resource.aws.subnet.a, resource.aws.subnet.b] }
}
`), nil)
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
	require.Less(t, indexOf("resource.aws.vpc.main"), indexOf("resource.aws.subnet.a"))
	require.Less(t, indexOf("resource.aws.vpc.main"), indexOf("resource.aws.subnet.b"))
	require.Less(t, indexOf("resource.aws.subnet.a"), indexOf("resource.aws.cluster.web"))
	require.Less(t, indexOf("resource.aws.subnet.b"), indexOf("resource.aws.cluster.web"))
}

func TestTopologicalOrderVarsDontBlock(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: { aws.vpc.main: { cidr-block: var.cidr } }
`), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.aws.vpc.main"}, got)
}

func TestTopologicalOrderReportsCycle(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws.a.x: { @depends-on: [resource.aws.b.y] }
  aws.b.y: { @depends-on: [resource.aws.a.x] }
}
`), nil)
	_, err := g.TopologicalOrder()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
	require.Contains(t, err.Error(), "resource.aws.a.x")
	require.Contains(t, err.Error(), "resource.aws.b.y")
}

func TestTopologicalOrderDeterministic(t *testing.T) {
	src := `
resources: { aws.a.x: {}, aws.b.y: {}, aws.c.z: {} }
`
	g := BuildDAG(parseStack(t, src), nil)
	first, err := g.TopologicalOrder()
	require.NoError(t, err)
	for range 5 {
		again, err := g.TopologicalOrder()
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
}

// Navigating into one field of an object-valued local must not pull in
// the local's other sources as dependencies. If it did, a sibling
// source that itself depends on the navigating node would form a
// spurious cycle.
func TestBuildDAGNarrowsObjectLocalAvoidingSpuriousCycle(t *testing.T) {
	src := `
locals: { net: { vpc: resource.local.file.vpcfile.path, other: resource.local.file.b.path } }
resources: {
  local.file.vpcfile: { path: 'vpc.txt' }
  local.file.a:       { path: local.net.vpc }
  local.file.b:       { path: resource.local.file.a.path }
}
`
	f := parseStack(t, src)
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := BuildDAG(f, libs)

	order, err := dag.TopologicalOrder()
	require.NoError(t, err)
	require.Len(t, order, 3)
}

func TestBuildDAGConfigurationNodeFromBlock(t *testing.T) {
	g := BuildDAG(parseStack(t, `
configurations: {
  k8s.cluster: { host: resource.aws.eks.main.endpoint }
}
resources: {
  aws.eks.main:       { name: 'web' }
  k8s.namespace.apps: { @configuration: k8s.cluster, name: 'apps' }
}
`), nil)
	cfg, ok := g.Nodes["configuration.k8s.cluster"]
	require.True(t, ok, "configuration node should exist")
	require.Equal(t, NodeConfiguration, cfg.Kind)
	require.Equal(t, "k8s", cfg.Alias)
	require.Equal(t, "cluster", cfg.Name)
	require.Equal(t, []string{"resource.aws.eks.main"},
		g.Edges["configuration.k8s.cluster"])
	require.Contains(t, g.Edges["resource.k8s.namespace.apps"],
		"configuration.k8s.cluster")
}

func TestBuildDAGDefaultSelectionEdgesToInternalDefault(t *testing.T) {
	g := BuildDAG(parseStack(t, `
configurations: {
  aws.default: { region: var.region }
}
resources: { aws.vpc.main: { cidr-block: '10.0.0.0/16' } }
`), nil)
	require.Contains(t, g.Edges["resource.aws.vpc.main"], "configuration.aws.default")
}

func TestBuildDAGNoEdgeWhenConfigurationNotInternal(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: { aws.vpc.main: { @configuration: aws.east2, cidr-block: '10.0.0.0/16' } }
`), nil)
	require.Empty(t, g.Edges["resource.aws.vpc.main"])
}

func TestBuildDAGConfigurationCycleDetected(t *testing.T) {
	g := BuildDAG(parseStack(t, `
configurations: {
  aws.default: { token: resource.aws.sts.session.token }
}
resources: { aws.sts.session: { name: 's' } }
`), nil)
	_, err := g.TopologicalOrder()
	require.ErrorContains(t, err, "cycle")
}
