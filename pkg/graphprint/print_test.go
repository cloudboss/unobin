package graphprint

import (
	"bytes"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func sampleDAG() *runtime.DAG {
	a := &runtime.Node{Address: "action.core.echo.first"}
	b := &runtime.Node{Address: "action.core.echo.second"}
	return &runtime.DAG{
		Nodes: map[string]*runtime.Node{
			a.Address: a,
			b.Address: b,
		},
		Edges: map[string][]string{
			a.Address: {"input.msg"},
			b.Address: {"action.core.echo.first"},
		},
	}
}

func TestPlainListsNodesAndEdges(t *testing.T) {
	var buf bytes.Buffer
	Plain(&buf, sampleDAG())

	want := `action.core.echo.first
  -> input.msg

action.core.echo.second
  -> action.core.echo.first
`
	require.Equal(t, want, buf.String())
}

func TestDOTSkipsNonNodeEdges(t *testing.T) {
	var buf bytes.Buffer
	DOT(&buf, sampleDAG(), "test-stack")

	want := `digraph "test-stack" {
  "action.core.echo.first";
  "action.core.echo.second";
  "action.core.echo.second" -> "action.core.echo.first";
}
`
	require.Equal(t, want, buf.String())
}
