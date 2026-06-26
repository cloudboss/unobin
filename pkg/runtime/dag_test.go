package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/stretchr/testify/require"
)

func dagFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/dag", name)
}

func dagFactorySource(src string) string {
	return "factory" + ": {\n" + src + "\n}"
}

func syntaxFactoryBody(t testing.TB, src string) syntax.FactoryBody {
	t.Helper()
	fixture := parseSyntaxFactoryFixture(t, dagFactorySource(src))
	return fixture.body
}

func syntaxDAG(t testing.TB, src string, libs map[string]*Library) *DAG {
	t.Helper()
	body := syntaxFactoryBody(t, src)
	return BuildSyntaxDAG(body, libs)
}

func syntaxDAGAndBody(
	t testing.TB,
	src string,
	libs map[string]*Library,
) (*DAG, *syntax.FactoryBody) {
	t.Helper()
	body := syntaxFactoryBody(t, src)
	return BuildSyntaxDAG(body, libs), &body
}

func syntaxComposite(t testing.TB, name string, kind NodeKind, src string) *CompositeType {
	t.Helper()
	body := parseSyntaxCompositeFixture(t, name+": "+string(kind)+" {\n"+src+"\n}").body
	return &CompositeType{Name: name, Kind: kind, SyntaxBody: &body}
}

func syntaxResourceComposite(t testing.TB, name, src string) *CompositeType {
	t.Helper()
	return syntaxComposite(t, name, NodeResource, src)
}

func TestBuildDAGEmpty(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-empty"), nil)
	require.Empty(t, g.Nodes)
	require.Empty(t, g.Edges)
}

func TestBuildDAGSingleResourceNoDeps(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-single-resource-no-deps"), nil)
	require.Len(t, g.Nodes, 1)
	require.Empty(t, g.Edges["resource.main"])
}

func TestBuildDAGLibraryConfigDependency(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-library-config-dependency"), nil)
	require.Contains(t, g.Nodes, "library-config.aws")
	require.Equal(t, NodeLibraryConfig, g.Nodes["library-config.aws"].Kind)
	require.Equal(t, []string{"library-config.aws"}, g.Edges["resource.main"])
}

func TestBuildDAGImplicitDependency(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-implicit-dependency"), nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["resource.web"])
}

func TestBuildDAGExplicitDependsOn(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-explicit-depends-on"), nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["resource.web"])
}

func TestBuildDAGLocalAddsEdge(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-local-adds-edge"), nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["action.notify"])
}

func TestBuildDAGLocalChainAddsEdge(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-local-chain-adds-edge"), nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["action.notify"])
}

func TestBuildDAGLocalMergesMultipleUpstreams(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-local-merges-multiple-upstreams"), nil)
	require.ElementsMatch(t,
		[]string{"resource.main", "resource.web"},
		g.Edges["action.go"])
}

func TestBuildDAGLiteralLocalAddsNoEdge(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-literal-local-adds-no-edge"), nil)
	require.Empty(t, g.Edges["action.go"])
}

func TestBuildDAGMergesImplicitAndExplicit(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-merges-implicit-and-explicit"), nil)
	require.ElementsMatch(t,
		[]string{"resource.main", "resource.public"},
		g.Edges["resource.web"])
}

func TestBuildDAGOutputReferencesResource(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-output-references-resource"), nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["output.vpc-id"])
}

func TestBuildDAGActionDependsOnResource(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-action-depends-on-resource"), nil)
	require.Equal(t, []string{"resource.main"}, g.Edges["action.log"])
}

func TestBuildDAGVarReferenceCreatesEdge(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-input-reference-creates-edge"), nil)
	require.Equal(t, []string{"input.cidr"}, g.Edges["resource.main"])
}

func TestBuildDAGCompositeBoundaryDependsOnInternals(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", dagFixture(t, "composite-boundary-body"))
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	g := syntaxDAG(t, dagFixture(t, "composite-boundary-call"), libs)
	require.ElementsMatch(t,
		[]string{"resource.web/resource.a", "resource.web/resource.b"},
		g.Edges["resource.web"])
}

func TestBuildDAGCompositeInternalRewritesSiblingRef(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", dagFixture(t, "composite-sibling-body"))
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	g := syntaxDAG(t, dagFixture(t, "composite-sibling-call"), libs)
	require.Equal(t,
		[]string{"resource.web/resource.a"},
		g.Edges["resource.web/resource.b"])
}

func TestBuildDAGCompositeInternalExcludesCompositeScopedInputs(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", dagFixture(t, "composite-scoped-inputs-body"))
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": composite,
			},
		},
	}
	g := syntaxDAG(t, dagFixture(t, "composite-scoped-inputs-call"), libs)
	deps := g.Edges["resource.web/resource.x"]
	require.NotContains(t, deps, "input.path")
	require.NotContains(t, deps, "input.message")
	require.Contains(t, deps, "input.target-path")
	require.Contains(t, deps, "input.target-message")
}

func TestBuildDAGCompositeInternalRewritesDataAndActionRefs(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", dagFixture(t, "composite-data-action-body"))
	libs := map[string]*Library{
		"net": {
			Name: "net",
			ResourceComposites: map[string]*CompositeType{
				"cluster": composite,
			},
		},
	}
	g := syntaxDAG(t, dagFixture(t, "composite-data-action-call"), libs)
	require.Contains(t,
		g.Edges["resource.web/action.lookup"],
		"resource.web/data-source.ubuntu")
	require.Contains(t,
		g.Edges["resource.web/action.verify"],
		"resource.web/action.lookup")
	require.Contains(t,
		g.Edges["resource.web/resource.x"],
		"resource.web/action.lookup")
}

func TestBuildDAGCompositeInternalInheritsCallSiteArgsRefs(t *testing.T) {
	composite := syntaxResourceComposite(t, "cluster", dagFixture(t, "composite-call-args-body"))
	libs := map[string]*Library{
		"net": {
			Name:               "net",
			ResourceComposites: map[string]*CompositeType{"cluster": composite},
		},
	}
	g := syntaxDAG(t, dagFixture(t, "composite-call-args-call"), libs)
	require.Contains(t,
		g.Edges["resource.web/resource.x"],
		"resource.src")
}

func TestBuildDAGNestedComposite(t *testing.T) {
	clusterBody := syntaxResourceComposite(t, "cluster", dagFixture(t, "build-dag-nested-composite-1"))
	layerBody := syntaxResourceComposite(t, "layer", dagFixture(t, "build-dag-nested-composite-2"))
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
	g := syntaxDAG(t, dagFixture(t, "build-dag-nested-composite-3"), libs)

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
	got, err := syntaxDAG(t, dagFixture(t, "topological-order-empty"), nil).TopologicalOrder()
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestTopologicalOrderSingle(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "topological-order-single"), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.main"}, got)
}

func TestTopologicalOrderLinearChain(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "topological-order-linear-chain"), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{
		"resource.main",
		"resource.public",
		"resource.web",
	}, got)
}

func TestTopologicalOrderDiamond(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "topological-order-diamond"), nil)
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
	g := syntaxDAG(t, dagFixture(t, "topological-order-inputs-dont-block"), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.main"}, got)
}

func TestTopologicalOrderReportsCycle(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "topological-order-reports-cycle"), nil)
	_, err := g.TopologicalOrder()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
	require.Contains(t, err.Error(), "resource.x")
	require.Contains(t, err.Error(), "resource.y")
}

func TestTopologicalOrderDeterministic(t *testing.T) {
	src := dagFixture(t, "topological-order-deterministic")
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
	src := dagFixture(t, "build-dag-narrow-object-local-avoids-spurious-cycle")
	libs := map[string]*Library{"local": {Name: "local"}}
	dag := syntaxDAG(t, src, libs)

	order, err := dag.TopologicalOrder()
	require.NoError(t, err)
	require.Len(t, order, 3)
}

func TestBuildDAGLibraryConfigNode(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-library-config-node"), nil)
	cfg, ok := g.Nodes["library-config.k8s"]
	require.True(t, ok, "library config node should exist")
	require.Equal(t, NodeLibraryConfig, cfg.Kind)
	require.Equal(t, "k8s", cfg.Alias)
	require.Equal(t, "k8s", cfg.Name)
	require.Equal(t, []string{"resource.main"}, g.Edges["library-config.k8s"])
	require.Contains(t, g.Edges["resource.apps"], "library-config.k8s")
}

func TestBuildDAGNoLibraryConfigEdgeWhenAliasUnbound(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-no-library-config-edge-when-alias-unbound"), nil)
	require.Empty(t, g.Edges["resource.main"])
}

func TestBuildDAGLibraryConfigCycleDetected(t *testing.T) {
	g := syntaxDAG(t, dagFixture(t, "build-dag-library-config-cycle-detected"), nil)
	_, err := g.TopologicalOrder()
	require.ErrorContains(t, err, "cycle")
}
