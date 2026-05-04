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
  vpc-id: resource.aws.vpc.main.id
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
