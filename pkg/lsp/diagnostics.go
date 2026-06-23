package lsp

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

// DiagnosticsForText parses and validates UB source text into LSP diagnostics.
func DiagnosticsForText(path string, text string) []protocol.Diagnostic {
	file, err := syntax.ParseSource(path, []byte(text))
	if err != nil {
		return diagnosticsForParseFailure(text, err)
	}
	if errs := syntax.ValidateFile(file); errs.Len() > 0 {
		return DiagnosticsForError(text, errs)
	}
	return nil
}

// DiagnosticsForError converts UB parser and syntax errors to LSP diagnostics.
func DiagnosticsForError(text string, err error) []protocol.Diagnostic {
	if err == nil {
		return nil
	}
	var list *parse.ErrorList
	if errors.As(err, &list) {
		out := make([]protocol.Diagnostic, 0, list.Len())
		for _, parseErr := range list.Errors() {
			out = append(out, diagnosticFromParseError(text, parseErr))
		}
		return out
	}
	var parseErr *parse.Error
	if errors.As(err, &parseErr) {
		return []protocol.Diagnostic{diagnosticFromParseError(text, parseErr)}
	}
	return []protocol.Diagnostic{{
		Range:    protocol.Range{},
		Severity: protocol.DiagnosticSeverityError,
		Source:   "unobin",
		Message:  err.Error(),
	}}
}

func diagnosticsForParseFailure(text string, err error) []protocol.Diagnostic {
	var list *parse.ErrorList
	if errors.As(err, &list) {
		return DiagnosticsForError(text, err)
	}
	var parseErr *parse.Error
	if errors.As(err, &parseErr) {
		return DiagnosticsForError(text, err)
	}
	return []protocol.Diagnostic{{
		Range:    protocol.Range{},
		Severity: protocol.DiagnosticSeverityError,
		Source:   "unobin",
		Message:  "parse: " + err.Error(),
	}}
}

func diagnosticFromParseError(text string, err *parse.Error) protocol.Diagnostic {
	pos := OffsetToLSP(text, err.Pos.Offset)
	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: pos,
			End:   pos,
		},
		Severity: diagnosticSeverity(err.Kind),
		Source:   "unobin",
		Message:  diagnosticMessage(err),
	}
}

func diagnosticSeverity(kind parse.ErrorKind) protocol.DiagnosticSeverity {
	switch kind {
	case parse.ErrParse, parse.ErrLex, parse.ErrSchema, parse.ErrType, parse.ErrResolve:
		return protocol.DiagnosticSeverityError
	default:
		return protocol.DiagnosticSeverityError
	}
}

func diagnosticMessage(err *parse.Error) string {
	message := err.Kind.String() + ": " + err.Msg
	if err.Hint != "" {
		message += "\n  hint: " + err.Hint
	}
	return message
}
