package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func TestSessionDiagnosticsRejectObsoleteCompletion(t *testing.T) {
	invalid := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	valid := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")
	session := NewSession("dev")
	updates := make(chan protocol.PublishDiagnosticsParams, 8)
	session.SetSender(func(method string, params any) error {
		if method != "textDocument/publishDiagnostics" {
			return nil
		}
		updates <- params.(protocol.PublishDiagnosticsParams)
		return nil
	})
	started := make(chan *Document, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		started <- doc
		if doc.Version == 1 {
			<-release
		}
		return analyzeDocumentSyntax(doc)
	}
	session.startDiagnosticWorkers(context.Background(), 2)
	t.Cleanup(func() { require.NoError(t, session.closeDiagnosticWorkers()) })
	t.Cleanup(unblock)
	uri := "file:///tmp/factory.ub"
	require.Nil(t, openDocument(t, session, uri, 1, invalid))
	require.Equal(t, int32(1), nextDiagnosticDocument(t, started).Version)
	changed := make(chan *protocol.ResponseError, 1)
	go func() { changed <- changeDocument(t, session, uri, 2, valid) }()
	select {
	case rpcErr := <-changed:
		require.Nil(t, rpcErr)
	case <-time.After(5 * time.Second):
		t.Fatal("document change waited for obsolete analysis")
	}
	require.Equal(t, int32(2), nextDiagnosticDocument(t, started).Version)
	select {
	case update := <-updates:
		require.Equal(t, uri, update.URI)
		require.NotNil(t, update.Version)
		require.Equal(t, int32(2), *update.Version)
		require.Empty(t, update.Diagnostics)
	case <-time.After(5 * time.Second):
		t.Fatal("latest diagnostics did not finish")
	}
	unblock()
	require.NoError(t, session.closeDiagnosticWorkers())
	select {
	case update := <-updates:
		t.Fatalf("obsolete diagnostics published for version %v", update.Version)
	default:
	}
}

func TestSessionQueuedChangesAnalyzeLatestVersion(t *testing.T) {
	invalid := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	valid := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")
	session := NewSession("dev")
	updates := make(chan protocol.PublishDiagnosticsParams, 8)
	session.SetSender(func(_ string, params any) error {
		updates <- params.(protocol.PublishDiagnosticsParams)
		return nil
	})
	started := make(chan *Document, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		started <- doc
		if doc.Version == 1 {
			<-release
		}
		return analyzeDocumentSyntax(doc)
	}
	session.startDiagnosticWorkers(context.Background(), 1)
	t.Cleanup(func() { require.NoError(t, session.closeDiagnosticWorkers()) })
	t.Cleanup(unblock)
	uri := "file:///tmp/factory.ub"
	require.Nil(t, openDocument(t, session, uri, 1, invalid))
	require.Equal(t, int32(1), nextDiagnosticDocument(t, started).Version)
	for version := int32(2); version <= 20; version++ {
		require.Nil(t, changeDocument(t, session, uri, version, valid))
	}
	unblock()
	require.Equal(t, int32(20), nextDiagnosticDocument(t, started).Version)
	update := nextSessionDiagnostics(t, updates)
	require.Equal(t, uri, update.URI)
	require.Equal(t, int32(20), *update.Version)
	require.Empty(t, update.Diagnostics)
	require.NoError(t, session.closeDiagnosticWorkers())
	select {
	case doc := <-started:
		t.Fatalf("obsolete queued version started: %d", doc.Version)
	default:
	}
	select {
	case update := <-updates:
		t.Fatalf("obsolete queued version published: %v", update.Version)
	default:
	}
}

func TestSessionAsyncDependencyChangesRepublishDiagnostics(t *testing.T) {
	testDependencyDiagnostics(t, func(t *testing.T, session *Session) {
		session.startDiagnosticWorkers(context.Background(), 2)
		t.Cleanup(func() { require.NoError(t, session.closeDiagnosticWorkers()) })
	})
}

func TestSessionConcurrentFeaturesSharePendingSyntax(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")
	session := NewSession("dev")
	updates := make(chan protocol.PublishDiagnosticsParams, 1)
	session.SetSender(func(_ string, params any) error {
		updates <- params.(protocol.PublishDiagnosticsParams)
		return nil
	})
	var builds atomic.Int32
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		builds.Add(1)
		return analyzeDocumentSyntax(doc)
	}
	session.startDiagnosticWorkers(context.Background(), 2)
	t.Cleanup(func() { require.NoError(t, session.closeDiagnosticWorkers()) })
	uri := "file:///tmp/factory.ub"
	require.Nil(t, openDocument(t, session, uri, 1, text))
	var requests sync.WaitGroup
	for range 32 {
		requests.Go(func() {
			symbols, rpcErr := requestDocumentSymbols(t, session, uri)
			require.Nil(t, rpcErr)
			require.Equal(t, []protocol.DocumentSymbol{}, symbols)
		})
	}
	requests.Wait()
	update := nextSessionDiagnostics(t, updates)
	require.Empty(t, update.Diagnostics)
	require.Equal(t, int32(1), *update.Version)
	require.Equal(t, int32(1), builds.Load())
}

func TestSessionShutdownJoinsDiagnosticWorkers(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")
	session := NewSession("dev")
	updates := make(chan protocol.PublishDiagnosticsParams, 1)
	session.SetSender(func(_ string, params any) error {
		updates <- params.(protocol.PublishDiagnosticsParams)
		return nil
	})
	started := make(chan *Document, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		started <- doc
		<-release
		return analyzeDocumentSyntax(doc)
	}
	session.startDiagnosticWorkers(context.Background(), 1)
	t.Cleanup(func() { require.NoError(t, session.closeDiagnosticWorkers()) })
	t.Cleanup(unblock)
	require.Nil(t, openDocument(t, session, "file:///tmp/factory.ub", 1, text))
	nextDiagnosticDocument(t, started)
	stopped := make(chan *protocol.ResponseError, 1)
	go func() {
		_, rpcErr := session.HandleRequest(context.Background(), &protocol.RequestMessage{
			JSONRPC: "2.0", ID: protocol.NewNumberID(1), Method: "shutdown",
		})
		stopped <- rpcErr
	}()
	require.Eventually(t, session.Shutdown, 5*time.Second, time.Millisecond)
	select {
	case <-stopped:
		t.Fatal("shutdown returned before analysis finished")
	default:
	}
	unblock()
	select {
	case rpcErr := <-stopped:
		require.Nil(t, rpcErr)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join analysis")
	}
	select {
	case update := <-updates:
		t.Fatalf("diagnostics published during shutdown: %v", update.Version)
	default:
	}
}

func TestSessionCloseAndReopenRejectsOldDiagnostics(t *testing.T) {
	invalid := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	valid := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")
	session := NewSession("dev")
	updates := make(chan protocol.PublishDiagnosticsParams, 8)
	session.SetSender(func(_ string, params any) error {
		updates <- params.(protocol.PublishDiagnosticsParams)
		return nil
	})
	started := make(chan *Document, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var first atomic.Bool
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		started <- doc
		if first.CompareAndSwap(false, true) {
			<-release
		}
		return analyzeDocumentSyntax(doc)
	}
	session.startDiagnosticWorkers(context.Background(), 2)
	t.Cleanup(func() { require.NoError(t, session.closeDiagnosticWorkers()) })
	t.Cleanup(unblock)
	uri := "file:///tmp/factory.ub"
	require.Nil(t, openDocument(t, session, uri, 1, invalid))
	nextDiagnosticDocument(t, started)
	require.Nil(t, closeDocument(t, session, uri))
	empty := nextSessionDiagnostics(t, updates)
	require.Equal(t, uri, empty.URI)
	require.Nil(t, empty.Version)
	require.Empty(t, empty.Diagnostics)
	require.Nil(t, openDocument(t, session, uri, 1, valid))
	nextDiagnosticDocument(t, started)
	current := nextSessionDiagnostics(t, updates)
	require.Equal(t, uri, current.URI)
	require.Equal(t, int32(1), *current.Version)
	require.Empty(t, current.Diagnostics)
	unblock()
	require.NoError(t, session.closeDiagnosticWorkers())
	select {
	case update := <-updates:
		t.Fatalf("closed document diagnostics published after reopen: %v", update.Version)
	default:
	}
}

func TestServeReturnsDiagnosticWriteFailure(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")
	inputReader, inputWriter := io.Pipe()
	failure := errors.New("diagnostic write failed")
	writer := &diagnosticFailureWriter{err: failure, release: make(chan struct{})}
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(writer.release) }) }
	stopped := make(chan error, 1)
	go func() { stopped <- Serve(context.Background(), inputReader, writer, "dev") }()
	t.Cleanup(func() {
		unblock()
		require.NoError(t, errors.Join(inputWriter.Close(), inputReader.Close()))
	})
	params, err := json.Marshal(protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: "file:///tmp/factory.ub", Version: 1, Text: text},
	})
	require.NoError(t, err)
	request, err := json.Marshal(protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/didOpen", Params: params,
	})
	require.NoError(t, err)
	require.NoError(t, protocol.WriteMessage(inputWriter, request))
	unblock()
	select {
	case err := <-stopped:
		require.ErrorIs(t, err, failure)
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic write failure did not stop the server")
	}
}

type diagnosticFailureWriter struct {
	err     error
	release chan struct{}
}

func (w *diagnosticFailureWriter) Write([]byte) (int, error) {
	<-w.release
	return 0, w.err
}

func TestSessionCancelsFeatureWaitingForProjectAnalysis(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/diagnostics", "factory")
	session := NewSession("dev")
	prepared := make(chan struct{})
	session.analyzeDocument = func(doc *Document) *documentAnalysis {
		analysis := analyzeDocumentSyntax(doc)
		close(prepared)
		return analysis
	}
	uri := "file:///tmp/factory.ub"
	require.Nil(t, openDocument(t, session, uri, 1, text))
	require.True(t, session.projects.acquireAnalysis(context.Background()))
	t.Cleanup(session.projects.releaseAnalysis)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	params, err := json.Marshal(protocol.HoverParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	require.NoError(t, err)
	finished := make(chan *protocol.ResponseError, 1)
	go func() {
		_, rpcErr := session.HandleRequest(ctx, &protocol.RequestMessage{
			JSONRPC: "2.0", ID: protocol.NewNumberID(1), Method: "textDocument/hover", Params: params,
		})
		finished <- rpcErr
	}()
	select {
	case <-prepared:
	case <-time.After(5 * time.Second):
		t.Fatal("feature did not prepare its syntax")
	}
	cancel()
	select {
	case rpcErr := <-finished:
		require.NotNil(t, rpcErr)
		require.Equal(t, protocol.ErrorCodeRequestCancel, rpcErr.Code)
	case <-time.After(time.Second):
		t.Fatal("feature waited for project analysis after cancellation")
	}
}

func TestDocumentAnalysisDiagnosticsCannotBeChangedByCaller(t *testing.T) {
	text := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	doc, err := newDocument("file:///tmp/factory.ub", 1, text)
	require.NoError(t, err)
	analysis := analyzeDocumentSyntax(doc)
	want := DiagnosticsForText(doc.Path, doc.Text)
	actual := analysis.documentDiagnostics(nil)
	require.Equal(t, want, actual)
	require.NotEmpty(t, actual)
	actual[0].Message = "changed"
	require.Equal(t, want, analysis.documentDiagnostics(nil))
}
