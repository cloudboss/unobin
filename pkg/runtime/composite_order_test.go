package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func TestCompositeOrderPreservesOrderAndAncestorMembership(t *testing.T) {
	root := &Node{Address: "resource.root", CompositeSyntaxBody: &syntax.FactoryBody{},
		ForEach: &lang.ArrayLit{}}
	inner := &Node{Address: "resource.root/resource.inner", Composite: root.Address,
		CompositeSyntaxBody: &syntax.FactoryBody{}}
	leaf := &Node{Address: inner.Address + "/resource.leaf", Composite: inner.Address}
	sibling := &Node{Address: root.Address + "/resource.sibling", Composite: root.Address}
	stray := &Node{Address: "resource.stray", Composite: "resource.missing"}
	plain := &Node{Address: "resource.plain", ForEach: &lang.ArrayLit{}}
	child := &Node{Address: "resource.plain/resource.child", Composite: plain.Address}
	nodes := map[string]*Node{
		root.Address: root, inner.Address: inner, leaf.Address: leaf,
		sibling.Address: sibling, stray.Address: stray, plain.Address: plain,
		child.Address: child,
	}
	order := []string{
		leaf.Address, sibling.Address, "absent", inner.Address, root.Address,
		stray.Address, child.Address, plain.Address,
	}
	index := buildCompositeOrder(nodes, order)
	require.Equal(t, map[string][]*Node{root.Address: {leaf, sibling, inner}}, index.internals)
	require.Equal(t, root.Address, index.owners[inner.Address])
	require.Equal(t, root.Address, index.owners[root.Address])
	require.Empty(t, index.owners[plain.Address])
	require.Empty(t, index.owners["resource.missing"])
	executor := &Executor{DAG: &DAG{Nodes: nodes}}
	run := &runState{order: order}
	require.Equal(t, []*Node{leaf}, executor.compositeInternalsInOrder(run, inner.Address))
	require.Equal(t, []*Node{stray}, executor.compositeInternalsInOrder(run, "resource.missing"))
	require.Equal(t, []*Node{child}, executor.compositeInternalsInOrder(run, plain.Address))
	inner.ForEach = &lang.ArrayLit{}
	nested := buildCompositeOrder(nodes, order)
	require.Equal(t, map[string][]*Node{
		root.Address: {leaf, sibling, inner}, inner.Address: {leaf},
	}, nested.internals)
	require.Equal(t, inner.Address, nested.owners[inner.Address])
}

func TestCompositeOrderWithoutNodes(t *testing.T) {
	index := buildCompositeOrder(nil, []string{"absent"})
	require.Empty(t, index.internals)
	require.Empty(t, index.owners)
}

func TestCompositeOrderIsLocalToEachOperation(t *testing.T) {
	parent := &Node{Address: "resource.parent", CompositeSyntaxBody: &syntax.FactoryBody{},
		ForEach: &lang.ArrayLit{}}
	child := &Node{Address: "resource.parent/resource.child", Composite: parent.Address}
	nodes := map[string]*Node{parent.Address: parent, child.Address: child}
	order := []string{child.Address, parent.Address}
	first := buildCompositeOrder(nodes, order)
	parent.ForEach = nil
	child.Composite = "resource.other"
	second := buildCompositeOrder(nodes, order)
	require.Equal(t, map[string][]*Node{parent.Address: {child}}, first.internals)
	require.Equal(t, parent.Address, first.owners[parent.Address])
	require.Empty(t, second.internals)
	require.Empty(t, second.owners["resource.other"])
}
