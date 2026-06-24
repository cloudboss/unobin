package lsp

import (
	"context"
	"encoding/json"
	"io"

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
	version   string
	documents *DocumentStore
	projects  *ProjectCache
	shutdown  bool
	exiting   bool
	sender    protocol.Sender
}

// NewSession returns a new LSP session.
func NewSession(version string) *Session {
	return &Session{
		version:   version,
		documents: NewDocumentStore(),
		projects:  NewProjectCache(""),
	}
}

// Serve runs an LSP session over stdio-compatible streams.
func Serve(ctx context.Context, in io.Reader, out io.Writer, version string) error {
	return ServeWithOptions(ctx, in, out, Options{Version: version})
}

// ServeWithOptions runs an LSP session over stdio-compatible streams.
func ServeWithOptions(ctx context.Context, in io.Reader, out io.Writer, options Options) error {
	session := NewSession(options.Version)
	server := protocol.NewServerWithOptions(in, out, session, protocol.ServerOptions{
		Trace: options.Trace,
		Log:   options.Log,
	})
	return server.Serve(ctx)
}

// Shutdown reports whether the client has requested shutdown.
func (s *Session) Shutdown() bool {
	return s.shutdown
}

// Exit reports whether the client has requested exit.
func (s *Session) Exit() bool {
	return s.exiting
}

// StopRequested reports whether the protocol server should stop serving.
func (s *Session) StopRequested() bool {
	return s.exiting
}

// SetSender sets the server-to-client notification sender.
func (s *Session) SetSender(sender protocol.Sender) {
	s.sender = sender
}

// HandleRequest dispatches one JSON-RPC request to the session.
func (s *Session) HandleRequest(
	ctx context.Context,
	req *protocol.RequestMessage,
) (any, *protocol.ResponseError) {
	_ = ctx
	if s.shutdown && req.Method != "exit" {
		return nil, &protocol.ResponseError{
			Code:    protocol.ErrorCodeInvalidRequest,
			Message: "server is shut down",
		}
	}
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params)
	case "initialized":
		return nil, nil
	case "shutdown":
		s.shutdown = true
		return nil, nil
	case "exit":
		s.exiting = true
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
	case "textDocument/formatting":
		return s.handleFormatting(req.Params)
	case "textDocument/documentSymbol":
		return s.handleDocumentSymbols(req.Params)
	case "textDocument/definition":
		return s.handleDefinition(req.Params)
	case "textDocument/completion":
		return s.handleCompletion(req.Params)
	case "textDocument/hover":
		return s.handleHover(req.Params)
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
	s.projects.SetWorkspaceRoots(roots)
	return protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync:           protocol.TextDocumentSyncKindFull,
			DocumentFormattingProvider: true,
			DefinitionProvider:         true,
			DocumentSymbolProvider:     true,
			CompletionProvider: &protocol.CompletionOptions{
				TriggerCharacters: []string{".", "@"},
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
	if doc, ok := s.documents.Get(save.TextDocument.URI); ok {
		return s.publishDiagnostics(doc)
	}
	return nil
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
	return nil
}

func (s *Session) handleDidClose(params json.RawMessage) *protocol.ResponseError {
	var close protocol.DidCloseTextDocumentParams
	if err := decodeParams(params, &close); err != nil {
		return err
	}
	s.documents.Close(close.TextDocument.URI)
	return nil
}

func (s *Session) handleFormatting(params json.RawMessage) (any, *protocol.ResponseError) {
	var formatting protocol.DocumentFormattingParams
	if err := decodeParams(params, &formatting); err != nil {
		return nil, err
	}
	doc, ok := s.documents.Get(formatting.TextDocument.URI)
	if !ok {
		return nil, protocol.InvalidParams("document is not open: " + formatting.TextDocument.URI)
	}
	return FormatText(doc.Path, doc.Text)
}

func (s *Session) handleDocumentSymbols(params json.RawMessage) (any, *protocol.ResponseError) {
	var documentSymbols protocol.DocumentSymbolParams
	if err := decodeParams(params, &documentSymbols); err != nil {
		return nil, err
	}
	doc, ok := s.documents.Get(documentSymbols.TextDocument.URI)
	if !ok {
		return nil, protocol.InvalidParams("document is not open: " + documentSymbols.TextDocument.URI)
	}
	return DocumentSymbolsForText(doc.Path, doc.Text)
}

func (s *Session) handleDefinition(params json.RawMessage) (any, *protocol.ResponseError) {
	var definition protocol.DefinitionParams
	if err := decodeParams(params, &definition); err != nil {
		return nil, err
	}
	doc, ok := s.documents.Get(definition.TextDocument.URI)
	if !ok {
		return nil, protocol.InvalidParams("document is not open: " + definition.TextDocument.URI)
	}
	return DefinitionForText(doc.Path, doc.Text, definition.Position, s.projects)
}

func (s *Session) handleCompletion(params json.RawMessage) (any, *protocol.ResponseError) {
	var completion protocol.CompletionParams
	if err := decodeParams(params, &completion); err != nil {
		return nil, err
	}
	doc, ok := s.documents.Get(completion.TextDocument.URI)
	if !ok {
		return nil, protocol.InvalidParams("document is not open: " + completion.TextDocument.URI)
	}
	return CompleteForText(doc.Path, doc.Text, completion.Position, s.projects)
}

func (s *Session) handleHover(params json.RawMessage) (any, *protocol.ResponseError) {
	var hover protocol.HoverParams
	if err := decodeParams(params, &hover); err != nil {
		return nil, err
	}
	doc, ok := s.documents.Get(hover.TextDocument.URI)
	if !ok {
		return nil, protocol.InvalidParams("document is not open: " + hover.TextDocument.URI)
	}
	return HoverForText(doc.Path, doc.Text, hover.Position, s.projects)
}

func (s *Session) invalidateURI(uri string) error {
	path, err := FileURIToPath(uri)
	if err != nil {
		return err
	}
	s.projects.InvalidatePath(path)
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
	version := doc.Version
	diagnostics := DiagnosticsForTextWithProjects(doc.Path, doc.Text, s.projects)
	if diagnostics == nil {
		diagnostics = []protocol.Diagnostic{}
	}
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

func decodeParams(params json.RawMessage, target any) *protocol.ResponseError {
	if len(params) == 0 {
		params = []byte("{}")
	}
	if err := json.Unmarshal(params, target); err != nil {
		return protocol.InvalidParams(err.Error())
	}
	return nil
}
