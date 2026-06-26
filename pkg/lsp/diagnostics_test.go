package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestDiagnosticFromSingleParseError(t *testing.T) {
	err := parse.Errorf(parse.ErrParse, parse.Position{
		File: "factory.ub", Line: 1, Column: 2, Offset: 1,
	}, "expected value")

	got := DiagnosticsForError("xx", err)
	require.Len(t, got, 1)
	require.Equal(t, protocol.DiagnosticSeverityError, got[0].Severity)
	require.Equal(t, "unobin", got[0].Source)
	require.Equal(t, "parse: expected value", got[0].Message)
	require.Equal(t, protocol.Position{Line: 0, Character: 1}, got[0].Range.Start)
	require.Equal(t, got[0].Range.Start, got[0].Range.End)
}

func TestDiagnosticsFromErrorListPreserveSourceOrder(t *testing.T) {
	errs := parse.NewErrorList(0)
	errs.Addf(parse.ErrSchema, parse.Position{File: "factory.ub", Line: 2, Column: 1, Offset: 4},
		"second")
	errs.Addf(parse.ErrSchema, parse.Position{File: "factory.ub", Line: 1, Column: 1, Offset: 0},
		"first")

	got := DiagnosticsForError("abc\ndef", errs)
	require.Len(t, got, 2)
	require.Equal(t, []string{"schema: first", "schema: second"}, diagnosticMessages(got))
}

func TestDiagnosticsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/diagnostics", func(name string, src []byte) (string, []string) {
		diags := diagnosticsForFixture(t, name, string(src))
		return "", diagnosticMessages(diags)
	}, ubtest.Substring())
}

func TestDiagnosticsUseCachedGoSchema(t *testing.T) {
	source := readDiagnosticFixture(t, "invalid/go-field-typo")

	diags := diagnosticsForDefinitionFixture(t, source)

	require.NotEmpty(t, diags)
	require.Contains(t, strings.Join(diagnosticMessages(diags), "\n"), "unknown-field")
}

func TestDiagnosticsNoFetchStillIgnoresUncachedRemote(t *testing.T) {
	source := readDiagnosticFixture(t, "invalid/missing-remote")

	diags := diagnosticsForMissingRemoteFixture(t, source)

	require.Empty(t, diags)
}

func TestDiagnosticsUseSchemaRootsForLibraryConfigTypes(t *testing.T) {
	source := readDiagnosticFixture(t, "invalid/go-config-field-type")

	diags := diagnosticsForConfigFieldTypeFixture(t, source)

	require.NotEmpty(t, diags)
	require.Contains(t, strings.Join(diagnosticMessages(diags), "\n"),
		"type mismatch: expected string, got integer")
}

func TestDiagnosticsReportLiteralConstraints(t *testing.T) {
	source := ubtest.ReadFixture(t,
		"testdata/ub/diagnostics/invalid/literal-constraint.ub")

	diags := diagnosticsForLiteralConstraintFixture(t, source)

	require.NotEmpty(t, diags)
	messages := strings.Join(diagnosticMessages(diags), "\n")
	require.Contains(t, messages, "expected exactly one to be set")
	require.Contains(t, messages, "expected at most one to be set")
}

func TestDiagnosticsReportForEachNesting(t *testing.T) {
	source := ubtest.ReadFixture(t,
		"testdata/ub/diagnostics/invalid/nested-for-each.ub")

	diags := diagnosticsForNestedForEachFixture(t, source)

	require.NotEmpty(t, diags)
	require.Contains(t, strings.Join(diagnosticMessages(diags), "\n"),
		"@for-each inside a @for-each composite is not supported")
}

func TestDiagnosticsCheckLibraryFiles(t *testing.T) {
	source := ubtest.ReadFixture(t,
		"testdata/ub/diagnostics/invalid/library-file-semantic-error.ub")

	diags := diagnosticsForLibraryFixture(t, source)

	require.NotEmpty(t, diags)
	require.Contains(t, strings.Join(diagnosticMessages(diags), "\n"),
		"library \"missing\" is not imported")
}

func BenchmarkDiagnosticsForTextWithProjectsLargeFactory(b *testing.B) {
	root, sourcePath, source := largeDiagnosticProject(b, 500)
	cache := NewProjectCache(root)

	b.ReportAllocs()
	for b.Loop() {
		diags := DiagnosticsForTextWithProjects(sourcePath, source, cache)
		if len(diags) > 0 {
			b.Fatalf("unexpected diagnostics: %v", diagnosticMessages(diags))
		}
	}
}

func TestSessionDidOpenPublishesParseDiagnostic(t *testing.T) {
	session, sent := newDiagnosticSession(t)
	src := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")

	rpcErr := openDocument(t, session, "file:///tmp/factory.ub", 1, src)
	require.Nil(t, rpcErr)
	require.Len(t, *sent, 1)
	params := requirePublishDiagnostics(t, (*sent)[0])
	require.Len(t, params.Diagnostics, 1)
	require.Equal(t, protocol.DiagnosticSeverityError, params.Diagnostics[0].Severity)
	require.Equal(t, "unobin", params.Diagnostics[0].Source)
	require.Contains(t, params.Diagnostics[0].Message, "parse:")
}

func TestSessionDidOpenPublishesSchemaDiagnostic(t *testing.T) {
	session, sent := newDiagnosticSession(t)
	src := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/wrong-role.ub")

	rpcErr := openDocument(t, session, "file:///tmp/factory.ub", 1, src)
	require.Nil(t, rpcErr)
	require.Len(t, *sent, 1)
	params := requirePublishDiagnostics(t, (*sent)[0])
	require.Len(t, params.Diagnostics, 1)
	require.Contains(t, params.Diagnostics[0].Message, "schema:")
}

func TestSessionDidCloseClearsDiagnostics(t *testing.T) {
	session, sent := newDiagnosticSession(t)
	src := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/wrong-role.ub")
	uri := "file:///tmp/factory.ub"

	rpcErr := openDocument(t, session, uri, 1, src)
	require.Nil(t, rpcErr)
	rpcErr = closeDocument(t, session, uri)
	require.Nil(t, rpcErr)

	require.Len(t, *sent, 2)
	params := requirePublishDiagnostics(t, (*sent)[1])
	require.Equal(t, uri, params.URI)
	require.Nil(t, params.Version)
	require.Empty(t, params.Diagnostics)
}

func TestSessionDidChangeClearsDiagnostics(t *testing.T) {
	session, sent := newDiagnosticSession(t)
	invalid := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/wrong-role.ub")
	valid := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")

	rpcErr := openDocument(t, session, "file:///tmp/factory.ub", 1, invalid)
	require.Nil(t, rpcErr)
	rpcErr = changeDocument(t, session, "file:///tmp/factory.ub", 2, valid)
	require.Nil(t, rpcErr)
	require.Len(t, *sent, 2)
	params := requirePublishDiagnostics(t, (*sent)[1])
	require.Empty(t, params.Diagnostics)
}

type sentNotification struct {
	method string
	params any
}

func newDiagnosticSession(t *testing.T) (*Session, *[]sentNotification) {
	t.Helper()
	session := NewSession("dev")
	sent := []sentNotification{}
	session.SetSender(func(method string, params any) error {
		sent = append(sent, sentNotification{method: method, params: params})
		return nil
	})
	return session, &sent
}

func openDocument(
	t *testing.T,
	session *Session,
	uri string,
	version int32,
	text string,
) *protocol.ResponseError {
	t.Helper()
	params := protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: uri, LanguageID: "unobin", Version: version, Text: text,
	}}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	_, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/didOpen", Params: body,
	})
	return rpcErr
}

func closeDocument(
	t *testing.T,
	session *Session,
	uri string,
) *protocol.ResponseError {
	t.Helper()
	params := protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	_, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/didClose", Params: body,
	})
	return rpcErr
}

func changeDocument(
	t *testing.T,
	session *Session,
	uri string,
	version int32,
	text string,
) *protocol.ResponseError {
	t.Helper()
	params := protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{URI: uri, Version: version},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: text}},
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	_, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/didChange", Params: body,
	})
	return rpcErr
}

func requirePublishDiagnostics(
	t *testing.T,
	notification sentNotification,
) protocol.PublishDiagnosticsParams {
	t.Helper()
	require.Equal(t, "textDocument/publishDiagnostics", notification.method)
	params, ok := notification.params.(protocol.PublishDiagnosticsParams)
	require.True(t, ok)
	return params
}

func readDiagnosticFixture(t testing.TB, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "ub", "diagnostics", filepath.FromSlash(name)+".ub")
	return ubtest.ReadFixture(t, path)
}

func writeDiagnosticProject(
	t *testing.T,
	source string,
	project *deps.Project,
	lock *deps.ProjectLock,
) (string, string) {
	t.Helper()
	root := writeUBProject(t, project, lock)
	sourcePath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(sourcePath, []byte(source), 0o644))
	return root, sourcePath
}

func diagnosticsForFixture(t *testing.T, name string, source string) []protocol.Diagnostic {
	t.Helper()
	switch name {
	case "invalid/go-field-typo":
		return diagnosticsForDefinitionFixture(t, source)
	case "invalid/missing-remote":
		return diagnosticsForMissingRemoteFixture(t, source)
	case "invalid/go-config-field-type":
		return diagnosticsForConfigFieldTypeFixture(t, source)
	case "invalid/literal-constraint":
		return diagnosticsForLiteralConstraintFixture(t, source)
	case "invalid/nested-for-each":
		return diagnosticsForNestedForEachFixture(t, source)
	case "invalid/library-file-semantic-error":
		return diagnosticsForLibraryFixture(t, source)
	default:
		if strings.HasPrefix(name, "support/valid/") {
			return diagnosticsForSupportFixture(t, name, source)
		}
		return DiagnosticsForText(diagnosticFixturePath(name), source)
	}
}

func diagnosticsForDefinitionFixture(t *testing.T, source string) []protocol.Diagnostic {
	t.Helper()
	goDir, err := filepath.Abs(filepath.Join("..", "goschema", "testdata", "definition"))
	require.NoError(t, err)
	root, sourcePath := writeDiagnosticProject(t, source, &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace: map[deps.Dependency]string{
			{URL: "example.com/definition"}: goDir,
		},
	}, nil)
	return DiagnosticsForTextWithProjects(sourcePath, source, NewProjectCache(root))
}

func diagnosticsForMissingRemoteFixture(t *testing.T, source string) []protocol.Diagnostic {
	t.Helper()
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["example.com/missing"] = &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindGo, Version: "v1.0.0", Commit: "missing",
	}
	root, sourcePath := writeDiagnosticProject(t, source, nil, lock)
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: t.TempDir()}, nil
	})
	return DiagnosticsForTextWithProjects(sourcePath, source, cache)
}

func diagnosticsForConfigFieldTypeFixture(t *testing.T, source string) []protocol.Diagnostic {
	t.Helper()
	libraryDir, err := filepath.Abs(filepath.Join("..", "goschema", "testdata", "extroot", "library"))
	require.NoError(t, err)
	sharedDir, err := filepath.Abs(filepath.Join("..", "goschema", "testdata", "extroot", "shared"))
	require.NoError(t, err)
	root, sourcePath := writeDiagnosticProject(t, source, &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace: map[deps.Dependency]string{
			{URL: "example.com/extlib"}: libraryDir,
		},
	}, nil)
	cache := newProjectCacheWithSchemaRoots(root, []goschema.ModuleRoot{
		{Path: "example.com/shared", Dir: sharedDir},
	})
	return DiagnosticsForTextWithProjects(sourcePath, source, cache)
}

func diagnosticsForLiteralConstraintFixture(
	t *testing.T,
	source string,
) []protocol.Diagnostic {
	t.Helper()
	constraintsDir, err := filepath.Abs(
		filepath.Join("..", "goschema", "testdata", "constraints"))
	require.NoError(t, err)
	root := writeUBProject(t, &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace: map[deps.Dependency]string{
			{URL: "example.com/constraints"}: constraintsDir,
		},
	}, nil)
	sourcePath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(sourcePath, []byte(source), 0o644))
	return DiagnosticsForTextWithProjects(sourcePath, source, NewProjectCache(root))
}

func diagnosticsForNestedForEachFixture(
	t testing.TB,
	source string,
) []protocol.Diagnostic {
	t.Helper()
	invalidDir := absDiagnosticFixturePath(t, "testdata/ub/diagnostics/invalid")
	path := filepath.Join(invalidDir, "factory.ub")
	workspace := absDiagnosticFixturePath(t, "testdata/ub/diagnostics")
	return DiagnosticsForTextWithProjects(path, source, NewProjectCache(workspace))
}

func diagnosticsForLibraryFixture(
	t testing.TB,
	source string,
) []protocol.Diagnostic {
	t.Helper()
	path := absDiagnosticFixturePath(t,
		"testdata/ub/diagnostics/invalid/library-file-semantic-error.ub")
	workspace := absDiagnosticFixturePath(t, "testdata/ub/diagnostics")
	return DiagnosticsForTextWithProjects(path, source, NewProjectCache(workspace))
}

func diagnosticsForSupportFixture(
	t testing.TB,
	name string,
	source string,
) []protocol.Diagnostic {
	t.Helper()
	path := absDiagnosticFixturePath(t,
		filepath.Join("testdata", "ub", "diagnostics", filepath.FromSlash(name)+".ub"))
	workspace := absDiagnosticFixturePath(t, "testdata/ub/diagnostics")
	return DiagnosticsForTextWithProjects(path, source, NewProjectCache(workspace))
}

func largeDiagnosticProject(b *testing.B, nodes int) (string, string, string) {
	b.Helper()
	definitionDir, err := filepath.Abs(
		filepath.Join("..", "goschema", "testdata", "definition"))
	require.NoError(b, err)
	root := b.TempDir()
	require.NoError(b, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace: map[deps.Dependency]string{
			{URL: "example.com/definition"}: definitionDir,
		},
	}))
	source := largeDiagnosticFactory(b, nodes)
	sourcePath := filepath.Join(root, "factory.ub")
	require.NoError(b, os.WriteFile(sourcePath, []byte(source), 0o644))
	return root, sourcePath, source
}

func largeDiagnosticFactory(b *testing.B, nodes int) string {
	b.Helper()
	template, err := os.ReadFile(filepath.Join("testdata", "large-diagnostic-factory.ub.tmpl"))
	require.NoError(b, err)
	var nodeText strings.Builder
	for i := range nodes {
		fmt.Fprintf(&nodeText, "    item-%03d: def.lookup { query: 'item-%03d' }\n", i, i)
	}
	return strings.ReplaceAll(string(template), "{{nodes}}", nodeText.String())
}

func absDiagnosticFixturePath(t testing.TB, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	return abs
}

func diagnosticFixturePath(name string) string {
	if strings.Contains(name, "project-") {
		return "project.ub"
	}
	return "factory.ub"
}

func diagnosticMessages(diags []protocol.Diagnostic) []string {
	messages := make([]string, 0, len(diags))
	for _, diag := range diags {
		messages = append(messages, diag.Message)
	}
	return messages
}
