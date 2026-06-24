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
		diags := DiagnosticsForText(diagnosticFixturePath(name), string(src))
		return "", diagnosticMessages(diags)
	}, ubtest.Substring())
}

func TestDiagnosticsUseCachedGoSchema(t *testing.T) {
	root, sourcePath, source := diagnosticProjectWithFixture(t, "go-field-typo")
	goDir, err := filepath.Abs(filepath.Join("..", "goschema", "testdata", "definition"))
	require.NoError(t, err)
	require.NoError(t, deps.WriteProject(filepath.Join(root, deps.ProjectFileName), &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace: map[deps.Dependency]string{
			{URL: "example.com/definition"}: goDir,
		},
	}))
	cache := NewProjectCache(root)

	diags := DiagnosticsForTextWithProjects(sourcePath, source, cache)
	require.NotEmpty(t, diags)
	require.Contains(t, strings.Join(diagnosticMessages(diags), "\n"), "unknown-field")
}

func TestDiagnosticsMissingRemoteCacheDoesNotPublishDiagnostic(t *testing.T) {
	root, sourcePath, source := diagnosticProjectWithFixture(t, "missing-remote")
	lock := deps.NewProjectLock()
	lock.ToolchainVersion = "dev"
	lock.Deps["example.com/missing"] = &deps.ProjectLockDep{
		Kind: deps.ProjectLockKindGo, Version: "v1.0.0", Commit: "missing",
	}
	require.NoError(t, deps.WriteProjectLock(filepath.Join(root, deps.ProjectLockFileName), lock))
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return &resolve.RemoteResolver{CacheRoot: t.TempDir()}, nil
	})

	diags := DiagnosticsForTextWithProjects(sourcePath, source, cache)
	require.Empty(t, diags)
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

func diagnosticProjectWithFixture(t *testing.T, name string) (string, string, string) {
	t.Helper()
	root := writeUBProject(t, nil, nil)
	source := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", name)
	sourcePath := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(sourcePath, []byte(source), 0o644))
	return root, sourcePath, source
}

func diagnosticFixturePath(name string) string {
	if strings.Contains(name, "wrong-role") {
		return "factory.ub"
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
