package lsp

import (
	"errors"
	"io/fs"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// DiagnosticsForText parses and validates UB source text into LSP diagnostics.
func DiagnosticsForText(path string, text string) []protocol.Diagnostic {
	return DiagnosticsForTextWithProjects(path, text, nil)
}

// DiagnosticsForTextWithProjects adds no-fetch semantic checks when project data is available.
func DiagnosticsForTextWithProjects(
	path string,
	text string,
	projects *ProjectCache,
) []protocol.Diagnostic {
	file, err := syntax.ParseSource(path, []byte(text))
	if err != nil {
		return diagnosticsForParseFailure(text, err)
	}
	if errs := syntax.ValidateFile(file); errs.Len() > 0 {
		return DiagnosticsForError(text, errs)
	}
	if file.Factory == nil {
		return nil
	}
	libs, err := diagnosticLibraries(path, file.Factory.Body, projects)
	if err != nil {
		return DiagnosticsForError(text, err)
	}
	checker := check.NewSyntax(file.Factory.Body, libs)
	if errs := checker.References(nil); errs.Len() > 0 {
		return DiagnosticsForError(text, errs)
	}
	return nil
}

func diagnosticLibraries(
	path string,
	body syntax.FactoryBody,
	projects *ProjectCache,
) (map[string]*runtime.Library, error) {
	libs := opaqueImportedLibraries(body)
	if projects == nil {
		return libs, nil
	}
	project, err := projects.ProjectForPath(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return libs, nil
		}
		return nil, err
	}
	for _, imp := range body.Imports {
		if imp.Ref == nil {
			continue
		}
		ref, err := resolve.ParseImportRef(imp.Ref.Value)
		if err != nil {
			return nil, err
		}
		source, ok, err := project.Resolver.ResolveNoFetch(ref)
		if err != nil {
			return nil, err
		}
		if !ok || source == nil || !resolve.IsGoLibrary(source) {
			continue
		}
		project.EnsureGoModuleRoot(source)
		schema, _, _, err := project.GoIndex.Read(source.Path)
		if err != nil {
			return nil, err
		}
		libs[imp.Alias.Name] = &runtime.Library{Schema: schema}
	}
	return libs, nil
}

func opaqueImportedLibraries(body syntax.FactoryBody) map[string]*runtime.Library {
	libs := make(map[string]*runtime.Library, len(body.Imports))
	for _, imp := range body.Imports {
		libs[imp.Alias.Name] = &runtime.Library{}
	}
	return libs
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
