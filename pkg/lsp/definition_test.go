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
)

func TestDefinitionInputName(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "input.region", "region"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"region: { type", "region")
}

func TestDefinitionLocalName(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "local.name", "name"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"name: input.region", "name")
}

func TestDefinitionResourceReference(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "resource.server }", "server"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"server: bundle.web", "server")
}

func TestDefinitionDataSourceAndActionReferences(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	cache := NewProjectCache(root)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "data-source.lookup", "lookup"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"lookup: bundle.lookup", "lookup")

	locations, rpcErr = DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "action.deploy", "deploy"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"deploy: bundle.deploy", "deploy")
}

func TestDefinitionUBCompositeSelector(t *testing.T) {
	root, factoryPath, factorySource, libraryPath := definitionProject(t)
	cache := NewProjectCache(root)
	librarySource := ubtest.ReadFixture(t, libraryPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "bundle.web", "web"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, libraryPath, librarySource,
		"web: resource", "web")
}

func TestDefinitionUBCompositeOutputReference(t *testing.T) {
	root, factoryPath, factorySource, libraryPath := definitionProject(t)
	cache := NewProjectCache(root)
	librarySource := ubtest.ReadFixture(t, libraryPath)

	locations, rpcErr := DefinitionForText(factoryPath, factorySource,
		positionInText(factorySource, "resource.server.id", "id"), cache)
	require.Nil(t, rpcErr)
	requireDefinitionLocation(t, locations, libraryPath, librarySource,
		"id: { value: 'web-id' }", "id")
}

func TestSessionDefinitionReturnsLocations(t *testing.T) {
	root, factoryPath, factorySource, _ := definitionProject(t)
	session := NewSession("dev")
	session.projects = NewProjectCache(root)
	uri := PathToFileURI(factoryPath)
	rpcErr := openDocument(t, session, uri, 1, factorySource)
	require.Nil(t, rpcErr)

	result, rpcErr := requestDefinition(t, session, uri,
		positionInText(factorySource, "input.region", "region"))
	require.Nil(t, rpcErr)
	locations, ok := result.([]protocol.Location)
	require.True(t, ok)
	requireDefinitionLocation(t, locations, factoryPath, factorySource,
		"region: { type", "region")
}

func definitionProject(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{},
	}))
	factorySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/factory.ub")
	factoryPath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(factoryPath, []byte(factorySource), 0o644))
	bundleDir := filepath.Join(root, "bundle")
	require.NoError(t, os.MkdirAll(bundleDir, 0o755))
	librarySource := ubtest.ReadFixture(t, "testdata/ub/definition/valid/bundle/library.ub")
	libraryPath := filepath.Join(bundleDir, "library.ub")
	require.NoError(t, os.WriteFile(libraryPath, []byte(librarySource), 0o644))
	return root, factoryPath, factorySource, libraryPath
}

func requestDefinition(
	t *testing.T,
	session *Session,
	uri string,
	pos protocol.Position,
) (any, *protocol.ResponseError) {
	t.Helper()
	params := protocol.DefinitionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/definition", Params: body,
	})
}

func positionInText(text string, contextText string, target string) protocol.Position {
	contextOffset := strings.Index(text, contextText)
	if contextOffset < 0 {
		return protocol.Position{}
	}
	targetOffset := strings.Index(contextText, target)
	if targetOffset < 0 {
		return protocol.Position{}
	}
	return OffsetToLSP(text, contextOffset+targetOffset)
}

func requireDefinitionLocation(
	t *testing.T,
	locations []protocol.Location,
	path string,
	text string,
	contextText string,
	target string,
) {
	t.Helper()
	require.Len(t, locations, 1)
	require.Equal(t, PathToFileURI(path), locations[0].URI)
	contextOffset := strings.Index(text, contextText)
	require.NotEqual(t, -1, contextOffset)
	targetOffset := strings.Index(contextText, target)
	require.NotEqual(t, -1, targetOffset)
	start := contextOffset + targetOffset
	require.Equal(t, protocol.Range{
		Start: OffsetToLSP(text, start),
		End:   OffsetToLSP(text, start+len(target)),
	}, locations[0].Range)
}
