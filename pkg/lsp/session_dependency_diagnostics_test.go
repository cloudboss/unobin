package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestSessionDependencyChangesRepublishDiagnostics(t *testing.T) {
	testDependencyDiagnostics(t, nil)
}

func testDependencyDiagnostics(t *testing.T, configure func(*testing.T, *Session)) {
	t.Helper()
	for _, method := range []string{"workspace/didChangeWatchedFiles", "textDocument/didSave"} {
		t.Run(method, func(t *testing.T) {
			root, path, _, cacheRoot := cachedGoDefinitionProject(t)
			text := readDiagnosticFixture(t, "valid/library-api")
			require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
			session := NewSession("dev")
			session.projects = newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
				return &resolve.RemoteResolver{CacheRoot: cacheRoot}, nil
			})
			updates := make(chan protocol.PublishDiagnosticsParams, 8)
			session.SetSender(func(_ string, params any) error {
				updates <- params.(protocol.PublishDiagnosticsParams)
				return nil
			})
			if configure != nil {
					configure(t, session)
			}
			uri := PathToFileURI(path)
			require.Nil(t, openDocument(t, session, uri, 1, text))
			first := nextSessionDiagnostics(t, updates)
			require.Equal(t, uri, first.URI)
			require.Empty(t, first.Diagnostics)
			doc, ok := session.documents.Get(uri)
			require.True(t, ok)
			before := session.analysisFor(doc)
			beforeSymbols, rpcErr := requestDocumentSymbols(t, session, uri)
			require.Nil(t, rpcErr)
			libraryPath := filepath.Join(
				cacheRoot, "imports", "example.com/definition", "abc123", "library.go")
			body, err := os.ReadFile(libraryPath)
			require.NoError(t, err)
			updated := strings.ReplaceAll(
				string(body), "Endpoint shared.Endpoint", "Address shared.Endpoint")
			require.NotEqual(t, string(body), updated)
			require.NoError(t, os.WriteFile(libraryPath, []byte(updated), 0o644))
			var params any = protocol.DidSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: PathToFileURI(libraryPath)},
			}
			if method == "workspace/didChangeWatchedFiles" {
				params = protocol.DidChangeWatchedFilesParams{Changes: []protocol.FileEvent{{
					URI: PathToFileURI(libraryPath), Type: protocol.FileChangeTypeChanged,
				}}}
			}
			encoded, err := json.Marshal(params)
			require.NoError(t, err)
			_, rpcErr = session.HandleRequest(context.Background(), &protocol.RequestMessage{
				JSONRPC: "2.0", Method: method, Params: encoded,
			})
			require.Nil(t, rpcErr)
			second := nextSessionDiagnostics(t, updates)
			require.Equal(t, uri, second.URI)
			require.NotNil(t, second.Version)
			require.Equal(t, int32(1), *second.Version)
			require.Equal(t, []string{`resolve: unknown field "endpoint" on def.server`},
				diagnosticMessages(second.Diagnostics))
			after := session.analysisFor(doc)
			require.NotSame(t, before, after)
			require.Greater(t, after.dependencyVersion, before.dependencyVersion)
			require.Equal(t, text, after.document.Text)
			afterSymbols, rpcErr := requestDocumentSymbols(t, session, uri)
			require.Nil(t, rpcErr)
			require.Equal(t, beforeSymbols, afterSymbols)
		})
	}
}

func nextSessionDiagnostics(
	t *testing.T, updates <-chan protocol.PublishDiagnosticsParams,
) protocol.PublishDiagnosticsParams {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostics did not finish")
		return protocol.PublishDiagnosticsParams{}
	}
}
