package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func TestDocumentAnalysisKeepsCompositeScopes(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/analysis", "scopes")
	doc, err := newDocument("file:///tmp/library.ub", 1, text)
	require.NoError(t, err)
	analysis := analyzeDocumentSyntax(doc)
	require.NoError(t, analysis.parseErr)
	require.Same(t, doc, analysis.document)
	require.Equal(t, syntax.FileLibrary, analysis.file.Kind)
	require.Len(t, analysis.file.Library.Exports, 2)
	first := &analysis.file.Library.Exports[0].Body
	second := &analysis.file.Library.Exports[1].Body
	firstDecls := analysis.declarationsFor(first)
	secondDecls := analysis.declarationsFor(second)
	require.Equal(t, first.Inputs[0], firstDecls.inputs["name"])
	require.Equal(t, first.Locals[0], firstDecls.locals["value"])
	require.NotContains(t, firstDecls.inputs, "count")
	require.Equal(t, second.Inputs[0], secondDecls.inputs["count"])
	require.Equal(t, second.Locals[0], secondDecls.locals["value"])
	require.NotContains(t, secondDecls.inputs, "name")
	want, rpcErr := DocumentSymbolsForText(doc.Path, text)
	require.Nil(t, rpcErr)
	require.Equal(t, want, analysis.documentSymbols())
	cloned := cloneDocumentSymbols(analysis.documentSymbols())
	cloned[0].Name = "changed"
	cloned[0].Children[0].Name = "changed child"
	require.Equal(t, want, analysis.documentSymbols())
}

func TestDocumentAnalysisKeepsParseFailure(t *testing.T) {
	text := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	doc, err := newDocument("file:///tmp/factory.ub", 1, text)
	require.NoError(t, err)
	analysis := analyzeDocumentSyntax(doc)
	require.Error(t, analysis.parseErr)
	require.Nil(t, analysis.file)
	require.Equal(t, []protocol.DocumentSymbol{}, analysis.documentSymbols())
	require.Empty(t, analysis.declarations)
}

func TestDocumentAnalysisConcurrentSymbols(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/analysis", "scopes")
	doc, err := newDocument("file:///tmp/library.ub", 1, text)
	require.NoError(t, err)
	analysis := analyzeDocumentSyntax(doc)
	require.NoError(t, analysis.parseErr)
	want, rpcErr := DocumentSymbolsForText(doc.Path, text)
	require.Nil(t, rpcErr)
	results := make(chan []protocol.DocumentSymbol, 16)
	for range 16 {
		go func() {
			results <- cloneDocumentSymbols(analysis.documentSymbols())
		}()
	}
	for range 16 {
		require.Equal(t, want, <-results)
	}
}

func TestSessionDocumentSymbolsCannotChangeCachedSymbols(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/analysis", "scopes")
	session := NewSession("dev")
	uri := "file:///tmp/library.ub"
	require.Nil(t, openDocument(t, session, uri, 1, text))
	want, rpcErr := DocumentSymbolsForText("/tmp/library.ub", text)
	require.Nil(t, rpcErr)
	result, rpcErr := requestDocumentSymbols(t, session, uri)
	require.Nil(t, rpcErr)
	symbols := result.([]protocol.DocumentSymbol)
	require.Equal(t, want, symbols)
	symbols[0].Name = "changed"
	symbols[0].Children[0].Name = "changed child"
	result, rpcErr = requestDocumentSymbols(t, session, uri)
	require.Nil(t, rpcErr)
	require.Equal(t, want, result)
}

func TestSessionFeaturesReuseDocumentAnalysis(t *testing.T) {
	_, path, text := completionProject(t)
	uri := PathToFileURI(path)
	session := NewSession("dev")
	builds := 0
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		builds++
		return analyzeDocumentSyntax(doc)
	}
	require.Nil(t, openDocument(t, session, uri, 1, text))
	doc, ok := session.documents.Get(uri)
	require.True(t, ok)
	analysis := session.analysisFor(doc)
	pos := positionInText(text, "input.region", "region")
	wantSymbols, rpcErr := DocumentSymbolsForText(path, text)
	require.Nil(t, rpcErr)
	wantDefinition, rpcErr := DefinitionForText(path, text, pos, session.projects)
	require.Nil(t, rpcErr)
	wantHover, rpcErr := HoverForText(path, text, pos, session.projects)
	require.Nil(t, rpcErr)
	wantCompletion, rpcErr := CompleteForText(path, text, pos, session.projects)
	require.Nil(t, rpcErr)
	for range 3 {
		symbols, rpcErr := requestDocumentSymbols(t, session, uri)
		require.Nil(t, rpcErr)
		require.Equal(t, wantSymbols, symbols)
		definition, rpcErr := requestDefinition(t, session, uri, pos)
		require.Nil(t, rpcErr)
		require.Equal(t, wantDefinition, definition)
		hover, rpcErr := requestHover(t, session, uri, pos)
		require.Nil(t, rpcErr)
		require.Equal(t, wantHover, hover)
		completion, rpcErr := requestCompletion(t, session, uri, pos)
		require.Nil(t, rpcErr)
		require.Equal(t, wantCompletion, completion)
		require.Same(t, analysis, session.analysisFor(doc))
	}
	require.Equal(t, 1, builds)

	changed := strings.ReplaceAll(text, "region", "location")
	require.Nil(t, changeDocument(t, session, uri, 2, changed))
	changedPos := positionInText(changed, "input.location", "location")
	hover, rpcErr := requestHover(t, session, uri, changedPos)
	require.Nil(t, rpcErr)
	require.Contains(t, hover.(*protocol.Hover).Contents.Value, "input location: string")
	require.Equal(t, 2, builds)
	require.Equal(t, text, analysis.document.Text)
	require.Equal(t, int32(1), analysis.document.Version)
	require.Nil(t, closeDocument(t, session, uri))
	require.NotContains(t, session.analyses, uri)
	require.Nil(t, openDocument(t, session, uri, 1, text))
	_, rpcErr = requestDocumentSymbols(t, session, uri)
	require.Nil(t, rpcErr)
	require.Equal(t, 3, builds)
}

func TestSessionDependencyChangeInvalidatesDocumentAnalysis(t *testing.T) {
	root, path, text := completionProject(t)
	session := NewSession("dev")
	uri := PathToFileURI(path)
	require.Nil(t, openDocument(t, session, uri, 1, text))
	doc, ok := session.documents.Get(uri)
	require.True(t, ok)
	before := session.analysisFor(doc)
	require.NoError(t, session.invalidateURI(PathToFileURI(filepath.Join(root, "helper.go"))))
	after := session.analysisFor(doc)
	require.NotSame(t, before, after)
	require.Greater(t, after.dependencyVersion, before.dependencyVersion)
	require.Same(t, doc, after.document)
	require.Equal(t, before.documentSymbols(), after.documentSymbols())
}

func TestSessionDiagnosticsUsesParsedDocument(t *testing.T) {
	session, sent := newDiagnosticSession(t)
	text := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	builds := 0
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		builds++
		return analyzeDocumentSyntax(doc)
	}
	uri := "file:///tmp/factory.ub"
	require.Nil(t, openDocument(t, session, uri, 1, text))
	require.Equal(t, 1, builds)
	params := requirePublishDiagnostics(t, (*sent)[0])
	require.Equal(t, DiagnosticsForText("/tmp/factory.ub", text), params.Diagnostics)
	symbols, rpcErr := requestDocumentSymbols(t, session, uri)
	require.Nil(t, rpcErr)
	require.Equal(t, []protocol.DocumentSymbol{}, symbols)
	require.Equal(t, 1, builds)
}

func TestSessionMalformedCompletionKeepsCachedSource(t *testing.T) {
	_, path, text := inputDeclarationCompletionProject(t)
	text, pos := inputDeclarationSourceWithPrefix(t, text, "d")
	session := NewSession("dev")
	uri := PathToFileURI(path)
	require.Nil(t, openDocument(t, session, uri, 1, text))
	doc, ok := session.documents.Get(uri)
	require.True(t, ok)
	analysis := session.analysisFor(doc)
	require.Error(t, analysis.parseErr)
	want, rpcErr := CompleteForText(path, text, pos, session.projects)
	require.Nil(t, rpcErr)
	require.NotEmpty(t, want.Items)
	for range 2 {
		got, rpcErr := requestCompletion(t, session, uri, pos)
		require.Nil(t, rpcErr)
		require.Equal(t, want, got)
		require.Error(t, analysis.parseErr)
		require.Equal(t, text, analysis.document.Text)
		require.Same(t, analysis, session.analysisFor(doc))
	}
}

func TestSessionCompletionRepairsPartialReference(t *testing.T) {
	_, path, text := completionProject(t)
	text = strings.Replace(text, "value: input.region", "value: input.", 1)
	pos := OffsetToLSP(text, strings.Index(text, "value: input.")+len("value: input."))
	session := NewSession("dev")
	uri := PathToFileURI(path)
	require.Nil(t, openDocument(t, session, uri, 1, text))
	doc, ok := session.documents.Get(uri)
	require.True(t, ok)
	analysis := session.analysisFor(doc)
	require.Error(t, analysis.parseErr)
	for range 2 {
		result, rpcErr := requestCompletion(t, session, uri, pos)
		require.Nil(t, rpcErr)
		requireCompletionLabels(t, result.(protocol.CompletionList), "region", "count")
		require.Nil(t, analysis.file)
		require.Equal(t, text, analysis.document.Text)
		require.Same(t, analysis, session.analysisFor(doc))
	}
}
