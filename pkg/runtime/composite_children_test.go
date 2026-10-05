package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestBuildDAGIndexesOnlyDirectCompositeChildren(t *testing.T) {
	root := "resource.root"
	nested := root + "/resource.mid"
	body := &syntax.FactoryBody{}
	nodes := []*Node{
		{Address: root, Kind: NodeResource, CompositeSyntaxBody: body},
		{Address: root + "/resource.z", Kind: NodeResource, Composite: root},
		{Address: root + "/action.a", Kind: NodeAction, Composite: root},
		{Address: nested, Kind: NodeResource, Composite: root, CompositeSyntaxBody: body},
		{Address: nested + "/data-source.leaf", Kind: NodeDataSource, Composite: nested},
		{Address: "resource.stray", Kind: NodeResource, Composite: "resource.missing"},
		{Address: "resource.plain", Kind: NodeResource},
		{Address: "resource.plain/resource.child", Kind: NodeResource, Composite: "resource.plain"},
		{Address: root + "/resource.z", Kind: NodeResource, Composite: root},
	}
	graph := buildDAG(nodes, nil)
	require.Equal(t, map[string][]string{
		root:                            {root + "/action.a", nested, root + "/resource.z"},
		nested:                          {nested + "/data-source.leaf"},
		root + "/resource.z":            nil,
		root + "/action.a":              nil,
		nested + "/data-source.leaf":    nil,
		"resource.stray":                nil,
		"resource.plain":                nil,
		"resource.plain/resource.child": nil,
	}, graph.Edges)
}
