package graphprint

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func sampleDAG() *runtime.DAG {
	compositeBody := &syntax.FactoryBody{}
	nodes := []*runtime.Node{
		{
			Address: "resource.app", Kind: runtime.NodeResource,
			Alias: "net", LibraryPath: "example.com/net", Type: "cluster",
			CompositeSyntaxBody: compositeBody,
		},
		{
			Address: "resource.app/resource.server", Kind: runtime.NodeResource,
			Alias: "aws", LibraryPath: "example.com/aws", Type: "instance",
		},
		{
			Address: "library-config.aws", Kind: runtime.NodeLibraryConfig, Alias: "aws",
		},
		{Address: "action.deploy", Kind: runtime.NodeAction, Alias: "ops", Type: "run"},
		{Address: "output.url", Kind: runtime.NodeOutput},
	}
	byAddress := make(map[string]*runtime.Node, len(nodes))
	for _, node := range nodes {
		byAddress[node.Address] = node
	}
	return &runtime.DAG{
		Nodes: byAddress,
		Edges: map[string][]string{
			"action.deploy":                {"library-config.aws"},
			"output.url":                   {"action.deploy"},
			"resource.app":                 {"resource.app/resource.server", "input.region"},
			"resource.app/resource.server": {"library-config.aws", "input.size"},
		},
	}
}

func TestTextListsNodesAndEdges(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, sampleDAG())
	requireGraphGolden(t, "testdata/text.stdout", buf.Bytes())
}

func TestDOTSkipsNonNodeEdges(t *testing.T) {
	var buf bytes.Buffer
	DOT(&buf, sampleDAG(), "test-stack")
	requireGraphGolden(t, "testdata/dot.stdout", buf.Bytes())
}

func TestBuildDocumentGolden(t *testing.T) {
	document := BuildDocument(sampleDAG(), "test-stack", []diagnostic.Diagnostic{
		{
			Code: "unobin.schema.extraction", Severity: diagnostic.SeverityWarning,
			Message: "import 'aws': schema unavailable",
		},
	})
	body, err := json.MarshalIndent(document, "", "  ")
	require.NoError(t, err)
	body = append(body, '\n')
	requireGraphGolden(t, "testdata/document.json", body)
}

func requireGraphGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}
