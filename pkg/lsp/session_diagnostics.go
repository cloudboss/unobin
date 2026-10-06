package lsp

import (
	"context"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func (s *Session) startDiagnosticWorkers(ctx context.Context, limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers = newDiagnosticWorkers(ctx, limit, s.analyzeDiagnostics, s.publishAnalyzedDiagnostics)
}

func (s *Session) closeDiagnosticWorkers() error {
	s.mu.Lock()
	workers := s.workers
	if workers != nil {
		workers.stop()
	}
	s.mu.Unlock()
	if workers != nil {
		workers.close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diagnosticsErr
}

func (s *Session) analyzeDiagnostics(
	ctx context.Context, doc *Document, revision uint64,
) *documentAnalysis {
	s.mu.Lock()
	current, open := s.documents.Get(doc.URI)
	if !open || current != doc || s.dependencyVersion != revision ||
		s.requestStateError(ctx) != nil {
		s.mu.Unlock()
		return nil
	}
	analysis := s.cachedAnalysisFor(doc)
	projects := s.projects
	s.mu.Unlock()
	analysis.prepareSyntax()
	if ctx.Err() != nil || !projects.acquireAnalysis(ctx) {
		return nil
	}
	analysis.documentDiagnostics(projects)
	projects.releaseAnalysis()
	if ctx.Err() != nil {
		return nil
	}
	return analysis
}

func (s *Session) publishAnalyzedDiagnostics(ctx context.Context, analysis *documentAnalysis) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := analysis.document
	current, open := s.documents.Get(doc.URI)
	if !open || current != doc || analysis.dependencyVersion != s.dependencyVersion ||
		s.requestStateError(ctx) != nil || s.sender == nil {
		return
	}
	version := doc.Version
	if err := s.sender("textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{
		URI: doc.URI, Version: &version, Diagnostics: slices.Clone(analysis.diagnostics),
	}); err != nil {
		s.diagnosticsErr = fmt.Errorf("publish diagnostics for %s: %w", doc.URI, err)
		s.workers.stop()
		if s.diagnosticsCancel != nil {
			s.diagnosticsCancel(s.diagnosticsErr)
		}
	}
}

func (s *Session) publishOpenDiagnostics() *protocol.ResponseError {
	for _, doc := range s.documents.documents {
		if err := s.publishDiagnostics(doc); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) handleStop(method string) (any, *protocol.ResponseError) {
	s.mu.Lock()
	if s.shutdown && method != "exit" {
		s.mu.Unlock()
		return nil, &protocol.ResponseError{
			Code: protocol.ErrorCodeInvalidRequest, Message: "server is shut down",
		}
	}
	if method == "shutdown" {
		s.shutdown = true
	} else {
		s.exiting = true
	}
	clear(s.analyses)
	s.mu.Unlock()
	if err := s.closeDiagnosticWorkers(); err != nil {
		return nil, protocol.InternalError(err)
	}
	return nil, nil
}

func (s *Session) documentSnapshot(
	ctx context.Context, uri string,
) (*Document, *documentAnalysis, *ProjectCache, *protocol.ResponseError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requestStateError(ctx); err != nil {
		return nil, nil, nil, err
	}
	doc, open := s.documents.Get(uri)
	if !open {
		return nil, nil, nil, protocol.InvalidParams("document is not open: " + uri)
	}
	return doc, s.cachedAnalysisFor(doc), s.projects, nil
}

func (s *Session) requestStateError(ctx context.Context) *protocol.ResponseError {
	if ctx.Err() != nil {
		return requestCanceled()
	}
	if s.shutdown || s.exiting {
		return &protocol.ResponseError{
			Code: protocol.ErrorCodeInvalidRequest, Message: "server is shut down",
		}
	}
	return nil
}

func requestCanceled() *protocol.ResponseError {
	return &protocol.ResponseError{Code: protocol.ErrorCodeRequestCancel, Message: "request canceled"}
}
