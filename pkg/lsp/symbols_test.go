package lsp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func TestDocumentSymbolsFactory(t *testing.T) {
	src, path := symbolFixture(t, "factory.ub")
	symbols, rpcErr := DocumentSymbolsForText(path, src)
	require.Nil(t, rpcErr)
	require.Equal(t, []string{
		"input.region",
		"local.full-name",
		"constraint",
		"import.aws",
		"library-config.aws",
		"state-move.resource.old -> resource.server",
		"resource.server",
		"data-source.lookup",
		"action.deploy",
		"output.endpoint",
	}, documentSymbolNames(symbols))

	requireSymbolNameRange(t, src, findDocumentSymbol(t, symbols, "input.region"),
		"region: { type", "region")
	requireSymbolNameRange(t, src, findDocumentSymbol(t, symbols, "resource.server"),
		"server: aws.instance", "server")
}

func TestDocumentSymbolsStack(t *testing.T) {
	src, path := symbolFixture(t, "stack.ub")
	symbols, rpcErr := DocumentSymbolsForText(path, src)
	require.Nil(t, rpcErr)
	require.Equal(t, []string{
		"factory.input.region",
		"state.local",
		"encryption.noop",
		"local.label",
		"parallelism",
	}, documentSymbolNames(symbols))
}

func TestDocumentSymbolsProject(t *testing.T) {
	src, path := symbolFixture(t, "project.ub")
	symbols, rpcErr := DocumentSymbolsForText(path, src)
	require.Nil(t, rpcErr)
	require.Equal(t, []string{
		"unobin-version",
		"requires.github.com/cloudboss/unobin-library-std",
		"requires.github.com/example/mono//libs/network",
		"replace.github.com/cloudboss/unobin-library-std",
	}, documentSymbolNames(symbols))
}

func TestDocumentSymbolsProjectLock(t *testing.T) {
	src, path := symbolFixture(t, "project-lock.ub")
	symbols, rpcErr := DocumentSymbolsForText(path, src)
	require.Nil(t, rpcErr)
	require.Equal(t, []string{
		"version",
		"toolchain.unobin-version",
		"deps.github.com/x/core",
	}, documentSymbolNames(symbols))
}

func TestDocumentSymbolsLibrary(t *testing.T) {
	src, path := symbolFixture(t, "library.ub")
	symbols, rpcErr := DocumentSymbolsForText(path, src)
	require.Nil(t, rpcErr)
	require.Equal(t, []string{
		"resource.web",
		"data-source.lookup",
		"action.run",
	}, documentSymbolNames(symbols))
}

func TestSessionDocumentSymbolsReturnsOpenDocumentSymbols(t *testing.T) {
	session := NewSession("dev")
	src, _ := symbolFixture(t, "factory.ub")

	rpcErr := openDocument(t, session, "file:///tmp/factory.ub", 1, src)
	require.Nil(t, rpcErr)
	result, rpcErr := requestDocumentSymbols(t, session, "file:///tmp/factory.ub")
	require.Nil(t, rpcErr)
	symbols, ok := result.([]protocol.DocumentSymbol)
	require.True(t, ok)
	require.Contains(t, documentSymbolNames(symbols), "resource.server")
}

func requestDocumentSymbols(
	t *testing.T,
	session *Session,
	uri string,
) (any, *protocol.ResponseError) {
	t.Helper()
	params := protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/documentSymbol", Params: body,
	})
}

func symbolFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	path := filepath.Join("testdata/ub/symbols/valid", name)
	return ubtest.ReadFixture(t, path), filepath.Base(path)
}

func documentSymbolNames(symbols []protocol.DocumentSymbol) []string {
	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		names = append(names, symbol.Name)
	}
	return names
}

func findDocumentSymbol(
	t *testing.T,
	symbols []protocol.DocumentSymbol,
	name string,
) protocol.DocumentSymbol {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.Name == name {
			return symbol
		}
	}
	t.Fatalf("missing document symbol %s", name)
	return protocol.DocumentSymbol{}
}

func requireSymbolNameRange(
	t *testing.T,
	src string,
	symbol protocol.DocumentSymbol,
	contextText string,
	name string,
) {
	t.Helper()
	contextOffset := strings.Index(src, contextText)
	require.NotEqual(t, -1, contextOffset)
	nameOffset := strings.Index(contextText, name)
	require.NotEqual(t, -1, nameOffset)
	start := contextOffset + nameOffset
	want := protocol.Range{
		Start: OffsetToLSP(src, start),
		End:   OffsetToLSP(src, start+len(name)),
	}
	require.Equal(t, want, symbol.Range)
	require.Equal(t, want, symbol.SelectionRange)
}
