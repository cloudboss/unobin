package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerConcurrentWritesKeepMessagesWhole(t *testing.T) {
	writer := &blockedMessageWriter{
		firstWrite: make(chan struct{}), nextWrite: make(chan struct{}),
		release: make(chan struct{}),
	}
	var release sync.Once
	unblock := func() { release.Do(func() { close(writer.release) }) }
	t.Cleanup(unblock)
	server := NewServer(bytes.NewReader(nil), writer, HandlerFunc(func(
		context.Context, *RequestMessage,
	) (any, *ResponseError) {
		return nil, nil
	}))
	done := make(chan error, 2)
	go func() {
		done <- server.Notify("textDocument/publishDiagnostics", map[string]string{"uri": "first"})
	}()
	select {
	case <-writer.firstWrite:
	case <-time.After(5 * time.Second):
		t.Fatal("notification did not start")
	}
	started := make(chan struct{})
	go func() {
		close(started)
		done <- server.writeResponse(ResponseMessage{ID: NewNumberID(7), Result: "second"})
	}()
	<-started
	select {
	case <-writer.nextWrite:
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	for range 2 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("output did not finish")
		}
	}
	reader := bufio.NewReader(bytes.NewReader(writer.buffer.Bytes()))
	first, err := ReadMessage(reader)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"first"}
	}`, string(first))
	second, err := ReadMessage(reader)
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":7,"result":"second"}`, string(second))
	_, err = ReadMessage(reader)
	require.ErrorIs(t, err, io.EOF)
}

type blockedMessageWriter struct {
	mu         sync.Mutex
	buffer     bytes.Buffer
	writes     int
	firstWrite chan struct{}
	nextWrite  chan struct{}
	release    chan struct{}
}

func (w *blockedMessageWriter) Write(body []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buffer.Write(body)
	w.writes++
	first := w.writes == 1
	if first {
		close(w.firstWrite)
	} else if w.writes == 2 {
		close(w.nextWrite)
	}
	w.mu.Unlock()
	if first {
		<-w.release
	}
	return n, err
}

func TestServerConcurrentTraceAndNotifications(t *testing.T) {
	var output bytes.Buffer
	var trace bytes.Buffer
	server := NewServerWithOptions(bytes.NewReader(nil), &output, nil,
		ServerOptions{Trace: &trace})
	done := make(chan error, 64)
	for id := range 64 {
		go func() {
			if id%2 == 0 {
				done <- server.Notify("test/update", map[string]int{"id": id})
				return
			}
			body, err := json.Marshal(map[string]int{"id": id})
			if err != nil {
				done <- err
				return
			}
			done <- server.traceMessage("in", body)
		}()
	}
	for range 64 {
		require.NoError(t, <-done)
	}
	decoder := json.NewDecoder(&trace)
	seen := map[int]bool{}
	for range 64 {
		var entry struct {
			Direction string `json:"direction"`
			Message   struct {
				ID     *int `json:"id"`
				Params struct {
					ID int `json:"id"`
				} `json:"params"`
			} `json:"message"`
		}
		require.NoError(t, decoder.Decode(&entry))
		id := entry.Message.Params.ID
		if entry.Direction == "in" {
			require.NotNil(t, entry.Message.ID)
			id = *entry.Message.ID
		} else {
			require.Equal(t, "out", entry.Direction)
		}
		require.False(t, seen[id], "duplicate trace message %d", id)
		seen[id] = true
	}
	for id := range 64 {
		require.True(t, seen[id], "missing trace message %d", id)
	}
	reader := bufio.NewReader(&output)
	for range 32 {
		body, err := ReadMessage(reader)
		require.NoError(t, err)
		require.True(t, json.Valid(body))
	}
	_, err := ReadMessage(reader)
	require.ErrorIs(t, err, io.EOF)
}
