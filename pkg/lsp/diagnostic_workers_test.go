package lsp

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func TestDiagnosticWorkersLimitAndCancelObsoleteWork(t *testing.T) {
	text := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	first, err := newDocument("file:///tmp/first.ub", 1, text)
	require.NoError(t, err)
	second, err := newDocument("file:///tmp/second.ub", 1, text)
	require.NoError(t, err)
	latest, err := newDocument(first.URI, 2, text)
	require.NoError(t, err)
	started := make(chan *Document, 8)
	finished := make(chan *Document, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var active atomic.Int32
	var maximum atomic.Int32
	analyze := func(ctx context.Context, doc *Document, revision uint64) *documentAnalysis {
		count := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); count > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, count) {
				break
			}
		}
		started <- doc
		if doc == first {
			<-release
		} else {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		analysis := analyzeDocumentSyntax(doc)
		analysis.dependencyVersion = revision
		return analysis
	}
	workers := newDiagnosticWorkers(context.Background(), 1, analyze,
		func(ctx context.Context, result *documentAnalysis) {
			if ctx.Err() == nil {
				finished <- result.document
			}
		})
	t.Cleanup(workers.close)
	t.Cleanup(unblock)
	workers.submit(first, 1)
	require.Same(t, first, nextDiagnosticDocument(t, started))
	workers.submit(second, 1)
	workers.submit(latest, 2)
	select {
	case doc := <-started:
		t.Fatalf("worker limit exceeded while first analysis was blocked: %s", doc.URI)
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	var actual []*Document
	for range 2 {
		actual = append(actual, nextDiagnosticDocument(t, finished))
	}
	require.ElementsMatch(t, []*Document{second, latest}, actual)
	workers.close()
	require.Equal(t, int32(1), maximum.Load())
	require.Equal(t, int32(0), active.Load())
	select {
	case doc := <-finished:
		t.Fatalf("obsolete diagnostics published for %s version %d", doc.URI, doc.Version)
	default:
	}
}

func TestDiagnosticWorkersCoalescePendingVersions(t *testing.T) {
	text := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	first, err := newDocument("file:///tmp/busy.ub", 1, text)
	require.NoError(t, err)
	started := make(chan *Document, 8)
	finished := make(chan *Document, 8)
	release := make(chan struct{})
	workers := newDiagnosticWorkers(context.Background(), 1,
		func(ctx context.Context, doc *Document, revision uint64) *documentAnalysis {
			started <- doc
			if doc == first {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			result := analyzeDocumentSyntax(doc)
			result.dependencyVersion = revision
			return result
		}, func(_ context.Context, result *documentAnalysis) { finished <- result.document })
	t.Cleanup(workers.close)
	workers.submit(first, 1)
	require.Same(t, first, nextDiagnosticDocument(t, started))
	var latest *Document
	for version := int32(1); version <= 20; version++ {
		latest, err = newDocument("file:///tmp/pending.ub", version, text)
		require.NoError(t, err)
		workers.submit(latest, 1)
	}
	close(release)
	require.Same(t, latest, nextDiagnosticDocument(t, started))
	actual := []*Document{nextDiagnosticDocument(t, finished), nextDiagnosticDocument(t, finished)}
	require.ElementsMatch(t, []*Document{first, latest}, actual)
	workers.close()
	select {
	case doc := <-started:
		t.Fatalf("obsolete pending version started: %d", doc.Version)
	default:
	}
}

func TestDiagnosticWorkersStopAndCancelDocuments(t *testing.T) {
	text := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	for _, test := range []struct {
		name string
		stop func(*diagnosticWorkers)
	}{
		{name: "close", stop: (*diagnosticWorkers).close},
		{name: "document close", stop: func(w *diagnosticWorkers) {
			w.cancelDocument("file:///tmp/factory.ub")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := newDocument("file:///tmp/factory.ub", 1, text)
			require.NoError(t, err)
			started := make(chan *Document, 1)
			canceled := make(chan struct{})
			finished := make(chan *Document, 1)
			workers := newDiagnosticWorkers(context.Background(), 2,
				func(ctx context.Context, doc *Document, revision uint64) *documentAnalysis {
					started <- doc
					<-ctx.Done()
					close(canceled)
					return analyzeDocumentSyntax(doc)
				}, func(_ context.Context, result *documentAnalysis) { finished <- result.document })
			t.Cleanup(workers.close)
			workers.submit(doc, 1)
			require.Same(t, doc, nextDiagnosticDocument(t, started))
			test.stop(workers)
			select {
			case <-canceled:
			case <-time.After(5 * time.Second):
				t.Fatal("analysis was not canceled")
			}
			workers.close()
			select {
			case result := <-finished:
				t.Fatalf("canceled diagnostics published for %s", result.URI)
			default:
			}
		})
	}
}

func nextDiagnosticDocument(t *testing.T, source <-chan *Document) *Document {
	t.Helper()
	select {
	case doc := <-source:
		return doc
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic work did not finish")
		return nil
	}
}

func TestDiagnosticWorkersStopWithParent(t *testing.T) {
	text := ubtest.ReadFixture(t, "testdata/ub/diagnostics/invalid/parse-error.ub")
	doc, err := newDocument("file:///tmp/factory.ub", 1, text)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	started := make(chan *Document, 1)
	canceled := make(chan struct{})
	finished := make(chan *Document, 1)
	workers := newDiagnosticWorkers(ctx, 1,
		func(ctx context.Context, doc *Document, _ uint64) *documentAnalysis {
			started <- doc
			<-ctx.Done()
			close(canceled)
			return analyzeDocumentSyntax(doc)
		}, func(_ context.Context, result *documentAnalysis) { finished <- result.document })
	t.Cleanup(workers.close)
	workers.submit(doc, 1)
	require.Same(t, doc, nextDiagnosticDocument(t, started))
	cancel()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("parent cancellation did not stop analysis")
	}
	workers.close()
	workers.submit(doc, 2)
	select {
	case result := <-finished:
		t.Fatalf("diagnostics published after parent cancellation for %s", result.URI)
	default:
	}
	select {
	case result := <-started:
		t.Fatalf("analysis started after close for %s", result.URI)
	default:
	}
}
