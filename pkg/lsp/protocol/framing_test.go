package protocol

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFramingReadsAndWritesContentLengthMessages(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"initialized"}`)
	var buf bytes.Buffer

	require.NoError(t, WriteMessage(&buf, payload))
	require.Contains(t, buf.String(), "Content-Length: 40\r\n\r\n")

	got, err := ReadMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestFramingReturnsEOFAtMessageBoundary(t *testing.T) {
	got, err := ReadMessage(bytes.NewReader(nil))
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, got)
}
