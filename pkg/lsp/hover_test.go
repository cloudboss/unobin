package lsp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func TestHoverInputTypeAndDescription(t *testing.T) {
	root, path, source := completionProject(t)

	hover, rpcErr := HoverForText(path, source,
		positionInText(source, "input.region", "region"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.NotNil(t, hover)
	require.Contains(t, hover.Contents.Value, "input region: string")
	require.Contains(t, hover.Contents.Value, "AWS region to use.")
}

func TestHoverGoSchemaFieldType(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)

	hover, rpcErr := HoverForText(path, source,
		positionInText(source, "server-name: 'web'", "server-name"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.NotNil(t, hover)
	require.Equal(t, "server-name: string", hover.Contents.Value)
}

func TestHoverUnknownGoNodeSelectorReturnsNoHover(t *testing.T) {
	root, path, source, _ := goDefinitionProjectFixture(
		t, "testdata/ub/definition/valid/go-backed-unknown-node.ub",
	)

	hover, rpcErr := HoverForText(path, source,
		positionInText(source, "server-name: 'web'", "server-name"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.Nil(t, hover)
}

func TestHoverGoFunctionSignature(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)

	hover, rpcErr := HoverForText(path, source,
		positionInText(source, "def.slug('v1')", "slug"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.NotNil(t, hover)
	require.Equal(t, "slug(string) string", hover.Contents.Value)
}

func TestHoverFunctionWithMissingCachedSourceReturnsNoHover(t *testing.T) {
	_, path, source, cache := missingCachedGoDefinitionProject(t)

	hover, rpcErr := HoverForText(path, source,
		positionInText(source, "def.slug('v1')", "slug"), cache)
	require.Nil(t, rpcErr)
	require.Nil(t, hover)
}

func TestHoverUnknownTarget(t *testing.T) {
	root, path, source := completionProject(t)

	hover, rpcErr := HoverForText(path, source,
		positionInText(source, "value: null", "null"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.Nil(t, hover)
}

func TestSessionHoverReturnsContents(t *testing.T) {
	_, path, source := completionProject(t)
	session := NewSession("dev")
	uri := PathToFileURI(path)
	rpcErr := openDocument(t, session, uri, 1, source)
	require.Nil(t, rpcErr)

	result, rpcErr := requestHover(t, session, uri,
		positionInText(source, "input.region", "region"))
	require.Nil(t, rpcErr)
	hover, ok := result.(*protocol.Hover)
	require.True(t, ok)
	require.Contains(t, hover.Contents.Value, "input region: string")
}

func requestHover(
	t *testing.T,
	session *Session,
	uri string,
	pos protocol.Position,
) (any, *protocol.ResponseError) {
	t.Helper()
	params := protocol.HoverParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/hover", Params: body,
	})
}
