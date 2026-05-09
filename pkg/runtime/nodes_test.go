package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func parseStack(t *testing.T, src string) *lang.File {
	t.Helper()
	f, err := lang.ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)
	return f
}

func TestExtractNodesEmpty(t *testing.T) {
	f := parseStack(t, `description: 'nothing here'`)
	require.Empty(t, ExtractNodes(f, nil))
}

func TestExtractNodesResources(t *testing.T) {
	src := `
resources: {
  aws: {
    vpc: {
      main:    { cidr-block: '10.0.0.0/16' }
      backup:  { cidr-block: '10.1.0.0/16' }
    }
    security-group: {
      web: { name: 'web' }
    }
  }
}
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 3)

	require.Equal(t, "resource.aws.vpc.main", got[0].Address)
	require.Equal(t, NodeResource, got[0].Kind)
	require.Equal(t, "aws", got[0].NS)
	require.Equal(t, "vpc", got[0].Type)
	require.Equal(t, "main", got[0].Name)

	require.Equal(t, "resource.aws.vpc.backup", got[1].Address)
	require.Equal(t, "resource.aws.security-group.web", got[2].Address)
}

func TestExtractNodesAllKinds(t *testing.T) {
	src := `
resources: {
  aws: { vpc: { main: { cidr-block: '10.0.0.0/16' } } }
}
data: {
  aws: { ami: { ubuntu: { most-recent: true } } }
}
actions: {
  core: { command: { hello: { argv: ['echo'] } } }
}
outputs: {
  vpc-id: resource.aws.vpc.main.id
  static: 'literal'
}
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
  vpc-id: resource.aws.vpc.main.id
}
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.IsType(t, &lang.DotPath{}, got[0].Body)
}

func TestExtractNodesResourceBody(t *testing.T) {
	src := `
resources: {
  aws: {
    vpc: {
      main: { cidr-block: '10.0.0.0/16' }
    }
  }
}
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
resources: {
  local: {
    file: { greeting: { path: 'hello.txt', content: 'hi' } }
  }
}
outputs: {
  greeting-path: resource.local.file.greeting.path
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
	stack := parseStack(t, `
resources: {
  net: {
    cluster: { web: { name: 'web' } }
  }
}
`)
	got := ExtractNodes(stack, mods)
	require.Len(t, got, 2)

	require.Equal(t, "resource.net.cluster.web", got[0].Address)
	require.Equal(t, NodeComposite, got[0].Kind)
	require.Equal(t, composite, got[0].CompositeBody)
	require.Empty(t, got[0].Composite)

	require.Equal(t, "resource.net.cluster.web/local.file.greeting", got[1].Address)
	require.Equal(t, NodeResource, got[1].Kind)
	require.Equal(t, "resource.net.cluster.web", got[1].Composite)
	require.Equal(t, "local", got[1].NS)
	require.Equal(t, "file", got[1].Type)
	require.Equal(t, "greeting", got[1].Name)
}

func TestExtractNodesCompositeDropsInternalOutputs(t *testing.T) {
	composite := parseStack(t, `
resources: {
  local: { file: { x: { path: 'x.txt' } } }
}
outputs: {
  path: resource.local.file.x.path
}
`)
	mods := map[string]*Module{
		"m": {
			Name:       "m",
			Composites: map[string]*CompositeType{"t": {Name: "t", Body: composite}},
		},
	}
	stack := parseStack(t, `
resources: {
  m: { t: { one: {} } }
}
`)
	got := ExtractNodes(stack, mods)
	for _, n := range got {
		require.NotEqual(t, NodeOutput, n.Kind,
			"output node should not become a DAG node; got %q", n.Address)
	}
}

func TestExtractNodesNestedComposite(t *testing.T) {
	// clusterBody is the body file for the `cluster` composite type
	// registered under module alias `inner-mod`. In a real project this
	// would live in `cluster.ub`, listed in inner-mod's `module.ub`
	// manifest as `exports: { cluster: 'cluster.ub' }`.
	clusterBody := parseStack(t, `
inputs: {
  path: { type: string }
}

resources: {
  local: {
    file: { x: { path: var.path } }
  }
}

outputs: {
  path: resource.local.file.x.path
}
`)
	// layerBody is the body file for the `layer` composite type registered
	// under module alias `outer-mod`. Its body calls inner-mod's `cluster`
	// composite, which is what makes this a nested composite.
	layerBody := parseStack(t, `
inputs: {
  target: { type: string }
}

resources: {
  inner-mod: {
    cluster: {
      only: { path: var.target }
    }
  }
}

outputs: {
  path: resource.inner-mod.cluster.only.path
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
	stack := parseStack(t, `
resources: {
  outer-mod: { layer: { mine: { target: '/tmp/x' } } }
}
`)
	got := ExtractNodes(stack, mods)

	byAddr := map[string]*Node{}
	for _, n := range got {
		byAddr[n.Address] = n
	}

	outerBoundary := byAddr["resource.outer-mod.layer.mine"]
	require.NotNil(t, outerBoundary, "outer boundary at root address")
	require.Equal(t, NodeComposite, outerBoundary.Kind)
	require.Empty(t, outerBoundary.Composite, "outer boundary has root scope")

	innerBoundary := byAddr["resource.outer-mod.layer.mine/inner-mod.cluster.only"]
	require.NotNil(t, innerBoundary, "inner boundary nested under outer")
	require.Equal(t, NodeComposite, innerBoundary.Kind)
	require.Equal(t, "resource.outer-mod.layer.mine", innerBoundary.Composite,
		"inner boundary's direct parent is outer call site")

	leafAddr := "resource.outer-mod.layer.mine/inner-mod.cluster.only/local.file.x"
	leaf := byAddr[leafAddr]
	require.NotNil(t, leaf, "leaf under inner composite")
	require.Equal(t, NodeResource, leaf.Kind)
	require.Equal(t, "resource.outer-mod.layer.mine/inner-mod.cluster.only", leaf.Composite,
		"leaf's direct parent is inner call site")
}

func TestExtractNodesSkipsMalformed(t *testing.T) {
	src := `
resources: {
  aws: 'not an object'
  net: {
    cluster: 'also not an object'
    real: {
      web: { size: 3 }
    }
  }
}
`
	got := ExtractNodes(parseStack(t, src), nil)
	require.Len(t, got, 1)
	require.Equal(t, "resource.net.real.web", got[0].Address)
}
