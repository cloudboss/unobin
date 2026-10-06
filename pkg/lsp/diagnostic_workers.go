package lsp

import (
	"context"
	"slices"
	"sync"
)

type diagnosticWorkers struct {
	ctx        context.Context
	cancel     context.CancelFunc
	stopParent func() bool
	mu         sync.Mutex
	ready      *sync.Cond
	closed     bool
	pending    map[string]*diagnosticWork
	latest     map[string]*diagnosticWork
	order      []string
	analyze    diagnosticAnalyzer
	publish    diagnosticPublisher
	workers    sync.WaitGroup
}

type diagnosticWork struct {
	ctx      context.Context
	cancel   context.CancelFunc
	document *Document
	revision uint64
}

type diagnosticAnalyzer func(context.Context, *Document, uint64) *documentAnalysis

type diagnosticPublisher func(context.Context, *documentAnalysis)

func newDiagnosticWorkers(
	ctx context.Context,
	limit int,
	analyze diagnosticAnalyzer,
	publish diagnosticPublisher,
) *diagnosticWorkers {
	workerCtx, cancel := context.WithCancel(ctx)
	w := &diagnosticWorkers{
		ctx: workerCtx, cancel: cancel,
		pending: make(map[string]*diagnosticWork), latest: make(map[string]*diagnosticWork),
		analyze: analyze, publish: publish,
	}
	w.ready = sync.NewCond(&w.mu)
	w.stopParent = context.AfterFunc(ctx, w.stop)
	for range max(1, limit) {
		w.workers.Go(w.run)
	}
	return w
}

func (w *diagnosticWorkers) submit(doc *Document, revision uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if previous := w.latest[doc.URI]; previous != nil {
		previous.cancel()
	}
	ctx, cancel := context.WithCancel(w.ctx)
	work := &diagnosticWork{ctx: ctx, cancel: cancel, document: doc, revision: revision}
	if _, waiting := w.pending[doc.URI]; !waiting {
		w.order = append(w.order, doc.URI)
	}
	w.latest[doc.URI] = work
	w.pending[doc.URI] = work
	w.ready.Signal()
}

func (w *diagnosticWorkers) cancelDocument(uri string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if work := w.latest[uri]; work != nil {
		work.cancel()
	}
	delete(w.latest, uri)
	delete(w.pending, uri)
	w.order = slices.DeleteFunc(w.order, func(pending string) bool { return pending == uri })
}

func (w *diagnosticWorkers) close() {
	w.stop()
	w.stopParent()
	w.workers.Wait()
}

func (w *diagnosticWorkers) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.cancel()
	for _, work := range w.latest {
		work.cancel()
	}
	clear(w.latest)
	clear(w.pending)
	w.order = nil
	w.ready.Broadcast()
}

func (w *diagnosticWorkers) next() *diagnosticWork {
	w.mu.Lock()
	defer w.mu.Unlock()
	for !w.closed {
		if len(w.order) == 0 {
			w.ready.Wait()
			continue
		}
		uri := w.order[0]
		w.order[0] = ""
		w.order = w.order[1:]
		work := w.pending[uri]
		delete(w.pending, uri)
		if work != nil && work.ctx.Err() == nil {
			return work
		}
	}
	return nil
}

func (w *diagnosticWorkers) run() {
	for {
		work := w.next()
		if work == nil {
			return
		}
		result := w.analyze(work.ctx, work.document, work.revision)
		w.mu.Lock()
		current := !w.closed && w.latest[work.document.URI] == work
		w.mu.Unlock()
		if current && work.ctx.Err() == nil && result != nil {
			w.publish(work.ctx, result)
		}
		w.mu.Lock()
		if w.latest[work.document.URI] == work {
			delete(w.latest, work.document.URI)
		}
		w.mu.Unlock()
		work.cancel()
	}
}
