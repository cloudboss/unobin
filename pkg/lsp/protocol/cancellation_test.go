package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerCancelsRunningRequest(t *testing.T) {
	for _, test := range []struct {
		name string
		id   *ID
	}{
		{name: "number", id: NewNumberID(7)},
		{name: "string", id: NewStringID("7")},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputReader, inputWriter := io.Pipe()
			outputReader, outputWriter := io.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			started := make(chan struct{})
			server := NewServer(inputReader, outputWriter, HandlerFunc(func(
				ctx context.Context, req *RequestMessage,
			) (any, *ResponseError) {
				if req.Method != "textDocument/hover" {
					return nil, MethodNotFound(req.Method)
				}
				close(started)
				<-ctx.Done()
				return nil, &ResponseError{
					Code: ErrorCodeRequestCancel, Message: "request canceled",
				}
			}))
			stopped := make(chan struct{})
			var serveErr error
			go func() {
				serveErr = server.Serve(ctx)
				close(stopped)
			}()
			t.Cleanup(func() {
				cancel()
				require.NoError(t, errors.Join(
					inputWriter.Close(), inputReader.Close(),
					outputWriter.Close(), outputReader.Close(),
				))
				select {
				case <-stopped:
				case <-time.After(5 * time.Second):
					t.Error("server did not stop")
				}
			})
			request, err := json.Marshal(RequestMessage{
				JSONRPC: "2.0", ID: test.id, Method: "textDocument/hover",
			})
			require.NoError(t, err)
			require.NoError(t, WriteMessage(inputWriter, request))
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not start")
			}
			params, err := json.Marshal(struct {
				ID *ID `json:"id"`
			}{ID: test.id})
			require.NoError(t, err)
			request, err = json.Marshal(RequestMessage{
				JSONRPC: "2.0", Method: "$/cancelRequest", Params: params,
			})
			require.NoError(t, err)
			writeDone := make(chan error, 1)
			go func() { writeDone <- WriteMessage(inputWriter, request) }()
			type messageResult struct {
				body []byte
				err  error
			}
			response := make(chan messageResult, 1)
			go func() {
				body, err := ReadMessage(outputReader)
				response <- messageResult{body: body, err: err}
			}()
			select {
			case result := <-response:
				require.NoError(t, result.err)
				var message ResponseMessage
				require.NoError(t, json.Unmarshal(result.body, &message))
				require.NotNil(t, message.Error)
				require.Equal(t, ErrorCodeRequestCancel, message.Error.Code)
				require.Equal(t, test.id, message.ID)
			case <-time.After(2 * time.Second):
				t.Fatal("request cancellation was not handled while the request was running")
			}
			require.NoError(t, <-writeDone)
			require.NoError(t, inputWriter.Close())
			select {
			case <-stopped:
				require.NoError(t, serveErr)
			case <-time.After(5 * time.Second):
				t.Fatal("server did not finish after cancellation")
			}
		})
	}
}

func TestServerStopsOnParentCancellation(t *testing.T) {
	for _, name := range []string{"idle", "running request", "pending response"} {
		t.Run(name, func(t *testing.T) {
			inputReader, inputWriter := io.Pipe()
			outputReader, outputWriter := io.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			started := make(chan struct{})
			finished := make(chan struct{})
			server := NewServer(inputReader, outputWriter, HandlerFunc(func(
				ctx context.Context, _ *RequestMessage,
			) (any, *ResponseError) {
				close(started)
				if name == "running request" {
					<-ctx.Done()
				}
				close(finished)
				return "complete", nil
			}))
			stopped := make(chan error, 1)
			go func() { stopped <- server.Serve(ctx) }()
			t.Cleanup(func() {
				cancel()
				require.NoError(t, errors.Join(
					inputWriter.Close(), inputReader.Close(),
					outputWriter.Close(), outputReader.Close(),
				))
			})
			if name != "idle" {
				request, err := json.Marshal(RequestMessage{
					JSONRPC: "2.0", ID: NewNumberID(1), Method: "test/work",
				})
				require.NoError(t, err)
				require.NoError(t, WriteMessage(inputWriter, request))
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("request did not start")
				}
			}
			cancel()
			select {
			case err := <-stopped:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("server did not stop after parent cancellation")
			}
			if name != "idle" {
				select {
				case <-finished:
				default:
					t.Fatal("server returned before the running request finished")
				}
			}
		})
	}
}

func TestServerCancelsQueuedRequestWithDistinctIDType(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	var output bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var handled []ID
	server := NewServer(inputReader, &output, HandlerFunc(func(
		_ context.Context, request *RequestMessage,
	) (any, *ResponseError) {
		handled = append(handled, *request.ID)
		if *request.ID == *NewNumberID(7) {
			close(started)
			<-release
			return "kept", nil
		}
		return "after", nil
	}))
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		unblock()
		require.NoError(t, errors.Join(inputWriter.Close(), inputReader.Close()))
	})
	send := func(id *ID, method string, params any) {
		t.Helper()
		body, err := json.Marshal(params)
		require.NoError(t, err)
		request, err := json.Marshal(RequestMessage{
			JSONRPC: "2.0", ID: id, Method: method, Params: body,
		})
		require.NoError(t, err)
		written := make(chan error, 1)
		go func() { written <- WriteMessage(inputWriter, request) }()
		select {
		case err := <-written:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not read the next request")
		}
	}
	send(NewNumberID(7), "test/work", nil)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	send(NewStringID("7"), "test/work", nil)
	send(nil, "$/cancelRequest", struct {
		ID *ID `json:"id"`
	}{ID: NewStringID("7")})
	send(NewNumberID(99), "test/work", nil)
	require.NoError(t, inputWriter.Close())
	unblock()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not drain queued requests")
	}
	require.Equal(t, []ID{*NewNumberID(7), *NewNumberID(99)}, handled)
	reader := bufio.NewReader(&output)
	for _, expected := range []string{
		`{"jsonrpc":"2.0","id":7,"result":"kept"}`,
		`{"jsonrpc":"2.0","id":"7","error":{"code":-32800,"message":"request canceled"}}`,
		`{"jsonrpc":"2.0","id":99,"result":"after"}`,
	} {
		body, err := ReadMessage(reader)
		require.NoError(t, err)
		require.JSONEq(t, expected, string(body))
	}
	_, err := ReadMessage(reader)
	require.ErrorIs(t, err, io.EOF)
}

func TestServerReturnsFramingFailure(t *testing.T) {
	server := NewServer(strings.NewReader("Content-Length: invalid\r\n\r\n"), io.Discard, nil)
	err := server.Serve(context.Background())
	require.ErrorContains(t, err, "Content-Length")
}
