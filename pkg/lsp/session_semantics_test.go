package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestSessionFeaturesReuseCheckedImports(t *testing.T) {
	session, updates, remote, _, path, text := semanticEditorSession(t)
	uri := PathToFileURI(path)
	require.Nil(t, openDocument(t, session, uri, 1, text))
	require.Empty(t, nextSessionDiagnostics(t, updates).Diagnostics)
	before := remote.calls.Load()
	require.Positive(t, before)
	for range 3 {
		hover, rpcErr := requestHover(t, session, uri,
			positionInText(text, "local.value", "value"))
		require.Nil(t, rpcErr)
		require.Equal(t, "local value: string", hover.(*protocol.Hover).Contents.Value)
		hover, rpcErr = requestHover(t, session, uri,
			positionInText(text, "data-source.first.result", "result"))
		require.Nil(t, rpcErr)
		require.Equal(t, "result: string", hover.(*protocol.Hover).Contents.Value)
		completion, rpcErr := requestCompletion(t, session, uri,
			OffsetToLSP(text, strings.Index(text, "query: local.value")+len("query: ")))
		require.Nil(t, rpcErr)
		requireOnlyCompletionLabels(t, completion.(protocol.CompletionList), "value")
		doc, ok := session.documents.Get(uri)
		require.True(t, ok)
		analysis := session.analysisFor(doc)
		body := &analysis.file.Factory.Body
		items, err := valueCompletionItems(path, body, typecheck.TString(),
			analysis.declarationsFor(body), session.projects)
		require.NoError(t, err)
		requireOnlyCompletionLabels(t, completionList(items), "input.query", "local.value")
	}
	require.Equal(t, before, remote.calls.Load())
}

func TestSessionDependencyChangeRefreshesInferredTypes(t *testing.T) {
	session, updates, _, cacheRoot, path, text := semanticEditorSession(t)
	uri := PathToFileURI(path)
	require.Nil(t, openDocument(t, session, uri, 1, text))
	require.Empty(t, nextSessionDiagnostics(t, updates).Diagnostics)
	pos := positionInText(text, "local.value", "value")
	hover, rpcErr := requestHover(t, session, uri, pos)
	require.Nil(t, rpcErr)
	require.Equal(t, "local value: string", hover.(*protocol.Hover).Contents.Value)
	provider := filepath.Join(cacheRoot, "imports", "example.com/definition", "abc123", "library.go")
	before, err := os.ReadFile(provider)
	require.NoError(t, err)
	after := strings.Replace(string(before), "Result string", "Result bool", 1)
	require.NotEqual(t, string(before), after)
	require.NoError(t, os.WriteFile(provider, []byte(after), 0o644))
	hover, rpcErr = requestHover(t, session, uri, pos)
	require.Nil(t, rpcErr)
	require.Equal(t, "local value: string", hover.(*protocol.Hover).Contents.Value)
	params, err := json.Marshal(protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: PathToFileURI(provider),
			Type: protocol.FileChangeTypeChanged}},
	})
	require.NoError(t, err)
	_, rpcErr = session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "workspace/didChangeWatchedFiles", Params: params,
	})
	require.Nil(t, rpcErr)
	publication := nextSessionDiagnostics(t, updates)
	require.Equal(t, int32(1), *publication.Version)
	require.NotEmpty(t, publication.Diagnostics)
	hover, rpcErr = requestHover(t, session, uri, pos)
	require.Nil(t, rpcErr)
	require.Equal(t, "local value: boolean", hover.(*protocol.Hover).Contents.Value)
	doc, ok := session.documents.Get(uri)
	require.True(t, ok)
	analysis := session.analysisFor(doc)
	body := &analysis.file.Factory.Body
	items, err := valueCompletionItems(path, body, typecheck.TString(),
		analysis.declarationsFor(body), session.projects)
	require.NoError(t, err)
	requireOnlyCompletionLabels(t, completionList(items), "input.query")
}

func semanticEditorSession(t *testing.T) (
	*Session, <-chan protocol.PublishDiagnosticsParams, *countedEditorRemote, string, string, string,
) {
	t.Helper()
	root, path, _, cacheRoot := cachedGoDefinitionProject(t)
	text := ubtest.ReadValidFixture(t, "testdata/ub/analysis", "imported-local-types")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
	remote := &countedEditorRemote{source: &resolve.RemoteResolver{CacheRoot: cacheRoot}}
	session := NewSession("dev")
	session.projects = newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return remote, nil
	})
	updates := make(chan protocol.PublishDiagnosticsParams, 8)
	session.SetSender(func(_ string, params any) error {
		updates <- params.(protocol.PublishDiagnosticsParams)
		return nil
	})
	return session, updates, remote, cacheRoot, path, text
}

type countedEditorRemote struct {
	source cachedRemoteSource
	calls  atomic.Int64
}

func (r *countedEditorRemote) CachedSource(
	ref *resolve.RemoteImport, commit string,
) (*resolve.Source, bool, error) {
	r.calls.Add(1)
	return r.source.CachedSource(ref, commit)
}
