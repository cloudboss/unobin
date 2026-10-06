package lsp

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type documentAnalysis struct {
	document          *Document
	dependencyVersion uint64
	file              *syntax.File
	parseErr          error
	symbols           []protocol.DocumentSymbol
	symbolsOnce       sync.Once
	declarations      map[*syntax.FactoryBody]definitionDecls
	prepare           func() *documentAnalysis
	prepareOnce       sync.Once
	diagnostics       []protocol.Diagnostic
	diagnosticsOnce   sync.Once
}

func analyzeDocumentSyntax(doc *Document) *documentAnalysis {
	analysis := &documentAnalysis{
		document:     doc,
		declarations: make(map[*syntax.FactoryBody]definitionDecls),
	}
	file, err := syntax.ParseSource(doc.Path, []byte(doc.Text))
	if err != nil {
		analysis.parseErr = err
		analysis.symbols = []protocol.DocumentSymbol{}
		return analysis
	}
	analysis.file = file
	if file.Factory != nil {
		body := &file.Factory.Body
		analysis.cacheDeclarations(body, nil)
	}
	if file.Library != nil {
		for i := range file.Library.Exports {
			body := &file.Library.Exports[i].Body
			analysis.cacheDeclarations(body, file.Library)
		}
	}
	return analysis
}

func (a *documentAnalysis) cacheDeclarations(
	body *syntax.FactoryBody, library *syntax.LibraryFile,
) {
	decls := definitionDeclsForBody(body)
	decls.analysis = &bodyAnalysis{path: a.document.Path, body: body, library: library}
	a.declarations[body] = decls
}

func (a *documentAnalysis) documentSymbols() []protocol.DocumentSymbol {
	a.prepareSyntax()
	a.symbolsOnce.Do(func() {
		if a.parseErr == nil {
			a.symbols = documentSymbols(a.file, a.document.Text)
		}
	})
	return a.symbols
}

func (a *documentAnalysis) syntaxFile(path, text string) (*syntax.File, error) {
	if a != nil {
		a.prepareSyntax()
		return a.file, a.parseErr
	}
	return syntax.ParseSource(path, []byte(text))
}

func (a *documentAnalysis) declarationsFor(body *syntax.FactoryBody) definitionDecls {
	if a != nil {
		a.prepareSyntax()
		if decls, ok := a.declarations[body]; ok {
			return decls
		}
	}
	return definitionDeclsForBody(body)
}

func (s *Session) analysisFor(doc *Document) *documentAnalysis {
	s.mu.Lock()
	analysis := s.cachedAnalysisFor(doc)
	s.mu.Unlock()
	analysis.prepareSyntax()
	return analysis
}

func (s *Session) cachedAnalysisFor(doc *Document) *documentAnalysis {
	if analysis := s.analyses[doc.URI]; analysis != nil &&
		analysis.document == doc && analysis.dependencyVersion == s.dependencyVersion {
		return analysis
	}
	analyze := s.analyzeDocument
	analysis := &documentAnalysis{
		document: doc, dependencyVersion: s.dependencyVersion,
		prepare: func() *documentAnalysis { return analyze(doc) },
	}
	s.analyses[doc.URI] = analysis
	return analysis
}

func (a *documentAnalysis) prepareSyntax() {
	if a == nil || a.prepare == nil {
		return
	}
	a.prepareOnce.Do(func() {
		parsed := a.prepare()
		a.file = parsed.file
		a.parseErr = parsed.parseErr
		a.symbols = parsed.symbols
		a.declarations = parsed.declarations
	})
}

func (a *documentAnalysis) documentDiagnostics(projects *ProjectCache) []protocol.Diagnostic {
	a.prepareSyntax()
	a.diagnosticsOnce.Do(func() {
		doc := a.document
		if a.parseErr != nil {
			a.diagnostics = diagnosticsForParseFailure(doc.Text, a.parseErr)
		} else {
			a.diagnostics = a.diagnosticsForFile(projects)
		}
		if a.diagnostics == nil {
			a.diagnostics = []protocol.Diagnostic{}
		}
	})
	return slices.Clone(a.diagnostics)
}

func (a *documentAnalysis) diagnosticsForFile(projects *ProjectCache) []protocol.Diagnostic {
	text := a.document.Text
	if errs := syntax.ValidateFile(a.file); errs.Len() > 0 {
		return DiagnosticsForError(text, errs)
	}
	if a.file.Factory != nil {
		body := &a.file.Factory.Body
		_, err := a.declarationsFor(body).analysis.checked(projects)
		return DiagnosticsForError(text, err)
	}
	if a.file.Library == nil {
		return nil
	}
	var violations []error
	for _, export := range a.file.Library.Exports {
		violations = append(violations,
			resolve.ValidateSyntaxCompositeBody(string(export.Kind), export.Name.Name, export.Body)...)
	}
	if len(violations) > 0 {
		return DiagnosticsForError(text, errors.Join(violations...))
	}
	var bodyErrs []error
	for i := range a.file.Library.Exports {
		export := &a.file.Library.Exports[i]
		_, err := a.declarationsFor(&export.Body).analysis.checked(projects)
		if err != nil {
			bodyErrs = append(bodyErrs, diagnostic.Context(
				fmt.Sprintf("%s composite %q", export.Kind, export.Name.Name), err,
			))
		}
	}
	return DiagnosticsForError(text, errors.Join(bodyErrs...))
}

func cloneDocumentSymbols(symbols []protocol.DocumentSymbol) []protocol.DocumentSymbol {
	cloned := slices.Clone(symbols)
	for i := range cloned {
		if len(cloned[i].Children) > 0 {
			cloned[i].Children = cloneDocumentSymbols(cloned[i].Children)
		}
	}
	return cloned
}
