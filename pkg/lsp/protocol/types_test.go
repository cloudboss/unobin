package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitializeTypesUseLSPFieldNames(t *testing.T) {
	paramsJSON := []byte(`{
		"processId": 12,
		"rootUri": "file:///tmp/work",
		"capabilities": {},
		"workspaceFolders": [{"uri":"file:///tmp/work","name":"work"}]
	}`)
	var params InitializeParams
	require.NoError(t, json.Unmarshal(paramsJSON, &params))
	require.NotNil(t, params.ProcessID)
	require.Equal(t, int32(12), *params.ProcessID)
	require.Equal(t, "file:///tmp/work", params.RootURI)
	require.Len(t, params.WorkspaceFolders, 1)

	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync:           TextDocumentSyncKindFull,
			DocumentFormattingProvider: true,
			DefinitionProvider:         true,
			DocumentSymbolProvider:     true,
			CompletionProvider:         &CompletionOptions{},
			HoverProvider:              true,
		},
		ServerInfo: &ServerInfo{Name: "unobin", Version: "dev"},
	}
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(body), "serverInfo")
	require.Contains(t, string(body), "textDocumentSync")
	require.NotContains(t, string(body), "protocolVersion")
}

func TestTextDocumentTypesUseLSPFieldNames(t *testing.T) {
	body := []byte(`{
		"textDocument": {"uri":"file:///tmp/a.ub","languageId":"unobin","version":3,"text":"x"}
	}`)
	var params DidOpenTextDocumentParams
	require.NoError(t, json.Unmarshal(body, &params))
	require.Equal(t, "file:///tmp/a.ub", params.TextDocument.URI)
	require.Equal(t, "unobin", params.TextDocument.LanguageID)
	require.Equal(t, int32(3), params.TextDocument.Version)
	require.Equal(t, "x", params.TextDocument.Text)
}

func TestProtocolFeatureTypesUseLSPFieldNames(t *testing.T) {
	value := struct {
		Diagnostic     Diagnostic     `json:"diagnostic"`
		TextEdit       TextEdit       `json:"textEdit"`
		Location       Location       `json:"location"`
		CompletionItem CompletionItem `json:"completionItem"`
		Hover          Hover          `json:"hover"`
	}{
		Diagnostic: Diagnostic{
			Range:    Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 2}},
			Severity: DiagnosticSeverityError,
			Source:   "unobin",
			Message:  "bad input",
		},
		TextEdit: TextEdit{
			Range:   Range{Start: Position{}, End: Position{Line: 1}},
			NewText: "formatted",
		},
		Location: Location{
			URI:   "file:///tmp/a.ub",
			Range: Range{Start: Position{}, End: Position{Character: 1}},
		},
		CompletionItem: CompletionItem{Label: "input", Kind: CompletionItemKindKeyword},
		Hover: Hover{
			Contents: MarkupContent{Kind: MarkupKindPlainText, Value: "input string"},
			Range:    &Range{Start: Position{}, End: Position{Character: 5}},
		},
	}
	body, err := json.Marshal(value)
	require.NoError(t, err)
	require.Contains(t, string(body), "newText")
	require.Contains(t, string(body), "completionItem")
	require.Contains(t, string(body), "plaintext")
}
