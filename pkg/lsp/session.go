package lsp

import (
	"context"
	"encoding/json"
	"io"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

// Session owns the state for one LSP client connection.
type Session struct {
	version   string
	documents *DocumentStore
	shutdown  bool
	sender    protocol.Sender
}

// NewSession returns a new LSP session.
func NewSession(version string) *Session {
	return &Session{
		version:   version,
		documents: NewDocumentStore(),
	}
}

// Serve runs an LSP session over stdio-compatible streams.
func Serve(ctx context.Context, in io.Reader, out io.Writer, version string) error {
	session := NewSession(version)
	server := protocol.NewServer(in, out, session)
	return server.Serve(ctx)
}

// Shutdown reports whether the client has requested shutdown.
func (s *Session) Shutdown() bool {
	return s.shutdown
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
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params)
	case "initialized":
		return nil, nil
	case "shutdown":
		s.shutdown = true
		return nil, nil
	case "exit":
		return nil, nil
	case "textDocument/didOpen":
		return nil, s.handleDidOpen(req.Params)
	case "textDocument/didChange":
		return nil, s.handleDidChange(req.Params)
	case "textDocument/didSave":
		return nil, s.handleDidSave(req.Params)
	case "textDocument/didClose":
		return nil, s.handleDidClose(req.Params)
	case "textDocument/formatting":
		return s.handleFormatting(req.Params)
	case "textDocument/documentSymbol":
		return s.handleDocumentSymbols(req.Params)
	case "textDocument/definition":
		return nil, nil
	case "textDocument/completion":
		return protocol.CompletionList{}, nil
	case "textDocument/hover":
		return nil, nil
	default:
		return nil, protocol.MethodNotFound(req.Method)
	}
}

func (s *Session) handleInitialize(params json.RawMessage) (any, *protocol.ResponseError) {
	var initialize protocol.InitializeParams
	if err := decodeParams(params, &initialize); err != nil {
		return nil, err
	}
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
	return decodeParams(params, &save)
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

func (s *Session) publishDiagnostics(doc *Document) *protocol.ResponseError {
	if s.sender == nil {
		return nil
	}
	version := doc.Version
	diagnostics := DiagnosticsForText(doc.Path, doc.Text)
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
