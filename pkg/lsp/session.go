package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"sync"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

// Options configures an LSP session.
type Options struct {
	Version string
	Trace   io.Writer
	Log     io.Writer
}

// Session owns the state for one LSP client connection.
type Session struct {
	mu                sync.Mutex
	version           string
	documents         *DocumentStore
	projects          *ProjectCache
	shutdown          bool
	exiting           bool
	sender            protocol.Sender
	analyses          map[string]*documentAnalysis
	dependencyVersion uint64
	analyzeDocument   func(*Document) *documentAnalysis
	workers           *diagnosticWorkers
	diagnosticsErr    error
	diagnosticsCancel context.CancelCauseFunc
}

// NewSession returns a new LSP session.
func NewSession(version string) *Session {
	return &Session{
		version:         version,
		documents:       NewDocumentStore(),
		projects:        NewProjectCache(""),
		analyses:        make(map[string]*documentAnalysis),
		analyzeDocument: analyzeDocumentSyntax,
	}
}

// Serve runs an LSP session over stdio-compatible streams.
func Serve(ctx context.Context, in io.Reader, out io.Writer, version string) error {
	return ServeWithOptions(ctx, in, out, Options{Version: version})
}

// ServeWithOptions runs an LSP session over stdio-compatible streams.
func ServeWithOptions(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	options Options,
) (serveErr error) {
	session := NewSession(options.Version)
	sessionCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	session.diagnosticsCancel = cancel
	session.startDiagnosticWorkers(sessionCtx, min(2, runtime.GOMAXPROCS(0)))
	defer func() {
		serveErr = errors.Join(serveErr, session.closeDiagnosticWorkers())
	}()
	server := protocol.NewServerWithOptions(in, out, session, protocol.ServerOptions{
		Trace: options.Trace,
		Log:   options.Log,
	})
	return server.Serve(sessionCtx)
}

// Shutdown reports whether the client has requested shutdown.
func (s *Session) Shutdown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdown
}

// Exit reports whether the client has requested exit.
func (s *Session) Exit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exiting
}

// StopRequested reports whether the protocol server should stop serving.
func (s *Session) StopRequested() bool {
	return s.Exit()
}

// SetSender sets the server-to-client notification sender.
func (s *Session) SetSender(sender protocol.Sender) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sender = sender
}

// HandleRequest dispatches one JSON-RPC request to the session.
func (s *Session) HandleRequest(
	ctx context.Context,
	req *protocol.RequestMessage,
) (any, *protocol.ResponseError) {
	s.mu.Lock()
	if req.Method != "exit" {
		if err := s.requestStateError(ctx); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	s.mu.Unlock()
	switch req.Method {
	case "shutdown", "exit":
		return s.handleStop(req.Method)
	case "textDocument/formatting":
		return s.handleFormatting(ctx, req.Params)
	case "textDocument/documentSymbol":
		return s.handleDocumentSymbols(ctx, req.Params)
	case "textDocument/definition":
		return s.handleDefinition(ctx, req.Params)
	case "textDocument/completion":
		return s.handleCompletion(ctx, req.Params)
	case "textDocument/hover":
		return s.handleHover(ctx, req.Params)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requestStateError(ctx); err != nil {
		return nil, err
	}
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params)
	case "initialized":
		return nil, nil
	case "textDocument/didOpen":
		return nil, s.handleDidOpen(req.Params)
	case "textDocument/didChange":
		return nil, s.handleDidChange(req.Params)
	case "textDocument/didSave":
		return nil, s.handleDidSave(req.Params)
	case "textDocument/didClose":
		return nil, s.handleDidClose(req.Params)
	case "workspace/didChangeWatchedFiles":
		return nil, s.handleDidChangeWatchedFiles(req.Params)
	default:
		return nil, protocol.MethodNotFound(req.Method)
	}
}

func (s *Session) handleInitialize(params json.RawMessage) (any, *protocol.ResponseError) {
	var initialize protocol.InitializeParams
	if err := decodeParams(params, &initialize); err != nil {
		return nil, err
	}
	roots, err := initializeWorkspaceRoots(initialize)
	if err != nil {
		return nil, protocol.InvalidParams(err.Error())
	}
	s.projects = s.projects.nextRevision(roots)
	s.dependencyVersion++
	clear(s.analyses)
	return protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync:           protocol.TextDocumentSyncKindFull,
			DocumentFormattingProvider: true,
			DefinitionProvider:         true,
			DocumentSymbolProvider:     true,
			CompletionProvider: &protocol.CompletionOptions{
				TriggerCharacters: []string{".", "@", ":", " "},
			},
			HoverProvider: true,
		},
		ServerInfo: &protocol.ServerInfo{Name: "unobin", Version: s.version},
	}, nil
}

func (s *Session) handleDidOpen(params json.RawMessage) *protocol.ResponseError {
	var open protocol.DidOpenTextDocumentParams
	if err := decodeParams(params, &open); err != nil {
		return err
	}
	doc, err := s.documents.Open(
		open.TextDocument.URI,
		open.TextDocument.Version,
		open.TextDocument.Text,
	)
	if err != nil {
		return protocol.InvalidParams(err.Error())
	}
	return s.publishDiagnostics(doc)
}

func (s *Session) handleDidChange(params json.RawMessage) *protocol.ResponseError {
	var change protocol.DidChangeTextDocumentParams
	if err := decodeParams(params, &change); err != nil {
		return err
	}
	if len(change.ContentChanges) == 0 {
		return nil
	}
	last := change.ContentChanges[len(change.ContentChanges)-1]
	if last.Range != nil || last.RangeLength != nil {
		return protocol.InvalidParams("incremental document changes are not supported")
	}
	doc, err := s.documents.Change(
		change.TextDocument.URI,
		change.TextDocument.Version,
		last.Text,
	)
	if err != nil {
		return protocol.InvalidParams(err.Error())
	}
	return s.publishDiagnostics(doc)
}

func (s *Session) handleDidSave(params json.RawMessage) *protocol.ResponseError {
	var save protocol.DidSaveTextDocumentParams
	if err := decodeParams(params, &save); err != nil {
		return err
	}
	if err := s.invalidateURI(save.TextDocument.URI); err != nil {
		return protocol.InvalidParams(err.Error())
	}
	return s.publishOpenDiagnostics()
}

func (s *Session) handleDidChangeWatchedFiles(params json.RawMessage) *protocol.ResponseError {
	var watched protocol.DidChangeWatchedFilesParams
	if err := decodeParams(params, &watched); err != nil {
		return err
	}
	for _, change := range watched.Changes {
		if err := s.invalidateURI(change.URI); err != nil {
			return protocol.InvalidParams(err.Error())
		}
	}
	if len(watched.Changes) > 0 {
		return s.publishOpenDiagnostics()
	}
	return nil
}

func (s *Session) handleDidClose(params json.RawMessage) *protocol.ResponseError {
	var close protocol.DidCloseTextDocumentParams
	if err := decodeParams(params, &close); err != nil {
		return err
	}
	s.documents.Close(close.TextDocument.URI)
	delete(s.analyses, close.TextDocument.URI)
	if s.workers != nil {
		s.workers.cancelDocument(close.TextDocument.URI)
	}
	return s.publishEmptyDiagnostics(close.TextDocument.URI)
}

func (s *Session) handleFormatting(
	ctx context.Context, params json.RawMessage,
) (any, *protocol.ResponseError) {
	var formatting protocol.DocumentFormattingParams
	if err := decodeParams(params, &formatting); err != nil {
		return nil, err
	}
	doc, _, _, err := s.documentSnapshot(ctx, formatting.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return FormatText(doc.Path, doc.Text)
}

func (s *Session) handleDocumentSymbols(
	ctx context.Context, params json.RawMessage,
) (any, *protocol.ResponseError) {
	var documentSymbols protocol.DocumentSymbolParams
	if err := decodeParams(params, &documentSymbols); err != nil {
		return nil, err
	}
	_, analysis, _, err := s.documentSnapshot(ctx, documentSymbols.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return cloneDocumentSymbols(analysis.documentSymbols()), nil
}

func (s *Session) handleDefinition(
	ctx context.Context, params json.RawMessage,
) (any, *protocol.ResponseError) {
	var definition protocol.DefinitionParams
	if err := decodeParams(params, &definition); err != nil {
		return nil, err
	}
	doc, analysis, projects, err := s.documentSnapshot(ctx, definition.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	analysis.prepareSyntax()
	if !projects.acquireAnalysis(ctx) {
		return nil, requestCanceled()
	}
	defer projects.releaseAnalysis()
	return definitionForText(doc.Path, doc.Text, definition.Position, projects, analysis)
}

func (s *Session) handleCompletion(
	ctx context.Context, params json.RawMessage,
) (any, *protocol.ResponseError) {
	var completion protocol.CompletionParams
	if err := decodeParams(params, &completion); err != nil {
		return nil, err
	}
	doc, analysis, projects, err := s.documentSnapshot(ctx, completion.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	analysis.prepareSyntax()
	if !projects.acquireAnalysis(ctx) {
		return nil, requestCanceled()
	}
	defer projects.releaseAnalysis()
	return completeForText(doc.Path, doc.Text, completion.Position, projects, analysis)
}

func (s *Session) handleHover(
	ctx context.Context, params json.RawMessage,
) (any, *protocol.ResponseError) {
	var hover protocol.HoverParams
	if err := decodeParams(params, &hover); err != nil {
		return nil, err
	}
	doc, analysis, projects, err := s.documentSnapshot(ctx, hover.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	analysis.prepareSyntax()
	if !projects.acquireAnalysis(ctx) {
		return nil, requestCanceled()
	}
	defer projects.releaseAnalysis()
	return hoverForText(doc.Path, doc.Text, hover.Position, projects, analysis)
}

func (s *Session) invalidateURI(uri string) error {
	if _, err := FileURIToPath(uri); err != nil {
		return err
	}
	s.projects = s.projects.nextRevision(s.projects.workspaceRoots)
	s.dependencyVersion++
	clear(s.analyses)
	return nil
}

func initializeWorkspaceRoots(initialize protocol.InitializeParams) ([]string, error) {
	if len(initialize.WorkspaceFolders) > 0 {
		roots := make([]string, 0, len(initialize.WorkspaceFolders))
		for _, folder := range initialize.WorkspaceFolders {
			path, err := FileURIToPath(folder.URI)
			if err != nil {
				return nil, err
			}
			roots = append(roots, path)
		}
		return roots, nil
	}
	if initialize.RootURI == "" {
		return nil, nil
	}
	root, err := FileURIToPath(initialize.RootURI)
	if err != nil {
		return nil, err
	}
	return []string{root}, nil
}

func (s *Session) publishDiagnostics(doc *Document) *protocol.ResponseError {
	if s.sender == nil {
		return nil
	}
	if s.workers != nil {
		s.workers.submit(doc, s.dependencyVersion)
		return nil
	}
	version := doc.Version
	analysis := s.cachedAnalysisFor(doc)
	s.projects.acquireAnalysis(context.Background())
	diagnostics := analysis.documentDiagnostics(s.projects)
	s.projects.releaseAnalysis()
	err := s.sender("textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{
		URI:         doc.URI,
		Version:     &version,
		Diagnostics: diagnostics,
	})
	if err != nil {
		return protocol.InternalError(err)
	}
	return nil
}

func (s *Session) publishEmptyDiagnostics(uri string) *protocol.ResponseError {
	if s.sender == nil {
		return nil
	}
	err := s.sender("textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []protocol.Diagnostic{},
	})
	if err != nil {
		return protocol.InternalError(err)
	}
	return nil
}

func decodeParams(params json.RawMessage, target any) *protocol.ResponseError {
	if len(params) == 0 {
		params = []byte("{}")
	}
	if err := json.Unmarshal(params, target); err != nil {
		return protocol.InvalidParams(err.Error())
	}
	return nil
}
