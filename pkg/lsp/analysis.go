package lsp

import (
	"slices"
	"sync"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

type documentAnalysis struct {
	document          *Document
	dependencyVersion uint64
	file              *syntax.File
	parseErr          error
	symbols           []protocol.DocumentSymbol
	symbolsOnce       sync.Once
	declarations      map[*syntax.FactoryBody]definitionDecls
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
		analysis.declarations[body] = definitionDeclsForBody(body)
	}
	if file.Library != nil {
		for i := range file.Library.Exports {
			body := &file.Library.Exports[i].Body
			analysis.declarations[body] = definitionDeclsForBody(body)
		}
	}
	return analysis
}

func (a *documentAnalysis) documentSymbols() []protocol.DocumentSymbol {
	a.symbolsOnce.Do(func() {
		if a.parseErr == nil {
			a.symbols = documentSymbols(a.file, a.document.Text)
		}
	})
	return a.symbols
}

func (a *documentAnalysis) syntaxFile(path, text string) (*syntax.File, error) {
	if a != nil {
		return a.file, a.parseErr
	}
	return syntax.ParseSource(path, []byte(text))
}

func (a *documentAnalysis) declarationsFor(body *syntax.FactoryBody) definitionDecls {
	if a != nil {
		if decls, ok := a.declarations[body]; ok {
			return decls
		}
	}
	return definitionDeclsForBody(body)
}

func (s *Session) analysisFor(doc *Document) *documentAnalysis {
	if analysis := s.analyses[doc.URI]; analysis != nil &&
		analysis.document == doc && analysis.dependencyVersion == s.dependencyVersion {
		return analysis
	}
	analysis := s.analyzeDocument(doc)
	analysis.dependencyVersion = s.dependencyVersion
	s.analyses[doc.URI] = analysis
	return analysis
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
