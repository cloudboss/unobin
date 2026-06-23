package protocol

const (
	TextDocumentSyncKindNone        TextDocumentSyncKind = 0
	TextDocumentSyncKindFull        TextDocumentSyncKind = 1
	TextDocumentSyncKindIncremental TextDocumentSyncKind = 2
)

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

const (
	CompletionItemKindText     CompletionItemKind = 1
	CompletionItemKindMethod   CompletionItemKind = 2
	CompletionItemKindFunction CompletionItemKind = 3
	CompletionItemKindField    CompletionItemKind = 5
	CompletionItemKindVariable CompletionItemKind = 6
	CompletionItemKindKeyword  CompletionItemKind = 14
)

const (
	MarkupKindPlainText MarkupKind = "plaintext"
	MarkupKindMarkdown  MarkupKind = "markdown"
)

// TextDocumentSyncKind is an LSP text sync mode.
type TextDocumentSyncKind int

// DiagnosticSeverity is an LSP diagnostic severity.
type DiagnosticSeverity int

// CompletionItemKind is an LSP completion item kind.
type CompletionItemKind int

// MarkupKind is an LSP markup content kind.
type MarkupKind string

// Position is a zero-based LSP UTF-16 text position.
type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

// Range is an LSP text range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// InitializeParams is the subset of LSP initialize params used by Unobin.
type InitializeParams struct {
	ProcessID        *int32             `json:"processId,omitempty"`
	RootURI          string             `json:"rootUri,omitempty"`
	Capabilities     ClientCapabilities `json:"capabilities"`
	WorkspaceFolders []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
}

// ClientCapabilities is present for initialize compatibility.
type ClientCapabilities struct{}

// WorkspaceFolder is an LSP workspace folder.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// InitializeResult is the LSP initialize response body.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

// ServerInfo names the LSP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ServerCapabilities advertises implemented LSP features.
type ServerCapabilities struct {
	TextDocumentSync           TextDocumentSyncKind `json:"textDocumentSync,omitempty"`
	DocumentFormattingProvider bool                 `json:"documentFormattingProvider,omitempty"`
	DefinitionProvider         bool                 `json:"definitionProvider,omitempty"`
	DocumentSymbolProvider     bool                 `json:"documentSymbolProvider,omitempty"`
	CompletionProvider         *CompletionOptions   `json:"completionProvider,omitempty"`
	HoverProvider              bool                 `json:"hoverProvider,omitempty"`
}

// CompletionOptions configures completion requests.
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// TextDocumentIdentifier identifies a text document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier identifies a versioned text document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int32  `json:"version"`
}

// TextDocumentItem is an open text document.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int32  `json:"version"`
	Text       string `json:"text"`
}

// DidOpenTextDocumentParams is sent when a text document opens.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeTextDocumentParams is sent when a text document changes.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// TextDocumentContentChangeEvent is a full-text change in the first server.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidSaveTextDocumentParams is sent when a text document is saved.
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}

// DidCloseTextDocumentParams is sent when a text document closes.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentFormattingParams is a whole-document formatting request.
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

// FormattingOptions is present for LSP formatting compatibility.
type FormattingOptions struct {
	TabSize      uint32 `json:"tabSize"`
	InsertSpaces bool   `json:"insertSpaces"`
}

// TextEdit replaces a range with new text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// Diagnostic is an LSP diagnostic.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// PublishDiagnosticsParams sends diagnostics for one document.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int32       `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// DocumentSymbolParams is a document symbol request.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentSymbol is a document-local symbol.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolKind is an LSP symbol kind.
type SymbolKind int

const (
	SymbolKindFile     SymbolKind = 1
	SymbolKindModule   SymbolKind = 2
	SymbolKindClass    SymbolKind = 5
	SymbolKindFunction SymbolKind = 12
	SymbolKindVariable SymbolKind = 13
	SymbolKindField    SymbolKind = 8
)

// DefinitionParams is a go-to-definition request.
type DefinitionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Location is an LSP source location.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// CompletionParams is a completion request.
type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// CompletionList is an LSP completion response.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// CompletionItem is one completion candidate.
type CompletionItem struct {
	Label string             `json:"label"`
	Kind  CompletionItemKind `json:"kind,omitempty"`
}

// HoverParams is a hover request.
type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Hover is an LSP hover response.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupContent is hover text with a markup kind.
type MarkupContent struct {
	Kind  MarkupKind `json:"kind"`
	Value string     `json:"value"`
}
