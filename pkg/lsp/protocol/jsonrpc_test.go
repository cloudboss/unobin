package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestIDAcceptsNumberAndString(t *testing.T) {
	var numeric RequestMessage
	require.NoError(t, json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","id":7,"method":"initialize"}`),
		&numeric,
	))
	require.NotNil(t, numeric.ID)
	number, ok := numeric.ID.Number()
	require.True(t, ok)
	require.Equal(t, json.Number("7"), number)

	var stringID RequestMessage
	require.NoError(t, json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","id":"abc","method":"initialize"}`),
		&stringID,
	))
	require.NotNil(t, stringID.ID)
	text, ok := stringID.ID.String()
	require.True(t, ok)
	require.Equal(t, "abc", text)
}

func TestServerReturnsMethodNotFound(t *testing.T) {
	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"unknown"}`)
	var input bytes.Buffer
	require.NoError(t, WriteMessage(&input, request))
	var output bytes.Buffer

	server := NewServer(&input, &output, HandlerFunc(func(
		context.Context,
		*RequestMessage,
	) (any, *ResponseError) {
		return nil, MethodNotFound("unknown")
	}))
	require.NoError(t, server.Serve(context.Background()))

	payload, err := ReadMessage(&output)
	require.NoError(t, err)
	var response ResponseMessage
	require.NoError(t, json.Unmarshal(payload, &response))
	require.NotNil(t, response.Error)
	require.Equal(t, ErrorCodeMethodNotFound, response.Error.Code)
	require.Equal(t, "method not found: unknown", response.Error.Message)
}
