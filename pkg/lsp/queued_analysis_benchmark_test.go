package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func BenchmarkServeQueuedDocumentEdits(b *testing.B) {
	for _, nodes := range []int{50, 500} {
		b.Run(fmt.Sprintf("nodes=%d", nodes), func(b *testing.B) {
			fixture := newEditorBenchmarkFixture(b, nodes)
			client := newQueuedEditorBenchmark(b)
			client.notify(b, "textDocument/didOpen", protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{URI: fixture.uri, Version: 1, Text: fixture.text},
			})
			client.diagnostics(b, fixture.uri, 0, 1)
			texts := []string{fixture.text, strings.Replace(fixture.text, "example", "updated", 1)}
			version := int32(1)
			publications := 0
			const edits = 8
			for b.Loop() {
				previous := version
				for range edits {
					version++
					client.notify(b, "textDocument/didChange", protocol.DidChangeTextDocumentParams{
						TextDocument: protocol.VersionedTextDocumentIdentifier{
							URI: fixture.uri, Version: version,
						},
						ContentChanges: []protocol.TextDocumentContentChangeEvent{
							{Text: texts[int(version)%len(texts)]},
						},
					})
				}
				publications += client.diagnostics(b, fixture.uri, previous, version)
			}
			b.ReportMetric(float64(nodes), "nodes")
			b.ReportMetric(edits, "edits")
			b.ReportMetric(float64(publications)/float64(b.N), "publications/op")
		})
	}
}

type queuedEditorBenchmark struct {
	input   io.Writer
	updates <-chan protocol.PublishDiagnosticsParams
	errors  <-chan error
}

func newQueuedEditorBenchmark(b *testing.B) queuedEditorBenchmark {
	b.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan protocol.PublishDiagnosticsParams, 64)
	failures := make(chan error, 2)
	served := make(chan struct{})
	read := make(chan struct{})
	go func() {
		defer func() {
			_ = inputReader.Close()
			_ = outputWriter.Close()
			close(served)
		}()
		if err := Serve(ctx, inputReader, outputWriter, "benchmark"); err != nil && ctx.Err() == nil {
			failures <- err
		}
	}()
	go func() {
		defer func() {
			_ = outputReader.Close()
			close(read)
		}()
		reader := bufio.NewReader(outputReader)
		for {
			body, err := protocol.ReadMessage(reader)
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					failures <- err
				}
				return
			}
			var message struct {
				Method string                            `json:"method"`
				Params protocol.PublishDiagnosticsParams `json:"params"`
			}
			if err := json.Unmarshal(body, &message); err != nil {
				failures <- err
				return
			}
			if message.Method != "textDocument/publishDiagnostics" {
				failures <- fmt.Errorf("unexpected notification %q", message.Method)
				return
			}
			select {
			case updates <- message.Params:
			case <-ctx.Done():
				return
			}
		}
	}()
	b.Cleanup(func() {
		cancel()
		for _, closer := range []io.Closer{inputWriter, inputReader, outputWriter, outputReader} {
			require.NoError(b, closer.Close())
		}
		for _, stopped := range []<-chan struct{}{served, read} {
			select {
			case <-stopped:
			case <-time.After(20 * time.Second):
				b.Error("editor connection did not stop")
			}
		}
	})
	return queuedEditorBenchmark{input: inputWriter, updates: updates, errors: failures}
}

func (c queuedEditorBenchmark) notify(b *testing.B, method string, params any) {
	b.Helper()
	body, err := json.Marshal(params)
	require.NoError(b, err)
	request, err := json.Marshal(protocol.RequestMessage{
		JSONRPC: "2.0", Method: method, Params: body,
	})
	require.NoError(b, err)
	require.NoError(b, protocol.WriteMessage(c.input, request))
}

func (c queuedEditorBenchmark) diagnostics(b *testing.B, uri string, previous, target int32) int {
	b.Helper()
	count := 0
	timeout := time.NewTimer(20 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case update := <-c.updates:
			require.Equal(b, uri, update.URI)
			require.NotNil(b, update.Version)
			require.Greater(b, *update.Version, previous)
			require.LessOrEqual(b, *update.Version, target)
			require.Empty(b, update.Diagnostics)
			previous = *update.Version
			count++
			if previous == target {
				return count
			}
		case err := <-c.errors:
			b.Fatal(err)
		case <-timeout.C:
			b.Fatalf("diagnostics did not finish for version %d", target)
		}
	}
}
