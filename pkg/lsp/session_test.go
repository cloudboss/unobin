package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

func TestServeShutdownResponseContainsNullResult(t *testing.T) {
	var input bytes.Buffer
	require.NoError(t, protocol.WriteMessage(&input, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"shutdown"}`,
	)))
	var output bytes.Buffer

	require.NoError(t, Serve(context.Background(), &input, &output, "dev"))

	payload, err := protocol.ReadMessage(&output)
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":null}`, string(payload))
}

func TestServeStopsAfterExit(t *testing.T) {
	var input bytes.Buffer
	require.NoError(t, protocol.WriteMessage(&input, []byte(
		`{"jsonrpc":"2.0","method":"exit"}`,
	)))
	require.NoError(t, protocol.WriteMessage(&input, []byte(
		`{"jsonrpc":"2.0","id":99,"method":"workspace/symbol"}`,
	)))
	var output bytes.Buffer

	require.NoError(t, Serve(context.Background(), &input, &output, "dev"))

	payload, err := protocol.ReadMessage(&output)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, payload)
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

func TestSessionRejectsRequestsAfterShutdown(t *testing.T) {
	session := NewSession("dev")
	_, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0",
		ID:      protocol.NewNumberID(1),
		Method:  "shutdown",
	})
	require.Nil(t, rpcErr)

	_, rpcErr = session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0",
		ID:      protocol.NewNumberID(2),
		Method:  "textDocument/definition",
	})
	require.NotNil(t, rpcErr)
	require.Equal(t, protocol.ErrorCodeInvalidRequest, rpcErr.Code)

	result, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0",
		Method:  "exit",
	})
	require.Nil(t, rpcErr)
	require.Nil(t, result)
	require.True(t, session.Exit())
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
