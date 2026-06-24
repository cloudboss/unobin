package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestCompletionFileRolesAtEmptySource(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "factory.ub")

	list, rpcErr := CompleteForText(path, "", protocol.Position{}, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "factory", "stack", "project", "project-lock")
}

func TestCompletionFactoryBlocks(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "factory.ub")
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "factory-empty-block")
	offset := strings.Index(source, "\n\n") + 1
	pos := OffsetToLSP(source, offset)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "inputs", "resources", "outputs", "constraints")
}

func TestCompletionInputDeclarationKeys(t *testing.T) {
	root, path, source := completionProject(t)
	pos := positionInText(source, "type: string", "type")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "type", "description", "default")
}

func TestCompletionTypeConstructors(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "type: string", "type: ")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "string", "integer", "list", "object", "optional")
}

func TestCompletionResourcesSelectorUsesResourceSchemaOnly(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "def.server", "def.")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server")
	requireNotCompletionLabels(t, list, "lookup", "deploy", "slug")
}

func TestCompletionGoBackedBodyFieldsAtBlankPosition(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source = strings.Replace(source, "server-name: 'web'", "", 1)
	pos := positionInText(source, "settings:", "")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "id", "server-name", "settings")
}

func TestCompletionRoots(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "null", "")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list,
		"input", "local", "resource", "data-source", "action", "@core")
}

func TestCompletionLocalNames(t *testing.T) {
	root, path, source := completionProject(t)
	cache := NewProjectCache(root)

	inputSource, inputPos := sourceWithCompletionCursor(t, source, "input.region", "input.")
	list, rpcErr := CompleteForText(path, inputSource, inputPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "region", "count")

	localSource, localPos := sourceWithCompletionCursor(t, source, "local.name", "local.")
	list, rpcErr = CompleteForText(path, localSource, localPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "name")
}

func TestCompletionNodeNames(t *testing.T) {
	root, path, source := completionProject(t)
	cache := NewProjectCache(root)

	resourceSource, resourcePos := sourceWithCompletionCursor(
		t, source, "resource.server", "resource.",
	)
	list, rpcErr := CompleteForText(path, resourceSource, resourcePos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server")

	dataSource, dataPos := sourceWithCompletionCursor(
		t, source, "data-source.lookup", "data-source.",
	)
	list, rpcErr = CompleteForText(path, dataSource, dataPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "lookup")

	actionSource, actionPos := sourceWithCompletionCursor(
		t, source, "action.deploy", "action.",
	)
	list, rpcErr = CompleteForText(path, actionSource, actionPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "deploy")
}

func TestCompletionGoBackedSelectors(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "def.server", "def.")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server")
	requireNotCompletionLabels(t, list, "lookup", "deploy", "slug")
}

func TestCompletionSelectorWithMissingCachedSourceReturnsEmptyList(t *testing.T) {
	_, path, source, cache := missingCachedGoDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "def.slug('v1')", "def.")

	list, rpcErr := CompleteForText(path, source, pos, cache)
	require.Nil(t, rpcErr)
	require.Empty(t, list.Items)
}

func TestCompletionGoBackedBodyFields(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)

	list, rpcErr := CompleteForText(path, source,
		positionInText(source, "server-name: 'web'", "server-name"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "id", "server-name", "settings")
}

func TestCompletionUnknownGoNodeSelectorReturnsEmptyList(t *testing.T) {
	root, path, source, _ := goDefinitionProjectFixture(
		t, "testdata/ub/definition/valid/go-backed-unknown-node.ub",
	)

	list, rpcErr := CompleteForText(path, source,
		positionInText(source, "server-name: 'web'", "server-name"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.Empty(t, list.Items)
}

func TestCompletionGoBackedConfigFields(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)

	list, rpcErr := CompleteForText(path, source,
		positionInText(source, "region: 'us-east-1'", "region"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "region", "retry")
}

func TestCompletionDoesNotFetchRemotes(t *testing.T) {
	root, path, source := completionProject(t)
	remote := &failingCachedRemote{t: t}
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return remote, nil
	})

	_, rpcErr := CompleteForText(path, source,
		positionInText(source, "value: null", "null"), cache)
	require.Nil(t, rpcErr)
}

func TestSessionCompletionReturnsItems(t *testing.T) {
	_, path, source := completionProject(t)
	session := NewSession("dev")
	uri := PathToFileURI(path)
	rpcErr := openDocument(t, session, uri, 1, source)
	require.Nil(t, rpcErr)

	result, rpcErr := requestCompletion(t, session, uri,
		positionInText(source, "input.region", "region"))
	require.Nil(t, rpcErr)
	list, ok := result.(protocol.CompletionList)
	require.True(t, ok)
	requireCompletionLabels(t, list, "region", "count")
}

func completionProject(t *testing.T) (string, string, string) {
	t.Helper()
	root := writeUBProject(t, &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{},
	}, nil)
	source := ubtest.ReadFixture(t, "testdata/ub/completion/valid/factory.ub")
	path := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	return root, path, source
}

func sourceWithCompletionCursor(
	t *testing.T,
	source string,
	old string,
	new string,
) (string, protocol.Position) {
	t.Helper()
	offset := strings.Index(source, old)
	require.NotEqual(t, -1, offset)
	source = source[:offset] + new + source[offset+len(old):]
	return source, OffsetToLSP(source, offset+len(new))
}

func requestCompletion(
	t *testing.T,
	session *Session,
	uri string,
	pos protocol.Position,
) (any, *protocol.ResponseError) {
	t.Helper()
	params := protocol.CompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/completion", Params: body,
	})
}

func requireCompletionLabels(
	t *testing.T,
	list protocol.CompletionList,
	labels ...string,
) {
	t.Helper()
	byLabel := map[string]bool{}
	for _, item := range list.Items {
		byLabel[item.Label] = true
	}
	for _, label := range labels {
		require.Truef(t, byLabel[label], "missing completion label %q in %#v", label, list.Items)
	}
}

func requireNotCompletionLabels(
	t *testing.T,
	list protocol.CompletionList,
	labels ...string,
) {
	t.Helper()
	byLabel := map[string]bool{}
	for _, item := range list.Items {
		byLabel[item.Label] = true
	}
	for _, label := range labels {
		require.Falsef(t, byLabel[label], "unexpected completion label %q in %#v", label, list.Items)
	}
}

type failingCachedRemote struct {
	t *testing.T
}

func (r *failingCachedRemote) CachedSource(
	ref *resolve.RemoteImport,
	commit string,
) (*resolve.Source, bool, error) {
	r.t.Helper()
	r.t.Fatalf("completion fetched remote source %s at %s", ref.URL, commit)
	return nil, false, nil
}
