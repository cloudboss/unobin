package lsp

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestBodyAnalysisRetainsCompositeLocalTypes(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/analysis", "scopes")
	doc, err := newDocument("file:///tmp/library.ub", 1, text)
	require.NoError(t, err)
	analysis := analyzeDocumentSyntax(doc)
	require.NoError(t, analysis.parseErr)
	for i, typ := range []typecheck.Type{typecheck.TString(), typecheck.TInteger()} {
		body := &analysis.file.Library.Exports[i].Body
		checked := analysis.declarationsFor(body).analysis
		result, err := checked.checked(nil)
		require.NoError(t, err)
		require.Equal(t, map[string]typecheck.Type{"value": typ}, result.LocalTypes)
		again, err := checked.checked(nil)
		require.NoError(t, err)
		require.Same(t, result, again)
	}
}

func TestBodyAnalysisConcurrentReads(t *testing.T) {
	text := ubtest.ReadValidFixture(t, "testdata/ub/analysis", "scopes")
	doc, err := newDocument("file:///tmp/library.ub", 1, text)
	require.NoError(t, err)
	analysis := analyzeDocumentSyntax(doc)
	body := &analysis.file.Library.Exports[0].Body
	checked := analysis.declarationsFor(body).analysis
	results := make(chan *program.CheckedBody, 16)
	errs := make(chan error, 16)
	var done sync.WaitGroup
	for range 16 {
		done.Go(func() {
			result, err := checked.checked(nil)
			results <- result
			errs <- err
		})
	}
	done.Wait()
	want := <-results
	require.Equal(t, map[string]typecheck.Type{"value": typecheck.TString()}, want.LocalTypes)
	for range 15 {
		require.Same(t, want, <-results)
	}
	for range 16 {
		require.NoError(t, <-errs)
	}
}

func TestDocumentAnalysisLibraryDiagnostics(t *testing.T) {
	for _, fixture := range []string{
		"testdata/ub/analysis/valid/scopes.ub",
		"testdata/ub/diagnostics/invalid/library-file-semantic-error.ub",
	} {
		t.Run(fixture, func(t *testing.T) {
			text := ubtest.ReadFixture(t, fixture)
			doc, err := newDocument("file:///tmp/library.ub", 1, text)
			require.NoError(t, err)
			analysis := analyzeDocumentSyntax(doc)
			want := DiagnosticsForText(doc.Path, text)
			if want == nil {
				want = []protocol.Diagnostic{}
			}
			require.Equal(t, want, analysis.documentDiagnostics(nil))
		})
	}
}
