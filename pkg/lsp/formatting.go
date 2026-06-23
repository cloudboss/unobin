package lsp

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

// FormatText formats one UB document and returns LSP text edits.
func FormatText(path string, text string) ([]protocol.TextEdit, *protocol.ResponseError) {
	parsed, err := lang.ParseSource(path, []byte(text))
	if err != nil {
		return nil, protocol.InvalidParams(err.Error())
	}
	out, err := lang.FormatWith(parsed, lang.FormatOptions{})
	if err != nil {
		return nil, protocol.InternalError(err)
	}
	formatted := string(out)
	if formatted == text {
		return []protocol.TextEdit{}, nil
	}
	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   OffsetToLSP(text, len(text)),
		},
		NewText: formatted,
	}}, nil
}
