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
resources: {
  aws: {
    vpc: {
      main: { cidr-block: '10.0.0.0/16' }
    }
  }
}
`), nil)
	require.Len(t, g.Nodes, 1)
	require.Empty(t, g.Edges["resource.aws.vpc.main"])
}

func TestBuildDAGImplicitDependency(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: {
    vpc: {
      main: { cidr-block: '10.0.0.0/16' }
    }
    security-group: {
      web: {
        vpc-id: resource.aws.vpc.main.id
      }
    }
  }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["resource.aws.security-group.web"])
}

func TestBuildDAGExplicitDependsOn(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: {
    vpc: {
      main: { cidr-block: '10.0.0.0/16' }
    }
    security-group: {
      web: {
        @depends-on: [resource.aws.vpc.main]
        name:        'web'
      }
    }
  }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["resource.aws.security-group.web"])
}

func TestBuildDAGLocalCarriesEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals: {
  endpoint: resource.aws.lb.main.dns-name
}
resources: {
  aws: { lb: { main: { name: 'main' } } }
}
actions: {
  core: { command: { notify: { argv: ['echo', local.endpoint] } } }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.lb.main"},
		g.Edges["action.core.command.notify"])
}

func TestBuildDAGLocalChainCarriesEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals: {
  raw:   resource.aws.lb.main.dns-name
  url:   local.raw
}
resources: {
  aws: { lb: { main: { name: 'main' } } }
}
actions: {
  core: { command: { notify: { argv: ['echo', local.url] } } }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.lb.main"},
		g.Edges["action.core.command.notify"])
}

func TestBuildDAGLocalMergesMultipleUpstreams(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals: {
  pair: { a: resource.aws.vpc.main.id  b: resource.aws.subnet.web.id }
}
resources: {
  aws: {
    vpc:    { main: { cidr-block: '10.0.0.0/16' } }
    subnet: { web: { cidr-block: '10.0.1.0/24' } }
  }
}
actions: {
  core: { command: { go: { argv: ['echo', local.pair] } } }
}
`), nil)
	require.ElementsMatch(t,
		[]string{"resource.aws.vpc.main", "resource.aws.subnet.web"},
		g.Edges["action.core.command.go"])
}

func TestBuildDAGLiteralLocalAddsNoEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
locals: {
  greeting: 'hello'
}
actions: {
  core: { command: { go: { argv: ['echo', local.greeting] } } }
}
`), nil)
	require.Empty(t, g.Edges["action.core.command.go"])
}

func TestBuildDAGMergesImplicitAndExplicit(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: {
    vpc: { main: { cidr-block: '10.0.0.0/16' } }
    subnet: { public: { vpc-id: resource.aws.vpc.main.id } }
    security-group: {
      web: {
        @depends-on: [resource.aws.subnet.public]
        vpc-id:      resource.aws.vpc.main.id
      }
    }
  }
}
`), nil)
	require.ElementsMatch(t,
		[]string{"resource.aws.vpc.main", "resource.aws.subnet.public"},
		g.Edges["resource.aws.security-group.web"])
}

func TestBuildDAGOutputReferencesResource(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: { vpc: { main: { cidr-block: '10.0.0.0/16' } } }
}
outputs: {
  vpc-id: { value: resource.aws.vpc.main.id }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["output.vpc-id"])
}

func TestBuildDAGActionDependsOnResource(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: { vpc: { main: { cidr-block: '10.0.0.0/16' } } }
}
actions: {
  core: {
    command: {
      log: { argv: ['echo', resource.aws.vpc.main.id] }
    }
  }
}
`), nil)
	require.Equal(t,
		[]string{"resource.aws.vpc.main"},
		g.Edges["action.core.command.log"])
}

func TestBuildDAGVarReferenceCreatesEdge(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: {
    vpc: { main: { cidr-block: var.cidr } }
  }
}
`), nil)
	require.Equal(t,
		[]string{"var.cidr"},
		g.Edges["resource.aws.vpc.main"])
}

func TestBuildDAGCompositeBoundaryDependsOnInternals(t *testing.T) {
	composite := parseStack(t, `
resources: {
  local: {
    file: {
      a: { path: 'a.txt' }
      b: { path: 'b.txt' }
    }
  }
}
`)
	mods := map[string]*Module{
		"net": {
			Name:       "net",
			Composites: map[string]*CompositeType{"cluster": {Name: "cluster", Body: composite}},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: {
  net: { cluster: { web: { name: 'web' } } }
}
`), mods)
	require.ElementsMatch(t,
		[]string{
			"resource.net.cluster.web/local.file.a",
			"resource.net.cluster.web/local.file.b",
		},
		g.Edges["resource.net.cluster.web"])
}

func TestBuildDAGCompositeInternalRewritesSiblingRef(t *testing.T) {
	composite := parseStack(t, `
resources: {
  local: {
    file: {
      a: { path: 'a.txt' }
      b: { path: resource.local.file.a.path }
    }
  }
}
`)
	mods := map[string]*Module{
		"net": {
			Name:       "net",
			Composites: map[string]*CompositeType{"cluster": {Name: "cluster", Body: composite}},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: {
  net: { cluster: { web: {} } }
}
`), mods)
	require.Equal(t,
		[]string{"resource.net.cluster.web/local.file.a"},
		g.Edges["resource.net.cluster.web/local.file.b"])
}

func TestBuildDAGCompositeInternalDropsCompositeScopedVars(t *testing.T) {
	composite := parseStack(t, `
resources: {
  local: { file: { x: { path: var.path, content: var.message } } }
}
`)
	mods := map[string]*Module{
		"net": {
			Name: "net",
			Composites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: composite},
			},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: {
  net: {
    cluster: {
      web: { path: var.target-path, message: var.target-message }
    }
  }
}
`), mods)
	deps := g.Edges["resource.net.cluster.web/local.file.x"]
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
data: {
  aws: { ami: { ubuntu: { most-recent: true } } }
}
actions: {
  core: {
    command: {
      lookup: { argv: ['echo', data.aws.ami.ubuntu.id] }
      verify: { argv: ['check', action.core.command.lookup.stdout] }
    }
  }
}
resources: {
  local: {
    file: {
      x: { content: action.core.command.lookup.stdout }
    }
  }
}
`)
	mods := map[string]*Module{
		"net": {
			Name: "net",
			Composites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: composite},
			},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: {
  net: { cluster: { web: {} } }
}
`), mods)
	require.Contains(t,
		g.Edges["resource.net.cluster.web/action.core.command.lookup"],
		"resource.net.cluster.web/data.aws.ami.ubuntu",
		"internal action -> internal data ref should be composite-prefixed")
	require.Contains(t,
		g.Edges["resource.net.cluster.web/action.core.command.verify"],
		"resource.net.cluster.web/action.core.command.lookup",
		"internal action -> internal action ref should be composite-prefixed")
	require.Contains(t,
		g.Edges["resource.net.cluster.web/local.file.x"],
		"resource.net.cluster.web/action.core.command.lookup",
		"internal resource -> internal action ref should be composite-prefixed")
}

func TestBuildDAGCompositeInternalInheritsCallSiteArgsRefs(t *testing.T) {
	composite := parseStack(t, `
resources: {
  local: { file: { x: { path: var.target } } }
}
`)
	mods := map[string]*Module{
		"net": {
			Name:       "net",
			Composites: map[string]*CompositeType{"cluster": {Name: "cluster", Body: composite}},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: {
  local: { file: { src: { path: 'src.txt' } } }
  net: {
    cluster: {
      web: { target: resource.local.file.src.path }
    }
  }
}
`), mods)
	require.Contains(t,
		g.Edges["resource.net.cluster.web/local.file.x"],
		"resource.local.file.src",
		"internal should inherit the call-site args' root refs")
}

func TestBuildDAGNestedComposite(t *testing.T) {
	clusterBody := parseStack(t, `
inputs: {
  path: { type: string }
}

resources: {
  local: {
    file: { x: { path: var.path } }
  }
}
`)
	layerBody := parseStack(t, `
inputs: {
  target: { type: string }
}

resources: {
  inner-mod: {
    cluster: { only: { path: var.target } }
  }
}
`)
	mods := map[string]*Module{
		"outer-mod": {
			Name: "outer-mod",
			Composites: map[string]*CompositeType{
				"layer": {Name: "layer", Body: layerBody},
			},
		},
		"inner-mod": {
			Name: "inner-mod",
			Composites: map[string]*CompositeType{
				"cluster": {Name: "cluster", Body: clusterBody},
			},
		},
	}
	g := BuildDAG(parseStack(t, `
resources: {
  aws: { vpc: { main: { cidr-block: '10.0.0.0/16' } } }
  outer-mod: {
    layer: {
      mine: { target: resource.aws.vpc.main.id }
    }
  }
}
`), mods)

	outerAddr := "resource.outer-mod.layer.mine"
	innerAddr := outerAddr + "/inner-mod.cluster.only"
	leafAddr := innerAddr + "/local.file.x"

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
resources: { aws: { vpc: { main: { cidr-block: '10.0.0.0/16' } } } }
`), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.aws.vpc.main"}, got)
}

func TestTopologicalOrderLinearChain(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: {
    vpc:    { main: { cidr-block: '10.0.0.0/16' } }
    subnet: { public: { vpc-id: resource.aws.vpc.main.id } }
    security-group: {
      web: { vpc-id: resource.aws.subnet.public.vpc-id }
    }
  }
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
  aws: {
    vpc:    { main: { cidr-block: '10.0.0.0/16' } }
    subnet: {
      a: { vpc-id: resource.aws.vpc.main.id }
      b: { vpc-id: resource.aws.vpc.main.id }
    }
    cluster: {
      web: {
        @depends-on: [resource.aws.subnet.a, resource.aws.subnet.b]
      }
    }
  }
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
resources: {
  aws: {
    vpc: { main: { cidr-block: var.cidr } }
  }
}
`), nil)
	got, err := g.TopologicalOrder()
	require.NoError(t, err)
	require.Equal(t, []string{"resource.aws.vpc.main"}, got)
}

func TestTopologicalOrderReportsCycle(t *testing.T) {
	g := BuildDAG(parseStack(t, `
resources: {
  aws: {
    a: {
      x: {
        @depends-on: [resource.aws.b.y]
      }
    }
    b: {
      y: {
        @depends-on: [resource.aws.a.x]
      }
    }
  }
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
resources: {
  aws: {
    a: { x: {} }
    b: { y: {} }
    c: { z: {} }
  }
}
`
	g := BuildDAG(parseStack(t, src), nil)
	first, err := g.TopologicalOrder()
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		again, err := g.TopologicalOrder()
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
}
