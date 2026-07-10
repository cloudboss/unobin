package diagnostic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type diagnosticGolden struct {
	Severities         []Severity             `json:"severities"`
	Tags               map[string][2]string   `json:"tags"`
	Normalized         []Diagnostic           `json:"normalized"`
	InputPreserved     bool                   `json:"input-preserved"`
	Deterministic      bool                   `json:"deterministic"`
	EmptyNonNull       bool                   `json:"empty-non-null"`
	Merged             []Diagnostic           `json:"merged"`
	NilReportDiscarded bool                   `json:"nil-report-discarded"`
	Forwarded          []Diagnostic           `json:"forwarded"`
	Text               []diagnosticTextGolden `json:"text"`
}

type diagnosticTextGolden struct {
	Name     string   `json:"name"`
	Severity Severity `json:"severity"`
	Output   string   `json:"output"`
	Error    string   `json:"error"`
}

func TestDiagnosticContractGolden(t *testing.T) {
	end := &Position{Line: 2, Column: 4, Offset: 8}
	positioned := Diagnostic{
		Code:     "b",
		Severity: SeverityWarning,
		Message:  "positioned",
		Hint:     "hint",
		Path:     "a.ub",
		Span: &Span{
			Start: Position{Line: 2, Column: 1, Offset: 5},
			End:   end,
		},
	}
	duplicate := positioned
	duplicate.Span = &Span{Start: positioned.Span.Start, End: &Position{
		Line: end.Line, Column: end.Column, Offset: end.Offset,
	}}
	start := Position{Line: 1, Column: 1, Offset: 0}
	endOne := Position{Line: 1, Column: 2, Offset: 1}
	endTwo := Position{Line: 1, Column: 3, Offset: 2}
	in := []Diagnostic{
		{Code: "z", Severity: SeverityInfo, Message: "last", Path: "z.ub"},
		{Code: "z", Severity: SeverityInfo, Message: "unpositioned", Path: "a.ub"},
		{Code: "b", Severity: SeverityInfo, Message: "info", Path: "a.ub", Span: point(5)},
		{Code: "b", Severity: SeverityWarning, Message: "warning", Path: "a.ub", Span: point(5)},
		{Code: "b", Severity: SeverityError, Message: "error", Path: "a.ub", Span: point(5)},
		positioned,
		duplicate,
		{Code: "a", Severity: SeverityError, Message: "earlier", Path: "a.ub", Span: point(1)},
		{Code: "b", Severity: SeverityError, Message: "a", Hint: "b", Span: &Span{
			Start: start, End: &endTwo,
		}},
		{Code: "b", Severity: SeverityError, Message: "a", Hint: "a", Span: &Span{
			Start: start, End: &endTwo,
		}},
		{Code: "b", Severity: SeverityError, Message: "b", Span: &Span{
			Start: start, End: &endTwo,
		}},
		{Code: "a", Severity: SeverityError, Message: "z", Span: &Span{
			Start: start, End: &endTwo,
		}},
		{Code: "z", Severity: SeverityError, Message: "z", Span: &Span{
			Start: start, End: &endOne,
		}},
		{Code: "z", Severity: SeverityError, Message: "z", Span: &Span{Start: start}},
	}
	before := append([]Diagnostic(nil), in...)
	normalized := Normalize(in)
	deterministic := true
	for range 5 {
		if !reflect.DeepEqual(normalized, Normalize(in)) {
			deterministic = false
		}
	}

	forwarded := &recordingReporter{}
	Report(nil, Diagnostic{Severity: SeverityWarning, Message: "discarded"})
	Report(forwarded, Diagnostic{
		Code: "test", Severity: SeverityInfo, Message: "forwarded",
	})

	result := diagnosticGolden{
		Severities: []Severity{SeverityError, SeverityWarning, SeverityInfo},
		Tags:       diagnosticTags(),
		Normalized: normalized,
		InputPreserved: reflect.DeepEqual(
			in,
			before,
		),
		Deterministic:      deterministic,
		EmptyNonNull:       Normalize(nil) != nil,
		Merged:             Merge([]Diagnostic{positioned}, nil, []Diagnostic{duplicate}),
		NilReportDiscarded: true,
		Forwarded:          forwarded.diagnostics,
	}
	textCases := []struct {
		name     string
		severity Severity
		writer   func(*bytes.Buffer) interface{ Write([]byte) (int, error) }
	}{
		{name: "warning", severity: SeverityWarning},
		{name: "info", severity: SeverityInfo},
		{name: "error rejected", severity: SeverityError},
		{name: "unknown rejected", severity: "unknown"},
		{
			name: "writer failure", severity: SeverityInfo,
			writer: func(*bytes.Buffer) interface{ Write([]byte) (int, error) } {
				return errorWriter{}
			},
		},
	}
	for _, tc := range textCases {
		var out bytes.Buffer
		writer := interface{ Write([]byte) (int, error) }(&out)
		if tc.writer != nil {
			writer = tc.writer(&out)
		}
		err := WriteText(writer, Diagnostic{Severity: tc.severity, Message: "message"})
		result.Text = append(result.Text, diagnosticTextGolden{
			Name:     tc.name,
			Severity: tc.severity,
			Output:   out.String(),
			Error:    diagnosticErrorString(err),
		})
	}

	requireDiagnosticGolden(t, "testdata/diagnostic.json", result)
}

type collectorGolden struct {
	EmptyNonNull    bool         `json:"empty-non-null"`
	InitiallyErrors bool         `json:"initially-errors"`
	Normalized      []Diagnostic `json:"normalized"`
	CopyPreserved   []Diagnostic `json:"copy-preserved"`
	HasErrors       bool         `json:"has-errors"`
	Concurrent      []Diagnostic `json:"concurrent"`
	ConcurrentCount int          `json:"concurrent-count"`
}

func TestCollectorGolden(t *testing.T) {
	collector := &Collector{}
	empty := collector.Diagnostics()
	initiallyErrors := collector.HasErrors()
	d := Diagnostic{Code: "w", Severity: SeverityWarning, Message: "warning"}
	collector.Report(d)
	collector.Report(d)
	first := collector.Diagnostics()
	first[0].Message = "changed copy"
	copyPreserved := collector.Diagnostics()
	collector.Report(Diagnostic{Code: "e", Severity: SeverityError, Message: "error"})

	concurrent := &Collector{}
	const count = 16
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() {
			concurrent.Report(Diagnostic{
				Code:     "test",
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("message-%03d", i),
			})
		})
	}
	wg.Wait()
	concurrentDiagnostics := concurrent.Diagnostics()

	requireDiagnosticGolden(t, "testdata/collector.json", collectorGolden{
		EmptyNonNull:    empty != nil,
		InitiallyErrors: initiallyErrors,
		Normalized:      collector.Diagnostics(),
		CopyPreserved:   copyPreserved,
		HasErrors:       collector.HasErrors(),
		Concurrent:      concurrentDiagnostics,
		ConcurrentCount: len(concurrentDiagnostics),
	})
}

func diagnosticTags() map[string][2]string {
	out := map[string][2]string{}
	for _, typ := range []reflect.Type{
		reflect.TypeFor[Position](),
		reflect.TypeFor[Span](),
		reflect.TypeFor[Diagnostic](),
	} {
		for field := range typ.Fields() {
			out[typ.Name()+"."+field.Name] = [2]string{
				field.Tag.Get("json"),
				field.Tag.Get("ub"),
			}
		}
	}
	return out
}

type recordingReporter struct {
	diagnostics []Diagnostic
}

func (r *recordingReporter) Report(d Diagnostic) {
	r.diagnostics = append(r.diagnostics, d)
}

func point(offset int) *Span {
	return &Span{Start: Position{Line: 1, Column: offset + 1, Offset: offset}}
}

func diagnosticErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func requireDiagnosticGolden(t *testing.T, path string, value any) {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(value))
	got := buffer.Bytes()
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
