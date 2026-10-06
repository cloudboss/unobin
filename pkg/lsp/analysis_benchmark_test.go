package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

type editorBenchmarkFixture struct {
	nodes    int
	root     string
	uri      string
	text     string
	provider string
	requests []protocol.RequestMessage
}

type editorBenchmarkResults struct {
	symbols    []protocol.DocumentSymbol
	definition []protocol.Location
	hover      *protocol.Hover
	completion protocol.CompletionList
}

func newEditorBenchmarkFixture(b *testing.B, nodes int) editorBenchmarkFixture {
	b.Helper()
	text := ubtest.ReadValidFixture(b, "testdata/ub/analysis-benchmark",
		fmt.Sprintf("factory-%d", nodes))
	root := b.TempDir()
	provider := filepath.Join(root, "provider")
	require.NoError(b, os.CopyFS(provider, os.DirFS("../goschema/testdata/definition")))
	require.NoError(b, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace: map[deps.Dependency]string{
			{URL: "example.com/definition"}: provider,
		},
	}))
	path := filepath.Join(root, "factory.ub")
	require.NoError(b, os.WriteFile(path, []byte(text), 0o644))
	uri := PathToFileURI(path)
	document := protocol.TextDocumentIdentifier{URI: uri}
	output := strings.Index(text, "data-source.item-0000.result")
	require.GreaterOrEqual(b, output, 0)
	definition := strings.Index(text, "local.filter")
	require.GreaterOrEqual(b, definition, 0)
	params := []struct {
		method string
		value  any
	}{
		{method: "textDocument/documentSymbol",
			value: protocol.DocumentSymbolParams{TextDocument: document}},
		{method: "textDocument/definition", value: protocol.DefinitionParams{
			TextDocument: document,
			Position:     OffsetToLSP(text, definition+len("local.")+1),
		}},
		{method: "textDocument/hover", value: protocol.HoverParams{
			TextDocument: document,
			Position:     OffsetToLSP(text, output+len("data-source.item-0000.")+1),
		}},
		{method: "textDocument/completion", value: protocol.CompletionParams{
			TextDocument: document,
			Position:     OffsetToLSP(text, definition+len("local.")+1),
		}},
	}
	requests := make([]protocol.RequestMessage, 0, len(params))
	for _, param := range params {
		body, err := json.Marshal(param.value)
		require.NoError(b, err)
		requests = append(requests, protocol.RequestMessage{
			JSONRPC: "2.0", ID: protocol.NewNumberID(1), Method: param.method, Params: body,
		})
	}
	return editorBenchmarkFixture{
		nodes: nodes, root: root, uri: uri, text: text,
		provider: filepath.Join(provider, "library.go"), requests: requests,
	}
}

func BenchmarkSessionColdDocument(b *testing.B) {
	for _, nodes := range []int{50, 500, 2000} {
		b.Run(fmt.Sprintf("nodes=%d", nodes), func(b *testing.B) {
			fixture := newEditorBenchmarkFixture(b, nodes)
			for b.Loop() {
				session, updates := fixture.open(b)
				fixture.checkDiagnostics(b, updates, 1)
				stopEditorBenchmark(b, session)
			}
			b.ReportMetric(float64(nodes), "nodes")
			b.ReportMetric(1, "publications")
		})
	}
}

func BenchmarkSessionWarmFeatures(b *testing.B) {
	for _, nodes := range []int{50, 500, 2000} {
		b.Run(fmt.Sprintf("nodes=%d", nodes), func(b *testing.B) {
			fixture := newEditorBenchmarkFixture(b, nodes)
			session, updates := fixture.open(b)
			fixture.checkDiagnostics(b, updates, 1)
			b.Cleanup(func() { stopEditorBenchmark(b, session) })
			var results editorBenchmarkResults
			for b.Loop() {
				results = fixture.features(b, session, 100)
			}
			fixture.checkFeatures(b, results, "string")
			b.ReportMetric(float64(nodes), "nodes")
			b.ReportMetric(100, "requests")
		})
	}
}

func BenchmarkSessionDocumentEdits(b *testing.B) {
	fixture := newEditorBenchmarkFixture(b, 500)
	session, updates := fixture.open(b)
	fixture.checkDiagnostics(b, updates, 1)
	b.Cleanup(func() { stopEditorBenchmark(b, session) })
	texts := []string{fixture.text, strings.Replace(fixture.text, "example", "updated", 1)}
	version := int32(1)
	var results editorBenchmarkResults
	for b.Loop() {
		version++
		params := protocol.DidChangeTextDocumentParams{
			TextDocument: protocol.VersionedTextDocumentIdentifier{URI: fixture.uri, Version: version},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{
				{Text: texts[int(version)%len(texts)]},
			},
		}
		editorBenchmarkRequest(b, session, "textDocument/didChange", params)
		fixture.checkDiagnostics(b, updates, version)
		results = fixture.features(b, session, 4)
	}
	fixture.checkFeatures(b, results, "string")
	b.ReportMetric(500, "nodes")
	b.ReportMetric(1, "publications")
	b.ReportMetric(4, "requests")
}

func BenchmarkSessionDependencyChange(b *testing.B) {
	fixture := newEditorBenchmarkFixture(b, 500)
	session, updates := fixture.open(b)
	fixture.checkDiagnostics(b, updates, 1)
	session.SetSender(nil)
	b.Cleanup(func() { stopEditorBenchmark(b, session) })
	original, err := os.ReadFile(fixture.provider)
	require.NoError(b, err)
	modified := []byte(strings.Replace(string(original), "Result string", "Result bool", 1))
	require.NotEqual(b, original, modified)
	bodies := [][]byte{original, modified}
	types := []string{"string", "boolean"}
	variant := 0
	var results editorBenchmarkResults
	for b.Loop() {
		variant = 1 - variant
		if err := os.WriteFile(fixture.provider, bodies[variant], 0o644); err != nil {
			b.Fatal(err)
		}
		editorBenchmarkRequest(b, session, "workspace/didChangeWatchedFiles",
			protocol.DidChangeWatchedFilesParams{Changes: []protocol.FileEvent{{
				URI: PathToFileURI(fixture.provider), Type: protocol.FileChangeTypeChanged,
			}}})
		results = fixture.features(b, session, 4)
		if results.hover == nil || !strings.Contains(results.hover.Contents.Value, types[variant]) {
			b.Fatalf("dependency hover uses stale metadata: %+v", results.hover)
		}
	}
	fixture.checkFeatures(b, results, types[variant])
	b.ReportMetric(500, "nodes")
	b.ReportMetric(1, "dependency-changes")
	b.ReportMetric(4, "requests")
}

func (f editorBenchmarkFixture) open(
	b *testing.B,
) (*Session, <-chan protocol.PublishDiagnosticsParams) {
	b.Helper()
	session := NewSession("benchmark")
	session.projects = NewProjectCache(f.root)
	updates := make(chan protocol.PublishDiagnosticsParams, 64)
	session.SetSender(func(method string, params any) error {
		if method != "textDocument/publishDiagnostics" {
			return fmt.Errorf("unexpected notification %q", method)
		}
		publication, ok := params.(protocol.PublishDiagnosticsParams)
		if !ok {
			return fmt.Errorf("unexpected diagnostic parameters %T", params)
		}
		updates <- publication
		return nil
	})
	editorBenchmarkRequest(b, session, "textDocument/didOpen", protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: f.uri, Version: 1, Text: f.text},
	})
	return session, updates
}

func (f editorBenchmarkFixture) checkDiagnostics(
	b *testing.B,
	updates <-chan protocol.PublishDiagnosticsParams,
	version int32,
) {
	b.Helper()
	select {
	case publication := <-updates:
		require.Equal(b, f.uri, publication.URI)
		require.NotNil(b, publication.Version)
		require.Equal(b, version, *publication.Version)
		require.Empty(b, publication.Diagnostics)
	case <-time.After(20 * time.Second):
		b.Fatal("diagnostics did not finish")
	}
}

func (f editorBenchmarkFixture) features(
	b *testing.B,
	session *Session,
	count int,
) editorBenchmarkResults {
	b.Helper()
	var results editorBenchmarkResults
	for i := range count {
		request := &f.requests[i%len(f.requests)]
		result, rpcErr := session.HandleRequest(context.Background(), request)
		if rpcErr != nil {
			b.Fatalf("%s: %s", request.Method, rpcErr.Message)
		}
		switch request.Method {
		case "textDocument/documentSymbol":
			results.symbols = result.([]protocol.DocumentSymbol)
		case "textDocument/definition":
			results.definition = result.([]protocol.Location)
		case "textDocument/hover":
			results.hover = result.(*protocol.Hover)
		case "textDocument/completion":
			results.completion = result.(protocol.CompletionList)
		}
	}
	return results
}

func (f editorBenchmarkFixture) checkFeatures(
	b *testing.B,
	results editorBenchmarkResults,
	typeName string,
) {
	b.Helper()
	names := []string{"input.query", "local.filter", "import.def", "library-config.def"}
	for i := range f.nodes {
		names = append(names, fmt.Sprintf("data-source.item-%04d", i))
	}
	names = append(names, "output.result")
	var actual []string
	for _, symbol := range results.symbols {
		actual = append(actual, symbol.Name)
	}
	require.Equal(b, names, actual)
	require.NotNil(b, results.hover)
	require.Contains(b, results.hover.Contents.Value, "result")
	require.Contains(b, results.hover.Contents.Value, typeName)
	start := OffsetToLSP(f.text, strings.Index(f.text, "filter:"))
	end := OffsetToLSP(f.text, strings.Index(f.text, "filter:")+len("filter"))
	require.Equal(b, []protocol.Location{{
		URI: f.uri, Range: protocol.Range{Start: start, End: end},
	}}, results.definition)
	var labels []string
	for _, item := range results.completion.Items {
		labels = append(labels, item.Label)
	}
	require.Equal(b, []string{"filter"}, labels)
}

func editorBenchmarkRequest(b *testing.B, session *Session, method string, params any) {
	b.Helper()
	body, err := json.Marshal(params)
	require.NoError(b, err)
	_, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: method, Params: body,
	})
	require.Nil(b, rpcErr)
}

func stopEditorBenchmark(b *testing.B, session *Session) {
	b.Helper()
	editorBenchmarkRequest(b, session, "shutdown", nil)
}
