package lsp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func TestSessionInitializeCapabilities(t *testing.T) {
	session := NewSession("dev")
	params := protocol.InitializeParams{RootURI: "file:///tmp/work"}
	body, err := json.Marshal(params)
	require.NoError(t, err)

	result, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0",
		ID:      protocol.NewNumberID(1),
		Method:  "initialize",
		Params:  body,
	})
	require.Nil(t, rpcErr)

	initialize, ok := result.(protocol.InitializeResult)
	require.True(t, ok)
	require.Equal(t, protocol.TextDocumentSyncKindFull,
		initialize.Capabilities.TextDocumentSync)
	require.True(t, initialize.Capabilities.DocumentFormattingProvider)
	require.True(t, initialize.Capabilities.DefinitionProvider)
	require.True(t, initialize.Capabilities.DocumentSymbolProvider)
	require.NotNil(t, initialize.Capabilities.CompletionProvider)
	require.True(t, initialize.Capabilities.HoverProvider)
	require.Equal(t, "unobin", initialize.ServerInfo.Name)
	require.Equal(t, "dev", initialize.ServerInfo.Version)

	encoded, err := json.Marshal(initialize)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "protocolVersion")
}

func TestSessionShutdown(t *testing.T) {
	session := NewSession("dev")
	result, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0",
		ID:      protocol.NewNumberID(1),
		Method:  "shutdown",
	})
	require.Nil(t, rpcErr)
	require.Nil(t, result)
	require.True(t, session.Shutdown())
}

func TestSessionUnknownMethod(t *testing.T) {
	session := NewSession("dev")
	_, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0",
		ID:      protocol.NewNumberID(1),
		Method:  "workspace/symbol",
	})
	require.NotNil(t, rpcErr)
	require.Equal(t, protocol.ErrorCodeMethodNotFound, rpcErr.Code)
}
