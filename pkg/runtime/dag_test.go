package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildDAGEmpty(t *testing.T) {
	g := BuildDAG(parseStack(t, `description: 'no nodes'`))
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
`))
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
`))
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
`))
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
`))
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
`))
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
`))
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
`))
	require.Equal(t,
		[]string{"var.cidr"},
		g.Edges["resource.aws.vpc.main"])
}
