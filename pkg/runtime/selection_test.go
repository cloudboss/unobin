package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeCategoryUsesPublicKind(t *testing.T) {
	node := &Node{Kind: NodeResource}
	require.Equal(t, NodeResource, node.Category())
	node.Kind = NodeAction
	require.Equal(t, NodeAction, node.Category())
}

func TestNodeExportUsesPublicType(t *testing.T) {
	node := &Node{Type: "bucket"}
	require.Equal(t, "bucket", node.Export())
	node.Type = "table"
	require.Equal(t, "table", node.Export())
}

func TestStepNodeExportUsesPublicField(t *testing.T) {
	node := StepNode{ExportKind: "bucket"}
	require.Equal(t, "bucket", node.Export())
	node.ExportKind = "table"
	require.Equal(t, "table", node.Export())
}
